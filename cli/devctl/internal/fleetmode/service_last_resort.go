package fleetmode

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	serviceStopped     = 1
	serviceStopPending = 3
	serviceRunning     = 4
	serviceDisabled    = 4
	serviceOwnProcess  = 0x10
)

type wslServiceSnapshot struct {
	StartType, ConfigType, StatusType, State, PID uint32
	ImageCommand                                  string
}

type serviceProcessIdentity struct {
	PID     uint32
	Created uint64
	Image   string
	Exited  bool
}

type heldServiceProcess interface {
	Identity(context.Context) (serviceProcessIdentity, error)
	Terminate(context.Context) error
	Wait(context.Context) error
	Close() error
}

type wslServiceProcessOps interface {
	ReadWSLService(context.Context) (wslServiceSnapshot, error)
	OpenWSLServiceProcess(context.Context, uint32) (heldServiceProcess, error)
	ObserveOff(context.Context) (bool, error)
}

type heldWSLService struct {
	process  heldServiceProcess
	identity serviceProcessIdentity
	image    string
}

// Accept only a single local executable, not an ambiguous unquoted command or
// service arguments. Unknown installation layouts are a refusal, not a reason
// to select a process by name.
func configuredWSLServiceImage(command string) (string, error) {
	image := strings.TrimSpace(command)
	if strings.HasPrefix(image, `"`) {
		end := strings.IndexByte(image[1:], '"')
		if end < 0 || strings.TrimSpace(image[end+2:]) != "" {
			return "", errors.New("WSLService configured image is not a single executable")
		}
		image = image[1 : end+1]
	} else if strings.ContainsAny(image, " \t\r\n") {
		return "", errors.New("WSLService configured image is ambiguous")
	}
	return canonicalWSLServiceImage(image)
}

func canonicalWSLServiceImage(image string) (string, error) {
	image = strings.ReplaceAll(image, "/", `\`)
	image = strings.TrimPrefix(image, `\\?\`)
	if len(image) < 4 || image[1] != ':' || image[2] != '\\' ||
		!((image[0] >= 'A' && image[0] <= 'Z') || (image[0] >= 'a' && image[0] <= 'z')) ||
		strings.ContainsAny(image[2:], ":\x00\"\r\n") {
		return "", errors.New("WSLService image is not an identified local executable")
	}
	parts := strings.Split(image[3:], `\`)
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("WSLService image has an ambiguous path")
		}
	}
	if !strings.EqualFold(parts[len(parts)-1], "wslservice.exe") {
		return "", errors.New("WSLService image is not wslservice.exe")
	}
	return strings.ToLower(image), nil
}

func dedicatedDisabledWSLService(s wslServiceSnapshot) bool {
	return s.StartType == serviceDisabled && s.ConfigType == serviceOwnProcess && s.StatusType == serviceOwnProcess
}

func matchesHeldWSLService(s wslServiceSnapshot, held *heldWSLService, state uint32) bool {
	image, err := configuredWSLServiceImage(s.ImageCommand)
	return held != nil && err == nil && dedicatedDisabledWSLService(s) && s.State == state &&
		s.PID != 0 && s.PID == held.identity.PID && image == held.image
}

// QueryServiceStatusEx does not guarantee a valid PID in STOP_PENDING. Pin the
// actual process while RUNNING, before requesting shutdown, and retain its
// handle until all stop/readback work finishes. Never acquire a kill target
// for the first time from a pending-stop PID.
// https://learn.microsoft.com/en-us/windows/win32/api/winsvc/nf-winsvc-queryservicestatusex
func captureWSLServiceProcess(ctx context.Context, ops wslServiceProcessOps) (*heldWSLService, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s, err := ops.ReadWSLService(ctx)
	if err != nil {
		return nil, err
	}
	if s.StartType == serviceDisabled && s.State == serviceStopped {
		return nil, nil
	}
	image, err := configuredWSLServiceImage(s.ImageCommand)
	if err != nil || !dedicatedDisabledWSLService(s) || s.State != serviceRunning || s.PID == 0 {
		return nil, errors.New("no disabled dedicated running WSLService process to retain")
	}
	process, err := ops.OpenWSLServiceProcess(ctx, s.PID)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = process.Close()
		}
	}()
	identity, err := process.Identity(ctx)
	if err != nil {
		return nil, err
	}
	actualImage, imageErr := canonicalWSLServiceImage(identity.Image)
	if imageErr != nil || identity.PID != s.PID || identity.Created == 0 || identity.Exited || actualImage != image {
		return nil, errors.New("held WSLService process identity does not match its configured image")
	}
	held := &heldWSLService{process: process, identity: identity, image: image}
	again, err := ops.ReadWSLService(ctx)
	if err != nil {
		return nil, err
	}
	if !matchesHeldWSLService(again, held, serviceRunning) {
		return nil, errors.New("WSLService binding changed while retaining its running process")
	}
	keep = true
	return held, nil
}

// This is only the final pending-stop remedy after real WSL absence. The held
// reference came from RUNNING and is never replaced with a new PID lookup.
func finishPendingWSLService(ctx context.Context, ops wslServiceProcessOps, held *heldWSLService) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	s, err := ops.ReadWSLService(ctx)
	if err != nil {
		return err
	}
	if s.StartType == serviceDisabled && s.State == serviceStopped {
		return nil
	}
	if !matchesHeldWSLService(s, held, serviceStopPending) {
		return errors.New("pending WSLService lacks its retained exact running-process binding")
	}
	off, err := ops.ObserveOff(ctx)
	if err != nil || !off {
		return errors.New("WSL absence was not confirmed before the pending-service remedy")
	}
	identity, err := held.process.Identity(ctx)
	if err != nil {
		return err
	}
	image, err := canonicalWSLServiceImage(identity.Image)
	if err != nil || identity.PID != held.identity.PID || identity.Created != held.identity.Created || image != held.image {
		return errors.New("retained WSLService process identity changed")
	}
	again, err := ops.ReadWSLService(ctx)
	if err != nil {
		return err
	}
	if !matchesHeldWSLService(again, held, serviceStopPending) {
		return errors.New("WSLService binding changed before exact-process termination")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !identity.Exited {
		if err := held.process.Terminate(ctx); err != nil {
			return fmt.Errorf("terminate retained WSLService process: %w", err)
		}
	}
	waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Second)
	err = held.process.Wait(waitCtx)
	waitCancel()
	if err != nil {
		return fmt.Errorf("wait for retained WSLService process: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		after, err := ops.ReadWSLService(ctx)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if after.State == serviceStopped && dedicatedDisabledWSLService(after) {
			image, imageErr := configuredWSLServiceImage(after.ImageCommand)
			if imageErr != nil || image != held.image {
				return errors.New("WSLService configured image changed after termination")
			}
			return nil
		}
		if !matchesHeldWSLService(after, held, serviceStopPending) {
			return errors.New("WSLService no longer matches the disabled pending process after termination")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

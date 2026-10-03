package broker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func fixedStationJob(ctx context.Context, c Config, action string) error {
	if c.Owner == nil || c.Owner.Service != StationOwnerService || (action != "start" && action != "stop") {
		return fmt.Errorf("fixed station broker job identity is absent")
	}
	bounded, cancel := context.WithTimeout(ctx, c.StartTimeout+5*time.Second)
	defer cancel()
	uid := strconv.Itoa(os.Getuid())
	command := exec.CommandContext(bounded, c.Owner.Systemctl, "--user", "--no-pager", action, StationOwnerService)
	command.Env = []string{"PATH=/no-ambient-path", "LC_ALL=C", "XDG_RUNTIME_DIR=/run/user/" + uid, "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/" + uid + "/bus"}
	if err := command.Run(); err != nil {
		return fmt.Errorf("fixed station broker %s job failed: %w", action, err)
	}
	return nil
}
func requestStationStart(ctx context.Context, c Config, dryRun, explicit bool) (Status, error) {
	g, err := lockLifecycle(ctx, c, !dryRun)
	if err != nil {
		return newStatus(c), err
	}
	status := newStatus(c)
	if g != nil {
		status, err = inspectLocked(c, g)
		if err != nil {
			g.Close()
			return status, err
		}
		intent, err := readStopIntent(c, g)
		if err != nil {
			g.Close()
			return status, err
		}
		if status.BindingError != "" {
			g.Close()
			return status, fmt.Errorf("refusing foreign broker binding: %s", status.BindingError)
		}
		if intent != nil {
			if !explicit || intent.InProgress {
				g.Close()
				return status, fmt.Errorf("shared broker owner is deliberately stopped; automatic acquisition is inhibited")
			}
			if !dryRun {
				if err = g.remove(stopIntentFile(c)); err != nil {
					g.Close()
					return status, err
				}
			}
		}
		if status.Running {
			g.Close()
			return status, nil
		}
		if processRunning(status.PID) {
			g.Close()
			return status, fmt.Errorf("matching broker owner is alive but not functionally ready: %s", status.Message)
		}
		g.Close()
	}
	status.Command = []string{c.Owner.Systemctl, "--user", "start", StationOwnerService}
	if dryRun {
		status.Message = "dry run: source-selected station owner would be requested"
		return status, nil
	}
	// Release the mutation lock before asking the unit: ExecStart needs it.
	if err = fixedStationJob(ctx, c, "start"); err != nil {
		return status, err
	}
	g, err = lockLifecycle(ctx, c, false)
	if err != nil {
		return status, err
	}
	if g == nil {
		return status, fmt.Errorf("station broker job did not establish its source-owned state")
	}
	defer g.Close()
	intent, err := readStopIntent(c, g)
	if err != nil {
		return status, err
	}
	if intent != nil {
		return status, fmt.Errorf("station broker acquisition crossed a deliberate stop boundary")
	}
	status, err = inspectLocked(c, g)
	if err != nil {
		return status, err
	}
	if !status.Running {
		return status, fmt.Errorf("station broker job failed functional admission: %s", status.Message)
	}
	return status, nil
}

// Query only the fixed unit's nonsecret runtime identity before authorizing
// its stop job. An active owner without matching retained metadata is refused.
func fixedStationStopBinding(c Config, status Status) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.StartTimeout+5*time.Second)
	defer cancel()
	uid := strconv.Itoa(os.Getuid())
	command := exec.CommandContext(ctx, c.Owner.Systemctl, "--user", "--no-pager", "show", "--property=MainPID", "--property=ActiveState", StationOwnerService)
	command.Env = []string{"PATH=/no-ambient-path", "LC_ALL=C", "XDG_RUNTIME_DIR=/run/user/" + uid, "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/" + uid + "/bus"}
	data, err := command.Output()
	if err != nil {
		return fmt.Errorf("inspect fixed station owner before stop: %w", err)
	}
	var pid int
	var phase string
	seenPID := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MainPID=") {
			pid, err = strconv.Atoi(strings.TrimPrefix(line, "MainPID="))
			if err != nil || pid < 0 || seenPID {
				return fmt.Errorf("fixed station MainPID is malformed")
			}
			seenPID = true
		} else if strings.HasPrefix(line, "ActiveState=") {
			if phase != "" {
				return fmt.Errorf("fixed station state is ambiguous")
			}
			phase = strings.TrimPrefix(line, "ActiveState=")
		}
	}
	if !seenPID || phase == "" {
		return fmt.Errorf("fixed station stop identity is absent")
	}
	if pid == 0 && (phase == "inactive" || phase == "failed") {
		return nil
	}
	if phase != "active" || pid == 0 || pid != status.PID || status.State == nil || !managedProcess(c, status.State) {
		return fmt.Errorf("refusing unrecorded or different active station owner")
	}
	return nil
}

func requestStationStop(c Config, dryRun bool) (Status, error) {
	g, err := lockLifecycle(context.Background(), c, !dryRun)
	if err != nil {
		return newStatus(c), err
	}
	status := newStatus(c)
	if g == nil {
		return status, nil
	}
	defer g.Close()
	status, err = inspectLocked(c, g)
	if err != nil {
		return status, err
	}
	previousIntent, err := readStopIntent(c, g)
	if err != nil {
		return status, err
	}
	// A failed job may already have retired systemd's PIDFile. Its persisted
	// exact state/PID witness admits only a typed retry of that same owner.
	if status.PID == 0 && g.files[PIDFile(c)] == nil && previousIntent != nil && previousIntent.InProgress &&
		status.State != nil && status.BindingError == "broker PID and state bindings disagree" &&
		previousIntent.RecordedPID == status.State.PID && previousIntent.StateSHA256 == status.StateDigest && previousIntent.PIDSHA256 != "" {
		status.PID, status.PIDDigest, status.BindingError = previousIntent.RecordedPID, previousIntent.PIDSHA256, ""
	}
	if status.BindingError != "" {
		return status, fmt.Errorf("refusing foreign broker stop binding: %s", status.BindingError)
	}
	retired, err := recordedProcessRetired(status)
	if err != nil {
		return status, err
	}
	if !retired && !managedProcess(c, status.State) {
		return status, fmt.Errorf("refusing different broker process before station stop")
	}
	if retired && status.SocketExists {
		if _, err = staleSocketOwner(c, status); err != nil {
			return status, err
		}
	}
	if err = fixedStationStopBinding(c, status); err != nil {
		return status, err
	}
	if dryRun {
		status.Message = "dry run: fixed station broker would be stopped"
		return status, nil
	}

	// Keep open descriptors in custody while releasing only the lock. A new
	// pathname or recycled inode cannot become the owner of the pending job.
	intent := stopIntent{Generation: time.Now().UnixNano(), InProgress: true, RecordedPID: status.PID, StateSHA256: status.StateDigest, PIDSHA256: status.PIDDigest}
	if err = writeStopIntent(c, g, intent); err != nil {
		return status, err
	}
	intentBytes, err := g.read(stopIntentFile(c))
	if err != nil {
		return status, err
	}
	var retained []*os.File
	defer func() {
		for _, f := range retained {
			if f != nil {
				f.Close()
			}
		}
	}()
	for _, path := range []string{PIDFile(c), StateFile(c), stopIntentFile(c)} {
		f, err := g.retainFile(path)
		if err != nil {
			return status, err
		}
		retained = append(retained, f)
	}
	socket, socketIdentity, err := g.retainSocket(c.Socket)
	if err != nil {
		return status, err
	}
	retained = append(retained, socket)
	if err = g.unlock(); err != nil {
		return status, err
	}
	jobErr := fixedStationJob(context.Background(), c, "stop")
	afterGuard, err := lockLifecycle(context.Background(), c, false)
	if err != nil {
		return status, errors.Join(jobErr, err)
	}
	if afterGuard == nil {
		return status, errors.Join(jobErr, fmt.Errorf("station broker stop lost its state identity"))
	}
	defer afterGuard.Close()
	if !sameRetainedFile(g.identity, afterGuard.identity) {
		return status, errors.Join(jobErr, fmt.Errorf("station broker root changed during stop job"))
	}
	// Systemd removes the configured PIDFile after service shutdown. Its
	// original descriptor remains held; disappearance may retire custody,
	// whereas a replacement pathname may never acquire it.
	pidRemoved := g.files[PIDFile(c)] != nil && afterGuard.files[PIDFile(c)] == nil
	for _, path := range []string{PIDFile(c), StateFile(c), stopIntentFile(c)} {
		if path == PIDFile(c) && pidRemoved {
			continue
		}
		if !sameRetainedFile(g.files[path], afterGuard.files[path]) {
			return status, errors.Join(jobErr, fmt.Errorf("station broker metadata changed during stop job: %s", path))
		}
	}
	if retained[0] != nil {
		data, err := io.ReadAll(io.LimitReader(retained[0], 32769))
		digest := sha256.Sum256(data)
		if err != nil || len(data) > 32768 || hex.EncodeToString(digest[:]) != status.PIDDigest {
			return status, errors.Join(jobErr, fmt.Errorf("retained broker PID bytes changed during stop job"))
		}
	}
	currentIntentBytes, err := afterGuard.read(stopIntentFile(c))
	if err != nil || !bytes.Equal(intentBytes, currentIntentBytes) {
		return status, errors.Join(jobErr, fmt.Errorf("station broker stop intent bytes changed during job"), err)
	}
	now, err := readStopIntent(c, afterGuard)
	if err != nil {
		return status, errors.Join(jobErr, err)
	}
	if now == nil || now.Generation != intent.Generation || !now.InProgress {
		return status, errors.Join(jobErr, fmt.Errorf("station broker stop generation changed"))
	}
	if jobErr != nil {
		return status, jobErr
	}
	after, err := inspectLocked(c, afterGuard)
	if err != nil {
		return status, err
	}
	if afterGuard.files[PIDFile(c)] == nil && status.State != nil && status.PIDDigest != "" {
		if after.PID != 0 || after.State == nil || after.State.PID != status.PID ||
			after.StateDigest != status.StateDigest || after.BindingError != "broker PID and state bindings disagree" {
			return after, fmt.Errorf("station broker binding changed during PIDFile retirement")
		}
		// Exact unchanged state plus the retained PID bytes identify the same
		// process even though systemd retired its named PIDFile.
		after.PID, after.PIDDigest, after.BindingError = status.PID, status.PIDDigest, ""
	}
	if after.BindingError != "" || after.StateDigest != status.StateDigest || after.PIDDigest != status.PIDDigest {
		return after, fmt.Errorf("station broker binding changed during stop job")
	}
	retired, err = recordedProcessRetired(after)
	if err != nil {
		return after, err
	}
	if !retired {
		return after, fmt.Errorf("station broker stop job did not retire its exact owner")
	}
	currentSocket, err := socketInfo(c.Socket)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return after, err
	}
	if currentSocket != nil && !sameRetainedFile(socketIdentity, currentSocket) {
		return after, fmt.Errorf("station broker socket changed during stop job")
	}
	// Retire only the recorded inode or exact source-owned legacy witness.
	if err = retireStaleSocket(c, after); err != nil {
		return after, err
	}
	if err = cleanupStateLocked(c, afterGuard, after.State); err != nil {
		return after, err
	}
	intent.InProgress = false
	if err = writeStopIntent(c, afterGuard, intent); err != nil {
		return after, err
	}
	after = newStatus(c)
	after.Message = "station broker is deliberately stopped"
	return after, nil
}

func sameRetainedFile(before, after os.FileInfo) bool {
	if before == nil || after == nil {
		return before == nil && after == nil
	}
	return os.SameFile(before, after)
}

//go:build windows

package fleetmode

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Only WSL's own service is changed. vmcompute and unrelated Hyper-V VMs are
// outside this command. Startup mode is retained before the first inhibition.
const serviceDisabled = 4
const serviceStopped = 1

var advapi = syscall.NewLazyDLL("advapi32.dll")
var openSCManager = advapi.NewProc("OpenSCManagerW")
var openService = advapi.NewProc("OpenServiceW")
var closeService = advapi.NewProc("CloseServiceHandle")
var queryServiceConfig = advapi.NewProc("QueryServiceConfigW")
var changeServiceConfig = advapi.NewProc("ChangeServiceConfigW")
var queryServiceStatus = advapi.NewProc("QueryServiceStatusEx")
var controlService = advapi.NewProc("ControlService")

type windowsOps struct{ cfg AgentConfig }
type serviceConfig struct {
	ServiceType, StartType, ErrorControl uint32
	BinaryPath, LoadOrderGroup           *uint16
	TagID                                uint32
	Dependencies, Account, DisplayName   *uint16
}
type serviceStatus struct {
	ServiceType, CurrentState, ControlsAccepted, Win32ExitCode             uint32
	ServiceSpecificExitCode, CheckPoint, WaitHint, ProcessID, ServiceFlags uint32
}

func NewAgentOps(cfg AgentConfig) (AgentOps, error) {
	if !strings.EqualFold(filepath.Clean(filepath.Dir(cfg.StatePath)), `C:\ProgramData\FleetMode`) {
		return nil, errors.New("Windows state must be installed in C:\\ProgramData\\FleetMode")
	}
	return &windowsOps{cfg: cfg}, nil
}

func (o *windowsOps) Hostname() (string, error) { return os.Hostname() }
func (o *windowsOps) RecordMode(ctx context.Context, mode string) error {
	return (FileState{Path: o.cfg.StatePath}).Record(ctx, mode, []string{o.cfg.Name})
}

func withWSLService(access uint32, f func(uintptr) error) error {
	scm, _, err := openSCManager.Call(0, 0, 1)
	if scm == 0 {
		return fmt.Errorf("open service manager: %w", err)
	}
	defer closeService.Call(scm)
	name, _ := syscall.UTF16PtrFromString("WSLService")
	h, _, err := openService.Call(scm, uintptr(unsafe.Pointer(name)), uintptr(access))
	if h == 0 {
		return fmt.Errorf("open WSLService: %w", err)
	}
	defer closeService.Call(h)
	return f(h)
}

func readService(h uintptr) (uint32, uint32, error) {
	buffer := make([]byte, 8192)
	var needed uint32
	ok, _, err := queryServiceConfig.Call(h, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), uintptr(unsafe.Pointer(&needed)))
	if ok == 0 {
		return 0, 0, fmt.Errorf("query WSLService configuration: %w", err)
	}
	config := (*serviceConfig)(unsafe.Pointer(&buffer[0]))
	var status serviceStatus
	ok, _, err = queryServiceStatus.Call(h, 0, uintptr(unsafe.Pointer(&status)), unsafe.Sizeof(status), uintptr(unsafe.Pointer(&needed)))
	if ok == 0 {
		return 0, 0, fmt.Errorf("query WSLService status: %w", err)
	}
	return config.StartType, status.CurrentState, nil
}

func (o *windowsOps) disableService(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return withWSLService(1|2|4, func(h uintptr) error {
		start, _, err := readService(h)
		if err != nil {
			return err
		}
		path := filepath.Join(filepath.Dir(o.cfg.StatePath), "service-before-vr.json")
		if err := retainRecoverySnapshot(path, recoverySnapshot{SchemaVersion: 1, Hostname: o.cfg.ExpectedHostname, Kind: "service", Name: "WSLService", StartType: &start}); err != nil {
			return fmt.Errorf("retain original WSL startup mode: %w", err)
		}
		if start != serviceDisabled {
			if err := ctx.Err(); err != nil {
				return err
			}
			ok, _, err := changeServiceConfig.Call(h, 0xffffffff, serviceDisabled, 0xffffffff, 0, 0, 0, 0, 0, 0, 0)
			if ok == 0 {
				return fmt.Errorf("disable WSLService restart: %w", err)
			}
		}
		start, _, err = readService(h)
		if err != nil {
			return err
		}
		if start != serviceDisabled {
			return errors.New("WSLService restart inhibition did not read back")
		}
		return nil
	})
}

func stage(name string, started time.Time, err error) Stage {
	s := Stage{Name: name, ElapsedMillis: time.Since(started).Milliseconds()}
	if err != nil {
		s.ExitCode = 1
		s.Error = err.Error()
	}
	return s
}

func command(ctx context.Context, budget time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	output := &cappedOutput{limit: 1 << 20}
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if output.overflow {
		return nil, errors.New("Windows command exceeded output limit")
	}
	return output.Bytes(), err
}

func systemExe(name string) string { return filepath.Join(os.Getenv("SystemRoot"), "System32", name) }

func (o *windowsOps) InhibitRestart(ctx context.Context, cfg AgentConfig) ([]Stage, bool) {
	started := time.Now()
	err := o.disableService(ctx)
	stages := []Stage{stage("inhibit-wsl-service-restart", started, err)}
	ok := err == nil
	for _, task := range cfg.KeepAliveTasks {
		started = time.Now()
		out, err := command(ctx, 3*time.Second, systemExe("schtasks.exe"), "/Query", "/TN", task, "/XML")
		var enabled bool
		if err == nil {
			enabled, err = parseTaskEnabled(out)
		}
		if err == nil {
			digest := sha256.Sum256([]byte(strings.ToLower(task)))
			path := filepath.Join(filepath.Dir(cfg.StatePath), fmt.Sprintf("task-%x-before-vr.json", digest))
			err = retainRecoverySnapshot(path, recoverySnapshot{SchemaVersion: 1, Hostname: cfg.ExpectedHostname, Kind: "task", Name: task, Enabled: &enabled})
		}
		stages = append(stages, stage("retain-known-keeper-task-state", started, err))
		if err != nil {
			ok = false
			continue
		}
		started = time.Now()
		_, err = command(ctx, 5*time.Second, systemExe("schtasks.exe"), "/Change", "/TN", task, "/Disable")
		stages = append(stages, stage("disable-known-keeper-task", started, err))
		if err != nil {
			ok = false
		}
		// End may return a nonzero status for an already stopped task. The final
		// service-disabled-and-stopped observation is the restart invariant.
		started = time.Now()
		_, endErr := command(ctx, 5*time.Second, systemExe("schtasks.exe"), "/End", "/TN", task)
		stages = append(stages, stage("end-known-keeper-task", started, endErr))
	}
	return stages, ok
}

func (o *windowsOps) ObserveRestartInhibition(ctx context.Context, cfg AgentConfig) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var inhibited bool
	err := withWSLService(1|4, func(h uintptr) error {
		start, state, err := readService(h)
		inhibited = err == nil && start == serviceDisabled && state == serviceStopped
		return err
	})
	if err != nil || !inhibited {
		return false, err
	}
	for _, task := range cfg.KeepAliveTasks {
		out, err := command(ctx, 3*time.Second, systemExe("schtasks.exe"), "/Query", "/TN", task, "/XML")
		if err != nil {
			return false, err
		}
		enabled, err := parseTaskEnabled(out)
		if err != nil {
			return false, err
		}
		if enabled {
			return false, nil
		}
	}
	return true, nil
}

func stopWSLService(ctx context.Context) error {
	return withWSLService(1|4|0x20, func(h uintptr) error {
		_, state, err := readService(h)
		if err != nil || state == serviceStopped {
			return err
		}
		var status serviceStatus
		ok, _, err := controlService.Call(h, 1, uintptr(unsafe.Pointer(&status)))
		if ok == 0 && err != syscall.Errno(1062) && err != syscall.Errno(1061) {
			return err
		}
		for {
			_, state, err = readService(h)
			if err != nil || state == serviceStopped {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
	})
}

func (o *windowsOps) ForceWSLOff(ctx context.Context) ([]Stage, bool) {
	started := time.Now()
	_, err := command(ctx, 35*time.Second, systemExe("wsl.exe"), "--shutdown", "--force")
	stages := []Stage{stage("wsl-shutdown-force", started, err)}
	started = time.Now()
	stopCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	err = stopWSLService(stopCtx)
	cancel()
	stages = append(stages, stage("stop-wsl-service", started, err))
	// An acknowledgement or a dead SSH connection does not establish VM absence.
	started = time.Now()
	ids, enumErr := hcsWSLSystems(ctx)
	if enumErr == nil {
		for _, id := range ids {
			terminateCtx, terminateCancel := context.WithTimeout(ctx, 15*time.Second)
			err = hcsTerminate(terminateCtx, id)
			terminateCancel()
			stages = append(stages, stage("terminate-wsl-compute-system", started, err))
			started = time.Now()
		}
	} else {
		stages = append(stages, stage("enumerate-wsl-compute-systems", started, enumErr))
	}
	for {
		off, err := o.ObserveOff(ctx)
		if err == nil && off {
			return stages, true
		}
		select {
		case <-ctx.Done():
			return append(stages, stage("observe-wsl-off", started, ctx.Err())), false
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (o *windowsOps) ObserveOff(ctx context.Context) (bool, error) {
	ids, err := hcsWSLSystems(ctx)
	if err != nil {
		return false, err
	}
	present, err := nativeVmmemWSLPresent()
	return err == nil && len(ids) == 0 && !present, err
}

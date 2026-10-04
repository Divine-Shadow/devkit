//go:build linux

package broker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Linux uses these numbers on the supported x86_64 and aarch64 runtimes.
// Unavailable pidfds fail closed instead of signalling a reused PID.
const pidfdOpenSyscall = 434
const pidfdSendSignalSyscall = 424

func processStart(pid int) (uint64, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return 0, fmt.Errorf("process stat has no command boundary")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return 0, fmt.Errorf("process stat lacks start identity")
	}
	return strconv.ParseUint(fields[19], 10, 64)
}
func managedProcess(c Config, state *State) bool {
	if state == nil || state.PID < 1 || state.StartTicks == 0 {
		return false
	}
	if c.Owner != nil && !inStationOwner() {
		return observedStationProcess(c, state)
	}
	return directManagedProcess(c, state)
}

func directManagedProcess(c Config, state *State) bool {
	if state == nil || state.PID < 1 || state.StartTicks == 0 {
		return false
	}
	before, err := processStart(state.PID)
	if err != nil || before != state.StartTicks {
		return false
	}
	exe, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(state.PID), "exe"))
	wanted, resolveErr := filepath.EvalSymlinks(c.Binary)
	if err != nil || resolveErr != nil || filepath.Clean(exe) != filepath.Clean(wanted) {
		return false
	}
	if c.Owner != nil {
		group, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(state.PID), "cgroup"))
		if err != nil || !stationOwnerCgroup(string(group)) {
			return false
		}
	}
	after, err := processStart(state.PID)
	return err == nil && after == before
}
func signalManaged(c Config, state *State, signal syscall.Signal) error {
	if state == nil {
		return fmt.Errorf("broker process identity is absent")
	}
	fd, _, errno := syscall.Syscall(pidfdOpenSyscall, uintptr(state.PID), 0, 0)
	if errno != 0 {
		if errno == syscall.ESRCH {
			return nil
		}
		return fmt.Errorf("retain broker process identity: %w", errno)
	}
	defer syscall.Close(int(fd))
	if !managedProcess(c, state) {
		return fmt.Errorf("refusing to signal a different broker process")
	}
	_, _, errno = syscall.Syscall6(pidfdSendSignalSyscall, fd, uintptr(signal), 0, 0, 0, 0)
	if errno != 0 && errno != syscall.ESRCH {
		return fmt.Errorf("signal retained broker identity: %w", errno)
	}
	return nil
}

// Only disappearance or a matching retired process proves death. Permission,
// parsing and reused-PID failures are unknown/foreign identities, never absence.
func recordedProcessRetired(status Status) (bool, error) {
	if status.PID == 0 && status.State == nil {
		return true, nil
	}
	if status.PID < 1 || status.State == nil || status.PID != status.State.PID {
		return false, fmt.Errorf("recorded broker process binding is absent")
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(status.PID), "stat"))
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("broker process retirement is unknown: %w", err)
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return false, fmt.Errorf("broker process retirement stat is malformed")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return false, fmt.Errorf("broker process retirement lacks start identity")
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || status.State.StartTicks == 0 || ticks != status.State.StartTicks {
		return false, fmt.Errorf("broker PID was reused or has unknown start identity")
	}
	return fields[0] == "Z" || fields[0] == "X", nil
}

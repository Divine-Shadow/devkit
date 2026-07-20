package execx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRunManagedAllowsActiveCommandBeyondIdleWindow(t *testing.T) {
	result := RunManaged(
		context.Background(),
		ManagedPolicy{IdleTimeout: time.Second, TerminationGrace: 100 * time.Millisecond},
		"sh", "-c", `for value in 1 2 3 4 5 6; do printf 'progress-%s\n' "$value"; sleep 0.2; done`,
	)
	if result.Code != 0 || result.Err != nil {
		t.Fatalf("active managed command = %#v, want success", result)
	}
}

func TestRunManagedIdleTimeoutTerminatesDescendantGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "descendant.pid")
	script := `
trap '' TERM
sh -c 'trap "" TERM; echo $$ > "$1"; while :; do sleep 1; done' descendant "$1" &
wait
`
	result := RunManaged(
		context.Background(),
		ManagedPolicy{IdleTimeout: 150 * time.Millisecond, TerminationGrace: 100 * time.Millisecond},
		"sh", "-c", script, "parent", pidFile,
	)
	if result.Code != 124 || !errors.Is(result.Err, ErrIdleTimeout) {
		t.Fatalf("idle managed command = %#v, want idle timeout code 124", result)
	}
	descendantPID := readManagedTestPID(t, pidFile)
	assertManagedTestProcessGone(t, descendantPID)
}

func TestRunManagedContextDeadlineTerminatesDescendantGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "descendant.pid")
	script := `
trap '' TERM
sh -c 'trap "" TERM; echo $$ > "$1"; while :; do sleep 1; done' descendant "$1" &
wait
`
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	result := RunManaged(
		ctx,
		ManagedPolicy{TerminationGrace: 100 * time.Millisecond},
		"sh", "-c", script, "parent", pidFile,
	)
	if result.Code != 124 || !errors.Is(result.Err, context.DeadlineExceeded) {
		t.Fatalf("deadline managed command = %#v, want deadline code 124", result)
	}
	descendantPID := readManagedTestPID(t, pidFile)
	assertManagedTestProcessGone(t, descendantPID)
}

func TestRunManagedPreservesCommandExitClassification(t *testing.T) {
	result := RunManaged(
		context.Background(),
		ManagedPolicy{IdleTimeout: time.Second, TerminationGrace: 100 * time.Millisecond},
		"sh", "-c", "exit 23",
	)
	if result.Code != 23 {
		t.Fatalf("managed exit code = %d, want 23 (error %v)", result.Code, result.Err)
	}
	if errors.Is(result.Err, ErrIdleTimeout) || errors.Is(result.Err, context.DeadlineExceeded) {
		t.Fatalf("ordinary exit was misclassified as lifecycle timeout: %v", result.Err)
	}
}

func readManagedTestPID(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read descendant PID: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse descendant PID %q: %v", data, err)
	}
	return pid
}

func assertManagedTestProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if !managedTestProcessAlive(pid) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("managed descendant %d survived cancellation", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func managedTestProcessAlive(pid int) bool {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		return true
	}
	fields := strings.Fields(string(data))
	return len(fields) < 3 || fields[2] != "Z"
}

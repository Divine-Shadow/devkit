package execx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunManagedAllowsActiveCommandBeyondIdleWindow(t *testing.T) {
	result := RunManaged(
		context.Background(),
		ManagedPolicy{IdleTimeout: 120 * time.Millisecond, TerminationGrace: 100 * time.Millisecond},
		"sh", "-c", `for value in 1 2 3 4; do printf 'progress-%s\n' "$value"; sleep 0.08; done`,
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

func TestCaptureManagedOutputLimitTerminatesDescendantGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "descendant.pid")
	script := `
trap '' TERM
sh -c 'trap "" TERM; echo $$ > "$1"; while :; do printf 0123456789; done' descendant "$1" &
wait
`
	output, result := CaptureManaged(
		context.Background(),
		ManagedPolicy{TerminationGrace: 100 * time.Millisecond},
		1024,
		"sh", "-c", script, "parent", pidFile,
	)
	if result.Code != 1 || !errors.Is(result.Err, ErrOutputLimit) {
		t.Fatalf("bounded capture = %#v, want output-limit failure", result)
	}
	if len(output) != 1024 {
		t.Fatalf("bounded capture retained %d bytes, want exactly 1024", len(output))
	}
	descendantPID := readManagedTestPID(t, pidFile)
	assertManagedTestProcessGone(t, descendantPID)
}

func TestCaptureManagedLeaderExitCleansRedirectedDescendantBeforeReturn(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "descendant.pid")
	script := `
sh -c 'trap "" TERM; echo $$ > "$1"; while :; do sleep 1; done' descendant "$1" >/dev/null 2>&1 &
while [ ! -s "$1" ]; do :; done
exit 0
`
	output, result := CaptureManaged(
		context.Background(),
		ManagedPolicy{TerminationGrace: 100 * time.Millisecond},
		1024,
		"sh", "-c", script, "parent", pidFile,
	)
	if output != "" || result.Code != 0 || result.Err != nil {
		t.Fatalf("normal leader exit capture = output %q result %#v, want success", output, result)
	}
	assertManagedTestProcessGoneAtReturn(t, readManagedTestPID(t, pidFile))
}

func TestCaptureManagedOverflowAndLeaderExitCleanDescendantBeforeReturn(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "descendant.pid")
	script := `
sh -c 'trap "" TERM; echo $$ > "$1"; while :; do sleep 1; done' descendant "$1" >/dev/null 2>&1 &
while [ ! -s "$1" ]; do :; done
printf 0123456789abcdef
exit 0
`
	output, result := CaptureManaged(
		context.Background(),
		ManagedPolicy{TerminationGrace: 100 * time.Millisecond},
		8,
		"sh", "-c", script, "parent", pidFile,
	)
	if output != "01234567" || result.Code != 1 || !errors.Is(result.Err, ErrOutputLimit) {
		t.Fatalf("overflow/leader-exit capture = output %q result %#v", output, result)
	}
	assertManagedTestProcessGoneAtReturn(t, readManagedTestPID(t, pidFile))
}

func TestCaptureManagedContextDeadlineTerminatesDescendantGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "descendant.pid")
	script := `
trap '' TERM
sh -c 'trap "" TERM; echo $$ > "$1"; while :; do sleep 1; done' descendant "$1" &
wait
`
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, result := CaptureManaged(
		ctx,
		ManagedPolicy{TerminationGrace: 100 * time.Millisecond},
		1024,
		"sh", "-c", script, "parent", pidFile,
	)
	if result.Code != 124 || !errors.Is(result.Err, context.DeadlineExceeded) {
		t.Fatalf("deadline bounded capture = %#v, want deadline code 124", result)
	}
	descendantPID := readManagedTestPID(t, pidFile)
	assertManagedTestProcessGone(t, descendantPID)
}

func TestCaptureManagedExplicitCancelCleansLeaderExitDescendantBeforeReturn(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "descendant.pid")
	script := `
trap 'exit 0' TERM
sh -c 'trap "" TERM; echo $$ > "$1"; while :; do sleep 1; done' descendant "$1" >/dev/null 2>&1 &
while [ ! -s "$1" ]; do :; done
while :; do sleep 1; done
`
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type captureResult struct {
		output string
		result Result
	}
	completed := make(chan captureResult, 1)
	go func() {
		output, result := CaptureManaged(
			ctx,
			ManagedPolicy{TerminationGrace: 100 * time.Millisecond},
			1024,
			"sh", "-c", script, "parent", pidFile,
		)
		completed <- captureResult{output: output, result: result}
	}()

	descendantPID := waitManagedTestPID(t, pidFile)
	cancel()
	captured := <-completed
	if captured.output != "" || captured.result.Code != 130 || !errors.Is(captured.result.Err, context.Canceled) {
		t.Fatalf("canceled leader-exit capture = output %q result %#v", captured.output, captured.result)
	}
	assertManagedTestProcessGoneAtReturn(t, descendantPID)
}

func TestCaptureManagedIdleCleansLeaderExitDescendantBeforeReturn(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "descendant.pid")
	script := `
trap 'exit 0' TERM
sh -c 'trap "" TERM; echo $$ > "$1"; while :; do sleep 1; done' descendant "$1" >/dev/null 2>&1 &
while [ ! -s "$1" ]; do :; done
while :; do sleep 1; done
`
	output, result := CaptureManaged(
		context.Background(),
		ManagedPolicy{IdleTimeout: 150 * time.Millisecond, TerminationGrace: 100 * time.Millisecond},
		1024,
		"sh", "-c", script, "parent", pidFile,
	)
	if output != "" || result.Code != 124 || !errors.Is(result.Err, ErrIdleTimeout) {
		t.Fatalf("idle leader-exit capture = output %q result %#v", output, result)
	}
	assertManagedTestProcessGoneAtReturn(t, readManagedTestPID(t, pidFile))
}

func TestCaptureManagedKeepsCaptureClosedStdinSemantics(t *testing.T) {
	inheritedReader, inheritedWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previousStdin := os.Stdin
	os.Stdin = inheritedReader
	defer func() {
		os.Stdin = previousStdin
		_ = inheritedReader.Close()
		_ = inheritedWriter.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	output, result := CaptureManaged(
		ctx,
		ManagedPolicy{TerminationGrace: 100 * time.Millisecond},
		1024,
		"sh", "-c", `if IFS= read -r value; then exit 42; fi; printf closed`,
	)
	if result.Code != 0 || result.Err != nil || output != "closed" {
		t.Fatalf("closed-stdin capture = output %q result %#v, want closed/zero", output, result)
	}
}

func TestCaptureManagedPreservesOrdinaryResults(t *testing.T) {
	for _, test := range []struct {
		name       string
		script     string
		wantOutput string
		wantCode   int
	}{
		{name: "success", script: `printf success`, wantOutput: "success"},
		{name: "nonzero", script: `printf failure; exit 23`, wantOutput: "failure", wantCode: 23},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, result := CaptureManaged(
				context.Background(),
				ManagedPolicy{TerminationGrace: 100 * time.Millisecond},
				1024,
				"sh", "-c", test.script,
			)
			if output != test.wantOutput || result.Code != test.wantCode {
				t.Fatalf("ordinary capture = output %q result %#v", output, result)
			}
			if test.wantCode == 0 && result.Err != nil {
				t.Fatalf("successful capture returned error: %v", result.Err)
			}
			if test.wantCode != 0 && result.Err == nil {
				t.Fatal("nonzero capture returned no error")
			}
		})
	}
}

func TestCaptureManagedExactOutputLimitSucceeds(t *testing.T) {
	output, result := CaptureManaged(
		context.Background(),
		ManagedPolicy{TerminationGrace: 100 * time.Millisecond},
		8,
		"sh", "-c", `printf 12345678`,
	)
	if output != "12345678" || result.Code != 0 || result.Err != nil {
		t.Fatalf("exact-limit capture = output %q result %#v, want success", output, result)
	}
}

func TestCaptureManagedIdleTimeout(t *testing.T) {
	_, result := CaptureManaged(
		context.Background(),
		ManagedPolicy{IdleTimeout: 100 * time.Millisecond, TerminationGrace: 100 * time.Millisecond},
		1024,
		"sh", "-c", `sleep 5`,
	)
	if result.Code != 124 || !errors.Is(result.Err, ErrIdleTimeout) {
		t.Fatalf("idle capture = %#v, want idle timeout code 124", result)
	}
}

func TestCaptureManagedExplicitCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, result := CaptureManaged(
		ctx,
		ManagedPolicy{TerminationGrace: 100 * time.Millisecond},
		1024,
		"sh", "-c", `sleep 5`,
	)
	if result.Code != 130 || !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("canceled capture = %#v, want canceled code 130", result)
	}
}

func TestManagedCaptureResultPreservesSelectedLifecycleCause(t *testing.T) {
	for _, test := range []struct {
		name  string
		cause error
		code  int
	}{
		{name: "deadline", cause: context.DeadlineExceeded, code: 124},
		{name: "cancel", cause: context.Canceled, code: 130},
		{name: "idle", cause: ErrIdleTimeout, code: 124},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := managedCaptureResult(nil, test.cause, true)
			if result.Code != test.code || !errors.Is(result.Err, test.cause) {
				t.Fatalf("selected cause result = %#v, want %v/code %d", result, test.cause, test.code)
			}
			if errors.Is(result.Err, ErrOutputLimit) {
				t.Fatalf("selected lifecycle cause was overwritten by output limit: %v", result.Err)
			}
		})
	}
	result := managedCaptureResult(nil, nil, true)
	if result.Code != 1 || !errors.Is(result.Err, ErrOutputLimit) {
		t.Fatalf("normal completion with overflow = %#v, want output-limit failure", result)
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

func waitManagedTestPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && len(strings.TrimSpace(string(data))) > 0 {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr != nil {
				t.Fatalf("parse descendant PID %q: %v", data, parseErr)
			}
			return pid
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read descendant PID: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("descendant PID was not recorded before cancellation: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertManagedTestProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("managed descendant %d survived cancellation: %v", pid, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func assertManagedTestProcessGoneAtReturn(t *testing.T, pid int) {
	t.Helper()
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("managed descendant %d remained at function return: %v", pid, err)
	}
}

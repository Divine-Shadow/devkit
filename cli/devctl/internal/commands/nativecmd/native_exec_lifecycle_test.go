package nativecmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"devkit/cli/devctl/internal/cmdregistry"
)

func TestNativeExecSettlesRedirectedDescendantBeforeReturn(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "owned-child.pid")
	cmd := exec.Command("sh", "-c", "sleep 30 >/dev/null 2>&1 & echo $! > \"$1\"", "sh", pidFile)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	defer func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}()
	if err := runCommandPreservingExit(cmd); err != nil {
		t.Fatalf("owned command completion: %v", err)
	}
	payload, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(payload)))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid own fixture PID %q: %v", payload, err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("ordinary exec returned while its redirected descendant %d remained alive", pid)
}

func TestNativeExecDeadlineSettlesForwardedColdGroupAndPreservesSibling(t *testing.T) {
	runNativeExecColdGroupControl(t, false, false)
}

func TestNativeExecCancelSettlesForwardedColdGroupAndPreservesSibling(t *testing.T) {
	runNativeExecColdGroupControl(t, true, false)
}

func TestNativeExecDeadlineDoesNotProveEscapedResistantGroupReaped(t *testing.T) {
	runNativeExecColdGroupControl(t, false, true)
}

func runNativeExecColdGroupControl(t *testing.T, cancelOwner, resistantCold bool) {
	t.Helper()
	dir := t.TempDir()
	fixture := filepath.Join(dir, "governed-cold-child.sh")
	body := "#!/bin/bash\nset -m\ntrap 'kill -TERM -- -\"$child\" 2>/dev/null || true; wait \"$child\" 2>/dev/null || true; printf reaped > \"$2\"; exit 130' TERM HUP INT\n\"$3\" -test.run=^TestNativeExecLifecycleHelper$ > /dev/null 2>&1 &\nchild=$!\nset +m\nprintf '%s' \"$child\" > \"$1\"\nwait \"$child\"\n"
	if err := os.WriteFile(fixture, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	sibling := exec.Command("sleep", "30")
	sibling.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := sibling.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sibling.Process.Kill(); _ = sibling.Wait() }()
	owner := exec.Command(os.Args[0], "-test.run=^TestNativeExecLifecycleHelper$")
	mode := "deadline"
	if cancelOwner {
		mode = "cancel"
	}
	owner.Env = append(os.Environ(), "DEVKIT_EXEC_LIFECYCLE_HELPER="+mode, "DEVKIT_EXEC_LIFECYCLE_DIR="+dir)
	if resistantCold {
		owner.Env = append(owner.Env, "DEVKIT_EXEC_LIFECYCLE_COLD_RESISTS=1")
	}
	var output bytes.Buffer
	owner.Stdout, owner.Stderr = &output, &output
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Process.Kill() }()
	ready := filepath.Join(dir, "cold.pid")
	waitNativeExecFixtureFile(t, ready)
	pidBytes, err := os.ReadFile(ready)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		t.Fatal(err)
	}
	if resistantCold {
		// The fixture deliberately leaves the managed group. Its private group
		// belongs to this test, which must dispose it independently afterward.
		defer func() { _ = syscall.Kill(-pid, syscall.SIGKILL) }()
		waitNativeExecFixtureFile(t, filepath.Join(dir, "cold.ready"))
	}
	if cancelOwner {
		// This is the fresh controller returned by this test's own Start, never a
		// numerical PID reconstructed from another execution.
		if err := owner.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	go func() { done <- owner.Wait() }()
	select {
	case err := <-done:
		var exit *exec.ExitError
		want := 124
		if cancelOwner {
			want = 130
		}
		if !errors.As(err, &exit) || exit.ExitCode() != want {
			t.Fatalf("native owner exit = %v, want %d; output=%s", err, want, output.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("owned native lifecycle did not return after cleanup")
	}
	if resistantCold {
		if err := syscall.Kill(-pid, 0); err != nil {
			t.Fatalf("negative control escaped group did not survive: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "cold.reaped")); !os.IsNotExist(err) {
			t.Fatalf("resistant cold group incorrectly reported reaped: %v", err)
		}
		// Exit 124 proves the owned command's deadline, group and pipe cleanup;
		// it does not prove termination of a separate redirected process group.
	} else {
		if _, err := os.Stat(filepath.Join(dir, "cold.reaped")); err != nil {
			t.Fatalf("forwarding wrapper did not reap its cold group: %v; output=%s", err, output.String())
		}
		if err := syscall.Kill(-pid, 0); err != syscall.ESRCH {
			t.Fatalf("cold process group still exists after terminal result: %v", err)
		}
	}
	if err := sibling.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("independent persistent sibling was affected: %v", err)
	}
}

func waitNativeExecFixtureFile(t *testing.T, path string) {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("own fixture did not become ready: %s", path)
}

func TestNativeExecLifecycleHelper(t *testing.T) {
	mode := os.Getenv("DEVKIT_EXEC_LIFECYCLE_HELPER")
	if mode == "" {
		return
	}
	if mode == "cold" || mode == "cold-resistant" {
		// This process is the wrapper's new cold process-group leader.
		if mode == "cold-resistant" {
			signal.Ignore(syscall.SIGTERM)
			if err := os.WriteFile(filepath.Join(os.Getenv("DEVKIT_EXEC_LIFECYCLE_DIR"), "cold.ready"), []byte("ready"), 0600); err != nil {
				os.Exit(1)
			}
		}
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	dir := os.Getenv("DEVKIT_EXEC_LIFECYCLE_DIR")
	if mode == "popen" {
		// Match Python Popen(...).wait(): no forwarding trap in this waiting
		// process. The governed wrapper below owns and forwards its cold group.
		child := exec.Command("bash", filepath.Join(dir, "governed-cold-child.sh"), filepath.Join(dir, "cold.pid"), filepath.Join(dir, "cold.reaped"), os.Args[0])
		child.Env = append(os.Environ(), "DEVKIT_EXEC_LIFECYCLE_HELPER=cold")
		if os.Getenv("DEVKIT_EXEC_LIFECYCLE_COLD_RESISTS") == "1" {
			child.Env = append(os.Environ(), "DEVKIT_EXEC_LIFECYCLE_HELPER=cold-resistant")
		}
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Run(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestNativeExecLifecycleHelper$")
	cmd.Env = append(os.Environ(), "DEVKIT_EXEC_LIFECYCLE_HELPER=popen")
	duration := time.Duration(0)
	if mode == "deadline" {
		duration = time.Second
	}
	err := runNativeCommandWithTimeout(cmd, duration)
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) {
		os.Exit(exit.ExitCode())
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

type nativeExecFailedOutput struct{}

func (nativeExecFailedOutput) Write([]byte) (int, error) {
	return 0, errors.New("owned fixture output failed")
}

func TestNativeExecCleanupFailureIsNotPayloadSuccess(t *testing.T) {
	cmd := exec.Command("sh", "-c", "printf output")
	cmd.Stdout = nativeExecFailedOutput{}
	err := runCommandPreservingExit(cmd)
	var exit interface{ ExitCode() int }
	if !errors.As(err, &exit) || exit.ExitCode() != 125 {
		t.Fatalf("cleanup failure = %v, want distinct native exit 125", err)
	}
}

func TestNativeExecDeadlineAndPayloadExitRemainDistinct(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 7")
	err := runNativeCommandWithTimeout(cmd, time.Second)
	var exit interface{ ExitCode() int }
	if !errors.As(err, &exit) || exit.ExitCode() != 7 || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("payload error = %v, want original 7", err)
	}
}

func TestNativeExecTerminalInputKeepsExistingInteractiveLifecycle(t *testing.T) {
	terminal, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("PTY fixture unavailable: %v", err)
	}
	defer terminal.Close()
	cmd := exec.Command("sh", "-c", "exit 0")
	cmd.Stdin = terminal
	if !nativeCommandHasTerminalInput(cmd) {
		t.Fatal("PTY not classified as interactive input")
	}
	if err := runNativeCommandWithTimeout(cmd, time.Second); err == nil || cmd.Process != nil {
		t.Fatalf("interactive deadline should refuse before launch: %v", err)
	}
	if err := runCommandPreservingExit(cmd); err != nil {
		t.Fatal(err)
	}
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid {
		t.Fatal("interactive command moved out of its controlling terminal group")
	}
}

func TestNativeExecTimeoutParsersAndManagedRefusal(t *testing.T) {
	for _, value := range []string{"0", "-1s", "", "invalid"} {
		if _, err := parseTopExecArgs(&cmdregistry.Context{Args: []string{"1", "--timeout", value, "--", "true"}}, false); err == nil {
			t.Fatalf("accepted invalid timeout %q", value)
		}
	}
	if _, err := parseTopExecArgs(&cmdregistry.Context{Args: []string{"1", "--timeout", "1s", "--timeout", "2s", "--", "true"}}, false); err == nil {
		t.Fatal("accepted duplicate timeout")
	}
	if _, err := parseTopExecArgs(&cmdregistry.Context{Args: []string{"1", "--timeout", "1s"}}, true); err == nil {
		t.Fatal("attach accepted timeout")
	}
	parsed, err := parseTopExecArgs(&cmdregistry.Context{Args: []string{"1", "--timeout", "1s", "--", "true"}}, false)
	if err != nil || parsed.executionTimeout != time.Second {
		t.Fatalf("top parser: %+v, %v", parsed, err)
	}
	native, err := parsePlanArgs(&cmdregistry.Context{Args: []string{"exec", "--repo", "ouroboros-ide", "--timeout", "2s", "--", "true"}}, true, false)
	if err != nil || native.executionTimeout != 2*time.Second {
		t.Fatalf("native parser: %+v, %v", native, err)
	}

}

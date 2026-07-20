package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type Result struct {
	Code int
	Err  error
}

var ErrIdleTimeout = errors.New("command made no observable progress before its idle deadline")

type ManagedPolicy struct {
	// IdleTimeout bounds a command only while it produces no stdout or stderr.
	// A zero value disables the idle deadline; the caller's context remains
	// authoritative for phases with a fixed wall-clock bound.
	IdleTimeout      time.Duration
	TerminationGrace time.Duration
}

type activityWriter struct {
	writer   io.Writer
	activity chan<- struct{}
}

func (w activityWriter) Write(payload []byte) (int, error) {
	written, err := w.writer.Write(payload)
	if written > 0 {
		select {
		case w.activity <- struct{}{}:
		default:
		}
	}
	return written, err
}

// RunManaged runs a command in its own process group. Cancellation and idle
// expiry terminate the complete descendant group, wait for it to unwind, and
// then force-kill any orphan that outlives the declared grace period.
func RunManaged(ctx context.Context, policy ManagedPolicy, name string, args ...string) Result {
	return RunManagedWithWriters(ctx, policy, os.Stdout, os.Stderr, name, args...)
}

// RunManagedWithWriters preserves the managed process-group lifecycle while
// allowing structured callers to keep subprocess output off their stdout
// protocol. Nil writers select io.Discard.
func RunManagedWithWriters(
	ctx context.Context,
	policy ManagedPolicy,
	stdout io.Writer,
	stderr io.Writer,
	name string,
	args ...string,
) Result {
	if os.Getenv("DEVKIT_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "+ %s\n", strings.Join(append([]string{name}, args...), " "))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	grace := policy.TerminationGrace
	if grace <= 0 {
		grace = 2 * time.Second
	}

	activity := make(chan struct{}, 1)
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = os.Stdin
	cmd.Stdout = activityWriter{writer: stdout, activity: activity}
	cmd.Stderr = activityWriter{writer: stderr, activity: activity}
	if err := cmd.Start(); err != nil {
		return Result{Code: 1, Err: err}
	}

	wait := make(chan error, 1)
	go func() {
		wait <- cmd.Wait()
	}()

	var idleTimer *time.Timer
	var idle <-chan time.Time
	if policy.IdleTimeout > 0 {
		idleTimer = time.NewTimer(policy.IdleTimeout)
		idle = idleTimer.C
		defer idleTimer.Stop()
	}
	resetIdle := func() {
		if idleTimer == nil {
			return
		}
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleTimer.Reset(policy.IdleTimeout)
	}

	for {
		select {
		case err := <-wait:
			return managedResult(err, nil)
		case <-activity:
			resetIdle()
		case <-ctx.Done():
			select {
			case err := <-wait:
				return managedResult(err, nil)
			default:
			}
			err := terminateManagedProcessGroup(cmd.Process.Pid, grace, wait)
			return managedResult(err, ctx.Err())
		case <-idle:
			select {
			case err := <-wait:
				return managedResult(err, nil)
			default:
			}
			err := terminateManagedProcessGroup(cmd.Process.Pid, grace, wait)
			return managedResult(err, ErrIdleTimeout)
		}
	}
}

func managedResult(waitErr, cause error) Result {
	if cause != nil {
		code := 1
		if errors.Is(cause, context.DeadlineExceeded) || errors.Is(cause, ErrIdleTimeout) {
			code = 124
		} else if errors.Is(cause, context.Canceled) {
			code = 130
		}
		if waitErr != nil {
			return Result{Code: code, Err: fmt.Errorf("%w (process wait: %v)", cause, waitErr)}
		}
		return Result{Code: code, Err: cause}
	}
	if waitErr == nil {
		return Result{}
	}
	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		return Result{Code: exitErr.ExitCode(), Err: waitErr}
	}
	return Result{Code: 1, Err: waitErr}
}

func terminateManagedProcessGroup(pgid int, grace time.Duration, wait <-chan error) error {
	termErr := syscall.Kill(-pgid, syscall.SIGTERM)
	if termErr != nil && !errors.Is(termErr, syscall.ESRCH) {
		_ = syscall.Kill(pgid, syscall.SIGTERM)
	}

	timer := time.NewTimer(grace)
	defer timer.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var (
		waitErr error
		waited  bool
	)
	recordWait := func(err error) {
		waitErr = err
		waited = true
	}
	for {
		select {
		case err := <-wait:
			recordWait(err)
			if !managedProcessGroupExists(pgid) {
				return waitErr
			}
		case <-ticker.C:
			if waited && !managedProcessGroupExists(pgid) {
				return waitErr
			}
		case <-timer.C:
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			if !waited {
				recordWait(<-wait)
			}
			return waitErr
		}
	}
}

func managedProcessGroupExists(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	return err == nil || !errors.Is(err, syscall.ESRCH)
}

func Run(name string, args ...string) Result {
	return RunCtx(context.Background(), name, args...)
}

func RunCtx(ctx context.Context, name string, args ...string) Result {
	if os.Getenv("DEVKIT_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "+ %s\n", strings.Join(append([]string{name}, args...), " "))
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			code = 124
		} else {
			code = 1
		}
	}
	return Result{Code: code, Err: err}
}

// RunCtxWithOutput mirrors RunCtx but captures combined stdout/stderr while still streaming to the host.
func RunCtxWithOutput(ctx context.Context, name string, args ...string) (Result, string) {
	if os.Getenv("DEVKIT_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "+ %s\n", strings.Join(append([]string{name}, args...), " "))
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	var buf bytes.Buffer
	stdout := io.MultiWriter(os.Stdout, &buf)
	stderr := io.MultiWriter(os.Stderr, &buf)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			code = 124
		} else {
			code = 1
		}
	}
	return Result{Code: code, Err: err}, buf.String()
}

func WithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// RunWithInput runs a command with provided stdin content.
func RunWithInput(ctx context.Context, input []byte, name string, args ...string) Result {
	if os.Getenv("DEVKIT_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "+ %s\n", strings.Join(append([]string{name}, args...), " "))
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = io.NopCloser(strings.NewReader(string(input)))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			code = 124
		} else {
			code = 1
		}
	}
	return Result{Code: code, Err: err}
}

// Capture runs a command and returns stdout as string and exit code.
func Capture(ctx context.Context, name string, args ...string) (string, Result) {
	if os.Getenv("DEVKIT_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "+ %s\n", strings.Join(append([]string{name}, args...), " "))
	}
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			code = 124
		} else {
			code = 1
		}
	}
	return string(out), Result{Code: code, Err: err}
}

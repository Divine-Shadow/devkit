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
	"sync"
	"syscall"
	"time"
)

type Result struct {
	Code int
	Err  error
}

var (
	ErrIdleTimeout                        = errors.New("command made no observable progress before its idle deadline")
	ErrOutputLimit                        = errors.New("command stdout exceeded its capture limit")
	errManagedCaptureProcessGroupSurvived = errors.New("managed capture process group survived forced termination")
)

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

type boundedCaptureWriter struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	limit    int
	overflow bool
	activity chan<- struct{}
	exceeded chan<- struct{}
}

func (w *boundedCaptureWriter) Write(payload []byte) (int, error) {
	written := len(payload)
	w.mu.Lock()
	remaining := w.limit - w.buffer.Len()
	if remaining > len(payload) {
		remaining = len(payload)
	}
	if remaining > 0 {
		_, _ = w.buffer.Write(payload[:remaining])
	}
	if remaining < len(payload) && !w.overflow {
		w.overflow = true
		select {
		case w.exceeded <- struct{}{}:
		default:
		}
	}
	w.mu.Unlock()
	if written > 0 {
		select {
		case w.activity <- struct{}{}:
		default:
		}
	}
	// Pretend the entire write was consumed so the process stays observable
	// until the managed loop terminates its complete process group.
	return written, nil
}

func (w *boundedCaptureWriter) snapshot() (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String(), w.overflow
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
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = os.Stdin
	cmd.Stdout = activityWriter{writer: os.Stdout, activity: activity}
	cmd.Stderr = activityWriter{writer: os.Stderr, activity: activity}
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

// CaptureManaged captures a bounded amount of stdout while retaining
// RunManaged's process-group cancellation and idle-timeout guarantees. Output
// beyond maxOutputBytes terminates the complete descendant group and reports
// ErrOutputLimit; stderr is observed for liveness but is not retained.
func CaptureManaged(
	ctx context.Context,
	policy ManagedPolicy,
	maxOutputBytes int,
	name string,
	args ...string,
) (string, Result) {
	if maxOutputBytes < 1 {
		return "", Result{Code: 1, Err: fmt.Errorf("capture output limit must be positive")}
	}
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
	exceeded := make(chan struct{}, 1)
	stdout := &boundedCaptureWriter{
		limit:    maxOutputBytes,
		activity: activity,
		exceeded: exceeded,
	}
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = stdout
	cmd.Stderr = activityWriter{writer: io.Discard, activity: activity}
	if err := cmd.Start(); err != nil {
		return "", Result{Code: 1, Err: err}
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
	finish := func(waitErr error, cause error) (string, Result) {
		output, overflow := stdout.snapshot()
		return output, managedCaptureResult(waitErr, cause, overflow)
	}

	for {
		select {
		case err := <-wait:
			err = terminateManagedCaptureProcessGroup(cmd.Process.Pid, grace, wait, err, true)
			return finish(err, nil)
		case <-activity:
			resetIdle()
		case <-exceeded:
			err := terminateManagedCaptureProcessGroup(cmd.Process.Pid, grace, wait, nil, false)
			return finish(err, ErrOutputLimit)
		case <-ctx.Done():
			err := terminateManagedCaptureProcessGroup(cmd.Process.Pid, grace, wait, nil, false)
			return finish(err, ctx.Err())
		case <-idle:
			err := terminateManagedCaptureProcessGroup(cmd.Process.Pid, grace, wait, nil, false)
			return finish(err, ErrIdleTimeout)
		}
	}
}

func terminateManagedCaptureProcessGroup(
	pgid int,
	grace time.Duration,
	wait <-chan error,
	waitErr error,
	waited bool,
) error {
	if grace <= 0 {
		grace = 2 * time.Second
	}
	if !managedProcessGroupExists(pgid) {
		if !waited {
			waitErr = <-wait
		}
		return waitErr
	}
	termErr := syscall.Kill(-pgid, syscall.SIGTERM)
	if termErr != nil && !errors.Is(termErr, syscall.ESRCH) {
		return errors.Join(waitErr, fmt.Errorf("terminate managed capture process group %d: %w", pgid, termErr))
	}

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	termTimer := time.NewTimer(grace)
	defer termTimer.Stop()
	for {
		if waited && !managedProcessGroupExists(pgid) {
			return waitErr
		}
		select {
		case err := <-wait:
			waitErr = err
			waited = true
		case <-ticker.C:
		case <-termTimer.C:
			goto force
		}
	}

force:
	if killErr := syscall.Kill(-pgid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
		return errors.Join(waitErr, fmt.Errorf("kill managed capture process group %d: %w", pgid, killErr))
	}
	proofGrace := grace
	if proofGrace < time.Second {
		proofGrace = time.Second
	}
	proofTimer := time.NewTimer(proofGrace)
	defer proofTimer.Stop()
	for {
		if waited && !managedProcessGroupExists(pgid) {
			return waitErr
		}
		select {
		case err := <-wait:
			waitErr = err
			waited = true
		case <-ticker.C:
		case <-proofTimer.C:
			if !managedProcessGroupExists(pgid) {
				if !waited {
					waitErr = <-wait
				}
				return waitErr
			}
			return errors.Join(
				waitErr,
				fmt.Errorf("%w: %d", errManagedCaptureProcessGroupSurvived, pgid),
			)
		}
	}
}

func managedCaptureResult(waitErr, selectedCause error, overflow bool) Result {
	// Once the managed loop selects a lifecycle cause, later observation of an
	// output overflow must not relabel that deadline, cancellation, or idle
	// expiry. A normal process completion still reports a recorded overflow.
	if selectedCause == nil && overflow {
		selectedCause = ErrOutputLimit
	}
	return managedResult(waitErr, selectedCause)
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

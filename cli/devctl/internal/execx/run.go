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
	ErrIdleTimeout = errors.New("command made no observable progress before its idle deadline")
	ErrOutputLimit = errors.New("command stdout exceeded its capture limit")
	// ErrManagedCleanup marks failures to reap a managed command or prove that
	// its process group and I/O-copy goroutines have terminated.
	ErrManagedCleanup              = errors.New("managed command cleanup failed")
	errManagedProcessGroupSurvived = errors.New("managed process group survived forced termination")
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

type managedCommandIO struct {
	readers []*os.File
	done    chan struct{}
	mu      sync.Mutex
	err     error
}

type boundedCaptureWriter struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	limit    int
	overflow bool
	activity chan<- struct{}
	exceeded chan<- struct{}
}

func joinManagedErrors(left, right error) error {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	return errors.Join(left, right)
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

func startManagedCommand(cmd *exec.Cmd, stdout, stderr io.Writer) (*managedCommandIO, error) {
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create managed command stdout pipe: %w", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		return nil, fmt.Errorf("create managed command stderr pipe: %w", err)
	}
	closePipes := func() {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
	}

	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	if err := cmd.Start(); err != nil {
		closePipes()
		return nil, err
	}
	// The child owns duplicated write descriptors after Start. Closing the
	// parent's copies lets EOF prove that every inheriting descendant closed
	// its corresponding descriptor.
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()

	managedIO := &managedCommandIO{
		readers: []*os.File{stdoutReader, stderrReader},
		done:    make(chan struct{}),
	}
	var copies sync.WaitGroup
	copies.Add(2)
	copyOutput := func(label string, destination io.Writer, source *os.File) {
		defer copies.Done()
		defer source.Close()
		if _, err := io.Copy(destination, source); err != nil {
			managedIO.mu.Lock()
			managedIO.err = errors.Join(managedIO.err, fmt.Errorf("copy managed command %s: %w", label, err))
			managedIO.mu.Unlock()
		}
	}
	go copyOutput("stdout", stdout, stdoutReader)
	go copyOutput("stderr", stderr, stderrReader)
	go func() {
		copies.Wait()
		close(managedIO.done)
	}()
	return managedIO, nil
}

func (managedIO *managedCommandIO) finish(grace time.Duration) error {
	if grace <= 0 {
		grace = 2 * time.Second
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-managedIO.done:
		return managedIO.copyError()
	case <-timer.C:
	}

	for _, reader := range managedIO.readers {
		_ = reader.Close()
	}
	cleanupErr := fmt.Errorf("%w: command I/O remained open after process exit for %s", ErrManagedCleanup, grace)
	cleanupTimer := time.NewTimer(grace)
	defer cleanupTimer.Stop()
	select {
	case <-managedIO.done:
		return errors.Join(cleanupErr, managedIO.copyError())
	case <-cleanupTimer.C:
		return errors.Join(
			cleanupErr,
			fmt.Errorf("%w: command I/O copy workers did not stop within %s", ErrManagedCleanup, grace),
		)
	}
}

func (managedIO *managedCommandIO) copyError() error {
	managedIO.mu.Lock()
	defer managedIO.mu.Unlock()
	if managedIO.err == nil {
		return nil
	}
	return errors.Join(ErrManagedCleanup, managedIO.err)
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
	// Descendants can escape the managed process group with setsid while still
	// holding I/O descriptors. Bound any os/exec wait work in addition to the
	// explicitly owned pipe drains below.
	cmd.WaitDelay = grace
	managedIO, err := startManagedCommand(
		cmd,
		activityWriter{writer: os.Stdout, activity: activity},
		activityWriter{writer: os.Stderr, activity: activity},
	)
	if err != nil {
		return Result{Code: 1, Err: err}
	}

	wait := make(chan error, 1)
	go func() {
		wait <- classifyManagedWaitError(cmd.Wait())
	}()
	finish := func(waitErr, cause error) Result {
		return managedResult(joinManagedErrors(waitErr, managedIO.finish(grace)), cause)
	}
	finishAfterWait := func(waitErr, cause error) Result {
		waitErr = terminateManagedProcessGroupAfterLeader(cmd.Process.Pid, grace, wait, waitErr, true)
		return finish(waitErr, cause)
	}

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
			return finishAfterWait(err, nil)
		case <-activity:
			resetIdle()
		case <-ctx.Done():
			select {
			case err := <-wait:
				return finishAfterWait(err, nil)
			default:
			}
			err := terminateManagedProcessGroupAfterLeader(cmd.Process.Pid, grace, wait, nil, false)
			return finish(err, ctx.Err())
		case <-idle:
			select {
			case err := <-wait:
				return finishAfterWait(err, nil)
			default:
			}
			err := terminateManagedProcessGroupAfterLeader(cmd.Process.Pid, grace, wait, nil, false)
			return finish(err, ErrIdleTimeout)
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
	cmd.WaitDelay = grace
	managedIO, err := startManagedCommand(
		cmd,
		stdout,
		activityWriter{writer: io.Discard, activity: activity},
	)
	if err != nil {
		return "", Result{Code: 1, Err: err}
	}

	wait := make(chan error, 1)
	go func() {
		wait <- classifyManagedWaitError(cmd.Wait())
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
		waitErr = joinManagedErrors(waitErr, managedIO.finish(grace))
		output, overflow := stdout.snapshot()
		return output, managedCaptureResult(waitErr, cause, overflow)
	}

	for {
		select {
		case err := <-wait:
			err = terminateManagedProcessGroupAfterLeader(cmd.Process.Pid, grace, wait, err, true)
			return finish(err, nil)
		case <-activity:
			resetIdle()
		case <-exceeded:
			err := terminateManagedProcessGroupAfterLeader(cmd.Process.Pid, grace, wait, nil, false)
			return finish(err, ErrOutputLimit)
		case <-ctx.Done():
			err := terminateManagedProcessGroupAfterLeader(cmd.Process.Pid, grace, wait, nil, false)
			return finish(err, ctx.Err())
		case <-idle:
			err := terminateManagedProcessGroupAfterLeader(cmd.Process.Pid, grace, wait, nil, false)
			return finish(err, ErrIdleTimeout)
		}
	}
}

func terminateManagedProcessGroupAfterLeader(
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
			waitErr = waitForManagedCommand(wait, grace)
		}
		return waitErr
	}
	termErr := syscall.Kill(-pgid, syscall.SIGTERM)
	if termErr != nil && !errors.Is(termErr, syscall.ESRCH) {
		return errors.Join(
			waitErr,
			ErrManagedCleanup,
			fmt.Errorf("terminate managed process group %d: %w", pgid, termErr),
		)
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
		return errors.Join(
			waitErr,
			ErrManagedCleanup,
			fmt.Errorf("kill managed process group %d: %w", pgid, killErr),
		)
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
					waitErr = waitForManagedCommand(wait, grace)
				}
				return waitErr
			}
			return errors.Join(
				waitErr,
				ErrManagedCleanup,
				fmt.Errorf("%w: %d", errManagedProcessGroupSurvived, pgid),
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
			return Result{Code: code, Err: errors.Join(cause, fmt.Errorf("process wait: %w", waitErr))}
		}
		return Result{Code: code, Err: cause}
	}
	if waitErr == nil {
		return Result{}
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return Result{Code: exitErr.ExitCode(), Err: waitErr}
	}
	return Result{Code: 1, Err: waitErr}
}

func classifyManagedWaitError(err error) error {
	if !errors.Is(err, exec.ErrWaitDelay) {
		return err
	}
	return errors.Join(
		ErrManagedCleanup,
		fmt.Errorf("managed command I/O pipes remained open after process exit: %w", err),
	)
}

func waitForManagedCommand(wait <-chan error, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-wait:
		return err
	case <-timer.C:
		return fmt.Errorf("%w: command wait exceeded %s", ErrManagedCleanup, timeout)
	}
}

// IsManagedCleanupError reports whether err includes a managed process
// lifecycle infrastructure or cleanup failure.
func IsManagedCleanupError(err error) bool {
	return errors.Is(err, ErrManagedCleanup)
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

package productsourceacquire

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type commandResult struct {
	stdout string
}

type transportStop struct {
	processStopped bool
	socketStopped  bool
}

type transportHandle interface {
	Stop() transportStop
}

type operationBoundary interface {
	StartTransport(context.Context, Manifest) (transportHandle, error)
	RunGit(context.Context, Manifest, ...string) (commandResult, error)
}

type productionBoundary struct{}

func (productionBoundary) StartTransport(ctx context.Context, manifest Manifest) (transportHandle, error) {
	command := exec.Command(
		manifest.Transport.ExecutablePath,
		"serve",
		"--socket", manifest.Transport.SocketPath,
		"--allowlist", manifest.Transport.AllowlistPath,
	)
	command.Env = []string{"PATH=/nonexistent"}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stderr boundedBuffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	handle := &productionTransport{
		command:    command,
		done:       make(chan error, 1),
		socketPath: manifest.Transport.SocketPath,
	}
	go func() { handle.done <- command.Wait() }()

	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-handle.done:
			handle.waited = true
			handle.waitErr = err
			return handle, fmt.Errorf("source transport exited before its socket was ready")
		case <-ctx.Done():
			return handle, ctx.Err()
		case <-deadline.C:
			return handle, fmt.Errorf("source transport socket readiness timed out")
		case <-ticker.C:
			info, err := os.Lstat(manifest.Transport.SocketPath)
			if err == nil && info.Mode()&os.ModeSocket != 0 && info.Mode().Perm() == 0o600 {
				return handle, nil
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return handle, fmt.Errorf("inspect source transport socket: %w", err)
			}
		}
	}
}

func (productionBoundary) RunGit(ctx context.Context, manifest Manifest, args ...string) (commandResult, error) {
	command := exec.Command(manifest.Runtime.GitExecutablePath, args...)
	command.Dir = manifest.Product.LifecycleRoot
	command.Env = []string{
		"HOME=" + manifest.Product.LifecycleRoot,
		"LANG=C",
		"LC_ALL=C",
		"PATH=" + manifest.Runtime.Path,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH=" + manifest.Transport.GitSSHExecutablePath,
		"GIT_SSH_VARIANT=ssh",
		"DEVKIT_SOURCE_TRANSPORT_IDENTITY=" + manifest.Product.IdentityPath,
		"DEVKIT_SOURCE_TRANSPORT_SOCKET=" + manifest.Transport.SocketPath,
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr boundedBuffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return commandResult{}, err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return commandResult{}, err
		}
		return commandResult{stdout: strings.TrimSpace(stdout.String())}, nil
	case <-ctx.Done():
		stopProcessGroup(command.Process.Pid, syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			stopProcessGroup(command.Process.Pid, syscall.SIGKILL)
			<-done
		}
		return commandResult{}, ctx.Err()
	}
}

type productionTransport struct {
	command    *exec.Cmd
	done       chan error
	socketPath string
	waited     bool
	waitErr    error
	once       sync.Once
	result     transportStop
}

func (handle *productionTransport) Stop() transportStop {
	handle.once.Do(func() {
		if !handle.waited {
			stopProcessGroup(handle.command.Process.Pid, syscall.SIGTERM)
			select {
			case handle.waitErr = <-handle.done:
				handle.waited = true
			case <-time.After(2 * time.Second):
				stopProcessGroup(handle.command.Process.Pid, syscall.SIGKILL)
				handle.waitErr = <-handle.done
				handle.waited = true
			}
		}
		handle.result.processStopped = handle.waited && !processGroupAlive(handle.command.Process.Pid)
		_, err := os.Lstat(handle.socketPath)
		handle.result.socketStopped = errors.Is(err, os.ErrNotExist)
	})
	return handle.result
}

func stopProcessGroup(pid int, signal syscall.Signal) {
	if pid > 0 {
		_ = syscall.Kill(-pid, signal)
	}
}

func processGroupAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(-pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

type boundedBuffer struct {
	bytes.Buffer
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	const limit = 32 * 1024
	original := len(data)
	if buffer.Len() < limit {
		remaining := limit - buffer.Len()
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = buffer.Buffer.Write(data)
	}
	return original, nil
}

func localGitFlakeURL(checkoutPath, revision string) string {
	return "git+file://" + checkoutPath + "?rev=" + url.QueryEscape(revision)
}

func createExclusive(path string, contents []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	writeErr := error(nil)
	if _, err := file.Write(contents); err != nil {
		writeErr = err
	} else if err := file.Sync(); err != nil {
		writeErr = err
	}
	closeErr := file.Close()
	return errors.Join(writeErr, closeErr)
}

func ensureRootMode(path string) error {
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("lifecycle root did not retain owner-only directory mode")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("lifecycle root is not owned by the effective user")
	}
	return nil
}

func checkoutRelativeToRoot(root, checkout string) bool {
	relative, err := filepath.Rel(root, checkout)
	return err == nil && relative == "product"
}

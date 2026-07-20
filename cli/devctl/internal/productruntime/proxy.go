package productruntime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"devkit/cli/devctl/internal/runtime/egressproxy"
)

func startProxy(socketPath, allowlistPath, upstreamProxyURL string) (func() error, error) {
	if _, err := os.Lstat(socketPath); err == nil {
		return nil, fmt.Errorf("Product proxy refuses preexisting path %s", socketPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- egressproxy.Serve(ctx, egressproxy.Config{
			SocketPath:       socketPath,
			AllowlistPath:    allowlistPath,
			UpstreamProxyURL: upstreamProxyURL,
		})
	}()
	if err := waitForUnixSocket(socketPath, errCh, 5*time.Second); err != nil {
		cancel()
		return nil, err
	}
	ownedInfo, err := os.Lstat(socketPath)
	if err != nil {
		cancel()
		return nil, err
	}
	var once sync.Once
	var cleanupErr error
	return func() error {
		once.Do(func() {
			cancel()
			select {
			case serveErr := <-errCh:
				if serveErr != nil {
					cleanupErr = errors.Join(cleanupErr, serveErr)
				}
			case <-time.After(2 * time.Second):
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("Product proxy did not stop"))
			}
			if unixSocketAccepts(socketPath) {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("Product proxy listener survived cleanup"))
				return
			}
			info, statErr := os.Lstat(socketPath)
			if errors.Is(statErr, os.ErrNotExist) {
				return
			}
			if statErr != nil {
				cleanupErr = errors.Join(cleanupErr, statErr)
				return
			}
			if !os.SameFile(ownedInfo, info) {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("Product proxy pathname identity changed"))
				return
			}
			if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		})
		return cleanupErr
	}, nil
}

func waitForUnixSocket(path string, errCh <-chan error, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for {
		select {
		case err := <-errCh:
			if err == nil {
				return fmt.Errorf("Product proxy exited before readiness")
			}
			return err
		default:
		}
		if unixSocketAccepts(path) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Product proxy socket was not ready within %s", limit)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func unixSocketAccepts(path string) bool {
	connection, err := net.DialTimeout("unix", path, 50*time.Millisecond)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

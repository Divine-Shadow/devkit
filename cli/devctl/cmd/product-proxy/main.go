package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"

	"devkit/cli/devctl/internal/productadapter"
	"devkit/cli/devctl/internal/runtime/egressproxy"
)

func main() {
	if len(os.Args) < 2 {
		fail("expected stdio or supervise")
	}
	privilegedEntry := os.Getuid() != os.Geteuid()
	var attestation productadapter.Attestation
	if privilegedEntry {
		var err error
		attestation, err = productadapter.AttestInitialNamespaces(productadapter.RoleProxy)
		if err != nil {
			fail("%v", err)
		}
		if os.Args[1] != "stdio" {
			fail("privileged Product proxy entry accepts only stdio")
		}
	}
	switch os.Args[1] {
	case "stdio":
		if !privilegedEntry {
			fail("Product proxy stdio requires its package-owned privileged entry wrapper")
		}
		runStdio(os.Args[2:], attestation)
	case "supervise":
		if len(os.Args) != 2 {
			fail("supervise accepts no command-line authority")
		}
		if os.Getuid() == 0 || os.Geteuid() == 0 {
			fail("Product proxy supervise refuses numeric root identity")
		}
		runSupervisor()
	default:
		fail("unknown mode %q", os.Args[1])
	}
}

func runStdio(args []string, attestation productadapter.Attestation) {
	if len(args) != 4 || args[0] != "--count" || args[2] != "--index" {
		fail("usage: product-proxy stdio --count N --index N")
	}
	count, index, err := productadapter.ParseCanonicalIdentity(args[1], args[3])
	if err != nil {
		fail("%v", err)
	}
	authority, err := productadapter.Load(productadapter.RoleProxy, index, attestation)
	if err != nil {
		fail("%v", err)
	}
	defer authority.Close()
	receipt, err := productadapter.ReadReceipt(authority, count, index)
	if err != nil {
		fail("%v", err)
	}
	if receipt.Status != "constructing" && receipt.Status != "executing" {
		fail("Product proxy refuses receipt status %q", receipt.Status)
	}
	if err := productadapter.ValidateProxyIdentity(receipt.Proxy); err != nil {
		fail("%v", err)
	}
	if err := egressproxy.Connect(
		context.Background(),
		receipt.Proxy.Path,
		"ssh.github.com:443",
		os.Stdin,
		os.Stdout,
	); err != nil {
		fail("%v", err)
	}
}

func runSupervisor() {
	capability, err := consumeSupervisorCapability(productadapter.SupervisorCapabilityPath)
	if err != nil {
		fail("%v", err)
	}
	if err := productadapter.ValidateSupervisorCapability(capability); err != nil {
		fail("%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridgeDone := make(chan error, 1)
	go func() {
		bridgeDone <- egressproxy.Bridge(ctx, "127.0.0.1:18888", capability.ProxySocketPath)
	}()
	if err := waitForBridge(bridgeDone); err != nil {
		fail("%v", err)
	}
	child := exec.Command(capability.RuntimeArgs[0], capability.RuntimeArgs[1:]...)
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	child.Env = os.Environ()
	err = child.Run()
	cancel()
	bridgeErr := <-bridgeDone
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fail("runtime child: %v", err)
	}
	if bridgeErr != nil {
		fail("runtime proxy bridge: %v", bridgeErr)
	}
}

func consumeSupervisorCapability(path string) (productadapter.SupervisorCapability, error) {
	var capability productadapter.SupervisorCapability
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return capability, fmt.Errorf("supervisor requires the one-shot adapter capability: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return capability, fmt.Errorf("open one-shot adapter capability")
	}
	defer file.Close()
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return capability, fmt.Errorf("inspect one-shot adapter capability: %w", err)
	}
	if stat.Mode&syscall.S_IFMT != syscall.S_IFREG ||
		stat.Mode&0o777 != 0o600 ||
		int(stat.Uid) != os.Geteuid() {
		return capability, fmt.Errorf("one-shot adapter capability has unsafe identity")
	}
	if err := os.Remove(path); err != nil {
		return capability, fmt.Errorf("consume one-shot adapter capability: %w", err)
	}
	payload, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
	if err != nil {
		return capability, fmt.Errorf("read adapter capability: %w", err)
	}
	if len(payload) > 64*1024 {
		return capability, fmt.Errorf("adapter capability exceeds the bounded size")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&capability); err != nil {
		return capability, fmt.Errorf("decode adapter capability: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return capability, fmt.Errorf("adapter capability contains trailing data")
	}
	return capability, nil
}

func waitForBridge(done <-chan error) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case err := <-done:
			if err == nil {
				return fmt.Errorf("Product proxy bridge exited before readiness")
			}
			return err
		default:
		}
		connection, err := net.DialTimeout("tcp", "127.0.0.1:18888", 50*time.Millisecond)
		if err == nil {
			connection.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Product proxy bridge was not ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func fail(format string, values ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "product-proxy: "+format+"\n", values...)
	os.Exit(2)
}

package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseLoginShellInvocationAcceptsOnlyExactSSHDShape(t *testing.T) {
	command := "/nix/store/session/bin/product-ssh-session force-command --count 2 --index 1"
	count, index, observed, err := parseLoginShellInvocation([]string{"-c", command})
	if err != nil || count != 2 || index != 1 || observed != command {
		t.Fatalf("exact sshd shell invocation rejected: %d/%d %q %v", count, index, observed, err)
	}
	invalid := [][]string{
		nil,
		{"force-command", "--count", "2", "--index", "1"},
		{"-c", command, "extra"},
		{"-c", command + "; id"},
		{"-c", "/nix/store/session/bin/product-ssh-session force-command --count 02 --index 1"},
		{"-c", "/nix/store/session/bin/product-ssh-session force-command --index 1 --count 2"},
	}
	for _, args := range invalid {
		if _, _, _, err := parseLoginShellInvocation(args); err == nil {
			t.Fatalf("accepted invalid account-shell argv %#v", args)
		}
	}
}

func TestRelayReturnsWhenSupervisorClosesWhileSSHInputRemainsOpen(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "supervisor.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan *net.UnixConn, 1)
	go func() {
		connection, acceptErr := listener.AcceptUnix()
		if acceptErr == nil {
			accepted <- connection
		}
	}()
	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	server := <-accepted

	inputReader, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inputReader.Close()
	defer inputWriter.Close()
	var output bytes.Buffer
	result := make(chan error, 1)
	go func() {
		result <- relay(client, inputReader, &output)
	}()

	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("relay returned an error after supervisor teardown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("relay remained blocked on open SSH stdin after supervisor teardown")
	}
	if _, err := inputWriter.Write([]byte("still-open-client")); err == nil {
		t.Fatalf("relay did not close its input after supervisor teardown: %v", err)
	}
}

package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseProcessStatHandlesParenthesesInCommand(t *testing.T) {
	fields := make([]string, 20)
	for index := range fields {
		fields[index] = "0"
	}
	fields[0] = "S"
	fields[1] = "41"
	fields[19] = "987654"
	payload := []byte("42 (product (readiness)) " + strings.Join(fields, " "))
	stat, err := parseProcessStat(payload)
	if err != nil {
		t.Fatal(err)
	}
	if stat.ParentPID != 41 || stat.StartTime != 987654 {
		t.Fatalf("unexpected process identity: %+v", stat)
	}
}

func TestParseProcessStatRejectsMalformedIdentity(t *testing.T) {
	for _, payload := range [][]byte{
		[]byte("42 no-parentheses"),
		[]byte("42 (short) S 1"),
		[]byte("42 (bad-parent) S nope 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 99"),
		[]byte("42 (bad-start) S 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 nope"),
	} {
		if _, err := parseProcessStat(payload); err == nil {
			t.Fatalf("malformed stat was accepted: %q", payload)
		}
	}
}

func TestExactManagedAppServerPreservesApprovedLeadingConfiguration(t *testing.T) {
	socket := "/agent-state/product/app-server.sock"
	valid := []string{
		"-c", "features.code_mode_host=true",
		"app-server", "--listen", "unix://" + socket,
	}
	if !isExactManagedAppServer(valid, socket) {
		t.Fatal("approved Desktop leading configuration was not preserved")
	}
	invalid := [][]string{
		{"app-server", "--listen", "unix://" + socket},
		{"-c", "features.code_mode_host=false", "app-server", "--listen", "unix://" + socket},
		{"-c", "features.code_mode_host=true", "app-server", "--listen", "unix://"},
		{"-c", "features.code_mode_host=true", "app-server", "proxy"},
		{"-c", "features.code_mode_host=true", "app-server", "--listen", "unix://" + socket, "extra"},
	}
	for _, arguments := range invalid {
		if !containsAppServerSubcommand(arguments) {
			t.Fatalf("app-server lookalike was not classified for rejection: %#v", arguments)
		}
		if isExactManagedAppServer(arguments, socket) {
			t.Fatalf("app-server lookalike matched exact target: %#v", arguments)
		}
	}
}

func TestExactReadinessAppServerIsOnlyTheOwnedTransientShape(t *testing.T) {
	if !isExactReadinessAppServer([]string{"app-server", "--stdio"}) {
		t.Fatal("package-owned readiness app-server shape was rejected")
	}
	for _, arguments := range [][]string{
		{"app-server"},
		{"app-server", "--listen", "stdio://"},
		{"-c", "features.code_mode_host=true", "app-server", "--stdio"},
		{"app-server", "--stdio", "extra"},
	} {
		if isExactReadinessAppServer(arguments) {
			t.Fatalf("noncanonical readiness app-server shape was accepted: %#v", arguments)
		}
	}
}

func TestProcessInventoryRejectsSameUIDLookalikeAcrossCgroupBoundary(t *testing.T) {
	procRoot := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	processRoot := filepath.Join(procRoot, "4242")
	if err := os.Mkdir(processRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executable, filepath.Join(processRoot, "exe")); err != nil {
		t.Fatal(err)
	}
	uid := os.Geteuid()
	status := fmt.Sprintf("Name:\tcodex\nUid:\t%d\t%d\t%d\t%d\n", uid, uid, uid, uid)
	if err := os.WriteFile(filepath.Join(processRoot, "status"), []byte(status), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(processRoot, "cgroup"),
		[]byte("0::/system.slice/foreign-product-lookalike.service\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	lookalikeSocket := "/tmp/foreign-lookalike.sock"
	arguments := []string{
		executable,
		"-c", "features.code_mode_host=true",
		"app-server", "--listen", "unix://" + lookalikeSocket,
	}
	if err := os.WriteFile(
		filepath.Join(processRoot, "cmdline"),
		[]byte(strings.Join(arguments, "\x00")+"\x00"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	processes, err := findConsumerAppServersAt(
		procRoot,
		executable,
		uid,
		"/agent-state/product/app-server.sock",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(processes) != 1 || processes[0].PID != 4242 || processes[0].ExactTarget {
		t.Fatalf("foreign-cgroup same-uid lookalike was not rejected: %+v", processes)
	}
}

func TestCandidateStateReusesOnlyReadyOrExactHealthyExecution(t *testing.T) {
	for _, test := range []struct {
		status  string
		running bool
		want    bool
	}{
		{status: "ready", running: false, want: true},
		{status: "ready", running: true, want: true},
		{status: "executing", running: true, want: true},
		{status: "executing", running: false, want: false},
		{status: "blocked", running: true, want: false},
		{status: "failed", running: false, want: false},
	} {
		if got := candidateStateReusable(test.status, test.running); got != test.want {
			t.Fatalf("candidateStateReusable(%q, %t) = %t, want %t", test.status, test.running, got, test.want)
		}
	}
}

func TestRemoveExactSocketRefusesReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app-server.sock")
	first, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := pathSocketIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	// Keep the captured socket open while replacing its directory entry. This
	// models the supervisor's held app-server process/socket generation and
	// prevents a filesystem from immediately reusing the same inode number.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	second, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := removeExactSocket(path, identity); err == nil {
		t.Fatal("replacement socket was removed")
	}
}

func TestDrainSupervisorHandlersStopsTargetAndClosesActiveProxy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}

	var handlers sync.WaitGroup
	var active sync.Map
	active.Store(server, struct{}{})
	handlers.Add(1)
	handlerDone := make(chan struct{})
	go func() {
		defer handlers.Done()
		defer close(handlerDone)
		_, _ = io.Copy(io.Discard, server)
	}()

	stopped := false
	if err := drainSupervisorHandlers(
		func() error {
			stopped = true
			return nil
		},
		&active,
		&handlers,
	); err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("supervisor waited for active proxy before stopping the target")
	}
	select {
	case <-handlerDone:
	default:
		t.Fatal("active proxy handler did not exit during supervisor shutdown")
	}
}

func TestUnixSocketOwnerHelper(t *testing.T) {
	if os.Getenv("DEVKIT_TEST_SOCKET_OWNER_HELPER") != "1" {
		return
	}
	path := os.Getenv("DEVKIT_TEST_SOCKET_OWNER_PATH")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, _ = fmt.Fprintln(os.Stdout, "ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestPinnedProcessMustOwnDeclaredListeningSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decoy.sock")
	command := exec.Command(os.Args[0], "-test.run=^TestUnixSocketOwnerHelper$")
	command.Env = append(
		os.Environ(),
		"DEVKIT_TEST_SOCKET_OWNER_HELPER=1",
		"DEVKIT_TEST_SOCKET_OWNER_PATH="+path,
	)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("socket-owner helper failed: %v", err)
			}
		case <-time.After(2 * time.Second):
			_ = command.Process.Kill()
			<-done
			t.Error("socket-owner helper did not stop")
		}
	}()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "ready" {
		t.Fatalf("socket-owner helper was not ready: %q %v", scanner.Text(), scanner.Err())
	}

	parentStart, err := processStartTime(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proveProcessOwnsListeningUnixSocket(os.Getpid(), parentStart, path, path); err == nil {
		t.Fatal("same-uid process that did not own the listener was accepted")
	}

	helperStart, err := processStartTime(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	ownership, err := proveProcessOwnsListeningUnixSocket(command.Process.Pid, helperStart, path, path)
	if err != nil {
		t.Fatal(err)
	}
	if ownership.KernelInode == 0 || ownership.Filesystem.Inode == 0 || ownership.Filesystem.Owner != uint32(os.Geteuid()) {
		t.Fatalf("incomplete socket ownership proof: %+v", ownership)
	}
}

func TestSocketOwnershipChangeDiagnosticDoesNotWrapNil(t *testing.T) {
	expected := socketOwnership{
		Filesystem:  socketIdentity{Device: 1, Inode: 2, Owner: 3},
		KernelInode: 4,
	}
	observed := socketOwnership{
		Filesystem:  socketIdentity{Device: 5, Inode: 6, Owner: 7},
		KernelInode: 8,
	}
	message := socketOwnershipChangedDuringAcceptanceError(expected, observed).Error()
	for _, required := range []string{
		"expected filesystem device=1 inode=2 owner=3 kernel_inode=4",
		"observed filesystem device=5 inode=6 owner=7 kernel_inode=8",
	} {
		if !strings.Contains(message, required) {
			t.Fatalf("socket ownership diagnostic %q is missing %q", message, required)
		}
	}
	if strings.Contains(message, "%!w(<nil>)") {
		t.Fatalf("socket ownership diagnostic formatted a nil wrapped error: %q", message)
	}
}

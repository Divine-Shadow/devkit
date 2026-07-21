package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

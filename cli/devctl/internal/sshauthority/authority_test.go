package sshauthority

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewBuildsExactAbsoluteCommand(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "immutable ssh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	knownHosts := filepath.Join(t.TempDir(), "known hosts")
	if err := os.WriteFile(knownHosts, []byte("[ssh.github.com]:443 ssh-ed25519 fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	authority, err := New(executable, knownHosts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	config := filepath.Join(t.TempDir(), "source config")
	command, err := authority.Command(config)
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	want := "'" + executable + "' -F '" + config + "'"
	if command != want {
		t.Fatalf("command = %q, want %q", command, want)
	}
	if strings.HasPrefix(command, "ssh ") || strings.Contains(command, "/usr/bin") ||
		strings.Contains(command, "/run/current-system") {
		t.Fatalf("command selected ambient or host SSH authority: %q", command)
	}
}

func TestNewRejectsMissingRelativeAndNonExecutablePaths(t *testing.T) {
	nonExecutable := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(nonExecutable, []byte("not executable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(knownHosts, []byte("fixture.invalid ssh-ed25519 fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, executable := range map[string]string{
		"empty":          "",
		"relative":       "ssh",
		"missing":        filepath.Join(t.TempDir(), "missing-ssh"),
		"non-executable": nonExecutable,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(executable, knownHosts); err == nil {
				t.Fatalf("New(%q) unexpectedly succeeded", executable)
			}
		})
	}
}

func TestNewRejectsMissingRelativeAndEmptyKnownHosts(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(t.TempDir(), "empty-known-hosts")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, knownHosts := range map[string]string{
		"empty-path": "",
		"relative":   "known_hosts",
		"missing":    filepath.Join(t.TempDir(), "missing-known-hosts"),
		"empty-file": empty,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(executable, knownHosts); err == nil {
				t.Fatalf("New(%q, %q) unexpectedly succeeded", executable, knownHosts)
			}
		})
	}
}

func TestCommandRejectsMissingOrRelativeConfig(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(knownHosts, []byte("fixture.invalid ssh-ed25519 fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	authority, err := New(executable, knownHosts)
	if err != nil {
		t.Fatal(err)
	}
	for _, config := range []string{"", ".ssh/config"} {
		if _, err := authority.Command(config); err == nil {
			t.Fatalf("Command(%q) unexpectedly succeeded", config)
		}
	}
}

func TestInstallKnownHostsReplacesCallerMaterialExactly(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourceContent := []byte("[ssh.github.com]:443 ssh-ed25519 pinned-fixture\n")
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(knownHosts, sourceContent, 0o644); err != nil {
		t.Fatal(err)
	}
	authority, err := New(executable, knownHosts)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), ".ssh", "known_hosts")
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("ambient.invalid ssh-ed25519 mismatch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := authority.InstallKnownHosts(destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(sourceContent) {
		t.Fatalf("installed known-hosts = %q, want exact package content %q", got, sourceContent)
	}
	if info, err := os.Stat(destination); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("installed known-hosts mode = %v, error = %v", info, err)
	}
}

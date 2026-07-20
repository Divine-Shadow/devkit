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
	authority, err := New(executable)
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
	for name, executable := range map[string]string{
		"empty":          "",
		"relative":       "ssh",
		"missing":        filepath.Join(t.TempDir(), "missing-ssh"),
		"non-executable": nonExecutable,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(executable); err == nil {
				t.Fatalf("New(%q) unexpectedly succeeded", executable)
			}
		})
	}
}

func TestCommandRejectsMissingOrRelativeConfig(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	authority, err := New(executable)
	if err != nil {
		t.Fatal(err)
	}
	for _, config := range []string{"", ".ssh/config"} {
		if _, err := authority.Command(config); err == nil {
			t.Fatalf("Command(%q) unexpectedly succeeded", config)
		}
	}
}

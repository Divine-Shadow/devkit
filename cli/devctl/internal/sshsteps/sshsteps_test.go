package sshsteps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/sshauthority"
)

func TestGitSSHStepsUseInjectedImmutableAuthority(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "package-ssh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	authority, err := sshauthority.New(executable)
	if err != nil {
		t.Fatal(err)
	}
	home := "/worktrees/agent1/.devhome-agent1"
	repo := "/worktrees/agent1/product"
	global, err := GitSetGlobalSSH(authority, home)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := GitSetRepoSSH(authority, repo, home)
	if err != nil {
		t.Fatal(err)
	}
	pull, err := GitPullWithSSH(authority, repo, home)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{global, worktree, pull} {
		if !strings.Contains(command, executable) ||
			!strings.Contains(command, filepath.Join(home, ".ssh", "config")) {
			t.Fatalf("command omitted injected SSH authority: %s", command)
		}
		if strings.Contains(command, "ssh -F") {
			t.Fatalf("command emitted ambient SSH authority: %s", command)
		}
	}
}

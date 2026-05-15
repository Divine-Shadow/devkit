package agentexec

import (
	"strings"
	"testing"
)

func TestBuildNativeCommandUsesDevkitExec(t *testing.T) {
	cmd, err := BuildNativeCommand(NativeCommandOpts{
		Exe:     "/home/me/dev/devkit/kit/scripts/devkit",
		Project: "dev-all",
		Index:   "2",
		Dest:    "/workspaces/dev/agent-worktrees/agent2/ouroboros-ide/frontend",
	})
	if err != nil {
		t.Fatalf("BuildNativeCommand returned error: %v", err)
	}
	for _, frag := range []string{
		"'/home/me/dev/devkit/kit/scripts/devkit' -p 'dev-all' exec 2 --repo 'ouroboros-ide' -- bash -lc",
		"/worktrees/agent2/ouroboros-ide/frontend",
		"exec zsh -i",
	} {
		if !strings.Contains(cmd, frag) {
			t.Fatalf("native command missing %q: %s", frag, cmd)
		}
	}
	if strings.Contains(cmd, "docker") {
		t.Fatalf("native command must not reference docker: %s", cmd)
	}
	if strings.Contains(cmd, "|| cd") || strings.Contains(cmd, "2>/dev/null") {
		t.Fatalf("native command must fail loudly on bad destinations: %s", cmd)
	}
	for _, retired := range []string{"/usr/local/bin/" + "codex", "codex" + "w", "docker " + "compose", "docker " + "exec"} {
		if strings.Contains(cmd, retired) {
			t.Fatalf("native command contains retired runtime assumption %q: %s", retired, cmd)
		}
	}
}

func TestBuildNativeCommandRelativeDestUsesAgentRepo(t *testing.T) {
	cmd, err := BuildNativeCommand(NativeCommandOpts{
		Exe:     "devkit",
		Project: "dev-all",
		Index:   "1",
		Repo:    "devkit",
		Dest:    "cli/devctl",
	})
	if err != nil {
		t.Fatalf("BuildNativeCommand returned error: %v", err)
	}
	if !strings.Contains(cmd, "/workspaces/dev/devkit/cli/devctl") {
		t.Fatalf("unexpected native command: %s", cmd)
	}
}

func TestBuildNativeCommandLayoutPathUsesAgentWorktree(t *testing.T) {
	cmd, err := BuildNativeCommand(NativeCommandOpts{
		Exe:     "devkit",
		Project: "dev-all",
		Index:   "3",
		Repo:    "ouroboros-ide",
		Dest:    "agent-worktrees/agent3/ouroboros-ide",
	})
	if err != nil {
		t.Fatalf("BuildNativeCommand returned error: %v", err)
	}
	if !strings.Contains(cmd, "/worktrees/agent3/ouroboros-ide") {
		t.Fatalf("unexpected native command: %s", cmd)
	}
	if strings.Contains(cmd, "/worktrees/agent3/ouroboros-ide/agent-worktrees/agent3/ouroboros-ide") {
		t.Fatalf("layout path was appended inside repo: %s", cmd)
	}
}

func TestBuildNativeCommandRepoLayoutPathUsesAgentBase(t *testing.T) {
	cmd, err := BuildNativeCommand(NativeCommandOpts{
		Exe:     "devkit",
		Project: "dev-all",
		Index:   "1",
		Repo:    "ouroboros-ide",
		Dest:    "ouroboros-ide",
	})
	if err != nil {
		t.Fatalf("BuildNativeCommand returned error: %v", err)
	}
	if !strings.Contains(cmd, "/workspaces/dev/ouroboros-ide") {
		t.Fatalf("unexpected native command: %s", cmd)
	}
	if strings.Contains(cmd, "/workspaces/dev/ouroboros-ide/ouroboros-ide") {
		t.Fatalf("repo layout path was appended inside repo: %s", cmd)
	}
}

package agentexec

import (
	"strings"
	"testing"
)

func TestBuildCommandPrefersZshInteractiveShell(t *testing.T) {
	cmd, err := BuildCommand(CommandOpts{
		Project:        "dev-all",
		Index:          "2",
		Dest:           "/workspaces/dev/agent-worktrees/agent2/ouroboros-ide",
		Service:        "dev-agent",
		ComposeProject: "devkit-ouro8",
		ContainerName:  "devkit-ouro8-dev-agent-2",
		GitName:        "Test User",
		GitEmail:       "test@example.com",
	})
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}
	for _, frag := range []string{
		"docker exec -it 'devkit-ouro8-dev-agent-2' bash -lc",
		`export HOME="/workspaces/dev/.devhome" CODEX_HOME="/workspaces/dev/.devhome/.codex"`,
		`cd "/workspaces/dev/agent-worktrees/agent2/ouroboros-ide" 2>/dev/null || true; if command -v zsh >/dev/null 2>&1; then exec zsh -i; fi; exec bash`,
	} {
		if !strings.Contains(cmd, frag) {
			t.Fatalf("command missing %q: %s", frag, cmd)
		}
	}
}

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
	for _, legacy := range []string{"/usr/local/bin/codex", "codexw", "docker compose", "docker exec"} {
		if strings.Contains(cmd, legacy) {
			t.Fatalf("native command contains legacy runtime assumption %q: %s", legacy, cmd)
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
	if !strings.Contains(cmd, "/worktrees/agent1/devkit/cli/devctl") {
		t.Fatalf("unexpected native command: %s", cmd)
	}
}

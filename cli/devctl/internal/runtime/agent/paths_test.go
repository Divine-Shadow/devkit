package agent

import "testing"

func TestResolvePathsDedicatedAgentOne(t *testing.T) {
	got, err := ResolvePaths(PathConfig{
		DevkitRoot:        "/home/me/dev/devkit",
		Project:           "dev-all",
		Repo:              "ouroboros-ide",
		Index:             1,
		DedicatedWorktree: true,
	})
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if got.HostWorktree != "/home/me/dev/agent-worktrees/agent1/ouroboros-ide" {
		t.Fatalf("host worktree = %q", got.HostWorktree)
	}
	if got.SandboxWorktree != "/worktrees/agent1/ouroboros-ide" {
		t.Fatalf("sandbox worktree = %q", got.SandboxWorktree)
	}
	if got.HostHome != "/home/me/dev/.devkit/native-agents/dev-all-agent1/home" {
		t.Fatalf("host home = %q", got.HostHome)
	}
	if got.SandboxHome != "/agent-state/dev-all-agent1/home" {
		t.Fatalf("sandbox home = %q", got.SandboxHome)
	}
}

func TestResolvePathsUsesConfiguredRoots(t *testing.T) {
	got, err := ResolvePaths(PathConfig{
		DevkitRoot:            "/home/me/dev/devkit",
		Project:               "dev-all",
		Repo:                  "devkit",
		Index:                 2,
		WorktreeRoot:          "../native-worktrees",
		StateRoot:             "../native-state",
		WorktreeContainerRoot: "/native-worktrees",
		StateContainerRoot:    "/native-state",
		DedicatedWorktree:     true,
	})
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if got.HostWorktreeRoot != "/home/me/dev/native-worktrees" {
		t.Fatalf("host worktree root = %q", got.HostWorktreeRoot)
	}
	if got.HostStateRoot != "/home/me/dev/native-state" {
		t.Fatalf("host state root = %q", got.HostStateRoot)
	}
	if got.SandboxWorktree != "/native-worktrees/agent2/devkit" {
		t.Fatalf("sandbox worktree = %q", got.SandboxWorktree)
	}
	if got.SandboxHome != "/native-state/dev-all-agent2/home" {
		t.Fatalf("sandbox home = %q", got.SandboxHome)
	}
}

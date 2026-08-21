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
	if got.HostHome != "/home/me/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1" {
		t.Fatalf("host home = %q", got.HostHome)
	}
	if got.SandboxHome != "/worktrees/agent1/ouroboros-ide/.devhome-agent1" {
		t.Fatalf("sandbox home = %q", got.SandboxHome)
	}
	if got.HostAgentStateRoot != "/home/me/dev/.devkit/native-agents/dev-all-agent1" {
		t.Fatalf("host agent state root = %q", got.HostAgentStateRoot)
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
	if got.SandboxHome != "/native-worktrees/agent2/.devhome-agent2" {
		t.Fatalf("sandbox home = %q", got.SandboxHome)
	}
	if got.HostHome != "/home/me/dev/native-worktrees/agent2/.devhome-agent2" {
		t.Fatalf("host home = %q", got.HostHome)
	}
}

func TestResolvePathsUsesSourceDeclaredAgentStatePrefix(t *testing.T) {
	got, err := ResolvePaths(PathConfig{
		DevkitRoot:        "/home/me/dev/devkit",
		Project:           "pokeemerald-expansion-shared-power",
		Repo:              "pokeemerald-expansion-shared-power",
		Index:             2,
		AgentStatePrefix:  "pokeemerald",
		DedicatedWorktree: true,
	})
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if got.HostWorktree != "/home/me/dev/agent-worktrees/agent2/pokeemerald-expansion-shared-power" {
		t.Fatalf("host worktree = %q", got.HostWorktree)
	}
	if got.HostHome != "/home/me/dev/.devkit/native-agents/pokeemerald-agent2/home" {
		t.Fatalf("host home = %q", got.HostHome)
	}
	if got.SandboxHome != "/agent-state/pokeemerald-agent2/home" {
		t.Fatalf("sandbox home = %q", got.SandboxHome)
	}
}

func TestResolvePathsRejectsUnsafeAgentStatePrefix(t *testing.T) {
	for _, prefix := range []string{"../pokeemerald", "nested/pokeemerald", ".", ".."} {
		_, err := ResolvePaths(PathConfig{
			DevkitRoot:       "/home/me/dev/devkit",
			Project:          "pokeemerald-expansion-shared-power",
			Repo:             "pokeemerald-expansion-shared-power",
			AgentStatePrefix: prefix,
		})
		if err == nil {
			t.Fatalf("unsafe agent state prefix %q was accepted", prefix)
		}
	}
}

func TestResolvePathsWorkspaceRootRepo(t *testing.T) {
	got, err := ResolvePaths(PathConfig{
		DevkitRoot:        "/home/me/dev/devkit",
		Project:           "dev-workspace",
		Repo:              ".",
		Index:             1,
		DedicatedWorktree: true,
	})
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if got.HostWorktree != "/home/me/dev" {
		t.Fatalf("host worktree = %q", got.HostWorktree)
	}
	if got.SandboxWorktree != "/workspaces/dev" {
		t.Fatalf("sandbox worktree = %q", got.SandboxWorktree)
	}
	if got.HostHome != "/home/me/dev/.devkit/native-agents/dev-workspace-agent1/home" {
		t.Fatalf("host home = %q", got.HostHome)
	}
	if got.SandboxHome != "/agent-state/dev-workspace-agent1/home" {
		t.Fatalf("sandbox home = %q", got.SandboxHome)
	}
}

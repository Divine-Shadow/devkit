package agent

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistentHomeProductionAppServerSocketFitsAndBinds(t *testing.T) {
	production, err := ResolvePaths(PathConfig{DevkitRoot: "/nix/store/fixture-devkit", HostRoot: "/home/bayesartre/dev", Project: "dev-all", Repo: "ouroboros-ide", Index: 1, DedicatedWorktree: true})
	if err != nil {
		t.Fatal(err)
	}
	productionSocket := filepath.Join(production.HostHome, ".codex", "a1-app.sock")
	if len(productionSocket) > 107 {
		t.Fatalf("canonical app-server socket exceeds Unix limit: %d bytes", len(productionSocket))
	}
	fixture, err := os.MkdirTemp("/tmp", "devkit-home-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(fixture)
	padding := len(production.HostStateRoot) - len(fixture) - 1
	if padding < 1 {
		t.Fatal("socket fixture parent is too long to model production")
	}
	paths, err := ResolvePaths(PathConfig{DevkitRoot: "/nix/store/fixture-devkit", HostRoot: "/home/bayesartre/dev", Project: "dev-all", Repo: "ouroboros-ide", Index: 1, DedicatedWorktree: true, StateRoot: filepath.Join(fixture, strings.Repeat("s", padding))})
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(paths.HostHome, ".codex", "a1-app.sock")
	if len(socket) != len(productionSocket) || !strings.HasPrefix(socket, fixture+"/") {
		t.Fatal("socket fixture differs from production length or escaped its private root")
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("production-length persistent-home app-server socket cannot bind: %v", err)
	}
	defer listener.Close()
}

func TestPersistentDevAllHomeKeepsAgentOneProtectedLegacyGeometry(t *testing.T) {
	worktree := "/home/me/dev/agent-worktrees/agent1/ouroboros-ide"
	if got, want := PersistentDevAllHome(worktree, 1), worktree+"/.devhome-agent1"; got != want {
		t.Fatalf("agent1 persistent home = %q, want protected legacy path %q", got, want)
	}
	if got, forbidden := PersistentDevAllHome(worktree, 1), "/home/me/dev/agent-worktrees/agent1/.devhome-agent1"; got == forbidden {
		t.Fatalf("agent1 selected sibling home %q instead of protected legacy geometry", got)
	}
	worktree = "/home/me/dev/agent-worktrees/agent2/ouroboros-ide"
	if got, want := PersistentDevAllHome(worktree, 2), "/home/me/dev/agent-worktrees/agent2/.devhome-agent2"; got != want {
		t.Fatalf("agent2 persistent home = %q, want %q", got, want)
	}
}

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
	if got.HostHome != "/home/me/dev/.devkit/native-agents/.homes/dev-all-agent1/ouroboros-ide" {
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
	if got.HostHome != "/home/me/dev/native-state/.homes/dev-all-agent2/devkit" {
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

package plan

import (
	"path/filepath"
	"testing"

	"devkit/cli/devctl/internal/runtime/agent"
)

func TestIsolatedWorkspaceAllowsOnlySelectedPersistentHomeBind(t *testing.T) {
	root := t.TempDir()
	paths, err := agent.ResolvePaths(agent.PathConfig{
		DevkitRoot: filepath.Join(root, "devkit"), Project: "dev-all", Repo: "ouroboros-ide", Index: 4,
		WorktreeRoot: filepath.Join(root, "worktrees"), StateRoot: filepath.Join(root, "state"), DedicatedWorktree: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	lane := filepath.Dir(paths.HostWorktree)
	paths.SandboxWorktree = "/workspaces/dev/ouroboros-ide"
	paths.SandboxHome = "/workspaces/dev/.devhome-agent4"
	base := Bind{Source: lane, Target: "/workspaces/dev", Mode: "rw", Required: true}
	home := Bind{Source: paths.HostHome, Target: paths.SandboxHome, Mode: "rw", Required: true}
	if err := validateIsolatedWorkspaceRootBinds([]Bind{base, home}, lane, "dev-all", "ouroboros-ide", paths); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		bind Bind
	}{
		{"sibling-home", Bind{Source: filepath.Join(agent.PersistentHomesRoot(paths.HostStateRoot), "dev-all-agent3", "ouroboros-ide"), Target: home.Target, Mode: "rw", Required: true}},
		{"whole-storage-root", Bind{Source: agent.PersistentHomesRoot(paths.HostStateRoot), Target: home.Target, Mode: "rw", Required: true}},
		{"other-repository", Bind{Source: filepath.Join(filepath.Dir(paths.HostHome), "other-repo"), Target: home.Target, Mode: "rw", Required: true}},
		{"wrong-target", Bind{Source: home.Source, Target: "/workspaces/dev/other-home", Mode: "rw", Required: true}},
		{"optional-home", Bind{Source: home.Source, Target: home.Target, Mode: "rw", Required: false}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateIsolatedWorkspaceRootBinds([]Bind{base, test.bind}, lane, "dev-all", "ouroboros-ide", paths); err == nil {
				t.Fatal("cross-lane home bind was accepted")
			}
		})
	}
	if isPersistentNativeHomeBind("dev-workspace", "shadow-throne-management", paths, home) {
		t.Fatal("Product home exception applied to Management")
	}
}

package launch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/runtime/agent"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

func persistentHomeFixture(t *testing.T, index int) nativeplan.Plan {
	t.Helper()
	root := t.TempDir()
	paths, err := agent.ResolvePaths(agent.PathConfig{
		DevkitRoot: filepath.Join(root, "devkit"), Project: "dev-all", Repo: "fixture",
		Index: index, DedicatedWorktree: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return nativeplan.Plan{
		HostStateRoot: paths.HostStateRoot,
		Agent: agent.Spec{ID: agent.ID{Project: "dev-all", Repo: "fixture", Index: index},
			HostHome: paths.HostHome, HostWorktree: paths.HostWorktree, StateRoot: paths.HostAgentStateRoot},
	}
}

func TestPersistentHomeEntryRefusesLegacyDataBeforeBootstrapMutation(t *testing.T) {
	for _, index := range []int{1, 2} {
		for _, kind := range []string{"worktree-home", "execution-state-home"} {
			t.Run(fmt.Sprintf("%d/%s", index, kind), func(t *testing.T) {
				p := persistentHomeFixture(t, index)
				legacy := agent.PersistentDevAllHome(p.Agent.HostWorktree, index)
				if kind == "execution-state-home" {
					legacy = filepath.Join(p.Agent.StateRoot, "home")
				}
				if err := os.MkdirAll(filepath.Join(legacy, ".codex"), 0o700); err != nil {
					t.Fatal(err)
				}
				data := filepath.Join(legacy, ".codex", "protected-input")
				if err := os.WriteFile(data, []byte("existing operator data"), 0o600); err != nil {
					t.Fatal(err)
				}
				if _, err := PrepareGitBootstrap(p); err == nil || !strings.Contains(err.Error(), "requires environment-owned migration") {
					t.Fatalf("bootstrap should refuse existing legacy data: %v", err)
				}
				if got, err := os.ReadFile(data); err != nil || string(got) != "existing operator data" {
					t.Fatalf("legacy input changed: %q %v", got, err)
				}
				if _, err := os.Lstat(p.Agent.HostHome); !os.IsNotExist(err) {
					t.Fatalf("refusal created new persistent home: %v", err)
				}
			})
		}
	}
}

func TestPersistentHomeEntryRejectsRedirectedStorageBeforeWriting(t *testing.T) {
	p := persistentHomeFixture(t, 2)
	outside := t.TempDir()
	if err := os.MkdirAll(p.HostStateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, agent.PersistentHomesRoot(p.HostStateRoot)); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareGitBootstrap(p); err == nil || !strings.Contains(err.Error(), "symlink or non-directory") {
		t.Fatalf("bootstrap should refuse redirected persistent storage: %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("redirected storage changed: %v %v", entries, err)
	}
}

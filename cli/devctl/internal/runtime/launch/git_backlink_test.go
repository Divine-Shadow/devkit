package launch

import (
	"os"
	"path/filepath"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

func gitBacklinkPlanFixture(t *testing.T) nativeplan.Plan {
	t.Helper()
	root := t.TempDir()
	worktreeRoot := filepath.Join(root, "lanes")
	wt := filepath.Join(worktreeRoot, "agent1", "ouroboros-ide")
	common := filepath.Join(worktreeRoot, ".devkit", "git", "agent1", "ouroboros-ide.git")
	registration := filepath.Join(common, "worktrees", "selected")
	for _, dir := range []string{wt, registration, filepath.Join(root, "devkit")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	forward, err := filepath.Rel(wt, registration)
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := filepath.Rel(registration, filepath.Join(wt, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	for path, value := range map[string]string{
		filepath.Join(wt, ".git"): "gitdir: " + forward + "\n", filepath.Join(registration, "commondir"): "../..\n",
		filepath.Join(registration, "gitdir"): reverse + "\n", filepath.Join(common, "devkit-owned-common"): "schema=devkit/native-owned-common-repository/v2\nrepository=ouroboros-ide\norigin=fixture\nlane=agent1\n",
	} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := nativeplan.Build(nativeplan.BuildOptions{Paths: devkitpaths.Paths{Root: filepath.Join(root, "devkit")}, Project: "dev-all", Repo: "ouroboros-ide", Index: 1, WorktreeRoot: worktreeRoot, WorkspaceRoot: filepath.Dir(wt), IsolationProfile: nativeplan.IsolationProfileWorkspaceEgress, EgressAllowlist: filepath.Join(root, "allowlist")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.Agent.StateRoot, 0700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGitBacklinkPreparationAndLaunchBarriers(t *testing.T) {
	t.Run("prepare-and-reuse-exact-file", func(t *testing.T) {
		p := gitBacklinkPlanFixture(t)
		if err := prepareGitBacklink(p); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(p.GitBacklink.Source)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyGitBacklink(p, false); err != nil {
			t.Fatal(err)
		}
		if err := prepareGitBacklink(p); err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(p.GitBacklink.Source)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(before, after) {
			t.Fatal("unchanged projection replaced a live mounted inode")
		}
	})
	for _, name := range []string{"wrong-bytes", "symlink-file", "symlink-state", "changed-host-backlink", "missing-recipe", "writable-bind"} {
		t.Run(name, func(t *testing.T) {
			p := gitBacklinkPlanFixture(t)
			if err := prepareGitBacklink(p); err != nil {
				t.Fatal(err)
			}
			source := p.GitBacklink.Source
			switch name {
			case "wrong-bytes":
				if err := os.WriteFile(source, []byte("/foreign/.git\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink-file", "symlink-state":
				path := source
				if name == "symlink-state" {
					path = p.Agent.StateRoot
				}
				if err := os.Rename(path, path+".fixture"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".fixture", path); err != nil {
					t.Fatal(err)
				}
			case "changed-host-backlink":
				path := filepath.Join(p.HostWorktreeRoot, ".devkit", "git", "agent1", "ouroboros-ide.git", "worktrees", "selected", "gitdir")
				if err := os.WriteFile(path, []byte("/foreign/.git\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-recipe":
				p.GitBacklink = nil
			case "writable-bind":
				p.Binds[len(p.Binds)-1].Mode = "rw"
			}
			if err := Prepare(p); err == nil {
				t.Fatal("unsafe preparation accepted")
			}
			if _, err := BuildBubblewrap(p, []string{"true"}); err == nil {
				t.Fatal("unsafe command construction accepted")
			}
			if _, err := os.Lstat(p.Agent.HostHome); !os.IsNotExist(err) {
				t.Fatalf("refusal wrote selected home: %v", err)
			}
		})
	}
}

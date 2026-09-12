package plan

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
	"devkit/cli/devctl/internal/worktrees"
)

func gitFixtureCommand(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func realProductGitPlan(t *testing.T) (Plan, string, string) {
	t.Helper()
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	gitFixtureCommand(t, "init", "--initial-branch=main", seed)
	gitFixtureCommand(t, "-C", seed, "commit", "--allow-empty", "-m", "seed")
	worktreeRoot := filepath.Join(root, "leased-roots")
	var selected, sibling string
	for index := 1; index <= 2; index++ {
		common := filepath.Join(worktreeRoot, ".devkit", "git", fmt.Sprintf("agent%d", index), "ouroboros-ide.git")
		gitFixtureCommand(t, "clone", "--bare", seed, common)
		marker := fmt.Sprintf("schema=devkit/native-owned-common-repository/v2\nrepository=ouroboros-ide\norigin=%s\nlane=agent%d\n", seed, index)
		if err := os.WriteFile(filepath.Join(common, "devkit-owned-common"), []byte(marker), 0600); err != nil {
			t.Fatal(err)
		}
		wt := filepath.Join(worktreeRoot, fmt.Sprintf("agent%d", index), "ouroboros-ide")
		gitFixtureCommand(t, "--git-dir", common, "worktree", "add", "-b", fmt.Sprintf("lane%d", index), wt, "main")
		if err := worktrees.EnsurePortableNativeGitdir(wt, worktreeRoot, "ouroboros-ide", index); err != nil {
			t.Fatal(err)
		}
		if index == 1 {
			selected = wt
		} else {
			sibling = common
		}
	}
	devkit := filepath.Join(root, "devkit")
	if err := os.MkdirAll(devkit, 0755); err != nil {
		t.Fatal(err)
	}
	p, err := Build(BuildOptions{Paths: devkitpaths.Paths{Root: devkit}, Project: "dev-all", Repo: "ouroboros-ide", Index: 1, WorktreeRoot: worktreeRoot, WorkspaceRoot: filepath.Dir(selected), IsolationProfile: IsolationProfileWorkspaceEgress, EgressAllowlist: filepath.Join(devkit, "allowlist")})
	if err != nil {
		t.Fatal(err)
	}
	return p, selected, sibling
}

func TestProductGitReciprocalSelectedNamespace(t *testing.T) {
	p, selected, sibling := realProductGitPlan(t)
	before := snapshotGitFiles(t, sibling)
	projection := p.GitBacklink
	if projection == nil {
		t.Fatal("missing native backlink recipe")
	}
	if err := os.MkdirAll(filepath.Dir(projection.Source), 0700); err != nil {
		t.Fatal(err)
	}
	// This plan fixture materializes the emitted recipe. Native CLI integration
	// separately exercises the real preparation and launch path.
	if err := os.WriteFile(projection.Source, []byte(projection.Value), 0600); err != nil {
		t.Fatal(err)
	}
	metadata := strings.TrimSpace(strings.TrimPrefix(string(mustReadGitFile(t, filepath.Join(selected, ".git"))), "gitdir:"))
	gitdir := filepath.Clean(filepath.Join(selected, metadata))
	hostPointers := map[string]string{}
	for _, path := range []string{filepath.Join(selected, ".git"), filepath.Join(gitdir, "commondir"), filepath.Join(gitdir, "gitdir")} {
		hostPointers[path] = string(mustReadGitFile(t, path))
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("real bubblewrap unavailable; no native namespace proof")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	git, err = filepath.EvalSymlinks(git)
	if err != nil {
		t.Fatal(err)
	}
	base := []string{"--unshare-user", "--ro-bind", "/nix/store", "/nix/store", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp"}
	canary := exec.Command(bwrap, append(append([]string{}, base...), git, "--version")...)
	if out, err := canary.CombinedOutput(); err != nil {
		t.Skipf("real namespace unavailable (not a PASS): %v: %s", err, out)
	}
	for _, bind := range p.Binds {
		if bind.Target == "/workspaces/dev" || strings.HasPrefix(bind.Target, "/workspaces/.devkit/git/") {
			flag := "--bind"
			if bind.Mode == "ro" {
				flag = "--ro-bind"
			}
			base = append(base, flag, bind.Source, bind.Target)
		}
	}
	cmd := exec.Command(bwrap, append(base, git, "-C", p.Agent.SandboxWorktree, "worktree", "list", "--porcelain")...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native Git: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "worktree "+p.Agent.SandboxWorktree+"\n") || strings.Contains(string(out), "prunable") {
		t.Fatalf("native reciprocal registration missing or prunable: %s", out)
	}
	nativeGit := func(args ...string) string {
		t.Helper()
		argv := append(append([]string{}, base...), git, "-C", p.Agent.SandboxWorktree)
		cmd := exec.Command(bwrap, append(argv, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("native Git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	if err := os.WriteFile(filepath.Join(selected, "proof"), []byte("native index proof\n"), 0600); err != nil {
		t.Fatal(err)
	}
	nativeGit("status", "--porcelain")
	nativeGit("add", "proof")
	if got := gitFixtureCommand(t, "-C", selected, "diff", "--cached", "--name-only"); got != "proof\n" {
		t.Fatalf("host index diverged: %s", got)
	}
	nativeGit("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "native view commit")
	nativeGit("update-ref", "refs/heads/projection-proof", "HEAD")
	nativeHead := nativeGit("rev-parse", "HEAD")
	if hostHead := gitFixtureCommand(t, "-C", selected, "rev-parse", "HEAD"); hostHead != nativeHead {
		t.Fatalf("HEAD diverged: %s != %s", hostHead, nativeHead)
	}
	if hostRef := gitFixtureCommand(t, "-C", selected, "rev-parse", "refs/heads/projection-proof"); hostRef != nativeHead {
		t.Fatal("host ref diverged")
	}
	if status := gitFixtureCommand(t, "-C", selected, "status", "--porcelain"); status != "" {
		t.Fatalf("host index not clean: %s", status)
	}
	if after := snapshotGitFiles(t, sibling); !reflect.DeepEqual(before, after) {
		t.Fatal("sibling metadata changed")
	}
	for path, want := range hostPointers {
		if string(mustReadGitFile(t, path)) != want {
			t.Fatalf("host pointer changed: %s", path)
		}
	}
	host := gitFixtureCommand(t, "-C", selected, "worktree", "list", "--porcelain")
	if !strings.Contains(host, "worktree "+selected+"\n") || strings.Contains(host, "prunable") {
		t.Fatalf("host reciprocal registration broken: %s", host)
	}
}

func mustReadGitFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func snapshotGitFiles(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	result := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		var data []byte
		if entry.Type()&os.ModeSymlink != 0 {
			value, e := os.Readlink(path)
			err = e
			data = []byte("symlink:" + value)
		} else {
			data, err = os.ReadFile(path)
		}
		if err != nil {
			return err
		}
		result[path] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestProductGitBacklinkRefusesUnownedOrConflictingMetadata(t *testing.T) {
	cases := []string{"foreign-common", "wrong-backlink", "missing-backlink", "wrong-lane-marker", "duplicate-marker", "malformed-forward", "symlink-forward", "symlink-common", "traversing-commondir", "symlink-backlink", "changed-plan", "missing-plan", "writable-projection", "shadowed-projection", "duplicate-common-bind", "conflicting-common-bind"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			p, wt, sibling := realProductGitPlan(t)
			metadata, err := worktrees.InspectNativeLaneGitMetadata(wt, p.HostWorktreeRoot, "ouroboros-ide", 1)
			if err != nil {
				t.Fatal(err)
			}
			write := func(path, value string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
			}
			link := func(path string) {
				t.Helper()
				saved := path + ".fixture"
				if err := os.Rename(path, saved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(saved, path); err != nil {
					t.Fatal(err)
				}
			}
			switch name {
			case "foreign-common":
				write(filepath.Join(metadata.GitDir, "commondir"), sibling+"\n")
			case "wrong-backlink":
				write(filepath.Join(metadata.GitDir, "gitdir"), filepath.Join(p.HostWorktreeRoot, "agent2", "ouroboros-ide", ".git")+"\n")
			case "missing-backlink":
				if err := os.Remove(filepath.Join(metadata.GitDir, "gitdir")); err != nil {
					t.Fatal(err)
				}
			case "wrong-lane-marker":
				path := filepath.Join(metadata.CommonDir, "devkit-owned-common")
				write(path, strings.ReplaceAll(string(mustReadGitFile(t, path)), "lane=agent1", "lane=agent2"))
			case "duplicate-marker":
				path := filepath.Join(metadata.CommonDir, "devkit-owned-common")
				write(path, string(mustReadGitFile(t, path))+"lane=agent1\n")
			case "malformed-forward":
				write(metadata.GitFile, "gitdir: one\ngitdir: two\n")
			case "symlink-forward":
				link(metadata.GitFile)
			case "symlink-common":
				link(metadata.CommonDir)
			case "symlink-backlink":
				link(filepath.Join(metadata.GitDir, "gitdir"))
			case "traversing-commondir":
				write(filepath.Join(metadata.GitDir, "commondir"), "../../../../../git/agent2/ouroboros-ide.git\n")
			case "changed-plan":
				p.GitBacklink.Value = "/workspaces/dev/foreign/.git\n"
			case "missing-plan":
				p.GitBacklink = nil
			case "writable-projection":
				p.Binds[len(p.Binds)-1].Mode = "rw"
			case "duplicate-common-bind", "conflicting-common-bind":
				for _, bind := range p.Binds {
					if bind.Source == metadata.CommonDir {
						if name == "conflicting-common-bind" {
							bind.Source = sibling
						}
						p.Binds = append([]Bind{bind}, p.Binds...)
						break
					}
				}

			case "shadowed-projection":
				p.Binds = append(p.Binds, Bind{Source: metadata.CommonDir, Target: filepath.Dir(filepath.Dir(p.GitBacklink.Target)), Mode: "rw", Required: true})
			}
			before := snapshotGitFiles(t, p.HostWorktreeRoot)
			if err := ValidateGitBacklinkProjection(p); err == nil {
				t.Fatal("unsafe native projection accepted")
			}
			if after := snapshotGitFiles(t, p.HostWorktreeRoot); !reflect.DeepEqual(before, after) {
				t.Fatal("refusal changed Git metadata")
			}
			if _, err := os.Lstat(filepath.Join(p.Agent.StateRoot, "native-git-backlink")); !os.IsNotExist(err) {
				t.Fatalf("refusal materialized state: %v", err)
			}
		})
	}
}

func TestProductGitBacklinkDoesNotAdmitByUnrelatedRegistrationState(t *testing.T) {
	p, _, _ := realProductGitPlan(t)
	common := filepath.Join(p.HostWorktreeRoot, ".devkit", "git", "agent1", "ouroboros-ide.git")
	stale := filepath.Join(common, "worktrees", "unrelated-stale")
	if err := os.Mkdir(stale, 0700); err != nil {
		t.Fatal(err)
	}
	// Missing or dangling unrelated backlinks are Git's pruning concern, not
	// authority to deny this already-proven selected reciprocal pair.
	if err := ValidateGitBacklinkProjection(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "gitdir"), []byte("/absent/unrelated/.git\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGitBacklinkProjection(p); err != nil {
		t.Fatal(err)
	}
}

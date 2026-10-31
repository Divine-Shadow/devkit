package worktrees

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/execx"
	"devkit/cli/devctl/internal/gitauthority"
)

func TestCurrentSourceRequiresActualNonsymbolicGitExit(t *testing.T) {
	s, _, _ := sourceFixture(t, false)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	_, direct := execx.Capture(context.Background(), git, "-C", s.Worktree, "symbolic-ref", "-q", "refs/remotes/origin/main")
	if !confirmedDirectTracking(direct, nil) {
		t.Fatalf("real nonsymbolic Git result not recognized: %#v", direct)
	}
	_, missing := execx.Capture(context.Background(), filepath.Join(t.TempDir(), "absent"))
	if missing.Code != 1 || confirmedDirectTracking(missing, nil) {
		t.Fatal("failed exec classified as nonsymbolic Git")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, cancelled := execx.Capture(ctx, git, "-C", s.Worktree, "symbolic-ref", "-q", "refs/remotes/origin/main")
	if confirmedDirectTracking(cancelled, ctx.Err()) {
		t.Fatal("cancelled inspection classified as nonsymbolic")
	}
	sourceTestGit(t, s.Worktree, "symbolic-ref", "refs/remotes/origin/main", "refs/heads/main")
	_, symbolic := execx.Capture(context.Background(), git, "-C", s.Worktree, "symbolic-ref", "-q", "refs/remotes/origin/main")
	if confirmedDirectTracking(symbolic, nil) {
		t.Fatal("symbolic target classified as direct")
	}
}

func sourceTestGit(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Fixture", "GIT_COMMITTER_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func sourceFixture(t *testing.T, shared bool) (CurrentSource, string, string) {
	t.Helper()
	root := t.TempDir()
	primary := filepath.Join(root, "primary")
	if err := os.Mkdir(primary, 0700); err != nil {
		t.Fatal(err)
	}
	sourceTestGit(t, primary, "init", "-b", "main")
	sourceTestGit(t, primary, "commit", "--allow-empty", "-m", "initial")
	remote := filepath.Join(root, "origin.git")
	sourceTestGit(t, root, "clone", "--bare", primary, remote)
	sourceTestGit(t, primary, "remote", "add", "origin", remote)
	base := filepath.Join(root, "dev", "ouroboros-ide")
	home := filepath.Join(base, ".devhome-agent1")
	writer := filepath.Join(home, ".codex", "worktrees", "writer", "ouroboros-ide")
	if err := os.MkdirAll(filepath.Dir(base), 0700); err != nil {
		t.Fatal(err)
	}
	if shared {
		common := filepath.Join(root, ".devkit", "git", "agent1", "ouroboros-ide.git")
		sourceTestGit(t, root, "init", "--bare", common)
		sourceTestGit(t, root, "--git-dir", common, "remote", "add", "origin", remote)
		sourceTestGit(t, root, "--git-dir", common, "fetch", "origin", "+refs/heads/main:refs/remotes/origin/main")
		marker, _ := nativeOwnedCommonRepositoryMarker("ouroboros-ide", remote, 1)
		if err := os.WriteFile(filepath.Join(common, "devkit-owned-common"), []byte(marker), 0600); err != nil {
			t.Fatal(err)
		}
		sourceTestGit(t, root, "--git-dir", common, "worktree", "add", "-b", "agent1", base, "origin/main")
		sourceTestGit(t, base, "worktree", "add", "-b", "writer", writer, "origin/main")
	} else {
		sourceTestGit(t, root, "clone", remote, base)
		if err := os.MkdirAll(filepath.Dir(writer), 0700); err != nil {
			t.Fatal(err)
		}
		sourceTestGit(t, root, "clone", remote, writer)
	}
	return CurrentSource{Worktree: writer, BaseWorktree: base, Home: home, Origin: remote, Index: 1}, primary, remote
}

func TestCurrentSourceAcquiresOnlyTrackingAndPreservesBothWriterForms(t *testing.T) {
	for _, variant := range []struct{ shared, registered bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		name := map[bool]string{false: "standalone", true: "shared"}[variant.shared]
		if variant.registered {
			name += "-registered-sibling"
		}
		t.Run(name, func(t *testing.T) {
			shared := variant.shared
			var s CurrentSource
			var primary string
			if variant.registered {
				s, primary, _ = registeredSourceFixture(t, shared)
			} else {
				s, primary, _ = sourceFixture(t, shared)
			}
			common := sourceTestGit(t, s.Worktree, "rev-parse", "--path-format=absolute", "--git-common-dir")
			gitDir := sourceTestGit(t, s.Worktree, "rev-parse", "--path-format=absolute", "--git-dir")
			file := filepath.Join(s.Worktree, "candidate")
			if err := os.WriteFile(file, []byte("original candidate\n"), 0751); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(file, 0751); err != nil {
				t.Fatal(err)
			}
			sourceTestGit(t, s.Worktree, "add", "candidate")
			if err := os.WriteFile(file, []byte("unaccepted candidate\n"), 0751); err != nil {
				t.Fatal(err)
			}
			index, err := os.ReadFile(filepath.Join(gitDir, "index"))
			if err != nil {
				t.Fatal(err)
			}
			beforeHead := sourceTestGit(t, s.Worktree, "rev-parse", "HEAD")
			beforeBase := sourceTestGit(t, s.BaseWorktree, "rev-parse", "HEAD")
			var aliasDefinition []byte
			if !shared {
				aliasDefinition, err = os.ReadFile(filepath.Join(common, "refs", "remotes", "origin", "HEAD"))
				if err != nil {
					t.Fatal(err)
				}
			}
			sourceTestGit(t, s.Worktree, "update-ref", "refs/remotes/origin/retained", beforeHead)
			sourceTestGit(t, s.Worktree, "config", "fetch.prune", "true")
			sourceTestGit(t, s.Worktree, "config", "fetch.pruneTags", "true")
			otherRefs := func() string {
				var kept []string
				for _, line := range strings.Split(sourceTestGit(t, s.Worktree, "for-each-ref", "--format=%(refname)|%(symref)|%(objectname)"), "\n") {
					fields := strings.Split(line, "|")
					if fields[0] == "refs/remotes/origin/main" {
						continue
					}
					// A symbolic alias retains its target; naturally resolving an
					// unchanged alias to origin/main observes the new commit.
					if len(fields) == 3 && fields[1] != "" {
						line = fields[0] + "|" + fields[1]
					}
					kept = append(kept, line)
				}
				return strings.Join(kept, "\n")
			}
			beforeRefs := otherRefs()
			if err := os.WriteFile(filepath.Join(common, "FETCH_HEAD"), []byte("preserve fetch head\n"), 0600); err != nil {
				t.Fatal(err)
			}
			sourceTestGit(t, primary, "commit", "--allow-empty", "-m", "advance")
			sourceTestGit(t, primary, "push", "origin", "main")
			want := sourceTestGit(t, primary, "rev-parse", "HEAD")
			planned, err := PrepareCurrentSource(s, false, "", true)
			if err != nil || planned.Acquired != "" || planned.Previous != beforeHead {
				t.Fatalf("plan %#v %v", planned, err)
			}
			result, err := PrepareCurrentSource(s, false, "", false)
			if err != nil || result.Previous != beforeHead || result.Acquired != want {
				t.Fatalf("prepare %#v %v", result, err)
			}
			if got := sourceTestGit(t, s.Worktree, "rev-parse", "origin/main"); got != want {
				t.Fatal(got)
			}
			if !shared {
				afterAlias, err := os.ReadFile(filepath.Join(common, "refs", "remotes", "origin", "HEAD"))
				if err != nil || string(afterAlias) != string(aliasDefinition) {
					t.Fatal("raw symbolic alias definition changed")
				}
				if got := sourceTestGit(t, s.Worktree, "rev-parse", "origin/HEAD"); got != want {
					t.Fatal("unchanged alias does not resolve to acquired main")
				}
			}
			if got := sourceTestGit(t, s.Worktree, "rev-parse", "HEAD"); got != beforeHead {
				t.Fatal("writer HEAD changed")
			}
			if got := sourceTestGit(t, s.BaseWorktree, "rev-parse", "HEAD"); got != beforeBase {
				t.Fatal("base HEAD changed")
			}
			if got := otherRefs(); got != beforeRefs {
				t.Fatalf("other refs changed\nbefore: %s\nafter: %s", beforeRefs, got)
			}
			after, _ := os.ReadFile(filepath.Join(gitDir, "index"))
			if string(after) != string(index) {
				t.Fatal("index changed")
			}
			bytes, _ := os.ReadFile(file)
			st, _ := os.Stat(file)
			if string(bytes) != "unaccepted candidate\n" || st.Mode().Perm() != 0751 {
				t.Fatal("candidate bytes/mode changed")
			}
			bytes, _ = os.ReadFile(filepath.Join(common, "FETCH_HEAD"))
			if string(bytes) != "preserve fetch head\n" {
				t.Fatal("FETCH_HEAD changed")
			}
		})
	}
}

func TestCurrentSourceRejectsRemoteRewindAndDivergence(t *testing.T) {
	for _, diverge := range []bool{false, true} {
		t.Run(map[bool]string{false: "rewind", true: "divergence"}[diverge], func(t *testing.T) {
			s, primary, _ := sourceFixture(t, false)
			initial := sourceTestGit(t, primary, "rev-parse", "HEAD")
			sourceTestGit(t, primary, "commit", "--allow-empty", "-m", "advance")
			sourceTestGit(t, primary, "push", "origin", "main")
			if _, err := PrepareCurrentSource(s, false, "", false); err != nil {
				t.Fatal(err)
			}
			previous := sourceTestGit(t, s.Worktree, "rev-parse", "origin/main")
			sourceTestGit(t, primary, "reset", "--hard", initial)
			if diverge {
				sourceTestGit(t, primary, "commit", "--allow-empty", "-m", "divergent")
			}
			sourceTestGit(t, primary, "push", "--force", "origin", "main")
			if _, err := PrepareCurrentSource(s, false, "", false); err == nil {
				t.Fatal("non-fast-forward accepted")
			}
			if got := sourceTestGit(t, s.Worktree, "rev-parse", "origin/main"); got != previous {
				t.Fatal("tracking ref rewound")
			}
		})
	}
}

func TestCurrentSourceCASPreservesConcurrentTrackingUpdate(t *testing.T) {
	s, primary, _ := sourceFixture(t, false)
	sourceTestGit(t, primary, "commit", "--allow-empty", "-m", "advance")
	sourceTestGit(t, primary, "push", "origin", "main")
	newer := sourceTestGit(t, s.Worktree, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit-tree", "HEAD^{tree}", "-p", "HEAD", "-m", "concurrent")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	wrapper := filepath.Join(t.TempDir(), "git")
	script := "#!/bin/sh\nfor arg do\n if [ \"$arg\" = update-ref ]; then " + q(realGit) + " -C " + q(s.Worktree) + " update-ref refs/remotes/origin/main " + newer + " || exit; fi\ndone\nexec " + q(realGit) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	restore := gitauthority.SetExecutableForTesting(wrapper)
	defer restore()
	if _, err := PrepareCurrentSource(s, false, "", false); err == nil {
		t.Fatal("concurrent tracking mutation overwritten")
	}
	if got := sourceTestGit(t, s.Worktree, "rev-parse", "origin/main"); got != newer {
		t.Fatal("concurrent update lost")
	}
}

func TestCurrentSourceIgnoresReplacementAncestry(t *testing.T) {
	s, primary, _ := sourceFixture(t, false)
	initial := sourceTestGit(t, primary, "rev-parse", "HEAD")
	sourceTestGit(t, primary, "commit", "--allow-empty", "-m", "accepted advance")
	sourceTestGit(t, primary, "push", "origin", "main")
	if _, err := PrepareCurrentSource(s, false, "", false); err != nil {
		t.Fatal(err)
	}
	previous := sourceTestGit(t, s.Worktree, "rev-parse", "origin/main")
	sourceTestGit(t, primary, "reset", "--hard", initial)
	sourceTestGit(t, primary, "commit", "--allow-empty", "-m", "divergent remote")
	sourceTestGit(t, primary, "push", "--force", "origin", "main")
	acquired := sourceTestGit(t, primary, "rev-parse", "HEAD")
	// Import only objects to install the hostile replacement fixture. Production
	// must still acquire/reject this actual divergent remote using real Git.
	sourceTestGit(t, s.Worktree, "fetch", "--no-write-fetch-head", "--refmap=", s.Origin, acquired)
	replacement := sourceTestGit(t, s.Worktree, "commit-tree", acquired+"^{tree}", "-p", previous, "-m", "replacement ancestry")
	sourceTestGit(t, s.Worktree, "replace", acquired, replacement)
	sourceTestGit(t, s.Worktree, "merge-base", "--is-ancestor", previous, acquired)
	if _, err := PrepareCurrentSource(s, false, "", false); err == nil {
		t.Fatal("replacement ancestry admitted divergence")
	}
	if got := sourceTestGit(t, s.Worktree, "rev-parse", "origin/main"); got != previous {
		t.Fatal("tracking changed")
	}
	if got := sourceTestGit(t, s.Worktree, "rev-parse", "refs/replace/"+acquired); got != replacement {
		t.Fatal("replacement ref altered")
	}
}

func TestCurrentSourceCASPreservesConcurrentSymbolicUpdate(t *testing.T) {
	s, primary, _ := sourceFixture(t, false)
	previous := sourceTestGit(t, s.Worktree, "rev-parse", "origin/main")
	sourceTestGit(t, primary, "commit", "--allow-empty", "-m", "advance")
	sourceTestGit(t, primary, "push", "origin", "main")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	wrapper := filepath.Join(t.TempDir(), "git")
	script := "#!/bin/sh\nfor arg do\n if [ \"$arg\" = update-ref ]; then " + q(realGit) + " -C " + q(s.Worktree) + " symbolic-ref refs/remotes/origin/main refs/heads/main || exit; fi\ndone\nexec " + q(realGit) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	restore := gitauthority.SetExecutableForTesting(wrapper)
	defer restore()
	result, err := PrepareCurrentSource(s, false, "", false)
	if err == nil || result.TrackingUpdate != "not-committed" {
		t.Fatalf("concurrent symbolic tracking accepted: %#v %v", result, err)
	}
	if got := sourceTestGit(t, s.Worktree, "symbolic-ref", "refs/remotes/origin/main"); got != "refs/heads/main" {
		t.Fatal("concurrent symbolic definition overwritten")
	}
	if got := sourceTestGit(t, s.Worktree, "rev-parse", "refs/heads/main"); got != previous {
		t.Fatal("symbolic target mutated")
	}
	common := sourceTestGit(t, s.Worktree, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if _, err := os.Lstat(filepath.Join(common, "refs", "remotes", "origin", "main.lock")); !os.IsNotExist(err) {
		t.Fatal("aborted Git transaction retained its lock")
	}
}

func TestCurrentSourcePreparedTransactionExcludesConcurrentWriter(t *testing.T) {
	s, primary, _ := sourceFixture(t, false)
	sourceTestGit(t, primary, "commit", "--allow-empty", "-m", "advance")
	sourceTestGit(t, primary, "push", "origin", "main")
	want := sourceTestGit(t, primary, "rev-parse", "HEAD")
	old := sourceTestGit(t, s.Worktree, "rev-parse", "HEAD")
	common := sourceTestGit(t, s.Worktree, "rev-parse", "--path-format=absolute", "--git-common-dir")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	root := t.TempDir()
	wrapper := filepath.Join(root, "git")
	receipt := filepath.Join(root, "contender-exit")
	script := "#!/bin/sh\nfor arg do\n if [ \"$arg\" = symbolic-ref ] && [ -f " + q(filepath.Join(common, "refs", "remotes", "origin", "main.lock")) + " ]; then\n " + q(realGit) + " -C " + q(s.Worktree) + " update-ref refs/remotes/origin/main " + old + " >/dev/null 2>&1\n printf '%s' \"$?\" > " + q(receipt) + "\n fi\ndone\nexec " + q(realGit) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	restore := gitauthority.SetExecutableForTesting(wrapper)
	defer restore()
	result, err := PrepareCurrentSource(s, false, "", false)
	if err != nil || result.TrackingUpdate != "committed" {
		t.Fatalf("prepare %#v %v", result, err)
	}
	bytes, err := os.ReadFile(receipt)
	if err != nil || string(bytes) == "0" {
		t.Fatalf("contender did not meet a held Git lock: %s %v", bytes, err)
	}
	if got := sourceTestGit(t, s.Worktree, "rev-parse", "origin/main"); got != want {
		t.Fatal("prepared transaction lost its update")
	}
}

func TestCurrentSourceRefusesUnsafeMetadataWithoutRefChanges(t *testing.T) {
	for _, kind := range []string{"origin", "graft", "hook", "symbolic", "geometry", "url-rewrite", "marker"} {
		t.Run(kind, func(t *testing.T) {
			s, _, _ := sourceFixture(t, kind == "marker")
			common := sourceTestGit(t, s.Worktree, "rev-parse", "--path-format=absolute", "--git-common-dir")
			previous := sourceTestGit(t, s.Worktree, "rev-parse", "origin/main")
			switch kind {
			case "origin":
				s.Origin += "/foreign"
			case "graft":
				if err := os.MkdirAll(filepath.Join(common, "info"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(common, "info", "grafts"), []byte(previous+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "hook":
				if err := os.MkdirAll(filepath.Join(common, "hooks"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(common, "hooks", "reference-transaction"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
					t.Fatal(err)
				}
			case "symbolic":
				sourceTestGit(t, s.Worktree, "symbolic-ref", "refs/remotes/origin/main", "refs/heads/main")
			case "geometry":
				s.Home = s.BaseWorktree
			case "url-rewrite":
				sourceTestGit(t, s.Worktree, "config", "url.https://foreign.invalid/.insteadOf", "https://other.invalid/")
			case "marker":
				if err := os.WriteFile(filepath.Join(common, "devkit-owned-common"), []byte("foreign"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := PrepareCurrentSource(s, false, "", false); err == nil {
				t.Fatal("unsafe source accepted")
			}
			if got := sourceTestGit(t, s.Worktree, "rev-parse", "origin/main"); got != previous {
				t.Fatal("tracking changed")
			}
		})
	}
}

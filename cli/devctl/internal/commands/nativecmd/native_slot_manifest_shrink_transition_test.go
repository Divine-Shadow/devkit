package nativecmd

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	wtx "devkit/cli/devctl/internal/worktrees"
)

type nativeSlotManifestShrinkFixture struct {
	opts     nativeplan.BuildOptions
	actual   nativeagent.Manifest
	expected nativeagent.Manifest
	path     string
	origin   string
}

func nativeSlotManifestShrinkResetOptions(fixture nativeSlotManifestShrinkFixture) wtx.NativeSlotResetOptions {
	origin := fixture.origin
	if origin == "" {
		origin = "file:///nonexistent/source-derived-origin.git"
	}
	return wtx.NativeSlotResetOptions{
		Project:      fixture.actual.Project,
		Repo:         fixture.actual.Repo,
		Origin:       origin,
		BranchPrefix: fixture.actual.BranchPrefix,
		Count:        fixture.actual.Count,
		WorktreeRoot: fixture.actual.HostWorktreeRoot,
		StateRoot:    fixture.actual.HostStateRoot,
	}
}

func newNativeSlotManifestShrinkFixture(t *testing.T, actualCount, expectedCount int) nativeSlotManifestShrinkFixture {
	t.Helper()
	previousCapture := captureNativeManifestShrinkSlotHistory
	captureNativeManifestShrinkSlotHistory = func(
		_ string,
		_ string,
		_ nativeSlotProcessIdentity,
		_ string,
		_ string,
		_ bool,
	) error {
		return nil
	}
	t.Cleanup(func() { captureNativeManifestShrinkSlotHistory = previousCapture })
	root, err := os.MkdirTemp("/tmp", "devkit-shrink-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	if err := os.MkdirAll(devkitRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	opts := nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{
			Root:                 devkitRoot,
			RuntimeAuthorityRoot: devkitRoot,
		},
		HostRoot:              devRoot,
		Project:               "dev-all",
		Repo:                  "ouroboros-ide",
		WorktreeRoot:          filepath.Join(devRoot, "agent-worktrees"),
		StateRoot:             filepath.Join(devRoot, ".devkit", "native-agents"),
		WorktreeContainerRoot: "/workspaces/dev/agent-worktrees",
		StateContainerRoot:    "/agent-state",
		BaseBranch:            "main",
		BranchPrefix:          "agent",
	}
	actual, err := nativeplan.BuildManifest(opts, actualCount)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := nativeplan.BuildManifest(opts, expectedCount)
	if err != nil {
		t.Fatal(err)
	}
	path := nativeagent.ManifestPath(expected.HostStateRoot, "dev-all")
	if err := nativeagent.WriteManifest(path, actual, false); err != nil {
		t.Fatal(err)
	}
	surplusStart := expectedCount
	if surplusStart > len(actual.Agents) {
		surplusStart = len(actual.Agents)
	}
	for _, spec := range actual.Agents[surplusStart:] {
		if err := os.MkdirAll(filepath.Dir(spec.HostWorktree), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return nativeSlotManifestShrinkFixture{opts: opts, actual: actual, expected: expected, path: path}
}

func readNativeSlotManifestShrinkFixture(t *testing.T, path string) nativeagent.Manifest {
	t.Helper()
	exists, manifest, err := readNativeSlotManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatalf("manifest %s is missing", path)
	}
	return manifest
}

func TestNativeSlotManifestShrinkReaderRejectsSymlinkNonRegularUnknownAndTrailingJSON(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
		realPath := fixture.path + ".real"
		if err := os.Rename(fixture.path, realPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(realPath, fixture.path); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readNativeSlotManifest(fixture.path); err == nil || !strings.Contains(err.Error(), "regular non-symlink") {
			t.Fatalf("symlink manifest rejection = %v", err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
		if err := os.Remove(fixture.path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(fixture.path, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readNativeSlotManifest(fixture.path); err == nil || !strings.Contains(err.Error(), "regular non-symlink") {
			t.Fatalf("directory manifest rejection = %v", err)
		}
	})

	t.Run("unknown-field", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
		data, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		document["undeclared_authority"] = true
		data, err = json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fixture.path, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readNativeSlotManifest(fixture.path); err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("unknown-field manifest rejection = %v", err)
		}
	})

	t.Run("trailing-json-value", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
		file, err := os.OpenFile(fixture.path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString("{}\n"); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readNativeSlotManifest(fixture.path); err == nil || !strings.Contains(err.Error(), "unexpected trailing JSON value") {
			t.Fatalf("trailing JSON manifest rejection = %v", err)
		}
	})
}

func TestNativeSlotManifestShrinkTransitionAcceptsOnlyAbsentCanonicalSuffix(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
	retainedSentinels := make(map[string]string, len(fixture.expected.Agents))
	for _, retained := range fixture.expected.Agents {
		sentinel := filepath.Join(filepath.Dir(retained.HostWorktree), fmt.Sprintf("lane%d-sentinel", retained.ID.Index))
		content := fmt.Sprintf("preserve lane %d\n", retained.ID.Index)
		if err := os.MkdirAll(filepath.Dir(sentinel), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sentinel, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		retainedSentinels[sentinel] = content
	}

	existed, err := reconcileNativeSlotManifestShrink(fixture.path, fixture.expected, fixture.opts, nativeSlotManifestShrinkResetOptions(fixture), false)
	if err != nil {
		t.Fatal(err)
	}
	if !existed {
		t.Fatal("canonical surplus manifest was reported missing")
	}
	if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, fixture.expected) {
		t.Fatalf("installed manifest = %#v, want %#v", got, fixture.expected)
	}
	for sentinel, want := range retainedSentinels {
		if got, err := os.ReadFile(sentinel); err != nil || string(got) != want {
			t.Fatalf("retained sentinel %s changed: data=%q err=%v", sentinel, got, err)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(fixture.actual.Agents[3].HostWorktree))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("absent surplus parent was mutated: %v", entries)
	}
}

func TestNativeSlotManifestShrinkTransitionRejectsGrowthAndGeometryDrift(t *testing.T) {
	t.Run("growth", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 2, 3)
		canonicalActual, err := nativeplan.BuildManifest(fixture.opts, fixture.actual.Count)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := classifyNativeSlotManifestShrink(fixture.actual, fixture.expected, canonicalActual); err == nil || !strings.Contains(err.Error(), "shrink-only") {
			t.Fatalf("growth classification = %v", err)
		}
	})

	t.Run("noncanonical-surplus", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
		fixture.actual.Agents[3].StateRoot += "-drift"
		if _, err := classifyNativeSlotManifestShrink(fixture.actual, fixture.expected, readNativeSlotManifestShrinkFixture(t, fixture.path)); err == nil || !strings.Contains(err.Error(), "not the exact source-derived") {
			t.Fatalf("noncanonical surplus classification = %v", err)
		}
	})

	t.Run("common-prefix-drift", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
		fixture.actual.BrokerEndpoint = "unix:///tmp/different.sock"
		if _, err := classifyNativeSlotManifestShrink(fixture.actual, fixture.expected, fixture.actual); err == nil || !strings.Contains(err.Error(), "exact source-derived prefix") {
			t.Fatalf("common prefix drift classification = %v", err)
		}
	})

	t.Run("noncontiguous-surplus", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
		fixture.actual.Agents[3].ID.Index = 6
		if _, err := classifyNativeSlotManifestShrink(fixture.actual, fixture.expected, fixture.actual); err == nil || !strings.Contains(err.Error(), "not contiguous") {
			t.Fatalf("noncontiguous suffix classification = %v", err)
		}
	})
}

func TestNativeSlotManifestShrinkTransitionRejectsUnsafeSurplusFilesystemResidue(t *testing.T) {
	tests := []struct {
		name   string
		create func(t *testing.T, fixture nativeSlotManifestShrinkFixture, spec nativeagent.Spec)
		want   string
	}{
		{
			name: "worktree-or-git-dirt",
			create: func(t *testing.T, _ nativeSlotManifestShrinkFixture, spec nativeagent.Spec) {
				t.Helper()
				if err := os.MkdirAll(spec.HostWorktree, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(spec.HostWorktree, "unpublished.txt"), []byte("unique\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: "without its package-owned common Git repository",
		},
		{
			name: "transaction-lock",
			create: func(t *testing.T, _ nativeSlotManifestShrinkFixture, spec nativeagent.Spec) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(filepath.Dir(spec.HostWorktree), ".lane4-transaction.lock"), []byte("held\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: "transaction, lock, or opaque residue",
		},
		{
			name: "state-transaction-residue",
			create: func(t *testing.T, fixture nativeSlotManifestShrinkFixture, spec nativeagent.Spec) {
				t.Helper()
				residue := filepath.Join(
					filepath.Dir(spec.StateRoot),
					".devkit-reset-"+fixture.actual.Project+"-agent4-stale",
				)
				if err := os.MkdirAll(residue, 0o700); err != nil {
					t.Fatal(err)
				}
			},
			want: "state transaction or lock residue",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
			spec := fixture.actual.Agents[3]
			test.create(t, fixture, spec)
			if _, err := reconcileNativeSlotManifestShrink(fixture.path, fixture.expected, fixture.opts, nativeSlotManifestShrinkResetOptions(fixture), false); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("residue rejection = %v, want %q", err, test.want)
			}
			if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, fixture.actual) {
				t.Fatalf("rejected transition changed manifest: %#v", got)
			}
		})
	}
}

func TestNativeSlotManifestShrinkTransitionRejectsSocketAndProcessRaces(t *testing.T) {
	t.Run("socket", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
		spec := fixture.actual.Agents[3]
		socketPath := filepath.Join(spec.HostHome, ".codex", "a4-app.sock")
		if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
			t.Fatal(err)
		}
		listener, err := net.Listen("unix", socketPath)
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		if _, err := reconcileNativeSlotManifestShrink(fixture.path, fixture.expected, fixture.opts, nativeSlotManifestShrinkResetOptions(fixture), false); err == nil || !strings.Contains(err.Error(), "app-server socket") {
			t.Fatalf("socket rejection = %v", err)
		}
	})

	t.Run("already-active-process", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
		spec := fixture.actual.Agents[3]
		err := requireNativeSlotManifestShrinkAbsentWithPlanner(
			fixture.actual,
			spec,
			"",
			func(identity nativeSlotProcessIdentity, dryRun bool) (*nativeSlotProcessPlan, error) {
				return &nativeSlotProcessPlan{identity: identity, pids: []int{4104}, dryRun: dryRun}, nil
			},
		)
		if err == nil || !strings.Contains(err.Error(), "active processes") {
			t.Fatalf("active process rejection = %v", err)
		}
	})

	t.Run("process-appears-during-proof", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
		spec := fixture.actual.Agents[3]
		calls := 0
		err := requireNativeSlotManifestShrinkAbsentWithPlanner(
			fixture.actual,
			spec,
			"",
			func(identity nativeSlotProcessIdentity, dryRun bool) (*nativeSlotProcessPlan, error) {
				calls++
				pids := []int(nil)
				if calls == 2 {
					pids = []int{4204}
				}
				return &nativeSlotProcessPlan{identity: identity, pids: pids, dryRun: dryRun}, nil
			},
		)
		if err == nil || !strings.Contains(err.Error(), "active processes") {
			t.Fatalf("process race rejection = %v", err)
		}
		if calls != 2 {
			t.Fatalf("process inspections = %d, want 2", calls)
		}
	})
}

func TestNativeSlotManifestShrinkTransitionRejectsGitMetadataLockAndAheadCommit(t *testing.T) {
	t.Run("registered-worktree-metadata", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
		spec := fixture.actual.Agents[3]
		commonDir, err := nativeSlotManifestShrinkCommonDir(fixture.actual, fixture.actual.Agents[3])
		if err != nil {
			t.Fatal(err)
		}
		metadata := filepath.Join(commonDir, "worktrees", "agent4-stale")
		if err := os.MkdirAll(metadata, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(metadata, "gitdir"), []byte(filepath.Join(spec.HostWorktree, ".git")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := reconcileNativeSlotManifestShrink(fixture.path, fixture.expected, fixture.opts, nativeSlotManifestShrinkResetOptions(fixture), false); err == nil || !strings.Contains(err.Error(), "registered Git worktree metadata without its worktree") {
			t.Fatalf("Git metadata rejection = %v", err)
		}
	})

	t.Run("branch-transaction-lock", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
		commonDir, err := nativeSlotManifestShrinkCommonDir(fixture.actual, fixture.actual.Agents[3])
		if err != nil {
			t.Fatal(err)
		}
		lockPath := filepath.Join(commonDir, "refs", "heads", "agent4.lock")
		if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lockPath, []byte("held\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := reconcileNativeSlotManifestShrink(fixture.path, fixture.expected, fixture.opts, nativeSlotManifestShrinkResetOptions(fixture), false); err == nil || !strings.Contains(err.Error(), "Git transaction lock residue") {
			t.Fatalf("Git lock rejection = %v", err)
		}
	})

	t.Run("ahead-or-unique-commit", func(t *testing.T) {
		git, err := exec.LookPath("git")
		if err != nil {
			t.Skip("git is supplied by the Nix test closure")
		}
		fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
		commonDir, err := nativeSlotManifestShrinkCommonDir(fixture.actual, fixture.actual.Agents[3])
		if err != nil {
			t.Fatal(err)
		}
		seed := filepath.Join(t.TempDir(), "seed")
		runNativeSlotManifestShrinkGit(t, git, "init", "--bare", commonDir)
		runNativeSlotManifestShrinkGit(t, git, "init", seed)
		runNativeSlotManifestShrinkGit(t, git, "-C", seed, "checkout", "-b", "main")
		runNativeSlotManifestShrinkGit(t, git, "-C", seed, "config", "user.email", "shrink-test@example.invalid")
		runNativeSlotManifestShrinkGit(t, git, "-C", seed, "config", "user.name", "shrink-test")
		tracked := filepath.Join(seed, "tracked.txt")
		if err := os.WriteFile(tracked, []byte("base\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runNativeSlotManifestShrinkGit(t, git, "-C", seed, "add", "tracked.txt")
		runNativeSlotManifestShrinkGit(t, git, "-C", seed, "commit", "-m", "base")
		runNativeSlotManifestShrinkGit(t, git, "-C", seed, "remote", "add", "origin", commonDir)
		runNativeSlotManifestShrinkGit(t, git, "-C", seed, "push", "origin", "main")
		runNativeSlotManifestShrinkGit(t, git, "--git-dir", commonDir, "update-ref", "refs/remotes/origin/main", "refs/heads/main")
		runNativeSlotManifestShrinkGit(t, git, "--git-dir", commonDir, "update-ref", "refs/heads/agent4", "refs/remotes/origin/main")
		if err := requireNativeSlotManifestShrinkGitQuiescent(fixture.actual, fixture.actual.Agents[3]); err != nil {
			t.Fatalf("base-contained surplus branch was not quiescent: %v", err)
		}
		if err := os.WriteFile(tracked, []byte("base\nunique lane4\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runNativeSlotManifestShrinkGit(t, git, "-C", seed, "add", "tracked.txt")
		runNativeSlotManifestShrinkGit(t, git, "-C", seed, "commit", "-m", "unique lane4")
		runNativeSlotManifestShrinkGit(t, git, "-C", seed, "push", "origin", "HEAD:refs/heads/agent4")

		if _, err := reconcileNativeSlotManifestShrink(fixture.path, fixture.expected, fixture.opts, nativeSlotManifestShrinkResetOptions(fixture), false); err == nil || !strings.Contains(err.Error(), "not contained in refs/remotes/origin/main") {
			t.Fatalf("unique commit rejection = %v", err)
		}
	})
}

func runNativeSlotManifestShrinkGit(t *testing.T, executable string, args ...string) {
	t.Helper()
	command := exec.Command(executable, args...)
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

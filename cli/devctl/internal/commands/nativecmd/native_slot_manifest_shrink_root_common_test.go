package nativecmd

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/gitauthority"
	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	wtx "devkit/cli/devctl/internal/worktrees"
)

var historicalRootCommonGeneratedSetupLayerFiles = []string{
	".codex/config.toml",
	"scripts/devops/governance-control-plane",
	"scripts/devops/governance-mcp-stdio-forward",
}

var historicalRootCommonDisposableGeneratedResidueRoots = []string{
	".bsp",
	"logs",
	"project/target",
	"target",
}

type historicalRootCommonSnapshotEntry struct {
	Mode fs.FileMode
	Data string
}

func snapshotHistoricalRootCommonTree(t *testing.T, root string) map[string]historicalRootCommonSnapshotEntry {
	t.Helper()
	result := map[string]historicalRootCommonSnapshotEntry{}
	if err := filepath.WalkDir(root, func(candidate string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, candidate)
		if err != nil {
			return err
		}
		info, err := os.Lstat(candidate)
		if err != nil {
			return err
		}
		snapshot := historicalRootCommonSnapshotEntry{Mode: info.Mode()}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			snapshot.Data, err = os.Readlink(candidate)
		case info.Mode().IsRegular():
			var data []byte
			data, err = os.ReadFile(candidate)
			snapshot.Data = string(data)
		}
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = snapshot
		return nil
	}); err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return result
}

func historicalRootCommonResetOptions(fixture nativeSlotManifestShrinkFixture) wtx.NativeSlotResetOptions {
	options := nativeSlotManifestShrinkResetOptions(fixture)
	options.GeneratedSetupLayerFiles = append([]string(nil), historicalRootCommonGeneratedSetupLayerFiles...)
	options.DisposableGeneratedResidueRoots = append([]string(nil), historicalRootCommonDisposableGeneratedResidueRoots...)
	return options
}

// prepareHistoricalRootCommonNativeSlotManifestShrinkGit reproduces the
// pre-lane-isolation layout: agent1 is the ordinary source checkout and the
// remaining agents are linked worktrees registered below that checkout's
// .git/worktrees directory.
func prepareHistoricalRootCommonNativeSlotManifestShrinkGit(
	t *testing.T,
	fixture *nativeSlotManifestShrinkFixture,
) (string, string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is supplied by the Nix test closure")
	}
	origin := filepath.Join(t.TempDir(), "origin.git")
	seed := filepath.Join(t.TempDir(), "seed")
	runNativeSlotManifestShrinkGit(t, git, "init", "--bare", "--initial-branch=main", origin)
	runNativeSlotManifestShrinkGit(t, git, "init", "--initial-branch=main", seed)
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "config", "user.email", "root-common-shrink-test@example.invalid")
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "config", "user.name", "root-common-shrink-test")
	for _, directory := range []string{filepath.Join(seed, ".codex"), filepath.Join(seed, "scripts", "devops")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		"tracked.txt":        "base\n",
		".gitignore":         "/.bsp/\n/logs/\n/project/target/\n/target/\n",
		".codex/config.toml": "# source setup layer\n",
		"scripts/devops/governance-control-plane":     "#!/bin/sh\n# source setup layer\n",
		"scripts/devops/governance-mcp-stdio-forward": "#!/bin/sh\n# source setup layer\n",
	} {
		if err := os.WriteFile(filepath.Join(seed, filepath.FromSlash(path)), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "add", "tracked.txt", ".gitignore", ".codex/config.toml", "scripts/devops/governance-control-plane", "scripts/devops/governance-mcp-stdio-forward")
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "commit", "-m", "base")
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "remote", "add", "origin", origin)
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "push", "-u", "origin", "main")

	rootCheckout := filepath.Join(filepath.Dir(fixture.actual.HostWorktreeRoot), fixture.actual.Repo)
	if err := os.MkdirAll(filepath.Dir(rootCheckout), 0o700); err != nil {
		t.Fatal(err)
	}
	runNativeSlotManifestShrinkGit(t, git, "clone", origin, rootCheckout)
	runNativeSlotManifestShrinkGit(t, git, "-C", rootCheckout, "config", "worktree.useRelativePaths", "true")
	for _, spec := range fixture.actual.Agents {
		if err := os.MkdirAll(filepath.Dir(spec.HostWorktree), 0o700); err != nil {
			t.Fatal(err)
		}
		branch := fixture.actual.BranchPrefix + fmt.Sprint(spec.ID.Index)
		runNativeSlotManifestShrinkGit(
			t,
			git,
			"-C", rootCheckout,
			"worktree", "add", spec.HostWorktree,
			"-b", branch,
			"origin/"+fixture.actual.BaseBranch,
		)
	}
	fixture.origin = origin
	return git, filepath.Join(rootCheckout, ".git")
}

func TestNativeSlotManifestShrinkRetiresExactHistoricalRootCommonSurplusWithoutTouchingSharedRoot(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
	git, rootCommon := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
	surplus := fixture.actual.Agents[2]
	materializeNativeSlotDisposableState(t, surplus)
	for _, root := range historicalRootCommonDisposableGeneratedResidueRoots {
		ignoredDir := filepath.Join(surplus.HostWorktree, filepath.FromSlash(root))
		if err := os.MkdirAll(ignoredDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(ignoredDir, "generated.log"), []byte("disposable generated residue\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	metadataOutput, err := exec.Command(git, "-C", surplus.HostWorktree, "rev-parse", "--absolute-git-dir").CombinedOutput()
	if err != nil {
		t.Fatalf("read historical metadata: %v\n%s", err, metadataOutput)
	}
	metadata := strings.TrimSpace(string(metadataOutput))
	if filepath.Dir(filepath.Dir(metadata)) != rootCommon {
		t.Fatalf("fixture metadata %s is not registered below root common %s", metadata, rootCommon)
	}

	for _, relativePath := range historicalRootCommonGeneratedSetupLayerFiles {
		candidate := filepath.Join(surplus.HostWorktree, filepath.FromSlash(relativePath))
		if err := os.WriteFile(candidate, []byte("source-projected setup layer\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rootCheckout := filepath.Dir(rootCommon)
	rootBefore := snapshotHistoricalRootCommonTree(t, rootCheckout)
	metadataBefore := snapshotHistoricalRootCommonTree(t, metadata)
	refPath := filepath.Join(rootCommon, "refs", "heads", "agent3")
	refBefore, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatal(err)
	}
	historyBeforeRemoval := false
	previousCapture := captureNativeManifestShrinkSlotHistory
	captureNativeManifestShrinkSlotHistory = func(
		_ string,
		_ string,
		identity nativeSlotProcessIdentity,
		_ string,
		_ string,
		_ bool,
	) error {
		if identity.index != surplus.ID.Index {
			return fmt.Errorf("unexpected history identity agent%d", identity.index)
		}
		if _, err := os.Lstat(surplus.HostWorktree); err != nil {
			return fmt.Errorf("history capture followed retirement: %w", err)
		}
		historyBeforeRemoval = true
		return nil
	}
	t.Cleanup(func() { captureNativeManifestShrinkSlotHistory = previousCapture })

	_, err = reconcileNativeSlotManifestShrink(
		fixture.path,
		fixture.expected,
		fixture.opts,
		historicalRootCommonResetOptions(fixture),
		false,
	)
	if err != nil {
		t.Fatalf("retire exact historical root-common surplus: %v", err)
	}
	if !historyBeforeRemoval {
		t.Fatal("cold history was not captured before retirement")
	}
	if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, fixture.expected) {
		t.Fatal("historical root-common retirement did not install expected manifest")
	}
	for _, retiredPath := range []string{surplus.HostWorktree, surplus.HostHome, surplus.StateRoot} {
		if _, statErr := os.Lstat(retiredPath); !os.IsNotExist(statErr) {
			t.Fatalf("retired path remains %s: %v", retiredPath, statErr)
		}
	}
	if got := snapshotHistoricalRootCommonTree(t, rootCheckout); !reflect.DeepEqual(got, rootBefore) {
		t.Fatal("historical root checkout or common Git repository changed")
	}
	if got := snapshotHistoricalRootCommonTree(t, metadata); !reflect.DeepEqual(got, metadataBefore) {
		t.Fatal("historical surplus metadata changed or was pruned")
	}
	if got, err := os.ReadFile(refPath); err != nil || !reflect.DeepEqual(got, refBefore) {
		t.Fatalf("historical surplus branch changed or was pruned: data=%q err=%v", got, err)
	}
}

func TestNativeSlotManifestShrinkHistoricalRootRefusesUnsafeCustodyBeforeMutation(t *testing.T) {
	t.Run("unexpected-fourth-tracked-file", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
		_, rootCommon := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
		surplus := fixture.actual.Agents[2]
		materializeNativeSlotDisposableState(t, surplus)
		if err := os.WriteFile(filepath.Join(surplus.HostWorktree, "tracked.txt"), []byte("unexpected tracked custody\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		rootBefore := snapshotHistoricalRootCommonTree(t, filepath.Dir(rootCommon))
		if _, err := reconcileNativeSlotManifestShrink(
			fixture.path,
			fixture.expected,
			fixture.opts,
			historicalRootCommonResetOptions(fixture),
			false,
		); err == nil || !strings.Contains(err.Error(), "unexpected tracked dirt at tracked.txt") {
			t.Fatalf("unexpected tracked custody rejection = %v", err)
		}
		if got := snapshotHistoricalRootCommonTree(t, filepath.Dir(rootCommon)); !reflect.DeepEqual(got, rootBefore) {
			t.Fatal("tracked-custody refusal changed shared root")
		}
		if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, fixture.actual) {
			t.Fatal("tracked-custody refusal changed manifest")
		}
		for _, candidate := range []string{surplus.HostWorktree, surplus.HostHome, surplus.StateRoot} {
			if _, err := os.Lstat(candidate); err != nil {
				t.Fatalf("tracked-custody refusal changed %s: %v", candidate, err)
			}
		}
	})

	t.Run("non-ignored-untracked", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
		prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
		surplus := fixture.actual.Agents[2]
		materializeNativeSlotDisposableState(t, surplus)
		if err := os.WriteFile(filepath.Join(surplus.HostWorktree, "untracked-custody"), []byte("custody\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := reconcileNativeSlotManifestShrink(
			fixture.path,
			fixture.expected,
			fixture.opts,
			historicalRootCommonResetOptions(fixture),
			false,
		); err == nil || !strings.Contains(err.Error(), "non-ignored untracked custody") {
			t.Fatalf("untracked custody rejection = %v", err)
		}
	})

	t.Run("submodule-index", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
		git, _ := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
		surplus := fixture.actual.Agents[2]
		materializeNativeSlotDisposableState(t, surplus)
		headOutput, err := exec.Command(git, "-C", surplus.HostWorktree, "rev-parse", "HEAD").CombinedOutput()
		if err != nil {
			t.Fatalf("read fixture HEAD: %v\n%s", err, headOutput)
		}
		runNativeSlotManifestShrinkGit(
			t,
			git,
			"-C", surplus.HostWorktree,
			"update-index", "--add", "--cacheinfo", "160000,"+strings.TrimSpace(string(headOutput))+",vendor/submodule",
		)
		if _, err := reconcileNativeSlotManifestShrink(
			fixture.path,
			fixture.expected,
			fixture.opts,
			historicalRootCommonResetOptions(fixture),
			false,
		); err == nil || !strings.Contains(err.Error(), "submodule custody") {
			t.Fatalf("submodule custody rejection = %v", err)
		}
	})

	t.Run("info-exclude-cannot-expand-disposable-custody", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
		_, rootCommon := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
		surplus := fixture.actual.Agents[2]
		materializeNativeSlotDisposableState(t, surplus)
		if err := os.WriteFile(filepath.Join(rootCommon, "info", "exclude"), []byte("/ambient-hidden\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(surplus.HostWorktree, "ambient-hidden"), []byte("custody\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := reconcileNativeSlotManifestShrink(
			fixture.path,
			fixture.expected,
			fixture.opts,
			historicalRootCommonResetOptions(fixture),
			false,
		); err == nil || !strings.Contains(err.Error(), "ignored custody outside source-declared") {
			t.Fatalf("info/exclude custody rejection = %v", err)
		}
	})

	t.Run("ambient-global-ignore-cannot-expand-disposable-custody", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
		prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
		surplus := fixture.actual.Agents[2]
		materializeNativeSlotDisposableState(t, surplus)
		globalIgnore := filepath.Join(t.TempDir(), "global-ignore")
		if err := os.WriteFile(globalIgnore, []byte("ambient-global-hidden\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		globalConfig := filepath.Join(t.TempDir(), "global-config")
		if err := os.WriteFile(globalConfig, []byte("[core]\n\texcludesFile = "+globalIgnore+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
		if err := os.WriteFile(filepath.Join(surplus.HostWorktree, "ambient-global-hidden"), []byte("custody\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := reconcileNativeSlotManifestShrink(
			fixture.path,
			fixture.expected,
			fixture.opts,
			historicalRootCommonResetOptions(fixture),
			false,
		); err == nil || !strings.Contains(err.Error(), "non-ignored untracked custody") {
			t.Fatalf("ambient global ignore custody rejection = %v", err)
		}
	})

	t.Run("ignored-declared-root-must-be-directory", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
		_, rootCommon := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
		surplus := fixture.actual.Agents[2]
		materializeNativeSlotDisposableState(t, surplus)
		if err := os.WriteFile(filepath.Join(rootCommon, "info", "exclude"), []byte("/logs\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(surplus.HostWorktree, "logs"), []byte("not a generated tree\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := reconcileNativeSlotManifestShrink(
			fixture.path,
			fixture.expected,
			fixture.opts,
			historicalRootCommonResetOptions(fixture),
			false,
		); err == nil || !strings.Contains(err.Error(), "must be a real non-symlink directory") {
			t.Fatalf("ignored regular-file root rejection = %v", err)
		}
	})

	t.Run("ignored-declared-root-must-not-be-symlink", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
		_, rootCommon := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
		surplus := fixture.actual.Agents[2]
		materializeNativeSlotDisposableState(t, surplus)
		if err := os.WriteFile(filepath.Join(rootCommon, "info", "exclude"), []byte("/logs\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), filepath.Join(surplus.HostWorktree, "logs")); err != nil {
			t.Fatal(err)
		}
		if _, err := reconcileNativeSlotManifestShrink(
			fixture.path,
			fixture.expected,
			fixture.opts,
			historicalRootCommonResetOptions(fixture),
			false,
		); err == nil || !strings.Contains(err.Error(), "must be a real non-symlink directory") {
			t.Fatalf("ignored symlink root rejection = %v", err)
		}
	})

	for _, test := range []struct {
		name  string
		apply func(*testing.T, string, nativeagent.Spec)
		want  string
	}{
		{
			name: "assume-unchanged",
			apply: func(t *testing.T, git string, surplus nativeagent.Spec) {
				runNativeSlotManifestShrinkGit(t, git, "-C", surplus.HostWorktree, "update-index", "--assume-unchanged", "tracked.txt")
			},
			want: "assume-unchanged, skip-worktree, sparse, or unexpected index flags",
		},
		{
			name: "skip-worktree",
			apply: func(t *testing.T, git string, surplus nativeagent.Spec) {
				runNativeSlotManifestShrinkGit(t, git, "-C", surplus.HostWorktree, "update-index", "--skip-worktree", "tracked.txt")
			},
			want: "assume-unchanged, skip-worktree, sparse, or unexpected index flags",
		},
		{
			name: "sparse-checkout-config",
			apply: func(t *testing.T, git string, surplus nativeagent.Spec) {
				runNativeSlotManifestShrinkGit(t, git, "-C", surplus.HostWorktree, "config", "core.sparseCheckout", "true")
			},
			want: "sparse-checkout enabled",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
			git, _ := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
			surplus := fixture.actual.Agents[2]
			materializeNativeSlotDisposableState(t, surplus)
			test.apply(t, git, surplus)
			if _, err := reconcileNativeSlotManifestShrink(
				fixture.path,
				fixture.expected,
				fixture.opts,
				historicalRootCommonResetOptions(fixture),
				false,
			); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("%s rejection = %v", test.name, err)
			}
		})
	}

	t.Run("non-stage-zero-index", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
		git, _ := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
		surplus := fixture.actual.Agents[2]
		materializeNativeSlotDisposableState(t, surplus)
		blobOutput, err := exec.Command(git, "-C", surplus.HostWorktree, "rev-parse", "HEAD:.codex/config.toml").CombinedOutput()
		if err != nil {
			t.Fatalf("resolve setup blob: %v\n%s", err, blobOutput)
		}
		blob := strings.TrimSpace(string(blobOutput))
		runNativeSlotManifestShrinkGit(t, git, "-C", surplus.HostWorktree, "update-index", "--force-remove", ".codex/config.toml")
		command := exec.Command(git, "-C", surplus.HostWorktree, "update-index", "--index-info")
		command.Stdin = strings.NewReader(
			"100644 " + blob + " 1\t.codex/config.toml\n" +
				"100644 " + blob + " 2\t.codex/config.toml\n",
		)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("install unmerged setup index: %v\n%s", err, output)
		}
		if _, err := reconcileNativeSlotManifestShrink(
			fixture.path,
			fixture.expected,
			fixture.opts,
			historicalRootCommonResetOptions(fixture),
			false,
		); err == nil || !strings.Contains(err.Error(), "unmerged non-stage-0 index entry") {
			t.Fatalf("unmerged index rejection = %v", err)
		}
	})
}

func TestNativeSlotManifestShrinkHistoricalRootValidatesExactSetupDeclarations(t *testing.T) {
	for _, test := range []struct {
		name  string
		paths []string
		want  string
	}{
		{name: "duplicate", paths: []string{".codex/config.toml", ".codex/config.toml"}, want: "declared more than once"},
		{name: "parent-escape", paths: []string{"../config.toml"}, want: "safe repo-relative"},
		{name: "git-metadata", paths: []string{".git/config"}, want: "cannot name Git metadata"},
		{name: "directory-spelling", paths: []string{"scripts/devops/../devops/governance-control-plane"}, want: "safe repo-relative"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := nativeSlotManifestShrinkGeneratedSetupLayerFiles(test.paths); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("declaration validation = %v, want %q", err, test.want)
			}
		})
	}
	allowed, err := nativeSlotManifestShrinkGeneratedSetupLayerFiles(historicalRootCommonGeneratedSetupLayerFiles)
	if err != nil {
		t.Fatal(err)
	}
	if len(allowed) != 3 {
		t.Fatalf("exact setup declaration count = %d, want 3", len(allowed))
	}
	for _, test := range []struct {
		name  string
		paths []string
		want  string
	}{
		{name: "duplicate", paths: []string{"target", "target"}, want: "declared more than once"},
		{name: "parent-escape", paths: []string{"../target"}, want: "safe repo-relative"},
		{name: "git-metadata", paths: []string{".git/objects"}, want: "cannot name Git metadata"},
		{name: "overlap", paths: []string{"project", "project/target"}, want: "overlap"},
	} {
		t.Run("generated-residue-"+test.name, func(t *testing.T) {
			if _, err := nativeSlotManifestShrinkDisposableGeneratedResidueRoots(test.paths); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("generated-residue declaration validation = %v, want %q", err, test.want)
			}
		})
	}
	residueRoots, err := nativeSlotManifestShrinkDisposableGeneratedResidueRoots(historicalRootCommonDisposableGeneratedResidueRoots)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(residueRoots, historicalRootCommonDisposableGeneratedResidueRoots) {
		t.Fatalf("exact generated-residue roots = %#v", residueRoots)
	}
}

func TestNativeSlotManifestShrinkHistoricalRootValidatesOriginAheadRemoteAndSymlinkDrift(t *testing.T) {
	t.Run("wrong-origin", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
		git, rootCommon := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
		surplus := fixture.actual.Agents[2]
		materializeNativeSlotDisposableState(t, surplus)
		runNativeSlotManifestShrinkGit(t, git, "--git-dir", rootCommon, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "foreign.git"))
		if _, err := reconcileNativeSlotManifestShrink(
			fixture.path,
			fixture.expected,
			fixture.opts,
			historicalRootCommonResetOptions(fixture),
			false,
		); err == nil || !strings.Contains(err.Error(), "does not match declared origin") {
			t.Fatalf("wrong origin rejection = %v", err)
		}
	})

	t.Run("ahead", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
		git, _ := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
		surplus := fixture.actual.Agents[2]
		materializeNativeSlotDisposableState(t, surplus)
		runNativeSlotManifestShrinkGit(t, git, "-C", surplus.HostWorktree, "config", "user.email", "ahead@example.invalid")
		runNativeSlotManifestShrinkGit(t, git, "-C", surplus.HostWorktree, "config", "user.name", "ahead")
		if err := os.WriteFile(filepath.Join(surplus.HostWorktree, "tracked.txt"), []byte("ahead\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runNativeSlotManifestShrinkGit(t, git, "-C", surplus.HostWorktree, "add", "tracked.txt")
		runNativeSlotManifestShrinkGit(t, git, "-C", surplus.HostWorktree, "commit", "-m", "ahead")
		if _, err := reconcileNativeSlotManifestShrink(
			fixture.path,
			fixture.expected,
			fixture.opts,
			historicalRootCommonResetOptions(fixture),
			false,
		); err == nil || !strings.Contains(err.Error(), "not contained in current refs/heads/main") {
			t.Fatalf("ahead rejection = %v", err)
		}
	})

	t.Run("current-remote-advance-proved-without-root-write", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
		git, rootCommon := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
		surplus := fixture.actual.Agents[2]
		materializeNativeSlotDisposableState(t, surplus)
		publisher := filepath.Join(t.TempDir(), "publisher")
		runNativeSlotManifestShrinkGit(t, git, "clone", fixture.origin, publisher)
		runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "config", "user.email", "remote@example.invalid")
		runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "config", "user.name", "remote")
		if err := os.WriteFile(filepath.Join(publisher, "remote-only.txt"), []byte("remote\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "add", "remote-only.txt")
		runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "commit", "-m", "remote advance")
		runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "push", "origin", "main")
		rootCheckout := filepath.Dir(rootCommon)
		rootBefore := snapshotHistoricalRootCommonTree(t, rootCheckout)
		if _, err := reconcileNativeSlotManifestShrink(
			fixture.path,
			fixture.expected,
			fixture.opts,
			historicalRootCommonResetOptions(fixture),
			false,
		); err != nil {
			t.Fatalf("current remote isolated proof = %v", err)
		}
		if got := snapshotHistoricalRootCommonTree(t, rootCheckout); !reflect.DeepEqual(got, rootBefore) {
			t.Fatal("current remote isolated proof changed root checkout or common Git state")
		}
		for _, candidate := range []string{surplus.HostWorktree, surplus.HostHome, surplus.StateRoot} {
			if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
				t.Fatalf("current remote isolated proof did not retire %s: %v", candidate, err)
			}
		}
	})

	t.Run("current-remote-force-rewrite-refused-without-root-write", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
		git, rootCommon := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
		surplus := fixture.actual.Agents[2]
		materializeNativeSlotDisposableState(t, surplus)
		publisher := filepath.Join(t.TempDir(), "publisher")
		runNativeSlotManifestShrinkGit(t, git, "clone", fixture.origin, publisher)
		runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "config", "user.email", "rewrite@example.invalid")
		runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "config", "user.name", "rewrite")
		runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "checkout", "--orphan", "rewritten-main")
		runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "rm", "-rf", ".")
		if err := os.WriteFile(filepath.Join(publisher, "rewritten.txt"), []byte("unrelated history\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "add", "rewritten.txt")
		runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "commit", "-m", "rewrite remote main")
		runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "push", "--force", "origin", "HEAD:main")
		rootCheckout := filepath.Dir(rootCommon)
		rootBefore := snapshotHistoricalRootCommonTree(t, rootCheckout)
		if _, err := reconcileNativeSlotManifestShrink(
			fixture.path,
			fixture.expected,
			fixture.opts,
			historicalRootCommonResetOptions(fixture),
			false,
		); err == nil || !strings.Contains(err.Error(), "not contained in current refs/heads/main") {
			t.Fatalf("force-rewritten current remote rejection = %v", err)
		}
		if got := snapshotHistoricalRootCommonTree(t, rootCheckout); !reflect.DeepEqual(got, rootBefore) {
			t.Fatal("force-rewritten current remote proof changed root checkout or common Git state")
		}
		if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, fixture.actual) {
			t.Fatal("force-rewritten current remote proof changed manifest")
		}
	})

	t.Run("symlinked-worktree", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
		prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
		surplus := fixture.actual.Agents[2]
		materializeNativeSlotDisposableState(t, surplus)
		realWorktree := surplus.HostWorktree + ".real"
		if err := os.Rename(surplus.HostWorktree, realWorktree); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(realWorktree, surplus.HostWorktree); err != nil {
			t.Fatal(err)
		}
		if _, err := reconcileNativeSlotManifestShrink(
			fixture.path,
			fixture.expected,
			fixture.opts,
			historicalRootCommonResetOptions(fixture),
			false,
		); err == nil || !strings.Contains(err.Error(), "not the exact source-derived all-slot geometry") {
			t.Fatalf("symlinked worktree rejection = %v", err)
		}
	})
}

func TestNativeSlotManifestShrinkHistoricalRootUsesOneOperationRemoteProofForWholeSuffix(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 5, 3)
	git, rootCommon := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
	for _, surplus := range fixture.actual.Agents[3:] {
		materializeNativeSlotDisposableState(t, surplus)
	}
	logPath := filepath.Join(t.TempDir(), "git-fetches.log")
	wrapper := filepath.Join(t.TempDir(), "git-wrapper")
	wrapperSource := "#!/bin/sh\n" +
		"for argument in \"$@\"; do\n" +
		"  if [ \"$argument\" = fetch ]; then printf 'fetch\\n' >> " + logPath + "; fi\n" +
		"done\n" +
		"exec " + git + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(wrapperSource), 0o700); err != nil {
		t.Fatal(err)
	}
	restoreGit := gitauthority.SetExecutableForTesting(wrapper)
	t.Cleanup(restoreGit)
	rootCheckout := filepath.Dir(rootCommon)
	rootBefore := snapshotHistoricalRootCommonTree(t, rootCheckout)
	if _, err := reconcileNativeSlotManifestShrink(
		fixture.path,
		fixture.expected,
		fixture.opts,
		historicalRootCommonResetOptions(fixture),
		false,
	); err != nil {
		t.Fatalf("multi-suffix historical retirement: %v", err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(logData), "fetch\n"); got != 1 {
		t.Fatalf("operation current-remote fetch count = %d, want 1; log=%q", got, logData)
	}
	if got := snapshotHistoricalRootCommonTree(t, rootCheckout); !reflect.DeepEqual(got, rootBefore) {
		t.Fatal("multi-suffix operation proof changed root checkout or common Git state")
	}
	for _, surplus := range fixture.actual.Agents[3:] {
		for _, candidate := range []string{surplus.HostWorktree, surplus.HostHome, surplus.StateRoot} {
			if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
				t.Fatalf("multi-suffix retirement residue remains %s: %v", candidate, err)
			}
		}
	}
}

func TestNativeSlotManifestShrinkHistoricalRootUsesFreshProofForAncestryAndSetupTree(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
	git, rootCommon := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
	surplus := fixture.actual.Agents[2]
	materializeNativeSlotDisposableState(t, surplus)

	captureGit := func(args ...string) string {
		t.Helper()
		command := exec.Command(git, args...)
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
		return string(output)
	}

	staleBase := strings.TrimSpace(captureGit(
		"--git-dir", rootCommon,
		"rev-parse", "--verify", "refs/remotes/origin/main^{commit}",
	))
	publisher := filepath.Join(t.TempDir(), "publisher")
	runNativeSlotManifestShrinkGit(t, git, "clone", fixture.origin, publisher)
	runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "config", "user.email", "fresh-proof@example.invalid")
	runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "config", "user.name", "fresh-proof")
	setupPath := "fresh/setup-projection"
	if err := os.MkdirAll(filepath.Join(publisher, "fresh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publisher, filepath.FromSlash(setupPath)), []byte("current remote setup\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "add", setupPath)
	runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "commit", "-m", "add current setup projection")
	runNativeSlotManifestShrinkGit(t, git, "-C", publisher, "push", "origin", "main")
	currentBase := strings.TrimSpace(captureGit("-C", publisher, "rev-parse", "HEAD^{commit}"))
	if currentBase == staleBase {
		t.Fatal("remote advance did not produce a new current base")
	}

	// Make the fresh commit available to the linked branch without advancing
	// the protected checkout's stale origin/main tracking ref.
	runNativeSlotManifestShrinkGit(
		t,
		git,
		"--git-dir", rootCommon,
		"fetch", "--no-tags", "--no-write-fetch-head",
		fixture.origin, currentBase,
	)
	runNativeSlotManifestShrinkGit(t, git, "-C", surplus.HostWorktree, "reset", "--hard", currentBase)
	if got := strings.TrimSpace(captureGit(
		"--git-dir", rootCommon,
		"rev-parse", "--verify", "refs/remotes/origin/main^{commit}",
	)); got != staleBase {
		t.Fatalf("protected origin/main advanced during fixture setup: got %s want %s", got, staleBase)
	}
	if got := captureGit(
		"--git-dir", rootCommon,
		"ls-tree", "-z", "--name-only", staleBase, "--", setupPath,
	); got != "" {
		t.Fatalf("setup path unexpectedly exists in stale protected tree: %q", got)
	}
	if got := captureGit(
		"--git-dir", rootCommon,
		"ls-tree", "-z", "--name-only", currentBase, "--", setupPath,
	); got != setupPath+"\x00" {
		t.Fatalf("setup path is absent from fresh current tree: %q", got)
	}
	if err := os.WriteFile(
		filepath.Join(surplus.HostWorktree, filepath.FromSlash(setupPath)),
		[]byte("source-projected setup layer\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(t.TempDir(), "git-fetches.log")
	wrapper := filepath.Join(t.TempDir(), "git-wrapper")
	wrapperSource := "#!/bin/sh\n" +
		"for argument in \"$@\"; do\n" +
		"  if [ \"$argument\" = fetch ]; then printf 'fetch\\n' >> " + logPath + "; fi\n" +
		"done\n" +
		"exec " + git + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(wrapperSource), 0o700); err != nil {
		t.Fatal(err)
	}
	restoreGit := gitauthority.SetExecutableForTesting(wrapper)
	t.Cleanup(restoreGit)

	options := historicalRootCommonResetOptions(fixture)
	options.GeneratedSetupLayerFiles = append(options.GeneratedSetupLayerFiles, setupPath)
	rootCheckout := filepath.Dir(rootCommon)
	rootBefore := snapshotHistoricalRootCommonTree(t, rootCheckout)
	if _, err := reconcileNativeSlotManifestShrink(
		fixture.path,
		fixture.expected,
		fixture.opts,
		options,
		false,
	); err != nil {
		t.Fatalf("historical retirement against fresh proof authority: %v", err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(logData), "fetch\n"); got != 1 {
		t.Fatalf("fresh authority proof fetch count = %d, want 1; log=%q", got, logData)
	}
	if got := snapshotHistoricalRootCommonTree(t, rootCheckout); !reflect.DeepEqual(got, rootBefore) {
		t.Fatal("fresh authority proof changed root checkout or common Git state")
	}
	if got := strings.TrimSpace(captureGit(
		"--git-dir", rootCommon,
		"rev-parse", "--verify", "refs/remotes/origin/main^{commit}",
	)); got != staleBase {
		t.Fatalf("historical retirement changed protected origin/main: got %s want %s", got, staleBase)
	}
	if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, fixture.expected) {
		t.Fatal("fresh authority proof did not install expected manifest")
	}
	for _, candidate := range []string{surplus.HostWorktree, surplus.HostHome, surplus.StateRoot} {
		if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
			t.Fatalf("fresh authority retirement residue remains %s: %v", candidate, err)
		}
	}
}

func TestNativeSlotManifestShrinkHistoricalRootRejectsLateSuffixCustodyBeforeAnyHistory(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 5, 3)
	_, rootCommon := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
	for _, surplus := range fixture.actual.Agents[3:] {
		materializeNativeSlotDisposableState(t, surplus)
	}
	late := fixture.actual.Agents[4]
	if err := os.WriteFile(filepath.Join(late.HostWorktree, "unexpected-custody"), []byte("late custody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	historyCalls := 0
	previousCapture := captureNativeManifestShrinkSlotHistory
	captureNativeManifestShrinkSlotHistory = func(string, string, nativeSlotProcessIdentity, string, string, bool) error {
		historyCalls++
		return nil
	}
	t.Cleanup(func() { captureNativeManifestShrinkSlotHistory = previousCapture })
	rootCheckout := filepath.Dir(rootCommon)
	rootBefore := snapshotHistoricalRootCommonTree(t, rootCheckout)
	if _, err := reconcileNativeSlotManifestShrink(
		fixture.path,
		fixture.expected,
		fixture.opts,
		historicalRootCommonResetOptions(fixture),
		false,
	); err == nil || !strings.Contains(err.Error(), "non-ignored untracked custody") {
		t.Fatalf("late suffix custody rejection = %v", err)
	}
	if historyCalls != 0 {
		t.Fatalf("late suffix refusal captured %d histories before complete preflight", historyCalls)
	}
	if got := snapshotHistoricalRootCommonTree(t, rootCheckout); !reflect.DeepEqual(got, rootBefore) {
		t.Fatal("late suffix refusal changed root checkout or common Git state")
	}
	if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, fixture.actual) {
		t.Fatal("late suffix refusal changed manifest")
	}
}

func TestNativeSlotManifestShrinkHistoricalRootScratchIgnoresAmbientRedirectionAndRejectsOverlap(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
	_, rootCommon := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
	surplus := fixture.actual.Agents[2]
	materializeNativeSlotDisposableState(t, surplus)
	rootCheckout := filepath.Dir(rootCommon)
	hostileTmp := filepath.Join(rootCheckout, "ambient-tmp")
	if err := os.Mkdir(hostileTmp, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", hostileTmp)
	t.Setenv("GIT_OBJECT_DIRECTORY", filepath.Join(rootCommon, "objects"))
	t.Setenv("GIT_ALTERNATE_OBJECT_DIRECTORIES", filepath.Join(rootCommon, "objects"))
	rootBefore := snapshotHistoricalRootCommonTree(t, rootCheckout)
	config := nativeSlotManifestShrinkRemoteProofConfig{
		ScratchParent:  filepath.Dir(fixture.actual.HostStateRoot),
		ProtectedRoots: historicalRootCommonResetOptions(fixture).ProtectedRoots,
	}
	if _, err := reconcileNativeSlotManifestShrink(
		fixture.path,
		fixture.expected,
		fixture.opts,
		historicalRootCommonResetOptions(fixture),
		false,
		config,
	); err != nil {
		t.Fatalf("ambient-redirection-isolated historical retirement: %v", err)
	}
	if got := snapshotHistoricalRootCommonTree(t, rootCheckout); !reflect.DeepEqual(got, rootBefore) {
		t.Fatal("ambient TMP/Git redirection changed historical root")
	}
	entries, err := os.ReadDir(hostileTmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("ambient TMP received proof residue: %#v", entries)
	}
	for _, key := range []string{"TMPDIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES"} {
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}

	overlapFixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
	_, overlapCommon := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &overlapFixture)
	overlapSurplus := overlapFixture.actual.Agents[2]
	materializeNativeSlotDisposableState(t, overlapSurplus)
	overlapRoot := filepath.Dir(overlapCommon)
	overlapBefore := snapshotHistoricalRootCommonTree(t, overlapRoot)
	if _, err := reconcileNativeSlotManifestShrink(
		overlapFixture.path,
		overlapFixture.expected,
		overlapFixture.opts,
		historicalRootCommonResetOptions(overlapFixture),
		false,
		nativeSlotManifestShrinkRemoteProofConfig{ScratchParent: overlapRoot},
	); err == nil || !strings.Contains(err.Error(), "scratch parent") || !strings.Contains(err.Error(), "protected/shared boundary") {
		t.Fatalf("overlapping proof scratch rejection = %v", err)
	}
	if got := snapshotHistoricalRootCommonTree(t, overlapRoot); !reflect.DeepEqual(got, overlapBefore) {
		t.Fatal("overlapping scratch refusal changed historical root")
	}
}

func TestNativeSlotManifestShrinkRemoteProofCleanupPreservesChangedIdentityAndStillStopsTransport(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
	operation, err := newNativeSlotManifestShrinkRemoteProofOperation([]nativeSlotManifestShrinkRemoteProofConfig{{
		ScratchParent: t.TempDir(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := operation.ensureScratch(fixture.actual); err != nil {
		t.Fatal(err)
	}
	original := operation.scratchDir + ".original"
	if err := os.Rename(operation.scratchDir, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(operation.scratchDir, 0o700); err != nil {
		t.Fatal(err)
	}
	transportStopped := false
	operation.transportCleanup = func() error {
		transportStopped = true
		return nil
	}
	operation.transportStarted = true
	err = operation.Close()
	if err == nil || !strings.Contains(err.Error(), "cleanup path identity changed") {
		t.Fatalf("changed scratch identity cleanup = %v", err)
	}
	if !transportStopped {
		t.Fatal("transport cleanup was skipped after scratch identity refusal")
	}
	if _, err := os.Lstat(operation.scratchDir); err != nil {
		t.Fatalf("changed scratch identity was recursively removed: %v", err)
	}
}

func TestNativeSlotManifestShrinkRemoteProofCanonicalizesProtectedBoundaryAliases(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
	scratchParent := t.TempDir()
	alias := filepath.Join(t.TempDir(), "protected-alias")
	if err := os.Symlink(scratchParent, alias); err != nil {
		t.Fatal(err)
	}
	operation, err := newNativeSlotManifestShrinkRemoteProofOperation([]nativeSlotManifestShrinkRemoteProofConfig{{
		ScratchParent:  scratchParent,
		ProtectedRoots: []string{alias},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := operation.ensureScratch(fixture.actual); err == nil || !strings.Contains(err.Error(), "inside protected/shared boundary") {
		t.Fatalf("canonical protected-boundary alias rejection = %v", err)
	}
	entries, err := os.ReadDir(scratchParent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("canonical protected-boundary refusal created scratch: %#v", entries)
	}
}

func TestNativeSlotManifestShrinkHistoricalRootResumesTypedPostCASCleanup(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
	_, rootCommon := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
	surplus := fixture.actual.Agents[2]
	materializeNativeSlotDisposableState(t, surplus)
	rootCheckout := filepath.Dir(rootCommon)
	rootBefore := snapshotHistoricalRootCommonTree(t, rootCheckout)
	options := historicalRootCommonResetOptions(fixture)
	plan, err := wtx.PlanNativeSlotReset(nativeManifestShrinkResetOptions(fixture.actual, surplus, options, false))
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := plan.Stage()
	if err != nil {
		t.Fatal(err)
	}
	journal := nativeManifestShrinkJournal(fixture.path, fixture.actual, fixture.expected, []nativeagent.Spec{surplus})
	journal.Status = "committed"
	journal.SlotTransactions = []wtx.NativeResetTransactionState{transaction.State()}
	transactionPath := nativeManifestShrinkTransactionPath(fixture.path)
	if err := writeNativeManifestShrinkTransaction(transactionPath, journal); err != nil {
		t.Fatal(err)
	}
	if err := nativeagent.WriteManifest(fixture.path, fixture.expected, false); err != nil {
		t.Fatal(err)
	}

	if _, err := reconcileNativeSlotManifestShrink(
		fixture.path,
		fixture.expected,
		fixture.opts,
		options,
		false,
	); err != nil {
		t.Fatalf("historical root post-CAS recovery: %v", err)
	}
	for _, candidate := range []string{surplus.HostWorktree, surplus.HostHome, surplus.StateRoot, transactionPath} {
		if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
			t.Fatalf("historical root recovery residue remains %s: %v", candidate, err)
		}
	}
	if got := snapshotHistoricalRootCommonTree(t, rootCheckout); !reflect.DeepEqual(got, rootBefore) {
		t.Fatal("historical root post-CAS recovery changed shared root Git state")
	}
}

func TestNativeSlotManifestShrinkHistoricalRootRecoversPreparedCrashPrefix(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 3, 2)
	_, rootCommon := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
	surplus := fixture.actual.Agents[2]
	materializeNativeSlotDisposableState(t, surplus)
	rootCheckout := filepath.Dir(rootCommon)
	rootBefore := snapshotHistoricalRootCommonTree(t, rootCheckout)
	options := historicalRootCommonResetOptions(fixture)
	plan, err := wtx.PlanNativeSlotReset(nativeManifestShrinkResetOptions(fixture.actual, surplus, options, false))
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := plan.PrepareTransaction()
	if err != nil {
		t.Fatal(err)
	}
	state := transaction.State()
	journal := nativeManifestShrinkJournal(fixture.path, fixture.actual, fixture.expected, []nativeagent.Spec{surplus})
	journal.SlotTransactions = []wtx.NativeResetTransactionState{state}
	transactionPath := nativeManifestShrinkTransactionPath(fixture.path)
	if err := writeNativeManifestShrinkTransaction(transactionPath, journal); err != nil {
		t.Fatal(err)
	}
	for _, quarantine := range state.Quarantines {
		if err := os.Mkdir(quarantine.Path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if len(state.Staged) == 0 {
		t.Fatal("prepared historical retirement has no staged paths")
	}
	if err := os.Rename(state.Staged[0].Source, state.Staged[0].Target); err != nil {
		t.Fatal(err)
	}

	if _, err := reconcileNativeSlotManifestShrink(
		fixture.path,
		fixture.expected,
		fixture.opts,
		options,
		false,
	); err != nil {
		t.Fatalf("historical root prepared recovery: %v", err)
	}
	if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, fixture.expected) {
		t.Fatal("prepared recovery did not complete expected shrink")
	}
	for _, candidate := range []string{surplus.HostWorktree, surplus.HostHome, surplus.StateRoot, transactionPath} {
		if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
			t.Fatalf("historical root prepared recovery residue remains %s: %v", candidate, err)
		}
	}
	if got := snapshotHistoricalRootCommonTree(t, rootCheckout); !reflect.DeepEqual(got, rootBefore) {
		t.Fatal("historical root prepared recovery changed shared root Git state")
	}
}

func TestNativeSlotManifestShrinkHistoricalRootRecoversPartialMultiSuffixCrash(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 5, 3)
	_, rootCommon := prepareHistoricalRootCommonNativeSlotManifestShrinkGit(t, &fixture)
	surplus := append([]nativeagent.Spec(nil), fixture.actual.Agents[3:]...)
	for _, spec := range surplus {
		materializeNativeSlotDisposableState(t, spec)
	}
	rootCheckout := filepath.Dir(rootCommon)
	rootBefore := snapshotHistoricalRootCommonTree(t, rootCheckout)
	options := historicalRootCommonResetOptions(fixture)
	journal := nativeManifestShrinkJournal(fixture.path, fixture.actual, fixture.expected, surplus)
	for _, spec := range surplus {
		plan, err := wtx.PlanNativeSlotReset(nativeManifestShrinkResetOptions(fixture.actual, spec, options, false))
		if err != nil {
			t.Fatal(err)
		}
		transaction, err := plan.PrepareTransaction()
		if err != nil {
			t.Fatal(err)
		}
		journal.SlotTransactions = append(journal.SlotTransactions, transaction.State())
	}
	transactionPath := nativeManifestShrinkTransactionPath(fixture.path)
	if err := writeNativeManifestShrinkTransaction(transactionPath, journal); err != nil {
		t.Fatal(err)
	}
	for stateIndex, state := range journal.SlotTransactions {
		for _, quarantine := range state.Quarantines {
			if err := os.Mkdir(quarantine.Path, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		moveCount := stateIndex + 1
		if moveCount > len(state.Staged) {
			moveCount = len(state.Staged)
		}
		for _, staged := range state.Staged[:moveCount] {
			if err := os.Rename(staged.Source, staged.Target); err != nil {
				t.Fatal(err)
			}
		}
	}

	if _, err := reconcileNativeSlotManifestShrink(
		fixture.path,
		fixture.expected,
		fixture.opts,
		options,
		false,
	); err != nil {
		t.Fatalf("historical root partial multi-suffix recovery: %v", err)
	}
	if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, fixture.expected) {
		t.Fatal("partial multi-suffix recovery did not complete expected shrink")
	}
	for _, spec := range surplus {
		for _, candidate := range []string{spec.HostWorktree, spec.HostHome, spec.StateRoot} {
			if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
				t.Fatalf("partial multi-suffix recovery residue remains %s: %v", candidate, err)
			}
		}
	}
	if _, err := os.Lstat(transactionPath); !os.IsNotExist(err) {
		t.Fatalf("partial multi-suffix transaction receipt remains %s: %v", transactionPath, err)
	}
	if got := snapshotHistoricalRootCommonTree(t, rootCheckout); !reflect.DeepEqual(got, rootBefore) {
		t.Fatal("partial multi-suffix recovery changed shared root Git state")
	}
}

package nativecmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	wtx "devkit/cli/devctl/internal/worktrees"
)

func prepareNativeSlotManifestShrinkGit(
	t *testing.T,
	fixture *nativeSlotManifestShrinkFixture,
) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is supplied by the Nix test closure")
	}
	origin := filepath.Join(t.TempDir(), "origin.git")
	seed := filepath.Join(t.TempDir(), "seed")
	runNativeSlotManifestShrinkGit(t, git, "init", "--bare", "--initial-branch=main", origin)
	runNativeSlotManifestShrinkGit(t, git, "init", "--initial-branch=main", seed)
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "config", "user.email", "shrink-test@example.invalid")
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "config", "user.name", "shrink-test")
	if err := os.WriteFile(filepath.Join(seed, "tracked.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "add", "tracked.txt")
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "commit", "-m", "base")
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "remote", "add", "origin", origin)
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "push", "-u", "origin", "main")
	if err := wtx.SetupNative(wtx.NativeOptions{
		DevkitRoot:   fixture.opts.Paths.Root,
		Repo:         fixture.actual.Repo,
		Origin:       origin,
		Count:        fixture.actual.Count,
		BaseBranch:   fixture.actual.BaseBranch,
		BranchPrefix: fixture.actual.BranchPrefix,
		WorktreeRoot: fixture.actual.HostWorktreeRoot,
	}); err != nil {
		t.Fatalf("prepare package-owned native Git worktrees: %v", err)
	}
	fixture.origin = origin
	return git
}

func prepareLegacyNativeSlotManifestShrinkGit(
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
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "config", "user.email", "legacy-shrink-test@example.invalid")
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "config", "user.name", "legacy-shrink-test")
	if err := os.WriteFile(filepath.Join(seed, "tracked.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "add", "tracked.txt")
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "commit", "-m", "base")
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "remote", "add", "origin", origin)
	runNativeSlotManifestShrinkGit(t, git, "-C", seed, "push", "-u", "origin", "main")

	legacyCommonDir, err := nativeSlotManifestShrinkLegacyCommonDir(fixture.actual)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(legacyCommonDir), 0o700); err != nil {
		t.Fatal(err)
	}
	runNativeSlotManifestShrinkGit(t, git, "clone", "--bare", origin, legacyCommonDir)
	runNativeSlotManifestShrinkGit(t, git, "--git-dir", legacyCommonDir, "config", "worktree.useRelativePaths", "true")
	runNativeSlotManifestShrinkGit(t, git, "--git-dir", legacyCommonDir, "fetch", "origin", "+refs/heads/*:refs/remotes/origin/*")
	marker := fmt.Sprintf(
		"schema=devkit/native-owned-common-repository/v1\nrepository=%s\norigin=%s\n",
		fixture.actual.Repo,
		origin,
	)
	if err := os.WriteFile(filepath.Join(legacyCommonDir, "devkit-owned-common"), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, spec := range fixture.actual.Agents {
		if err := os.MkdirAll(filepath.Dir(spec.HostWorktree), 0o700); err != nil {
			t.Fatal(err)
		}
		branch := fixture.actual.BranchPrefix + fmt.Sprint(spec.ID.Index)
		runNativeSlotManifestShrinkGit(
			t,
			git,
			"--git-dir", legacyCommonDir,
			"worktree", "add", spec.HostWorktree,
			"-b", branch,
			"origin/"+fixture.actual.BaseBranch,
		)
	}
	fixture.origin = origin
	return git, legacyCommonDir
}

func materializeNativeSlotDisposableState(t *testing.T, spec nativeagent.Spec) {
	t.Helper()
	for _, path := range []string{spec.HostHome, spec.StateRoot} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "retirement-sentinel"), []byte(fmt.Sprintf("lane-%d\n", spec.ID.Index)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNativeSlotManifestShrinkRetiresCleanRealGitSuffixAtomically(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 5, 3)
	prepareNativeSlotManifestShrinkGit(t, &fixture)
	for _, spec := range fixture.actual.Agents[3:] {
		materializeNativeSlotDisposableState(t, spec)
	}
	retained := map[string]string{}
	for _, spec := range fixture.expected.Agents {
		path := filepath.Join(spec.HostWorktree, fmt.Sprintf("retained-%d", spec.ID.Index))
		value := fmt.Sprintf("retained lane %d\n", spec.ID.Index)
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		retained[path] = value
	}
	previousCapture := captureNativeManifestShrinkSlotHistory
	var captured []int
	captureNativeManifestShrinkSlotHistory = func(
		project, resetKind string,
		identity nativeSlotProcessIdentity,
		stateRoot, workspaceRoot string,
		dryRun bool,
	) error {
		if project != "dev-all" || resetKind != "manifest-shrink-retirement" || stateRoot != fixture.actual.HostStateRoot || workspaceRoot != "" || dryRun {
			return fmt.Errorf("unexpected history capture identity")
		}
		if _, err := os.Stat(filepath.Join(identity.hostHome, "retirement-sentinel")); err != nil {
			return fmt.Errorf("history capture ran after home removal: %w", err)
		}
		captured = append(captured, identity.index)
		return nil
	}
	t.Cleanup(func() { captureNativeManifestShrinkSlotHistory = previousCapture })

	if _, err := reconcileNativeSlotManifestShrink(
		fixture.path,
		fixture.expected,
		fixture.opts,
		nativeSlotManifestShrinkResetOptions(fixture),
		false,
	); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(captured, []int{4, 5}) {
		t.Fatalf("history captures = %v, want [4 5]", captured)
	}
	if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, fixture.expected) {
		t.Fatalf("installed manifest = %#v, want expected", got)
	}
	for _, spec := range fixture.actual.Agents[3:] {
		for _, path := range []string{spec.HostWorktree, spec.HostHome, spec.StateRoot} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("retired path still exists %s: %v", path, err)
			}
		}
	}
	for path, want := range retained {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("retained lane changed at %s: data=%q err=%v", path, got, err)
		}
	}
	if _, err := os.Lstat(nativeManifestShrinkTransactionPath(fixture.path)); !os.IsNotExist(err) {
		t.Fatalf("completed transaction receipt remains: %v", err)
	}

	// A second application is an idempotent no-op.
	if _, err := reconcileNativeSlotManifestShrink(
		fixture.path,
		fixture.expected,
		fixture.opts,
		nativeSlotManifestShrinkResetOptions(fixture),
		false,
	); err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	if !reflect.DeepEqual(captured, []int{4, 5}) {
		t.Fatalf("idempotent replay captured history again: %v", captured)
	}
}

func TestNativeSlotManifestShrinkRetiresLegacySurplusWithoutTouchingRetainedCustody(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
	git, legacyCommonDir := prepareLegacyNativeSlotManifestShrinkGit(t, &fixture)
	actualOrigin := "git@github.com:Divine-Shadow/ouroboros-ide.git"
	declaredOrigin := "ssh://git@ssh.github.com:443/Divine-Shadow/ouroboros-ide.git"
	runNativeSlotManifestShrinkGit(t, git, "--git-dir", legacyCommonDir, "remote", "set-url", "origin", actualOrigin)
	marker := fmt.Sprintf(
		"schema=devkit/native-owned-common-repository/v1\nrepository=%s\norigin=%s\n",
		fixture.actual.Repo,
		actualOrigin,
	)
	if err := os.WriteFile(filepath.Join(legacyCommonDir, "devkit-owned-common"), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
	surplus := fixture.actual.Agents[3]
	materializeNativeSlotDisposableState(t, surplus)

	retained := fixture.actual.Agents[1]
	retainedMetadataOutput, result := exec.Command(
		git,
		"-C", retained.HostWorktree,
		"rev-parse", "--absolute-git-dir",
	).CombinedOutput()
	if result != nil {
		t.Fatalf("read retained legacy metadata: %v\n%s", result, retainedMetadataOutput)
	}
	retainedMetadata := strings.TrimSpace(string(retainedMetadataOutput))
	retainedFiles := []string{
		filepath.Join(retainedMetadata, "gitdir"),
		filepath.Join(retainedMetadata, "commondir"),
		filepath.Join(retainedMetadata, "HEAD"),
		filepath.Join(retainedMetadata, "index"),
		filepath.Join(legacyCommonDir, "devkit-owned-common"),
		filepath.Join(legacyCommonDir, "refs", "heads", "agent2"),
	}
	retainedBefore := make(map[string][]byte, len(retainedFiles))
	for _, path := range retainedFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read retained legacy custody %s: %v", path, err)
		}
		retainedBefore[path] = append([]byte(nil), data...)
	}
	retainedSentinel := filepath.Join(retained.HostWorktree, "active-agent2-sentinel")
	if err := os.WriteFile(retainedSentinel, []byte("retained agent2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	retainedLock := filepath.Join(legacyCommonDir, "refs", "heads", "agent2.lock")
	if err := os.WriteFile(retainedLock, []byte("active sibling lock domain\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	retainedBefore[retainedLock] = []byte("active sibling lock domain\n")

	surplusMetadataOutput, result := exec.Command(
		git,
		"-C", surplus.HostWorktree,
		"rev-parse", "--absolute-git-dir",
	).CombinedOutput()
	if result != nil {
		t.Fatalf("read surplus legacy metadata: %v\n%s", result, surplusMetadataOutput)
	}
	surplusMetadata := strings.TrimSpace(string(surplusMetadataOutput))
	surplusReverseBefore, err := os.ReadFile(filepath.Join(surplusMetadata, "gitdir"))
	if err != nil {
		t.Fatal(err)
	}
	surplusRefBefore, err := os.ReadFile(filepath.Join(legacyCommonDir, "refs", "heads", "agent4"))
	if err != nil {
		t.Fatal(err)
	}

	options := nativeSlotManifestShrinkResetOptions(fixture)
	options.Origin = declaredOrigin
	if _, err := reconcileNativeSlotManifestShrink(
		fixture.path,
		fixture.expected,
		fixture.opts,
		options,
		false,
	); err != nil {
		t.Fatalf("retire exact legacy surplus: %v", err)
	}
	if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, fixture.expected) {
		t.Fatalf("installed manifest = %#v, want expected", got)
	}
	for _, path := range []string{surplus.HostWorktree, surplus.HostHome, surplus.StateRoot} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("legacy surplus path still exists %s: %v", path, err)
		}
	}
	for path, want := range retainedBefore {
		got, err := os.ReadFile(path)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("legacy retained custody changed at %s: data=%q err=%v", path, got, err)
		}
	}
	if got, err := os.ReadFile(retainedSentinel); err != nil || string(got) != "retained agent2\n" {
		t.Fatalf("retained legacy worktree changed: data=%q err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(surplusMetadata, "gitdir")); err != nil || !reflect.DeepEqual(got, surplusReverseBefore) {
		t.Fatalf("legacy shared metadata was pruned or changed: data=%q err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(legacyCommonDir, "refs", "heads", "agent4")); err != nil || !reflect.DeepEqual(got, surplusRefBefore) {
		t.Fatalf("legacy shared branch was pruned or changed: data=%q err=%v", got, err)
	}
}

func TestNativeSlotManifestShrinkRejectsLegacyLookalikeBeforeMutation(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
	_, legacyCommonDir := prepareLegacyNativeSlotManifestShrinkGit(t, &fixture)
	surplus := fixture.actual.Agents[3]
	materializeNativeSlotDisposableState(t, surplus)
	if err := os.WriteFile(
		filepath.Join(legacyCommonDir, "devkit-owned-common"),
		[]byte("schema=devkit/native-owned-common-repository/v1\nrepository=foreign\norigin="+fixture.origin+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := reconcileNativeSlotManifestShrink(
		fixture.path,
		fixture.expected,
		fixture.opts,
		nativeSlotManifestShrinkResetOptions(fixture),
		false,
	); err == nil || !strings.Contains(err.Error(), "identity does not match") {
		t.Fatalf("legacy lookalike rejection = %v", err)
	}
	if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, fixture.actual) {
		t.Fatalf("legacy lookalike rejection changed manifest")
	}
	for _, path := range []string{surplus.HostWorktree, surplus.HostHome, surplus.StateRoot} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("legacy lookalike rejection changed %s: %v", path, err)
		}
	}
}

func TestNativeSlotManifestShrinkRejectsOversizeLegacyMarkerBeforeMutation(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
	_, legacyCommonDir := prepareLegacyNativeSlotManifestShrinkGit(t, &fixture)
	surplus := fixture.actual.Agents[3]
	materializeNativeSlotDisposableState(t, surplus)
	markerPath := filepath.Join(legacyCommonDir, "devkit-owned-common")
	if err := os.WriteFile(
		markerPath,
		[]byte(strings.Repeat("x", nativeSlotManifestShrinkPointerFileLimit+1)),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := reconcileNativeSlotManifestShrink(
		fixture.path,
		fixture.expected,
		fixture.opts,
		nativeSlotManifestShrinkResetOptions(fixture),
		false,
	); err == nil || !strings.Contains(err.Error(), "exceeds the bounded metadata-view file limit") {
		t.Fatalf("oversize legacy marker rejection = %v", err)
	}
	if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, fixture.actual) {
		t.Fatal("oversize legacy marker rejection changed manifest")
	}
	for _, path := range []string{surplus.HostWorktree, surplus.HostHome, surplus.StateRoot, markerPath} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("oversize legacy marker rejection changed %s: %v", path, err)
		}
	}
}

func TestNativeSlotManifestShrinkPreflightsEntireSuffixBeforeMutation(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 5, 3)
	prepareNativeSlotManifestShrinkGit(t, &fixture)
	for _, spec := range fixture.actual.Agents[3:] {
		materializeNativeSlotDisposableState(t, spec)
	}
	cleanSentinel := filepath.Join(fixture.actual.Agents[3].HostWorktree, "clean-slot-sentinel")
	if err := os.WriteFile(cleanSentinel, []byte("preserve\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dirtyPath := filepath.Join(fixture.actual.Agents[4].HostWorktree, "untracked-dirt")
	if err := os.WriteFile(dirtyPath, []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	captureCalls := 0
	previousCapture := captureNativeManifestShrinkSlotHistory
	captureNativeManifestShrinkSlotHistory = func(string, string, nativeSlotProcessIdentity, string, string, bool) error {
		captureCalls++
		return nil
	}
	t.Cleanup(func() { captureNativeManifestShrinkSlotHistory = previousCapture })

	if _, err := reconcileNativeSlotManifestShrink(
		fixture.path,
		fixture.expected,
		fixture.opts,
		nativeSlotManifestShrinkResetOptions(fixture),
		false,
	); err == nil || !strings.Contains(err.Error(), "worktree is dirty") {
		t.Fatalf("dirty suffix rejection = %v", err)
	}
	if captureCalls != 0 {
		t.Fatalf("history effects started before all-surplus preflight: %d", captureCalls)
	}
	if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, fixture.actual) {
		t.Fatalf("failed preflight changed manifest")
	}
	if got, err := os.ReadFile(cleanSentinel); err != nil || string(got) != "preserve\n" {
		t.Fatalf("earlier clean surplus lane was mutated: data=%q err=%v", got, err)
	}
}

func TestNativeSlotManifestShrinkRejectsAheadAndWrongBranch(t *testing.T) {
	t.Run("ahead", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
		git := prepareNativeSlotManifestShrinkGit(t, &fixture)
		spec := fixture.actual.Agents[3]
		runNativeSlotManifestShrinkGit(t, git, "-C", spec.HostWorktree, "config", "user.email", "shrink-test@example.invalid")
		runNativeSlotManifestShrinkGit(t, git, "-C", spec.HostWorktree, "config", "user.name", "shrink-test")
		if err := os.WriteFile(filepath.Join(spec.HostWorktree, "tracked.txt"), []byte("ahead\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runNativeSlotManifestShrinkGit(t, git, "-C", spec.HostWorktree, "add", "tracked.txt")
		runNativeSlotManifestShrinkGit(t, git, "-C", spec.HostWorktree, "commit", "-m", "unpublished")
		if _, err := reconcileNativeSlotManifestShrink(
			fixture.path, fixture.expected, fixture.opts, nativeSlotManifestShrinkResetOptions(fixture), false,
		); err == nil || !strings.Contains(err.Error(), "not contained in refs/remotes/origin/main") {
			t.Fatalf("ahead rejection = %v", err)
		}
	})

	t.Run("wrong-branch", func(t *testing.T) {
		fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
		git := prepareNativeSlotManifestShrinkGit(t, &fixture)
		spec := fixture.actual.Agents[3]
		runNativeSlotManifestShrinkGit(t, git, "-C", spec.HostWorktree, "checkout", "--detach")
		if _, err := reconcileNativeSlotManifestShrink(
			fixture.path, fixture.expected, fixture.opts, nativeSlotManifestShrinkResetOptions(fixture), false,
		); err == nil || !strings.Contains(err.Error(), "exact branch refs/heads/agent4") {
			t.Fatalf("wrong branch rejection = %v", err)
		}
	})
}

func TestNativeSlotManifestShrinkCASRaceRollsBackEveryStagedPath(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
	prepareNativeSlotManifestShrinkGit(t, &fixture)
	spec := fixture.actual.Agents[3]
	materializeNativeSlotDisposableState(t, spec)
	worktreeSentinel := filepath.Join(spec.HostWorktree, "tracked.txt")
	original, err := os.ReadFile(worktreeSentinel)
	if err != nil {
		t.Fatal(err)
	}
	drift := fixture.actual
	drift.BrokerEndpoint += "-concurrent-writer"
	previousHook := nativeManifestShrinkBeforeCASInstall
	nativeManifestShrinkBeforeCASInstall = func() {
		if err := nativeagent.WriteManifest(fixture.path, drift, false); err != nil {
			panic(err)
		}
	}
	t.Cleanup(func() { nativeManifestShrinkBeforeCASInstall = previousHook })

	if _, err := reconcileNativeSlotManifestShrink(
		fixture.path, fixture.expected, fixture.opts, nativeSlotManifestShrinkResetOptions(fixture), false,
	); err == nil || !strings.Contains(err.Error(), "changed during compare-and-swap") {
		t.Fatalf("CAS race result = %v", err)
	}
	if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, drift) {
		t.Fatalf("CAS race overwrote concurrent manifest")
	}
	if got, err := os.ReadFile(worktreeSentinel); err != nil || !reflect.DeepEqual(got, original) {
		t.Fatalf("worktree was not rolled back: data=%q err=%v", got, err)
	}
	for _, path := range []string{spec.HostHome, spec.StateRoot} {
		if _, err := os.Stat(filepath.Join(path, "retirement-sentinel")); err != nil {
			t.Fatalf("staged path was not rolled back %s: %v", path, err)
		}
	}
	if _, err := os.Lstat(nativeManifestShrinkTransactionPath(fixture.path)); !os.IsNotExist(err) {
		t.Fatalf("rolled-back transaction receipt remains: %v", err)
	}
}

func TestNativeSlotManifestShrinkResumesTypedPostCASCleanup(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
	prepareNativeSlotManifestShrinkGit(t, &fixture)
	spec := fixture.actual.Agents[3]
	materializeNativeSlotDisposableState(t, spec)
	plan, err := wtx.PlanNativeSlotReset(nativeManifestShrinkResetOptions(
		fixture.actual,
		spec,
		nativeSlotManifestShrinkResetOptions(fixture),
		false,
	))
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := plan.Stage()
	if err != nil {
		t.Fatal(err)
	}
	journal := nativeManifestShrinkJournal(fixture.path, fixture.actual, fixture.expected, []nativeagent.Spec{spec})
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
		fixture.path, fixture.expected, fixture.opts, nativeSlotManifestShrinkResetOptions(fixture), false,
	); err != nil {
		t.Fatalf("typed cleanup recovery: %v", err)
	}
	for _, path := range []string{spec.HostWorktree, spec.HostHome, spec.StateRoot, transactionPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("recovery residue remains %s: %v", path, err)
		}
	}
}

func TestNativeSlotManifestShrinkResumesTypedPostCASCleanupForLegacySurplus(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
	git, legacyCommonDir := prepareLegacyNativeSlotManifestShrinkGit(t, &fixture)
	spec := fixture.actual.Agents[3]
	materializeNativeSlotDisposableState(t, spec)
	metadataOutput, metadataErr := exec.Command(
		git,
		"-C", spec.HostWorktree,
		"rev-parse", "--absolute-git-dir",
	).CombinedOutput()
	if metadataErr != nil {
		t.Fatalf("read legacy surplus metadata: %v\n%s", metadataErr, metadataOutput)
	}
	metadata := strings.TrimSpace(string(metadataOutput))
	plan, err := wtx.PlanNativeSlotReset(nativeManifestShrinkResetOptions(
		fixture.actual,
		spec,
		nativeSlotManifestShrinkResetOptions(fixture),
		false,
	))
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := plan.Stage()
	if err != nil {
		t.Fatal(err)
	}
	journal := nativeManifestShrinkJournal(fixture.path, fixture.actual, fixture.expected, []nativeagent.Spec{spec})
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
		fixture.path, fixture.expected, fixture.opts, nativeSlotManifestShrinkResetOptions(fixture), false,
	); err != nil {
		t.Fatalf("typed legacy cleanup recovery: %v", err)
	}
	for _, path := range []string{spec.HostWorktree, spec.HostHome, spec.StateRoot, transactionPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("legacy recovery residue remains %s: %v", path, err)
		}
	}
	for _, path := range []string{
		legacyCommonDir,
		metadata,
		filepath.Join(legacyCommonDir, "refs", "heads", "agent4"),
	} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("legacy recovery mutated shared custody %s: %v", path, err)
		}
	}
}

func TestNativeSlotManifestShrinkDryRunHasNoMutation(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
	prepareNativeSlotManifestShrinkGit(t, &fixture)
	spec := fixture.actual.Agents[3]
	materializeNativeSlotDisposableState(t, spec)
	historyDryRun := false
	previousCapture := captureNativeManifestShrinkSlotHistory
	captureNativeManifestShrinkSlotHistory = func(
		_ string, _ string, _ nativeSlotProcessIdentity, _ string, _ string, dryRun bool,
	) error {
		historyDryRun = dryRun
		return nil
	}
	t.Cleanup(func() { captureNativeManifestShrinkSlotHistory = previousCapture })

	if _, err := reconcileNativeSlotManifestShrink(
		fixture.path, fixture.expected, fixture.opts, nativeSlotManifestShrinkResetOptions(fixture), true,
	); err != nil {
		t.Fatal(err)
	}
	if !historyDryRun {
		t.Fatal("history capture was not projected as dry-run")
	}
	if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, fixture.actual) {
		t.Fatalf("dry-run changed manifest")
	}
	for _, path := range []string{spec.HostWorktree, spec.HostHome, spec.StateRoot} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("dry-run changed path %s: %v", path, err)
		}
	}
	if _, err := os.Lstat(nativeManifestShrinkTransactionPath(fixture.path)); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote transaction receipt: %v", err)
	}
}

func TestNativeSlotManifestShrinkPreparedJournalRecoversEveryCrashPrefix(t *testing.T) {
	for _, test := range []struct {
		name        string
		stagePrefix bool
	}{
		{name: "before-first-filesystem-effect"},
		{name: "after-first-rename", stagePrefix: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
			prepareNativeSlotManifestShrinkGit(t, &fixture)
			spec := fixture.actual.Agents[3]
			materializeNativeSlotDisposableState(t, spec)
			plan, err := wtx.PlanNativeSlotReset(nativeManifestShrinkResetOptions(
				fixture.actual,
				spec,
				nativeSlotManifestShrinkResetOptions(fixture),
				false,
			))
			if err != nil {
				t.Fatal(err)
			}
			transaction, err := plan.PrepareTransaction()
			if err != nil {
				t.Fatal(err)
			}
			state := transaction.State()
			if len(state.Staged) == 0 || len(state.Quarantines) == 0 {
				t.Fatal("durable preparation did not describe the retirement")
			}
			for _, quarantine := range state.Quarantines {
				if _, err := os.Lstat(quarantine.Path); !os.IsNotExist(err) {
					t.Fatalf("preparation created quarantine before journal: %s: %v", quarantine.Path, err)
				}
			}

			journal := nativeManifestShrinkJournal(
				fixture.path,
				fixture.actual,
				fixture.expected,
				[]nativeagent.Spec{spec},
			)
			journal.SlotTransactions = []wtx.NativeResetTransactionState{state}
			transactionPath := nativeManifestShrinkTransactionPath(fixture.path)
			if err := writeNativeManifestShrinkTransaction(transactionPath, journal); err != nil {
				t.Fatal(err)
			}
			if test.stagePrefix {
				for _, quarantine := range state.Quarantines {
					if err := os.Mkdir(quarantine.Path, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				first := state.Staged[0]
				if err := os.Rename(first.Source, first.Target); err != nil {
					t.Fatal(err)
				}
			}

			if _, err := reconcileNativeSlotManifestShrink(
				fixture.path,
				fixture.expected,
				fixture.opts,
				nativeSlotManifestShrinkResetOptions(fixture),
				false,
			); err != nil {
				t.Fatalf("recover prepared crash prefix: %v", err)
			}
			if got := readNativeSlotManifestShrinkFixture(t, fixture.path); !reflect.DeepEqual(got, fixture.expected) {
				t.Fatalf("recovered manifest = %#v, want expected", got)
			}
			for _, path := range []string{spec.HostWorktree, spec.HostHome, spec.StateRoot, transactionPath} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("recovered retirement residue remains %s: %v", path, err)
				}
			}
		})
	}
}

func TestNativeSlotManifestShrinkRecoveryRejectsBroadenedJournalPaths(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
	prepareNativeSlotManifestShrinkGit(t, &fixture)
	spec := fixture.actual.Agents[3]
	materializeNativeSlotDisposableState(t, spec)
	plan, err := wtx.PlanNativeSlotReset(nativeManifestShrinkResetOptions(
		fixture.actual,
		spec,
		nativeSlotManifestShrinkResetOptions(fixture),
		false,
	))
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := plan.PrepareTransaction()
	if err != nil {
		t.Fatal(err)
	}
	state := transaction.State()
	if len(state.Staged) == 0 {
		t.Fatal("prepared transaction has no staged mapping")
	}

	t.Run("missing-source-under-allowed-boundary", func(t *testing.T) {
		forged := state
		forged.Staged = append([]wtx.NativeResetStagedPath(nil), state.Staged...)
		forged.Staged[0].Source = filepath.Join(filepath.Dir(spec.HostWorktree), "undeclared-retained-name")
		if _, err := wtx.ResumeNativeResetTransaction(plan, forged); err == nil ||
			!strings.Contains(err.Error(), "outside the reconstructed plan") {
			t.Fatalf("broadened missing source accepted: %v", err)
		}
	})

	t.Run("source-retargeted-to-another-owned-boundary", func(t *testing.T) {
		forged := state
		forged.Staged = append([]wtx.NativeResetStagedPath(nil), state.Staged...)
		var stateQuarantine string
		for _, quarantine := range state.Quarantines {
			if quarantine.Boundary == fixture.actual.HostStateRoot {
				stateQuarantine = quarantine.Path
			}
		}
		if stateQuarantine == "" {
			t.Fatal("prepared transaction has no state-root quarantine")
		}
		forged.Staged[0].Target = filepath.Join(stateQuarantine, "000")
		if _, err := wtx.ResumeNativeResetTransaction(plan, forged); err == nil ||
			!strings.Contains(err.Error(), "outside the reconstructed plan") {
			t.Fatalf("cross-boundary source mapping accepted: %v", err)
		}
	})

	t.Run("undeclared-quarantine-payload", func(t *testing.T) {
		for _, quarantine := range state.Quarantines {
			if err := os.Mkdir(quarantine.Path, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		unexpected := filepath.Join(state.Quarantines[0].Path, "retained-opaque-payload")
		if err := os.Mkdir(unexpected, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := wtx.ResumeNativeResetTransaction(plan, state); err == nil ||
			!strings.Contains(err.Error(), "undeclared payload") {
			t.Fatalf("undeclared quarantine payload accepted: %v", err)
		}
	})
}

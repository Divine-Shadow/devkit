package managementinspect

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testRevision = "0123456789abcdef0123456789abcdef01234567"

func TestParseRefreshOptionsAcceptsExplicitRevisionAndInterspersedCommonFlags(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	opts, err := parseRefreshOptions([]string{
		"--repo", "/workspaces/dev/ouroboros-ide",
		"--name", "agent-source",
		"--revision", "origin/main",
		"--state-root", state,
		"--format", "json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Repository != "/workspaces/dev/ouroboros-ide" || opts.Revision != "origin/main" {
		t.Fatalf("source selection = repo %q revision %q", opts.Repository, opts.Revision)
	}
	if opts.Name != "agent-source" || opts.StateRoot != state || opts.Format != "json" {
		t.Fatalf("common options = %+v", opts.commonOptions)
	}
}

func TestRefreshRequiresRevisionInsteadOfFollowingBranchImplicitly(t *testing.T) {
	_, err := parseRefreshOptions([]string{"--repo", "/workspaces/dev/ouroboros-ide"})
	if err == nil || !strings.Contains(err.Error(), "branch tips are never followed implicitly") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadbackExposesRevisionAndIsolationIdentity(t *testing.T) {
	manifest := fixtureManifest("/nix/store/source-fixture", "/nix/store/identity-fixture", "/explicit/state")
	readback := BuildReadback(manifest)
	if readback.ResolvedRevision != testRevision {
		t.Fatalf("resolved revision = %q", readback.ResolvedRevision)
	}
	if !readback.SourceReadOnly || !readback.RuntimeStateWritable || readback.CodexHomeCopied {
		t.Fatalf("isolation readback = %+v", readback)
	}
	data, err := json.Marshal(readback)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"resolvedRevision":"` + testRevision + `"`,
		`"sourceReadOnly":true`,
		`"runtimeStateWritable":true`,
		`"codexHomeCopied":false`,
	} {
		if !strings.Contains(string(data), expected) {
			t.Fatalf("readback JSON missing %s: %s", expected, data)
		}
	}
}

func TestSandboxArgsMountSourceAndIdentityReadOnlyWithSeparateWritableState(t *testing.T) {
	manifest := fixtureManifest("/nix/store/source-fixture", "/nix/store/identity-fixture", "/explicit/state")
	args := SandboxArgs(manifest, "/nix/store/bash/bin/bash", []string{"-c", "true"}, "/nix/store/rg/bin:/run/current-system/sw/bin")

	requiredSequences := [][]string{
		{"--clearenv"},
		{"--ro-bind", manifest.Identity.SourceStorePath, workspaceSource},
		{"--bind", manifest.RuntimeStatePath, workspaceState},
		{"--ro-bind", manifest.IdentityStorePath, identityRoot},
		{"--unsetenv", "CODEX_HOME"},
		{"--unsetenv", "CODEX_ROLLOUT_DIR"},
		{"--setenv", "DEVKIT_INSPECTION_REVISION", testRevision},
	}
	for _, sequence := range requiredSequences {
		if !containsSequence(args, sequence) {
			t.Fatalf("sandbox args missing %q: %q", sequence, args)
		}
	}
	if containsSequence(args, []string{"--bind", manifest.Identity.SourceStorePath, workspaceSource}) {
		t.Fatalf("source was mounted writable: %q", args)
	}
	if strings.Contains(strings.Join(args, " "), "/run/current-system/sw/bin") {
		t.Fatalf("host runtime PATH leaked into profile: %q", args)
	}
}

func TestArchiveCommitDoesNotCopyDirtyWorktreeOrGitMetadata(t *testing.T) {
	repo := t.TempDir()
	runTestCommand(t, repo, "git", "init", "-q")
	path := filepath.Join(repo, "tracked.txt")
	if err := os.WriteFile(path, []byte("committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestCommand(t, repo, "git", "add", "tracked.txt")
	runTestCommand(t, repo, "git", "-c", "user.name=Devkit Test", "-c", "user.email=devkit@example.invalid", "commit", "-qm", "fixture")
	revision := strings.TrimSpace(runTestCommand(t, repo, "git", "rev-parse", "HEAD"))
	if err := os.WriteFile(path, []byte("dirty throne content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(t.TempDir(), "snapshot")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := archiveCommit(context.Background(), repo, revision, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "tracked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "committed\n" {
		t.Fatalf("snapshot copied worktree content: %q", data)
	}
	if _, err := os.Stat(filepath.Join(destination, "untracked.txt")); !os.IsNotExist(err) {
		t.Fatalf("snapshot copied untracked worktree content: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, ".git")); !os.IsNotExist(err) {
		t.Fatalf("snapshot copied Git metadata: %v", err)
	}
}

func TestStateLayoutRejectsSymlinkEscape(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(filepath.Join(stateRoot, "profiles", "product"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outside, testRevision), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(stateRoot, "profiles", "product", "runtime")); err != nil {
		t.Fatal(err)
	}
	manifest := fixtureManifest("/nix/store/source-fixture", "/nix/store/identity-fixture", stateRoot)
	if err := validateStateLayout(manifest); err == nil || !strings.Contains(err.Error(), "escapes explicit state root") {
		t.Fatalf("err = %v", err)
	}
}

func TestBubblewrapProfileRejectsSourceMutationAndAllowsExplicitStateWrite(t *testing.T) {
	if os.Getenv("DEVKIT_MANAGEMENT_INSPECTION_BWRAP_TEST") != "1" {
		t.Skip("set DEVKIT_MANAGEMENT_INSPECTION_BWRAP_TEST=1 to exercise the host bubblewrap profile")
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		t.Fatal("bubblewrap is required for the profile isolation test")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	bash, err = filepath.EvalSymlinks(bash)
	if err != nil {
		t.Fatal(err)
	}
	if !isNixStorePath(bash) {
		t.Fatalf("test bash is not Nix-store-backed: %s", bash)
	}

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	identity := filepath.Join(tmp, "identity")
	state := filepath.Join(tmp, "state")
	for _, dir := range []string{source, identity, state, filepath.Join(state, "home"), filepath.Join(state, "cache"), filepath.Join(state, "config"), filepath.Join(state, "data")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	probe := filepath.Join(source, "probe")
	if err := os.WriteFile(probe, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(identity, "identity.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := fixtureManifest(source, identity, tmp)
	manifest.RuntimeStatePath = state
	args := SandboxArgs(manifest, bash, []string{"-c", "printf state-ok > /workspace/state/result; printf mutated > /workspace/source/probe"}, filepath.Dir(bash))
	cmd := exec.Command(bwrap, args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("source mutation unexpectedly succeeded: %s", output)
	}
	if data, readErr := os.ReadFile(probe); readErr != nil || string(data) != "original\n" {
		t.Fatalf("source changed through profile: data=%q err=%v", data, readErr)
	}
	if data, readErr := os.ReadFile(filepath.Join(state, "result")); readErr != nil || string(data) != "state-ok" {
		t.Fatalf("explicit runtime state was not writable: data=%q err=%v output=%s", data, readErr, output)
	}
}

func fixtureManifest(source, identity, stateRoot string) Manifest {
	return Manifest{
		Schema: manifestSchema,
		Identity: Identity{
			Schema:            identitySchema,
			Profile:           "product",
			RepositoryPath:    "/workspaces/dev/ouroboros-ide",
			RequestedRevision: "origin/main",
			ResolvedRevision:  testRevision,
			SourceStorePath:   source,
			SnapshotMethod:    "git-archive+nix-store",
		},
		IdentityStorePath: identity,
		RuntimeStatePath:  filepath.Join(stateRoot, "profiles", "product", "runtime", testRevision),
		StateRoot:         stateRoot,
		RefreshedAt:       "2026-07-13T00:00:00Z",
	}
}

func containsSequence(haystack, needle []string) bool {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return false
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if reflect.DeepEqual(haystack[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}

func runTestCommand(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v: %s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

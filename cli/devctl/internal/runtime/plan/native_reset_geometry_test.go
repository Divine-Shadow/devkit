package plan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
)

func TestNativeResetObservesSourceGeometryWithoutAdmittingDiscardedGit(t *testing.T) {
	root := t.TempDir()
	opts := BuildOptions{
		Paths:   devkitpaths.Paths{Root: filepath.Join(root, "devkit")},
		Project: "dev-all", Repo: "ouroboros-ide", Index: 3,
		WorktreeRoot:     filepath.Join(root, "agent-worktrees"),
		StateRoot:        filepath.Join(root, ".devkit", "native-agents"),
		WorkspaceRoot:    filepath.Join(root, "agent-worktrees", "agent3"),
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(root, "allowlist"),
	}
	clean, err := Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(clean.Agent.HostWorktree, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "gui-projections.json")
	previous := guiCodexConfigProjectionManifestPath
	guiCodexConfigProjectionManifestPath = manifest
	t.Cleanup(func() { guiCodexConfigProjectionManifestPath = previous })
	record := guiConfigRecordForPlan(clean, "source-slot-3", "product-governance",
		"/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-gui-config.toml", strings.Repeat("a", 64))
	record.SandboxHome = clean.Agent.SandboxHome
	writeGUIConfigManifest(t, manifest, []guiCodexConfigProjectionRecord{record})
	opts.RequireGUIConfig = true
	opts.WorkspaceRoot = "" // Same implicit isolated projection as whole reset.
	ordinary, err := Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(root, "foreign.git", "worktrees", "protected")
	if err := os.MkdirAll(foreign, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(foreign, "accepted-business-data")
	payload := []byte("external business data must survive opaque Git disposal\n")
	if err := os.WriteFile(sentinel, payload, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	gitFile := filepath.Join(ordinary.Agent.HostWorktree, ".git")
	pointer := []byte("gitdir: " + foreign + "\n")
	if err := os.WriteFile(gitFile, pointer, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(opts); err == nil || !strings.Contains(err.Error(), "Git metadata outside the lane root") {
		t.Fatalf("ordinary execution admitted foreign Git: %v", err)
	}
	geometry, err := ObserveNativeResetSlotGeometry(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(geometry, ordinary.Agent) {
		t.Fatalf("reset source boundary differs from admitted execution: %#v != %#v", geometry, ordinary.Agent)
	}
	for _, path := range []string{sentinel, gitFile} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		want := payload
		if path == gitFile {
			want = pointer
		}
		if string(got) != string(want) {
			t.Fatalf("source observation changed %s", path)
		}
	}
	after, err := os.Stat(sentinel)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("external object changed: %v", err)
	}

	// Geometry-only observation still requires the immutable GUI home pair.
	record.HostHome = filepath.Join(root, "foreign-home")
	writeGUIConfigManifest(t, manifest, []guiCodexConfigProjectionRecord{record})
	if _, err := ObserveNativeResetSlotGeometry(opts); err == nil {
		t.Fatal("reset observation admitted a mismatched immutable GUI home")
	}
}

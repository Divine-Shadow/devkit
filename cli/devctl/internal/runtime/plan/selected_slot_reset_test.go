package plan

import (
	"devkit/cli/devctl/internal/worktrees"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
)

func TestSelectedSlotResetBuildsIsolatedStandardGeometryWithoutChangingSharedManifest(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "projections.json")
	saved := guiCodexConfigProjectionManifestPath
	guiCodexConfigProjectionManifestPath = manifestPath
	t.Cleanup(func() { guiCodexConfigProjectionManifestPath = saved })
	for _, kind := range []string{"devkit-agent", "local-wsl-devkit-agent"} {
		for _, index := range []int{1, 2, 3} {
			t.Run(fmt.Sprintf("%s-%d", kind, index), func(t *testing.T) {
				opts := BuildOptions{Paths: devkitpaths.Paths{Root: "/repo/devkit"}, HostRoot: "/home/bayesartre/dev", Project: "dev-all", Repo: "ouroboros-ide", Index: index, IsolationProfile: IsolationProfileWorkspaceEgress, EgressAllowlist: "/fixture/allowlist"}
				prefix, err := BuildDevAll(opts)
				if err != nil {
					t.Fatal(err)
				}
				before, err := BuildManifest(opts, 3)
				if err != nil {
					t.Fatal(err)
				}
				record := guiConfigRecordForPlan(prefix, "selected-target", "product-governance", "/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-config.toml", strings.Repeat("a", 64))
				record.Kind, record.ExpectedExecutionHost = kind, "source-selected-host"
				writeGUIConfigManifest(t, manifestPath, []guiCodexConfigProjectionRecord{record})
				opts.GUITargetID = record.TargetID
				selected, err := BuildSelectedSlotReset(opts)
				if err != nil {
					t.Fatal(err)
				}
				workspace := fmt.Sprintf("/home/bayesartre/dev/agent-worktrees/agent%d", index)
				homeRoot := "/workspaces/dev"
				if index == 1 {
					homeRoot += "/ouroboros-ide"
				}
				if selected.HostWorkspaceRoot != workspace || selected.SandboxWorkspaceRoot != "/workspaces/dev" || selected.Agent.SandboxWorktree != "/workspaces/dev/ouroboros-ide" || selected.Agent.SandboxHome != fmt.Sprintf("%s/.devhome-agent%d", homeRoot, index) {
					t.Fatalf("selected reset retained prefix geometry: hostRoot=%q sandboxRoot=%q worktree=%q home=%q", selected.HostWorkspaceRoot, selected.SandboxWorkspaceRoot, selected.Agent.SandboxWorktree, selected.Agent.SandboxHome)
				}
				if selected.Agent.HostWorktree != prefix.Agent.HostWorktree || selected.Agent.HostHome != prefix.Agent.HostHome || selected.Agent.StateRoot != prefix.Agent.StateRoot || selected.GUITargetConfig.TargetID != record.TargetID {
					t.Fatal("source host/state/config identity changed")
				}
				after, err := BuildManifest(opts, 3)
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatalf("shared manifest changed: %v", err)
				}
				opts.GUITargetID = ""
				opts.RequireGUIConfig = true
				geometric, err := BuildSelectedSlotReset(opts)
				if err != nil || !reflect.DeepEqual(selected, geometric) {
					t.Fatalf("geometry-selected reset differs: %v", err)
				}
				opts.WorkspaceRoot = workspace
				if _, err := BuildSelectedSlotReset(opts); err == nil {
					t.Fatal("caller workspace admitted")
				}
			})
		}
	}
}

func TestSelectedSlotResetRejectsUnselectedOrForeignGeometryAndPreservesLegacyClass(t *testing.T) {
	opts := BuildOptions{Paths: devkitpaths.Paths{Root: "/repo/devkit"}, HostRoot: "/home/bayesartre/dev", Project: "dev-all", Repo: "ouroboros-ide", Index: 3, IsolationProfile: IsolationProfileWorkspaceEgress, EgressAllowlist: "/fixture/allowlist"}
	if _, err := BuildSelectedSlotReset(opts); err == nil {
		t.Fatal("unselected lane admitted")
	}
	prefix, err := BuildDevAll(opts)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	saved := guiCodexConfigProjectionManifestPath
	guiCodexConfigProjectionManifestPath = path
	t.Cleanup(func() { guiCodexConfigProjectionManifestPath = saved })
	record := guiConfigRecordForPlan(prefix, "legacy-fixture", "product-governance", "/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-config.toml", strings.Repeat("a", 64))
	writeGUIConfigManifest(t, path, []guiCodexConfigProjectionRecord{record})
	opts.GUITargetID = record.TargetID
	expected, err := BuildDevAll(opts)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := BuildSelectedSlotReset(opts)
	if err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("legacy class changed: %v", err)
	}
	opts.GUITargetID = "foreign"
	if _, err := BuildSelectedSlotReset(opts); err == nil {
		t.Fatal("foreign target admitted")
	}
	opts.GUITargetID = record.TargetID
	record.HostHome += "/foreign"
	writeGUIConfigManifest(t, path, []guiCodexConfigProjectionRecord{record})
	if _, err := BuildSelectedSlotReset(opts); err == nil {
		t.Fatal("foreign projection geometry admitted")
	}
}

// A missing forward pointer is legitimate before source-owned bootstrap. Its
// arrival changes the required Git mounts; an old plan must still fail closed.
func TestSelectedSlotResetCapturesNewGitRegistration(t *testing.T) {
	original, wt, sibling := realProductGitPlan(t)
	siblingBefore := snapshotGitFiles(t, sibling)
	metadata, err := worktrees.InspectNativeLaneGitMetadata(wt, original.HostWorktreeRoot, "ouroboros-ide", 1)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(t.TempDir(), "projections.json")
	saved := guiCodexConfigProjectionManifestPath
	guiCodexConfigProjectionManifestPath = manifest
	t.Cleanup(func() { guiCodexConfigProjectionManifestPath = saved })
	record := guiConfigRecordForPlan(original, "post-bootstrap-fixture", "product-governance", "/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-config.toml", strings.Repeat("a", 64))
	record.Kind, record.ExpectedExecutionHost = "local-wsl-devkit-agent", "fixture-host"
	writeGUIConfigManifest(t, manifest, []guiCodexConfigProjectionRecord{record})
	opts := BuildOptions{Paths: devkitpaths.Paths{Root: original.DevkitHostRoot}, Project: "dev-all", Repo: "ouroboros-ide", Index: 1, WorktreeRoot: original.HostWorktreeRoot, GUITargetID: record.TargetID, IsolationProfile: IsolationProfileWorkspaceEgress, EgressAllowlist: original.Proxy.AllowlistPath}
	forward := filepath.Join(wt, ".git")
	withheld := filepath.Join(t.TempDir(), "forward-pointer")
	if err := os.Rename(forward, withheld); err != nil {
		t.Fatal(err)
	}
	beforeManifest, err := BuildManifest(opts, 3)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := BuildSelectedSlotReset(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(withheld, forward); err != nil {
		t.Fatal(err)
	}
	target := "/workspaces/.devkit/git/agent1/ouroboros-ide.git"
	expected := Bind{Source: metadata.CommonDir, Target: target, Mode: "rw", Required: true}
	count := func(p Plan) int {
		n := 0
		for _, b := range p.Binds {
			if b == expected {
				n++
			}
		}
		return n
	}
	if count(stale) != 0 || stale.GitBacklink != nil {
		t.Fatal("pre-bootstrap fixture already contains Git projection")
	}
	if err := ValidateGitBacklinkProjection(stale); err == nil || !strings.Contains(err.Error(), "requires one exact common repository bind") {
		t.Fatalf("stale plan bypassed exact bind guard: %v", err)
	}
	beforeCommon := snapshotGitFiles(t, metadata.CommonDir)
	fresh, err := BuildSelectedSlotReset(opts)
	if err != nil {
		t.Fatal(err)
	}
	if count(fresh) != 1 || fresh.GitBacklink == nil {
		t.Fatalf("new registration not projected: %#v", fresh.Binds)
	}
	if err := ValidateGitBacklinkProjection(fresh); err != nil {
		t.Fatal(err)
	}
	afterManifest, err := BuildManifest(opts, 3)
	if err != nil || !reflect.DeepEqual(beforeManifest, afterManifest) {
		t.Fatalf("prefix manifest changed: %v", err)
	}
	if !reflect.DeepEqual(beforeCommon, snapshotGitFiles(t, metadata.CommonDir)) || !reflect.DeepEqual(siblingBefore, snapshotGitFiles(t, sibling)) {
		t.Fatal("planning changed selected or sibling Git custody")
	}
	t.Logf("actual fixture expected=%+v pre_bootstrap_exact_bind_count=%d post_bootstrap_exact_bind_count=%d strict_stale_refusal=true selected_and_sibling_common_unchanged=true prefix_manifest_unchanged=true", expected, count(stale), count(fresh))
}

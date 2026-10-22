package plan

import (
	"fmt"
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

package plan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGUIConsumerWorkspaceDerivesFixedV4Geometry(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "projection.json")
	record := guiCodexConfigProjectionRecord{
		TargetID: "darksteel-1", ConfigProfile: "product-governance", Project: "dev-all", Repo: "ouroboros-ide", AgentIndex: 1,
		WorkspaceRoot: "/host/agent1", HostWorktree: "/host/agent1/ouroboros-ide", HostHome: "/host/agent1/ouroboros-ide/.devhome-agent1",
		Kind: "devkit-agent", Source: "/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-config.toml", SourceSHA256: strings.Repeat("b", 64),
	}
	writeGUIConfigManifest(t, manifest, []guiCodexConfigProjectionRecord{record})
	got, err := resolveGUIConsumerWorkspaceFrom(manifest, "/nix/store", "dev-all", "ouroboros-ide", "darksteel-1", 1)
	if err != nil || got.Worktree != "/workspaces/dev/ouroboros-ide" || got.Home != "/workspaces/dev/ouroboros-ide/.devhome-agent1" {
		t.Fatalf("consumer = %#v, %v", got, err)
	}
	for _, tc := range []struct {
		name   string
		change func(*guiCodexConfigProjectionRecord)
	}{
		{"other project", func(r *guiCodexConfigProjectionRecord) { r.Project = "other" }},
		{"other repo", func(r *guiCodexConfigProjectionRecord) { r.Repo = "other" }},
		{"other index", func(r *guiCodexConfigProjectionRecord) { r.AgentIndex = 2 }},
		{"other class", func(r *guiCodexConfigProjectionRecord) { r.Kind = "management" }},
		{"foreign home", func(r *guiCodexConfigProjectionRecord) { r.HostHome = "/host/agent2/home" }},
		{"different root geometry", func(r *guiCodexConfigProjectionRecord) { r.HostWorktree = "/host/agent1/extra/ouroboros-ide" }},
		{"mutable config", func(r *guiCodexConfigProjectionRecord) { r.Source = "/tmp/config.toml" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := record
			tc.change(&changed)
			writeGUIConfigManifest(t, manifest, []guiCodexConfigProjectionRecord{changed})
			if _, err := resolveGUIConsumerWorkspaceFrom(manifest, "/nix/store", "dev-all", "ouroboros-ide", "darksteel-1", 1); err == nil {
				t.Fatal("invalid consumer accepted")
			}
		})
	}
	writeGUIConfigManifest(t, manifest, []guiCodexConfigProjectionRecord{record, record})
	if _, err := resolveGUIConsumerWorkspaceFrom(manifest, "/nix/store", "dev-all", "ouroboros-ide", "darksteel-1", 1); err == nil {
		t.Fatal("duplicate target accepted")
	}
	if err := os.WriteFile(manifest, []byte(`{"schemaVersion":"wrong","projections":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveGUIConsumerWorkspaceFrom(manifest, "/nix/store", "dev-all", "ouroboros-ide", "darksteel-1", 1); err == nil {
		t.Fatal("missing target accepted")
	}
}

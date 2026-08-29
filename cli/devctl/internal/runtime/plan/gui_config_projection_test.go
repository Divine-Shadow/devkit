package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
)

func TestBuildSelectsExactGUITargetConfigProjection(t *testing.T) {
	if guiCodexConfigProjectionManifestPath != "/etc/fleet/source/codex-config-projections.json" {
		t.Fatalf("default GUI config projection manifest = %q", guiCodexConfigProjectionManifestPath)
	}
	if guiCodexConfigProjectionSchema != "devkit/gui-codex-config-projections/v1" {
		t.Fatalf("GUI config projection schema = %q", guiCodexConfigProjectionSchema)
	}
	opts := BuildOptions{
		Paths:   devkitpaths.Paths{Root: "/repo/devkit"},
		Project: "dev-all",
		Repo:    "ouroboros-ide",
		Index:   2,
	}
	t.Setenv("DEVKIT_CODEX_CONFIG_SOURCE", "/tmp/hostile-legacy-config.toml")
	base, err := Build(opts)
	if err != nil {
		t.Fatalf("Build ordinary plan: %v", err)
	}
	if base.GUITargetConfig != nil {
		t.Fatalf("ordinary plan selected GUI config: %#v", base.GUITargetConfig)
	}
	if _, ok := base.Env["DEVKIT_CODEX_CONFIG_SOURCE"]; ok {
		t.Fatalf("ordinary plan propagated hostile legacy config authority: %#v", base.Env)
	}
	if got, want := guiTargetGeometryForPlan(base).WorkspaceRoot, filepath.Dir(base.Agent.HostWorktree); got != want {
		t.Fatalf("computed GUI workspace root = %q, want %q", got, want)
	}

	manifestPath := filepath.Join(t.TempDir(), "codex-config-projections.json")
	previousManifestPath := guiCodexConfigProjectionManifestPath
	guiCodexConfigProjectionManifestPath = manifestPath
	t.Cleanup(func() { guiCodexConfigProjectionManifestPath = previousManifestPath })
	record := guiConfigRecordForPlan(
		base,
		"product-agent-2",
		"product-governance",
		"/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-gui-config.toml",
		strings.Repeat("a", sha256.Size*2),
	)
	writeGUIConfigManifest(t, manifestPath, []guiCodexConfigProjectionRecord{
		{
			TargetID:      "unrelated-target",
			ConfigProfile: "management",
			Project:       "unrelated",
			Repo:          "unrelated",
			AgentIndex:    1,
			Source:        "/nix/store/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-unrelated.toml",
			SourceSHA256:  strings.Repeat("b", sha256.Size*2),
		},
		record,
	})
	opts.GUITargetID = record.TargetID
	p, err := Build(opts)
	if err != nil {
		t.Fatalf("Build GUI target plan: %v", err)
	}
	want := GUITargetConfigProjection{
		TargetID:      record.TargetID,
		ConfigProfile: record.ConfigProfile,
		Source:        record.Source,
		SourceSHA256:  record.SourceSHA256,
	}
	if p.GUITargetConfig == nil || *p.GUITargetConfig != want {
		t.Fatalf("selected GUI config = %#v, want %#v", p.GUITargetConfig, want)
	}
	if text := RenderText(p); !strings.Contains(text, "gui_target_id: "+record.TargetID) ||
		!strings.Contains(text, "gui_config_profile: "+record.ConfigProfile) ||
		!strings.Contains(text, "gui_config_source_sha256: "+record.SourceSHA256) {
		t.Fatalf("rendered plan omitted selected GUI config:\n%s", text)
	}
}

func TestGUITargetConfigProjectionRejectsMissingAndDuplicateTarget(t *testing.T) {
	storeRoot := filepath.Join(t.TempDir(), "nix", "store")
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	geometry := guiTargetGeometry{
		Project:       "dev-workspace",
		Repo:          "shadow-throne-management",
		AgentIndex:    2,
		WorkspaceRoot: "/home/bayesartre/dev/control-plane-worktrees/agent2",
		HostWorktree:  "/home/bayesartre/dev/control-plane-worktrees/agent2/shadow-throne-management",
		HostHome:      "/home/bayesartre/dev/control-plane-worktrees/agent2/.devhome-agent2",
	}
	record := guiConfigRecordForGeometry(geometry, "shadow-throne-management-2", "management", filepath.Join(storeRoot, "config.toml"), strings.Repeat("a", 64))
	writeGUIConfigManifest(t, manifestPath, []guiCodexConfigProjectionRecord{record})
	if _, err := loadGUITargetConfigProjectionFrom(manifestPath, storeRoot, "missing-target", geometry); err == nil || !strings.Contains(err.Error(), "is missing") {
		t.Fatalf("missing target error = %v", err)
	}
	writeGUIConfigManifest(t, manifestPath, []guiCodexConfigProjectionRecord{record, record})
	if _, err := loadGUITargetConfigProjectionFrom(manifestPath, storeRoot, record.TargetID, geometry); err == nil || !strings.Contains(err.Error(), "duplicate targetId") {
		t.Fatalf("duplicate target error = %v", err)
	}
}

func TestGUITargetConfigProjectionBindsEveryGeometryField(t *testing.T) {
	storeRoot := filepath.Join(t.TempDir(), "nix", "store")
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	geometry := guiTargetGeometry{
		Project:       "dev-workspace",
		Repo:          "shadow-throne-management",
		AgentIndex:    2,
		WorkspaceRoot: "/home/bayesartre/dev/control-plane-worktrees/agent2",
		HostWorktree:  "/home/bayesartre/dev/control-plane-worktrees/agent2/shadow-throne-management",
		HostHome:      "/home/bayesartre/dev/control-plane-worktrees/agent2/.devhome-agent2",
	}
	base := guiConfigRecordForGeometry(geometry, "shadow-throne-management-2", "management", filepath.Join(storeRoot, "config.toml"), strings.Repeat("a", 64))
	tests := map[string]func(*guiCodexConfigProjectionRecord){
		"project":       func(record *guiCodexConfigProjectionRecord) { record.Project = "wrong" },
		"repo":          func(record *guiCodexConfigProjectionRecord) { record.Repo = "wrong" },
		"agentIndex":    func(record *guiCodexConfigProjectionRecord) { record.AgentIndex++ },
		"workspaceRoot": func(record *guiCodexConfigProjectionRecord) { record.WorkspaceRoot += "-wrong" },
		"hostWorktree":  func(record *guiCodexConfigProjectionRecord) { record.HostWorktree += "-wrong" },
		"hostHome":      func(record *guiCodexConfigProjectionRecord) { record.HostHome += "-wrong" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			record := base
			mutate(&record)
			writeGUIConfigManifest(t, manifestPath, []guiCodexConfigProjectionRecord{record})
			if _, err := loadGUITargetConfigProjectionFrom(manifestPath, storeRoot, record.TargetID, geometry); err == nil || !strings.Contains(err.Error(), name+" geometry mismatch") {
				t.Fatalf("geometry mismatch error = %v", err)
			}
		})
	}
}

func TestGUITargetConfigProjectionRejectsNonStoreSource(t *testing.T) {
	root := t.TempDir()
	storeRoot := filepath.Join(root, "nix", "store")
	manifestPath := filepath.Join(root, "manifest.json")
	geometry := guiTargetGeometry{Project: "dev-all", Repo: "ouroboros-ide", AgentIndex: 1, HostWorktree: "/worktree", HostHome: "/home"}
	record := guiConfigRecordForGeometry(geometry, "product-agent-1", "product-governance", filepath.Join(root, "hostile.toml"), strings.Repeat("a", 64))
	writeGUIConfigManifest(t, manifestPath, []guiCodexConfigProjectionRecord{record})
	if _, err := loadGUITargetConfigProjectionFrom(manifestPath, storeRoot, record.TargetID, geometry); err == nil || !strings.Contains(err.Error(), "must be beneath") {
		t.Fatalf("non-store source error = %v", err)
	}
}

func TestReadValidatedGUITargetConfigSourceRejectsSymlinkEmptyAndHashMismatch(t *testing.T) {
	storeRoot := filepath.Join(t.TempDir(), "nix", "store")
	if err := os.MkdirAll(storeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	validData := []byte("# immutable GUI config\nmodel_provider = \"openai\"\n")
	validSource := filepath.Join(storeRoot, "valid-config.toml")
	if err := os.WriteFile(validSource, validData, 0o444); err != nil {
		t.Fatal(err)
	}
	validDigest := sha256.Sum256(validData)
	valid := GUITargetConfigProjection{
		TargetID:      "product-agent-1",
		ConfigProfile: "product-governance",
		Source:        validSource,
		SourceSHA256:  hex.EncodeToString(validDigest[:]),
	}
	if got, err := readValidatedGUITargetConfigSource(valid, storeRoot); err != nil || string(got) != string(validData) {
		t.Fatalf("valid source = %q, %v", got, err)
	}

	emptySource := filepath.Join(storeRoot, "empty-config.toml")
	if err := os.WriteFile(emptySource, nil, 0o444); err != nil {
		t.Fatal(err)
	}
	realSource := filepath.Join(storeRoot, "real-config.toml")
	if err := os.WriteFile(realSource, validData, 0o444); err != nil {
		t.Fatal(err)
	}
	symlinkSource := filepath.Join(storeRoot, "symlink-config.toml")
	if err := os.Symlink(realSource, symlinkSource); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		projection GUITargetConfigProjection
		want       string
	}{
		{name: "symlink", projection: func() GUITargetConfigProjection { p := valid; p.Source = symlinkSource; return p }(), want: "regular non-symlink"},
		{name: "empty", projection: func() GUITargetConfigProjection { p := valid; p.Source = emptySource; return p }(), want: "is empty"},
		{name: "hash mismatch", projection: func() GUITargetConfigProjection { p := valid; p.SourceSHA256 = strings.Repeat("0", 64); return p }(), want: "SHA-256 mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := readValidatedGUITargetConfigSource(test.projection, storeRoot); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("source validation error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestReadValidatedGUITargetConfigSourceRejectsParentSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	storeRoot := filepath.Join(root, "nix", "store")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(storeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte("# outside mutable config\n")
	outsideSource := filepath.Join(outside, "config.toml")
	if err := os.WriteFile(outsideSource, data, 0o600); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(storeRoot, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	projection := GUITargetConfigProjection{
		TargetID:      "product-agent-1",
		ConfigProfile: "product-governance",
		Source:        filepath.Join(escape, "config.toml"),
		SourceSHA256:  hex.EncodeToString(digest[:]),
	}
	if _, err := readValidatedGUITargetConfigSource(projection, storeRoot); err == nil || !strings.Contains(err.Error(), "without following links") {
		t.Fatalf("parent symlink escape rejection = %v", err)
	}
}

func TestGUITargetConfigSourceReadPinsOpenedHandleAcrossPathReplacement(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "config.toml")
	original := []byte("# immutable original\n")
	if err := os.WriteFile(source, original, 0o600); err != nil {
		t.Fatal(err)
	}
	opened, err := openCanonicalRegularFileNoFollow(source)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if err := os.Rename(source, source+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("hostile replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(opened)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("opened source followed path replacement: got %q, want %q", got, original)
	}
}

func guiConfigRecordForPlan(p Plan, targetID, profile, source, sourceSHA256 string) guiCodexConfigProjectionRecord {
	return guiConfigRecordForGeometry(guiTargetGeometryForPlan(p), targetID, profile, source, sourceSHA256)
}

func guiConfigRecordForGeometry(geometry guiTargetGeometry, targetID, profile, source, sourceSHA256 string) guiCodexConfigProjectionRecord {
	return guiCodexConfigProjectionRecord{
		TargetID:      targetID,
		ConfigProfile: profile,
		Project:       geometry.Project,
		Repo:          geometry.Repo,
		AgentIndex:    geometry.AgentIndex,
		WorkspaceRoot: geometry.WorkspaceRoot,
		HostWorktree:  geometry.HostWorktree,
		HostHome:      geometry.HostHome,
		Source:        source,
		SourceSHA256:  sourceSHA256,
	}
}

func writeGUIConfigManifest(t *testing.T, path string, records []guiCodexConfigProjectionRecord) {
	t.Helper()
	data, err := json.Marshal(guiCodexConfigProjectionManifest{
		SchemaVersion: guiCodexConfigProjectionSchema,
		Projections:   records,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

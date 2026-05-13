package imagematrix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverSkipsNonCanonicalByDefault(t *testing.T) {
	root := t.TempDir()
	writeOverlay(t, root, "primary", `
service: dev-agent
defaults:
  repo: app
runtime:
  flake: .#primary
  codex_version: 0.128.0
  core_check: make test
`)
	writeOverlay(t, root, "legacy", `
service: dev-agent
defaults:
  repo: app
runtime:
  canonical: false
  flake: .#legacy
  codex_version: 0.128.0
  core_check: make test
`)

	entries, err := Discover([]string{root}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Overlay != "primary" {
		t.Fatalf("entries=%+v", entries)
	}

	all, err := Discover([]string{root}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("all entries=%+v", all)
	}
}

func TestCheckRejectsDuplicateRepos(t *testing.T) {
	root := t.TempDir()
	writeOverlay(t, root, "a", `
service: dev-agent
defaults:
  repo: app
runtime:
  flake: .#a
  codex_version: 0.128.0
  core_check: make test
`)
	writeOverlay(t, root, "b", `
service: dev-agent
defaults:
  repo: app
runtime:
  flake: .#b
  codex_version: 0.128.0
  core_check: make test
`)

	err := Check([]Entry{
		{Overlay: "a", Repo: "app", Flake: ".#a", CodexVersion: "0.128.0", CoreCheck: "make test", FlakePath: filepath.Join(root, "a", "flake.nix"), Canonical: true},
		{Overlay: "b", Repo: "app", Flake: ".#b", CodexVersion: "0.128.0", CoreCheck: "make test", FlakePath: filepath.Join(root, "b", "flake.nix"), Canonical: true},
	}, true)
	if err == nil {
		t.Fatal("expected duplicate repo error")
	}
}

func TestDiscoverIncludesNativeFlakeRuntime(t *testing.T) {
	root := t.TempDir()
	writeOverlay(t, root, "native", `
service: dev-agent
defaults:
  repo: app
runtime:
  flake: .#native
  codex_version: 0.130.0
  core_check: make test
`)

	entries, err := Discover([]string{root}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%+v", entries)
	}
	if entries[0].Image != "" || entries[0].Flake != ".#native" {
		t.Fatalf("runtime entry=%+v", entries[0])
	}
	if entries[0].FlakePath == "" {
		t.Fatalf("missing overlay-local flake path: %+v", entries[0])
	}
	if err := Check(entries, true); err != nil {
		t.Fatalf("native flake check failed: %v", err)
	}
}

func TestCheckAcceptsOverlayLocalFlakeRef(t *testing.T) {
	flakePath := filepath.Join(t.TempDir(), "flake.nix")
	if err := os.WriteFile(flakePath, []byte("{ outputs = { ... }: {}; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Check([]Entry{{
		Overlay:      "_template",
		Repo:         "your-repo-name",
		Flake:        "./overlays/_template#default",
		CodexVersion: "0.130.0",
		CoreCheck:    "echo ok",
		FlakePath:    flakePath,
		Canonical:    true,
	}}, true)
	if err != nil {
		t.Fatalf("overlay-local flake ref rejected: %v", err)
	}
}

func TestCheckRejectsWrongOverlayLocalFlakeRef(t *testing.T) {
	flakePath := filepath.Join(t.TempDir(), "flake.nix")
	if err := os.WriteFile(flakePath, []byte("{ outputs = { ... }: {}; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Check([]Entry{{
		Overlay:      "_template",
		Repo:         "your-repo-name",
		Flake:        "./overlays/dev-all#default",
		CodexVersion: "0.130.0",
		CoreCheck:    "echo ok",
		FlakePath:    flakePath,
		Canonical:    true,
	}}, true)
	if err == nil || !strings.Contains(err.Error(), "is not an accepted ref") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckRejectsNonTemplateOverlayLocalFlakeRef(t *testing.T) {
	flakePath := filepath.Join(t.TempDir(), "flake.nix")
	if err := os.WriteFile(flakePath, []byte("{ outputs = { ... }: {}; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Check([]Entry{{
		Overlay:      "pokeemerald",
		Repo:         "pokeemerald",
		Flake:        "./overlays/pokeemerald#default",
		CodexVersion: "0.130.0",
		CoreCheck:    "make modern",
		FlakePath:    flakePath,
		Canonical:    true,
	}}, true)
	if err == nil || !strings.Contains(err.Error(), "is not an accepted ref") {
		t.Fatalf("err = %v", err)
	}
}

func TestDiscoverAllIncludesTemplateAndMissingRuntime(t *testing.T) {
	root := t.TempDir()
	writeOverlay(t, root, "_template", `
service: app
defaults:
  repo: template
`)

	entries, err := Discover([]string{root}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Overlay != "_template" {
		t.Fatalf("entries=%+v", entries)
	}
	if err := Check(entries, true); err == nil {
		t.Fatal("expected missing runtime.flake error")
	}
}

func TestCheckRejectsRuntimeImageAndFlakeDrift(t *testing.T) {
	flakePath := filepath.Join(t.TempDir(), "flake.nix")
	if err := os.WriteFile(flakePath, []byte("{ outputs = { ... }: {}; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Check([]Entry{{
		Overlay:      "pokeemerald",
		Repo:         "pokeemerald",
		Image:        "local/dev-agent:pokeemerald",
		Flake:        ".#wrong",
		CodexVersion: "0.130.0",
		CoreCheck:    "make modern",
		FlakePath:    flakePath,
		Canonical:    true,
	}}, true)
	if err == nil {
		t.Fatal("expected runtime image and flake drift error")
	}
}

func TestCheckRejectsMissingOverlayLocalFlake(t *testing.T) {
	err := Check([]Entry{{
		Overlay:      "pokeemerald",
		Repo:         "pokeemerald",
		Flake:        ".#pokeemerald",
		CodexVersion: "0.130.0",
		CoreCheck:    "make modern",
		FlakePath:    filepath.Join(t.TempDir(), "flake.nix"),
		Canonical:    true,
	}}, true)
	if err == nil || !strings.Contains(err.Error(), "missing overlay-local flake.nix") {
		t.Fatalf("err = %v", err)
	}
}

func writeOverlay(t *testing.T, root, name, yaml string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devkit.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "flake.nix"), []byte("{ outputs = { ... }: {}; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

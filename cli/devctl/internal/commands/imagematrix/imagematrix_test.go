package imagematrix

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSkipsNonCanonicalByDefault(t *testing.T) {
	root := t.TempDir()
	writeOverlay(t, root, "primary", `
service: dev-agent
defaults:
  repo: app
runtime:
  image: local/dev-agent:app
  codex_version: 0.128.0
  core_check: make test
`)
	writeOverlay(t, root, "legacy", `
service: dev-agent
defaults:
  repo: app
runtime:
  canonical: false
  image: local/dev-agent:app
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
	a := writeOverlay(t, root, "a", `
service: dev-agent
defaults:
  repo: app
runtime:
  image: local/dev-agent:a
  codex_version: 0.128.0
  core_check: make test
`)
	b := writeOverlay(t, root, "b", `
service: dev-agent
defaults:
  repo: app
runtime:
  image: local/dev-agent:b
  codex_version: 0.128.0
  core_check: make test
`)

	err := Check([]Entry{
		{Overlay: "a", Repo: "app", Image: "local/dev-agent:a", CodexVersion: "0.128.0", CoreCheck: "make test", ComposePath: a},
		{Overlay: "b", Repo: "app", Image: "local/dev-agent:b", CodexVersion: "0.128.0", CoreCheck: "make test", ComposePath: b},
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
  flake: .#dev-all
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
	if entries[0].Image != "" || entries[0].Flake != ".#dev-all" {
		t.Fatalf("runtime entry=%+v", entries[0])
	}
	if err := Check(entries, true); err != nil {
		t.Fatalf("native flake check failed: %v", err)
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
	composePath := filepath.Join(dir, "compose.override.yml")
	image := "local/dev-agent:" + name
	if name == "primary" || name == "legacy" {
		image = "local/dev-agent:app"
	}
	if err := os.WriteFile(composePath, []byte("services:\n  dev-agent:\n    image: "+image+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return composePath
}

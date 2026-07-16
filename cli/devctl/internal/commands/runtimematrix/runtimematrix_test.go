package runtimematrix

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
  flake: ./overlays/primary#default
  codex_version: 0.128.0
  core_check: make test
`)
	writeOverlay(t, root, "alternate", `
service: dev-agent
defaults:
  repo: app
runtime:
  canonical: false
  flake: ./overlays/alternate#default
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
  flake: ./overlays/a#default
  codex_version: 0.128.0
  core_check: make test
`)
	writeOverlay(t, root, "b", `
service: dev-agent
defaults:
  repo: app
runtime:
  flake: ./overlays/b#default
  codex_version: 0.128.0
  core_check: make test
`)

	err := Check([]Entry{
		{Overlay: "a", Repo: "app", Flake: "./overlays/a#default", CodexVersion: "0.128.0", CoreCheck: "make test", FlakePath: filepath.Join(root, "a", "flake.nix"), Canonical: true},
		{Overlay: "b", Repo: "app", Flake: "./overlays/b#default", CodexVersion: "0.128.0", CoreCheck: "make test", FlakePath: filepath.Join(root, "b", "flake.nix"), Canonical: true},
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
  flake: ./overlays/native#default
  codex_version: 0.133.0
  core_check: make test
`)

	entries, err := Discover([]string{root}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%+v", entries)
	}
	if entries[0].Image != "" || entries[0].Flake != "./overlays/native#default" {
		t.Fatalf("runtime entry=%+v", entries[0])
	}
	if entries[0].FlakePath == "" {
		t.Fatalf("missing overlay-local flake path: %+v", entries[0])
	}
	if entries[0].RuntimeWorkDir != filepath.Dir(root) {
		t.Fatalf("runtime work dir = %q, want %q", entries[0].RuntimeWorkDir, filepath.Dir(root))
	}
	if err := Check(entries, true); err != nil {
		t.Fatalf("native flake check failed: %v", err)
	}
}

func TestFilterSelectsRepoOrOverlay(t *testing.T) {
	entries := []Entry{
		{Overlay: "dev-all", Repo: "ouroboros-ide"},
		{Overlay: "ouroboros-terraform", Repo: "ouroboros-terraform"},
		{Overlay: "pokeemerald", Repo: "pokeemerald"},
	}
	filtered, err := Filter(entries, []string{"ouroboros-terraform"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Overlay != "ouroboros-terraform" {
		t.Fatalf("repo filtered entries=%+v", filtered)
	}

	filtered, err = Filter(entries, nil, []string{"pokeemerald"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Repo != "pokeemerald" {
		t.Fatalf("overlay filtered entries=%+v", filtered)
	}
}

func TestFilterRejectsNoMatch(t *testing.T) {
	_, err := Filter([]Entry{{Overlay: "dev-all", Repo: "ouroboros-ide"}}, []string{"missing"}, nil)
	if err == nil || !strings.Contains(err.Error(), "no runtime pairings matched") {
		t.Fatalf("err = %v", err)
	}
}

func TestDiscoverResolvesRuntimeFlakeInputOverrides(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "devkit", "overlays")
	repo := filepath.Join(tmp, "repo-runtime")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeOverlay(t, root, "native", `
service: dev-agent
defaults:
  repo: app
runtime:
  flake: ./overlays/native#default
  flake_input_overrides:
    app-runtime: ../repo-runtime
  codex_version: 0.133.0
  core_check: make test
`)

	entries, err := Discover([]string{root}, false)
	if err != nil {
		t.Fatal(err)
	}
	got := entries[0].FlakeInputOverrides["app-runtime"]
	if got != "path:"+repo {
		t.Fatalf("override = %q, want path:%s", got, repo)
	}
	args := strings.Join(flakeCodexVersionArgs(entries[0].Flake, entries[0].FlakeInputOverrides), " ")
	if !strings.Contains(args, "--override-input app-runtime path:"+repo+" ./overlays/native#default") {
		t.Fatalf("codex version args missing override before flake: %s", args)
	}
	if err := Check(entries, true); err != nil {
		t.Fatalf("matrix check rejected override: %v", err)
	}
}

func TestCheckRejectsMissingRuntimeFlakeInputOverridePath(t *testing.T) {
	flakePath := filepath.Join(t.TempDir(), "flake.nix")
	if err := os.WriteFile(flakePath, []byte("{ outputs = { ... }: {}; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Check([]Entry{{
		Overlay:             "native",
		Repo:                "app",
		Flake:               "./overlays/native#default",
		FlakeInputOverrides: map[string]string{"app-runtime": "path:/missing/repo-runtime"},
		CodexVersion:        "0.133.0",
		CoreCheck:           "make test",
		FlakePath:           flakePath,
		Canonical:           true,
	}}, true)
	if err == nil || !strings.Contains(err.Error(), "flake input override app-runtime path missing") {
		t.Fatalf("err = %v", err)
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
		CodexVersion: "0.133.0",
		CoreCheck:    "echo ok",
		FlakePath:    flakePath,
		Canonical:    true,
	}}, true)
	if err != nil {
		t.Fatalf("overlay-local flake ref rejected: %v", err)
	}
}

func TestCheckAcceptsDevAllRootedAndLegacyFlakeRefs(t *testing.T) {
	flakePath := filepath.Join(t.TempDir(), "flake.nix")
	if err := os.WriteFile(flakePath, []byte("{ outputs = { ... }: {}; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, flake := range []string{
		"path:.?dir=overlays/dev-all#default",
		"./overlays/dev-all#default",
	} {
		flake := flake
		t.Run(flake, func(t *testing.T) {
			err := Check([]Entry{{
				Overlay:      "dev-all",
				Repo:         "ouroboros-ide",
				Flake:        flake,
				CodexVersion: "0.144.0",
				CoreCheck:    `bash scripts/sbt2 "Compile / compile"`,
				FlakePath:    flakePath,
				Canonical:    true,
			}}, true)
			if err != nil {
				t.Fatalf("dev-all flake ref %q rejected: %v", flake, err)
			}
		})
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
		CodexVersion: "0.133.0",
		CoreCheck:    "echo ok",
		FlakePath:    flakePath,
		Canonical:    true,
	}}, true)
	if err == nil || !strings.Contains(err.Error(), "is not an accepted ref") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckAcceptsNonTemplateOverlayLocalFlakeRef(t *testing.T) {
	flakePath := filepath.Join(t.TempDir(), "flake.nix")
	if err := os.WriteFile(flakePath, []byte("{ outputs = { ... }: {}; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Check([]Entry{{
		Overlay:      "pokeemerald",
		Repo:         "pokeemerald",
		Flake:        "./overlays/pokeemerald#default",
		CodexVersion: "0.133.0",
		CoreCheck:    "make modern",
		FlakePath:    flakePath,
		Canonical:    true,
	}}, true)
	if err != nil {
		t.Fatalf("overlay-local flake ref rejected: %v", err)
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
		Flake:        "./overlays/wrong#default",
		CodexVersion: "0.133.0",
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
		Flake:        "./overlays/pokeemerald#default",
		CodexVersion: "0.133.0",
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

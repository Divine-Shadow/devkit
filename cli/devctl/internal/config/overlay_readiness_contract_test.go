package config

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestFlakeBackedOverlaysDeclareReadinessContract(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	overlays := []string{
		"_template",
		"codex",
		"dev-all",
		"dumb-onion-hax",
		"ouro-integration",
		"ouroboros-static-front-end",
		"ouroboros-terraform",
		"pokeemerald",
	}
	for _, overlay := range overlays {
		t.Run(overlay, func(t *testing.T) {
			cfg, _, err := ReadAll([]string{filepath.Join(root, "overlays")}, overlay)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if !HasRuntimeFlake(cfg) {
				t.Fatalf("%s missing runtime.flake", overlay)
			}
			if cfg.Runtime.CoreCheck == "" {
				t.Fatalf("%s missing runtime.core_check", overlay)
			}
			if cfg.Runtime.CodexVersion == "" {
				t.Fatalf("%s missing runtime.codex_version", overlay)
			}
			if len(cfg.Readiness.RuntimeChecks) == 0 {
				t.Fatalf("%s missing readiness.runtime_checks", overlay)
			}
			if len(cfg.Readiness.RepoChecks) == 0 {
				t.Fatalf("%s missing readiness.repo_checks", overlay)
			}
			if !hasRuntimeCheck(cfg, "required-tools") {
				t.Fatalf("%s missing required-tools runtime check", overlay)
			}
			if !hasRepoCheck(cfg, "core-check") {
				t.Fatalf("%s missing core-check repo check", overlay)
			}
		})
	}
}

func hasRuntimeCheck(cfg OverlayConfig, name string) bool {
	for _, check := range cfg.Readiness.RuntimeChecks {
		if check.Name == name {
			return true
		}
	}
	return false
}

func hasRepoCheck(cfg OverlayConfig, name string) bool {
	for _, check := range cfg.Readiness.RepoChecks {
		if check.Name == name {
			return true
		}
	}
	return false
}

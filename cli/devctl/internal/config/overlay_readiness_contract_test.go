package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
			if mode, ok := NormalizeReadinessMode(cfg.Readiness.DefaultMode); !ok || mode != ReadinessModeRuntimeOnly {
				t.Fatalf("%s readiness.default_mode = %q", overlay, cfg.Readiness.DefaultMode)
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

func TestDevAllRequiredToolsFailsOnMissingIntermediateTool(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	cfg, _, err := ReadAll([]string{filepath.Join(root, "overlays")}, "dev-all")
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	var command string
	for _, check := range cfg.Readiness.RuntimeChecks {
		if check.Name == "required-tools" {
			command = check.Command
			break
		}
	}
	if command == "" {
		t.Fatal("dev-all missing required-tools runtime check")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("resolve package test shell: %v", err)
	}

	bin := t.TempDir()
	for _, tool := range []string{
		"git", "ssh", "curl", "docker", "go", "java", "sbt",
		"npm", "purs", "spago", "vite", "netlify", "playwright",
		"deno", "aws", "make", "gcc", "mgba-headless",
	} {
		if err := os.WriteFile(filepath.Join(bin, tool), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bash, "-c", command)
	cmd.Env = []string{"PATH=" + bin}
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("required-tools masked missing intermediate node; output=%q", output)
	}
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 127 {
		t.Fatalf("required-tools missing-node exit = %v; output=%q", err, output)
	}
	if !strings.Contains(string(output), "missing required runtime tool: node") {
		t.Fatalf("required-tools missing-node detail = %q", output)
	}
}

func TestDevAllGovernanceProvenanceReadinessIsSlotScoped(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	cfg, _, err := ReadAll([]string{filepath.Join(root, "overlays")}, "dev-all")
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	var command string
	for _, check := range cfg.Readiness.RuntimeChecks {
		if check.Name == "governance-provenance" {
			command = check.Command
			break
		}
	}
	if command == "" {
		t.Fatal("dev-all missing governance-provenance runtime check")
	}
	if !strings.Contains(command, `${CODEX_HOME:-$HOME/.codex}/log/governance-mcp-stdio-forward/provenance.json`) {
		t.Fatal("dev-all governance provenance must resolve from the current slot CODEX_HOME")
	}
	if strings.Contains(command, "find /workspaces/dev/ouroboros-ide /workspaces/dev/agent-worktrees /agent-state") {
		t.Fatal("dev-all governance provenance must not couple current readiness to sibling leased homes")
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

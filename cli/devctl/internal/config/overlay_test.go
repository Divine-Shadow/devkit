package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadHooks_FromYaml(t *testing.T) {
	dir := t.TempDir()
	over := filepath.Join(dir, "overlays", "proj")
	if err := os.MkdirAll(over, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := "" +
		"workspace: ../../x\n" +
		"env:\n  FOO: bar\n" +
		"hooks:\n  warm: echo 'hi'\n  maintain: echo world\n"
	if err := os.WriteFile(filepath.Join(over, "devkit.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadHooks([]string{filepath.Join(dir, "overlays")}, "proj")
	if err != nil {
		t.Fatal(err)
	}
	if got.Warm != "echo 'hi'" {
		t.Fatalf("warm=%q", got.Warm)
	}
	if got.Maintain != "echo world" {
		t.Fatalf("maintain=%q", got.Maintain)
	}
}

func TestReadAllIncludesWorkspaceAndEnv(t *testing.T) {
	dir := t.TempDir()
	over := filepath.Join(dir, "overlays", "proj")
	if err := os.MkdirAll(over, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := "" +
		"workspace: ../../my-repo\n" +
		"env:\n  FOO: bar\n  BAZ: qux\n"
	if err := os.WriteFile(filepath.Join(over, "devkit.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, dirPath, err := ReadAll([]string{filepath.Join(dir, "overlays")}, "proj")
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if dirPath != over {
		t.Fatalf("overlay dir=%q", dirPath)
	}
	if cfg.Workspace != "../../my-repo" {
		t.Fatalf("workspace=%q", cfg.Workspace)
	}
	if cfg.Env == nil || cfg.Env["FOO"] != "bar" || cfg.Env["BAZ"] != "qux" {
		t.Fatalf("env=%v", cfg.Env)
	}
}

func TestReadAllParsesBrokerAndReadiness(t *testing.T) {
	dir := t.TempDir()
	over := filepath.Join(dir, "overlays", "proj")
	if err := os.MkdirAll(over, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := "" +
		"broker:\n" +
		"  socket: /run/devkit/test-container-broker.sock\n" +
		"  upstream: unix:///var/run/docker.sock\n" +
		"  allowed_images:\n" +
		"    - postgres:latest\n" +
		"  allow_pulls: true\n" +
		"readiness:\n" +
		"  default_mode: runtime-only\n" +
		"  runtime_checks:\n" +
		"    - name: tools\n" +
		"      command: command -v spago\n" +
		"  repo_checks:\n" +
		"    - name: typecheck\n" +
		"      command: npm test\n" +
		"native:\n" +
		"  worktree_root: ../agent-worktrees\n" +
		"  state_root: ../.devkit/native-agents\n" +
		"  worktree_container_root: /worktrees\n" +
		"  state_container_root: /agent-state\n" +
		"runtime:\n" +
		"  flake: .#dev-all\n"
	if err := os.WriteFile(filepath.Join(over, "devkit.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := ReadAll([]string{filepath.Join(dir, "overlays")}, "proj")
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if cfg.Broker.Socket != "/run/devkit/test-container-broker.sock" {
		t.Fatalf("broker socket = %q", cfg.Broker.Socket)
	}
	if cfg.Broker.AllowPulls == nil || !*cfg.Broker.AllowPulls {
		t.Fatalf("allow pulls = %#v", cfg.Broker.AllowPulls)
	}
	if len(cfg.Broker.AllowedImages) != 1 || cfg.Broker.AllowedImages[0] != "postgres:latest" {
		t.Fatalf("allowed images = %#v", cfg.Broker.AllowedImages)
	}
	if len(cfg.Readiness.RepoChecks) != 1 || cfg.Readiness.RepoChecks[0].Name != "typecheck" || cfg.Readiness.RepoChecks[0].Command != "npm test" {
		t.Fatalf("repo checks = %#v", cfg.Readiness.RepoChecks)
	}
	if cfg.Readiness.DefaultMode != "runtime-only" {
		t.Fatalf("default mode = %q", cfg.Readiness.DefaultMode)
	}
	if len(cfg.Readiness.RuntimeChecks) != 1 || cfg.Readiness.RuntimeChecks[0].Name != "tools" || cfg.Readiness.RuntimeChecks[0].Command != "command -v spago" {
		t.Fatalf("runtime checks = %#v", cfg.Readiness.RuntimeChecks)
	}
	if cfg.Native.WorktreeRoot != "../agent-worktrees" || cfg.Native.StateRoot != "../.devkit/native-agents" {
		t.Fatalf("native roots = %#v", cfg.Native)
	}
	if cfg.Native.WorktreeContainerRoot != "/worktrees" || cfg.Native.StateContainerRoot != "/agent-state" {
		t.Fatalf("native container roots = %#v", cfg.Native)
	}
	if cfg.Runtime.Flake != ".#dev-all" {
		t.Fatalf("runtime flake = %q", cfg.Runtime.Flake)
	}
}

func TestReadAllSkipsMissing(t *testing.T) {
	cfg, dirPath, err := ReadAll([]string{"/does/not/exist"}, "proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dirPath != "" {
		t.Fatalf("expected empty dir, got %q", dirPath)
	}
	if cfg.Env != nil && len(cfg.Env) > 0 {
		t.Fatalf("expected empty env, got %v", cfg.Env)
	}
}

func TestReadAllParsesIngress(t *testing.T) {
	dir := t.TempDir()
	over := filepath.Join(dir, "overlays", "proj")
	if err := os.MkdirAll(over, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := "" +
		"service: frontend\n" +
		"ingress:\n" +
		"  kind: caddy\n" +
		"  config: infra/Caddyfile\n" +
		"  routes:\n" +
		"    - host: ouroboros.test\n" +
		"      path: /_governance/*\n" +
		"      service: frontend\n" +
		"      port: 4173\n" +
		"      cert: infra/ouroboros.test.pem\n" +
		"      key: infra/ouroboros.test-key.pem\n" +
		"  certs:\n" +
		"    - path: infra/ouroboros.test.pem\n" +
		"    - path: infra/ouroboros.test-key.pem\n" +
		"  hosts:\n" +
		"    - ouroboros.test\n" +
		"    - webserver.ouroboros.test\n" +
		"  env:\n" +
		"    CADDY_EXTRA: 1\n"
	if err := os.WriteFile(filepath.Join(over, "devkit.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := ReadAll([]string{filepath.Join(dir, "overlays")}, "proj")
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if cfg.Ingress == nil {
		t.Fatalf("expected ingress config")
	}
	if cfg.Ingress.Kind != "caddy" {
		t.Fatalf("kind=%q", cfg.Ingress.Kind)
	}
	if cfg.Ingress.Config != "infra/Caddyfile" {
		t.Fatalf("config=%q", cfg.Ingress.Config)
	}
	if len(cfg.Ingress.Routes) != 1 || cfg.Ingress.Routes[0].Host != "ouroboros.test" || cfg.Ingress.Routes[0].Path != "/_governance/*" || cfg.Ingress.Routes[0].Service != "frontend" || cfg.Ingress.Routes[0].Port != 4173 {
		t.Fatalf("routes=%v", cfg.Ingress.Routes)
	}
	if cfg.Ingress.Routes[0].Cert != "infra/ouroboros.test.pem" || cfg.Ingress.Routes[0].Key != "infra/ouroboros.test-key.pem" {
		t.Fatalf("route cert/key %+v", cfg.Ingress.Routes[0])
	}
	if len(cfg.Ingress.Certs) != 2 || cfg.Ingress.Certs[0].Path != "infra/ouroboros.test.pem" {
		t.Fatalf("certs=%v", cfg.Ingress.Certs)
	}
	if len(cfg.Ingress.Hosts) != 2 || cfg.Ingress.Hosts[1] != "webserver.ouroboros.test" {
		t.Fatalf("hosts=%v", cfg.Ingress.Hosts)
	}
	if cfg.Ingress.Env == nil || cfg.Ingress.Env["CADDY_EXTRA"] != "1" {
		t.Fatalf("env=%v", cfg.Ingress.Env)
	}
}

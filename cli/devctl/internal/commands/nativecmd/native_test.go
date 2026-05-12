package nativecmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/compose"
	"devkit/cli/devctl/internal/config"
	runtimebroker "devkit/cli/devctl/internal/runtime/broker"
	"devkit/cli/devctl/internal/runtime/capacity"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/runtime/readiness"
)

func TestRepoChecksForUsesExplicitRepoCheckOnly(t *testing.T) {
	checks, err := repoChecksFor(&cmdregistry.Context{}, planArgs{repoCheck: "exit 7"})
	if err != nil {
		t.Fatalf("repoChecksFor: %v", err)
	}
	if len(checks) != 1 || checks[0].Name != "repo-check" || checks[0].Command != "exit 7" {
		t.Fatalf("checks = %#v", checks)
	}
}

func TestRepoChecksForUsesStructuredOverlayChecks(t *testing.T) {
	tmp := t.TempDir()
	overlay := filepath.Join(tmp, "overlays", "dev-all")
	if err := os.MkdirAll(overlay, 0o755); err != nil {
		t.Fatalf("mkdir overlay: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "devkit.yaml"), []byte(`
hooks:
  warm: echo warm
runtime:
  core_check: echo core
readiness:
  repo_checks:
    - name: typecheck
      command: npm test
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Paths:   compose.Paths{OverlayPaths: []string{filepath.Join(tmp, "overlays")}},
	}
	checks, err := repoChecksFor(ctx, planArgs{})
	if err != nil {
		t.Fatalf("repoChecksFor: %v", err)
	}
	if len(checks) != 1 || checks[0].Name != "typecheck" || checks[0].Command != "npm test" {
		t.Fatalf("checks = %#v", checks)
	}
}

func TestRuntimeChecksForUsesStructuredOverlayChecks(t *testing.T) {
	tmp := t.TempDir()
	overlay := filepath.Join(tmp, "overlays", "dev-all")
	if err := os.MkdirAll(overlay, 0o755); err != nil {
		t.Fatalf("mkdir overlay: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "devkit.yaml"), []byte(`
readiness:
  runtime_checks:
    - name: tools
      command: command -v spago
    - command: command -v playwright
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Paths:   compose.Paths{OverlayPaths: []string{filepath.Join(tmp, "overlays")}},
	}
	checks, err := runtimeChecksFor(ctx)
	if err != nil {
		t.Fatalf("runtimeChecksFor: %v", err)
	}
	if len(checks) != 2 || checks[0].Name != "tools" || checks[0].Command != "command -v spago" {
		t.Fatalf("checks = %#v", checks)
	}
	if checks[1].Name != "runtime-check-2" || checks[1].Command != "command -v playwright" {
		t.Fatalf("defaulted check = %#v", checks[1])
	}
}

func TestRepoChecksForFallsBackToWarmAndCoreCheck(t *testing.T) {
	tmp := t.TempDir()
	overlay := filepath.Join(tmp, "overlays", "dev-all")
	if err := os.MkdirAll(overlay, 0o755); err != nil {
		t.Fatalf("mkdir overlay: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "devkit.yaml"), []byte(`
hooks:
  warm: echo warm
runtime:
  core_check: echo core
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Paths:   compose.Paths{OverlayPaths: []string{filepath.Join(tmp, "overlays")}},
	}
	checks, err := repoChecksFor(ctx, planArgs{})
	if err != nil {
		t.Fatalf("repoChecksFor: %v", err)
	}
	if len(checks) != 2 || checks[0].Name != "warm-hook" || checks[1].Name != "core-check" {
		t.Fatalf("checks = %#v", checks)
	}
}

func TestParseLifecycleArgs(t *testing.T) {
	allowPulls := false
	parsed, err := parseLifecycleArgs(&cmdregistry.Context{Args: []string{
		"--repo", "devkit",
		"--count", "2",
		"--flake", ".#runtime-test-agent",
		"--broker-socket", "/tmp/broker.sock",
		"--broker-state-root", "/tmp/broker-state",
		"--allow-image", "postgres:15",
		"--no-allow-pulls",
		"--ready",
		"--skip-repo-checks",
		"--format", "json",
	}})
	if err != nil {
		t.Fatalf("parseLifecycleArgs: %v", err)
	}
	if parsed.repo != "devkit" || parsed.count != 2 || parsed.flake != ".#runtime-test-agent" {
		t.Fatalf("parsed = %#v", parsed)
	}
	if parsed.brokerSocket != "/tmp/broker.sock" || parsed.brokerStateRoot != "/tmp/broker-state" {
		t.Fatalf("broker paths = %#v", parsed)
	}
	if len(parsed.brokerAllowImage) != 1 || parsed.brokerAllowImage[0] != "postgres:15" {
		t.Fatalf("allow images = %#v", parsed.brokerAllowImage)
	}
	if parsed.brokerAllowPulls == nil || *parsed.brokerAllowPulls != allowPulls {
		t.Fatalf("allow pulls = %#v", parsed.brokerAllowPulls)
	}
	if !parsed.ready || !parsed.skipRepoChecks || parsed.format != "json" {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestLifecycleStatusRequiresExplicitReadiness(t *testing.T) {
	if lifecycleStatusRunsReadiness(lifecycleArgs{}) {
		t.Fatalf("status should be lightweight by default")
	}
	if !lifecycleStatusRunsReadiness(lifecycleArgs{ready: true}) {
		t.Fatalf("status --ready should run readiness")
	}
	if lifecycleStatusRunsReadiness(lifecycleArgs{ready: true, skipReady: true}) {
		t.Fatalf("--skip-ready should override --ready")
	}
}

func TestLifecyclePlanOptionsUsesOverlayNativeRoots(t *testing.T) {
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Paths:   compose.Paths{Root: "/home/me/dev/devkit"},
	}
	opts := lifecyclePlanOptions(ctx, config.OverlayConfig{
		Native: config.Native{
			WorktreeRoot:          "../native-worktrees",
			StateRoot:             "../native-state",
			WorktreeContainerRoot: "/native-worktrees",
			StateContainerRoot:    "/native-state",
		},
	}, lifecycleArgs{}, "ouroboros-ide", runtimebroker.Config{Socket: "/tmp/broker.sock"})
	if opts.WorktreeRoot != "/home/me/dev/native-worktrees" {
		t.Fatalf("worktree root = %q", opts.WorktreeRoot)
	}
	if opts.StateRoot != "/home/me/dev/native-state" {
		t.Fatalf("state root = %q", opts.StateRoot)
	}
	if opts.WorktreeContainerRoot != "/native-worktrees" || opts.StateContainerRoot != "/native-state" {
		t.Fatalf("container roots = %q %q", opts.WorktreeContainerRoot, opts.StateContainerRoot)
	}
}

func TestLifecyclePlanOptionsCLIOverridesOverlayNativeRoots(t *testing.T) {
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Paths:   compose.Paths{Root: "/home/me/dev/devkit"},
	}
	opts := lifecyclePlanOptions(ctx, config.OverlayConfig{
		Native: config.Native{
			WorktreeRoot: "../native-worktrees",
			StateRoot:    "../native-state",
		},
	}, lifecycleArgs{
		worktreeRoot:            "/tmp/wt",
		agentStateRoot:          "/tmp/state",
		worktreeContainerRoot:   "/wt",
		agentStateContainerRoot: "/state",
	}, "ouroboros-ide", runtimebroker.Config{Socket: "/tmp/broker.sock"})
	if opts.WorktreeRoot != "/tmp/wt" || opts.StateRoot != "/tmp/state" {
		t.Fatalf("host roots = %q %q", opts.WorktreeRoot, opts.StateRoot)
	}
	if opts.WorktreeContainerRoot != "/wt" || opts.StateContainerRoot != "/state" {
		t.Fatalf("container roots = %q %q", opts.WorktreeContainerRoot, opts.StateContainerRoot)
	}
}

func TestLifecycleBrokerConfigResolvesRelativeOverlaySocket(t *testing.T) {
	ctx := &cmdregistry.Context{Paths: compose.Paths{Root: "/home/me/dev/devkit"}}
	cfg := config.OverlayConfig{Broker: config.Broker{Socket: "../.devkit/native-broker/broker.sock"}}
	got := lifecycleBrokerConfig(ctx, cfg, lifecycleArgs{})
	if got.Socket != "/home/me/dev/.devkit/native-broker/broker.sock" {
		t.Fatalf("socket = %q", got.Socket)
	}
}

func TestLifecycleBrokerConfigDerivesStateRootForExplicitSocket(t *testing.T) {
	ctx := &cmdregistry.Context{Paths: compose.Paths{Root: "/home/me/dev/devkit"}}
	got := lifecycleBrokerConfig(ctx, config.OverlayConfig{}, lifecycleArgs{
		brokerSocket: "/tmp/devkit-smoke/broker.sock",
	})
	if got.Socket != "/tmp/devkit-smoke/broker.sock" {
		t.Fatalf("socket = %q", got.Socket)
	}
	if got.StateRoot != "/tmp/devkit-smoke" {
		t.Fatalf("state root = %q", got.StateRoot)
	}
}

func TestLifecycleBrokerConfigHonorsExplicitStateRootWithSocket(t *testing.T) {
	ctx := &cmdregistry.Context{Paths: compose.Paths{Root: "/home/me/dev/devkit"}}
	got := lifecycleBrokerConfig(ctx, config.OverlayConfig{}, lifecycleArgs{
		brokerSocket:    "/tmp/devkit-smoke/broker.sock",
		brokerStateRoot: "/tmp/devkit-state",
	})
	if got.StateRoot != "/tmp/devkit-state" {
		t.Fatalf("state root = %q", got.StateRoot)
	}
}

func TestApplyNativeConfigDefaultsUsesOverlayBrokerSocket(t *testing.T) {
	ctx := &cmdregistry.Context{Paths: compose.Paths{Root: "/home/me/dev/devkit"}}
	opts := nativeplan.BuildOptions{}
	applyNativeConfigDefaults(ctx, config.OverlayConfig{
		Broker: config.Broker{Socket: "../.devkit/native-broker/broker.sock"},
	}, &opts)
	if opts.BrokerEndpoint != "/home/me/dev/.devkit/native-broker/broker.sock" {
		t.Fatalf("broker endpoint = %q", opts.BrokerEndpoint)
	}
}

func TestLifecycleEnsureReadyBrokerStartsAndPropagatesReusedSocket(t *testing.T) {
	ctx := &cmdregistry.Context{DryRun: true}
	initial := runtimebroker.Config{
		Socket:        "/tmp/requested-broker.sock",
		StateRoot:     "/tmp/requested-state",
		AllowedImages: []string{"postgres:15"},
		AllowPulls:    true,
	}
	called := false
	gotCfg, status, err := lifecycleEnsureReadyBroker(ctx, lifecycleArgs{}, initial, func(_ context.Context, cfg runtimebroker.Config, dryRun bool) (runtimebroker.Status, error) {
		called = true
		if cfg.Socket != initial.Socket || cfg.StateRoot != initial.StateRoot {
			t.Fatalf("start cfg = %#v", cfg)
		}
		if len(cfg.AllowedImages) != 1 || cfg.AllowedImages[0] != "postgres:15" || !cfg.AllowPulls {
			t.Fatalf("broker policy cfg = %#v", cfg)
		}
		if !dryRun {
			t.Fatalf("expected dry-run broker start")
		}
		return runtimebroker.Status{Running: true, Socket: "/tmp/reused-broker.sock"}, nil
	})
	if err != nil {
		t.Fatalf("lifecycleEnsureReadyBroker: %v", err)
	}
	if !called {
		t.Fatalf("expected broker start")
	}
	if status == nil || !status.Running || status.Socket != "/tmp/reused-broker.sock" {
		t.Fatalf("status = %#v", status)
	}
	if gotCfg.Socket != "/tmp/reused-broker.sock" {
		t.Fatalf("propagated socket = %q", gotCfg.Socket)
	}
}

func TestLifecycleEnsureReadyBrokerSkipBrokerPreservesCurrentStateOnly(t *testing.T) {
	ctx := &cmdregistry.Context{}
	initial := runtimebroker.Config{Socket: "/tmp/current-state.sock"}
	gotCfg, status, err := lifecycleEnsureReadyBroker(ctx, lifecycleArgs{skipBroker: true}, initial, func(context.Context, runtimebroker.Config, bool) (runtimebroker.Status, error) {
		t.Fatalf("broker start should not run with --skip-broker")
		return runtimebroker.Status{}, nil
	})
	if err != nil {
		t.Fatalf("lifecycleEnsureReadyBroker: %v", err)
	}
	if status != nil {
		t.Fatalf("status = %#v", status)
	}
	if gotCfg.Socket != initial.Socket {
		t.Fatalf("socket = %q", gotCfg.Socket)
	}
}

func TestLifecycleEnsureReadyBrokerPropagatesSocketToPlanOptions(t *testing.T) {
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Paths:   compose.Paths{Root: "/home/me/dev/devkit"},
	}
	brokerCfg, _, err := lifecycleEnsureReadyBroker(ctx, lifecycleArgs{}, runtimebroker.Config{Socket: "/tmp/requested.sock"}, func(context.Context, runtimebroker.Config, bool) (runtimebroker.Status, error) {
		return runtimebroker.Status{Running: true, Socket: "/tmp/running.sock"}, nil
	})
	if err != nil {
		t.Fatalf("lifecycleEnsureReadyBroker: %v", err)
	}
	opts := lifecyclePlanOptions(ctx, config.OverlayConfig{}, lifecycleArgs{}, "ouroboros-ide", brokerCfg)
	if opts.BrokerEndpoint != "/tmp/running.sock" {
		t.Fatalf("broker endpoint = %q", opts.BrokerEndpoint)
	}
}

func TestLifecycleBrokerConfigWithStatusSocketPreservesEmptyStatusSocket(t *testing.T) {
	initial := runtimebroker.Config{Socket: "/tmp/configured.sock"}
	got := lifecycleBrokerConfigWithStatusSocket(initial, runtimebroker.Status{Running: true})
	if got.Socket != initial.Socket {
		t.Fatalf("socket = %q", got.Socket)
	}
}

func TestLifecycleBrokerConfigWithStatusSocketUsesRunningSocket(t *testing.T) {
	got := lifecycleBrokerConfigWithStatusSocket(
		runtimebroker.Config{Socket: "/tmp/configured.sock"},
		runtimebroker.Status{Running: true, Socket: "/tmp/running.sock"},
	)
	if got.Socket != "/tmp/running.sock" {
		t.Fatalf("socket = %q", got.Socket)
	}
}

func TestLifecycleReadinessErrorRequiresRepoReadinessByDefault(t *testing.T) {
	err := lifecycleReadinessError(capacity.Summary{
		Total:             1,
		RuntimeReady:      1,
		CapacityAvailable: 1,
		RepoReady:         0,
		Agents: []capacity.Agent{{
			Index:             1,
			RuntimeReady:      true,
			CapacityAvailable: true,
			RepoReady:         false,
			Checks: []readiness.Check{{
				Name:  "frontend-netlify-dev-server",
				Phase: readiness.PhaseRepo,
				OK:    false,
			}},
		}},
	}, lifecycleArgs{})
	if err == nil || err.Error() != "native repo readiness is not fully available" {
		t.Fatalf("err = %v", err)
	}
}

func TestLifecycleReadinessErrorAllowsSkippedRepoChecks(t *testing.T) {
	err := lifecycleReadinessError(capacity.Summary{
		Total:             1,
		RuntimeReady:      1,
		CapacityAvailable: 1,
		RepoReady:         0,
	}, lifecycleArgs{skipRepoChecks: true})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestRegisterTopLevelLifecycleAndNativeNamespace(t *testing.T) {
	reg := cmdregistry.New()
	Register(reg)
	for _, name := range []string{"native", "up", "down", "restart", "status", "logs", "scale", "ensure-ready", "exec", "attach"} {
		if _, ok := reg.Lookup(name); !ok {
			t.Fatalf("expected registered command %s", name)
		}
	}
}

func TestParseTopExecArgs(t *testing.T) {
	parsed, err := parseTopExecArgs(&cmdregistry.Context{Args: []string{"2", "--repo", "devkit", "--broker-socket", "/tmp/b.sock", "--", "git", "status"}}, false)
	if err != nil {
		t.Fatalf("parseTopExecArgs: %v", err)
	}
	if parsed.index != 2 || parsed.repo != "devkit" || parsed.brokerSocket != "/tmp/b.sock" {
		t.Fatalf("parsed = %#v", parsed)
	}
	if len(parsed.command) != 2 || parsed.command[0] != "git" || parsed.command[1] != "status" {
		t.Fatalf("command = %#v", parsed.command)
	}
}

func TestExitCodeFromError(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 7")
	err := cmd.Run()
	code, ok := exitCodeFromError(err)
	if !ok || code != 7 {
		t.Fatalf("exitCodeFromError = %d, %t; err=%v", code, ok, err)
	}
	if code, ok := exitCodeFromError(nil); ok || code != 0 {
		t.Fatalf("nil exitCodeFromError = %d, %t", code, ok)
	}
}

func TestTailLines(t *testing.T) {
	got := tailLines("a\nb\nc", 2)
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Fatalf("tail = %#v", got)
	}
}

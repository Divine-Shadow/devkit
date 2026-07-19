package nativecmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/devkitpaths"
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

func TestParseSandboxReadinessResultsPreservesPerCheckDetails(t *testing.T) {
	encodedOK := base64.StdEncoding.EncodeToString([]byte("tool ready\n"))
	encodedFail := base64.StdEncoding.EncodeToString([]byte("missing playwright\n"))
	out := strings.Join([]string{
		"nix warning that should be ignored",
		"__DEVKIT_READINESS_CHECK__\truntime\trequired-tools\t0\t" + encodedOK,
		"__DEVKIT_READINESS_CHECK__\trepo\tfrontend-test\t2\t" + encodedFail,
	}, "\n")
	results, err := parseSandboxReadinessResults(out)
	if err != nil {
		t.Fatalf("parseSandboxReadinessResults: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %#v", results)
	}
	if results[0].Phase != readiness.PhaseRuntime || results[0].Name != "required-tools" || !results[0].OK || results[0].Detail != "tool ready" {
		t.Fatalf("runtime result = %#v", results[0])
	}
	if results[1].Phase != readiness.PhaseRepo || results[1].Name != "frontend-test" || results[1].OK || !strings.Contains(results[1].Detail, "exit status 2: missing playwright") {
		t.Fatalf("repo result = %#v", results[1])
	}
}

func TestBuildSandboxReadinessScriptPreservesImmutableRuntimePath(t *testing.T) {
	checks := []sandboxReadinessCheck{{
		Phase:   readiness.PhaseRuntime,
		Name:    "playwright-browser",
		Command: "node playwright-check.js",
	}}
	command := sandboxReadinessCommand(checks)
	if len(command) != 3 || command[0] != "bash" || command[1] != "-c" {
		t.Fatalf("readiness batch must preserve the immutable runtime environment: %#v", command)
	}
	script := command[2]
	if !strings.Contains(script, `bash -c "$command"`) {
		t.Fatalf("readiness command did not preserve the selected runtime environment:\n%s", script)
	}
	if strings.Contains(script, `bash -lc "$command"`) {
		t.Fatalf("readiness command retained a login-shell PATH reinterpretation:\n%s", script)
	}
}

func TestGovernanceEnvHostDevRootPrefersWorkspaceBind(t *testing.T) {
	plan := nativeplan.Plan{
		DevkitHostRoot: "/tmp/devkit-worktree/devkit",
		Binds: []nativeplan.Bind{{
			Source:   "/workspaces/dev",
			Target:   "/workspaces/dev",
			Required: true,
		}},
	}

	if got := governanceEnvHostDevRoot(plan); got != "/workspaces/dev" {
		t.Fatalf("host dev root = %q, want /workspaces/dev", got)
	}
}

func TestGovernanceEnvHostDevRootFallsBackToDevkitParent(t *testing.T) {
	plan := nativeplan.Plan{DevkitHostRoot: "/workspaces/dev/devkit"}

	if got := governanceEnvHostDevRoot(plan); got != "/workspaces/dev" {
		t.Fatalf("host dev root = %q, want /workspaces/dev", got)
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
		Paths:   devkitpaths.Paths{OverlayPaths: []string{filepath.Join(tmp, "overlays")}},
	}
	checks, err := repoChecksFor(ctx, planArgs{})
	if err != nil {
		t.Fatalf("repoChecksFor: %v", err)
	}
	if len(checks) != 3 {
		t.Fatalf("checks = %#v", checks)
	}
	if checks[0].Name != "warm-hook" || checks[0].Command != "echo warm" {
		t.Fatalf("warm fallback = %#v", checks[0])
	}
	if checks[1].Name != "typecheck" || checks[1].Command != "npm test" {
		t.Fatalf("structured check = %#v", checks[1])
	}
	if checks[2].Name != "core-check" || checks[2].Command != "echo core" {
		t.Fatalf("core fallback = %#v", checks[2])
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
runtime:
  codex_version: 0.133.0
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Paths:   devkitpaths.Paths{OverlayPaths: []string{filepath.Join(tmp, "overlays")}},
	}
	checks, err := runtimeChecksFor(ctx)
	if err != nil {
		t.Fatalf("runtimeChecksFor: %v", err)
	}
	if len(checks) != 3 || checks[0].Name != "tools" || checks[0].Command != "command -v spago" {
		t.Fatalf("checks = %#v", checks)
	}
	if checks[1].Name != "runtime-check-2" || checks[1].Command != "command -v playwright" {
		t.Fatalf("defaulted check = %#v", checks[1])
	}
	if checks[2].Name != "codex-version" || checks[2].Command == "" {
		t.Fatalf("codex version check = %#v", checks[2])
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
		Paths:   devkitpaths.Paths{OverlayPaths: []string{filepath.Join(tmp, "overlays")}},
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
		"--runtime-only",
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
	if !parsed.ready || parsed.readinessMode != config.ReadinessModeRuntimeOnly || !parsed.readinessModeSet || parsed.format != "json" {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestParsePlanReadinessModeFlags(t *testing.T) {
	parsed, err := parsePlanArgs(&cmdregistry.Context{Args: []string{"readiness", "--runtime-only"}}, false, true)
	if err != nil {
		t.Fatalf("parsePlanArgs runtime-only: %v", err)
	}
	if parsed.readinessMode != config.ReadinessModeRuntimeOnly || !parsed.skipRepoChecks {
		t.Fatalf("runtime-only parsed = %#v", parsed)
	}
	parsed, err = parsePlanArgs(&cmdregistry.Context{Args: []string{"readiness", "--repo", "ouroboros-ide", "--repo-readiness"}}, false, true)
	if err != nil {
		t.Fatalf("parsePlanArgs repo-readiness: %v", err)
	}
	if parsed.opts.Repo != "ouroboros-ide" || parsed.readinessMode != config.ReadinessModeRepo || parsed.skipRepoChecks {
		t.Fatalf("repo-readiness parsed = %#v", parsed)
	}
	parsed, err = parsePlanArgs(&cmdregistry.Context{Args: []string{"readiness", "--repo"}}, false, true)
	if err != nil {
		t.Fatalf("parsePlanArgs bare repo mode: %v", err)
	}
	if parsed.readinessMode != config.ReadinessModeRepo || parsed.skipRepoChecks {
		t.Fatalf("bare repo mode parsed = %#v", parsed)
	}
}

func TestParseReadinessModeConflicts(t *testing.T) {
	if _, err := parsePlanArgs(&cmdregistry.Context{Args: []string{"readiness", "--runtime-only", "--repo-readiness"}}, false, true); err == nil {
		t.Fatalf("expected runtime-only/repo-readiness conflict")
	}
	if _, err := parsePlanArgs(&cmdregistry.Context{Args: []string{"readiness", "--runtime-only", "--repo-check", "echo ok"}}, false, true); err == nil {
		t.Fatalf("expected runtime-only/repo-check conflict")
	}
	if _, err := parseLifecycleArgs(&cmdregistry.Context{Args: []string{"--runtime-only", "--repo-readiness"}}); err == nil {
		t.Fatalf("expected lifecycle mode conflict")
	}
	if _, err := parseLifecycleArgs(&cmdregistry.Context{Args: []string{
		"--broker-binary", "/tmp/caller-selected-broker",
	}}); err == nil || !strings.Contains(err.Error(), "unknown native lifecycle arg --broker-binary") {
		t.Fatalf("caller-selected broker binary was not rejected: %v", err)
	}
}

func TestApplyLifecycleReadinessModeUsesOverlayDefault(t *testing.T) {
	parsed := lifecycleArgs{}
	if err := applyLifecycleReadinessMode(&parsed, config.OverlayConfig{
		Readiness: config.Readiness{DefaultMode: config.ReadinessModeRuntimeOnly},
	}); err != nil {
		t.Fatalf("applyLifecycleReadinessMode: %v", err)
	}
	if parsed.readinessMode != config.ReadinessModeRuntimeOnly || !parsed.skipRepoChecks {
		t.Fatalf("runtime default parsed = %#v", parsed)
	}
	parsed = lifecycleArgs{}
	if err := applyLifecycleReadinessMode(&parsed, config.OverlayConfig{
		Readiness: config.Readiness{DefaultMode: config.ReadinessModeRepo},
	}); err != nil {
		t.Fatalf("applyLifecycleReadinessMode repo: %v", err)
	}
	if parsed.readinessMode != config.ReadinessModeRepo || parsed.skipRepoChecks {
		t.Fatalf("repo default parsed = %#v", parsed)
	}
}

func TestApplyLifecycleReadinessModeRejectsInvalidDefault(t *testing.T) {
	err := applyLifecycleReadinessMode(&lifecycleArgs{}, config.OverlayConfig{
		Readiness: config.Readiness{DefaultMode: "sometimes"},
	})
	if err == nil || err.Error() != "readiness.default_mode must be runtime-only or repo" {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyLifecycleReadinessModeRejectsRepoCheckWithRuntimeOnly(t *testing.T) {
	parsed := lifecycleArgs{repoCheck: "echo ok", readinessMode: config.ReadinessModeRuntimeOnly, readinessModeSet: true}
	err := applyLifecycleReadinessMode(&parsed, config.OverlayConfig{})
	if err == nil || err.Error() != "--repo-check conflicts with runtime-only readiness" {
		t.Fatalf("err = %v", err)
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

func TestLifecycleStatusAttachCapacitySurfacesOperatorSummary(t *testing.T) {
	var runtimeOnly readiness.Report
	runtimeOnly.AddRuntime("sandbox-command", true, "")
	runtimeOnly.AddRepo("typecheck", false, "compile failed")

	var blocked readiness.Report
	blocked.AddRuntime("runtime-check-1", false, "spago missing")
	blocked.AddRepo("typecheck", true, "")

	summary := capacity.Build(map[int]readiness.Report{
		1: runtimeOnly,
		2: blocked,
	})
	status := lifecycleStatus{Command: "status", Runtime: "native"}
	status.attachCapacity(summary)

	if status.Status != "degraded" {
		t.Fatalf("status = %q", status.Status)
	}
	if status.Action == "" {
		t.Fatalf("expected lifecycle action")
	}
	if len(status.Failures) != 2 {
		t.Fatalf("failures = %#v", status.Failures)
	}

	text := captureStdout(t, func() error {
		return printLifecycleStatus(status, "text")
	})
	for _, want := range []string{
		"status: degraded",
		"capacity_status: degraded",
		"usable_capacity: 1",
		"agent1_status: status=degraded",
		"sandbox=ready",
		"repo=blocked",
		`agent1_failure: phase=repo name=typecheck detail="compile failed"`,
		"agent2_status: status=blocked",
		"tooling=blocked",
		`agent2_failure: phase=runtime name=runtime-check-1 detail="spago missing"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text output missing %q:\n%s", want, text)
		}
	}

	jsonOut := captureStdout(t, func() error {
		return printLifecycleStatus(status, "json")
	})
	for _, want := range []string{
		`"status": "degraded"`,
		`"usable_capacity": 1`,
		`"failed_checks"`,
		`"failures"`,
		`"sandbox_state": "ready"`,
		`"tooling_state": "blocked"`,
	} {
		if !strings.Contains(jsonOut, want) {
			t.Fatalf("json output missing %q:\n%s", want, jsonOut)
		}
	}
}

func TestLifecycleAttachBrokerStatusSurfacesStoppedAndStaleStates(t *testing.T) {
	stale := lifecycleStatus{}
	stale.attachBrokerStatus(runtimebroker.Status{StaleState: true, StateRoot: "/tmp/state"}, false)
	if stale.Status != "degraded" || stale.Action == "" || stale.Broker == nil {
		t.Fatalf("stale status = %#v", stale)
	}

	stoppedDown := lifecycleStatus{}
	stoppedDown.attachBrokerStatus(runtimebroker.Status{}, true)
	if stoppedDown.Status != "stopped" || stoppedDown.Action != "" {
		t.Fatalf("down status = %#v", stoppedDown)
	}

	stoppedStatus := lifecycleStatus{}
	stoppedStatus.attachBrokerStatus(runtimebroker.Status{}, false)
	if stoppedStatus.Status != "stopped" || stoppedStatus.Action == "" {
		t.Fatalf("status output = %#v", stoppedStatus)
	}
}

func TestLifecyclePlanOptionsUsesOverlayNativeRoots(t *testing.T) {
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Paths:   devkitpaths.Paths{Root: "/home/me/dev/devkit"},
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

func TestLifecyclePlanOptionsUsesOverlayRuntimeFlake(t *testing.T) {
	ctx := &cmdregistry.Context{
		Project: "pokeemerald",
		Paths:   devkitpaths.Paths{Root: "/home/me/dev/devkit"},
	}
	opts := lifecyclePlanOptions(ctx, config.OverlayConfig{
		Runtime: config.Runtime{Flake: "./overlays/pokeemerald#default"},
	}, lifecycleArgs{}, "pokeemerald", runtimebroker.Config{Socket: "/tmp/broker.sock"})
	if opts.Flake != "./overlays/pokeemerald#default" {
		t.Fatalf("flake = %q", opts.Flake)
	}
}

func TestLifecyclePlanOptionsResolvesFlakeInputOverrides(t *testing.T) {
	ctx := &cmdregistry.Context{
		Project: "ouroboros-terraform",
		Paths:   devkitpaths.Paths{Root: "/home/me/dev/devkit"},
	}
	opts := lifecyclePlanOptions(ctx, config.OverlayConfig{
		Runtime: config.Runtime{
			Flake: "./overlays/ouroboros-terraform#default",
			FlakeInputOverrides: map[string]string{
				"ouroboros-terraform": "../ouroboros-terraform",
			},
		},
	}, lifecycleArgs{}, "ouroboros-terraform", runtimebroker.Config{Socket: "/tmp/broker.sock"})
	if opts.FlakeInputOverrides["ouroboros-terraform"] != "path:/home/me/dev/ouroboros-terraform" {
		t.Fatalf("flake input overrides = %#v", opts.FlakeInputOverrides)
	}
}

func TestLifecyclePlanOptionsCLIFlakeOverridesOverlayRuntimeFlake(t *testing.T) {
	ctx := &cmdregistry.Context{
		Project: "pokeemerald",
		Paths:   devkitpaths.Paths{Root: "/home/me/dev/devkit"},
	}
	opts := lifecyclePlanOptions(ctx, config.OverlayConfig{
		Runtime: config.Runtime{Flake: "./overlays/pokeemerald#default"},
	}, lifecycleArgs{flake: ".#custom"}, "pokeemerald", runtimebroker.Config{Socket: "/tmp/broker.sock"})
	if opts.Flake != ".#custom" {
		t.Fatalf("flake = %q", opts.Flake)
	}
}

func TestLifecyclePlanOptionsCLIOverridesOverlayNativeRoots(t *testing.T) {
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Paths:   devkitpaths.Paths{Root: "/home/me/dev/devkit"},
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

func TestLifecyclePlanOptionsResolvesWorkspaceEgressProfileFromOverlay(t *testing.T) {
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Paths:   devkitpaths.Paths{Root: "/home/me/dev/devkit"},
	}
	opts := lifecyclePlanOptions(ctx, config.OverlayConfig{
		SourceDir: "/home/me/dev/ouroboros-ide/infra/ourchitect/overlay",
		Native: config.Native{
			IsolationProfiles: map[string]config.IsolationProfile{
				"workspace-egress": {
					Filesystem:      "workspace-only",
					EgressAllowlist: "../../docker/dev/tinyproxy/allowlist.txt",
					ProxySocket:     "../.devkit/native-egress/ouro-agent3.sock",
					Proxy:           "http://127.0.0.1:18888",
				},
			},
		},
	}, lifecycleArgs{isolationProfile: "workspace-egress"}, "ouroboros-ide", runtimebroker.Config{Socket: "/tmp/broker.sock"})
	if opts.IsolationProfile != "workspace-egress" {
		t.Fatalf("isolation profile = %q", opts.IsolationProfile)
	}
	wantAllowlist := "/home/me/dev/ouroboros-ide/infra/docker/dev/tinyproxy/allowlist.txt"
	if opts.EgressAllowlist != wantAllowlist {
		t.Fatalf("egress allowlist = %q, want %q", opts.EgressAllowlist, wantAllowlist)
	}
	if opts.ProxySocket != "/home/me/dev/.devkit/native-egress/ouro-agent3.sock" {
		t.Fatalf("proxy socket = %q", opts.ProxySocket)
	}
	if opts.Proxy != "http://127.0.0.1:18888" {
		t.Fatalf("proxy = %q", opts.Proxy)
	}
}

func TestLifecyclePlanOptionsEnforcesRequiredWorkspaceEgressProfile(t *testing.T) {
	ctx := &cmdregistry.Context{
		Project: "dev-workspace",
		Paths:   devkitpaths.Paths{Root: "/home/me/dev/devkit"},
	}
	opts := lifecyclePlanOptions(ctx, config.OverlayConfig{
		SourceDir: "/home/me/dev/devkit/overlays/dev-workspace",
		Native: config.Native{
			RequiredIsolationProfile: "workspace-egress",
			IsolationProfiles: map[string]config.IsolationProfile{
				"workspace-egress": {
					Filesystem:      "workspace-only",
					EgressAllowlist: "../../kit/proxy/allowlist.txt",
				},
			},
		},
	}, lifecycleArgs{isolationProfile: "none"}, ".", runtimebroker.Config{Socket: "/tmp/broker.sock"})
	if opts.IsolationProfile != "workspace-egress" {
		t.Fatalf("required isolation profile was bypassed: %q", opts.IsolationProfile)
	}
	if opts.EgressAllowlist != "/home/me/dev/devkit/kit/proxy/allowlist.txt" {
		t.Fatalf("egress allowlist = %q", opts.EgressAllowlist)
	}
}

func TestLifecyclePlanOptionsCLIAllowlistOverridesProfileAllowlist(t *testing.T) {
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Paths:   devkitpaths.Paths{Root: "/home/me/dev/devkit"},
	}
	opts := lifecyclePlanOptions(ctx, config.OverlayConfig{
		SourceDir: "/home/me/dev/ouroboros-ide/infra/ourchitect/overlay",
		Native: config.Native{
			IsolationProfiles: map[string]config.IsolationProfile{
				"workspace-egress": {EgressAllowlist: "../../docker/dev/tinyproxy/allowlist.txt"},
			},
		},
	}, lifecycleArgs{
		isolationProfile: "workspace-egress",
		egressAllowlist:  "/tmp/custom-allowlist.txt",
	}, "ouroboros-ide", runtimebroker.Config{Socket: "/tmp/broker.sock"})
	if opts.EgressAllowlist != "/tmp/custom-allowlist.txt" {
		t.Fatalf("egress allowlist = %q", opts.EgressAllowlist)
	}
}

func TestLifecyclePlanOptionsDiscoversRepoOwnedIsolationProfile(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	overlayDir := filepath.Join(devRoot, "ouroboros-ide", "infra", "ourchitect", "overlay")
	if err := os.MkdirAll(overlayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlayDir, "devkit.yaml"), []byte(""+
		"native:\n"+
		"  isolation_profiles:\n"+
		"    workspace-egress:\n"+
		"      filesystem: workspace-only\n"+
		"      egress_allowlist: ../../docker/dev/tinyproxy/allowlist.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Paths:   devkitpaths.Paths{Root: devkitRoot},
	}
	opts := lifecyclePlanOptions(ctx, config.OverlayConfig{}, lifecycleArgs{isolationProfile: "workspace-egress"}, "ouroboros-ide", runtimebroker.Config{Socket: "/tmp/broker.sock"})
	want := filepath.Join(devRoot, "ouroboros-ide", "infra", "docker", "dev", "tinyproxy", "allowlist.txt")
	if opts.EgressAllowlist != want {
		t.Fatalf("egress allowlist = %q, want %q", opts.EgressAllowlist, want)
	}
}

func TestLifecycleBrokerConfigResolvesRelativeOverlaySocket(t *testing.T) {
	t.Setenv("DEVKIT_RUNTIME_BROKER_BINARY", "/nix/store/example-postgres-broker/bin/postgres-broker")
	ctx := &cmdregistry.Context{Paths: devkitpaths.Paths{Root: "/home/me/dev/devkit"}}
	cfg := config.OverlayConfig{Broker: config.Broker{Socket: "../.devkit/native-broker/broker.sock"}}
	got := lifecycleBrokerConfig(ctx, cfg, lifecycleArgs{})
	if got.Socket != "/home/me/dev/.devkit/native-broker/broker.sock" {
		t.Fatalf("socket = %q", got.Socket)
	}
	if got.Binary != "/nix/store/example-postgres-broker/bin/postgres-broker" {
		t.Fatalf("broker binary = %q", got.Binary)
	}
}

func TestLifecyclePlanOptionsConsumesImmutableRuntimeExecutables(t *testing.T) {
	t.Setenv("DEVKIT_RUNTIME_SHELL_LAUNCHER", "/nix/store/runtime/bin/dev-all-runtime-shell")
	t.Setenv("DEVKIT_RUNTIME_BWRAP_BINARY", "/nix/store/bubblewrap/bin/bwrap")
	ctx := &cmdregistry.Context{Paths: devkitpaths.Paths{Root: "/home/me/dev/devkit"}}
	opts := lifecyclePlanOptions(
		ctx,
		config.OverlayConfig{},
		lifecycleArgs{},
		"ouroboros-ide",
		runtimebroker.Config{Socket: "/tmp/broker.sock"},
	)
	if opts.RuntimeLauncher != "/nix/store/runtime/bin/dev-all-runtime-shell" {
		t.Fatalf("runtime launcher = %q", opts.RuntimeLauncher)
	}
	if opts.BubblewrapBinary != "/nix/store/bubblewrap/bin/bwrap" {
		t.Fatalf("bubblewrap binary = %q", opts.BubblewrapBinary)
	}
}

func TestLifecycleBrokerConfigDerivesStateRootForExplicitSocket(t *testing.T) {
	ctx := &cmdregistry.Context{Paths: devkitpaths.Paths{Root: "/home/me/dev/devkit"}}
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
	ctx := &cmdregistry.Context{Paths: devkitpaths.Paths{Root: "/home/me/dev/devkit"}}
	got := lifecycleBrokerConfig(ctx, config.OverlayConfig{}, lifecycleArgs{
		brokerSocket:    "/tmp/devkit-smoke/broker.sock",
		brokerStateRoot: "/tmp/devkit-state",
	})
	if got.StateRoot != "/tmp/devkit-state" {
		t.Fatalf("state root = %q", got.StateRoot)
	}
}

func TestApplyNativeConfigDefaultsUsesOverlayBrokerSocket(t *testing.T) {
	ctx := &cmdregistry.Context{Paths: devkitpaths.Paths{Root: "/home/me/dev/devkit"}}
	opts := nativeplan.BuildOptions{}
	if err := applyNativeConfigDefaults(ctx, config.OverlayConfig{
		Broker: config.Broker{Socket: "../.devkit/native-broker/broker.sock"},
	}, &opts); err != nil {
		t.Fatalf("applyNativeConfigDefaults: %v", err)
	}
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
		Paths:   devkitpaths.Paths{Root: "/home/me/dev/devkit"},
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

func TestEnsureNativeLifecycleProjectAcceptsRuntimeFlakeOverlay(t *testing.T) {
	tmp := t.TempDir()
	overlay := filepath.Join(tmp, "overlays", "pokeemerald")
	if err := os.MkdirAll(overlay, 0o755); err != nil {
		t.Fatalf("mkdir overlay: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "devkit.yaml"), []byte(`
runtime:
  flake: ./overlays/pokeemerald#default
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ctx := &cmdregistry.Context{
		Project: "pokeemerald",
		Paths:   devkitpaths.Paths{OverlayPaths: []string{filepath.Join(tmp, "overlays")}},
	}
	if err := ensureNativeLifecycleProject(ctx); err != nil {
		t.Fatalf("ensureNativeLifecycleProject: %v", err)
	}
}

func TestEnsureNativeLifecycleProjectRejectsOverlayWithoutRuntimeFlake(t *testing.T) {
	tmp := t.TempDir()
	overlay := filepath.Join(tmp, "overlays", "missing-flake")
	if err := os.MkdirAll(overlay, 0o755); err != nil {
		t.Fatalf("mkdir overlay: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "devkit.yaml"), []byte(`service: dev-agent`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ctx := &cmdregistry.Context{
		Project: "missing-flake",
		Paths:   devkitpaths.Paths{OverlayPaths: []string{filepath.Join(tmp, "overlays")}},
	}
	err := ensureNativeLifecycleProject(ctx)
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := err.Error(); got != "native lifecycle requires runtime.flake for -p missing-flake; add a flake-backed runtime before using lifecycle commands" {
		t.Fatalf("err = %q", got)
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

func TestIsolateManagedEgressProxyForRunUsesLauncherOwnedSocket(t *testing.T) {
	sharedSocket := "/home/me/dev/.devkit/native-egress/dev-workspace-agent1-workspace-egress.sock"
	p := nativeplan.Plan{
		Proxy: nativeplan.ProxyConfig{
			UnixSocket:    sharedSocket,
			AllowlistPath: "/home/me/dev/devkit/kit/proxy/allowlist.txt",
		},
		Binds: []nativeplan.Bind{
			{Source: "/nix/store", Target: "/nix/store", Mode: "ro", Required: true},
			{Source: sharedSocket, Target: sharedSocket, Mode: "rw", Required: true},
		},
	}

	got, err := isolateManagedEgressProxyForRun(p, 4242)
	if err != nil {
		t.Fatalf("isolateManagedEgressProxyForRun: %v", err)
	}
	wantSocket := "/home/me/dev/.devkit/native-egress/.managed-egress-4242.sock"
	if got.Proxy.UnixSocket != wantSocket {
		t.Fatalf("proxy socket = %q, want %q", got.Proxy.UnixSocket, wantSocket)
	}
	if got.Binds[1].Source != wantSocket || got.Binds[1].Target != wantSocket {
		t.Fatalf("proxy bind = %#v", got.Binds[1])
	}
	if p.Proxy.UnixSocket != sharedSocket || p.Binds[1].Source != sharedSocket || p.Binds[1].Target != sharedSocket {
		t.Fatalf("input plan mutated: %#v", p)
	}
}

func TestIsolateManagedEgressProxyForRunRequiresExactSocketBind(t *testing.T) {
	p := nativeplan.Plan{
		Proxy: nativeplan.ProxyConfig{
			UnixSocket:    "/tmp/shared.sock",
			AllowlistPath: "/tmp/allowlist.txt",
		},
	}
	_, err := isolateManagedEgressProxyForRun(p, 9)
	if err == nil || !strings.Contains(err.Error(), "must appear exactly once") {
		t.Fatalf("err = %v", err)
	}
}

func TestManagedEgressUpstreamProxyURLOnlyChainsNestedWorkspaceEgress(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:18888")
	t.Setenv("HTTP_PROXY", "http://fallback.invalid:8080")
	t.Setenv("DEVKIT_NATIVE_ISOLATION_PROFILE", "")
	if got := managedEgressUpstreamProxyURL(); got != "" {
		t.Fatalf("non-isolated upstream proxy = %q", got)
	}
	t.Setenv("DEVKIT_NATIVE_ISOLATION_PROFILE", nativeplan.IsolationProfileWorkspaceEgress)
	if got := managedEgressUpstreamProxyURL(); got != "http://127.0.0.1:18888" {
		t.Fatalf("nested workspace-egress upstream proxy = %q", got)
	}
}

func TestWithManagedEgressProxyEstablishesSocketBeforeBootstrapAndCleansUp(t *testing.T) {
	t.Setenv("DEVKIT_NATIVE_ISOLATION_PROFILE", "")
	tmp, err := os.MkdirTemp("", "dke-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	allowlistPath := filepath.Join(tmp, "allowlist.txt")
	if err := os.WriteFile(allowlistPath, []byte("github.com\nssh.github.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(tmp, "managed-egress.sock")
	p := nativeplan.Plan{
		Proxy: nativeplan.ProxyConfig{
			UnixSocket:    socketPath,
			AllowlistPath: allowlistPath,
		},
	}
	called := false
	if err := withManagedEgressProxy(p, false, func() error {
		called = true
		if !unixSocketAccepts(socketPath) {
			t.Fatalf("managed proxy was not accepting before Git bootstrap")
		}
		return nil
	}); err != nil {
		t.Fatalf("withManagedEgressProxy: %v", err)
	}
	if !called {
		t.Fatal("Git bootstrap callback was not called")
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("managed proxy socket survived bootstrap: %v", err)
	}
}

func TestWithManagedEgressProxyCleansExactSocketWhenCallbackFails(t *testing.T) {
	t.Setenv("DEVKIT_NATIVE_ISOLATION_PROFILE", "")
	tmp := t.TempDir()
	allowlistPath := filepath.Join(tmp, "allowlist.txt")
	if err := os.WriteFile(allowlistPath, []byte("ssh.github.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(tmp, "managed-egress.sock")
	p := nativeplan.Plan{
		Proxy: nativeplan.ProxyConfig{
			UnixSocket:    socketPath,
			AllowlistPath: allowlistPath,
		},
	}
	sentinel := errors.New("bootstrap failed")
	err := withManagedEgressProxy(p, false, func() error {
		if !unixSocketAccepts(socketPath) {
			t.Fatal("managed proxy was not accepting before callback")
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("withManagedEgressProxy error = %v, want callback failure", err)
	}
	if unixSocketAccepts(socketPath) {
		t.Fatalf("managed proxy listener survived callback failure at %s", socketPath)
	}
	if _, err := os.Lstat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed proxy pathname survived callback failure: %v", err)
	}
}

func TestEnsureManagedEgressProxyRefusesArbitraryExistingListener(t *testing.T) {
	t.Setenv("DEVKIT_NATIVE_ISOLATION_PROFILE", "")
	tmp := t.TempDir()
	allowlistPath := filepath.Join(tmp, "allowlist.txt")
	if err := os.WriteFile(allowlistPath, []byte("ssh.github.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(tmp, "managed-egress.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	p := nativeplan.Plan{
		Proxy: nativeplan.ProxyConfig{
			UnixSocket:    socketPath,
			AllowlistPath: allowlistPath,
		},
	}
	_, err = ensureManagedEgressProxy(p, false)
	if err == nil || !strings.Contains(err.Error(), "refuses an existing listener") {
		t.Fatalf("ensureManagedEgressProxy error = %v, want existing listener rejection", err)
	}
	if !unixSocketAccepts(socketPath) {
		t.Fatal("existing listener was mutated while being refused")
	}
}

func TestRunCommandPreservingExitProjectsStdoutByteExactly(t *testing.T) {
	var stdout bytes.Buffer
	cmd := exec.Command("sh", "-c", "printf '%s' '__DEVKIT_RESULT__=PASS'")
	cmd.Stdout = &stdout
	if err := runCommandPreservingExit(cmd); err != nil {
		t.Fatalf("runCommandPreservingExit: %v", err)
	}
	if got := stdout.String(); got != "__DEVKIT_RESULT__=PASS" {
		t.Fatalf("stdout projection = %q", got)
	}
}

func TestTailLines(t *testing.T) {
	got := tailLines("a\nb\nc", 2)
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Fatalf("tail = %#v", got)
	}
}

func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	fnErr := fn()
	closeErr := w.Close()
	os.Stdout = old
	data, readErr := io.ReadAll(r)
	if err := r.Close(); err != nil {
		t.Fatalf("close read pipe: %v", err)
	}
	if fnErr != nil {
		t.Fatalf("captured function failed: %v", fnErr)
	}
	if closeErr != nil {
		t.Fatalf("close write pipe: %v", closeErr)
	}
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	return string(data)
}

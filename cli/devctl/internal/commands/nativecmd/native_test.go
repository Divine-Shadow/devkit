package nativecmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/devkitpaths"
	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	runtimebroker "devkit/cli/devctl/internal/runtime/broker"
	"devkit/cli/devctl/internal/runtime/capacity"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/runtime/readiness"
	wtx "devkit/cli/devctl/internal/worktrees"
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

func TestParsePlanArgsRecognizesManagedAppServerOnlyForExec(t *testing.T) {
	ctx := &cmdregistry.Context{Args: []string{"exec", "--managed-app-server", "--", "codex", "app-server"}}
	parsed, err := parsePlanArgs(ctx, true, false)
	if err != nil || !parsed.managedAppServer {
		t.Fatalf("managed app-server parse = %#v, %v", parsed, err)
	}
	if _, err := parsePlanArgs(ctx, false, false); err == nil || !strings.Contains(err.Error(), "only valid for native exec") {
		t.Fatalf("non-exec accepted managed app-server: %v", err)
	}
}

func TestParseTopExecArgsRecognizesManagedAppServerBeforeSeparator(t *testing.T) {
	ctx := &cmdregistry.Context{Args: []string{"2", "--managed-app-server", "--", "codex", "app-server"}}
	parsed, err := parseTopExecArgs(ctx, false)
	if err != nil || !parsed.managedAppServer || !reflect.DeepEqual(parsed.command, []string{"codex", "app-server"}) {
		t.Fatalf("top exec managed app-server parse = %#v, %v", parsed, err)
	}
}

func TestGUITargetIDPropagatesAcrossNativePlanAndExec(t *testing.T) {
	const targetID = "shadow-throne-management-2"
	plan, err := parsePlanArgs(&cmdregistry.Context{Args: []string{
		"plan", "--gui-target-id", targetID,
	}}, false, false)
	if err != nil || plan.opts.GUITargetID != targetID {
		t.Fatalf("native plan GUI target = %q, %v", plan.opts.GUITargetID, err)
	}
	nativeExec, err := parsePlanArgs(&cmdregistry.Context{Args: []string{
		"exec", "--gui-target-id", targetID, "--", "codex", "app-server",
	}}, true, false)
	if err != nil || nativeExec.opts.GUITargetID != targetID {
		t.Fatalf("native exec GUI target = %q, %v", nativeExec.opts.GUITargetID, err)
	}
	topExec, err := parseTopExecArgs(&cmdregistry.Context{Args: []string{
		"2", "--gui-target-id", targetID, "--", "codex", "app-server",
	}}, false)
	if err != nil || topExec.guiTargetID != targetID {
		t.Fatalf("top-level exec GUI target = %q, %v", topExec.guiTargetID, err)
	}
	opts := lifecyclePlanOptions(
		&cmdregistry.Context{Project: "dev-workspace", Paths: devkitpaths.Paths{Root: "/nix/store/example-devkit"}},
		config.OverlayConfig{Native: config.Native{HostRoot: "/home/bayesartre/dev"}},
		lifecycleArgs{guiTargetID: topExec.guiTargetID},
		"shadow-throne-management",
		runtimebroker.Config{Socket: "/tmp/broker.sock"},
	)
	if opts.GUITargetID != targetID {
		t.Fatalf("top-level exec planner GUI target = %q", opts.GUITargetID)
	}
}

func TestGUITargetIDFlagIsCanonicalAndHasNoSourceOrProfileCompanion(t *testing.T) {
	for _, args := range [][]string{
		{"plan", "--gui-target-id"},
		{"plan", "--gui-target-id", " "},
		{"plan", "--gui-target-id", "one", "--gui-target-id", "two"},
		{"readiness", "--gui-target-id", "one"},
		{"plan", "--config-profile", "management"},
		{"plan", "--config-source", "/nix/store/hostile"},
	} {
		if _, err := parsePlanArgs(&cmdregistry.Context{Args: args}, false, args[0] == "readiness"); err == nil {
			t.Fatalf("invalid GUI target authority was accepted: %#v", args)
		}
	}
	for _, args := range [][]string{
		{"2", "--gui-target-id"},
		{"2", "--gui-target-id", " "},
		{"2", "--gui-target-id", "one", "--gui-target-id", "two"},
	} {
		if _, err := parseTopExecArgs(&cmdregistry.Context{Args: args}, false); err == nil {
			t.Fatalf("invalid top-level GUI target authority was accepted: %#v", args)
		}
	}
}

func TestWorkspaceRootTranslationAcrossNativeCommands(t *testing.T) {
	const workspaceRoot = "/home/bayesartre/dev/control-plane-worktrees/agent2"
	plan, err := parsePlanArgs(&cmdregistry.Context{Args: []string{
		"plan", "--workspace-root", workspaceRoot,
	}}, false, false)
	if err != nil || plan.opts.WorkspaceRoot != workspaceRoot {
		t.Fatalf("native plan workspace root = %q, %v", plan.opts.WorkspaceRoot, err)
	}
	lifecycle, err := parseLifecycleArgs(&cmdregistry.Context{Args: []string{
		"--workspace-root", workspaceRoot,
	}})
	if err != nil || lifecycle.workspaceRoot != workspaceRoot {
		t.Fatalf("lifecycle workspace root = %q, %v", lifecycle.workspaceRoot, err)
	}
	execArgs, err := parseTopExecArgs(&cmdregistry.Context{Args: []string{
		"2", "--workspace-root", workspaceRoot, "--", "git", "status",
	}}, false)
	if err != nil || execArgs.workspaceRoot != workspaceRoot {
		t.Fatalf("exec workspace root = %q, %v", execArgs.workspaceRoot, err)
	}
	opts := lifecyclePlanOptions(
		&cmdregistry.Context{Project: "dev-workspace", Paths: devkitpaths.Paths{Root: "/nix/store/example-devkit"}},
		config.OverlayConfig{Native: config.Native{HostRoot: "/home/bayesartre/dev"}},
		lifecycleArgs{workspaceRoot: workspaceRoot},
		"shadow-throne-management",
		runtimebroker.Config{Socket: "/tmp/broker.sock"},
	)
	if opts.WorkspaceRoot != workspaceRoot {
		t.Fatalf("lifecycle planner workspace root = %q", opts.WorkspaceRoot)
	}
	for _, args := range [][]string{
		{"plan", "--workspace-root"},
		{"--workspace-root"},
		{"2", "--workspace-root"},
	} {
		var parseErr error
		switch args[0] {
		case "plan":
			_, parseErr = parsePlanArgs(&cmdregistry.Context{Args: args}, false, false)
		case "2":
			_, parseErr = parseTopExecArgs(&cmdregistry.Context{Args: args}, false)
		default:
			_, parseErr = parseLifecycleArgs(&cmdregistry.Context{Args: args})
		}
		if parseErr == nil {
			t.Fatalf("missing workspace root value was accepted: %#v", args)
		}
	}
}

func TestManagedAppServerRequiresDedicatedGUIServiceIdentity(t *testing.T) {
	if managedAppServerServiceIdentity != "fleet-gui-shadow-throne-management-2.service" {
		t.Fatalf("managed app-server service identity = %q", managedAppServerServiceIdentity)
	}
	if managedAppServerServiceIdentity == "fleet-controller-operation.service" {
		t.Fatal("transient operation broker must not be accepted as the durable GUI owner")
	}
	if !managedAppServerServiceOwnedWithCgroup(
		managedAppServerServiceIdentity,
		"invocation-id",
		"0::/system.slice/fleet-gui-shadow-throne-management-2.service",
	) {
		t.Fatal("exact managed GUI service cgroup was rejected")
	}
	if !managedAppServerServiceOwnedWithCgroup(
		managedAdditionalAppServerServiceIdentity,
		"invocation-id",
		"0::/user.slice/user-1000.slice/fleet-gui-shadow-throne-management.service",
	) {
		t.Fatal("additional declared managed GUI service cgroup was rejected")
	}
	for _, candidate := range []struct{ service, invocation, cgroup string }{
		{"fleet-controller-operation.service", "invocation-id", "0::/system.slice/fleet-controller-operation.service"},
		{managedAppServerServiceIdentity, "", "0::/system.slice/fleet-gui-shadow-throne-management-2.service"},
		{managedAppServerServiceIdentity, "invocation-id", "0::/user.slice/user-1000.slice"},
	} {
		if managedAppServerServiceOwnedWithCgroup(candidate.service, candidate.invocation, candidate.cgroup) {
			t.Fatalf("non-service managed app-server owner accepted: %#v", candidate)
		}
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

func TestLifecyclePlanOptionsResolvesInstalledFlakeInputOverridesFromLogicalHostRoot(t *testing.T) {
	ctx := &cmdregistry.Context{
		Project: "ouroboros-terraform",
		Paths: devkitpaths.Paths{
			Root:                 "/nix/store/example-devkit-runtime",
			RuntimeAuthorityRoot: "/nix/store/example-devkit-runtime",
		},
	}
	opts := lifecyclePlanOptions(ctx, config.OverlayConfig{
		Runtime: config.Runtime{
			Flake: "./overlays/ouroboros-terraform#default",
			FlakeInputOverrides: map[string]string{
				"ouroboros-terraform": "../ouroboros-terraform",
			},
		},
		Native: config.Native{
			HostRoot:                 "/home/bayesartre/dev",
			WorktreeRoot:             "/home/bayesartre/dev/agent-worktrees",
			StateRoot:                "/home/bayesartre/dev/.devkit/native-agents",
			RequiredIsolationProfile: nativeplan.IsolationProfileWorkspaceEgress,
			IsolationProfiles: map[string]config.IsolationProfile{
				nativeplan.IsolationProfileWorkspaceEgress: {
					Filesystem:      "workspace-only",
					EgressAllowlist: "/nix/store/example-devkit-runtime/kit/proxy/allowlist.txt",
				},
			},
		},
	}, lifecycleArgs{}, "ouroboros-terraform", runtimebroker.Config{Socket: "/tmp/broker.sock"})
	if opts.FlakeInputOverrides["ouroboros-terraform"] != "path:/home/bayesartre/dev/ouroboros-terraform" {
		t.Fatalf("flake input overrides = %#v", opts.FlakeInputOverrides)
	}
	p, err := nativeplan.BuildDevAll(opts)
	if err != nil {
		t.Fatalf("installed Terraform plan: %v", err)
	}
	want := "path:" + p.Agent.SandboxWorktree
	if p.FlakeInputOverrides["ouroboros-terraform"] != want {
		t.Fatalf("sandbox override = %q, want %q", p.FlakeInputOverrides["ouroboros-terraform"], want)
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

func devkitSourceRootForNativeTest(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if info, statErr := os.Stat(filepath.Join(root, "flake.nix")); statErr == nil && !info.IsDir() {
			if overlayInfo, overlayErr := os.Stat(filepath.Join(root, "overlays")); overlayErr == nil && overlayInfo.IsDir() {
				return root
			}
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("unable to locate Devkit source root")
		}
		root = parent
	}
}

func TestPackagedAdmittedNativeOverlaysDeclareAbsoluteGeometry(t *testing.T) {
	root := devkitSourceRootForNativeTest(t)
	overlaysRoot := filepath.Join(root, "overlays")
	entries, err := os.ReadDir(overlaysRoot)
	if err != nil {
		t.Fatalf("read packaged overlays: %v", err)
	}
	wantAdmitted := map[string]bool{
		"dev-all":                            true,
		"dev-workspace":                      true,
		"ouroboros-terraform":                true,
		"pokeemerald":                        true,
		"pokeemerald-expansion-shared-power": true,
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		cfg, _, err := config.ReadAll([]string{overlaysRoot}, name)
		if err != nil || !config.HasRuntimeFlake(cfg) {
			continue
		}
		if strings.TrimSpace(cfg.Native.RequiredIsolationProfile) == "" {
			if wantAdmitted[name] {
				t.Fatalf("packaged native overlay %s lost its admission profile", name)
			}
			continue
		}
		if !wantAdmitted[name] {
			t.Fatalf("unexpected packaged native overlay admitted without geometry audit: %s", name)
		}
		seen[name] = true
		for field, value := range map[string]string{
			"host_root":               cfg.Native.HostRoot,
			"worktree_root":           cfg.Native.WorktreeRoot,
			"state_root":              cfg.Native.StateRoot,
			"worktree_container_root": cfg.Native.WorktreeContainerRoot,
			"state_container_root":    cfg.Native.StateContainerRoot,
		} {
			if !filepath.IsAbs(strings.TrimSpace(value)) {
				t.Fatalf("packaged native overlay %s %s must be absolute, got %q", name, field, value)
			}
		}
	}
	for name := range wantAdmitted {
		if !seen[name] {
			t.Fatalf("expected packaged native overlay was not audited: %s", name)
		}
	}
}

func TestRealPackagedDevWorkspaceOverlayKeepsMutableGeometryOutsideRuntimeAuthority(t *testing.T) {
	root := devkitSourceRootForNativeTest(t)
	cfg, _, err := config.ReadAll([]string{filepath.Join(root, "overlays")}, "dev-workspace")
	if err != nil {
		t.Fatalf("read real dev-workspace overlay: %v", err)
	}
	packageRoot := "/nix/store/example-devkit-devctl"
	// Preserve the real parsed production configuration while modeling its
	// installed package location.
	cfg.SourceDir = filepath.Join(packageRoot, "overlays", "dev-workspace")
	ctx := &cmdregistry.Context{
		Project: "dev-workspace",
		Paths: devkitpaths.Paths{
			Root:                 packageRoot,
			RuntimeAuthorityRoot: packageRoot,
		},
	}
	if err := validateInstalledPackageNativeGeometry(ctx, cfg); err != nil {
		t.Fatalf("real packaged dev-workspace geometry rejected: %v", err)
	}
	brokerCfg := lifecycleBrokerConfig(ctx, cfg, lifecycleArgs{})
	opts := lifecyclePlanOptions(ctx, cfg, lifecycleArgs{}, ".", brokerCfg)
	p, err := nativeplan.BuildDevAll(opts)
	if err != nil {
		t.Fatalf("build real packaged dev-workspace plan: %v", err)
	}
	if p.Agent.HostWorktree != "/home/bayesartre/dev" {
		t.Fatalf("host worktree = %q", p.Agent.HostWorktree)
	}
	if p.HostWorktreeRoot != "/home/bayesartre/dev/agent-worktrees" {
		t.Fatalf("host worktree root = %q", p.HostWorktreeRoot)
	}
	if p.HostStateRoot != "/home/bayesartre/dev/.devkit/native-agents" {
		t.Fatalf("host state root = %q", p.HostStateRoot)
	}
	if p.Agent.SandboxWorktree != "/workspaces/dev" || p.SandboxStateRoot != "/agent-state" {
		t.Fatalf("sandbox geometry = worktree %q state %q", p.Agent.SandboxWorktree, p.SandboxStateRoot)
	}
	if p.BrokerEndpoint != "/home/bayesartre/dev/.devkit/native-broker/broker.sock" {
		t.Fatalf("broker endpoint = %q", p.BrokerEndpoint)
	}
	for _, bind := range p.Binds {
		if bind.Mode == "rw" && (bind.Source == "/nix/store" || strings.HasPrefix(bind.Source, "/nix/store/")) {
			t.Fatalf("real packaged dev-workspace plan exposes writable Nix-store bind: %#v", bind)
		}
	}
}

func TestBuildAgentPlanKeepsWorkspaceEgressAuthority(t *testing.T) {
	root := devkitSourceRootForNativeTest(t)
	ctx := &cmdregistry.Context{
		Project: "dev-workspace",
		Paths: devkitpaths.Paths{
			Root:                 root,
			RuntimeAuthorityRoot: root,
			OverlayPaths:         []string{filepath.Join(root, "overlays")},
		},
	}
	p, err := BuildAgentPlan(ctx, ".", 1)
	if err != nil {
		t.Fatalf("BuildAgentPlan: %v", err)
	}
	if p.IsolationProfile != nativeplan.IsolationProfileWorkspaceEgress {
		t.Fatalf("isolation profile = %q", p.IsolationProfile)
	}
	if p.RuntimeAuthorityRoot != root {
		t.Fatalf("runtime authority root = %q, want %q", p.RuntimeAuthorityRoot, root)
	}
	if !filepath.IsAbs(p.Proxy.UnixSocket) || !strings.Contains(p.Proxy.UnixSocket, "dev-workspace-agent1-workspace-egress.sock") {
		t.Fatalf("managed egress socket = %q", p.Proxy.UnixSocket)
	}
}

func TestInstalledPackageNativeGeometryRejectsMissingOrRelativeHostRoot(t *testing.T) {
	packageRoot := "/nix/store/example-devkit-devctl"
	ctx := &cmdregistry.Context{
		Project: "dev-workspace",
		Paths: devkitpaths.Paths{
			Root:                 packageRoot,
			RuntimeAuthorityRoot: packageRoot,
		},
	}
	base := config.OverlayConfig{
		SourceDir: filepath.Join(packageRoot, "overlays", "dev-workspace"),
		Runtime:   config.Runtime{Flake: "./overlays/dev-workspace#default"},
		Native: config.Native{
			RequiredIsolationProfile: "workspace-egress",
		},
	}
	for name, hostRoot := range map[string]string{"missing": "", "relative": "../dev"} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			cfg.Native.HostRoot = hostRoot
			err := validateInstalledPackageNativeGeometry(ctx, cfg)
			if err == nil || !strings.Contains(err.Error(), "requires an absolute native.host_root") && !strings.Contains(err.Error(), "native.host_root must be absolute") {
				t.Fatalf("host root %q produced %v", hostRoot, err)
			}
		})
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
	if got.StateRoot != "/home/me/dev/.devkit/native-broker" {
		t.Fatalf("state root = %q", got.StateRoot)
	}
	if got.Binary != "/nix/store/example-postgres-broker/bin/postgres-broker" {
		t.Fatalf("broker binary = %q", got.Binary)
	}
}

func TestLifecycleBrokerConfigInstalledHostRootKeepsSocketAndStateTogether(t *testing.T) {
	ctx := &cmdregistry.Context{Paths: devkitpaths.Paths{Root: "/nix/store/example-devkit/kit"}}
	cfg := config.OverlayConfig{
		Broker: config.Broker{Socket: ".devkit/native-broker/broker.sock"},
		Native: config.Native{HostRoot: "/home/bayesartre/dev"},
	}
	got := lifecycleBrokerConfig(ctx, cfg, lifecycleArgs{})
	if got.Socket != "/home/bayesartre/dev/.devkit/native-broker/broker.sock" {
		t.Fatalf("socket = %q", got.Socket)
	}
	if got.StateRoot != "/home/bayesartre/dev/.devkit/native-broker" {
		t.Fatalf("state root = %q", got.StateRoot)
	}
}

func TestRemoveOwnedBootstrapHomePreservesUnownedParents(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "agent3")
	home := filepath.Join(parent, ".devhome-agent3")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	owned, err := os.Lstat(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "partial"), []byte("failed bootstrap"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeOwnedBootstrapHome(home, owned); err != nil {
		t.Fatalf("removeOwnedBootstrapHome: %v", err)
	}
	if _, err := os.Lstat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned home still exists: %v", err)
	}
	if info, err := os.Lstat(parent); err != nil || !info.IsDir() {
		t.Fatalf("unowned parent was removed or changed: info=%v err=%v", info, err)
	}
}

func TestLifecyclePlanOptionsConsumesImmutableRuntimeExecutables(t *testing.T) {
	t.Setenv("DEVKIT_RUNTIME_SHELL_LAUNCHER", "/nix/store/runtime/bin/dev-all-runtime-shell")
	t.Setenv("DEVKIT_RUNTIME_BWRAP_BINARY", "/nix/store/bubblewrap/bin/bwrap")
	brokerBinary := "/nix/store/broker/bin/postgres-broker"
	ctx := &cmdregistry.Context{Paths: devkitpaths.Paths{Root: "/home/me/dev/devkit"}}
	opts := lifecyclePlanOptions(
		ctx,
		config.OverlayConfig{},
		lifecycleArgs{},
		"ouroboros-ide",
		runtimebroker.Config{Socket: "/tmp/broker.sock", Binary: brokerBinary},
	)
	if opts.RuntimeLauncher != "/nix/store/runtime/bin/dev-all-runtime-shell" {
		t.Fatalf("runtime launcher = %q", opts.RuntimeLauncher)
	}
	if opts.BubblewrapBinary != "/nix/store/bubblewrap/bin/bwrap" {
		t.Fatalf("bubblewrap binary = %q", opts.BubblewrapBinary)
	}
	if opts.BrokerBinary != brokerBinary {
		t.Fatalf("broker binary = %q", opts.BrokerBinary)
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

func TestLifecycleReadinessErrorIncludesFailedCheckDetails(t *testing.T) {
	err := lifecycleReadinessError(capacity.Summary{
		Total: 1,
		Agents: []capacity.Agent{{
			Index: 1,
			FailedChecks: []readiness.Check{{
				Phase:  readiness.PhaseRuntime,
				Name:   "product-governance-envelope",
				Detail: "broker socket is unavailable",
			}},
		}},
	}, lifecycleArgs{})
	if err == nil || err.Error() != "native capacity is not fully available: agent1 runtime/product-governance-envelope: broker socket is unavailable" {
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

func TestParseNativeSlotResetArgsRequiresExactTypedLaneInterface(t *testing.T) {
	workspaceRoot := "/home/bayesartre/dev/agent-worktrees/agent2"
	parsed, err := parseNativeSlotResetArgs(&cmdregistry.Context{Args: []string{
		"reset", "--repo", "ouroboros-ide", "--index", "2",
		"--workspace-root", workspaceRoot, "--gui-target-id", "shadow-4", "--format", "json",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.repo != "ouroboros-ide" || parsed.index != 2 ||
		parsed.workspaceRoot != workspaceRoot || parsed.guiTargetID != "shadow-4" || parsed.format != "json" {
		t.Fatalf("parsed = %#v", parsed)
	}
	for _, args := range [][]string{
		{"reset", "--repo", "ouroboros-ide", "--index", "1", "--workspace-root", workspaceRoot, "--format", "json", "--worktree-root", "/tmp/escape"},
		{"reset", "--repo", "ouroboros-ide", "--index", "1", "--workspace-root", workspaceRoot, "--format", "json", "--agent-state-root", "/tmp/escape"},
		{"reset", "--repo", "ouroboros-ide", "--index", "1", "--workspace-root", workspaceRoot, "--format", "json", "--base-branch", "other"},
		{"reset", "--repo", "ouroboros-ide", "--index", "1", "--workspace-root", workspaceRoot, "--format", "json", "--proxy-socket", "/tmp/proxy"},
	} {
		if _, err := parseNativeSlotResetArgs(&cmdregistry.Context{Args: args}); err == nil || !strings.Contains(err.Error(), "rejects caller lifecycle override") {
			t.Fatalf("args %v error = %v", args, err)
		}
	}
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing-workspace-root",
			args: []string{"reset", "--repo", "ouroboros-ide", "--index", "1", "--format", "json"},
			want: "requires --workspace-root",
		},
		{
			name: "missing-json-format",
			args: []string{"reset", "--repo", "ouroboros-ide", "--index", "1", "--workspace-root", workspaceRoot},
			want: "requires --format json",
		},
		{
			name: "text-format",
			args: []string{"reset", "--repo", "ouroboros-ide", "--index", "1", "--workspace-root", workspaceRoot, "--format", "text"},
			want: "--format must be json",
		},
		{
			name: "duplicate-workspace-root",
			args: []string{"reset", "--repo", "ouroboros-ide", "--index", "1", "--workspace-root", workspaceRoot, "--workspace-root", workspaceRoot, "--format", "json"},
			want: "rejects duplicate option --workspace-root",
		},
		{
			name: "empty-gui-target-id",
			args: []string{"reset", "--repo", "ouroboros-ide", "--index", "1", "--workspace-root", workspaceRoot, "--gui-target-id", " ", "--format", "json"},
			want: "--gui-target-id must be non-empty and canonical",
		},
		{
			name: "duplicate-gui-target-id",
			args: []string{"reset", "--repo", "ouroboros-ide", "--index", "1", "--workspace-root", workspaceRoot, "--gui-target-id", "shadow-4", "--gui-target-id", "shadow-4", "--format", "json"},
			want: "rejects duplicate option --gui-target-id",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseNativeSlotResetArgs(&cmdregistry.Context{Args: test.args}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("args %v error = %v, want %q", test.args, err, test.want)
			}
		})
	}
}

func TestNativeSlotResetLifecycleAlwaysSelectsImmutableGUIConfig(t *testing.T) {
	withoutTarget := nativeSlotResetLifecycleArgs(
		nativeSlotResetArgs{},
		"ouroboros-ide",
		"main",
		"agent",
	)
	if !withoutTarget.requireGUIConfig || withoutTarget.guiTargetID != "" {
		t.Fatalf("targetless selected reset did not require geometry-selected config: %#v", withoutTarget)
	}

	withTarget := nativeSlotResetLifecycleArgs(
		nativeSlotResetArgs{guiTargetID: "shadow-throne-local-3"},
		"ouroboros-ide",
		"main",
		"agent",
	)
	if withTarget.requireGUIConfig || withTarget.guiTargetID != "shadow-throne-local-3" {
		t.Fatalf("target-bound selected reset did not preserve exact-ID config selection: %#v", withTarget)
	}
	if withTarget.repo != "ouroboros-ide" || withTarget.baseBranch != "main" || withTarget.branchPrefix != "agent" {
		t.Fatalf("selected reset lifecycle authority drifted: %#v", withTarget)
	}
}

func TestNativeSlotWorkspaceRootConstrainsButDoesNotDeriveLifecycleGeometry(t *testing.T) {
	root := t.TempDir()
	worktreeRoot := filepath.Join(root, "agent-worktrees")
	workspaceRoot := filepath.Join(worktreeRoot, "agent3")
	hostWorktree := filepath.Join(workspaceRoot, "ouroboros-ide")
	if err := os.MkdirAll(hostWorktree, 0o700); err != nil {
		t.Fatal(err)
	}
	selectedPlan := nativeplan.Plan{
		DevkitHostRoot:   filepath.Join(root, "devkit"),
		HostWorktreeRoot: worktreeRoot,
		HostStateRoot:    filepath.Join(root, ".devkit", "native-agents"),
		Agent: nativeagent.Spec{
			HostWorktree: hostWorktree,
			HostHome:     filepath.Join(workspaceRoot, ".devhome-agent3"),
		},
	}
	got, err := validateNativeSlotWorkspaceRoot(workspaceRoot, selectedPlan)
	if err != nil {
		t.Fatal(err)
	}
	if got != workspaceRoot {
		t.Fatalf("validated workspace root = %s, want %s", got, workspaceRoot)
	}
	if selectedPlan.HostWorkspaceRoot != "" {
		t.Fatalf("caller constraint became lifecycle authority: %#v", selectedPlan)
	}

	for _, test := range []struct {
		name string
		root string
		plan nativeplan.Plan
		want string
	}{
		{
			name: "wrong-lane",
			root: filepath.Join(worktreeRoot, "agent2"),
			plan: selectedPlan,
			want: "selected source-derived worktree parent",
		},
		{
			name: "shared-collection",
			root: worktreeRoot,
			plan: selectedPlan,
			want: "shared collection root",
		},
		{
			name: "shared-dev-root",
			root: "/home/bayesartre/dev",
			plan: selectedPlan,
			want: "unsafe or shared",
		},
		{
			name: "windows-mount",
			root: "/mnt/c/Users/operator/dev",
			plan: selectedPlan,
			want: "unsafe or shared",
		},
		{
			name: "noncanonical",
			root: workspaceRoot + "/../agent3",
			plan: selectedPlan,
			want: "absolute and canonical",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := validateNativeSlotWorkspaceRoot(test.root, test.plan); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("workspace root %s error = %v, want %q", test.root, err, test.want)
			}
		})
	}

	missingRoot := filepath.Join(root, "missing-agent5")
	missingPlan := selectedPlan
	missingPlan.Agent.HostWorktree = filepath.Join(missingRoot, "ouroboros-ide")
	if _, err := validateNativeSlotWorkspaceRoot(missingRoot, missingPlan); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing workspace root error = %v", err)
	}

	realRoot := filepath.Join(root, "real-agent4")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkRoot := filepath.Join(root, "agent4")
	if err := os.Symlink(realRoot, symlinkRoot); err != nil {
		t.Fatal(err)
	}
	symlinkPlan := selectedPlan
	symlinkPlan.Agent.HostWorktree = filepath.Join(symlinkRoot, "ouroboros-ide")
	if _, err := validateNativeSlotWorkspaceRoot(symlinkRoot, symlinkPlan); err == nil || !strings.Contains(err.Error(), "traverses a symlink") {
		t.Fatalf("symlink workspace root error = %v", err)
	}
}

func TestNativeSlotResetJSONOutputReservation(t *testing.T) {
	status := nativeSlotResetStatus{
		Command: "reset", Runtime: "native", Repo: "ouroboros-ide", Index: 1,
		Status: "ready", Head: strings.Repeat("a", 40), ManifestPath: "/state/dev-all.json",
		Capacity: capacity.Summary{Total: 1, Status: "ready", RuntimeReady: 1, RepoReady: 1},
	}

	t.Run("restores stdout, bounds diagnostics, and emits one strict receipt", func(t *testing.T) {
		originalStdout := os.Stdout
		originalStderr := os.Stderr
		receiptReader, receiptWriter, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		diagnosticReader, diagnosticWriter, err := os.Pipe()
		if err != nil {
			_ = receiptReader.Close()
			_ = receiptWriter.Close()
			t.Fatal(err)
		}
		var output *nativeSlotResetJSONOutput
		t.Cleanup(func() {
			if output != nil {
				_ = output.close()
			}
			os.Stdout = originalStdout
			os.Stderr = originalStderr
			_ = receiptReader.Close()
			_ = receiptWriter.Close()
			_ = diagnosticReader.Close()
			_ = diagnosticWriter.Close()
		})

		type readResult struct {
			data []byte
			err  error
		}
		diagnosticResult := make(chan readResult, 1)
		go func() {
			data, err := io.ReadAll(diagnosticReader)
			diagnosticResult <- readResult{data: data, err: err}
		}()
		os.Stdout = receiptWriter
		os.Stderr = diagnosticWriter
		output, err = reserveNativeSlotResetJSONOutput("json")
		if err != nil {
			t.Fatal(err)
		}
		if output.receipt != receiptWriter || os.Stdout != output.diagnosticWriter {
			t.Fatal("JSON output reservation did not preserve the receipt writer and install its diagnostic pipe")
		}
		payload := bytes.Repeat([]byte("x"), nativeSlotResetJSONDiagnosticLimit+1024)
		if written, err := os.Stdout.Write(payload); err != nil || written != len(payload) {
			t.Fatalf("write reserved diagnostic stdout = %d, %v", written, err)
		}
		if err := finishNativeSlotResetOutput(output, status, "json"); err != nil {
			t.Fatal(err)
		}
		if os.Stdout != receiptWriter {
			t.Fatal("JSON output finalization did not restore stdout before writing its receipt")
		}

		os.Stdout = originalStdout
		os.Stderr = originalStderr
		if err := receiptWriter.Close(); err != nil {
			t.Fatal(err)
		}
		if err := diagnosticWriter.Close(); err != nil {
			t.Fatal(err)
		}
		receiptData, err := io.ReadAll(receiptReader)
		if err != nil {
			t.Fatal(err)
		}
		diagnostics := <-diagnosticResult
		if diagnostics.err != nil {
			t.Fatal(diagnostics.err)
		}
		wantDiagnostics := append(
			bytes.Repeat([]byte("x"), nativeSlotResetJSONDiagnosticLimit),
			[]byte(nativeSlotResetJSONDiagnosticTruncationMarker())...,
		)
		if !bytes.Equal(diagnostics.data, wantDiagnostics) {
			t.Fatalf("bounded diagnostic output length = %d, want %d", len(diagnostics.data), len(wantDiagnostics))
		}
		decoder := json.NewDecoder(bytes.NewReader(receiptData))
		var receipt nativeSlotResetStatus
		if err := decoder.Decode(&receipt); err != nil {
			t.Fatalf("decode reserved receipt: %v\n%s", err, receiptData)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			t.Fatalf("reserved receipt has trailing output: %v %#v\n%s", err, extra, receiptData)
		}
		if !reflect.DeepEqual(receipt, status) {
			t.Fatalf("reserved receipt = %#v, want %#v", receipt, status)
		}
	})

	t.Run("diagnostic close failure restores stdout and suppresses ready receipt", func(t *testing.T) {
		originalStdout := os.Stdout
		originalStderr := os.Stderr
		receiptReader, receiptWriter, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		diagnosticReader, diagnosticWriter, err := os.Pipe()
		if err != nil {
			_ = receiptReader.Close()
			_ = receiptWriter.Close()
			t.Fatal(err)
		}
		if err := diagnosticReader.Close(); err != nil {
			t.Fatal(err)
		}
		var output *nativeSlotResetJSONOutput
		t.Cleanup(func() {
			if output != nil {
				_ = output.close()
			}
			os.Stdout = originalStdout
			os.Stderr = originalStderr
			_ = receiptReader.Close()
			_ = receiptWriter.Close()
			_ = diagnosticWriter.Close()
		})

		os.Stdout = receiptWriter
		os.Stderr = diagnosticWriter
		output, err = reserveNativeSlotResetJSONOutput("json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stdout.Write([]byte("diagnostic projection must fail")); err != nil {
			t.Fatal(err)
		}
		finishErr := finishNativeSlotResetOutput(output, status, "json")
		restored := os.Stdout == receiptWriter
		os.Stdout = originalStdout
		os.Stderr = originalStderr
		_ = receiptWriter.Close()
		_ = diagnosticWriter.Close()
		receiptData, readErr := io.ReadAll(receiptReader)
		if finishErr == nil || !strings.Contains(finishErr.Error(), "finalize native reset diagnostic output") {
			t.Fatalf("diagnostic projection error = %v", finishErr)
		}
		if !restored {
			t.Fatal("diagnostic projection failure did not restore stdout")
		}
		if readErr != nil || len(receiptData) != 0 {
			t.Fatalf("diagnostic projection failure emitted receipt %q: %v", receiptData, readErr)
		}
	})

	t.Run("final receipt write failure is propagated", func(t *testing.T) {
		originalStdout := os.Stdout
		receiptReader, receiptWriter, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := receiptReader.Close(); err != nil {
			t.Fatal(err)
		}
		var output *nativeSlotResetJSONOutput
		t.Cleanup(func() {
			if output != nil {
				_ = output.close()
			}
			os.Stdout = originalStdout
			_ = receiptWriter.Close()
		})

		os.Stdout = receiptWriter
		output, err = reserveNativeSlotResetJSONOutput("json")
		if err != nil {
			t.Fatal(err)
		}
		finishErr := finishNativeSlotResetOutput(output, status, "json")
		restored := os.Stdout == receiptWriter
		os.Stdout = originalStdout
		_ = receiptWriter.Close()
		if finishErr == nil || !strings.Contains(finishErr.Error(), "write native reset json receipt") {
			t.Fatalf("receipt write error = %v", finishErr)
		}
		if !restored {
			t.Fatal("receipt write failure did not leave stdout restored")
		}
	})
}

func TestPlanNativeSlotProcessesSelectsOnlyExactSlotIdentity(t *testing.T) {
	originalProcRoot := nativeSlotProcRoot
	t.Cleanup(func() { nativeSlotProcRoot = originalProcRoot })
	nativeSlotProcRoot = t.TempDir()
	identity := nativeSlotProcessIdentity{
		index:           1,
		hostWorktree:    "/host/worktrees/agent1/ouroboros-ide",
		sandboxWorktree: "/workspaces/dev/agent-worktrees/agent1/ouroboros-ide",
		hostHome:        "/host/worktrees/agent1/ouroboros-ide/.devhome-agent1",
		sandboxHome:     "/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1",
		stateRoot:       "/host/state/dev-all-agent1",
		sandboxState:    "/agent-state/dev-all-agent1",
	}
	writeProc := func(pid, ppid int, env map[string]string, cwd string) {
		t.Helper()
		root := filepath.Join(nativeSlotProcRoot, strconv.Itoa(pid))
		if err := os.MkdirAll(filepath.Join(root, "fd"), 0o700); err != nil {
			t.Fatal(err)
		}
		var fields []string
		for key, value := range env {
			fields = append(fields, key+"="+value)
		}
		if err := os.WriteFile(filepath.Join(root, "environ"), []byte(strings.Join(fields, "\x00")+"\x00"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "stat"), []byte(fmt.Sprintf("%d (fixture) S %d 0 0 0\n", pid, ppid)), 0o600); err != nil {
			t.Fatal(err)
		}
		if cwd != "" {
			if err := os.Symlink(cwd, filepath.Join(root, "cwd")); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeProc(101, 1, map[string]string{
		"DEVKIT_NATIVE_AGENT": "1",
		"HOME":                identity.sandboxHome,
		"CODEX_HOME":          "/agent-state/dev-all-agent1/governed-codex-homes/implementer",
	}, identity.sandboxWorktree)
	writeProc(102, 1, map[string]string{
		"DEVKIT_NATIVE_AGENT": "2",
		"HOME":                "/workspaces/dev/agent-worktrees/agent2/.devhome-agent2",
		"CODEX_HOME":          "/workspaces/dev/agent-worktrees/agent2/.devhome-agent2/.codex",
	}, "/workspaces/dev/agent-worktrees/agent2/ouroboros-ide")
	// A legitimate child may sanitize its environment while retaining a cwd or
	// file descriptor in the slot. Descendant closure must happen before the
	// unowned-touch rejection.
	writeProc(103, 101, map[string]string{}, identity.sandboxWorktree)
	plan, err := planNativeSlotProcesses(identity, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(plan.pids); got != "[103 101]" {
		t.Fatalf("selected pids = %s", got)
	}

	for _, candidate := range []struct {
		name string
		pid  int
		env  map[string]string
	}{
		{
			name: "wrong agent",
			pid:  104,
			env: map[string]string{
				"DEVKIT_NATIVE_AGENT": "2",
				"HOME":                identity.sandboxHome,
				"CODEX_HOME":          "/agent-state/dev-all-agent1/governed-codex-homes/implementer",
			},
		},
		{
			name: "wrong home",
			pid:  105,
			env: map[string]string{
				"DEVKIT_NATIVE_AGENT": "1",
				"HOME":                "/workspaces/dev/agent-worktrees/agent2/.devhome-agent2",
				"CODEX_HOME":          "/agent-state/dev-all-agent1/governed-codex-homes/implementer",
			},
		},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			writeProc(candidate.pid, 1, candidate.env, identity.hostWorktree)
			t.Cleanup(func() {
				_ = os.RemoveAll(filepath.Join(nativeSlotProcRoot, strconv.Itoa(candidate.pid)))
			})
			if _, err := planNativeSlotProcesses(identity, true); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("unowned active process %d", candidate.pid)) {
				t.Fatalf("unowned process error = %v", err)
			}
			if err := os.RemoveAll(filepath.Join(nativeSlotProcRoot, strconv.Itoa(candidate.pid))); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeProcessStillMatchesRequiresStableStartTimeIdentity(t *testing.T) {
	originalProcRoot := nativeSlotProcRoot
	t.Cleanup(func() { nativeSlotProcRoot = originalProcRoot })
	nativeSlotProcRoot = t.TempDir()
	const pid = 201
	procRoot := filepath.Join(nativeSlotProcRoot, strconv.Itoa(pid))
	if err := os.MkdirAll(procRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	writeStat := func(start string) {
		t.Helper()
		fields := []string{"S", "1"}
		for len(fields) < 20 {
			fields = append(fields, "0")
		}
		fields[19] = start
		if err := os.WriteFile(
			filepath.Join(procRoot, "stat"),
			[]byte(fmt.Sprintf("%d (fixture) %s\n", pid, strings.Join(fields, " "))),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
	}

	writeStat("4242")
	if matches, err := nativeProcessStillMatches(pid, "4242"); err != nil || !matches {
		t.Fatalf("stable process identity = %v, %v", matches, err)
	}
	if matches, err := nativeProcessStillMatches(pid, "4343"); err == nil || matches || !strings.Contains(err.Error(), "was replaced") {
		t.Fatalf("changed process identity = %v, %v", matches, err)
	}
	if err := os.WriteFile(filepath.Join(procRoot, "stat"), []byte(fmt.Sprintf("%d (fixture) S 1\n", pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	if matches, err := nativeProcessStillMatches(pid, "4242"); err == nil || matches || !strings.Contains(err.Error(), "no stable start-time identity") {
		t.Fatalf("missing process identity = %v, %v", matches, err)
	}
	if err := os.RemoveAll(procRoot); err != nil {
		t.Fatal(err)
	}
	if matches, err := nativeProcessStillMatches(pid, "4242"); err != nil || matches {
		t.Fatalf("absent process identity = %v, %v", matches, err)
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

func TestNativeSlotResetLockSerializesPackageOwnedDisposal(t *testing.T) {
	coordinationRoot := filepath.Join(t.TempDir(), "worktrees", ".devkit", "git")
	first, err := acquireNativeSlotResetLock(coordinationRoot, "dev-all")
	if err != nil {
		t.Fatal(err)
	}
	if first.path != filepath.Join(coordinationRoot, ".dev-all-native-reset.lock") {
		t.Fatalf("native reset lock path = %q", first.path)
	}
	second, err := acquireNativeSlotResetLock(coordinationRoot, "dev-all")
	if err == nil || !strings.Contains(err.Error(), "another package-owned native reset is active") {
		if second != nil {
			_ = second.release()
		}
		t.Fatalf("second reset lock error = %v", err)
	}
	if err := first.release(); err != nil {
		t.Fatal(err)
	}
	reacquired, err := acquireNativeSlotResetLock(coordinationRoot, "dev-all")
	if err != nil {
		t.Fatalf("reacquire native reset lock: %v", err)
	}
	if err := reacquired.release(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeSlotMutationPreflightChecksEveryProductionRootAndLeavesNoProbe(t *testing.T) {
	root := t.TempDir()
	valid := filepath.Join(root, "valid")
	if err := os.MkdirAll(valid, 0o700); err != nil {
		t.Fatal(err)
	}
	notDirectory := filepath.Join(root, "not-directory")
	if err := os.WriteFile(notDirectory, []byte("no\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(root, "symlink")
	if err := os.Symlink(valid, symlink); err != nil {
		t.Fatal(err)
	}
	err := preflightNativeSlotMutationRoots([]nativeSlotMutationRoot{
		{name: "selected-worktree-parent", path: valid},
		{name: "shared-git-coordination", path: notDirectory},
		{name: "shared-runtime-broker", path: symlink},
	})
	if err == nil {
		t.Fatal("native slot mutation preflight accepted invalid production roots")
	}
	for _, want := range []string{"shared-git-coordination", "shared-runtime-broker"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("native slot mutation preflight did not report %s: %v", want, err)
		}
	}
	entries, err := os.ReadDir(valid)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("native slot mutation preflight left residue: %#v", entries)
	}
}

func TestNativeSlotMutationPreflightExercisesStageRollbackAndDeleteChildAuthority(t *testing.T) {
	originalRename := nativeSlotMutationRename
	originalRemoveAll := nativeSlotMutationRemoveAll
	t.Cleanup(func() {
		nativeSlotMutationRename = originalRename
		nativeSlotMutationRemoveAll = originalRemoveAll
	})

	t.Run("stage denied", func(t *testing.T) {
		root := t.TempDir()
		nativeSlotMutationRename = func(oldPath, newPath string) error {
			return &os.PathError{Op: "rename", Path: oldPath, Err: os.ErrPermission}
		}
		nativeSlotMutationRemoveAll = os.RemoveAll
		err := preflightNativeSlotMutationRoots([]nativeSlotMutationRoot{{name: "selected-worktree-parent", path: root}})
		if err == nil || !strings.Contains(err.Error(), "stage existing candidate") {
			t.Fatalf("stage-denied preflight error = %v", err)
		}
		entries, readErr := os.ReadDir(root)
		if readErr != nil || len(entries) != 0 {
			t.Fatalf("stage-denied preflight residue = %#v, %v", entries, readErr)
		}
	})

	t.Run("rollback denied", func(t *testing.T) {
		root := t.TempDir()
		calls := 0
		nativeSlotMutationRename = func(oldPath, newPath string) error {
			calls++
			if calls == 2 {
				return &os.PathError{Op: "rename", Path: oldPath, Err: os.ErrPermission}
			}
			return os.Rename(oldPath, newPath)
		}
		nativeSlotMutationRemoveAll = os.RemoveAll
		err := preflightNativeSlotMutationRoots([]nativeSlotMutationRoot{{name: "selected-state", path: root}})
		if err == nil || !strings.Contains(err.Error(), "roll back staged candidate") {
			t.Fatalf("rollback-denied preflight error = %v", err)
		}
		entries, readErr := os.ReadDir(root)
		if readErr != nil || len(entries) != 0 {
			t.Fatalf("rollback-denied preflight residue = %#v, %v", entries, readErr)
		}
	})

	t.Run("delete child denied", func(t *testing.T) {
		root := t.TempDir()
		nativeSlotMutationRename = os.Rename
		denied := true
		nativeSlotMutationRemoveAll = func(path string) error {
			if denied && strings.Contains(filepath.Base(path), ".devkit-native-reset-mutation-probe-") {
				return &os.PathError{Op: "removeall", Path: path, Err: os.ErrPermission}
			}
			return os.RemoveAll(path)
		}
		err := preflightNativeSlotMutationRoots([]nativeSlotMutationRoot{{name: "selected-state", path: root}})
		if err == nil || !strings.Contains(err.Error(), "discard existing candidate contents") {
			t.Fatalf("delete-child-denied preflight error = %v", err)
		}
		denied = false
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
	})
}

func TestNativeSlotMutationRootsCoverTheCompleteReconstructionContract(t *testing.T) {
	roots, err := nativeSlotMutationRoots(
		wtx.NativeSlotResetOptions{
			WorktreeRoot: "/home/bayesartre/dev/agent-worktrees",
		},
		nativeplan.Plan{
			Agent: nativeagent.Spec{
				HostWorktree: "/home/bayesartre/dev/agent-worktrees/agent3/ouroboros-ide",
				StateRoot:    "/home/bayesartre/dev/.devkit/native-agents/dev-all-agent3",
			},
			Proxy: nativeplan.ProxyConfig{
				UnixSocket: "/home/bayesartre/dev/.devkit/native-egress/dev-all-agent3-workspace-egress.sock",
			},
		},
		"/home/bayesartre/dev/.devkit/native-agents/manifests/dev-all.json",
		runtimebroker.Config{StateRoot: "/home/bayesartre/dev/.devkit/native-broker"},
	)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, root := range roots {
		got = append(got, root.name+"="+root.path)
	}
	want := []string{
		"selected-worktree-parent=/home/bayesartre/dev/agent-worktrees/agent3",
		"selected-state=/home/bayesartre/dev/.devkit/native-agents/dev-all-agent3",
		"shared-git-coordination=/home/bayesartre/dev/agent-worktrees/.devkit/git",
		"shared-native-manifest=/home/bayesartre/dev/.devkit/native-agents/manifests",
		"shared-runtime-broker=/home/bayesartre/dev/.devkit/native-broker",
		"managed-egress=/home/bayesartre/dev/.devkit/native-egress",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("native slot mutation roots =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
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

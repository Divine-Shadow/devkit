package readiness

import "testing"

func TestRepoFailureDoesNotHideRuntimeReadiness(t *testing.T) {
	var report Report
	report.AddRuntime("sandbox", true, "")
	report.AddRuntime("shell", true, "")
	report.AddRepo("warm-hook", false, "npm install failed")

	if !report.RuntimeReady() {
		t.Fatalf("runtime should stay ready when only repo readiness fails")
	}
	if report.RepoReady() {
		t.Fatalf("repo should not be ready")
	}
	if !report.CapacityAvailable() {
		t.Fatalf("capacity should depend on runtime checks only")
	}
	if got := report.Status(); got != "degraded" {
		t.Fatalf("status = %q", got)
	}
	if got := report.ComponentState("repo"); got != "blocked" {
		t.Fatalf("repo component = %q", got)
	}
	if report.Action() == "" {
		t.Fatalf("expected repo failure action")
	}
}

func TestRuntimeFailureBlocksCapacity(t *testing.T) {
	var report Report
	report.AddRuntime("sandbox", false, "bubblewrap failed")
	report.AddRepo("warm-hook", true, "")

	if report.RuntimeReady() {
		t.Fatalf("runtime should not be ready")
	}
	if report.CapacityAvailable() {
		t.Fatalf("capacity must be unavailable when runtime checks fail")
	}
	if got := report.Status(); got != "blocked" {
		t.Fatalf("status = %q", got)
	}
	if got := report.ComponentState("tooling"); got != "blocked" {
		t.Fatalf("tooling component = %q", got)
	}
}

func TestRepoChecksAreVisibleAndRetryable(t *testing.T) {
	var report Report
	report.AddRuntime("sandbox", true, "")
	report.AddRepo("typecheck", false, "sbt failed")

	if !report.HasRepoChecks() {
		t.Fatalf("expected repo check to be visible")
	}
	if len(report.Checks) != 2 {
		t.Fatalf("checks = %#v", report.Checks)
	}
	repoCheck := report.Checks[1]
	if repoCheck.RequiredForCapacity {
		t.Fatalf("repo check must not be required for capacity")
	}
	if !repoCheck.Retryable {
		t.Fatalf("repo check should be retryable")
	}
}

func TestRuntimeCheckMetadataIdentifiesOperatorComponents(t *testing.T) {
	var report Report
	report.AddRuntime("prepare-state", false, "missing worktree")
	report.AddRuntime("broker-socket", true, "")
	report.AddRuntime("sandbox-command", true, "")
	report.AddRepo("typecheck", true, "")

	failed := report.FailedChecks()
	if len(failed) != 1 {
		t.Fatalf("failed checks = %#v", failed)
	}
	if failed[0].Component != "worktree" {
		t.Fatalf("failed component = %q", failed[0].Component)
	}
	if failed[0].Action == "" {
		t.Fatalf("expected missing worktree action")
	}
	for component, want := range map[string]string{
		"worktree": "blocked",
		"broker":   "ready",
		"sandbox":  "ready",
		"tooling":  "unknown",
		"repo":     "ready",
	} {
		if got := report.ComponentState(component); got != want {
			t.Fatalf("%s component = %q, want %q", component, got, want)
		}
	}
}

func TestMissingRuntimeToolGetsToolingAction(t *testing.T) {
	var report Report
	report.AddRuntime("spago", false, "not found")

	failed := report.FailedChecks()
	if len(failed) != 1 {
		t.Fatalf("failed checks = %#v", failed)
	}
	if failed[0].Component != "tooling" {
		t.Fatalf("component = %q", failed[0].Component)
	}
	if failed[0].Action == "" {
		t.Fatalf("expected missing tool action")
	}
	if got := report.ComponentState("tooling"); got != "blocked" {
		t.Fatalf("tooling component = %q", got)
	}
}

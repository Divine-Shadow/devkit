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

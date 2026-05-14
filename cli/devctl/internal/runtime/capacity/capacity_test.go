package capacity

import (
	"testing"

	"devkit/cli/devctl/internal/runtime/readiness"
)

func TestBuildCountsRuntimeCapacitySeparatelyFromRepoReadiness(t *testing.T) {
	var runtimeOnly readiness.Report
	runtimeOnly.AddRuntime("sandbox-command", true, "")
	runtimeOnly.AddRepo("core-check", false, "compile failed")

	var blocked readiness.Report
	blocked.AddRuntime("runtime-check-1", false, "spago missing")
	blocked.AddRepo("core-check", true, "")

	summary := Build(map[int]readiness.Report{
		1: runtimeOnly,
		2: blocked,
	})

	if summary.Total != 2 {
		t.Fatalf("total = %d", summary.Total)
	}
	if summary.RuntimeReady != 1 {
		t.Fatalf("runtime ready = %d", summary.RuntimeReady)
	}
	if summary.RepoReady != 1 {
		t.Fatalf("repo ready = %d", summary.RepoReady)
	}
	if summary.CapacityAvailable != 1 {
		t.Fatalf("capacity available = %d", summary.CapacityAvailable)
	}
	if summary.UsableCapacity != 1 {
		t.Fatalf("usable capacity = %d", summary.UsableCapacity)
	}
	if summary.Status != "degraded" {
		t.Fatalf("status = %q", summary.Status)
	}
	if !sameInts(summary.BlockedAgents, []int{2}) {
		t.Fatalf("blocked agents = %#v", summary.BlockedAgents)
	}
	if !sameInts(summary.DegradedAgents, []int{1}) {
		t.Fatalf("degraded agents = %#v", summary.DegradedAgents)
	}
	if summary.Action == "" {
		t.Fatalf("expected summary action")
	}
	if len(summary.Agents) != 2 {
		t.Fatalf("agents = %#v", summary.Agents)
	}
	agent1 := summary.Agents[0]
	if agent1.Status != "degraded" || agent1.SandboxState != "ready" || agent1.RepoState != "blocked" || len(agent1.FailedChecks) != 1 {
		t.Fatalf("agent1 = %#v", agent1)
	}
	agent2 := summary.Agents[1]
	if agent2.Status != "blocked" || agent2.ToolingState != "blocked" || agent2.RepoState != "ready" || len(agent2.FailedChecks) != 1 {
		t.Fatalf("agent2 = %#v", agent2)
	}
}

func TestBuildEmptyCapacitySummary(t *testing.T) {
	summary := Build(nil)
	if summary.Status != "empty" || summary.UsableCapacity != 0 || summary.Total != 0 {
		t.Fatalf("summary = %#v", summary)
	}
}

func sameInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

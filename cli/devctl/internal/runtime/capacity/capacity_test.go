package capacity

import (
	"testing"

	"devkit/cli/devctl/internal/runtime/readiness"
)

func TestBuildCountsRuntimeCapacitySeparatelyFromRepoReadiness(t *testing.T) {
	var runtimeOnly readiness.Report
	runtimeOnly.AddRuntime("sandbox", true, "")
	runtimeOnly.AddRepo("core-check", false, "compile failed")

	var blocked readiness.Report
	blocked.AddRuntime("sandbox", false, "bubblewrap failed")
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
}

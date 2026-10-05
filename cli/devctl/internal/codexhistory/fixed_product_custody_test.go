package codexhistory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixedFixture(t *testing.T) captureFixture {
	f := newCaptureFixture(t)
	f.options.Project = "product-agent"
	f.options.ResetKind = "fixed-product-slot-retirement"
	f.options.WorkspaceRoot = ""
	f.options.SandboxHome = "/var/lib/product-agent/home"
	return f
}

func TestFixedProductRetirementCapturesValidatedHistoryAndDetectsReentryMutation(t *testing.T) {
	f := fixedFixture(t)
	rel := "sessions/fixed.jsonl"
	createStateDatabase(t, f, filepath.Join(f.options.SandboxHome, ".codex", rel))
	rollout := filepath.Join(f.codexRoot, rel)
	writeTestFile(t, rollout, "{\"type\":\"session_meta\",\"payload\":{\"id\":\"gui-thread\"}}\n")
	writeTestFile(t, filepath.Join(f.codexRoot, "auth.json"), "excluded-credential-fixture")
	r, err := Capture(f.options)
	if err != nil || r.Status != "captured" || r.GUIRollouts != 1 {
		t.Fatalf("fixed capture: %+v %v", r, err)
	}
	m := readManifest(t, r.ManifestPath)
	if m.Project != "product-agent" || m.ResetKind != f.options.ResetKind || len(m.Files) != 2 {
		t.Fatalf("fixed manifest: %+v", m)
	}
	if err := VerifyCapturedSource(f.options, r.ManifestPath); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, rollout, "changed after capture\n")
	if err := VerifyCapturedSource(f.options, r.ManifestPath); err == nil {
		t.Fatal("post-capture source mutation was accepted")
	}
	if _, err := os.Stat(r.ManifestPath); err != nil {
		t.Fatal("captured custody was lost on refusal", err)
	}
}

func TestFixedProductRetirementRetainsExistingCorruptAndMissingRolloutRefusals(t *testing.T) {
	for _, kind := range []string{"corrupt", "missing-rollout"} {
		t.Run(kind, func(t *testing.T) {
			f := fixedFixture(t)
			if kind == "corrupt" {
				writeTestFile(t, filepath.Join(f.codexRoot, "state_5.sqlite"), "not a database")
			} else {
				createStateDatabase(t, f, filepath.Join(f.options.SandboxHome, ".codex/sessions/missing.jsonl"))
			}
			if _, err := Capture(f.options); err == nil {
				t.Fatal("invalid fixed-slot history accepted")
			}
			root, err := CustodyRoot(f.options.StateRoot, f.options.Project)
			if err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Join(root, "agent1"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("refusal published a generation: %v %v", entries, err)
			}
			if _, err := os.Stat(filepath.Join(f.codexRoot, "state_5.sqlite")); err != nil {
				t.Fatal("refusal changed source", err)
			}
		})
	}
}

func TestFixedProductRetirementDoesNotBroadenOrdinaryDevAllGeometry(t *testing.T) {
	f := newCaptureFixture(t)
	f.options.WorkspaceRoot = ""
	if err := validateOptions(f.options); err == nil || !strings.Contains(err.Error(), "requires a workspace root") {
		t.Fatal("ordinary selected-slot geometry weakened", err)
	}
	f.options.ResetKind = "fixed-product-slot-retirement"
	if err := validateOptions(f.options); err == nil {
		t.Fatal("dev-all admitted a fixed Product binding")
	}
	f.options.Project = "product-agent"
	f.options.AgentIndex = 3
	if err := validateOptions(f.options); err == nil {
		t.Fatal("undeclared fixed slot admitted")
	}
}

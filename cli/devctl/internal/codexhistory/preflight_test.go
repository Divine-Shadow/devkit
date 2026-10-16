package codexhistory

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPreflightUsesPrivateSQLiteAndRetainsMissingReferenceRefusal(t *testing.T) {
	f, _, member := retainedFixture(t)
	database := filepath.Join(f.codexRoot, "state_5.sqlite")
	before, err := os.ReadFile(database)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Preflight(f.options)
	got := requireMissingRolloutObservation(t, err)
	if got.MappedHistoryMember != member || got.RetainedCustody == nil || len(got.RetainedCustody.Matches) != 1 {
		t.Fatalf("pre-stop observation lost owning custody evidence: %+v", got)
	}
	after, err := os.ReadFile(database)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("preflight changed original SQLite")
	}
	if _, err := os.Stat(filepath.Join(f.codexRoot, filepath.FromSlash(member))); !os.IsNotExist(err) {
		t.Fatal("preflight restored or fabricated history")
	}
}

func TestPreflightPreservesArchivedReferenceRuleAndNeverClaimsColdCapture(t *testing.T) {
	for _, archived := range []string{"0", "1", "NULL"} {
		t.Run(archived, func(t *testing.T) {
			f := newCaptureFixture(t)
			database := createStateDatabase(t, f, filepath.Join(f.options.SandboxHome, ".codex/sessions/missing.jsonl"))
			runSQLite(t, f.sqlite, database, "ALTER TABLE threads ADD COLUMN archived; UPDATE threads SET archived = "+archived+";")
			result, err := Preflight(f.options)
			if archived == "1" {
				if err != nil || result.ColdCapture || result.Status != "metadata-checked" || result.GUIReferences != 1 {
					t.Fatalf("approved archived gap/preflight scope changed: %+v %v", result, err)
				}
			} else {
				requireMissingRolloutObservation(t, err)
			}
			root, _ := WorkspaceCustodyRoot(f.options.WorkspaceRoot, f.options.Project)
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("preflight created custody generation/root")
			}
		})
	}
}

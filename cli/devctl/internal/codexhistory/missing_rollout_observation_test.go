package codexhistory

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requireMissingRolloutObservation(t *testing.T, err error) missingRolloutObservation {
	t.Helper()
	var refusal *missingRolloutError
	if !errors.As(err, &refusal) {
		t.Fatalf("expected preserved missing-member refusal, got %v", err)
	}
	message := err.Error()
	if !strings.HasPrefix(message, "GUI rollout reference is missing from resumable history: ") {
		t.Fatal("original refusal prefix lost")
	}
	parts := strings.Split(message, "\ncodex_gui_history_refusal=")
	if len(parts) != 2 {
		t.Fatal("missing single structured refusal observation")
	}
	var got missingRolloutObservation
	if err := json.Unmarshal([]byte(parts[1]), &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != "devkit/codex-gui-history-refusal/v1" || got.Invariant != "captured-gui-rollout-member-absent" || got.CapturedMemberPresent || got.Resumability != "unclassified" {
		t.Fatalf("observation expanded admission or resumability: %+v", got)
	}
	if _, err := time.Parse(time.RFC3339Nano, got.ObservedAt); err != nil {
		t.Fatal("missing actual observation time", err)
	}
	if strings.Contains(message, testGUIObjective) || strings.Contains(message, "excluded-private-session-content") || strings.Contains(message, "excluded-private-credential") {
		t.Fatal("private content appeared in refusal")
	}
	return got
}

func TestMissingRolloutObservationUsesDeclaredMappingWithoutChangingRefusal(t *testing.T) {
	for _, mapping := range []string{"sandbox-home", "host-home", "workspace-projection"} {
		t.Run(mapping, func(t *testing.T) {
			f := newCaptureFixture(t)
			root := filepath.Join(f.options.SandboxHome, ".codex")
			if mapping == "host-home" {
				root = f.codexRoot
			} else if mapping == "workspace-projection" {
				var err error
				root, err = workspaceProjectedCodexRoot(f.options)
				if err != nil {
					t.Fatal(err)
				}
			}
			rel := "archived_sessions/missing.jsonl"
			createStateDatabase(t, f, filepath.Join(root, filepath.FromSlash(rel)))
			writeTestFile(t, filepath.Join(f.codexRoot, "auth.json"), "excluded-private-credential")
			_, err := Capture(f.options)
			got := requireMissingRolloutObservation(t, err)
			if got.SourceDatabase != "state_5.sqlite" || got.ThreadID != "gui-thread" || got.ReferenceMapping != mapping || got.MappedHistoryMember != rel || got.DeclaredSourceRoot != f.codexRoot || got.DeclaredSourceMember != "absent" || got.RowArchived != "unknown" || got.ArchiveObservation != "archived-column-unavailable" {
				t.Fatalf("wrong single-reference evidence: %+v", got)
			}
			root, err = WorkspaceCustodyRoot(f.options.WorkspaceRoot, f.options.Project)
			if err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Join(root, "agent1"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("refusal published a generation: %v %v", entries, err)
			}
			if b, err := os.ReadFile(filepath.Join(f.codexRoot, "auth.json")); err != nil || string(b) != "excluded-private-credential" {
				t.Fatal("source credential fixture changed")
			}
		})
	}
}

func TestArchivedMetadataDoesNotMakeMissingHistoryDispensable(t *testing.T) {
	for _, tc := range []struct{ name, value, state, observation string }{
		{"archived", "1", "archived", "captured-row-metadata"},
		{"unarchived", "0", "unarchived", "captured-row-metadata"},
		{"unknown-null", "NULL", "unknown", "row-value-unavailable"},
		{"unknown-text", "'not-a-flag'", "unknown", "row-value-unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCaptureFixture(t)
			database := createStateDatabase(t, f, filepath.Join(f.options.SandboxHome, ".codex/archived_sessions/missing.jsonl"))
			runSQLite(t, f.sqlite, database, "ALTER TABLE threads ADD COLUMN archived; UPDATE threads SET archived = "+tc.value+" WHERE id = 'gui-thread';")
			_, err := Capture(f.options)
			got := requireMissingRolloutObservation(t, err)
			if got.RowArchived != tc.state || got.ArchiveObservation != tc.observation {
				t.Fatalf("archive metadata = %+v, want %+v", got, tc)
			}
		})
	}
}

func TestCapturedMembershipAbsenceIsNotOriginalFileAbsence(t *testing.T) {
	f := newCaptureFixture(t)
	rel := "archived_sessions/present-original.jsonl"
	database := createStateDatabase(t, f, filepath.Join(f.options.SandboxHome, ".codex", filepath.FromSlash(rel)))
	writeTestFile(t, filepath.Join(f.codexRoot, filepath.FromSlash(rel)), "excluded-private-session-content")
	payload := filepath.Join(t.TempDir(), "payload")
	if err := os.Mkdir(payload, 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(database)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "state_5.sqlite"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	files := map[string]manifestFile{"state_5.sqlite": {Path: "state_5.sqlite", Bytes: int64(len(b))}}
	count, quarantine, err := validateSQLiteAndRollouts(f.sqlite, payload, files, f.options)
	got := requireMissingRolloutObservation(t, err)
	if count != 0 || len(quarantine) != 0 || got.DeclaredSourceMember != "present-regular" {
		t.Fatalf("source presence substituted for captured membership: %+v %d %v", got, count, quarantine)
	}
	if b, err := os.ReadFile(filepath.Join(f.codexRoot, filepath.FromSlash(rel))); err != nil || string(b) != "excluded-private-session-content" {
		t.Fatal("original fixture was read into or changed by diagnostic output")
	}
}

func TestDiagnosticMetadataFailureStaysUnknown(t *testing.T) {
	state, observation := observeGUIArchiveState(filepath.Join(t.TempDir(), "absent-sqlite"), "unused", "gui-thread")
	if state != "unknown" || observation != "query-unavailable" {
		t.Fatalf("failed diagnostic query classified a row: %q %q", state, observation)
	}
	if got := sourceMemberObservation(nil, os.ErrPermission); got != "unknown" {
		t.Fatalf("permission refusal became absence: %s", got)
	}
	if got := sourceMemberObservation(nil, nil); got != "unknown" {
		t.Fatalf("missing metadata became absence: %s", got)
	}
}

func TestArchiveObservationQuotesExactThreadIdentity(t *testing.T) {
	f := newCaptureFixture(t)
	database := createStateDatabase(t, f, filepath.Join(f.options.SandboxHome, ".codex/sessions/missing.jsonl"))
	id := "gui' OR 1=1 --"
	runSQLite(t, f.sqlite, database, "ALTER TABLE threads ADD COLUMN archived INTEGER; UPDATE threads SET archived = 0; INSERT INTO threads VALUES ("+sqlQuote(id)+", '/unused', 'vscode', 'excluded-private-session-content', 1);")
	state, observation := observeGUIArchiveState(f.sqlite, database, id)
	if state != "archived" || observation != "captured-row-metadata" {
		t.Fatalf("identity escaped its one-row query: %q %q", state, observation)
	}
}

func TestDiagnosticSourceObservationDoesNotFollowAliases(t *testing.T) {
	f := newCaptureFixture(t)
	outside := t.TempDir()
	writeTestFile(t, filepath.Join(outside, "private.jsonl"), "excluded-private-session-content")
	if err := os.Symlink(outside, filepath.Join(f.codexRoot, "archived_sessions")); err != nil {
		t.Fatal(err)
	}
	if got := observeDeclaredSourceMember(f.codexRoot, "archived_sessions/private.jsonl"); got != "unknown" {
		t.Fatalf("parent alias supplied a source observation: %s", got)
	}
	if err := os.MkdirAll(filepath.Join(f.codexRoot, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "private.jsonl"), filepath.Join(f.codexRoot, "sessions/link.jsonl")); err != nil {
		t.Fatal(err)
	}
	if got := observeDeclaredSourceMember(f.codexRoot, "sessions/link.jsonl"); got != "present-symlink" {
		t.Fatalf("final alias followed or hidden: %s", got)
	}
	if got := observeDeclaredSourceMember(f.codexRoot, "../private.jsonl"); got != "unknown" {
		t.Fatalf("escaping diagnostic member admitted: %s", got)
	}
	writeTestFile(t, filepath.Join(f.codexRoot, "auth.json"), "excluded-private-credential")
	if got := observeDeclaredSourceMember(f.codexRoot, "auth.json"); got != "unknown" {
		t.Fatalf("non-history member supplied metadata: %s", got)
	}
}

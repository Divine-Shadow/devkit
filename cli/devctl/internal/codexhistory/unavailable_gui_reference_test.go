package codexhistory

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func guiMissingFixture(t *testing.T, fixed bool) captureFixture {
	t.Helper()
	f := archivedMissingFixture(t, fixed)
	runSQLite(t, f.sqlite, filepath.Join(f.codexRoot, "state_5.sqlite"), "UPDATE threads SET archived=0 WHERE id='gui-thread';")
	return f
}

func TestV4PreservesActualSQLiteGoalsReferencesAndAvailableBodies(t *testing.T) {
	for _, kind := range []string{"native", "fixed", "legacy", "null", "text", "real", "blob", "mixed", "host-mapping", "workspace-mapping"} {
		t.Run(kind, func(t *testing.T) {
			f := guiMissingFixture(t, kind == "fixed")
			db := filepath.Join(f.codexRoot, "state_5.sqlite")
			observation, missing := "integer-0", 1
			switch kind {
			case "legacy":
				runSQLite(t, f.sqlite, db, "ALTER TABLE threads DROP COLUMN archived;")
				observation = "column-absent"
			case "null", "text", "real", "blob":
				value := map[string]string{"null": "NULL", "text": "'excluded-private-archive-value'", "real": "0.5", "blob": "x'736563726574'"}[kind]
				runSQLite(t, f.sqlite, db, "UPDATE threads SET archived="+value+" WHERE id='gui-thread';")
				observation = "non-boolean"
			case "mixed":
				runSQLite(t, f.sqlite, db, "INSERT INTO threads VALUES('older-thread',"+sqlQuote(filepath.Join(f.options.SandboxHome, ".codex/archived_sessions/second-absent.jsonl"))+",'vscode','excluded-private-second-goal',1);")
				missing = 2
			case "host-mapping", "workspace-mapping":
				root := f.codexRoot
				if kind == "workspace-mapping" {
					var err error
					root, err = workspaceProjectedCodexRoot(f.options)
					if err != nil {
						t.Fatal(err)
					}
				}
				runSQLite(t, f.sqlite, db, "UPDATE threads SET rollout_path="+sqlQuote(filepath.Join(root, "archived_sessions/absent.jsonl"))+" WHERE id='gui-thread';")
			}
			writeTestFile(t, filepath.Join(f.codexRoot, "auth.json"), "excluded-private-credential")
			before := map[string][]byte{}
			for _, rel := range []string{"state_5.sqlite", "goals_1.sqlite", "sessions/available.jsonl"} {
				before[rel] = mustProjectionRead(t, filepath.Join(f.codexRoot, rel))
			}
			preflight, err := Preflight(f.options)
			if err != nil || preflight.ColdCapture || preflight.Status != "metadata-checked" || preflight.GUIReferences != missing+1 {
				t.Fatalf("preflight claimed more than presence/metadata: %+v %v", preflight, err)
			}
			result, err := Capture(f.options)
			if err != nil {
				t.Fatal(err)
			}
			m := readManifest(t, result.ManifestPath)
			if m.SchemaVersion != GUIAvailableEvidenceSchema || m.Status != GUIAvailableEvidenceStatus || result.UnavailableGUIReferences != missing || m.UnavailableGUIReferenceCount != missing || len(m.UnavailableGUIReferences) != missing || len(m.UnavailableArchivedReferences) != 0 || result.UnavailableArchivedReferences != 0 || m.GUIRolloutCount != 1 || len(m.Files) != 3 {
				t.Fatalf("v4/body/archive counts confused: %+v %+v", result, m)
			}
			payload := filepath.Join(filepath.Dir(result.ManifestPath), "payload")
			for rel, original := range before {
				if !bytes.Equal(original, mustProjectionRead(t, filepath.Join(f.codexRoot, rel))) || !bytes.Equal(original, mustProjectionRead(t, filepath.Join(payload, rel))) {
					t.Fatalf("available original changed: %s", rel)
				}
			}
			for _, u := range m.UnavailableGUIReferences {
				if u.CustodyClass != "unavailable-gui-reference" || u.Resumability != "unclassified" || u.ResumeEligible || u.CapturedMemberPresent || u.DeclaredSourceMember != "absent" {
					t.Fatalf("invented body/resume warrant: %+v", u)
				}
				if u.ThreadID == "gui-thread" && u.ArchiveObservation != observation {
					t.Fatalf("wrong observation: %+v", u)
				}
				if u.ThreadID == "older-thread" && u.ArchiveObservation != "integer-1" {
					t.Fatal("mixed v4 mislabeled archived evidence", u)
				}
				if _, err := os.Lstat(filepath.Join(payload, u.MappedHistoryMember)); !os.IsNotExist(err) {
					t.Fatal("invented missing body", err)
				}
			}
			raw := mustProjectionRead(t, result.ManifestPath)
			for _, secret := range []string{"excluded-private-archive-value", "excluded-private-second-goal", "keep absent archive objective", "excluded-private-credential"} {
				if bytes.Contains(raw, []byte(secret)) {
					t.Fatal("private value escaped into manifest")
				}
			}
			if _, err := os.Stat(filepath.Join(payload, "auth.json")); !os.IsNotExist(err) {
				t.Fatal("captured credentials", err)
			}
			if kind == "fixed" {
				if err := VerifyCapturedSource(f.options, result.ManifestPath); err != nil {
					t.Fatal(err)
				}
				// Only this synthetic fixture's source is removed. Retained proof is source-free.
				if err := os.RemoveAll(f.options.HostHome); err != nil {
					t.Fatal(err)
				}
				if err := VerifyCapturedBundle(f.options, result.ManifestPath); err != nil {
					t.Fatal(err)
				}
				if err := VerifyCapturedSource(f.options, result.ManifestPath); err == nil {
					t.Fatal("deleted source accepted")
				}
			}
		})
	}
}

func TestV4StrictReaderRejectsChangedBindingsAndSchemaMasquerades(t *testing.T) {
	for _, mode := range []string{"class", "database", "thread", "reference", "member", "mapping", "archive", "presence", "absence", "resumability", "eligibility", "duplicate", "count", "v2", "v3", "downgrade-remove", "status", "total", "body-count", "missing-field", "unknown-field", "duplicate-field", "null-field", "duplicate-header", "case-alias", "mixed-class"} {
		t.Run(mode, func(t *testing.T) {
			f := guiMissingFixture(t, true)
			r, err := Capture(f.options)
			if err != nil {
				t.Fatal(err)
			}
			m := readManifest(t, r.ManifestPath)
			u := &m.UnavailableGUIReferences[0]
			switch mode {
			case "class":
				u.CustodyClass = "complete"
			case "database":
				u.SourceDatabase = "state_6.sqlite"
			case "thread":
				u.ThreadID = "other"
			case "reference":
				u.RolloutReference += "-other"
			case "member":
				u.MappedHistoryMember = "sessions/available.jsonl"
			case "mapping":
				u.ReferenceMapping = "wrong-mapping"
			case "archive":
				u.ArchiveObservation = "integer-1"
			case "presence":
				u.CapturedMemberPresent = true
			case "absence":
				u.DeclaredSourceMember = "unknown"
			case "resumability":
				u.Resumability = "nonresumable"
			case "eligibility":
				u.ResumeEligible = true
			case "duplicate":
				m.UnavailableGUIReferences = append(m.UnavailableGUIReferences, *u)
				m.UnavailableGUIReferenceCount++
			case "count":
				m.UnavailableGUIReferenceCount++
			case "v2":
				m.SchemaVersion = SnapshotSchema
				m.Status = "complete"
			case "v3":
				m.SchemaVersion = AvailableEvidenceSchema
				m.Status = AvailableEvidenceStatus
			case "downgrade-remove":
				m.SchemaVersion = SnapshotSchema
				m.Status = "complete"
				m.UnavailableGUIReferences = nil
				m.UnavailableGUIReferenceCount = 0
			case "status":
				m.Status = "complete"
			case "total":
				m.TotalBytes++
			case "body-count":
				m.GUIRolloutCount++
			case "mixed-class":
				m.UnavailableArchivedReferences = []unavailableArchivedReference{archivedReferenceFromGUI(*u)}
				m.UnavailableArchivedReferenceCount = 1
			}
			b, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "missing-field":
				b = bytes.Replace(b, []byte(`"resumability":"unclassified",`), nil, 1)
			case "unknown-field":
				b = bytes.Replace(b, []byte(`"resume_eligible":false`), []byte(`"resume_eligible":false,"extra":false`), 1)
			case "duplicate-field":
				b = bytes.Replace(b, []byte(`"resume_eligible":false`), []byte(`"resume_eligible":true,"resume_eligible":false`), 1)
			case "null-field":
				b = bytes.Replace(b, []byte(`"resume_eligible":false`), []byte(`"resume_eligible":null`), 1)
			case "duplicate-header":
				b = append([]byte(`{"schema_version":"complete",`), b[1:]...)
			case "case-alias":
				b = bytes.Replace(b, []byte(`"schema_version"`), []byte(`"SCHEMA_VERSION"`), 1)
			}
			if err := os.WriteFile(r.ManifestPath, b, 0o600); err != nil {
				t.Fatal(err)
			}
			if mode == "downgrade-remove" {
				if err := os.RemoveAll(f.options.HostHome); err != nil {
					t.Fatal(err)
				}
			}
			if err := VerifyCapturedBundle(f.options, r.ManifestPath); err == nil {
				t.Fatal("tampered v4 accepted")
			}
		})
	}
}

func TestV4ColdCaptureRefusesSourceRacesBeforeAtomicCompletion(t *testing.T) {
	for _, mode := range []string{"body-appeared", "row-write", "goal-write", "sidecar-appeared", "available-body-write", "available-body-removed"} {
		t.Run(mode, func(t *testing.T) {
			f := guiMissingFixture(t, false)
			captureBeforeCommitCheckpoint = func() error {
				switch mode {
				case "body-appeared":
					writeTestFile(t, filepath.Join(f.codexRoot, "archived_sessions/absent.jsonl"), "real new body")
				case "row-write":
					runSQLite(t, f.sqlite, filepath.Join(f.codexRoot, "state_5.sqlite"), "UPDATE threads SET title='changed objective' WHERE id='gui-thread';")
				case "goal-write":
					runSQLite(t, f.sqlite, filepath.Join(f.codexRoot, "goals_1.sqlite"), "UPDATE goals SET objective='changed goal';")
				case "sidecar-appeared":
					writeTestFile(t, filepath.Join(f.codexRoot, "state_5.sqlite-wal"), "changed cold sidecar")
				case "available-body-write":
					writeTestFile(t, filepath.Join(f.codexRoot, "sessions/available.jsonl"), "changed body")
				case "available-body-removed":
					return os.Remove(filepath.Join(f.codexRoot, "sessions/available.jsonl"))
				}
				return nil
			}
			t.Cleanup(func() { captureBeforeCommitCheckpoint = nil })
			if _, err := Capture(f.options); err == nil {
				t.Fatal("race crossed atomic completion")
			}
			captureBeforeCommitCheckpoint = nil
			root, _ := WorkspaceCustodyRoot(f.options.WorkspaceRoot, f.options.Project)
			entries, err := os.ReadDir(filepath.Join(root, "agent1"))
			if err != nil || len(entries) != 0 {
				t.Fatal("race published custody/staging", entries, err)
			}
		})
	}
}

func TestV4RefusesPhysicalAmbiguityAndNeverUsesSourcePresenceAsCapturedMembership(t *testing.T) {
	for _, mode := range []string{"parent-symlink", "final-symlink", "nonregular", "permission", "foreign-path", "duplicate-identity", "duplicate-member", "corrupt-goals"} {
		t.Run(mode, func(t *testing.T) {
			f := guiMissingFixture(t, false)
			rel := "archived_sessions/absent.jsonl"
			db := filepath.Join(f.codexRoot, "state_5.sqlite")
			switch mode {
			case "parent-symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(f.codexRoot, "archived_sessions")); err != nil {
					t.Fatal(err)
				}
			case "final-symlink":
				if err := os.MkdirAll(filepath.Join(f.codexRoot, "archived_sessions"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), filepath.Join(f.codexRoot, rel)); err != nil {
					t.Fatal(err)
				}
			case "nonregular":
				if err := os.MkdirAll(filepath.Join(f.codexRoot, rel), 0o700); err != nil {
					t.Fatal(err)
				}
			case "permission":
				if os.Geteuid() == 0 {
					t.Skip("actual EACCES is exercised by the non-root Nix test builder")
				}
				parent := filepath.Join(f.codexRoot, "archived_sessions")
				if err := os.MkdirAll(parent, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(parent, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
			case "foreign-path":
				runSQLite(t, f.sqlite, db, "UPDATE threads SET rollout_path='/foreign/private/sessions/absent.jsonl' WHERE id='gui-thread';")
			case "duplicate-identity":
				if err := os.WriteFile(filepath.Join(f.codexRoot, "state_6.sqlite"), mustProjectionRead(t, db), 0o600); err != nil {
					t.Fatal(err)
				}
			case "duplicate-member":
				runSQLite(t, f.sqlite, db, "UPDATE threads SET rollout_path=(SELECT rollout_path FROM threads WHERE id='gui-thread') WHERE id='available-thread';")
			case "corrupt-goals":
				writeTestFile(t, filepath.Join(f.codexRoot, "goals_1.sqlite"), "invalid SQLite")
			}
			if _, err := Preflight(f.options); err == nil {
				t.Fatal("physical/SQLite ambiguity passed preflight")
			}
			if _, err := Capture(f.options); err == nil {
				t.Fatal("physical/SQLite ambiguity became v4")
			}
		})
	}
	f := guiMissingFixture(t, false)
	rel := "archived_sessions/absent.jsonl"
	writeTestFile(t, filepath.Join(f.codexRoot, rel), "excluded-private-session-content")
	rollout := sqliteRollout{ThreadID: "gui-thread", RolloutPath: filepath.Join(f.options.SandboxHome, ".codex", rel)}
	if _, err := classifyUnavailableGUIReference(f.sqlite, filepath.Join(f.codexRoot, "state_5.sqlite"), "state_5.sqlite", rollout, rel, f.options, true); err == nil {
		t.Fatal("present source supplied omitted captured membership")
	}
}

func TestV4RetainedProofAndCurrentSourceVerificationStaySeparate(t *testing.T) {
	f := guiMissingFixture(t, true)
	r, err := Capture(f.options)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(f.codexRoot, "archived_sessions/absent.jsonl"), "{\"type\":\"session_meta\",\"payload\":{\"id\":\"gui-thread\"}}\n")
	if err := VerifyCapturedBundle(f.options, r.ManifestPath); err != nil {
		t.Fatal("retained proof used mutable current source", err)
	}
	if err := VerifyCapturedSource(f.options, r.ManifestPath); err == nil {
		t.Fatal("appeared source body accepted")
	}
	fresh, err := Capture(f.options)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.SchemaVersion != SnapshotSchema || fresh.GUIRollouts != 2 || fresh.UnavailableGUIReferences != 0 {
		t.Fatal("present body skipped", fresh)
	}
}

func TestV4QueryFailureIsNotArchiveObservationUnavailability(t *testing.T) {
	f := guiMissingFixture(t, false)
	db := filepath.Join(f.codexRoot, "state_5.sqlite")
	for _, executable := range []string{filepath.Join(t.TempDir(), "missing-sqlite")} {
		if _, err := capturedArchiveObservation(executable, db, "gui-thread"); err == nil || strings.Contains(err.Error(), "excluded-private") {
			t.Fatal("query failure became benign observation or leaked content", err)
		}
	}
	if _, err := capturedArchiveObservation(f.sqlite, db, "absent-thread"); err == nil {
		t.Fatal("missing SQLite row became unavailable reference")
	}
	if _, err := capturedArchiveObservation(f.sqlite, filepath.Join(t.TempDir(), "absent.sqlite"), "gui-thread"); err == nil {
		t.Fatal("unreadable database became legacy column absence")
	}
}

func TestV4CannotEnterTheFiniteV2PreparationProjection(t *testing.T) {
	f := newPreparationFixture(t)
	b := mustProjectionRead(t, f.binding.ManifestPath)
	b = bytes.Replace(b, []byte(SnapshotSchema), []byte(GUIAvailableEvidenceSchema), 1)
	if err := os.WriteFile(f.binding.ManifestPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	f.binding.ManifestSHA256 = projectionDigest(b)
	if err := ProjectPreparationHistory(f.options, f.binding); err == nil {
		t.Fatal("v4 entered finite v2 projection")
	}
}

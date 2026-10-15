package codexhistory

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func archivedMissingFixture(t *testing.T, fixed bool) captureFixture {
	t.Helper()
	f := newCaptureFixture(t)
	if fixed {
		f = fixedFixture(t)
	}
	db := createStateDatabase(t, f, filepath.Join(f.options.SandboxHome, ".codex/archived_sessions/absent.jsonl"))
	runSQLite(t, f.sqlite, db, "ALTER TABLE threads ADD COLUMN archived; UPDATE threads SET archived=1 WHERE id='gui-thread';")
	writeTestFile(t, filepath.Join(f.codexRoot, "sessions/available.jsonl"), "{\"type\":\"session_meta\",\"payload\":{\"id\":\"available-thread\"}}\n")
	runSQLite(t, f.sqlite, db, "INSERT INTO threads VALUES('available-thread',"+sqlQuote(filepath.Join(f.options.SandboxHome, ".codex/sessions/available.jsonl"))+",'vscode','keep actual present goal',0);")
	runSQLite(t, f.sqlite, filepath.Join(f.codexRoot, "goals_1.sqlite"), "CREATE TABLE goals(thread_id TEXT,objective TEXT); INSERT INTO goals VALUES('gui-thread','keep absent archive objective');")
	return f
}

func TestUnavailableArchivedCaptureRetainsAllAvailableOriginalsWithHonestV3(t *testing.T) {
	for _, fixed := range []bool{false, true} {
		t.Run(map[bool]string{true: "fixed", false: "native"}[fixed], func(t *testing.T) {
			f := archivedMissingFixture(t, fixed)
			before := map[string][]byte{}
			for _, rel := range []string{"state_5.sqlite", "goals_1.sqlite", "sessions/available.jsonl"} {
				before[rel] = mustProjectionRead(t, filepath.Join(f.codexRoot, rel))
			}
			result, err := Capture(f.options)
			if err != nil {
				t.Fatal(err)
			}
			m := readManifest(t, result.ManifestPath)
			if result.SchemaVersion != AvailableEvidenceSchema || result.UnavailableArchivedReferences != 1 || m.SchemaVersion != AvailableEvidenceSchema || m.Status != AvailableEvidenceStatus || m.GUIRolloutCount != 1 || m.QuarantinedGUIRolloutCount != 0 || m.UnavailableArchivedReferenceCount != 1 || len(m.Files) != 3 {
				t.Fatalf("dishonest custody: %+v %+v", result, m)
			}
			u := m.UnavailableArchivedReferences[0]
			if u.ThreadID != "gui-thread" || u.SourceDatabase != "state_5.sqlite" || u.MappedHistoryMember != "archived_sessions/absent.jsonl" || u.ResumeEligible || u.CapturedMemberPresent || u.RowArchived != "integer-1" {
				t.Fatalf("wrong gap binding: %+v", u)
			}
			for rel, b := range before {
				if !bytes.Equal(b, mustProjectionRead(t, filepath.Join(filepath.Dir(result.ManifestPath), "payload", rel))) || !bytes.Equal(b, mustProjectionRead(t, filepath.Join(f.codexRoot, rel))) {
					t.Fatalf("original changed: %s", rel)
				}
			}
			if _, err := os.Lstat(filepath.Join(filepath.Dir(result.ManifestPath), "payload", u.MappedHistoryMember)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invented absent payload", err)
			}
			if fixed {
				if err := VerifyCapturedSource(f.options, result.ManifestPath); err != nil {
					t.Fatal(err)
				}
				if err := os.RemoveAll(f.options.HostHome); err != nil {
					t.Fatal(err)
				} // only synthetic fixture
				if err := VerifyCapturedBundle(f.options, result.ManifestPath); err != nil {
					t.Fatal("retained proof depended on deleted source", err)
				}
				if err := VerifyCapturedSource(f.options, result.ManifestPath); err == nil {
					t.Fatal("source proof accepted deleted source")
				}
			}
		})
	}
}

func TestUnavailableArchiveMetadataTamperingCannotSupplyAnException(t *testing.T) {
	for _, mode := range []string{"class", "database", "thread", "raw-reference", "mapped-member", "mapping", "archive-state", "eligibility", "presence", "source-absence", "duplicate", "count", "schema", "downgrade-remove-gap", "status", "total", "gui-count", "quarantine-count", "missing-field", "extra-field", "duplicate-field", "null-field"} {
		t.Run(mode, func(t *testing.T) {
			f := archivedMissingFixture(t, true)
			result, err := Capture(f.options)
			if err != nil {
				t.Fatal(err)
			}
			m := readManifest(t, result.ManifestPath)
			u := &m.UnavailableArchivedReferences[0]
			switch mode {
			case "class":
				u.CustodyClass = "complete"
			case "database":
				u.SourceDatabase = "state_6.sqlite"
			case "thread":
				u.ThreadID = "other"
			case "raw-reference":
				u.RolloutReference += "-other"
			case "mapped-member":
				u.MappedHistoryMember = "sessions/available.jsonl"
			case "mapping":
				u.ReferenceMapping = "host-home"
			case "archive-state":
				u.RowArchived = "integer-0"
			case "eligibility":
				u.ResumeEligible = true
			case "presence":
				u.CapturedMemberPresent = true
			case "source-absence":
				u.DeclaredSourceMember = "unknown"
			case "duplicate":
				m.UnavailableArchivedReferences = append(m.UnavailableArchivedReferences, *u)
				m.UnavailableArchivedReferenceCount++
			case "count":
				m.UnavailableArchivedReferenceCount++
			case "schema":
				m.SchemaVersion = SnapshotSchema
				m.Status = "complete"
			case "downgrade-remove-gap":
				m.SchemaVersion = SnapshotSchema
				m.Status = "complete"
				m.UnavailableArchivedReferenceCount = 0
				m.UnavailableArchivedReferences = nil
			case "status":
				m.Status = "complete"
			case "total":
				m.TotalBytes++
			case "gui-count":
				m.GUIRolloutCount++
			case "quarantine-count":
				m.QuarantinedGUIRolloutCount++
			}
			b, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "missing-field":
				b = bytes.Replace(b, []byte(`"resume_eligible":false`), []byte(`"unused":false`), 1)
			case "extra-field":
				b = bytes.Replace(b, []byte(`"resume_eligible":false`), []byte(`"resume_eligible":false,"extra":false`), 1)
			case "duplicate-field":
				b = bytes.Replace(b, []byte(`"resume_eligible":false`), []byte(`"resume_eligible":true,"resume_eligible":false`), 1)
			case "null-field":
				b = bytes.Replace(b, []byte(`"resume_eligible":false`), []byte(`"resume_eligible":null`), 1)
			}
			if err := os.WriteFile(result.ManifestPath, b, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := VerifyCapturedBundle(f.options, result.ManifestPath); err == nil {
				t.Fatal("tampered v3 accepted")
			}
		})
	}
}

func TestUnavailableArchiveRequiresUniqueCapturedIdentityAndMember(t *testing.T) {
	for _, mode := range []string{"other-family", "same-member", "other-thread-same-member"} {
		t.Run(mode, func(t *testing.T) {
			f := archivedMissingFixture(t, false)
			switch mode {
			case "other-family":
				b := mustProjectionRead(t, filepath.Join(f.codexRoot, "state_5.sqlite"))
				if err := os.WriteFile(filepath.Join(f.codexRoot, "state_6.sqlite"), b, 0o600); err != nil {
					t.Fatal(err)
				}
			case "same-member":
				runSQLite(t, f.sqlite, filepath.Join(f.codexRoot, "state_5.sqlite"), "UPDATE threads SET rollout_path=(SELECT rollout_path FROM threads WHERE id='gui-thread') WHERE id='available-thread';")
			case "other-thread-same-member":
				runSQLite(t, f.sqlite, filepath.Join(f.codexRoot, "state_5.sqlite"), "INSERT INTO threads SELECT 'other-thread',rollout_path,'vscode','other goal',1 FROM threads WHERE id='gui-thread';")
			}
			if _, err := Capture(f.options); err == nil {
				t.Fatal("ambiguous unavailable reference accepted")
			}
		})
	}
}

func TestUnavailableArchiveSourceReappearanceAndSymlinksStillRefuse(t *testing.T) {
	f := archivedMissingFixture(t, true)
	result, err := Capture(f.options)
	if err != nil {
		t.Fatal(err)
	}
	rel := "archived_sessions/absent.jsonl"
	writeTestFile(t, filepath.Join(f.codexRoot, rel), "{\"type\":\"session_meta\",\"payload\":{\"id\":\"gui-thread\"}}\n")
	if err := VerifyCapturedBundle(f.options, result.ManifestPath); err != nil {
		t.Fatal("retained bundle unexpectedly consulted current source", err)
	}
	if err := VerifyCapturedSource(f.options, result.ManifestPath); err == nil {
		t.Fatal("appeared body accepted at source boundary")
	}
	fresh, err := Capture(f.options)
	if err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, fresh.ManifestPath)
	if m.SchemaVersion != SnapshotSchema || m.Status != "complete" || m.GUIRolloutCount != 2 || len(m.UnavailableArchivedReferences) != 0 {
		t.Fatal("present archive was omitted", m)
	}
	for _, mode := range []string{"parent-symlink", "final-symlink", "nonregular"} {
		t.Run(mode, func(t *testing.T) {
			f := archivedMissingFixture(t, false)
			outside := t.TempDir()
			switch mode {
			case "parent-symlink":
				err = os.Symlink(outside, filepath.Join(f.codexRoot, "archived_sessions"))
			case "final-symlink":
				if err = os.MkdirAll(filepath.Join(f.codexRoot, "archived_sessions"), 0o700); err == nil {
					err = os.Symlink(filepath.Join(outside, "absent"), filepath.Join(f.codexRoot, rel))
				}
			case "nonregular":
				err = os.MkdirAll(filepath.Join(f.codexRoot, rel), 0o700)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Capture(f.options); err == nil {
				t.Fatal("unsafe source became unavailable archive")
			}
		})
	}
}

func TestPreparationProjectionStillRefusesV3WithOtherwiseValidThreeMemberFixture(t *testing.T) {
	f := newPreparationFixture(t)
	b := mustProjectionRead(t, f.binding.ManifestPath)
	b = bytes.Replace(b, []byte(SnapshotSchema), []byte(AvailableEvidenceSchema), 1)
	if err := os.WriteFile(f.binding.ManifestPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	f.binding.ManifestSHA256 = projectionDigest(b)
	if err := ProjectPreparationHistory(f.options, f.binding); err == nil || !strings.Contains(err.Error(), "one complete nonquarantined native thread") {
		t.Fatal("finite projection admitted v3 or failed unrelated precondition", err)
	}
}

func TestUnavailableArchiveCaptureRefusesChangesBeforeAtomicCommit(t *testing.T) {
	for _, mode := range []string{"body-appeared", "sqlite-row-changed", "sidecar-appeared"} {
		t.Run(mode, func(t *testing.T) {
			f := archivedMissingFixture(t, false)
			captureBeforeCommitCheckpoint = func() error {
				switch mode {
				case "body-appeared":
					writeTestFile(t, filepath.Join(f.codexRoot, "archived_sessions/absent.jsonl"), "new real body")
				case "sqlite-row-changed":
					runSQLite(t, f.sqlite, filepath.Join(f.codexRoot, "state_5.sqlite"), "UPDATE threads SET archived=0 WHERE id='gui-thread';")
				case "sidecar-appeared":
					writeTestFile(t, filepath.Join(f.codexRoot, "state_5.sqlite-wal"), "changed cold SQLite family")
				}
				return nil
			}
			t.Cleanup(func() { captureBeforeCommitCheckpoint = nil })
			if _, err := Capture(f.options); err == nil {
				t.Fatal("changing history crossed atomic completion")
			}
			captureBeforeCommitCheckpoint = nil
			root, err := WorkspaceCustodyRoot(f.options.WorkspaceRoot, f.options.Project)
			if err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Join(root, "agent1"))
			if err != nil || len(entries) != 0 {
				t.Fatal("race published generation/staging", entries, err)
			}
		})
	}
}

func TestUnavailableArchiveDoesNotClassifyQueryFailureAsArchived(t *testing.T) {
	f := archivedMissingFixture(t, false)
	rollout := sqliteRollout{ThreadID: "gui-thread", RolloutPath: filepath.Join(f.options.SandboxHome, ".codex/archived_sessions/absent.jsonl")}
	if _, err := classifyUnavailableArchivedReference(filepath.Join(t.TempDir(), "no-sqlite"), filepath.Join(f.codexRoot, "state_5.sqlite"), "state_5.sqlite", rollout, "archived_sessions/absent.jsonl", f.options, true); err == nil {
		t.Fatal("query failure became archived")
	}
}

func TestUnavailableArchiveRequiresCanonicalRawReference(t *testing.T) {
	for _, suffix := range []string{"/sessions/../archived_sessions/absent.jsonl", "/archived_sessions//absent.jsonl", "/archived_sessions/absent.jsonl "} {
		t.Run(suffix, func(t *testing.T) {
			f := archivedMissingFixture(t, false)
			runSQLite(t, f.sqlite, filepath.Join(f.codexRoot, "state_5.sqlite"), "UPDATE threads SET rollout_path="+sqlQuote(f.options.SandboxHome+"/.codex"+suffix)+" WHERE id='gui-thread';")
			if _, err := Capture(f.options); err == nil {
				t.Fatal("noncanonical reference acquired unavailable class")
			}
		})
	}
}

func TestRetainedDowngradeRefusalDoesNotUseCurrentSourceDiagnostic(t *testing.T) {
	f := archivedMissingFixture(t, true)
	result, err := Capture(f.options)
	if err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, result.ManifestPath)
	m.SchemaVersion = SnapshotSchema
	m.Status = "complete"
	m.UnavailableArchivedReferenceCount = 0
	m.UnavailableArchivedReferences = nil
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result.ManifestPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.options.HostHome); err != nil {
		t.Fatal(err)
	} // only synthetic fixture
	err = VerifyCapturedBundle(f.options, result.ManifestPath)
	var diagnostic *missingRolloutError
	if err == nil || errors.As(err, &diagnostic) || !strings.Contains(err.Error(), "retained captured GUI reference") {
		t.Fatal("source-free reader used source diagnostic or accepted downgrade", err)
	}
}

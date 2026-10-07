package codexhistory

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const projectionTestThread = "01a068da-e98a-7f30-88d7-7682bd2abbb8"

type preparationFixture struct {
	captureFixture
	binding    PreparationProjection
	rollout    string
	cold       map[string][]byte
	coldInfo   map[string]os.FileInfo
	before     []byte
	beforeInfo os.FileInfo
	protected  map[string][]byte
}

func newPreparationFixture(t *testing.T) preparationFixture {
	t.Helper()
	// This gate must execute real SQLite assertions, never become a skip.
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Fatal("projection gate requires the Nix-supplied sqlite3 fixture executable")
	}
	f := newCaptureFixture(t)
	rel := "sessions/2026/09/03/rollout-2026-09-03T19-59-32-" + projectionTestThread + ".jsonl"
	rollout := filepath.Join(f.codexRoot, rel)
	writeTestFile(t, rollout, `{"type":"session_meta","payload":{"id":"`+projectionTestThread+`"}}`+"\n"+`{"type":"event","payload":{}}`+"\n")
	runSQLite(t, f.sqlite, filepath.Join(f.codexRoot, "state_5.sqlite"), "CREATE TABLE threads(id TEXT PRIMARY KEY,rollout_path TEXT,source TEXT,title TEXT); INSERT INTO threads VALUES("+sqlQuote(projectionTestThread)+","+sqlQuote(filepath.Join(f.options.SandboxHome, ".codex", rel))+",'vscode','retained objective');")
	goals := filepath.Join(f.codexRoot, "goals_1.sqlite")
	runSQLite(t, f.sqlite, goals, "CREATE TABLE _sqlx_migrations(version BIGINT PRIMARY KEY,description TEXT NOT NULL,installed_on TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,success BOOLEAN NOT NULL,checksum BLOB NOT NULL,execution_time BIGINT NOT NULL); CREATE TABLE thread_goals(thread_id TEXT NOT NULL PRIMARY KEY,goal_id TEXT NOT NULL,objective TEXT NOT NULL,status TEXT NOT NULL,token_budget INTEGER,tokens_used INTEGER NOT NULL DEFAULT 0,time_used_seconds INTEGER NOT NULL DEFAULT 0,created_at_ms INTEGER NOT NULL,updated_at_ms INTEGER NOT NULL); CREATE TABLE thread_goal_continuation_deferrals(thread_id TEXT NOT NULL PRIMARY KEY); INSERT INTO _sqlx_migrations(version,description,success,checksum,execution_time) VALUES(1,'thread goals',1,X'8CE6280244A0B4B39C7C37C7F67AFF1670EC963D2401CDF2ABCFEA9904102DB6CD99B7BEDF366267791A45EB12F17713',0); INSERT INTO _sqlx_migrations(version,description,success,checksum,execution_time) VALUES(2,'thread goal continuation deferrals',1,X'F3FF7DE2FD89914820BEF371081E885FD17DF18A6FBDC432399A83255A8A01AA32FB7EEA5E4DE21A17BDA3DA16C49584',0);"+" INSERT INTO thread_goals VALUES("+sqlQuote(projectionTestThread)+",'original-goal-id','retained objective','blocked',NULL,7,11,101,202);")
	result, err := Capture(f.options)
	if err != nil {
		t.Fatal(err)
	}
	if result.FileCount != 3 || result.GUIRollouts != 1 {
		t.Fatalf("unexpected cold fixture: %+v", result)
	}
	mb, err := os.ReadFile(result.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	p := preparationFixture{captureFixture: f, binding: PreparationProjection{result.ManifestPath, projectionDigest(mb), result.BundleSHA256, projectionTestThread}, rollout: rel, cold: map[string][]byte{}, coldInfo: map[string]os.FileInfo{}, protected: map[string][]byte{}}
	for _, record := range readManifest(t, result.ManifestPath).Files {
		path := filepath.Join(filepath.Dir(result.ManifestPath), "payload", record.Path)
		p.cold[record.Path] = mustProjectionRead(t, path)
		p.coldInfo[record.Path] = mustProjectionStat(t, path)
	}
	for _, name := range []string{"goals_1.sqlite", "state_5.sqlite", rel} {
		if err := os.Remove(filepath.Join(f.codexRoot, name)); err != nil {
			t.Fatal(err)
		}
	}
	runSQLite(t, f.sqlite, goals, "CREATE TABLE _sqlx_migrations(version BIGINT PRIMARY KEY,description TEXT NOT NULL,installed_on TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,success BOOLEAN NOT NULL,checksum BLOB NOT NULL,execution_time BIGINT NOT NULL); CREATE TABLE thread_goals(thread_id TEXT NOT NULL PRIMARY KEY,goal_id TEXT NOT NULL,objective TEXT NOT NULL,status TEXT NOT NULL,token_budget INTEGER,tokens_used INTEGER NOT NULL DEFAULT 0,time_used_seconds INTEGER NOT NULL DEFAULT 0,created_at_ms INTEGER NOT NULL,updated_at_ms INTEGER NOT NULL); CREATE TABLE thread_goal_continuation_deferrals(thread_id TEXT NOT NULL PRIMARY KEY); INSERT INTO _sqlx_migrations(version,description,success,checksum,execution_time) VALUES(1,'thread goals',1,X'8CE6280244A0B4B39C7C37C7F67AFF1670EC963D2401CDF2ABCFEA9904102DB6CD99B7BEDF366267791A45EB12F17713',0); INSERT INTO _sqlx_migrations(version,description,success,checksum,execution_time) VALUES(2,'thread goal continuation deferrals',1,X'F3FF7DE2FD89914820BEF371081E885FD17DF18A6FBDC432399A83255A8A01AA32FB7EEA5E4DE21A17BDA3DA16C49584',0);")
	p.before = mustProjectionRead(t, goals)
	p.beforeInfo = mustProjectionStat(t, goals)
	for _, name := range []string{"state_5.sqlite", "auth.json", "config.toml"} {
		writeTestFile(t, filepath.Join(f.codexRoot, name), "protected synthetic sentinel: "+name+"\n")
		p.protected[name] = mustProjectionRead(t, filepath.Join(f.codexRoot, name))
	}
	return p
}
func mustProjectionRead(t *testing.T, path string) []byte {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func mustProjectionStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	i, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	return i
}
func (f preparationFixture) journalDir() string {
	return filepath.Join(f.codexRoot, ".retained-history-projection", f.binding.ManifestSHA256)
}
func (f preparationFixture) assertColdAndProtected(t *testing.T) {
	t.Helper()
	for rel, b := range f.cold {
		p := filepath.Join(filepath.Dir(f.binding.ManifestPath), "payload", rel)
		if !bytes.Equal(b, mustProjectionRead(t, p)) || !os.SameFile(f.coldInfo[rel], mustProjectionStat(t, p)) {
			t.Fatalf("cold member changed: %s", rel)
		}
	}
	for rel, b := range f.protected {
		if !bytes.Equal(b, mustProjectionRead(t, filepath.Join(f.codexRoot, rel))) {
			t.Fatalf("protected current member changed: %s", rel)
		}
	}
}
func (f preparationFixture) assertCommitted(t *testing.T) {
	t.Helper()
	f.assertColdAndProtected(t)
	for _, rel := range []string{"goals_1.sqlite", f.rollout} {
		p := filepath.Join(f.codexRoot, rel)
		if !bytes.Equal(f.cold[rel], mustProjectionRead(t, p)) {
			t.Fatalf("wrong projection: %s", rel)
		}
		if os.SameFile(f.coldInfo[rel], mustProjectionStat(t, p)) {
			t.Fatalf("cold original linked: %s", rel)
		}
	}
	archive := filepath.Join(f.journalDir(), "before-goals.sqlite")
	if !bytes.Equal(f.before, mustProjectionRead(t, archive)) || !os.SameFile(f.beforeInfo, mustProjectionStat(t, archive)) {
		t.Fatal("original current empty goals inode/bytes were not preserved")
	}
	for _, rel := range []string{"pending-goals.sqlite", "pending-rollout.jsonl", "journal.committed"} {
		if _, e := os.Lstat(filepath.Join(f.journalDir(), rel)); !errors.Is(e, os.ErrNotExist) {
			t.Fatalf("staging alias survived commit: %s", rel)
		}
	}
	if !bytes.Contains(mustProjectionRead(t, filepath.Join(f.journalDir(), "journal.json")), []byte(`"status":"committed"`)) {
		t.Fatal("no committed journal")
	}
}

func TestPreparationProjectionPreservesHistoryAndCurrentState(t *testing.T) {
	f := newPreparationFixture(t)
	if e := ProjectPreparationHistory(f.options, f.binding); e != nil {
		t.Fatal(e)
	}
	f.assertCommitted(t)
	b, e := exec.Command(f.sqlite, "-readonly", "-batch", filepath.Join(f.codexRoot, "goals_1.sqlite"), "SELECT goal_id||'|'||tokens_used||'|'||time_used_seconds||'|'||created_at_ms||'|'||updated_at_ms FROM thread_goals;").Output()
	if e != nil || string(b) != "original-goal-id|7|11|101|202\n" {
		t.Fatalf("goal lifetime/accounting changed: %q %v", b, e)
	}
	// A committed preparation permits an existing healthy native owner and append.
	writeTestFile(t, filepath.Join(f.codexRoot, "a1-app.sock"), "synthetic active marker")
	file, e := os.OpenFile(filepath.Join(f.codexRoot, f.rollout), os.O_WRONLY|os.O_APPEND, 0)
	if e != nil {
		t.Fatal(e)
	}
	_, e = file.WriteString(`{"type":"event","payload":{}}` + "\n")
	file.Close()
	if e != nil {
		t.Fatal(e)
	}
	if e := ProjectPreparationHistory(f.options, f.binding); e != nil {
		t.Fatalf("committed healthy continuation refused: %v", e)
	}
	f.assertColdAndProtected(t)
	if e := os.Remove(filepath.Join(f.codexRoot, "goals_1.sqlite")); e != nil {
		t.Fatal(e)
	}
	if e := ProjectPreparationHistory(f.options, f.binding); e == nil {
		t.Fatal("committed replay accepted missing goals")
	}
}

func TestPreparationProjectionRecoversDurableTransitions(t *testing.T) {
	rel := "sessions/2026/09/03/rollout-2026-09-03T19-59-32-" + projectionTestThread + ".jsonl"
	for _, point := range []string{"before-retirement", "after-retirement", "installed-goals_1.sqlite", "installed-" + rel, "before-commit", "commit-artifact-written"} {
		t.Run(point, func(t *testing.T) {
			f := newPreparationFixture(t)
			sentinel := errors.New("owned interruption")
			preparationProjectionCheckpoint = func(s string) error {
				if s == point {
					return sentinel
				}
				return nil
			}
			t.Cleanup(func() { preparationProjectionCheckpoint = nil })
			e := ProjectPreparationHistory(f.options, f.binding)
			preparationProjectionCheckpoint = nil
			if !errors.Is(e, sentinel) {
				t.Fatalf("did not exercise transition %s: %v", point, e)
			}
			f.assertColdAndProtected(t)
			if e := ProjectPreparationHistory(f.options, f.binding); e != nil {
				t.Fatalf("actual journal recovery: %v", e)
			}
			f.assertCommitted(t)
		})
	}
}

func TestPreparationProjectionRefusesUnsafeInitialState(t *testing.T) {
	for _, name := range []string{"nonempty-goals", "nonempty-other-business-table", "sidecar", "live-socket", "rollout-collision", "symlink-goals", "extra-cold-member", "changed-cold-member", "manifest-digest", "partial-binding", "foreign-journal", "unjournaled-stage", "malformed-empty-schema", "migration-drift"} {
		t.Run(name, func(t *testing.T) {
			f := newPreparationFixture(t)
			switch name {
			case "malformed-empty-schema":
				runSQLite(t, f.sqlite, filepath.Join(f.codexRoot, "goals_1.sqlite"), "DROP TABLE thread_goals; CREATE TABLE thread_goals(thread_id TEXT);")
			case "migration-drift":
				runSQLite(t, f.sqlite, filepath.Join(f.codexRoot, "goals_1.sqlite"), "UPDATE _sqlx_migrations SET success=0 WHERE version=2;")
			case "nonempty-goals":
				runSQLite(t, f.sqlite, filepath.Join(f.codexRoot, "goals_1.sqlite"), "INSERT INTO thread_goals VALUES('another-thread','other-goal','other objective','active',NULL,0,0,10,20);")
			case "nonempty-other-business-table":
				runSQLite(t, f.sqlite, filepath.Join(f.codexRoot, "goals_1.sqlite"), "INSERT INTO thread_goal_continuation_deferrals VALUES('another-thread');")
			case "sidecar":
				writeTestFile(t, filepath.Join(f.codexRoot, "goals_1.sqlite-wal"), "sidecar")
			case "live-socket":
				writeTestFile(t, filepath.Join(f.codexRoot, "a1-app.sock"), "owned marker")
			case "rollout-collision":
				writeTestFile(t, filepath.Join(f.codexRoot, f.rollout), "foreign rollout")
			case "symlink-goals":
				if e := os.Rename(filepath.Join(f.codexRoot, "goals_1.sqlite"), filepath.Join(f.codexRoot, "foreign.sqlite")); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink("foreign.sqlite", filepath.Join(f.codexRoot, "goals_1.sqlite")); e != nil {
					t.Fatal(e)
				}
			case "extra-cold-member":
				writeTestFile(t, filepath.Join(filepath.Dir(f.binding.ManifestPath), "payload", "sessions", "extra.jsonl"), "extra")
			case "changed-cold-member":
				writeTestFile(t, filepath.Join(filepath.Dir(f.binding.ManifestPath), "payload", f.rollout), "changed")
			case "manifest-digest":
				f.binding.ManifestSHA256 = strings.Repeat("0", 64)
			case "partial-binding":
				f.binding.ManifestPath = ""
			case "foreign-journal":
				writeTestFile(t, filepath.Join(f.journalDir(), "journal.json"), `{"schema":"foreign"}`)
			case "unjournaled-stage":
				writeTestFile(t, filepath.Join(f.journalDir(), "pending-goals.sqlite"), "unadmitted")
			}
			before := mustProjectionRead(t, filepath.Join(f.codexRoot, "goals_1.sqlite"))
			if e := ProjectPreparationHistory(f.options, f.binding); e == nil {
				t.Fatalf("accepted %s", name)
			}
			if !bytes.Equal(before, mustProjectionRead(t, filepath.Join(f.codexRoot, "goals_1.sqlite"))) {
				t.Fatal("refusal changed current goal bytes")
			}
			for rel, b := range f.protected {
				if !bytes.Equal(b, mustProjectionRead(t, filepath.Join(f.codexRoot, rel))) {
					t.Fatalf("refusal changed %s", rel)
				}
			}
		})
	}
}

func TestPreparationProjectionRecoveryRefusesNewSidecarsAndLiveOwner(t *testing.T) {
	for _, name := range []string{"sidecar", "socket"} {
		t.Run(name, func(t *testing.T) {
			f := newPreparationFixture(t)
			stop := errors.New("pause after retirement")
			preparationProjectionCheckpoint = func(s string) error {
				if s == "after-retirement" {
					return stop
				}
				return nil
			}
			t.Cleanup(func() { preparationProjectionCheckpoint = nil })
			e := ProjectPreparationHistory(f.options, f.binding)
			preparationProjectionCheckpoint = nil
			if !errors.Is(e, stop) {
				t.Fatal(e)
			}
			nameOnDisk := "goals_1.sqlite-shm"
			if name == "socket" {
				nameOnDisk = "a1-app.sock"
			}
			writeTestFile(t, filepath.Join(f.codexRoot, nameOnDisk), "owning refusal witness")
			if e := ProjectPreparationHistory(f.options, f.binding); e == nil {
				t.Fatal("recovery accepted new sidecar/owner")
			}
			if _, e := os.Lstat(filepath.Join(f.codexRoot, "goals_1.sqlite")); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("rejected recovery installed a goal")
			}
			f.assertColdAndProtected(t)
			if !bytes.Equal(f.before, mustProjectionRead(t, filepath.Join(f.journalDir(), "before-goals.sqlite"))) {
				t.Fatal("refusal lost original current goals")
			}
		})
	}
}

func TestPreparationProjectionCommittedReplayAllowsSuffixActivity(t *testing.T) {
	f := newPreparationFixture(t)
	if err := ProjectPreparationHistory(f.options, f.binding); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(f.codexRoot, "a1-app.sock"), "healthy synthetic owner")
	reached := false
	preparationProjectionCheckpoint = func(point string) error {
		if point != "before-committed-prefix-read" {
			return nil
		}
		reached = true
		file, err := os.OpenFile(filepath.Join(f.codexRoot, f.rollout), os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			return err
		}
		_, err = file.WriteString(strings.Repeat("ordinary appended suffix\n", 4096))
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
	t.Cleanup(func() { preparationProjectionCheckpoint = nil })
	if err := ProjectPreparationHistory(f.options, f.binding); err != nil {
		t.Fatalf("live append made committed continuation fail: %v", err)
	}
	preparationProjectionCheckpoint = nil
	if !reached {
		t.Fatal("did not exercise append after the retained member was opened")
	}
	f.assertColdAndProtected(t)
}

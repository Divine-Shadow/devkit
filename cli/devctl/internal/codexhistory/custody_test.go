package codexhistory

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"devkit/cli/devctl/internal/sqliteauthority"
)

type captureFixture struct {
	options   SnapshotOptions
	codexRoot string
	sqlite    string
}

func newCaptureFixture(t *testing.T) captureFixture {
	t.Helper()
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 is supplied by the Nix test closure")
	}
	restore := sqliteauthority.SetExecutableForTesting(sqlite)
	t.Cleanup(restore)
	root := t.TempDir()
	stateRoot := filepath.Join(root, ".devkit", "native-agents")
	if err := os.MkdirAll(filepath.Dir(stateRoot), 0o700); err != nil {
		t.Fatal(err)
	}
	hostHome := filepath.Join(root, "agent-worktrees", "agent1", ".devhome-agent1")
	codexRoot := filepath.Join(hostHome, ".codex")
	if err := os.MkdirAll(codexRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	return captureFixture{
		options: SnapshotOptions{
			Project:     "dev-all",
			ResetKind:   "selected-slot-reset",
			AgentIndex:  1,
			HostHome:    hostHome,
			SandboxHome: "/workspaces/dev/agent-worktrees/agent1/.devhome-agent1",
			StateRoot:   stateRoot,
		},
		codexRoot: codexRoot,
		sqlite:    sqlite,
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func sqlQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func runSQLite(t *testing.T, executable, database, sql string) {
	t.Helper()
	command := exec.Command(executable, "-batch", database, sql)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("sqlite3 failed: %v: %s", err, output)
	}
}

func createStateDatabase(t *testing.T, fixture captureFixture, rollout string) string {
	t.Helper()
	database := filepath.Join(fixture.codexRoot, "state_5.sqlite")
	runSQLite(t, fixture.sqlite, database, fmt.Sprintf(
		"CREATE TABLE threads (id TEXT PRIMARY KEY, rollout_path TEXT NOT NULL, source TEXT NOT NULL); INSERT INTO threads VALUES ('gui-thread', %s, 'vscode'); INSERT INTO threads VALUES ('cli-thread', '/discarded/cli.jsonl', 'cli');",
		sqlQuote(rollout),
	))
	return database
}

func holdWALSidecars(t *testing.T, executable, database string) {
	t.Helper()
	command := exec.Command(executable, "-batch", database)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(stdin, "PRAGMA journal_mode=WAL; BEGIN IMMEDIATE; UPDATE threads SET source = source; COMMIT;\n"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		walInfo, walErr := os.Stat(database + "-wal")
		shmInfo, shmErr := os.Stat(database + "-shm")
		if walErr == nil && shmErr == nil && walInfo.Mode().IsRegular() && shmInfo.Mode().IsRegular() {
			break
		}
		if time.Now().After(deadline) {
			_ = stdin.Close()
			_ = command.Wait()
			t.Fatalf("sqlite3 did not retain WAL/SHM sidecars: %s", output.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = command.Wait()
	})
}

func readManifest(t *testing.T, path string) snapshotManifest {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest snapshotManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func manifestPaths(manifest snapshotManifest) []string {
	paths := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		paths = append(paths, file.Path)
	}
	sort.Strings(paths)
	return paths
}

func TestCaptureStoresOnlyValidatedResumableGUIHistory(t *testing.T) {
	fixture := newCaptureFixture(t)
	sessionRel := "sessions/2026/08/27/rollout-gui-thread.jsonl"
	sandboxRollout := filepath.ToSlash(filepath.Join(fixture.options.SandboxHome, ".codex", filepath.FromSlash(sessionRel)))
	stateDatabase := createStateDatabase(t, fixture, sandboxRollout)
	runSQLite(t, fixture.sqlite, filepath.Join(fixture.codexRoot, "goals_1.sqlite"), "CREATE TABLE thread_goals (thread_id TEXT PRIMARY KEY, goal TEXT); INSERT INTO thread_goals VALUES ('gui-thread', 'durable goal');")
	writeTestFile(t, filepath.Join(fixture.codexRoot, filepath.FromSlash(sessionRel)), "{\"type\":\"session_meta\",\"payload\":{\"id\":\"gui-thread\"}}\n{\"type\":\"event\",\"payload\":{}}\n")
	writeTestFile(t, filepath.Join(fixture.codexRoot, "archived_sessions", "archived.jsonl"), "archived\n")
	writeTestFile(t, filepath.Join(fixture.codexRoot, "rollouts", "legacy.jsonl"), "rollout\n")
	writeTestFile(t, filepath.Join(fixture.codexRoot, "shell_snapshots", "shell.sh"), "export SAFE=1\n")
	for _, excluded := range []string{
		"auth.json", "config.toml", "logs/desktop.log", "memories/memory.md",
		"packages/package.json", "plugins/plugin.json", "skills/skill.md", "cache/blob",
	} {
		writeTestFile(t, filepath.Join(fixture.codexRoot, filepath.FromSlash(excluded)), "excluded-secret-or-runtime-data\n")
	}
	holdWALSidecars(t, fixture.sqlite, stateDatabase)

	result, err := Capture(fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "captured" || result.GUIRollouts != 1 {
		t.Fatalf("capture result = %+v", result)
	}
	manifest := readManifest(t, result.ManifestPath)
	if manifest.SchemaVersion != SnapshotSchema || manifest.Status != "complete" || manifest.GUIRolloutCount != 1 {
		t.Fatalf("manifest identity = %+v", manifest)
	}
	want := []string{
		"archived_sessions/archived.jsonl",
		"goals_1.sqlite",
		"rollouts/legacy.jsonl",
		"sessions/2026/08/27/rollout-gui-thread.jsonl",
		"shell_snapshots/shell.sh",
		"state_5.sqlite",
		"state_5.sqlite-shm",
		"state_5.sqlite-wal",
	}
	sort.Strings(want)
	if got := manifestPaths(manifest); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("captured paths = %v, want %v", got, want)
	}
	finalRoot := filepath.Dir(result.ManifestPath)
	for _, record := range manifest.Files {
		path := filepath.Join(finalRoot, "payload", filepath.FromSlash(record.Path))
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatalf("captured file mode %s = %v", record.Path, info.Mode())
		}
		hash, err := hashFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if hash != record.SHA256 || info.Size() != record.Bytes {
			t.Fatalf("captured file integrity %s = size %d hash %s", record.Path, info.Size(), hash)
		}
	}
	if got := aggregateHash(manifest.Files); got != manifest.BundleSHA256 || got != result.BundleSHA256 {
		t.Fatalf("bundle hash = %s manifest=%s result=%s", got, manifest.BundleSHA256, result.BundleSHA256)
	}
	if data, err := os.ReadFile(result.ManifestPath); err != nil || bytes.Contains(data, []byte("durable goal")) || bytes.Contains(data, []byte("excluded-secret")) {
		t.Fatalf("manifest leaked content or was unreadable: %v", err)
	}
	if info, err := os.Stat(result.ManifestPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("manifest permissions = %v, %v", info, err)
	}
	for _, directory := range []string{finalRoot, filepath.Join(finalRoot, "payload"), filepath.Join(finalRoot, "payload", "sessions")} {
		if info, err := os.Stat(directory); err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("private directory %s = %v, %v", directory, info, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(finalRoot, ".sqlite-validation")); !os.IsNotExist(err) {
		t.Fatalf("private validation copy survived completion: %v", err)
	}
}

func TestCaptureRefusesMissingGUIRolloutWithoutCompletedGeneration(t *testing.T) {
	fixture := newCaptureFixture(t)
	missing := filepath.ToSlash(filepath.Join(fixture.options.SandboxHome, ".codex", "sessions", "missing.jsonl"))
	createStateDatabase(t, fixture, missing)
	_, err := Capture(fixture.options)
	if err == nil || !strings.Contains(err.Error(), "missing from resumable history") {
		t.Fatalf("missing rollout error = %v", err)
	}
	agentRoot, rootErr := CustodyRoot(fixture.options.StateRoot, fixture.options.Project)
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	entries, readErr := os.ReadDir(filepath.Join(agentRoot, "agent1"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("failed capture left generation residue: %v", entries)
	}
}

func TestCaptureRefusesNonResumableGUIRolloutPayload(t *testing.T) {
	tests := []struct {
		name     string
		rel      string
		contents string
		want     string
	}{
		{name: "empty", rel: "sessions/empty.jsonl", contents: "", want: "rollout is empty"},
		{name: "truncated-json", rel: "sessions/truncated.jsonl", contents: "{\"type\":\"session_meta\",\"payload\":{\"id\":\"gui-thread\"}}\n{", want: "invalid JSON"},
		{name: "wrong-history-type", rel: "shell_snapshots/shell.sh", contents: "{\"type\":\"session_meta\",\"payload\":{\"id\":\"gui-thread\"}}\n", want: "not a resumable JSONL history file"},
		{name: "wrong-thread", rel: "sessions/wrong-thread.jsonl", contents: "{\"type\":\"session_meta\",\"payload\":{\"id\":\"other-thread\"}}\n", want: "session identity mismatch"},
		{name: "missing-session-meta", rel: "rollouts/no-meta.jsonl", contents: "{\"type\":\"event\",\"payload\":{}}\n", want: "no matching session_meta identity"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCaptureFixture(t)
			rollout := filepath.ToSlash(filepath.Join(fixture.options.SandboxHome, ".codex", filepath.FromSlash(test.rel)))
			createStateDatabase(t, fixture, rollout)
			writeTestFile(t, filepath.Join(fixture.codexRoot, filepath.FromSlash(test.rel)), test.contents)
			_, err := Capture(fixture.options)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("rollout validation error = %v, want %q", err, test.want)
			}
			custodyRoot, rootErr := CustodyRoot(fixture.options.StateRoot, fixture.options.Project)
			if rootErr != nil {
				t.Fatal(rootErr)
			}
			entries, readErr := os.ReadDir(filepath.Join(custodyRoot, "agent1"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("invalid rollout left generation residue: %v", entries)
			}
		})
	}
}

func TestCaptureRefusesCorruptSQLite(t *testing.T) {
	fixture := newCaptureFixture(t)
	writeTestFile(t, filepath.Join(fixture.codexRoot, "state_5.sqlite"), "not a sqlite database")
	_, err := Capture(fixture.options)
	if err == nil || !strings.Contains(err.Error(), "quick_check") {
		t.Fatalf("corrupt SQLite error = %v", err)
	}
}

func TestCaptureRefusesSymlinkUnsupportedAndEscapingHistory(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		fixture := newCaptureFixture(t)
		target := filepath.Join(filepath.Dir(fixture.codexRoot), "outside.jsonl")
		writeTestFile(t, target, "outside\n")
		if err := os.MkdirAll(filepath.Join(fixture.codexRoot, "sessions"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(fixture.codexRoot, "sessions", "linked.jsonl")); err != nil {
			t.Fatal(err)
		}
		_, err := Capture(fixture.options)
		if err == nil || !strings.Contains(err.Error(), "refuses symlink") {
			t.Fatalf("symlink error = %v", err)
		}
	})
	t.Run("unsupported", func(t *testing.T) {
		fixture := newCaptureFixture(t)
		fifo := filepath.Join(fixture.codexRoot, "sessions", "pipe")
		if err := os.MkdirAll(filepath.Dir(fifo), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Capture(fixture.options)
		if err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("unsupported file error = %v", err)
		}
	})
	t.Run("rollout-escape", func(t *testing.T) {
		fixture := newCaptureFixture(t)
		createStateDatabase(t, fixture, "/etc/passwd")
		_, err := Capture(fixture.options)
		if err == nil || !strings.Contains(err.Error(), "escapes the source-derived Codex home") {
			t.Fatalf("rollout escape error = %v", err)
		}
	})
}

func TestSourceChangeDuringCaptureIsDetected(t *testing.T) {
	fixture := newCaptureFixture(t)
	sourcePath := filepath.Join(fixture.codexRoot, "sessions", "turn.jsonl")
	writeTestFile(t, sourcePath, "before\n")
	files, directories, existed, err := enumerateHistory(fixture.codexRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("enumerated files = %d", len(files))
	}
	hash, err := hashFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	captured := map[string]manifestFile{files[0].relPath: {Path: files[0].relPath, Bytes: files[0].size, SHA256: hash}}
	writeTestFile(t, sourcePath, "after-and-different\n")
	if err := verifySourceSetStable(fixture.codexRoot, files, directories, existed, captured); err == nil || !strings.Contains(err.Error(), "changed during capture") {
		t.Fatalf("source change error = %v", err)
	}
}

func TestCaptureWriteFailureLeavesSourceAndNoGeneration(t *testing.T) {
	fixture := newCaptureFixture(t)
	sourcePath := filepath.Join(fixture.codexRoot, "sessions", "turn.jsonl")
	writeTestFile(t, sourcePath, "preserve\n")
	privateRoot := filepath.Dir(fixture.options.StateRoot)
	if err := os.Chmod(privateRoot, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(privateRoot, 0o700) })
	_, err := Capture(fixture.options)
	if err == nil {
		t.Fatal("write-constrained custody root unexpectedly captured")
	}
	data, readErr := os.ReadFile(sourcePath)
	if readErr != nil || string(data) != "preserve\n" {
		t.Fatalf("source changed after write failure: %q, %v", data, readErr)
	}
}

func TestCapturePreflightENOSPCLeavesNoGeneration(t *testing.T) {
	fixture := newCaptureFixture(t)
	sourcePath := filepath.Join(fixture.codexRoot, "sessions", "turn.jsonl")
	writeTestFile(t, sourcePath, "{\"type\":\"event\",\"payload\":{}}\n")
	restore := SetAvailableSpaceCheckForTesting(func(string, int64) error {
		return fmt.Errorf("deterministic storage exhaustion: %w", syscall.ENOSPC)
	})
	t.Cleanup(restore)

	_, err := Capture(fixture.options)
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("preflight storage failure = %v", err)
	}
	data, readErr := os.ReadFile(sourcePath)
	if readErr != nil || string(data) != "{\"type\":\"event\",\"payload\":{}}\n" {
		t.Fatalf("source changed after ENOSPC: %q, %v", data, readErr)
	}
	custodyRoot, rootErr := CustodyRoot(fixture.options.StateRoot, fixture.options.Project)
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	entries, readErr := os.ReadDir(filepath.Join(custodyRoot, "agent1"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("ENOSPC left committed or staging generation: %v", entries)
	}
}

func TestEmptyCaptureUsesUniqueAtomicGenerationsAndNeverImports(t *testing.T) {
	fixture := newCaptureFixture(t)
	first, err := Capture(fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "empty" {
		t.Fatalf("empty status = %q", first.Status)
	}
	agentRoot := filepath.Dir(filepath.Dir(first.ManifestPath))
	stale := filepath.Join(agentRoot, ".staging-stale-partial")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	second, err := Capture(fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	if first.GenerationID == second.GenerationID {
		t.Fatalf("generation overwritten: %s", first.GenerationID)
	}
	for _, path := range []string{first.ManifestPath, second.ManifestPath, stale} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("generation or stale partial missing %s: %v", path, err)
		}
	}
	if err := os.RemoveAll(fixture.options.HostHome); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fixture.codexRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(fixture.codexRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("fresh home imported cold snapshot: %v", entries)
	}
}

func TestCaptureRequiredBytesIncludesSQLiteValidationCopy(t *testing.T) {
	files := []sourceFile{
		{relPath: "state_5.sqlite", size: 100},
		{relPath: "state_5.sqlite-wal", size: 200},
		{relPath: "sessions/turn.jsonl", size: 300},
	}
	got, err := captureRequiredBytes(files)
	if err != nil {
		t.Fatal(err)
	}
	want := int64(1<<20) + 2*100 + 2*200 + 300
	if got != want {
		t.Fatalf("required bytes = %d, want %d", got, want)
	}
}

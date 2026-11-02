package worktrees

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	runtimeagent "devkit/cli/devctl/internal/runtime/agent"
)

func persistentSQLite(t *testing.T, db, sql string) string {
	t.Helper()
	executable, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Fatal("persistent lifecycle test requires the supplied sqlite3 tool")
	}
	command := exec.Command(executable, db, sql)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("SQLite lifecycle read/write: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestNativePersistentHomesSurviveSelectedAndWholeResetReconstruction(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "host")
	if err := os.MkdirAll(filepath.Join(devRoot, "devkit"), 0o755); err != nil {
		t.Fatal(err)
	}
	makeRepoWithBare(t, root, devRoot, "fixture")
	worktreeRoot := filepath.Join(devRoot, "agent-worktrees")
	stateRoot := filepath.Join(devRoot, ".devkit", "native-agents")
	opts := NativeOptions{
		DevkitRoot: filepath.Join(devRoot, "devkit"), Repo: "fixture", Count: 3,
		Origin: filepath.Join(root, "remotes", "fixture.git"), BaseBranch: "main", BranchPrefix: "agent",
		WorktreeRoot: worktreeRoot,
	}
	if err := SetupNative(opts); err != nil {
		t.Fatal(err)
	}
	pathsFor := func(index int) runtimeagent.Paths {
		t.Helper()
		paths, err := runtimeagent.ResolvePaths(runtimeagent.PathConfig{
			DevkitRoot: opts.DevkitRoot, Project: "dev-all", Repo: opts.Repo, Index: index,
			WorktreeRoot: worktreeRoot, StateRoot: stateRoot, DedicatedWorktree: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return paths
	}
	before := map[string]os.FileInfo{}
	for index := 1; index <= 3; index++ {
		paths := pathsFor(index)
		codex := filepath.Join(paths.HostHome, ".codex")
		if err := os.MkdirAll(codex, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(paths.HostAgentStateRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(paths.HostAgentStateRoot, "disposable"), []byte("runtime"), 0o600); err != nil {
			t.Fatal(err)
		}
		persistentSQLite(t, filepath.Join(codex, "state_5.sqlite"), fmt.Sprintf("CREATE TABLE threads(id TEXT PRIMARY KEY,title TEXT); INSERT INTO threads VALUES('thread-%d','protected task');", index))
		persistentSQLite(t, filepath.Join(codex, "goals_1.sqlite"), fmt.Sprintf("CREATE TABLE thread_goals(thread_id TEXT PRIMARY KEY,objective TEXT,status TEXT); INSERT INTO thread_goals VALUES('thread-%d','finish the useful task','active');", index))
		if err := os.WriteFile(filepath.Join(codex, "session.jsonl"), []byte("protected history\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"state_5.sqlite", "goals_1.sqlite", "session.jsonl"} {
			path := filepath.Join(codex, name)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			before[path] = info
		}
	}
	assertData := func() {
		t.Helper()
		for index := 1; index <= 3; index++ {
			paths := pathsFor(index)
			codex := filepath.Join(paths.HostHome, ".codex")
			if got := persistentSQLite(t, filepath.Join(codex, "state_5.sqlite"), "PRAGMA quick_check; SELECT id||':'||title FROM threads;"); got != fmt.Sprintf("ok\nthread-%d:protected task", index) {
				t.Fatalf("thread database after lifecycle: %q", got)
			}
			if got := persistentSQLite(t, filepath.Join(codex, "goals_1.sqlite"), "PRAGMA quick_check; SELECT thread_id||':'||objective||':'||status FROM thread_goals;"); got != fmt.Sprintf("ok\nthread-%d:finish the useful task:active", index) {
				t.Fatalf("goals after lifecycle: %q", got)
			}
			if got, err := os.ReadFile(filepath.Join(codex, "session.jsonl")); err != nil || string(got) != "protected history\n" {
				t.Fatalf("history after lifecycle: %q %v", got, err)
			}
		}
		for path, old := range before {
			info, err := os.Stat(path)
			if err != nil || !os.SameFile(old, info) {
				t.Fatalf("persistent object was deleted or copied instead of retained: %s: %v", path, err)
			}
		}
	}
	for repeat := 0; repeat < 2; repeat++ {
		plan, err := PlanNativeSlotReset(NativeSlotResetOptions{
			Project: "dev-all", Repo: opts.Repo, Origin: opts.Origin, Index: 2, Count: 3,
			WorktreeRoot: worktreeRoot, StateRoot: stateRoot,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := plan.Apply(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(pathsFor(2).HostAgentStateRoot, "disposable")); !os.IsNotExist(err) {
			t.Fatalf("selected execution state was not reset: %v", err)
		}
		if _, err := os.Stat(pathsFor(3).HostWorktree); err != nil {
			t.Fatalf("sibling worktree changed: %v", err)
		}
		assertData()
		selected := opts
		selected.Index = 2
		if err := SetupNative(selected); err != nil {
			t.Fatal(err)
		}
		assertData()
	}
	for repeat := 0; repeat < 2; repeat++ {
		plan, err := PlanNativeReset(NativeResetOptions{
			Project: "dev-all", Repo: opts.Repo, Count: 3, WorktreeRoot: worktreeRoot, StateRoot: stateRoot,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := plan.Apply(); err != nil {
			t.Fatal(err)
		}
		assertData()
		if err := SetupNative(opts); err != nil {
			t.Fatal(err)
		}
		assertData()
	}
}

func TestNativeResetRefusesLegacyHomesBeforeDeletingAnyData(t *testing.T) {
	for _, index := range []int{1, 2} {
		root := t.TempDir()
		worktreeRoot, stateRoot := filepath.Join(root, "worktrees"), filepath.Join(root, "state")
		legacy := runtimeagent.PersistentDevAllHome(filepath.Join(worktreeRoot, fmt.Sprintf("agent%d", index), "fixture"), index)
		if err := os.MkdirAll(legacy, 0o700); err != nil {
			t.Fatal(err)
		}
		data := filepath.Join(legacy, "goals_1.sqlite")
		persistentSQLite(t, data, "CREATE TABLE thread_goals(objective TEXT); INSERT INTO thread_goals VALUES('protected objective');")
		_, wholeErr := PlanNativeReset(NativeResetOptions{Project: "dev-all", Repo: "fixture", Count: 2, WorktreeRoot: worktreeRoot, StateRoot: stateRoot})
		_, selectedErr := PlanNativeSlotReset(NativeSlotResetOptions{Project: "dev-all", Repo: "fixture", Index: index, Count: 2, WorktreeRoot: worktreeRoot, StateRoot: stateRoot})
		for _, err := range []error{wholeErr, selectedErr} {
			if err == nil || !strings.Contains(err.Error(), "requires environment-owned migration") {
				t.Fatalf("reset should refuse unmigrated protected home: %v", err)
			}
		}
		if got := persistentSQLite(t, data, "SELECT objective FROM thread_goals;"); got != "protected objective" {
			t.Fatalf("refused reset changed data: %q", got)
		}
	}
}

func TestNativeWholeResetRefusesPersistentStorageInsideDisposableRoot(t *testing.T) {
	root := t.TempDir()
	worktreeRoot := filepath.Join(root, "worktrees")
	stateRoot := filepath.Join(worktreeRoot, "agent1", "state")
	_, err := PlanNativeReset(NativeResetOptions{Project: "dev-all", Repo: "fixture", Count: 1, WorktreeRoot: worktreeRoot, StateRoot: stateRoot})
	if err == nil || !strings.Contains(err.Error(), "overlaps protected root") {
		t.Fatalf("reset should refuse an invalid persistent storage mapping: %v", err)
	}
}

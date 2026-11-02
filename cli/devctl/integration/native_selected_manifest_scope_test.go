package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

// Extend the existing package-SSH production-path lifecycle test. Its exact
// CLI, runtime/SSH authority, station owner and readiness fixture stay in use.
func testSelectedResetWithRetainedManifestSuffix(t *testing.T, root, base, worktreeRoot, packageDevctl string, env []string) {
	t.Helper()
	manifestPath := filepath.Join(base, ".devkit", "native-agents", "manifests", "dev-all.json")
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var current nativeagent.Manifest
	if err := json.Unmarshal(before, &current); err != nil {
		t.Fatal(err)
	}
	options := nativeplan.BuildOptions{
		Paths:    devkitpaths.Paths{Root: root, RuntimeAuthorityRoot: root},
		HostRoot: base, Project: current.Project, Repo: current.Repo,
		WorktreeRoot: current.HostWorktreeRoot, StateRoot: current.HostStateRoot,
		WorktreeContainerRoot: current.SandboxWorktreeRoot, StateContainerRoot: current.SandboxStateRoot,
		BaseBranch: current.BaseBranch, BranchPrefix: current.BranchPrefix, BrokerEndpoint: current.BrokerEndpoint,
	}
	canonical, err := nativeplan.BuildManifest(options, 3)
	if err != nil || !reflect.DeepEqual(canonical, current) {
		t.Fatalf("fixture differs from actual CLI manifest: %v", err)
	}
	retained, err := nativeplan.BuildManifest(options, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := nativeagent.WriteManifest(manifestPath, retained, false); err != nil {
		t.Fatal(err)
	}
	retainedBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	selected, surplus := retained.Agents[1], retained.Agents[3]
	// The surplus has durable state and history, but is not an allocation in the
	// current three-slot source selection. Its worktree and lane Git stay absent.
	for _, path := range []string{surplus.HostHome, surplus.StateRoot} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "retained-surplus"), []byte("unrelated state stays exact\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(surplus.HostHome, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	surplusDatabase := filepath.Join(surplus.HostHome, ".codex", "goals_1.sqlite")
	runNativeFixtureCommand(t, "sqlite3", surplusDatabase, "CREATE TABLE goals (goal TEXT); INSERT INTO goals VALUES ('unrelated-protected-goal');")
	surplusBytes, err := os.ReadFile(surplusDatabase)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(base, ".devkit", "codex-gui-history", "dev-all", "agent4")
	if err := os.MkdirAll(archive, 0700); err != nil {
		t.Fatal(err)
	}
	archiveSentinel := filepath.Join(archive, "existing-archive")
	if err := os.WriteFile(archiveSentinel, []byte("existing archive remains exact\n"), 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(archive, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(archive, 0700) })
	// This ordinary unprivileged test cannot write the unrelated destination.
	// It supplies an actual filesystem refusal, not an injected capture error.
	probe := filepath.Join(archive, "must-not-be-created")
	if err := os.WriteFile(probe, nil, 0600); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("surplus archive fixture is not read-only for caller: %v", err)
	}
	assertSurplus := func() {
		t.Helper()
		if got, err := os.ReadFile(manifestPath); err != nil || !bytes.Equal(got, retainedBytes) {
			t.Fatalf("shared ownership record changed: %v", err)
		}
		if got, err := os.ReadFile(surplusDatabase); err != nil || !bytes.Equal(got, surplusBytes) {
			t.Fatalf("unselected protected history changed: %v", err)
		}
		for _, path := range []string{surplus.HostHome, surplus.StateRoot} {
			if got, err := os.ReadFile(filepath.Join(path, "retained-surplus")); err != nil || string(got) != "unrelated state stays exact\n" {
				t.Fatalf("surplus state changed: %s %v", path, err)
			}
		}
		entries, err := os.ReadDir(archive)
		if err != nil || len(entries) != 1 || entries[0].Name() != "existing-archive" {
			t.Fatalf("unselected archive was mutated: %v %v", entries, err)
		}
		if got, err := os.ReadFile(archiveSentinel); err != nil || string(got) != "existing archive remains exact\n" {
			t.Fatalf("archive bytes changed: %v", err)
		}
		info, err := os.Stat(archive)
		if err != nil || info.Mode().Perm() != 0500 {
			t.Fatalf("unselected archive permissions changed: %v", err)
		}
		for _, path := range []string{surplus.HostWorktree, filepath.Join(worktreeRoot, ".devkit", "git", "agent4", "ouroboros-ide.git")} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("unselected slot was constructed: %s %v", path, err)
			}
		}
	}
	// A killed unreaped child still answers kill(pid, 0). Bind its actual
	// kernel start identity and reject zombie/dead states for preservation.
	processStart := map[int]string{}
	readProcessState := func(command *exec.Cmd) (string, string) {
		t.Helper()
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", command.Process.Pid))
		if err != nil {
			t.Fatalf("inspect test-owned process: %v", err)
		}
		close := strings.LastIndexByte(string(data), ')')
		if close < 0 {
			t.Fatal("test-owned process stat has no command boundary")
		}
		fields := strings.Fields(string(data[close+1:]))
		if len(fields) < 20 {
			t.Fatal("test-owned process start identity unavailable")
		}
		return fields[0], fields[19]
	}
	assertRunning := func(command *exec.Cmd) {
		t.Helper()
		state, start := readProcessState(command)
		if start != processStart[command.Process.Pid] || state == "Z" || state == "X" || state == "x" {
			t.Fatalf("preserved process %d lost live start identity: state=%s start=%s", command.Process.Pid, state, start)
		}
	}
	start := func(index int, home, cwd string) *exec.Cmd {
		t.Helper()
		command := exec.Command("sleep", "300")
		command.Env = append(os.Environ(), "DEVKIT_NATIVE_AGENT="+strconv.Itoa(index), "HOME="+home, "CODEX_HOME="+filepath.Join(home, ".codex"))
		command.Dir = cwd
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		_, processStart[command.Process.Pid] = readProcessState(command)
		t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
		return command
	}
	run := func(index int) ([]byte, []byte, error) {
		command := exec.Command(packageDevctl, "-p", "dev-all", "native", "reset", "--repo", "ouroboros-ide", "--index", strconv.Itoa(index), "--workspace-root", filepath.Join(worktreeRoot, fmt.Sprintf("agent%d", index)), "--format", "json")
		command.Env = env
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		return stdout.Bytes(), stderr.Bytes(), err
	}
	selectedDatabase := filepath.Join(selected.HostHome, ".codex", "goals_1.sqlite")
	runNativeFixtureCommand(t, "sqlite3", selectedDatabase, "CREATE TABLE goals (goal TEXT); INSERT INTO goals VALUES ('selected-slot2-goal');")
	selectedHistory, err := os.ReadFile(selectedDatabase)
	if err != nil {
		t.Fatal(err)
	}
	selectedObject, err := os.Stat(selectedDatabase)
	if err != nil {
		t.Fatal(err)
	}
	selectedProcess := start(2, selected.HostHome, selected.HostWorktree)
	assertSelectedIntact := func() {
		t.Helper()
		assertRunning(selectedProcess)
		if got, err := os.ReadFile(selectedDatabase); err != nil || !bytes.Equal(got, selectedHistory) {
			t.Fatalf("refusal changed selected history: %v", err)
		}
	}
	incompatible := retained
	incompatible.Agents = append([]nativeagent.Spec(nil), retained.Agents...)
	incompatible.Agents[1].HostHome += "-foreign"
	if err := nativeagent.WriteManifest(manifestPath, incompatible, false); err != nil {
		t.Fatal(err)
	}
	if _, diagnostic, err := run(2); err == nil || !strings.Contains(string(diagnostic), "incompatible with selected reset geometry") {
		t.Fatalf("incompatible selected binding admitted: %v %s", err, diagnostic)
	}
	assertSelectedIntact()
	if err := nativeagent.WriteManifest(manifestPath, retained, false); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"prepared", "committed"} {
		journal := manifestPath + ".shrink-transaction.json"
		journalBytes, err := json.Marshal(map[string]any{"schema": "devkit/native-manifest-shrink-transaction/v1", "status": status, "manifestPath": manifestPath, "prior": retained, "replacement": current, "retiredIndices": []int{4}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(journal, journalBytes, 0600); err != nil {
			t.Fatal(err)
		}
		if _, diagnostic, err := run(2); err == nil || !strings.Contains(string(diagnostic), "pending shared manifest shrink transaction") {
			t.Fatalf("overlapping %s transaction admitted: %v %s", status, err, diagnostic)
		}
		assertSelectedIntact()
		if got, err := os.ReadFile(journal); err != nil || !bytes.Equal(got, journalBytes) {
			t.Fatalf("selected reset recovered global transaction: %v", err)
		}
		if err := os.Remove(journal); err != nil {
			t.Fatal(err)
		} // Test-owned fixture, never a runtime repair.
	}
	blocker := start(4, selected.HostHome, selected.HostWorktree)
	if _, diagnostic, err := run(2); err == nil || !strings.Contains(string(diagnostic), "unowned active process") {
		t.Fatalf("unowned selected-state contact admitted: %v %s", err, diagnostic)
	}
	assertSelectedIntact()
	if err := blocker.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = blocker.Wait()
	assertSurplus()
	stdout, diagnostic, err := run(2)
	if err != nil {
		t.Fatalf("actual slot2 reconstruction depended on surplus archive: %v\n%s\n%s", err, diagnostic, stdout)
	}
	var receipt struct {
		Index    int    `json:"index"`
		Status   string `json:"status"`
		Head     string `json:"head"`
		Capacity struct {
			Total        int `json:"total"`
			RuntimeReady int `json:"runtime_ready"`
		} `json:"capacity"`
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout))
	if err := decoder.Decode(&receipt); err != nil {
		t.Fatal(err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatal("reset emitted extra stdout")
	}
	if receipt.Index != 2 || receipt.Status != "ready" || receipt.Capacity.Total != 1 || receipt.Capacity.RuntimeReady != 1 {
		t.Fatalf("surplus was counted as available capacity: %+v", receipt)
	}
	if err := selectedProcess.Wait(); err == nil {
		t.Fatal("selected process was not retired")
	}
	if !strings.Contains(string(diagnostic), "codex_gui_history status=retained-in-place agent=2 home="+selected.HostHome) {
		t.Fatalf("selected reset did not report retained storage: %s", diagnostic)
	}
	afterObject, err := os.Stat(selectedDatabase)
	if err != nil || !os.SameFile(selectedObject, afterObject) {
		t.Fatalf("selected reset replaced its protected history object: %v", err)
	}
	if got, err := os.ReadFile(selectedDatabase); err != nil || !bytes.Equal(got, selectedHistory) {
		t.Fatalf("actual selected history bytes changed after reconstruction: %v", err)
	}
	if got := runNativeFixtureCommand(t, "sqlite3", selectedDatabase, "PRAGMA integrity_check; SELECT goal FROM goals; INSERT INTO goals VALUES ('selected-slot2-later-goal'); SELECT count(*) FROM goals;"); got != "ok\nselected-slot2-goal\n2\n" {
		t.Fatalf("retained selected goal is not readable and writable: %q", got)
	}
	selectedHistoryAfterUpdate, err := os.ReadFile(selectedDatabase)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(diagnostic), "selected-slot2-goal") {
		t.Fatal("selected reset disclosed private goal content")
	}
	if got := strings.TrimSpace(runNativeFixtureCommand(t, "git", "-C", selected.HostWorktree, "rev-parse", "HEAD")); got != receipt.Head {
		t.Fatal("reconstructed actual HEAD differs from receipt")
	}
	if got := strings.TrimSpace(runNativeFixtureCommand(t, "git", "-C", selected.HostWorktree, "status", "--porcelain=v1")); got != "" {
		t.Fatalf("reconstructed selected root dirty: %s", got)
	}
	assertSurplus()
	// The second actual reconstruction must also preserve a running surplus.
	activeSurplus := start(4, surplus.HostHome, surplus.HostHome)
	secondSelected := start(2, selected.HostHome, selected.HostWorktree)
	if stdout, diagnostic, err := run(2); err != nil {
		t.Fatalf("active unrelated slot blocked selected reconstruction: %v\n%s\n%s", err, diagnostic, stdout)
	}
	if err := secondSelected.Wait(); err == nil {
		t.Fatal("second selected process was not retired")
	}
	afterSecond, err := os.Stat(selectedDatabase)
	if err != nil || !os.SameFile(selectedObject, afterSecond) {
		t.Fatalf("second selected reset replaced its retained history object: %v", err)
	}
	if got, err := os.ReadFile(selectedDatabase); err != nil || !bytes.Equal(got, selectedHistoryAfterUpdate) {
		t.Fatalf("second selected reset changed usable retained history: %v", err)
	}
	if got := runNativeFixtureCommand(t, "sqlite3", selectedDatabase, "PRAGMA integrity_check; SELECT goal FROM goals ORDER BY rowid;"); got != "ok\nselected-slot2-goal\nselected-slot2-later-goal\n" {
		t.Fatalf("second selected reset lost the later goal update: %q", got)
	}
	assertRunning(activeSurplus)
	assertSurplus()
	if _, diagnostic, err := run(4); err == nil || !strings.Contains(string(diagnostic), "outside declared capacity 1..3") {
		t.Fatalf("retained slot4 was reactivated: %v %s", err, diagnostic)
	}
	assertSurplus()
	t.Log("actual package CLI reconstructed slot2 twice with read-only surplus history, retained the same usable selected SQLite object and later update plus exact four-slot manifest/state/history, preserved active slot4, and refused selected mismatch, unowned contact, overlapping transactions and out-of-range reactivation")
}

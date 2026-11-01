package main

import (
	"bytes"
	"crypto/sha256"
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
	var historyManifestPath string
	for _, line := range strings.Split(string(diagnostic), "\n") {
		if strings.HasPrefix(line, "codex_gui_history_custody ") && strings.Contains(line, " status=captured ") {
			for _, field := range strings.Fields(line) {
				if value, ok := strings.CutPrefix(field, "manifest="); ok {
					historyManifestPath = value
				}
			}
		}
	}
	raw, err := os.ReadFile(historyManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var captured struct {
		Index int                             `json:"agent_index"`
		Kind  string                          `json:"reset_kind"`
		Files []struct{ Path, SHA256 string } `json:"files"`
	}
	if err := json.Unmarshal(raw, &captured); err != nil {
		t.Fatal(err)
	}
	if captured.Index != 2 || captured.Kind != "selected-slot-reset" || len(captured.Files) != 1 || captured.Files[0].Path != "goals_1.sqlite" {
		t.Fatalf("wrong selected custody: %s", raw)
	}
	payload := filepath.Join(filepath.Dir(historyManifestPath), "payload", "goals_1.sqlite")
	got, err := os.ReadFile(payload)
	if err != nil || !bytes.Equal(got, selectedHistory) || fmt.Sprintf("%x", sha256.Sum256(got)) != captured.Files[0].SHA256 {
		t.Fatalf("actual selected history lost after reconstruction: %v", err)
	}
	if got := runNativeFixtureCommand(t, "sqlite3", payload, "PRAGMA integrity_check; SELECT goal FROM goals;"); got != "ok\nselected-slot2-goal\n" {
		t.Fatalf("protected goal not readable after reset: %q", got)
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
	assertRunning(activeSurplus)
	assertSurplus()
	if _, diagnostic, err := run(4); err == nil || !strings.Contains(string(diagnostic), "outside declared capacity 1..3") {
		t.Fatalf("retained slot4 was reactivated: %v %s", err, diagnostic)
	}
	assertSurplus()
	t.Log("actual package CLI reconstructed slot2 twice with read-only surplus history, retained exact four-slot manifest/state/history, preserved active slot4, and refused selected mismatch, unowned contact, overlapping transactions and out-of-range reactivation")
}

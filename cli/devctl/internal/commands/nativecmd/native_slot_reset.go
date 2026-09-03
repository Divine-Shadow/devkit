package nativecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/execx"
	"devkit/cli/devctl/internal/gitauthority"
	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	"devkit/cli/devctl/internal/runtime/broker"
	"devkit/cli/devctl/internal/runtime/capacity"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/runtime/readiness"
	wtx "devkit/cli/devctl/internal/worktrees"
)

type nativeSlotResetArgs struct {
	repo          string
	index         int
	workspaceRoot string
	guiTargetID   string
	format        string
}

func parseNativeSlotResetArgs(ctx *cmdregistry.Context) (nativeSlotResetArgs, error) {
	parsed := nativeSlotResetArgs{}
	seen := map[string]bool{}
	for i := 1; i < len(ctx.Args); i++ {
		option := ctx.Args[i]
		if seen[option] {
			return parsed, fmt.Errorf("native reset rejects duplicate option %s", option)
		}
		switch ctx.Args[i] {
		case "--repo":
			seen[option] = true
			if i+1 >= len(ctx.Args) || strings.HasPrefix(ctx.Args[i+1], "--") {
				return parsed, fmt.Errorf("--repo requires a value")
			}
			parsed.repo = strings.TrimSpace(ctx.Args[i+1])
			i++
		case "--index":
			seen[option] = true
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--index requires a value")
			}
			index, err := strconv.Atoi(ctx.Args[i+1])
			if err != nil || index < 1 {
				return parsed, fmt.Errorf("--index must be a positive integer")
			}
			parsed.index = index
			i++
		case "--workspace-root":
			seen[option] = true
			if i+1 >= len(ctx.Args) || strings.HasPrefix(ctx.Args[i+1], "--") {
				return parsed, fmt.Errorf("--workspace-root requires a value")
			}
			parsed.workspaceRoot = strings.TrimSpace(ctx.Args[i+1])
			if parsed.workspaceRoot == "" {
				return parsed, fmt.Errorf("--workspace-root requires a value")
			}
			i++
		case "--gui-target-id":
			seen[option] = true
			if i+1 >= len(ctx.Args) || strings.HasPrefix(ctx.Args[i+1], "--") {
				return parsed, fmt.Errorf("--gui-target-id requires a value")
			}
			parsed.guiTargetID = strings.TrimSpace(ctx.Args[i+1])
			if parsed.guiTargetID == "" || parsed.guiTargetID != ctx.Args[i+1] {
				return parsed, fmt.Errorf("--gui-target-id must be non-empty and canonical")
			}
			i++
		case "--format":
			seen[option] = true
			if i+1 >= len(ctx.Args) || strings.HasPrefix(ctx.Args[i+1], "--") {
				return parsed, fmt.Errorf("--format requires a value")
			}
			parsed.format = strings.TrimSpace(ctx.Args[i+1])
			if parsed.format != "json" {
				return parsed, fmt.Errorf("--format must be json")
			}
			i++
		default:
			return parsed, fmt.Errorf("native reset rejects caller lifecycle override %s", ctx.Args[i])
		}
	}
	if parsed.repo == "" {
		return parsed, fmt.Errorf("native reset requires --repo")
	}
	if parsed.index < 1 {
		return parsed, fmt.Errorf("native reset requires --index")
	}
	if parsed.workspaceRoot == "" {
		return parsed, fmt.Errorf("native reset requires --workspace-root")
	}
	if parsed.format != "json" {
		return parsed, fmt.Errorf("native reset requires --format json")
	}
	return parsed, nil
}

func nativeSlotResetLifecycleArgs(parsed nativeSlotResetArgs, repo, baseBranch, branchPrefix string) lifecycleArgs {
	return lifecycleArgs{
		repo:             repo,
		baseBranch:       baseBranch,
		branchPrefix:     branchPrefix,
		guiTargetID:      parsed.guiTargetID,
		requireGUIConfig: parsed.guiTargetID == "",
	}
}

func validateNativeSlotWorkspaceRoot(value string, selectedPlan nativeplan.Plan) (string, error) {
	workspaceRoot := strings.TrimSpace(value)
	if workspaceRoot == "" || !filepath.IsAbs(workspaceRoot) || filepath.Clean(workspaceRoot) != workspaceRoot {
		return "", fmt.Errorf("native reset workspace root must be absolute and canonical: %s", value)
	}
	slashRoot := filepath.ToSlash(workspaceRoot)
	if workspaceRoot == string(filepath.Separator) ||
		workspaceRoot == "/home/bayesartre/dev" ||
		slashRoot == "/mnt" ||
		strings.HasPrefix(slashRoot, "/mnt/") {
		return "", fmt.Errorf("native reset workspace root is unsafe or shared: %s", workspaceRoot)
	}
	sharedRoots := []string{
		selectedPlan.DevkitHostRoot,
		selectedPlan.RuntimeAuthorityRoot,
		selectedPlan.HostWorktreeRoot,
		selectedPlan.HostStateRoot,
		filepath.Dir(filepath.Clean(selectedPlan.HostWorktreeRoot)),
		filepath.Dir(filepath.Clean(selectedPlan.HostStateRoot)),
	}
	for _, shared := range sharedRoots {
		shared = filepath.Clean(strings.TrimSpace(shared))
		if shared != "" && shared != "." && workspaceRoot == shared {
			return "", fmt.Errorf("native reset workspace root rejects shared collection root %s", workspaceRoot)
		}
	}
	expected := filepath.Dir(filepath.Clean(selectedPlan.Agent.HostWorktree))
	if workspaceRoot != expected {
		return "", fmt.Errorf("native reset workspace root must equal the selected source-derived worktree parent %s: %s", expected, workspaceRoot)
	}
	current := string(filepath.Separator)
	relative := strings.TrimPrefix(workspaceRoot, current)
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("native reset workspace root component does not exist: %s", current)
		}
		if err != nil {
			return "", fmt.Errorf("inspect native reset workspace root component %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("native reset workspace root traverses a symlink or junction: %s", current)
		}
		if current == workspaceRoot && !info.IsDir() {
			return "", fmt.Errorf("native reset workspace root must be a real directory: %s", workspaceRoot)
		}
	}
	return workspaceRoot, nil
}

type nativeSlotProcessIdentity struct {
	index           int
	hostWorktree    string
	sandboxWorktree string
	hostHome        string
	sandboxHome     string
	stateRoot       string
	sandboxState    string
}

type nativeSlotProcessPlan struct {
	identity nativeSlotProcessIdentity
	pids     []int
	starts   map[int]string
	dryRun   bool
}

type nativeSlotDestructiveBoundary struct {
	stopSelected    func() error
	inspectIdle     func() error
	snapshotHistory func() error
	applyPlan       func() error
}

// executeNativeSlotDestructiveBoundary keeps the selected-slot deletion gate
// explicit and independently testable. The same process identity is re-read
// after stop and after the cold snapshot before the reset plan may delete it.
func executeNativeSlotDestructiveBoundary(boundary nativeSlotDestructiveBoundary) error {
	if boundary.stopSelected == nil || boundary.inspectIdle == nil || boundary.snapshotHistory == nil || boundary.applyPlan == nil {
		return fmt.Errorf("selected native slot destructive boundary is incomplete")
	}
	if err := boundary.stopSelected(); err != nil {
		return err
	}
	if err := boundary.inspectIdle(); err != nil {
		return fmt.Errorf("selected native slot post-stop process recheck: %w", err)
	}
	if err := boundary.snapshotHistory(); err != nil {
		return fmt.Errorf("selected native slot Codex GUI history custody: %w", err)
	}
	if err := boundary.inspectIdle(); err != nil {
		return fmt.Errorf("selected native slot post-history process recheck: %w", err)
	}
	return boundary.applyPlan()
}

type nativeProc struct {
	pid     int
	ppid    int
	start   string
	environ map[string]string
	touches bool
}

var (
	nativeSlotProcRoot          = "/proc"
	nativeSlotMutationRename    = os.Rename
	nativeSlotMutationRemoveAll = os.RemoveAll
)

func parseNativeProcEnviron(data []byte) map[string]string {
	result := map[string]string{}
	for _, field := range strings.Split(string(data), "\x00") {
		key, value, ok := strings.Cut(field, "=")
		if ok {
			result[key] = value
		}
	}
	return result
}

func readNativeProcIdentity(path string) (int, string) {
	data, err := os.ReadFile(filepath.Join(path, "stat"))
	if err != nil {
		return 0, ""
	}
	text := string(data)
	close := strings.LastIndex(text, ")")
	if close < 0 {
		return 0, ""
	}
	fields := strings.Fields(text[close+1:])
	if len(fields) < 2 {
		return 0, ""
	}
	ppid, _ := strconv.Atoi(fields[1])
	start := ""
	// /proc/<pid>/stat field 22 is process start time. fields begins at field
	// 3 after the parenthesized comm value, so start time is index 19.
	if len(fields) > 19 {
		start = fields[19]
	}
	return ppid, start
}

func nativePathTouchesTargets(path string, targets []string) bool {
	path = filepath.Clean(strings.TrimSuffix(path, " (deleted)"))
	if path == "" || path == "." {
		return false
	}
	for _, target := range targets {
		target = filepath.Clean(strings.TrimSpace(target))
		if target == "" || target == "." {
			continue
		}
		rel, err := filepath.Rel(target, path)
		if err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
			return true
		}
	}
	return false
}

func nativeProcTouchesTargets(procPath string, targets []string) bool {
	for _, name := range []string{"cwd", "root", "exe"} {
		path, err := os.Readlink(filepath.Join(procPath, name))
		if err == nil && nativePathTouchesTargets(path, targets) {
			return true
		}
	}
	entries, err := os.ReadDir(filepath.Join(procPath, "fd"))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		path, err := os.Readlink(filepath.Join(procPath, "fd", entry.Name()))
		if err == nil && nativePathTouchesTargets(path, targets) {
			return true
		}
	}
	return false
}

func nativeProcHasSlotIdentity(env map[string]string, identity nativeSlotProcessIdentity) bool {
	if env["DEVKIT_NATIVE_AGENT"] != strconv.Itoa(identity.index) {
		return false
	}
	home := filepath.Clean(strings.TrimSpace(env["HOME"]))
	for _, expected := range []string{identity.hostHome, identity.sandboxHome} {
		expected = filepath.Clean(strings.TrimSpace(expected))
		// CODEX_HOME is a child-runtime concern: governed Product work may replace
		// it with a generated role home while retaining the Devkit slot's exact
		// agent and HOME identity. Do not make that mutable child context part of
		// the slot-root ownership witness.
		if expected != "" && expected != "." && home == expected {
			return true
		}
	}
	return false
}

func planNativeSlotProcesses(identity nativeSlotProcessIdentity, dryRun bool) (*nativeSlotProcessPlan, error) {
	targets := []string{
		identity.hostWorktree,
		identity.sandboxWorktree,
		identity.hostHome,
		identity.sandboxHome,
		identity.stateRoot,
		identity.sandboxState,
	}
	entries, err := os.ReadDir(nativeSlotProcRoot)
	if err != nil {
		return nil, fmt.Errorf("read native process inventory: %w", err)
	}
	processes := map[int]nativeProc{}
	owned := map[int]bool{}
	starts := map[int]string{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid < 1 || pid == os.Getpid() {
			continue
		}
		path := filepath.Join(nativeSlotProcRoot, entry.Name())
		environData, environErr := os.ReadFile(filepath.Join(path, "environ"))
		environ := map[string]string{}
		if environErr == nil {
			environ = parseNativeProcEnviron(environData)
		}
		touches := nativeProcTouchesTargets(path, targets)
		ppid, start := readNativeProcIdentity(path)
		processes[pid] = nativeProc{pid: pid, ppid: ppid, start: start, environ: environ, touches: touches}
		if nativeProcHasSlotIdentity(environ, identity) {
			owned[pid] = true
		}
	}

	// Descendants of an identified launcher are selected even if they sanitize
	// their environment. No ancestor or sibling process is acquired.
	changed := true
	for changed {
		changed = false
		for pid, process := range processes {
			if !owned[pid] && owned[process.ppid] {
				owned[pid] = true
				changed = true
			}
		}
	}
	for pid, process := range processes {
		if process.touches && !owned[pid] {
			return nil, fmt.Errorf("native slot reset found unowned active process %d touching selected slot paths", pid)
		}
	}
	pids := make([]int, 0, len(owned))
	for pid := range owned {
		pids = append(pids, pid)
		starts[pid] = processes[pid].start
	}
	sort.Sort(sort.Reverse(sort.IntSlice(pids)))
	return &nativeSlotProcessPlan{identity: identity, pids: pids, starts: starts, dryRun: dryRun}, nil
}

func nativeProcessStillMatches(pid int, expectedStart string) (bool, error) {
	path := filepath.Join(nativeSlotProcRoot, strconv.Itoa(pid))
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("revalidate selected native process %d: %w", pid, err)
	}
	_, actualStart := readNativeProcIdentity(path)
	if expectedStart == "" || actualStart == "" {
		return false, fmt.Errorf("selected native process %d has no stable start-time identity", pid)
	}
	if actualStart != expectedStart {
		return false, fmt.Errorf("selected native process %d was replaced before signaling", pid)
	}
	return true, nil
}

func nativeProcessExists(pid int) bool {
	if data, err := os.ReadFile(filepath.Join(nativeSlotProcRoot, strconv.Itoa(pid), "stat")); err == nil {
		text := string(data)
		if close := strings.LastIndex(text, ")"); close >= 0 {
			fields := strings.Fields(text[close+1:])
			if len(fields) > 0 && fields[0] == "Z" {
				return false
			}
		}
	}
	err := syscall.Kill(pid, 0)
	return err == nil || !errors.Is(err, syscall.ESRCH)
}

func (plan *nativeSlotProcessPlan) Stop() error {
	if plan == nil {
		return fmt.Errorf("native slot process plan is required")
	}
	if plan.dryRun {
		for _, pid := range plan.pids {
			fmt.Fprintf(os.Stderr, "+ stop-native-slot-process %d\n", pid)
		}
		return nil
	}
	for _, pid := range plan.pids {
		matches, err := nativeProcessStillMatches(pid, plan.starts[pid])
		if err != nil {
			return err
		}
		if !matches {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("stop selected native slot process %d: %w", pid, err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		alive := false
		for _, pid := range plan.pids {
			alive = alive || nativeProcessExists(pid)
		}
		if !alive {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, pid := range plan.pids {
		if nativeProcessExists(pid) {
			matches, err := nativeProcessStillMatches(pid, plan.starts[pid])
			if err != nil {
				return err
			}
			if !matches {
				continue
			}
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		alive := false
		for _, pid := range plan.pids {
			alive = alive || nativeProcessExists(pid)
		}
		if !alive {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("selected native slot processes did not terminate: %v", plan.pids)
}

type nativeSlotResetStatus struct {
	Command      string           `json:"command"`
	Runtime      string           `json:"runtime"`
	Repo         string           `json:"repo"`
	Index        int              `json:"index"`
	Status       string           `json:"status"`
	Head         string           `json:"head,omitempty"`
	ManifestPath string           `json:"manifest_path,omitempty"`
	Capacity     capacity.Summary `json:"capacity"`
}

const nativeSlotResetJSONDiagnosticLimit = 64 * 1024

// nativeSlotResetJSONOutput reserves stdout for the one typed reset receipt.
// Fresh Git construction uses package-owned helpers that intentionally stream
// command output. In JSON mode that diagnostic output is redirected through a
// bounded pipe to stderr so it cannot prefix or suffix the machine receipt.
// Text mode retains the existing streaming behavior.
type nativeSlotResetJSONOutput struct {
	receipt          *os.File
	diagnosticReader *os.File
	diagnosticWriter *os.File
	diagnostics      *boundedNativeSlotResetDiagnostics
	done             chan struct{}
	copyErr          error
	active           bool
}

type boundedNativeSlotResetDiagnostics struct {
	destination io.Writer
	remaining   int
	truncated   bool
	err         error
}

func nativeSlotResetJSONDiagnosticTruncationMarker() string {
	return fmt.Sprintf("\n[native reset JSON diagnostic stdout truncated after %d bytes]\n", nativeSlotResetJSONDiagnosticLimit)
}

func (writer *boundedNativeSlotResetDiagnostics) writeDestination(payload []byte) {
	if len(payload) == 0 || writer.err != nil {
		return
	}
	written, err := writer.destination.Write(payload)
	if err != nil {
		writer.err = err
		return
	}
	if written != len(payload) {
		writer.err = io.ErrShortWrite
	}
}

func (writer *boundedNativeSlotResetDiagnostics) Write(payload []byte) (int, error) {
	total := len(payload)
	allowed := total
	if allowed > writer.remaining {
		allowed = writer.remaining
	}
	writer.writeDestination(payload[:allowed])
	writer.remaining -= allowed
	if allowed != total && !writer.truncated {
		writer.truncated = true
		writer.writeDestination([]byte(nativeSlotResetJSONDiagnosticTruncationMarker()))
	}
	// Always drain the pipe even if stderr is unavailable. close() surfaces the
	// first projection error after the child process has exited, avoiding a
	// blocked child that would hide the actual lifecycle failure.
	return total, nil
}

func reserveNativeSlotResetJSONOutput(format string) (*nativeSlotResetJSONOutput, error) {
	reserved := &nativeSlotResetJSONOutput{receipt: os.Stdout}
	if format != "json" {
		return reserved, nil
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("reserve native reset JSON stdout: %w", err)
	}
	reserved.diagnosticReader = reader
	reserved.diagnosticWriter = writer
	reserved.diagnostics = &boundedNativeSlotResetDiagnostics{
		destination: os.Stderr,
		remaining:   nativeSlotResetJSONDiagnosticLimit,
	}
	reserved.done = make(chan struct{})
	reserved.active = true
	os.Stdout = writer
	go func() {
		_, reserved.copyErr = io.Copy(reserved.diagnostics, reader)
		close(reserved.done)
	}()
	return reserved, nil
}

func (reserved *nativeSlotResetJSONOutput) close() error {
	if reserved == nil || !reserved.active {
		return nil
	}
	os.Stdout = reserved.receipt
	writeErr := reserved.diagnosticWriter.Close()
	<-reserved.done
	readErr := reserved.diagnosticReader.Close()
	reserved.active = false
	return errors.Join(writeErr, reserved.copyErr, reserved.diagnostics.err, readErr)
}

type nativeSlotResetLock struct {
	file *os.File
	path string
}

type nativeSlotMutationRoot struct {
	name string
	path string
}

func nativeSlotResetCoordinationRoot(worktreeRoot string) (string, error) {
	worktreeRoot = filepath.Clean(strings.TrimSpace(worktreeRoot))
	if worktreeRoot == "" || !filepath.IsAbs(worktreeRoot) {
		return "", fmt.Errorf("native reset coordination requires an absolute source-declared worktree root")
	}
	return filepath.Join(worktreeRoot, ".devkit", "git"), nil
}

func nativeSlotMutationRoots(
	resetOptions wtx.NativeSlotResetOptions,
	selectedPlan nativeplan.Plan,
	manifestPath string,
	brokerCfg broker.Config,
) ([]nativeSlotMutationRoot, error) {
	coordinationRoot, err := nativeSlotResetCoordinationRoot(resetOptions.WorktreeRoot)
	if err != nil {
		return nil, err
	}
	roots := []nativeSlotMutationRoot{
		{name: "selected-worktree-parent", path: filepath.Dir(selectedPlan.Agent.HostWorktree)},
		{name: "selected-state", path: selectedPlan.Agent.StateRoot},
		{name: "shared-git-coordination", path: coordinationRoot},
		{name: "shared-native-manifest", path: filepath.Dir(manifestPath)},
		{name: "shared-runtime-broker", path: brokerCfg.StateRoot},
	}
	if proxySocket := strings.TrimSpace(selectedPlan.Proxy.UnixSocket); proxySocket != "" {
		roots = append(roots, nativeSlotMutationRoot{
			name: "managed-egress",
			path: filepath.Dir(proxySocket),
		})
	}
	return roots, nil
}

func probeNativeSlotMutationRoot(root nativeSlotMutationRoot) (retErr error) {
	path := filepath.Clean(strings.TrimSpace(root.path))
	if strings.TrimSpace(root.name) == "" || path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("native slot mutation root %q requires a named absolute path", root.name)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create %s root %s: %w", root.name, path, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s root %s: %w", root.name, path, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s root %s must be a real directory", root.name, path)
	}
	probePath, err := os.MkdirTemp(path, ".devkit-native-reset-mutation-probe-")
	if err != nil {
		return fmt.Errorf("create mutation probe in %s root %s: %w", root.name, path, err)
	}
	stagedPath := probePath + ".staged"
	defer func() {
		for _, candidate := range []string{probePath, stagedPath} {
			if err := nativeSlotMutationRemoveAll(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
				retErr = errors.Join(retErr, fmt.Errorf("remove %s mutation probe %s: %w", root.name, candidate, err))
			}
		}
	}()
	child := filepath.Join(probePath, "existing-child")
	if err := os.Mkdir(child, 0o700); err != nil {
		return fmt.Errorf("create existing-child probe in %s root %s: %w", root.name, path, err)
	}
	payload := filepath.Join(child, "payload")
	if err := os.WriteFile(payload, []byte("disposable\n"), 0o600); err != nil {
		return fmt.Errorf("create delete-child probe in %s root %s: %w", root.name, path, err)
	}
	if err := nativeSlotMutationRename(probePath, stagedPath); err != nil {
		return fmt.Errorf("stage existing candidate in %s root %s: %w", root.name, path, err)
	}
	if err := nativeSlotMutationRename(stagedPath, probePath); err != nil {
		return fmt.Errorf("roll back staged candidate in %s root %s: %w", root.name, path, err)
	}
	if err := nativeSlotMutationRemoveAll(probePath); err != nil {
		return fmt.Errorf("discard existing candidate contents in %s root %s: %w", root.name, path, err)
	}
	if _, err := os.Lstat(probePath); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("mutation probe survived disposal in %s root %s", root.name, path)
	}
	return nil
}

func preflightNativeSlotMutationRoots(roots []nativeSlotMutationRoot) error {
	var result error
	for _, root := range roots {
		if err := probeNativeSlotMutationRoot(root); err != nil {
			result = errors.Join(result, err)
		}
	}
	if result != nil {
		return fmt.Errorf("native slot reconstruction mutation contract is unavailable: %w", result)
	}
	return nil
}

func acquireNativeSlotResetLock(coordinationRoot, project string) (*nativeSlotResetLock, error) {
	coordinationRoot = filepath.Clean(strings.TrimSpace(coordinationRoot))
	if coordinationRoot == "" || !filepath.IsAbs(coordinationRoot) {
		return nil, fmt.Errorf("native reset lock requires an absolute package-owned coordination root")
	}
	if err := os.MkdirAll(coordinationRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create native reset coordination root %s: %w", coordinationRoot, err)
	}
	info, err := os.Lstat(coordinationRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect native reset coordination root %s: %w", coordinationRoot, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("native reset coordination root %s must be a real directory", coordinationRoot)
	}
	lockPath := filepath.Join(coordinationRoot, "."+project+"-native-reset.lock")
	fd, err := syscall.Open(
		lockPath,
		syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return nil, fmt.Errorf("open package-owned native reset lock %s: %w", lockPath, err)
	}
	file := os.NewFile(uintptr(fd), lockPath)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("open package-owned native reset lock %s", lockPath)
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("another package-owned native reset is active")
		}
		return nil, fmt.Errorf("lock package-owned native reset %s: %w", lockPath, err)
	}
	return &nativeSlotResetLock{file: file, path: lockPath}, nil
}

func (lock *nativeSlotResetLock) release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	fd := int(lock.file.Fd())
	unlockErr := syscall.Flock(fd, syscall.LOCK_UN)
	closeErr := lock.file.Close()
	lock.file = nil
	return errors.Join(unlockErr, closeErr)
}

func printNativeSlotResetStatus(status nativeSlotResetStatus, format string, stdout io.Writer) error {
	if format == "json" {
		data, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, string(data))
		return err
	}
	if _, err := fmt.Fprintf(stdout, "runtime: %s\ncommand: %s\nrepo: %s\nindex: %d\nstatus: %s\n", status.Runtime, status.Command, status.Repo, status.Index, status.Status); err != nil {
		return err
	}
	if status.Head != "" {
		if _, err := fmt.Fprintf(stdout, "head: %s\n", status.Head); err != nil {
			return err
		}
	}
	if status.ManifestPath != "" {
		if _, err := fmt.Fprintf(stdout, "manifest: %s\n", status.ManifestPath); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(stdout, "runtime_ready: %d/%d\nrepo_ready: %d/%d\n", status.Capacity.RuntimeReady, status.Capacity.Total, status.Capacity.RepoReady, status.Capacity.Total)
	return err
}

func finishNativeSlotResetOutput(
	output *nativeSlotResetJSONOutput,
	status nativeSlotResetStatus,
	format string,
) error {
	if output == nil {
		return fmt.Errorf("native reset output reservation is required")
	}
	if err := output.close(); err != nil {
		return fmt.Errorf("finalize native reset diagnostic output: %w", err)
	}
	if err := printNativeSlotResetStatus(status, format, output.receipt); err != nil {
		return fmt.Errorf("write native reset %s receipt: %w", format, err)
	}
	return nil
}

func readNativeSlotManifest(path string) (bool, nativeagent.Manifest, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, syscall.ENOENT) {
		return false, nativeagent.Manifest{}, nil
	}
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return false, nativeagent.Manifest{}, fmt.Errorf("shared native manifest %s must be a regular non-symlink file", path)
		}
		return false, nativeagent.Manifest{}, fmt.Errorf("read shared native manifest %s: %w", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return false, nativeagent.Manifest{}, fmt.Errorf("read shared native manifest %s: invalid file descriptor", path)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, nativeagent.Manifest{}, fmt.Errorf("inspect shared native manifest %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return false, nativeagent.Manifest{}, fmt.Errorf("shared native manifest %s must be a regular non-symlink file", path)
	}
	var actual nativeagent.Manifest
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&actual); err != nil {
		return false, nativeagent.Manifest{}, fmt.Errorf("decode shared native manifest %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON value")
		}
		return false, nativeagent.Manifest{}, fmt.Errorf("decode shared native manifest %s: %w", path, err)
	}
	return true, actual, nil
}

func classifyNativeSlotManifestShrink(
	actual nativeagent.Manifest,
	expected nativeagent.Manifest,
	canonicalActual nativeagent.Manifest,
) ([]nativeagent.Spec, error) {
	if reflect.DeepEqual(actual, expected) {
		return nil, nil
	}
	if expected.Count < 1 || expected.Count != len(expected.Agents) {
		return nil, fmt.Errorf("source-derived native manifest has an invalid declared count")
	}
	if actual.Count <= expected.Count {
		return nil, fmt.Errorf(
			"shared native manifest count %d cannot be reconciled to source-derived count %d: automatic reconciliation is shrink-only",
			actual.Count,
			expected.Count,
		)
	}
	if actual.Count != len(actual.Agents) {
		return nil, fmt.Errorf(
			"shared native manifest count %d does not equal its agent geometry length %d",
			actual.Count,
			len(actual.Agents),
		)
	}
	if !reflect.DeepEqual(actual, canonicalActual) {
		return nil, fmt.Errorf(
			"shared native manifest is not the exact source-derived all-slot geometry for declared count %d",
			actual.Count,
		)
	}
	prefix := actual
	prefix.Count = expected.Count
	prefix.Agents = append([]nativeagent.Spec(nil), actual.Agents[:expected.Count]...)
	if !reflect.DeepEqual(prefix, expected) {
		return nil, fmt.Errorf(
			"shared native manifest does not preserve the exact source-derived prefix 1..%d",
			expected.Count,
		)
	}
	surplus := append([]nativeagent.Spec(nil), actual.Agents[expected.Count:]...)
	for offset, spec := range surplus {
		want := expected.Count + offset + 1
		if spec.ID.Index != want {
			return nil, fmt.Errorf(
				"shared native manifest surplus is not contiguous: position %d declares index %d",
				want,
				spec.ID.Index,
			)
		}
	}
	return surplus, nil
}

type nativeSlotManifestShrinkProcessPlanner func(nativeSlotProcessIdentity, bool) (*nativeSlotProcessPlan, error)

type nativeSlotManifestShrinkCommonKind string

const (
	nativeSlotManifestShrinkCommonAbsent         nativeSlotManifestShrinkCommonKind = "absent"
	nativeSlotManifestShrinkCommonLane           nativeSlotManifestShrinkCommonKind = "lane"
	nativeSlotManifestShrinkCommonLegacy         nativeSlotManifestShrinkCommonKind = "legacy"
	nativeSlotManifestShrinkCommonHistoricalRoot nativeSlotManifestShrinkCommonKind = "historical-root"
)

type nativeSlotManifestShrinkCommon struct {
	kind                     nativeSlotManifestShrinkCommonKind
	path                     string
	consumerPath             string
	matchingMetadata         string
	matchingConsumerMetadata string
	consumerGitFile          string
	branchRef                string
}

type nativeSlotManifestShrinkPathPair struct {
	host     string
	consumer string
}

const (
	nativeSlotManifestShrinkPointerFileLimit           = 4 << 10
	nativeSlotManifestShrinkConfigFileLimit            = 1 << 20
	nativeSlotManifestShrinkMetadataViewIndexFileLimit = 64 << 20
	nativeSlotManifestShrinkMetadataViewEntryLimit     = 16
	nativeSlotManifestShrinkMetadataViewCompanionLimit = 1
	nativeSlotManifestShrinkGitOutputLimit             = 4 << 20
)

type nativeSlotManifestShrinkGitHubSSHIdentity struct {
	owner      string
	repository string
}

func nativeSlotManifestShrinkGitHubSSHSegment(value string, owner bool) bool {
	if value == "" || len(value) > 255 || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '-' || (!owner && (character == '_' || character == '.')) {
			continue
		}
		return false
	}
	return true
}

func parseNativeSlotManifestShrinkGitHubSSHIdentity(raw string) (nativeSlotManifestShrinkGitHubSSHIdentity, bool) {
	if raw == "" || len(raw) > nativeSlotManifestShrinkPointerFileLimit || strings.TrimSpace(raw) != raw ||
		strings.ContainsAny(raw, "\\\r\n\x00%?#") {
		return nativeSlotManifestShrinkGitHubSSHIdentity{}, false
	}
	repositoryPath := ""
	const scpPrefix = "git@github.com:"
	if strings.HasPrefix(raw, scpPrefix) {
		repositoryPath = strings.TrimPrefix(raw, scpPrefix)
	} else {
		if !strings.HasPrefix(raw, "ssh://") {
			return nativeSlotManifestShrinkGitHubSSHIdentity{}, false
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "ssh" || parsed.Opaque != "" || parsed.User == nil ||
			parsed.User.Username() != "git" || parsed.User.String() != "git" || parsed.RawPath != "" ||
			parsed.RawQuery != "" || parsed.Fragment != "" || parsed.ForceQuery ||
			!strings.HasPrefix(parsed.Path, "/") {
			return nativeSlotManifestShrinkGitHubSSHIdentity{}, false
		}
		if parsed.Host != "github.com" && parsed.Host != "github.com:22" &&
			parsed.Host != "ssh.github.com:443" {
			return nativeSlotManifestShrinkGitHubSSHIdentity{}, false
		}
		repositoryPath = strings.TrimPrefix(parsed.Path, "/")
	}
	parts := strings.Split(repositoryPath, "/")
	if len(parts) != 2 || !strings.HasSuffix(parts[1], ".git") {
		return nativeSlotManifestShrinkGitHubSSHIdentity{}, false
	}
	repository := strings.TrimSuffix(parts[1], ".git")
	if !nativeSlotManifestShrinkGitHubSSHSegment(parts[0], true) ||
		!nativeSlotManifestShrinkGitHubSSHSegment(repository, false) {
		return nativeSlotManifestShrinkGitHubSSHIdentity{}, false
	}
	return nativeSlotManifestShrinkGitHubSSHIdentity{
		owner:      parts[0],
		repository: repository,
	}, true
}

func nativeSlotManifestShrinkOriginsEquivalent(
	manifest nativeagent.Manifest,
	left string,
	right string,
) bool {
	if left == right {
		return true
	}
	// This is a read-only migration allowance for the one known historical
	// Product topology and its v1 transitional common repository. It does not
	// normalize origins for v2 lane repositories or choose the URL used by the
	// fresh remote proof.
	if manifest.Project != "dev-all" || manifest.Repo != "ouroboros-ide" {
		return false
	}
	leftIdentity, leftOK := parseNativeSlotManifestShrinkGitHubSSHIdentity(left)
	rightIdentity, rightOK := parseNativeSlotManifestShrinkGitHubSSHIdentity(right)
	return leftOK && rightOK && leftIdentity == rightIdentity && leftIdentity.repository == manifest.Repo
}

func nativeSlotManifestShrinkDeclaredBranchRef(manifest nativeagent.Manifest, spec nativeagent.Spec) string {
	return "refs/heads/" + strings.TrimSpace(manifest.BranchPrefix) + strconv.Itoa(spec.ID.Index)
}

func nativeSlotManifestShrinkHistoricalBranchRefs(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
) []string {
	declared := nativeSlotManifestShrinkDeclaredBranchRef(manifest, spec)
	refs := []string{declared}
	// Before Devkit owned branch naming, Codex Desktop created these Product
	// lanes as codex/agentN/main. Admit only that one fully derived historical
	// spelling for the exact known migration domain.
	if manifest.Project == "dev-all" && manifest.Repo == "ouroboros-ide" &&
		manifest.BranchPrefix == "agent" && manifest.BaseBranch == "main" && spec.ID.Index > 0 {
		refs = append(refs, "refs/heads/codex/agent"+strconv.Itoa(spec.ID.Index)+"/main")
	}
	return refs
}

func nativeSlotManifestShrinkHistoricalMetadataBranchRef(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
	metadata string,
) (string, error) {
	data, err := readNativeSlotManifestShrinkRegularFile(
		fmt.Sprintf("surplus native slot %d historical HEAD", spec.ID.Index),
		filepath.Join(metadata, "HEAD"),
		nativeSlotManifestShrinkPointerFileLimit,
	)
	if err != nil {
		return "", err
	}
	line, err := parseNativeSlotManifestShrinkPointerLine(
		fmt.Sprintf("surplus native slot %d historical HEAD", spec.ID.Index),
		data,
	)
	if err != nil || !strings.HasPrefix(line, "ref: ") {
		return "", fmt.Errorf("surplus native slot %d historical HEAD is detached or unsafe", spec.ID.Index)
	}
	branchRef := strings.TrimPrefix(line, "ref: ")
	for _, allowed := range nativeSlotManifestShrinkHistoricalBranchRefs(manifest, spec) {
		if branchRef == allowed {
			return branchRef, nil
		}
	}
	return "", fmt.Errorf(
		"surplus native slot %d historical HEAD %s is outside its exact declared and migration branch identities",
		spec.ID.Index,
		branchRef,
	)
}

func (pair nativeSlotManifestShrinkPathPair) matchesRaw(candidate, relativeBase string) bool {
	// Consumer spellings are bwrap targets and may not exist host-side. Absolute
	// pointers therefore match only the exact manifest-derived byte spelling.
	// A relative pointer remains valid only when it is exactly Git's canonical
	// relative spelling for the host path; do not Clean caller-controlled text.
	if filepath.IsAbs(candidate) {
		return candidate == pair.host || (pair.consumer != "" && candidate == pair.consumer)
	}
	relativeHost, err := filepath.Rel(relativeBase, pair.host)
	return err == nil && candidate == relativeHost
}

func parseNativeSlotManifestShrinkPointerLine(label string, data []byte) (string, error) {
	if len(data) != 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	value := string(data)
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("%s must contain one exact path line", label)
	}
	return value, nil
}

func readNativeSlotManifestShrinkRegularFile(label string, path string, limit int64) ([]byte, error) {
	if limit < 1 {
		return nil, fmt.Errorf("%s has an invalid read limit", label)
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("%s must be a regular non-symlink file: %s", label, path)
		}
		return nil, fmt.Errorf("read %s %s: %w", label, path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("read %s %s: invalid file descriptor", label, path)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect %s %s: %w", label, path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular non-symlink file: %s", label, path)
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%s exceeds the bounded metadata-view file limit: %s", label, path)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s %s: %w", label, path, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds the bounded metadata-view file limit while reading: %s", label, path)
	}
	if int64(len(data)) != info.Size() {
		return nil, fmt.Errorf("%s changed while reading: %s", label, path)
	}
	return data, nil
}

func readNativeSlotManifestShrinkDirectoryEntries(label, directory string, maxEntries int) ([]os.DirEntry, error) {
	if maxEntries < 0 {
		return nil, fmt.Errorf("%s has an invalid entry limit", label)
	}
	fd, err := syscall.Open(directory, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_DIRECTORY, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("%s must be a real non-symlink directory: %s", label, directory)
		}
		return nil, fmt.Errorf("open %s %s: %w", label, directory, err)
	}
	directoryHandle := os.NewFile(uintptr(fd), directory)
	if directoryHandle == nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("open %s %s: invalid file descriptor", label, directory)
	}
	defer directoryHandle.Close()
	entries, readErr := directoryHandle.ReadDir(maxEntries + 1)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, fmt.Errorf("enumerate %s %s: %w", label, directory, readErr)
	}
	if len(entries) > maxEntries {
		return nil, fmt.Errorf("%s exceeds the bounded entry limit %d: %s", label, maxEntries, directory)
	}
	return entries, nil
}

func nativeSlotManifestShrinkHistoricalConsumerCommonDir(manifest nativeagent.Manifest) (string, error) {
	repo := strings.TrimSpace(manifest.Repo)
	if repo == "" || repo == "." || filepath.IsAbs(repo) || filepath.Clean(repo) != repo || filepath.Base(repo) != repo {
		return "", fmt.Errorf("surplus native slot repository identity is unsafe: %q", manifest.Repo)
	}
	root := strings.TrimSpace(manifest.SandboxWorktreeRoot)
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", fmt.Errorf("surplus native slot sandbox worktree root must be absolute and canonical")
	}
	return filepath.Join(filepath.Dir(root), repo, ".git"), nil
}

func nativeSlotManifestShrinkHistoricalGitFilePair(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
) (nativeSlotManifestShrinkPathPair, error) {
	hostRoot := strings.TrimSpace(manifest.HostWorktreeRoot)
	consumerRoot := strings.TrimSpace(manifest.SandboxWorktreeRoot)
	hostWorktree := strings.TrimSpace(spec.HostWorktree)
	consumerWorktree := strings.TrimSpace(spec.SandboxWorktree)
	for label, candidate := range map[string]string{
		"host worktree root":     hostRoot,
		"consumer worktree root": consumerRoot,
		"host worktree":          hostWorktree,
		"consumer worktree":      consumerWorktree,
	} {
		if candidate == "" || !filepath.IsAbs(candidate) || filepath.Clean(candidate) != candidate {
			return nativeSlotManifestShrinkPathPair{}, fmt.Errorf("surplus native slot %s must be absolute and canonical", label)
		}
	}
	relativeWorktree, err := filepath.Rel(hostRoot, hostWorktree)
	if err != nil || relativeWorktree == "." || relativeWorktree == ".." || filepath.IsAbs(relativeWorktree) ||
		strings.HasPrefix(relativeWorktree, ".."+string(filepath.Separator)) {
		return nativeSlotManifestShrinkPathPair{}, fmt.Errorf("surplus native slot host worktree is outside its declared root")
	}
	expectedRelative := filepath.Join(fmt.Sprintf("agent%d", spec.ID.Index), strings.TrimSpace(manifest.Repo))
	if relativeWorktree != expectedRelative || consumerWorktree != filepath.Join(consumerRoot, relativeWorktree) {
		return nativeSlotManifestShrinkPathPair{}, fmt.Errorf("surplus native slot host and consumer worktrees are not an exact manifest-derived pair")
	}
	return nativeSlotManifestShrinkPathPair{
		host:     filepath.Join(hostWorktree, ".git"),
		consumer: filepath.Join(consumerWorktree, ".git"),
	}, nil
}

func nativeSlotManifestShrinkHistoricalMetadataPair(
	common nativeSlotManifestShrinkPathPair,
	metadata string,
) (nativeSlotManifestShrinkPathPair, error) {
	relativeMetadata, err := filepath.Rel(common.host, metadata)
	if err != nil || relativeMetadata == "." || relativeMetadata == ".." || filepath.IsAbs(relativeMetadata) ||
		strings.HasPrefix(relativeMetadata, ".."+string(filepath.Separator)) ||
		filepath.Dir(relativeMetadata) != "worktrees" {
		return nativeSlotManifestShrinkPathPair{}, fmt.Errorf("historical surplus native slot metadata is outside its exact common repository")
	}
	return nativeSlotManifestShrinkPathPair{
		host:     metadata,
		consumer: filepath.Join(common.consumer, relativeMetadata),
	}, nil
}

func nativeSlotManifestShrinkCommonDir(manifest nativeagent.Manifest, spec nativeagent.Spec) (string, error) {
	repo := strings.TrimSpace(manifest.Repo)
	if repo == "" || repo == "." || filepath.IsAbs(repo) || filepath.Clean(repo) != repo || filepath.Base(repo) != repo {
		return "", fmt.Errorf("surplus native slot repository identity is unsafe: %q", manifest.Repo)
	}
	root := filepath.Clean(strings.TrimSpace(manifest.HostWorktreeRoot))
	if root == "" || !filepath.IsAbs(root) {
		return "", fmt.Errorf("surplus native slot worktree root must be absolute")
	}
	if spec.ID.Index < 1 {
		return "", fmt.Errorf("surplus native slot lane identity is unsafe: agent%d", spec.ID.Index)
	}
	return filepath.Join(root, ".devkit", "git", fmt.Sprintf("agent%d", spec.ID.Index), repo+".git"), nil
}

func nativeSlotManifestShrinkLegacyCommonDir(manifest nativeagent.Manifest) (string, error) {
	repo := strings.TrimSpace(manifest.Repo)
	if repo == "" || repo == "." || filepath.IsAbs(repo) || filepath.Clean(repo) != repo || filepath.Base(repo) != repo {
		return "", fmt.Errorf("surplus native slot repository identity is unsafe: %q", manifest.Repo)
	}
	root := filepath.Clean(strings.TrimSpace(manifest.HostWorktreeRoot))
	if root == "" || !filepath.IsAbs(root) {
		return "", fmt.Errorf("surplus native slot worktree root must be absolute")
	}
	return filepath.Join(root, ".devkit", "git", repo+".git"), nil
}

func nativeSlotManifestShrinkHistoricalRootCommonDir(manifest nativeagent.Manifest) (string, string, error) {
	repo := strings.TrimSpace(manifest.Repo)
	if repo == "" || repo == "." || filepath.IsAbs(repo) || filepath.Clean(repo) != repo || filepath.Base(repo) != repo {
		return "", "", fmt.Errorf("surplus native slot repository identity is unsafe: %q", manifest.Repo)
	}
	worktreeRoot := filepath.Clean(strings.TrimSpace(manifest.HostWorktreeRoot))
	if worktreeRoot == "" || !filepath.IsAbs(worktreeRoot) {
		return "", "", fmt.Errorf("surplus native slot worktree root must be absolute")
	}
	// Historical native setup registered linked lanes in the ordinary source
	// checkout immediately beside the source-declared worktree root. Deriving
	// this one path from both declared geometry components avoids accepting an
	// arbitrary ambient or caller-supplied common repository.
	rootCheckout := filepath.Join(filepath.Dir(worktreeRoot), repo)
	return rootCheckout, filepath.Join(rootCheckout, ".git"), nil
}

func nativeSlotManifestShrinkCommonExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect surplus native slot common Git repository %s: %w", path, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("surplus native slot common Git repository must be a real directory: %s", path)
	}
	return true, nil
}

func requireNativeSlotManifestShrinkCanonicalRealDirectory(label, path string) error {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("%s must be an absolute canonical path: %s", label, path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s %s: %w", label, path, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s must be a real directory: %s", label, path)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("canonicalize %s %s: %w", label, path, err)
	}
	if canonical != path {
		return fmt.Errorf("%s contains a symlinked path component: %s resolves to %s", label, path, canonical)
	}
	return nil
}

func nativeSlotManifestShrinkMatchingMetadata(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
	commonDir string,
	allowedQuarantines map[string]bool,
	gitFile nativeSlotManifestShrinkPathPair,
) (string, error) {
	expectedGitFile := filepath.Join(filepath.Clean(spec.HostWorktree), ".git")
	if gitFile.host == "" {
		gitFile.host = expectedGitFile
	}
	if gitFile.host != expectedGitFile {
		return "", fmt.Errorf("surplus native slot %d host Git link identity is inconsistent", spec.ID.Index)
	}
	metadataRoot := filepath.Join(commonDir, "worktrees")
	entries, err := os.ReadDir(metadataRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("enumerate surplus native slot Git metadata %s: %w", metadataRoot, err)
	}
	matchingMetadata := ""
	for _, entry := range entries {
		metadata := filepath.Join(metadataRoot, entry.Name())
		if allowedQuarantines[metadata] {
			continue
		}
		if strings.HasPrefix(entry.Name(), fmt.Sprintf(".devkit-reset-%s-agent%d-", manifest.Project, spec.ID.Index)) {
			return "", fmt.Errorf("surplus native slot %d has Git transaction quarantine residue %s", spec.ID.Index, metadata)
		}
		metadataInfo, statErr := os.Lstat(metadata)
		if statErr != nil {
			return "", fmt.Errorf("inspect surplus native slot Git metadata %s: %w", metadata, statErr)
		}
		if !metadataInfo.IsDir() || metadataInfo.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("surplus native slot Git metadata must be a real directory: %s", metadata)
		}
		gitdirData, readErr := readNativeSlotManifestShrinkRegularFile(
			fmt.Sprintf("surplus native slot %d reverse Git link", spec.ID.Index),
			filepath.Join(metadata, "gitdir"),
			nativeSlotManifestShrinkPointerFileLimit,
		)
		if readErr != nil {
			return "", fmt.Errorf("read surplus native slot Git metadata %s: %w", metadata, readErr)
		}
		candidate, parseErr := parseNativeSlotManifestShrinkPointerLine(
			fmt.Sprintf("surplus native slot %d reverse Git link", spec.ID.Index),
			gitdirData,
		)
		if parseErr != nil {
			return "", parseErr
		}
		if gitFile.matchesRaw(candidate, metadata) {
			if matchingMetadata != "" {
				return "", fmt.Errorf("surplus native slot %d has multiple registered Git worktree metadata entries", spec.ID.Index)
			}
			matchingMetadata = metadata
		}
	}
	return matchingMetadata, nil
}

func nativeSlotManifestShrinkHistoricalMetadataFromWorktree(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
	common nativeSlotManifestShrinkPathPair,
	gitFile nativeSlotManifestShrinkPathPair,
) (string, error) {
	data, err := readNativeSlotManifestShrinkRegularFile(
		fmt.Sprintf("surplus native slot %d Git link", spec.ID.Index),
		gitFile.host,
		nativeSlotManifestShrinkPointerFileLimit,
	)
	if err != nil {
		return "", fmt.Errorf("read surplus native slot %d Git link %s: %w", spec.ID.Index, gitFile.host, err)
	}
	line, err := parseNativeSlotManifestShrinkPointerLine(
		fmt.Sprintf("surplus native slot %d Git link", spec.ID.Index),
		data,
	)
	if err != nil || !strings.HasPrefix(line, "gitdir: ") {
		return "", fmt.Errorf("surplus native slot %d Git link has an unsafe target", spec.ID.Index)
	}
	selectedMetadata := strings.TrimPrefix(line, "gitdir: ")
	metadataName := filepath.Base(selectedMetadata)
	if metadataName == "" || metadataName == "." || metadataName == ".." {
		return "", fmt.Errorf("surplus native slot %d Git link has an unsafe target", spec.ID.Index)
	}
	if strings.HasPrefix(metadataName, fmt.Sprintf(".devkit-reset-%s-agent%d-", manifest.Project, spec.ID.Index)) {
		return "", fmt.Errorf("surplus native slot %d has Git transaction quarantine residue selected by its Git link", spec.ID.Index)
	}
	metadata, err := nativeSlotManifestShrinkHistoricalMetadataPair(
		common,
		filepath.Join(common.host, "worktrees", metadataName),
	)
	if err != nil {
		return "", err
	}
	if !metadata.matchesRaw(selectedMetadata, filepath.Dir(gitFile.host)) {
		return "", fmt.Errorf("surplus native slot %d Git link does not select its exact registered metadata", spec.ID.Index)
	}
	metadataInfo, err := os.Lstat(metadata.host)
	if err != nil {
		return "", fmt.Errorf("surplus native slot %d Git link does not select its exact registered metadata: %w", spec.ID.Index, err)
	}
	if !metadataInfo.IsDir() || metadataInfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("historical surplus native slot registered Git metadata must be a real directory: %s", metadata.host)
	}
	return metadata.host, nil
}

func nativeSlotManifestShrinkCommonBranchOID(
	commonDir string,
	branchRef string,
	operation *nativeSlotManifestShrinkRemoteProofOperation,
) (string, error) {
	branchLock := filepath.Join(commonDir, filepath.FromSlash(branchRef)+".lock")
	if _, err := os.Lstat(branchLock); err == nil {
		return "", fmt.Errorf("exact native slot branch domain %s has Git transaction lock residue %s", branchRef, branchLock)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect exact native slot branch domain lock %s: %w", branchLock, err)
	}
	capture := func(args ...string) (string, execx.Result, error) {
		if operation != nil {
			return operation.capture(args...)
		}
		output, result := execx.Capture(context.Background(), gitauthority.Executable(), args...)
		return output, result, nil
	}
	_, result, captureErr := capture("--git-dir", commonDir, "show-ref", "--verify", "--quiet", branchRef)
	if captureErr != nil {
		return "", fmt.Errorf("inspect exact native slot branch domain %s in %s: %w", branchRef, commonDir, captureErr)
	}
	switch result.Code {
	case 0:
		output, resolveResult, resolveErr := capture("--git-dir", commonDir, "rev-parse", "--verify", branchRef+"^{commit}")
		if resolveErr != nil || resolveResult.Code != 0 {
			return "", fmt.Errorf("resolve exact native slot branch domain %s in %s", branchRef, commonDir)
		}
		oid, parseErr := parseNativeSlotManifestShrinkPointerLine("native slot branch object ID", []byte(output))
		if parseErr != nil || (len(oid) != 40 && len(oid) != 64) {
			return "", fmt.Errorf("exact native slot branch domain %s in %s returned an invalid object ID", branchRef, commonDir)
		}
		for _, character := range oid {
			if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
				return "", fmt.Errorf("exact native slot branch domain %s in %s returned an invalid object ID", branchRef, commonDir)
			}
		}
		return oid, nil
	case 1:
		return "", nil
	default:
		return "", fmt.Errorf("inspect exact native slot branch domain %s in %s: git exit %d", branchRef, commonDir, result.Code)
	}
}

func nativeSlotManifestShrinkSingleConfigValue(label string, output string) (string, error) {
	values := strings.Split(output, "\x00")
	if len(values) != 2 || values[0] == "" || values[1] != "" ||
		strings.ContainsAny(values[0], "\r\n\x00") {
		return "", fmt.Errorf("%s must contain exactly one safe value", label)
	}
	return values[0], nil
}

func nativeSlotManifestShrinkLegacyMarkerMatches(
	manifest nativeagent.Manifest,
	marker []byte,
	origin string,
) bool {
	lines := strings.Split(string(marker), "\n")
	if len(lines) != 4 || lines[3] != "" ||
		lines[0] != "schema=devkit/native-owned-common-repository/v1" ||
		lines[1] != "repository="+strings.TrimSpace(manifest.Repo) ||
		!strings.HasPrefix(lines[2], "origin=") {
		return false
	}
	markerOrigin := strings.TrimPrefix(lines[2], "origin=")
	return nativeSlotManifestShrinkOriginsEquivalent(manifest, markerOrigin, origin)
}

func validateNativeSlotManifestShrinkLegacyCommon(
	manifest nativeagent.Manifest,
	commonDir string,
	expectedOrigin string,
) error {
	origin := strings.TrimSpace(expectedOrigin)
	if origin == "" || strings.ContainsAny(origin, "\r\n\x00") {
		return fmt.Errorf("legacy surplus native slot requires the source-declared origin")
	}
	root := filepath.Clean(strings.TrimSpace(manifest.HostWorktreeRoot))
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("canonicalize surplus native slot worktree root %s: %w", root, err)
	}
	canonicalCommon, err := filepath.EvalSymlinks(commonDir)
	if err != nil {
		return fmt.Errorf("canonicalize legacy surplus native slot common Git repository %s: %w", commonDir, err)
	}
	expectedCommon := filepath.Join(canonicalRoot, ".devkit", "git", strings.TrimSpace(manifest.Repo)+".git")
	if canonicalCommon != expectedCommon {
		return fmt.Errorf("legacy surplus native slot common Git repository escapes its exact package-owned path: %s", commonDir)
	}
	markerPath := filepath.Join(commonDir, "devkit-owned-common")
	marker, err := readNativeSlotManifestShrinkRegularFile(
		"legacy package-owned common repository marker",
		markerPath,
		nativeSlotManifestShrinkPointerFileLimit,
	)
	if err != nil {
		return err
	}
	if !nativeSlotManifestShrinkLegacyMarkerMatches(manifest, marker, origin) {
		return fmt.Errorf("legacy package-owned common repository %s identity does not match repository %s and its declared origin", commonDir, manifest.Repo)
	}
	git := gitauthority.Executable()
	bare, result := execx.Capture(context.Background(), git, "--git-dir", commonDir, "rev-parse", "--is-bare-repository")
	if result.Code != 0 || strings.TrimSpace(bare) != "true" {
		return fmt.Errorf("legacy package-owned common repository %s is not a bare Git repository", commonDir)
	}
	configPath := filepath.Join(commonDir, "config")
	if _, err := readNativeSlotManifestShrinkRegularFile(
		"legacy package-owned common repository Git config",
		configPath,
		nativeSlotManifestShrinkConfigFileLimit,
	); err != nil {
		return err
	}
	actualOrigins, result := execx.Capture(
		context.Background(),
		git,
		"config", "--file", configPath, "--no-includes", "--null", "--get-all", "remote.origin.url",
	)
	if result.Code != 0 {
		return fmt.Errorf("read legacy package-owned common repository origin %s: git exit %d", commonDir, result.Code)
	}
	actualOrigin, err := nativeSlotManifestShrinkSingleConfigValue(
		"legacy package-owned common repository origin",
		actualOrigins,
	)
	if err != nil {
		return fmt.Errorf("read legacy package-owned common repository origin %s: %w", commonDir, err)
	}
	if !nativeSlotManifestShrinkOriginsEquivalent(manifest, actualOrigin, origin) {
		return fmt.Errorf("legacy package-owned common repository %s origin %q does not match declared origin %q", commonDir, actualOrigin, origin)
	}
	return nil
}

func validateNativeSlotManifestShrinkHistoricalRootCommon(
	manifest nativeagent.Manifest,
	commonDir string,
	expectedOrigin string,
	operation *nativeSlotManifestShrinkRemoteProofOperation,
) error {
	origin := strings.TrimSpace(expectedOrigin)
	if origin == "" || strings.ContainsAny(origin, "\r\n\x00") {
		return fmt.Errorf("historical-root surplus native slot requires the source-declared origin")
	}
	rootCheckout, expectedCommon, err := nativeSlotManifestShrinkHistoricalRootCommonDir(manifest)
	if err != nil {
		return err
	}
	if commonDir != expectedCommon {
		return fmt.Errorf("historical-root surplus native slot common Git repository is not exact: got %s want %s", commonDir, expectedCommon)
	}
	for label, path := range map[string]string{
		"historical source root checkout":              rootCheckout,
		"historical source root common Git repository": commonDir,
	} {
		if err := requireNativeSlotManifestShrinkCanonicalRealDirectory(label, path); err != nil {
			return err
		}
	}
	if operation == nil {
		return fmt.Errorf("historical-root retirement requires one operation-bound current-remote proof")
	}
	actualCommon, result, captureErr := operation.capture("-C", rootCheckout, "rev-parse", "--absolute-git-dir")
	if captureErr != nil || result.Code != 0 || filepath.Clean(strings.TrimSpace(actualCommon)) != commonDir {
		return fmt.Errorf("historical source root checkout does not own exact common Git repository %s", commonDir)
	}
	actualRoot, result, captureErr := operation.capture("-C", rootCheckout, "rev-parse", "--show-toplevel")
	if captureErr != nil || result.Code != 0 || filepath.Clean(strings.TrimSpace(actualRoot)) != rootCheckout {
		return fmt.Errorf("historical source root checkout Git identity does not resolve exactly to %s", rootCheckout)
	}
	bare, result, captureErr := operation.capture("--git-dir", commonDir, "rev-parse", "--is-bare-repository")
	if captureErr != nil || result.Code != 0 || strings.TrimSpace(bare) != "false" {
		return fmt.Errorf("historical source root common Git repository %s must be the non-bare root checkout repository", commonDir)
	}
	configPath := filepath.Join(commonDir, "config")
	if _, err := readNativeSlotManifestShrinkRegularFile(
		"historical source root common Git config",
		configPath,
		nativeSlotManifestShrinkConfigFileLimit,
	); err != nil {
		return err
	}
	actualOrigins, result, captureErr := operation.capture(
		"config", "--file", configPath, "--no-includes", "--null", "--get-all", "remote.origin.url",
	)
	if captureErr != nil || result.Code != 0 {
		return fmt.Errorf("read historical source root common Git origin %s: git exit %d", commonDir, result.Code)
	}
	actualOrigin, err := nativeSlotManifestShrinkSingleConfigValue(
		"historical source root common Git origin",
		actualOrigins,
	)
	if err != nil {
		return fmt.Errorf("read historical source root common Git origin %s: %w", commonDir, err)
	}
	if !nativeSlotManifestShrinkOriginsEquivalent(manifest, actualOrigin, origin) {
		return fmt.Errorf(
			"historical source root common Git repository %s origin %q does not match declared origin %q",
			commonDir,
			actualOrigin,
			origin,
		)
	}
	return nil
}

func resolveNativeSlotManifestShrinkCommon(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
	expectedOrigin string,
	worktreeExists bool,
	allowedQuarantines map[string]bool,
	operation *nativeSlotManifestShrinkRemoteProofOperation,
) (nativeSlotManifestShrinkCommon, error) {
	laneCommonDir, err := nativeSlotManifestShrinkCommonDir(manifest, spec)
	if err != nil {
		return nativeSlotManifestShrinkCommon{}, err
	}
	legacyCommonDir, err := nativeSlotManifestShrinkLegacyCommonDir(manifest)
	if err != nil {
		return nativeSlotManifestShrinkCommon{}, err
	}
	_, historicalRootCommonDir, err := nativeSlotManifestShrinkHistoricalRootCommonDir(manifest)
	if err != nil {
		return nativeSlotManifestShrinkCommon{}, err
	}
	laneExists, err := nativeSlotManifestShrinkCommonExists(laneCommonDir)
	if err != nil {
		return nativeSlotManifestShrinkCommon{}, err
	}
	legacyExists, err := nativeSlotManifestShrinkCommonExists(legacyCommonDir)
	if err != nil {
		return nativeSlotManifestShrinkCommon{}, err
	}
	historicalRootExists, err := nativeSlotManifestShrinkCommonExists(historicalRootCommonDir)
	if err != nil {
		return nativeSlotManifestShrinkCommon{}, err
	}
	laneMetadata := ""
	hostGitFile := nativeSlotManifestShrinkPathPair{
		host: filepath.Join(filepath.Clean(spec.HostWorktree), ".git"),
	}
	if laneExists {
		laneMetadata, err = nativeSlotManifestShrinkMatchingMetadata(manifest, spec, laneCommonDir, allowedQuarantines, hostGitFile)
		if err != nil {
			return nativeSlotManifestShrinkCommon{}, err
		}
	}
	legacyMetadata := ""
	if legacyExists {
		legacyMetadata, err = nativeSlotManifestShrinkMatchingMetadata(manifest, spec, legacyCommonDir, allowedQuarantines, hostGitFile)
		if err != nil {
			return nativeSlotManifestShrinkCommon{}, err
		}
	}
	historicalRootMetadata := ""
	historicalRootConsumerCommonDir := ""
	historicalGitFile := hostGitFile
	if historicalRootExists {
		historicalRootConsumerCommonDir, err = nativeSlotManifestShrinkHistoricalConsumerCommonDir(manifest)
		if err != nil {
			return nativeSlotManifestShrinkCommon{}, err
		}
		historicalGitFile, err = nativeSlotManifestShrinkHistoricalGitFilePair(manifest, spec)
		if err != nil {
			return nativeSlotManifestShrinkCommon{}, err
		}
		if worktreeExists && laneMetadata == "" && legacyMetadata == "" {
			historicalRootMetadata, err = nativeSlotManifestShrinkHistoricalMetadataFromWorktree(
				manifest,
				spec,
				nativeSlotManifestShrinkPathPair{
					host:     historicalRootCommonDir,
					consumer: historicalRootConsumerCommonDir,
				},
				historicalGitFile,
			)
			if err != nil {
				return nativeSlotManifestShrinkCommon{}, err
			}
		}
	}
	if !worktreeExists {
		branchRef := nativeSlotManifestShrinkDeclaredBranchRef(manifest, spec)
		if laneMetadata != "" {
			return nativeSlotManifestShrinkCommon{
				kind:             nativeSlotManifestShrinkCommonLane,
				path:             laneCommonDir,
				matchingMetadata: laneMetadata,
				branchRef:        branchRef,
			}, nil
		}
		if laneExists {
			for _, lockPath := range []string{
				filepath.Join(laneCommonDir, "index.lock"),
				filepath.Join(laneCommonDir, "packed-refs.lock"),
				filepath.Join(laneCommonDir, "shallow.lock"),
				filepath.Join(laneCommonDir, filepath.FromSlash(branchRef)+".lock"),
			} {
				if _, lockErr := os.Lstat(lockPath); lockErr == nil {
					return nativeSlotManifestShrinkCommon{}, fmt.Errorf(
						"surplus native slot %d has Git transaction lock residue %s",
						spec.ID.Index,
						lockPath,
					)
				} else if !errors.Is(lockErr, os.ErrNotExist) {
					return nativeSlotManifestShrinkCommon{}, fmt.Errorf(
						"inspect surplus native slot Git transaction lock %s: %w",
						lockPath,
						lockErr,
					)
				}
			}
		}
		type branchDomain struct {
			kind      nativeSlotManifestShrinkCommonKind
			path      string
			metadata  string
			branchRef string
			oid       string
		}
		var domains []branchDomain
		for _, candidate := range []struct {
			kind       nativeSlotManifestShrinkCommonKind
			path       string
			metadata   string
			exists     bool
			branchRefs []string
		}{
			{
				kind: nativeSlotManifestShrinkCommonLane, path: laneCommonDir,
				metadata: laneMetadata, exists: laneExists, branchRefs: []string{branchRef},
			},
			{
				kind: nativeSlotManifestShrinkCommonLegacy, path: legacyCommonDir,
				metadata: legacyMetadata, exists: legacyExists, branchRefs: []string{branchRef},
			},
			{
				kind: nativeSlotManifestShrinkCommonHistoricalRoot, path: historicalRootCommonDir,
				exists: historicalRootExists, branchRefs: nativeSlotManifestShrinkHistoricalBranchRefs(manifest, spec),
			},
		} {
			if !candidate.exists {
				continue
			}
			for _, candidateBranchRef := range candidate.branchRefs {
				oid, branchErr := nativeSlotManifestShrinkCommonBranchOID(candidate.path, candidateBranchRef, operation)
				if branchErr != nil {
					return nativeSlotManifestShrinkCommon{}, branchErr
				}
				if oid != "" {
					domains = append(domains, branchDomain{
						kind:      candidate.kind,
						path:      candidate.path,
						metadata:  candidate.metadata,
						branchRef: candidateBranchRef,
						oid:       oid,
					})
				}
			}
		}
		if len(domains) > 0 {
			if operation == nil && len(domains) > 1 {
				return nativeSlotManifestShrinkCommon{}, fmt.Errorf(
					"surplus native slot %d competing branch-domain resolution requires one operation-bound current-remote proof",
					spec.ID.Index,
				)
			}
			preferred := domains[0]
			for _, domain := range domains[1:] {
				if (domain.kind == nativeSlotManifestShrinkCommonHistoricalRoot && preferred.kind != nativeSlotManifestShrinkCommonHistoricalRoot) ||
					(domain.kind == nativeSlotManifestShrinkCommonLegacy && preferred.kind == nativeSlotManifestShrinkCommonLane) {
					preferred = domain
				}
			}
			if operation != nil && len(domains) > 1 {
				baseBranch := strings.TrimSpace(manifest.BaseBranch)
				if baseBranch == "" {
					return nativeSlotManifestShrinkCommon{}, fmt.Errorf(
						"surplus native slot %d cannot prove branch domains without a source-derived base branch",
						spec.ID.Index,
					)
				}
				proof, proofErr := operation.ensureRemoteProof(manifest, preferred.path, baseBranch, expectedOrigin)
				if proofErr != nil {
					return nativeSlotManifestShrinkCommon{}, fmt.Errorf(
						"prepare current-remote proof for surplus native slot %d branch domains: %w",
						spec.ID.Index,
						proofErr,
					)
				}
				for _, domain := range domains {
					_, result, captureErr := operation.capture(
						"--git-dir", proof.proofDir,
						"merge-base", "--is-ancestor", domain.oid, proof.remoteOID,
					)
					if captureErr != nil || result.Code != 0 {
						return nativeSlotManifestShrinkCommon{}, fmt.Errorf(
							"surplus native slot %d branch %s (%s) from %s is not contained in current %s at %s",
							spec.ID.Index,
							domain.branchRef,
							domain.oid,
							domain.path,
							proof.remoteRef,
							proof.remoteOID,
						)
					}
				}
			}
			domains = []branchDomain{preferred}
		}
		var metadataDomains []branchDomain
		for _, candidate := range []branchDomain{
			{kind: nativeSlotManifestShrinkCommonLane, path: laneCommonDir, metadata: laneMetadata, branchRef: branchRef},
			{kind: nativeSlotManifestShrinkCommonLegacy, path: legacyCommonDir, metadata: legacyMetadata, branchRef: branchRef},
		} {
			if candidate.metadata != "" {
				metadataDomains = append(metadataDomains, candidate)
			}
		}
		if len(metadataDomains) > 1 ||
			(len(metadataDomains) == 1 && len(domains) == 1 && metadataDomains[0].kind != domains[0].kind) {
			return nativeSlotManifestShrinkCommon{}, fmt.Errorf(
				"surplus native slot %d has competing registration and branch common Git repositories",
				spec.ID.Index,
			)
		}
		selectedDomains := domains
		if len(selectedDomains) == 0 {
			selectedDomains = metadataDomains
		}
		if len(selectedDomains) == 1 {
			domain := selectedDomains[0]
			switch domain.kind {
			case nativeSlotManifestShrinkCommonLane:
				return nativeSlotManifestShrinkCommon{
					kind:             domain.kind,
					path:             domain.path,
					matchingMetadata: domain.metadata,
					branchRef:        domain.branchRef,
				}, nil
			case nativeSlotManifestShrinkCommonLegacy:
				if err := validateNativeSlotManifestShrinkLegacyCommon(manifest, domain.path, expectedOrigin); err != nil {
					return nativeSlotManifestShrinkCommon{}, err
				}
				return nativeSlotManifestShrinkCommon{
					kind:             domain.kind,
					path:             domain.path,
					matchingMetadata: domain.metadata,
					branchRef:        domain.branchRef,
				}, nil
			case nativeSlotManifestShrinkCommonHistoricalRoot:
				if err := validateNativeSlotManifestShrinkHistoricalRootCommon(manifest, domain.path, expectedOrigin, operation); err != nil {
					return nativeSlotManifestShrinkCommon{}, err
				}
				return nativeSlotManifestShrinkCommon{
					kind:         domain.kind,
					path:         domain.path,
					consumerPath: historicalRootConsumerCommonDir,
					branchRef:    domain.branchRef,
				}, nil
			}
		}
	}
	registrations := 0
	for _, metadata := range []string{laneMetadata, legacyMetadata, historicalRootMetadata} {
		if metadata != "" {
			registrations++
		}
	}
	if registrations > 1 {
		return nativeSlotManifestShrinkCommon{}, fmt.Errorf(
			"surplus native slot %d is registered in multiple exact common Git repositories",
			spec.ID.Index,
		)
	}
	if laneMetadata != "" || (laneExists && registrations == 0) {
		return nativeSlotManifestShrinkCommon{
			kind:             nativeSlotManifestShrinkCommonLane,
			path:             laneCommonDir,
			matchingMetadata: laneMetadata,
			branchRef:        nativeSlotManifestShrinkDeclaredBranchRef(manifest, spec),
		}, nil
	}
	if legacyMetadata != "" {
		if err := validateNativeSlotManifestShrinkLegacyCommon(manifest, legacyCommonDir, expectedOrigin); err != nil {
			return nativeSlotManifestShrinkCommon{}, err
		}
		return nativeSlotManifestShrinkCommon{
			kind:             nativeSlotManifestShrinkCommonLegacy,
			path:             legacyCommonDir,
			matchingMetadata: legacyMetadata,
			branchRef:        nativeSlotManifestShrinkDeclaredBranchRef(manifest, spec),
		}, nil
	}
	if historicalRootMetadata != "" {
		metadata := nativeSlotManifestShrinkPathPair{}
		if historicalRootMetadata != "" {
			metadata, err = nativeSlotManifestShrinkHistoricalMetadataPair(
				nativeSlotManifestShrinkPathPair{
					host:     historicalRootCommonDir,
					consumer: historicalRootConsumerCommonDir,
				},
				historicalRootMetadata,
			)
			if err != nil {
				return nativeSlotManifestShrinkCommon{}, err
			}
		}
		if err := validateNativeSlotManifestShrinkHistoricalRootCommon(manifest, historicalRootCommonDir, expectedOrigin, operation); err != nil {
			return nativeSlotManifestShrinkCommon{}, err
		}
		branchRef, err := nativeSlotManifestShrinkHistoricalMetadataBranchRef(
			manifest,
			spec,
			historicalRootMetadata,
		)
		if err != nil {
			return nativeSlotManifestShrinkCommon{}, err
		}
		return nativeSlotManifestShrinkCommon{
			kind:                     nativeSlotManifestShrinkCommonHistoricalRoot,
			path:                     historicalRootCommonDir,
			consumerPath:             historicalRootConsumerCommonDir,
			matchingMetadata:         historicalRootMetadata,
			matchingConsumerMetadata: metadata.consumer,
			consumerGitFile:          historicalGitFile.consumer,
			branchRef:                branchRef,
		}, nil
	}
	if worktreeExists {
		return nativeSlotManifestShrinkCommon{}, fmt.Errorf(
			"surplus native slot %d has a worktree without its package-owned common Git repository (neither exact lane, legacy, nor historical-root registration)",
			spec.ID.Index,
		)
	}
	return nativeSlotManifestShrinkCommon{kind: nativeSlotManifestShrinkCommonAbsent}, nil
}

func validateNativeSlotManifestShrinkWorktreeLink(
	spec nativeagent.Spec,
	common nativeSlotManifestShrinkCommon,
) error {
	gitFile := filepath.Join(filepath.Clean(spec.HostWorktree), ".git")
	data, err := readNativeSlotManifestShrinkRegularFile(
		fmt.Sprintf("surplus native slot %d Git link", spec.ID.Index),
		gitFile,
		nativeSlotManifestShrinkPointerFileLimit,
	)
	if err != nil {
		return fmt.Errorf("read surplus native slot %d Git link %s: %w", spec.ID.Index, gitFile, err)
	}
	line, err := parseNativeSlotManifestShrinkPointerLine(
		fmt.Sprintf("surplus native slot %d Git link", spec.ID.Index),
		data,
	)
	if err != nil || !strings.HasPrefix(line, "gitdir: ") {
		return fmt.Errorf("surplus native slot %d Git link has an unsafe target", spec.ID.Index)
	}
	selectedMetadata := strings.TrimPrefix(line, "gitdir: ")
	metadata := nativeSlotManifestShrinkPathPair{
		host:     common.matchingMetadata,
		consumer: common.matchingConsumerMetadata,
	}
	if !metadata.matchesRaw(selectedMetadata, filepath.Dir(gitFile)) {
		return fmt.Errorf("surplus native slot %d Git link does not select its exact registered metadata", spec.ID.Index)
	}
	reverseData, err := readNativeSlotManifestShrinkRegularFile(
		fmt.Sprintf("surplus native slot %d reverse Git link", spec.ID.Index),
		filepath.Join(metadata.host, "gitdir"),
		nativeSlotManifestShrinkPointerFileLimit,
	)
	if err != nil {
		return fmt.Errorf("read surplus native slot %d reverse Git link: %w", spec.ID.Index, err)
	}
	reverseValue, err := parseNativeSlotManifestShrinkPointerLine(
		fmt.Sprintf("surplus native slot %d reverse Git link", spec.ID.Index),
		reverseData,
	)
	if err != nil {
		return fmt.Errorf("surplus native slot %d reverse Git link is unsafe", spec.ID.Index)
	}
	worktreeGitFile := nativeSlotManifestShrinkPathPair{
		host:     gitFile,
		consumer: common.consumerGitFile,
	}
	if !worktreeGitFile.matchesRaw(reverseValue, metadata.host) {
		return fmt.Errorf("surplus native slot %d reverse Git link does not select its exact worktree", spec.ID.Index)
	}
	commondirData, err := readNativeSlotManifestShrinkRegularFile(
		fmt.Sprintf("surplus native slot %d commondir", spec.ID.Index),
		filepath.Join(metadata.host, "commondir"),
		nativeSlotManifestShrinkPointerFileLimit,
	)
	if err != nil {
		return fmt.Errorf("read surplus native slot %d commondir: %w", spec.ID.Index, err)
	}
	commondirValue, err := parseNativeSlotManifestShrinkPointerLine(
		fmt.Sprintf("surplus native slot %d commondir", spec.ID.Index),
		commondirData,
	)
	if err != nil {
		return fmt.Errorf("surplus native slot %d commondir is unsafe", spec.ID.Index)
	}
	commonPath := nativeSlotManifestShrinkPathPair{
		host:     common.path,
		consumer: common.consumerPath,
	}
	if !commonPath.matchesRaw(commondirValue, metadata.host) {
		return fmt.Errorf("surplus native slot %d commondir does not select its exact package-owned common Git repository", spec.ID.Index)
	}
	return nil
}

func nativeSlotManifestShrinkGeneratedSetupLayerFiles(values []string) (map[string]bool, error) {
	allowed := make(map[string]bool, len(values))
	for _, value := range values {
		candidate := strings.TrimSpace(value)
		if candidate == "" || candidate != value || strings.ContainsAny(candidate, "\\\r\n\x00") ||
			strings.HasPrefix(candidate, "/") || path.Clean(candidate) != candidate || candidate == "." ||
			candidate == ".." || strings.HasPrefix(candidate, "../") {
			return nil, fmt.Errorf("generated setup-layer file must be an exact safe repo-relative path: %q", value)
		}
		if candidate == ".git" || strings.HasPrefix(candidate, ".git/") {
			return nil, fmt.Errorf("generated setup-layer file cannot name Git metadata: %q", value)
		}
		if allowed[candidate] {
			return nil, fmt.Errorf("generated setup-layer file is declared more than once: %q", value)
		}
		allowed[candidate] = true
	}
	return allowed, nil
}

func nativeSlotManifestShrinkDisposableGeneratedResidueRoots(values []string) ([]string, error) {
	allowed := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		candidate := strings.TrimSpace(value)
		if candidate == "" || candidate != value || strings.ContainsAny(candidate, "\\\r\n\x00") ||
			strings.HasPrefix(candidate, "/") || path.Clean(candidate) != candidate || candidate == "." ||
			candidate == ".." || strings.HasPrefix(candidate, "../") {
			return nil, fmt.Errorf("disposable generated-residue root must be an exact safe repo-relative path: %q", value)
		}
		if candidate == ".git" || strings.HasPrefix(candidate, ".git/") {
			return nil, fmt.Errorf("disposable generated-residue root cannot name Git metadata: %q", value)
		}
		if seen[candidate] {
			return nil, fmt.Errorf("disposable generated-residue root is declared more than once: %q", value)
		}
		for _, existing := range allowed {
			if strings.HasPrefix(candidate, existing+"/") || strings.HasPrefix(existing, candidate+"/") {
				return nil, fmt.Errorf("disposable generated-residue roots overlap: %q and %q", existing, candidate)
			}
		}
		seen[candidate] = true
		allowed = append(allowed, candidate)
	}
	sort.Strings(allowed)
	return allowed, nil
}

func nativeSlotManifestShrinkDeclaredRootForPath(relativePath string, roots []string) (string, bool) {
	relativePath = path.Clean(strings.TrimSuffix(relativePath, "/"))
	for _, root := range roots {
		if relativePath == root || strings.HasPrefix(relativePath, root+"/") {
			return root, true
		}
	}
	return "", false
}

type nativeSlotManifestShrinkRemoteProofConfig struct {
	ScratchParent                   string
	ProtectedRoots                  []string
	GitSSHCommand                   string
	RequirePackageSourceExecutables bool
	StartTransport                  func() (func() error, error)
}

type nativeSlotManifestShrinkRemoteProof struct {
	proofDir  string
	remoteOID string
	remoteRef string
}

type nativeSlotManifestShrinkRemoteProofOperation struct {
	config           nativeSlotManifestShrinkRemoteProofConfig
	scratchDir       string
	scratchInfo      os.FileInfo
	transportCleanup func() error
	transportStarted bool
	proofs           map[string]nativeSlotManifestShrinkRemoteProof
	nextWorktreeView int
}

type nativeSlotManifestShrinkHistoricalWorktreeView struct {
	gitDir   string
	worktree string
}

func newNativeSlotManifestShrinkRemoteProofOperation(
	configs []nativeSlotManifestShrinkRemoteProofConfig,
) (*nativeSlotManifestShrinkRemoteProofOperation, error) {
	if len(configs) > 1 {
		return nil, fmt.Errorf("native manifest shrink accepts exactly one remote-proof configuration")
	}
	if len(configs) == 0 {
		return &nativeSlotManifestShrinkRemoteProofOperation{
			proofs: map[string]nativeSlotManifestShrinkRemoteProof{},
		}, nil
	}
	config := configs[0]
	config.ProtectedRoots = append([]string(nil), config.ProtectedRoots...)
	return &nativeSlotManifestShrinkRemoteProofOperation{
		config: config,
		proofs: map[string]nativeSlotManifestShrinkRemoteProof{},
	}, nil
}

func nativeSlotManifestShrinkPathsOverlap(first, second string) bool {
	return pathWithin(first, second) || pathWithin(second, first)
}

func canonicalNativeSlotManifestShrinkBoundary(pathValue string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(pathValue))
	if clean == "." || !filepath.IsAbs(clean) {
		return "", fmt.Errorf("current-remote proof received a non-absolute protected/shared boundary: %s", clean)
	}
	candidate := clean
	var missing []string
	for {
		canonical, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				canonical = filepath.Join(canonical, missing[index])
			}
			return filepath.Clean(canonical), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("canonicalize current-remote proof protected/shared boundary %s: %w", clean, err)
		}
		if _, lstatErr := os.Lstat(candidate); lstatErr == nil {
			return "", fmt.Errorf("canonicalize current-remote proof protected/shared boundary %s through a dangling link: %w", clean, err)
		} else if !errors.Is(lstatErr, os.ErrNotExist) {
			return "", fmt.Errorf("inspect current-remote proof protected/shared boundary %s: %w", clean, lstatErr)
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", fmt.Errorf("canonicalize current-remote proof protected/shared boundary %s: no existing ancestor", clean)
		}
		missing = append(missing, filepath.Base(candidate))
		candidate = parent
	}
}

func removeNativeSlotManifestShrinkOwnedScratch(path string, owned os.FileInfo) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("inspect current-remote proof cleanup path %s: %w", path, err)
	case owned == nil || !os.SameFile(owned, info):
		return fmt.Errorf("current-remote proof cleanup path identity changed at %s", path)
	default:
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove current-remote proof scratch %s: %w", path, err)
		}
		return nil
	}
}

func (operation *nativeSlotManifestShrinkRemoteProofOperation) ensureScratch(
	manifest nativeagent.Manifest,
) error {
	if operation.scratchDir != "" {
		return nil
	}
	parent := filepath.Clean(strings.TrimSpace(operation.config.ScratchParent))
	if parent == "." || !filepath.IsAbs(parent) {
		return fmt.Errorf("historical-root current-remote proof requires an absolute source-derived scratch parent")
	}
	if err := requireNativeSlotManifestShrinkCanonicalRealDirectory("current-remote proof scratch parent", parent); err != nil {
		return err
	}
	rootCheckout, historicalCommon, err := nativeSlotManifestShrinkHistoricalRootCommonDir(manifest)
	if err != nil {
		return err
	}
	boundaries := append([]string(nil), operation.config.ProtectedRoots...)
	boundaries = append(boundaries, manifest.HostWorktreeRoot, manifest.HostStateRoot, rootCheckout, historicalCommon)
	for _, spec := range manifest.Agents {
		boundaries = append(boundaries, spec.HostWorktree, spec.HostHome, spec.StateRoot)
	}
	canonicalBoundaries := make([]string, 0, len(boundaries))
	for _, boundary := range boundaries {
		boundary, err = canonicalNativeSlotManifestShrinkBoundary(boundary)
		if err != nil {
			return err
		}
		canonicalBoundaries = append(canonicalBoundaries, boundary)
		if pathWithin(parent, boundary) {
			return fmt.Errorf("current-remote proof scratch parent %s is inside protected/shared boundary %s", parent, boundary)
		}
	}
	scratchDir, err := os.MkdirTemp(parent, ".devkit-native-shrink-proof-")
	if err != nil {
		return fmt.Errorf("create operation-bound current-remote proof scratch: %w", err)
	}
	scratchInfo, err := os.Lstat(scratchDir)
	if err != nil {
		// Without a captured filesystem identity, recursive cleanup would turn a
		// rare inspection failure into ambient-path deletion authority. Fail and
		// retain the just-created path for bounded operator inspection instead.
		return fmt.Errorf(
			"inspect operation-bound current-remote proof scratch %s; identity is unavailable, so cleanup is intentionally refused: %w",
			scratchDir,
			err,
		)
	}
	cleanupOnError := func(cause error) error {
		return errors.Join(cause, removeNativeSlotManifestShrinkOwnedScratch(scratchDir, scratchInfo))
	}
	if err := requireNativeSlotManifestShrinkCanonicalRealDirectory("operation-bound current-remote proof scratch", scratchDir); err != nil {
		return cleanupOnError(err)
	}
	for index, boundaryValue := range boundaries {
		boundary, boundaryErr := canonicalNativeSlotManifestShrinkBoundary(boundaryValue)
		if boundaryErr != nil {
			return cleanupOnError(boundaryErr)
		}
		if boundary != canonicalBoundaries[index] {
			return cleanupOnError(fmt.Errorf("current-remote proof protected/shared boundary identity changed during scratch creation: %s", boundaryValue))
		}
		if nativeSlotManifestShrinkPathsOverlap(scratchDir, boundary) {
			return cleanupOnError(fmt.Errorf("current-remote proof scratch %s overlaps protected/shared boundary %s", scratchDir, boundary))
		}
	}
	operation.scratchDir = scratchDir
	operation.scratchInfo = scratchInfo
	return nil
}

func (operation *nativeSlotManifestShrinkRemoteProofOperation) capture(args ...string) (string, execx.Result, error) {
	name, invocationArgs, err := wtx.NativeSourceGitInvocation(
		operation.config.RequirePackageSourceExecutables,
		operation.config.GitSSHCommand,
		args...,
	)
	if err != nil {
		return "", execx.Result{Code: 1, Err: err}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, result := execx.CaptureManaged(
		ctx,
		execx.ManagedPolicy{TerminationGrace: 250 * time.Millisecond},
		nativeSlotManifestShrinkGitOutputLimit,
		name,
		invocationArgs...,
	)
	if captureErr := nativeSlotManifestShrinkManagedCaptureError(result); captureErr != nil {
		return output, result, captureErr
	}
	return output, result, nil
}

func nativeSlotManifestShrinkManagedCaptureError(result execx.Result) error {
	if result.Err == nil {
		return nil
	}
	if errors.Is(result.Err, execx.ErrOutputLimit) ||
		errors.Is(result.Err, context.DeadlineExceeded) ||
		errors.Is(result.Err, context.Canceled) ||
		execx.IsManagedCleanupError(result.Err) {
		return result.Err
	}
	// A direct ExitError is the command's ordinary predicate result. Any
	// wrapper or other error is executor/infrastructure failure and must not be
	// mistaken for Git's meaningful exit 1 (for example, an unset config key).
	if _, ok := result.Err.(*exec.ExitError); !ok {
		return result.Err
	}
	return nil
}

func nativeSlotManifestShrinkSharedIndexName(name string) bool {
	const prefix = "sharedindex."
	digest := strings.TrimPrefix(name, prefix)
	if digest == name || (len(digest) != 40 && len(digest) != 64) {
		return false
	}
	for _, character := range digest {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func (operation *nativeSlotManifestShrinkRemoteProofOperation) newHistoricalWorktreeView(
	commonDir string,
	metadataDir string,
	worktree string,
) (nativeSlotManifestShrinkHistoricalWorktreeView, error) {
	// Git validates the consumer-valued reverse gitdir even when GIT_COMMON_DIR
	// and --work-tree point at the host. Build one fresh, bounded metadata view
	// per custody pass so Git reads that pass's HEAD/index/config while only the
	// two pointers are normalized inside operation-owned scratch.
	if operation.scratchDir == "" || operation.scratchInfo == nil {
		return nativeSlotManifestShrinkHistoricalWorktreeView{}, fmt.Errorf("historical-root worktree custody requires the owned operation scratch")
	}
	currentScratch, err := os.Lstat(operation.scratchDir)
	if err != nil || !currentScratch.IsDir() || currentScratch.Mode()&os.ModeSymlink != 0 ||
		!os.SameFile(operation.scratchInfo, currentScratch) {
		return nativeSlotManifestShrinkHistoricalWorktreeView{}, fmt.Errorf("historical-root worktree custody scratch identity changed at %s", operation.scratchDir)
	}
	entries, err := readNativeSlotManifestShrinkDirectoryEntries(
		"historical-root worktree metadata view source",
		metadataDir,
		nativeSlotManifestShrinkMetadataViewEntryLimit,
	)
	if err != nil {
		return nativeSlotManifestShrinkHistoricalWorktreeView{}, err
	}
	present := map[string]bool{}
	var indexCompanions []string
	for _, entry := range entries {
		name := entry.Name()
		candidate := filepath.Join(metadataDir, name)
		info, statErr := os.Lstat(candidate)
		if statErr != nil {
			return nativeSlotManifestShrinkHistoricalWorktreeView{}, fmt.Errorf("inspect historical-root worktree metadata entry %s: %w", candidate, statErr)
		}
		switch {
		case name == "HEAD" || name == "index" || name == "commondir" || name == "gitdir" || name == "config.worktree":
			if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return nativeSlotManifestShrinkHistoricalWorktreeView{}, fmt.Errorf("historical-root worktree metadata entry must be a regular non-symlink file: %s", candidate)
			}
			present[name] = true
		case name == "ORIG_HEAD":
			if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return nativeSlotManifestShrinkHistoricalWorktreeView{}, fmt.Errorf("historical-root worktree metadata entry must be a regular non-symlink file: %s", candidate)
			}
		case name == "logs":
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nativeSlotManifestShrinkHistoricalWorktreeView{}, fmt.Errorf("historical-root worktree metadata logs must be a real directory: %s", candidate)
			}
		case name == "refs":
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nativeSlotManifestShrinkHistoricalWorktreeView{}, fmt.Errorf("historical-root worktree metadata refs must be a real directory: %s", candidate)
			}
			if _, refsErr := readNativeSlotManifestShrinkDirectoryEntries(
				"historical-root worktree-specific refs",
				candidate,
				0,
			); refsErr != nil {
				return nativeSlotManifestShrinkHistoricalWorktreeView{}, refsErr
			}
		case nativeSlotManifestShrinkSharedIndexName(name):
			if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return nativeSlotManifestShrinkHistoricalWorktreeView{}, fmt.Errorf("historical-root shared index must be a regular non-symlink file: %s", candidate)
			}
			indexCompanions = append(indexCompanions, name)
			if len(indexCompanions) > nativeSlotManifestShrinkMetadataViewCompanionLimit {
				return nativeSlotManifestShrinkHistoricalWorktreeView{}, fmt.Errorf("historical-root worktree metadata has more than one supported split-index companion")
			}
		default:
			return nativeSlotManifestShrinkHistoricalWorktreeView{}, fmt.Errorf("historical-root worktree metadata has unsupported custody state: %s", candidate)
		}
	}
	for _, required := range []string{"HEAD", "index", "commondir", "gitdir"} {
		if !present[required] {
			return nativeSlotManifestShrinkHistoricalWorktreeView{}, fmt.Errorf("historical-root worktree metadata is missing required %s", required)
		}
	}

	operation.nextWorktreeView++
	viewDir := filepath.Join(operation.scratchDir, fmt.Sprintf("worktree-%d.git", operation.nextWorktreeView))
	if err := os.Mkdir(viewDir, 0o700); err != nil {
		return nativeSlotManifestShrinkHistoricalWorktreeView{}, fmt.Errorf("create historical-root worktree metadata view %s: %w", viewDir, err)
	}
	writeViewFile := func(name string, data []byte) error {
		viewPath := filepath.Join(viewDir, name)
		file, openErr := os.OpenFile(viewPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if openErr != nil {
			return fmt.Errorf("create historical-root worktree metadata view file %s: %w", viewPath, openErr)
		}
		if _, writeErr := file.Write(data); writeErr != nil {
			_ = file.Close()
			return fmt.Errorf("write historical-root worktree metadata view file %s: %w", viewPath, writeErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			return fmt.Errorf("close historical-root worktree metadata view file %s: %w", viewPath, closeErr)
		}
		return nil
	}
	copyFiles := append([]string{"HEAD", "index"}, indexCompanions...)
	if present["config.worktree"] {
		copyFiles = append(copyFiles, "config.worktree")
	}
	for _, name := range copyFiles {
		limit := int64(nativeSlotManifestShrinkMetadataViewIndexFileLimit)
		if name == "HEAD" {
			limit = nativeSlotManifestShrinkPointerFileLimit
		} else if name == "config.worktree" {
			limit = nativeSlotManifestShrinkConfigFileLimit
		}
		data, readErr := readNativeSlotManifestShrinkRegularFile(
			"historical-root worktree metadata "+name,
			filepath.Join(metadataDir, name),
			limit,
		)
		if readErr != nil {
			return nativeSlotManifestShrinkHistoricalWorktreeView{}, readErr
		}
		if err := writeViewFile(name, data); err != nil {
			return nativeSlotManifestShrinkHistoricalWorktreeView{}, err
		}
	}
	if err := writeViewFile("commondir", []byte(commonDir+"\n")); err != nil {
		return nativeSlotManifestShrinkHistoricalWorktreeView{}, err
	}
	if err := writeViewFile("gitdir", []byte(filepath.Join(worktree, ".git")+"\n")); err != nil {
		return nativeSlotManifestShrinkHistoricalWorktreeView{}, err
	}
	return nativeSlotManifestShrinkHistoricalWorktreeView{gitDir: viewDir, worktree: worktree}, nil
}

func (operation *nativeSlotManifestShrinkRemoteProofOperation) captureHistoricalWorktree(
	view nativeSlotManifestShrinkHistoricalWorktreeView,
	args ...string,
) (string, execx.Result, error) {
	for label, candidate := range map[string]string{
		"Git metadata view": view.gitDir,
		"worktree":          view.worktree,
	} {
		if candidate == "" || !filepath.IsAbs(candidate) || filepath.Clean(candidate) != candidate {
			return "", execx.Result{Code: 1}, fmt.Errorf("historical-root %s must be absolute and canonical: %s", label, candidate)
		}
	}
	if operation.scratchDir == "" || !pathWithin(view.gitDir, operation.scratchDir) {
		return "", execx.Result{Code: 1}, fmt.Errorf("historical-root Git metadata view is outside the owned operation scratch")
	}
	gitArgs := []string{
		"--no-optional-locks",
		"--git-dir", view.gitDir,
		"--work-tree", view.worktree,
	}
	gitArgs = append(gitArgs, args...)
	name, invocationArgs, err := wtx.NativeSourceGitInvocation(
		operation.config.RequirePackageSourceExecutables,
		operation.config.GitSSHCommand,
		gitArgs...,
	)
	if err != nil {
		return "", execx.Result{Code: 1, Err: err}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, result := execx.CaptureManaged(
		ctx,
		execx.ManagedPolicy{TerminationGrace: 250 * time.Millisecond},
		nativeSlotManifestShrinkGitOutputLimit,
		name,
		invocationArgs...,
	)
	if captureErr := nativeSlotManifestShrinkManagedCaptureError(result); captureErr != nil {
		return output, result, captureErr
	}
	return output, result, nil
}

func (operation *nativeSlotManifestShrinkRemoteProofOperation) startTransport() error {
	if operation.transportStarted {
		return nil
	}
	if operation.config.RequirePackageSourceExecutables && strings.TrimSpace(operation.config.GitSSHCommand) == "" {
		return fmt.Errorf("historical-root current-remote proof requires the package-owned SSH command")
	}
	if operation.config.StartTransport == nil {
		operation.transportCleanup = func() error { return nil }
		operation.transportStarted = true
		return nil
	}
	cleanup, err := operation.config.StartTransport()
	if err != nil {
		return fmt.Errorf("start managed egress for historical-root current-remote proof: %w", err)
	}
	if cleanup == nil {
		return fmt.Errorf("managed egress current-remote proof returned no cleanup")
	}
	operation.transportCleanup = cleanup
	operation.transportStarted = true
	return nil
}

func (operation *nativeSlotManifestShrinkRemoteProofOperation) ensureRemoteProof(
	manifest nativeagent.Manifest,
	commonDir string,
	baseBranch string,
	expectedOrigin string,
) (nativeSlotManifestShrinkRemoteProof, error) {
	if strings.TrimSpace(operation.config.ScratchParent) == "" {
		return nativeSlotManifestShrinkRemoteProof{}, fmt.Errorf("historical-root retirement requires one operation-bound current-remote proof")
	}
	key := strings.Join([]string{commonDir, baseBranch, strings.TrimSpace(expectedOrigin)}, "\x00")
	if proof, ok := operation.proofs[key]; ok {
		return proof, nil
	}
	if err := operation.ensureScratch(manifest); err != nil {
		return nativeSlotManifestShrinkRemoteProof{}, err
	}
	objectFormat, result, err := operation.capture("--git-dir", commonDir, "rev-parse", "--show-object-format")
	objectFormat = strings.TrimSpace(objectFormat)
	if err != nil || result.Code != 0 || (objectFormat != "sha1" && objectFormat != "sha256") {
		return nativeSlotManifestShrinkRemoteProof{}, fmt.Errorf("read historical root Git object format")
	}
	objectsDir := filepath.Join(commonDir, "objects")
	if err := requireNativeSlotManifestShrinkCanonicalRealDirectory("historical root read-only object alternate", objectsDir); err != nil {
		return nativeSlotManifestShrinkRemoteProof{}, err
	}
	proofDir := filepath.Join(operation.scratchDir, fmt.Sprintf("repo-%d.git", len(operation.proofs)+1))
	_, result, err = operation.capture("init", "--bare", "--object-format="+objectFormat, proofDir)
	if err != nil || result.Code != 0 {
		return nativeSlotManifestShrinkRemoteProof{}, fmt.Errorf("initialize isolated current-remote proof repository: git exit %d", result.Code)
	}
	alternatesPath := filepath.Join(proofDir, "objects", "info", "alternates")
	if err := os.WriteFile(alternatesPath, []byte(objectsDir+"\n"), 0o600); err != nil {
		return nativeSlotManifestShrinkRemoteProof{}, fmt.Errorf("install read-only historical object alternate in current-remote proof: %w", err)
	}
	if err := operation.startTransport(); err != nil {
		return nativeSlotManifestShrinkRemoteProof{}, err
	}
	remoteRef := "refs/heads/" + baseBranch
	proofRef := "refs/remotes/devkit-proof/" + baseBranch
	if err := wtx.RunNativeSourceGitFetch(
		operation.config.RequirePackageSourceExecutables,
		operation.config.GitSSHCommand,
		"--git-dir", proofDir,
		"fetch", "--force", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--progress",
		"--", strings.TrimSpace(expectedOrigin), "+"+remoteRef+":"+proofRef,
	); err != nil {
		return nativeSlotManifestShrinkRemoteProof{}, fmt.Errorf("fetch exact current remote base into isolated operation proof: %w", err)
	}
	remoteOID, result, err := operation.capture("--git-dir", proofDir, "rev-parse", "--verify", proofRef+"^{commit}")
	if err != nil || result.Code != 0 {
		return nativeSlotManifestShrinkRemoteProof{}, fmt.Errorf("resolve fetched current remote base")
	}
	proof := nativeSlotManifestShrinkRemoteProof{
		proofDir:  proofDir,
		remoteOID: strings.TrimSpace(remoteOID),
		remoteRef: remoteRef,
	}
	operation.proofs[key] = proof
	return proof, nil
}

func (operation *nativeSlotManifestShrinkRemoteProofOperation) Close() error {
	var result error
	if operation.scratchDir != "" {
		result = errors.Join(result, removeNativeSlotManifestShrinkOwnedScratch(operation.scratchDir, operation.scratchInfo))
	}
	if operation.transportCleanup != nil {
		result = errors.Join(result, operation.transportCleanup())
	}
	return result
}

func requireNativeSlotManifestShrinkCurrentRemoteContainment(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
	commonDir string,
	branchRef string,
	baseBranch string,
	expectedOrigin string,
	operation *nativeSlotManifestShrinkRemoteProofOperation,
) (nativeSlotManifestShrinkRemoteProof, error) {
	proof, err := operation.ensureRemoteProof(manifest, commonDir, baseBranch, expectedOrigin)
	if err != nil {
		return nativeSlotManifestShrinkRemoteProof{}, fmt.Errorf("prepare current-remote proof for surplus native slot %d: %w", spec.ID.Index, err)
	}
	branchOID, result, captureErr := operation.capture("--git-dir", commonDir, "rev-parse", "--verify", branchRef+"^{commit}")
	if captureErr != nil || result.Code != 0 {
		return nativeSlotManifestShrinkRemoteProof{}, fmt.Errorf("resolve historical surplus native slot %d branch commit", spec.ID.Index)
	}
	branchOID = strings.TrimSpace(branchOID)
	ahead, result, captureErr := operation.capture(
		"--git-dir", proof.proofDir,
		"rev-list", "--count", branchOID, "--not", proof.remoteOID,
	)
	if captureErr != nil || result.Code != 0 {
		return nativeSlotManifestShrinkRemoteProof{}, fmt.Errorf(
			"surplus native slot %d branch %s is not present in the exact fetched current %s history",
			spec.ID.Index,
			branchRef,
			proof.remoteRef,
		)
	}
	if strings.TrimSpace(ahead) != "0" {
		return nativeSlotManifestShrinkRemoteProof{}, fmt.Errorf(
			"surplus native slot %d branch %s has %s commit(s) not contained in current %s at %s",
			spec.ID.Index,
			branchRef,
			strings.TrimSpace(ahead),
			proof.remoteRef,
			proof.remoteOID,
		)
	}
	return proof, nil
}

func requireNativeSlotManifestShrinkHistoricalWorktreeCustody(
	spec nativeagent.Spec,
	view nativeSlotManifestShrinkHistoricalWorktreeView,
	proof nativeSlotManifestShrinkRemoteProof,
	generatedSetupLayerFiles []string,
	disposableGeneratedResidueRoots []string,
	operation *nativeSlotManifestShrinkRemoteProofOperation,
) error {
	allowed, err := nativeSlotManifestShrinkGeneratedSetupLayerFiles(generatedSetupLayerFiles)
	if err != nil {
		return err
	}
	residueRoots, err := nativeSlotManifestShrinkDisposableGeneratedResidueRoots(disposableGeneratedResidueRoots)
	if err != nil {
		return err
	}
	if operation == nil {
		return fmt.Errorf("historical-root retirement requires one operation-bound current-remote proof")
	}
	worktree := filepath.Clean(spec.HostWorktree)
	index, result, captureErr := operation.captureHistoricalWorktree(
		view,
		"ls-files", "--stage", "-z",
	)
	if captureErr != nil || result.Code != 0 {
		return fmt.Errorf("inspect surplus native slot %d index custody: git exit %d", spec.ID.Index, result.Code)
	}
	for _, entry := range strings.Split(index, "\x00") {
		if entry == "" {
			continue
		}
		metadata, _, found := strings.Cut(entry, "\t")
		if !found {
			return fmt.Errorf("inspect surplus native slot %d index custody: malformed ls-files entry", spec.ID.Index)
		}
		fields := strings.Fields(metadata)
		if len(fields) != 3 {
			return fmt.Errorf("inspect surplus native slot %d index custody: malformed staged metadata", spec.ID.Index)
		}
		if fields[2] != "0" {
			return fmt.Errorf("surplus native slot %d has an unmerged non-stage-0 index entry", spec.ID.Index)
		}
		if fields[0] == "160000" {
			return fmt.Errorf("surplus native slot %d has submodule custody", spec.ID.Index)
		}
	}
	flags, result, captureErr := operation.captureHistoricalWorktree(
		view,
		"ls-files", "-v", "-z",
	)
	if captureErr != nil || result.Code != 0 {
		return fmt.Errorf("inspect surplus native slot %d index flags: git exit %d", spec.ID.Index, result.Code)
	}
	for _, entry := range strings.Split(flags, "\x00") {
		if entry == "" {
			continue
		}
		if len(entry) < 3 || entry[:2] != "H " {
			return fmt.Errorf("surplus native slot %d has assume-unchanged, skip-worktree, sparse, or unexpected index flags at %s", spec.ID.Index, entry)
		}
	}
	for _, key := range []string{"core.sparseCheckout", "core.sparseCheckoutCone"} {
		configured, configResult, configErr := operation.captureHistoricalWorktree(
			view,
			"config", "--bool", "--get", key,
		)
		if configErr != nil || (configResult.Code != 0 && configResult.Code != 1) {
			return fmt.Errorf("inspect surplus native slot %d sparse-checkout config %s: git exit %d", spec.ID.Index, key, configResult.Code)
		}
		if configResult.Code == 0 && strings.TrimSpace(configured) != "false" {
			return fmt.Errorf("surplus native slot %d has sparse-checkout enabled by %s", spec.ID.Index, key)
		}
	}
	status, result, captureErr := operation.captureHistoricalWorktree(
		view,
		"status", "--porcelain=v1", "-z", "--no-renames",
		"--untracked-files=all", "--ignored=matching", "--ignore-submodules=none",
	)
	if captureErr != nil || result.Code != 0 {
		return fmt.Errorf("inspect surplus native slot %d Git status: git exit %d", spec.ID.Index, result.Code)
	}
	for _, entry := range strings.Split(status, "\x00") {
		if entry == "" {
			continue
		}
		if len(entry) < 4 || entry[2] != ' ' {
			return fmt.Errorf("inspect surplus native slot %d Git status: malformed porcelain entry", spec.ID.Index)
		}
		state := entry[:2]
		relativePath := entry[3:]
		canonicalRelativePath := path.Clean(strings.TrimSuffix(relativePath, "/"))
		if canonicalRelativePath == "." || strings.HasPrefix(canonicalRelativePath, "../") || path.IsAbs(canonicalRelativePath) ||
			strings.ContainsAny(canonicalRelativePath, "\\\r\n\x00") {
			return fmt.Errorf("surplus native slot %d Git status contains an unsafe path", spec.ID.Index)
		}
		if state == "??" {
			return fmt.Errorf("surplus native slot %d has non-ignored untracked custody at %s", spec.ID.Index, relativePath)
		}
		if state == "!!" {
			declaredRoot, matched := nativeSlotManifestShrinkDeclaredRootForPath(canonicalRelativePath, residueRoots)
			if !matched {
				return fmt.Errorf(
					"surplus native slot %d has ignored custody outside source-declared disposable generated-residue roots at %s",
					spec.ID.Index,
					relativePath,
				)
			}
			rootPath := filepath.Join(worktree, filepath.FromSlash(declaredRoot))
			rootInfo, rootErr := os.Lstat(rootPath)
			if rootErr != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf(
					"surplus native slot %d disposable generated-residue root must be a real non-symlink directory: %s",
					spec.ID.Index,
					declaredRoot,
				)
			}
			canonicalRoot, rootEvalErr := filepath.EvalSymlinks(rootPath)
			if rootEvalErr != nil || canonicalRoot != rootPath {
				return fmt.Errorf(
					"surplus native slot %d disposable generated-residue root path is not canonical: %s",
					spec.ID.Index,
					declaredRoot,
				)
			}
			continue
		}
		if !allowed[canonicalRelativePath] {
			return fmt.Errorf("surplus native slot %d worktree has unexpected tracked dirt at %s", spec.ID.Index, relativePath)
		}
		baseEntry, treeResult, treeErr := operation.capture(
			"--git-dir", proof.proofDir,
			"ls-tree", "-z", "--name-only", proof.remoteOID, "--", canonicalRelativePath,
		)
		if treeErr != nil || treeResult.Code != 0 || baseEntry != canonicalRelativePath+"\x00" {
			return fmt.Errorf(
				"surplus native slot %d generated setup-layer dirt is not tracked by current %s at %s: %s",
				spec.ID.Index,
				proof.remoteRef,
				proof.remoteOID,
				relativePath,
			)
		}
		candidate := filepath.Join(worktree, filepath.FromSlash(canonicalRelativePath))
		info, statErr := os.Lstat(candidate)
		if statErr == nil {
			if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("surplus native slot %d generated setup-layer file must be regular and non-symlink: %s", spec.ID.Index, relativePath)
			}
			canonical, evalErr := filepath.EvalSymlinks(candidate)
			if evalErr != nil || canonical != candidate {
				return fmt.Errorf("surplus native slot %d generated setup-layer file path is not canonical: %s", spec.ID.Index, relativePath)
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("inspect surplus native slot %d generated setup-layer file %s: %w", spec.ID.Index, relativePath, statErr)
		}
	}
	return nil
}

func requireNativeSlotManifestShrinkHistoricalConfigSafe(
	spec nativeagent.Spec,
	commonDir string,
	metadataDir string,
	operation *nativeSlotManifestShrinkRemoteProofOperation,
) error {
	if operation == nil {
		return fmt.Errorf("historical-root retirement requires one operation-bound current-remote proof")
	}
	configs := []struct {
		path     string
		optional bool
	}{
		{path: filepath.Join(commonDir, "config")},
		{path: filepath.Join(metadataDir, "config.worktree"), optional: true},
	}
	for _, config := range configs {
		if config.optional {
			if _, err := os.Lstat(config.path); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return fmt.Errorf("inspect historical-root worktree config %s: %w", config.path, err)
			}
		}
		if _, err := readNativeSlotManifestShrinkRegularFile(
			"historical-root Git config",
			config.path,
			nativeSlotManifestShrinkConfigFileLimit,
		); err != nil {
			return err
		}
		keys, result, captureErr := operation.capture(
			"config", "--file", config.path, "--no-includes", "--null", "--name-only", "--list",
		)
		if captureErr != nil || result.Code != 0 {
			return fmt.Errorf("inspect historical-root Git config names for surplus native slot %d: git exit %d", spec.ID.Index, result.Code)
		}
		for _, key := range strings.Split(keys, "\x00") {
			key = strings.ToLower(key)
			if key == "" {
				continue
			}
			if strings.HasPrefix(key, "include.") || strings.HasPrefix(key, "includeif.") {
				return fmt.Errorf(
					"surplus native slot %d historical-root Git config has unsupported include semantics at %s",
					spec.ID.Index,
					key,
				)
			}
			if strings.HasPrefix(key, "filter.") &&
				(strings.HasSuffix(key, ".clean") || strings.HasSuffix(key, ".process")) {
				return fmt.Errorf(
					"surplus native slot %d historical-root Git config has executable clean/process filter custody at %s",
					spec.ID.Index,
					key,
				)
			}
		}
	}
	return nil
}

type nativeSlotManifestShrinkCustodyPolicy struct {
	generatedSetupLayerFiles        []string
	disposableGeneratedResidueRoots []string
	remoteProof                     *nativeSlotManifestShrinkRemoteProofOperation
}

func requireNativeSlotManifestShrinkGitQuiescentWithQuarantines(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
	expectedOrigin string,
	allowedQuarantines map[string]bool,
	policy nativeSlotManifestShrinkCustodyPolicy,
) error {
	worktree := filepath.Clean(strings.TrimSpace(spec.HostWorktree))
	worktreeInfo, worktreeErr := os.Lstat(worktree)
	worktreeExists := worktreeErr == nil
	if worktreeErr != nil && !errors.Is(worktreeErr, os.ErrNotExist) {
		return fmt.Errorf("inspect surplus native slot worktree %s: %w", worktree, worktreeErr)
	}
	if worktreeExists && (!worktreeInfo.IsDir() || worktreeInfo.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("surplus native slot worktree must be a real directory: %s", worktree)
	}
	common, err := resolveNativeSlotManifestShrinkCommon(
		manifest,
		spec,
		expectedOrigin,
		worktreeExists,
		allowedQuarantines,
		policy.remoteProof,
	)
	if err != nil {
		return err
	}
	if common.kind == nativeSlotManifestShrinkCommonAbsent {
		return nil
	}
	commonDir := common.path
	if common.kind == nativeSlotManifestShrinkCommonHistoricalRoot {
		if worktreeExists {
			if err := requireNativeSlotManifestShrinkCanonicalRealDirectory("historical surplus native slot worktree", worktree); err != nil {
				return err
			}
		}
		if common.matchingMetadata != "" {
			if err := requireNativeSlotManifestShrinkCanonicalRealDirectory("historical surplus native slot registered Git metadata", common.matchingMetadata); err != nil {
				return err
			}
		}
	}

	lockPaths := []string{
		filepath.Join(commonDir, "index.lock"),
		filepath.Join(commonDir, "packed-refs.lock"),
		filepath.Join(commonDir, "shallow.lock"),
	}
	if common.kind == nativeSlotManifestShrinkCommonHistoricalRoot {
		lockPaths = append(
			lockPaths,
			filepath.Join(commonDir, "config.lock"),
			filepath.Join(commonDir, "HEAD.lock"),
		)
	}
	for _, lockPath := range lockPaths {
		if _, err := os.Lstat(lockPath); err == nil {
			return fmt.Errorf("surplus native slot %d has Git transaction lock residue %s", spec.ID.Index, lockPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect surplus native slot Git transaction lock %s: %w", lockPath, err)
		}
	}

	expectedGitFile := filepath.Join(worktree, ".git")
	matchingMetadata := common.matchingMetadata
	if worktreeExists && matchingMetadata == "" {
		return fmt.Errorf("surplus native slot %d worktree is not registered in the package-owned common Git repository", spec.ID.Index)
	}
	if !worktreeExists && matchingMetadata != "" && common.kind != nativeSlotManifestShrinkCommonLegacy && common.kind != nativeSlotManifestShrinkCommonHistoricalRoot {
		return fmt.Errorf("surplus native slot %d has registered Git worktree metadata without its worktree: %s", spec.ID.Index, matchingMetadata)
	}
	if matchingMetadata != "" {
		metadataLocks := []string{filepath.Join(matchingMetadata, "index.lock")}
		if common.kind == nativeSlotManifestShrinkCommonHistoricalRoot {
			metadataLocks = append(
				metadataLocks,
				filepath.Join(matchingMetadata, "HEAD.lock"),
				filepath.Join(matchingMetadata, "commondir.lock"),
				filepath.Join(matchingMetadata, "gitdir.lock"),
				filepath.Join(matchingMetadata, "locked"),
			)
		}
		for _, metadataLock := range metadataLocks {
			if _, err := os.Lstat(metadataLock); err == nil {
				return fmt.Errorf("surplus native slot %d has Git transaction lock residue %s", spec.ID.Index, metadataLock)
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("inspect surplus native slot Git transaction lock %s: %w", metadataLock, err)
			}
		}
	}

	branchRef := common.branchRef
	if branchRef == "" {
		return fmt.Errorf("surplus native slot %d has no exact selected branch identity", spec.ID.Index)
	}
	branchLockRefs := []string{branchRef}
	if common.kind == nativeSlotManifestShrinkCommonHistoricalRoot {
		branchLockRefs = nativeSlotManifestShrinkHistoricalBranchRefs(manifest, spec)
	}
	for _, lockedBranchRef := range branchLockRefs {
		branchLock := filepath.Join(commonDir, filepath.FromSlash(lockedBranchRef)+".lock")
		if _, err := os.Lstat(branchLock); err == nil {
			return fmt.Errorf("surplus native slot %d has Git transaction lock residue %s", spec.ID.Index, branchLock)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect surplus native slot Git transaction lock %s: %w", branchLock, err)
		}
	}

	git := gitauthority.Executable()
	captureGit := func(args ...string) (string, execx.Result, error) {
		if common.kind == nativeSlotManifestShrinkCommonHistoricalRoot {
			if policy.remoteProof == nil {
				return "", execx.Result{Code: 1}, fmt.Errorf("historical-root retirement requires one operation-bound current-remote proof")
			}
			return policy.remoteProof.capture(args...)
		}
		output, result := execx.Capture(context.Background(), git, args...)
		return output, result, nil
	}
	_, showRef, showRefErr := captureGit("--git-dir", commonDir, "show-ref", "--verify", "--quiet", branchRef)
	if showRefErr != nil {
		return fmt.Errorf("inspect surplus native slot %d branch %s: %w", spec.ID.Index, branchRef, showRefErr)
	}
	if showRef.Code == 1 {
		if worktreeExists || matchingMetadata != "" {
			return fmt.Errorf("surplus native slot %d worktree has no exact local branch %s", spec.ID.Index, branchRef)
		}
		return nil
	}
	if showRef.Code != 0 {
		return fmt.Errorf("inspect surplus native slot %d branch %s: git exit %d", spec.ID.Index, branchRef, showRef.Code)
	}
	baseBranch := strings.TrimSpace(manifest.BaseBranch)
	if baseBranch == "" {
		return fmt.Errorf("surplus native slot %d cannot prove Git custody without a source-derived base branch", spec.ID.Index)
	}
	baseRef := "refs/remotes/origin/" + baseBranch
	var historicalProof nativeSlotManifestShrinkRemoteProof
	if common.kind == nativeSlotManifestShrinkCommonHistoricalRoot {
		var err error
		historicalProof, err = requireNativeSlotManifestShrinkCurrentRemoteContainment(
			manifest,
			spec,
			commonDir,
			branchRef,
			baseBranch,
			expectedOrigin,
			policy.remoteProof,
		)
		if err != nil {
			return err
		}
		for _, otherBranchRef := range nativeSlotManifestShrinkHistoricalBranchRefs(manifest, spec) {
			if otherBranchRef == branchRef {
				continue
			}
			_, otherShowRef, otherShowRefErr := captureGit(
				"--git-dir", commonDir,
				"show-ref", "--verify", "--quiet", otherBranchRef,
			)
			if otherShowRefErr != nil {
				return fmt.Errorf("inspect surplus native slot %d branch %s: %w", spec.ID.Index, otherBranchRef, otherShowRefErr)
			}
			switch otherShowRef.Code {
			case 1:
				continue
			case 0:
				if _, err := requireNativeSlotManifestShrinkCurrentRemoteContainment(
					manifest,
					spec,
					commonDir,
					otherBranchRef,
					baseBranch,
					expectedOrigin,
					policy.remoteProof,
				); err != nil {
					return err
				}
			default:
				return fmt.Errorf(
					"inspect surplus native slot %d branch %s: git exit %d",
					spec.ID.Index,
					otherBranchRef,
					otherShowRef.Code,
				)
			}
		}
	} else {
		ahead, result, captureErr := captureGit(
			"--git-dir", commonDir,
			"rev-list", "--count", branchRef, "--not", baseRef,
		)
		if captureErr != nil || result.Code != 0 {
			return fmt.Errorf(
				"prove surplus native slot %d branch has no unpublished or ahead commits relative to %s: git exit %d",
				spec.ID.Index,
				baseRef,
				result.Code,
			)
		}
		if strings.TrimSpace(ahead) != "0" {
			return fmt.Errorf(
				"surplus native slot %d branch %s has %s commit(s) not contained in %s",
				spec.ID.Index,
				branchRef,
				strings.TrimSpace(ahead),
				baseRef,
			)
		}
	}
	if worktreeExists {
		gitFile, err := os.Lstat(expectedGitFile)
		if err != nil {
			return fmt.Errorf("inspect surplus native slot %d Git link %s: %w", spec.ID.Index, expectedGitFile, err)
		}
		if !gitFile.Mode().IsRegular() || gitFile.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("surplus native slot %d Git link must be a regular non-symlink file", spec.ID.Index)
		}
		if err := validateNativeSlotManifestShrinkWorktreeLink(spec, common); err != nil {
			return err
		}
		if common.kind == nativeSlotManifestShrinkCommonHistoricalRoot {
			if err := requireNativeSlotManifestShrinkHistoricalConfigSafe(
				spec,
				commonDir,
				matchingMetadata,
				policy.remoteProof,
			); err != nil {
				return err
			}
		}
		var historicalView nativeSlotManifestShrinkHistoricalWorktreeView
		if common.kind == nativeSlotManifestShrinkCommonHistoricalRoot {
			historicalView, err = policy.remoteProof.newHistoricalWorktreeView(
				commonDir,
				matchingMetadata,
				worktree,
			)
			if err != nil {
				return err
			}
		}
		var head string
		var headResult execx.Result
		var headErr error
		if common.kind == nativeSlotManifestShrinkCommonHistoricalRoot {
			head, headResult, headErr = policy.remoteProof.captureHistoricalWorktree(
				historicalView,
				"symbolic-ref", "--quiet", "HEAD",
			)
		} else {
			head, headResult, headErr = captureGit("-C", worktree, "symbolic-ref", "--quiet", "HEAD")
		}
		if headErr != nil || headResult.Code != 0 || strings.TrimSpace(head) != branchRef {
			return fmt.Errorf(
				"surplus native slot %d must be checked out on exact branch %s (git exit %d: %v)",
				spec.ID.Index,
				branchRef,
				headResult.Code,
				headResult.Err,
			)
		}
		if common.kind == nativeSlotManifestShrinkCommonHistoricalRoot {
			if err := requireNativeSlotManifestShrinkHistoricalWorktreeCustody(
				spec,
				historicalView,
				historicalProof,
				policy.generatedSetupLayerFiles,
				policy.disposableGeneratedResidueRoots,
				policy.remoteProof,
			); err != nil {
				return err
			}
		} else {
			status, statusResult := execx.Capture(
				context.Background(),
				git,
				"-C", worktree,
				"status", "--porcelain=v1", "--untracked-files=all",
			)
			if statusResult.Code != 0 {
				return fmt.Errorf("inspect surplus native slot %d Git status: git exit %d", spec.ID.Index, statusResult.Code)
			}
			if strings.TrimSpace(status) != "" {
				return fmt.Errorf("surplus native slot %d worktree is dirty", spec.ID.Index)
			}
		}
	}
	return nil
}

func requireNativeSlotManifestShrinkGitQuiescent(manifest nativeagent.Manifest, spec nativeagent.Spec) error {
	return requireNativeSlotManifestShrinkGitQuiescentWithQuarantines(
		manifest,
		spec,
		"",
		nil,
		nativeSlotManifestShrinkCustodyPolicy{},
	)
}

func requireNativeSlotManifestShrinkAbsentWithPlannerAndCustody(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
	expectedOrigin string,
	policy nativeSlotManifestShrinkCustodyPolicy,
	planner nativeSlotManifestShrinkProcessPlanner,
) error {
	if planner == nil {
		return fmt.Errorf("surplus native slot process planner is required")
	}
	identity := nativeSlotProcessIdentity{
		index:           spec.ID.Index,
		hostWorktree:    spec.HostWorktree,
		sandboxWorktree: spec.SandboxWorktree,
		hostHome:        spec.HostHome,
		sandboxHome:     spec.SandboxHome,
		stateRoot:       spec.StateRoot,
		sandboxState:    spec.SandboxStateRoot,
	}
	inspectProcesses := func() error {
		plan, err := planner(identity, false)
		if err != nil {
			return fmt.Errorf("inspect surplus native slot %d processes: %w", spec.ID.Index, err)
		}
		if len(plan.pids) != 0 {
			return fmt.Errorf("surplus native slot %d still has active processes: %v", spec.ID.Index, plan.pids)
		}
		return nil
	}
	if err := inspectProcesses(); err != nil {
		return err
	}

	socketPath := filepath.Join(filepath.Clean(spec.HostHome), ".codex", fmt.Sprintf("a%d-app.sock", spec.ID.Index))
	if _, err := os.Lstat(socketPath); err == nil {
		return fmt.Errorf("surplus native slot %d still has app-server socket residue at %s", spec.ID.Index, socketPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect surplus native slot %d app-server socket %s: %w", spec.ID.Index, socketPath, err)
	}
	paths := []struct {
		name string
		path string
	}{
		{name: "worktree/Git custody", path: spec.HostWorktree},
		{name: "home", path: spec.HostHome},
		{name: "state", path: spec.StateRoot},
	}
	for _, candidate := range paths {
		path := filepath.Clean(strings.TrimSpace(candidate.path))
		if path == "" || !filepath.IsAbs(path) {
			return fmt.Errorf("surplus native slot %d %s path must be absolute", spec.ID.Index, candidate.name)
		}
		if info, err := os.Lstat(path); err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("surplus native slot %d %s must be a real directory: %s", spec.ID.Index, candidate.name, path)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect surplus native slot %d %s %s: %w", spec.ID.Index, candidate.name, path, err)
		}
	}
	stateParent := filepath.Dir(filepath.Clean(spec.StateRoot))
	stateEntries, err := os.ReadDir(stateParent)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("enumerate surplus native slot %d state parent %s: %w", spec.ID.Index, stateParent, err)
	}
	quarantinePrefix := fmt.Sprintf(".devkit-reset-%s-agent%d-", manifest.Project, spec.ID.Index)
	lockIdentity := fmt.Sprintf("agent%d", spec.ID.Index)
	for _, entry := range stateEntries {
		name := entry.Name()
		if strings.HasPrefix(name, quarantinePrefix) ||
			(strings.Contains(name, lockIdentity) && strings.HasSuffix(name, ".lock")) {
			return fmt.Errorf(
				"surplus native slot %d has state transaction or lock residue at %s",
				spec.ID.Index,
				filepath.Join(stateParent, name),
			)
		}
	}

	agentRoot := filepath.Dir(filepath.Clean(spec.HostWorktree))
	entries, err := os.ReadDir(agentRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("enumerate surplus native slot %d parent %s: %w", spec.ID.Index, agentRoot, err)
	}
	allowed := map[string]bool{filepath.Base(filepath.Clean(spec.HostWorktree)): true}
	hostHome := filepath.Clean(spec.HostHome)
	if filepath.Dir(hostHome) == agentRoot {
		allowed[filepath.Base(hostHome)] = true
	}
	var opaque []string
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			opaque = append(opaque, entry.Name())
		}
	}
	if len(opaque) != 0 {
		sort.Strings(opaque)
		return fmt.Errorf(
			"surplus native slot %d parent contains transaction, lock, or opaque residue: %s",
			spec.ID.Index,
			strings.Join(opaque, ","),
		)
	}
	if err := requireNativeSlotManifestShrinkGitQuiescentWithQuarantines(manifest, spec, expectedOrigin, nil, policy); err != nil {
		return err
	}
	return inspectProcesses()
}

func requireNativeSlotManifestShrinkAbsentWithPlanner(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
	expectedOrigin string,
	planner nativeSlotManifestShrinkProcessPlanner,
) error {
	return requireNativeSlotManifestShrinkAbsentWithPlannerAndCustody(
		manifest,
		spec,
		expectedOrigin,
		nativeSlotManifestShrinkCustodyPolicy{},
		planner,
	)
}

func requireNativeSlotManifestShrinkAbsent(manifest nativeagent.Manifest, spec nativeagent.Spec, expectedOrigin string) error {
	return requireNativeSlotManifestShrinkAbsentWithPlanner(manifest, spec, expectedOrigin, planNativeSlotProcesses)
}

func requireNativeSlotManifestShrinkAbsentWithGeneratedSetup(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
	expectedOrigin string,
	generatedSetupLayerFiles []string,
) error {
	return requireNativeSlotManifestShrinkAbsentWithPlannerAndCustody(
		manifest,
		spec,
		expectedOrigin,
		nativeSlotManifestShrinkCustodyPolicy{generatedSetupLayerFiles: generatedSetupLayerFiles},
		planNativeSlotProcesses,
	)
}

func requireNativeSlotManifestShrinkAbsentWithCustody(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
	expectedOrigin string,
	policy nativeSlotManifestShrinkCustodyPolicy,
) error {
	return requireNativeSlotManifestShrinkAbsentWithPlannerAndCustody(
		manifest,
		spec,
		expectedOrigin,
		policy,
		planNativeSlotProcesses,
	)
}

func reconcileNativeSlotManifestShrink(
	path string,
	expected nativeagent.Manifest,
	opts nativeplan.BuildOptions,
	resetOptions wtx.NativeSlotResetOptions,
	dryRun bool,
	remoteProofConfigs ...nativeSlotManifestShrinkRemoteProofConfig,
) (existed bool, retErr error) {
	if _, err := nativeSlotManifestShrinkGeneratedSetupLayerFiles(resetOptions.GeneratedSetupLayerFiles); err != nil {
		return false, err
	}
	if _, err := nativeSlotManifestShrinkDisposableGeneratedResidueRoots(resetOptions.DisposableGeneratedResidueRoots); err != nil {
		return false, err
	}
	if len(remoteProofConfigs) == 0 && !resetOptions.RequirePackageSourceExecutables {
		// Source tests and non-promoted local fixtures use file origins. The
		// promoted command always supplies explicit package transport authority.
		remoteProofConfigs = []nativeSlotManifestShrinkRemoteProofConfig{{
			ScratchParent:  filepath.Dir(resetOptions.StateRoot),
			ProtectedRoots: append([]string(nil), resetOptions.ProtectedRoots...),
		}}
	}
	remoteProof, err := newNativeSlotManifestShrinkRemoteProofOperation(remoteProofConfigs)
	if err != nil {
		return false, err
	}
	defer func() {
		retErr = errors.Join(retErr, remoteProof.Close())
	}()
	custodyPolicy := nativeSlotManifestShrinkCustodyPolicy{
		generatedSetupLayerFiles:        append([]string(nil), resetOptions.GeneratedSetupLayerFiles...),
		disposableGeneratedResidueRoots: append([]string(nil), resetOptions.DisposableGeneratedResidueRoots...),
		remoteProof:                     remoteProof,
	}
	existed, actual, err := readNativeSlotManifest(path)
	if err != nil || !existed {
		return existed, err
	}
	recovered, err := recoverNativeManifestShrinkTransaction(path, actual, expected, opts, resetOptions, dryRun, custodyPolicy)
	if err != nil {
		return false, err
	}
	if recovered {
		fmt.Fprintf(
			os.Stderr,
			"native_manifest_shrink status=recovered expected_count=%d manifest=%s\n",
			expected.Count,
			path,
		)
		return true, nil
	}
	// Recovery may have rolled back a prepared pre-CAS transaction. Re-read the
	// manifest rather than relying on the value observed before recovery.
	existed, actual, err = readNativeSlotManifest(path)
	if err != nil || !existed {
		return existed, err
	}
	if reflect.DeepEqual(actual, expected) {
		return true, nil
	}
	canonicalActual, err := nativeplan.BuildManifest(opts, actual.Count)
	if err != nil {
		return false, fmt.Errorf("derive canonical native manifest for declared count %d: %w", actual.Count, err)
	}
	surplus, err := classifyNativeSlotManifestShrink(actual, expected, canonicalActual)
	if err != nil {
		return false, fmt.Errorf("shared native manifest %s does not match the source-derived all-slot geometry: %w", path, err)
	}
	// Validate the entire suffix before any history, quarantine, or manifest
	// effect. A late dirty/ahead/active lane therefore prevents every retirement.
	for _, spec := range surplus {
		if err := requireNativeSlotManifestShrinkAbsentWithCustody(actual, spec, resetOptions.Origin, custodyPolicy); err != nil {
			return false, fmt.Errorf("refuse shared native manifest shrink: %w", err)
		}
	}
	plans, err := planNativeManifestShrinkSurplus(actual, surplus, resetOptions, dryRun)
	if err != nil {
		return false, err
	}
	if err := captureNativeManifestShrinkHistory(actual, surplus, dryRun); err != nil {
		return false, err
	}
	for _, spec := range surplus {
		if err := requireNativeSlotManifestShrinkAbsentWithCustody(actual, spec, resetOptions.Origin, custodyPolicy); err != nil {
			return false, fmt.Errorf("refuse shared native manifest shrink recheck: %w", err)
		}
	}
	stableExists, stableActual, err := readNativeSlotManifest(path)
	if err != nil {
		return false, err
	}
	if !stableExists || !reflect.DeepEqual(stableActual, actual) {
		return false, fmt.Errorf("shared native manifest changed during shrink preflight")
	}
	if dryRun {
		for _, plan := range plans {
			transaction, err := plan.Stage()
			if err != nil {
				return false, err
			}
			if err := transaction.Commit(); err != nil {
				return false, err
			}
		}
		if err := compareAndSwapNativeSlotManifest(path, actual, expected, true); err != nil {
			return false, err
		}
		fmt.Fprintf(
			os.Stderr,
			"native_manifest_shrink status=planned prior_count=%d expected_count=%d retired_indices=%s manifest=%s\n",
			actual.Count,
			expected.Count,
			nativeManifestShrinkIndices(surplus),
			path,
		)
		return true, nil
	}

	transactions := make([]*wtx.NativeResetTransaction, 0, len(plans))
	for index, plan := range plans {
		transaction, err := plan.PrepareTransaction()
		if err != nil {
			return false, fmt.Errorf("prepare surplus native slot %d retirement: %w", surplus[index].ID.Index, err)
		}
		transactions = append(transactions, transaction)
	}
	journal := nativeManifestShrinkJournal(path, actual, expected, surplus)
	for _, transaction := range transactions {
		journal.SlotTransactions = append(journal.SlotTransactions, transaction.State())
	}
	transactionPath := nativeManifestShrinkTransactionPath(path)
	rollbackPreCAS := func(cause error) error {
		rollbackErr := rollbackNativeManifestShrinkTransactions(transactions)
		if rollbackErr == nil {
			if err := removeNativeManifestShrinkTransaction(transactionPath); err != nil {
				rollbackErr = err
			}
		}
		if rollbackErr != nil {
			return errors.Join(
				cause,
				fmt.Errorf("pre-CAS rollback incomplete; typed recovery retained at %s: %w", transactionPath, rollbackErr),
			)
		}
		return cause
	}
	if err := writeNativeManifestShrinkTransaction(transactionPath, journal); err != nil {
		return false, rollbackPreCAS(fmt.Errorf("persist prepared native manifest shrink transaction: %w", err))
	}
	for index, transaction := range transactions {
		if err := transaction.StagePrepared(); err != nil {
			return false, rollbackPreCAS(fmt.Errorf(
				"stage prepared surplus native slot %d retirement: %w",
				surplus[index].ID.Index,
				err,
			))
		}
	}
	stagedState := mergeNativeManifestShrinkTransactionStates(journal.SlotTransactions)
	for _, spec := range surplus {
		if err := requireNativeSlotManifestShrinkStagedWithCustody(actual, spec, resetOptions.Origin, stagedState, custodyPolicy); err != nil {
			return false, rollbackPreCAS(fmt.Errorf("verify staged surplus native slot %d retirement: %w", spec.ID.Index, err))
		}
	}
	if err := compareAndSwapNativeSlotManifest(path, actual, expected, false); err != nil {
		return false, rollbackPreCAS(err)
	}
	journal.Status = "committed"
	if err := writeNativeManifestShrinkTransaction(transactionPath, journal); err != nil {
		return false, fmt.Errorf(
			"manifest CAS committed but typed cleanup receipt could not advance; recover from %s: %w",
			transactionPath,
			err,
		)
	}
	if err := commitNativeManifestShrinkTransactions(transactions); err != nil {
		return false, fmt.Errorf("post-CAS surplus native slot cleanup failed; typed recovery retained at %s: %w", transactionPath, err)
	}
	for _, spec := range surplus {
		if err := requireNativeSlotManifestShrinkStagedWithCustody(actual, spec, resetOptions.Origin, wtx.NativeResetTransactionState{}, custodyPolicy); err != nil {
			return false, fmt.Errorf("strict surplus native slot %d absence postcondition failed; typed recovery retained at %s: %w", spec.ID.Index, transactionPath, err)
		}
	}
	if err := removeNativeManifestShrinkTransaction(transactionPath); err != nil {
		return false, err
	}
	fmt.Fprintf(
		os.Stderr,
		"native_manifest_shrink status=complete prior_count=%d expected_count=%d retired_indices=%s manifest=%s\n",
		actual.Count,
		expected.Count,
		nativeManifestShrinkIndices(surplus),
		path,
	)
	return true, nil
}

func handleNativeSlotReset(ctx *cmdregistry.Context) (retErr error) {
	if strings.TrimSpace(ctx.Project) != "dev-all" {
		return fmt.Errorf("native reset requires the exact dev-all project")
	}
	parsed, err := parseNativeSlotResetArgs(ctx)
	if err != nil {
		return err
	}
	output, err := reserveNativeSlotResetJSONOutput(parsed.format)
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, output.close())
	}()
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, ctx.Project)
	if err != nil {
		return err
	}
	if !config.HasRuntimeFlake(cfg) {
		return fmt.Errorf("native reset requires the source-declared dev-all runtime flake")
	}
	if hostRoot := strings.TrimSpace(cfg.Native.HostRoot); hostRoot == "" || !filepath.IsAbs(hostRoot) {
		return fmt.Errorf("native reset requires an absolute source-declared host root")
	}
	declaredRepo := strings.TrimSpace(cfg.Defaults.Repo)
	if declaredRepo == "" || parsed.repo != declaredRepo {
		return fmt.Errorf("native reset repository %q must equal the source-declared repository %q", parsed.repo, declaredRepo)
	}
	count := cfg.Defaults.Agents
	if count < 1 {
		return fmt.Errorf("native reset requires a positive source-declared agent count")
	}
	if parsed.index > count {
		return fmt.Errorf("native reset index %d is outside declared capacity 1..%d", parsed.index, count)
	}
	baseBranch := strings.TrimSpace(cfg.Defaults.BaseBranch)
	if baseBranch == "" {
		baseBranch = "main"
	}
	branchPrefix := strings.TrimSpace(cfg.Defaults.BranchPrefix)
	if branchPrefix == "" {
		branchPrefix = "agent"
	}
	// Selected-slot reset always reconstructs one GUI-owned Product lane. An
	// exact caller target ID selects its immutable projection directly. The
	// controller-local route intentionally carries only source-derived lane
	// geometry, so require that geometry to select exactly one projection
	// before the destructive boundary. Neither path admits ambient config.
	lifecycleParsed := nativeSlotResetLifecycleArgs(parsed, declaredRepo, baseBranch, branchPrefix)
	if err := applyLifecycleReadinessMode(&lifecycleParsed, cfg); err != nil {
		return err
	}
	brokerCfg := lifecycleBrokerConfig(ctx, cfg, lifecycleParsed)
	opts := lifecyclePlanOptions(ctx, cfg, lifecycleParsed, declaredRepo, brokerCfg)
	opts.Index = parsed.index
	selectedPlan, err := nativeplan.BuildDevAll(opts)
	if err != nil {
		return err
	}
	// The caller must name the exact selected lane so a route/inventory
	// mismatch fails before effects. It never feeds lifecycle planning: source
	// configuration remains the sole authority for the worktree, home, state,
	// branch, and runtime geometry.
	workspaceRoot, err := validateNativeSlotWorkspaceRoot(parsed.workspaceRoot, selectedPlan)
	if err != nil {
		return err
	}
	expectedManifest, err := nativeplan.BuildManifest(opts, count)
	if err != nil {
		return err
	}
	manifestPath := nativeagent.ManifestPath(expectedManifest.HostStateRoot, ctx.Project)
	historyCustodyRoot, err := nativeWorkspaceHistoryCustodyRoot(workspaceRoot, ctx.Project)
	if err != nil {
		return err
	}
	protectedRoots := []string{
		ctx.Paths.Root,
		ctx.Paths.RuntimeAuthorityRoot,
		filepath.Join(resolveNativeHostRoot(ctx.Paths.Root, cfg), declaredRepo),
		historyCustodyRoot,
	}
	protectedRoots = append(protectedRoots, ctx.Paths.OverlayPaths...)
	resetOptions := wtx.NativeSlotResetOptions{
		Project:                         ctx.Project,
		Repo:                            declaredRepo,
		Origin:                          strings.TrimSpace(cfg.Defaults.Origin),
		BranchPrefix:                    branchPrefix,
		Index:                           parsed.index,
		Count:                           count,
		WorktreeRoot:                    opts.WorktreeRoot,
		StateRoot:                       opts.StateRoot,
		ProtectedRoots:                  protectedRoots,
		GeneratedSetupLayerFiles:        append([]string(nil), cfg.Native.GeneratedSetupLayerFiles...),
		DisposableGeneratedResidueRoots: append([]string(nil), cfg.Native.DisposableGeneratedResidueRoots...),
		RequirePackageSourceExecutables: true,
		DryRun:                          ctx.DryRun,
	}
	resetPlan, err := wtx.PlanNativeSlotReset(resetOptions)
	if err != nil {
		return err
	}
	mutationRoots, err := nativeSlotMutationRoots(resetOptions, selectedPlan, manifestPath, brokerCfg)
	if err != nil {
		return err
	}
	if !ctx.DryRun {
		if err := preflightNativeSlotMutationRoots(mutationRoots); err != nil {
			return err
		}
	}
	coordinationRoot, err := nativeSlotResetCoordinationRoot(resetOptions.WorktreeRoot)
	if err != nil {
		return err
	}
	resetLock, err := acquireNativeSlotResetLock(coordinationRoot, ctx.Project)
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, resetLock.release())
	}()
	gitSSHCommand, err := launch.GitBootstrapSSHCommand(selectedPlan)
	if err != nil {
		return err
	}
	manifestExisted, err := reconcileNativeSlotManifestShrink(
		manifestPath,
		expectedManifest,
		opts,
		resetOptions,
		ctx.DryRun,
		nativeSlotManifestShrinkRemoteProofConfig{
			ScratchParent:                   filepath.Dir(resetOptions.StateRoot),
			ProtectedRoots:                  append([]string(nil), resetOptions.ProtectedRoots...),
			GitSSHCommand:                   gitSSHCommand,
			RequirePackageSourceExecutables: true,
			StartTransport: func() (func() error, error) {
				// Current-remote proof is read-only with respect to the leased
				// slot, but it must use the same managed egress path as native
				// source acquisition even for a dry-run lifecycle request.
				return ensureManagedEgressProxy(selectedPlan, false)
			},
		},
	)
	if err != nil {
		return err
	}
	// Re-plan after acquiring the lifecycle lock so stale quarantine residue
	// cannot appear between discovery and the destructive boundary.
	resetPlan, err = wtx.PlanNativeSlotReset(resetOptions)
	if err != nil {
		return err
	}
	processIdentity := nativeSlotProcessIdentity{
		index:           parsed.index,
		hostWorktree:    selectedPlan.Agent.HostWorktree,
		sandboxWorktree: selectedPlan.Agent.SandboxWorktree,
		hostHome:        selectedPlan.Agent.HostHome,
		sandboxHome:     selectedPlan.Agent.SandboxHome,
		stateRoot:       selectedPlan.Agent.StateRoot,
		sandboxState:    selectedPlan.Agent.SandboxStateRoot,
	}
	processPlan, err := planNativeSlotProcesses(processIdentity, ctx.DryRun)
	if err != nil {
		return err
	}
	inspectIdle := func() error {
		if ctx.DryRun {
			return nil
		}
		observed, err := planNativeSlotProcesses(processIdentity, false)
		if err != nil {
			return err
		}
		if len(observed.pids) != 0 {
			return fmt.Errorf("selected native slot acquired new processes: %v", observed.pids)
		}
		return nil
	}
	if err := executeNativeSlotDestructiveBoundary(nativeSlotDestructiveBoundary{
		stopSelected: processPlan.Stop,
		inspectIdle:  inspectIdle,
		snapshotHistory: func() error {
			return captureNativeSlotHistory(ctx.Project, "selected-slot-reset", processIdentity, opts.StateRoot, workspaceRoot, ctx.DryRun)
		},
		applyPlan: resetPlan.Apply,
	}); err != nil {
		return err
	}

	brokerCreated := false
	brokerCreatedPID := 0
	cleanupCreatedBroker := func() error {
		if !brokerCreated {
			return nil
		}
		status, err := broker.Inspect(brokerCfg)
		if err != nil {
			return err
		}
		if !status.Running {
			return nil
		}
		if status.PID != brokerCreatedPID {
			return fmt.Errorf("shared broker process changed from created pid %d to pid %d during failed slot reconstruction", brokerCreatedPID, status.PID)
		}
		_, err = broker.Stop(brokerCfg, false)
		return err
	}
	cleanupFailedSlot := func(cause error) error {
		if ctx.DryRun {
			return cause
		}
		cleanupProcesses, processErr := planNativeSlotProcesses(processIdentity, false)
		if processErr != nil {
			// Never dispose files beneath an unowned live process. The failure is
			// terminal and typed; a later canonical reconstruction may retry after
			// the external effect has been reconciled.
			return errors.Join(cause, processErr, cleanupCreatedBroker())
		}
		if processErr = cleanupProcesses.Stop(); processErr != nil {
			return errors.Join(cause, processErr, cleanupCreatedBroker())
		}
		cleanupPlan, planErr := wtx.PlanNativeSlotReset(resetOptions)
		if planErr != nil {
			return errors.Join(cause, processErr, fmt.Errorf("plan selected-slot cleanup after failed reconstruction: %w", planErr), cleanupCreatedBroker())
		}
		cleanupErr := cleanupPlan.Apply()
		brokerErr := cleanupCreatedBroker()
		return errors.Join(cause, processErr, cleanupErr, brokerErr)
	}

	if !ctx.DryRun {
		beforeBroker, err := broker.Inspect(brokerCfg)
		if err != nil {
			return cleanupFailedSlot(err)
		}
		status, err := broker.Start(context.Background(), brokerCfg, false)
		if err != nil {
			return cleanupFailedSlot(err)
		}
		if !beforeBroker.Running && status.Running {
			brokerCreated = true
			brokerCreatedPID = status.PID
		}
		brokerCfg = lifecycleBrokerConfigWithStatusSocket(brokerCfg, status)
		opts = lifecyclePlanOptions(ctx, cfg, lifecycleParsed, declaredRepo, brokerCfg)
		opts.Index = parsed.index
		selectedPlan, err = nativeplan.BuildDevAll(opts)
		if err != nil {
			return cleanupFailedSlot(err)
		}
		if _, err := validateNativeSlotWorkspaceRoot(parsed.workspaceRoot, selectedPlan); err != nil {
			return cleanupFailedSlot(err)
		}
		activeManifest, err := nativeplan.BuildManifest(opts, count)
		if err != nil {
			return cleanupFailedSlot(err)
		}
		if !reflect.DeepEqual(activeManifest, expectedManifest) {
			return cleanupFailedSlot(fmt.Errorf("started broker changed the source-derived shared native manifest geometry"))
		}
	}
	bootstrapOpts := opts
	bootstrapOpts.Index = parsed.index
	bootstrapPlan, err := nativeplan.BuildDevAll(bootstrapOpts)
	if err != nil {
		return cleanupFailedSlot(err)
	}
	worktreeOptions := wtx.NativeOptions{
		DevkitRoot:          filepath.Join(resolveNativeHostRoot(ctx.Paths.Root, cfg), "devkit"),
		Repo:                declaredRepo,
		Origin:              strings.TrimSpace(cfg.Defaults.Origin),
		Index:               parsed.index,
		Count:               count,
		BaseBranch:          baseBranch,
		BranchPrefix:        branchPrefix,
		WorktreeRoot:        opts.WorktreeRoot,
		RequireSSHOrigin:    declaredRepo == "ouroboros-ide",
		ReconstructSelected: true,
	}
	if err := prepareNativeGitBootstrapAndWorktrees(bootstrapPlan, worktreeOptions, ctx.DryRun); err != nil {
		return cleanupFailedSlot(err)
	}
	if err := prepareWithManagedEgressProxy(selectedPlan, ctx.DryRun, launch.Prepare); err != nil {
		return cleanupFailedSlot(err)
	}
	planArgs := planArgs{opts: opts, skipRepoChecks: lifecycleParsed.skipRepoChecks}
	runtimeChecks, repoChecks, err := readinessChecksFor(ctx, planArgs)
	if err != nil {
		return cleanupFailedSlot(err)
	}
	report := runReadinessReport(selectedPlan, runtimeChecks, repoChecks)
	summary := capacity.Build(map[int]readiness.Report{parsed.index: report})
	if err := lifecycleReadinessError(summary, lifecycleParsed); err != nil {
		data, marshalErr := json.Marshal(summary)
		if marshalErr != nil {
			return cleanupFailedSlot(errors.Join(err, fmt.Errorf("encode failed native slot readiness: %w", marshalErr)))
		}
		return cleanupFailedSlot(fmt.Errorf("%w: readiness=%s", err, data))
	}
	head := ""
	if !ctx.DryRun {
		if err := wtx.VerifyFreshNativeWorktree(
			selectedPlan.Agent.HostWorktree,
			fmt.Sprintf("%s%d", branchPrefix, parsed.index),
			"origin/"+baseBranch,
			gitauthority.Executable(),
		); err != nil {
			return cleanupFailedSlot(fmt.Errorf("verify reconstructed native slot Git custody before launch: %w", err))
		}
		out, result := execx.Capture(context.Background(), gitauthority.Executable(), "-C", selectedPlan.Agent.HostWorktree, "rev-parse", "HEAD")
		if result.Code != 0 {
			return cleanupFailedSlot(fmt.Errorf("read reconstructed native slot HEAD: exit %d", result.Code))
		}
		head = strings.TrimSpace(out)
	}
	if !manifestExisted {
		if err := nativeagent.WriteManifest(manifestPath, expectedManifest, ctx.DryRun); err != nil {
			return cleanupFailedSlot(err)
		}
	}
	return finishNativeSlotResetOutput(output, nativeSlotResetStatus{
		Command: "reset", Runtime: "native", Repo: declaredRepo, Index: parsed.index,
		Status: "ready", Head: head, ManifestPath: manifestPath, Capacity: summary,
	}, parsed.format)
}

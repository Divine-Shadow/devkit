package nativecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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

func validateNativeSlotManifest(path string, expected nativeagent.Manifest) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read shared native manifest %s: %w", path, err)
	}
	var actual nativeagent.Manifest
	if err := json.Unmarshal(data, &actual); err != nil {
		return false, fmt.Errorf("decode shared native manifest %s: %w", path, err)
	}
	if !reflect.DeepEqual(actual, expected) {
		return false, fmt.Errorf("shared native manifest %s does not match the source-derived all-slot geometry", path)
	}
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
	lifecycleParsed := lifecycleArgs{repo: declaredRepo, baseBranch: baseBranch, branchPrefix: branchPrefix}
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
	manifestExisted, err := validateNativeSlotManifest(manifestPath, expectedManifest)
	if err != nil {
		return err
	}
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

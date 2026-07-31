package nativecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	repo   string
	index  int
	format string
}

func parseNativeSlotResetArgs(ctx *cmdregistry.Context) (nativeSlotResetArgs, error) {
	parsed := nativeSlotResetArgs{format: "text"}
	for i := 1; i < len(ctx.Args); i++ {
		switch ctx.Args[i] {
		case "--repo":
			if i+1 >= len(ctx.Args) || strings.HasPrefix(ctx.Args[i+1], "--") {
				return parsed, fmt.Errorf("--repo requires a value")
			}
			parsed.repo = strings.TrimSpace(ctx.Args[i+1])
			i++
		case "--index":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--index requires a value")
			}
			index, err := strconv.Atoi(ctx.Args[i+1])
			if err != nil || index < 1 {
				return parsed, fmt.Errorf("--index must be a positive integer")
			}
			parsed.index = index
			i++
		case "--format":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--format requires a value")
			}
			parsed.format = strings.TrimSpace(ctx.Args[i+1])
			if parsed.format != "text" && parsed.format != "json" {
				return parsed, fmt.Errorf("--format must be text or json")
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
	return parsed, nil
}

type nativeSlotProcessIdentity struct {
	index           int
	hostWorktree    string
	sandboxWorktree string
	hostHome        string
	sandboxHome     string
	stateRoot       string
	sandboxState    string
	owner           *nativeSlotOwner
	orphanRules     []nativeSlotOrphanRule
}

type nativeSlotProcessPlan struct {
	identity           nativeSlotProcessIdentity
	pids               []int
	starts             map[int]string
	parents            map[int]int
	adoptedRoots       map[int]string
	adoptedDescendants map[int]int
	bootID             string
	signalOutcomes     map[int]*nativeSlotProcessSignalOutcome
	dryRun             bool
}

type nativeSlotProcessSignalOutcome struct {
	termAttempted bool
	termOutcome   string
	killAttempted bool
	killOutcome   string
}

type nativeSlotProcessHandles struct {
	byPID map[int]int
}

type nativeProc struct {
	pid        int
	ppid       int
	start      string
	environ    map[string]string
	touches    bool
	fdTouches  bool
	uid        [4]uint32
	gid        [4]uint32
	hasOwner   bool
	args       []string
	cwd        string
	executable string
}

type nativeSlotOwner struct {
	uid uint32
	gid uint32
}

type nativeSlotOrphanRule struct {
	name                    string
	executableName          string
	argumentsBeforeLauncher []string
	launcherRelativePath    string
	codexHomeRoot           string
}

var (
	nativeSlotProcRoot           = "/proc"
	nativeExecutableEvalSymlinks = filepath.EvalSymlinks
	nativeReadBootID             = readNativeBootID
	nativeOpenPidfd              = openNativePidfd
	nativeSendPidfdSignal        = sendNativePidfdSignal
	nativeClosePidfd             = syscall.Close
	nativeSelectedProcessExists  = nativeProcessExists
	nativeTermWait               = 2 * time.Second
	nativeKillWait               = time.Second
	nativeStopPoll               = 20 * time.Millisecond
)

func normalizeNativeSlotOrphanRules(declared []config.NativeResetOrphanProcess) ([]nativeSlotOrphanRule, error) {
	rules := make([]nativeSlotOrphanRule, 0, len(declared))
	names := map[string]bool{}
	for index, candidate := range declared {
		name := strings.TrimSpace(candidate.Name)
		if name == "" {
			return nil, fmt.Errorf("native reset orphan process rule %d requires a name", index+1)
		}
		if names[name] {
			return nil, fmt.Errorf("native reset orphan process rule name %q is duplicated", name)
		}
		names[name] = true
		executableName := strings.TrimSpace(candidate.ExecutableName)
		if executableName == "" || executableName == "." || executableName == ".." || filepath.Base(executableName) != executableName {
			return nil, fmt.Errorf("native reset orphan process rule %q requires an executable basename", name)
		}
		arguments := append([]string(nil), candidate.ArgumentsBeforeLauncher...)
		if len(arguments) == 0 {
			return nil, fmt.Errorf("native reset orphan process rule %q requires exact arguments before its launcher", name)
		}
		for _, argument := range arguments {
			if strings.TrimSpace(argument) == "" || strings.ContainsRune(argument, '\x00') {
				return nil, fmt.Errorf("native reset orphan process rule %q contains an invalid launcher argument", name)
			}
		}
		launcher := strings.TrimSpace(candidate.LauncherRelativePath)
		cleanLauncher := filepath.Clean(launcher)
		if launcher == "" || cleanLauncher == "." || filepath.IsAbs(cleanLauncher) || cleanLauncher != launcher ||
			cleanLauncher == ".." || strings.HasPrefix(cleanLauncher, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("native reset orphan process rule %q requires a clean relative launcher path", name)
		}
		codexHomeRoot := strings.TrimSpace(candidate.CodexHomeRoot)
		cleanCodexHomeRoot := filepath.Clean(codexHomeRoot)
		if codexHomeRoot == "" || !filepath.IsAbs(cleanCodexHomeRoot) || cleanCodexHomeRoot != codexHomeRoot || cleanCodexHomeRoot == string(filepath.Separator) {
			return nil, fmt.Errorf("native reset orphan process rule %q requires a clean absolute Codex home root", name)
		}
		rules = append(rules, nativeSlotOrphanRule{
			name:                    name,
			executableName:          executableName,
			argumentsBeforeLauncher: arguments,
			launcherRelativePath:    cleanLauncher,
			codexHomeRoot:           cleanCodexHomeRoot,
		})
	}
	return rules, nil
}

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

func nativeProcTouchEvidence(procPath string, targets []string) (bool, bool) {
	touches := false
	for _, name := range []string{"cwd", "root", "exe"} {
		path, err := os.Readlink(filepath.Join(procPath, name))
		if err == nil && nativePathTouchesTargets(path, targets) {
			touches = true
		}
	}
	entries, err := os.ReadDir(filepath.Join(procPath, "fd"))
	if err != nil {
		return touches, false
	}
	fdTouches := false
	for _, entry := range entries {
		path, err := os.Readlink(filepath.Join(procPath, "fd", entry.Name()))
		if err == nil && nativePathTouchesTargets(path, targets) {
			touches = true
			fdTouches = true
		}
	}
	return touches, fdTouches
}

func readNativeProcOwner(path string) ([4]uint32, [4]uint32, bool) {
	var uid [4]uint32
	var gid [4]uint32
	data, err := os.ReadFile(filepath.Join(path, "status"))
	if err != nil {
		return uid, gid, false
	}
	read := func(prefix string, target *[4]uint32) bool {
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			fields := strings.Fields(strings.TrimPrefix(line, prefix))
			if len(fields) != len(target) {
				return false
			}
			for index, field := range fields {
				value, err := strconv.ParseUint(field, 10, 32)
				if err != nil {
					return false
				}
				target[index] = uint32(value)
			}
			return true
		}
		return false
	}
	uidOK := read("Uid:", &uid)
	gidOK := read("Gid:", &gid)
	return uid, gid, uidOK && gidOK
}

func readNativeProcArgs(path string) []string {
	data, err := os.ReadFile(filepath.Join(path, "cmdline"))
	if err != nil {
		return nil
	}
	fields := strings.Split(string(data), "\x00")
	for len(fields) > 0 && fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	return fields
}

func readNativeProc(pid int, targets []string) nativeProc {
	path := filepath.Join(nativeSlotProcRoot, strconv.Itoa(pid))
	environData, environErr := os.ReadFile(filepath.Join(path, "environ"))
	environ := map[string]string{}
	if environErr == nil {
		environ = parseNativeProcEnviron(environData)
	}
	touches, fdTouches := nativeProcTouchEvidence(path, targets)
	ppid, start := readNativeProcIdentity(path)
	uid, gid, hasOwner := readNativeProcOwner(path)
	cwd, _ := os.Readlink(filepath.Join(path, "cwd"))
	executable, _ := os.Readlink(filepath.Join(path, "exe"))
	return nativeProc{
		pid:        pid,
		ppid:       ppid,
		start:      start,
		environ:    environ,
		touches:    touches,
		fdTouches:  fdTouches,
		uid:        uid,
		gid:        gid,
		hasOwner:   hasOwner,
		args:       readNativeProcArgs(path),
		cwd:        cwd,
		executable: executable,
	}
}

func nativeProcHasSlotIdentity(env map[string]string, identity nativeSlotProcessIdentity) bool {
	if env["DEVKIT_NATIVE_AGENT"] != strconv.Itoa(identity.index) {
		return false
	}
	home := filepath.Clean(strings.TrimSpace(env["HOME"]))
	codexHome := filepath.Clean(strings.TrimSpace(env["CODEX_HOME"]))
	for _, expected := range []string{identity.hostHome, identity.sandboxHome} {
		expected = filepath.Clean(strings.TrimSpace(expected))
		if expected != "" && expected != "." && home == expected && codexHome == filepath.Join(expected, ".codex") {
			return true
		}
	}
	return false
}

func discoverNativeSlotOwner(identity nativeSlotProcessIdentity) (*nativeSlotOwner, error) {
	var owner *nativeSlotOwner
	for _, path := range []string{identity.hostWorktree, identity.sandboxWorktree} {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "" || path == "." {
			continue
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect native slot owner at %s: %w", path, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("native slot owner path %s must be a real directory", path)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return nil, fmt.Errorf("native slot owner path %s has no Unix ownership identity", path)
		}
		candidate := nativeSlotOwner{uid: stat.Uid, gid: stat.Gid}
		if owner != nil && *owner != candidate {
			return nil, fmt.Errorf("native slot host and sandbox ownership identities disagree")
		}
		value := candidate
		owner = &value
	}
	return owner, nil
}

func nativeProcOwnerMatches(process nativeProc, owner *nativeSlotOwner) bool {
	if owner == nil || !process.hasOwner {
		return false
	}
	for _, value := range process.uid {
		if value != owner.uid {
			return false
		}
	}
	for _, value := range process.gid {
		if value != owner.gid {
			return false
		}
	}
	return true
}

func nativePathWithin(root, candidate string, minimumDescendants int) bool {
	root = filepath.Clean(strings.TrimSpace(root))
	candidate = filepath.Clean(strings.TrimSpace(candidate))
	if root == "" || root == "." || candidate == "" || candidate == "." || !filepath.IsAbs(root) || !filepath.IsAbs(candidate) {
		return false
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	return len(strings.Split(relative, string(filepath.Separator))) >= minimumDescendants
}

func nativeNixStoreExecutableMatches(executable, argument, name string) bool {
	executable = filepath.Clean(strings.TrimSuffix(strings.TrimSpace(executable), " (deleted)"))
	argument = filepath.Clean(strings.TrimSuffix(strings.TrimSpace(argument), " (deleted)"))
	name = strings.TrimSpace(name)
	if executable == "" || argument == "" || filepath.Base(executable) != name || filepath.Base(argument) != name {
		return false
	}
	if filepath.Base(filepath.Dir(executable)) != "bin" || filepath.Base(filepath.Dir(argument)) != "bin" ||
		!nativePathWithin("/nix/store", executable, 3) || !nativePathWithin("/nix/store", argument, 3) {
		return false
	}
	if executable == argument {
		return true
	}
	resolved, err := nativeExecutableEvalSymlinks(argument)
	return err == nil && filepath.Clean(resolved) == executable
}

func nativeStringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func nativeProcMatchesOrphanRule(process nativeProc, identity nativeSlotProcessIdentity, rule nativeSlotOrphanRule) bool {
	if process.ppid != 1 || !process.touches || !process.fdTouches || !nativeProcOwnerMatches(process, identity.owner) {
		return false
	}
	if strings.TrimSpace(process.environ["DEVKIT_NATIVE_AGENT"]) != "" || len(process.args) < 2 {
		return false
	}
	if !nativeNixStoreExecutableMatches(process.executable, process.args[0], rule.executableName) {
		return false
	}
	type pathPair struct {
		worktree string
		home     string
	}
	pairs := []pathPair{
		{worktree: identity.hostWorktree, home: identity.hostHome},
		{worktree: identity.sandboxWorktree, home: identity.sandboxHome},
	}
	for _, pair := range pairs {
		worktree := filepath.Clean(strings.TrimSpace(pair.worktree))
		home := filepath.Clean(strings.TrimSpace(pair.home))
		if worktree == "" || worktree == "." || home == "" || home == "." {
			continue
		}
		if filepath.Clean(strings.TrimSpace(process.cwd)) != worktree ||
			filepath.Clean(strings.TrimSpace(process.environ["HOME"])) != home {
			continue
		}
		launcher := filepath.Join(worktree, rule.launcherRelativePath)
		expectedArgs := make([]string, 0, len(rule.argumentsBeforeLauncher)+2)
		expectedArgs = append(expectedArgs, process.args[0])
		expectedArgs = append(expectedArgs, rule.argumentsBeforeLauncher...)
		expectedArgs = append(expectedArgs, launcher)
		if !nativeStringSlicesEqual(process.args, expectedArgs) {
			continue
		}
		codexHomeBase := filepath.Join(
			rule.codexHomeRoot,
			fmt.Sprintf("agent%d", identity.index),
			"control-plane",
			"workspace-submit-artifacts",
		)
		if !nativePathWithin(codexHomeBase, process.environ["CODEX_HOME"], 2) {
			continue
		}
		return true
	}
	return false
}

func nativeProcMatchingOrphanRule(process nativeProc, identity nativeSlotProcessIdentity) string {
	for _, rule := range identity.orphanRules {
		if nativeProcMatchesOrphanRule(process, identity, rule) {
			return rule.name
		}
	}
	return ""
}

func nativeSlotOrphanRuleNamed(identity nativeSlotProcessIdentity, name string) (nativeSlotOrphanRule, bool) {
	for _, rule := range identity.orphanRules {
		if rule.name == name {
			return rule, true
		}
	}
	return nativeSlotOrphanRule{}, false
}

func nativeSlotProcessTargets(identity nativeSlotProcessIdentity) []string {
	return []string{
		identity.hostWorktree,
		identity.sandboxWorktree,
		identity.hostHome,
		identity.sandboxHome,
		identity.stateRoot,
		identity.sandboxState,
	}
}

func readNativeBootID() string {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func planNativeSlotProcesses(identity nativeSlotProcessIdentity, dryRun bool) (*nativeSlotProcessPlan, error) {
	targets := nativeSlotProcessTargets(identity)
	entries, err := os.ReadDir(nativeSlotProcRoot)
	if err != nil {
		return nil, fmt.Errorf("read native process inventory: %w", err)
	}
	processes := map[int]nativeProc{}
	owned := map[int]bool{}
	starts := map[int]string{}
	adoptedRoots := map[int]string{}
	adoptedDescendants := map[int]int{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid < 1 || pid == os.Getpid() {
			continue
		}
		process := readNativeProc(pid, targets)
		processes[pid] = process
		if nativeProcHasSlotIdentity(process.environ, identity) {
			owned[pid] = true
		} else if ruleName := nativeProcMatchingOrphanRule(process, identity); ruleName != "" {
			owned[pid] = true
			adoptedRoots[pid] = ruleName
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
				if ruleName, ok := adoptedRoots[process.ppid]; ok && ruleName != "" {
					adoptedDescendants[pid] = process.ppid
				} else if rootPID, ok := adoptedDescendants[process.ppid]; ok {
					adoptedDescendants[pid] = rootPID
				}
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
	parents := make(map[int]int, len(owned))
	for pid := range owned {
		pids = append(pids, pid)
		starts[pid] = processes[pid].start
		parents[pid] = processes[pid].ppid
	}
	depths := make(map[int]int, len(pids))
	for _, pid := range pids {
		current := pid
		visited := map[int]bool{}
		for owned[parents[current]] {
			if visited[current] {
				return nil, fmt.Errorf("selected native process ancestry contains a cycle at pid %d", current)
			}
			visited[current] = true
			depths[pid]++
			current = parents[current]
		}
	}
	sort.Slice(pids, func(left, right int) bool {
		leftPID, rightPID := pids[left], pids[right]
		if depths[leftPID] != depths[rightPID] {
			return depths[leftPID] > depths[rightPID]
		}
		return leftPID > rightPID
	})
	bootID := ""
	if len(pids) != 0 {
		bootID = strings.TrimSpace(nativeReadBootID())
		if bootID == "" {
			return nil, fmt.Errorf("selected native processes have no stable boot identity")
		}
	}
	return &nativeSlotProcessPlan{
		identity:           identity,
		pids:               pids,
		starts:             starts,
		parents:            parents,
		adoptedRoots:       adoptedRoots,
		adoptedDescendants: adoptedDescendants,
		bootID:             bootID,
		signalOutcomes:     map[int]*nativeSlotProcessSignalOutcome{},
		dryRun:             dryRun,
	}, nil
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

func (plan *nativeSlotProcessPlan) selectedProcessStillMatches(pid int) (bool, error) {
	if plan.bootID == "" || strings.TrimSpace(nativeReadBootID()) != plan.bootID {
		return false, fmt.Errorf("selected native process %d lost its stable boot identity", pid)
	}
	matches, err := nativeProcessStillMatches(pid, plan.starts[pid])
	if err != nil || !matches {
		return matches, err
	}
	targets := nativeSlotProcessTargets(plan.identity)
	if ruleName, adopted := plan.adoptedRoots[pid]; adopted {
		rule, ok := nativeSlotOrphanRuleNamed(plan.identity, ruleName)
		if !ok {
			return false, fmt.Errorf("selected native orphan process %d lost source rule %q", pid, ruleName)
		}
		process := readNativeProc(pid, targets)
		if process.start != plan.starts[pid] || !nativeProcMatchesOrphanRule(process, plan.identity, rule) {
			return false, fmt.Errorf("selected native orphan process %d changed before signaling", pid)
		}
		return true, nil
	}
	rootPID, adoptedDescendant := plan.adoptedDescendants[pid]
	if !adoptedDescendant {
		return true, nil
	}
	rootMatches, err := plan.selectedProcessStillMatches(rootPID)
	if err != nil || !rootMatches {
		return false, err
	}
	currentPID := pid
	visited := map[int]bool{}
	for currentPID != rootPID {
		if currentPID < 1 || visited[currentPID] {
			return false, fmt.Errorf("selected native orphan descendant %d lost its source-declared ancestry", pid)
		}
		visited[currentPID] = true
		process := readNativeProc(currentPID, targets)
		expectedStart, selected := plan.starts[currentPID]
		if !selected || process.start == "" || process.start != expectedStart ||
			!process.touches || !nativeProcOwnerMatches(process, plan.identity.owner) {
			return false, fmt.Errorf("selected native orphan descendant %d changed before signaling", pid)
		}
		marker := strings.TrimSpace(process.environ["DEVKIT_NATIVE_AGENT"])
		if marker != "" && marker != strconv.Itoa(plan.identity.index) {
			return false, fmt.Errorf("selected native orphan descendant %d acquired a contradictory slot identity", pid)
		}
		currentPID = process.ppid
	}
	return true, nil
}

func (plan *nativeSlotProcessPlan) selectedProcessStillMatchesAfterTerm(pid int) (bool, error) {
	if plan.bootID == "" || strings.TrimSpace(nativeReadBootID()) != plan.bootID {
		return false, fmt.Errorf("selected native process %d lost its stable boot identity", pid)
	}
	matches, err := nativeProcessStillMatches(pid, plan.starts[pid])
	if err != nil || !matches {
		return matches, err
	}
	if _, adoptedDescendant := plan.adoptedDescendants[pid]; !adoptedDescendant {
		return plan.selectedProcessStillMatches(pid)
	}
	// A descendant may be reparented after the package-owned TERM phase stops
	// its adopted root. The already-open pidfd preserves kernel identity; retain
	// custody only while stable start, owner, slot contact, and marker evidence
	// remain non-contradictory.
	process := readNativeProc(pid, nativeSlotProcessTargets(plan.identity))
	if process.start == "" || process.start != plan.starts[pid] ||
		!process.touches || !nativeProcOwnerMatches(process, plan.identity.owner) {
		return false, fmt.Errorf("selected native orphan descendant %d changed after TERM", pid)
	}
	marker := strings.TrimSpace(process.environ["DEVKIT_NATIVE_AGENT"])
	if marker != "" && marker != strconv.Itoa(plan.identity.index) {
		return false, fmt.Errorf("selected native orphan descendant %d acquired a contradictory slot identity after TERM", pid)
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

func (handles *nativeSlotProcessHandles) close() error {
	if handles == nil {
		return nil
	}
	var result error
	for pid, fd := range handles.byPID {
		if err := nativeClosePidfd(fd); err != nil {
			result = errors.Join(result, fmt.Errorf("close selected native process %d pidfd: %w", pid, err))
		}
		delete(handles.byPID, pid)
	}
	return result
}

func (plan *nativeSlotProcessPlan) openHandles() (*nativeSlotProcessHandles, error) {
	handles := &nativeSlotProcessHandles{byPID: make(map[int]int, len(plan.pids))}
	for _, pid := range plan.pids {
		fd, err := nativeOpenPidfd(pid)
		if err != nil {
			return nil, errors.Join(
				fmt.Errorf("acquire identity-bound handle for selected native process %d: %w", pid, err),
				handles.close(),
			)
		}
		handles.byPID[pid] = fd
	}
	return handles, nil
}

func (plan *nativeSlotProcessPlan) prevalidateSelectedSet() (map[int]bool, error) {
	matches := make(map[int]bool, len(plan.pids))
	for _, pid := range plan.pids {
		matched, err := plan.selectedProcessStillMatches(pid)
		if err != nil {
			return nil, err
		}
		matches[pid] = matched
	}
	return matches, nil
}

func (plan *nativeSlotProcessPlan) signalOutcome(pid int) *nativeSlotProcessSignalOutcome {
	outcome := plan.signalOutcomes[pid]
	if outcome == nil {
		outcome = &nativeSlotProcessSignalOutcome{}
		plan.signalOutcomes[pid] = outcome
	}
	return outcome
}

func (plan *nativeSlotProcessPlan) sendSignal(handles *nativeSlotProcessHandles, pid int, signal syscall.Signal) error {
	fd, ok := handles.byPID[pid]
	if !ok {
		return fmt.Errorf("selected native process %d has no identity-bound signal handle", pid)
	}
	outcome := plan.signalOutcome(pid)
	label := "term"
	if signal == syscall.SIGKILL {
		label = "kill"
		outcome.killAttempted = true
	} else {
		outcome.termAttempted = true
	}
	err := nativeSendPidfdSignal(fd, signal)
	result := "sent"
	if errors.Is(err, syscall.ESRCH) {
		result = "already-exited"
		err = nil
	} else if err != nil {
		result = "failed: " + err.Error()
	}
	if signal == syscall.SIGKILL {
		outcome.killOutcome = result
	} else {
		outcome.termOutcome = result
	}
	if err != nil {
		return fmt.Errorf("%s selected native slot process %d through pidfd: %w", label, pid, err)
	}
	return nil
}

func (plan *nativeSlotProcessPlan) Stop() (retErr error) {
	if plan == nil {
		return fmt.Errorf("native slot process plan is required")
	}
	defer func() {
		retErr = plan.withEffectReceipt(retErr)
	}()
	if plan.dryRun {
		for _, pid := range plan.pids {
			fmt.Fprintf(os.Stderr, "+ stop-native-slot-process %d\n", pid)
		}
		return nil
	}
	handles, err := plan.openHandles()
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, handles.close())
	}()
	selected, err := plan.prevalidateSelectedSet()
	if err != nil {
		return err
	}
	for _, pid := range plan.pids {
		if !selected[pid] {
			continue
		}
		matches, err := plan.selectedProcessStillMatches(pid)
		if err != nil {
			return err
		}
		if !matches {
			continue
		}
		if err := plan.sendSignal(handles, pid, syscall.SIGTERM); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(nativeTermWait)
	for time.Now().Before(deadline) {
		alive := false
		for _, pid := range plan.pids {
			alive = alive || nativeSelectedProcessExists(pid)
		}
		if !alive {
			return nil
		}
		time.Sleep(nativeStopPoll)
	}
	killSelected := make(map[int]bool, len(plan.pids))
	for _, pid := range plan.pids {
		if !nativeSelectedProcessExists(pid) {
			continue
		}
		matches, err := plan.selectedProcessStillMatchesAfterTerm(pid)
		if err != nil {
			return err
		}
		killSelected[pid] = matches
	}
	for _, pid := range plan.pids {
		if !killSelected[pid] {
			continue
		}
		matches, err := plan.selectedProcessStillMatchesAfterTerm(pid)
		if err != nil {
			return err
		}
		if !matches {
			continue
		}
		if err := plan.sendSignal(handles, pid, syscall.SIGKILL); err != nil {
			return err
		}
	}
	deadline = time.Now().Add(nativeKillWait)
	for time.Now().Before(deadline) {
		alive := false
		for _, pid := range plan.pids {
			alive = alive || nativeSelectedProcessExists(pid)
		}
		if !alive {
			return nil
		}
		time.Sleep(nativeStopPoll)
	}
	return fmt.Errorf("selected native slot processes did not terminate: %v", plan.pids)
}

type nativeSlotResetStatus struct {
	Command           string                          `json:"command"`
	Runtime           string                          `json:"runtime"`
	Repo              string                          `json:"repo"`
	Index             int                             `json:"index"`
	Status            string                          `json:"status"`
	Head              string                          `json:"head,omitempty"`
	ManifestPath      string                          `json:"manifest_path,omitempty"`
	SelectedProcesses []nativeSlotResetProcessReceipt `json:"selected_processes,omitempty"`
	Capacity          capacity.Summary                `json:"capacity"`
}

type nativeSlotResetProcessReceipt struct {
	PID           int    `json:"pid"`
	StartTicks    string `json:"start_ticks"`
	BootID        string `json:"boot_id,omitempty"`
	Custody       string `json:"custody"`
	Rule          string `json:"rule,omitempty"`
	RootPID       int    `json:"root_pid,omitempty"`
	TermAttempted bool   `json:"term_attempted,omitempty"`
	TermOutcome   string `json:"term_outcome,omitempty"`
	KillAttempted bool   `json:"kill_attempted,omitempty"`
	KillOutcome   string `json:"kill_outcome,omitempty"`
}

func (plan *nativeSlotProcessPlan) receipts() []nativeSlotResetProcessReceipt {
	if plan == nil {
		return nil
	}
	receipts := make([]nativeSlotResetProcessReceipt, 0, len(plan.pids))
	for _, pid := range plan.pids {
		receipt := nativeSlotResetProcessReceipt{
			PID:        pid,
			StartTicks: plan.starts[pid],
			BootID:     plan.bootID,
			Custody:    "native-slot-identity",
		}
		if ruleName, ok := plan.adoptedRoots[pid]; ok {
			receipt.Custody = "source-declared-orphan"
			receipt.Rule = ruleName
		} else if rootPID, ok := plan.adoptedDescendants[pid]; ok {
			receipt.Custody = "source-declared-orphan-descendant"
			receipt.RootPID = rootPID
			receipt.Rule = plan.adoptedRoots[rootPID]
		}
		if outcome := plan.signalOutcomes[pid]; outcome != nil {
			receipt.TermAttempted = outcome.termAttempted
			receipt.TermOutcome = outcome.termOutcome
			receipt.KillAttempted = outcome.killAttempted
			receipt.KillOutcome = outcome.killOutcome
		}
		receipts = append(receipts, receipt)
	}
	return receipts
}

type nativeSlotResetProcessEffectError struct {
	cause    error
	receipts []nativeSlotResetProcessReceipt
	plans    map[*nativeSlotProcessPlan]bool
}

func (err *nativeSlotResetProcessEffectError) Error() string {
	data, marshalErr := json.Marshal(err.receipts)
	if marshalErr != nil {
		return fmt.Sprintf("%v: encode native slot process effect receipt: %v", err.cause, marshalErr)
	}
	return fmt.Sprintf("%v: native_slot_process_effects=%s", err.cause, data)
}

func (err *nativeSlotResetProcessEffectError) Unwrap() error {
	return err.cause
}

func nativeSlotResetEffectHasPlan(cause error, plan *nativeSlotProcessPlan) bool {
	if cause == nil || plan == nil {
		return false
	}
	if effect, ok := cause.(*nativeSlotResetProcessEffectError); ok && effect.plans[plan] {
		return true
	}
	if joined, ok := cause.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if nativeSlotResetEffectHasPlan(child, plan) {
				return true
			}
		}
		return false
	}
	if wrapped, ok := cause.(interface{ Unwrap() error }); ok {
		return nativeSlotResetEffectHasPlan(wrapped.Unwrap(), plan)
	}
	return false
}

func (plan *nativeSlotProcessPlan) withEffectReceipt(cause error) error {
	if plan == nil || cause == nil {
		return cause
	}
	if nativeSlotResetEffectHasPlan(cause, plan) {
		return cause
	}
	hasEffect := false
	for _, outcome := range plan.signalOutcomes {
		if outcome != nil && (outcome.termAttempted || outcome.killAttempted) {
			hasEffect = true
			break
		}
	}
	if !hasEffect {
		return cause
	}
	receipts := plan.receipts()
	plans := map[*nativeSlotProcessPlan]bool{plan: true}
	var existing *nativeSlotResetProcessEffectError
	if errors.As(cause, &existing) {
		receipts = append(append([]nativeSlotResetProcessReceipt(nil), existing.receipts...), receipts...)
		for existingPlan := range existing.plans {
			plans[existingPlan] = true
		}
	}
	return &nativeSlotResetProcessEffectError{cause: cause, receipts: receipts, plans: plans}
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
	probe, err := os.CreateTemp(path, ".devkit-native-reset-write-probe-")
	if err != nil {
		return fmt.Errorf("write-probe %s root %s: %w", root.name, path, err)
	}
	probePath := probe.Name()
	defer func() {
		if err := os.Remove(probePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			retErr = errors.Join(retErr, fmt.Errorf("remove %s write probe %s: %w", root.name, probePath, err))
		}
	}()
	if err := probe.Close(); err != nil {
		return fmt.Errorf("close %s write probe %s: %w", root.name, probePath, err)
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

func printNativeSlotResetStatus(status nativeSlotResetStatus, format string) error {
	if format == "json" {
		data, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	}
	fmt.Fprintf(os.Stdout, "runtime: %s\ncommand: %s\nrepo: %s\nindex: %d\nstatus: %s\n", status.Runtime, status.Command, status.Repo, status.Index, status.Status)
	if status.Head != "" {
		fmt.Fprintf(os.Stdout, "head: %s\n", status.Head)
	}
	if status.ManifestPath != "" {
		fmt.Fprintf(os.Stdout, "manifest: %s\n", status.ManifestPath)
	}
	for _, process := range status.SelectedProcesses {
		fmt.Fprintf(
			os.Stdout,
			"selected_process: pid=%d start_ticks=%s custody=%s rule=%s root_pid=%d boot_id=%s\n",
			process.PID,
			process.StartTicks,
			process.Custody,
			process.Rule,
			process.RootPID,
			process.BootID,
		)
	}
	fmt.Fprintf(os.Stdout, "runtime_ready: %d/%d\nrepo_ready: %d/%d\n", status.Capacity.RuntimeReady, status.Capacity.Total, status.Capacity.RepoReady, status.Capacity.Total)
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
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, ctx.Project)
	if err != nil {
		return err
	}
	orphanRules, err := normalizeNativeSlotOrphanRules(cfg.Native.ResetOrphanProcesses)
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
	expectedManifest, err := nativeplan.BuildManifest(opts, count)
	if err != nil {
		return err
	}
	manifestPath := nativeagent.ManifestPath(expectedManifest.HostStateRoot, ctx.Project)
	manifestExisted, err := validateNativeSlotManifest(manifestPath, expectedManifest)
	if err != nil {
		return err
	}
	protectedRoots := []string{
		ctx.Paths.Root,
		ctx.Paths.RuntimeAuthorityRoot,
		filepath.Join(resolveNativeHostRoot(ctx.Paths.Root, cfg), declaredRepo),
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
		orphanRules:     orphanRules,
	}
	processIdentity.owner, err = discoverNativeSlotOwner(processIdentity)
	if err != nil {
		return err
	}
	processPlan, err := planNativeSlotProcesses(processIdentity, ctx.DryRun)
	if err != nil {
		return err
	}
	defer func() {
		retErr = processPlan.withEffectReceipt(retErr)
	}()
	if err := processPlan.Stop(); err != nil {
		return err
	}
	// Re-scan after shutdown so a process that appeared during the bounded stop
	// window blocks disposal rather than racing the selected path removal.
	if !ctx.DryRun {
		postStop, err := planNativeSlotProcesses(processIdentity, false)
		if err != nil {
			return err
		}
		if len(postStop.pids) != 0 {
			return fmt.Errorf("selected native slot acquired new processes during reset preflight: %v", postStop.pids)
		}
	}
	if err := resetPlan.Apply(); err != nil {
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
			return cleanupProcesses.withEffectReceipt(errors.Join(
				cause,
				processErr,
				fmt.Errorf("plan selected-slot cleanup after failed reconstruction: %w", planErr),
				cleanupCreatedBroker(),
			))
		}
		cleanupErr := cleanupPlan.Apply()
		brokerErr := cleanupCreatedBroker()
		return cleanupProcesses.withEffectReceipt(errors.Join(cause, processErr, cleanupErr, brokerErr))
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
		DevkitRoot:       filepath.Join(resolveNativeHostRoot(ctx.Paths.Root, cfg), "devkit"),
		Repo:             declaredRepo,
		Origin:           strings.TrimSpace(cfg.Defaults.Origin),
		Index:            parsed.index,
		Count:            count,
		BaseBranch:       baseBranch,
		BranchPrefix:     branchPrefix,
		WorktreeRoot:     opts.WorktreeRoot,
		RequireSSHOrigin: declaredRepo == "ouroboros-ide",
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
	return printNativeSlotResetStatus(nativeSlotResetStatus{
		Command: "reset", Runtime: "native", Repo: declaredRepo, Index: parsed.index,
		Status: "ready", Head: head, ManifestPath: manifestPath,
		SelectedProcesses: processPlan.receipts(), Capacity: summary,
	}, parsed.format)
}

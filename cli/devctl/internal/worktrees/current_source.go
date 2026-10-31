package worktrees

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"devkit/cli/devctl/internal/execx"
)

// CurrentSource is an existing GUI consumer, not authority to construct, reset,
// or integrate a writer. The command layer supplies immutable lane geometry.
type CurrentSource struct {
	Worktree, BaseWorktree, Home, Origin string
	Index                                int
	TargetID                             string
	Registration                         *OwnedSourceRegistration
}

type SourcePreparation struct {
	CommonDirectory string `json:"commonDirectory"`
	Previous        string `json:"previous"`
	Acquired        string `json:"acquired"`
	TrackingUpdate  string `json:"trackingUpdate"`
}

// Prepare holds Git's own no-deref ref lock before inspecting symbolic shape.
// An expected old OID alone cannot distinguish a concurrently installed symref
// which resolves to that OID. No mutation is committed until both checks pass.
func commitCurrentSourceTracking(s CurrentSource, requirePackage bool, ssh, previous, acquired string) (outcome string, err error) {
	outcome = "not-committed"
	name, argv, err := NativeSourceGitInvocation(requirePackage, ssh, "--no-replace-objects", "-C", s.Worktree, "update-ref", "--no-deref", "--stdin")
	if err != nil {
		return outcome, err
	}
	limit := nativeMetadataOperationLimit
	if s.Registration != nil {
		limit += SourceRegistrationLookupLimit
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	command := exec.CommandContext(ctx, name, argv...)
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	command.WaitDelay = nativeTerminationGrace
	input, err := command.StdinPipe()
	if err != nil {
		return outcome, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		return outcome, err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return outcome, err
	}
	waited := false
	// EOF aborts an uncommitted explicit Git transaction and releases its lock.
	// The existing metadata deadline terminates a stuck command with TERM before
	// the ordinary grace period; never unlink a lock as caller-side repair.
	defer func() {
		_ = input.Close()
		if !waited {
			_ = command.Wait()
		}
	}()
	reader := bufio.NewReader(output)
	step := func(request, expected string) error {
		if _, err := fmt.Fprint(input, request); err != nil {
			return err
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		if line != expected+"\n" {
			return fmt.Errorf("unexpected Git ref transaction response")
		}
		return nil
	}
	if err := step("start\n", "start: ok"); err != nil {
		return outcome, err
	}
	if err := step("update refs/remotes/origin/main "+acquired+" "+previous+"\nprepare\n", "prepare: ok"); err != nil {
		return outcome, fmt.Errorf("tracking compare-and-swap preparation refused: %w", err)
	}
	probeName, probeArgs, err := NativeSourceGitInvocation(requirePackage, ssh, "--no-replace-objects", "-C", s.Worktree, "symbolic-ref", "-q", "refs/remotes/origin/main")
	if err != nil {
		return outcome, err
	}
	_, probe := execx.Capture(ctx, probeName, probeArgs...)
	if !confirmedDirectTracking(probe, ctx.Err()) {
		return outcome, fmt.Errorf("locked tracking ref is symbolic or shape inspection failed; transaction aborted")
	}
	if s.Registration != nil {
		if err := s.Registration.validate(ctx, s, requirePackage, ssh); err != nil {
			return outcome, fmt.Errorf("locked registered-source binding refused; transaction aborted: %w", err)
		}
	}
	// A lost reply after commit begins has unknown disposition, not proof of
	// no effect. The caller must read actual tracking before another attempt.
	outcome = "unknown"
	if err := step("commit\n", "commit: ok"); err != nil {
		return outcome, err
	}
	_ = input.Close()
	err = command.Wait()
	waited = true
	if err != nil {
		return outcome, err
	}
	return "committed", nil
}

var sourceCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

func confirmedDirectTracking(probe execx.Result, contextErr error) bool {
	exit, executed := probe.Err.(*exec.ExitError)
	return contextErr == nil && executed && exit.ExitCode() == 1 && probe.Code == 1
}

func currentSourceGit(requirePackage bool, ssh, cwd string, args ...string) (string, error) {
	return currentSourceGitContext(context.Background(), requirePackage, ssh, cwd, args...)
}

func currentSourceGitContext(parent context.Context, requirePackage bool, ssh, cwd string, args ...string) (string, error) {
	output, err := currentSourceGitRawContext(parent, requirePackage, ssh, cwd, args...)
	return strings.TrimSpace(output), err
}

func currentSourceGitRawContext(parent context.Context, requirePackage bool, ssh, cwd string, args ...string) (string, error) {
	name, argv, err := NativeSourceGitInvocation(requirePackage, ssh, append([]string{"--no-replace-objects", "-C", cwd}, args...)...)
	if err != nil {
		return "", err
	}
	output, result := execx.Capture(parent, name, argv...)
	if result.Code != 0 {
		return "", fmt.Errorf("env %v: exit %d", argv, result.Code)
	}
	return output, nil
}

var currentSourceGitHubDeclaration = regexp.MustCompile(`^ssh://git@ssh\.github\.com:443/([A-Za-z0-9-]{1,255})/ouroboros-ide\.git$`)

func currentSourceOriginMatches(observed, declared string) bool {
	if observed == declared {
		return true
	}
	// Only current-source admission gains this historical representation. The
	// immutable declaration still selects acquisition; lane markers, bootstrap
	// and reuse retain their exact-origin contracts. No caller selects an alias.
	match := currentSourceGitHubDeclaration.FindStringSubmatch(declared)
	return match != nil && observed == "git@github.com:"+match[1]+"/ouroboros-ide.git"
}

func admitCurrentSource(s CurrentSource, requirePackage bool, ssh string) (string, error) {
	for _, path := range []string{s.Worktree, s.BaseWorktree, s.Home} {
		actual, err := filepath.EvalSymlinks(path)
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || err != nil || actual != path {
			return "", fmt.Errorf("source preparation requires existing canonical GUI paths")
		}
	}
	if s.Index < 1 || strings.TrimSpace(s.Origin) == "" {
		return "", fmt.Errorf("source preparation lacks declared lane/origin")
	}
	git := func(args ...string) (string, error) {
		return currentSourceGit(requirePackage, ssh, s.Worktree, args...)
	}
	top, err := git("rev-parse", "--show-toplevel")
	if err != nil || top != s.Worktree {
		return "", fmt.Errorf("source preparation requires the exact writer root")
	}
	common, err := git("rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	actual, err := filepath.EvalSymlinks(common)
	if err != nil || actual != common {
		return "", fmt.Errorf("source preparation rejects redirected common metadata")
	}
	if s.Registration != nil {
		if err := s.Registration.validate(context.Background(), s, requirePackage, ssh); err != nil {
			return "", err
		}
	}
	if common == filepath.Join(s.Worktree, ".git") {
		// A topology child can be a standalone clone. Admit only the fixed GUI
		// child geometry, not an arbitrary repository under the mounted workspace.
		parent := filepath.Dir(s.Worktree)
		if s.Registration == nil && (filepath.Base(s.Worktree) != "ouroboros-ide" || filepath.Dir(parent) != filepath.Join(s.Home, ".codex", "worktrees")) {
			return "", fmt.Errorf("standalone source preparation requires a GUI child root")
		}
		entries, err := git("worktree", "list", "--porcelain", "-z")
		if err != nil {
			return "", err
		}
		count := 0
		for _, field := range strings.Split(entries, "\x00") {
			if strings.HasPrefix(field, "worktree ") {
				count++
				if field != "worktree "+s.Worktree {
					return "", fmt.Errorf("standalone source has another worktree")
				}
			}
		}
		if count != 1 {
			return "", fmt.Errorf("standalone source lacks its single registration")
		}
	} else {
		root := filepath.Dir(filepath.Dir(s.BaseWorktree))
		expected := filepath.Join(root, ".devkit", "git", fmt.Sprintf("agent%d", s.Index), "ouroboros-ide.git")
		if common != expected {
			return "", fmt.Errorf("source preparation rejects another lane's common repository")
		}
		marker, err := os.ReadFile(filepath.Join(common, "devkit-owned-common"))
		wanted, markerErr := nativeOwnedCommonRepositoryMarker("ouroboros-ide", s.Origin, s.Index)
		if err != nil || markerErr != nil || string(marker) != wanted {
			return "", fmt.Errorf("source preparation requires the lane's declared ownership marker")
		}
		baseCommon, err := currentSourceGit(requirePackage, ssh, s.BaseWorktree, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil || baseCommon != common {
			return "", fmt.Errorf("writer does not share the declared GUI lane repository")
		}
		entries, err := currentSourceGit(requirePackage, ssh, s.BaseWorktree, "worktree", "list", "--porcelain", "-z")
		if err != nil {
			return "", err
		}
		found := false
		for _, field := range strings.Split(entries, "\x00") {
			found = found || field == "worktree "+s.Worktree
		}
		if !found {
			return "", fmt.Errorf("writer is not registered in the declared GUI lane")
		}
		rel, err := filepath.Rel(s.BaseWorktree, s.Worktree)
		if err != nil || (s.Registration == nil && (rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
			return "", fmt.Errorf("writer is outside its GUI lane")
		}
	}
	raw, err := currentSourceGitRawContext(context.Background(), requirePackage, ssh, s.Worktree, "config", "--null", "--get-all", "remote.origin.url")
	origins := strings.Split(raw, "\x00")
	if err != nil || len(origins) != 2 || origins[0] == "" || origins[1] != "" || strings.ContainsAny(origins[0], "\r\n") {
		return "", fmt.Errorf("source preparation requires one raw origin")
	}
	origin, err := git("remote", "get-url", "origin")
	if err != nil || origin != origins[0] || !currentSourceOriginMatches(origin, s.Origin) {
		return "", fmt.Errorf("source origin differs from immutable declaration")
	}
	if rewrites, _ := git("config", "--name-only", "--get-regexp", `^url\..*\.(insteadof|pushinsteadof)$`); rewrites != "" {
		return "", fmt.Errorf("source preparation refuses repository URL rewrites")
	}
	grafts, err := git("rev-parse", "--path-format=absolute", "--git-path", "info/grafts")
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(grafts); !os.IsNotExist(err) {
		return "", fmt.Errorf("source preparation refuses legacy graft metadata")
	}
	// Ref updates must not dispatch repository-supplied code. Do not change a
	// caller's configuration to make it acceptable.
	if hooks, _ := git("config", "--get", "core.hooksPath"); hooks != "" {
		return "", fmt.Errorf("source preparation refuses a custom hooks path")
	}
	if _, err := os.Lstat(filepath.Join(common, "hooks", "reference-transaction")); !os.IsNotExist(err) {
		return "", fmt.Errorf("source preparation refuses a reference-transaction hook")
	}
	return common, nil
}

// PrepareCurrentSource acquires objects without updating any fetched ref, checks
// ancestry explicitly, then uses Git's compare-and-swap ref transaction. Git's
// own ref lock coordinates this shared metadata with other worktrees/callers.
// Neither fixed fetch flags nor a successful preparation grants Product approval.
func PrepareCurrentSource(s CurrentSource, requirePackage bool, ssh string, dryRun bool) (SourcePreparation, error) {
	result := SourcePreparation{TrackingUpdate: "not-started"}
	common, err := admitCurrentSource(s, requirePackage, ssh)
	if err != nil {
		return result, err
	}
	result.CommonDirectory = common
	git := func(args ...string) (string, error) {
		return currentSourceGit(requirePackage, ssh, s.Worktree, args...)
	}
	if target, _ := git("symbolic-ref", "-q", "refs/remotes/origin/main"); target != "" {
		return result, fmt.Errorf("source tracking ref must not be symbolic")
	}
	result.Previous, err = git("rev-parse", "--verify", "refs/remotes/origin/main")
	if err != nil || !sourceCommit.MatchString(result.Previous) {
		return result, fmt.Errorf("source preparation requires an existing tracking commit")
	}
	if kind, err := git("cat-file", "-t", result.Previous); err != nil || kind != "commit" {
		return result, fmt.Errorf("source tracking ref must name a commit")
	}
	if dryRun {
		return result, nil
	}
	remote, err := git("ls-remote", "--exit-code", s.Origin, "refs/heads/main")
	if err != nil {
		return result, err
	}
	fields := strings.Fields(remote)
	if len(fields) != 2 || fields[1] != "refs/heads/main" || !sourceCommit.MatchString(fields[0]) {
		return result, fmt.Errorf("remote main identity is malformed")
	}
	result.Acquired = fields[0]
	if err := RunNativeSourceGitFetch(requirePackage, ssh, "--no-replace-objects", "-C", s.Worktree, "fetch", "--no-tags", "--no-prune", "--no-prune-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--no-auto-maintenance", "--refmap=", s.Origin, result.Acquired); err != nil {
		return result, err
	}
	if _, err := git("cat-file", "-e", result.Acquired+"^{commit}"); err != nil {
		return result, err
	}
	if _, err := git("merge-base", "--is-ancestor", result.Previous, result.Acquired); err != nil {
		return result, fmt.Errorf("remote main does not fast-forward the acquired tracking ref; tracking preserved: %w", err)
	}
	result.TrackingUpdate, err = commitCurrentSourceTracking(s, requirePackage, ssh, result.Previous, result.Acquired)
	if err != nil {
		return result, fmt.Errorf("tracking transaction outcome %s; read actual tracking before retry: %w", result.TrackingUpdate, err)
	}
	return result, nil
}

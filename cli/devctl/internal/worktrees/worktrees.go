package worktrees

import (
	"context"
	"devkit/cli/devctl/internal/execx"
	"devkit/cli/devctl/internal/paths"
	runtimeagent "devkit/cli/devctl/internal/runtime/agent"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	nativeMetadataOperationLimit = 10 * time.Second
	// Fetch is bounded by observable protocol progress rather than total
	// transfer duration. This permits a healthy full pack to outlive the short
	// metadata-operation limit while failing closed when Git/OpenSSH and its
	// package-owned ProxyCommand make no progress for a complete idle window.
	nativeFetchIdleLimit   = 2 * time.Minute
	nativeTerminationGrace = 2 * time.Second
)

func runWithPolicy(dry bool, fixedLimit, idleLimit time.Duration, name string, args ...string) error {
	if dry {
		fmt.Fprintf(os.Stderr, "+ %s\n", strings.Join(append([]string{name}, args...), " "))
		return nil
	}
	ctx := context.Background()
	cancel := func() {}
	if fixedLimit > 0 {
		ctx, cancel = context.WithTimeout(ctx, fixedLimit)
	}
	defer cancel()
	res := execx.RunManaged(ctx, execx.ManagedPolicy{
		IdleTimeout:      idleLimit,
		TerminationGrace: nativeTerminationGrace,
	}, name, args...)
	if res.Code != 0 {
		if res.Err != nil {
			return fmt.Errorf("%s %v: exit %d: %w", name, args, res.Code, res.Err)
		}
		return fmt.Errorf("%s %v: exit %d", name, args, res.Code)
	}
	return nil
}

// run preserves the short fixed wall-clock policy for local metadata commands.
func run(dry bool, name string, args ...string) error {
	return runWithPolicy(dry, nativeMetadataOperationLimit, 0, name, args...)
}

// runFetch uses an idle/progress deadline for a full remote smart-protocol
// transfer. Its process group includes Git, OpenSSH, and the package-owned
// ProxyCommand, so cancellation cannot leave a transport descendant behind.
func runFetch(dry bool, name string, args ...string) error {
	return runWithPolicy(dry, 0, nativeFetchIdleLimit, name, args...)
}

// rewriteGitdir writes a .git file pointing to a gitdir form suitable for the
// runtime that will mount the worktree.
func rewriteGitdir(wt string, relative bool) {
	if info, err := os.Stat(filepath.Join(wt, ".git")); err == nil && info.IsDir() {
		return
	}
	out, res := execx.Capture(context.Background(), "git", "-C", wt, "rev-parse", "--git-dir")
	if res.Code != 0 {
		return
	}
	gitdir := strings.TrimSpace(out)
	if gitdir == "" {
		return
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Clean(filepath.Join(wt, gitdir))
	}
	if relative {
		rel, err := filepath.Rel(wt, gitdir)
		if err != nil {
			return
		}
		gitdir = rel
	}
	_ = os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gitdir+"\n"), 0644)
}

// rewriteNativeGitdir makes package-owned linked-worktree metadata portable.
// Ownership is established by one common repository and all of its worktrees
// living beneath the configured worktree root. That topology retains identical
// relative relationships when the root is projected at an unrelated sandbox
// path. A common repository outside the root is a foreign authority and is
// rejected rather than rewritten or exposed through a host alias.
func rewriteNativeGitdir(wt, worktreesRoot, repoCommonDir string) error {
	gitFile := filepath.Join(wt, ".git")
	info, err := os.Lstat(gitFile)
	if err != nil {
		return fmt.Errorf("inspect native worktree gitdir %s: %w", gitFile, err)
	}
	if info.IsDir() {
		return fmt.Errorf("native worktree %s is a standalone checkout, not a package-owned linked worktree", canonicalOrClean(wt))
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("native worktree gitdir %s must be a regular file", gitFile)
	}

	canonicalWorktree, err := canonicalExistingPath(wt)
	if err != nil {
		return fmt.Errorf("canonicalize native worktree %s: %w", wt, err)
	}
	canonicalWorktreesRoot, err := canonicalExistingPath(worktreesRoot)
	if err != nil {
		return fmt.Errorf("canonicalize native worktree root %s: %w", worktreesRoot, err)
	}
	if !pathWithinRoot(canonicalWorktreesRoot, canonicalWorktree) {
		return fmt.Errorf("native worktree %s is outside package-owned worktree root %s", canonicalWorktree, canonicalWorktreesRoot)
	}

	gitdirValue, err := readGitdirPointer(gitFile)
	if err != nil {
		return err
	}
	gitdir, err := canonicalMetadataPath(wt, gitdirValue)
	if err != nil {
		return fmt.Errorf("canonicalize native worktree gitdir %s: %w", gitFile, err)
	}
	commonDirValue, err := readPlainMetadataPath(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return err
	}
	commonDir, err := canonicalMetadataPath(gitdir, commonDirValue)
	if err != nil {
		return fmt.Errorf("canonicalize native worktree commondir %s: %w", gitdir, err)
	}
	canonicalRepoCommonDir, err := canonicalExistingPath(repoCommonDir)
	if err != nil {
		return fmt.Errorf("canonicalize source repository common Git directory %s: %w", repoCommonDir, err)
	}
	if commonDir != canonicalRepoCommonDir {
		return fmt.Errorf("native worktree %s commondir %s is not the package-owned common Git directory %s", canonicalWorktree, commonDir, canonicalRepoCommonDir)
	}
	if !pathWithinRoot(canonicalWorktreesRoot, canonicalRepoCommonDir) {
		return fmt.Errorf("package-owned common Git directory %s is outside worktree root %s", canonicalRepoCommonDir, canonicalWorktreesRoot)
	}
	ownedGitdirsRoot := filepath.Join(canonicalRepoCommonDir, "worktrees")
	if gitdir == ownedGitdirsRoot || !pathWithinRoot(ownedGitdirsRoot, gitdir) {
		return fmt.Errorf("native worktree %s gitdir %s is outside package-owned Git worktrees %s", canonicalWorktree, gitdir, ownedGitdirsRoot)
	}

	reverseGitFile := filepath.Join(gitdir, "gitdir")
	reverseGitdirValue, err := readPlainMetadataPath(reverseGitFile)
	if err != nil {
		return err
	}
	reverseGitdir, err := canonicalMetadataPath(gitdir, reverseGitdirValue)
	if err != nil {
		return fmt.Errorf("canonicalize native reverse gitdir %s: %w", reverseGitFile, err)
	}
	canonicalGitFile, err := canonicalExistingPath(gitFile)
	if err != nil {
		return fmt.Errorf("canonicalize native worktree .git file %s: %w", gitFile, err)
	}
	if reverseGitdir != canonicalGitFile {
		return fmt.Errorf("native worktree %s reverse gitdir %s does not resolve to %s", canonicalWorktree, reverseGitdir, canonicalGitFile)
	}
	relativeGitdir, err := filepath.Rel(canonicalWorktree, gitdir)
	if err != nil {
		return fmt.Errorf("make native worktree gitdir relative for %s: %w", canonicalWorktree, err)
	}
	relativeCommonDir, err := filepath.Rel(gitdir, canonicalRepoCommonDir)
	if err != nil {
		return fmt.Errorf("make native worktree commondir relative for %s: %w", canonicalWorktree, err)
	}
	relativeReverseGitdir, err := filepath.Rel(gitdir, canonicalGitFile)
	if err != nil {
		return fmt.Errorf("make native reverse gitdir relative for %s: %w", canonicalWorktree, err)
	}
	if err := os.WriteFile(filepath.Join(gitdir, "commondir"), []byte(relativeCommonDir+"\n"), 0o644); err != nil {
		return fmt.Errorf("write native worktree commondir %s: %w", gitdir, err)
	}
	if err := os.WriteFile(reverseGitFile, []byte(relativeReverseGitdir+"\n"), 0o644); err != nil {
		return fmt.Errorf("write native reverse gitdir %s: %w", reverseGitFile, err)
	}
	if err := os.WriteFile(gitFile, []byte("gitdir: "+relativeGitdir+"\n"), 0o644); err != nil {
		return fmt.Errorf("write native worktree gitdir %s: %w", gitFile, err)
	}
	return nil
}

func canonicalOrClean(path string) string {
	if canonical, err := canonicalExistingPath(path); err == nil {
		return canonical
	}
	return filepath.Clean(path)
}

func readGitdirPointer(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read native worktree gitdir %s: %w", path, err)
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(strings.ToLower(line), "gitdir:") {
		return "", fmt.Errorf("native worktree gitdir %s is malformed", path)
	}
	value := strings.TrimSpace(line[len("gitdir:"):])
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("native worktree gitdir %s has an invalid path", path)
	}
	return value, nil
}

func readPlainMetadataPath(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read native Git metadata path %s: %w", path, err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("native Git metadata path %s is invalid", path)
	}
	return value, nil
}

func canonicalMetadataPath(base, value string) (string, error) {
	path := filepath.Clean(value)
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	return canonicalExistingPath(path)
}

func canonicalExistingPath(path string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(strings.TrimSpace(path)))
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absolute)
}

func pathWithinRoot(root, candidate string) bool {
	root = filepath.Clean(strings.TrimSpace(root))
	candidate = filepath.Clean(strings.TrimSpace(candidate))
	if root == "." || candidate == "." {
		return false
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// cleanWorktreePath removes the target directory when it is clearly stale:
// missing .git metadata, pointing to a different repository, or referencing a
// gitdir that no longer exists. We keep the directory when it still looks like
// a valid worktree for the given repo.
func cleanWorktreePath(repoWorktreesDir, wt string, rejectForeign bool) error {
	info, err := os.Stat(wt)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return os.RemoveAll(wt)
	}
	gitFile := filepath.Join(wt, ".git")
	data, err := os.ReadFile(gitFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.RemoveAll(wt)
		}
		return err
	}
	content := strings.TrimSpace(string(data))
	if !strings.HasPrefix(content, "gitdir:") {
		return os.RemoveAll(wt)
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(content, "gitdir:"))
	if gitdir == "" {
		return os.RemoveAll(wt)
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Clean(filepath.Join(wt, gitdir))
	}
	resolved, err := filepath.EvalSymlinks(gitdir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.RemoveAll(wt)
		}
		return err
	}
	if repoWorktreesDir != "" {
		rel, err := filepath.Rel(repoWorktreesDir, resolved)
		if err != nil || strings.HasPrefix(rel, "..") {
			if rejectForeign {
				return fmt.Errorf("native worktree %s points to foreign Git metadata %s outside %s", wt, resolved, repoWorktreesDir)
			}
			return os.RemoveAll(wt)
		}
	}
	if _, err := os.Stat(resolved); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.RemoveAll(wt)
		}
		return err
	}
	return nil
}

func existingGitCheckout(wt string) (bool, error) {
	info, err := os.Stat(wt)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return false, nil
	}
	out, res := execx.Capture(context.Background(), "git", "-C", wt, "rev-parse", "--show-toplevel")
	if res.Code != 0 {
		return false, nil
	}
	top := filepath.Clean(strings.TrimSpace(out))
	if top == "" {
		return false, nil
	}
	if resolvedTop, err := filepath.EvalSymlinks(top); err == nil {
		top = resolvedTop
	}
	resolvedWT := filepath.Clean(wt)
	if r, err := filepath.EvalSymlinks(resolvedWT); err == nil {
		resolvedWT = r
	}
	return top == resolvedWT, nil
}

func verifyFreshNativeWorktree(wt, baseRef string) error {
	ok, err := existingGitCheckout(wt)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("fresh native worktree %s is not a Git checkout", wt)
	}
	head, result := execx.Capture(context.Background(), "git", "-C", wt, "rev-parse", "HEAD")
	if result.Code != 0 {
		return fmt.Errorf("read fresh native worktree HEAD %s: exit %d", wt, result.Code)
	}
	base, result := execx.Capture(context.Background(), "git", "-C", wt, "rev-parse", baseRef)
	if result.Code != 0 {
		return fmt.Errorf("read fresh native worktree base %s at %s: exit %d", baseRef, wt, result.Code)
	}
	if strings.TrimSpace(head) != strings.TrimSpace(base) {
		return fmt.Errorf("fresh native worktree %s HEAD %s does not match %s %s", wt, strings.TrimSpace(head), baseRef, strings.TrimSpace(base))
	}
	status, result := execx.Capture(context.Background(), "git", "-C", wt, "status", "--porcelain=v1")
	if result.Code != 0 {
		return fmt.Errorf("read fresh native worktree status %s: exit %d", wt, result.Code)
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("fresh native worktree %s is dirty", wt)
	}
	return nil
}

// Setup ensures worktrees and branches exist for agents 1..n.
// devkitRoot: path to devkit/ root (we derive dev root as parent dir).
// repo: primary repo folder name under dev root.
// n: number of agents
// baseBranch: e.g., "main"
// branchPrefix: e.g., "agent"
// dry: log only without changing state
func Setup(devkitRoot, repo string, n int, baseBranch, branchPrefix string, dry bool) error {
	devRoot := filepath.Clean(filepath.Join(devkitRoot, ".."))
	repoPath := filepath.Join(devRoot, repo)
	// Legacy local worktree setup must not invent an SSH executable or config.
	// Promoted SSH bootstrap uses SetupNative with the package-owned command.
	envGit := func(args ...string) []string {
		return append([]string{"-u", "GIT_SSH_COMMAND", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "git"}, args...)
	}

	var repoWorktreesDir string
	if !dry {
		if out, res := execx.Capture(context.Background(), "git", "-C", repoPath, "rev-parse", "--git-dir"); res.Code == 0 {
			gitdir := strings.TrimSpace(out)
			if gitdir != "" {
				if !filepath.IsAbs(gitdir) {
					gitdir = filepath.Clean(filepath.Join(repoPath, gitdir))
				}
				if resolved, err := filepath.EvalSymlinks(gitdir); err == nil {
					gitdir = resolved
				}
				repoWorktreesDir = filepath.Join(gitdir, "worktrees")
				if resolved, err := filepath.EvalSymlinks(repoWorktreesDir); err == nil {
					repoWorktreesDir = resolved
				} else {
					repoWorktreesDir = filepath.Clean(repoWorktreesDir)
				}
			}
		}
	}

	if err := runFetch(dry, "env", envGit("-C", repoPath, "fetch", "--all", "--prune", "--progress")...); err != nil {
		return err
	}
	if err := run(dry, "env", envGit("-C", repoPath, "config", "push.default", "upstream")...); err != nil {
		return err
	}
	if err := run(dry, "env", envGit("-C", repoPath, "config", "worktree.useRelativePaths", "false")...); err != nil {
		return err
	}
	// agent1 uses primary path
	b1 := fmt.Sprintf("%s1", branchPrefix)
	if err := run(dry, "env", envGit("-C", repoPath, "checkout", "-B", b1)...); err != nil {
		return err
	}
	if err := run(dry, "env", envGit("-C", repoPath, "branch", "--set-upstream-to=origin/"+baseBranch, b1)...); err != nil {
		return err
	}

	worktreesRoot := filepath.Join(devRoot, paths.AgentWorktreesDir)
	if !dry {
		_ = os.MkdirAll(worktreesRoot, 0755)
	}

	for i := 2; i <= n; i++ {
		parent := filepath.Join(worktreesRoot, fmt.Sprintf("%s%d", branchPrefix, i))
		if !dry {
			_ = os.MkdirAll(parent, 0755)
		}
		wt := filepath.Join(parent, repo)
		bi := fmt.Sprintf("%s%d", branchPrefix, i)
		if !dry {
			if err := cleanWorktreePath(repoWorktreesDir, wt, false); err != nil {
				return err
			}
		}
		_ = run(dry, "env", envGit("-C", repoPath, "worktree", "prune")...)            // best effort
		_ = run(dry, "env", envGit("-C", repoPath, "worktree", "remove", "-f", wt)...) // best effort
		if err := run(dry, "env", envGit("-C", repoPath, "worktree", "add", wt, "-B", bi, "origin/"+baseBranch)...); err != nil {
			if dry {
				return err
			}
			if remErr := os.RemoveAll(wt); remErr != nil && !errors.Is(remErr, os.ErrNotExist) {
				return fmt.Errorf("remove stale worktree %s: %w", wt, remErr)
			}
			if err2 := run(dry, "env", envGit("-C", repoPath, "worktree", "add", wt, "-B", bi, "origin/"+baseBranch)...); err2 != nil {
				return err2
			}
		}
		if !dry {
			rewriteGitdir(wt, true)
		}
		if err := run(dry, "env", envGit("-C", wt, "branch", "--set-upstream-to=origin/"+baseBranch, bi)...); err != nil {
			return err
		}
	}
	return nil
}

const nativeOwnedCommonRepositorySchema = "devkit/native-owned-common-repository/v1"

func nativeOwnedCommonRepositoryPath(worktreesRoot, repo string) (string, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" || repo == "." || filepath.IsAbs(repo) || filepath.Clean(repo) != repo || filepath.Base(repo) != repo {
		return "", fmt.Errorf("native repository name %q cannot select a package-owned common repository path", repo)
	}
	root, err := filepath.Abs(filepath.Clean(strings.TrimSpace(worktreesRoot)))
	if err != nil {
		return "", fmt.Errorf("resolve native worktree root %s: %w", worktreesRoot, err)
	}
	return filepath.Join(root, ".devkit", "git", repo+".git"), nil
}

func nativeOwnedCommonRepositoryMarker(repo, remoteURL string) (string, error) {
	for label, value := range map[string]string{"repository": repo, "origin": remoteURL} {
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n\x00") {
			return "", fmt.Errorf("native %s identity is invalid", label)
		}
	}
	return fmt.Sprintf(
		"schema=%s\nrepository=%s\norigin=%s\n",
		nativeOwnedCommonRepositorySchema,
		repo,
		remoteURL,
	), nil
}

func captureNativeGit(args []string) (string, error) {
	out, result := execx.Capture(context.Background(), "env", args...)
	if result.Code != 0 {
		return "", fmt.Errorf("env %v: exit %d", args, result.Code)
	}
	return strings.TrimSpace(out), nil
}

func validateNativeOwnedCommonRepository(
	worktreesRoot, commonDir, repo, remoteURL string,
	envLocalGit func(...string) []string,
) error {
	info, err := os.Lstat(commonDir)
	if err != nil {
		return fmt.Errorf("inspect package-owned common repository %s: %w", commonDir, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("package-owned common repository %s must be a real directory", commonDir)
	}
	canonicalRoot, err := canonicalExistingPath(worktreesRoot)
	if err != nil {
		return fmt.Errorf("canonicalize native worktree root %s: %w", worktreesRoot, err)
	}
	canonicalCommon, err := canonicalExistingPath(commonDir)
	if err != nil {
		return fmt.Errorf("canonicalize package-owned common repository %s: %w", commonDir, err)
	}
	if !pathWithinRoot(canonicalRoot, canonicalCommon) {
		return fmt.Errorf("package-owned common repository %s escapes worktree root %s", canonicalCommon, canonicalRoot)
	}
	expectedMarker, err := nativeOwnedCommonRepositoryMarker(repo, remoteURL)
	if err != nil {
		return err
	}
	markerPath := filepath.Join(commonDir, "devkit-owned-common")
	marker, err := os.ReadFile(markerPath)
	if err != nil {
		return fmt.Errorf("read package-owned common repository marker %s: %w", markerPath, err)
	}
	if string(marker) != expectedMarker {
		return fmt.Errorf("package-owned common repository %s identity does not match repository %s and its declared origin", commonDir, repo)
	}
	bare, err := captureNativeGit(envLocalGit("--git-dir", commonDir, "rev-parse", "--is-bare-repository"))
	if err != nil || bare != "true" {
		return fmt.Errorf("package-owned common repository %s is not a bare Git repository", commonDir)
	}
	origin, err := captureNativeGit(envLocalGit("--git-dir", commonDir, "remote", "get-url", "origin"))
	if err != nil {
		return fmt.Errorf("read package-owned common repository origin %s: %w", commonDir, err)
	}
	if origin != remoteURL {
		return fmt.Errorf("package-owned common repository %s origin %q does not match declared origin %q", commonDir, origin, remoteURL)
	}
	return nil
}

func ensureNativeOwnedCommonRepository(
	worktreesRoot, repo, remoteURL string,
	envLocalGit func(...string) []string,
	envRemoteGit func(...string) []string,
	dryRun bool,
) (string, error) {
	commonDir, err := nativeOwnedCommonRepositoryPath(worktreesRoot, repo)
	if err != nil {
		return "", err
	}
	marker, err := nativeOwnedCommonRepositoryMarker(repo, remoteURL)
	if err != nil {
		return "", err
	}
	if dryRun {
		if err := run(true, "env", envLocalGit("init", "--bare", "--initial-branch=main", commonDir)...); err != nil {
			return "", err
		}
		if err := run(true, "env", envLocalGit("--git-dir", commonDir, "remote", "add", "origin", remoteURL)...); err != nil {
			return "", err
		}
		if err := runFetch(true, "env", envRemoteGit("--git-dir", commonDir, "fetch", "--all", "--prune", "--progress")...); err != nil {
			return "", err
		}
		return commonDir, nil
	}

	if err := os.MkdirAll(worktreesRoot, 0o755); err != nil {
		return "", fmt.Errorf("create native worktree root %s: %w", worktreesRoot, err)
	}
	if info, statErr := os.Lstat(commonDir); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("package-owned common repository %s must be a real directory", commonDir)
		}
		if err := validateNativeOwnedCommonRepository(worktreesRoot, commonDir, repo, remoteURL, envLocalGit); err != nil {
			return "", err
		}
		if err := runFetch(false, "env", envRemoteGit("--git-dir", commonDir, "fetch", "--all", "--prune", "--progress")...); err != nil {
			return "", err
		}
		return commonDir, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("inspect package-owned common repository %s: %w", commonDir, statErr)
	}

	parent := filepath.Dir(commonDir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("create package-owned common repository parent %s: %w", parent, err)
	}
	staging, err := os.MkdirTemp(parent, "."+repo+".bootstrap-")
	if err != nil {
		return "", fmt.Errorf("create package-owned common repository staging directory: %w", err)
	}
	keepStaging := false
	defer func() {
		if !keepStaging {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := run(false, "env", envLocalGit("init", "--bare", "--initial-branch=main", staging)...); err != nil {
		return "", err
	}
	if err := run(false, "env", envLocalGit("--git-dir", staging, "config", "worktree.useRelativePaths", "true")...); err != nil {
		return "", err
	}
	if err := run(false, "env", envLocalGit("--git-dir", staging, "remote", "add", "origin", remoteURL)...); err != nil {
		return "", err
	}
	if err := runFetch(false, "env", envRemoteGit("--git-dir", staging, "fetch", "--all", "--prune", "--progress")...); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(staging, "devkit-owned-common"), []byte(marker), 0o600); err != nil {
		return "", fmt.Errorf("write package-owned common repository marker: %w", err)
	}
	if err := validateNativeOwnedCommonRepository(worktreesRoot, staging, repo, remoteURL, envLocalGit); err != nil {
		return "", err
	}
	if err := os.Rename(staging, commonDir); err != nil {
		return "", fmt.Errorf("publish package-owned common repository %s: %w", commonDir, err)
	}
	keepStaging = true
	return commonDir, nil
}

func preflightNativeOwnedWorktreeTargets(worktreesRoot, commonDir, repo string, count int) error {
	for i := 1; i <= count; i++ {
		worktree := filepath.Join(worktreesRoot, fmt.Sprintf("agent%d", i), repo)
		info, err := os.Lstat(worktree)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect native worktree target %s: %w", worktree, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("native worktree target %s must be absent or a package-owned linked worktree", worktree)
		}
		gitFile := filepath.Join(worktree, ".git")
		gitInfo, err := os.Lstat(gitFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				entries, readErr := os.ReadDir(worktree)
				if readErr != nil {
					return fmt.Errorf("inspect native worktree target %s: %w", worktree, readErr)
				}
				if len(entries) == 0 {
					continue
				}
				allowedHome := fmt.Sprintf(".devhome-agent%d", i)
				if len(entries) == 1 && entries[0].Name() == allowedHome && entries[0].IsDir() && entries[0].Type()&os.ModeSymlink == 0 {
					continue
				}
			}
			return fmt.Errorf("native worktree target %s contains partial or stale state without linked Git metadata: %w", worktree, err)
		}
		if gitInfo.IsDir() {
			return fmt.Errorf("native worktree %s is a standalone checkout, not a package-owned linked worktree", canonicalOrClean(worktree))
		}
		gitdirValue, err := readGitdirPointer(gitFile)
		if err != nil {
			return err
		}
		gitdir := filepath.Clean(gitdirValue)
		if !filepath.IsAbs(gitdir) {
			gitdir = filepath.Join(worktree, gitdir)
		}
		gitdir, err = filepath.Abs(gitdir)
		if err != nil {
			return fmt.Errorf("resolve native worktree target gitdir %s: %w", gitFile, err)
		}
		ownedGitdirsRoot, err := filepath.Abs(filepath.Join(commonDir, "worktrees"))
		if err != nil {
			return fmt.Errorf("resolve package-owned Git worktrees root %s: %w", commonDir, err)
		}
		if gitdir == ownedGitdirsRoot || !pathWithinRoot(ownedGitdirsRoot, gitdir) {
			return fmt.Errorf("native worktree target %s points to foreign Git metadata %s outside %s", worktree, gitdir, ownedGitdirsRoot)
		}
	}
	return nil
}

func stageNativeWorktreePayload(worktree, allowedHome string) (string, error) {
	entries, err := os.ReadDir(worktree)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect native worktree target %s before materialization: %w", worktree, err)
	}
	if len(entries) == 0 {
		if err := os.Remove(worktree); err != nil {
			return "", fmt.Errorf("remove empty native worktree target %s: %w", worktree, err)
		}
		return "", nil
	}
	if len(entries) != 1 || entries[0].Name() != allowedHome || !entries[0].IsDir() || entries[0].Type()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("native worktree target %s became non-empty before materialization", worktree)
	}
	stagingDir, err := os.MkdirTemp(filepath.Dir(worktree), ".devkit-native-payload-")
	if err != nil {
		return "", fmt.Errorf("create native worktree payload staging directory: %w", err)
	}
	stagedHome := filepath.Join(stagingDir, allowedHome)
	if err := os.Rename(filepath.Join(worktree, allowedHome), stagedHome); err != nil {
		_ = os.RemoveAll(stagingDir)
		return "", fmt.Errorf("stage native worktree payload %s: %w", allowedHome, err)
	}
	if err := os.Remove(worktree); err != nil {
		_ = os.Rename(stagedHome, filepath.Join(worktree, allowedHome))
		_ = os.RemoveAll(stagingDir)
		return "", fmt.Errorf("remove staged native worktree target %s: %w", worktree, err)
	}
	return stagingDir, nil
}

func restoreNativeWorktreePayload(worktree, allowedHome, stagingDir string) error {
	if stagingDir == "" {
		return nil
	}
	stagedHome := filepath.Join(stagingDir, allowedHome)
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		return fmt.Errorf("restore native worktree payload root %s: %w", worktree, err)
	}
	if err := os.Rename(stagedHome, filepath.Join(worktree, allowedHome)); err != nil {
		return fmt.Errorf("restore native worktree payload %s: %w", allowedHome, err)
	}
	if err := os.Remove(stagingDir); err != nil {
		return fmt.Errorf("remove native worktree payload staging directory %s: %w", stagingDir, err)
	}
	return nil
}

type NativeOptions struct {
	DevkitRoot       string
	Repo             string
	Origin           string
	Count            int
	BaseBranch       string
	BranchPrefix     string
	WorktreeRoot     string
	GitSSHCommand    string
	RequireSSHOrigin bool
	DryRun           bool
}

// NativeResetOptions describes the one destructive native lifecycle boundary.
// Reset authority comes from the explicit dev-all reset command plus the
// source-declared roots; callers cannot opt arbitrary paths into disposal.
type NativeResetOptions struct {
	Project        string
	Repo           string
	Count          int
	WorktreeRoot   string
	StateRoot      string
	ProtectedRoots []string
	DryRun         bool
}

type nativeResetCandidate struct {
	path     string
	boundary string
}

// NativeResetPlan is an opaque, preflighted disposal plan. The paths are
// deliberately not exported so a caller cannot turn validation into an
// arbitrary recursive-delete facility.
type NativeResetPlan struct {
	dryRun         bool
	candidates     []nativeResetCandidate
	protectedRoots []string
	mountPoints    []string
}

var nativeResetMountPoints = readNativeResetMountPoints

func decodeMountInfoPath(value string) string {
	replacer := strings.NewReplacer(
		`\040`, " ",
		`\011`, "\t",
		`\012`, "\n",
		`\134`, `\`,
	)
	return replacer.Replace(value)
}

func readNativeResetMountPoints() ([]string, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, fmt.Errorf("read native reset mount inventory: %w", err)
	}
	var result []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		result = append(result, filepath.Clean(decodeMountInfoPath(fields[4])))
	}
	return result, nil
}

func validateNativeResetPathComponents(path string) error {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return fmt.Errorf("native reset path %s must be absolute", path)
	}
	current := string(filepath.Separator)
	volume := filepath.VolumeName(path)
	if volume != "" {
		current = volume + string(filepath.Separator)
	}
	relative := strings.TrimPrefix(path, current)
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect native reset path %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("native reset path %s traverses a symlink or junction", current)
		}
	}
	return nil
}

func nativeResetPathsOverlap(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	return pathWithinRoot(left, right) || pathWithinRoot(right, left)
}

func validateNativeResetCandidate(candidate nativeResetCandidate, protectedRoots, mountPoints []string) error {
	if !pathWithinRoot(candidate.boundary, candidate.path) || candidate.path == candidate.boundary {
		return fmt.Errorf("native reset target %s escapes its declared ownership boundary %s", candidate.path, candidate.boundary)
	}
	if err := validateNativeResetPathComponents(candidate.path); err != nil {
		return err
	}
	for _, protected := range protectedRoots {
		protected = strings.TrimSpace(protected)
		if protected == "" {
			continue
		}
		absolute, err := filepath.Abs(filepath.Clean(protected))
		if err != nil {
			return fmt.Errorf("resolve native reset protected root %s: %w", protected, err)
		}
		if nativeResetPathsOverlap(candidate.path, absolute) {
			return fmt.Errorf("native reset target %s overlaps protected root %s", candidate.path, absolute)
		}
	}
	for _, mountPoint := range mountPoints {
		mountPoint = filepath.Clean(strings.TrimSpace(mountPoint))
		if mountPoint == "" {
			continue
		}
		if candidate.path == mountPoint || pathWithinRoot(candidate.path, mountPoint) {
			return fmt.Errorf("native reset target %s contains mount point %s", candidate.path, mountPoint)
		}
	}
	return nil
}

// PlanNativeReset validates the complete destructive boundary before any
// broker, session, workspace, home, or bootstrap effect occurs. Existing
// payload beneath an exact owned target is intentionally opaque: a foreign
// .git pointer is inert data to unlink, never a source of expanded custody.
func PlanNativeReset(opts NativeResetOptions) (*NativeResetPlan, error) {
	if strings.TrimSpace(opts.Project) != "dev-all" {
		return nil, fmt.Errorf("native destructive reset requires the exact dev-all prefix")
	}
	repo := strings.TrimSpace(opts.Repo)
	if strings.TrimSpace(opts.WorktreeRoot) == "" {
		return nil, fmt.Errorf("native reset worktree root is required")
	}
	worktreeRoot, err := filepath.Abs(filepath.Clean(strings.TrimSpace(opts.WorktreeRoot)))
	if err != nil {
		return nil, fmt.Errorf("resolve native reset worktree root %s: %w", opts.WorktreeRoot, err)
	}
	if strings.TrimSpace(opts.StateRoot) == "" {
		return nil, fmt.Errorf("native reset state root is required")
	}
	stateRoot, err := filepath.Abs(filepath.Clean(strings.TrimSpace(opts.StateRoot)))
	if err != nil {
		return nil, fmt.Errorf("resolve native reset state root %s: %w", opts.StateRoot, err)
	}
	commonDir, err := nativeOwnedCommonRepositoryPath(worktreeRoot, repo)
	if err != nil {
		return nil, err
	}
	count := opts.Count
	if count < 1 {
		return nil, fmt.Errorf("native destructive reset count must be positive")
	}
	if err := validateNativeResetPathComponents(worktreeRoot); err != nil {
		return nil, err
	}
	if err := validateNativeResetPathComponents(stateRoot); err != nil {
		return nil, err
	}

	candidates := make([]nativeResetCandidate, 0, count*2+2)
	for index := 1; index <= count; index++ {
		candidates = append(candidates,
			nativeResetCandidate{
				path:     filepath.Join(worktreeRoot, fmt.Sprintf("agent%d", index)),
				boundary: worktreeRoot,
			},
			nativeResetCandidate{
				path:     filepath.Join(stateRoot, fmt.Sprintf("%s-agent%d", opts.Project, index)),
				boundary: stateRoot,
			},
		)
	}
	candidates = append(candidates,
		nativeResetCandidate{path: commonDir, boundary: worktreeRoot},
		nativeResetCandidate{
			path:     runtimeagent.ManifestPath(stateRoot, opts.Project),
			boundary: stateRoot,
		},
	)
	staging, err := filepath.Glob(filepath.Join(filepath.Dir(commonDir), "."+repo+".bootstrap-*"))
	if err != nil {
		return nil, fmt.Errorf("enumerate native reset bootstrap staging paths: %w", err)
	}
	for _, path := range staging {
		candidates = append(candidates, nativeResetCandidate{path: path, boundary: worktreeRoot})
	}

	mountPoints, err := nativeResetMountPoints()
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		if err := validateNativeResetCandidate(candidate, opts.ProtectedRoots, mountPoints); err != nil {
			return nil, err
		}
	}
	return &NativeResetPlan{
		dryRun:         opts.DryRun,
		candidates:     candidates,
		protectedRoots: append([]string(nil), opts.ProtectedRoots...),
		mountPoints:    append([]string(nil), mountPoints...),
	}, nil
}

type stagedNativeResetPath struct {
	source string
	target string
}

func rollbackNativeResetStaging(staged []stagedNativeResetPath) error {
	var result error
	for index := len(staged) - 1; index >= 0; index-- {
		item := staged[index]
		if err := os.Rename(item.target, item.source); err != nil {
			result = errors.Join(result, fmt.Errorf("restore native reset target %s: %w", item.source, err))
		}
	}
	return result
}

// Apply atomically removes the validated active names before discarding their
// contents. A staging failure restores every name already moved.
func (plan *NativeResetPlan) Apply() error {
	if plan == nil {
		return fmt.Errorf("native reset plan is required")
	}
	for _, candidate := range plan.candidates {
		if err := validateNativeResetCandidate(candidate, plan.protectedRoots, plan.mountPoints); err != nil {
			return fmt.Errorf("revalidate native reset boundary before disposal: %w", err)
		}
	}
	if plan.dryRun {
		for _, candidate := range plan.candidates {
			fmt.Fprintf(os.Stderr, "+ reset-owned %s\n", candidate.path)
		}
		return nil
	}
	quarantines := map[string]string{}
	var staged []stagedNativeResetPath
	cleanupEmptyQuarantines := func() {
		for _, quarantine := range quarantines {
			_ = os.Remove(quarantine)
		}
	}
	for _, candidate := range plan.candidates {
		info, err := os.Lstat(candidate.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			rollbackErr := rollbackNativeResetStaging(staged)
			cleanupEmptyQuarantines()
			return errors.Join(fmt.Errorf("inspect native reset target %s: %w", candidate.path, err), rollbackErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			rollbackErr := rollbackNativeResetStaging(staged)
			cleanupEmptyQuarantines()
			return errors.Join(fmt.Errorf("native reset target %s became a symlink or junction", candidate.path), rollbackErr)
		}
		quarantine := quarantines[candidate.boundary]
		if quarantine == "" {
			quarantine, err = os.MkdirTemp(candidate.boundary, ".devkit-reset-")
			if err != nil {
				rollbackErr := rollbackNativeResetStaging(staged)
				cleanupEmptyQuarantines()
				return errors.Join(fmt.Errorf("create native reset quarantine under %s: %w", candidate.boundary, err), rollbackErr)
			}
			quarantines[candidate.boundary] = quarantine
		}
		target := filepath.Join(quarantine, fmt.Sprintf("%03d", len(staged)))
		if err := os.Rename(candidate.path, target); err != nil {
			rollbackErr := rollbackNativeResetStaging(staged)
			cleanupEmptyQuarantines()
			return errors.Join(fmt.Errorf("stage native reset target %s: %w", candidate.path, err), rollbackErr)
		}
		staged = append(staged, stagedNativeResetPath{source: candidate.path, target: target})
	}
	for _, quarantine := range quarantines {
		if err := os.RemoveAll(quarantine); err != nil {
			return fmt.Errorf("discard native reset quarantine %s: %w", quarantine, err)
		}
	}
	return nil
}

// PreflightNative validates every target in a multi-slot reconstruction before
// bootstrap identity or transport state is materialized. Existing
// package-owned worktrees and their exact per-agent homes are accepted;
// foreign, standalone, partial, and traversing targets fail closed.
func PreflightNative(opts NativeOptions) error {
	devkitRoot := filepath.Clean(opts.DevkitRoot)
	devRoot := filepath.Clean(filepath.Join(devkitRoot, ".."))
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		return fmt.Errorf("repo is required")
	}
	if runtimeagent.IsWorkspaceRootRepo(repo) {
		return nil
	}
	count := opts.Count
	if count < 1 {
		count = 1
	}
	worktreesRoot := strings.TrimSpace(opts.WorktreeRoot)
	if worktreesRoot == "" {
		worktreesRoot = filepath.Join(devRoot, paths.AgentWorktreesDir)
	}
	commonDir, err := nativeOwnedCommonRepositoryPath(worktreesRoot, repo)
	if err != nil {
		return err
	}
	return preflightNativeOwnedWorktreeTargets(worktreesRoot, commonDir, repo, count)
}

// SetupNative creates dedicated worktrees for every native agent, including
// agent1, without changing the primary checkout's current branch.
func SetupNative(opts NativeOptions) error {
	devkitRoot := filepath.Clean(opts.DevkitRoot)
	devRoot := filepath.Clean(filepath.Join(devkitRoot, ".."))
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		return fmt.Errorf("repo is required")
	}
	if runtimeagent.IsWorkspaceRootRepo(repo) {
		return nil
	}
	count := opts.Count
	if count < 1 {
		count = 1
	}
	baseBranch := strings.TrimSpace(opts.BaseBranch)
	if baseBranch == "" {
		baseBranch = "main"
	}
	branchPrefix := strings.TrimSpace(opts.BranchPrefix)
	if branchPrefix == "" {
		branchPrefix = "agent"
	}
	worktreesRoot := strings.TrimSpace(opts.WorktreeRoot)
	if worktreesRoot == "" {
		worktreesRoot = filepath.Join(devRoot, paths.AgentWorktreesDir)
	}
	repoCommonDir, err := nativeOwnedCommonRepositoryPath(worktreesRoot, repo)
	if err != nil {
		return err
	}
	repoPath := filepath.Join(devRoot, repo)
	gitSSHCommand := strings.TrimSpace(opts.GitSSHCommand)
	envLocalGit := func(args ...string) []string {
		return append([]string{"-u", "GIT_SSH_COMMAND", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "git"}, args...)
	}
	envRemoteGit := func(args ...string) []string {
		prefix := []string{}
		if gitSSHCommand != "" {
			prefix = append(prefix, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_SSH_COMMAND="+gitSSHCommand)
		} else {
			prefix = append(prefix, "-u", "GIT_SSH_COMMAND", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		}
		return append(prefix, append([]string{"git"}, args...)...)
	}
	remoteURL := strings.TrimSpace(opts.Origin)
	result := execx.Result{Code: 0}
	if remoteURL == "" {
		if opts.RequireSSHOrigin {
			return fmt.Errorf("native Git bootstrap requires the source-declared SSH origin; ambient checkout remotes are not bootstrap authority")
		}
		remoteURL, result = execx.Capture(context.Background(), "git", "-C", repoPath, "remote", "get-url", "origin")
		if result.Code != 0 && !opts.DryRun {
			return fmt.Errorf("read native Git bootstrap origin: exit %d", result.Code)
		}
	}
	sshOrigin := opts.RequireSSHOrigin
	if result.Code == 0 {
		sshOrigin = gitRemoteRequiresSSH(strings.TrimSpace(remoteURL))
	}
	if opts.RequireSSHOrigin && !sshOrigin {
		return fmt.Errorf("native Git bootstrap requires the source-declared SSH origin; HTTPS, file, and ambient transport fallbacks are prohibited")
	}
	if gitSSHCommand == "" {
		if sshOrigin {
			return fmt.Errorf("native Git bootstrap for SSH origin requires a package-owned SSH command")
		}
	}
	remoteURL = strings.TrimSpace(remoteURL)
	if remoteURL == "" {
		if opts.DryRun {
			remoteURL = "SOURCE_DECLARED_ORIGIN"
		} else {
			return fmt.Errorf("native Git bootstrap origin is empty")
		}
	}
	if !opts.DryRun {
		if err := PreflightNative(opts); err != nil {
			return err
		}
	}
	repoCommonDir, err = ensureNativeOwnedCommonRepository(
		worktreesRoot,
		repo,
		remoteURL,
		envLocalGit,
		envRemoteGit,
		opts.DryRun,
	)
	if err != nil {
		return err
	}
	if err := run(opts.DryRun, "env", envLocalGit("--git-dir", repoCommonDir, "config", "worktree.useRelativePaths", "true")...); err != nil {
		return err
	}
	for i := 1; i <= count; i++ {
		parent := filepath.Join(worktreesRoot, fmt.Sprintf("agent%d", i))
		if !opts.DryRun {
			_ = os.MkdirAll(parent, 0o755)
		}
		wt := filepath.Join(parent, repo)
		branch := fmt.Sprintf("%s%d", branchPrefix, i)
		allowedHome := fmt.Sprintf(".devhome-agent%d", i)
		if !opts.DryRun {
			if ok, err := existingGitCheckout(wt); err != nil {
				return err
			} else if ok {
				if err := run(false, "env", envLocalGit("-C", wt, "config", "worktree.useRelativePaths", "true")...); err != nil {
					return err
				}
				if err := rewriteNativeGitdir(wt, worktreesRoot, repoCommonDir); err != nil {
					return err
				}
				continue
			}
		}
		stagingDir := ""
		if !opts.DryRun {
			stagingDir, err = stageNativeWorktreePayload(wt, allowedHome)
			if err != nil {
				return err
			}
		}
		_ = run(opts.DryRun, "env", envLocalGit("--git-dir", repoCommonDir, "worktree", "prune")...)
		_ = run(opts.DryRun, "env", envLocalGit("--git-dir", repoCommonDir, "worktree", "remove", "-f", wt)...)
		if err := run(opts.DryRun, "env", envLocalGit("--git-dir", repoCommonDir, "worktree", "add", wt, "-B", branch, "origin/"+baseBranch)...); err != nil {
			if opts.DryRun {
				return err
			}
			_ = run(false, "env", envLocalGit("--git-dir", repoCommonDir, "worktree", "remove", "-f", wt)...)
			if remErr := os.RemoveAll(wt); remErr != nil && !errors.Is(remErr, os.ErrNotExist) {
				return errors.Join(err, fmt.Errorf("remove failed native worktree %s: %w", wt, remErr))
			}
			if restoreErr := restoreNativeWorktreePayload(wt, allowedHome, stagingDir); restoreErr != nil {
				return errors.Join(err, restoreErr)
			}
			return err
		}
		if !opts.DryRun {
			if err := restoreNativeWorktreePayload(wt, allowedHome, stagingDir); err != nil {
				return err
			}
		}
		if !opts.DryRun {
			if err := rewriteNativeGitdir(wt, worktreesRoot, repoCommonDir); err != nil {
				return err
			}
		}
		if err := run(opts.DryRun, "env", envLocalGit("-C", wt, "branch", "--set-upstream-to=origin/"+baseBranch, branch)...); err != nil {
			return err
		}
		if !opts.DryRun {
			if err := verifyFreshNativeWorktree(wt, "origin/"+baseBranch); err != nil {
				return err
			}
		}
	}
	return nil
}

func gitRemoteRequiresSSH(remoteURL string) bool {
	remoteURL = strings.TrimSpace(remoteURL)
	return strings.HasPrefix(remoteURL, "ssh://") ||
		(strings.Contains(remoteURL, "@") && strings.Contains(remoteURL, ":") &&
			!strings.Contains(remoteURL, "://"))
}

package worktrees

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"devkit/cli/devctl/internal/execx"
	"devkit/cli/devctl/internal/paths"
	runtimeagent "devkit/cli/devctl/internal/runtime/agent"
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

var (
	packageGitExecutable string
	packageEnvExecutable string
)

func resolvedPackageGitExecutable() (string, error) {
	path := filepath.Clean(strings.TrimSpace(packageGitExecutable))
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("native lifecycle requires the package-owned absolute Git executable")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("inspect package-owned Git executable %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("package-owned Git executable %s is not executable", path)
	}
	return path, nil
}

func resolvedPackageEnvExecutable() (string, error) {
	path := filepath.Clean(strings.TrimSpace(packageEnvExecutable))
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("native lifecycle requires the package-owned absolute env executable")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("inspect package-owned env executable %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("package-owned env executable %s is not executable", path)
	}
	return path, nil
}

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

func runSlotWithPolicy(dry bool, fixedLimit, idleLimit time.Duration, name string, args ...string) error {
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
	res := execx.RunManagedWithWriters(ctx, execx.ManagedPolicy{
		IdleTimeout:      idleLimit,
		TerminationGrace: nativeTerminationGrace,
	}, os.Stderr, os.Stderr, name, args...)
	if res.Code != 0 {
		if res.Err != nil {
			return fmt.Errorf("%s %v: exit %d: %w", name, args, res.Code, res.Err)
		}
		return fmt.Errorf("%s %v: exit %d", name, args, res.Code)
	}
	return nil
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
		return fmt.Errorf("fresh native worktree %s HEAD does not match %s", wt, baseRef)
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
	if isCanonicalProductIdentity(repo, "") {
		return fmt.Errorf("legacy worktree setup has no Product authority; use the manifest-bound product-adapter")
	}
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

type NativeSlotOptions struct {
	DevkitRoot       string
	Repo             string
	Origin           string
	Count            int
	Index            int
	BaseBranch       string
	SourceRevision   string
	BranchPrefix     string
	WorktreeRoot     string
	BootstrapHome    string
	GitSSHCommand    string
	RequireSSHOrigin bool
	DryRun           bool
}

type NativeSlotValidationOptions struct {
	Repo           string
	Origin         string
	Index          int
	WorktreeRoot   string
	HostHome       string
	StateRoot      string
	SourceRevision string
}

func packageGitCommand(
	envExecutable string,
	gitExecutable string,
	sshCommand string,
	args ...string,
) []string {
	command := make([]string, 0, 8+len(args))
	if filepath.Base(envExecutable) == "coreutils" {
		command = append(command, "--coreutils-prog=env")
	}
	command = append(command,
		"-i",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	if sshCommand = strings.TrimSpace(sshCommand); sshCommand != "" {
		command = append(command, "GIT_SSH_COMMAND="+sshCommand)
	}
	command = append(command, gitExecutable)
	return append(command, args...)
}

// SetupNativeSlot creates a new independent common repository and linked
// worktree wholly beneath one selected disposable agent leaf. It never opens
// or mutates the legacy shared common repository or any sibling registration.
func SetupNativeSlot(opts NativeSlotOptions) error {
	if opts.Index < 1 || opts.Index > opts.Count {
		return fmt.Errorf("native absent-slot construction requires a selected index")
	}
	revision := strings.TrimSpace(opts.SourceRevision)
	if !isExactGitRevision(revision) {
		return fmt.Errorf("native absent-slot construction requires an exact Product source revision")
	}
	remoteURL := strings.TrimSpace(opts.Origin)
	if !gitRemoteRequiresSSH(remoteURL) {
		return fmt.Errorf("native absent-slot construction requires the source-declared SSH origin")
	}
	sshCommand := strings.TrimSpace(opts.GitSSHCommand)
	if sshCommand == "" {
		return fmt.Errorf("native absent-slot construction requires the package-owned SSH command")
	}
	gitExecutable, err := resolvedPackageGitExecutable()
	if err != nil {
		return err
	}
	envExecutable, err := resolvedPackageEnvExecutable()
	if err != nil {
		return err
	}
	worktreesRoot := filepath.Clean(strings.TrimSpace(opts.WorktreeRoot))
	agentRoot := filepath.Join(worktreesRoot, fmt.Sprintf("agent%d", opts.Index))
	worktree := filepath.Join(agentRoot, opts.Repo)
	commonDir := filepath.Join(agentRoot, ".devkit", "git", opts.Repo+".git")
	bootstrapHome := filepath.Clean(strings.TrimSpace(opts.BootstrapHome))
	if opts.DryRun {
		fmt.Fprintf(os.Stderr, "+ construct-native-absent-slot index=%d common=%s worktree=%s revision=%s\n",
			opts.Index, commonDir, worktree, revision)
		return nil
	}
	if _, err := os.Lstat(commonDir); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("selected native slot common repository %s must be absent", commonDir)
		}
		return err
	}
	worktreeContainsBootstrapHome := false
	if _, err := os.Lstat(worktree); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		relativeHome, relativeErr := filepath.Rel(worktree, bootstrapHome)
		allowedHome := fmt.Sprintf(".devhome-agent%d", opts.Index)
		if relativeErr != nil || relativeHome != allowedHome {
			return fmt.Errorf("selected native slot worktree %s must be absent", worktree)
		}
		entries, readErr := os.ReadDir(worktree)
		if readErr != nil ||
			len(entries) != 1 ||
			entries[0].Name() != allowedHome ||
			!entries[0].IsDir() ||
			entries[0].Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("selected native slot worktree %s contains payload other than the package-created bootstrap home", worktree)
		}
		worktreeContainsBootstrapHome = true
	}
	envLocalGit := func(args ...string) []string {
		return packageGitCommand(envExecutable, gitExecutable, "", args...)
	}
	envRemoteGit := func(args ...string) []string {
		return packageGitCommand(envExecutable, gitExecutable, sshCommand, args...)
	}
	if err := os.MkdirAll(filepath.Dir(commonDir), 0o700); err != nil {
		return fmt.Errorf("create selected native slot Git root: %w", err)
	}
	if err := runSlotWithPolicy(opts.DryRun, nativeMetadataOperationLimit, 0, envExecutable, envLocalGit("init", "--bare", "--initial-branch=main", commonDir)...); err != nil {
		return err
	}
	if err := runSlotWithPolicy(opts.DryRun, nativeMetadataOperationLimit, 0, envExecutable, envLocalGit("--git-dir", commonDir, "config", "worktree.useRelativePaths", "true")...); err != nil {
		return err
	}
	if err := runSlotWithPolicy(opts.DryRun, nativeMetadataOperationLimit, 0, envExecutable, envLocalGit("--git-dir", commonDir, "config", "extensions.worktreeConfig", "true")...); err != nil {
		return err
	}
	if err := runSlotWithPolicy(opts.DryRun, nativeMetadataOperationLimit, 0, envExecutable, envLocalGit("--git-dir", commonDir, "remote", "add", "origin", remoteURL)...); err != nil {
		return err
	}
	if err := runSlotWithPolicy(opts.DryRun, 0, nativeFetchIdleLimit, envExecutable, envRemoteGit(
		"--git-dir", commonDir,
		"fetch", "--no-tags", "--progress", "origin", revision,
	)...); err != nil {
		return fmt.Errorf("fetch exact authoritative Product source %s: %w", revision, err)
	}
	out, result := execx.Capture(context.Background(), envExecutable, envLocalGit("--git-dir", commonDir, "rev-parse", "FETCH_HEAD^{commit}")...)
	object := strings.TrimSpace(out)
	err = nil
	if result.Code != 0 {
		err = fmt.Errorf("%s exited %d", envExecutable, result.Code)
	}
	if err != nil || object != revision {
		return fmt.Errorf("selected native slot fetched Product object %s, expected %s", object, revision)
	}
	marker, err := nativeOwnedCommonRepositoryMarker(opts.Repo, remoteURL)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(commonDir, "devkit-owned-common"), []byte(marker), 0o600); err != nil {
		return fmt.Errorf("write selected native slot common repository marker: %w", err)
	}
	stagedBootstrapHome := ""
	if worktreeContainsBootstrapHome {
		stagedBootstrapHome, err = stageNativeWorktreePayload(
			worktree,
			fmt.Sprintf(".devhome-agent%d", opts.Index),
		)
		if err != nil {
			return err
		}
	}
	if err := runSlotWithPolicy(false, nativeMetadataOperationLimit, 0, envExecutable, envLocalGit("--git-dir", commonDir, "worktree", "add", "--detach", worktree, revision)...); err != nil {
		return err
	}
	if err := restoreNativeWorktreePayload(
		worktree,
		fmt.Sprintf(".devhome-agent%d", opts.Index),
		stagedBootstrapHome,
	); err != nil {
		return err
	}
	registration := filepath.Join(commonDir, "worktrees", filepath.Base(opts.Repo))
	if err := runSlotWithPolicy(false, nativeMetadataOperationLimit, 0, envExecutable, envLocalGit("--git-dir", registration, "config", "--worktree", "core.bare", "false")...); err != nil {
		return fmt.Errorf("mark selected native slot registration as a worktree: %w", err)
	}
	if err := rewriteNativeGitdir(worktree, agentRoot, commonDir); err != nil {
		return err
	}
	slotGit := func(args ...string) (string, error) {
		out, result := execx.Capture(context.Background(), envExecutable, envLocalGit(args...)...)
		if result.Code != 0 {
			return "", fmt.Errorf("%s %v: exit %d", envExecutable, args, result.Code)
		}
		return strings.TrimSpace(out), nil
	}
	top, err := slotGit("-C", worktree, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("resolve selected native slot worktree: %w", err)
	}
	canonicalTop, err := canonicalExistingPath(top)
	if err != nil {
		return err
	}
	canonicalWorktree, err := canonicalExistingPath(worktree)
	if err != nil {
		return err
	}
	if canonicalTop != canonicalWorktree {
		return fmt.Errorf("selected native slot top-level %s does not match worktree %s", canonicalTop, canonicalWorktree)
	}
	head, err := slotGit("-C", worktree, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("resolve selected native slot revision: %w", err)
	}
	if head != revision {
		return fmt.Errorf("selected native slot HEAD %s does not match authoritative source revision %s", head, revision)
	}
	status, err := slotGit("-C", worktree, "status", "--porcelain=v1")
	if err != nil {
		return fmt.Errorf("inspect selected native slot status: %w", err)
	}
	if status != "" {
		return fmt.Errorf("selected native slot materialization is dirty")
	}
	return nil
}

// ValidateNativeSlot proves that an already-constructed selected slot is the
// exact manifest-pinned, portable, writable package-owned checkout expected by
// the source-derived plan. It is read-only and never materializes or changes state.
func ValidateNativeSlot(opts NativeSlotValidationOptions) error {
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" || opts.Index < 1 {
		return fmt.Errorf("native slot validation requires repo and selected index")
	}
	revision := strings.TrimSpace(opts.SourceRevision)
	if !isExactGitRevision(revision) {
		return fmt.Errorf("native slot validation requires the exact Product source revision")
	}
	worktreeRoot := filepath.Clean(strings.TrimSpace(opts.WorktreeRoot))
	agentRoot := filepath.Join(worktreeRoot, fmt.Sprintf("agent%d", opts.Index))
	worktree := filepath.Join(agentRoot, repo)
	commonDir := filepath.Join(agentRoot, ".devkit", "git", repo+".git")
	hostHome := filepath.Clean(strings.TrimSpace(opts.HostHome))
	stateRoot := filepath.Clean(strings.TrimSpace(opts.StateRoot))
	for label, path := range map[string]string{
		"selected agent root":  agentRoot,
		"selected worktree":    worktree,
		"selected common repo": commonDir,
		"selected home":        hostHome,
		"selected state":       stateRoot,
	} {
		if path == "." || !filepath.IsAbs(path) {
			return fmt.Errorf("%s must be an exact absolute path", label)
		}
		resolved, err := canonicalExistingPath(path)
		if err != nil {
			return fmt.Errorf("validate %s %s: %w", label, path, err)
		}
		if resolved != path {
			return fmt.Errorf("%s %s resolves through unexpected alias %s", label, path, resolved)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect %s %s: %w", label, path, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s %s must be a plain directory", label, path)
		}
	}
	if hostHome != agentRoot && !pathWithinRoot(agentRoot, hostHome) {
		return fmt.Errorf("selected home %s escapes agent root %s", hostHome, agentRoot)
	}
	gitFile := filepath.Join(worktree, ".git")
	gitInfo, err := os.Lstat(gitFile)
	if err != nil {
		return fmt.Errorf("inspect selected worktree metadata %s: %w", gitFile, err)
	}
	if gitInfo.Mode()&os.ModeSymlink != 0 || !gitInfo.Mode().IsRegular() {
		return fmt.Errorf("selected worktree metadata %s must be a plain file", gitFile)
	}
	gitdirValue, err := readGitdirPointer(gitFile)
	if err != nil {
		return err
	}
	if filepath.IsAbs(gitdirValue) {
		return fmt.Errorf("selected worktree gitdir must be relative")
	}
	gitdir := filepath.Clean(filepath.Join(worktree, gitdirValue))
	worktreesDir := filepath.Join(commonDir, "worktrees")
	if !pathWithinRoot(worktreesDir, gitdir) {
		return fmt.Errorf("selected worktree gitdir %s escapes package-owned common repository %s", gitdir, commonDir)
	}
	for name, expected := range map[string]string{
		"commondir": commonDir,
		"gitdir":    gitFile,
	} {
		data, err := os.ReadFile(filepath.Join(gitdir, name))
		if err != nil {
			return fmt.Errorf("read selected worktree %s metadata: %w", name, err)
		}
		value := strings.TrimSpace(string(data))
		if value == "" || filepath.IsAbs(value) {
			return fmt.Errorf("selected worktree %s metadata must be relative", name)
		}
		if resolved := filepath.Clean(filepath.Join(gitdir, value)); resolved != expected {
			return fmt.Errorf("selected worktree %s resolves to %s, expected %s", name, resolved, expected)
		}
	}
	marker, err := nativeOwnedCommonRepositoryMarker(repo, strings.TrimSpace(opts.Origin))
	if err != nil {
		return err
	}
	markerData, err := os.ReadFile(filepath.Join(commonDir, "devkit-owned-common"))
	if err != nil {
		return fmt.Errorf("read selected common repository authority marker: %w", err)
	}
	if string(markerData) != marker {
		return fmt.Errorf("selected common repository authority marker does not match source-declared origin")
	}
	gitExecutable, err := resolvedPackageGitExecutable()
	if err != nil {
		return err
	}
	envExecutable, err := resolvedPackageEnvExecutable()
	if err != nil {
		return err
	}
	git := func(args ...string) (string, error) {
		args = append([]string{"--no-optional-locks"}, args...)
		out, result := execx.Capture(
			context.Background(),
			envExecutable,
			packageGitCommand(envExecutable, gitExecutable, "", args...)...,
		)
		if result.Code != 0 {
			return "", fmt.Errorf("package Git %v exited %d", args, result.Code)
		}
		return strings.TrimSpace(out), nil
	}
	top, err := git("-C", worktree, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("resolve selected worktree top-level: %w", err)
	}
	if filepath.Clean(top) != worktree {
		return fmt.Errorf("selected worktree top-level %s does not match %s", top, worktree)
	}
	head, err := git("-C", worktree, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("resolve selected Product HEAD: %w", err)
	}
	if head != revision {
		return fmt.Errorf("selected Product HEAD %s does not match manifest revision %s", head, revision)
	}
	origin, err := git("-C", worktree, "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("resolve selected Product origin: %w", err)
	}
	if origin != strings.TrimSpace(opts.Origin) {
		return fmt.Errorf("selected Product origin %s does not match source declaration %s", origin, strings.TrimSpace(opts.Origin))
	}
	status, err := git("-C", worktree, "status", "--porcelain=v1")
	if err != nil {
		return fmt.Errorf("inspect selected Product status: %w", err)
	}
	if status != "" {
		return fmt.Errorf("selected Product worktree is not clean")
	}
	const writeAccess = 0x2
	for _, path := range []string{worktree, commonDir, gitdir} {
		if err := syscall.Access(path, writeAccess); err != nil {
			return fmt.Errorf("selected Product Git path %s is not writable: %w", path, err)
		}
	}
	return nil
}

// PreflightNative validates every target in a legacy multi-slot setup before
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
	if isCanonicalProductIdentity(opts.Repo, opts.Origin) {
		return fmt.Errorf("legacy native setup has no Product authority; use the manifest-bound product-adapter")
	}
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

func isExactGitRevision(value string) bool {
	if len(value) != 40 || strings.ToLower(value) != value {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

func gitRemoteRequiresSSH(remoteURL string) bool {
	remoteURL = strings.TrimSpace(remoteURL)
	return strings.HasPrefix(remoteURL, "ssh://") ||
		(strings.Contains(remoteURL, "@") && strings.Contains(remoteURL, ":") &&
			!strings.Contains(remoteURL, "://"))
}

func isCanonicalProductIdentity(repo, remoteURL string) bool {
	if strings.EqualFold(strings.TrimSpace(repo), "ouroboros-ide") {
		return true
	}
	remote := strings.ToLower(strings.TrimSpace(remoteURL))
	remote = strings.TrimSuffix(strings.TrimSuffix(remote, "/"), ".git")
	if strings.HasPrefix(remote, "git@github.com:") {
		return filepath.Base(strings.TrimPrefix(remote, "git@github.com:")) == "ouroboros-ide"
	}
	for _, prefix := range []string{
		"ssh://git@github.com/",
		"ssh://git@ssh.github.com:443/",
		"https://github.com/",
	} {
		if strings.HasPrefix(remote, prefix) {
			return filepath.Base(strings.TrimPrefix(remote, prefix)) == "ouroboros-ide"
		}
	}
	return false
}

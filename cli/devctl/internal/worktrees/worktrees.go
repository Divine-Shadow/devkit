package worktrees

import (
	"context"
	"crypto/rand"
	"devkit/cli/devctl/internal/execx"
	"devkit/cli/devctl/internal/gitauthority"
	"devkit/cli/devctl/internal/paths"
	runtimeagent "devkit/cli/devctl/internal/runtime/agent"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
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

// Production packages bind these source-acquisition executables at link time.
// Source tests retain conventional names; promoted native bootstrap/reset
// requires the absolute package-owned values before effects.
var (
	packageEnvExecutable = "env"
)

func nativeSourceExecutables(requirePackage bool) (string, string, error) {
	envExecutable := strings.TrimSpace(packageEnvExecutable)
	gitExecutable := gitauthority.Executable()
	if envExecutable == "" {
		envExecutable = "env"
	}
	if requirePackage && !filepath.IsAbs(envExecutable) {
		return "", "", fmt.Errorf("native source acquisition requires package-owned absolute env and Git executables")
	}
	if requirePackage {
		var err error
		gitExecutable, err = gitauthority.RequirePackage()
		if err != nil {
			return "", "", err
		}
	}
	for name, executable := range map[string]string{"env": envExecutable, "Git": gitExecutable} {
		if !filepath.IsAbs(executable) {
			continue
		}
		info, err := os.Stat(executable)
		if err != nil {
			return "", "", fmt.Errorf("validate package-owned %s executable %s: %w", name, executable, err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return "", "", fmt.Errorf("package-owned %s executable is not executable: %s", name, executable)
		}
	}
	return envExecutable, gitExecutable, nil
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

// rewriteGitdir writes a .git file pointing to a gitdir form suitable for the
// runtime that will mount the worktree.
func rewriteGitdir(wt string, relative bool) {
	if info, err := os.Stat(filepath.Join(wt, ".git")); err == nil && info.IsDir() {
		return
	}
	out, res := execx.Capture(context.Background(), gitauthority.Executable(), "-C", wt, "rev-parse", "--git-dir")
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

// EnsurePortableNativeGitdir migrates an existing package-owned linked
// worktree away from an absolute Git metadata pointer before an isolated exec
// plan is built. Older native lanes may retain the consumer-visible
// /workspaces/dev alias even though it resolves to the package-owned metadata
// beneath worktreesRoot. rewriteNativeGitdir performs the full ownership,
// commondir, and reverse-pointer validation before changing any file.
func EnsurePortableNativeGitdir(wt, worktreesRoot, repo string, index int) error {
	gitFile := filepath.Join(wt, ".git")
	info, err := os.Lstat(gitFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect native worktree gitdir %s: %w", gitFile, err)
	}
	if info.IsDir() {
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("native worktree gitdir %s must be a regular file", gitFile)
	}
	gitdir, err := readGitdirPointer(gitFile)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(gitdir) {
		return nil
	}
	repoCommonDir, err := nativeOwnedCommonRepositoryPath(worktreesRoot, repo, index)
	if err != nil {
		return err
	}
	actualCommonDir, err := nativeWorktreeCommonDirectory(wt)
	if err != nil {
		return err
	}
	legacyCommonDir, err := nativeLegacyOwnedCommonRepositoryPath(worktreesRoot, repo)
	if err != nil {
		return err
	}
	canonicalLaneCommon := canonicalOrClean(repoCommonDir)
	canonicalLegacyCommon := canonicalOrClean(legacyCommonDir)
	if actualCommonDir != canonicalLaneCommon && actualCommonDir != canonicalLegacyCommon {
		return fmt.Errorf(
			"native worktree %s common Git directory %s is neither lane-owned %s nor legacy package-owned %s",
			canonicalOrClean(wt),
			actualCommonDir,
			canonicalLaneCommon,
			canonicalLegacyCommon,
		)
	}
	repoCommonDir = actualCommonDir
	return rewriteNativeGitdir(wt, worktreesRoot, repoCommonDir)
}

func nativeWorktreeCommonDirectory(worktree string) (string, error) {
	gitdirValue, err := readGitdirPointer(filepath.Join(worktree, ".git"))
	if err != nil {
		return "", err
	}
	gitdir, err := canonicalMetadataPath(worktree, gitdirValue)
	if err != nil {
		return "", fmt.Errorf("canonicalize native worktree gitdir %s: %w", worktree, err)
	}
	commondirValue, err := readPlainMetadataPath(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return "", err
	}
	commonDir, err := canonicalMetadataPath(gitdir, commondirValue)
	if err != nil {
		return "", fmt.Errorf("canonicalize native worktree commondir %s: %w", worktree, err)
	}
	return commonDir, nil
}

func ensureNativeLinkedWorktreeNonBare(wt, envExecutable string, envLocalGit func(...string) []string, dryRun bool) error {
	gitFile := filepath.Join(wt, ".git")
	if dryRun {
		return run(true, envExecutable, envLocalGit("--git-dir", gitFile, "config", "--worktree", "core.bare", "false")...)
	}
	value, err := readGitdirPointer(gitFile)
	if err != nil {
		return err
	}
	gitdir := filepath.Clean(value)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Clean(filepath.Join(wt, gitdir))
	}
	if err := run(false, envExecutable, envLocalGit("--git-dir", gitdir, "config", "extensions.worktreeConfig", "true")...); err != nil {
		return err
	}
	if err := run(false, envExecutable, envLocalGit("--git-dir", gitdir, "config", "--worktree", "core.bare", "false")...); err != nil {
		return err
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

func existingGitCheckout(wt, gitExecutable string) (bool, error) {
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
	out, res := execx.Capture(context.Background(), gitExecutable, "-C", wt, "rev-parse", "--show-toplevel")
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

// VerifyFreshNativeWorktree proves that a package-owned worktree is on the
// declared branch, tracks the fetched source-derived base ref, and is clean
// at that exact ref.
func VerifyFreshNativeWorktree(wt, branch, baseRef, gitExecutable string) error {
	ok, err := existingGitCheckout(wt, gitExecutable)
	if err != nil {
		return err
	}
	if !ok {
		out, err := exec.Command(gitExecutable, "-C", wt, "rev-parse", "--show-toplevel", "--git-dir", "--git-common-dir").CombinedOutput()
		gitFile, _ := os.ReadFile(filepath.Join(wt, ".git"))
		return fmt.Errorf("fresh native worktree %s is not a Git checkout: %v: %s; gitdir=%q", wt, err, strings.TrimSpace(string(out)), strings.TrimSpace(string(gitFile)))
	}
	actualBranch, result := execx.Capture(context.Background(), gitExecutable, "-C", wt, "symbolic-ref", "--short", "HEAD")
	if result.Code != 0 {
		return fmt.Errorf("read fresh native worktree branch %s: exit %d", wt, result.Code)
	}
	if strings.TrimSpace(actualBranch) != branch {
		return fmt.Errorf("fresh native worktree %s branch %s does not match declared branch %s", wt, strings.TrimSpace(actualBranch), branch)
	}
	upstream, result := execx.Capture(context.Background(), gitExecutable, "-C", wt, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if result.Code != 0 {
		return fmt.Errorf("read fresh native worktree upstream %s: exit %d", wt, result.Code)
	}
	if strings.TrimSpace(upstream) != baseRef {
		return fmt.Errorf("fresh native worktree %s upstream %s does not match declared base %s", wt, strings.TrimSpace(upstream), baseRef)
	}
	head, result := execx.Capture(context.Background(), gitExecutable, "-C", wt, "rev-parse", "HEAD")
	if result.Code != 0 {
		return fmt.Errorf("read fresh native worktree HEAD %s: exit %d", wt, result.Code)
	}
	base, result := execx.Capture(context.Background(), gitExecutable, "-C", wt, "rev-parse", baseRef)
	if result.Code != 0 {
		return fmt.Errorf("read fresh native worktree base %s at %s: exit %d", baseRef, wt, result.Code)
	}
	if strings.TrimSpace(head) != strings.TrimSpace(base) {
		return fmt.Errorf("fresh native worktree %s HEAD %s does not match %s %s", wt, strings.TrimSpace(head), baseRef, strings.TrimSpace(base))
	}
	status, result := execx.Capture(context.Background(), gitExecutable, "-C", wt, "status", "--porcelain=v1")
	if result.Code != 0 {
		return fmt.Errorf("read fresh native worktree status %s: exit %d", wt, result.Code)
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("fresh native worktree %s is dirty", wt)
	}
	return nil
}

func reconstructSelectedNativeWorktree(
	wt, branch, baseRef, envExecutable string,
	envLocalGit func(...string) []string,
	gitExecutable string,
) error {
	// ReconstructSelected is set only by the native slot reset lane, after that
	// lane has validated package-owned geometry and acquired its destructive
	// reset lock. Ordinary setup retains non-destructive existing-worktree
	// behavior.
	if err := run(false, envExecutable, envLocalGit("-C", wt, "checkout", "--force", "-B", branch, baseRef)...); err != nil {
		return fmt.Errorf("reset selected native worktree %s to %s: %w", wt, baseRef, err)
	}
	if err := run(false, envExecutable, envLocalGit("-C", wt, "clean", "-ffd")...); err != nil {
		return fmt.Errorf("remove untracked state from selected native worktree %s: %w", wt, err)
	}
	if err := run(false, envExecutable, envLocalGit("-C", wt, "branch", "--set-upstream-to="+baseRef, branch)...); err != nil {
		return err
	}
	return VerifyFreshNativeWorktree(wt, branch, baseRef, gitExecutable)
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
		return append([]string{"-u", "GIT_SSH_COMMAND", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", gitauthority.Executable()}, args...)
	}

	var repoWorktreesDir string
	if !dry {
		if out, res := execx.Capture(context.Background(), gitauthority.Executable(), "-C", repoPath, "rev-parse", "--git-dir"); res.Code == 0 {
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

const (
	nativeLegacyOwnedCommonRepositorySchema = "devkit/native-owned-common-repository/v1"
	nativeOwnedCommonRepositorySchema       = "devkit/native-owned-common-repository/v2"
)

func nativeOwnedCommonRepositoryPath(worktreesRoot, repo string, index int) (string, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" || repo == "." || filepath.IsAbs(repo) || filepath.Clean(repo) != repo || filepath.Base(repo) != repo {
		return "", fmt.Errorf("native repository name %q cannot select a package-owned common repository path", repo)
	}
	if index < 1 {
		return "", fmt.Errorf("native lane index %d cannot select a package-owned common repository path", index)
	}
	root, err := filepath.Abs(filepath.Clean(strings.TrimSpace(worktreesRoot)))
	if err != nil {
		return "", fmt.Errorf("resolve native worktree root %s: %w", worktreesRoot, err)
	}
	return filepath.Join(root, ".devkit", "git", fmt.Sprintf("agent%d", index), repo+".git"), nil
}

func nativeLegacyOwnedCommonRepositoryPath(worktreesRoot, repo string) (string, error) {
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

func nativeOwnedCommonRepositoryMarker(repo, remoteURL string, index int) (string, error) {
	for label, value := range map[string]string{"repository": repo, "origin": remoteURL} {
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n\x00") {
			return "", fmt.Errorf("native %s identity is invalid", label)
		}
	}
	if index < 1 {
		return "", fmt.Errorf("native lane identity agent%d is invalid", index)
	}
	return fmt.Sprintf(
		"schema=%s\nrepository=%s\norigin=%s\nlane=agent%d\n",
		nativeOwnedCommonRepositorySchema,
		repo,
		remoteURL,
		index,
	), nil
}

func nativeLegacyOwnedCommonRepositoryMarker(repo, remoteURL string) (string, error) {
	for label, value := range map[string]string{"repository": repo, "origin": remoteURL} {
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n\x00") {
			return "", fmt.Errorf("native %s identity is invalid", label)
		}
	}
	return fmt.Sprintf(
		"schema=%s\nrepository=%s\norigin=%s\n",
		nativeLegacyOwnedCommonRepositorySchema,
		repo,
		remoteURL,
	), nil
}

func captureNativeGit(envExecutable string, args []string) (string, error) {
	out, result := execx.Capture(context.Background(), envExecutable, args...)
	if result.Code != 0 {
		return "", fmt.Errorf("env %v: exit %d", args, result.Code)
	}
	return strings.TrimSpace(out), nil
}

func validateNativeOwnedCommonRepository(
	worktreesRoot, commonDir, repo, remoteURL string, index int,
	envExecutable string,
	envLocalGit func(...string) []string,
) error {
	expectedMarker, err := nativeOwnedCommonRepositoryMarker(repo, remoteURL, index)
	if err != nil {
		return err
	}
	return validateNativeCommonRepository(worktreesRoot, commonDir, repo, remoteURL, expectedMarker, envExecutable, envLocalGit)
}

func validateNativeLegacyOwnedCommonRepository(
	worktreesRoot, commonDir, repo, remoteURL string,
	envExecutable string,
	envLocalGit func(...string) []string,
) error {
	expectedMarker, err := nativeLegacyOwnedCommonRepositoryMarker(repo, remoteURL)
	if err != nil {
		return err
	}
	return validateNativeCommonRepository(worktreesRoot, commonDir, repo, remoteURL, expectedMarker, envExecutable, envLocalGit)
}

func validateNativeCommonRepository(
	worktreesRoot, commonDir, repo, remoteURL, expectedMarker string,
	envExecutable string,
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
	markerPath := filepath.Join(commonDir, "devkit-owned-common")
	marker, err := os.ReadFile(markerPath)
	if err != nil {
		return fmt.Errorf("read package-owned common repository marker %s: %w", markerPath, err)
	}
	if string(marker) != expectedMarker {
		return fmt.Errorf("package-owned common repository %s identity does not match repository %s and its declared origin", commonDir, repo)
	}
	bare, err := captureNativeGit(envExecutable, envLocalGit("--git-dir", commonDir, "rev-parse", "--is-bare-repository"))
	if err != nil || bare != "true" {
		return fmt.Errorf("package-owned common repository %s is not a bare Git repository", commonDir)
	}
	origin, err := captureNativeGit(envExecutable, envLocalGit("--git-dir", commonDir, "remote", "get-url", "origin"))
	if err != nil {
		return fmt.Errorf("read package-owned common repository origin %s: %w", commonDir, err)
	}
	if origin != remoteURL {
		return fmt.Errorf("package-owned common repository %s origin %q does not match declared origin %q", commonDir, origin, remoteURL)
	}
	return nil
}

func ensureNativeOwnedCommonRepository(
	worktreesRoot, repo, remoteURL string, index int,
	envExecutable string,
	envLocalGit func(...string) []string,
	envRemoteGit func(...string) []string,
	dryRun bool,
) (string, error) {
	commonDir, err := nativeOwnedCommonRepositoryPath(worktreesRoot, repo, index)
	if err != nil {
		return "", err
	}
	marker, err := nativeOwnedCommonRepositoryMarker(repo, remoteURL, index)
	if err != nil {
		return "", err
	}
	if dryRun {
		if err := run(true, envExecutable, envLocalGit("init", "--bare", "--initial-branch=main", commonDir)...); err != nil {
			return "", err
		}
		if err := run(true, envExecutable, envLocalGit("--git-dir", commonDir, "remote", "add", "origin", remoteURL)...); err != nil {
			return "", err
		}
		if err := runFetch(true, envExecutable, envRemoteGit("--git-dir", commonDir, "fetch", "--all", "--prune", "--progress")...); err != nil {
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
		if err := validateNativeOwnedCommonRepository(worktreesRoot, commonDir, repo, remoteURL, index, envExecutable, envLocalGit); err != nil {
			return "", err
		}
		if err := runFetch(false, envExecutable, envRemoteGit("--git-dir", commonDir, "fetch", "--all", "--prune", "--progress")...); err != nil {
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
	if err := run(false, envExecutable, envLocalGit("init", "--bare", "--initial-branch=main", staging)...); err != nil {
		return "", err
	}
	if err := run(false, envExecutable, envLocalGit("--git-dir", staging, "config", "worktree.useRelativePaths", "true")...); err != nil {
		return "", err
	}
	if err := run(false, envExecutable, envLocalGit("--git-dir", staging, "remote", "add", "origin", remoteURL)...); err != nil {
		return "", err
	}
	if err := runFetch(false, envExecutable, envRemoteGit("--git-dir", staging, "fetch", "--all", "--prune", "--progress")...); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(staging, "devkit-owned-common"), []byte(marker), 0o600); err != nil {
		return "", fmt.Errorf("write package-owned common repository marker: %w", err)
	}
	if err := validateNativeOwnedCommonRepository(worktreesRoot, staging, repo, remoteURL, index, envExecutable, envLocalGit); err != nil {
		return "", err
	}
	if err := os.Rename(staging, commonDir); err != nil {
		return "", fmt.Errorf("publish package-owned common repository %s: %w", commonDir, err)
	}
	keepStaging = true
	return commonDir, nil
}

func preflightNativeOwnedWorktreeTargets(worktreesRoot, repo string, first, last int) error {
	if first < 1 || last < first {
		return fmt.Errorf("native worktree preflight range %d..%d is invalid", first, last)
	}
	for i := first; i <= last; i++ {
		laneCommonDir, err := nativeOwnedCommonRepositoryPath(worktreesRoot, repo, i)
		if err != nil {
			return err
		}
		legacyCommonDir, err := nativeLegacyOwnedCommonRepositoryPath(worktreesRoot, repo)
		if err != nil {
			return err
		}
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
		actualCommonDir, err := nativeWorktreeCommonDirectory(worktree)
		if err != nil {
			return err
		}
		canonicalLaneCommon := canonicalOrClean(laneCommonDir)
		canonicalLegacyCommon := canonicalOrClean(legacyCommonDir)
		if actualCommonDir != canonicalLaneCommon && actualCommonDir != canonicalLegacyCommon {
			return fmt.Errorf(
				"native worktree target %s points to foreign common Git directory %s; expected lane-owned %s or legacy package-owned %s",
				worktree,
				actualCommonDir,
				canonicalLaneCommon,
				canonicalLegacyCommon,
			)
		}
		ownedGitdirsRoot := filepath.Join(actualCommonDir, "worktrees")
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
		if err := nativeResetRemove(worktree); err != nil {
			// A reset may have had to preserve the exact source-declared root
			// because its parent denies replacement of that existing name (for
			// example a protected bind or sticky/foreign-owner boundary). Git
			// accepts an existing empty directory, so retain it only for these
			// narrow mutation-authority errors.
			if nativeResetRootReuseError(err) {
				return "", nil
			}
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
	DevkitRoot string
	Repo       string
	Origin     string
	// Index selects one slot when positive. A zero index preserves the existing
	// multi-slot SetupNative behavior for callers that intentionally prepare the
	// complete declared capacity.
	Index            int
	Count            int
	BaseBranch       string
	BranchPrefix     string
	WorktreeRoot     string
	GitSSHCommand    string
	RequireSSHOrigin bool
	// ReconstructSelected authorizes destructive convergence of the exact
	// Index-selected package-owned worktree. It is invalid without Index and is
	// reserved for the native slot reset lane.
	ReconstructSelected bool
	DryRun              bool
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

// NativeSlotResetOptions identifies exactly one source-declared native slot.
// The command layer derives these roots from the selected overlay; this type
// carries no arbitrary extra deletion paths.
type NativeSlotResetOptions struct {
	Project                         string
	Repo                            string
	Origin                          string
	BranchPrefix                    string
	Index                           int
	Count                           int
	WorktreeRoot                    string
	StateRoot                       string
	ProtectedRoots                  []string
	RequirePackageSourceExecutables bool
	DryRun                          bool
}

type nativeResetCandidate struct {
	path                      string
	boundary                  string
	preserveRoot              bool
	reuseRootWhenRenameDenied bool
	requireRealDirectory      bool
}

// NativeResetPlan is an opaque, preflighted disposal plan. The paths are
// deliberately not exported so a caller cannot turn validation into an
// arbitrary recursive-delete facility.
type NativeResetPlan struct {
	dryRun               bool
	candidates           []nativeResetCandidate
	protectedRoots       []string
	mountPoints          []string
	quarantineName       string
	recoverySources      map[string]string
	quarantineBoundaries []string
}

var (
	nativeResetMountPoints = readNativeResetMountPoints
	nativeResetRename      = os.Rename
	nativeResetRemove      = os.Remove
	nativeResetDiscardTree = os.RemoveAll
)

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
	if candidate.requireRealDirectory {
		info, err := os.Lstat(candidate.path)
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("native reset target %s must remain a real directory", candidate.path)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect native reset directory target %s: %w", candidate.path, err)
		}
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
		if candidate.path == mountPoint && candidate.preserveRoot {
			continue
		}
		if candidate.path == mountPoint || pathWithinRoot(candidate.path, mountPoint) {
			return fmt.Errorf("native reset target %s contains mount point %s", candidate.path, mountPoint)
		}
	}
	return nil
}

func nativeResetLegacyQuarantineName(name string) bool {
	const prefix = ".devkit-reset-"
	suffix := strings.TrimPrefix(name, prefix)
	if suffix == name || suffix == "" {
		return false
	}
	for _, char := range suffix {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func appendNativeResetQuarantineResidue(
	candidates []nativeResetCandidate,
	boundaries []string,
	quarantineName string,
) ([]nativeResetCandidate, error) {
	seen := map[string]bool{}
	for _, boundary := range boundaries {
		boundary = filepath.Clean(strings.TrimSpace(boundary))
		if boundary == "" || boundary == "." || seen[boundary] {
			continue
		}
		seen[boundary] = true
		entries, err := os.ReadDir(boundary)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("enumerate native reset quarantine residue under %s: %w", boundary, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasPrefix(name, quarantineName) && !nativeResetLegacyQuarantineName(name) {
				continue
			}
			candidates = append(candidates, nativeResetCandidate{
				path:     filepath.Join(boundary, name),
				boundary: boundary,
			})
		}
	}
	return candidates, nil
}

func nativeResetCanonicalTransactionQuarantineName(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) || len(name) != len(prefix)+32 {
		return false
	}
	for _, char := range name[len(prefix):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

// appendNativeResetSelectedQuarantineResidue admits the durable transaction
// identities produced by reserveNativeResetQuarantinePath. Every other entry
// retains caller ownership outside the whole-reset candidate set.
func appendNativeResetSelectedQuarantineResidue(
	candidates []nativeResetCandidate,
	boundaries []string,
	quarantineName string,
) ([]nativeResetCandidate, error) {
	seenBoundaries := map[string]bool{}
	for _, boundary := range boundaries {
		boundary = filepath.Clean(strings.TrimSpace(boundary))
		if boundary == "" || boundary == "." || seenBoundaries[boundary] {
			continue
		}
		seenBoundaries[boundary] = true
		if err := validateNativeResetPathComponents(boundary); err != nil {
			return nil, fmt.Errorf("validate selected native reset quarantine boundary %s: %w", boundary, err)
		}
		entries, err := os.ReadDir(boundary)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("enumerate selected native reset quarantine residue under %s: %w", boundary, err)
		}
		for _, entry := range entries {
			if !nativeResetCanonicalTransactionQuarantineName(entry.Name(), quarantineName) {
				continue
			}
			path := filepath.Join(boundary, entry.Name())
			info, err := os.Lstat(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("inspect selected native reset quarantine residue %s: %w", path, err)
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			candidates = append(candidates, nativeResetCandidate{
				path:                 path,
				boundary:             boundary,
				requireRealDirectory: true,
			})
		}
	}
	return candidates, nil
}

func deduplicateNativeResetCandidateOverlaps(candidates []nativeResetCandidate) ([]nativeResetCandidate, error) {
	result := make([]nativeResetCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		candidate.path = filepath.Clean(candidate.path)
		candidate.boundary = filepath.Clean(candidate.boundary)
		redundant := false
		for index := 0; index < len(result); {
			existing := result[index]
			if candidate.path == existing.path {
				if candidate.boundary != existing.boundary {
					return nil, fmt.Errorf(
						"native reset target %s has conflicting ownership boundaries %s and %s",
						candidate.path,
						existing.boundary,
						candidate.boundary,
					)
				}
				result[index].preserveRoot = existing.preserveRoot || candidate.preserveRoot
				result[index].reuseRootWhenRenameDenied = existing.reuseRootWhenRenameDenied || candidate.reuseRootWhenRenameDenied
				result[index].requireRealDirectory = existing.requireRealDirectory || candidate.requireRealDirectory
				redundant = true
				break
			}
			if pathWithinRoot(existing.path, candidate.path) {
				// The already-declared ancestor owns this opaque descendant.
				redundant = true
				break
			}
			if pathWithinRoot(candidate.path, existing.path) {
				// The newly-declared ancestor owns the existing opaque descendant.
				result = append(result[:index], result[index+1:]...)
				continue
			}
			index++
		}
		if !redundant {
			result = append(result, candidate)
		}
	}
	return result, nil
}

// PlanNativeReset validates the complete destructive boundary before any
// broker, session, workspace, home, or bootstrap effect occurs. Existing
// payload beneath an exact owned target is intentionally opaque: a foreign
// The .git pointer is opaque data to unlink; it never expands the reset
// boundary.
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

	candidates := make([]nativeResetCandidate, 0, count*3+2)
	commonDirs := make([]string, 0, count+1)
	for index := 1; index <= count; index++ {
		commonDir, err := nativeOwnedCommonRepositoryPath(worktreeRoot, repo, index)
		if err != nil {
			return nil, err
		}
		commonDirs = append(commonDirs, commonDir)
		candidates = append(candidates,
			nativeResetCandidate{
				path:                      filepath.Join(worktreeRoot, fmt.Sprintf("agent%d", index)),
				boundary:                  worktreeRoot,
				reuseRootWhenRenameDenied: true,
			},
			nativeResetCandidate{
				path:                      filepath.Join(stateRoot, fmt.Sprintf("%s-agent%d", opts.Project, index)),
				boundary:                  stateRoot,
				reuseRootWhenRenameDenied: true,
			},
			nativeResetCandidate{path: commonDir, boundary: worktreeRoot},
		)
		selectedQuarantineName := fmt.Sprintf(".devkit-reset-%s-agent%d-", opts.Project, index)
		candidates, err = appendNativeResetSelectedQuarantineResidue(
			candidates,
			[]string{stateRoot, filepath.Dir(commonDir)},
			selectedQuarantineName,
		)
		if err != nil {
			return nil, err
		}
	}
	legacyCommonDir, err := nativeLegacyOwnedCommonRepositoryPath(worktreeRoot, repo)
	if err != nil {
		return nil, err
	}
	commonDirs = append(commonDirs, legacyCommonDir)
	candidates = append(candidates,
		nativeResetCandidate{path: legacyCommonDir, boundary: worktreeRoot},
		nativeResetCandidate{
			path:     runtimeagent.ManifestPath(stateRoot, opts.Project),
			boundary: stateRoot,
		},
	)
	quarantineName := fmt.Sprintf(".devkit-reset-%s-all-", opts.Project)
	candidates, err = appendNativeResetQuarantineResidue(
		candidates,
		[]string{worktreeRoot, stateRoot},
		quarantineName,
	)
	if err != nil {
		return nil, err
	}
	for _, commonDir := range commonDirs {
		staging, err := filepath.Glob(filepath.Join(filepath.Dir(commonDir), "."+repo+".bootstrap-*"))
		if err != nil {
			return nil, fmt.Errorf("enumerate native reset bootstrap staging paths: %w", err)
		}
		for _, path := range staging {
			candidates = append(candidates, nativeResetCandidate{path: path, boundary: worktreeRoot})
		}
	}
	candidates, err = deduplicateNativeResetCandidateOverlaps(candidates)
	if err != nil {
		return nil, err
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
		quarantineName: quarantineName,
	}, nil
}

// PlanNativeSlotReset validates the complete selected-slot destructive
// boundary before any process, Git, home, or filesystem effect occurs. The
// selected lane's common repository is disposable with the slot; the legacy
// shared common repository and shared manifest are deliberately excluded.
func PlanNativeSlotReset(opts NativeSlotResetOptions) (*NativeResetPlan, error) {
	envExecutable, gitExecutable, err := nativeSourceExecutables(opts.RequirePackageSourceExecutables)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.Project) != "dev-all" {
		return nil, fmt.Errorf("native slot reset requires the exact dev-all prefix")
	}
	repo := strings.TrimSpace(opts.Repo)
	if opts.Count < 1 {
		return nil, fmt.Errorf("native slot reset declared count must be positive")
	}
	if opts.Index < 1 || opts.Index > opts.Count {
		return nil, fmt.Errorf("native slot reset index %d is outside declared capacity 1..%d", opts.Index, opts.Count)
	}
	if strings.TrimSpace(opts.WorktreeRoot) == "" {
		return nil, fmt.Errorf("native slot reset worktree root is required")
	}
	worktreeRoot, err := filepath.Abs(filepath.Clean(strings.TrimSpace(opts.WorktreeRoot)))
	if err != nil {
		return nil, fmt.Errorf("resolve native slot reset worktree root %s: %w", opts.WorktreeRoot, err)
	}
	if strings.TrimSpace(opts.StateRoot) == "" {
		return nil, fmt.Errorf("native slot reset state root is required")
	}
	stateRoot, err := filepath.Abs(filepath.Clean(strings.TrimSpace(opts.StateRoot)))
	if err != nil {
		return nil, fmt.Errorf("resolve native slot reset state root %s: %w", opts.StateRoot, err)
	}
	if err := validateNativeResetPathComponents(worktreeRoot); err != nil {
		return nil, err
	}
	if err := validateNativeResetPathComponents(stateRoot); err != nil {
		return nil, err
	}

	agentRoot := filepath.Join(worktreeRoot, fmt.Sprintf("agent%d", opts.Index))
	worktree := filepath.Join(agentRoot, repo)
	selectedState := filepath.Join(stateRoot, fmt.Sprintf("%s-agent%d", opts.Project, opts.Index))
	commonDir, err := nativeOwnedCommonRepositoryPath(worktreeRoot, repo, opts.Index)
	if err != nil {
		return nil, err
	}
	commonBoundary := filepath.Dir(commonDir)
	candidates := []nativeResetCandidate{
		{path: worktree, boundary: agentRoot, reuseRootWhenRenameDenied: true},
		{
			path:                      selectedState,
			boundary:                  stateRoot,
			reuseRootWhenRenameDenied: true,
		},
		{path: commonDir, boundary: commonBoundary},
	}
	recoverySources := map[string]string{
		worktree:      agentRoot,
		selectedState: stateRoot,
		commonDir:     commonBoundary,
	}
	quarantineName := fmt.Sprintf(".devkit-reset-%s-agent%d-", opts.Project, opts.Index)
	quarantineBoundaries := []string{agentRoot, stateRoot, commonBoundary}
	// Agent 1's home is inside its worktree. Later slots keep the home beside
	// the worktree, so select that exact directory without deleting the shared
	// agent root or any other repository beneath it.
	if opts.Index > 1 {
		hostHome := filepath.Join(agentRoot, fmt.Sprintf(".devhome-agent%d", opts.Index))
		candidates = append(candidates, nativeResetCandidate{
			path:                      hostHome,
			boundary:                  agentRoot,
			reuseRootWhenRenameDenied: true,
		})
		recoverySources[hostHome] = agentRoot
	}

	if info, statErr := os.Lstat(commonDir); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("package-owned common repository %s must be a real directory", commonDir)
		}
		origin := strings.TrimSpace(opts.Origin)
		if origin == "" {
			return nil, fmt.Errorf("native slot reset requires the source-declared origin to validate existing common Git metadata")
		}
		envLocalGit := func(args ...string) []string {
			return append([]string{"-u", "GIT_SSH_COMMAND", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", gitExecutable}, args...)
		}
		if err := validateNativeOwnedCommonRepository(worktreeRoot, commonDir, repo, origin, opts.Index, envExecutable, envLocalGit); err != nil {
			return nil, err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect package-owned common repository %s: %w", commonDir, statErr)
	}
	staging, err := filepath.Glob(filepath.Join(commonBoundary, "."+repo+".bootstrap-*"))
	if err != nil {
		return nil, fmt.Errorf("enumerate selected native reset bootstrap staging paths: %w", err)
	}
	for _, path := range staging {
		candidates = append(candidates, nativeResetCandidate{path: path, boundary: commonBoundary})
		recoverySources[path] = commonBoundary
	}
	candidates, err = appendNativeResetQuarantineResidue(candidates, quarantineBoundaries, quarantineName)
	if err != nil {
		return nil, err
	}

	mountPoints, err := nativeResetMountPoints()
	if err != nil {
		return nil, err
	}
	for index := range candidates {
		if candidates[index].path == selectedState {
			for _, mountPoint := range mountPoints {
				if filepath.Clean(strings.TrimSpace(mountPoint)) == selectedState {
					candidates[index].preserveRoot = true
					break
				}
			}
		}
		candidate := candidates[index]
		if err := validateNativeResetCandidate(candidate, opts.ProtectedRoots, mountPoints); err != nil {
			return nil, err
		}
	}
	return &NativeResetPlan{
		dryRun:               opts.DryRun,
		candidates:           candidates,
		protectedRoots:       append([]string(nil), opts.ProtectedRoots...),
		mountPoints:          append([]string(nil), mountPoints...),
		quarantineName:       quarantineName,
		recoverySources:      recoverySources,
		quarantineBoundaries: append([]string(nil), quarantineBoundaries...),
	}, nil
}

func removeNativeResetQuarantine(path string) error {
	// Go and SBT caches intentionally make package directories read-only. They
	// remain disposable state, so restore owner traversal/write permission on
	// real directories without following payload symlinks before unlinking the
	// already-quarantined tree.
	if err := filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil
		}
		mode := info.Mode().Perm()
		if mode&0o700 != 0o700 {
			if err := os.Chmod(current, mode|0o700); err != nil {
				return fmt.Errorf("make native reset directory removable %s: %w", current, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := nativeResetDiscardTree(path); err != nil {
		return err
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("native reset quarantine still exists after removal: %s", path)
	}
	return nil
}

type stagedNativeResetPath struct {
	source string
	target string
}

// NativeResetStagedPath is the typed, serializable identity of one validated
// reset-owned name moved into quarantine. Callers may persist this state for
// crash recovery, but Resume revalidates every path against the opaque plan
// before any cleanup effect is permitted.
type NativeResetStagedPath struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// NativeResetQuarantine records one plan-owned quarantine directory and the
// exact source-derived boundary under which it was created.
type NativeResetQuarantine struct {
	Boundary string `json:"boundary"`
	Path     string `json:"path"`
}

// NativeResetTransactionState is sufficient to finish or roll back an
// interrupted two-phase native reset without expanding its validated paths.
type NativeResetTransactionState struct {
	Staged      []NativeResetStagedPath `json:"staged"`
	Quarantines []NativeResetQuarantine `json:"quarantines"`
}

// NativeResetTransaction separates removal of active names from irreversible
// quarantine cleanup. Manifest callers can therefore install their CAS only
// after every slot has staged successfully.
type NativeResetTransaction struct {
	plan     *NativeResetPlan
	state    NativeResetTransactionState
	closed   bool
	prepared bool
}

func cloneNativeResetTransactionState(state NativeResetTransactionState) NativeResetTransactionState {
	return NativeResetTransactionState{
		Staged:      append([]NativeResetStagedPath(nil), state.Staged...),
		Quarantines: append([]NativeResetQuarantine(nil), state.Quarantines...),
	}
}

// State returns a copy suitable for a typed recovery receipt.
func (transaction *NativeResetTransaction) State() NativeResetTransactionState {
	if transaction == nil {
		return NativeResetTransactionState{}
	}
	return cloneNativeResetTransactionState(transaction.state)
}

func reserveNativeResetQuarantinePath(
	boundary string,
	quarantineName string,
	reserved map[string]bool,
) (string, error) {
	for attempt := 0; attempt < 32; attempt++ {
		entropy := make([]byte, 16)
		if _, err := rand.Read(entropy); err != nil {
			return "", fmt.Errorf("generate native reset quarantine identity: %w", err)
		}
		path := filepath.Join(boundary, quarantineName+hex.EncodeToString(entropy))
		if reserved[path] {
			continue
		}
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			reserved[path] = true
			return path, nil
		} else if err != nil {
			return "", fmt.Errorf("inspect proposed native reset quarantine %s: %w", path, err)
		}
	}
	return "", fmt.Errorf("allocate unique native reset quarantine identity under %s", boundary)
}

func createNativeResetQuarantinePath(boundary, quarantineName string) (string, error) {
	reserved := make(map[string]bool)
	for attempt := 0; attempt < 32; attempt++ {
		path, err := reserveNativeResetQuarantinePath(boundary, quarantineName, reserved)
		if err != nil {
			return "", err
		}
		if err := os.Mkdir(path, 0o700); err == nil {
			return path, nil
		} else if errors.Is(err, os.ErrExist) {
			continue
		} else {
			return "", fmt.Errorf("create native reset quarantine %s: %w", path, err)
		}
	}
	return "", fmt.Errorf("create unique native reset quarantine under %s", boundary)
}

// PrepareTransaction computes and validates the complete source-to-quarantine
// mapping without creating a directory or renaming a path. A caller can durably
// persist State before StagePrepared performs the first filesystem mutation.
// This strict mode is intentionally limited to source-derived selected-slot
// plans and refuses mount-preserving or stale-quarantine fallback behavior.
func (plan *NativeResetPlan) PrepareTransaction() (*NativeResetTransaction, error) {
	if plan == nil {
		return nil, fmt.Errorf("native reset plan is required")
	}
	if plan.dryRun {
		return nil, fmt.Errorf("durable native reset preparation is unavailable in dry-run mode")
	}
	if len(plan.recoverySources) == 0 || plan.quarantineName == "" {
		return nil, fmt.Errorf("durable native reset preparation requires a source-derived selected-slot plan")
	}
	for _, candidate := range plan.candidates {
		if err := validateNativeResetCandidate(candidate, plan.protectedRoots, plan.mountPoints); err != nil {
			return nil, fmt.Errorf("revalidate native reset boundary before durable preparation: %w", err)
		}
		if candidate.preserveRoot {
			return nil, fmt.Errorf("durable native reset preparation refuses a mount-preserving reset root: %s", candidate.path)
		}
		if strings.HasPrefix(filepath.Base(candidate.path), plan.quarantineName) {
			return nil, fmt.Errorf("durable native reset preparation refuses pre-existing quarantine residue: %s", candidate.path)
		}
		if boundary, ok := plan.recoverySources[candidate.path]; !ok || boundary != candidate.boundary {
			return nil, fmt.Errorf("durable native reset preparation found an undeclared recovery source: %s", candidate.path)
		}
	}

	quarantineByBoundary := make(map[string]string)
	reserved := make(map[string]bool)
	state := NativeResetTransactionState{}
	for _, candidate := range plan.candidates {
		info, err := os.Lstat(candidate.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect native reset source before durable preparation %s: %w", candidate.path, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("durable native reset source must be a real directory: %s", candidate.path)
		}
		quarantine := quarantineByBoundary[candidate.boundary]
		if quarantine == "" {
			quarantine, err = reserveNativeResetQuarantinePath(candidate.boundary, plan.quarantineName, reserved)
			if err != nil {
				return nil, err
			}
			quarantineByBoundary[candidate.boundary] = quarantine
		}
		state.Staged = append(state.Staged, NativeResetStagedPath{
			Source: candidate.path,
			Target: filepath.Join(quarantine, fmt.Sprintf("%03d", len(state.Staged))),
		})
	}
	boundaries := make([]string, 0, len(quarantineByBoundary))
	for boundary := range quarantineByBoundary {
		boundaries = append(boundaries, boundary)
	}
	sort.Strings(boundaries)
	for _, boundary := range boundaries {
		state.Quarantines = append(state.Quarantines, NativeResetQuarantine{
			Boundary: boundary,
			Path:     quarantineByBoundary[boundary],
		})
	}
	return &NativeResetTransaction{
		plan:     plan,
		state:    state,
		prepared: true,
	}, nil
}

func syncNativeResetDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func syncNativeResetTransactionBoundaries(state NativeResetTransactionState) error {
	paths := make(map[string]bool)
	for _, quarantine := range state.Quarantines {
		paths[quarantine.Boundary] = true
	}
	for _, item := range state.Staged {
		paths[filepath.Dir(item.Source)] = true
	}
	var result error
	for path := range paths {
		if err := syncNativeResetDirectory(path); err != nil {
			result = errors.Join(result, fmt.Errorf("sync native reset transaction boundary %s: %w", path, err))
		}
	}
	return result
}

// StagePrepared performs exactly the already-persistable mapping returned by
// PrepareTransaction. It never invents a fallback source or target after the
// durable intent has been emitted.
func (transaction *NativeResetTransaction) StagePrepared() error {
	if transaction == nil || transaction.plan == nil {
		return fmt.Errorf("native reset transaction is required")
	}
	if !transaction.prepared {
		return fmt.Errorf("native reset transaction was not prepared before staging")
	}
	if transaction.closed {
		return fmt.Errorf("native reset transaction is already closed")
	}
	for _, candidate := range transaction.plan.candidates {
		if err := validateNativeResetCandidate(candidate, transaction.plan.protectedRoots, transaction.plan.mountPoints); err != nil {
			return fmt.Errorf("revalidate native reset boundary before prepared staging: %w", err)
		}
	}
	for _, quarantine := range transaction.state.Quarantines {
		if err := os.Mkdir(quarantine.Path, 0o700); err != nil {
			return fmt.Errorf("create prepared native reset quarantine %s: %w", quarantine.Path, err)
		}
	}
	for _, item := range transaction.state.Staged {
		if !nativeResetPlanAllowsSource(transaction.plan, item.Source, item.Target) {
			return fmt.Errorf("prepared native reset source is outside the reconstructed plan: %s", item.Source)
		}
		info, err := os.Lstat(item.Source)
		if err != nil {
			return fmt.Errorf("inspect prepared native reset source %s: %w", item.Source, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("prepared native reset source must remain a real directory: %s", item.Source)
		}
		if _, err := os.Lstat(item.Target); err == nil {
			return fmt.Errorf("prepared native reset target already exists: %s", item.Target)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect prepared native reset target %s: %w", item.Target, err)
		}
		if err := nativeResetRename(item.Source, item.Target); err != nil {
			return fmt.Errorf("stage prepared native reset source %s: %w", item.Source, err)
		}
	}
	syncPaths := make(map[string]bool)
	for _, quarantine := range transaction.state.Quarantines {
		syncPaths[quarantine.Boundary] = true
		syncPaths[quarantine.Path] = true
	}
	for _, item := range transaction.state.Staged {
		syncPaths[filepath.Dir(item.Source)] = true
	}
	for path := range syncPaths {
		if err := syncNativeResetDirectory(path); err != nil {
			return fmt.Errorf("sync prepared native reset directory %s: %w", path, err)
		}
	}
	return nil
}

func nativeResetPathState(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Rollback restores every staged active name in reverse order. It is
// idempotent after a complete rollback and fails closed on ambiguous
// source/target pairs.
func (transaction *NativeResetTransaction) Rollback() error {
	if transaction == nil {
		return fmt.Errorf("native reset transaction is required")
	}
	if transaction.closed {
		return nil
	}
	if transaction.prepared {
		if _, err := ResumeNativeResetTransaction(transaction.plan, transaction.state); err != nil {
			return fmt.Errorf("revalidate prepared native reset transaction before rollback: %w", err)
		}
	}
	var result error
	for index := len(transaction.state.Staged) - 1; index >= 0; index-- {
		item := transaction.state.Staged[index]
		sourceExists, sourceErr := nativeResetPathState(item.Source)
		targetExists, targetErr := nativeResetPathState(item.Target)
		if sourceErr != nil || targetErr != nil {
			result = errors.Join(result, sourceErr, targetErr)
			continue
		}
		switch {
		case sourceExists && !targetExists:
			continue
		case !sourceExists && targetExists:
			if err := nativeResetRename(item.Target, item.Source); err != nil {
				result = errors.Join(result, fmt.Errorf("restore native reset target %s: %w", item.Source, err))
			}
		case sourceExists && targetExists:
			result = errors.Join(result, fmt.Errorf("native reset rollback is ambiguous because source and quarantine both exist: %s", item.Source))
		default:
			result = errors.Join(result, fmt.Errorf("native reset rollback cannot find source or quarantine for %s", item.Source))
		}
	}
	for _, quarantine := range transaction.state.Quarantines {
		if err := nativeResetRemove(quarantine.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("remove empty native reset quarantine %s: %w", quarantine.Path, err))
		}
	}
	if result == nil {
		result = syncNativeResetTransactionBoundaries(transaction.state)
	}
	if result == nil {
		transaction.closed = true
	}
	return result
}

// Commit irreversibly discards only the previously staged quarantine trees.
// Missing trees are accepted so cleanup can be resumed idempotently after a
// partial post-CAS failure.
func (transaction *NativeResetTransaction) Commit() error {
	if transaction == nil {
		return fmt.Errorf("native reset transaction is required")
	}
	if transaction.closed {
		return nil
	}
	if transaction.prepared {
		if _, err := ResumeNativeResetTransaction(transaction.plan, transaction.state); err != nil {
			return fmt.Errorf("revalidate prepared native reset transaction before commit: %w", err)
		}
	}
	var result error
	for _, quarantine := range transaction.state.Quarantines {
		exists, err := nativeResetPathState(quarantine.Path)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("inspect native reset quarantine %s: %w", quarantine.Path, err))
			continue
		}
		if !exists {
			continue
		}
		if err := removeNativeResetQuarantine(quarantine.Path); err != nil {
			result = errors.Join(result, fmt.Errorf("discard native reset quarantine %s: %w", quarantine.Path, err))
		}
	}
	if result == nil {
		result = syncNativeResetTransactionBoundaries(transaction.state)
	}
	if result == nil {
		transaction.closed = true
	}
	return result
}

func nativeResetPlanAllowsSource(plan *NativeResetPlan, source, target string) bool {
	if plan == nil {
		return false
	}
	if boundary, ok := plan.recoverySources[source]; ok {
		return filepath.Dir(filepath.Dir(target)) == boundary
	}
	return false
}

func nativeResetPlanAllowsBoundary(plan *NativeResetPlan, boundary string) bool {
	for _, candidate := range plan.candidates {
		if boundary == candidate.boundary || (candidate.preserveRoot && boundary == candidate.path) {
			return true
		}
	}
	return false
}

func nativeResetPlanOwnsQuarantine(plan *NativeResetPlan, boundary, path string) bool {
	if plan == nil || plan.quarantineName == "" || filepath.Dir(path) != boundary ||
		!nativeResetCanonicalTransactionQuarantineName(filepath.Base(path), plan.quarantineName) {
		return false
	}
	for _, allowed := range plan.quarantineBoundaries {
		if boundary == allowed {
			return true
		}
	}
	return false
}

// ResumeNativeResetTransaction validates persisted transaction state against
// the freshly reconstructed opaque plan. Persisted JSON alone can never grant
// authority to remove an arbitrary path.
func ResumeNativeResetTransaction(
	plan *NativeResetPlan,
	state NativeResetTransactionState,
) (*NativeResetTransaction, error) {
	if plan == nil {
		return nil, fmt.Errorf("native reset plan is required")
	}
	quarantineName := plan.quarantineName
	if quarantineName == "" {
		quarantineName = ".devkit-reset-"
	}
	quarantines := make(map[string]NativeResetQuarantine, len(state.Quarantines))
	currentMountPoints, err := nativeResetMountPoints()
	if err != nil {
		return nil, fmt.Errorf("refresh native reset mount inventory for recovery: %w", err)
	}
	for _, quarantine := range state.Quarantines {
		boundary := filepath.Clean(strings.TrimSpace(quarantine.Boundary))
		path := filepath.Clean(strings.TrimSpace(quarantine.Path))
		if boundary == "" || path == "" || !filepath.IsAbs(boundary) || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("native reset recovery quarantine paths must be absolute")
		}
		if boundary != quarantine.Boundary || path != quarantine.Path {
			return nil, fmt.Errorf("native reset recovery quarantine paths must be canonical")
		}
		if !nativeResetPlanAllowsBoundary(plan, boundary) ||
			!nativeResetPlanOwnsQuarantine(plan, boundary, path) ||
			!nativeResetCanonicalTransactionQuarantineName(filepath.Base(path), quarantineName) {
			return nil, fmt.Errorf("native reset recovery quarantine is outside the reconstructed plan: %s", path)
		}
		if err := validateNativeResetCandidate(
			nativeResetCandidate{path: path, boundary: boundary},
			plan.protectedRoots,
			currentMountPoints,
		); err != nil {
			return nil, fmt.Errorf("revalidate native reset recovery quarantine %s: %w", path, err)
		}
		if _, duplicate := quarantines[path]; duplicate {
			return nil, fmt.Errorf("native reset recovery repeats quarantine %s", path)
		}
		if info, err := os.Lstat(path); err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("native reset recovery quarantine must be a real directory: %s", path)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect native reset recovery quarantine %s: %w", path, err)
		}
		quarantines[path] = NativeResetQuarantine{Boundary: boundary, Path: path}
	}
	seenSources := make(map[string]struct{}, len(state.Staged))
	seenTargets := make(map[string]struct{}, len(state.Staged))
	targetsByQuarantine := make(map[string]map[string]bool, len(quarantines))
	for index, item := range state.Staged {
		source := filepath.Clean(strings.TrimSpace(item.Source))
		target := filepath.Clean(strings.TrimSpace(item.Target))
		if source == "" || target == "" || !filepath.IsAbs(source) || !filepath.IsAbs(target) {
			return nil, fmt.Errorf("native reset recovery staged paths must be absolute")
		}
		if source != item.Source || target != item.Target {
			return nil, fmt.Errorf("native reset recovery staged paths must be canonical")
		}
		if !nativeResetPlanAllowsSource(plan, source, target) {
			return nil, fmt.Errorf("native reset recovery source is outside the reconstructed plan: %s", source)
		}
		quarantinePath := filepath.Dir(target)
		if _, ok := quarantines[quarantinePath]; !ok {
			return nil, fmt.Errorf("native reset recovery target is outside a declared quarantine: %s", target)
		}
		if filepath.Base(target) != fmt.Sprintf("%03d", index) {
			return nil, fmt.Errorf("native reset recovery target sequence mismatch: %s", target)
		}
		if _, duplicate := seenSources[source]; duplicate {
			return nil, fmt.Errorf("native reset recovery repeats source %s", source)
		}
		if _, duplicate := seenTargets[target]; duplicate {
			return nil, fmt.Errorf("native reset recovery repeats target %s", target)
		}
		seenSources[source] = struct{}{}
		seenTargets[target] = struct{}{}
		if targetsByQuarantine[quarantinePath] == nil {
			targetsByQuarantine[quarantinePath] = make(map[string]bool)
		}
		targetsByQuarantine[quarantinePath][target] = true

		sourceExists, sourceErr := nativeResetPathState(source)
		targetExists, targetErr := nativeResetPathState(target)
		if sourceErr != nil || targetErr != nil {
			return nil, errors.Join(sourceErr, targetErr)
		}
		if sourceExists && targetExists {
			return nil, fmt.Errorf("native reset recovery source and target both exist: %s", source)
		}
		if sourceExists {
			info, err := os.Lstat(source)
			if err != nil {
				return nil, fmt.Errorf("inspect native reset recovery source %s: %w", source, err)
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("native reset recovery source must be a real directory: %s", source)
			}
		}
	}
	for quarantinePath := range quarantines {
		entries, err := os.ReadDir(quarantinePath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("enumerate native reset recovery quarantine %s: %w", quarantinePath, err)
		}
		for _, entry := range entries {
			path := filepath.Join(quarantinePath, entry.Name())
			if !targetsByQuarantine[quarantinePath][path] {
				return nil, fmt.Errorf("native reset recovery quarantine contains undeclared payload: %s", path)
			}
			info, err := os.Lstat(path)
			if err != nil {
				return nil, fmt.Errorf("inspect native reset recovery payload %s: %w", path, err)
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("native reset recovery payload must be a real directory: %s", path)
			}
		}
	}
	return &NativeResetTransaction{
		plan:     plan,
		state:    cloneNativeResetTransactionState(state),
		prepared: true,
	}, nil
}

func nativeResetRootReuseError(err error) bool {
	return errors.Is(err, os.ErrPermission) ||
		errors.Is(err, syscall.EBUSY) ||
		errors.Is(err, syscall.EXDEV)
}

func stageNativeResetDirectoryContents(
	plan *NativeResetPlan,
	candidate nativeResetCandidate,
	staged []stagedNativeResetPath,
) (string, []stagedNativeResetPath, error) {
	// The root may be a source-declared mount or a directory whose parent
	// permits new names but not replacement of this existing name. Revalidate
	// immediately before entering it so a symlink/path swap cannot expand the
	// disposal boundary.
	if err := validateNativeResetCandidate(candidate, plan.protectedRoots, plan.mountPoints); err != nil {
		return "", staged, fmt.Errorf("revalidate native reset reusable root: %w", err)
	}
	info, err := os.Lstat(candidate.path)
	if err != nil {
		return "", staged, fmt.Errorf("inspect native reset reusable root %s: %w", candidate.path, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", staged, fmt.Errorf("native reset reusable root %s must be a real directory", candidate.path)
	}
	quarantineName := plan.quarantineName
	if quarantineName == "" {
		quarantineName = ".devkit-reset-"
	}
	quarantine, err := createNativeResetQuarantinePath(candidate.path, quarantineName)
	if err != nil {
		return "", staged, fmt.Errorf("create native reset quarantine inside reusable root %s: %w", candidate.path, err)
	}
	entries, err := os.ReadDir(candidate.path)
	if err != nil {
		_ = nativeResetRemove(quarantine)
		return "", staged, fmt.Errorf("enumerate native reset reusable root %s: %w", candidate.path, err)
	}
	for _, entry := range entries {
		source := filepath.Join(candidate.path, entry.Name())
		if source == quarantine {
			continue
		}
		target := filepath.Join(quarantine, fmt.Sprintf("%03d", len(staged)))
		if err := nativeResetRename(source, target); err != nil {
			return quarantine, staged, fmt.Errorf("stage native reset reusable-root content %s: %w", source, err)
		}
		staged = append(staged, stagedNativeResetPath{source: source, target: target})
	}
	return quarantine, staged, nil
}

// Stage atomically removes the validated active names without discarding their
// contents. A staging failure restores every name already moved.
func (plan *NativeResetPlan) Stage() (*NativeResetTransaction, error) {
	if plan == nil {
		return nil, fmt.Errorf("native reset plan is required")
	}
	for _, candidate := range plan.candidates {
		if err := validateNativeResetCandidate(candidate, plan.protectedRoots, plan.mountPoints); err != nil {
			return nil, fmt.Errorf("revalidate native reset boundary before disposal: %w", err)
		}
	}
	if plan.dryRun {
		for _, candidate := range plan.candidates {
			if candidate.preserveRoot {
				fmt.Fprintf(os.Stderr, "+ reset-owned-contents %s\n", candidate.path)
			} else {
				fmt.Fprintf(os.Stderr, "+ reset-owned %s\n", candidate.path)
			}
		}
		return &NativeResetTransaction{plan: plan}, nil
	}
	quarantines := map[string]string{}
	var staged []stagedNativeResetPath
	transaction := &NativeResetTransaction{plan: plan}
	refreshState := func() {
		transaction.state.Staged = make([]NativeResetStagedPath, 0, len(staged))
		for _, item := range staged {
			transaction.state.Staged = append(transaction.state.Staged, NativeResetStagedPath{Source: item.source, Target: item.target})
		}
		transaction.state.Quarantines = transaction.state.Quarantines[:0]
		boundaries := make([]string, 0, len(quarantines))
		for boundary := range quarantines {
			boundaries = append(boundaries, boundary)
		}
		sort.Strings(boundaries)
		for _, boundary := range boundaries {
			transaction.state.Quarantines = append(transaction.state.Quarantines, NativeResetQuarantine{
				Boundary: boundary,
				Path:     quarantines[boundary],
			})
		}
	}
	fail := func(cause error) (*NativeResetTransaction, error) {
		refreshState()
		rollbackErr := transaction.Rollback()
		return nil, errors.Join(cause, rollbackErr)
	}
	for _, candidate := range plan.candidates {
		info, err := os.Lstat(candidate.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fail(fmt.Errorf("inspect native reset target %s: %w", candidate.path, err))
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fail(fmt.Errorf("native reset target %s became a symlink or junction", candidate.path))
		}
		if candidate.requireRealDirectory && !info.IsDir() {
			return fail(fmt.Errorf("native reset target %s ceased to be a real directory", candidate.path))
		}
		if candidate.preserveRoot {
			quarantine, updated, stageErr := stageNativeResetDirectoryContents(plan, candidate, staged)
			staged = updated
			if quarantine != "" {
				quarantines[candidate.path] = quarantine
			}
			if stageErr != nil {
				return fail(fmt.Errorf("stage mounted native reset target %s: %w", candidate.path, stageErr))
			}
			continue
		}
		quarantine := quarantines[candidate.boundary]
		if quarantine == "" {
			quarantineName := plan.quarantineName
			if quarantineName == "" {
				quarantineName = ".devkit-reset-"
			}
			quarantine, err = createNativeResetQuarantinePath(candidate.boundary, quarantineName)
			if err != nil {
				return fail(fmt.Errorf("create native reset quarantine under %s: %w", candidate.boundary, err))
			}
			quarantines[candidate.boundary] = quarantine
		}
		target := filepath.Join(quarantine, fmt.Sprintf("%03d", len(staged)))
		if err := nativeResetRename(candidate.path, target); err != nil {
			if candidate.reuseRootWhenRenameDenied && info.IsDir() && nativeResetRootReuseError(err) {
				quarantine, updated, reclaimErr := stageNativeResetDirectoryContents(plan, candidate, staged)
				staged = updated
				if quarantine != "" {
					quarantines[candidate.path] = quarantine
				}
				if reclaimErr == nil {
					continue
				}
				return fail(errors.Join(
					fmt.Errorf("stage native reset target %s: %w", candidate.path, err),
					fmt.Errorf("reclaim exact native reset reusable root %s: %w", candidate.path, reclaimErr),
				))
			}
			return fail(fmt.Errorf("stage native reset target %s: %w", candidate.path, err))
		}
		staged = append(staged, stagedNativeResetPath{source: candidate.path, target: target})
	}
	refreshState()
	return transaction, nil
}

// Apply preserves the one-call reset API while using the same two-phase
// transaction used by manifest shrink.
func (plan *NativeResetPlan) Apply() error {
	transaction, err := plan.Stage()
	if err != nil {
		return err
	}
	return transaction.Commit()
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
	first, last := 1, count
	if opts.Index > 0 {
		if opts.Index > count {
			return fmt.Errorf("native slot index %d is outside declared capacity 1..%d", opts.Index, count)
		}
		first, last = opts.Index, opts.Index
	}
	return preflightNativeOwnedWorktreeTargets(worktreesRoot, repo, first, last)
}

// SetupNative creates dedicated worktrees for every native agent, including
// agent1, without changing the primary checkout's current branch. When Index
// is positive, only that slot is materialized while Count remains the declared
// capacity used to validate the selection.
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
	if opts.ReconstructSelected && opts.Index < 1 {
		return fmt.Errorf("selected native worktree reconstruction requires an explicit slot index")
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
	validationIndex := 1
	if opts.Index > 0 {
		validationIndex = opts.Index
	}
	if _, err := nativeOwnedCommonRepositoryPath(worktreesRoot, repo, validationIndex); err != nil {
		return err
	}
	repoPath := filepath.Join(devRoot, repo)
	gitSSHCommand := strings.TrimSpace(opts.GitSSHCommand)
	envExecutable, gitExecutable, err := nativeSourceExecutables(opts.RequireSSHOrigin)
	if err != nil {
		return err
	}
	envLocalGit := func(args ...string) []string {
		return append([]string{"-u", "GIT_SSH_COMMAND", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", gitExecutable}, args...)
	}
	envRemoteGit := func(args ...string) []string {
		prefix := []string{}
		if gitSSHCommand != "" {
			prefix = append(prefix, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_SSH_COMMAND="+gitSSHCommand)
		} else {
			prefix = append(prefix, "-u", "GIT_SSH_COMMAND", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		}
		return append(prefix, append([]string{gitExecutable}, args...)...)
	}
	remoteURL := strings.TrimSpace(opts.Origin)
	result := execx.Result{Code: 0}
	if remoteURL == "" {
		if opts.RequireSSHOrigin {
			return fmt.Errorf("native Git bootstrap requires the source-declared SSH origin; ambient checkout remotes are not bootstrap authority")
		}
		remoteURL, result = execx.Capture(context.Background(), gitExecutable, "-C", repoPath, "remote", "get-url", "origin")
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
	first, last := 1, count
	if opts.Index > 0 {
		if opts.Index > count {
			return fmt.Errorf("native slot index %d is outside declared capacity 1..%d", opts.Index, count)
		}
		first, last = opts.Index, opts.Index
	}
	for i := first; i <= last; i++ {
		repoCommonDir, err := nativeOwnedCommonRepositoryPath(worktreesRoot, repo, i)
		if err != nil {
			return err
		}
		legacyCommonDir, err := nativeLegacyOwnedCommonRepositoryPath(worktreesRoot, repo)
		if err != nil {
			return err
		}
		parent := filepath.Join(worktreesRoot, fmt.Sprintf("agent%d", i))
		if !opts.DryRun {
			_ = os.MkdirAll(parent, 0o755)
		}
		wt := filepath.Join(parent, repo)
		branch := fmt.Sprintf("%s%d", branchPrefix, i)
		allowedHome := fmt.Sprintf(".devhome-agent%d", i)
		if !opts.DryRun {
			if ok, err := existingGitCheckout(wt, gitExecutable); err != nil {
				return err
			} else if ok {
				actualCommonDir, err := nativeWorktreeCommonDirectory(wt)
				if err != nil {
					return err
				}
				if actualCommonDir == canonicalOrClean(legacyCommonDir) {
					if err := validateNativeLegacyOwnedCommonRepository(
						worktreesRoot,
						legacyCommonDir,
						repo,
						remoteURL,
						envExecutable,
						envLocalGit,
					); err != nil {
						return err
					}
					if opts.ReconstructSelected {
						return fmt.Errorf(
							"selected native lane agent%d still uses the legacy shared common Git repository; run the selected-slot reset before reconstruction",
							i,
						)
					}
					// Migration is slot-scoped. An active legacy lane remains exactly
					// where it is until its own selected-slot reset creates the v2
					// lane repository; routine setup preserves shared refs and locks
					// byte-for-byte under the legacy lane's custody.
					continue
				}
				if actualCommonDir != canonicalOrClean(repoCommonDir) {
					return fmt.Errorf("native worktree %s common Git directory %s does not match lane-owned %s", wt, actualCommonDir, repoCommonDir)
				}
				repoCommonDir, err = ensureNativeOwnedCommonRepository(
					worktreesRoot,
					repo,
					remoteURL,
					i,
					envExecutable,
					envLocalGit,
					envRemoteGit,
					false,
				)
				if err != nil {
					return err
				}
				if err := run(false, envExecutable, envLocalGit("--git-dir", repoCommonDir, "config", "worktree.useRelativePaths", "true")...); err != nil {
					return err
				}
				if err := run(false, envExecutable, envLocalGit("-C", wt, "config", "worktree.useRelativePaths", "true")...); err != nil {
					return err
				}
				if err := rewriteNativeGitdir(wt, worktreesRoot, repoCommonDir); err != nil {
					return err
				}
				if opts.ReconstructSelected {
					if err := reconstructSelectedNativeWorktree(
						wt,
						branch,
						"origin/"+baseBranch,
						envExecutable,
						envLocalGit,
						gitExecutable,
					); err != nil {
						return err
					}
				}
				continue
			}
		}
		repoCommonDir, err = ensureNativeOwnedCommonRepository(
			worktreesRoot,
			repo,
			remoteURL,
			i,
			envExecutable,
			envLocalGit,
			envRemoteGit,
			opts.DryRun,
		)
		if err != nil {
			return err
		}
		if err := run(opts.DryRun, envExecutable, envLocalGit("--git-dir", repoCommonDir, "config", "worktree.useRelativePaths", "true")...); err != nil {
			return err
		}
		stagingDir := ""
		if !opts.DryRun {
			stagingDir, err = stageNativeWorktreePayload(wt, allowedHome)
			if err != nil {
				return err
			}
		}
		_ = run(opts.DryRun, envExecutable, envLocalGit("--git-dir", repoCommonDir, "worktree", "prune")...)
		if err := run(opts.DryRun, envExecutable, envLocalGit("--git-dir", repoCommonDir, "worktree", "add", wt, "-B", branch, "origin/"+baseBranch)...); err != nil {
			if opts.DryRun {
				return err
			}
			_ = run(false, envExecutable, envLocalGit("--git-dir", repoCommonDir, "worktree", "remove", "-f", wt)...)
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
		if err := ensureNativeLinkedWorktreeNonBare(wt, envExecutable, envLocalGit, opts.DryRun); err != nil {
			return err
		}
		if err := run(opts.DryRun, envExecutable, envLocalGit("-C", wt, "branch", "--set-upstream-to=origin/"+baseBranch, branch)...); err != nil {
			return err
		}
		if !opts.DryRun {
			if err := VerifyFreshNativeWorktree(wt, branch, "origin/"+baseBranch, gitExecutable); err != nil {
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

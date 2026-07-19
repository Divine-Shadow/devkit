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
)

// run runs a host command with timeout and prints when DEVKIT_DEBUG=1.
func run(dry bool, name string, args ...string) error {
	if dry {
		fmt.Fprintf(os.Stderr, "+ %s\n", strings.Join(append([]string{name}, args...), " "))
		return nil
	}
	ctx, cancel := execx.WithTimeout(10_000_000_000) // 10s default; outer callers usually wrap
	defer cancel()
	res := execx.RunCtx(ctx, name, args...)
	if res.Code != 0 {
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

// rewriteNativeGitdir makes linked-worktree metadata portable whenever the
// common Git repository lives under the canonical dev root. That preserves the
// relative topology when the dev root is projected from its host path to
// /workspaces/dev, including for nested worktrees created later by Codex.
//
// A linked source checkout can legitimately use a common Git directory outside
// the dev root. Keep that exceptional path absolute because there is no
// canonical dev-root-relative projection for it; the narrow runtime metadata
// bind remains responsible for making that external common directory visible.
func rewriteNativeGitdir(wt, devRoot string) error {
	gitFile := filepath.Join(wt, ".git")
	if info, err := os.Stat(gitFile); err == nil && info.IsDir() {
		return nil
	}
	out, res := execx.Capture(context.Background(), "git", "-C", wt, "rev-parse", "--git-dir")
	if res.Code != 0 {
		return fmt.Errorf("read native worktree gitdir %s: exit %d", wt, res.Code)
	}
	gitdir := strings.TrimSpace(out)
	if gitdir == "" {
		return fmt.Errorf("native worktree %s returned an empty gitdir", wt)
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Clean(filepath.Join(wt, gitdir))
	}
	if pathWithinRoot(devRoot, wt) && pathWithinRoot(devRoot, gitdir) {
		rel, err := filepath.Rel(wt, gitdir)
		if err != nil {
			return fmt.Errorf("make native worktree gitdir relative for %s: %w", wt, err)
		}
		gitdir = rel
	}
	if err := os.WriteFile(gitFile, []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		return fmt.Errorf("write native worktree gitdir %s: %w", gitFile, err)
	}
	return nil
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
func cleanWorktreePath(repoWorktreesDir, wt string) error {
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
	// Host git should not inherit container SSH settings; force ssh
	envGit := func(args ...string) []string {
		return append([]string{"-u", "GIT_SSH_COMMAND", "git", "-c", "core.sshCommand=ssh -F /dev/null"}, args...)
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

	if err := run(dry, "env", envGit("-C", repoPath, "fetch", "--all", "--prune")...); err != nil {
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
			if err := cleanWorktreePath(repoWorktreesDir, wt); err != nil {
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

type NativeOptions struct {
	DevkitRoot       string
	Repo             string
	Count            int
	BaseBranch       string
	BranchPrefix     string
	WorktreeRoot     string
	GitSSHCommand    string
	RequireSSHOrigin bool
	DryRun           bool
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
	var repoWorktreesDir string
	if !opts.DryRun {
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
	remoteURL, result := execx.Capture(context.Background(), "git", "-C", repoPath, "remote", "get-url", "origin")
	if result.Code != 0 && !opts.DryRun {
		return fmt.Errorf("read native Git bootstrap origin: exit %d", result.Code)
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
	if err := run(opts.DryRun, "env", envRemoteGit("-C", repoPath, "fetch", "--all", "--prune")...); err != nil {
		if opts.RequireSSHOrigin || opts.DryRun || !nativeWorktreesExist(worktreesRoot, repo, count) {
			return err
		}
	}
	if err := run(opts.DryRun, "env", envLocalGit("-C", repoPath, "config", "worktree.useRelativePaths", "true")...); err != nil {
		return err
	}
	if !opts.DryRun {
		_ = os.MkdirAll(worktreesRoot, 0o755)
	}
	for i := 1; i <= count; i++ {
		parent := filepath.Join(worktreesRoot, fmt.Sprintf("agent%d", i))
		if !opts.DryRun {
			_ = os.MkdirAll(parent, 0o755)
		}
		wt := filepath.Join(parent, repo)
		branch := fmt.Sprintf("%s%d", branchPrefix, i)
		if !opts.DryRun {
			if ok, err := existingGitCheckout(wt); err != nil {
				return err
			} else if ok {
				if err := run(false, "env", envLocalGit("-C", wt, "config", "worktree.useRelativePaths", "true")...); err != nil {
					return err
				}
				if err := rewriteNativeGitdir(wt, devRoot); err != nil {
					return err
				}
				continue
			}
			if err := cleanWorktreePath(repoWorktreesDir, wt); err != nil {
				return err
			}
		}
		_ = run(opts.DryRun, "env", envLocalGit("-C", repoPath, "worktree", "prune")...)
		_ = run(opts.DryRun, "env", envLocalGit("-C", repoPath, "worktree", "remove", "-f", wt)...)
		if err := run(opts.DryRun, "env", envLocalGit("-C", repoPath, "worktree", "add", wt, "-B", branch, "origin/"+baseBranch)...); err != nil {
			if opts.DryRun {
				return err
			}
			if remErr := os.RemoveAll(wt); remErr != nil && !errors.Is(remErr, os.ErrNotExist) {
				return fmt.Errorf("remove stale worktree %s: %w", wt, remErr)
			}
			if err2 := run(opts.DryRun, "env", envLocalGit("-C", repoPath, "worktree", "add", wt, "-B", branch, "origin/"+baseBranch)...); err2 != nil {
				return err2
			}
		}
		if !opts.DryRun {
			if err := rewriteNativeGitdir(wt, devRoot); err != nil {
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

func nativeWorktreesExist(worktreesRoot, repo string, count int) bool {
	if count < 1 {
		count = 1
	}
	for i := 1; i <= count; i++ {
		wt := filepath.Join(worktreesRoot, fmt.Sprintf("agent%d", i), repo)
		ok, err := existingGitCheckout(wt)
		if err != nil || !ok {
			return false
		}
	}
	return true
}

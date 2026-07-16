package worktrees

import (
	"devkit/cli/devctl/internal/paths"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func mustRun(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func readTrim(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func makeRepoWithBare(t *testing.T, root, devRoot, name string) {
	bare := filepath.Join(root, "remotes", name+".git")
	work := filepath.Join(devRoot, name)
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "git", "init", "--bare", bare)
	mustRun(t, "git", "-C", work, "init")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("# "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "git", "-C", work, "add", ".")
	mustRun(t, "git", "-C", work, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-m", "init")
	mustRun(t, "git", "-C", work, "branch", "-M", "main")
	mustRun(t, "git", "-C", work, "remote", "add", "origin", bare)
	mustRun(t, "git", "-C", work, "push", "-u", "origin", "main")
}

func checkBranchAndUpstream(t *testing.T, path, wantBranch string) {
	t.Helper()
	got := readTrim(t, "git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD")
	if got != wantBranch {
		t.Fatalf("%s: want branch %s, got %s", path, wantBranch, got)
	}
	up := readTrim(t, "git", "-C", path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if up != "origin/main" {
		t.Fatalf("%s: want upstream origin/main, got %s", path, up)
	}
}

func checkGitdirForm(t *testing.T, path string, wantAbs bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(path, ".git"))
	if err != nil {
		t.Fatalf("read .git: %v", err)
	}
	content := strings.TrimSpace(string(data))
	if !strings.HasPrefix(content, "gitdir:") {
		t.Fatalf("unexpected .git content: %q", content)
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(content, "gitdir:"))
	if filepath.IsAbs(gitdir) != wantAbs {
		t.Fatalf("%s: gitdir absolute = %v, want %v (%q)", path, filepath.IsAbs(gitdir), wantAbs, content)
	}
}

// This test performs a real host-side worktree setup across two repos with two agents each
// (agent1 in-place, agent2 as worktree), verifying branch names and upstreams.
func TestSetup_TwoRepos_TwoAgents(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Host layout: <root>/dev/{devkit, dumb-onion-hax, ouroboros-ide}, plus bare remotes under <root>/remotes
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	if err := os.MkdirAll(filepath.Join(devRoot, "devkit"), 0o755); err != nil {
		t.Fatal(err)
	}
	devkitRoot := filepath.Join(devRoot, "devkit")

	makeRepoWithBare(t, root, devRoot, "dumb-onion-hax")
	makeRepoWithBare(t, root, devRoot, "ouroboros-ide")

	// Run setup for each repo with two agents
	if err := Setup(devkitRoot, "dumb-onion-hax", 2, "main", "agent", false); err != nil {
		t.Fatalf("setup doh failed: %v", err)
	}
	if err := Setup(devkitRoot, "ouroboros-ide", 2, "main", "agent", false); err != nil {
		t.Fatalf("setup ouro failed: %v", err)
	}

	// Validate branches and upstreams
	checkBranchAndUpstream(t, filepath.Join(devRoot, "dumb-onion-hax"), "agent1")
	checkBranchAndUpstream(t, filepath.Join(devRoot, paths.AgentWorktreesDir, "agent2", "dumb-onion-hax"), "agent2")
	checkBranchAndUpstream(t, filepath.Join(devRoot, "ouroboros-ide"), "agent1")
	checkBranchAndUpstream(t, filepath.Join(devRoot, paths.AgentWorktreesDir, "agent2", "ouroboros-ide"), "agent2")
	checkGitdirForm(t, filepath.Join(devRoot, paths.AgentWorktreesDir, "agent2", "ouroboros-ide"), false)
}

func TestSetup_RemovesStaleWorktreeDirectories(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	if err := os.MkdirAll(filepath.Join(devRoot, "devkit"), 0o755); err != nil {
		t.Fatal(err)
	}
	devkitRoot := filepath.Join(devRoot, "devkit")

	makeRepoWithBare(t, root, devRoot, "dumb-onion-hax")
	makeRepoWithBare(t, root, devRoot, "ouroboros-ide")

	if err := Setup(devkitRoot, "dumb-onion-hax", 2, "main", "agent", false); err != nil {
		t.Fatalf("setup doh failed: %v", err)
	}
	if err := Setup(devkitRoot, "ouroboros-ide", 2, "main", "agent", false); err != nil {
		t.Fatalf("initial ouro setup failed: %v", err)
	}

	staleWt := filepath.Join(devRoot, paths.AgentWorktreesDir, "agent2", "ouroboros-ide")
	foreignGitdir := filepath.Join(devRoot, "dumb-onion-hax", ".git", "worktrees", "agent2")
	if err := os.WriteFile(filepath.Join(staleWt, ".git"), []byte("gitdir: "+foreignGitdir+"\n"), 0o644); err != nil {
		t.Fatalf("prepare stale gitdir: %v", err)
	}

	if err := Setup(devkitRoot, "ouroboros-ide", 2, "main", "agent", false); err != nil {
		t.Fatalf("ouro setup with stale dir failed: %v", err)
	}

	checkBranchAndUpstream(t, filepath.Join(devRoot, paths.AgentWorktreesDir, "agent2", "ouroboros-ide"), "agent2")
}

func TestSetupNative_DedicatedWorktreesForEveryAgent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	if err := os.MkdirAll(filepath.Join(devRoot, "devkit"), 0o755); err != nil {
		t.Fatal(err)
	}
	devkitRoot := filepath.Join(devRoot, "devkit")
	makeRepoWithBare(t, root, devRoot, "ouroboros-ide")

	if err := SetupNative(NativeOptions{
		DevkitRoot:   devkitRoot,
		Repo:         "ouroboros-ide",
		Count:        2,
		BaseBranch:   "main",
		BranchPrefix: "agent",
	}); err != nil {
		t.Fatalf("native setup failed: %v", err)
	}

	checkBranchAndUpstream(t, filepath.Join(devRoot, paths.AgentWorktreesDir, "agent1", "ouroboros-ide"), "agent1")
	checkBranchAndUpstream(t, filepath.Join(devRoot, paths.AgentWorktreesDir, "agent2", "ouroboros-ide"), "agent2")
	checkGitdirForm(t, filepath.Join(devRoot, paths.AgentWorktreesDir, "agent1", "ouroboros-ide"), true)
	checkGitdirForm(t, filepath.Join(devRoot, paths.AgentWorktreesDir, "agent2", "ouroboros-ide"), true)
	if got := readTrim(t, "git", "-C", filepath.Join(devRoot, "ouroboros-ide"), "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Fatalf("primary checkout branch changed to %s", got)
	}
}

func TestSetupNative_ReconstructsMissingRepoBesidePartialAgentHome(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	makeRepoWithBare(t, root, devRoot, "ouroboros-ide")

	agentParent := filepath.Join(devRoot, paths.AgentWorktreesDir, "agent2")
	if err := os.MkdirAll(filepath.Join(agentParent, ".devhome-agent2", ".codex"), 0o700); err != nil {
		t.Fatalf("prepare partial agent home: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(agentParent, "ouroboros-ide"), 0o755); err != nil {
		t.Fatalf("prepare plain repo directory: %v", err)
	}

	if err := SetupNative(NativeOptions{
		DevkitRoot:   devkitRoot,
		Repo:         "ouroboros-ide",
		Count:        2,
		BaseBranch:   "main",
		BranchPrefix: "agent",
	}); err != nil {
		t.Fatalf("native reconstruction failed: %v", err)
	}

	worktree := filepath.Join(agentParent, "ouroboros-ide")
	checkBranchAndUpstream(t, worktree, "agent2")
	if got, want := readTrim(t, "git", "-C", worktree, "rev-parse", "HEAD"), readTrim(t, "git", "-C", worktree, "rev-parse", "origin/main"); got != want {
		t.Fatalf("fresh worktree HEAD = %s, origin/main = %s", got, want)
	}
	if got := readTrim(t, "git", "-C", worktree, "status", "--porcelain=v1"); got != "" {
		t.Fatalf("fresh worktree is dirty: %q", got)
	}
	if _, err := os.Stat(filepath.Join(agentParent, ".devhome-agent2", ".codex")); err != nil {
		t.Fatalf("partial agent home was not preserved for source bootstrap: %v", err)
	}
}

func TestSetupNative_FromLinkedSourceWorktreeUsesAbsoluteGitdir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	primaryDevRoot := filepath.Join(root, "primary-dev")
	sourceDevRoot := filepath.Join(root, "dev")
	if err := os.MkdirAll(filepath.Join(sourceDevRoot, "devkit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(primaryDevRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	devkitRoot := filepath.Join(sourceDevRoot, "devkit")
	makeRepoWithBare(t, root, primaryDevRoot, "ouroboros-ide")

	sourceRepo := filepath.Join(sourceDevRoot, "ouroboros-ide-nix-readiness")
	mustRun(t, "git", "-C", filepath.Join(primaryDevRoot, "ouroboros-ide"), "worktree", "add", "--detach", sourceRepo, "origin/main")

	if err := SetupNative(NativeOptions{
		DevkitRoot:   devkitRoot,
		Repo:         "ouroboros-ide-nix-readiness",
		Count:        1,
		BaseBranch:   "main",
		BranchPrefix: "nixready-agent",
	}); err != nil {
		t.Fatalf("native setup from source worktree failed: %v", err)
	}

	agentWorktree := filepath.Join(sourceDevRoot, paths.AgentWorktreesDir, "agent1", "ouroboros-ide-nix-readiness")
	checkBranchAndUpstream(t, agentWorktree, "nixready-agent1")
	checkGitdirForm(t, agentWorktree, true)
	checkGitdirForm(t, sourceRepo, true)
}

func TestSetupNative_ReusesStandaloneAgentCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	if err := os.MkdirAll(filepath.Join(devRoot, "devkit"), 0o755); err != nil {
		t.Fatal(err)
	}
	devkitRoot := filepath.Join(devRoot, "devkit")
	makeRepoWithBare(t, root, devRoot, "ouroboros-ide")

	standalone := filepath.Join(devRoot, paths.AgentWorktreesDir, "agent1", "ouroboros-ide")
	if err := os.MkdirAll(filepath.Dir(standalone), 0o755); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "git", "clone", filepath.Join(root, "remotes", "ouroboros-ide.git"), standalone)
	mustRun(t, "git", "-C", standalone, "checkout", "main")

	if err := SetupNative(NativeOptions{
		DevkitRoot:   devkitRoot,
		Repo:         "ouroboros-ide",
		Count:        1,
		BaseBranch:   "main",
		BranchPrefix: "agent",
	}); err != nil {
		t.Fatalf("native setup should reuse standalone checkout: %v", err)
	}
	if info, err := os.Stat(filepath.Join(standalone, ".git")); err != nil || !info.IsDir() {
		t.Fatalf("standalone checkout .git dir was not preserved: %v", err)
	}
	if got := readTrim(t, "git", "-C", standalone, "rev-parse", "--show-toplevel"); filepath.Clean(got) != standalone {
		t.Fatalf("unexpected standalone toplevel: %s", got)
	}
}

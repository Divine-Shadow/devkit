package worktrees

import (
	"devkit/cli/devctl/internal/paths"
	"errors"
	"fmt"
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

func checkRelativeNativeMetadata(t *testing.T, worktree, wantCommonDir, forbiddenHostSpelling string) string {
	t.Helper()
	gitdirValue, err := readGitdirPointer(filepath.Join(worktree, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.IsAbs(gitdirValue) {
		t.Fatalf("%s: package-owned gitdir remained absolute: %q", worktree, gitdirValue)
	}
	gitdir, err := canonicalMetadataPath(worktree, gitdirValue)
	if err != nil {
		t.Fatal(err)
	}
	commondirValue, err := readPlainMetadataPath(filepath.Join(gitdir, "commondir"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.IsAbs(commondirValue) {
		t.Fatalf("%s: package-owned commondir remained absolute: %q", worktree, commondirValue)
	}
	commondir, err := canonicalMetadataPath(gitdir, commondirValue)
	if err != nil {
		t.Fatal(err)
	}
	wantCommonDir, err = canonicalExistingPath(wantCommonDir)
	if err != nil {
		t.Fatal(err)
	}
	if commondir != wantCommonDir {
		t.Fatalf("%s: commondir = %s, want %s", worktree, commondir, wantCommonDir)
	}
	reverseValue, err := readPlainMetadataPath(filepath.Join(gitdir, "gitdir"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.IsAbs(reverseValue) {
		t.Fatalf("%s: package-owned reverse gitdir remained absolute: %q", worktree, reverseValue)
	}
	reverseGitFile, err := canonicalMetadataPath(gitdir, reverseValue)
	if err != nil {
		t.Fatal(err)
	}
	wantGitFile, err := canonicalExistingPath(filepath.Join(worktree, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if reverseGitFile != wantGitFile {
		t.Fatalf("%s: reverse gitdir = %s, want %s", worktree, reverseGitFile, wantGitFile)
	}
	for _, path := range []string{filepath.Join(worktree, ".git"), filepath.Join(gitdir, "commondir"), filepath.Join(gitdir, "gitdir")} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), forbiddenHostSpelling) {
			t.Fatalf("%s retained host spelling %q: %s", path, forbiddenHostSpelling, data)
		}
	}
	if got := readTrim(t, "git", "-C", worktree, "rev-parse", "--absolute-git-dir"); filepath.Clean(got) != gitdir {
		t.Fatalf("%s: absolute gitdir = %s, want %s", worktree, got, gitdir)
	}
	commonFromGit := readTrim(t, "git", "-C", worktree, "rev-parse", "--git-common-dir")
	if !filepath.IsAbs(commonFromGit) {
		commonFromGit = filepath.Join(worktree, commonFromGit)
	}
	commonFromGit, err = canonicalExistingPath(commonFromGit)
	if err != nil {
		t.Fatal(err)
	}
	if commonFromGit != wantCommonDir {
		t.Fatalf("%s: Git common dir = %s, want %s", worktree, commonFromGit, wantCommonDir)
	}
	return gitdir
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
	checkGitdirForm(t, filepath.Join(devRoot, paths.AgentWorktreesDir, "agent1", "ouroboros-ide"), false)
	checkGitdirForm(t, filepath.Join(devRoot, paths.AgentWorktreesDir, "agent2", "ouroboros-ide"), false)
	if got := readTrim(t, "git", "-C", filepath.Join(devRoot, "ouroboros-ide"), "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Fatalf("primary checkout branch changed to %s", got)
	}
}

func TestSetupNative_NestedGuiWorktreeSurvivesDevRootProjection(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	devRoot := filepath.Join(root, "host-dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
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

	outer := filepath.Join(devRoot, paths.AgentWorktreesDir, "agent2", "ouroboros-ide")
	legacyGitDir := readTrim(t, "git", "-C", outer, "rev-parse", "--git-dir")
	if !filepath.IsAbs(legacyGitDir) {
		legacyGitDir = filepath.Join(outer, legacyGitDir)
	}
	if err := os.WriteFile(filepath.Join(outer, ".git"), []byte("gitdir: "+filepath.Clean(legacyGitDir)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "git", "-C", outer, "config", "worktree.useRelativePaths", "false")
	if err := SetupNative(NativeOptions{
		DevkitRoot:   devkitRoot,
		Repo:         "ouroboros-ide",
		Count:        2,
		BaseBranch:   "main",
		BranchPrefix: "agent",
	}); err != nil {
		t.Fatalf("native reconvergence failed: %v", err)
	}
	checkGitdirForm(t, outer, false)
	if got := readTrim(t, "git", "-C", outer, "config", "--bool", "worktree.useRelativePaths"); got != "true" {
		t.Fatalf("worktree.useRelativePaths = %q, want true", got)
	}

	nested := filepath.Join(devRoot, paths.AgentWorktreesDir, "agent2", ".devhome-agent2", ".codex", "worktrees", "3293", "ouroboros-ide")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "git", "-C", outer, "worktree", "add", "--detach", nested, "HEAD")
	checkGitdirForm(t, nested, false)
	gitFile, err := os.ReadFile(filepath.Join(nested, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gitFile), devRoot) {
		t.Fatalf("nested worktree captured host dev root: %s", gitFile)
	}

	projectedDevRoot := filepath.Join(root, "workspaces-dev")
	if err := os.Rename(devRoot, projectedDevRoot); err != nil {
		t.Fatalf("project dev root: %v", err)
	}
	projectedNested := filepath.Join(projectedDevRoot, paths.AgentWorktreesDir, "agent2", ".devhome-agent2", ".codex", "worktrees", "3293", "ouroboros-ide")
	if got := readTrim(t, "git", "-C", projectedNested, "rev-parse", "--show-toplevel"); got != projectedNested {
		t.Fatalf("projected nested worktree top = %s, want %s", got, projectedNested)
	}
	if got := readTrim(t, "git", "-C", projectedNested, "status", "--porcelain=v1"); got != "" {
		t.Fatalf("projected nested worktree is dirty: %q", got)
	}
}

func TestSetupNativeSSHOriginUsesExplicitBootstrapCommand(t *testing.T) {
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

	bare := filepath.Join(root, "remotes", "ouroboros-ide.git")
	repo := filepath.Join(devRoot, "ouroboros-ide")
	mustRun(t, "git", "-C", repo, "remote", "set-url", "origin", "ssh://git@fixture.invalid"+bare)

	logPath := filepath.Join(root, "ssh.log")
	sshPath := filepath.Join(root, "package-owned-ssh")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\n" +
		"if [ \"${1:-}\" = -G ]; then exit 0; fi\n" +
		"last=\n" +
		"for arg in \"$@\"; do last=$arg; done\n" +
		"case \"$last\" in\n" +
		"  \"git-upload-pack \"*) exec sh -c \"$last\" ;;\n" +
		"esac\n" +
		"exit 1\n"
	if err := os.WriteFile(sshPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "package-owned-config")
	if err := os.WriteFile(configPath, []byte("Host fixture.invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := SetupNative(NativeOptions{
		DevkitRoot:    devkitRoot,
		Repo:          "ouroboros-ide",
		Count:         1,
		BaseBranch:    "main",
		BranchPrefix:  "agent",
		GitSSHCommand: sshPath + " -F " + configPath,
	}); err != nil {
		t.Fatalf("native setup with package-owned SSH command failed: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	for _, want := range []string{"-F " + configPath, "git-upload-pack"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("SSH invocation missing %q:\n%s", want, logText)
		}
	}
	if strings.Contains(logText, "/dev/null") {
		t.Fatalf("SSH invocation selected the prohibited /dev/null transport:\n%s", logText)
	}
	checkBranchAndUpstream(t, filepath.Join(devRoot, paths.AgentWorktreesDir, "agent1", "ouroboros-ide"), "agent1")
}

func TestSetupNativeIsolatedOwnedRootsUseRelativeCanonicalMetadata(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	hostTopology := filepath.Join(root, "host-topology")
	devRoot := filepath.Join(hostTopology, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	makeRepoWithBare(t, root, devRoot, "ouroboros-ide")

	bare := filepath.Join(root, "remotes", "ouroboros-ide.git")
	repo := filepath.Join(devRoot, "ouroboros-ide")
	mustRun(t, "git", "-C", repo, "remote", "set-url", "origin", "ssh://git@fixture.invalid"+bare)
	logPath := filepath.Join(root, "isolated-ssh.log")
	sshPath := filepath.Join(root, "package-owned-isolated-ssh")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\n" +
		"if [ \"${1:-}\" = -G ]; then exit 0; fi\n" +
		"last=\n" +
		"for arg in \"$@\"; do last=$arg; done\n" +
		"case \"$last\" in\n" +
		"  \"git-upload-pack \"*) exec sh -c \"$last\" ;;\n" +
		"esac\n" +
		"exit 1\n"
	if err := os.WriteFile(sshPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "package-owned-isolated-config")
	if err := os.WriteFile(configPath, []byte("Host fixture.invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	worktreeRoots := []string{
		filepath.Join(hostTopology, "isolated-product-a"),
		filepath.Join(hostTopology, "isolated-product-b"),
	}
	gitdirs := make([]string, 0, len(worktreeRoots))
	for index, worktreeRoot := range worktreeRoots {
		branchPrefix := fmt.Sprintf("isolated%d-agent", index+1)
		if err := SetupNative(NativeOptions{
			DevkitRoot:       devkitRoot,
			Repo:             "ouroboros-ide",
			Origin:           "ssh://git@fixture.invalid" + bare,
			Count:            1,
			BaseBranch:       "main",
			BranchPrefix:     branchPrefix,
			WorktreeRoot:     worktreeRoot,
			GitSSHCommand:    sshPath + " -F " + configPath,
			RequireSSHOrigin: true,
		}); err != nil {
			t.Fatalf("isolated native setup %d failed: %v", index+1, err)
		}
		worktree := filepath.Join(worktreeRoot, "agent1", "ouroboros-ide")
		commonDir, err := nativeOwnedCommonRepositoryPath(worktreeRoot, "ouroboros-ide")
		if err != nil {
			t.Fatal(err)
		}
		checkBranchAndUpstream(t, worktree, branchPrefix+"1")
		gitdirs = append(gitdirs, checkRelativeNativeMetadata(
			t,
			worktree,
			commonDir,
			hostTopology,
		))
		mustRun(t, "git", "-C", worktree, "update-ref", "refs/devkit/isolated-metadata-proof", "HEAD")
		mustRun(t, "git", "-C", worktree, "update-ref", "-d", "refs/devkit/isolated-metadata-proof")
	}
	if gitdirs[0] == gitdirs[1] {
		t.Fatalf("isolated consumers shared a worktree gitdir: %s", gitdirs[0])
	}
	commonDirs := make([]string, 0, len(worktreeRoots))
	for _, worktreeRoot := range worktreeRoots {
		commonDir, err := nativeOwnedCommonRepositoryPath(worktreeRoot, "ouroboros-ide")
		if err != nil {
			t.Fatal(err)
		}
		commonDirs = append(commonDirs, commonDir)
		if got := readTrim(t, "git", "--git-dir", commonDir, "rev-parse", "--is-bare-repository"); got != "true" {
			t.Fatalf("owned common repository %s is not bare: %s", commonDir, got)
		}
	}
	if commonDirs[0] == commonDirs[1] {
		t.Fatalf("isolated consumers shared a common repository: %s", commonDirs[0])
	}
	crossRootProbe := exec.Command("git", "--git-dir", commonDirs[1], "show-ref", "--verify", "refs/heads/isolated1-agent1")
	if out, err := crossRootProbe.CombinedOutput(); err == nil {
		t.Fatalf("isolated consumer B contains consumer A branch: %s", out)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(logData), "git-upload-pack"); count != 2 {
		t.Fatalf("package-owned SSH fetch count = %d, want 2:\n%s", count, logData)
	}

	projectedRoots := []string{
		filepath.Join(root, "sandbox", "workspaces", "dev", "consumer-a"),
		filepath.Join(root, "sandbox", "unrelated", "depth", "consumer-b"),
	}
	for index, worktreeRoot := range worktreeRoots {
		projectedRoot := projectedRoots[index]
		if err := os.MkdirAll(filepath.Dir(projectedRoot), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(worktreeRoot, projectedRoot); err != nil {
			t.Fatalf("project isolated topology %s -> %s: %v", worktreeRoot, projectedRoot, err)
		}
		worktree := filepath.Join(projectedRoot, "agent1", "ouroboros-ide")
		if got := readTrim(t, "git", "-C", worktree, "rev-parse", "--show-toplevel"); filepath.Clean(got) != worktree {
			t.Fatalf("projected worktree top = %s, want %s", got, worktree)
		}
		if got := readTrim(t, "git", "-C", worktree, "status", "--porcelain=v1"); got != "" {
			t.Fatalf("projected worktree is dirty: %q", got)
		}
		mustRun(t, "git", "-C", worktree, "update-ref", "refs/devkit/projected-metadata-proof", "HEAD")
		mustRun(t, "git", "-C", worktree, "update-ref", "-d", "refs/devkit/projected-metadata-proof")
	}
}

func TestRewriteNativeGitdirRejectsForeignCommondirTraversal(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	makeRepoWithBare(t, root, devRoot, "ouroboros-ide")
	worktreeRoot := filepath.Join(root, "isolated-product")
	if err := SetupNative(NativeOptions{
		DevkitRoot:   devkitRoot,
		Repo:         "ouroboros-ide",
		Count:        1,
		BaseBranch:   "main",
		BranchPrefix: "isolated-agent",
		WorktreeRoot: worktreeRoot,
	}); err != nil {
		t.Fatalf("prepare isolated native worktree: %v", err)
	}
	worktree := filepath.Join(worktreeRoot, "agent1", "ouroboros-ide")
	commonDir, err := nativeOwnedCommonRepositoryPath(worktreeRoot, "ouroboros-ide")
	if err != nil {
		t.Fatal(err)
	}
	gitdirValue, err := readGitdirPointer(filepath.Join(worktree, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	gitdir, err := canonicalMetadataPath(worktree, gitdirValue)
	if err != nil {
		t.Fatal(err)
	}
	foreignCommonDir := filepath.Join(root, "foreign-common")
	if err := os.MkdirAll(foreignCommonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	traversal, err := filepath.Rel(gitdir, foreignCommonDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitdir, "commondir"), []byte(traversal+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitFileBefore, err := os.ReadFile(filepath.Join(worktree, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	err = rewriteNativeGitdir(worktree, worktreeRoot, commonDir)
	if err == nil || !strings.Contains(err.Error(), "is not the package-owned common Git directory") {
		t.Fatalf("rewriteNativeGitdir error = %v, want foreign commondir rejection", err)
	}
	gitFileAfter, readErr := os.ReadFile(filepath.Join(worktree, ".git"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(gitFileAfter) != string(gitFileBefore) {
		t.Fatalf("foreign commondir rejection rewrote .git: before=%q after=%q", gitFileBefore, gitFileAfter)
	}
}

func TestSetupNativeRejectsStaleCommonRepositoryWithoutOwnershipMarker(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	makeRepoWithBare(t, root, devRoot, "ouroboros-ide")
	worktreeRoot := filepath.Join(root, "isolated-product")
	commonDir, err := nativeOwnedCommonRepositoryPath(worktreeRoot, "ouroboros-ide")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(commonDir), 0o700); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "git", "init", "--bare", commonDir)
	mustRun(t, "git", "--git-dir", commonDir, "remote", "add", "origin", filepath.Join(root, "remotes", "ouroboros-ide.git"))

	err = SetupNative(NativeOptions{
		DevkitRoot:   devkitRoot,
		Repo:         "ouroboros-ide",
		Count:        1,
		BaseBranch:   "main",
		BranchPrefix: "isolated-agent",
		WorktreeRoot: worktreeRoot,
	})
	if err == nil || !strings.Contains(err.Error(), "marker") {
		t.Fatalf("SetupNative error = %v, want stale ownership-marker rejection", err)
	}
	if _, statErr := os.Stat(filepath.Join(worktreeRoot, "agent1", "ouroboros-ide")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stale common repository created a worktree: %v", statErr)
	}
}

func TestSetupNativeFailedFetchCleansPartialOwnedRepository(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	makeRepoWithBare(t, root, devRoot, "ouroboros-ide")
	bare := filepath.Join(root, "remotes", "ouroboros-ide.git")
	repo := filepath.Join(devRoot, "ouroboros-ide")
	mustRun(t, "git", "-C", repo, "remote", "set-url", "origin", "ssh://git@fixture.invalid"+bare)
	sshPath := filepath.Join(root, "package-owned-failing-ssh")
	if err := os.WriteFile(sshPath, []byte("#!/bin/sh\nexit 71\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "package-owned-config")
	if err := os.WriteFile(configPath, []byte("Host fixture.invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	worktreeRoot := filepath.Join(root, "isolated-product")
	commonDir, err := nativeOwnedCommonRepositoryPath(worktreeRoot, "ouroboros-ide")
	if err != nil {
		t.Fatal(err)
	}
	err = SetupNative(NativeOptions{
		DevkitRoot:       devkitRoot,
		Repo:             "ouroboros-ide",
		Origin:           "ssh://git@fixture.invalid" + bare,
		Count:            1,
		BaseBranch:       "main",
		BranchPrefix:     "isolated-agent",
		WorktreeRoot:     worktreeRoot,
		GitSSHCommand:    sshPath + " -F " + configPath,
		RequireSSHOrigin: true,
	})
	if err == nil {
		t.Fatal("SetupNative unexpectedly accepted failed package-owned fetch")
	}
	if _, statErr := os.Stat(commonDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed fetch retained common repository %s: %v", commonDir, statErr)
	}
	staging, globErr := filepath.Glob(filepath.Join(filepath.Dir(commonDir), ".ouroboros-ide.bootstrap-*"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(staging) != 0 {
		t.Fatalf("failed fetch retained staging repositories: %v", staging)
	}
	if _, statErr := os.Stat(filepath.Join(worktreeRoot, "agent1", "ouroboros-ide")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed fetch created a worktree: %v", statErr)
	}
}

func TestSetupNativeRejectsRepositoryPathTraversalBeforeBootstrap(t *testing.T) {
	root := t.TempDir()
	worktreeRoot := filepath.Join(root, "isolated-product")
	err := SetupNative(NativeOptions{
		DevkitRoot:   filepath.Join(root, "dev", "devkit"),
		Repo:         "../foreign",
		Count:        1,
		WorktreeRoot: worktreeRoot,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot select a package-owned common repository path") {
		t.Fatalf("SetupNative error = %v, want repository traversal rejection", err)
	}
	if _, statErr := os.Stat(worktreeRoot); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("repository traversal created worktree root: %v", statErr)
	}
}

func TestSetupNativeRejectsAndPreservesPartialWorktreeBeforeBootstrap(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	makeRepoWithBare(t, root, devRoot, "ouroboros-ide")
	worktreeRoot := filepath.Join(root, "isolated-product")
	partialWorktree := filepath.Join(worktreeRoot, "agent1", "ouroboros-ide")
	if err := os.MkdirAll(partialWorktree, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(partialWorktree, "preserve-me")
	if err := os.WriteFile(sentinel, []byte("caller-owned partial state\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := SetupNative(NativeOptions{
		DevkitRoot:   devkitRoot,
		Repo:         "ouroboros-ide",
		Count:        1,
		BaseBranch:   "main",
		BranchPrefix: "isolated-agent",
		WorktreeRoot: worktreeRoot,
	})
	if err == nil || !strings.Contains(err.Error(), "partial or stale state") {
		t.Fatalf("SetupNative error = %v, want partial worktree rejection", err)
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "caller-owned partial state\n" {
		t.Fatalf("partial worktree was mutated: data=%q error=%v", got, readErr)
	}
	commonDir, pathErr := nativeOwnedCommonRepositoryPath(worktreeRoot, "ouroboros-ide")
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if _, statErr := os.Stat(commonDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partial worktree preflight created common repository %s: %v", commonDir, statErr)
	}
}

func TestNativeResetDisposesOpaqueInPrefixPayloadWithoutForeignCustody(t *testing.T) {
	root := t.TempDir()
	worktreeRoot := filepath.Join(root, "owned-worktrees")
	stateRoot := filepath.Join(root, "owned-state")
	foreignRoot := filepath.Join(root, "foreign-common.git", "worktrees", "legacy")
	if err := os.MkdirAll(foreignRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	foreignSentinel := filepath.Join(foreignRoot, "accepted-business-data")
	if err := os.WriteFile(foreignSentinel, []byte("outside reset custody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	staleWorktree := filepath.Join(worktreeRoot, "agent1", "ouroboros-ide")
	if err := os.MkdirAll(staleWorktree, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleWorktree, ".git"), []byte("gitdir: "+foreignRoot+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleWorktree, "dirty-payload"), []byte("disposable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stateRoot, "dev-all-agent1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, "dev-all-agent1", "stale"), []byte("disposable\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanNativeReset(NativeResetOptions{
		Project:      "dev-all",
		Repo:         "ouroboros-ide",
		Count:        1,
		WorktreeRoot: worktreeRoot,
		StateRoot:    stateRoot,
	})
	if err != nil {
		t.Fatalf("plan native reset: %v", err)
	}
	if err := plan.Apply(); err != nil {
		t.Fatalf("apply native reset: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(worktreeRoot, "agent1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned stale worktree survived reset: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(stateRoot, "dev-all-agent1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned stale state survived reset: %v", err)
	}
	if got, err := os.ReadFile(foreignSentinel); err != nil || string(got) != "outside reset custody\n" {
		t.Fatalf("foreign Git metadata acquired reset custody: data=%q error=%v", got, err)
	}
}

func TestNativeResetRejectsOwnershipEscapesBeforeDisposal(t *testing.T) {
	originalMountPoints := nativeResetMountPoints
	t.Cleanup(func() { nativeResetMountPoints = originalMountPoints })
	nativeResetMountPoints = func() ([]string, error) { return []string{"/"}, nil }

	newOptions := func(root string) NativeResetOptions {
		return NativeResetOptions{
			Project:      "dev-all",
			Repo:         "ouroboros-ide",
			Count:        1,
			WorktreeRoot: filepath.Join(root, "owned-worktrees"),
			StateRoot:    filepath.Join(root, "owned-state"),
		}
	}
	t.Run("mismatched prefix", func(t *testing.T) {
		opts := newOptions(t.TempDir())
		sentinel := filepath.Join(opts.WorktreeRoot, "agent1", "preserve")
		if err := os.MkdirAll(filepath.Dir(sentinel), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sentinel, []byte("preserve\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		opts.Project = "other"
		if _, err := PlanNativeReset(opts); err == nil || !strings.Contains(err.Error(), "exact dev-all prefix") {
			t.Fatalf("mismatched prefix error = %v", err)
		}
		if _, err := os.Stat(sentinel); err != nil {
			t.Fatalf("mismatched prefix changed owned payload before rejection: %v", err)
		}
	})
	t.Run("repository traversal", func(t *testing.T) {
		opts := newOptions(t.TempDir())
		sentinel := filepath.Join(opts.WorktreeRoot, "agent1", "preserve")
		if err := os.MkdirAll(filepath.Dir(sentinel), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sentinel, []byte("preserve\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		opts.Repo = "../outside"
		if _, err := PlanNativeReset(opts); err == nil || !strings.Contains(err.Error(), "cannot select a package-owned common repository path") {
			t.Fatalf("repository traversal error = %v", err)
		}
		if _, err := os.Stat(sentinel); err != nil {
			t.Fatalf("repository traversal changed payload before rejection: %v", err)
		}
	})
	t.Run("symlink escape", func(t *testing.T) {
		root := t.TempDir()
		opts := newOptions(root)
		outside := filepath.Join(root, "outside")
		if err := os.MkdirAll(outside, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(opts.WorktreeRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(opts.WorktreeRoot, "agent1")); err != nil {
			t.Fatal(err)
		}
		if _, err := PlanNativeReset(opts); err == nil || !strings.Contains(err.Error(), "symlink or junction") {
			t.Fatalf("symlink escape error = %v", err)
		}
		if _, err := os.Stat(outside); err != nil {
			t.Fatalf("symlink escape changed outside target before rejection: %v", err)
		}
	})
	t.Run("common repository parent escape", func(t *testing.T) {
		root := t.TempDir()
		opts := newOptions(root)
		outside := filepath.Join(root, "outside-git")
		if err := os.MkdirAll(outside, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(opts.WorktreeRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(opts.WorktreeRoot, ".devkit")); err != nil {
			t.Fatal(err)
		}
		if _, err := PlanNativeReset(opts); err == nil || !strings.Contains(err.Error(), "symlink or junction") {
			t.Fatalf("common repository escape error = %v", err)
		}
		if _, err := os.Stat(outside); err != nil {
			t.Fatalf("common repository escape changed outside target before rejection: %v", err)
		}
	})
	t.Run("protected root overlap", func(t *testing.T) {
		root := t.TempDir()
		opts := newOptions(root)
		protected := filepath.Join(opts.WorktreeRoot, "agent1", "ouroboros-ide")
		if err := os.MkdirAll(protected, 0o700); err != nil {
			t.Fatal(err)
		}
		sentinel := filepath.Join(protected, "preserve")
		if err := os.WriteFile(sentinel, []byte("preserve\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		opts.ProtectedRoots = []string{protected}
		if _, err := PlanNativeReset(opts); err == nil || !strings.Contains(err.Error(), "overlaps protected root") {
			t.Fatalf("protected overlap error = %v", err)
		}
		if _, err := os.Stat(sentinel); err != nil {
			t.Fatalf("protected overlap changed payload before rejection: %v", err)
		}
	})
	t.Run("mount overlap", func(t *testing.T) {
		root := t.TempDir()
		opts := newOptions(root)
		sentinel := filepath.Join(opts.WorktreeRoot, "agent1", "preserve")
		if err := os.MkdirAll(filepath.Dir(sentinel), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sentinel, []byte("preserve\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		nativeResetMountPoints = func() ([]string, error) {
			return []string{filepath.Join(opts.WorktreeRoot, "agent1", "mounted")}, nil
		}
		defer func() { nativeResetMountPoints = func() ([]string, error) { return []string{"/"}, nil } }()
		if _, err := PlanNativeReset(opts); err == nil || !strings.Contains(err.Error(), "contains mount point") {
			t.Fatalf("mount overlap error = %v", err)
		}
		if _, err := os.Stat(sentinel); err != nil {
			t.Fatalf("mount overlap changed payload before rejection: %v", err)
		}
	})
}

func TestNativeResetRevalidatesCompleteBoundaryBeforeDisposal(t *testing.T) {
	originalMountPoints := nativeResetMountPoints
	t.Cleanup(func() { nativeResetMountPoints = originalMountPoints })
	nativeResetMountPoints = func() ([]string, error) { return []string{"/"}, nil }

	root := t.TempDir()
	worktreeRoot := filepath.Join(root, "owned-worktrees")
	stateRoot := filepath.Join(root, "owned-state")
	agentTwoSentinel := filepath.Join(worktreeRoot, "agent2", "preserve")
	if err := os.MkdirAll(filepath.Dir(agentTwoSentinel), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentTwoSentinel, []byte("preserve\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanNativeReset(NativeResetOptions{
		Project:      "dev-all",
		Repo:         "ouroboros-ide",
		Count:        2,
		WorktreeRoot: worktreeRoot,
		StateRoot:    stateRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(worktreeRoot, "agent1")); err != nil {
		t.Fatal(err)
	}
	if err := plan.Apply(); err == nil || !strings.Contains(err.Error(), "revalidate native reset boundary") {
		t.Fatalf("apply error = %v, want full boundary revalidation", err)
	}
	if got, err := os.ReadFile(agentTwoSentinel); err != nil || string(got) != "preserve\n" {
		t.Fatalf("boundary revalidation disposed another slot before rejecting escape: data=%q error=%v", got, err)
	}
}

func TestNativeSlotResetDisposesOnlySelectedSlot(t *testing.T) {
	originalMountPoints := nativeResetMountPoints
	t.Cleanup(func() { nativeResetMountPoints = originalMountPoints })
	nativeResetMountPoints = func() ([]string, error) { return []string{"/"}, nil }

	root := t.TempDir()
	worktreeRoot := filepath.Join(root, "owned-worktrees")
	stateRoot := filepath.Join(root, "owned-state")
	selectedWorktree := filepath.Join(worktreeRoot, "agent1", "ouroboros-ide")
	selectedState := filepath.Join(stateRoot, "dev-all-agent1")
	siblingWorktree := filepath.Join(worktreeRoot, "agent2", "ouroboros-ide")
	siblingHome := filepath.Join(worktreeRoot, "agent2", ".devhome-agent2")
	siblingState := filepath.Join(stateRoot, "dev-all-agent2")
	for _, path := range []string{selectedWorktree, selectedState, siblingWorktree, siblingHome, siblingState} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "sentinel"), []byte(path+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := PlanNativeSlotReset(NativeSlotResetOptions{
		Project:      "dev-all",
		Repo:         "ouroboros-ide",
		Index:        1,
		Count:        2,
		WorktreeRoot: worktreeRoot,
		StateRoot:    stateRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{selectedWorktree, selectedState} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("selected slot path survived %s: %v", path, err)
		}
	}
	for _, path := range []string{siblingWorktree, siblingHome, siblingState} {
		data, err := os.ReadFile(filepath.Join(path, "sentinel"))
		if err != nil || string(data) != path+"\n" {
			t.Fatalf("sibling path changed %s: %q %v", path, data, err)
		}
	}
}

func TestNativeSlotResetDisposesReadOnlyCachesAndOwnedQuarantineResidue(t *testing.T) {
	originalMountPoints := nativeResetMountPoints
	t.Cleanup(func() { nativeResetMountPoints = originalMountPoints })
	nativeResetMountPoints = func() ([]string, error) { return []string{"/"}, nil }

	root := t.TempDir()
	worktreeRoot := filepath.Join(root, "owned-worktrees")
	stateRoot := filepath.Join(root, "owned-state")
	agentRoot := filepath.Join(worktreeRoot, "agent1")
	selectedWorktree := filepath.Join(agentRoot, "ouroboros-ide")
	selectedState := filepath.Join(stateRoot, "dev-all-agent1")
	siblingState := filepath.Join(stateRoot, "dev-all-agent2")
	legacyAgentQuarantine := filepath.Join(agentRoot, ".devkit-reset-12345")
	legacyStateQuarantine := filepath.Join(stateRoot, ".devkit-reset-67890")
	ownedAgentQuarantine := filepath.Join(agentRoot, ".devkit-reset-dev-all-agent1-stale")
	outside := filepath.Join(root, "outside")

	for _, path := range []string{
		filepath.Join(selectedWorktree, ".devhome-agent1", "go", "pkg", "mod", "github.com", "dustin", "go-humanize@v1.0.1"),
		selectedState,
		siblingState,
		filepath.Join(legacyAgentQuarantine, "readonly"),
		filepath.Join(legacyStateQuarantine, "readonly"),
		filepath.Join(ownedAgentQuarantine, "readonly"),
		outside,
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	readOnlyCache := filepath.Join(selectedWorktree, ".devhome-agent1", "go", "pkg", "mod", "github.com", "dustin", "go-humanize@v1.0.1")
	if err := os.WriteFile(filepath.Join(readOnlyCache, "ftoa_test.go"), []byte("disposable cache\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	outsideSentinel := filepath.Join(outside, "preserve")
	if err := os.WriteFile(outsideSentinel, []byte("outside reset boundary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(readOnlyCache, "outside-link")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{readOnlyCache, filepath.Dir(readOnlyCache), filepath.Dir(filepath.Dir(readOnlyCache))} {
		if err := os.Chmod(path, 0o500); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(legacyAgentQuarantine, "readonly"),
		filepath.Join(legacyStateQuarantine, "readonly"),
		filepath.Join(ownedAgentQuarantine, "readonly"),
	} {
		if err := os.WriteFile(filepath.Join(path, "payload"), []byte("discard\n"), 0o400); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o500); err != nil {
			t.Fatal(err)
		}
	}
	siblingSentinel := filepath.Join(siblingState, "preserve")
	if err := os.WriteFile(siblingSentinel, []byte("sibling remains exact\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanNativeSlotReset(NativeSlotResetOptions{
		Project:      "dev-all",
		Repo:         "ouroboros-ide",
		Index:        1,
		Count:        2,
		WorktreeRoot: worktreeRoot,
		StateRoot:    stateRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		selectedWorktree,
		selectedState,
		legacyAgentQuarantine,
		legacyStateQuarantine,
		ownedAgentQuarantine,
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("reset-owned path survived %s: %v", path, err)
		}
	}
	if got, err := os.ReadFile(siblingSentinel); err != nil || string(got) != "sibling remains exact\n" {
		t.Fatalf("sibling state changed: %q %v", got, err)
	}
	if got, err := os.ReadFile(outsideSentinel); err != nil || string(got) != "outside reset boundary\n" {
		t.Fatalf("payload symlink acquired outside custody: %q %v", got, err)
	}
}

func TestNativeSlotResetRejectsInvalidIndexAndSelectedEscapesBeforeDisposal(t *testing.T) {
	originalMountPoints := nativeResetMountPoints
	t.Cleanup(func() { nativeResetMountPoints = originalMountPoints })
	nativeResetMountPoints = func() ([]string, error) { return []string{"/"}, nil }

	newOptions := func(root string) NativeSlotResetOptions {
		return NativeSlotResetOptions{
			Project:      "dev-all",
			Repo:         "ouroboros-ide",
			Index:        1,
			Count:        2,
			WorktreeRoot: filepath.Join(root, "worktrees"),
			StateRoot:    filepath.Join(root, "state"),
		}
	}
	t.Run("out of range", func(t *testing.T) {
		opts := newOptions(t.TempDir())
		opts.Index = 3
		if _, err := PlanNativeSlotReset(opts); err == nil || !strings.Contains(err.Error(), "outside declared capacity") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		root := t.TempDir()
		opts := newOptions(root)
		outside := filepath.Join(root, "outside")
		if err := os.MkdirAll(outside, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(opts.WorktreeRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(opts.WorktreeRoot, "agent1")); err != nil {
			t.Fatal(err)
		}
		if _, err := PlanNativeSlotReset(opts); err == nil || !strings.Contains(err.Error(), "symlink or junction") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("mount", func(t *testing.T) {
		root := t.TempDir()
		opts := newOptions(root)
		selected := filepath.Join(opts.WorktreeRoot, "agent1", "ouroboros-ide")
		if err := os.MkdirAll(selected, 0o700); err != nil {
			t.Fatal(err)
		}
		nativeResetMountPoints = func() ([]string, error) { return []string{filepath.Join(selected, "mounted")}, nil }
		if _, err := PlanNativeSlotReset(opts); err == nil || !strings.Contains(err.Error(), "contains mount point") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("protected root", func(t *testing.T) {
		root := t.TempDir()
		opts := newOptions(root)
		selected := filepath.Join(opts.WorktreeRoot, "agent1", "ouroboros-ide")
		if err := os.MkdirAll(selected, 0o700); err != nil {
			t.Fatal(err)
		}
		opts.ProtectedRoots = []string{selected}
		if _, err := PlanNativeSlotReset(opts); err == nil || !strings.Contains(err.Error(), "overlaps protected root") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestSetupNativeSSHOriginRejectsMissingBootstrapCommand(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	makeRepoWithBare(t, root, devRoot, "ouroboros-ide")
	repo := filepath.Join(devRoot, "ouroboros-ide")
	mustRun(t, "git", "-C", repo, "remote", "set-url", "origin", "git@fixture.invalid:ouroboros-ide.git")

	err := SetupNative(NativeOptions{
		DevkitRoot: devkitRoot,
		Repo:       "ouroboros-ide",
		Count:      1,
		DryRun:     true,
	})
	if err == nil || !strings.Contains(err.Error(), "requires a package-owned SSH command") {
		t.Fatalf("SetupNative error = %v, want package-owned SSH command rejection", err)
	}
}

func TestSetupNativeProductCheckoutRejectsHTTPSFallback(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	makeRepoWithBare(t, root, devRoot, "ouroboros-ide")
	repo := filepath.Join(devRoot, "ouroboros-ide")
	mustRun(t, "git", "-C", repo, "remote", "set-url", "origin", "https://github.com/Divine-Shadow/ouroboros-ide.git")

	err := SetupNative(NativeOptions{
		DevkitRoot:       devkitRoot,
		Repo:             "ouroboros-ide",
		Origin:           "https://github.com/Divine-Shadow/ouroboros-ide.git",
		Count:            1,
		GitSSHCommand:    "/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-openssh/bin/ssh -F /nix/store/package-owned/config",
		RequireSSHOrigin: true,
		DryRun:           true,
	})
	if err == nil || !strings.Contains(err.Error(), "HTTPS, file, and ambient transport fallbacks are prohibited") {
		t.Fatalf("SetupNative error = %v, want HTTPS fallback rejection", err)
	}
}

func TestSetupNativeProductCheckoutRejectsAmbientCheckoutOriginAuthority(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	makeRepoWithBare(t, root, devRoot, "ouroboros-ide")
	repo := filepath.Join(devRoot, "ouroboros-ide")
	mustRun(t, "git", "-C", repo, "remote", "set-url", "origin", "ssh://git@fixture.invalid/ambient.git")

	err := SetupNative(NativeOptions{
		DevkitRoot:       devkitRoot,
		Repo:             "ouroboros-ide",
		Count:            1,
		GitSSHCommand:    "/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-openssh/bin/ssh -F /nix/store/package-owned/config",
		RequireSSHOrigin: true,
		DryRun:           true,
	})
	if err == nil || !strings.Contains(err.Error(), "ambient checkout remotes are not bootstrap authority") {
		t.Fatalf("SetupNative error = %v, want ambient-origin rejection", err)
	}
}

func TestSetupNativeProductCheckoutDoesNotReuseWorktreeAfterFetchFailure(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	makeRepoWithBare(t, root, devRoot, "ouroboros-ide")
	if err := SetupNative(NativeOptions{
		DevkitRoot: devkitRoot,
		Repo:       "ouroboros-ide",
		Count:      1,
	}); err != nil {
		t.Fatalf("prepare existing worktree: %v", err)
	}
	repo := filepath.Join(devRoot, "ouroboros-ide")
	worktree := filepath.Join(devRoot, paths.AgentWorktreesDir, "agent1", "ouroboros-ide")
	worktreeRoot := filepath.Join(devRoot, paths.AgentWorktreesDir)
	commonDir, pathErr := nativeOwnedCommonRepositoryPath(worktreeRoot, "ouroboros-ide")
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	mustRun(t, "git", "--git-dir", commonDir, "worktree", "remove", "--force", worktree)
	missingOrigin := "ssh://git@fixture.invalid/missing.git"
	mustRun(t, "git", "-C", repo, "remote", "set-url", "origin", missingOrigin)
	mustRun(t, "git", "--git-dir", commonDir, "remote", "set-url", "origin", missingOrigin)
	marker, markerErr := nativeOwnedCommonRepositoryMarker("ouroboros-ide", missingOrigin)
	if markerErr != nil {
		t.Fatal(markerErr)
	}
	if err := os.WriteFile(filepath.Join(commonDir, "devkit-owned-common"), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}

	err := SetupNative(NativeOptions{
		DevkitRoot:       devkitRoot,
		Repo:             "ouroboros-ide",
		Origin:           missingOrigin,
		Count:            1,
		GitSSHCommand:    "/bin/false",
		RequireSSHOrigin: true,
	})
	if err == nil || !strings.Contains(err.Error(), "fetch --all --prune") {
		t.Fatalf("SetupNative error = %v, want fail-closed Product fetch error", err)
	}
	if _, statErr := os.Stat(worktree); !os.IsNotExist(statErr) {
		t.Fatalf("failed Product fetch materialized worktree %s: %v", worktree, statErr)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
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

func TestSetupNative_FromLinkedSourceWorktreeUsesOwnedPortableCommonRepository(t *testing.T) {
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
	checkGitdirForm(t, agentWorktree, false)
	checkGitdirForm(t, sourceRepo, true)
	commonDir, err := nativeOwnedCommonRepositoryPath(
		filepath.Join(sourceDevRoot, paths.AgentWorktreesDir),
		"ouroboros-ide-nix-readiness",
	)
	if err != nil {
		t.Fatal(err)
	}
	checkRelativeNativeMetadata(t, agentWorktree, commonDir, primaryDevRoot)
}

func TestSetupNative_RejectsStandaloneAgentCheckoutAsForeignAuthority(t *testing.T) {
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

	err := SetupNative(NativeOptions{
		DevkitRoot:   devkitRoot,
		Repo:         "ouroboros-ide",
		Count:        1,
		BaseBranch:   "main",
		BranchPrefix: "agent",
	})
	if err == nil || !strings.Contains(err.Error(), "standalone checkout") {
		t.Fatalf("native setup error = %v, want standalone foreign-authority rejection", err)
	}
	commonDir, pathErr := nativeOwnedCommonRepositoryPath(
		filepath.Join(devRoot, paths.AgentWorktreesDir),
		"ouroboros-ide",
	)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if _, statErr := os.Stat(commonDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("foreign standalone preflight created common repository %s: %v", commonDir, statErr)
	}
	if info, err := os.Stat(filepath.Join(standalone, ".git")); err != nil || !info.IsDir() {
		t.Fatalf("standalone checkout .git dir was not preserved: %v", err)
	}
	if got := readTrim(t, "git", "-C", standalone, "rev-parse", "--show-toplevel"); filepath.Clean(got) != standalone {
		t.Fatalf("unexpected standalone toplevel: %s", got)
	}
}

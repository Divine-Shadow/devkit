package launch

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/sshauthority"
)

type Command struct {
	Path string
	Args []string
	Dir  string
}

func Prepare(p nativeplan.Plan) error {
	sshAuthority, err := resolvePackageSSHAuthority()
	if err != nil {
		return err
	}
	if strings.TrimSpace(p.Agent.HostWorktree) == "" {
		return fmt.Errorf("host worktree is empty")
	}
	if st, err := os.Stat(p.Agent.HostWorktree); err != nil {
		return fmt.Errorf("host worktree %s: %w", p.Agent.HostWorktree, err)
	} else if !st.IsDir() {
		return fmt.Errorf("host worktree %s is not a directory", p.Agent.HostWorktree)
	}
	if err := requireGitWorktree(p.Agent.HostWorktree); err != nil {
		return err
	}
	for _, dir := range []string{p.Agent.HostHome, p.Agent.StateRoot} {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	for _, rel := range []string{
		filepath.Join(".codex", "rollouts"),
		".cache",
		".config",
		".local",
		".sbt",
	} {
		dir := filepath.Join(p.Agent.HostHome, rel)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	if err := migrateMissingCodexState(p.Agent.HostHome, filepath.Join(p.Agent.StateRoot, "home")); err != nil {
		return err
	}
	if err := ensureCodexShellHook(p); err != nil {
		return err
	}
	if err := ensureWorkspaceSkillLinks(p); err != nil {
		return err
	}
	if err := ensureScalaCacheDirs(p); err != nil {
		return err
	}
	if err := capCodexTUILog(p.Agent.HostHome); err != nil {
		return err
	}
	if err := SeedCodexAuth(p.Agent.HostHome, false); err != nil {
		return err
	}
	if err := SeedSSH(p.Agent.HostHome, false); err != nil {
		return err
	}
	if err := SeedAWS(p.Agent.HostHome, false); err != nil {
		return err
	}
	if err := ensureGitSSHConfig(p, sshAuthority); err != nil {
		return err
	}
	if err := ensureResolvConf(p.DNS.ResolvConf); err != nil {
		return err
	}
	for _, bind := range p.Binds {
		if !bind.Required {
			continue
		}
		if _, err := os.Stat(bind.Source); err != nil {
			return fmt.Errorf("required bind source %s: %w", bind.Source, err)
		}
	}
	return nil
}

func requireGitWorktree(worktree string) error {
	cmd := exec.Command("git", "-C", worktree, "rev-parse", "--show-toplevel")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("host worktree %s is not a Git worktree: %w: %s", worktree, err, strings.TrimSpace(string(out)))
	}
	top := filepath.Clean(strings.TrimSpace(string(out)))
	expected := filepath.Clean(worktree)
	if resolved, err := filepath.EvalSymlinks(top); err == nil {
		top = resolved
	}
	if resolved, err := filepath.EvalSymlinks(expected); err == nil {
		expected = resolved
	}
	if top != expected {
		return fmt.Errorf("host worktree %s resolves to foreign Git root %s", worktree, top)
	}
	return nil
}

const (
	gitSSHManagedBegin = "# BEGIN DEVKIT NATIVE GIT SSH"
	gitSSHManagedEnd   = "# END DEVKIT NATIVE GIT SSH"
)

var resolvePackageSSHAuthority = sshauthority.Package

func ensureGitSSHConfig(p nativeplan.Plan, sshAuthority sshauthority.Authority) error {
	hostHome := strings.TrimSpace(p.Agent.HostHome)
	sandboxHome := strings.TrimSpace(p.Agent.SandboxHome)
	if hostHome == "" || sandboxHome == "" {
		return nil
	}
	sshDir := filepath.Join(hostHome, ".ssh")
	identityNames := existingSSHIdentities(sshDir)
	if len(identityNames) == 0 {
		return nil
	}
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", sshDir, err)
	}
	if err := sshAuthority.InstallKnownHosts(filepath.Join(sshDir, "known_hosts")); err != nil {
		return err
	}
	sshCommand, err := sshAuthority.Command(filepath.Join(sandboxHome, ".ssh", "config"))
	if err != nil {
		return err
	}
	proxyURL := ""
	if p.IsolationProfile == nativeplan.IsolationProfileWorkspaceEgress {
		proxyURL = p.Proxy.HTTPProxy
	}
	if err := writeGitSSHConfig(hostHome, sandboxHome, identityNames, proxyURL); err != nil {
		return err
	}
	if err := runGitConfigFile(filepath.Join(hostHome, ".gitconfig"), "core.sshCommand", sshCommand); err != nil {
		return err
	}
	if !isDevWorkspacePlan(p) {
		if err := configureWorktreeGitSSH(p.Agent.HostWorktree, sshCommand); err != nil {
			return err
		}
	}
	return nil
}

// GitBootstrapSSHCommand returns the package-owned host command used by
// native prepare before a linked worktree exists. The bootstrap command must
// use the per-consumer home; the normal runtime preparation later rewrites the
// same managed config block to sandbox-visible identity paths.
func GitBootstrapSSHCommand(p nativeplan.Plan) (string, error) {
	hostHome := strings.TrimSpace(p.Agent.HostHome)
	if hostHome == "" {
		return "", fmt.Errorf("native Git bootstrap requires an agent host home")
	}
	sshAuthority, err := resolvePackageSSHAuthority()
	if err != nil {
		return "", err
	}
	return sshAuthority.Command(filepath.Join(hostHome, ".ssh", "config"))
}

// PrepareGitBootstrap seeds and configures only the package-owned SSH identity
// needed for the first remote fetch. It deliberately does not require or
// configure a Git worktree because native prepare calls it before materializing
// the linked worktree.
func PrepareGitBootstrap(p nativeplan.Plan) (string, error) {
	sshAuthority, err := resolvePackageSSHAuthority()
	if err != nil {
		return "", err
	}
	hostHome := strings.TrimSpace(p.Agent.HostHome)
	sshCommand, err := sshAuthority.Command(filepath.Join(hostHome, ".ssh", "config"))
	if err != nil {
		return "", err
	}
	if err := SeedSSH(hostHome, false); err != nil {
		return "", err
	}
	if err := sshAuthority.InstallKnownHosts(filepath.Join(hostHome, ".ssh", "known_hosts")); err != nil {
		return "", err
	}
	identityNames := existingSSHIdentities(filepath.Join(hostHome, ".ssh"))
	if len(identityNames) == 0 {
		return "", fmt.Errorf("native Git bootstrap requires a seeded SSH identity in %s", filepath.Join(hostHome, ".ssh"))
	}
	proxyCommand := ""
	if p.IsolationProfile == nativeplan.IsolationProfileWorkspaceEgress {
		proxyCommand, err = gitBootstrapProxyCommand(p)
		if err != nil {
			return "", err
		}
	}
	if err := writeGitSSHConfigWithProxyCommand(hostHome, hostHome, identityNames, proxyCommand); err != nil {
		return "", err
	}
	if !isDevWorkspacePlan(p) {
		if err := configureWorktreeGitSSH(p.Agent.HostWorktree, sshCommand); err != nil {
			return "", err
		}
	}
	return sshCommand, nil
}

func gitBootstrapProxyCommand(p nativeplan.Plan) (string, error) {
	runtimeRoot := strings.TrimSpace(p.RuntimeAuthorityRoot)
	if runtimeRoot == "" {
		return "", fmt.Errorf("native Git bootstrap requires a source-derived runtime authority root")
	}
	project := strings.TrimSpace(p.Agent.ID.Project)
	if project == "" {
		return "", fmt.Errorf("native Git bootstrap requires a project identity")
	}
	socketPath := strings.TrimSpace(p.Proxy.UnixSocket)
	if socketPath == "" {
		return "", fmt.Errorf("native Git bootstrap requires a managed egress proxy socket")
	}
	devctlPath := filepath.Join(filepath.Clean(runtimeRoot), "kit", "bin", "devctl")
	if !isExecutable(devctlPath) {
		return "", fmt.Errorf("native Git bootstrap requires package-owned proxy helper %s", devctlPath)
	}
	return strings.Join([]string{
		shellQuote(devctlPath),
		"-p", shellQuote(project),
		"native", "proxy-connect",
		"--socket", shellQuote(socketPath),
		"--target", "%h:%p",
	}, " "), nil
}

func existingSSHIdentities(sshDir string) []string {
	var names []string
	for _, name := range []string{"id_ed25519", "id_rsa"} {
		if st, err := os.Stat(filepath.Join(sshDir, name)); err == nil && st.Mode().IsRegular() {
			names = append(names, name)
		}
	}
	return names
}

func WriteGitSSHConfig(hostHome, configHome string, identityNames []string) error {
	return writeGitSSHConfig(hostHome, configHome, identityNames, "")
}

func writeGitSSHConfig(hostHome, configHome string, identityNames []string, proxyURL string) error {
	proxyCommand := ""
	if parsed, err := url.Parse(strings.TrimSpace(proxyURL)); err == nil && parsed.Scheme == "http" && parsed.Host != "" {
		proxyCommand = "nc -X connect -x " + parsed.Host + " %h %p"
	}
	return writeGitSSHConfigWithProxyCommand(hostHome, configHome, identityNames, proxyCommand)
}

func writeGitSSHConfigWithProxyCommand(hostHome, configHome string, identityNames []string, proxyCommand string) error {
	hostHome = strings.TrimSpace(hostHome)
	configHome = strings.TrimSpace(configHome)
	if hostHome == "" || configHome == "" {
		return nil
	}
	sshDir := filepath.Join(hostHome, ".ssh")
	if len(identityNames) == 0 {
		identityNames = existingSSHIdentities(sshDir)
	}
	if len(identityNames) == 0 {
		return nil
	}
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", sshDir, err)
	}
	cfgPath := filepath.Join(sshDir, "config")
	cfg := buildGitSSHConfigWithProxyCommand(configHome, identityNames, proxyCommand)
	if err := writeManagedBlock(cfgPath, cfg, 0o600); err != nil {
		return err
	}
	return nil
}

func buildGitSSHConfig(configHome string, identityNames []string, proxyURL string) string {
	proxyCommand := ""
	if parsed, err := url.Parse(strings.TrimSpace(proxyURL)); err == nil && parsed.Scheme == "http" && parsed.Host != "" {
		proxyCommand = "nc -X connect -x " + parsed.Host + " %h %p"
	}
	return buildGitSSHConfigWithProxyCommand(configHome, identityNames, proxyCommand)
}

func buildGitSSHConfigWithProxyCommand(configHome string, identityNames []string, proxyCommand string) string {
	var b strings.Builder
	b.WriteString(gitSSHManagedBegin + "\n")
	b.WriteString("Host github.com ssh.github.com\n")
	if proxyCommand = strings.TrimSpace(proxyCommand); proxyCommand != "" {
		b.WriteString("  HostName ssh.github.com\n")
		b.WriteString("  Port 443\n")
		b.WriteString("  ProxyCommand " + proxyCommand + "\n")
	}
	b.WriteString("  User git\n")
	for _, name := range identityNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		b.WriteString("  IdentityFile " + filepath.Join(configHome, ".ssh", name) + "\n")
	}
	b.WriteString("  IdentitiesOnly yes\n")
	b.WriteString("  BatchMode yes\n")
	b.WriteString("  StrictHostKeyChecking yes\n")
	b.WriteString("  UserKnownHostsFile " + filepath.Join(configHome, ".ssh", "known_hosts") + "\n")
	b.WriteString(gitSSHManagedEnd + "\n")
	return b.String()
}

func writeManagedBlock(path string, block string, mode os.FileMode) error {
	original := ""
	if data, err := os.ReadFile(path); err == nil {
		original = string(data)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	rest := removeManagedGitSSHBlocks(original)
	// Native agent Git points directly at this SSH config. Keep github.com
	// authoritative so stale devkit-era Host blocks cannot be merged by ssh(1).
	rest = removeGitHubHostBlocks(rest)
	next := strings.TrimRight(block, "\r\n") + "\n"
	if strings.TrimSpace(rest) != "" {
		next += "\n" + strings.TrimLeft(rest, "\r\n")
	}
	if err := os.WriteFile(path, []byte(next), mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func removeManagedGitSSHBlocks(config string) string {
	for {
		start := strings.Index(config, gitSSHManagedBegin)
		if start < 0 {
			return config
		}
		endRel := strings.Index(config[start:], gitSSHManagedEnd)
		if endRel < 0 {
			return config[:start]
		}
		end := start + endRel + len(gitSSHManagedEnd)
		config = strings.TrimRight(config[:start], "\r\n") + "\n" + strings.TrimLeft(config[end:], "\r\n")
	}
}

func removeGitHubHostBlocks(config string) string {
	lines := strings.SplitAfter(config, "\n")
	var kept []string
	for i := 0; i < len(lines); {
		if isGitHubHostLine(lines[i]) {
			i++
			for i < len(lines) && !startsSSHHostOrMatchBlock(lines[i]) {
				i++
			}
			continue
		}
		kept = append(kept, lines[i])
		i++
	}
	return strings.Join(kept, "")
}

func isGitHubHostLine(line string) bool {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 2 || !strings.EqualFold(fields[0], "Host") {
		return false
	}
	for _, pattern := range fields[1:] {
		if pattern == "github.com" {
			return true
		}
	}
	return false
}

func startsSSHHostOrMatchBlock(line string) bool {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return false
	}
	return strings.EqualFold(fields[0], "Host") || strings.EqualFold(fields[0], "Match")
}

func runGitConfigFile(configPath, key, value string) error {
	if strings.TrimSpace(configPath) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(configPath), err)
	}
	if out, err := runGitConfigWithLockRetry("config", "--file", configPath, key, value); err != nil {
		return fmt.Errorf("git config --file %s %s: %w: %s", configPath, key, err, strings.TrimSpace(string(out)))
	}
	if err := os.Chmod(configPath, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", configPath, err)
	}
	return nil
}

func configureWorktreeGitSSH(worktree string, sshCommand string) error {
	worktree = strings.TrimSpace(worktree)
	if worktree == "" {
		return nil
	}
	gitMarker := filepath.Join(worktree, ".git")
	if _, err := os.Lstat(gitMarker); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect Git metadata marker %s: %w", gitMarker, err)
	}
	out, err := exec.Command("git", "-C", worktree, "rev-parse", "--git-dir").CombinedOutput()
	if err != nil {
		return fmt.Errorf("resolve Git metadata for %s: %w: %s", worktree, err, strings.TrimSpace(string(out)))
	}
	gitDir := filepath.Clean(strings.TrimSpace(string(out)))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktree, gitDir)
	}
	if out, err := runGitConfigWithLockRetry("--git-dir", gitDir, "config", "extensions.worktreeConfig", "true"); err != nil {
		return fmt.Errorf("git config extensions.worktreeConfig in %s: %w: %s", worktree, err, strings.TrimSpace(string(out)))
	}
	// Package-owned native worktrees are linked from a bare common
	// repository. Once worktreeConfig is enabled, preserve the selected
	// worktree's non-bare identity explicitly; otherwise the shared
	// core.bare=true makes subsequent Git and libgit2 opens lose the worktree.
	if out, err := runGitConfigWithLockRetry("--git-dir", gitDir, "config", "--worktree", "core.bare", "false"); err != nil {
		return fmt.Errorf("git config --worktree core.bare in %s: %w: %s", worktree, err, strings.TrimSpace(string(out)))
	}
	if out, err := runGitConfigWithLockRetry("--git-dir", gitDir, "config", "--worktree", "core.sshCommand", sshCommand); err != nil {
		return fmt.Errorf("git config --worktree core.sshCommand in %s: %w: %s", worktree, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runGitConfigWithLockRetry(args ...string) ([]byte, error) {
	var out []byte
	var err error
	for attempt := 0; attempt < 6; attempt++ {
		out, err = exec.Command("git", args...).CombinedOutput()
		if err == nil || !isGitConfigLockError(out) {
			return out, err
		}
		time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
	}
	return out, err
}

func isGitConfigLockError(out []byte) bool {
	text := strings.ToLower(string(out))
	return strings.Contains(text, "could not lock config file") || strings.Contains(text, "file exists")
}

func ensureScalaCacheDirs(p nativeplan.Plan) error {
	for _, key := range []string{"COURSIER_CACHE", "SBT_IVY_HOME", "SBT_GLOBAL_BASE", "SBT_BOOT_DIR"} {
		sandboxPath := strings.TrimSpace(p.Env[key])
		if sandboxPath == "" {
			continue
		}
		hostPath, ok := sandboxPathToHost(p, sandboxPath)
		if !ok {
			continue
		}
		if err := os.MkdirAll(hostPath, 0o755); err != nil {
			return fmt.Errorf("mkdir %s for %s: %w", hostPath, key, err)
		}
	}
	return nil
}

func sandboxPathToHost(p nativeplan.Plan, sandboxPath string) (string, bool) {
	sandboxPath = filepath.Clean(sandboxPath)
	if strings.TrimSpace(p.Agent.SandboxHome) != "" {
		if rel, err := filepath.Rel(filepath.Clean(p.Agent.SandboxHome), sandboxPath); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.Join(p.Agent.HostHome, rel), true
		}
	}
	const workspaceRoot = "/workspaces/dev"
	if rel, err := filepath.Rel(workspaceRoot, sandboxPath); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.Join(filepath.Dir(p.DevkitHostRoot), rel), true
	}
	return "", false
}

const defaultCodexTUILogMaxBytes int64 = 256 * 1024 * 1024

func codexTUILogMaxBytes() (int64, error) {
	raw := strings.TrimSpace(os.Getenv("DEVKIT_CODEX_TUI_LOG_MAX_BYTES"))
	if raw == "" {
		return defaultCodexTUILogMaxBytes, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid DEVKIT_CODEX_TUI_LOG_MAX_BYTES=%q: %w", raw, err)
	}
	return value, nil
}

func capCodexTUILog(hostHome string) error {
	maxBytes, err := codexTUILogMaxBytes()
	if err != nil {
		return err
	}
	if maxBytes <= 0 || strings.TrimSpace(hostHome) == "" {
		return nil
	}
	path := filepath.Join(hostHome, ".codex", "log", "codex-tui.log")
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Size() <= maxBytes {
		return nil
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	if _, err := file.Seek(info.Size()-maxBytes, io.SeekStart); err != nil {
		return fmt.Errorf("seek %s: %w", path, err)
	}
	tail, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read tail from %s: %w", path, err)
	}
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("truncate %s: %w", path, err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind %s: %w", path, err)
	}
	if _, err := file.Write(tail); err != nil {
		return fmt.Errorf("write capped tail to %s: %w", path, err)
	}
	return nil
}

func ensureCodexShellHook(p nativeplan.Plan) error {
	return repairRetiredCodexShellHook(p.Agent.HostHome)
}

func isDevWorkspacePlan(p nativeplan.Plan) bool {
	return strings.TrimSpace(p.Agent.ID.Project) == "dev-workspace"
}

func ensureWorkspaceSkillLinks(p nativeplan.Plan) error {
	if !isDevWorkspacePlan(p) {
		return nil
	}
	hostHome := strings.TrimSpace(p.Agent.HostHome)
	hostDevRoot := hostDevRootForPlan(p)
	if hostHome == "" || hostDevRoot == "" {
		return nil
	}
	targetRoot := filepath.Join(hostHome, ".codex", "skills")
	if err := os.MkdirAll(targetRoot, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", targetRoot, err)
	}
	seen := map[string]bool{}
	for _, sourceRoot := range []string{
		filepath.Join(hostDevRoot, ".codex", "skills"),
		filepath.Join(hostDevRoot, "ouroboros-ide", ".codex", "skills"),
	} {
		entries, err := os.ReadDir(sourceRoot)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read skills %s: %w", sourceRoot, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Name() == ".system" || seen[entry.Name()] {
				continue
			}
			seen[entry.Name()] = true
			source := filepath.Join(sourceRoot, entry.Name())
			target := filepath.Join(targetRoot, entry.Name())
			if err := ensureSkillSymlink(target, source); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureSkillSymlink(target, source string) error {
	target = filepath.Clean(target)
	source = filepath.Clean(source)
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		if existing, err := os.Readlink(target); err == nil && filepath.Clean(existing) == source {
			return nil
		}
		if err := os.Remove(target); err != nil {
			return fmt.Errorf("remove stale skill link %s: %w", target, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat skill link %s: %w", target, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(target), err)
	}
	if err := os.Symlink(source, target); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", target, source, err)
	}
	return nil
}

func hostDevRootForPlan(p nativeplan.Plan) string {
	for _, bind := range p.Binds {
		if filepath.Clean(strings.TrimSpace(bind.Target)) == "/workspaces/dev" {
			source := strings.TrimSpace(bind.Source)
			if source != "" {
				return filepath.Clean(source)
			}
		}
	}
	devkitRoot := strings.TrimSpace(p.DevkitHostRoot)
	if devkitRoot == "" {
		return ""
	}
	devkitRoot = filepath.Clean(devkitRoot)
	if filepath.Base(devkitRoot) == "devkit" {
		return filepath.Dir(devkitRoot)
	}
	return filepath.Dir(devkitRoot)
}

func repairRetiredCodexShellHook(hostHome string) error {
	hostHome = strings.TrimSpace(hostHome)
	if hostHome == "" {
		return nil
	}
	zshrc := filepath.Join(hostHome, ".zshrc")
	data, err := os.ReadFile(zshrc)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", zshrc, err)
	}
	original := string(data)
	repaired := original
	retired := "/usr/local/bin/" + "codex"
	if strings.Contains(repaired, retired) {
		repaired = strings.ReplaceAll(repaired, retired, "command codex")
	}
	if strings.Contains(repaired, "codex() {") && !strings.Contains(repaired, "devkit_codex_tui_log_guard()") {
		repaired = strings.Replace(repaired, "codex() {\n", codexTUILogGuardZsh+"\ncodex() {\n", 1)
	}
	oldCodexCommand := `  HOME="$HOME" CODEX_HOME="$HOME/.codex" CODEX_ROLLOUT_DIR="$HOME/.codex/rollouts" command codex "${` + `extra[@]` + `}" "$@"`
	const codexCommand = `  HOME="$HOME" CODEX_HOME="$HOME/.codex" CODEX_ROLLOUT_DIR="$HOME/.codex/rollouts" command codex "$@"`
	if strings.Contains(repaired, oldCodexCommand) {
		repaired = strings.Replace(repaired, oldCodexCommand, "  devkit_codex_tui_log_guard\n"+codexCommand, 1)
	}
	repaired = removeLegacyCodexExtraBlock(repaired)
	if repaired == original {
		return nil
	}
	if err := os.WriteFile(zshrc, []byte(repaired), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", zshrc, err)
	}
	return nil
}

func removeLegacyCodexExtraBlock(zshrc string) string {
	for {
		start := strings.Index(zshrc, "\n  local -a extra\n  extra=(\n")
		if start < 0 {
			return zshrc
		}
		blockStart := start + 1
		endRel := strings.Index(zshrc[blockStart:], "\n  )\n")
		if endRel < 0 {
			return zshrc
		}
		blockEnd := blockStart + endRel + len("\n  )\n")
		zshrc = zshrc[:blockStart] + zshrc[blockEnd:]
	}
}

const codexTUILogGuardZsh = `devkit_codex_tui_log_guard() {
  local log="$HOME/.codex/log/codex-tui.log"
  local max="${DEVKIT_CODEX_TUI_LOG_MAX_BYTES:-268435456}"
  [[ "$max" == <-> ]] || return 0
  (( max > 0 )) || return 0
  [[ -f "$log" ]] || return 0
  local size tmp
  size=$(wc -c < "$log" 2>/dev/null) || return 0
  (( size > max )) || return 0
  tmp="${log}.tmp.$$"
  tail -c "$max" "$log" > "$tmp" 2>/dev/null && cat "$tmp" > "$log"
  rm -f "$tmp"
}`

func migrateMissingCodexState(dstHome, srcHome string) error {
	dstHome = strings.TrimSpace(dstHome)
	srcHome = strings.TrimSpace(srcHome)
	if dstHome == "" || srcHome == "" || sameFilesystemPath(dstHome, srcHome) {
		return nil
	}
	srcCodex := filepath.Join(srcHome, ".codex")
	dstCodex := filepath.Join(dstHome, ".codex")
	if st, err := os.Stat(srcCodex); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat legacy Codex home %s: %w", srcCodex, err)
	} else if !st.IsDir() {
		return nil
	}
	if err := os.MkdirAll(dstCodex, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dstCodex, err)
	}
	for _, rel := range []string{"sessions", "rollouts", "shell_snapshots", "log"} {
		if err := copyTreeMissing(filepath.Join(srcCodex, rel), filepath.Join(dstCodex, rel)); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(srcCodex)
	if err != nil {
		return fmt.Errorf("read legacy Codex home %s: %w", srcCodex, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !shouldCopyCodexRootFile(entry.Name()) {
			continue
		}
		if err := copyFileMissing(filepath.Join(srcCodex, entry.Name()), filepath.Join(dstCodex, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func sameFilesystemPath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	if resolvedA, err := filepath.EvalSymlinks(a); err == nil {
		a = filepath.Clean(resolvedA)
	}
	if resolvedB, err := filepath.EvalSymlinks(b); err == nil {
		b = filepath.Clean(resolvedB)
	}
	return a == b
}

func shouldCopyCodexRootFile(name string) bool {
	switch name {
	case "history.jsonl", "installation_id", "models_cache.json", "version.json", ".personality_migration":
		return true
	}
	return strings.HasPrefix(name, "state_") || strings.HasPrefix(name, "logs_")
}

func copyTreeMissing(srcRoot, dstRoot string) error {
	if st, err := os.Stat(srcRoot); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", srcRoot, err)
	} else if !st.IsDir() {
		return nil
	}
	return filepath.WalkDir(srcRoot, func(src string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcRoot, src)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstRoot, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(dst, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFileMissing(src, dst)
	})
}

func copyFileMissing(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if _, err := os.Stat(dst); err == nil {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", dst, err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	if err := os.Chtimes(dst, info.ModTime(), info.ModTime()); err != nil {
		return fmt.Errorf("preserve mtime for %s: %w", dst, err)
	}
	return nil
}

func SeedSSH(hostHome string, force bool) error {
	hostHome = strings.TrimSpace(hostHome)
	if hostHome == "" {
		return nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(userHome) == "" {
		return nil
	}
	srcDir := filepath.Join(userHome, ".ssh")
	if st, err := os.Stat(srcDir); err != nil || !st.IsDir() {
		return nil
	}
	targetDir := filepath.Join(hostHome, ".ssh")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", targetDir, err)
	}
	for _, file := range []string{
		"id_ed25519",
		"id_ed25519.pub",
		"id_rsa",
		"id_rsa.pub",
	} {
		src := filepath.Join(srcDir, file)
		data, err := os.ReadFile(src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read SSH seed %s: %w", src, err)
		}
		target := filepath.Join(targetDir, file)
		if !force {
			if _, err := os.Stat(target); err == nil {
				continue
			} else if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("stat SSH seed target %s: %w", target, err)
			}
		}
		mode := os.FileMode(0o600)
		if strings.HasSuffix(file, ".pub") {
			mode = 0o644
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return fmt.Errorf("write SSH seed %s: %w", target, err)
		}
	}
	return nil
}

func SeedCodexAuth(hostHome string, force bool) error {
	hostHome = strings.TrimSpace(hostHome)
	if hostHome == "" {
		return nil
	}
	src := strings.TrimSpace(os.Getenv("CODEX_AUTH_JSON"))
	if src == "" {
		codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
		if codexHome == "" {
			if home, err := os.UserHomeDir(); err == nil {
				codexHome = filepath.Join(home, ".codex")
			}
		}
		if codexHome != "" {
			src = filepath.Join(codexHome, "auth.json")
		}
	}
	if src == "" {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read Codex auth %s: %w", src, err)
	}
	targetDir := filepath.Join(hostHome, ".codex")
	target := filepath.Join(targetDir, "auth.json")
	if !force {
		if _, err := os.Stat(target); err == nil {
			return nil
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("stat Codex auth %s: %w", target, err)
		}
	}
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", targetDir, err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return fmt.Errorf("write Codex auth %s: %w", target, err)
	}
	return nil
}

func SeedAWS(hostHome string, force bool) error {
	hostHome = strings.TrimSpace(hostHome)
	if hostHome == "" {
		return nil
	}
	srcAWS := strings.TrimSpace(os.Getenv("DEVKIT_AWS_HOME"))
	if srcAWS == "" {
		if configPath := strings.TrimSpace(os.Getenv("AWS_CONFIG_FILE")); configPath != "" {
			srcAWS = filepath.Dir(configPath)
		}
	}
	if srcAWS == "" {
		if home, err := os.UserHomeDir(); err == nil {
			srcAWS = filepath.Join(home, ".aws")
		}
	}
	if srcAWS == "" {
		return nil
	}
	if st, err := os.Stat(srcAWS); err != nil || !st.IsDir() {
		return nil
	}

	targetAWS := filepath.Join(hostHome, ".aws")
	if err := materializeAWSHome(targetAWS); err != nil {
		return err
	}
	if err := os.MkdirAll(targetAWS, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", targetAWS, err)
	}
	if err := os.Chmod(targetAWS, 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", targetAWS, err)
	}
	for _, rel := range []string{"sso", "cli"} {
		if err := ensureAWSDir(filepath.Join(targetAWS, rel)); err != nil {
			return err
		}
	}
	if err := copyAWSFile(filepath.Join(srcAWS, "config"), filepath.Join(targetAWS, "config"), 0o600, force); err != nil {
		return err
	}

	credentialsSource := filepath.Join(srcAWS, "credentials")
	if override := strings.TrimSpace(os.Getenv("AWS_SHARED_CREDENTIALS_FILE")); override != "" {
		credentialsSource = override
	}
	if err := copyAWSFile(credentialsSource, filepath.Join(targetAWS, "credentials"), 0o600, force); err != nil {
		return err
	}
	for _, rel := range []string{
		filepath.Join("sso", "cache"),
		filepath.Join("cli", "cache"),
	} {
		if err := copyAWSDir(filepath.Join(srcAWS, rel), filepath.Join(targetAWS, rel), force); err != nil {
			return err
		}
	}
	return nil
}

func materializeAWSHome(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("lstat AWS state target %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove external AWS state symlink %s: %w", path, err)
		}
		return nil
	}
	if !info.IsDir() {
		return fmt.Errorf("AWS state target %s exists and is not a directory", path)
	}
	return nil
}

func ensureAWSDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func copyAWSFile(src, dst string, mode os.FileMode, force bool) error {
	src = strings.TrimSpace(src)
	dst = strings.TrimSpace(dst)
	if src == "" || dst == "" {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read AWS state %s: %w", src, err)
	}
	if !force {
		if existing, err := os.ReadFile(dst); err == nil && string(existing) == string(data) {
			return nil
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read AWS state target %s: %w", dst, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		return fmt.Errorf("write AWS state %s: %w", dst, err)
	}
	return nil
}

func copyAWSDir(src, dst string, force bool) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read AWS state dir %s: %w", src, err)
	}
	if err := ensureAWSDir(dst); err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyAWSDir(srcPath, dstPath, force); err != nil {
				return err
			}
			continue
		}
		if entry.Type()&os.ModeType != 0 {
			continue
		}
		if err := copyAWSFile(srcPath, dstPath, 0o600, force); err != nil {
			return err
		}
	}
	return nil
}

func BuildBubblewrap(p nativeplan.Plan, command []string) (Command, error) {
	if strings.TrimSpace(p.DevkitSandboxRoot) == "" {
		return Command{}, fmt.Errorf("devkit sandbox root is empty")
	}
	args := []string{
		"--die-with-parent",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
	}
	if strings.TrimSpace(p.Proxy.UnixSocket) != "" {
		args = append(args, "--unshare-net")
	} else {
		args = append(args, "--share-net")
	}
	dirSet := map[string]bool{"/tmp": true}
	dirArgs := []string{}
	bindArgs := []string{}
	symlinkArgs := []string{}
	var addDir func(string)
	addDir = func(path string) {
		path = filepath.Clean(path)
		if path == "." || path == "/" || dirSet[path] {
			return
		}
		parent := filepath.Dir(path)
		if parent != "/" && !dirSet[parent] {
			addDir(parent)
		}
		dirArgs = append(dirArgs, "--dir", path)
		dirSet[path] = true
	}
	addBind := func(mode, source, target string, required bool) error {
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if source == "" || target == "" {
			if required {
				return fmt.Errorf("required bind has empty source or target")
			}
			return nil
		}
		if _, err := os.Stat(source); err != nil {
			if required {
				addDir(filepath.Dir(target))
				if mode == "ro" {
					bindArgs = append(bindArgs, "--ro-bind", source, target)
				} else {
					bindArgs = append(bindArgs, "--bind", source, target)
				}
			}
			return nil
		}
		addDir(filepath.Dir(target))
		if mode == "ro" {
			bindArgs = append(bindArgs, "--ro-bind", source, target)
		} else {
			bindArgs = append(bindArgs, "--bind", source, target)
		}
		return nil
	}
	addSymlink := func(target, linkPath string) {
		addDir(filepath.Dir(linkPath))
		symlinkArgs = append(symlinkArgs, "--symlink", target, linkPath)
	}

	if err := addBind("ro", "/nix/store", "/nix/store", true); err != nil {
		return Command{}, err
	}
	if err := addBind("ro", "/nix/var/nix", "/nix/var/nix", true); err != nil {
		return Command{}, err
	}
	_ = addBind("ro", "/run/current-system", "/run/current-system", false)
	_ = addBind("ro", "/etc/nix", "/etc/nix", false)
	_ = addBind("ro", "/etc/static", "/etc/static", false)
	_ = addBind("ro", "/etc/ssl", "/etc/ssl", false)
	_ = addBind("ro", "/etc/pki", "/etc/pki", false)
	_ = addBind("ro", "/etc/passwd", "/etc/passwd", false)
	_ = addBind("ro", "/etc/group", "/etc/group", false)

	for _, bind := range p.Binds {
		if bind.Target == "/nix/store" || bind.Target == "/etc/resolv.conf" {
			continue
		}
		if err := addBind(bind.Mode, bind.Source, bind.Target, bind.Required); err != nil {
			return Command{}, err
		}
	}
	if strings.TrimSpace(p.DNS.ResolvConf) != "" {
		if err := addBind("ro", p.DNS.ResolvConf, "/etc/resolv.conf", false); err != nil {
			return Command{}, err
		}
	} else {
		_ = addBind("ro", "/etc/resolv.conf", "/etc/resolv.conf", false)
	}
	addSymlink("/run/current-system/sw/bin/env", "/usr/bin/env")
	addSymlink("/run/current-system/sw/bin/bash", "/usr/bin/bash")
	addSymlink("/run/current-system/sw/bin/bash", "/bin/bash")
	addSymlink("/run/current-system/sw/bin/sh", "/bin/sh")

	args = append(args, dirArgs...)
	args = append(args, bindArgs...)
	args = append(args, symlinkArgs...)

	keys := make([]string, 0, len(p.Env))
	for key := range p.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--setenv", key, p.Env[key])
	}

	args = append(args, "--chdir", p.DevkitSandboxRoot)
	runtimeLauncher := strings.TrimSpace(p.RuntimeLauncher)
	if runtimeLauncher == "" {
		return Command{}, fmt.Errorf("immutable native runtime launcher is required")
	}
	if !filepath.IsAbs(runtimeLauncher) {
		return Command{}, fmt.Errorf("immutable native runtime launcher must be an absolute path: %s", runtimeLauncher)
	}
	if !isExecutable(runtimeLauncher) {
		return Command{}, fmt.Errorf("immutable native runtime launcher is not executable: %s", runtimeLauncher)
	}
	bubblewrapBinary := strings.TrimSpace(p.BubblewrapBinary)
	if bubblewrapBinary == "" {
		return Command{}, fmt.Errorf("immutable native bubblewrap binary is required")
	}
	if !filepath.IsAbs(bubblewrapBinary) {
		return Command{}, fmt.Errorf("immutable native bubblewrap binary must be an absolute path: %s", bubblewrapBinary)
	}
	if !isExecutable(bubblewrapBinary) {
		return Command{}, fmt.Errorf("immutable native bubblewrap binary is not executable: %s", bubblewrapBinary)
	}
	runtimeArgs := []string{runtimeLauncher}
	runtimeArgs = append(runtimeArgs, shellCommand(p.DevkitSandboxRoot, p.Agent.ID.Project, p.Agent.SandboxWorktree, command, p.Proxy, p.Env, false)...)
	if strings.TrimSpace(p.Proxy.UnixSocket) != "" {
		args = append(args, "/run/current-system/sw/bin/bash", "-lc", outerProxyRuntimeCommand(p.DevkitSandboxRoot, p.Agent.ID.Project, runtimeArgs, p.Proxy))
	} else {
		args = append(args, runtimeArgs...)
	}
	return Command{Path: bubblewrapBinary, Args: args, Dir: p.DevkitHostRoot}, nil
}

func ShellString(cmd Command) string {
	parts := append([]string{cmd.Path}, cmd.Args...)
	for i, part := range parts {
		parts[i] = shellQuote(part)
	}
	return strings.Join(parts, " ")
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

var nativeResolvConfSource = "/etc/resolv.conf"

func ensureResolvConf(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat resolv.conf %s: %w", path, err)
	}
	data, err := os.ReadFile(nativeResolvConfSource)
	if err != nil {
		return fmt.Errorf("read %s: %w", nativeResolvConfSource, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write resolv.conf %s: %w", path, err)
	}
	return nil
}

func outerProxyRuntimeCommand(devkitRoot string, project string, runtimeArgs []string, proxy nativeplan.ProxyConfig) string {
	script := proxyBridgeScript(devkitRoot, project, proxy)
	script += " && " + shellJoin(runtimeArgs)
	return script
}

func shellCommand(devkitRoot string, project string, workdir string, command []string, proxy nativeplan.ProxyConfig, env map[string]string, bridgeProxy bool) []string {
	exports := make([]string, 0, len(env))
	for key, value := range env {
		exports = append(exports, "export "+key+"="+shellQuote(value))
	}
	sort.Strings(exports)
	script := strings.Join(exports, "; ")
	if script != "" {
		script += "; "
	}
	script += "cd " + shellQuote(workdir)
	if bridgeProxy {
		script += " && " + proxyBridgeScript(devkitRoot, project, proxy)
	}
	if len(command) == 0 {
		if bridgeProxy {
			script += " && ${SHELL:-bash}"
		} else {
			script += " && exec ${SHELL:-bash}"
		}
	} else {
		quoted := make([]string, 0, len(command))
		for _, arg := range command {
			quoted = append(quoted, shellQuote(arg))
		}
		if bridgeProxy {
			script += " && " + strings.Join(quoted, " ")
		} else {
			script += " && exec " + strings.Join(quoted, " ")
		}
	}
	return []string{"bash", "-c", script}
}

func proxyBridgeScript(devkitRoot string, project string, proxy nativeplan.ProxyConfig) string {
	proxyURL := strings.TrimSpace(proxy.HTTPProxy)
	if proxyURL == "" {
		proxyURL = "http://127.0.0.1:18888"
	}
	devctlPath := filepath.Join(devkitRoot, "kit", "bin", "devctl")
	script := "{ " + shellQuote(devctlPath) + " -p " + shellQuote(project) + " native proxy-bridge --listen 127.0.0.1:18888 --socket " + shellQuote(proxy.UnixSocket) + " & devkit_proxy_bridge_pid=$!; }"
	script += " && trap 'kill \"$devkit_proxy_bridge_pid\" >/dev/null 2>&1 || true; wait \"$devkit_proxy_bridge_pid\" >/dev/null 2>&1 || true' EXIT"
	script += " && sleep 0.1"
	script += " && { kill -0 \"$devkit_proxy_bridge_pid\" >/dev/null 2>&1 || { echo 'native proxy bridge failed to start' >&2; exit 1; }; }"
	script += " && export HTTP_PROXY=" + shellQuote(proxyURL)
	script += " HTTPS_PROXY=" + shellQuote(proxyURL)
	script += " http_proxy=" + shellQuote(proxyURL)
	script += " https_proxy=" + shellQuote(proxyURL)
	script += " NO_PROXY=" + shellQuote(proxy.NoProxy)
	script += " no_proxy=" + shellQuote(proxy.NoProxy)
	return script
}

func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

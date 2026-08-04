package launch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"syscall"
	"time"

	"devkit/cli/devctl/internal/gitauthority"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/sshauthority"
)

type Command struct {
	Path string
	Args []string
	Dir  string
}

var (
	terraformProviderCredentialHydrator = "/run/current-system/sw/bin/terraform-ouro-provider-credential-hydrate"
	terraformProviderCredentialRoot     = "/run/terraform-ouro-provider-credentials"
)

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
	if err := validateManagementControllerProfilePlan(p); err != nil {
		return err
	}
	if err := validateProductGovernanceEnvironmentPlan(p); err != nil {
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
	if err := SeedCodexConfig(p.Agent.HostHome); err != nil {
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
	if err := SeedTerraformProviderCredentials(p); err != nil {
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

// SeedTerraformProviderCredentials projects only the two provider credential
// files owned by the source-derived WSL hydrator into the sole Terraform
// acceptance consumer. The helper has no caller-supplied source, target, or
// secret identifier; Devkit never reads a credential from an ambient path.
func SeedTerraformProviderCredentials(p nativeplan.Plan) error {
	if p.Agent.ID.Project != "ouroboros-terraform" ||
		p.Agent.ID.Repo != "ouroboros-terraform" ||
		p.Agent.ID.Index != 1 {
		return nil
	}
	hydrator := filepath.Clean(strings.TrimSpace(terraformProviderCredentialHydrator))
	root := filepath.Clean(strings.TrimSpace(terraformProviderCredentialRoot))
	if !filepath.IsAbs(hydrator) || !filepath.IsAbs(root) {
		return fmt.Errorf("Terraform provider credential authority is not absolute")
	}
	cmd := exec.Command(hydrator)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if reason := terraformProviderCredentialHydrationFailureReason(stderr.String()); reason != "" {
			return fmt.Errorf("source-derived Terraform provider credential hydration failed: %s", reason)
		}
		return fmt.Errorf("source-derived Terraform provider credential hydration failed")
	}
	files := []struct {
		source string
		target string
	}{
		{
			source: filepath.Join(root, "google-adc.json"),
			target: filepath.Join(p.Agent.HostHome, ".config", "gcloud", "application_default_credentials.d", "ouroboros-ai-498921-codex-terraform.json"),
		},
		{
			source: filepath.Join(root, "dnsimple-provider.env"),
			target: filepath.Join(p.Agent.HostHome, ".config", "ouroboros", "dnsimple-provider.env"),
		},
	}
	for _, file := range files {
		if err := copyProtectedProviderCredential(file.source, file.target); err != nil {
			return err
		}
	}
	return nil
}

func terraformProviderCredentialHydrationFailureReason(stderr string) string {
	const prefix = "Terraform provider credential hydration failed: "
	allowed := map[string]struct{}{
		"protected credential source is not a regular non-symlink file": {},
		"protected credential source mode is not private":               {},
		"Google ADC source does not have an accepted credential schema": {},
		"approved DNSimple token source is unavailable":                 {},
		"approved DNSimple account source is unavailable":               {},
		"approved DNSimple credential source returned an empty value":   {},
		"approved DNSimple credential must be one line":                 {},
	}
	for _, line := range strings.Split(stderr, "\n") {
		reason := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		if _, ok := allowed[reason]; ok {
			return reason
		}
	}
	return ""
}

func copyProtectedProviderCredential(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("inspect protected provider credential source: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("protected provider credential source must be a regular non-symlink file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("protected provider credential source has unsafe mode %04o", info.Mode().Perm())
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read protected provider credential source: %w", err)
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create protected provider credential target directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("chmod protected provider credential target directory: %w", err)
	}
	if targetInfo, err := os.Lstat(target); err == nil {
		if targetInfo.Mode()&os.ModeSymlink != 0 || !targetInfo.Mode().IsRegular() {
			return fmt.Errorf("protected provider credential target must be a regular non-symlink file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect protected provider credential target: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".provider-credential-")
	if err != nil {
		return fmt.Errorf("create protected provider credential staging file: %w", err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod protected provider credential staging file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write protected provider credential staging file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync protected provider credential staging file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close protected provider credential staging file: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("install protected provider credential target: %w", err)
	}
	committed = true
	return nil
}

func requireGitWorktree(worktree string) error {
	cmd := exec.Command(gitauthority.Executable(), "-C", worktree, "rev-parse", "--show-toplevel")
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
	proxyCommand := ""
	if p.IsolationProfile == nativeplan.IsolationProfileWorkspaceEgress {
		proxyCommand, err = gitManagedProxyCommand(p)
		if err != nil {
			return err
		}
	}
	if err := writeGitSSHConfigWithProxyCommand(hostHome, sandboxHome, identityNames, proxyCommand); err != nil {
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
		proxyCommand, err = gitManagedProxyCommand(p)
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

func gitManagedProxyCommand(p nativeplan.Plan) (string, error) {
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
	out, err := exec.Command(gitauthority.Executable(), "-C", worktree, "rev-parse", "--git-dir").CombinedOutput()
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
		out, err = exec.Command(gitauthority.Executable(), args...).CombinedOutput()
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

const (
	managementRuntimeSkillsIdentitySchema = "wsl-nix-management-runtime-skills/v2"
	managementRuntimeSkillsReceiptSchema  = "devkit-management-runtime-skills/v1"
)

var managementRuntimeSkillsStoreRoot = "/nix/store"

type managementRuntimeSkillFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type managementRuntimeSkillsIdentity struct {
	SchemaVersion       string                       `json:"schemaVersion"`
	ManagementSourceRev string                       `json:"managementSourceRev"`
	PackagePath         string                       `json:"packagePath"`
	SkillsRoot          string                       `json:"skillsRoot"`
	SkillPath           string                       `json:"skillPath"`
	SkillSHA256         string                       `json:"skillSha256"`
	Files               []managementRuntimeSkillFile `json:"files"`
}

type managementRuntimeSkillsReceipt struct {
	SchemaVersion       string   `json:"schemaVersion"`
	ManagementSourceRev string   `json:"managementSourceRev"`
	PackagePath         string   `json:"packagePath"`
	SkillsRoot          string   `json:"skillsRoot"`
	IdentitySHA256      string   `json:"identitySha256"`
	Links               []string `json:"links"`
	LegacySkillPath     string   `json:"skillPath,omitempty"`
	LegacySkillSHA256   string   `json:"skillSha256,omitempty"`
}

func ensureWorkspaceSkillLinks(p nativeplan.Plan) error {
	if !isDevWorkspacePlan(p) {
		return nil
	}
	hostHome := strings.TrimSpace(p.Agent.HostHome)
	hostDevRoot := hostDevRootForPlan(p)
	if hostHome == "" || hostDevRoot == "" {
		return fmt.Errorf("Management runtime skill preparation requires host home and workspace root")
	}
	targetRoot := filepath.Join(hostHome, ".codex", "skills")
	if err := os.MkdirAll(targetRoot, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", targetRoot, err)
	}
	identity, identitySHA256, skillNames, err := loadManagementRuntimeSkillsIdentity()
	if err != nil {
		return err
	}
	receiptPath := filepath.Join(hostHome, ".codex", "management-runtime-skills.json")
	previous, err := readManagementRuntimeSkillsReceipt(receiptPath)
	if err != nil {
		return err
	}
	if previous != nil && previous.LegacySkillPath != "" {
		previous.Links, err = discoverLegacyManagementRuntimeSkillLinks(targetRoot, previous.SkillsRoot)
		if err != nil {
			return err
		}
	}
	legacyRoots := []string{
		filepath.Join(hostDevRoot, ".codex", "skills"),
		filepath.Join(hostDevRoot, "ouroboros-ide", ".codex", "skills"),
	}
	if err := reconcileManagementRuntimeSkillLinks(targetRoot, identity.SkillsRoot, skillNames, previous, legacyRoots); err != nil {
		return err
	}
	receipt := managementRuntimeSkillsReceipt{
		SchemaVersion:       managementRuntimeSkillsReceiptSchema,
		ManagementSourceRev: identity.ManagementSourceRev,
		PackagePath:         identity.PackagePath,
		SkillsRoot:          identity.SkillsRoot,
		IdentitySHA256:      identitySHA256,
		Links:               append([]string(nil), skillNames...),
	}
	if err := writeManagementRuntimeSkillsReceipt(receiptPath, receipt); err != nil {
		return err
	}

	seen := make(map[string]bool, len(skillNames))
	for _, name := range skillNames {
		seen[name] = true
	}
	productSkillsRoot := filepath.Join(hostDevRoot, "ouroboros-ide", ".codex", "skills")
	entries, err := os.ReadDir(productSkillsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read Product skills %s: %w", productSkillsRoot, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == ".system" || seen[entry.Name()] {
			continue
		}
		seen[entry.Name()] = true
		source := filepath.Join(productSkillsRoot, entry.Name())
		target := filepath.Join(targetRoot, entry.Name())
		if err := ensureSkillSymlink(target, source); err != nil {
			return err
		}
	}
	return nil
}

func loadManagementRuntimeSkillsIdentity() (managementRuntimeSkillsIdentity, string, []string, error) {
	var identity managementRuntimeSkillsIdentity
	skillsRoot := filepath.Clean(strings.TrimSpace(os.Getenv("DEVKIT_MANAGEMENT_SKILLS_ROOT")))
	sourceRev := strings.TrimSpace(os.Getenv("DEVKIT_MANAGEMENT_SOURCE_REV"))
	expectedSkillSHA256 := strings.ToLower(strings.TrimSpace(os.Getenv("DEVKIT_MANAGEMENT_SKILL_SHA256")))
	if skillsRoot == "." || sourceRev == "" || expectedSkillSHA256 == "" {
		return identity, "", nil, fmt.Errorf("Management fresh-consumer readiness requires DEVKIT_MANAGEMENT_SKILLS_ROOT, DEVKIT_MANAGEMENT_SOURCE_REV, and DEVKIT_MANAGEMENT_SKILL_SHA256")
	}
	if !validSHA256(expectedSkillSHA256) {
		return identity, "", nil, fmt.Errorf("DEVKIT_MANAGEMENT_SKILL_SHA256 must be a lowercase SHA-256 digest")
	}
	packagePath, err := validateManagementRuntimeSkillsRoot(skillsRoot)
	if err != nil {
		return identity, "", nil, err
	}
	identityPath := filepath.Join(filepath.Dir(skillsRoot), "identity.json")
	identityInfo, err := os.Lstat(identityPath)
	if err != nil {
		return identity, "", nil, fmt.Errorf("inspect Management runtime skill identity %s: %w", identityPath, err)
	}
	if identityInfo.Mode()&os.ModeSymlink != 0 || !identityInfo.Mode().IsRegular() {
		return identity, "", nil, fmt.Errorf("Management runtime skill identity %s must be a regular non-symlink file", identityPath)
	}
	identityBytes, err := os.ReadFile(identityPath)
	if err != nil {
		return identity, "", nil, fmt.Errorf("read Management runtime skill identity %s: %w", identityPath, err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(identityBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&identity); err != nil {
		return identity, "", nil, fmt.Errorf("decode Management runtime skill identity %s: %w", identityPath, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return identity, "", nil, fmt.Errorf("decode Management runtime skill identity %s: %w", identityPath, err)
	}
	if identity.SchemaVersion != managementRuntimeSkillsIdentitySchema {
		return identity, "", nil, fmt.Errorf("Management runtime skill identity schema = %q, want %q", identity.SchemaVersion, managementRuntimeSkillsIdentitySchema)
	}
	if identity.ManagementSourceRev != sourceRev {
		return identity, "", nil, fmt.Errorf("Management runtime skill revision = %q, want %q", identity.ManagementSourceRev, sourceRev)
	}
	if filepath.Clean(identity.PackagePath) != packagePath {
		return identity, "", nil, fmt.Errorf("Management runtime skill package path = %q, want %q", identity.PackagePath, packagePath)
	}
	if filepath.Clean(identity.SkillsRoot) != skillsRoot {
		return identity, "", nil, fmt.Errorf("Management runtime skills root = %q, want %q", identity.SkillsRoot, skillsRoot)
	}
	if strings.ToLower(identity.SkillSHA256) != expectedSkillSHA256 {
		return identity, "", nil, fmt.Errorf("Management runtime primary skill SHA-256 = %q, want %q", identity.SkillSHA256, expectedSkillSHA256)
	}
	if !validRelativeManifestPath(identity.SkillPath) {
		return identity, "", nil, fmt.Errorf("Management runtime primary skill path %q is not a canonical relative path", identity.SkillPath)
	}
	actualFiles, skillNames, err := hashManagementRuntimeSkills(skillsRoot)
	if err != nil {
		return identity, "", nil, err
	}
	if err := validateManagementRuntimeSkillsManifest(identity.Files, actualFiles); err != nil {
		return identity, "", nil, err
	}
	primarySHA256, ok := actualFiles[identity.SkillPath]
	if !ok {
		return identity, "", nil, fmt.Errorf("Management runtime primary skill %q is absent from the complete manifest", identity.SkillPath)
	}
	if primarySHA256 != expectedSkillSHA256 {
		return identity, "", nil, fmt.Errorf("Management runtime primary skill %q SHA-256 = %q, want %q", identity.SkillPath, primarySHA256, expectedSkillSHA256)
	}
	identityDigest := sha256.Sum256(identityBytes)
	return identity, hex.EncodeToString(identityDigest[:]), skillNames, nil
}

func validateManagementRuntimeSkillsRoot(skillsRoot string) (string, error) {
	if !filepath.IsAbs(skillsRoot) {
		return "", fmt.Errorf("Management runtime skills root must be absolute: %s", skillsRoot)
	}
	storeRoot := filepath.Clean(managementRuntimeSkillsStoreRoot)
	rel, err := filepath.Rel(storeRoot, skillsRoot)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("Management runtime skills root %s must be beneath %s", skillsRoot, storeRoot)
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 4 || parts[0] == "" || parts[1] != "share" || parts[2] != "management-runtime-skills" || parts[3] != "skills" {
		return "", fmt.Errorf("Management runtime skills root %s must match %s/<package>/share/management-runtime-skills/skills", skillsRoot, storeRoot)
	}
	info, err := os.Lstat(skillsRoot)
	if err != nil {
		return "", fmt.Errorf("inspect Management runtime skills root %s: %w", skillsRoot, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("Management runtime skills root %s must be a real non-symlink directory", skillsRoot)
	}
	resolved, err := filepath.EvalSymlinks(skillsRoot)
	if err != nil {
		return "", fmt.Errorf("resolve Management runtime skills root %s: %w", skillsRoot, err)
	}
	if filepath.Clean(resolved) != skillsRoot {
		return "", fmt.Errorf("Management runtime skills root %s resolves through a symlink to %s", skillsRoot, resolved)
	}
	return filepath.Join(storeRoot, parts[0]), nil
}

func hashManagementRuntimeSkills(skillsRoot string) (map[string]string, []string, error) {
	files := map[string]string{}
	skillSet := map[string]bool{}
	err := filepath.WalkDir(skillsRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(skillsRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("Management runtime skills contain symlink %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect Management runtime skill path %s: %w", path, err)
		}
		if info.IsDir() {
			if !strings.Contains(filepath.ToSlash(rel), "/") && entry.Name() != ".system" {
				skillSet[entry.Name()] = true
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Management runtime skill path %s must be a regular file or directory", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read Management runtime skill file %s: %w", path, err)
		}
		digest := sha256.Sum256(data)
		files[filepath.ToSlash(rel)] = hex.EncodeToString(digest[:])
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if len(files) == 0 || len(skillSet) == 0 {
		return nil, nil, fmt.Errorf("Management runtime skills root %s contains no skills", skillsRoot)
	}
	skillNames := make([]string, 0, len(skillSet))
	for name := range skillSet {
		hasManifestedFile := false
		prefix := name + "/"
		for path := range files {
			if strings.HasPrefix(path, prefix) {
				hasManifestedFile = true
				break
			}
		}
		if !hasManifestedFile {
			return nil, nil, fmt.Errorf("Management runtime skill %s contains no manifested files", name)
		}
		skillNames = append(skillNames, name)
	}
	sort.Strings(skillNames)
	return files, skillNames, nil
}

func validateManagementRuntimeSkillsManifest(declared []managementRuntimeSkillFile, actual map[string]string) error {
	if len(declared) != len(actual) {
		return fmt.Errorf("Management runtime skill manifest contains %d files, filesystem contains %d", len(declared), len(actual))
	}
	previous := ""
	for _, file := range declared {
		if !validRelativeManifestPath(file.Path) {
			return fmt.Errorf("Management runtime skill manifest path %q is not a canonical relative path", file.Path)
		}
		if previous != "" && file.Path <= previous {
			return fmt.Errorf("Management runtime skill manifest is not strictly lexicographically sorted at %q", file.Path)
		}
		previous = file.Path
		declaredSHA256 := strings.ToLower(file.SHA256)
		if !validSHA256(declaredSHA256) {
			return fmt.Errorf("Management runtime skill manifest SHA-256 for %q is invalid", file.Path)
		}
		actualSHA256, ok := actual[file.Path]
		if !ok {
			return fmt.Errorf("Management runtime skill manifest declares absent file %q", file.Path)
		}
		if declaredSHA256 != actualSHA256 {
			return fmt.Errorf("Management runtime skill manifest SHA-256 for %q = %q, want %q", file.Path, declaredSHA256, actualSHA256)
		}
	}
	return nil
}

func validRelativeManifestPath(path string) bool {
	if path == "" || filepath.IsAbs(path) || filepath.ToSlash(path) != path {
		return false
	}
	clean := filepath.Clean(path)
	return clean != "." && clean == path && clean != ".." && !strings.HasPrefix(clean, "../")
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func readManagementRuntimeSkillsReceipt(path string) (*managementRuntimeSkillsReceipt, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect Management runtime skill receipt %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("Management runtime skill receipt %s must be a regular non-symlink file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Management runtime skill receipt %s: %w", path, err)
	}
	var receipt managementRuntimeSkillsReceipt
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return nil, fmt.Errorf("decode Management runtime skill receipt %s: %w", path, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode Management runtime skill receipt %s: %w", path, err)
	}
	if receipt.SchemaVersion != managementRuntimeSkillsReceiptSchema {
		return nil, fmt.Errorf("Management runtime skill receipt schema = %q, want %q", receipt.SchemaVersion, managementRuntimeSkillsReceiptSchema)
	}
	if filepath.Clean(receipt.SkillsRoot) != receipt.SkillsRoot || !filepath.IsAbs(receipt.SkillsRoot) {
		return nil, fmt.Errorf("Management runtime skill receipt has invalid skills root %q", receipt.SkillsRoot)
	}
	if (receipt.LegacySkillPath == "") != (receipt.LegacySkillSHA256 == "") {
		return nil, fmt.Errorf("Management runtime skill receipt has incomplete legacy skill identity")
	}
	if receipt.LegacySkillPath != "" {
		if !validRelativeManifestPath(receipt.LegacySkillPath) || !validSHA256(receipt.LegacySkillSHA256) {
			return nil, fmt.Errorf("Management runtime skill receipt has invalid legacy skill identity")
		}
		if receipt.PackagePath != "" || receipt.IdentitySHA256 != "" || len(receipt.Links) != 0 {
			return nil, fmt.Errorf("Management runtime skill receipt mixes legacy and current fields")
		}
	}
	seen := map[string]bool{}
	for _, name := range receipt.Links {
		if !validSkillName(name) || seen[name] {
			return nil, fmt.Errorf("Management runtime skill receipt has invalid or duplicate link %q", name)
		}
		seen[name] = true
	}
	return &receipt, nil
}

func discoverLegacyManagementRuntimeSkillLinks(targetRoot, skillsRoot string) ([]string, error) {
	entries, err := os.ReadDir(targetRoot)
	if err != nil {
		return nil, fmt.Errorf("read Management runtime skill links %s: %w", targetRoot, err)
	}
	links := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !validSkillName(name) {
			continue
		}
		target := filepath.Join(targetRoot, name)
		info, err := os.Lstat(target)
		if err != nil {
			return nil, fmt.Errorf("inspect Management runtime skill target %s: %w", target, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		existing, err := os.Readlink(target)
		if err != nil {
			return nil, fmt.Errorf("read Management runtime skill link %s: %w", target, err)
		}
		if filepath.Clean(existing) == filepath.Join(skillsRoot, name) {
			links = append(links, name)
		}
	}
	sort.Strings(links)
	return links, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return fmt.Errorf("trailing JSON content: %w", err)
	}
	return nil
}

func reconcileManagementRuntimeSkillLinks(targetRoot, currentRoot string, currentNames []string, previous *managementRuntimeSkillsReceipt, legacyRoots []string) error {
	current := make(map[string]bool, len(currentNames))
	for _, name := range currentNames {
		if !validSkillName(name) {
			return fmt.Errorf("Management runtime skill directory has invalid top-level name %q", name)
		}
		current[name] = true
	}
	if previous != nil {
		for _, name := range previous.Links {
			if current[name] {
				continue
			}
			target := filepath.Join(targetRoot, name)
			if err := requireOwnedSkillSymlink(target, []string{filepath.Join(previous.SkillsRoot, name)}, true); err != nil {
				return err
			}
		}
	}
	for _, name := range currentNames {
		target := filepath.Join(targetRoot, name)
		source := filepath.Join(currentRoot, name)
		ownedSources := make([]string, 0, len(legacyRoots)+1)
		if previous != nil {
			ownedSources = append(ownedSources, filepath.Join(previous.SkillsRoot, name))
		}
		for _, root := range legacyRoots {
			ownedSources = append(ownedSources, filepath.Join(root, name))
		}
		if err := requireOwnedSkillSymlink(target, ownedSources, false); err != nil {
			return err
		}
		if err := ensureSkillSymlink(target, source); err != nil {
			return err
		}
	}
	return nil
}

func requireOwnedSkillSymlink(target string, ownedSources []string, remove bool) error {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect Management runtime skill target %s: %w", target, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("Management runtime skill target %s is a foreign non-symlink path", target)
	}
	existing, err := os.Readlink(target)
	if err != nil {
		return fmt.Errorf("read Management runtime skill link %s: %w", target, err)
	}
	existing = filepath.Clean(existing)
	for _, owned := range ownedSources {
		if existing == filepath.Clean(owned) {
			if remove {
				if err := os.Remove(target); err != nil {
					return fmt.Errorf("remove stale Management runtime skill link %s: %w", target, err)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("Management runtime skill target %s is a foreign symlink to %s", target, existing)
}

func validSkillName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, `/\`)
}

func writeManagementRuntimeSkillsReceipt(path string, receipt managementRuntimeSkillsReceipt) error {
	sort.Strings(receipt.Links)
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Management runtime skill receipt: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	staged, err := os.CreateTemp(dir, ".management-runtime-skills.json.new-")
	if err != nil {
		return fmt.Errorf("create Management runtime skill receipt: %w", err)
	}
	stagedPath := staged.Name()
	committed := false
	defer func() {
		_ = staged.Close()
		if !committed {
			_ = os.Remove(stagedPath)
		}
	}()
	if err := staged.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod Management runtime skill receipt %s: %w", stagedPath, err)
	}
	if _, err := staged.Write(data); err != nil {
		return fmt.Errorf("write Management runtime skill receipt %s: %w", stagedPath, err)
	}
	if err := staged.Sync(); err != nil {
		return fmt.Errorf("sync Management runtime skill receipt %s: %w", stagedPath, err)
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("close Management runtime skill receipt %s: %w", stagedPath, err)
	}
	if err := os.Rename(stagedPath, path); err != nil {
		return fmt.Errorf("install Management runtime skill receipt %s: %w", path, err)
	}
	committed = true
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
		if err := os.Chmod(target, mode); err != nil {
			return fmt.Errorf("chmod SSH seed %s: %w", target, err)
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

// SeedCodexConfig materializes the Nix-authored Codex configuration selected
// by the controller into a consumer home. The source is runtime authority; a
// freshly reconstructed home must not depend on a prior activation having
// happened to populate the same path.
func SeedCodexConfig(hostHome string) error {
	hostHome = strings.TrimSpace(hostHome)
	if hostHome == "" {
		return nil
	}
	src := strings.TrimSpace(os.Getenv("DEVKIT_CODEX_CONFIG_SOURCE"))
	if src == "" {
		return nil
	}
	if !filepath.IsAbs(src) {
		return fmt.Errorf("Codex config source must be absolute: %s", src)
	}
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("inspect Codex config source %s: %w", src, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("Codex config source %s must be a regular non-symlink file", src)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read Codex config source %s: %w", src, err)
	}
	if len(data) == 0 {
		return fmt.Errorf("Codex config source %s is empty", src)
	}
	targetDir := filepath.Join(hostHome, ".codex")
	target := filepath.Join(targetDir, "config.toml")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", targetDir, err)
	}
	if current, err := os.Lstat(target); err == nil {
		if current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() {
			return fmt.Errorf("Codex config target %s must be a regular non-symlink file", target)
		}
		existing, readErr := os.ReadFile(target)
		if readErr != nil {
			return fmt.Errorf("read Codex config target %s: %w", target, readErr)
		}
		if string(existing) == string(data) {
			if err := os.Chmod(target, 0o600); err != nil {
				return fmt.Errorf("chmod Codex config target %s: %w", target, err)
			}
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect Codex config target %s: %w", target, err)
	}
	tmp, err := os.CreateTemp(targetDir, ".config.toml.new-")
	if err != nil {
		return fmt.Errorf("create Codex config staging file: %w", err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod Codex config staging file %s: %w", tmpPath, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write Codex config staging file %s: %w", tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync Codex config staging file %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close Codex config staging file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("install Codex config target %s: %w", target, err)
	}
	committed = true
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
	return buildBubblewrap(p, command, true)
}

// BuildManagedAppServerBubblewrap is reserved for the typed, systemd-owned
// GUI lifecycle. Ordinary native exec must retain parent-death coupling.
func BuildManagedAppServerBubblewrap(p nativeplan.Plan, command []string) (Command, error) {
	if p.IsolationProfile != nativeplan.IsolationProfileWorkspaceEgress {
		return Command{}, fmt.Errorf("managed app-server launcher requires workspace-egress isolation")
	}
	return buildBubblewrap(p, command, false)
}

func buildBubblewrap(p nativeplan.Plan, command []string, dieWithParent bool) (Command, error) {
	if err := validateManagementControllerProfilePlan(p); err != nil {
		return Command{}, err
	}
	if strings.TrimSpace(p.DevkitSandboxRoot) == "" {
		return Command{}, fmt.Errorf("devkit sandbox root is empty")
	}
	args := []string{"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
	}
	if dieWithParent {
		args = append([]string{"--die-with-parent"}, args...)
	}
	if p.IsolationProfile == nativeplan.IsolationProfileWorkspaceEgress {
		// Bubblewrap starts from the host mount namespace. Mask the WSL
		// automount root before adding the profile's narrow, source-derived
		// binds so no Windows drive remains visible inside workspace-egress.
		args = append(args, "--tmpfs", "/mnt")
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
		if required && isWorkspaceControllerCapabilityTarget(target) {
			if err := validateWorkspaceControllerCapabilityBinding(source, target, mode); err != nil {
				return err
			}
		}
		if required && filepath.Clean(target) == nativeplan.WorkspaceProductGovernanceEnvTarget {
			if err := validateProductGovernanceEnvironmentBindingContract(source, target, mode); err != nil {
				return err
			}
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

func isWorkspaceControllerCapabilityTarget(target string) bool {
	clean := filepath.Clean(strings.TrimSpace(target))
	if clean == filepath.Clean(nativeplan.WorkspaceControllerExecSocket) ||
		clean == filepath.Clean(nativeplan.WorkspaceControllerOperationSocket) ||
		clean == filepath.Clean(nativeplan.WorkspaceControllerOperationIdentity) ||
		clean == filepath.Clean(nativeplan.ManagementControllerProfileManifestPath) {
		return true
	}
	switch clean {
	case "/etc/fleet/source/fleet-inventory.json",
		"/etc/fleet/source/fleet-codex-gui-inventory.json":
		return true
	default:
		return false
	}
}

func validateWorkspaceControllerCapability(source, target string) error {
	target = filepath.Clean(target)
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("inspect package-owned controller capability %s: %w", target, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if isControllerSocketTarget(target) || target == nativeplan.WorkspaceControllerOperationIdentity {
			return fmt.Errorf("controller runtime capability %s must be non-symlink", target)
		}
		resolved, resolveErr := filepath.EvalSymlinks(source)
		expected := filepath.Join("/etc/static", strings.TrimPrefix(filepath.Clean(target), "/etc/"))
		expectedResolved, expectedErr := filepath.EvalSymlinks(expected)
		if resolveErr != nil || expectedErr != nil || filepath.Clean(resolved) != filepath.Clean(expectedResolved) {
			return fmt.Errorf("controller inventory %s must resolve only to its immutable /etc/static projection", target)
		}
		info, err = os.Stat(source)
		if err != nil {
			return fmt.Errorf("inspect resolved controller inventory %s: %w", target, err)
		}
	}
	if isControllerSocketTarget(target) {
		if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 {
			return fmt.Errorf("controller socket capability must be a mode 0600 Unix socket: %s", source)
		}
		parentInfo, parentErr := os.Lstat(filepath.Dir(source))
		if parentErr != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 ||
			parentInfo.Mode().Perm() != 0o700 {
			return fmt.Errorf("controller exec capability parent must be a protected mode 0700 directory: %s", filepath.Dir(source))
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || uint32(stat.Uid) != uint32(os.Getuid()) {
			return fmt.Errorf("controller exec capability owner does not match launching user: %s", source)
		}
		parentStat, ok := parentInfo.Sys().(*syscall.Stat_t)
		if !ok || uint32(parentStat.Uid) != uint32(os.Getuid()) {
			return fmt.Errorf("controller exec capability parent owner does not match launching user: %s", filepath.Dir(source))
		}
		return nil
	}
	if target == nativeplan.WorkspaceControllerOperationIdentity {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o400 {
			return fmt.Errorf("controller operation identity must be a mode 0400 regular file: %s", source)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || uint32(stat.Uid) != uint32(os.Getuid()) {
			return fmt.Errorf("controller operation identity owner does not match launching user: %s", source)
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("controller capability %s must resolve to a regular file", target)
	}
	return nil
}

func isControllerSocketTarget(target string) bool {
	target = filepath.Clean(strings.TrimSpace(target))
	return target == filepath.Clean(nativeplan.WorkspaceControllerExecSocket) ||
		target == filepath.Clean(nativeplan.WorkspaceControllerOperationSocket)
}

func validateWorkspaceControllerCapabilityBinding(source, target, mode string) error {
	if filepath.Clean(source) != filepath.Clean(target) {
		return fmt.Errorf("controller capability source override rejected for %s", target)
	}
	if mode != "ro" {
		return fmt.Errorf("controller capability %s must be projected read-only", target)
	}
	return validateWorkspaceControllerCapability(source, target)
}

func validateProductGovernanceEnvironmentPlan(p nativeplan.Plan) error {
	if p.IsolationProfile != nativeplan.IsolationProfileWorkspaceEgress ||
		!nativeplan.IsGovernedRuntimePlan(p.Agent.ID.Project, p.Agent.ID.Repo) {
		return nil
	}
	found := 0
	for _, bind := range p.Binds {
		if filepath.Clean(bind.Target) != nativeplan.WorkspaceProductGovernanceEnvTarget {
			continue
		}
		found++
		if !bind.Required {
			return fmt.Errorf("Product governance environment projection must be required")
		}
		if err := validateProductGovernanceEnvironmentBinding(bind.Source, bind.Target, bind.Mode); err != nil {
			return err
		}
	}
	if found != 1 {
		return fmt.Errorf("governed workspace plan requires exactly one Product governance environment projection, got %d", found)
	}
	return nil
}

func validateProductGovernanceEnvironmentBinding(source, target, mode string) error {
	if err := validateProductGovernanceEnvironmentBindingContract(source, target, mode); err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("inspect source-derived Product governance environment: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("source-derived Product governance environment must be a regular file or immutable store symlink")
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return fmt.Errorf("resolve source-derived Product governance environment: %w", err)
	}
	storeRoot := filepath.Clean(nativeplan.WorkspaceProductGovernanceStoreRoot)
	rel, err := filepath.Rel(storeRoot, filepath.Clean(resolved))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("source-derived Product governance environment must resolve beneath %s", storeRoot)
	}
	resolvedInfo, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("inspect resolved Product governance environment: %w", err)
	}
	if !resolvedInfo.Mode().IsRegular() || resolvedInfo.Mode().Perm()&0o222 != 0 {
		return fmt.Errorf("source-derived Product governance environment must resolve to an immutable regular file")
	}
	return nil
}

func validateProductGovernanceEnvironmentBindingContract(source, target, mode string) error {
	if filepath.Clean(target) != nativeplan.WorkspaceProductGovernanceEnvTarget {
		return fmt.Errorf("Product governance environment target override rejected: %s", target)
	}
	if filepath.Clean(source) != filepath.Clean(nativeplan.WorkspaceProductGovernanceEnvSource) {
		return fmt.Errorf("Product governance environment source override rejected for %s", target)
	}
	if mode != "ro" {
		return fmt.Errorf("Product governance environment must be projected read-only")
	}
	return nil
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

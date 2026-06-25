package launch

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"devkit/cli/devctl/internal/governanceentrypoint"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

type Command struct {
	Path string
	Args []string
	Dir  string
}

func Prepare(p nativeplan.Plan) error {
	if strings.TrimSpace(p.Agent.HostWorktree) == "" {
		return fmt.Errorf("host worktree is empty")
	}
	if st, err := os.Stat(p.Agent.HostWorktree); err != nil {
		return fmt.Errorf("host worktree %s: %w", p.Agent.HostWorktree, err)
	} else if !st.IsDir() {
		return fmt.Errorf("host worktree %s is not a directory", p.Agent.HostWorktree)
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
	if err := ensureOuroGovernanceEnv(p); err != nil {
		return err
	}
	if err := ensureCodexGovernanceConfig(p); err != nil {
		return err
	}
	if err := installProjectCodexRules(p); err != nil {
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
	if err := ensureGitSSHConfig(p); err != nil {
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

const (
	gitSSHManagedBegin = "# BEGIN DEVKIT NATIVE GIT SSH"
	gitSSHManagedEnd   = "# END DEVKIT NATIVE GIT SSH"
)

const (
	codexGovernanceManagedBegin = "# BEGIN DEVKIT NATIVE GOVERNANCE MCP"
	codexGovernanceManagedEnd   = "# END DEVKIT NATIVE GOVERNANCE MCP"
)

const (
	codexNixManagedConfigMarker    = "# source = nixos-wsl codex config"
	codexOpenAIProfileTable        = "profiles.openai"
	codexDevkitConfigSourceRelPath = ".devkit/nix-codex-config.toml"
)

var codexSystemConfigPath = "/etc/codex/config.toml"

func ensureGitSSHConfig(p nativeplan.Plan) error {
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
	sshCommand := "ssh -F " + filepath.Join(sandboxHome, ".ssh", "config")
	if err := WriteGitSSHConfig(hostHome, sandboxHome, identityNames); err != nil {
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
	cfg := buildGitSSHConfig(configHome, identityNames)
	if err := writeManagedBlock(cfgPath, cfg, 0o600); err != nil {
		return err
	}
	return nil
}

func buildGitSSHConfig(configHome string, identityNames []string) string {
	var b strings.Builder
	b.WriteString(gitSSHManagedBegin + "\n")
	b.WriteString("Host github.com\n")
	b.WriteString("  HostName github.com\n")
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
	b.WriteString("  StrictHostKeyChecking accept-new\n")
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
	out, err := exec.Command("git", "-C", worktree, "rev-parse", "--is-inside-work-tree").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		return nil
	}
	if out, err := runGitConfigWithLockRetry("-C", worktree, "config", "extensions.worktreeConfig", "true"); err != nil {
		return fmt.Errorf("git config extensions.worktreeConfig in %s: %w: %s", worktree, err, strings.TrimSpace(string(out)))
	}
	if out, err := runGitConfigWithLockRetry("-C", worktree, "config", "--worktree", "core.sshCommand", sshCommand); err != nil {
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

func installProjectCodexRules(p nativeplan.Plan) error {
	if !isOuroGovernedPlan(p) {
		return nil
	}
	if strings.TrimSpace(p.Agent.HostHome) == "" || strings.TrimSpace(p.DevkitHostRoot) == "" {
		return nil
	}
	source := filepath.Join(p.DevkitHostRoot, "overlays", "dev-all", "codex-governed-search-policy.rules")
	data, err := os.ReadFile(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read governed search policy %s: %w", source, err)
	}
	target := filepath.Join(p.Agent.HostHome, ".codex", "rules", "governed-search-policy.rules")
	if existing, err := os.ReadFile(target); err == nil && string(existing) == string(data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return fmt.Errorf("write governed search policy %s: %w", target, err)
	}
	return nil
}

func ensureCodexShellHook(p nativeplan.Plan) error {
	if isOuroGovernedPlan(p) {
		return writeOuroCodexShellHook(p.Agent.HostHome)
	}
	return repairRetiredCodexShellHook(p.Agent.HostHome)
}

func ensureOuroGovernanceEnv(p nativeplan.Plan) error {
	if !isOuroGovernedPlan(p) {
		return nil
	}
	hostDevRoot := hostDevRootForPlan(p)
	if hostDevRoot == "" {
		return nil
	}
	envPath := filepath.Join(hostDevRoot, ".devkit", "ouro8-governance-env.sh")
	repoConfigPath := filepath.Join(hostDevRoot, ".devkit", "ouro8-governance-repo-env.json")
	sandboxRepoConfigPath := "/workspaces/dev/.devkit/ouro8-governance-repo-env.json"
	repoConfig, err := buildOuroGovernanceRepoConfig(hostDevRoot)
	if err != nil {
		return err
	}
	repoConfigSha256 := fmt.Sprintf("%x", sha256.Sum256(repoConfig))
	runtimeIdentity, err := resolveOuroGovernanceRuntimeIdentity(hostDevRoot, ouroGovernanceRuntimeFlake())
	if err != nil {
		return err
	}
	content := buildOuroGovernanceEnv(hostDevRoot, sandboxRepoConfigPath, repoConfigSha256, runtimeIdentity)
	if data, err := os.ReadFile(envPath); err == nil && string(data) == content {
		// Keep checking the paired repo config below; it may have been generated
		// by an older devkit and carry a stale workspace catalog.
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read governance env %s: %w", envPath, err)
	} else {
		if err := os.MkdirAll(filepath.Dir(envPath), 0o700); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(envPath), err)
		}
		if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
			return fmt.Errorf("write governance env %s: %w", envPath, err)
		}
	}
	if data, err := os.ReadFile(repoConfigPath); err == nil && string(data) == string(repoConfig) {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read governance repo config %s: %w", repoConfigPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(repoConfigPath), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(repoConfigPath), err)
	}
	if err := os.WriteFile(repoConfigPath, repoConfig, 0o600); err != nil {
		return fmt.Errorf("write governance repo config %s: %w", repoConfigPath, err)
	}
	return nil
}

func isOuroGovernedPlan(p nativeplan.Plan) bool {
	project := strings.TrimSpace(p.Agent.ID.Project)
	repo := strings.TrimSpace(p.Agent.ID.Repo)
	switch repo {
	case "ouroboros-ide", "ouroboros-terraform":
		return true
	}
	switch project {
	case "dev-workspace", "ouro-integration", "ouroboros-terraform":
		return true
	default:
		return false
	}
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

type ouroGovernanceRuntimeIdentity struct {
	LatestJarPath                 string
	ControlPlaneJar               string
	ExpectedJarPath               string
	ExpectedJarSHA256             string
	SubagentExpectedJarSHA256     string
	SubmitToCiJarPath             string
	SubmitToCiHashPath            string
	SubmitToCiExpectedJarPath     string
	SubmitToCiExpectedSHA256      string
	SubmitSbt2ClientMode          string
	SubmitSbt2JavaXmx             string
	LintInvarianceSbt2Mode        string
	ArtifactColumnRepositoryPath  string
	ArtifactColumnRepositoryAlias string
	ArtifactColumnMetadataEnv     string
	ArtifactColumnVersion         string
	ArtifactColumnSourceRev       string
	ArtifactColumnSourceShortRev  string
	ArtifactColumnIvyPath         string
	ArtifactColumnJarSHA256       string
	ArtifactColumnPinnedArtifact  string
	ArtifactColumnFlakeArtifact   string
	JavaHome                      string
}

const ouroGovernanceRuntimeIdentityMarker = "__DEVKIT_GOVERNANCE_RUNTIME_IDENTITY__"

func (identity ouroGovernanceRuntimeIdentity) Complete() bool {
	return strings.TrimSpace(identity.LatestJarPath) != "" &&
		strings.TrimSpace(identity.ControlPlaneJar) != "" &&
		strings.TrimSpace(identity.ExpectedJarPath) != "" &&
		strings.TrimSpace(identity.ExpectedJarSHA256) != "" &&
		strings.TrimSpace(identity.SubagentExpectedJarSHA256) != "" &&
		strings.TrimSpace(identity.SubmitToCiJarPath) != "" &&
		strings.TrimSpace(identity.SubmitToCiHashPath) != "" &&
		strings.TrimSpace(identity.SubmitToCiExpectedJarPath) != "" &&
		strings.TrimSpace(identity.SubmitToCiExpectedSHA256) != "" &&
		strings.TrimSpace(identity.SubmitSbt2ClientMode) != "" &&
		strings.TrimSpace(identity.SubmitSbt2JavaXmx) != "" &&
		strings.TrimSpace(identity.LintInvarianceSbt2Mode) != "" &&
		strings.TrimSpace(identity.ArtifactColumnRepositoryPath) != "" &&
		strings.TrimSpace(identity.ArtifactColumnRepositoryAlias) != "" &&
		strings.TrimSpace(identity.ArtifactColumnMetadataEnv) != "" &&
		strings.TrimSpace(identity.ArtifactColumnVersion) != "" &&
		strings.TrimSpace(identity.ArtifactColumnSourceRev) != "" &&
		strings.TrimSpace(identity.ArtifactColumnSourceShortRev) != "" &&
		strings.TrimSpace(identity.ArtifactColumnIvyPath) != "" &&
		strings.TrimSpace(identity.ArtifactColumnJarSHA256) != "" &&
		strings.TrimSpace(identity.ArtifactColumnPinnedArtifact) != "" &&
		strings.TrimSpace(identity.ArtifactColumnFlakeArtifact) != "" &&
		strings.TrimSpace(identity.JavaHome) != ""
}

func validOuroGovernanceSbtClientMode(value string) bool {
	switch strings.TrimSpace(value) {
	case "force", "off":
		return true
	default:
		return false
	}
}

func validOuroGovernanceJavaXmx(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return false
	}
	unit := value[len(value)-1]
	if unit != 'm' && unit != 'M' && unit != 'g' && unit != 'G' {
		return false
	}
	for _, ch := range value[:len(value)-1] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func ouroGovernanceRuntimeFlake() string {
	return filepath.Join("/workspaces/dev", "devkit") + "#dev-all"
}

func resolveOuroGovernanceRuntimeIdentity(hostDevRoot string, runtimeFlake string) (ouroGovernanceRuntimeIdentity, error) {
	flakePath := filepath.Join(hostDevRoot, "devkit", "flake.nix")
	if !pathExists(flakePath) {
		return ouroGovernanceRuntimeIdentity{}, nil
	}
	nixBin, err := exec.LookPath("nix")
	if err != nil {
		if pathExists("/run/current-system/sw/bin/nix") {
			nixBin = "/run/current-system/sw/bin/nix"
		} else {
			return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env: nix not found")
		}
	}
	script := strings.Join([]string{
		`set -euo pipefail`,
		`runtime_env="$($1 --extra-experimental-features 'nix-command flakes' --no-warn-dirty --option eval-cache false print-dev-env "$DEVKIT_GOVERNANCE_RUNTIME_FLAKE")"`,
		`eval "$runtime_env"`,
		`printf '` + ouroGovernanceRuntimeIdentityMarker + `\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0' "${SUBAGENT_GOVERNANCE_LATEST_JAR_PATH:-}" "${SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR:-}" "${DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH:-}" "${DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256:-}" "${SUBAGENT_GOVERNANCE_EXPECTED_JAR_SHA256:-}" "${SUBMIT_TO_CI_JAR:-}" "${SUBMIT_TO_CI_HASH_PATH:-}" "${DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_PATH:-}" "${DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_SHA256:-}" "${SBT2_CLIENT_MODE:-}" "${SBT2_JAVA_XMX:-}" "${OURO_LINT_INVARIANCE_SCRIPTED_SBT2_CLIENT_MODE:-}" "${ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH:-}" "${ARTIFACT_COLUMN_PLUGIN_REPOSITORY:-}" "${ARTIFACT_COLUMN_PLUGIN_METADATA_ENV:-}" "${ARTIFACT_COLUMN_PLUGIN_VERSION:-}" "${ARTIFACT_COLUMN_PLUGIN_SOURCE_REV:-}" "${ARTIFACT_COLUMN_PLUGIN_SOURCE_SHORT_REV:-}" "${ARTIFACT_COLUMN_PLUGIN_IVY_PATH:-}" "${ARTIFACT_COLUMN_PLUGIN_JAR_SHA256:-}" "${ARTIFACT_COLUMN_PLUGIN_PINNED_ARTIFACT:-}" "${ARTIFACT_COLUMN_PLUGIN_FLAKE_ARTIFACT:-}" "${JAVA_HOME:-}"`,
	}, "; ")
	cmd := exec.Command("bash", "-lc", script, "resolve-governance-runtime", nixBin)
	cmd.Env = append(os.Environ(), "DEVKIT_GOVERNANCE_RUNTIME_FLAKE="+runtimeFlake)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: %w: %s", runtimeFlake, err, strings.TrimSpace(string(out)))
	}
	identity, err := parseOuroGovernanceRuntimeIdentityOutput(out, runtimeFlake)
	if err != nil {
		return ouroGovernanceRuntimeIdentity{}, err
	}
	if !identity.Complete() {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: incomplete pinned governance/submit-to-ci jar, artifact-column plugin repository, Java, or submit runtime authority identity", runtimeFlake)
	}
	if !validOuroGovernanceSbtClientMode(identity.SubmitSbt2ClientMode) {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: SBT2_CLIENT_MODE must be force or off, got %q", runtimeFlake, identity.SubmitSbt2ClientMode)
	}
	if !validOuroGovernanceJavaXmx(identity.SubmitSbt2JavaXmx) {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: SBT2_JAVA_XMX must match scripts/sbt2 heap syntax, got %q", runtimeFlake, identity.SubmitSbt2JavaXmx)
	}
	if !validOuroGovernanceSbtClientMode(identity.LintInvarianceSbt2Mode) {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: OURO_LINT_INVARIANCE_SCRIPTED_SBT2_CLIENT_MODE must be force or off, got %q", runtimeFlake, identity.LintInvarianceSbt2Mode)
	}
	for label, path := range map[string]string{
		"latest governance jar":             identity.LatestJarPath,
		"control-plane governance jar":      identity.ControlPlaneJar,
		"expected governance jar":           identity.ExpectedJarPath,
		"submit-to-ci jar":                  identity.SubmitToCiJarPath,
		"submit-to-ci jar hash":             identity.SubmitToCiHashPath,
		"artifact-column plugin repository": identity.ArtifactColumnRepositoryPath,
		"artifact-column plugin metadata":   identity.ArtifactColumnMetadataEnv,
		"JAVA_HOME":                         identity.JavaHome,
	} {
		if !pathExists(path) {
			return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: missing %s %s", runtimeFlake, label, path)
		}
	}
	if identity.LatestJarPath != identity.ControlPlaneJar || identity.LatestJarPath != identity.ExpectedJarPath {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: pinned jar path mismatch", runtimeFlake)
	}
	if identity.ExpectedJarSHA256 != identity.SubagentExpectedJarSHA256 {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: expected jar sha mismatch", runtimeFlake)
	}
	if !strings.HasPrefix(identity.LatestJarPath, "/nix/store/") {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: pinned jar is not in /nix/store: %s", runtimeFlake, identity.LatestJarPath)
	}
	if identity.SubmitToCiJarPath != identity.SubmitToCiExpectedJarPath {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: pinned submit-to-ci jar path mismatch", runtimeFlake)
	}
	if identity.SubmitToCiHashPath != identity.SubmitToCiJarPath+".sha256" {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: submit-to-ci hash path mismatch", runtimeFlake)
	}
	if !strings.HasPrefix(identity.SubmitToCiJarPath, "/nix/store/") || !strings.HasSuffix(identity.SubmitToCiJarPath, "/share/submit-to-ci/submit-to-ci.jar") {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: pinned submit-to-ci jar is not a Nix-store submit jar: %s", runtimeFlake, identity.SubmitToCiJarPath)
	}
	submitJarData, err := os.ReadFile(identity.SubmitToCiJarPath)
	if err != nil {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: read submit-to-ci jar %s: %w", runtimeFlake, identity.SubmitToCiJarPath, err)
	}
	submitJarSHA := fmt.Sprintf("%x", sha256.Sum256(submitJarData))
	if !strings.EqualFold(submitJarSHA, identity.SubmitToCiExpectedSHA256) {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: submit-to-ci jar sha mismatch", runtimeFlake)
	}
	submitHashData, err := os.ReadFile(identity.SubmitToCiHashPath)
	if err != nil {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: read submit-to-ci hash %s: %w", runtimeFlake, identity.SubmitToCiHashPath, err)
	}
	if strings.TrimSpace(string(submitHashData)) != identity.SubmitToCiExpectedSHA256 {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: submit-to-ci hash file mismatch", runtimeFlake)
	}
	if identity.ArtifactColumnRepositoryAlias != identity.ArtifactColumnRepositoryPath {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: artifact-column plugin repository path mismatch", runtimeFlake)
	}
	if !strings.HasPrefix(identity.ArtifactColumnRepositoryPath, "/nix/store/") {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: artifact-column plugin repository is not in /nix/store: %s", runtimeFlake, identity.ArtifactColumnRepositoryPath)
	}
	artifactMetadataPath := filepath.Join(identity.ArtifactColumnRepositoryPath, "share", "artifact-column-plugin", "metadata.env")
	if identity.ArtifactColumnMetadataEnv != artifactMetadataPath {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: artifact-column plugin metadata path drift: expected %s got %s", runtimeFlake, artifactMetadataPath, identity.ArtifactColumnMetadataEnv)
	}
	if strings.Contains(strings.ToUpper(identity.ArtifactColumnVersion), "SNAPSHOT") {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: artifact-column plugin version must be a pinned non-SNAPSHOT version, got %q", runtimeFlake, identity.ArtifactColumnVersion)
	}
	if !strings.HasPrefix(identity.ArtifactColumnIvyPath, "ivy2/local/com.crib.bills.ouroboros/artifact-column-plugin_sbt2_3/") {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: artifact-column plugin Ivy path is not canonical: %s", runtimeFlake, identity.ArtifactColumnIvyPath)
	}
	if identity.ArtifactColumnPinnedArtifact != "1" || identity.ArtifactColumnFlakeArtifact != "0" {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: artifact-column plugin must be pinned artifact=1 flake artifact=0", runtimeFlake)
	}
	artifactMetadataData, err := os.ReadFile(identity.ArtifactColumnMetadataEnv)
	if err != nil {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: read artifact-column plugin metadata %s: %w", runtimeFlake, identity.ArtifactColumnMetadataEnv, err)
	}
	for key, want := range map[string]string{
		"ARTIFACT_COLUMN_PLUGIN_VERSION":          identity.ArtifactColumnVersion,
		"ARTIFACT_COLUMN_PLUGIN_SOURCE_REV":       identity.ArtifactColumnSourceRev,
		"ARTIFACT_COLUMN_PLUGIN_SOURCE_SHORT_REV": identity.ArtifactColumnSourceShortRev,
		"ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH":  identity.ArtifactColumnRepositoryPath,
		"ARTIFACT_COLUMN_PLUGIN_IVY_PATH":         identity.ArtifactColumnIvyPath,
		"ARTIFACT_COLUMN_PLUGIN_JAR_SHA256":       identity.ArtifactColumnJarSHA256,
	} {
		if got := metadataEnvValue(string(artifactMetadataData), key); got != want {
			return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: artifact-column plugin metadata %s mismatch: expected %q got %q", runtimeFlake, key, want, got)
		}
	}
	artifactHashPath := filepath.Join(identity.ArtifactColumnRepositoryPath, "share", "artifact-column-plugin", "artifact-column-plugin.jar.sha256")
	artifactHashData, err := os.ReadFile(artifactHashPath)
	if err != nil {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: read artifact-column plugin hash %s: %w", runtimeFlake, artifactHashPath, err)
	}
	if strings.TrimSpace(string(artifactHashData)) != identity.ArtifactColumnJarSHA256 {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: artifact-column plugin hash file mismatch", runtimeFlake)
	}
	return identity, nil
}

func metadataEnvValue(text string, key string) string {
	prefix := key + "="
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func parseOuroGovernanceRuntimeIdentityOutput(out []byte, runtimeFlake string) (ouroGovernanceRuntimeIdentity, error) {
	marker := ouroGovernanceRuntimeIdentityMarker + "\x00"
	text := string(out)
	index := strings.LastIndex(text, marker)
	if index < 0 {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: missing identity marker", runtimeFlake)
	}
	payload := text[index+len(marker):]
	parts := strings.Split(strings.TrimSuffix(payload, "\x00"), "\x00")
	if len(parts) != 23 {
		return ouroGovernanceRuntimeIdentity{}, fmt.Errorf("resolve governance runtime env from %s: expected 23 fields, got %d", runtimeFlake, len(parts))
	}
	return ouroGovernanceRuntimeIdentity{
		LatestJarPath:                 parts[0],
		ControlPlaneJar:               parts[1],
		ExpectedJarPath:               parts[2],
		ExpectedJarSHA256:             parts[3],
		SubagentExpectedJarSHA256:     parts[4],
		SubmitToCiJarPath:             parts[5],
		SubmitToCiHashPath:            parts[6],
		SubmitToCiExpectedJarPath:     parts[7],
		SubmitToCiExpectedSHA256:      parts[8],
		SubmitSbt2ClientMode:          parts[9],
		SubmitSbt2JavaXmx:             parts[10],
		LintInvarianceSbt2Mode:        parts[11],
		ArtifactColumnRepositoryPath:  parts[12],
		ArtifactColumnRepositoryAlias: parts[13],
		ArtifactColumnMetadataEnv:     parts[14],
		ArtifactColumnVersion:         parts[15],
		ArtifactColumnSourceRev:       parts[16],
		ArtifactColumnSourceShortRev:  parts[17],
		ArtifactColumnIvyPath:         parts[18],
		ArtifactColumnJarSHA256:       parts[19],
		ArtifactColumnPinnedArtifact:  parts[20],
		ArtifactColumnFlakeArtifact:   parts[21],
		JavaHome:                      parts[22],
	}, nil
}

func buildOuroGovernanceEnv(hostDevRoot string, repoConfigPath string, repoConfigSha256 string, runtimeIdentity ouroGovernanceRuntimeIdentity) string {
	hostDevRoot = filepath.Clean(hostDevRoot)
	repoConfigPath = filepath.Clean(repoConfigPath)
	sandboxDevRoot := "/workspaces/dev"
	catalog := buildOuroGovernanceCatalogForRoot(hostDevRoot)
	stateDir := filepath.Join(sandboxDevRoot, "ouroboros-ide", "logs", "subagent-governance", "control-plane")
	schemaRoot := filepath.Join(sandboxDevRoot, "ouroboros-ide", "tools", "subagent-governance", "schemas")
	runtimeFlake := ouroGovernanceRuntimeFlake()
	lines := []string{
		"# Shared governance MCP/control-plane environment for native Ouroboros GUI agents.",
		"# Do not set SUBAGENT_GOVERNANCE_WORKSPACE_ID here; each agent wrapper derives it from PWD.",
		"export DEVKIT_GOVERNANCE_RUNTIME_FLAKE=" + shellQuote(runtimeFlake),
		"export DEVKIT_GOVERNANCE_REPO_CONFIG_PATH=" + shellQuote(repoConfigPath),
		"export SUBAGENT_GOVERNANCE_REPO_CONFIG_PATH=" + shellQuote(repoConfigPath),
		"export DEVKIT_GOVERNANCE_REPO_CONFIG_SHA256=" + shellQuote(repoConfigSha256),
		"export DEVKIT_GOVERNANCE_EXPECTED_MCP_ENTRYPOINT_SHA256=" + shellQuote(governanceentrypoint.SHA256()),
		"export DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256=" + shellQuote(governanceentrypoint.SHA256()),
		"export SUBAGENT_GOVERNANCE_PINNED_ARTIFACT=1",
		"export SUBAGENT_GOVERNANCE_FLAKE_ARTIFACT=0",
	}
	if runtimeIdentity.Complete() {
		lines = append(lines,
			"export SUBAGENT_GOVERNANCE_LATEST_JAR_PATH="+shellQuote(runtimeIdentity.LatestJarPath),
			"export SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR="+shellQuote(runtimeIdentity.ControlPlaneJar),
			"export DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH="+shellQuote(runtimeIdentity.ExpectedJarPath),
			"export DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256="+shellQuote(runtimeIdentity.ExpectedJarSHA256),
			"export SUBAGENT_GOVERNANCE_EXPECTED_JAR_SHA256="+shellQuote(runtimeIdentity.SubagentExpectedJarSHA256),
			"export SUBMIT_TO_CI_JAR="+shellQuote(runtimeIdentity.SubmitToCiJarPath),
			"export SUBMIT_TO_CI_HASH_PATH="+shellQuote(runtimeIdentity.SubmitToCiHashPath),
			"export SUBMIT_TO_CI_BUILD_POLICY='reuse'",
			"export SUBMIT_TO_CI_EXTERNAL_JAR=1",
			"export SUBMIT_TO_CI_FLAKE_ARTIFACT=0",
			"export SUBMIT_TO_CI_PINNED_ARTIFACT=0",
			"export DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_PATH="+shellQuote(runtimeIdentity.SubmitToCiExpectedJarPath),
			"export DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_SHA256="+shellQuote(runtimeIdentity.SubmitToCiExpectedSHA256),
			"export SBT2_CLIENT_MODE="+shellQuote(runtimeIdentity.SubmitSbt2ClientMode),
			"export SBT2_JAVA_XMX="+shellQuote(runtimeIdentity.SubmitSbt2JavaXmx),
			"export OURO_LINT_INVARIANCE_SCRIPTED_SBT2_CLIENT_MODE="+shellQuote(runtimeIdentity.LintInvarianceSbt2Mode),
			"export ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH="+shellQuote(runtimeIdentity.ArtifactColumnRepositoryPath),
			"export ARTIFACT_COLUMN_PLUGIN_REPOSITORY="+shellQuote(runtimeIdentity.ArtifactColumnRepositoryAlias),
			"export ARTIFACT_COLUMN_PLUGIN_METADATA_ENV="+shellQuote(runtimeIdentity.ArtifactColumnMetadataEnv),
			"export ARTIFACT_COLUMN_PLUGIN_VERSION="+shellQuote(runtimeIdentity.ArtifactColumnVersion),
			"export ARTIFACT_COLUMN_PLUGIN_SOURCE_REV="+shellQuote(runtimeIdentity.ArtifactColumnSourceRev),
			"export ARTIFACT_COLUMN_PLUGIN_SOURCE_SHORT_REV="+shellQuote(runtimeIdentity.ArtifactColumnSourceShortRev),
			"export ARTIFACT_COLUMN_PLUGIN_IVY_PATH="+shellQuote(runtimeIdentity.ArtifactColumnIvyPath),
			"export ARTIFACT_COLUMN_PLUGIN_JAR_SHA256="+shellQuote(runtimeIdentity.ArtifactColumnJarSHA256),
			"export ARTIFACT_COLUMN_PLUGIN_PINNED_ARTIFACT="+shellQuote(runtimeIdentity.ArtifactColumnPinnedArtifact),
			"export ARTIFACT_COLUMN_PLUGIN_FLAKE_ARTIFACT="+shellQuote(runtimeIdentity.ArtifactColumnFlakeArtifact),
			"export JAVA_HOME="+shellQuote(runtimeIdentity.JavaHome),
		)
	}
	lines = append(lines,
		"devkit_governance_load_runtime_env() {",
		"  local nix_bin",
		"  if command -v nix >/dev/null 2>&1; then",
		"    nix_bin=\"$(command -v nix)\"",
		"  elif [ -x /run/current-system/sw/bin/nix ]; then",
		"    nix_bin=/run/current-system/sw/bin/nix",
		"  else",
		"    echo \"[devkit-governance-env] unable to locate nix for ${DEVKIT_GOVERNANCE_RUNTIME_FLAKE}\" >&2",
		"    return 1",
		"  fi",
		"  local runtime_env",
		"  if ! runtime_env=\"$($nix_bin --extra-experimental-features 'nix-command flakes' --no-warn-dirty --option eval-cache false print-dev-env \"$DEVKIT_GOVERNANCE_RUNTIME_FLAKE\")\"; then",
		"    echo \"[devkit-governance-env] unable to load runtime env from ${DEVKIT_GOVERNANCE_RUNTIME_FLAKE}\" >&2",
		"    return 1",
		"  fi",
		"  eval \"$runtime_env\"",
		"}",
		"devkit_governance_have_expected_jar() {",
		"  [ -n \"${SUBAGENT_GOVERNANCE_LATEST_JAR_PATH:-}\" ] || return 1",
		"  [ -n \"${SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR:-}\" ] || return 1",
		"  [ -n \"${DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH:-}\" ] || return 1",
		"  [ -n \"${DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256:-}\" ] || return 1",
		"  [ -n \"${SUBAGENT_GOVERNANCE_EXPECTED_JAR_SHA256:-}\" ] || return 1",
		"  local jar_path=\"${SUBAGENT_GOVERNANCE_LATEST_JAR_PATH}\"",
		"  [ \"${SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR}\" = \"$jar_path\" ] || return 1",
		"  [ \"${DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH}\" = \"$jar_path\" ] || return 1",
		"  case \"$jar_path\" in",
		"    /nix/store/*/share/subagent-governance/subagent-governance.jar) ;;",
		"    *) return 1 ;;",
		"  esac",
		"  [ -f \"$jar_path\" ] || return 1",
		"  local jar_sha",
		"  if command -v sha256sum >/dev/null 2>&1; then",
		"    jar_sha=\"$(sha256sum \"$jar_path\" | awk '{print $1}')\"",
		"  elif command -v shasum >/dev/null 2>&1; then",
		"    jar_sha=\"$(shasum -a 256 \"$jar_path\" | awk '{print $1}')\"",
		"  else",
		"    return 1",
		"  fi",
		"  [ \"$jar_sha\" = \"${DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256}\" ] || return 1",
		"  [ \"$jar_sha\" = \"${SUBAGENT_GOVERNANCE_EXPECTED_JAR_SHA256}\" ] || return 1",
		"}",
		"devkit_governance_have_expected_submit_to_ci_jar() {",
		"  [ -n \"${SUBMIT_TO_CI_JAR:-}\" ] || return 1",
		"  [ -n \"${SUBMIT_TO_CI_HASH_PATH:-}\" ] || return 1",
		"  [ -n \"${DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_PATH:-}\" ] || return 1",
		"  [ -n \"${DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_SHA256:-}\" ] || return 1",
		"  [ \"${SUBMIT_TO_CI_BUILD_POLICY:-}\" = \"reuse\" ] || return 1",
		"  [ \"${SUBMIT_TO_CI_EXTERNAL_JAR:-}\" = \"1\" ] || return 1",
		"  [ \"${SUBMIT_TO_CI_FLAKE_ARTIFACT:-}\" = \"0\" ] || return 1",
		"  [ \"${SUBMIT_TO_CI_PINNED_ARTIFACT:-}\" = \"0\" ] || return 1",
		"  local jar_path=\"${SUBMIT_TO_CI_JAR}\"",
		"  [ \"${DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_PATH}\" = \"$jar_path\" ] || return 1",
		"  [ \"${SUBMIT_TO_CI_HASH_PATH}\" = \"${jar_path}.sha256\" ] || return 1",
		"  case \"$jar_path\" in",
		"    /nix/store/*/share/submit-to-ci/submit-to-ci.jar) ;;",
		"    *) return 1 ;;",
		"  esac",
		"  [ -f \"$jar_path\" ] || return 1",
		"  [ -f \"$SUBMIT_TO_CI_HASH_PATH\" ] || return 1",
		"  local jar_sha",
		"  if command -v sha256sum >/dev/null 2>&1; then",
		"    jar_sha=\"$(sha256sum \"$jar_path\" | awk '{print $1}')\"",
		"  elif command -v shasum >/dev/null 2>&1; then",
		"    jar_sha=\"$(shasum -a 256 \"$jar_path\" | awk '{print $1}')\"",
		"  else",
		"    return 1",
		"  fi",
		"  [ \"$jar_sha\" = \"${DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_SHA256}\" ] || return 1",
		"  [ \"$(tr -d '[:space:]' < \"$SUBMIT_TO_CI_HASH_PATH\")\" = \"${DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_SHA256}\" ] || return 1",
		"}",
		"devkit_governance_have_expected_artifact_column_plugin_repository() {",
		"  [ -n \"${ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH:-}\" ] || return 1",
		"  [ -n \"${ARTIFACT_COLUMN_PLUGIN_REPOSITORY:-}\" ] || return 1",
		"  [ -n \"${ARTIFACT_COLUMN_PLUGIN_METADATA_ENV:-}\" ] || return 1",
		"  [ -n \"${ARTIFACT_COLUMN_PLUGIN_VERSION:-}\" ] || return 1",
		"  [ -n \"${ARTIFACT_COLUMN_PLUGIN_SOURCE_REV:-}\" ] || return 1",
		"  [ -n \"${ARTIFACT_COLUMN_PLUGIN_SOURCE_SHORT_REV:-}\" ] || return 1",
		"  [ -n \"${ARTIFACT_COLUMN_PLUGIN_IVY_PATH:-}\" ] || return 1",
		"  [ -n \"${ARTIFACT_COLUMN_PLUGIN_JAR_SHA256:-}\" ] || return 1",
		"  [ \"${ARTIFACT_COLUMN_PLUGIN_REPOSITORY}\" = \"${ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH}\" ] || return 1",
		"  [ \"${ARTIFACT_COLUMN_PLUGIN_METADATA_ENV}\" = \"${ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH}/share/artifact-column-plugin/metadata.env\" ] || return 1",
		"  [ \"${ARTIFACT_COLUMN_PLUGIN_PINNED_ARTIFACT:-}\" = \"1\" ] || return 1",
		"  [ \"${ARTIFACT_COLUMN_PLUGIN_FLAKE_ARTIFACT:-}\" = \"0\" ] || return 1",
		"  case \"${ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH}\" in",
		"    /nix/store/*) ;;",
		"    *) return 1 ;;",
		"  esac",
		"  case \"${ARTIFACT_COLUMN_PLUGIN_VERSION}\" in",
		"    *SNAPSHOT*|*snapshot*) return 1 ;;",
		"  esac",
		"  case \"${ARTIFACT_COLUMN_PLUGIN_IVY_PATH}\" in",
		"    ivy2/local/com.crib.bills.ouroboros/artifact-column-plugin_sbt2_3/*) ;;",
		"    *) return 1 ;;",
		"  esac",
		"  [ -f \"${ARTIFACT_COLUMN_PLUGIN_METADATA_ENV}\" ] || return 1",
		"  [ -f \"${ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH}/share/artifact-column-plugin/artifact-column-plugin.jar.sha256\" ] || return 1",
		"  [ \"$(tr -d '[:space:]' < \"${ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH}/share/artifact-column-plugin/artifact-column-plugin.jar.sha256\")\" = \"${ARTIFACT_COLUMN_PLUGIN_JAR_SHA256}\" ] || return 1",
		"}",
		"devkit_governance_have_submit_runtime_authority() {",
		"  case \"${SBT2_CLIENT_MODE:-}\" in force|off) ;; *) return 1 ;; esac",
		"  case \"${OURO_LINT_INVARIANCE_SCRIPTED_SBT2_CLIENT_MODE:-}\" in force|off) ;; *) return 1 ;; esac",
		"  local java_xmx=\"${SBT2_JAVA_XMX:-}\"",
		"  [ -n \"$java_xmx\" ] || return 1",
		"  case \"$java_xmx\" in *[!0123456789mMgG]*) return 1 ;; esac",
		"  case \"$java_xmx\" in *[mMgG]) ;; *) return 1 ;; esac",
		"  local java_xmx_digits=\"${java_xmx%?}\"",
		"  [ -n \"$java_xmx_digits\" ] || return 1",
		"  case \"$java_xmx_digits\" in *[!0123456789]*) return 1 ;; esac",
		"}",
		"devkit_governance_require_runtime_jar() {",
		"  if devkit_governance_have_expected_jar && devkit_governance_have_expected_submit_to_ci_jar && devkit_governance_have_expected_artifact_column_plugin_repository && devkit_governance_have_submit_runtime_authority; then",
		"    return 0",
		"  fi",
		"  echo \"[devkit-governance-env] runtime env did not provide pinned Nix-store governance and submit-to-ci jars, artifact-column plugin repository, plus submit runtime authority from ${DEVKIT_GOVERNANCE_RUNTIME_FLAKE}\" >&2",
		"  return 1",
		"}",
		"devkit_governance_static_runtime_env_ready() {",
		"  devkit_governance_have_expected_jar || return 1",
		"  devkit_governance_have_expected_submit_to_ci_jar || return 1",
		"  devkit_governance_have_expected_artifact_column_plugin_repository || return 1",
		"  devkit_governance_have_submit_runtime_authority || return 1",
		"  [ -n \"${JAVA_HOME:-}\" ] || return 1",
		"  [ -x \"${JAVA_HOME}/bin/java\" ] || return 1",
		"}",
		"if ! devkit_governance_static_runtime_env_ready; then",
		"  devkit_governance_load_runtime_env",
		"fi",
		"devkit_governance_require_runtime_jar",
		"if [ -z \"${JAVA_HOME:-}\" ] || [ ! -x \"${JAVA_HOME}/bin/java\" ]; then",
		"  echo \"[devkit-governance-env] runtime env did not provide an executable JAVA_HOME\" >&2",
		"  return 1",
		"fi",
		"export DEVKIT_GOVERNANCE_AUTHORITATIVE_ENV=1",
		"export SUBAGENT_GOVERNANCE_KNOWN_WORKSPACE_IDS="+strings.Join(catalog.ids, ","),
		"export SUBAGENT_GOVERNANCE_WORKSPACE_ROOTS="+strings.Join(catalog.rootBindings, ","),
		"export SUBAGENT_GOVERNANCE_SCHEMA_ROOT="+schemaRoot,
		"export SUBAGENT_GOVERNANCE_CONTROL_PLANE_URL=http://127.0.0.1:7778",
		"export SUBAGENT_GOVERNANCE_FORWARD_SERVER_URL=http://127.0.0.1:7778",
		"export SUBAGENT_GOVERNANCE_WARM_HOOK_CMD='scripts/devops/governance-control-plane warm'",
		"export SUBAGENT_GOVERNANCE_CONTROL_PLANE_STATE_DIR="+stateDir,
		"",
	)
	return strings.Join(lines, "\n")
}

type ouroGovernanceCatalog struct {
	ids          []string
	rootBindings []string
	rootMap      map[string]string
}

func buildOuroGovernanceCatalog() ouroGovernanceCatalog {
	return buildOuroGovernanceCatalogForRoot("")
}

func buildOuroGovernanceCatalogForRoot(hostDevRoot string) ouroGovernanceCatalog {
	ids := []string{"dev-workspace", "ouroboros-ide", "ouroboros-terraform"}
	roots := map[string]string{
		"dev-workspace":       "/workspaces/dev",
		"ouroboros-ide":       "/workspaces/dev/ouroboros-ide",
		"ouroboros-terraform": "/workspaces/dev/ouroboros-terraform",
	}
	rootBindings := []string{
		"dev-workspace=/workspaces/dev",
		"ouroboros-ide=/workspaces/dev/ouroboros-ide",
		"ouroboros-terraform=/workspaces/dev/ouroboros-terraform",
	}
	add := func(id, root string) {
		if id == "" || root == "" {
			return
		}
		if _, exists := roots[id]; exists {
			return
		}
		ids = append(ids, id)
		roots[id] = root
		rootBindings = append(rootBindings, fmt.Sprintf("%s=%s", id, root))
	}
	for i := 1; i <= 9; i++ {
		agent := fmt.Sprintf("agent%d", i)
		root := fmt.Sprintf("/workspaces/dev/agent-worktrees/%s/ouroboros-ide", agent)
		add(agent, root)
	}
	for i := 1; i <= 9; i++ {
		agent := fmt.Sprintf("agent%d", i)
		id := fmt.Sprintf("%s-ouroboros-terraform", agent)
		root := fmt.Sprintf("/workspaces/dev/agent-worktrees/%s/ouroboros-terraform", agent)
		add(id, root)
	}
	add("email-policy-mcp-app", "/workspaces/dev/agent-worktrees/email-policy-mcp-app/ouroboros-ide")
	// Keep the shared catalog source-derived and predictable. Ad-hoc worktrees
	// need explicit launch identity instead of being broadcast to every
	// governed app-server, especially Terraform lanes.
	return ouroGovernanceCatalog{ids: ids, rootBindings: rootBindings, rootMap: roots}
}

func buildOuroGovernanceRepoConfig(hostDevRoot string) ([]byte, error) {
	hostDevRoot = filepath.Clean(hostDevRoot)
	catalog := buildOuroGovernanceCatalogForRoot(hostDevRoot)
	type governanceAdapter struct {
		KnownWorkspaceIDs    []string          `json:"knownWorkspaceIds"`
		WorkspaceRoots       map[string]string `json:"workspaceRoots"`
		SchemaRoot           string            `json:"schemaRoot"`
		ControlPlaneURL      string            `json:"controlPlaneUrl"`
		WarmHookCommand      string            `json:"warmHookCommand"`
		ControlPlaneStateDir string            `json:"controlPlaneStateDir"`
	}
	type repoConfig struct {
		WorkspaceRoot     string            `json:"workspaceRoot"`
		SkillCatalogPath  string            `json:"skillCatalogPath"`
		PolicyCatalogPath string            `json:"policyCatalogPath"`
		PromptBundleRoot  string            `json:"promptBundleRoot"`
		GovernanceAdapter governanceAdapter `json:"governanceAdapter"`
	}
	const governanceSourceRoot = "/workspaces/dev/ouroboros-ide/tools/subagent-governance"
	cfg := repoConfig{
		WorkspaceRoot:     "/workspaces/dev/ouroboros-ide",
		SkillCatalogPath:  governanceSourceRoot + "/catalog/skills.json",
		PolicyCatalogPath: governanceSourceRoot + "/catalog/policies.json",
		PromptBundleRoot:  governanceSourceRoot + "/skills",
		GovernanceAdapter: governanceAdapter{
			KnownWorkspaceIDs:    catalog.ids,
			WorkspaceRoots:       catalog.rootMap,
			SchemaRoot:           governanceSourceRoot + "/schemas",
			ControlPlaneURL:      "http://127.0.0.1:7778",
			WarmHookCommand:      "scripts/devops/governance-control-plane warm",
			ControlPlaneStateDir: "/workspaces/dev/ouroboros-ide/logs/subagent-governance/control-plane",
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render governance repo config: %w", err)
	}
	return append(data, '\n'), nil
}

func ensureCodexGovernanceConfig(p nativeplan.Plan) error {
	if !isOuroGovernedPlan(p) {
		return nil
	}
	if worktree := strings.TrimSpace(p.Agent.HostWorktree); worktree != "" {
		if err := cleanCodexGovernanceConfigAt(filepath.Join(worktree, ".codex", "config.toml")); err != nil {
			return err
		}
	}
	hostHome := strings.TrimSpace(p.Agent.HostHome)
	if hostHome == "" {
		return nil
	}
	return ensureCodexGovernanceConfigAt(filepath.Join(hostHome, ".codex", "config.toml"), p)
}

func cleanCodexGovernanceConfigAt(configPath string) error {
	var original string
	if data, err := os.ReadFile(configPath); err == nil {
		original = string(data)
	} else if os.IsNotExist(err) {
		return nil
	} else {
		return fmt.Errorf("read Codex config %s: %w", configPath, err)
	}
	next := removeManagedCodexGovernanceBlock(original)
	next = removeTomlTable(next, "mcp_servers.governance")
	next = removeTomlTablesWithPrefix(next, "mcp_servers.governance.")
	next = removeTomlTablesWithPrefix(next, "projects.")
	next = strings.TrimRight(next, "\r\n")
	if strings.TrimSpace(next) != "" {
		next += "\n"
	}
	if next == original {
		return nil
	}
	if err := os.WriteFile(configPath, []byte(next), 0o600); err != nil {
		return fmt.Errorf("write Codex config %s: %w", configPath, err)
	}
	return nil
}

func ensureCodexGovernanceConfigAt(configPath string, p nativeplan.Plan) error {
	original, source, err := authoritativeCodexConfig(configPath, p)
	if err != nil {
		return err
	}
	if err := requireNixCodexConfig(original, source); err != nil {
		return err
	}
	next := removeManagedCodexGovernanceBlock(original)
	next = removeTomlTable(next, "mcp_servers.governance")
	next = removeTomlTablesWithPrefix(next, "mcp_servers.governance.")
	next = removeTomlTablesWithPrefix(next, "projects.")
	next = strings.TrimRight(next, "\r\n")
	if strings.TrimSpace(next) != "" {
		next += "\n\n"
	}
	next += codexGovernanceConfigBlock(p)
	if next == original {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(configPath), err)
	}
	if err := os.WriteFile(configPath, []byte(next), 0o600); err != nil {
		return fmt.Errorf("write Codex config %s: %w", configPath, err)
	}
	return nil
}

func authoritativeCodexConfig(configPath string, p nativeplan.Plan) (string, string, error) {
	explicitSource := strings.TrimSpace(os.Getenv("DEVKIT_CODEX_CONFIG_SOURCE"))
	if explicitSource != "" {
		config, err := readRequiredNixCodexConfig(explicitSource)
		if err != nil {
			return "", "", err
		}
		return config, filepath.Clean(explicitSource), nil
	}

	cachePath := codexConfigCachePath(p)
	checked := []string{codexSystemConfigPath}
	if cachePath != "" {
		checked = append(checked, cachePath)
	}
	if config, err := readRequiredNixCodexConfig(codexSystemConfigPath); err == nil {
		if cachePath != "" {
			if err := syncCodexConfigCache(cachePath, config); err != nil {
				return "", "", err
			}
		}
		return config, codexSystemConfigPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}

	if cachePath != "" {
		if config, err := readRequiredNixCodexConfig(cachePath); err == nil {
			return config, cachePath, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", "", err
		}
	}
	return "", "", fmt.Errorf("missing Nix-authored Codex config for governed launch; checked %s and %s; refusing to synthesize a base-only config.toml",
		strings.Join(checked, ", "), configPath)
}

func readRequiredNixCodexConfig(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("read authoritative Codex config %s: %w", path, os.ErrNotExist)
		}
		return "", fmt.Errorf("read authoritative Codex config %s: %w", path, err)
	}
	config := string(data)
	if err := requireNixCodexConfig(config, path); err != nil {
		return "", err
	}
	return config, nil
}

func codexConfigCachePath(p nativeplan.Plan) string {
	if hostDevRoot := hostDevRootForPlan(p); hostDevRoot != "" {
		return filepath.Join(hostDevRoot, codexDevkitConfigSourceRelPath)
	}
	return ""
}

func syncCodexConfigCache(cachePath, config string) error {
	current, err := os.ReadFile(cachePath)
	if err == nil && string(current) == config {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read Codex config cache %s: %w", cachePath, err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir Codex config cache dir %s: %w", filepath.Dir(cachePath), err)
	}
	if err := os.WriteFile(cachePath, []byte(config), 0o644); err != nil {
		return fmt.Errorf("sync Codex config cache %s: %w", cachePath, err)
	}
	return nil
}

func requireNixCodexConfig(config, source string) error {
	var missing []string
	if !strings.Contains(config, codexNixManagedConfigMarker) {
		missing = append(missing, codexNixManagedConfigMarker)
	}
	provider, ok := topLevelTomlString(config, "model_provider")
	if !ok {
		missing = append(missing, "top-level model_provider")
	} else if provider != "openai" {
		missing = append(missing, fmt.Sprintf(`model_provider = "openai" (got %q)`, provider))
	} else if !hasTomlTable(config, codexOpenAIProfileTable) {
		missing = append(missing, "["+codexOpenAIProfileTable+"]")
	}
	if len(missing) == 0 {
		return nil
	}
	if strings.TrimSpace(source) == "" {
		source = "<missing>"
	}
	return fmt.Errorf("authoritative Codex config %s missing %s; Nix must provide the source-derived config.toml options, refusing to synthesize a base-only config.toml",
		source, strings.Join(missing, ", "))
}

func topLevelTomlString(config, key string) (string, bool) {
	for _, line := range strings.Split(config, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			return "", false
		}
		name, raw, ok := strings.Cut(trimmed, "=")
		if !ok || strings.TrimSpace(name) != key {
			continue
		}
		value := strings.TrimSpace(raw)
		value = strings.Trim(value, `"`)
		return value, true
	}
	return "", false
}

func hasTomlTable(config, table string) bool {
	for _, line := range strings.Split(config, "\n") {
		if header, ok := tomlTableHeader(line); ok && header == table {
			return true
		}
	}
	return false
}

func codexGovernanceConfigBlock(p nativeplan.Plan) string {
	cwd := strings.TrimSpace(p.Agent.SandboxWorktree)
	if cwd == "" {
		cwd = strings.TrimSpace(p.DevkitSandboxRoot)
	}
	envPath := "/workspaces/dev/.devkit/ouro8-governance-env.sh"
	repoConfigPath := "/workspaces/dev/.devkit/ouro8-governance-repo-env.json"
	entrypoint := governanceentrypoint.Zsh()
	fingerprint := governanceentrypoint.SHA256()
	tools := []string{
		"run",
		"run_lint_migration",
		"submit_to_ci",
		"governance.workspace_topology",
		"governance.graph_status",
		"governance.search",
		"governance.write_yaml",
		"governance.operator_attention_opt_in",
		"governance.operator_attention_opt_out",
		"governance.operator_attention_status",
		"governance.operator_attention_inbox",
		"governance.operator_attention_record_blocker",
	}
	var b strings.Builder
	b.WriteString(codexGovernanceManagedBegin)
	b.WriteString("\n")
	b.WriteString("# source = devkit native launch generator\n")
	fmt.Fprintf(&b, "# governance_mcp_entrypoint_sha256 = %s\n", tomlQuote(fingerprint))
	b.WriteString("[mcp_servers.governance]\n")
	b.WriteString("command = \"/run/current-system/sw/bin/bash\"\n")
	fmt.Fprintf(&b, "cwd = %s\n", tomlQuote(cwd))
	fmt.Fprintf(&b, "args = [\"-lc\", %s]\n", tomlQuote(entrypoint))
	b.WriteString("startup_timeout_sec = 240\n")
	b.WriteString("tool_timeout_sec = 10800\n")
	b.WriteString("default_tools_approval_mode = \"approve\"\n")
	b.WriteString("enabled_tools = [")
	for i, tool := range tools {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(tomlQuote(tool))
	}
	b.WriteString("]\n\n")
	b.WriteString("[mcp_servers.governance.env]\n")
	fmt.Fprintf(&b, "DEVKIT_GOVERNANCE_ENV = %s\n", tomlQuote(envPath))
	fmt.Fprintf(&b, "DEVKIT_GOVERNANCE_REPO_CONFIG_PATH = %s\n", tomlQuote(repoConfigPath))
	fmt.Fprintf(&b, "SUBAGENT_GOVERNANCE_REPO_CONFIG_PATH = %s\n", tomlQuote(repoConfigPath))
	fmt.Fprintf(&b, "DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256 = %s\n", tomlQuote(fingerprint))
	for _, projectPath := range codexGovernanceProjectTrustPaths(p, cwd) {
		b.WriteString("\n")
		fmt.Fprintf(&b, "[projects.%s]\n", tomlQuote(projectPath))
		b.WriteString("trust_level = \"trusted\"\n")
	}
	b.WriteString(codexGovernanceManagedEnd)
	b.WriteString("\n")
	return b.String()
}

func codexGovernanceProjectTrustPaths(p nativeplan.Plan, cwd string) []string {
	hostDevRoot := hostDevRootForPlan(p)
	repo := strings.TrimSpace(p.Agent.ID.Repo)
	candidates := []string{
		cwd,
		strings.TrimSpace(p.Agent.SandboxWorktree),
		strings.TrimSpace(p.Agent.HostWorktree),
	}
	if hostDevRoot != "" && repo != "" {
		candidates = append(candidates,
			filepath.Join(hostDevRoot, repo),
			filepath.Join("/workspaces/dev", repo),
		)
	}
	var out []string
	seen := map[string]bool{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		value = filepath.Clean(value)
		if !strings.HasPrefix(value, "/") || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
	}
	for _, candidate := range candidates {
		add(candidate)
		if hostDevRoot == "" {
			continue
		}
		candidate = filepath.Clean(strings.TrimSpace(candidate))
		switch {
		case candidate == "/workspaces/dev":
			add(hostDevRoot)
		case strings.HasPrefix(candidate, "/workspaces/dev/"):
			add(filepath.Join(hostDevRoot, strings.TrimPrefix(candidate, "/workspaces/dev/")))
		case candidate == hostDevRoot:
			add("/workspaces/dev")
		case strings.HasPrefix(candidate, hostDevRoot+string(filepath.Separator)):
			if rel, err := filepath.Rel(hostDevRoot, candidate); err == nil && rel != "." {
				add(filepath.Join("/workspaces/dev", filepath.ToSlash(rel)))
			}
		}
	}
	return out
}

func tomlQuote(value string) string {
	return fmt.Sprintf("%q", value)
}
func removeManagedCodexGovernanceBlock(config string) string {
	for {
		start := strings.Index(config, codexGovernanceManagedBegin)
		if start < 0 {
			return config
		}
		endRel := strings.Index(config[start:], codexGovernanceManagedEnd)
		if endRel < 0 {
			return strings.TrimRight(config[:start], "\r\n")
		}
		end := start + endRel + len(codexGovernanceManagedEnd)
		config = strings.TrimRight(config[:start], "\r\n") + "\n" + strings.TrimLeft(config[end:], "\r\n")
	}
}

func removeTomlTable(config, table string) string {
	lines := strings.SplitAfter(config, "\n")
	var kept []string
	skip := false
	for _, line := range lines {
		if header, ok := tomlTableHeader(line); ok {
			skip = header == table
		}
		if skip {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "")
}

func removeTomlTablesWithPrefix(config, prefix string) string {
	lines := strings.SplitAfter(config, "\n")
	var kept []string
	skip := false
	for _, line := range lines {
		if header, ok := tomlTableHeader(line); ok {
			skip = strings.HasPrefix(header, prefix)
		}
		if skip {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "")
}

func tomlTableHeader(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "[[") || !strings.HasPrefix(trimmed, "[") || !strings.Contains(trimmed, "]") {
		return "", false
	}
	end := strings.Index(trimmed, "]")
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(trimmed[1:end]), true
}

func writeOuroCodexShellHook(hostHome string) error {
	hostHome = strings.TrimSpace(hostHome)
	if hostHome == "" {
		return nil
	}
	zshrc := filepath.Join(hostHome, ".zshrc")
	content := ouroCodexShellHookZsh()
	if data, err := os.ReadFile(zshrc); err == nil && string(data) == content {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", zshrc, err)
	}
	if err := os.MkdirAll(filepath.Dir(zshrc), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(zshrc), err)
	}
	if err := os.WriteFile(zshrc, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", zshrc, err)
	}
	return nil
}

func ouroCodexShellHookZsh() string {
	lines := []string{
		"export POWERLEVEL9K_DISABLE_CONFIGURATION_WIZARD=true",
		"typeset -g POWERLEVEL9K_INSTANT_PROMPT=off",
		"[[ -r /usr/local/share/powerlevel10k/powerlevel10k.zsh-theme ]] && source /usr/local/share/powerlevel10k/powerlevel10k.zsh-theme",
		"[[ -r ~/.p10k.zsh ]] && source ~/.p10k.zsh",
		`export CODEX_HOME="${CODEX_HOME:-$HOME/.codex}"`,
		`export CODEX_ROLLOUT_DIR="${CODEX_ROLLOUT_DIR:-$HOME/.codex/rollouts}"`,
		`export XDG_CACHE_HOME="${XDG_CACHE_HOME:-$HOME/.cache}"`,
		`export XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"`,
		`export DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256="` + governanceentrypoint.SHA256() + `"`,
		"unalias codex 2>/dev/null || true",
		codexTUILogGuardZsh,
		codexConfigGuardZsh,
		"codex() {",
		"  devkit_codex_tui_log_guard",
		"  devkit_codex_require_config || return",
		`  HOME="$HOME" CODEX_HOME="$CODEX_HOME" CODEX_ROLLOUT_DIR="$CODEX_ROLLOUT_DIR" XDG_CACHE_HOME="$XDG_CACHE_HOME" XDG_CONFIG_HOME="$XDG_CONFIG_HOME" command codex "$@"`,
		"}",
		"(( $+commands[claudew] )) && alias claude=claudew",
	}
	return strings.Join(lines, "\n") + "\n"
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

const codexConfigGuardZsh = `devkit_codex_require_config() {
  local config="${CODEX_HOME:-$HOME/.codex}/config.toml"
  if [[ ! -r "$config" ]]; then
    echo "[devkit-codex] required Nix-authored Codex config missing: $config" >&2
    return 1
  fi
  grep -Fqx '# source = nixos-wsl codex config' "$config" || {
    echo "[devkit-codex] Codex config is not Nix-authored: $config" >&2
    return 1
  }
  grep -Fqx 'model_provider = "openai"' "$config" || {
    echo "[devkit-codex] Codex config must use model_provider = \"openai\": $config" >&2
    return 1
  }
  grep -Fqx '[profiles.openai]' "$config" || {
    echo "[devkit-codex] Codex config missing [profiles.openai]: $config" >&2
    return 1
  }
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
		"known_hosts",
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
		if strings.HasSuffix(file, ".pub") || file == "known_hosts" {
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
	args = append(args, "/run/current-system/sw/bin/nix", "--extra-experimental-features", "nix-command flakes", "develop")
	names := make([]string, 0, len(p.FlakeInputOverrides))
	for name := range p.FlakeInputOverrides {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		args = append(args, "--override-input", name, p.FlakeInputOverrides[name])
	}
	args = append(args, p.Flake, "--output-lock-file", "/dev/null", "--command")
	args = append(args, shellCommand(p.DevkitSandboxRoot, p.Agent.ID.Project, p.Agent.SandboxWorktree, command, p.Proxy, p.Env)...)
	return Command{Path: "bwrap", Args: args, Dir: p.DevkitHostRoot}, nil
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
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return fmt.Errorf("read /etc/resolv.conf: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write resolv.conf %s: %w", path, err)
	}
	return nil
}

func shellCommand(devkitRoot string, project string, workdir string, command []string, proxy nativeplan.ProxyConfig, env map[string]string) []string {
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
	bridgeProxy := strings.TrimSpace(proxy.UnixSocket) != ""
	if bridgeProxy {
		proxyURL := strings.TrimSpace(proxy.HTTPProxy)
		if proxyURL == "" {
			proxyURL = "http://127.0.0.1:18888"
		}
		devctlPath := filepath.Join(devkitRoot, "kit", "bin", "devctl")
		script += " && { " + shellQuote(devctlPath) + " -p " + shellQuote(project) + " native proxy-bridge --listen 127.0.0.1:18888 --socket " + shellQuote(proxy.UnixSocket) + " & devkit_proxy_bridge_pid=$!; }"
		script += " && trap 'kill \"$devkit_proxy_bridge_pid\" >/dev/null 2>&1 || true; wait \"$devkit_proxy_bridge_pid\" >/dev/null 2>&1 || true' EXIT"
		script += " && sleep 0.1"
		script += " && { kill -0 \"$devkit_proxy_bridge_pid\" >/dev/null 2>&1 || { echo 'native proxy bridge failed to start' >&2; exit 1; }; }"
		script += " && export HTTP_PROXY=" + shellQuote(proxyURL)
		script += " HTTPS_PROXY=" + shellQuote(proxyURL)
		script += " http_proxy=" + shellQuote(proxyURL)
		script += " https_proxy=" + shellQuote(proxyURL)
		script += " NO_PROXY=" + shellQuote(proxy.NoProxy)
		script += " no_proxy=" + shellQuote(proxy.NoProxy)
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
	return []string{"bash", "-lc", script}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

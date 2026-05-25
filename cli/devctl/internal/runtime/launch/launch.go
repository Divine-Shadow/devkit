package launch

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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
	if err := configureWorktreeGitSSH(p.Agent.HostWorktree, sshCommand); err != nil {
		return err
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
	if strings.TrimSpace(p.Agent.ID.Project) != "dev-all" || strings.TrimSpace(p.Agent.ID.Repo) != "ouroboros-ide" {
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
	if strings.TrimSpace(p.Agent.ID.Project) == "dev-all" && strings.TrimSpace(p.Agent.ID.Repo) == "ouroboros-ide" {
		return writeOuroCodexShellHook(p.Agent.HostHome)
	}
	return repairRetiredCodexShellHook(p.Agent.HostHome)
}

func ensureOuroGovernanceEnv(p nativeplan.Plan) error {
	if strings.TrimSpace(p.Agent.ID.Project) != "dev-all" || strings.TrimSpace(p.Agent.ID.Repo) != "ouroboros-ide" {
		return nil
	}
	hostDevRoot := hostDevRootForPlan(p)
	if hostDevRoot == "" {
		return nil
	}
	envPath := filepath.Join(hostDevRoot, ".devkit", "ouro8-governance-env.sh")
	content := buildOuroGovernanceEnv(hostDevRoot)
	if data, err := os.ReadFile(envPath); err == nil && string(data) == content {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read governance env %s: %w", envPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(envPath), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(envPath), err)
	}
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write governance env %s: %w", envPath, err)
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

func buildOuroGovernanceEnv(hostDevRoot string) string {
	hostDevRoot = filepath.Clean(hostDevRoot)
	ids := []string{"ouroboros-ide"}
	roots := []string{"ouroboros-ide=/workspaces/dev/ouroboros-ide"}
	for i := 1; i <= 8; i++ {
		agent := fmt.Sprintf("agent%d", i)
		ids = append(ids, agent)
		roots = append(roots, fmt.Sprintf("%s=/workspaces/dev/agent-worktrees/%s/ouroboros-ide", agent, agent))
	}
	jar := filepath.Join(hostDevRoot, "ouroboros-ide", "tools", "subagent-governance", "subagent-governance.jar")
	stateDir := filepath.Join(hostDevRoot, "ouroboros-ide", "logs", "subagent-governance", "control-plane")
	schemaRoot := filepath.Join(hostDevRoot, "ouroboros-ide", "tools", "subagent-governance", "schemas")
	runtimeFlake := filepath.Join(hostDevRoot, "devkit") + "#dev-all"
	return strings.Join([]string{
		"# Shared governance MCP/control-plane environment for native dev-all GUI agents.",
		"# Do not set SUBAGENT_GOVERNANCE_WORKSPACE_ID here; each agent wrapper derives it from PWD.",
		"export DEVKIT_GOVERNANCE_RUNTIME_FLAKE=" + shellQuote(runtimeFlake),
		"devkit_governance_load_runtime_env() {",
		"  if [ -n \"${JAVA_HOME:-}\" ] && [ -x \"${JAVA_HOME}/bin/java\" ]; then",
		"    return 0",
		"  fi",
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
		"devkit_governance_load_runtime_env",
		"if [ -z \"${JAVA_HOME:-}\" ] || [ ! -x \"${JAVA_HOME}/bin/java\" ]; then",
		"  echo \"[devkit-governance-env] runtime env did not provide an executable JAVA_HOME\" >&2",
		"  return 1",
		"fi",
		"export SUBAGENT_GOVERNANCE_KNOWN_WORKSPACE_IDS=" + strings.Join(ids, ","),
		"export SUBAGENT_GOVERNANCE_WORKSPACE_ROOTS=" + strings.Join(roots, ","),
		"export SUBAGENT_GOVERNANCE_LATEST_JAR_PATH=" + jar,
		"export SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR=" + jar,
		"export SUBAGENT_GOVERNANCE_SCHEMA_ROOT=" + schemaRoot,
		"export SUBAGENT_GOVERNANCE_CONTROL_PLANE_URL=http://127.0.0.1:7778",
		"export SUBAGENT_GOVERNANCE_FORWARD_SERVER_URL=http://127.0.0.1:7778",
		"export SUBAGENT_GOVERNANCE_WARM_HOOK_CMD='scripts/devops/governance-control-plane warm'",
		"export SUBAGENT_GOVERNANCE_CONTROL_PLANE_STATE_DIR=" + stateDir,
		"",
	}, "\n")
}

func ensureCodexGovernanceConfig(p nativeplan.Plan) error {
	if strings.TrimSpace(p.Agent.ID.Project) != "dev-all" || strings.TrimSpace(p.Agent.ID.Repo) != "ouroboros-ide" {
		return nil
	}
	hostHome := strings.TrimSpace(p.Agent.HostHome)
	if hostHome == "" {
		return nil
	}
	configPaths := []string{filepath.Join(hostHome, ".codex", "config.toml")}
	if worktree := strings.TrimSpace(p.Agent.HostWorktree); worktree != "" {
		configPaths = append(configPaths, filepath.Join(worktree, ".codex", "config.toml"))
	}
	seen := map[string]bool{}
	for _, configPath := range configPaths {
		configPath = filepath.Clean(configPath)
		if seen[configPath] {
			continue
		}
		seen[configPath] = true
		if err := ensureCodexGovernanceConfigAt(configPath, p); err != nil {
			return err
		}
	}
	return nil
}

func ensureCodexGovernanceConfigAt(configPath string, p nativeplan.Plan) error {
	var original string
	if data, err := os.ReadFile(configPath); err == nil {
		original = string(data)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read Codex config %s: %w", configPath, err)
	}
	next := removeManagedCodexGovernanceBlock(original)
	next = removeTomlTable(next, "mcp_servers.governance")
	block := codexGovernanceConfigBlock(p)
	next = strings.TrimRight(next, "\r\n")
	if strings.TrimSpace(next) != "" {
		next += "\n\n"
	}
	next += block
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

func codexGovernanceConfigBlock(p nativeplan.Plan) string {
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
	b.WriteString(codexGovernanceManagedBegin + "\n")
	b.WriteString("[mcp_servers.governance]\n")
	b.WriteString("command = \"bash\"\n")
	if cwd := strings.TrimSpace(p.Agent.SandboxWorktree); cwd != "" {
		b.WriteString("cwd = " + strconv.Quote(cwd) + "\n")
	}
	b.WriteString("args = [\"-lc\", " + strconv.Quote(governanceMCPEntrypointZsh()) + "]\n")
	b.WriteString("default_tools_approval_mode = \"auto\"\n")
	b.WriteString("startup_timeout_sec = 60\n")
	b.WriteString("tool_timeout_sec = 10800\n")
	b.WriteString("enabled_tools = [\n")
	for _, tool := range tools {
		b.WriteString("  " + strconv.Quote(tool) + ",\n")
	}
	b.WriteString("]\n")
	b.WriteString(codexGovernanceManagedEnd + "\n")
	return b.String()
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
		"unalias codex 2>/dev/null || true",
		codexTUILogGuardZsh,
		"codex() {",
		"  local -a extra",
		"  extra=(",
		"    -m gpt-5.2",
		"    -a never",
		"    -s danger-full-access",
		`    -c 'mcp_servers.codex-cli.command="codex"'`,
		`    -c 'mcp_servers.codex-cli.args=["mcp-server"]'`,
		`    -c 'mcp_servers.codex-cli.startup_timeout_sec=60'`,
		`    -c 'mcp_servers.governance.command="bash"'`,
		`    -c "mcp_servers.governance.cwd=\"$PWD\""`,
		`    -c 'mcp_servers.governance.args=["-lc","` + governanceMCPEntrypointZsh() + `"]'`,
		`    -c 'mcp_servers.governance.startup_timeout_sec=60'`,
		`    -c 'mcp_servers.governance.tool_timeout_sec=10800'`,
		`    -c 'mcp_servers.governance.default_tools_approval_mode="auto"'`,
		`    -c 'mcp_servers.governance.enabled_tools=["run","run_lint_migration","submit_to_ci","governance.workspace_topology","governance.graph_status","governance.search","governance.write_yaml","governance.operator_attention_opt_in","governance.operator_attention_opt_out","governance.operator_attention_status","governance.operator_attention_inbox","governance.operator_attention_record_blocker"]'`,
		"  )",
		"  devkit_codex_tui_log_guard",
		`  HOME="$HOME" CODEX_HOME="$CODEX_HOME" CODEX_ROLLOUT_DIR="$CODEX_ROLLOUT_DIR" XDG_CACHE_HOME="$XDG_CACHE_HOME" XDG_CONFIG_HOME="$XDG_CONFIG_HOME" command codex "${extra[@]}" "$@"`,
		"}",
		"(( $+commands[claudew] )) && alias claude=claudew",
	}
	return strings.Join(lines, "\n") + "\n"
}

func governanceMCPEntrypointZsh() string {
	return strings.Join([]string{
		"unset SUBAGENT_GOVERNANCE_CONTROL_PLANE_AUTOWARM",
		"governance_env=",
		"governance_root=",
		"case ${PWD:-} in */agent-worktrees/*/ouroboros-ide) governance_env=${PWD%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh; governance_root=${PWD} ;; /workspaces/dev/ouroboros-ide) governance_env=/workspaces/dev/.devkit/ouro8-governance-env.sh; governance_root=${PWD} ;; /workspaces/dev/*) governance_env=/workspaces/dev/.devkit/ouro8-governance-env.sh; governance_root=/workspaces/dev/ouroboros-ide ;; */ouroboros-ide) governance_env=${PWD%/ouroboros-ide}/.devkit/ouro8-governance-env.sh; governance_root=${PWD} ;; esac",
		"if [[ -z ${governance_root} && -n ${CODEX_HOME:-} ]]; then case ${CODEX_HOME} in */agent-worktrees/*/ouroboros-ide/.devhome-agent*/.codex) governance_root=${CODEX_HOME%/.devhome-agent*/.codex}; governance_env=${governance_root%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh ;; */agent-worktrees/*/.devhome-agent*/.codex) governance_agent_dir=${CODEX_HOME%/.devhome-agent*/.codex}; governance_root=${governance_agent_dir}/ouroboros-ide; governance_env=${governance_agent_dir%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh ;; */ouroboros-ide/.codex) governance_root=${CODEX_HOME%/.codex}; governance_env=${governance_root%/ouroboros-ide}/.devkit/ouro8-governance-env.sh ;; esac; fi",
		"if [[ -z ${governance_env} ]]; then echo required governance env missing: unable to derive path for PWD=${PWD:-} >&2; exit 1; fi",
		"if [[ ! -r ${governance_env} ]]; then echo required governance env missing: ${governance_env} >&2; exit 1; fi",
		"if [[ -z ${governance_root} ]]; then echo required governance root missing: unable to derive path for PWD=${PWD:-} >&2; exit 1; fi",
		"if [[ ! -x ${governance_root}/scripts/devops/governance-mcp-stdio-forward ]]; then echo required governance bridge missing: ${governance_root}/scripts/devops/governance-mcp-stdio-forward >&2; exit 1; fi",
		"echo using governance env: ${governance_env} >&2",
		"echo using governance root: ${governance_root} >&2",
		"source ${governance_env}",
		"if [[ -z ${SUBAGENT_GOVERNANCE_WORKSPACE_ID:-} ]]; then case ${governance_root} in */agent-worktrees/*/ouroboros-ide) workspace_tail=${governance_root#*/agent-worktrees/}; export SUBAGENT_GOVERNANCE_WORKSPACE_ID=${workspace_tail%%/*} ;; */ouroboros-ide) export SUBAGENT_GOVERNANCE_WORKSPACE_ID=ouroboros-ide ;; esac; fi",
		"exec bash ${governance_root}/scripts/devops/governance-mcp-stdio-forward",
	}, "; ")
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
	const codexCommand = `  HOME="$HOME" CODEX_HOME="$HOME/.codex" CODEX_ROLLOUT_DIR="$HOME/.codex/rollouts" command codex "${extra[@]}" "$@"`
	if strings.Contains(repaired, codexCommand) && !strings.Contains(repaired, "  devkit_codex_tui_log_guard\n"+codexCommand) {
		repaired = strings.Replace(repaired, codexCommand, "  devkit_codex_tui_log_guard\n"+codexCommand, 1)
	}
	if repaired == original {
		return nil
	}
	if err := os.WriteFile(zshrc, []byte(repaired), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", zshrc, err)
	}
	return nil
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
		value := p.Env[key]
		if key == "XDG_CACHE_HOME" {
			value = filepath.Join("/tmp", "devkit-nix-cache", p.Agent.ID.Name())
		}
		args = append(args, "--setenv", key, value)
	}

	args = append(args, "--chdir", p.DevkitSandboxRoot)
	args = append(args, "/run/current-system/sw/bin/nix", "--extra-experimental-features", "nix-command flakes", "develop", p.Flake, "--output-lock-file", "/dev/null", "--command")
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

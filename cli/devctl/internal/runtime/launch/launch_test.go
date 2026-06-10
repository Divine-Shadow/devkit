package launch

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devkit/cli/devctl/internal/devkitpaths"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

func TestBuildBubblewrapUsesBrokerAndNoHostDockerSocket(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	repoRoot := filepath.Join(devRoot, "ouroboros-ide")
	for _, dir := range []string{devkitRoot, repoRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	brokerSocket := filepath.Join(tmp, "broker.sock")
	if err := os.WriteFile(brokerSocket, []byte("socket placeholder"), 0o600); err != nil {
		t.Fatalf("write broker socket placeholder: %v", err)
	}
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths:          devkitpaths.Paths{Root: devkitRoot},
		Repo:           "ouroboros-ide",
		Flake:          ".#runtime-test-agent",
		BrokerEndpoint: brokerSocket,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if err := os.MkdirAll(p.Agent.HostWorktree, 0o755); err != nil {
		t.Fatalf("mkdir host worktree: %v", err)
	}
	if err := os.MkdirAll(p.Agent.HostHome, 0o700); err != nil {
		t.Fatalf("mkdir host home: %v", err)
	}
	if err := os.MkdirAll(p.Agent.StateRoot, 0o700); err != nil {
		t.Fatalf("mkdir state root: %v", err)
	}
	if err := os.WriteFile(p.DNS.ResolvConf, []byte("nameserver 127.0.0.1\n"), 0o644); err != nil {
		t.Fatalf("write resolv: %v", err)
	}

	cmd, err := BuildBubblewrap(p, []string{"git", "--version"})
	if err != nil {
		t.Fatalf("BuildBubblewrap: %v", err)
	}
	joined := ShellString(cmd)
	for _, want := range []string{
		"'--bind' '" + devRoot + "' '/workspaces/dev'",
		"'--symlink' '/run/current-system/sw/bin/env' '/usr/bin/env'",
		"'--symlink' '/run/current-system/sw/bin/sh' '/bin/sh'",
		"'--bind' '" + brokerSocket + "' '" + brokerSocket + "'",
		"'--setenv' 'COURSIER_CACHE' '/workspaces/dev/.cache/shared/coursier'",
		"'--setenv' 'DOCKER_HOST' 'unix://" + brokerSocket + "'",
		"'--setenv' 'OURO_NIX_SANDBOX' '1'",
		"'--setenv' 'SBT_IVY_HOME' '/workspaces/dev/.cache/shared/ivy2'",
		"'--setenv' 'TMPDIR' '/tmp'",
		"'--setenv' 'XDG_CACHE_HOME' '/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1/.cache'",
		"'/run/current-system/sw/bin/nix' '--extra-experimental-features' 'nix-command flakes' 'develop' '.#runtime-test-agent' '--output-lock-file' '/dev/null'",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command missing %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(cmd.Args[len(cmd.Args)-1], "export XDG_CACHE_HOME='/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1/.cache'") {
		t.Fatalf("agent shell does not restore XDG_CACHE_HOME:\n%#v", cmd.Args)
	}
	if !strings.Contains(cmd.Args[len(cmd.Args)-1], "export COURSIER_CACHE='/workspaces/dev/.cache/shared/coursier'") {
		t.Fatalf("agent shell does not export shared coursier cache:\n%#v", cmd.Args)
	}
	if !strings.Contains(cmd.Args[len(cmd.Args)-1], "export OURO_NIX_SANDBOX='1'") {
		t.Fatalf("agent shell does not export Nix sandbox marker:\n%#v", cmd.Args)
	}
	if !strings.Contains(cmd.Args[len(cmd.Args)-1], "export SBT_BOOT_DIR='/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1/.sbt/boot'") {
		t.Fatalf("agent shell does not export per-agent SBT boot dir:\n%#v", cmd.Args)
	}
	for _, optionalHostPath := range []string{"/etc/static", "/etc/ssl", "/etc/pki"} {
		if _, err := os.Stat(optionalHostPath); err != nil {
			continue
		}
		want := "'--ro-bind' '" + optionalHostPath + "' '" + optionalHostPath + "'"
		if !strings.Contains(joined, want) {
			t.Fatalf("command missing optional host bind %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "'--dir' '/tmp'") {
		t.Fatalf("launcher must not create /tmp after mounting it as tmpfs:\n%s", joined)
	}
	if len(cmd.Args) < 1 || !strings.Contains(cmd.Args[len(cmd.Args)-1], "cd '/workspaces/dev/agent-worktrees/agent1/ouroboros-ide' && exec 'git' '--version'") {
		t.Fatalf("launch script did not cd and exec command:\n%#v", cmd.Args)
	}
	if strings.Contains(joined, "/var/run/docker.sock") {
		t.Fatalf("native launcher must not expose /var/run/docker.sock:\n%s", joined)
	}
}

func assertSourceGeneratedGovernanceConfig(t *testing.T, got string, preserved string) {
	t.Helper()
	if !strings.Contains(got, preserved) {
		t.Fatalf("config did not preserve existing content %q:\n%s", preserved, got)
	}
	for _, want := range []string{
		codexGovernanceManagedBegin,
		"# source = devkit native launch generator",
		"governance_mcp_entrypoint_sha256",
		"[mcp_servers.governance]",
		`command = "/run/current-system/sw/bin/bash"`,
		`DEVKIT_GOVERNANCE_REPO_CONFIG_PATH = "/workspaces/dev/.devkit/ouro8-governance-repo-env.json"`,
		"DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256",
		"[projects.",
		codexGovernanceManagedEnd,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("source-generated governance config missing %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{
		"env_vars = [",
		`"SUBAGENT_GOVERNANCE_CONTROL_PLANE_AUTOWARM"`,
		"SUBAGENT_GOVERNANCE_CONTROL_PLANE_BIND=0.0.0.0",
		`command = "bash"`,
		`args = ["-lc", "mkdir -p ${HOME:-/tmp}/.codex/log; set -x; exec bash scripts/devops/governance-mcp-stdio-forward"]`,
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("source-generated governance config contains stale mutable value %q:\n%s", forbidden, got)
		}
	}
}

func TestBuildBubblewrapPassesFlakeInputOverrides(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	repoRoot := filepath.Join(devRoot, "ouroboros-terraform")
	for _, dir := range []string{devkitRoot, repoRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	p, err := nativeplan.Build(nativeplan.BuildOptions{
		Paths:   devkitpaths.Paths{Root: devkitRoot},
		Project: "ouroboros-terraform",
		Repo:    "ouroboros-terraform",
		Flake:   "./overlays/ouroboros-terraform#default",
		FlakeInputOverrides: map[string]string{
			"ouroboros-terraform": "path:" + repoRoot,
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := os.MkdirAll(p.Agent.HostWorktree, 0o755); err != nil {
		t.Fatalf("mkdir host worktree: %v", err)
	}
	if err := os.MkdirAll(p.Agent.HostHome, 0o700); err != nil {
		t.Fatalf("mkdir host home: %v", err)
	}
	if err := os.MkdirAll(p.Agent.StateRoot, 0o700); err != nil {
		t.Fatalf("mkdir state root: %v", err)
	}
	if err := os.WriteFile(p.DNS.ResolvConf, []byte("nameserver 127.0.0.1\n"), 0o644); err != nil {
		t.Fatalf("write resolv: %v", err)
	}
	cmd, err := BuildBubblewrap(p, []string{"true"})
	if err != nil {
		t.Fatalf("BuildBubblewrap: %v", err)
	}
	joined := ShellString(cmd)
	want := "'develop' '--override-input' 'ouroboros-terraform' 'path:" + repoRoot + "' './overlays/ouroboros-terraform#default'"
	if !strings.Contains(joined, want) {
		t.Fatalf("command missing %q:\n%s", want, joined)
	}
}

func TestBuildBubblewrapProxySocketUnsharesNetworkAndStartsBridge(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	repoRoot := filepath.Join(devRoot, "ouroboros-ide")
	for _, dir := range []string{devkitRoot, repoRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	proxySocket := filepath.Join(tmp, "egress.sock")
	if err := os.WriteFile(proxySocket, []byte("socket placeholder"), 0o600); err != nil {
		t.Fatalf("write proxy socket placeholder: %v", err)
	}
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths:       devkitpaths.Paths{Root: devkitRoot},
		Repo:        "ouroboros-ide",
		Flake:       ".#runtime-test-agent",
		ProxySocket: proxySocket,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	cmd, err := BuildBubblewrap(p, []string{"curl", "-I", "https://api.openai.com"})
	if err != nil {
		t.Fatalf("BuildBubblewrap: %v", err)
	}
	joined := ShellString(cmd)
	for _, want := range []string{
		"'--unshare-net'",
		"'--bind' '" + proxySocket + "' '" + proxySocket + "'",
		"native proxy-bridge --listen 127.0.0.1:18888",
		"'--setenv' 'HTTP_PROXY' 'http://127.0.0.1:18888'",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command missing %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "proxy-bridge") || !strings.Contains(joined, proxySocket) {
		t.Fatalf("command missing proxy socket bridge:\n%s", joined)
	}
	if strings.Contains(joined, "'--share-net'") {
		t.Fatalf("proxy socket launches must not share host network:\n%s", joined)
	}
}

func TestPrepareRequiresExistingWorktree(t *testing.T) {
	tmp := t.TempDir()
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: filepath.Join(tmp, "devkit")},
		Repo:  "missing",
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if err := Prepare(p); err == nil {
		t.Fatalf("expected missing worktree error")
	}
}

func TestPrepareConfiguresGitSSHForSeededNativeIdentity(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: devkitRoot},
		Repo:  "ouroboros-ide",
		Index: 2,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p.Agent.HostWorktree), 0o755); err != nil {
		t.Fatalf("mkdir worktree parent: %v", err)
	}
	runTestCommand(t, "", "git", "init", p.Agent.HostWorktree)

	hostUserHome := filepath.Join(tmp, "host-user")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519"), "private")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519.pub"), "public")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "known_hosts"), "github.com ssh-ed25519 key")
	t.Setenv("HOME", hostUserHome)

	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".ssh", "config"), strings.Join([]string{
		"Host example.invalid",
		"  User keep",
		"Host github.com",
		"  HostName ssh.github.com",
		"  Port 443",
		"  User git",
		"  IdentityFile /old/home/.ssh/id_ed25519",
		"",
	}, "\n"))

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	sshCommand := "ssh -F " + filepath.Join(p.Agent.SandboxHome, ".ssh", "config")
	cfg := readTestFile(t, filepath.Join(p.Agent.HostHome, ".ssh", "config"))
	for _, want := range []string{
		gitSSHManagedBegin,
		"Host github.com",
		"  IdentityFile " + filepath.Join(p.Agent.SandboxHome, ".ssh", "id_ed25519"),
		"  IdentitiesOnly yes",
		"  BatchMode yes",
		"  UserKnownHostsFile " + filepath.Join(p.Agent.SandboxHome, ".ssh", "known_hosts"),
		gitSSHManagedEnd,
		"Host example.invalid",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("ssh config missing %q:\n%s", want, cfg)
		}
	}
	if got := runTestCommand(t, "", "git", "config", "--file", filepath.Join(p.Agent.HostHome, ".gitconfig"), "--get", "core.sshCommand"); got != sshCommand {
		t.Fatalf("global core.sshCommand = %q, want %q", got, sshCommand)
	}
	if got := runTestCommand(t, "", "git", "-C", p.Agent.HostWorktree, "config", "--get", "extensions.worktreeConfig"); got != "true" {
		t.Fatalf("extensions.worktreeConfig = %q, want true", got)
	}
	if got := runTestCommand(t, "", "git", "-C", p.Agent.HostWorktree, "config", "--worktree", "--get", "core.sshCommand"); got != sshCommand {
		t.Fatalf("worktree core.sshCommand = %q, want %q", got, sshCommand)
	}

	if err := Prepare(p); err != nil {
		t.Fatalf("second Prepare: %v", err)
	}
	cfg = readTestFile(t, filepath.Join(p.Agent.HostHome, ".ssh", "config"))
	if count := strings.Count(cfg, gitSSHManagedBegin); count != 1 {
		t.Fatalf("managed block count = %d, want 1:\n%s", count, cfg)
	}
	if strings.Contains(cfg, "ssh.github.com") || strings.Contains(cfg, "Port 443") || strings.Contains(cfg, "/old/home") {
		t.Fatalf("legacy github host block was preserved:\n%s", cfg)
	}
	if count := strings.Count(cfg, "Host github.com"); count != 1 {
		t.Fatalf("github host block count = %d, want 1:\n%s", count, cfg)
	}
}

func TestPrepareImportsMissingLegacyCodexStateWithoutClobber(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: devkitRoot},
		Repo:  "ouroboros-ide",
		Index: 2,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if err := os.MkdirAll(p.Agent.HostWorktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}

	legacyCodex := filepath.Join(p.Agent.StateRoot, "home", ".codex")
	legacySession := filepath.Join(legacyCodex, "sessions", "2026", "05", "15", "rollout.jsonl")
	writeTestFile(t, legacySession, "legacy session")
	legacySessionMtime := time.Date(2026, 5, 15, 12, 34, 56, 0, time.UTC)
	if err := os.Chtimes(legacySession, legacySessionMtime, legacySessionMtime); err != nil {
		t.Fatalf("set legacy session mtime: %v", err)
	}
	writeTestFile(t, filepath.Join(legacyCodex, "rollouts", "in-flight.jsonl"), "legacy rollout")
	writeTestFile(t, filepath.Join(legacyCodex, "shell_snapshots", "snapshot.sh"), "legacy shell")
	writeTestFile(t, filepath.Join(legacyCodex, "log", "codex-tui.log"), "legacy log")
	writeTestFile(t, filepath.Join(legacyCodex, "state_5.sqlite"), "legacy state")
	writeTestFile(t, filepath.Join(legacyCodex, "logs_2.sqlite"), "legacy logs")
	writeTestFile(t, filepath.Join(legacyCodex, "auth.json"), "legacy auth")
	writeTestFile(t, filepath.Join(legacyCodex, "config.toml"), "legacy config")

	dstCodex := filepath.Join(p.Agent.HostHome, ".codex")
	writeTestFile(t, filepath.Join(dstCodex, "auth.json"), "current auth")
	writeTestFile(t, filepath.Join(dstCodex, "config.toml"), "current config")
	beforeSessions := countFiles(t, filepath.Join(dstCodex, "sessions"))

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	importedSession := filepath.Join(dstCodex, "sessions", "2026", "05", "15", "rollout.jsonl")
	if got := readTestFile(t, importedSession); got != "legacy session" {
		t.Fatalf("session content = %q", got)
	}
	if st, err := os.Stat(importedSession); err != nil {
		t.Fatalf("stat imported session: %v", err)
	} else if !st.ModTime().Equal(legacySessionMtime) {
		t.Fatalf("imported session mtime = %s, want %s", st.ModTime(), legacySessionMtime)
	}
	for rel, want := range map[string]string{
		filepath.Join("rollouts", "in-flight.jsonl"):    "legacy rollout",
		filepath.Join("shell_snapshots", "snapshot.sh"): "legacy shell",
		filepath.Join("log", "codex-tui.log"):           "legacy log",
		"state_5.sqlite":                                "legacy state",
		"logs_2.sqlite":                                 "legacy logs",
		"auth.json":                                     "current auth",
	} {
		if got := readTestFile(t, filepath.Join(dstCodex, rel)); got != want {
			t.Fatalf("%s content = %q, want %q", rel, got, want)
		}
	}
	assertSourceGeneratedGovernanceConfig(t, readTestFile(t, filepath.Join(dstCodex, "config.toml")), "current config")
	afterSessions := countFiles(t, filepath.Join(dstCodex, "sessions"))
	if afterSessions < beforeSessions+1 {
		t.Fatalf("session count did not increase as expected: before=%d after=%d", beforeSessions, afterSessions)
	}

	if err := Prepare(p); err != nil {
		t.Fatalf("second Prepare: %v", err)
	}
	if got := countFiles(t, filepath.Join(dstCodex, "sessions")); got != afterSessions {
		t.Fatalf("second Prepare changed session count: before=%d after=%d", afterSessions, got)
	}
	if got := readTestFile(t, filepath.Join(dstCodex, "auth.json")); got != "current auth" {
		t.Fatalf("auth was clobbered: %q", got)
	}
	assertSourceGeneratedGovernanceConfig(t, readTestFile(t, filepath.Join(dstCodex, "config.toml")), "current config")
}

func TestPrepareRepairsRetiredCodexShellHookWithoutTouchingSessions(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: devkitRoot},
		Repo:  "ouroboros-ide",
		Index: 2,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if err := os.MkdirAll(p.Agent.HostWorktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	sessionPath := filepath.Join(p.Agent.HostHome, ".codex", "sessions", "past.jsonl")
	writeTestFile(t, sessionPath, "past")
	zshrc := filepath.Join(p.Agent.HostHome, ".zshrc")
	retiredPath := "/usr/local/bin/" + "codex"
	writeTestFile(t, zshrc, `codex() {
  HOME="$HOME" CODEX_HOME="$HOME/.codex" `+retiredPath+` "$@"
}
`)

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	got := readTestFile(t, zshrc)
	if strings.Contains(got, retiredPath) {
		t.Fatalf("retired codex path was not repaired:\n%s", got)
	}
	if !strings.Contains(got, "command codex") {
		t.Fatalf("repaired wrapper missing command codex:\n%s", got)
	}
	if !strings.Contains(got, "-m gpt-5.5") {
		t.Fatalf("repaired wrapper missing fleet-safe model pin:\n%s", got)
	}
	if !strings.Contains(got, "devkit_codex_tui_log_guard()") {
		t.Fatalf("repaired wrapper missing TUI log guard:\n%s", got)
	}
	if !strings.Contains(got, "DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256") {
		t.Fatalf("repaired wrapper missing governance entrypoint fingerprint:\n%s", got)
	}
	if !strings.Contains(got, "using governance env: ${governance_env}") {
		t.Fatalf("repaired wrapper does not loudly label selected governance env:\n%s", got)
	}
	if strings.Contains(got, "]] && source /workspaces/dev/.devkit/ouro8-governance-env.sh") {
		t.Fatalf("repaired wrapper silently skips missing governance env:\n%s", got)
	}
	if session := readTestFile(t, sessionPath); session != "past" {
		t.Fatalf("session was changed: %q", session)
	}
}

func TestPrepareAddsTUILogGuardToGeneratedCodexShellHook(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: devkitRoot},
		Repo:  "ouroboros-ide",
		Index: 2,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if err := os.MkdirAll(p.Agent.HostWorktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	zshrc := filepath.Join(p.Agent.HostHome, ".zshrc")
	writeTestFile(t, zshrc, `unalias codex 2>/dev/null || true
codex() {
  local -a extra
  extra=(
    -a never
  )
  HOME="$HOME" CODEX_HOME="$HOME/.codex" CODEX_ROLLOUT_DIR="$HOME/.codex/rollouts" command codex "${extra[@]}" "$@"
}
`)

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	got := readTestFile(t, zshrc)
	if !strings.Contains(got, "devkit_codex_tui_log_guard()") {
		t.Fatalf("generated wrapper missing TUI log guard function:\n%s", got)
	}
	if !strings.Contains(got, "-m gpt-5.5") {
		t.Fatalf("generated wrapper missing fleet-safe model pin:\n%s", got)
	}
	if !strings.Contains(got, "DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256") {
		t.Fatalf("generated wrapper missing governance entrypoint fingerprint:\n%s", got)
	}
	if !strings.Contains(got, "  devkit_codex_tui_log_guard\n  HOME=\"$HOME\" CODEX_HOME=") {
		t.Fatalf("generated wrapper does not call TUI log guard before codex:\n%s", got)
	}
	if !strings.Contains(got, "${PWD%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh") {
		t.Fatalf("generated wrapper missing host dev-root governance env derivation:\n%s", got)
	}
	if !strings.Contains(got, "required governance env missing") {
		t.Fatalf("generated wrapper missing loud governance env failure:\n%s", got)
	}
	if strings.Contains(got, "]] && source /workspaces/dev/.devkit/ouro8-governance-env.sh") {
		t.Fatalf("generated wrapper silently skips missing governance env:\n%s", got)
	}
}

func TestPrepareInstallsDevAllGovernedSearchPolicyRules(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	policy := "prefix_rule(\n    pattern = [\"rg\"],\n    decision = \"forbidden\"\n)\n"
	writeTestFile(t, filepath.Join(devkitRoot, "overlays", "dev-all", "codex-governed-search-policy.rules"), policy)

	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: devkitRoot},
		Repo:  "ouroboros-ide",
		Index: 2,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if err := os.MkdirAll(p.Agent.HostWorktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	existingConfig := strings.Join([]string{
		`personality = "existing"`,
		``,
		`[projects."/home/bayesartre/dev/ouroboros-ide"]`,
		`trust_level = "trusted"`,
		``,
		`[projects."/workspaces/dev/agent-worktrees/agent2/ouroboros-ide"]`,
		`trust_level = "trusted"`,
		``,
		codexGovernanceManagedBegin,
		`[mcp_servers.governance]`,
		`command = "bash"`,
		codexGovernanceManagedEnd,
		``,
	}, "\n")
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml"), existingConfig)
	writeTestFile(t, filepath.Join(p.Agent.HostWorktree, ".codex", "config.toml"), strings.Join([]string{
		`approval_policy = "never"`,
		`[mcp_servers.governance]`,
		`command = "bash"`,
		`args = ["-lc", "mkdir -p ${HOME:-/tmp}/.codex/log; set -x; exec bash scripts/devops/governance-mcp-stdio-forward"]`,
		`startup_timeout_sec = 60`,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "sessions", "past.jsonl"), "past session")
	for _, altForwarder := range []string{
		filepath.Join(devRoot, "agent-worktrees", "agent2", "ouroboros-ide-statement-classifier-submit", "scripts", "devops", "governance-mcp-stdio-forward"),
		filepath.Join(devRoot, "agent-worktrees", "terraform-ouro-1-redaction-safe", "ouroboros-ide", "scripts", "devops", "governance-mcp-stdio-forward"),
	} {
		writeTestFile(t, altForwarder, "#!/usr/bin/env bash\n")
	}

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	target := filepath.Join(p.Agent.HostHome, ".codex", "rules", "governed-search-policy.rules")
	if got := readTestFile(t, target); got != policy {
		t.Fatalf("policy rules = %q, want %q", got, policy)
	}
	gotConfig := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml"))
	assertSourceGeneratedGovernanceConfig(t, gotConfig, `personality = "existing"`)
	if strings.Count(gotConfig, `[projects."/home/bayesartre/dev/ouroboros-ide"]`) > 1 {
		t.Fatalf("config retained duplicate stale project trust table:\n%s", gotConfig)
	}
	for _, forbidden := range []string{
		"env_vars = [",
		"mcp_servers.governance.env.",
		`"SUBAGENT_GOVERNANCE_CONTROL_PLANE_AUTOWARM"`,
		"SUBAGENT_GOVERNANCE_CONTROL_PLANE_BIND=0.0.0.0",
	} {
		if strings.Contains(gotConfig, forbidden) {
			t.Fatalf("config contains forbidden %q:\n%s", forbidden, gotConfig)
		}
	}
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "sessions", "past.jsonl")); got != "past session" {
		t.Fatalf("session was clobbered: %q", got)
	}
	wantWorktreeConfig := strings.Join([]string{
		`approval_policy = "never"`,
		"",
	}, "\n")
	if gotWorktreeConfig := readTestFile(t, filepath.Join(p.Agent.HostWorktree, ".codex", "config.toml")); gotWorktreeConfig != wantWorktreeConfig {
		t.Fatalf("worktree config should remove mutable governance table, got:\n%s\nwant:\n%s", gotWorktreeConfig, wantWorktreeConfig)
	}
	envPath := filepath.Join(devRoot, ".devkit", "ouro8-governance-env.sh")
	gotEnv := readTestFile(t, envPath)
	for _, want := range []string{
		"Shared governance MCP/control-plane environment",
		"export DEVKIT_GOVERNANCE_RUNTIME_FLAKE='/workspaces/dev/devkit#dev-all'",
		"export DEVKIT_GOVERNANCE_JAR_RESOLVER='/workspaces/dev/ouroboros-ide/scripts/devops/resolve-pinned-jar'",
		"export DEVKIT_GOVERNANCE_REPO_CONFIG_PATH='/workspaces/dev/.devkit/ouro8-governance-repo-env.json'",
		"export DEVKIT_GOVERNANCE_REPO_CONFIG_SHA256=",
		"export DEVKIT_GOVERNANCE_EXPECTED_MCP_ENTRYPOINT_SHA256=",
		"export DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256=",
		"export SUBAGENT_GOVERNANCE_PINNED_ARTIFACT=1",
		"export SUBAGENT_GOVERNANCE_FLAKE_ARTIFACT=0",
		"devkit_governance_load_runtime_env()",
		"devkit_governance_have_expected_jar()",
		`[ -n "${SUBAGENT_GOVERNANCE_LATEST_JAR_PATH:-}" ] || return 1`,
		`[ "${SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR}" = "$jar_path" ] || return 1`,
		`[ "${DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH}" = "$jar_path" ] || return 1`,
		`/nix/store/*|*/target/pinned-jars/*) ;;`,
		`[ "$jar_sha" = "${DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256}" ] || return 1`,
		`[ "$jar_sha" = "${SUBAGENT_GOVERNANCE_EXPECTED_JAR_SHA256}" ] || return 1`,
		"devkit_governance_resolve_jar()",
		"--no-warn-dirty --option eval-cache false",
		"print-dev-env \"$DEVKIT_GOVERNANCE_RUNTIME_FLAKE\"",
		"pinned governance jar resolver missing: ${DEVKIT_GOVERNANCE_JAR_RESOLVER}",
		"${DEVKIT_GOVERNANCE_JAR_RESOLVER} --runtime subagent-governance",
		"unable to resolve published governance jar via ${DEVKIT_GOVERNANCE_JAR_RESOLVER}",
		"governance jar missing from pinned resolver output",
		"export DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH=\"$jar_path\"",
		"export DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256=\"$jar_sha\"",
		"export SUBAGENT_GOVERNANCE_EXPECTED_JAR_SHA256=\"$jar_sha\"",
		"runtime env did not provide an executable JAVA_HOME",
		"export DEVKIT_GOVERNANCE_AUTHORITATIVE_ENV=1",
		"export SUBAGENT_GOVERNANCE_KNOWN_WORKSPACE_IDS=dev-workspace,ouroboros-ide,ouroboros-terraform,agent1,agent2,agent3,agent4,agent5,agent6,agent7,agent8,agent9,agent1-ouroboros-terraform,agent2-ouroboros-terraform,agent3-ouroboros-terraform,agent4-ouroboros-terraform,agent5-ouroboros-terraform,agent6-ouroboros-terraform,agent7-ouroboros-terraform,agent8-ouroboros-terraform,agent9-ouroboros-terraform,email-policy-mcp-app",
		"dev-workspace=/workspaces/dev",
		"ouroboros-ide=/workspaces/dev/ouroboros-ide",
		"ouroboros-terraform=/workspaces/dev/ouroboros-terraform",
		"agent1=/workspaces/dev/agent-worktrees/agent1/ouroboros-ide",
		"agent8=/workspaces/dev/agent-worktrees/agent8/ouroboros-ide",
		"agent9=/workspaces/dev/agent-worktrees/agent9/ouroboros-ide",
		"agent1-ouroboros-terraform=/workspaces/dev/agent-worktrees/agent1/ouroboros-terraform",
		"agent9-ouroboros-terraform=/workspaces/dev/agent-worktrees/agent9/ouroboros-terraform",
		"email-policy-mcp-app=/workspaces/dev/agent-worktrees/email-policy-mcp-app/ouroboros-ide",
		"export SUBAGENT_GOVERNANCE_SCHEMA_ROOT=/workspaces/dev/ouroboros-ide/tools/subagent-governance/schemas",
		"export SUBAGENT_GOVERNANCE_WARM_HOOK_CMD='scripts/devops/governance-control-plane warm'",
	} {
		if !strings.Contains(gotEnv, want) {
			t.Fatalf("governance env missing %q:\n%s", want, gotEnv)
		}
	}
	guardedResolve := strings.Join([]string{
		"devkit_governance_load_runtime_env",
		"devkit_governance_resolve_jar",
	}, "\n")
	if !strings.Contains(gotEnv, guardedResolve) {
		t.Fatalf("governance env must resolve the source-pinned jar before accepting runtime state:\n%s", gotEnv)
	}
	for _, forbidden := range []string{
		"devkit_governance_clear_inherited_jar_identity",
		"DEVKIT_GOVERNANCE_JAR_FLAKE",
		"git+file:///workspaces/dev/ouroboros-ide",
		"build --no-link --print-out-paths \"$DEVKIT_GOVERNANCE_JAR_FLAKE\"",
		"unset SUBAGENT_GOVERNANCE_LATEST_JAR_PATH",
		"unset SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR",
		"unset DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH",
	} {
		if strings.Contains(gotEnv, forbidden) {
			t.Fatalf("governance env must not clear verified prewarmed jar identity %q:\n%s", forbidden, gotEnv)
		}
	}
	for _, forbidden := range []string{
		"agent2-ouroboros-ide-statement-classifier-submit",
		"terraform-ouro-1-redaction-safe",
	} {
		if strings.Contains(gotEnv, forbidden) {
			t.Fatalf("governance env leaked ad-hoc worktree %q:\n%s", forbidden, gotEnv)
		}
	}
	if strings.Contains(gotEnv, "SUBAGENT_GOVERNANCE_WORKSPACE_ID=") {
		t.Fatalf("shared governance env must not pin a per-agent workspace id:\n%s", gotEnv)
	}
	for _, forbidden := range []string{
		"export SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR=" + filepath.Join(devRoot, "ouroboros-ide", "tools", "subagent-governance", "subagent-governance.jar"),
		"export SUBAGENT_GOVERNANCE_LATEST_JAR_PATH=" + filepath.Join(devRoot, "ouroboros-ide", "tools", "subagent-governance", "subagent-governance.jar"),
	} {
		if strings.Contains(gotEnv, forbidden) {
			t.Fatalf("shared governance env retained mutable jar path %q:\n%s", forbidden, gotEnv)
		}
	}
	gotRepoConfig := readTestFile(t, filepath.Join(devRoot, ".devkit", "ouro8-governance-repo-env.json"))
	for _, want := range []string{
		`"workspaceRoot": "/workspaces/dev/ouroboros-ide"`,
		`"knownWorkspaceIds": [`,
		`"dev-workspace"`,
		`"ouroboros-ide"`,
		`"ouroboros-terraform"`,
		`"agent4"`,
		`"agent4-ouroboros-terraform"`,
		`"email-policy-mcp-app"`,
		`"dev-workspace": "/workspaces/dev"`,
		`"ouroboros-terraform": "/workspaces/dev/ouroboros-terraform"`,
		`"agent1": "/workspaces/dev/agent-worktrees/agent1/ouroboros-ide"`,
		`"agent4": "/workspaces/dev/agent-worktrees/agent4/ouroboros-ide"`,
		`"agent4-ouroboros-terraform": "/workspaces/dev/agent-worktrees/agent4/ouroboros-terraform"`,
		`"email-policy-mcp-app": "/workspaces/dev/agent-worktrees/email-policy-mcp-app/ouroboros-ide"`,
		`"controlPlaneUrl": "http://127.0.0.1:7778"`,
	} {
		if !strings.Contains(gotRepoConfig, want) {
			t.Fatalf("governance repo config missing %q:\n%s", want, gotRepoConfig)
		}
	}
	for _, forbidden := range []string{
		`"agent2-ouroboros-ide-statement-classifier-submit"`,
		`"terraform-ouro-1-redaction-safe"`,
	} {
		if strings.Contains(gotRepoConfig, forbidden) {
			t.Fatalf("governance repo config leaked ad-hoc worktree %q:\n%s", forbidden, gotRepoConfig)
		}
	}
	if strings.Contains(gotRepoConfig, `"latestJarPath"`) || strings.Contains(gotRepoConfig, "tools/subagent-governance/subagent-governance.jar") {
		t.Fatalf("governance repo config must not carry mutable jar authority:\n%s", gotRepoConfig)
	}
	if wantHash := fmt.Sprintf("%x", sha256.Sum256([]byte(gotRepoConfig))); !strings.Contains(gotEnv, "export DEVKIT_GOVERNANCE_REPO_CONFIG_SHA256='"+wantHash+"'") {
		t.Fatalf("governance env missing repo config hash %s:\n%s", wantHash, gotEnv)
	}
	if st, err := os.Stat(envPath); err != nil {
		t.Fatalf("stat governance env: %v", err)
	} else if got := st.Mode().Perm(); got != 0o600 {
		t.Fatalf("governance env mode = %o, want 600", got)
	}
}

func TestPrepareOuroTerraformCleansHomeGovernanceConfig(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	repoRoot := filepath.Join(devRoot, "ouroboros-terraform")
	for _, dir := range []string{devkitRoot, repoRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	p, err := nativeplan.Build(nativeplan.BuildOptions{
		Paths:   devkitpaths.Paths{Root: devkitRoot},
		Project: "ouroboros-terraform",
		Repo:    "ouroboros-terraform",
		Flake:   "./overlays/ouroboros-terraform#default",
		FlakeInputOverrides: map[string]string{
			"ouroboros-terraform": "path:" + repoRoot,
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := os.MkdirAll(p.Agent.HostWorktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml"), strings.Join([]string{
		`personality = "terraform"`,
		``,
		codexGovernanceManagedBegin,
		`[mcp_servers.governance]`,
		`command = "bash"`,
		codexGovernanceManagedEnd,
		``,
	}, "\n"))
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "sessions", "past.jsonl"), "past session")

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	assertSourceGeneratedGovernanceConfig(t, readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml")), `personality = "terraform"`)
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "sessions", "past.jsonl")); got != "past session" {
		t.Fatalf("session was clobbered: %q", got)
	}
	if _, err := os.Stat(filepath.Join(p.Agent.HostWorktree, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("Prepare must not create repo-local Terraform Codex config, err=%v", err)
	}
	gotEnv := readTestFile(t, filepath.Join(devRoot, ".devkit", "ouro8-governance-env.sh"))
	for _, want := range []string{
		"ouroboros-terraform=/workspaces/dev/ouroboros-terraform",
		"agent1-ouroboros-terraform=/workspaces/dev/agent-worktrees/agent1/ouroboros-terraform",
	} {
		if !strings.Contains(gotEnv, want) {
			t.Fatalf("governance env missing %q:\n%s", want, gotEnv)
		}
	}
	gotRepoConfig := readTestFile(t, filepath.Join(devRoot, ".devkit", "ouro8-governance-repo-env.json"))
	for _, want := range []string{
		`"ouroboros-terraform": "/workspaces/dev/ouroboros-terraform"`,
		`"agent1-ouroboros-terraform": "/workspaces/dev/agent-worktrees/agent1/ouroboros-terraform"`,
	} {
		if !strings.Contains(gotRepoConfig, want) {
			t.Fatalf("governance repo config missing %q:\n%s", want, gotRepoConfig)
		}
	}
}

func TestPrepareDevWorkspaceCleansHomeGovernanceConfigAndLinksSkills(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	policy := "prefix_rule(\n    pattern = [\"rg\"],\n    decision = \"forbidden\"\n)\n"
	writeTestFile(t, filepath.Join(devkitRoot, "overlays", "dev-all", "codex-governed-search-policy.rules"), policy)
	writeTestFile(t, filepath.Join(devRoot, ".codex", "config.toml"), "top_level = true\n")
	writeTestFile(t, filepath.Join(devRoot, ".codex", "skills", "devkit-management", "SKILL.md"), "# devkit management\n")
	writeTestFile(t, filepath.Join(devRoot, ".codex", "skills", "autonomy-contract", "SKILL.md"), "# autonomy contract\n")
	writeTestFile(t, filepath.Join(devRoot, "ouroboros-ide", ".codex", "skills", "governed-search", "SKILL.md"), "# governed search\n")

	p, err := nativeplan.Build(nativeplan.BuildOptions{
		Paths:   devkitpaths.Paths{Root: devkitRoot},
		Project: "dev-workspace",
		Flake:   "./overlays/dev-workspace#default",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := os.MkdirAll(p.Agent.HostWorktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml"), strings.Join([]string{
		`personality = "dev-workspace"`,
		``,
		codexGovernanceManagedBegin,
		`[mcp_servers.governance]`,
		`command = "bash"`,
		codexGovernanceManagedEnd,
		``,
	}, "\n"))

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if got := readTestFile(t, filepath.Join(devRoot, ".codex", "config.toml")); got != "top_level = true\n" {
		t.Fatalf("top-level config was modified:\n%s", got)
	}
	assertSourceGeneratedGovernanceConfig(t, readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml")), `personality = "dev-workspace"`)
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "rules", "governed-search-policy.rules")); got != policy {
		t.Fatalf("policy rules = %q, want %q", got, policy)
	}
	for skill, wantTarget := range map[string]string{
		"devkit-management": filepath.Join(devRoot, ".codex", "skills", "devkit-management"),
		"autonomy-contract": filepath.Join(devRoot, ".codex", "skills", "autonomy-contract"),
		"governed-search":   filepath.Join(devRoot, "ouroboros-ide", ".codex", "skills", "governed-search"),
	} {
		link := filepath.Join(p.Agent.HostHome, ".codex", "skills", skill)
		gotTarget, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("read skill link %s: %v", link, err)
		}
		if gotTarget != wantTarget {
			t.Fatalf("skill link %s = %q, want %q", skill, gotTarget, wantTarget)
		}
	}
	gotEnv := readTestFile(t, filepath.Join(devRoot, ".devkit", "ouro8-governance-env.sh"))
	for _, want := range []string{
		"dev-workspace=/workspaces/dev",
		"ouroboros-ide=/workspaces/dev/ouroboros-ide",
		"ouroboros-terraform=/workspaces/dev/ouroboros-terraform",
	} {
		if !strings.Contains(gotEnv, want) {
			t.Fatalf("governance env missing %q:\n%s", want, gotEnv)
		}
	}
	gotRepoConfig := readTestFile(t, filepath.Join(devRoot, ".devkit", "ouro8-governance-repo-env.json"))
	for _, want := range []string{
		`"dev-workspace"`,
		`"dev-workspace": "/workspaces/dev"`,
		`"ouroboros-ide": "/workspaces/dev/ouroboros-ide"`,
		`"ouroboros-terraform": "/workspaces/dev/ouroboros-terraform"`,
	} {
		if !strings.Contains(gotRepoConfig, want) {
			t.Fatalf("governance repo config missing %q:\n%s", want, gotRepoConfig)
		}
	}
	if strings.Contains(gotRepoConfig, `"latestJarPath"`) || strings.Contains(gotRepoConfig, "tools/subagent-governance/subagent-governance.jar") {
		t.Fatalf("governance repo config must not carry mutable jar authority:\n%s", gotRepoConfig)
	}
}

func TestPrepareCreatesSharedScalaCachesAndCapsOnlyCodexTUILog(t *testing.T) {
	t.Setenv("DEVKIT_CODEX_TUI_LOG_MAX_BYTES", "8")
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: devkitRoot},
		Repo:  "ouroboros-ide",
		Index: 2,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if err := os.MkdirAll(p.Agent.HostWorktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "log", "codex-tui.log"), "0123456789abcdef")
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "sessions", "keep.jsonl"), "session")
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "state.sqlite"), "sqlite")
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "rollouts", "keep.jsonl"), "rollout")

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	for _, dir := range []string{
		filepath.Join(devRoot, ".cache", "shared", "coursier"),
		filepath.Join(devRoot, ".cache", "shared", "ivy2"),
		filepath.Join(p.Agent.HostHome, ".sbt"),
		filepath.Join(p.Agent.HostHome, ".sbt", "boot"),
	} {
		if st, err := os.Stat(dir); err != nil {
			t.Fatalf("shared/per-agent cache dir missing %s: %v", dir, err)
		} else if !st.IsDir() {
			t.Fatalf("cache path is not a dir: %s", dir)
		}
	}
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "log", "codex-tui.log")); got != "89abcdef" {
		t.Fatalf("capped codex-tui.log = %q", got)
	}
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "sessions", "keep.jsonl")); got != "session" {
		t.Fatalf("session was touched: %q", got)
	}
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "state.sqlite")); got != "sqlite" {
		t.Fatalf("sqlite state was touched: %q", got)
	}
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "rollouts", "keep.jsonl")); got != "rollout" {
		t.Fatalf("rollout was touched: %q", got)
	}
}

func TestSeedSSHSeedsHostKeysAndKnownHosts(t *testing.T) {
	hostUserHome := t.TempDir()
	srcSSH := filepath.Join(hostUserHome, ".ssh")
	if err := os.MkdirAll(srcSSH, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"id_ed25519":     "private",
		"id_ed25519.pub": "public",
		"known_hosts":    "github.com key",
	} {
		if err := os.WriteFile(filepath.Join(srcSSH, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", hostUserHome)

	nativeHome := filepath.Join(t.TempDir(), "native-home")
	if err := SeedSSH(nativeHome, false); err != nil {
		t.Fatalf("SeedSSH: %v", err)
	}
	for name, wantMode := range map[string]os.FileMode{
		"id_ed25519":     0o600,
		"id_ed25519.pub": 0o644,
		"known_hosts":    0o644,
	} {
		info, err := os.Stat(filepath.Join(nativeHome, ".ssh", name))
		if err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
		if got := info.Mode().Perm(); got != wantMode {
			t.Fatalf("%s mode = %v, want %v", name, got, wantMode)
		}
	}
}

func TestDevAllOverlayRequiresGovernanceProvenance(t *testing.T) {
	overlayConfig := readTestFile(t, filepath.Join("..", "..", "..", "..", "..", "overlays", "dev-all", "devkit.yaml"))
	for _, want := range []string{
		"name: governance-provenance",
		"env_file=/workspaces/dev/.devkit/ouro8-governance-env.sh",
		"/nix/store/*/share/subagent-governance/subagent-governance.jar",
		"DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH",
		"DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256",
		"governance jar hash drift",
		"server.jar.path",
		"server.jar.sha256",
		"governance singleton jar path drift",
		"governance singleton jar hash drift",
		"/agent-state",
		"governance-mcp-stdio-forward/provenance.json",
		"governance bridge jar path drift",
		"governance bridge catalog hash drift",
		"governance bridge entrypoint fingerprint drift",
		"ouroboros-ide=/workspaces/dev/ouroboros-ide",
	} {
		if !strings.Contains(overlayConfig, want) {
			t.Fatalf("dev-all overlay missing governance provenance guard %q:\n%s", want, overlayConfig)
		}
	}
}

func TestSeedAWSSyncsConfigAndCaches(t *testing.T) {
	srcAWS := filepath.Join(t.TempDir(), ".aws")
	for _, dir := range []string{
		srcAWS,
		filepath.Join(srcAWS, "sso", "cache"),
		filepath.Join(srcAWS, "cli", "cache"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	files := map[string]string{
		"config":                                "[profile ouroboros]\nsso_session = mysesh\n",
		"credentials":                           "",
		filepath.Join("sso", "cache", "a.json"): `{"accessToken":"redacted"}`,
		filepath.Join("cli", "cache", "b.json"): `{"Credentials":{"AccessKeyId":"redacted"}}`,
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(srcAWS, rel), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	t.Setenv("DEVKIT_AWS_HOME", srcAWS)

	nativeHome := filepath.Join(t.TempDir(), "native-home")
	if err := SeedAWS(nativeHome, false); err != nil {
		t.Fatalf("SeedAWS: %v", err)
	}
	for rel, want := range files {
		if got := readTestFile(t, filepath.Join(nativeHome, ".aws", rel)); got != want {
			t.Fatalf("%s = %q, want %q", rel, got, want)
		}
		info, err := os.Stat(filepath.Join(nativeHome, ".aws", rel))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s mode = %v, want 0600", rel, got)
		}
	}
	for _, rel := range []string{".aws", filepath.Join(".aws", "sso"), filepath.Join(".aws", "sso", "cache"), filepath.Join(".aws", "cli"), filepath.Join(".aws", "cli", "cache")} {
		info, err := os.Stat(filepath.Join(nativeHome, rel))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("%s mode = %v, want 0700", rel, got)
		}
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func countFiles(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk %s: %v", root, err)
	}
	return count
}

func runTestCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

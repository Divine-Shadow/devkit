package launch

import (
	"os"
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
		"'--setenv' 'SBT_IVY_HOME' '/workspaces/dev/.cache/shared/ivy2'",
		"'--setenv' 'TMPDIR' '/tmp'",
		"'--setenv' 'XDG_CACHE_HOME' '/tmp/devkit-nix-cache/dev-all-agent1'",
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
		"config.toml":                                   "current config",
	} {
		if got := readTestFile(t, filepath.Join(dstCodex, rel)); got != want {
			t.Fatalf("%s content = %q, want %q", rel, got, want)
		}
	}
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
}

func TestPrepareRepairsRetiredCodexWrapperWithoutTouchingSessions(t *testing.T) {
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
	writeTestFile(t, zshrc, `codex() {
  HOME="$HOME" CODEX_HOME="$HOME/.codex" /usr/local/bin/codex "$@"
}
`)

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	got := readTestFile(t, zshrc)
	if strings.Contains(got, "/usr/local/bin/codex") {
		t.Fatalf("retired codex path was not repaired:\n%s", got)
	}
	if !strings.Contains(got, "command codex") {
		t.Fatalf("repaired wrapper missing command codex:\n%s", got)
	}
	if !strings.Contains(got, "devkit_codex_tui_log_guard()") {
		t.Fatalf("repaired wrapper missing TUI log guard:\n%s", got)
	}
	if session := readTestFile(t, sessionPath); session != "past" {
		t.Fatalf("session was changed: %q", session)
	}
}

func TestPrepareAddsTUILogGuardToGeneratedCodexWrapper(t *testing.T) {
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
	if !strings.Contains(got, "  devkit_codex_tui_log_guard\n  HOME=\"$HOME\" CODEX_HOME=") {
		t.Fatalf("generated wrapper does not call TUI log guard before codex:\n%s", got)
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
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml"), "existing config")
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "sessions", "past.jsonl"), "past session")

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	target := filepath.Join(p.Agent.HostHome, ".codex", "rules", "governed-search-policy.rules")
	if got := readTestFile(t, target); got != policy {
		t.Fatalf("policy rules = %q, want %q", got, policy)
	}
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml")); got != "existing config" {
		t.Fatalf("config was clobbered: %q", got)
	}
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "sessions", "past.jsonl")); got != "past session" {
		t.Fatalf("session was clobbered: %q", got)
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

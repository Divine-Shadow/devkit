package seed

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSeedScripts(t *testing.T) {
	scripts := BuildSeedScripts("/workspace/.devhome-agent1")
	if len(scripts) < 5 {
		t.Fatalf("expected >=5 scripts, got %d", len(scripts))
	}
	// Check presence of key steps
	mustContain := []string{
		"/var/host-codex", // wait condition
		"mkdir -p '/workspace/.devhome-agent1/.codex' '/workspace/.devhome-agent1/.codex/rollouts'",
		"/var/host-codex/auth.json '/workspace/.devhome-agent1/.codex/auth.json'",
		"cp -f /var/auth.json '/workspace/.devhome-agent1/.codex/auth.json'",
		"chmod 600 '/workspace/.devhome-agent1/.codex/auth.json'",
	}
	joined := ""
	for _, s := range scripts {
		joined += s + "\n"
	}
	for _, m := range mustContain {
		if !contains(joined, m) {
			t.Fatalf("missing expected fragment: %q in scripts: %s", m, joined)
		}
	}
	assertNoDestructiveCodexCleanup(t, joined)
}

func TestBuildSeedScriptsPreservesCodexState(t *testing.T) {
	home := t.TempDir()
	writeCodexSentinels(t, home)
	runSeedSnippets(t, BuildSeedScripts(home), skipHostMountSnippet)
	assertCodexSentinels(t, home)
	assertRequiredHomeDirs(t, home)
}

func TestBuildAnchorScripts(t *testing.T) {
	cfg := AnchorConfig{
		Anchor:    "/workspace/.devhome",
		Base:      "/workspace/.devhomes",
		SeedCodex: true,
	}
	scripts := BuildAnchorScripts(cfg)
	if len(scripts) != 1 {
		t.Fatalf("expected single combined script, got %d", len(scripts))
	}
	sc := scripts[0]
	for _, frag := range []string{
		"target=\"/workspace/.devhomes/$(hostname)\"",
		"dev_home_ok=0; if mkdir -p /home/dev 2>/dev/null; then dev_home_ok=1; elif [ -d /home/dev ]; then dev_home_ok=1; fi",
		"ln -sfn \"$target\" \"/workspace/.devhome\"",
		"mkdir -p \"$target/.sbt\"",
		"if [ \"$dev_home_ok\" = 1 ] && [ -n \"${DOCKER_HOST:-}\" ]; then printf \"docker.host=%s\\n\" \"$DOCKER_HOST\" > \"$target/.testcontainers.properties\"; ln -sfn \"$target/.testcontainers.properties\" /home/dev/.testcontainers.properties; fi",
		"ln -sfn \"$target/.sbt\" /home/dev/.sbt",
		"if [ -r /var/host-home/.p10k.zsh ]; then cp -f /var/host-home/.p10k.zsh \"$target/.p10k.zsh\"; sed -i 's/^\\([[:space:]]*typeset -g POWERLEVEL9K_INSTANT_PROMPT=\\).*/\\1off/' \"$target/.p10k.zsh\"; fi",
		"POWERLEVEL9K_INSTANT_PROMPT=off",
		"source /usr/local/share/powerlevel10k/powerlevel10k.zsh-theme",
		"source ~/.p10k.zsh",
		"unalias codex 2>/dev/null || true",
		"devkit_codex_tui_log_guard()",
		"codex-tui.log",
		"tail -c \"$max\" \"$log\"",
		"codex() {",
		"mcp_servers.codex-cli.command=\\\"codex\\\"",
		"mcp_servers.governance.command=\\\"bash\\\"",
		"/workspaces/dev/.devkit/ouro8-governance-env.sh",
		"\\${PWD%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh",
		"required governance env missing",
		"required governance root missing",
		"SUBAGENT_GOVERNANCE_WORKSPACE_ID",
		"workspace_tail=\\${governance_root#*/agent-worktrees/}",
		"governance_root=\\${PWD}",
		"required governance bridge missing",
		"using governance env: \\${governance_env}",
		"exec bash \\${governance_root}/scripts/devops/governance-mcp-stdio-forward",
		"devkit_codex_tui_log_guard",
		`command codex "${extra[@]}" "$@"`,
		"marker=\"$target/.codex/.seeded\"",
		"if [ ! -f \"$marker\" ]; then",
		"touch \"$marker\"",
		"mkdir -p \"$target/.codex\" \"$target/.codex/rollouts\"",
		"cp -f /var/host-codex/auth.json \"$target/.codex/auth.json\"",
	} {
		if !contains(sc, frag) {
			t.Fatalf("combined script missing %q: %s", frag, sc)
		}
	}
	joined := JoinScripts(scripts)
	if !contains(joined, sc) {
		t.Fatalf("joined scripts missing combined anchor script: %s", joined)
	}
	if contains(sc, "SUBAGENT_GOVERNANCE_CONTROL_PLANE_AUTOWARM=0") {
		t.Fatalf("anchor script must not disable governance singleton auto-warm: %s", sc)
	}
	if contains(sc, "]] && source /workspaces/dev/.devkit/ouro8-governance-env.sh") {
		t.Fatalf("anchor script must not silently skip missing governance env: %s", sc)
	}
	assertNoDestructiveCodexCleanup(t, sc)
	if contains(sc, "/usr/local/bin/"+"codex") {
		t.Fatalf("anchor script must resolve codex from PATH: %s", sc)
	}
	for _, retired := range []string{"codex" + "w", "docker " + "compose", "docker " + "exec"} {
		if contains(sc, retired) {
			t.Fatalf("anchor script contains retired runtime assumption %q: %s", retired, sc)
		}
	}
}

func TestBuildForceReseedScripts(t *testing.T) {
	scripts := BuildForceReseedScripts("/workspace/.devhome")
	if len(scripts) < 6 {
		t.Fatalf("expected >=6 scripts, got %d", len(scripts))
	}
	joined := ""
	for _, s := range scripts {
		joined += s + "\n"
	}
	for _, frag := range []string{
		"mkdir -p '/workspace/.devhome/.codex' '/workspace/.devhome/.codex/rollouts' '/workspace/.devhome/.cache' '/workspace/.devhome/.config' '/workspace/.devhome/.local'",
		"/var/host-codex/auth.json '/workspace/.devhome/.codex/auth.json'",
		"cp -f /var/auth.json '/workspace/.devhome/.codex/auth.json'",
		"touch '/workspace/.devhome/.codex/.seeded'",
	} {
		if !contains(joined, frag) {
			t.Fatalf("missing expected fragment %q in force reseed scripts: %s", frag, joined)
		}
	}
	assertNoDestructiveCodexCleanup(t, joined)
}

func TestBuildForceReseedScriptsPreservesCodexStateWithoutHostAuth(t *testing.T) {
	home := t.TempDir()
	writeCodexSentinels(t, home)
	runSeedSnippets(t, BuildForceReseedScripts(home), skipHostMountSnippet)
	assertCodexSentinels(t, home)
	assertRequiredHomeDirs(t, home)
	if _, err := os.Stat(filepath.Join(home, ".codex", ".seeded")); err != nil {
		t.Fatalf("force reseed marker missing: %v", err)
	}
}

func TestBuildDirectHomeScripts(t *testing.T) {
	scripts := BuildDirectHomeScripts("/workspaces/dev/agent-worktrees/agent2/.devhome-agent2", true)
	if len(scripts) != 1 {
		t.Fatalf("expected single combined script, got %d", len(scripts))
	}
	sc := scripts[0]
	for _, frag := range []string{
		`home="/workspaces/dev/agent-worktrees/agent2/.devhome-agent2"`,
		`mkdir -p "$home/.ssh" "$home/.cache" "$home/.config" "$home/.local"`,
		`cp -f /var/host-codex/auth.json "$home/.codex/auth.json"`,
		`touch $home/.codex/.seeded`,
		`codex() {`,
		`devkit_codex_tui_log_guard()`,
		`tail -c "$max" "$log"`,
		`/workspaces/dev/.devkit/ouro8-governance-env.sh`,
		`\${PWD%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh`,
		`required governance env missing`,
		`required governance root missing`,
		`SUBAGENT_GOVERNANCE_WORKSPACE_ID`,
		`workspace_tail=\${governance_root#*/agent-worktrees/}`,
		`governance_root=\${PWD}`,
		`required governance bridge missing`,
		`using governance env: \${governance_env}`,
		`exec bash \${governance_root}/scripts/devops/governance-mcp-stdio-forward`,
		`devkit_codex_tui_log_guard`,
		`command codex "${extra[@]}" "$@"`,
	} {
		if !contains(sc, frag) {
			t.Fatalf("direct home script missing %q: %s", frag, sc)
		}
	}
	if contains(sc, "SUBAGENT_GOVERNANCE_CONTROL_PLANE_AUTOWARM=0") {
		t.Fatalf("direct home script must not disable governance singleton auto-warm: %s", sc)
	}
	if contains(sc, "]] && source /workspaces/dev/.devkit/ouro8-governance-env.sh") {
		t.Fatalf("direct home script must not silently skip missing governance env: %s", sc)
	}
	assertNoDestructiveCodexCleanup(t, sc)
	if contains(sc, "/usr/local/bin/"+"codex") {
		t.Fatalf("direct home script must resolve codex from PATH: %s", sc)
	}
	for _, retired := range []string{"codex" + "w", "docker " + "compose", "docker " + "exec"} {
		if contains(sc, retired) {
			t.Fatalf("direct home script contains retired runtime assumption %q: %s", retired, sc)
		}
	}
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (len(needle) == 0 || indexOf(hay, needle) >= 0)
}
func indexOf(h, n string) int {
	// simple substring search
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

const (
	authSentinel    = `{"auth":"preserve"}`
	rolloutSentinel = "in-flight rollout"
	configSentinel  = "model = \"preserve\"\n"
)

func writeCodexSentinels(t *testing.T, home string) {
	t.Helper()
	mustMkdir(t, filepath.Join(home, ".codex", "rollouts"))
	mustWrite(t, filepath.Join(home, ".codex", "auth.json"), authSentinel)
	mustWrite(t, filepath.Join(home, ".codex", "rollouts", "in-flight.jsonl"), rolloutSentinel)
	mustWrite(t, filepath.Join(home, ".codex", "config.toml"), configSentinel)
}

func assertCodexSentinels(t *testing.T, home string) {
	t.Helper()
	assertFileContent(t, filepath.Join(home, ".codex", "auth.json"), authSentinel)
	assertFileContent(t, filepath.Join(home, ".codex", "rollouts", "in-flight.jsonl"), rolloutSentinel)
	assertFileContent(t, filepath.Join(home, ".codex", "config.toml"), configSentinel)
}

func assertRequiredHomeDirs(t *testing.T, home string) {
	t.Helper()
	for _, rel := range []string{".codex", ".codex/rollouts", ".cache", ".config", ".local"} {
		path := filepath.Join(home, rel)
		if st, err := os.Stat(path); err != nil || !st.IsDir() {
			t.Fatalf("required dir %s missing: stat=%v err=%v", rel, st, err)
		}
	}
}

func assertNoDestructiveCodexCleanup(t *testing.T, script string) {
	t.Helper()
	for _, frag := range []string{
		"rm -r" + "f '$HOME/.codex'",
		"rm -r" + "f \"$HOME/.codex\"",
		"rm -r" + "f '/workspace/.devhome-agent1/.codex'",
		"rm -r" + "f '/workspace/.devhome/.codex'",
		"rm -r" + "f \"$target/.codex\"",
		"rm -r" + "f \"$home/.codex\"",
		"rm -" + "f '$HOME/.codex/auth.json'",
		"rm -" + "f \"$HOME/.codex/auth.json\"",
		"rm -" + "f '/workspace/.devhome/.codex/auth.json'",
		"rm -" + "f \"$target/.codex/auth.json\"",
		"rm -" + "f \"$home/.codex/auth.json\"",
	} {
		if strings.Contains(script, frag) {
			t.Fatalf("script must preserve existing Codex state; found %q in: %s", frag, script)
		}
	}
}

func runSeedSnippets(t *testing.T, snippets []string, skip func(string) bool) {
	t.Helper()
	for _, snippet := range snippets {
		if skip != nil && skip(snippet) {
			continue
		}
		cmd := exec.Command("bash", "-lc", snippet)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("run snippet %q: %v\n%s", snippet, err, out)
		}
	}
}

func skipHostMountSnippet(snippet string) bool {
	return strings.Contains(snippet, "/var/host-codex") || strings.Contains(snippet, "/var/auth.json")
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWrite(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s content changed: got %q want %q", path, got, want)
	}
}

package seed

import "testing"

func TestBuildSeedScripts(t *testing.T) {
	scripts := BuildSeedScripts("/workspace/.devhome-agent1")
	if len(scripts) < 5 {
		t.Fatalf("expected >=5 scripts, got %d", len(scripts))
	}
	// Check presence of key steps
	mustContain := []string{
		"/var/host-codex", // wait condition
		"rm -rf '/workspace/.devhome-agent1/.codex'",
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
		"codex() {",
		"mcp_servers.codex-cli.command=\\\"codex\\\"",
		"mcp_servers.governance.command=\\\"bash\\\"",
		"exec bash scripts/devops/governance-mcp-stdio-forward",
		"marker=\"$target/.codex/.seeded\"",
		"if [ ! -f \"$marker\" ]; then",
		"touch \"$marker\"",
		"rm -rf \"$target/.codex\"",
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
		"rm -f '/workspace/.devhome/.codex/auth.json'",
		"/var/host-codex/auth.json '/workspace/.devhome/.codex/auth.json'",
		"cp -f /var/auth.json '/workspace/.devhome/.codex/auth.json'",
		"touch '/workspace/.devhome/.codex/.seeded'",
	} {
		if !contains(joined, frag) {
			t.Fatalf("missing expected fragment %q in force reseed scripts: %s", frag, joined)
		}
	}
	for _, frag := range []string{
		"rm -rf '/workspace/.devhome/.codex'",
	} {
		if contains(joined, frag) {
			t.Fatalf("unexpected destructive fragment %q in force reseed scripts: %s", frag, joined)
		}
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
		`exec bash scripts/devops/governance-mcp-stdio-forward`,
	} {
		if !contains(sc, frag) {
			t.Fatalf("direct home script missing %q: %s", frag, sc)
		}
	}
	if contains(sc, "SUBAGENT_GOVERNANCE_CONTROL_PLANE_AUTOWARM=0") {
		t.Fatalf("direct home script must not disable governance singleton auto-warm: %s", sc)
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

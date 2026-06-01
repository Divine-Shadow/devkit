package seed

import (
	"fmt"
	"strings"
)

// WaitForHostMountsScript returns a script that waits (up to ~10s) for
// /var/host-codex or /var/auth.json to become available.
func WaitForHostMountsScript() string {
	return `for i in $(seq 1 20); do { [ -d /var/host-codex ] || [ -f /var/auth.json ]; } && break || sleep 0.5; done`
}

// ResetAndCreateDirsScript preserves $HOME/.codex and ensures auxiliary dirs exist.
func ResetAndCreateDirsScript(home string) string {
	h := home
	return `mkdir -p '` + h + `/.codex' '` + h + `/.codex/rollouts' '` + h + `/.cache' '` + h + `/.config' '` + h + `/.local'`
}

// CopyHostAuthScript copies the host auth.json into $HOME/.codex/auth.json when present.
func CopyHostAuthScript(home string) string {
	h := home
	return `if [ -r /var/host-codex/auth.json ]; then cp -f /var/host-codex/auth.json '` + h + `/.codex/auth.json'; fi`
}

// FallbackCopyAuthScript copies /var/auth.json into $HOME/.codex/auth.json if still missing.
func FallbackCopyAuthScript(home string) string {
	h := home
	return `if [ ! -f '` + h + `/.codex/auth.json' ] && [ -r /var/auth.json ]; then cp -f /var/auth.json '` + h + `/.codex/auth.json'; fi`
}

// TightenPermsScript chmods 600 on $HOME/.codex/auth.json if present.
func TightenPermsScript(home string) string {
	h := home
	return `if [ -f '` + h + `/.codex/auth.json' ]; then chmod 600 '` + h + `/.codex/auth.json'; fi`
}

// BuildSeedScripts returns a sequence of small bash scripts that, when run
// inside the agent sandbox (via `bash -lc`), refresh the per-agent Codex HOME
// from host mounts.
func BuildSeedScripts(home string) []string {
	return []string{
		WaitForHostMountsScript(),
		ResetAndCreateDirsScript(home),
		CopyHostAuthScript(home),
		FallbackCopyAuthScript(home),
		TightenPermsScript(home),
	}
}

// BuildForceReseedScripts returns a sequence of bash snippets that refresh
// $HOME/.codex auth from host mounts when available and recreate the seeded
// marker expected by anchor startup flows.
func BuildForceReseedScripts(home string) []string {
	h := home
	marker := h + "/.codex/.seeded"
	return []string{
		WaitForHostMountsScript(),
		`mkdir -p '` + h + `/.codex' '` + h + `/.codex/rollouts' '` + h + `/.cache' '` + h + `/.config' '` + h + `/.local'`,
		CopyHostAuthScript(home),
		FallbackCopyAuthScript(home),
		TightenPermsScript(home),
		`touch '` + marker + `'`,
	}
}

// AnchorConfig describes how to anchor HOME for an agent sandbox and optionally seed Codex.
type AnchorConfig struct {
	// Anchor is the symlink path exposed to tooling, e.g. /workspace/.devhome.
	Anchor string
	// Base is the directory holding per-agent homes, e.g. /workspace/.devhomes.
	Base string
	// SeedCodex indicates whether Codex credentials should be copied after relinking.
	SeedCodex bool
}

// BuildAnchorScripts returns bash snippets that (1) ensure the anchor symlink points at the
// agent-unique directory and (2) optionally seed Codex credentials beneath it. The seeding
// work operates directly on the resolved target (instead of the shared symlink) so multiple
// agents can run it concurrently without clobbering each other's state.
func BuildAnchorScripts(cfg AnchorConfig) []string {
	anchor := strings.TrimSpace(cfg.Anchor)
	base := strings.TrimSpace(cfg.Base)
	if anchor == "" || base == "" {
		return nil
	}
	parts := []string{
		"set -e",
		fmt.Sprintf("target=\"%s/$(hostname)\"", base),
		"mkdir -p \"$target/.ssh\" \"$target/.cache\" \"$target/.config\" \"$target/.local\"",
		"chmod 700 \"$target/.ssh\"",
		"dev_home_ok=0; if mkdir -p /home/dev 2>/dev/null; then dev_home_ok=1; elif [ -d /home/dev ]; then dev_home_ok=1; fi",
		fmt.Sprintf("ln -sfn \"$target\" %s", shQuote(anchor)),
		"mkdir -p \"$target/.sbt\"",
		"chmod -R a+rwX \"$target/.sbt\"",
		"if [ \"$dev_home_ok\" = 1 ]; then if [ -d /home/dev/.ivy2 ]; then ln -sfn /home/dev/.ivy2 \"$target/.ivy2\"; fi; fi",
		"if [ \"$dev_home_ok\" = 1 ]; then if [ -e /home/dev/.sbt ] || [ -L /home/dev/.sbt ]; then rm -rf /home/dev/.sbt; fi; ln -sfn \"$target/.sbt\" /home/dev/.sbt; fi",
		"if [ \"$dev_home_ok\" = 1 ]; then if [ -d /home/dev/.cache/coursier ]; then ln -sfn /home/dev/.cache/coursier \"$target/.cache/coursier\"; fi; fi",
		"if [ \"$dev_home_ok\" = 1 ] && [ -n \"${DOCKER_HOST:-}\" ]; then printf \"docker.host=%s\\n\" \"$DOCKER_HOST\" > \"$target/.testcontainers.properties\"; ln -sfn \"$target/.testcontainers.properties\" /home/dev/.testcontainers.properties; fi",
		"if [ -r /var/host-home/.p10k.zsh ]; then cp -f /var/host-home/.p10k.zsh \"$target/.p10k.zsh\"; sed -i 's/^\\([[:space:]]*typeset -g POWERLEVEL9K_INSTANT_PROMPT=\\).*/\\1off/' \"$target/.p10k.zsh\"; fi",
		"printf '%s\\n' " +
			"'export POWERLEVEL9K_DISABLE_CONFIGURATION_WIZARD=true' " +
			"'typeset -g POWERLEVEL9K_INSTANT_PROMPT=off' " +
			"'[[ -r /usr/local/share/powerlevel10k/powerlevel10k.zsh-theme ]] && source /usr/local/share/powerlevel10k/powerlevel10k.zsh-theme' " +
			"'[[ -r ~/.p10k.zsh ]] && source ~/.p10k.zsh' " +
			"'unalias codex 2>/dev/null || true' " +
			"'devkit_codex_tui_log_guard() {' " +
			"'  local log=\"$HOME/.codex/log/codex-tui.log\"' " +
			"'  local max=\"${DEVKIT_CODEX_TUI_LOG_MAX_BYTES:-268435456}\"' " +
			"'  [[ \"$max\" == <-> ]] || return 0' " +
			"'  (( max > 0 )) || return 0' " +
			"'  [[ -f \"$log\" ]] || return 0' " +
			"'  local size tmp' " +
			"'  size=$(wc -c < \"$log\" 2>/dev/null) || return 0' " +
			"'  (( size > max )) || return 0' " +
			"'  tmp=\"${log}.tmp.$$\"' " +
			"'  tail -c \"$max\" \"$log\" > \"$tmp\" 2>/dev/null && cat \"$tmp\" > \"$log\"' " +
			"'  rm -f \"$tmp\"' " +
			"'}' " +
			"'codex() {' " +
			"'  local -a extra' " +
			"'  extra=(' " +
			"'    -a never' " +
			"'    -s danger-full-access' " +
			"\"    -c 'mcp_servers.codex-cli.command=\\\"codex\\\"'\" " +
			"\"    -c 'mcp_servers.codex-cli.args=[\\\"mcp-server\\\"]'\" " +
			"'    -c '\\''mcp_servers.codex-cli.startup_timeout_sec=60'\\''' " +
			"\"    -c 'mcp_servers.governance.command=\\\"bash\\\"'\" " +
			"'    -c \"mcp_servers.governance.cwd=\\\"$PWD\\\"\"' " +
			"\"    -c 'mcp_servers.governance.args=[\\\"-lc\\\",\\\"" + governanceMCPEntrypointSeedScript() + "\\\"]'\" " +
			"'    -c '\\''mcp_servers.governance.startup_timeout_sec=60'\\''' " +
			"'    -c '\\''mcp_servers.governance.tool_timeout_sec=10800'\\''' " +
			"'    -c '\\''mcp_servers.governance.default_tools_approval_mode=\"auto\"'\\''' " +
			"'    -c '\\''mcp_servers.governance.enabled_tools=[\"run\",\"run_lint_migration\",\"submit_to_ci\",\"governance.workspace_topology\",\"governance.graph_status\",\"governance.search\",\"governance.write_yaml\",\"governance.operator_attention_opt_in\",\"governance.operator_attention_opt_out\",\"governance.operator_attention_status\",\"governance.operator_attention_inbox\",\"governance.operator_attention_record_blocker\"]'\\''' " +
			"'  )' " +
			"'  devkit_codex_tui_log_guard' " +
			"'  HOME=\"$HOME\" CODEX_HOME=\"$HOME/.codex\" CODEX_ROLLOUT_DIR=\"$HOME/.codex/rollouts\" command codex \"${extra[@]}\" \"$@\"' " +
			"'}' > \"$target/.zshrc\"",
	}
	if cfg.SeedCodex {
		seedSteps := []string{
			WaitForHostMountsScript(),
			"mkdir -p \"$target/.codex\" \"$target/.codex/rollouts\" \"$target/.cache\" \"$target/.config\" \"$target/.local\"",
			"if [ -r /var/host-codex/auth.json ]; then cp -f /var/host-codex/auth.json \"$target/.codex/auth.json\"; fi",
			"if [ ! -f \"$target/.codex/auth.json\" ] && [ -r /var/auth.json ]; then cp -f /var/auth.json \"$target/.codex/auth.json\"; fi",
			"if [ -f \"$target/.codex/auth.json\" ]; then chmod 600 \"$target/.codex/auth.json\"; fi",
			"touch \"$marker\"",
		}
		parts = append(parts,
			"marker=\"$target/.codex/.seeded\"",
			"if [ ! -f \"$marker\" ]; then "+strings.Join(seedSteps, "; ")+"; fi",
		)
	}
	return []string{strings.Join(parts, "; ")}
}

// BuildDirectHomeScripts returns bash snippets that prepare a concrete home path
// without using the shared anchor symlink used by older dev-all flows.
func BuildDirectHomeScripts(home string, seedCodex bool) []string {
	home = strings.TrimSpace(home)
	if home == "" {
		return nil
	}
	parts := []string{
		"set -e",
		fmt.Sprintf("home=%s", shQuote(home)),
		"mkdir -p \"$home/.ssh\" \"$home/.cache\" \"$home/.config\" \"$home/.local\"",
		"chmod 700 \"$home/.ssh\"",
		"dev_home_ok=0; if mkdir -p /home/dev 2>/dev/null; then dev_home_ok=1; elif [ -d /home/dev ]; then dev_home_ok=1; fi",
		"mkdir -p \"$home/.sbt\"",
		"chmod -R a+rwX \"$home/.sbt\"",
		"if [ \"$dev_home_ok\" = 1 ]; then if [ -d /home/dev/.ivy2 ]; then ln -sfn /home/dev/.ivy2 \"$home/.ivy2\"; fi; fi",
		"if [ \"$dev_home_ok\" = 1 ]; then if [ -e /home/dev/.sbt ] || [ -L /home/dev/.sbt ]; then rm -rf /home/dev/.sbt; fi; ln -sfn \"$home/.sbt\" /home/dev/.sbt; fi",
		"if [ \"$dev_home_ok\" = 1 ]; then if [ -d /home/dev/.cache/coursier ]; then ln -sfn /home/dev/.cache/coursier \"$home/.cache/coursier\"; fi; fi",
		"if [ \"$dev_home_ok\" = 1 ] && [ -n \"${DOCKER_HOST:-}\" ]; then printf \"docker.host=%s\\n\" \"$DOCKER_HOST\" > \"$home/.testcontainers.properties\"; ln -sfn \"$home/.testcontainers.properties\" /home/dev/.testcontainers.properties; fi",
		"if [ -r /var/host-home/.p10k.zsh ]; then cp -f /var/host-home/.p10k.zsh \"$home/.p10k.zsh\"; sed -i 's/^\\([[:space:]]*typeset -g POWERLEVEL9K_INSTANT_PROMPT=\\).*/\\1off/' \"$home/.p10k.zsh\"; fi",
		"printf '%s\\n' " +
			"'export POWERLEVEL9K_DISABLE_CONFIGURATION_WIZARD=true' " +
			"'typeset -g POWERLEVEL9K_INSTANT_PROMPT=off' " +
			"'[[ -r /usr/local/share/powerlevel10k/powerlevel10k.zsh-theme ]] && source /usr/local/share/powerlevel10k/powerlevel10k.zsh-theme' " +
			"'[[ -r ~/.p10k.zsh ]] && source ~/.p10k.zsh' " +
			"'unalias codex 2>/dev/null || true' " +
			"'devkit_codex_tui_log_guard() {' " +
			"'  local log=\"$HOME/.codex/log/codex-tui.log\"' " +
			"'  local max=\"${DEVKIT_CODEX_TUI_LOG_MAX_BYTES:-268435456}\"' " +
			"'  [[ \"$max\" == <-> ]] || return 0' " +
			"'  (( max > 0 )) || return 0' " +
			"'  [[ -f \"$log\" ]] || return 0' " +
			"'  local size tmp' " +
			"'  size=$(wc -c < \"$log\" 2>/dev/null) || return 0' " +
			"'  (( size > max )) || return 0' " +
			"'  tmp=\"${log}.tmp.$$\"' " +
			"'  tail -c \"$max\" \"$log\" > \"$tmp\" 2>/dev/null && cat \"$tmp\" > \"$log\"' " +
			"'  rm -f \"$tmp\"' " +
			"'}' " +
			"'codex() {' " +
			"'  local -a extra' " +
			"'  extra=(' " +
			"'    -a never' " +
			"'    -s danger-full-access' " +
			"\"    -c 'mcp_servers.codex-cli.command=\\\"codex\\\"'\" " +
			"\"    -c 'mcp_servers.codex-cli.args=[\\\"mcp-server\\\"]'\" " +
			"'    -c '\\''mcp_servers.codex-cli.startup_timeout_sec=60'\\''' " +
			"\"    -c 'mcp_servers.governance.command=\\\"bash\\\"'\" " +
			"'    -c \"mcp_servers.governance.cwd=\\\"$PWD\\\"\"' " +
			"\"    -c 'mcp_servers.governance.args=[\\\"-lc\\\",\\\"" + governanceMCPEntrypointSeedScript() + "\\\"]'\" " +
			"'    -c '\\''mcp_servers.governance.startup_timeout_sec=60'\\''' " +
			"'    -c '\\''mcp_servers.governance.tool_timeout_sec=10800'\\''' " +
			"'    -c '\\''mcp_servers.governance.default_tools_approval_mode=\"auto\"'\\''' " +
			"'    -c '\\''mcp_servers.governance.enabled_tools=[\"run\",\"run_lint_migration\",\"submit_to_ci\",\"governance.workspace_topology\",\"governance.graph_status\",\"governance.search\",\"governance.write_yaml\",\"governance.operator_attention_opt_in\",\"governance.operator_attention_opt_out\",\"governance.operator_attention_status\",\"governance.operator_attention_inbox\",\"governance.operator_attention_record_blocker\"]'\\''' " +
			"'  )' " +
			"'  devkit_codex_tui_log_guard' " +
			"'  HOME=\"$HOME\" CODEX_HOME=\"$HOME/.codex\" CODEX_ROLLOUT_DIR=\"$HOME/.codex/rollouts\" command codex \"${extra[@]}\" \"$@\"' " +
			"'}' > \"$home/.zshrc\"",
	}
	if seedCodex {
		marker := "$home/.codex/.seeded"
		seedSteps := []string{
			WaitForHostMountsScript(),
			"mkdir -p \"$home/.codex\" \"$home/.codex/rollouts\" \"$home/.cache\" \"$home/.config\" \"$home/.local\"",
			"if [ -r /var/host-codex/auth.json ]; then cp -f /var/host-codex/auth.json \"$home/.codex/auth.json\"; fi",
			"if [ ! -f \"$home/.codex/auth.json\" ] && [ -r /var/auth.json ]; then cp -f /var/auth.json \"$home/.codex/auth.json\"; fi",
			"if [ -f \"$home/.codex/auth.json\" ]; then chmod 600 \"$home/.codex/auth.json\"; fi",
			"touch " + marker,
		}
		parts = append(parts,
			"marker="+marker,
			"if [ ! -f \"$marker\" ]; then "+strings.Join(seedSteps, "; ")+"; fi",
		)
	}
	return []string{strings.Join(parts, "; ")}
}

func governanceMCPEntrypointSeedScript() string {
	return strings.Join([]string{
		"export PATH=/run/current-system/sw/bin:/run/wrappers/bin:/home/bayesartre/.nix-profile/bin:/etc/profiles/per-user/bayesartre/bin:\\${PATH:-}",
		"unset SUBAGENT_GOVERNANCE_CONTROL_PLANE_AUTOWARM",
		"governance_env=",
		"governance_root=",
		"case \\${PWD:-} in /workspaces/dev) governance_env=/workspaces/dev/.devkit/ouro8-governance-env.sh; governance_root=/workspaces/dev ;; */agent-worktrees/*/ouroboros-ide) governance_env=\\${PWD%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh; governance_root=\\${PWD} ;; */agent-worktrees/*/*) governance_env=\\${PWD%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh; governance_root=\\${PWD} ;; */.devkit/gui-worktree-aliases/*/ouroboros-ide) governance_env=\\${PWD%%/.devkit/gui-worktree-aliases/*}/.devkit/ouro8-governance-env.sh; governance_root=\\${PWD} ;; /workspaces/dev/ouroboros-ide) governance_env=/workspaces/dev/.devkit/ouro8-governance-env.sh; governance_root=\\${PWD} ;; /workspaces/dev/*) governance_env=/workspaces/dev/.devkit/ouro8-governance-env.sh; governance_root=/workspaces/dev/ouroboros-ide ;; */ouroboros-ide) governance_env=\\${PWD%/ouroboros-ide}/.devkit/ouro8-governance-env.sh; governance_root=\\${PWD} ;; esac",
		"if [[ -z \\${governance_root} && -n \\${CODEX_HOME:-} ]]; then case \\${CODEX_HOME} in */agent-worktrees/*/ouroboros-ide/.devhome-agent*/.codex) governance_root=\\${CODEX_HOME%/.devhome-agent*/.codex}; governance_env=\\${governance_root%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh ;; */agent-worktrees/*/.devhome-agent*/.codex) governance_agent_dir=\\${CODEX_HOME%/.devhome-agent*/.codex}; governance_root=\\${governance_agent_dir}/ouroboros-ide; governance_env=\\${governance_agent_dir%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh ;; */ouroboros-ide/.codex) governance_root=\\${CODEX_HOME%/.codex}; governance_env=\\${governance_root%/ouroboros-ide}/.devkit/ouro8-governance-env.sh ;; esac; fi",
		"if [[ -z \\${governance_env} ]]; then echo required governance env missing: unable to derive path for PWD=\\${PWD:-} >&2; exit 1; fi",
		"if [[ ! -r \\${governance_env} ]]; then echo required governance env missing: \\${governance_env} >&2; exit 1; fi",
		"if [[ -z \\${governance_root} ]]; then echo required governance root missing: unable to derive path for PWD=\\${PWD:-} >&2; exit 1; fi",
		"if [[ ! -x \\${governance_root}/scripts/devops/governance-mcp-stdio-forward ]]; then echo required governance bridge missing: \\${governance_root}/scripts/devops/governance-mcp-stdio-forward >&2; exit 1; fi",
		"echo using governance env: \\${governance_env} >&2",
		"echo using governance root: \\${governance_root} >&2",
		"source \\${governance_env}",
		"if [[ -z \\${SUBAGENT_GOVERNANCE_WORKSPACE_ID:-} ]]; then IFS=, read -ra governance_pairs <<< \\${SUBAGENT_GOVERNANCE_WORKSPACE_ROOTS:-}; for governance_pair in \\${governance_pairs[@]}; do governance_pair_id=\\${governance_pair%%=*}; governance_pair_root=\\${governance_pair#*=}; if [[ \\${governance_pair_root} == \\${governance_root} ]]; then export SUBAGENT_GOVERNANCE_WORKSPACE_ID=\\${governance_pair_id}; break; fi; done; fi",
		"if [[ -z \\${SUBAGENT_GOVERNANCE_WORKSPACE_ID:-} ]]; then case \\${PWD:-} in /workspaces/dev) export SUBAGENT_GOVERNANCE_WORKSPACE_ID=dev-workspace ;; */agent-worktrees/*/ouroboros-ide) workspace_tail=\\${PWD#*/agent-worktrees/}; export SUBAGENT_GOVERNANCE_WORKSPACE_ID=\\${workspace_tail%%/*} ;; esac; fi",
		"if [[ -z \\${SUBAGENT_GOVERNANCE_WORKSPACE_ID:-} ]]; then case \\${governance_root} in */ouroboros-ide) export SUBAGENT_GOVERNANCE_WORKSPACE_ID=ouroboros-ide ;; /workspaces/dev) export SUBAGENT_GOVERNANCE_WORKSPACE_ID=dev-workspace ;; esac; fi",
		"exec bash \\${governance_root}/scripts/devops/governance-mcp-stdio-forward",
	}, "; ")
}

// shQuote provides the minimal quoting needed for simple POSIX-safe paths.
func shQuote(path string) string {
	if !strings.ContainsAny(path, " '\"$") {
		return fmt.Sprintf("\"%s\"", path)
	}
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "$", "\\$", "`", "\\`")
	return "\"" + replacer.Replace(path) + "\""
}

// JoinScripts joins bash snippets with a " ; " delimiter, trimming whitespace.
func JoinScripts(scripts []string) string {
	parts := make([]string, 0, len(scripts))
	for _, sc := range scripts {
		s := strings.TrimSpace(sc)
		if s == "" {
			continue
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "; ")
}

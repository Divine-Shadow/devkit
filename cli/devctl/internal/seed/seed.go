package seed

import (
	"fmt"
	"strings"

	"devkit/cli/devctl/internal/governanceentrypoint"
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

// CopyHostConfigScript copies the host config.toml into $HOME/.codex/config.toml when present.
func CopyHostConfigScript(home string) string {
	h := home
	return `if [ -r /var/host-codex/config.toml ]; then cp -f /var/host-codex/config.toml '` + h + `/.codex/config.toml'; fi`
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

// TightenConfigPermsScript chmods 600 on $HOME/.codex/config.toml if present.
func TightenConfigPermsScript(home string) string {
	h := home
	return `if [ -f '` + h + `/.codex/config.toml' ]; then chmod 600 '` + h + `/.codex/config.toml'; fi`
}

// BuildSeedScripts returns a sequence of small bash scripts that, when run
// inside the agent sandbox (via `bash -lc`), refresh the per-agent Codex HOME
// from host mounts.
func BuildSeedScripts(home string) []string {
	return []string{
		WaitForHostMountsScript(),
		ResetAndCreateDirsScript(home),
		CopyHostAuthScript(home),
		CopyHostConfigScript(home),
		FallbackCopyAuthScript(home),
		TightenPermsScript(home),
		TightenConfigPermsScript(home),
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
		CopyHostConfigScript(home),
		FallbackCopyAuthScript(home),
		TightenPermsScript(home),
		TightenConfigPermsScript(home),
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
			"'export DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256=\"" + governanceentrypoint.SHA256() + "\"' " +
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
			"'devkit_codex_require_config() {' " +
			"'  local config=\"${CODEX_HOME:-$HOME/.codex}/config.toml\"' " +
			"'  if [[ ! -r \"$config\" ]]; then' " +
			"'    echo \"[devkit-codex] required Nix-authored Codex config missing: $config\" >&2' " +
			"'    return 1' " +
			"'  fi' " +
			"'  grep -Fqx '\\''# source = nixos-wsl codex config'\\'' \"$config\" || {' " +
			"'    echo \"[devkit-codex] Codex config is not Nix-authored: $config\" >&2' " +
			"'    return 1' " +
			"'  }' " +
			"'  grep -Fqx '\\''model_provider = \"openai\"'\\'' \"$config\" || {' " +
			"'    echo \"[devkit-codex] Codex config must use model_provider = \\\"openai\\\": $config\" >&2' " +
			"'    return 1' " +
			"'  }' " +
			"'  grep -Fqx '\\''[profiles.openai]'\\'' \"$config\" || {' " +
			"'    echo \"[devkit-codex] Codex config missing [profiles.openai]: $config\" >&2' " +
			"'    return 1' " +
			"'  }' " +
			"'}' " +
			"'codex() {' " +
			"'  devkit_codex_tui_log_guard' " +
			"'  devkit_codex_require_config || return' " +
			"'  HOME=\"$HOME\" CODEX_HOME=\"$HOME/.codex\" CODEX_ROLLOUT_DIR=\"$HOME/.codex/rollouts\" command codex \"$@\"' " +
			"'}' > \"$target/.zshrc\"",
	}
	if cfg.SeedCodex {
		seedSteps := []string{
			WaitForHostMountsScript(),
			"mkdir -p \"$target/.codex\" \"$target/.codex/rollouts\" \"$target/.cache\" \"$target/.config\" \"$target/.local\"",
			"if [ -r /var/host-codex/auth.json ]; then cp -f /var/host-codex/auth.json \"$target/.codex/auth.json\"; fi",
			"if [ -r /var/host-codex/config.toml ]; then cp -f /var/host-codex/config.toml \"$target/.codex/config.toml\"; fi",
			"if [ ! -f \"$target/.codex/auth.json\" ] && [ -r /var/auth.json ]; then cp -f /var/auth.json \"$target/.codex/auth.json\"; fi",
			"if [ -f \"$target/.codex/auth.json\" ]; then chmod 600 \"$target/.codex/auth.json\"; fi",
			"if [ -f \"$target/.codex/config.toml\" ]; then chmod 600 \"$target/.codex/config.toml\"; fi",
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
			"'export DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256=\"" + governanceentrypoint.SHA256() + "\"' " +
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
			"'devkit_codex_require_config() {' " +
			"'  local config=\"${CODEX_HOME:-$HOME/.codex}/config.toml\"' " +
			"'  if [[ ! -r \"$config\" ]]; then' " +
			"'    echo \"[devkit-codex] required Nix-authored Codex config missing: $config\" >&2' " +
			"'    return 1' " +
			"'  fi' " +
			"'  grep -Fqx '\\''# source = nixos-wsl codex config'\\'' \"$config\" || {' " +
			"'    echo \"[devkit-codex] Codex config is not Nix-authored: $config\" >&2' " +
			"'    return 1' " +
			"'  }' " +
			"'  grep -Fqx '\\''model_provider = \"openai\"'\\'' \"$config\" || {' " +
			"'    echo \"[devkit-codex] Codex config must use model_provider = \\\"openai\\\": $config\" >&2' " +
			"'    return 1' " +
			"'  }' " +
			"'  grep -Fqx '\\''[profiles.openai]'\\'' \"$config\" || {' " +
			"'    echo \"[devkit-codex] Codex config missing [profiles.openai]: $config\" >&2' " +
			"'    return 1' " +
			"'  }' " +
			"'}' " +
			"'codex() {' " +
			"'  devkit_codex_tui_log_guard' " +
			"'  devkit_codex_require_config || return' " +
			"'  HOME=\"$HOME\" CODEX_HOME=\"$HOME/.codex\" CODEX_ROLLOUT_DIR=\"$HOME/.codex/rollouts\" command codex \"$@\"' " +
			"'}' > \"$home/.zshrc\"",
	}
	if seedCodex {
		marker := "$home/.codex/.seeded"
		seedSteps := []string{
			WaitForHostMountsScript(),
			"mkdir -p \"$home/.codex\" \"$home/.codex/rollouts\" \"$home/.cache\" \"$home/.config\" \"$home/.local\"",
			"if [ -r /var/host-codex/auth.json ]; then cp -f /var/host-codex/auth.json \"$home/.codex/auth.json\"; fi",
			"if [ -r /var/host-codex/config.toml ]; then cp -f /var/host-codex/config.toml \"$home/.codex/config.toml\"; fi",
			"if [ ! -f \"$home/.codex/auth.json\" ] && [ -r /var/auth.json ]; then cp -f /var/auth.json \"$home/.codex/auth.json\"; fi",
			"if [ -f \"$home/.codex/auth.json\" ]; then chmod 600 \"$home/.codex/auth.json\"; fi",
			"if [ -f \"$home/.codex/config.toml\" ]; then chmod 600 \"$home/.codex/config.toml\"; fi",
			"touch " + marker,
		}
		parts = append(parts,
			"marker="+marker,
			"if [ ! -f \"$marker\" ]; then "+strings.Join(seedSteps, "; ")+"; fi",
		)
	}
	return []string{strings.Join(parts, "; ")}
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

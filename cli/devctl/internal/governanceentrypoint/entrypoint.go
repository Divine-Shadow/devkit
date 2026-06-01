package governanceentrypoint

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Body returns the canonical governance MCP stdio entrypoint body used by
// generated Codex config and shell wrappers. Keep runtime launch and seed paths
// calling this package instead of copying the shell snippet into multiple
// packages.
func Body() string {
	return strings.Join([]string{
		"export PATH=/run/current-system/sw/bin:/run/wrappers/bin:/home/bayesartre/.nix-profile/bin:/etc/profiles/per-user/bayesartre/bin:${PATH:-}",
		"unset SUBAGENT_GOVERNANCE_CONTROL_PLANE_AUTOWARM",
		"governance_env=",
		"governance_root=",
		"case ${PWD:-} in /workspaces/dev) governance_env=/workspaces/dev/.devkit/ouro8-governance-env.sh; governance_root=/workspaces/dev ;; */agent-worktrees/*/ouroboros-ide) governance_env=${PWD%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh; governance_root=${PWD} ;; */agent-worktrees/*/*) governance_env=${PWD%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh; governance_root=${PWD} ;; */.devkit/gui-worktree-aliases/*/ouroboros-ide) governance_env=${PWD%%/.devkit/gui-worktree-aliases/*}/.devkit/ouro8-governance-env.sh; governance_root=${PWD} ;; /workspaces/dev/ouroboros-ide) governance_env=/workspaces/dev/.devkit/ouro8-governance-env.sh; governance_root=${PWD} ;; /workspaces/dev/*) governance_env=/workspaces/dev/.devkit/ouro8-governance-env.sh; governance_root=/workspaces/dev/ouroboros-ide ;; */ouroboros-ide) governance_env=${PWD%/ouroboros-ide}/.devkit/ouro8-governance-env.sh; governance_root=${PWD} ;; esac",
		"if [[ -z ${governance_root} && -n ${CODEX_HOME:-} ]]; then case ${CODEX_HOME} in */agent-worktrees/*/ouroboros-ide/.devhome-agent*/.codex) governance_root=${CODEX_HOME%/.devhome-agent*/.codex}; governance_env=${governance_root%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh ;; */agent-worktrees/*/.devhome-agent*/.codex) governance_agent_dir=${CODEX_HOME%/.devhome-agent*/.codex}; governance_root=${governance_agent_dir}/ouroboros-ide; governance_env=${governance_agent_dir%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh ;; */ouroboros-ide/.codex) governance_root=${CODEX_HOME%/.codex}; governance_env=${governance_root%/ouroboros-ide}/.devkit/ouro8-governance-env.sh ;; esac; fi",
		"if [[ -z ${governance_env} ]]; then echo required governance env missing: unable to derive path for PWD=${PWD:-} >&2; exit 1; fi",
		"if [[ ! -r ${governance_env} ]]; then echo required governance env missing: ${governance_env} >&2; exit 1; fi",
		"if [[ -z ${governance_root} ]]; then echo required governance root missing: unable to derive path for PWD=${PWD:-} >&2; exit 1; fi",
		"if [[ ! -x ${governance_root}/scripts/devops/governance-mcp-stdio-forward ]]; then echo required governance bridge missing: ${governance_root}/scripts/devops/governance-mcp-stdio-forward >&2; exit 1; fi",
		"echo using governance env: ${governance_env} >&2",
		"echo using governance root: ${governance_root} >&2",
		"source ${governance_env}",
		"if [[ -z ${SUBAGENT_GOVERNANCE_WORKSPACE_ID:-} ]]; then IFS=, read -ra governance_pairs <<< ${SUBAGENT_GOVERNANCE_WORKSPACE_ROOTS:-}; for governance_pair in ${governance_pairs[@]}; do governance_pair_id=${governance_pair%%=*}; governance_pair_root=${governance_pair#*=}; if [[ ${governance_pair_root} == ${governance_root} ]]; then export SUBAGENT_GOVERNANCE_WORKSPACE_ID=${governance_pair_id}; break; fi; done; fi",
		"if [[ -z ${SUBAGENT_GOVERNANCE_WORKSPACE_ID:-} ]]; then case ${PWD:-} in /workspaces/dev) export SUBAGENT_GOVERNANCE_WORKSPACE_ID=dev-workspace ;; */agent-worktrees/*/ouroboros-ide) workspace_tail=${PWD#*/agent-worktrees/}; export SUBAGENT_GOVERNANCE_WORKSPACE_ID=${workspace_tail%%/*} ;; esac; fi",
		"if [[ -z ${SUBAGENT_GOVERNANCE_WORKSPACE_ID:-} ]]; then case ${governance_root} in */ouroboros-ide) export SUBAGENT_GOVERNANCE_WORKSPACE_ID=ouroboros-ide ;; /workspaces/dev) export SUBAGENT_GOVERNANCE_WORKSPACE_ID=dev-workspace ;; esac; fi",
		"exec bash ${governance_root}/scripts/devops/governance-mcp-stdio-forward",
	}, "; ")
}

// SHA256 fingerprints the canonical entrypoint body so generated config,
// wrappers, and runtime env can prove which entrypoint contract they carry.
func SHA256() string {
	sum := sha256.Sum256([]byte(Body()))
	return fmt.Sprintf("%x", sum)
}

// Zsh returns the canonical governance MCP stdio entrypoint with an exported
// provenance marker for runtime subprocesses.
func Zsh() string {
	return "export DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256=" + SHA256() + "; " + Body()
}

// EscapedForNestedDoubleQuotes returns the same entrypoint with braced shell
// expansions protected for seed scripts that write another shell script through
// a double-quoted command string.
func EscapedForNestedDoubleQuotes() string {
	return strings.ReplaceAll(Zsh(), "${", "\\${")
}

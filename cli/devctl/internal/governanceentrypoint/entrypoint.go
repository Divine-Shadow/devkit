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
	return body("")
}

// BodyForRuntimeBundle returns the canonical entrypoint with runtime identity
// applied by an exact immutable bundle launcher after mutable workspace routing
// has been loaded.
func BodyForRuntimeBundle(runtimeBundlePath string) string {
	runtimeBundlePath = strings.TrimSpace(runtimeBundlePath)
	if runtimeBundlePath == "" {
		return ""
	}
	return body(runtimeBundlePath)
}

func body(runtimeBundlePath string) string {
	finalCommand := "exec bash ${governance_root}/scripts/devops/governance-mcp-stdio-forward"
	if runtimeBundlePath != "" {
		launcher := shellSingleQuote(strings.TrimRight(runtimeBundlePath, "/") + "/bin/dev-all-runtime-bundle")
		finalCommand = "exec " + launcher + " governance-forward ${governance_root}/scripts/devops/governance-mcp-stdio-forward"
	}
	return strings.Join([]string{
		"export PATH=/run/current-system/sw/bin:/run/wrappers/bin:${PATH:-}",
		"unset SUBAGENT_GOVERNANCE_CONTROL_PLANE_AUTOWARM",
		"governance_env=",
		"governance_root=",
		"governance_workspace_root=",
		"governance_entrypoint_sha=${DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256:-}",
		`governance_normalize_canonical_pwd() { local host_home host_dev normalized_pwd; unset OLDPWD; host_home=$(printf '\057home\057bayesartre'); host_dev=${host_home}/dev; case ${PWD:-} in ${host_dev}|${host_dev}/*) normalized_pwd=/workspaces/dev${PWD#${host_dev}}; if [[ ! -d ${normalized_pwd} ]]; then echo required governance workspace path missing after cwd normalization: ${normalized_pwd} >&2; exit 1; fi; cd ${normalized_pwd}; export PWD=${normalized_pwd} ;; esac; }`,
		"governance_normalize_canonical_pwd",
		`governance_sanitize_runtime_env() { local host_home host_dev host_sbt host_npm sbt_home path_part sanitized_path governance_var governance_value; host_home=$(printf '\057home\057bayesartre'); host_dev=${host_home}/dev; host_sbt=${host_home}/.sbt; host_npm=${host_home}/.npm-global; export DEVKIT_WORKTREE_ROOT=/workspaces/dev/agent-worktrees; export COURSIER_CACHE=/workspaces/dev/.cache/shared/coursier; export WSL_NIX_CONFIG_ROOT=/workspaces/dev/wsl-nix; if [[ -n ${SBT_OPTS:-} ]]; then sbt_home=${HOME:-/tmp}/.sbt; SBT_OPTS=${SBT_OPTS//${host_dev}/\/workspaces\/dev}; SBT_OPTS=${SBT_OPTS//${host_sbt}/${sbt_home}}; export SBT_OPTS; fi; if [[ -n ${DOCKER_HOST:-} ]]; then DOCKER_HOST=${DOCKER_HOST//${host_dev}/\/workspaces\/dev}; export DOCKER_HOST; fi; if [[ -n ${NIX_LDFLAGS:-} ]]; then NIX_LDFLAGS=${NIX_LDFLAGS//${host_dev}/\/workspaces\/dev}; export NIX_LDFLAGS; fi; if [[ -n ${out:-} ]]; then out=${out//${host_dev}/\/workspaces\/dev}; export out; fi; if [[ -n ${PATH:-} ]]; then sanitized_path=; IFS=: read -ra governance_path_parts <<< ${PATH}; for path_part in ${governance_path_parts[@]}; do case ${path_part} in ${host_dev}*|${host_sbt}*|${host_npm}*) continue ;; esac; sanitized_path=${sanitized_path:+${sanitized_path}:}${path_part}; done; export PATH=${sanitized_path}; fi; for governance_var in $(compgen -e); do governance_value=${!governance_var}; case ${governance_value} in *${host_dev}*|*${host_sbt}*|*${host_npm}*) echo required governance env contains host-local path in ${governance_var} >&2; exit 1 ;; esac; done; }`,
		"case ${PWD:-} in /workspaces/dev) governance_env=/workspaces/dev/.devkit/ouro8-governance-env.sh; governance_root=/workspaces/dev/ouroboros-ide; governance_workspace_root=/workspaces/dev ;; */agent-worktrees/*/ouroboros-ide) governance_env=${PWD%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh; governance_root=${PWD%%/agent-worktrees/*}/ouroboros-ide; governance_workspace_root=${PWD} ;; */agent-worktrees/*/ouroboros-terraform) governance_env=${PWD%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh; governance_root=${PWD%%/agent-worktrees/*}/ouroboros-ide; governance_workspace_root=${PWD} ;; */agent-worktrees/*/*) governance_env=${PWD%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh; governance_root=${PWD%%/agent-worktrees/*}/ouroboros-ide; governance_workspace_root=${PWD} ;; */.devkit/gui-worktree-aliases/*/ouroboros-ide) governance_env=${PWD%%/.devkit/gui-worktree-aliases/*}/.devkit/ouro8-governance-env.sh; governance_root=${PWD%%/.devkit/gui-worktree-aliases/*}/ouroboros-ide; governance_workspace_root=${PWD} ;; /workspaces/dev/ouroboros-ide) governance_env=/workspaces/dev/.devkit/ouro8-governance-env.sh; governance_root=${PWD}; governance_workspace_root=${PWD} ;; /workspaces/dev/ouroboros-terraform) governance_env=/workspaces/dev/.devkit/ouro8-governance-env.sh; governance_root=/workspaces/dev/ouroboros-ide; governance_workspace_root=${PWD} ;; /workspaces/dev/*) governance_env=/workspaces/dev/.devkit/ouro8-governance-env.sh; governance_root=/workspaces/dev/ouroboros-ide; governance_workspace_root=${PWD} ;; */ouroboros-ide) governance_env=${PWD%/ouroboros-ide}/.devkit/ouro8-governance-env.sh; governance_root=${PWD}; governance_workspace_root=${PWD} ;; */ouroboros-terraform) governance_env=${PWD%/ouroboros-terraform}/.devkit/ouro8-governance-env.sh; governance_root=${PWD%/ouroboros-terraform}/ouroboros-ide; governance_workspace_root=${PWD} ;; esac",
		"if [[ -z ${governance_root} && -n ${CODEX_HOME:-} ]]; then case ${CODEX_HOME} in */agent-worktrees/*/ouroboros-ide/.devhome-agent*/.codex) governance_workspace_root=${CODEX_HOME%/.devhome-agent*/.codex}; governance_root=${governance_workspace_root%%/agent-worktrees/*}/ouroboros-ide; governance_env=${governance_workspace_root%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh ;; */agent-worktrees/*/.devhome-agent*/.codex) governance_agent_dir=${CODEX_HOME%/.devhome-agent*/.codex}; governance_workspace_root=${governance_agent_dir}/ouroboros-ide; governance_root=${governance_agent_dir%%/agent-worktrees/*}/ouroboros-ide; governance_env=${governance_agent_dir%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh ;; */ouroboros-ide/.devhome-agent*/.codex) governance_root=${CODEX_HOME%/.devhome-agent*/.codex}; governance_workspace_root=${governance_root}; governance_env=${governance_root%/ouroboros-ide}/.devkit/ouro8-governance-env.sh ;; */ouroboros-terraform/.devhome-agent*/.codex) governance_workspace_root=${CODEX_HOME%/.devhome-agent*/.codex}; governance_root=${governance_workspace_root%/ouroboros-terraform}/ouroboros-ide; governance_env=${governance_workspace_root%/ouroboros-terraform}/.devkit/ouro8-governance-env.sh ;; */ouroboros-ide/.codex) governance_root=${CODEX_HOME%/.codex}; governance_workspace_root=${governance_root}; governance_env=${governance_root%/ouroboros-ide}/.devkit/ouro8-governance-env.sh ;; esac; fi",
		"if [[ -z ${governance_env} ]]; then echo required governance env missing: unable to derive path for PWD=${PWD:-} >&2; exit 1; fi",
		"if [[ ! -r ${governance_env} ]]; then echo required governance env missing: ${governance_env} >&2; exit 1; fi",
		"if [[ -z ${governance_root} ]]; then echo required governance root missing: unable to derive path for PWD=${PWD:-} >&2; exit 1; fi",
		"if [[ ! -x ${governance_root}/scripts/devops/governance-mcp-stdio-forward ]]; then echo required governance bridge missing: ${governance_root}/scripts/devops/governance-mcp-stdio-forward >&2; exit 1; fi",
		"echo using governance env: ${governance_env} >&2",
		"echo using governance root: ${governance_root} >&2",
		"source ${governance_env}",
		"if [[ -z ${governance_entrypoint_sha} ]]; then echo required governance MCP entrypoint fingerprint missing before env load >&2; exit 1; fi",
		"export DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256=${governance_entrypoint_sha}",
		"export DEVKIT_GOVERNANCE_EXPECTED_MCP_ENTRYPOINT_SHA256=${governance_entrypoint_sha}",
		"governance_sanitize_runtime_env",
		"if [[ -z ${SUBAGENT_GOVERNANCE_WORKSPACE_ID:-} ]]; then governance_match_root=${governance_workspace_root:-${governance_root}}; IFS=, read -ra governance_pairs <<< ${SUBAGENT_GOVERNANCE_WORKSPACE_ROOTS:-}; for governance_pair in ${governance_pairs[@]}; do governance_pair_id=${governance_pair%%=*}; governance_pair_root=${governance_pair#*=}; if [[ ${governance_pair_root} == ${governance_match_root} ]]; then export SUBAGENT_GOVERNANCE_WORKSPACE_ID=${governance_pair_id}; break; fi; done; fi",
		"if [[ -z ${SUBAGENT_GOVERNANCE_WORKSPACE_ID:-} ]]; then case ${governance_workspace_root:-${PWD:-}} in /workspaces/dev) export SUBAGENT_GOVERNANCE_WORKSPACE_ID=dev-workspace ;; /workspaces/dev/ouroboros-terraform) export SUBAGENT_GOVERNANCE_WORKSPACE_ID=ouroboros-terraform ;; */agent-worktrees/*/ouroboros-ide) workspace_tail=${governance_workspace_root#*/agent-worktrees/}; export SUBAGENT_GOVERNANCE_WORKSPACE_ID=${workspace_tail%%/*} ;; esac; fi",
		"if [[ -z ${SUBAGENT_GOVERNANCE_WORKSPACE_ID:-} ]]; then case ${governance_root} in */ouroboros-ide) export SUBAGENT_GOVERNANCE_WORKSPACE_ID=ouroboros-ide ;; /workspaces/dev) export SUBAGENT_GOVERNANCE_WORKSPACE_ID=dev-workspace ;; esac; fi",
		finalCommand,
	}, "; ")
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// SHA256 fingerprints the canonical entrypoint body so generated config,
// wrappers, and runtime env can prove which entrypoint contract they carry.
func SHA256() string {
	sum := sha256.Sum256([]byte(Body()))
	return fmt.Sprintf("%x", sum)
}

// SHA256ForRuntimeBundle fingerprints the entrypoint including its immutable
// runtime-bundle selection.
func SHA256ForRuntimeBundle(runtimeBundlePath string) string {
	sum := sha256.Sum256([]byte(BodyForRuntimeBundle(runtimeBundlePath)))
	return fmt.Sprintf("%x", sum)
}

// Zsh returns the canonical governance MCP stdio entrypoint with an exported
// provenance marker for runtime subprocesses.
func Zsh() string {
	return "export DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256=" + SHA256() + "; " + Body()
}

// ZshForRuntimeBundle returns the immutable-bundle entrypoint with its exact
// provenance fingerprint exported before mutable workspace routing is loaded.
func ZshForRuntimeBundle(runtimeBundlePath string) string {
	return "export DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256=" + SHA256ForRuntimeBundle(runtimeBundlePath) + "; " + BodyForRuntimeBundle(runtimeBundlePath)
}

// EscapedForNestedDoubleQuotes returns the same entrypoint with braced shell
// expansions protected for seed scripts that write another shell script through
// a double-quoted command string.
func EscapedForNestedDoubleQuotes() string {
	return strings.ReplaceAll(Zsh(), "${", "\\${")
}

package governanceentrypoint

import (
	"strings"
	"testing"
)

func TestBodyPreservesVerifiedGovernanceJarIdentity(t *testing.T) {
	body := Body()
	for _, want := range []string{
		"governance_normalize_canonical_pwd",
		"normalized_pwd=/workspaces/dev${PWD#${host_dev}}",
		"unset OLDPWD",
		"required governance workspace path missing after cwd normalization",
		"source ${governance_env}",
		"governance_normalize_canonical_pwd",
		"normalized_pwd=/workspaces/dev${PWD#${host_dev}}",
		"unset OLDPWD",
		"required governance workspace path missing after cwd normalization",
		"governance_sanitize_runtime_env",
		"NIX_LDFLAGS=${NIX_LDFLAGS//${host_dev}/\\/workspaces\\/dev}",
		"out=${out//${host_dev}/\\/workspaces\\/dev}",
		"*/agent-worktrees/*/ouroboros-ide) governance_env=${PWD%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh; governance_root=${PWD%%/agent-worktrees/*}/ouroboros-ide; governance_workspace_root=${PWD}",
		"*/agent-worktrees/*/.devhome-agent*/.codex) governance_agent_dir=${CODEX_HOME%/.devhome-agent*/.codex}; governance_workspace_root=${governance_agent_dir}/ouroboros-ide; governance_root=${governance_agent_dir%%/agent-worktrees/*}/ouroboros-ide",
		"exec bash ${governance_root}/scripts/devops/governance-mcp-stdio-forward",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("governance entrypoint missing %q:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{
		"unset SUBAGENT_GOVERNANCE_LATEST_JAR_PATH",
		"unset SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR",
		"unset DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH",
		"unset DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256",
		"unset SUBAGENT_GOVERNANCE_EXPECTED_JAR_SHA256",
		"*/agent-worktrees/*/ouroboros-ide) governance_env=${PWD%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh; governance_root=${PWD}; governance_workspace_root=${PWD}",
		"if [[ -x ${PWD}/scripts/devops/governance-mcp-stdio-forward ]]; then governance_root=${PWD}",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("governance entrypoint must preserve verified prewarmed jar identity %q:\n%s", forbidden, body)
		}
	}
}

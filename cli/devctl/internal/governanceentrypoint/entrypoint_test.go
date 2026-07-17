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
		"governance_entrypoint_sha=${DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256:-}",
		"required governance MCP entrypoint fingerprint missing before env load",
		"export DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256=${governance_entrypoint_sha}",
		"export DEVKIT_GOVERNANCE_EXPECTED_MCP_ENTRYPOINT_SHA256=${governance_entrypoint_sha}",
		"governance_normalize_canonical_pwd",
		"normalized_pwd=/workspaces/dev${PWD#${host_dev}}",
		"unset OLDPWD",
		"required governance workspace path missing after cwd normalization",
		"governance_sanitize_runtime_env",
		"case ${governance_var} in SUBAGENT_GOVERNANCE_HOST_DEV_ROOT_ALIAS) continue ;; esac",
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

func TestRuntimeBundleEntrypointAppliesExactImmutableAuthorityAfterRouting(t *testing.T) {
	bundle := "/nix/store/ffffffffffffffffffffffffffffffff-dev-all-runtime-bundle"
	body := BodyForRuntimeBundle(bundle)
	launcher := "exec '" + bundle + "/bin/dev-all-runtime-bundle' governance-forward ${governance_root}/scripts/devops/governance-mcp-stdio-forward"
	if !strings.Contains(body, "source ${governance_env}; if [[ -z ${governance_entrypoint_sha}") {
		t.Fatalf("runtime entrypoint must load routing before applying immutable identity:\n%s", body)
	}
	if !strings.Contains(body, launcher) {
		t.Fatalf("runtime entrypoint missing exact immutable bundle launcher %q:\n%s", launcher, body)
	}
	if strings.Contains(body, "DEVKIT_GOVERNANCE_RUNTIME_BUNDLE") || strings.Contains(body, "print-dev-env") {
		t.Fatalf("runtime entrypoint must not select authority from mutable environment:\n%s", body)
	}
	if got, want := ZshForRuntimeBundle(bundle), "export DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256="+SHA256ForRuntimeBundle(bundle)+"; "+body; got != want {
		t.Fatalf("runtime entrypoint wrapper mismatch:\n%s", got)
	}
	if SHA256ForRuntimeBundle(bundle) == SHA256ForRuntimeBundle(bundle+"-other") {
		t.Fatal("runtime entrypoint fingerprint must bind the exact bundle path")
	}
	if BodyForRuntimeBundle("") != "" {
		t.Fatal("empty immutable bundle path must not fall back to the legacy entrypoint")
	}
}

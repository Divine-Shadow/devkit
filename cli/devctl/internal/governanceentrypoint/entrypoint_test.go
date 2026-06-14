package governanceentrypoint

import (
	"strings"
	"testing"
)

func TestBodyPreservesVerifiedGovernanceJarIdentity(t *testing.T) {
	body := Body()
	for _, want := range []string{
		"source ${governance_env}",
		"governance_normalize_canonical_pwd",
		"normalized_pwd=/workspaces/dev${PWD#${host_dev}}",
		"unset OLDPWD",
		"required governance workspace path missing after cwd normalization",
		"governance_sanitize_runtime_env",
		"NIX_LDFLAGS=${NIX_LDFLAGS//${host_dev}/\\/workspaces\\/dev}",
		"out=${out//${host_dev}/\\/workspaces\\/dev}",
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
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("governance entrypoint must preserve verified prewarmed jar identity %q:\n%s", forbidden, body)
		}
	}
}

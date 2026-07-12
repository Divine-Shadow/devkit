package main

import (
	"strings"
	"testing"
)

func TestNativeCodexGovernanceAlivenessScriptRequiresGovernanceEvidence(t *testing.T) {
	script := nativeCodexGovernanceAlivenessScript()

	for _, want := range []string{
		"functions codex",
		"mcp__governance__",
		"mcp__governance__governance_operator_attention_status",
		"governance_operator_attention_status",
		"GOVERNANCE_MCP_OK",
		"function_call",
		"function_call_output",
		"CODEX_HOME/sessions",
		"no new Codex TUI log bytes",
		"MCP client .*failed to start",
		"MCP startup incomplete",
		"Transport closed",
		"prewarmed singleton control plane is not ready",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("governance aliveness script missing %q", want)
		}
	}

	if strings.Contains(script, "reply with: ok") {
		t.Fatalf("governance aliveness script still accepts the old bare ok prompt")
	}
	if !strings.Contains(script, "grep -qx 'GOVERNANCE_MCP_OK'") {
		t.Fatalf("governance aliveness script does not require an exact GOVERNANCE_MCP_OK line")
	}
}

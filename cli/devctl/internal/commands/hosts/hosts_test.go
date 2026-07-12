package hosts

import (
	"strings"
	"testing"

	"devkit/cli/devctl/internal/cmdregistry"
)

func TestHandleRequiresRuntimeFlake(t *testing.T) {
	err := handle(&cmdregistry.Context{Project: "dev-all", Args: []string{"print"}})
	if err == nil {
		t.Fatalf("expected hosts command to fail without runtime.flake")
	}
	if got := err.Error(); !strings.Contains(got, "hosts requires runtime.flake") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseOptionsRejectsAgentTarget(t *testing.T) {
	_, err := parseOptions([]string{"print", "--target", "agents"})
	if err == nil {
		t.Fatalf("expected agents target to fail")
	}
	if got := err.Error(); !strings.Contains(got, "only --target host") {
		t.Fatalf("unexpected error: %v", err)
	}
}

package hosts

import (
	"strings"
	"testing"

	"devkit/cli/devctl/internal/cmdregistry"
)

func TestHandleRejectsDevAll(t *testing.T) {
	err := handle(&cmdregistry.Context{Project: "dev-all", Args: []string{"print"}})
	if err == nil {
		t.Fatalf("expected dev-all hosts command to fail")
	}
	if got := err.Error(); !strings.Contains(got, "legacy Compose/container command for dev-all") {
		t.Fatalf("unexpected error: %v", err)
	}
}

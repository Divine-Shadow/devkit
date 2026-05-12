package composecmd

import (
	"strings"
	"testing"

	"devkit/cli/devctl/internal/cmdregistry"
)

func TestRegisterUsesHistoricalComposeNamespace(t *testing.T) {
	reg := cmdregistry.New()
	Register(reg)
	if _, ok := reg.Lookup("compose"); !ok {
		t.Fatalf("expected compose command to be registered")
	}
	for _, name := range []string{"up", "down", "restart", "status", "logs", "exec", "attach"} {
		if _, ok := reg.Lookup(name); ok {
			t.Fatalf("did not expect top-level legacy compose command %s", name)
		}
	}
}

func TestHandleRejectsUnknownComposeCommand(t *testing.T) {
	err := handle(&cmdregistry.Context{Project: "codex", Args: []string{"bogus"}})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestHandleRejectsDevAllCompose(t *testing.T) {
	err := handle(&cmdregistry.Context{Project: "dev-all", Args: []string{"up"}})
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "retired for dev-all") {
		t.Fatalf("unexpected error: %v", err)
	}
}

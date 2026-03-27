package agentexec

import (
	"strings"
	"testing"
)

func TestBuildCommandPrefersZshInteractiveShell(t *testing.T) {
	cmd, err := BuildCommand(CommandOpts{
		Project:        "dev-all",
		Index:          "2",
		Dest:           "/workspaces/dev/agent-worktrees/agent2/ouroboros-ide",
		Service:        "dev-agent",
		ComposeProject: "devkit-ouro8",
		ContainerName:  "devkit-ouro8-dev-agent-2",
		GitName:        "Test User",
		GitEmail:       "test@example.com",
	})
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}
	for _, frag := range []string{
		"docker exec -it 'devkit-ouro8-dev-agent-2' bash -lc",
		`export HOME="/workspaces/dev/.devhome" CODEX_HOME="/workspaces/dev/.devhome/.codex"`,
		`cd "/workspaces/dev/agent-worktrees/agent2/ouroboros-ide" 2>/dev/null || true; if command -v zsh >/dev/null 2>&1; then exec zsh -i; fi; exec bash`,
	} {
		if !strings.Contains(cmd, frag) {
			t.Fatalf("command missing %q: %s", frag, cmd)
		}
	}
}

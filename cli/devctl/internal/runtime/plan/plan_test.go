package plan

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/compose"
)

func TestBuildDevAllPlan(t *testing.T) {
	paths := compose.Paths{Root: "/home/bayesartre/dev/devkit", Kit: "/home/bayesartre/dev/devkit/kit"}
	p, err := BuildDevAll(BuildOptions{
		Paths:    paths,
		Project:  "dev-all",
		Index:    2,
		Repo:     "ouroboros-ide",
		Flake:    ".#ouroboros-dev-agent",
		Launcher: "bubblewrap",
	})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	if p.Agent.HostWorktree != "/home/bayesartre/dev/agent-worktrees/agent2/ouroboros-ide" {
		t.Fatalf("host worktree = %q", p.Agent.HostWorktree)
	}
	if p.Agent.SandboxWorktree != "/workspaces/dev/agent-worktrees/agent2/ouroboros-ide" {
		t.Fatalf("sandbox worktree = %q", p.Agent.SandboxWorktree)
	}
	if p.Agent.HostHome != "/home/bayesartre/dev/agent-worktrees/agent2/.devhome-agent2" {
		t.Fatalf("host home = %q", p.Agent.HostHome)
	}
	if p.Env["CODEX_HOME"] != filepath.Join(p.Agent.SandboxHome, ".codex") {
		t.Fatalf("CODEX_HOME = %q", p.Env["CODEX_HOME"])
	}
	if p.Env["DOCKER_HOST"] != "unix:///run/devkit/test-container-broker.sock" {
		t.Fatalf("DOCKER_HOST = %q", p.Env["DOCKER_HOST"])
	}
	if p.DirectDockerSocket {
		t.Fatalf("native plan must not expose direct Docker socket")
	}
	if len(p.LauncherArgs) == 0 || p.LauncherArgs[0] != "bwrap" {
		t.Fatalf("launcher args = %#v", p.LauncherArgs)
	}
}

func TestBuildDevAllRejectsOtherProjects(t *testing.T) {
	_, err := BuildDevAll(BuildOptions{
		Paths:   compose.Paths{Root: "/repo/devkit"},
		Project: "codex",
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRenderJSONRoundTrip(t *testing.T) {
	p, err := BuildDevAll(BuildOptions{Paths: compose.Paths{Root: "/repo/devkit"}})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	data, err := RenderJSON(p)
	if err != nil {
		t.Fatalf("RenderJSON error: %v", err)
	}
	var decoded Plan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json unmarshal: %v\n%s", err, data)
	}
	if decoded.Agent.ID.Project != "dev-all" {
		t.Fatalf("project = %q", decoded.Agent.ID.Project)
	}
}

func TestRenderTextIncludesSafetyFields(t *testing.T) {
	p, err := BuildDevAll(BuildOptions{Paths: compose.Paths{Root: "/repo/devkit"}})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	text := RenderText(p)
	for _, want := range []string{
		"Native agent plan",
		"direct_docker_socket: false",
		"broker_endpoint: /run/devkit/test-container-broker.sock",
		"CODEX_HOME=",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q:\n%s", want, text)
		}
	}
}

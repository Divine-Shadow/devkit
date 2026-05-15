package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
)

func TestBuildDevAllPlan(t *testing.T) {
	paths := devkitpaths.Paths{Root: "/home/bayesartre/dev/devkit", Kit: "/home/bayesartre/dev/devkit/kit"}
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
	if p.Agent.SandboxWorktree != "/worktrees/agent2/ouroboros-ide" {
		t.Fatalf("sandbox worktree = %q", p.Agent.SandboxWorktree)
	}
	if p.Agent.HostHome != "/home/bayesartre/dev/agent-worktrees/agent2/.devhome-agent2" {
		t.Fatalf("host home = %q", p.Agent.HostHome)
	}
	if p.Agent.SandboxHome != "/worktrees/agent2/.devhome-agent2" {
		t.Fatalf("sandbox home = %q", p.Agent.SandboxHome)
	}
	if p.Env["CODEX_HOME"] != filepath.Join(p.Agent.SandboxHome, ".codex") {
		t.Fatalf("CODEX_HOME = %q", p.Env["CODEX_HOME"])
	}
	if p.Env["DOCKER_HOST"] != "unix:///run/devkit/test-container-broker.sock" {
		t.Fatalf("DOCKER_HOST = %q", p.Env["DOCKER_HOST"])
	}
	if p.Env["HTTP_PROXY"] != "" || p.Env["HTTPS_PROXY"] != "" {
		t.Fatalf("native plan should not default to retired proxy env: %#v", p.Env)
	}
	if p.DirectDockerSocket {
		t.Fatalf("native plan must not expose direct Docker socket")
	}
	if !hasBind(p.Binds, "/home/bayesartre/dev", "/workspaces/dev") {
		t.Fatalf("native plan must mount dev root at /workspaces/dev: %#v", p.Binds)
	}
	if !hasBind(p.Binds, "/home/bayesartre/dev", "/home/bayesartre/dev") {
		t.Fatalf("native plan must mount dev root at its host path for git metadata: %#v", p.Binds)
	}
	if len(p.LauncherArgs) == 0 || p.LauncherArgs[0] != "bwrap" {
		t.Fatalf("launcher args = %#v", p.LauncherArgs)
	}
}

func hasBind(binds []Bind, source, target string) bool {
	for _, bind := range binds {
		if bind.Source == source && bind.Target == target {
			return true
		}
	}
	return false
}

func TestBuildDevAllHonorsExplicitProxy(t *testing.T) {
	p, err := BuildDevAll(BuildOptions{
		Paths:   devkitpaths.Paths{Root: "/repo/devkit"},
		Project: "dev-all",
		Proxy:   "http://127.0.0.1:8899",
	})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	if p.Env["HTTP_PROXY"] != "http://127.0.0.1:8899" || p.Env["HTTPS_PROXY"] != "http://127.0.0.1:8899" {
		t.Fatalf("proxy env = %#v", p.Env)
	}
}

func TestBuildDevAllProxySocketUsesLocalProxy(t *testing.T) {
	p, err := BuildDevAll(BuildOptions{
		Paths:       devkitpaths.Paths{Root: "/repo/devkit"},
		Project:     "dev-all",
		ProxySocket: "/repo/.devkit/native-egress/proxy.sock",
	})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	if p.Env["HTTP_PROXY"] != "http://127.0.0.1:18888" || p.Env["HTTPS_PROXY"] != "http://127.0.0.1:18888" {
		t.Fatalf("proxy env = %#v", p.Env)
	}
	if p.Proxy.UnixSocket != "/repo/.devkit/native-egress/proxy.sock" {
		t.Fatalf("proxy socket = %q", p.Proxy.UnixSocket)
	}
	if !hasBind(p.Binds, "/repo/.devkit/native-egress/proxy.sock", "/repo/.devkit/native-egress/proxy.sock") {
		t.Fatalf("native plan must bind proxy socket: %#v", p.Binds)
	}
}

func TestBuildNativeSupportsOtherProjects(t *testing.T) {
	p, err := Build(BuildOptions{
		Paths:   devkitpaths.Paths{Root: "/repo/devkit"},
		Project: "codex",
		Repo:    "ouroboros-ide",
		Flake:   "./overlays/codex#default",
	})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if p.Agent.ID.Project != "codex" {
		t.Fatalf("project = %q", p.Agent.ID.Project)
	}
	if p.Flake != "./overlays/codex#default" {
		t.Fatalf("flake = %q", p.Flake)
	}
	if p.Agent.StateRoot != "/repo/.devkit/native-agents/codex-agent1" {
		t.Fatalf("state root = %q", p.Agent.StateRoot)
	}
	if !hasBind(p.Binds, "/repo/agent-worktrees/agent1/ouroboros-ide", "/workspace") {
		t.Fatalf("native plan must mount agent worktree at /workspace: %#v", p.Binds)
	}
}

func TestBuildDevAllDedicatedWorktreeUsesFanoutForAgentOne(t *testing.T) {
	p, err := BuildDevAll(BuildOptions{
		Paths:             devkitpaths.Paths{Root: "/repo/devkit"},
		Project:           "dev-all",
		Index:             1,
		Repo:              "ouroboros-ide",
		DedicatedWorktree: true,
	})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	if p.Agent.HostWorktree != "/repo/agent-worktrees/agent1/ouroboros-ide" {
		t.Fatalf("host worktree = %q", p.Agent.HostWorktree)
	}
	if p.Agent.SandboxWorktree != "/worktrees/agent1/ouroboros-ide" {
		t.Fatalf("sandbox worktree = %q", p.Agent.SandboxWorktree)
	}
	if p.Agent.HostHome != "/repo/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1" {
		t.Fatalf("host home = %q", p.Agent.HostHome)
	}
}

func TestBuildDevAllProjectsSymlinkedWorktreeIntoMountedDevRoot(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	repoRoot := filepath.Join(devRoot, "ouroboros-ide")
	agentRoot := filepath.Join(devRoot, "agent-worktrees", "agent1")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(agentRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../ouroboros-ide", filepath.Join(agentRoot, "ouroboros-ide")); err != nil {
		t.Fatal(err)
	}

	p, err := BuildDevAll(BuildOptions{
		Paths:   devkitpaths.Paths{Root: devkitRoot},
		Project: "dev-all",
		Index:   1,
		Repo:    "ouroboros-ide",
	})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	if p.Agent.HostWorktree != filepath.Join(agentRoot, "ouroboros-ide") {
		t.Fatalf("host worktree = %q", p.Agent.HostWorktree)
	}
	if p.Agent.SandboxWorktree != "/workspaces/dev/ouroboros-ide" {
		t.Fatalf("sandbox worktree = %q", p.Agent.SandboxWorktree)
	}
	if p.Agent.HostHome != filepath.Join(repoRoot, ".devhome-agent1") {
		t.Fatalf("host home = %q", p.Agent.HostHome)
	}
	if p.Agent.SandboxHome != "/workspaces/dev/ouroboros-ide/.devhome-agent1" {
		t.Fatalf("sandbox home = %q", p.Agent.SandboxHome)
	}
	if p.Env["HOME"] != p.Agent.SandboxHome || p.Env["CODEX_HOME"] != filepath.Join(p.Agent.SandboxHome, ".codex") {
		t.Fatalf("env home mismatch: %#v", p.Env)
	}
}

func TestBuildDevAllUsesConfiguredRoots(t *testing.T) {
	p, err := BuildDevAll(BuildOptions{
		Paths:                 devkitpaths.Paths{Root: "/repo/devkit"},
		Project:               "dev-all",
		Index:                 2,
		Repo:                  "devkit",
		WorktreeRoot:          "/tmp/worktrees",
		StateRoot:             "/tmp/state",
		WorktreeContainerRoot: "/native-worktrees",
		StateContainerRoot:    "/native-state",
	})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	if p.Agent.HostWorktree != "/tmp/worktrees/agent2/devkit" {
		t.Fatalf("host worktree = %q", p.Agent.HostWorktree)
	}
	if p.Agent.StateRoot != "/tmp/state/dev-all-agent2" {
		t.Fatalf("state root = %q", p.Agent.StateRoot)
	}
	if p.Agent.SandboxWorktree != "/native-worktrees/agent2/devkit" {
		t.Fatalf("sandbox worktree = %q", p.Agent.SandboxWorktree)
	}
	if p.Agent.SandboxHome != "/native-worktrees/agent2/.devhome-agent2" {
		t.Fatalf("sandbox home = %q", p.Agent.SandboxHome)
	}
}

func TestBuildManifestIncludesEveryAgent(t *testing.T) {
	manifest, err := BuildManifest(BuildOptions{
		Paths:        devkitpaths.Paths{Root: "/repo/devkit"},
		Project:      "dev-all",
		Repo:         "devkit",
		BaseBranch:   "main",
		BranchPrefix: "native-agent",
	}, 2)
	if err != nil {
		t.Fatalf("BuildManifest error: %v", err)
	}
	if manifest.Runtime != "native" || manifest.Count != 2 {
		t.Fatalf("manifest = %#v", manifest)
	}
	if len(manifest.Agents) != 2 {
		t.Fatalf("agents = %#v", manifest.Agents)
	}
	if manifest.Agents[0].HostWorktree != "/repo/agent-worktrees/agent1/devkit" {
		t.Fatalf("agent1 worktree = %q", manifest.Agents[0].HostWorktree)
	}
	if manifest.Agents[0].StateRoot == manifest.Agents[1].StateRoot {
		t.Fatalf("state roots should be distinct: %#v", manifest.Agents)
	}
	if manifest.BaseBranch != "main" || manifest.BranchPrefix != "native-agent" {
		t.Fatalf("branch metadata = %#v", manifest)
	}
}

func TestRenderJSONRoundTrip(t *testing.T) {
	p, err := BuildDevAll(BuildOptions{Paths: devkitpaths.Paths{Root: "/repo/devkit"}})
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
	p, err := BuildDevAll(BuildOptions{Paths: devkitpaths.Paths{Root: "/repo/devkit"}})
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

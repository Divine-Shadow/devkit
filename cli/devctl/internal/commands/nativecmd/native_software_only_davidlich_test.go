package nativecmd

import (
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"path/filepath"
	"testing"
)

func TestSoftwareOnlyDavidlichPreservesMountedCurrentAndNestedHome(t *testing.T) {
	selected := "/nix/store/owned-selected-codex/bin/codex"
	command := []string{"/run/current-system/sw/bin/fleet-governed-app-server", "--", "/workspaces/dev/ouroboros-ide/.devhome-agent1/.codex/packages/standalone/current/bin/codex", "app-server",
		"-c", `projects."/workspaces/dev/ouroboros-ide".trust_level="trusted"`,
		"-c", `projects."/home/bayesartre/dev/agent-worktrees/agent1/ouroboros-ide".trust_level="trusted"`,
		"--listen", "unix:///workspaces/dev/ouroboros-ide/.devhome-agent1/.codex/a1-app.sock", "--analytics-default-enabled"}
	if err := validateSoftwareOnlyNativeAppServerCommand(command, selected, softwareOnlyRemoteGUITarget); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func([]string){
		"host-executable":    func(c []string) { c[2] = selected },
		"foreign-executable": func(c []string) { c[2] = "/tmp/codex" },
		"sibling-home": func(c []string) {
			c[2] = "/workspaces/dev/.devhome-agent2/.codex/packages/standalone/current/bin/codex"
		},
		"listener": func(c []string) { c[9] = "unix:///workspaces/dev/.devhome-agent1/.codex/a1-app.sock" },
		"trust":    func(c []string) { c[5] = `approval_policy="never"` },
	} {
		t.Run(name, func(t *testing.T) {
			c := append([]string{}, command...)
			mutate(c)
			if err := validateSoftwareOnlyNativeAppServerCommand(c, selected, softwareOnlyRemoteGUITarget); err == nil {
				t.Fatal("foreign executable/home/effect admitted")
			}
		})
	}
	if err := validateSoftwareOnlyNativeAppServerCommand(command, selected, "davidlich-2"); err == nil {
		t.Fatal("unselected sibling admitted")
	}
	hostHome, sandboxHome := softwareOnlyNativeHomes(softwareOnlyRemoteGUITarget)
	p := nativeplan.Plan{GUITargetConfig: &nativeplan.GUITargetConfigProjection{TargetID: softwareOnlyRemoteGUITarget}, HostWorkspaceRoot: softwareOnlyNativeHostWorkspace(1), SandboxWorkspaceRoot: softwareOnlySandboxWorkspace, IsolationProfile: "workspace-egress"}
	p.Agent.ID.Project, p.Agent.ID.Repo, p.Agent.ID.Index = "dev-all", "ouroboros-ide", 1
	p.Agent.HostWorktree = filepath.Join(p.HostWorkspaceRoot, "ouroboros-ide")
	p.Agent.HostHome = hostHome
	p.Agent.SandboxWorktree = filepath.Join(softwareOnlySandboxWorkspace, "ouroboros-ide")
	p.Agent.SandboxHome = sandboxHome
	if err := validateSoftwareOnlyNativePlan(true, p); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*nativeplan.Plan){
		"non-nested-host-home": func(p *nativeplan.Plan) {
			p.Agent.HostHome = "/home/bayesartre/dev/agent-worktrees/agent1/.devhome-agent1"
		},
		"non-nested-sandbox-home": func(p *nativeplan.Plan) { p.Agent.SandboxHome = "/workspaces/dev/.devhome-agent1" },
		"index":                   func(p *nativeplan.Plan) { p.Agent.ID.Index = 2 },
		"workspace":               func(p *nativeplan.Plan) { p.HostWorkspaceRoot = "/caller" },
		"profile":                 func(p *nativeplan.Plan) { p.IsolationProfile = "none" },
	} {
		t.Run(name, func(t *testing.T) {
			q := p
			mutate(&q)
			if err := validateSoftwareOnlyNativePlan(true, q); err == nil {
				t.Fatal("foreign native plan admitted")
			}
		})
	}
}

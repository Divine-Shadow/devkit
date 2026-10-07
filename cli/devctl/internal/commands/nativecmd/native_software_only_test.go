package nativecmd

import (
	"devkit/cli/devctl/internal/cmdregistry"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSoftwareOnlyNativeStartupRejectsOtherEffectsBeforePreparation(t *testing.T) {
	selected := "/nix/store/owned-selected-codex/bin/codex"
	accepted := []string{"/run/current-system/sw/bin/fleet-governed-app-server", "--", selected, "app-server",
		"-c", `projects."/workspaces/dev/ouroboros-ide".trust_level="trusted"`,
		"-c", `projects."/home/bayesartre/dev/agent-worktrees/agent3/ouroboros-ide".trust_level="trusted"`,
		"--listen", "unix:///workspaces/dev/.devhome-agent3/.codex/a3-app.sock", "--analytics-default-enabled"}
	if err := validateSoftwareOnlyNativeAppServerCommand(accepted, selected, testSoftwareOnlyNativePlan("local-wsl-devkit-agent", softwareOnlyGUITarget, "shadow-throne", 3)); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func([]string) []string{
		func(c []string) []string { c[2] = "/tmp/codex"; return c },
		func(c []string) []string { c[2] = "codex"; return c },
		func(c []string) []string { c[2] = "/nix/store/other/bin/codex"; return c },
		func(c []string) []string { c[3] = "exec"; return c },
		func(c []string) []string { c[5] = `approval_policy="never"`; return c },
		func(c []string) []string { c[9] = "unix:///tmp/other.sock"; return c },
		func(c []string) []string { return append(c, "--config", "model=caller-selected") },
		func(c []string) []string { return append(c, "--home", "/caller-selected") },
		func(c []string) []string { return c[2:] },
	} {
		invalid := mutate(append([]string{}, accepted...))
		if err := validateSoftwareOnlyNativeAppServerCommand(invalid, selected, testSoftwareOnlyNativePlan("local-wsl-devkit-agent", softwareOnlyGUITarget, "shadow-throne", 3)); err == nil {
			t.Fatalf("accepted caller effect: %v", invalid)
		}
	}
	for _, test := range []struct {
		target, project string
		managed         bool
	}{
		{"", "dev-all", false},
		{softwareOnlyGUITarget, "dev-workspace", false},
		{softwareOnlyGUITarget, "dev-all", true},
	} {
		if err := validateSoftwareOnlyNativeAppServer(true, test.target, test.project, test.managed, accepted); err == nil {
			t.Fatalf("accepted other target/profile: %#v", test)
		}
	}
	ctx := &cmdregistry.Context{Project: "dev-all"}
	if err := runTopExec(ctx, topExecArgs{softwareOnly: true, guiTargetID: softwareOnlyGUITarget}, []string{"bash"}); err == nil || !strings.Contains(err.Error(), "permits only") {
		t.Fatalf("non-app-server reached preparation: %v", err)
	}
	if _, err := parsePlanArgs(&cmdregistry.Context{Args: []string{"prepare", "--software-only"}}, false, false); err == nil {
		t.Fatal("prepare accepted software-only exec selector")
	}
	if _, err := parseTopExecArgs(&cmdregistry.Context{Args: []string{"3", "--software-only"}}, true); err == nil {
		t.Fatal("attach accepted software-only exec selector")
	}
}

func TestSoftwareOnlySelectorReachesBothNativeExecParsers(t *testing.T) {
	plan, err := parsePlanArgs(&cmdregistry.Context{Args: []string{"exec", "--software-only", "--gui-target-id", softwareOnlyGUITarget, "--", "/run/current-system/sw/bin/fleet-governed-app-server", "--", "/nix/store/owned-selected-codex/bin/codex", "app-server"}}, true, false)
	if err != nil || !plan.softwareOnly {
		t.Fatalf("native exec selector: %#v %v", plan, err)
	}
	top, err := parseTopExecArgs(&cmdregistry.Context{Args: []string{"3", "--software-only", "--gui-target-id", softwareOnlyGUITarget, "--", "/run/current-system/sw/bin/fleet-governed-app-server", "--", "/nix/store/owned-selected-codex/bin/codex", "app-server"}}, false)
	if err != nil || !top.softwareOnly {
		t.Fatalf("top exec selector: %#v %v", top, err)
	}
}

func TestSoftwareOnlyLocal2AppServerCommandRejectsSiblingGeometry(t *testing.T) {
	selected := "/nix/store/owned-selected-codex/bin/codex"
	command := []string{"/run/current-system/sw/bin/fleet-governed-app-server", "--", selected, "app-server",
		"-c", `projects."/workspaces/dev/ouroboros-ide".trust_level="trusted"`,
		"-c", `projects."/home/bayesartre/dev/agent-worktrees/agent2/ouroboros-ide".trust_level="trusted"`,
		"--listen", "unix:///workspaces/dev/.devhome-agent2/.codex/a2-app.sock", "--analytics-default-enabled"}
	if err := validateSoftwareOnlyNativeAppServerCommand(command, selected, testSoftwareOnlyNativePlan("local-wsl-devkit-agent", "shadow-throne-local-2", "shadow-throne", 2)); err != nil {
		t.Fatal(err)
	}
	if err := validateSoftwareOnlyNativeAppServerCommand(command, selected, testSoftwareOnlyNativePlan("local-wsl-devkit-agent", softwareOnlyGUITarget, "shadow-throne", 3)); err == nil {
		t.Fatal("Local2 argv admitted as Local3")
	}
	for _, mutate := range []func([]string){
		func(c []string) {
			c[7] = `projects."/home/bayesartre/dev/agent-worktrees/agent3/ouroboros-ide".trust_level="trusted"`
		},
		func(c []string) { c[9] = "unix:///workspaces/dev/.devhome-agent3/.codex/a3-app.sock" },
	} {
		invalid := append([]string{}, command...)
		mutate(invalid)
		if err := validateSoftwareOnlyNativeAppServerCommand(invalid, selected, testSoftwareOnlyNativePlan("local-wsl-devkit-agent", "shadow-throne-local-2", "shadow-throne", 2)); err == nil {
			t.Fatalf("sibling geometry admitted: %v", invalid)
		}
	}
}

// The ID and public execution class come from the selected projection; no
// hostname/ID parser supplies authority in the implementation.
func testSoftwareOnlyNativePlan(kind, id, host string, index int) nativeplan.Plan {
	hostHome, sandboxHome := softwareOnlyNativeHomes(index)
	p := nativeplan.Plan{GUITargetConfig: &nativeplan.GUITargetConfigProjection{TargetID: id, Kind: kind, ExpectedExecutionHost: host}, HostWorkspaceRoot: softwareOnlyNativeHostWorkspace(index), SandboxWorkspaceRoot: softwareOnlySandboxWorkspace, IsolationProfile: "workspace-egress"}
	p.Agent.ID.Project, p.Agent.ID.Repo, p.Agent.ID.Index = "dev-all", "ouroboros-ide", index
	p.Agent.HostWorktree, p.Agent.SandboxWorktree = filepath.Join(p.HostWorkspaceRoot, "ouroboros-ide"), filepath.Join(p.SandboxWorkspaceRoot, "ouroboros-ide")
	p.Agent.HostHome, p.Agent.SandboxHome = hostHome, sandboxHome
	return p
}

func TestSoftwareOnlyNativeSourceSelectedStandardFleetClass(t *testing.T) {
	for _, c := range []struct {
		kind, id, host string
		index          int
	}{
		{"local-wsl-devkit-agent", "shadow-throne-local-1", "shadow-throne", 1},
		{"local-wsl-devkit-agent", "shadow-throne-local-2", "shadow-throne", 2},
		{"local-wsl-devkit-agent", "shadow-throne-local-3", "shadow-throne", 3},
		{"devkit-agent", "davidlich-1", "davidlich-nix", 1},
		{"devkit-agent", "derpinator-1", "derpinator-nix", 1},
		{"devkit-agent", "derpinator-2", "derpinator-nix", 2},
		{"devkit-agent", "drtalos-1", "drtalos-nix", 1},
		{"devkit-agent", "new-source-owned-station-7", "new-source-host", 7},
	} {
		t.Run(c.id, func(t *testing.T) {
			p := testSoftwareOnlyNativePlan(c.kind, c.id, c.host, c.index)
			if err := validateSoftwareOnlyNativePlanOnHost(p, c.host); err != nil {
				t.Fatal(err)
			}
			selected := "/nix/store/owned-selected-codex/bin/codex"
			executable := selected
			if c.kind == "devkit-agent" {
				executable = filepath.Join(p.Agent.SandboxHome, ".codex/packages/standalone/current/bin/codex")
			}
			command := []string{"/run/current-system/sw/bin/fleet-governed-app-server", "--", executable, "app-server", "-c", "projects." + strconv.Quote(p.Agent.SandboxWorktree) + `.trust_level="trusted"`, "-c", "projects." + strconv.Quote(p.Agent.HostWorktree) + `.trust_level="trusted"`, "--listen", "unix://" + filepath.Join(p.Agent.SandboxHome, ".codex", "a"+strconv.Itoa(c.index)+"-app.sock"), "--analytics-default-enabled"}
			if err := validateSoftwareOnlyNativeAppServerCommand(command, selected, p); err != nil {
				t.Fatal(err)
			}
			if err := validateSoftwareOnlyNativePlanOnHost(p, "foreign-execution-host"); err == nil {
				t.Fatal("foreign execution host admitted")
			}
			for name, mutate := range map[string]func(*nativeplan.Plan){
				"missing-class":    func(p *nativeplan.Plan) { p.GUITargetConfig.Kind = "" },
				"guest-class":      func(p *nativeplan.Plan) { p.GUITargetConfig.Kind = "local-wsl-devkit-agent-vm" },
				"management-class": func(p *nativeplan.Plan) { p.GUITargetConfig.Kind = "management" },
				"missing-host":     func(p *nativeplan.Plan) { p.GUITargetConfig.ExpectedExecutionHost = "" },
				"foreign-home":     func(p *nativeplan.Plan) { p.Agent.HostHome = "/caller/home" },
				"foreign-profile":  func(p *nativeplan.Plan) { p.Agent.ID.Project = "ouroboros-terraform" },
				"foreign-worktree": func(p *nativeplan.Plan) { p.Agent.SandboxWorktree = "/workspaces/dev/other" },
			} {
				t.Run(name, func(t *testing.T) {
					q := p
					projection := *p.GUITargetConfig
					q.GUITargetConfig = &projection
					mutate(&q)
					if err := validateSoftwareOnlyNativePlanOnHost(q, c.host); err == nil {
						t.Fatal("foreign source identity/geometry admitted")
					}
				})
			}
		})
	}
}

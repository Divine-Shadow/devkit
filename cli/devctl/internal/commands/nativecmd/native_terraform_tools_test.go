package nativecmd

import (
	"devkit/cli/devctl/internal/runtime/agent"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"strings"
	"testing"
)

func terraformToolsPlan() nativeplan.Plan {
	const workspace = "/home/bayesartre/dev/control-plane-worktrees/terraform-ouro-1"
	const state = "/home/bayesartre/dev/.devkit/native-agents/ouroboros-terraform-agent1"
	return nativeplan.Plan{Agent: agent.Spec{ID: agent.ID{Project: "ouroboros-terraform", Repo: "ouroboros-terraform", Index: 1}, HostWorktree: workspace + "/ouroboros-terraform", SandboxWorktree: "/workspaces/dev/ouroboros-terraform", HostHome: state + "/home", SandboxHome: "/agent-state/ouroboros-terraform-agent1/home", StateRoot: state}, HostWorkspaceRoot: workspace, SandboxWorkspaceRoot: "/workspaces/dev", IsolationProfile: "workspace-egress", GUITargetConfig: &nativeplan.GUITargetConfigProjection{TargetID: "terraform-ouro-1", Kind: "devkit-agent", ExpectedExecutionHost: "shadow-throne"}}
}
func terraformToolsCommand() []string {
	return []string{"/run/current-system/sw/bin/ouro-terra-runtime", "--", "/nix/store/selected-codex/bin/codex", "app-server", "-c", `projects."/workspaces/dev/ouroboros-terraform".trust_level="trusted"`, "-c", `projects."/home/bayesartre/dev/control-plane-worktrees/terraform-ouro-1/ouroboros-terraform".trust_level="trusted"`, "--listen", "unix:///agent-state/ouroboros-terraform-agent1/home/.codex/a1-app.sock", "--analytics-default-enabled"}
}
func TestTerraformToolsSelectedCommandAndGeometry(t *testing.T) {
	p := terraformToolsPlan()
	command := terraformToolsCommand()
	if err := launch.ValidateTerraformToolsPlan(p, "shadow-throne"); err != nil {
		t.Fatal(err)
	}
	if err := validateTerraformToolsAppServerCommand(p, command, command[2]); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"target", "host", "index", "project", "workspace", "home", "isolation", "projection", "arbitrary", "override", "codex"} {
		t.Run(kind, func(t *testing.T) {
			p := terraformToolsPlan()
			c := terraformToolsCommand()
			host := "shadow-throne"
			switch kind {
			case "target":
				p.GUITargetConfig.TargetID = "terraform-ouro-2"
			case "host":
				host = "shadow"
			case "index":
				p.Agent.ID.Index = 2
			case "project":
				p.Agent.ID.Project = "dev-all"
			case "workspace":
				p.HostWorkspaceRoot += "/.."
			case "home":
				p.Agent.HostHome = "/tmp/home"
			case "isolation":
				p.IsolationProfile = "none"
			case "projection":
				p.GUITargetConfig = nil
			case "arbitrary":
				c = []string{"/bin/sh", "-c", "terraform plan"}
			case "override":
				c = append(c, "--config", "foreign")
			case "codex":
				c[2] = "/tmp/codex"
			}
			if launch.ValidateTerraformToolsPlan(p, host) == nil && validateTerraformToolsAppServerCommand(p, c, command[2]) == nil {
				t.Fatal("accepted foreign tools startup")
			}
		})
	}
}
func TestTerraformToolsExecPhasesAreClosed(t *testing.T) {
	c := terraformToolsCommand()
	for _, phase := range []string{"", terraformToolsPreparationPhase} {
		if err := validateTerraformToolsExecSelection(phase, false, false, "ouroboros-terraform", "terraform-ouro-1", c); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"unknown", "software", "managed", "project", "target", "shell"} {
		phase := terraformToolsPreparationPhase
		software, managed := false, false
		project, target := "ouroboros-terraform", "terraform-ouro-1"
		command := c
		switch kind {
		case "unknown":
			phase = "skip-auth"
		case "software":
			software = true
		case "managed":
			managed = true
		case "project":
			project = "dev-all"
		case "target":
			target = "terraform-ouro-2"
		case "shell":
			command = []string{"bash", "-c", "true"}
		}
		if err := validateTerraformToolsExecSelection(phase, software, managed, project, target, command); err == nil {
			t.Fatalf("accepted %s", kind)
		}
	}
	if terraformToolsProviderReadiness(terraformToolsPreparationPhase) != "unassessed" || terraformToolsProviderHydration(terraformToolsPreparationPhase) != "deferred" || strings.Contains(terraformToolsReadinessStatus(terraformToolsPreparationPhase, true, "ready"), "provider") {
		t.Fatal("invalid provider assertion")
	}
}

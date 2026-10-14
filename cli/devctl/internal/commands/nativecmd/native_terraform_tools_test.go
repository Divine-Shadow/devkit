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

func TestTerraformToolsImmutableChecksAdmission(t *testing.T) {
	ctx, cfg, _ := terraformSelection(t)
	root := "/nix/store/selected-devctl"
	ctx.Paths.Root = root
	ctx.Paths.RuntimeAuthorityRoot = root
	ctx.Paths.OverlayPaths = []string{root + "/overlays"}
	cfg.SourceDir = root + "/overlays/ouroboros-terraform"
	cfg.Defaults.BaseBranch = "main"
	cfg.Defaults.BranchPrefix = "agent"
	if err := validateTerraformToolsSource(terraformToolsPreparationPhase, ctx, cfg); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"root", "overlays", "extra-overlay", "source", "origin", "branch"} {
		cc := *ctx
		cc.Paths = ctx.Paths
		other := cfg
		switch kind {
		case "root":
			cc.Paths.Root = "/tmp/root"
		case "overlays":
			cc.Paths.OverlayPaths = []string{"/tmp/arbitrary-checks"}
		case "extra-overlay":
			cc.Paths.OverlayPaths = append([]string{}, ctx.Paths.OverlayPaths...)
			cc.Paths.OverlayPaths = append(cc.Paths.OverlayPaths, "/tmp/checks")
		case "source":
			other.SourceDir = "/tmp/source-fixed-looking-overlay"
		case "origin":
			other.Defaults.Origin = "foreign"
		case "branch":
			other.Defaults.BranchPrefix = "foreign"
		}
		if err := validateTerraformToolsSource(terraformToolsPreparationPhase, &cc, other); err == nil {
			t.Fatalf("accepted %s", kind)
		}
		if err := validateTerraformToolsSource("", &cc, other); err != nil {
			t.Fatalf("changed default source behavior: %v", err)
		}
	}
}

func TestTerraformToolsRejectsRuntimeAndLifecycleOverrides(t *testing.T) {
	for _, args := range [][]string{{"exec", "1", "--flake", "foreign", "--", "anything"}, {"prepare", "--proxy", "foreign"}, {"readiness", "--repo-check", "arbitrary"}, {"readiness", "--full"}, {"exec", "1", "--broker-socket", "/tmp/broker", "--", "anything"}, {"prepare", "--dry-run"}, {"shell", "--preparation-phase", terraformToolsPreparationPhase}} {
		if err := validateTerraformToolsArguments(args); err == nil {
			t.Fatalf("accepted override %v", args)
		}
	}
}

func TestTerraformToolsTopExecParserPropagation(t *testing.T) {
	ctx, cfg, _ := terraformSelection(t)
	root := "/nix/store/selected-devctl"
	ctx.Paths.Root = root
	ctx.Paths.RuntimeAuthorityRoot = root
	ctx.Paths.OverlayPaths = []string{root + "/overlays"}
	cfg.SourceDir = root + "/overlays/ouroboros-terraform"
	cfg.Defaults.BaseBranch = "main"
	cfg.Defaults.BranchPrefix = "agent"
	ctx.Args = []string{"1", "--repo", "ouroboros-terraform", "--worktree-root", "/home/bayesartre/dev/control-plane-worktrees", "--agent-state-root", "/home/bayesartre/dev/.devkit/native-agents", "--workspace-root", "/home/bayesartre/dev/control-plane-worktrees/terraform-ouro-1", "--gui-target-id", "terraform-ouro-1", "--isolation-profile", "workspace-egress", "--preparation-phase", terraformToolsPreparationPhase, "--"}
	ctx.Args = append(ctx.Args, terraformToolsCommand()...)
	parsed, err := parseTopExecArgs(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTerraformToolsSource(parsed.preparationPhase, ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if err := validateTerraformToolsExecSelection(parsed.preparationPhase, parsed.softwareOnly, parsed.managedAppServer, ctx.Project, parsed.guiTargetID, parsed.command); err != nil {
		t.Fatal(err)
	}
	if err := validateTerraformToolsAppServerCommand(terraformToolsPlan(), parsed.command, parsed.command[2]); err != nil {
		t.Fatal(err)
	}
}

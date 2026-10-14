package nativecmd

import (
	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/devkitpaths"
	"testing"
)

func terraformSelection(t *testing.T) (*cmdregistry.Context, config.OverlayConfig, planArgs) {
	t.Helper()
	const root = "/home/bayesartre/dev"
	ctx := &cmdregistry.Context{Project: "ouroboros-terraform", Paths: devkitpaths.Paths{Root: root + "/devkit"}, Args: []string{"prepare", "--repo", "ouroboros-terraform", "--index", "1", "--worktree-root", root + "/control-plane-worktrees", "--state-root", root + "/.devkit/native-agents", "--workspace-root", root + "/control-plane-worktrees/terraform-ouro-1", "--gui-target-id", "terraform-ouro-1", "--format", "json"}}
	p, err := parsePlanArgs(ctx, false, false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.OverlayConfig{}
	cfg.Defaults.Agents = 1
	cfg.Defaults.Origin = terraformDeclaredOrigin
	p.opts.BaseBranch = "main"
	p.opts.BranchPrefix = "agent"
	return ctx, cfg, p
}
func TestTerraformSelectedPrepareAdmission(t *testing.T) {
	ctx, cfg, p := terraformSelection(t)
	opts, err := selectedTerraformWorktreeOptions(ctx, cfg, p)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Count != 1 || opts.Index != 1 || !opts.RequireSSHOrigin || opts.ReconstructSelected || opts.Origin != terraformDeclaredOrigin {
		t.Fatalf("wrong materialization: %#v", opts)
	}
	for _, kind := range []string{"target", "implicit index", "index", "count", "base branch", "branch prefix", "workspace", "worktree root", "state", "origin", "capacity"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cfg, p := terraformSelection(t)
			switch kind {
			case "target":
				p.opts.GUITargetID = "terraform-ouro-2"
			case "implicit index":
				p.indexSet = false
			case "index":
				p.opts.Index = 2
			case "count":
				p.count = 1
			case "base branch":
				p.opts.BaseBranch = "foreign"
			case "branch prefix":
				p.opts.BranchPrefix = "foreign"
			case "workspace":
				p.opts.WorkspaceRoot += "/../terraform-ouro-1"
			case "worktree root":
				p.opts.WorktreeRoot = "/tmp/arbitrary"
			case "state":
				p.opts.StateRoot = "/tmp/arbitrary"
			case "origin":
				cfg.Defaults.Origin = "https://github.com/Divine-Shadow/ouroboros-terraform.git"
			case "capacity":
				cfg.Defaults.Agents = 2
			}
			if _, err := selectedTerraformWorktreeOptions(ctx, cfg, p); err == nil {
				t.Fatal("accepted ambiguous acquisition")
			}
		})
	}
}

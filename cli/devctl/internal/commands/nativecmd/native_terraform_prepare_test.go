package nativecmd

import (
	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/devkitpaths"
	"devkit/cli/devctl/internal/gitauthority"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	wtx "devkit/cli/devctl/internal/worktrees"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// Real local bare Git is an injected transport fixture, not production origin
// authority. It exercises acquisition -> native plan -> actual launch builder.
func TestTerraformAcquisitionPlanLaunchConsequences(t *testing.T) {
	root := t.TempDir()
	dev := filepath.Join(root, "dev")
	repo := "ouroboros-terraform"
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	restore := gitauthority.SetExecutableForTesting(git)
	defer restore()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command(git, args...)
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		b, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, b)
		}
	}
	primary := filepath.Join(dev, repo)
	origin := filepath.Join(root, "remote.git")
	if err := os.MkdirAll(primary, 0700); err != nil {
		t.Fatal(err)
	}
	run("init", "--bare", origin)
	run("-C", primary, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(primary, "proof.txt"), []byte("canonical fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("-C", primary, "add", "proof.txt")
	run("-C", primary, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-m", "fixture")
	run("-C", primary, "remote", "add", "origin", origin)
	run("-C", primary, "push", "origin", "main")
	wr := filepath.Join(dev, "control-plane-worktrees")
	workspace := filepath.Join(wr, "terraform-ouro-1")
	opts := wtx.NativeOptions{DevkitRoot: filepath.Join(dev, "devkit"), Repo: repo, Origin: origin, Index: 1, Count: 1, BaseBranch: "main", BranchPrefix: "agent", WorktreeRoot: wr, WorkspaceRoot: workspace}
	if err := wtx.SetupNative(opts); err != nil {
		t.Fatal(err)
	}
	plan, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{Paths: devkitpaths.Paths{Root: opts.DevkitRoot}, Project: repo, Repo: repo, Index: 1, WorktreeRoot: wr, WorkspaceRoot: workspace, IsolationProfile: nativeplan.IsolationProfileWorkspaceEgress, EgressAllowlist: filepath.Join(root, "allowlist.txt")})
	if err != nil {
		t.Fatal(err)
	}
	command, err := launch.BuildBubblewrap(plan, []string{"codex", "app-server"})
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(command.Args, " ")
	if plan.Agent.HostWorktree != filepath.Join(workspace, repo) || plan.Agent.SandboxWorktree != "/workspaces/dev/"+repo || !strings.Contains(argv, workspace+" /workspaces/dev") || !strings.Contains(argv, "codex app-server") {
		t.Fatalf("wrong acquisition/launch geometry: %#v %s", plan.Agent, argv)
	}
	if strings.Contains(argv, wr+" /workspaces/dev") || strings.Contains(argv, "terraform-ouro-2") {
		t.Fatalf("broad or sibling launch: %s", argv)
	}
}

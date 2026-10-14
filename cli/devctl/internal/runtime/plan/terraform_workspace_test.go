package plan

import (
	"devkit/cli/devctl/internal/devkitpaths"
	"path/filepath"
	"testing"
)

func terraformWorkspaceOptions(t *testing.T) BuildOptions {
	root := t.TempDir()
	wr := filepath.Join(root, "control-plane-worktrees")
	return BuildOptions{Paths: devkitpaths.Paths{Root: filepath.Join(root, "devkit")},
		Project: "ouroboros-terraform", Repo: "ouroboros-terraform", Index: 1, WorktreeRoot: wr,
		WorkspaceRoot: filepath.Join(wr, "terraform-ouro-1"), IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist: filepath.Join(root, "allowlist.txt")}
}
func TestTerraformDeclaredNativePlanGeometry(t *testing.T) {
	opts := terraformWorkspaceOptions(t)
	p, err := Build(opts)
	if err != nil {
		t.Fatalf("declared TF1 native plan unavailable: %v", err)
	}
	if p.Agent.HostWorktree != filepath.Join(opts.WorkspaceRoot, opts.Repo) || p.Agent.SandboxWorktree != "/workspaces/dev/ouroboros-terraform" || p.Agent.SandboxHome != "/agent-state/ouroboros-terraform-agent1/home" {
		t.Fatalf("wrong geometry: %#v", p.Agent)
	}
	if !hasExactBind(p.Binds, Bind{Source: opts.WorkspaceRoot, Target: "/workspaces/dev", Mode: "rw", Required: true}) || p.WindowsMountsVisible {
		t.Fatalf("wrong isolated mounts: %#v", p.Binds)
	}
	opts.WorkspaceRoot = ""
	legacy, err := Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Agent.HostWorktree != filepath.Join(opts.WorktreeRoot, "agent1", opts.Repo) {
		t.Fatal("generic lifecycle changed")
	}
}
func TestTerraformNativePlanRejectsOtherLanes(t *testing.T) {
	for _, kind := range []string{"TF2", "legacy", "other repo", "no isolation", "traversal", "arbitrary root"} {
		t.Run(kind, func(t *testing.T) {
			opts := terraformWorkspaceOptions(t)
			switch kind {
			case "TF2":
				opts.Index = 2
				opts.WorkspaceRoot = filepath.Join(opts.WorktreeRoot, "terraform-ouro-2")
			case "legacy":
				opts.WorkspaceRoot = filepath.Join(opts.WorktreeRoot, "agent1")
			case "other repo":
				opts.Repo = "foreign"
			case "no isolation":
				opts.IsolationProfile = ""
			case "arbitrary root":
				opts.WorktreeRoot = filepath.Join(filepath.Dir(opts.Paths.Root), "arbitrary")
				opts.WorkspaceRoot = filepath.Join(opts.WorktreeRoot, "terraform-ouro-1")
			case "traversal":
				opts.WorkspaceRoot += "/../terraform-ouro-1"
			}
			if _, err := Build(opts); err == nil {
				t.Fatal("accepted incompatible geometry")
			}
		})
	}
}

package nativecmd

import (
	"fmt"
	"os"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	wtx "devkit/cli/devctl/internal/worktrees"
)

// handleSelectedDevAllPrepare materializes one source-declared lane without
// rewriting the shared prefix manifest or iterating over other lanes.
func handleSelectedDevAllPrepare(ctx *cmdregistry.Context, cfg config.OverlayConfig, parsed planArgs) error {
	for _, arg := range ctx.Args {
		switch arg {
		case "--count", "--workspace-root", "--worktree-root", "--state-root", "--host-root":
			return fmt.Errorf("selected dev-all native prepare derives roots and capacity from source configuration")
		}
	}
	if parsed.count != 0 || parsed.opts.GUITargetID != "" || parsed.opts.Index > cfg.Defaults.Agents {
		return fmt.Errorf("selected dev-all native prepare requires one declared index without caller target or capacity")
	}
	p, err := nativeplan.BuildDevAll(parsed.opts)
	if err != nil {
		return err
	}
	dryRun := ctx.DryRun || parsed.dryRun
	worktreeOpts := wtx.NativeOptions{DevkitRoot: ctx.Paths.Root, Repo: parsed.opts.Repo, Origin: cfg.Defaults.Origin,
		Index: parsed.opts.Index, Count: cfg.Defaults.Agents, BaseBranch: parsed.opts.BaseBranch,
		BranchPrefix: parsed.opts.BranchPrefix, WorktreeRoot: parsed.opts.WorktreeRoot,
		RequireSSHOrigin: parsed.opts.Repo == "ouroboros-ide"}
	if err := prepareNativeGitBootstrapAndWorktrees(p, worktreeOpts, dryRun); err != nil {
		return err
	}
	p, err = nativeplan.BuildDevAll(parsed.opts)
	if err != nil {
		return err
	}
	if err := prepareWithManagedEgressProxy(p, nativePlanBrokerConfig(ctx, cfg, parsed.opts), dryRun, launch.Prepare); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "repo: %s\ncount: 1\nagent%d: worktree=%s home=%s state=%s\n", parsed.opts.Repo, p.Agent.ID.Index, p.Agent.HostWorktree, p.Agent.HostHome, p.Agent.StateRoot)
	return nil
}

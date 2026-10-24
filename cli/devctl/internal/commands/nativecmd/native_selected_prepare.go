package nativecmd

import (
	"fmt"
	"os"
	"path/filepath"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	wtx "devkit/cli/devctl/internal/worktrees"
)

// handleSelectedDevAllPrepare materializes one source-declared lane without
// rewriting the shared prefix manifest or iterating over other lanes.
func handleSelectedDevAllPrepare(ctx *cmdregistry.Context, cfg config.OverlayConfig, parsed planArgs) error {
	p, worktreeOpts, err := planSelectedDevAllPrepare(ctx, cfg, parsed)
	if err != nil {
		return err
	}
	dryRun := ctx.DryRun || parsed.dryRun
	claim, err := claimFreshSelectedPreparation(p, worktreeOpts, dryRun)
	if err != nil {
		return err
	}
	defer claim.close()
	if err := claim.runPhase(func() error { return prepareNativeGitBootstrapAndWorktrees(p, worktreeOpts, dryRun) }); err != nil {
		return err
	}
	if err := claim.runPhase(func() error {
		preparedOptions := parsed.opts
		preparedOptions.WorkspaceRoot = p.HostWorkspaceRoot
		p, err = nativeplan.BuildDevAll(preparedOptions)
		if err != nil {
			return err
		}
		return prepareWithManagedEgressProxy(p, nativePlanBrokerConfig(ctx, cfg, parsed.opts), dryRun, launch.PrepareFresh)
	}); err != nil {
		return err
	}
	if err := claim.complete(p); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "repo: %s\ncount: 1\nagent%d: worktree=%s home=%s state=%s\n", parsed.opts.Repo, p.Agent.ID.Index, p.Agent.HostWorktree, p.Agent.HostHome, p.Agent.StateRoot)
	return nil
}

// Keep selection and its worktree acquisition bound to the same admitted plan.
// This phase has no effects; callers cannot supply geometry or capacity.
func planSelectedDevAllPrepare(ctx *cmdregistry.Context, cfg config.OverlayConfig, parsed planArgs) (nativeplan.Plan, wtx.NativeOptions, error) {
	for _, arg := range ctx.Args {
		switch arg {
		case "--count", "--workspace-root", "--worktree-root", "--state-root", "--host-root", "--worktree-container-root", "--state-container-root":
			return nativeplan.Plan{}, wtx.NativeOptions{}, fmt.Errorf("selected dev-all native prepare derives roots and capacity from source configuration")
		}
	}
	capacity := cfg.Defaults.Agents
	admitted := parsed.opts.Index > 0 && parsed.opts.Index <= capacity
	if cfg.Defaults.SelectedAgents != nil {
		admitted = false
		seen := map[int]bool{}
		for _, index := range cfg.Defaults.SelectedAgents {
			if index < 1 || seen[index] {
				return nativeplan.Plan{}, wtx.NativeOptions{}, fmt.Errorf("selected native construction indexes must be unique positive integers")
			}
			seen[index] = true
			if index > capacity {
				capacity = index
			}
			if index == parsed.opts.Index {
				admitted = true
			}
		}
	}
	if parsed.count != 0 || parsed.opts.GUITargetID != "" || !admitted {
		return nativeplan.Plan{}, wtx.NativeOptions{}, fmt.Errorf("selected dev-all native prepare requires one declared index without caller target or capacity")
	}
	// A selected lineage projects its own source-owned workspace at /workspaces/dev,
	// matching ordinary GUI execution and keeping runtime context lane-local.
	paths, err := nativeagent.ResolvePaths(nativeagent.PathConfig{DevkitRoot: ctx.Paths.Root,
		HostRoot: cfg.Native.HostRoot, Project: ctx.Project, Repo: parsed.opts.Repo, Index: parsed.opts.Index,
		WorktreeRoot: parsed.opts.WorktreeRoot, DedicatedWorktree: true})
	if err != nil {
		return nativeplan.Plan{}, wtx.NativeOptions{}, err
	}
	parsed.opts.WorkspaceRoot = filepath.Dir(paths.HostWorktree)
	p, err := nativeplan.BuildDevAll(parsed.opts)
	if err != nil {
		return nativeplan.Plan{}, wtx.NativeOptions{}, err
	}
	worktreeOpts := wtx.NativeOptions{DevkitRoot: ctx.Paths.Root, Repo: parsed.opts.Repo, Origin: cfg.Defaults.Origin,
		Index: parsed.opts.Index, Count: capacity, BaseBranch: parsed.opts.BaseBranch,
		BranchPrefix: parsed.opts.BranchPrefix, WorktreeRoot: parsed.opts.WorktreeRoot,
		RequireSSHOrigin: parsed.opts.Repo == "ouroboros-ide"}
	return p, worktreeOpts, nil
}

// Prefix overrides cannot absorb a separately declared selected lineage or
// its excluded numeric holes. The unchanged prefix remains independently usable.
func validateSelectedConstructionPrefix(project string, cfg config.OverlayConfig, count int) error {
	if project != "dev-all" || len(cfg.Defaults.SelectedAgents) == 0 {
		return nil
	}
	if count > cfg.Defaults.Agents {
		return fmt.Errorf("prefix capacity cannot include selected construction or its undeclared holes")
	}
	for _, index := range cfg.Defaults.SelectedAgents {
		if index < 1 || index <= count {
			return fmt.Errorf("prefix capacity overlaps selected construction")
		}
	}
	return nil
}

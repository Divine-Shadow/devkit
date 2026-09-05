package nativecmd

import (
	"encoding/json"
	"fmt"
	"os"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	wtx "devkit/cli/devctl/internal/worktrees"
)

// selectedSharedPowerWorktreeOptions adapts the existing native preparation
// command to Fleet's exact isolated lane. It does not change prefix lifecycle
// semantics or select, rewrite, or migrate any historical agent home.
func selectedSharedPowerWorktreeOptions(ctx *cmdregistry.Context, cfg config.OverlayConfig, parsed planArgs) (wtx.NativeOptions, error) {
	const project = "pokeemerald-expansion-shared-power"
	index := parsed.opts.Index
	if ctx.Project != project || parsed.opts.Repo != project ||
		!parsed.indexSet || (index != 1 && index != 2) ||
		parsed.opts.GUITargetID != fmt.Sprintf("pokeemerald-agent%d", index) {
		return wtx.NativeOptions{}, fmt.Errorf("isolated native prepare requires the exact Shared Power project, repo, explicit index, and GUI target")
	}
	if cfg.Defaults.Agents < index {
		return wtx.NativeOptions{}, fmt.Errorf("selected Shared Power index exceeds source-declared capacity")
	}
	if parsed.count != 0 {
		return wtx.NativeOptions{}, fmt.Errorf("selected isolated native prepare rejects --count; only the selected index is prepared")
	}
	if parsed.format != "" && parsed.format != "text" && parsed.format != "json" {
		return wtx.NativeOptions{}, fmt.Errorf("--format must be text or json")
	}
	return wtx.NativeOptions{
		DevkitRoot:       ctx.Paths.Root,
		Repo:             parsed.opts.Repo,
		Origin:           cfg.Defaults.Origin,
		Index:            index,
		Count:            cfg.Defaults.Agents,
		BaseBranch:       parsed.opts.BaseBranch,
		BranchPrefix:     parsed.opts.BranchPrefix,
		WorktreeRoot:     parsed.opts.WorktreeRoot,
		WorkspaceRoot:    parsed.opts.WorkspaceRoot,
		RequireSSHOrigin: true,
	}, nil
}

type selectedSharedPowerPreparedAgent struct {
	Index            int    `json:"index"`
	HostWorktree     string `json:"host_worktree"`
	SandboxWorktree  string `json:"sandbox_worktree"`
	HostHome         string `json:"host_home"`
	SandboxHome      string `json:"sandbox_home"`
	StateRoot        string `json:"state_root"`
	SandboxStateRoot string `json:"sandbox_state_root"`
}

func handleSelectedSharedPowerPrepare(ctx *cmdregistry.Context, cfg config.OverlayConfig, parsed planArgs) error {
	worktreeOpts, err := selectedSharedPowerWorktreeOptions(ctx, cfg, parsed)
	if err != nil {
		return err
	}
	// Building the same plan as exec validates immutable GUI config selection
	// and exact lane geometry before any home or checkout is materialized.
	p, err := nativeplan.BuildDevAll(parsed.opts)
	if err != nil {
		return err
	}
	dryRun := ctx.DryRun || parsed.dryRun
	if err := prepareNativeGitBootstrapAndWorktrees(p, worktreeOpts, dryRun); err != nil {
		return err
	}
	// The newly created linked checkout now contributes its Git metadata.
	// Rebuild and validate the complete mount set before preparing the runtime.
	p, err = nativeplan.BuildDevAll(parsed.opts)
	if err != nil {
		return err
	}
	if err := prepareWithManagedEgressProxy(p, dryRun, launch.Prepare); err != nil {
		return err
	}
	// A selected GUI invocation cannot replace the shared prefix manifest.
	// Fleet consumes this selected receipt and exec repeats runtime preparation.
	out := struct {
		Repo   string                             `json:"repo"`
		Count  int                                `json:"count"`
		Agents []selectedSharedPowerPreparedAgent `json:"agents"`
	}{
		Repo: parsed.opts.Repo, Count: 1,
		Agents: []selectedSharedPowerPreparedAgent{{
			Index:        p.Agent.ID.Index,
			HostWorktree: p.Agent.HostWorktree, SandboxWorktree: p.Agent.SandboxWorktree,
			HostHome: p.Agent.HostHome, SandboxHome: p.Agent.SandboxHome,
			StateRoot: p.Agent.StateRoot, SandboxStateRoot: p.Agent.SandboxStateRoot,
		}},
	}
	if parsed.format == "json" {
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(data))
	} else {
		fmt.Fprintf(os.Stdout, "repo: %s\ncount: 1\nagent%d: worktree=%s home=%s state=%s\n",
			out.Repo, p.Agent.ID.Index, p.Agent.HostWorktree, p.Agent.HostHome, p.Agent.StateRoot)
	}
	return nil
}

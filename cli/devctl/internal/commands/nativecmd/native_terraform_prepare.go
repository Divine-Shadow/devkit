package nativecmd

import (
	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	wtx "devkit/cli/devctl/internal/worktrees"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const terraformDeclaredOrigin = "ssh://git@ssh.github.com:443/Divine-Shadow/ouroboros-terraform.git"

// The existing Fleet startup selects this fixed target. This adapter adds no
// arbitrary repo/origin/path, reset, or second Terraform lane authority.
func selectedTerraformWorktreeOptions(ctx *cmdregistry.Context, cfg config.OverlayConfig, parsed planArgs) (wtx.NativeOptions, error) {
	const root = "/home/bayesartre/dev"
	if ctx.Project != "ouroboros-terraform" || parsed.opts.Repo != "ouroboros-terraform" ||
		!parsed.indexSet || parsed.opts.Index != 1 || parsed.opts.GUITargetID != "terraform-ouro-1" ||
		cfg.Defaults.Agents != 1 || cfg.Defaults.Origin != terraformDeclaredOrigin ||
		parsed.opts.BaseBranch != "main" || parsed.opts.BranchPrefix != "agent" || parsed.count != 0 ||
		parsed.opts.WorktreeRoot != filepath.Join(root, "control-plane-worktrees") ||
		parsed.opts.WorkspaceRoot != filepath.Join(root, "control-plane-worktrees", "terraform-ouro-1") ||
		parsed.opts.StateRoot != filepath.Join(root, ".devkit", "native-agents") ||
		(parsed.format != "" && parsed.format != "text" && parsed.format != "json") {
		return wtx.NativeOptions{}, fmt.Errorf("Terraform preparation requires exact declared Agent1 target, origin, branch, capacity and geometry")
	}
	return wtx.NativeOptions{DevkitRoot: ctx.Paths.Root, Repo: parsed.opts.Repo, Origin: cfg.Defaults.Origin,
		Index: 1, Count: 1, BaseBranch: "main", BranchPrefix: "agent", WorktreeRoot: parsed.opts.WorktreeRoot,
		WorkspaceRoot: parsed.opts.WorkspaceRoot, RequireSSHOrigin: true}, nil
}

func handleSelectedTerraformPrepare(ctx *cmdregistry.Context, cfg config.OverlayConfig, parsed planArgs) error {
	if err := validateTerraformToolsSource(parsed.preparationPhase, ctx, cfg); err != nil {
		return err
	}
	opts, err := selectedTerraformWorktreeOptions(ctx, cfg, parsed)
	if err != nil {
		return err
	}
	p, err := nativeplan.BuildDevAll(parsed.opts)
	if err != nil {
		return err
	}
	if parsed.preparationPhase != "" {
		hostname, err := os.Hostname()
		if err != nil {
			return err
		}
		if err := launch.ValidateTerraformToolsPlan(p, hostname); err != nil {
			return err
		}
	}
	dryRun := ctx.DryRun || parsed.dryRun
	exists := false
	if !dryRun {
		exists, err = wtx.ValidateTerraformNativeCheckout(opts)
		if err != nil {
			return err
		}
	}
	if !exists {
		if err := prepareNativeGitBootstrapAndWorktrees(p, opts, dryRun); err != nil {
			return err
		}
	}
	p, err = nativeplan.BuildDevAll(parsed.opts)
	if err != nil {
		return err
	}
	if err := prepareWithManagedEgressProxy(p, nativePlanBrokerConfig(ctx, cfg, parsed.opts), dryRun, terraformPreparationCallback(parsed.preparationPhase)); err != nil {
		return err
	}
	if parsed.preparationPhase != "" && !dryRun {
		// Only immutable overlay runtime checks; repo/provider readiness is not earned.
		runtimeChecks, err := runtimeChecksFor(ctx)
		if err != nil {
			return err
		}
		report := runReadinessReportPrepared(p, nativePlanBrokerConfig(ctx, cfg, parsed.opts), runtimeChecks, nil, terraformPreparationCallback(parsed.preparationPhase))
		if !report.RuntimeReady() {
			return fmt.Errorf("Terraform tools startup verification failed: %v", report.FailedChecks())
		}
	}
	out := struct {
		PreparationPhase  string                             `json:"preparation_phase,omitempty"`
		ProviderHydration string                             `json:"provider_hydration,omitempty"`
		ProviderReadiness string                             `json:"provider_readiness,omitempty"`
		Repo              string                             `json:"repo"`
		Count             int                                `json:"count"`
		Agents            []selectedSharedPowerPreparedAgent `json:"agents"`
	}{
		PreparationPhase: parsed.preparationPhase, ProviderHydration: terraformToolsProviderHydration(parsed.preparationPhase), ProviderReadiness: terraformToolsProviderReadiness(parsed.preparationPhase),
		Repo: opts.Repo, Count: 1, Agents: []selectedSharedPowerPreparedAgent{{Index: 1,
			HostWorktree: p.Agent.HostWorktree, SandboxWorktree: p.Agent.SandboxWorktree,
			HostHome: p.Agent.HostHome, SandboxHome: p.Agent.SandboxHome,
			StateRoot: p.Agent.StateRoot, SandboxStateRoot: p.Agent.SandboxStateRoot}}}
	if parsed.format == "json" {
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(b))
	} else {
		fmt.Fprintf(os.Stdout, "repo: %s\ncount: 1\nagent1: worktree=%s home=%s\n", out.Repo, p.Agent.HostWorktree, p.Agent.HostHome)
	}
	return nil
}

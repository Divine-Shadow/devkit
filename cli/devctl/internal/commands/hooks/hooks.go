package hooks

import (
	"fmt"
	"strings"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	runner "devkit/cli/devctl/internal/runner"
)

// Register adds warm/maintain commands to the registry.
func Register(r *cmdregistry.Registry) {
	r.Register("warm", func(ctx *cmdregistry.Context) error { return handleHook(ctx, true) })
	r.Register("maintain", func(ctx *cmdregistry.Context) error { return handleHook(ctx, false) })
}

func handleHook(ctx *cmdregistry.Context, warm bool) error {
	project := strings.TrimSpace(ctx.Project)
	if project == "" {
		return fmt.Errorf("-p <project> is required")
	}
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, project)
	if err != nil {
		return err
	}
	script := cfg.Hooks.Maintain
	label := "maintain"
	if warm {
		script = cfg.Hooks.Warm
		label = "warm"
	}
	if strings.TrimSpace(script) == "" {
		fmt.Printf("No %s hook defined\n", label)
		return nil
	}
	if !config.HasRuntimeFlake(cfg) {
		return fmt.Errorf("%s hook requires an overlay with runtime.flake", label)
	}
	exe := strings.TrimSpace(ctx.Exe)
	if exe == "" {
		exe = "devkit"
	}
	repo := strings.TrimSpace(cfg.Defaults.Repo)
	if repo == "" {
		if project == "dev-all" {
			repo = "ouroboros-ide"
		} else {
			repo = project
		}
	}
	runner.Host(ctx.DryRun, exe, "-p", project, "exec", "1", "--repo", repo, "--", "bash", "-lc", script)
	return nil
}

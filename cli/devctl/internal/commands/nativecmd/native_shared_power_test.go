package nativecmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/devkitpaths"
)

func TestSharedPowerSelectedPrepareMatchesFleetExecGeometry(t *testing.T) {
	const project = "pokeemerald-expansion-shared-power"
	for _, index := range []int{1, 2} {
		t.Run(fmt.Sprintf("agent%d", index), func(t *testing.T) {
			root := t.TempDir()
			worktreeRoot := filepath.Join(root, "project-worktrees")
			workspaceRoot := filepath.Join(worktreeRoot, fmt.Sprintf("pokeemerald-agent%d", index))
			stateRoot := filepath.Join(root, ".devkit", "native-agents")
			target := fmt.Sprintf("pokeemerald-agent%d", index)
			ctx := &cmdregistry.Context{
				Project: project, Paths: devkitpaths.Paths{Root: filepath.Join(root, "devkit")},
				Args: []string{"prepare", "--repo", project, "--index", fmt.Sprint(index),
					"--worktree-root", worktreeRoot, "--state-root", stateRoot,
					"--workspace-root", workspaceRoot, "--gui-target-id", target, "--format", "json"},
			}
			parsed, err := parsePlanArgs(ctx, false, false)
			if err != nil {
				t.Fatal(err)
			}
			cfg := config.OverlayConfig{}
			cfg.Defaults.Agents = 2
			cfg.Defaults.Origin = "ssh://git@ssh.github.com:443/Divine-Shadow/pokeemerald-pret.git"
			cfg.Native.HostRoot = root
			cfg.Native.AgentStatePrefix = "pokeemerald"
			if err := applyNativeConfigDefaults(ctx, cfg, &parsed.opts); err != nil {
				t.Fatal(err)
			}
			opts, err := selectedSharedPowerWorktreeOptions(ctx, cfg, parsed)
			if err != nil {
				t.Fatal(err)
			}
			if opts.Index != index || opts.Count != 2 || opts.WorkspaceRoot != workspaceRoot ||
				opts.WorktreeRoot != worktreeRoot || !opts.RequireSSHOrigin || opts.ReconstructSelected {
				t.Fatalf("wrong selected materialization: %#v", opts)
			}
			execArgs, err := parseTopExecArgs(&cmdregistry.Context{Args: []string{fmt.Sprint(index),
				"--repo", project, "--worktree-root", worktreeRoot, "--agent-state-root", stateRoot,
				"--workspace-root", workspaceRoot, "--gui-target-id", target, "--", "git", "status"}}, false)
			if err != nil {
				t.Fatal(err)
			}
			if execArgs.index != parsed.opts.Index || execArgs.workspaceRoot != parsed.opts.WorkspaceRoot ||
				execArgs.worktreeRoot != parsed.opts.WorktreeRoot || execArgs.agentStateRoot != parsed.opts.StateRoot ||
				execArgs.guiTargetID != parsed.opts.GUITargetID {
				t.Fatalf("prepare and exec geometry disagree: prepare=%#v exec=%#v", parsed.opts, execArgs)
			}
		})
	}
}

func TestSharedPowerSelectedPrepareRejectsAmbiguousSelectionBeforeEffects(t *testing.T) {
	const project = "pokeemerald-expansion-shared-power"
	ctx := &cmdregistry.Context{Project: project, Args: []string{"prepare", "--repo", project,
		"--index", "2", "--workspace-root", "/dev/project-worktrees/pokeemerald-agent2",
		"--gui-target-id", "pokeemerald-agent2", "--format", "json"}}
	base, err := parsePlanArgs(ctx, false, false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.OverlayConfig{}
	cfg.Defaults.Agents = 2
	for name, mutate := range map[string]func(*planArgs){
		"count":              func(p *planArgs) { p.count = 2 },
		"implicit index":     func(p *planArgs) { p.indexSet = false },
		"other target":       func(p *planArgs) { p.opts.GUITargetID = "pokeemerald-agent1" },
		"other repo":         func(p *planArgs) { p.opts.Repo = "pokeemerald" },
		"other index":        func(p *planArgs) { p.opts.Index = 3 },
		"unsupported format": func(p *planArgs) { p.format = "yaml" },
	} {
		t.Run(name, func(t *testing.T) {
			parsed := base
			mutate(&parsed)
			if _, err := selectedSharedPowerWorktreeOptions(ctx, cfg, parsed); err == nil {
				t.Fatalf("accepted %s", name)
			}
		})
	}
	cfg.Defaults.Agents = 1
	if _, err := selectedSharedPowerWorktreeOptions(ctx, cfg, base); err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("accepted index beyond declared capacity: %v", err)
	}
	legacy, err := parsePlanArgs(&cmdregistry.Context{Args: []string{"prepare", "--count", "2"}}, false, false)
	if err != nil || legacy.indexSet || legacy.count != 2 || legacy.opts.WorkspaceRoot != "" {
		t.Fatalf("legacy parser changed: %#v, %v", legacy, err)
	}
}

func TestSharedPowerAdapterDoesNotInterceptManagementPrepare(t *testing.T) {
	root := devkitSourceRootForNativeTest(t)
	worktreeRoot := filepath.Join(t.TempDir(), "control-plane-worktrees")
	ctx := &cmdregistry.Context{
		Project: "dev-workspace", DryRun: true,
		Paths: devkitpaths.Paths{Root: root, OverlayPaths: []string{filepath.Join(root, "overlays")}},
		Args: []string{"prepare", "--repo", "shadow-throne-management", "--count", "1",
			"--worktree-root", worktreeRoot, "--workspace-root", filepath.Join(worktreeRoot, "wrong-lane")},
	}
	// The existing Management planner owns this error. An overbroad dispatch
	// to selected Shared Power preparation would reject the project first.
	err := handlePrepare(ctx)
	if err == nil || !strings.Contains(err.Error(), "must be the exact selected lane root") {
		t.Fatalf("Management prepare no longer reaches its existing planner: %v", err)
	}
}

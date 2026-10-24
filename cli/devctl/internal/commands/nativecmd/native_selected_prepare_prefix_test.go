package nativecmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/devkitpaths"
)

func TestSelectedConstructionRejectsPrefixWritersBeforeAnyEffects(t *testing.T) {
	p, opts := freshSelectedFixture(t)
	claim, err := claimFreshSelectedPreparation(p, opts, false)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.close()
	overlay := filepath.Join(opts.DevkitRoot, "overlays", "dev-all")
	if err := os.MkdirAll(overlay, 0700); err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprintf("runtime:\n  flake: path:.#default\ndefaults:\n  agents: 3\n  selected_agents: [5]\n  repo: fixture\nnative:\n  host_root: %s\n  worktree_root: %s\n  state_root: %s\n",
		filepath.Dir(opts.DevkitRoot), opts.WorktreeRoot, filepath.Dir(p.Agent.StateRoot))
	if err := os.WriteFile(filepath.Join(overlay, "devkit.yaml"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := &cmdregistry.Context{Project: "dev-all", Paths: devkitpaths.Paths{Root: opts.DevkitRoot, OverlayPaths: []string{filepath.Dir(overlay)}}}
	ctx.Args = []string{"5"}
	if err := handleLifecycleScale(ctx); err == nil || !strings.Contains(err.Error(), "selected construction") {
		t.Fatalf("scale reached prefix effects: %v", err)
	}
	ctx.Args = []string{"prepare", "--count", "5", "--repo", "fixture"}
	if err := handlePrepare(ctx); err == nil || !strings.Contains(err.Error(), "selected construction") {
		t.Fatalf("prepare reached prefix effects: %v", err)
	}
	ctx.Args = []string{"prepare", "--count", "4", "--repo", "fixture"}
	if err := handlePrepare(ctx); err == nil || !strings.Contains(err.Error(), "undeclared holes") {
		t.Fatalf("protected hole admitted: %v", err)
	}
	cfg, _, count, _, _, err := lifecycleDefaults(ctx, lifecycleArgs{count: 3})
	if err != nil || count != 3 || cfg.Defaults.Agents != 3 {
		t.Fatalf("unchanged prefix disabled: %d %v", count, err)
	}
	for _, n := range []string{"agent1", "agent2", "agent3", "agent4"} {
		if _, err := os.Lstat(filepath.Join(opts.WorktreeRoot, n)); !os.IsNotExist(err) {
			t.Fatalf("prefix writer touched %s", n)
		}
	}
	if err := claim.verifyNamespace(); err != nil {
		t.Fatal(err)
	}
}

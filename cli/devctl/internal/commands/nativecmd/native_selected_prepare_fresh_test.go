package nativecmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/devkitpaths"
	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	wtx "devkit/cli/devctl/internal/worktrees"
)

func freshSelectedFixture(t *testing.T) (nativeplan.Plan, wtx.NativeOptions) {
	t.Helper()
	root := t.TempDir()
	trees := filepath.Join(root, "lanes")
	lane := filepath.Join(trees, "agent5")
	p := nativeplan.Plan{Agent: nativeagent.Spec{ID: nativeagent.ID{Project: "dev-all", Repo: "fixture", Index: 5},
		HostWorktree: filepath.Join(lane, "fixture"), HostHome: filepath.Join(lane, ".devhome-agent5"), StateRoot: filepath.Join(root, "state", "dev-all-agent5")}}
	return p, wtx.NativeOptions{DevkitRoot: filepath.Join(root, "devkit"), Repo: "fixture", Index: 5, Count: 5, WorktreeRoot: trees}
}

func TestFreshSelectedPreparationRefusesEveryOccupiedNamespace(t *testing.T) {
	for _, which := range []int{0, 1, 2} {
		t.Run(string(rune('a'+which)), func(t *testing.T) {
			p, opts := freshSelectedFixture(t)
			roots, err := freshPreparationPaths(p, opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(roots[which], 0700); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(roots[which], "old-runtime-context")
			if err := os.WriteFile(sentinel, []byte("protected"), 0600); err != nil {
				t.Fatal(err)
			}
			if claim, err := claimFreshSelectedPreparation(p, opts, false); err == nil {
				claim.close()
				t.Fatal("occupied namespace admitted")
			}
			b, err := os.ReadFile(sentinel)
			if err != nil || string(b) != "protected" {
				t.Fatal("prior state changed")
			}
			for i, root := range roots {
				if i != which {
					if _, err := os.Lstat(root); !os.IsNotExist(err) {
						t.Fatalf("mutated another selected root before refusal: %s", root)
					}
				}
			}
		})
	}
}

func TestFreshSelectedPreparationRejectsSymlinkParent(t *testing.T) {
	p, opts := freshSelectedFixture(t)
	foreign := t.TempDir()
	if err := os.Symlink(foreign, opts.WorktreeRoot); err != nil {
		t.Fatal(err)
	}
	if claim, err := claimFreshSelectedPreparation(p, opts, false); err == nil {
		claim.close()
		t.Fatal("symlink admitted")
	}
	children, err := os.ReadDir(foreign)
	if err != nil || len(children) != 0 {
		t.Fatal("followed symlink")
	}
}

func TestFreshSelectedPreparationClaimsOnceAndRetainsFailure(t *testing.T) {
	p, opts := freshSelectedFixture(t)
	claim, err := claimFreshSelectedPreparation(p, opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := claimFreshSelectedPreparation(p, opts, false); err == nil {
		other.close()
		t.Fatal("concurrent claim admitted")
	}
	claim.close()
	if other, err := claimFreshSelectedPreparation(p, opts, false); err == nil {
		other.close()
		t.Fatal("partial failed allocation silently reused")
	}
}

func TestFreshSelectedPreparationCompletesWithoutTouchingSurplusHistory(t *testing.T) {
	p, opts := freshSelectedFixture(t)
	neighbor := filepath.Join(opts.WorktreeRoot, "agent4")
	if err := os.MkdirAll(neighbor, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(neighbor, "opaque-old-task-database")
	if err := os.WriteFile(sentinel, []byte("old error; multiple historical tasks"), 0400); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(neighbor, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(neighbor, 0700)
	claim, err := claimFreshSelectedPreparation(p, opts, false)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.close()
	for _, dir := range []string{p.Agent.HostWorktree, p.Agent.HostHome, filepath.Join(claim.directories[2].Name(), "fixture.git")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := claim.complete(p); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(p.Agent.StateRoot, selectedPreparationReceiptName))
	if err != nil {
		t.Fatal(err)
	}
	var receipt selectedPreparationReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Schema != "devkit/selected-preparation/v1" || receipt.Index != 5 || len(receipt.Roots) != 6 {
		t.Fatalf("wrong completion: %+v", receipt)
	}
	if _, err := os.Lstat(filepath.Join(p.Agent.StateRoot, "home")); !os.IsNotExist(err) {
		t.Fatal("legacy home materialized")
	}
	if err := os.Chmod(neighbor, 0700); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(sentinel)
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("protected neighbor changed")
	}
	if err := claim.complete(p); err == nil {
		t.Fatal("completion overwritten")
	}
}

func TestSelectedConstructionSparseIndicesDoNotExpandPrefix(t *testing.T) {
	root := devkitSourceRootForNativeTest(t)
	ctx := &cmdregistry.Context{Project: "dev-all", Paths: devkitpaths.Paths{Root: root, OverlayPaths: []string{filepath.Join(root, "overlays")}}, Args: []string{"prepare", "--index", "5"}}
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, ctx.Project)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Defaults.SelectedAgents = []int{5}
	parsed, err := parsePlanArgs(ctx, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyNativeConfigDefaults(ctx, cfg, &parsed.opts); err != nil {
		t.Fatal(err)
	}
	_, acquisition, err := planSelectedDevAllPrepare(ctx, cfg, parsed)
	if err != nil {
		t.Fatal(err)
	}
	if acquisition.Index != 5 || acquisition.Count != 5 || cfg.Defaults.Agents != 3 {
		t.Fatal("sparse selection changed prefix")
	}
	parsed.opts.Index = 4
	if _, _, err := planSelectedDevAllPrepare(ctx, cfg, parsed); err == nil {
		t.Fatal("hole admitted")
	}
	cfg.Defaults.SelectedAgents = []int{5, 5}
	if _, _, err := planSelectedDevAllPrepare(ctx, cfg, parsed); err == nil {
		t.Fatal("duplicate selection admitted")
	}
}

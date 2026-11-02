package nativecmd

import (
	"encoding/json"
	"os"
	"os/exec"
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
	state := filepath.Join(root, "state")
	p := nativeplan.Plan{HostStateRoot: state, Agent: nativeagent.Spec{ID: nativeagent.ID{Project: "dev-all", Repo: "fixture", Index: 5},
		HostWorktree: filepath.Join(lane, "fixture"), HostHome: filepath.Join(nativeagent.PersistentHomesRoot(state), "dev-all-agent5", "fixture"), StateRoot: filepath.Join(state, "dev-all-agent5")}}
	return p, wtx.NativeOptions{DevkitRoot: filepath.Join(root, "devkit"), Repo: "fixture", Index: 5, Count: 5, WorktreeRoot: trees}
}

func TestFreshSelectedPreparationRetainsUsableSQLiteAndHomeIdentity(t *testing.T) {
	p, opts := freshSelectedFixture(t)
	if err := os.MkdirAll(filepath.Join(p.Agent.HostHome, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(p.Agent.HostHome, ".codex", "state_5.sqlite")
	query := func(sql string) string {
		t.Helper()
		out, err := exec.Command("sqlite3", database, sql).CombinedOutput()
		if err != nil {
			t.Fatalf("SQLite continuity fixture: %v %s", err, out)
		}
		return string(out)
	}
	query("CREATE TABLE continuity(value TEXT); INSERT INTO continuity VALUES('retained goal');")
	before, err := os.Stat(database)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := claimFreshSelectedPreparation(p, opts, false)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.close()
	for _, dir := range []string{p.Agent.HostWorktree, filepath.Join(claim.directories[2].Name(), "fixture.git")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := claim.complete(p); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(database)
	if err != nil || !os.SameFile(before, after) || query("SELECT value FROM continuity;") != "retained goal\n" {
		t.Fatal("fresh execution claim lost existing usable data")
	}
	query("INSERT INTO continuity VALUES('later update');")
	if query("SELECT count(*) FROM continuity;") != "2\n" {
		t.Fatal("retained database cannot accept later updates")
	}
	if err := os.Rename(p.Agent.HostHome, p.Agent.HostHome+"-retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p.Agent.HostHome, 0700); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := claim.runPhase(func() error { called = true; return nil }); err == nil || called {
		t.Fatal("retained home replacement was admitted")
	}
}

func TestFreshSelectedPreparationRetainsUnownedAppServerEndpoint(t *testing.T) {
	p, opts := freshSelectedFixture(t)
	socket := filepath.Join(p.Agent.HostHome, ".codex", "a5-app.sock")
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(socket, []byte("unclassified endpoint"), 0600); err != nil {
		t.Fatal(err)
	}
	if claim, err := claimFreshSelectedPreparation(p, opts, false); err == nil {
		claim.close()
		t.Fatal("endpoint admitted without owning lifecycle")
	}
	data, err := os.ReadFile(socket)
	if err != nil || string(data) != "unclassified endpoint" {
		t.Fatal("unowned endpoint changed")
	}
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

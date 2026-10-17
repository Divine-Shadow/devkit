package nativecmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	nativeagent "devkit/cli/devctl/internal/runtime/agent"
)

func custodyObservationFixture(t *testing.T) (nativeSlotProcessIdentity, string, string) {
	t.Helper()
	physicalProcRootFixture(t)
	id := physicalSlotFixture(t, 2)
	view, home := physicalViewFixture(t, id, "/workspaces/dev/ouroboros-ide")
	proc := writePhysicalProc(t, 801, 71, view, map[string]string{"DEVKIT_NATIVE_AGENT": "2", "HOME": home, "SECRET": "private-token-sentinel"}, id.hostWorktree)
	before := nativeCustodyBoot
	nativeCustodyBoot = func() string { return "8582cb5f-ab03-4be1-84ca-55a65be679d7" }
	t.Cleanup(func() { nativeCustodyBoot = before })
	for _, root := range []string{proc, filepath.Join(nativeSlotProcRoot, "self")} {
		if err := os.MkdirAll(filepath.Join(root, "ns"), 0700); err != nil {
			t.Fatal(err)
		}
		for _, ns := range []string{"pid", "mnt", "user"} {
			target := filepath.Join(root, ns+":[123]")
			if err := os.WriteFile(target, nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(ns+":[123]", filepath.Join(root, "ns", ns)); err != nil {
				t.Fatal(err)
			} // symbolic fixture link value is namespace identity
			if err := os.Symlink(target, filepath.Join(root, "ns", ns+":[123]")); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Namespace links name fixture files relative to ns/ while retaining kernel-shaped readlink values.
	if err := os.WriteFile(filepath.Join(proc, "status"), []byte("Uid:\t1000\t1000\t1000\t1000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return id, proc, view
}
func custodySpec(id nativeSlotProcessIdentity) nativeagent.Spec {
	return nativeagent.Spec{ID: nativeagent.ID{Index: id.index}, HostHome: id.hostHome, HostWorktree: id.hostWorktree, StateRoot: id.stateRoot, SandboxStateRoot: id.sandboxState}
}
func TestCustodyObservationExposesExactStateAliasWithoutSecretsOrMutation(t *testing.T) {
	for _, kind := range []string{"match", "sandboxState", "stateRoot", "absent"} {
		t.Run(kind, func(t *testing.T) {
			id, _, view := custodyObservationFixture(t)
			other := t.TempDir()
			if kind == "sandboxState" || kind == "absent" {
				if err := os.Remove(view + id.sandboxState); err != nil {
					t.Fatal(err)
				}
				if kind != "absent" {
					mapPhysicalView(t, view, id.sandboxState, other)
				}
			}
			if kind == "stateRoot" {
				mapPhysicalView(t, view, id.stateRoot, other)
			}
			r := observeNativeSlotCustody(custodySpec(id))
			if len(r.Observations) != 1 {
				t.Fatalf("missing witness: %+v", r)
			}
			w := r.Observations[0]
			if w.Status != "stable" || !w.HomeMatches || !w.WorktreeMatches || w.PID != 801 || w.Start != "100801" {
				t.Fatalf("identity: %+v", w)
			}
			if kind == "sandboxState" || kind == "stateRoot" {
				found := false
				for _, a := range w.Aliases {
					if a.Kind == kind && !a.Same && a.Observed != nil && a.Expected != nil && a.Observed.Inode != a.Expected.Inode {
						found = true
					}
				}
				if !found {
					t.Fatalf("wrong alias/objects: %+v", w)
				}
			}
			b, e := json.Marshal(r)
			if e != nil || strings.Contains(string(b), "private-token-sentinel") || strings.Contains(string(b), "HOME") || strings.Contains(string(b), "cmdline") {
				t.Fatalf("private export: %s %v", b, e)
			}
			if _, e := os.Stat(id.hostHome); e != nil {
				t.Fatal("observation changed home")
			}
		})
	}
}
func TestCustodyObservationIdentityAndObjectChangesRemainUnstable(t *testing.T) {
	for _, kind := range []string{"start", "parent", "boot", "namespace", "state-object", "unreadable-namespace"} {
		t.Run(kind, func(t *testing.T) {
			id, proc, view := custodyObservationFixture(t)
			saved := nativeCustodyBeforeRevalidate
			t.Cleanup(func() { nativeCustodyBeforeRevalidate = saved })
			nativeCustodyBeforeRevalidate = func() {
				switch kind {
				case "start", "parent":
					b, _ := os.ReadFile(filepath.Join(proc, "stat"))
					old, new := "100801", "200801"
					if kind == "parent" {
						old, new = "S 71", "S 99"
					}
					_ = os.WriteFile(filepath.Join(proc, "stat"), []byte(strings.ReplaceAll(string(b), old, new)), 0600)
				case "boot":
					nativeCustodyBoot = func() string { return "different" }
				case "namespace", "unreadable-namespace":
					_ = os.Remove(filepath.Join(proc, "ns", "mnt"))
					if kind == "namespace" {
						_ = os.Symlink("mnt:[456]", filepath.Join(proc, "ns", "mnt"))
					}
				case "state-object":
					_ = os.Remove(view + id.sandboxState)
					mapPhysicalView(t, view, id.sandboxState, t.TempDir())
				}
			}
			r := observeNativeSlotCustody(custodySpec(id))
			if len(r.Observations) != 1 || r.Observations[0].Status != "unstable" {
				t.Fatalf("race became stable: %+v", r)
			}
		})
	}
}
func TestCustodyObservationRejectsCallerGeometryAndNonPlanUse(t *testing.T) {
	base := []string{"plan", "--repo", "ouroboros-ide", "--index", "2", "--gui-target-id", "shadow-throne-local-2", "--process-custody-observation", "--format", "json"}
	if e := validateCustodyObservationPlanArgs(base); e != nil {
		t.Fatal(e)
	}
	for _, extra := range [][]string{{"--state-root", "/tmp/foreign"}, {"--flake", "foreign"}, {"--index", "3"}, {"--dry-run"}, {"--workspace-root", "/tmp/foreign"}} {
		if e := validateCustodyObservationPlanArgs(append(append([]string{}, base...), extra...)); e == nil {
			t.Fatalf("caller override admitted: %v", extra)
		}
	}
}

func TestCustodyObservationAbsentStateAppearanceCannotBecomeStable(t *testing.T) {
	for _, kind := range []string{"host-state", "sandbox-alias"} {
		t.Run(kind, func(t *testing.T) {
			id, _, view := custodyObservationFixture(t)
			foreign := t.TempDir()
			if kind == "host-state" {
				if err := os.Remove(id.stateRoot); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(view + id.sandboxState); err != nil {
					t.Fatal(err)
				}
			}
			saved := nativeCustodyBeforeRevalidate
			t.Cleanup(func() { nativeCustodyBeforeRevalidate = saved })
			nativeCustodyBeforeRevalidate = func() {
				if kind == "host-state" {
					if err := os.Mkdir(id.stateRoot, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(view + id.sandboxState); err != nil {
						t.Fatal(err)
					}
					mapPhysicalView(t, view, id.sandboxState, foreign)
				} else {
					mapPhysicalView(t, view, id.sandboxState, foreign)
				}
			}
			r := observeNativeSlotCustody(custodySpec(id))
			if len(r.Observations) != 1 || r.Observations[0].Status != "unstable" {
				t.Fatalf("appearance became stable: %+v", r)
			}
		})
	}
}

func TestCustodyObservationSiblingGeometryConsumedAbsenceAppearingIsUnstable(t *testing.T) {
	for _, kind := range []string{"observed-home", "observed-worktree", "host-worktree"} {
		t.Run(kind, func(t *testing.T) {
			id, proc, _ := custodyObservationFixture(t)
			id.hostHome = filepath.Join(filepath.Dir(id.hostWorktree), ".devhome-agent2")
			if err := os.Mkdir(id.hostHome, 0700); err != nil {
				t.Fatal(err)
			}
			alias := "/workspaces/dev/ouroboros-ide"
			view, home := physicalViewFixture(t, id, alias)
			if err := os.Remove(filepath.Join(proc, "root")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(view, filepath.Join(proc, "root")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(proc, "environ"), []byte("DEVKIT_NATIVE_AGENT=2\x00HOME="+home+"\x00"), 0600); err != nil {
				t.Fatal(err)
			}
			control := observeNativeSlotCustody(custodySpec(id))
			if len(control.Observations) != 1 || control.Observations[0].Status != "stable" || control.Observations[0].Predicate != "match" || !control.Observations[0].HomeMatches || !control.Observations[0].WorktreeMatches {
				t.Fatalf("sibling control not stable: %+v", control)
			}
			switch kind {
			case "observed-home":
				if err := os.Remove(view + home); err != nil {
					t.Fatal(err)
				}
			case "observed-worktree":
				if err := os.Remove(view + alias); err != nil {
					t.Fatal(err)
				}
			case "host-worktree":
				if err := os.Rename(id.hostWorktree, id.hostWorktree+"-held"); err != nil {
					t.Fatal(err)
				}
			}
			saved := nativeCustodyBeforeRevalidate
			t.Cleanup(func() { nativeCustodyBeforeRevalidate = saved })
			nativeCustodyBeforeRevalidate = func() {
				switch kind {
				case "observed-home":
					mapPhysicalView(t, view, home, id.hostHome)
				case "observed-worktree":
					mapPhysicalView(t, view, alias, id.hostWorktree)
				case "host-worktree":
					if err := os.Rename(id.hostWorktree+"-held", id.hostWorktree); err != nil {
						t.Fatal(err)
					}
				}
			}
			r := observeNativeSlotCustody(custodySpec(id))
			if len(r.Observations) != 1 || r.Observations[0].Status != "unstable" {
				t.Fatalf("appearing sibling alias became stable: %+v", r)
			}
		})
	}
}

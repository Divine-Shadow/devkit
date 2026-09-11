package nativecmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func physicalSlotFixture(t *testing.T, index int) nativeSlotProcessIdentity {
	t.Helper()
	root := t.TempDir()
	worktree := filepath.Join(root, "agent-worktrees", fmt.Sprintf("agent%d", index), "ouroboros-ide")
	identity := nativeSlotProcessIdentity{index: index, hostWorktree: worktree, hostHome: filepath.Join(worktree, fmt.Sprintf(".devhome-agent%d", index)), stateRoot: filepath.Join(root, "state", fmt.Sprintf("dev-all-agent%d", index)), sandboxWorktree: fmt.Sprintf("/workspaces/dev/agent-worktrees/agent%d/ouroboros-ide", index), sandboxHome: fmt.Sprintf("/workspaces/dev/agent-worktrees/agent%d/ouroboros-ide/.devhome-agent%d", index, index), sandboxState: fmt.Sprintf("/agent-state/dev-all-agent%d", index)}
	for _, p := range []string{identity.hostWorktree, identity.hostHome, identity.stateRoot} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return identity
}

func physicalProcRootFixture(t *testing.T) {
	t.Helper()
	saved := nativeSlotProcRoot
	nativeSlotProcRoot = t.TempDir()
	t.Cleanup(func() { nativeSlotProcRoot = saved })
}

func physicalViewFixture(t *testing.T, identity nativeSlotProcessIdentity, worktreeAlias string) (string, string) {
	t.Helper()
	view := t.TempDir()
	mapPhysicalView(t, view, worktreeAlias, identity.hostWorktree)
	mapPhysicalView(t, view, identity.sandboxState, identity.stateRoot)
	rel, err := filepath.Rel(identity.hostWorktree, identity.hostHome)
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(worktreeAlias, rel)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		mapPhysicalView(t, view, home, identity.hostHome)
	}
	return view, home
}

func mapPhysicalView(t *testing.T, view, visible, actual string) {
	t.Helper()
	p := view + visible
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actual, p); err != nil {
		t.Fatal(err)
	}
}

func writePhysicalProc(t *testing.T, pid, ppid int, view string, env map[string]string, cwd string) string {
	t.Helper()
	p := filepath.Join(nativeSlotProcRoot, strconv.Itoa(pid))
	if err := os.MkdirAll(filepath.Join(p, "fd"), 0700); err != nil {
		t.Fatal(err)
	}
	var fields []string
	for key, value := range env {
		fields = append(fields, key+"="+value)
	}
	if err := os.WriteFile(filepath.Join(p, "environ"), []byte(strings.Join(fields, "\x00")+"\x00"), 0600); err != nil {
		t.Fatal(err)
	}
	stat := []string{"S", strconv.Itoa(ppid)}
	for len(stat) < 20 {
		stat = append(stat, "0")
	}
	stat[19] = strconv.Itoa(100000 + pid)
	if err := os.WriteFile(filepath.Join(p, "stat"), []byte(fmt.Sprintf("%d (physical-fixture) %s\n", pid, strings.Join(stat, " "))), 0600); err != nil {
		t.Fatal(err)
	}
	if view != "" {
		if err := os.Symlink(view, filepath.Join(p, "root")); err != nil {
			t.Fatal(err)
		}
	}
	if cwd != "" {
		if err := os.Symlink(cwd, filepath.Join(p, "cwd")); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestNativeSlotPhysicalCustodyAcceptsHistoricalAliasAndSanitizedDescendants(t *testing.T) {
	physicalProcRootFixture(t)
	identity := physicalSlotFixture(t, 1)
	view, home := physicalViewFixture(t, identity, "/workspaces/dev/ouroboros-ide")
	env := map[string]string{"DEVKIT_NATIVE_AGENT": "1", "HOME": home, "CODEX_HOME": home + "/.codex"}
	writePhysicalProc(t, 101, 1, view, env, identity.hostWorktree)
	writePhysicalProc(t, 102, 101, view, env, identity.hostWorktree)
	writePhysicalProc(t, 103, 102, view, map[string]string{}, identity.hostHome)
	plan, err := planNativeSlotProcesses(identity, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.pids, []int{103, 102, 101}) {
		t.Fatalf("selected=%v", plan.pids)
	}
}

func TestNativeSlotPhysicalCustodyIgnoresSameSpellingWithDifferentBacking(t *testing.T) {
	physicalProcRootFixture(t)
	selected := physicalSlotFixture(t, 1)
	other := physicalSlotFixture(t, 1)
	// Both processes claim the same index and logical HOME. Only inode custody
	// distinguishes their independent namespace-backed workspaces.
	alias := "/workspaces/dev/ouroboros-ide"
	view, home := physicalViewFixture(t, other, alias)
	writePhysicalProc(t, 201, 1, view, map[string]string{"DEVKIT_NATIVE_AGENT": "1", "HOME": home}, other.hostWorktree)
	plan, err := planNativeSlotProcesses(selected, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.pids) != 0 {
		t.Fatalf("foreign namespace selected=%v", plan.pids)
	}
}

func TestNativeSlotPhysicalCustodyRejectsConflictingClaims(t *testing.T) {
	for _, kind := range []string{"wrong-agent", "wrong-home", "missing-root", "conflicting-worktree", "conflicting-state", "whitespace-home", "noncanonical-home"} {
		t.Run(kind, func(t *testing.T) {
			physicalProcRootFixture(t)
			selected := physicalSlotFixture(t, 1)
			other := physicalSlotFixture(t, 2)
			alias := "/workspaces/dev/ouroboros-ide"
			view, home := physicalViewFixture(t, selected, alias)
			env := map[string]string{"DEVKIT_NATIVE_AGENT": "1", "HOME": home}
			switch kind {
			case "wrong-agent":
				env["DEVKIT_NATIVE_AGENT"] = "2"
			case "wrong-home":
				env["HOME"] = "/wrong/home"
			case "whitespace-home":
				env["HOME"] = " " + home + " "
			case "noncanonical-home":
				env["HOME"] = filepath.Dir(home) + "/unresolved/../" + filepath.Base(home)
			case "missing-root":
				view = ""
			case "conflicting-worktree":
				view = t.TempDir()
				mapPhysicalView(t, view, home, selected.hostHome)
			case "conflicting-state":
				if err := os.Remove(view + selected.sandboxState); err != nil {
					t.Fatal(err)
				}
				mapPhysicalView(t, view, selected.sandboxState, other.stateRoot)
			}
			writePhysicalProc(t, 301, 1, view, env, selected.hostWorktree)
			if plan, err := planNativeSlotProcesses(selected, true); err == nil {
				t.Fatalf("conflicting claim admitted: %v", plan.pids)
			}
		})
	}
}

func TestNativeSlotPhysicalCustodyRejectsForeignActualFileDescriptor(t *testing.T) {
	physicalProcRootFixture(t)
	selected := physicalSlotFixture(t, 1)
	file := filepath.Join(selected.hostWorktree, "candidate.txt")
	if err := os.WriteFile(file, []byte("foreign open file"), 0600); err != nil {
		t.Fatal(err)
	}
	p := writePhysicalProc(t, 401, 1, "/", map[string]string{}, t.TempDir())
	if err := os.Symlink(file, filepath.Join(p, "fd", "3")); err != nil {
		t.Fatal(err)
	}
	if _, err := planNativeSlotProcesses(selected, true); err == nil || !strings.Contains(err.Error(), "unowned active process 401") {
		t.Fatalf("foreign fd error=%v", err)
	}
}

func TestNativeSlotUnownedRefusalRetainsSnapshotAfterExitOrReuse(t *testing.T) {
	for _, later := range []string{"exit", "reuse", "parent-drift", "unavailable-environ", "unavailable-start"} {
		t.Run(later, func(t *testing.T) {
			physicalProcRootFixture(t)
			selected := physicalSlotFixture(t, 3)
			secret := strings.Repeat("credential-sentinel-雪\n", 4096)
			proc := writePhysicalProc(t, 801, 71, "/", map[string]string{"SECRET": secret}, selected.hostWorktree)
			if err := os.WriteFile(filepath.Join(proc, "cmdline"), []byte(secret), 0600); err != nil {
				t.Fatal(err)
			}
			if later == "unavailable-environ" {
				if err := os.Remove(filepath.Join(proc, "environ")); err != nil {
					t.Fatal(err)
				}
			}
			if later == "unavailable-start" {
				stat, err := os.ReadFile(filepath.Join(proc, "stat"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(proc, "stat"), bytes.ReplaceAll(stat, []byte("100801"), []byte(strings.Repeat("9", 10000))), 0600); err != nil {
					t.Fatal(err)
				}
			}
			plan, refusal := planNativeSlotProcesses(selected, false)
			if plan != nil || refusal == nil {
				t.Fatalf("unowned toucher returned plan=%v error=%v", plan, refusal)
			}
			// Change the proc fixture before formatting the error, exactly when a
			// controller could otherwise lose or borrow the blocker's identity.
			if err := os.RemoveAll(proc); err != nil {
				t.Fatal(err)
			}
			if later == "reuse" || later == "parent-drift" {
				replacement := writePhysicalProc(t, 801, 99, "/", nil, t.TempDir())
				if later == "reuse" {
					stat, err := os.ReadFile(filepath.Join(replacement, "stat"))
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(replacement, "stat"), bytes.ReplaceAll(stat, []byte("100801"), []byte("200801")), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			text := refusal.Error()
			start := "100801"
			if later == "unavailable-start" {
				start = "unavailable"
			}
			for _, want := range []string{"unowned active process 801", "slot_index=3", "phase=process-custody-snapshot", "parent_pid=71", "start_ticks=" + start, "physical_touch=true", "selected_identity=false", "descendant_owned=false", "reason=no-selected-or-descendant-custody"} {
				if !strings.Contains(text, want) {
					t.Fatalf("refusal lost %q: %s", want, text)
				}
			}
			availability := "available"
			if later == "unavailable-environ" {
				availability = "unavailable"
			}
			if !strings.Contains(text, "environ="+availability) || len(text) > 512 || strings.Contains(text, "sentinel") || strings.Contains(text, "parent_pid=99") || strings.Contains(text, "200801") || strings.Contains(text, selected.hostWorktree) {
				t.Fatalf("unbounded, sensitive, or later evidence in refusal: %s", text)
			}
			if _, err := os.Stat(selected.hostWorktree); err != nil {
				t.Fatalf("refusal changed selected slot: %v", err)
			}
		})
	}
}

func TestNativeSlotCustodyObservationDriftNeverProducesAPlan(t *testing.T) {
	for _, field := range []string{"parent", "start"} {
		t.Run(field, func(t *testing.T) {
			physicalProcRootFixture(t)
			selected := physicalSlotFixture(t, 1)
			view, home := physicalViewFixture(t, selected, "/workspaces/dev/ouroboros-ide")
			proc := writePhysicalProc(t, 802, 71, view, map[string]string{"DEVKIT_NATIVE_AGENT": "1", "HOME": home}, selected.hostWorktree)
			saved := nativeSlotOpenProcessDirectory
			t.Cleanup(func() { nativeSlotOpenProcessDirectory = saved })
			nativeSlotOpenProcessDirectory = func(path string) (*os.File, error) {
				file, err := saved(path)
				stat, readErr := os.ReadFile(filepath.Join(proc, "stat"))
				if readErr != nil {
					t.Fatal(readErr)
				}
				old, replacement := "S 71", "S 99"
				if field == "start" {
					old, replacement = "100802", "200802"
				}
				if writeErr := os.WriteFile(filepath.Join(proc, "stat"), bytes.ReplaceAll(stat, []byte(old), []byte(replacement)), 0600); writeErr != nil {
					t.Fatal(writeErr)
				}
				return file, err
			}
			plan, err := planNativeSlotProcesses(selected, false)
			if plan != nil || err == nil || !strings.Contains(err.Error(), "changed identity during custody observation") {
				t.Fatalf("drift returned plan=%v error=%v", plan, err)
			}
		})
	}
}

func TestNativeSlotPhysicalCustodyConflictingDescendantCannotInherit(t *testing.T) {
	for _, kind := range []string{"agent", "home", "worktree", "state", "sanitized-sibling-view", "sanitized-conflicting-state"} {
		t.Run(kind, func(t *testing.T) {
			physicalProcRootFixture(t)
			selected := physicalSlotFixture(t, 1)
			other := physicalSlotFixture(t, 2)
			alias := "/workspaces/dev/ouroboros-ide"
			view, home := physicalViewFixture(t, selected, alias)
			writePhysicalProc(t, 501, 1, view, map[string]string{"DEVKIT_NATIVE_AGENT": "1", "HOME": home}, selected.hostWorktree)
			childView, childHome := physicalViewFixture(t, selected, alias)
			env := map[string]string{"HOME": childHome}
			switch kind {
			case "agent":
				env["DEVKIT_NATIVE_AGENT"] = "2"
			case "home":
				childView, childHome = physicalViewFixture(t, other, alias)
				env["HOME"] = childHome
			case "worktree":
				childView = t.TempDir()
				mapPhysicalView(t, childView, childHome, selected.hostHome)
			case "sanitized-sibling-view":
				childView, _ = physicalViewFixture(t, other, alias)
				env = map[string]string{}
			case "sanitized-conflicting-state":
				if err := os.Remove(childView + selected.sandboxState); err != nil {
					t.Fatal(err)
				}
				mapPhysicalView(t, childView, selected.sandboxState, other.stateRoot)
				env = map[string]string{}
			case "state":
				if err := os.Remove(childView + selected.sandboxState); err != nil {
					t.Fatal(err)
				}
				mapPhysicalView(t, childView, selected.sandboxState, other.stateRoot)
			}
			writePhysicalProc(t, 502, 501, childView, env, "")
			if _, err := planNativeSlotProcesses(selected, true); err == nil {
				t.Fatal("conflicting child inherited selected custody")
			}
		})
	}
}

func TestNativeSlotPhysicalCustodyRejectsIncompleteStableIdentity(t *testing.T) {
	physicalProcRootFixture(t)
	selected := physicalSlotFixture(t, 1)
	view, home := physicalViewFixture(t, selected, "/workspaces/dev/ouroboros-ide")
	p := writePhysicalProc(t, 601, 1, view, map[string]string{"DEVKIT_NATIVE_AGENT": "1", "HOME": home}, selected.hostWorktree)
	if err := os.WriteFile(filepath.Join(p, "stat"), []byte("601 (fixture) S 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := planNativeSlotProcesses(selected, true); err == nil || !strings.Contains(err.Error(), "changed identity") {
		t.Fatalf("unstable identity error=%v", err)
	}
}

func TestNativeSlotPhysicalCustodyRejectsForeignHomeWithSelectedDerivedWorktree(t *testing.T) {
	physicalProcRootFixture(t)
	selected := physicalSlotFixture(t, 1)
	other := physicalSlotFixture(t, 2)
	alias := "/workspaces/dev/ouroboros-ide"
	view, _ := physicalViewFixture(t, selected, alias)
	if err := os.Symlink(other.hostHome, filepath.Join(selected.hostWorktree, "foreign-home")); err != nil {
		t.Fatal(err)
	}
	writePhysicalProc(t, 701, 1, view, map[string]string{"DEVKIT_NATIVE_AGENT": "1", "HOME": alias + "/foreign-home"}, "")
	if _, err := planNativeSlotProcesses(selected, true); err == nil || !strings.Contains(err.Error(), "conflicting physical custody") {
		t.Fatalf("mixed custody error=%v", err)
	}
}

func TestNativePhysicalTouchPinsDirectoryAcrossTargetDescriptorClose(t *testing.T) {
	selected := physicalSlotFixture(t, 1)
	nested := filepath.Join(selected.hostWorktree, "nested", "directory")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	target, err := os.Open(nested)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	targetPath := fmt.Sprintf("/proc/self/fd/%d", target.Fd())
	saved := nativeSlotOpenProcessDirectory
	t.Cleanup(func() { nativeSlotOpenProcessDirectory = saved })
	nativeSlotOpenProcessDirectory = func(path string) (*os.File, error) {
		observed, err := saved(path)
		if err == nil {
			if err := target.Close(); err != nil {
				t.Fatal(err)
			}
		}
		return observed, err
	}
	anchors, err := nativeSlotPhysicalTargets(selected)
	if err != nil {
		t.Fatal(err)
	}
	touches, err := nativePhysicalDirectoryTouches(targetPath, anchors)
	if err != nil || !touches {
		t.Fatalf("held directory touch=%v err=%v", touches, err)
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("target descriptor remains: %v", err)
	}
}

func TestNativePhysicalTouchDirectoryOpenRejectsNonDirectoriesWithoutBlocking(t *testing.T) {
	for _, kind := range []string{"regular-file", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), kind)
			if kind == "fifo" {
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				file, err := nativeSlotOpenProcessDirectory(path)
				if file != nil {
					_ = file.Close()
				}
				result <- err
			}()
			select {
			case err := <-result:
				if !errors.Is(err, syscall.ENOTDIR) {
					t.Fatalf("non-directory open error=%v", err)
				}
			case <-time.After(time.Second):
				// Release an accidentally blocking FIFO open before failing so
				// this regression remains bounded even if the guard is removed.
				if kind == "fifo" {
					fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
					if err == nil {
						defer syscall.Close(fd)
					}
				}
				t.Fatal("process directory observation blocked on a non-directory")
			}
		})
	}
}

func TestNativePhysicalTouchDirectoryTypeChangeKeepsSelectedAmbiguityClosed(t *testing.T) {
	selected := physicalSlotFixture(t, 1)
	anchors, err := nativeSlotPhysicalTargets(selected)
	if err != nil {
		t.Fatal(err)
	}
	saved := nativeSlotOpenProcessDirectory
	t.Cleanup(func() { nativeSlotOpenProcessDirectory = saved })
	nativeSlotOpenProcessDirectory = func(string) (*os.File, error) { return nil, syscall.ENOTDIR }
	for _, tc := range []struct {
		name, visible string
		wantError     bool
	}{
		{"unrelated", t.TempDir(), false},
		{"selected", selected.hostWorktree, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proc := t.TempDir()
			if err := os.Symlink(tc.visible, filepath.Join(proc, "cwd")); err != nil {
				t.Fatal(err)
			}
			touches, err := nativeProcLinkTouches(proc, "cwd", selected, anchors)
			if touches || (err != nil) != tc.wantError {
				t.Fatalf("touches=%v error=%v", touches, err)
			}
		})
	}
}

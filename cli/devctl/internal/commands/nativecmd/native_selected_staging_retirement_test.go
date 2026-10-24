package nativecmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"devkit/cli/devctl/internal/cmdregistry"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

func allowFixtureSelectedStagingProtection(t *testing.T, inspect func(nativeplan.Plan, []string, []os.FileInfo) error) {
	t.Helper()
	old := inspectSelectedStagingProtection
	inspectSelectedStagingProtection = inspect
	t.Cleanup(func() { inspectSelectedStagingProtection = old })
}

func makeEmptySelectedStaging(t *testing.T, roots []string) []os.FileInfo {
	t.Helper()
	before := []os.FileInfo{}
	for i, root := range roots {
		mode := os.FileMode(0700)
		if i == 0 {
			mode = 0755
		}
		if err := os.MkdirAll(root, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(root, mode); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(root)
		if err != nil {
			t.Fatal(err)
		}
		before = append(before, info)
	}
	return before
}

func TestSelectedStagingRetiresEmptyVectorAndReservesUnderContinuousLock(t *testing.T) {
	p, opts, root, _, parent := testSelectedControllerLease(t)
	if err := syscall.Flock(int(parent.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	lease, err := openSelectedControllerLifecycleLeaseAt(p, dupSelectedLease(t, parent), root)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.close()
	roots, err := freshPreparationPaths(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	before := makeEmptySelectedStaging(t, roots)
	checks := 0
	allowFixtureSelectedStagingProtection(t, func(nativeplan.Plan, []string, []os.FileInfo) error { checks++; return nil })
	claim, err := claimFreshSelectedPreparationWithLease(p, opts, false, lease)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.close()
	if checks < len(roots)+2 {
		t.Fatalf("protection was not retained through retirement: %d", checks)
	}
	for i, selected := range roots {
		after, err := os.Stat(selected)
		if err != nil || os.SameFile(before[i], after) {
			t.Fatalf("root was not exclusively reconstructed: %s %v", selected, err)
		}
	}
	coordination, err := nativeSlotResetCoordinationRoot(opts.WorktreeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := acquireNativeSlotResetLock(coordination, "dev-all"); err == nil {
		other.release()
		t.Fatal("reset entered between retirement and preparation")
	}
	if err := claim.verifyNamespace(); err != nil {
		t.Fatal(err)
	}
}

func TestSelectedStagingClassifiesEntireVectorBeforeMutation(t *testing.T) {
	for _, protected := range []string{"state_5.sqlite", "selected-preparation.json", ".git", "rollouts", "runtime-record.json", "unexpected"} {
		t.Run(protected, func(t *testing.T) {
			p, opts, root, _, parent := testSelectedControllerLease(t)
			if err := syscall.Flock(int(parent.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			lease, err := openSelectedControllerLifecycleLeaseAt(p, dupSelectedLease(t, parent), root)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.close()
			roots, err := freshPreparationPaths(p, opts)
			if err != nil {
				t.Fatal(err)
			}
			before := makeEmptySelectedStaging(t, roots)
			name := filepath.Join(roots[1], protected)
			if err := os.WriteFile(name, []byte("protected original bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			allowFixtureSelectedStagingProtection(t, func(nativeplan.Plan, []string, []os.FileInfo) error { return nil })
			if claim, err := claimFreshSelectedPreparationWithLease(p, opts, false, lease); err == nil {
				claim.close()
				t.Fatal("nonempty staging admitted without former-producer proof")
			}
			for i, selected := range roots {
				after, err := os.Stat(selected)
				if err != nil || !os.SameFile(before[i], after) {
					t.Fatal("earlier eligible root mutated before full classification")
				}
			}
			data, err := os.ReadFile(name)
			if err != nil || string(data) != "protected original bytes" {
				t.Fatal("protected state changed")
			}
		})
	}
}

func TestSelectedStagingProtectionAndNamespaceRacePreserveRoots(t *testing.T) {
	for _, race := range []bool{false, true} {
		t.Run(map[bool]string{false: "active-process-or-mount", true: "namespace-substitution"}[race], func(t *testing.T) {
			p, opts, root, _, parent := testSelectedControllerLease(t)
			if err := syscall.Flock(int(parent.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			lease, err := openSelectedControllerLifecycleLeaseAt(p, dupSelectedLease(t, parent), root)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.close()
			roots, err := freshPreparationPaths(p, opts)
			if err != nil {
				t.Fatal(err)
			}
			before := makeEmptySelectedStaging(t, roots)
			calls := 0
			allowFixtureSelectedStagingProtection(t, func(nativeplan.Plan, []string, []os.FileInfo) error {
				calls++
				if !race {
					return errors.New("selected active process or mount")
				}
				if calls == 2 {
					if err := os.Rename(roots[0], roots[0]+".preserved"); err != nil {
						return err
					}
					return os.Mkdir(roots[0], 0755)
				}
				return nil
			})
			if claim, err := claimFreshSelectedPreparationWithLease(p, opts, false, lease); err == nil {
				claim.close()
				t.Fatal("unsafe retirement admitted")
			}
			for i, selected := range roots {
				if race && i == 0 {
					selected += ".preserved"
				}
				after, err := os.Stat(selected)
				if err != nil || !os.SameFile(before[i], after) {
					t.Fatal("selected original namespace lost")
				}
			}
		})
	}
}

func TestSelectedStagingMountEvidenceRefusesBindDescendantsAndUnknownRows(t *testing.T) {
	const selected = "/selected lane/agent5"
	base := "1 1 0:1 / / rw - tmpfs tmpfs rw\n"
	if err := rejectSelectedStagingMounts([]byte(base), []string{selected}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []string{
		"2 1 0:1 / /selected\\040lane/agent5 rw - tmpfs tmpfs rw\n",
		"2 1 0:1 / /selected\\040lane/agent5/child rw - tmpfs tmpfs rw\n",
		"garbage\n", "2 1 invalid / /elsewhere rw - tmpfs tmpfs rw\n", strings.TrimSuffix(base, "\n"),
	} {
		if err := rejectSelectedStagingMounts([]byte(row), []string{selected}); err == nil {
			t.Fatalf("unsafe mount evidence admitted: %q", row)
		}
	}
}

func TestSelectedStagingRefusesForeignRootTypesModesAndLinks(t *testing.T) {
	for _, kind := range []string{"symlink", "regular-file", "fifo", "unreadable-mode", "writable-mode", "hardlinked-child"} {
		t.Run(kind, func(t *testing.T) {
			p, opts, root, _, parent := testSelectedControllerLease(t)
			if err := syscall.Flock(int(parent.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			lease, err := openSelectedControllerLifecycleLeaseAt(p, dupSelectedLease(t, parent), root)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.close()
			roots, err := freshPreparationPaths(p, opts)
			if err != nil {
				t.Fatal(err)
			}
			makeEmptySelectedStaging(t, roots)
			protected := filepath.Join(t.TempDir(), "protected")
			if err := os.WriteFile(protected, []byte("outside bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink", "regular-file", "fifo":
				if err := os.Remove(roots[0]); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "symlink":
					err = os.Symlink(filepath.Dir(protected), roots[0])
				case "regular-file":
					err = os.WriteFile(roots[0], []byte("foreign root bytes"), 0600)
				case "fifo":
					err = syscall.Mkfifo(roots[0], 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "unreadable-mode":
				if err := os.Chmod(roots[0], 0000); err != nil {
					t.Fatal(err)
				}
				defer os.Chmod(roots[0], 0700)
			case "writable-mode":
				if err := os.Chmod(roots[0], 0777); err != nil {
					t.Fatal(err)
				}
			case "hardlinked-child":
				if err := os.Link(protected, filepath.Join(roots[0], "unexpected")); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(roots[0])
			if err != nil {
				t.Fatal(err)
			}
			allowFixtureSelectedStagingProtection(t, func(nativeplan.Plan, []string, []os.FileInfo) error { return nil })
			if claim, err := claimFreshSelectedPreparationWithLease(p, opts, false, lease); err == nil {
				claim.close()
				t.Fatal("foreign root retired")
			}
			after, err := os.Lstat(roots[0])
			if err != nil || !os.SameFile(before, after) {
				t.Fatal("foreign root identity changed")
			}
			data, err := os.ReadFile(protected)
			if err != nil || string(data) != "outside bytes" {
				t.Fatal("protected foreign bytes changed")
			}
		})
	}
}

func TestSelectedStagingDescriptorFlagIsNarrowPrepareOnly(t *testing.T) {
	ctx := &cmdregistry.Context{Project: "dev-all", Args: []string{"prepare", "--index", "5", "--controller-lifecycle-lease-fd", "3"}}
	parsed, err := parsePlanArgs(ctx, false, false)
	if err != nil || parsed.controllerLifecycleLeaseFD != 3 || !parsed.indexSet {
		t.Fatalf("fixed internal descriptor rejected: %v", err)
	}
	for _, test := range []struct {
		project string
		args    []string
	}{
		{"dev-all", []string{"plan", "--controller-lifecycle-lease-fd", "3"}},
		{"dev-all", []string{"exec", "--controller-lifecycle-lease-fd", "3"}},
		{"ouroboros-terraform", []string{"prepare", "--controller-lifecycle-lease-fd", "3"}},
		{"dev-all", []string{"prepare", "--controller-lifecycle-lease-fd", "4"}},
		{"dev-all", []string{"prepare", "--controller-lifecycle-lease-fd"}},
		{"dev-all", []string{"prepare", "--controller-lifecycle-lease-fd", "3", "--controller-lifecycle-lease-fd", "3"}},
	} {
		ctx := &cmdregistry.Context{Project: test.project, Args: test.args}
		if _, err := parsePlanArgs(ctx, false, false); err == nil {
			t.Fatalf("descriptor admitted outside fixed selected prepare: %v %v", test.project, test.args)
		}
	}
}

func TestSelectedStagingProcessScopePinsWorkspaceAndGitAfterUnlink(t *testing.T) {
	p, opts := freshSelectedFixture(t)
	roots, err := freshPreparationPaths(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	makeEmptySelectedStaging(t, roots)
	for _, index := range []int{0, 2} {
		held, err := openFreshPreparationDirectory(roots[index], false)
		if err != nil {
			t.Fatal(err)
		}
		defer held.Close()
		info, err := held.Stat()
		if err != nil {
			t.Fatal(err)
		}
		targets, err := nativeSlotPhysicalTargets(nativeSlotProcessIdentity{stagingTargets: []os.FileInfo{info}})
		if err != nil {
			t.Fatal(err)
		}
		if touches, err := nativePhysicalDirectoryTouches(roots[index], targets); err != nil || !touches {
			t.Fatalf("selected parent omitted from custody: %v %v", touches, err)
		}
		if touches, err := nativePhysicalDirectoryTouches(t.TempDir(), targets); err != nil || touches {
			t.Fatalf("neighbor promoted to selected custody: %v %v", touches, err)
		}
		if err := os.Remove(roots[index]); err != nil {
			t.Fatal(err)
		}
		if touches, err := nativePhysicalDirectoryTouches(fmt.Sprintf("/proc/self/fd/%d", held.Fd()), targets); err != nil || !touches {
			t.Fatalf("retired directory contact lost before reservation: %v %v", touches, err)
		}
	}
}

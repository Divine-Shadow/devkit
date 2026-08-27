package nativecmd

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	wtx "devkit/cli/devctl/internal/worktrees"
)

func wholeResetTestIdentities(root string) []nativeSlotProcessIdentity {
	identities := make([]nativeSlotProcessIdentity, 0, 3)
	for index := 1; index <= 3; index++ {
		identities = append(identities, nativeSlotProcessIdentity{
			index:           index,
			hostWorktree:    filepath.Join(root, "worktrees", fmt.Sprintf("agent%d", index), "ouroboros-ide"),
			sandboxWorktree: filepath.Join("/worktrees", fmt.Sprintf("agent%d", index), "ouroboros-ide"),
			hostHome:        filepath.Join(root, "worktrees", fmt.Sprintf("agent%d", index), fmt.Sprintf(".devhome-agent%d", index)),
			sandboxHome:     filepath.Join("/worktrees", fmt.Sprintf("agent%d", index), fmt.Sprintf(".devhome-agent%d", index)),
			stateRoot:       filepath.Join(root, "state", fmt.Sprintf("dev-all-agent%d", index)),
			sandboxState:    filepath.Join("/agent-state", fmt.Sprintf("dev-all-agent%d", index)),
		})
	}
	return identities
}

func writeWholeResetTestProcess(t *testing.T, pid int, identity nativeSlotProcessIdentity) {
	t.Helper()
	procRoot := filepath.Join(nativeSlotProcRoot, strconv.Itoa(pid))
	if err := os.MkdirAll(filepath.Join(procRoot, "fd"), 0o700); err != nil {
		t.Fatal(err)
	}
	environ := fmt.Sprintf("DEVKIT_NATIVE_AGENT=%d\x00HOME=%s\x00", identity.index, identity.sandboxHome)
	if err := os.WriteFile(filepath.Join(procRoot, "environ"), []byte(environ), 0o600); err != nil {
		t.Fatal(err)
	}
	fields := []string{"S", "1"}
	for len(fields) < 20 {
		fields = append(fields, "0")
	}
	fields[19] = strconv.Itoa(100000 + pid)
	if err := os.WriteFile(
		filepath.Join(procRoot, "stat"),
		[]byte(fmt.Sprintf("%d (whole-reset-fixture) %s\n", pid, strings.Join(fields, " "))),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
}

func TestSourceDerivedNativeSlotProcessIdentitiesIncludeEveryDeclaredSlot(t *testing.T) {
	root := t.TempDir()
	devkitRoot := filepath.Join(root, "devkit")
	identities, err := sourceDerivedNativeSlotProcessIdentities(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{
			Root:                 devkitRoot,
			RuntimeAuthorityRoot: devkitRoot,
		},
		HostRoot:              root,
		Project:               "dev-all",
		Repo:                  "ouroboros-ide",
		Flake:                 "path:/nix/store/source",
		WorktreeRoot:          filepath.Join(root, "agent-worktrees"),
		StateRoot:             filepath.Join(root, ".devkit", "native-agents"),
		WorktreeContainerRoot: "/worktrees",
		StateContainerRoot:    "/agent-state",
		BrokerEndpoint:        filepath.Join(root, ".devkit", "broker.sock"),
		DedicatedWorktree:     true,
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(identities) != 3 {
		t.Fatalf("source-derived identities = %d, want 3", len(identities))
	}
	for index, identity := range identities {
		want := index + 1
		if identity.index != want {
			t.Fatalf("identity %d index = %d, want %d", index, identity.index, want)
		}
		if !strings.Contains(identity.hostWorktree, fmt.Sprintf("agent%d", want)) {
			t.Fatalf("identity %d host worktree = %q", want, identity.hostWorktree)
		}
	}
}

func TestNativeWholeResetCountMustEqualSourceDeclarationBeforeEffects(t *testing.T) {
	for _, count := range []int{2, 4} {
		err := validateNativeWholeResetCount(count, 3)
		want := fmt.Sprintf("count %d must equal source-declared agent count 3", count)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("count mismatch error = %v, want %q", err, want)
		}
	}
	if err := validateNativeWholeResetCount(3, 3); err != nil {
		t.Fatalf("matching count rejected: %v", err)
	}
}

func TestNativeWholeResetFailureCleanupRefusesProcessLeftByReconstruction(t *testing.T) {
	originalProcRoot := nativeSlotProcRoot
	t.Cleanup(func() { nativeSlotProcRoot = originalProcRoot })
	nativeSlotProcRoot = t.TempDir()
	identities := wholeResetTestIdentities(t.TempDir())
	const pid = 9303
	writeWholeResetTestProcess(t, pid, identities[2])
	applied := false
	err := applyNativeWholeResetFailureCleanup(identities, func() error {
		applied = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("index 3 pids [%d]", pid)) {
		t.Fatalf("failed-reconstruction cleanup error = %v", err)
	}
	if applied {
		t.Fatal("failed-reconstruction cleanup disposed an active slot")
	}
}

func TestNativeWholeResetRejectsActiveProcessInEveryDeclaredSlotBeforeEffects(t *testing.T) {
	originalProcRoot := nativeSlotProcRoot
	t.Cleanup(func() { nativeSlotProcRoot = originalProcRoot })

	for slot := 1; slot <= 3; slot++ {
		t.Run(fmt.Sprintf("slot-%d", slot), func(t *testing.T) {
			nativeSlotProcRoot = t.TempDir()
			identities := wholeResetTestIdentities(t.TempDir())
			pid := 9100 + slot
			writeWholeResetTestProcess(t, pid, identities[slot-1])
			var effects []string
			err := executeNativeWholeResetBoundary(nativeWholeResetBoundary{
				inspectSlots: func() error {
					return requireSourceDerivedNativeSlotsIdle(identities)
				},
				lifecycleDown: func() error {
					effects = append(effects, "lifecycle-down")
					return nil
				},
				stopSessions: func() { effects = append(effects, "stop-sessions") },
				snapshotHistory: func() error {
					effects = append(effects, "snapshot-history")
					return nil
				},
				applyPlan: func() error {
					effects = append(effects, "apply-plan")
					return nil
				},
			})
			want := fmt.Sprintf("index %d pids [%d]", slot, pid)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("active slot error = %v, want %q", err, want)
			}
			if len(effects) != 0 {
				t.Fatalf("active slot permitted effects: %v", effects)
			}
		})
	}
}

func TestNativeWholeResetNoProcessPathReachesExistingPlan(t *testing.T) {
	var events []string
	err := executeNativeWholeResetBoundary(nativeWholeResetBoundary{
		inspectSlots: func() error {
			events = append(events, "inspect")
			return nil
		},
		lifecycleDown: func() error {
			events = append(events, "lifecycle-down")
			return nil
		},
		stopSessions: func() { events = append(events, "stop-sessions") },
		snapshotHistory: func() error {
			events = append(events, "snapshot-history")
			return nil
		},
		applyPlan: func() error {
			events = append(events, "apply-plan")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"inspect", "lifecycle-down", "stop-sessions", "inspect", "snapshot-history", "inspect", "apply-plan"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("whole reset events = %v, want %v", events, want)
	}
}

func TestNativeWholeResetProcessAppearingBetweenScansBlocksDisposal(t *testing.T) {
	originalProcRoot := nativeSlotProcRoot
	t.Cleanup(func() { nativeSlotProcRoot = originalProcRoot })
	nativeSlotProcRoot = t.TempDir()
	identities := wholeResetTestIdentities(t.TempDir())
	const pid = 9203
	var effects []string
	err := executeNativeWholeResetBoundary(nativeWholeResetBoundary{
		inspectSlots: func() error {
			return requireSourceDerivedNativeSlotsIdle(identities)
		},
		lifecycleDown: func() error {
			effects = append(effects, "lifecycle-down")
			return nil
		},
		stopSessions: func() {
			effects = append(effects, "stop-sessions")
			writeWholeResetTestProcess(t, pid, identities[2])
		},
		snapshotHistory: func() error {
			effects = append(effects, "snapshot-history")
			return nil
		},
		applyPlan: func() error {
			effects = append(effects, "apply-plan")
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("index 3 pids [%d]", pid)) {
		t.Fatalf("post-stop process error = %v", err)
	}
	if want := []string{"lifecycle-down", "stop-sessions"}; !reflect.DeepEqual(effects, want) {
		t.Fatalf("post-stop effects = %v, want %v", effects, want)
	}
}

func TestNativeWholeResetLockSerializesEffectsAndDryRunDoesNotCreateIt(t *testing.T) {
	coordinationRoot := filepath.Join(t.TempDir(), "worktrees", ".devkit", "git")
	dryRunLock, err := acquireNativeWholeResetLock(coordinationRoot, "dev-all", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := dryRunLock.release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(coordinationRoot); !os.IsNotExist(err) {
		t.Fatalf("dry-run created coordination root: %v", err)
	}

	first, err := acquireNativeWholeResetLock(coordinationRoot, "dev-all", false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := acquireNativeWholeResetLock(coordinationRoot, "dev-all", false)
	if err == nil || !strings.Contains(err.Error(), "another package-owned native reset is active") {
		if second != nil {
			_ = second.release()
		}
		t.Fatalf("concurrent whole reset lock error = %v", err)
	}
	if err := first.release(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeWholeResetProductionLockWrapsScansAndApply(t *testing.T) {
	coordinationRoot := filepath.Join(t.TempDir(), "worktrees", ".devkit", "git")
	assertHeld := func(stage string) {
		t.Helper()
		contender, err := acquireNativeWholeResetLock(coordinationRoot, "dev-all", false)
		if err == nil || !strings.Contains(err.Error(), "another package-owned native reset is active") {
			if contender != nil {
				_ = contender.release()
			}
			t.Fatalf("reset lock was not held during %s: %v", stage, err)
		}
	}
	inspections := 0
	err := withNativeWholeResetLock(coordinationRoot, "dev-all", false, func() error {
		return executeNativeWholeResetBoundary(nativeWholeResetBoundary{
			inspectSlots: func() error {
				inspections++
				assertHeld(fmt.Sprintf("inspection %d", inspections))
				return nil
			},
			lifecycleDown: func() error {
				assertHeld("lifecycle down")
				return nil
			},
			stopSessions: func() { assertHeld("session stop") },
			snapshotHistory: func() error {
				assertHeld("history snapshot")
				return nil
			},
			applyPlan: func() error {
				assertHeld("plan apply")
				return nil
			},
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if inspections != 3 {
		t.Fatalf("slot inspections = %d, want 3", inspections)
	}
	reacquired, err := acquireNativeWholeResetLock(coordinationRoot, "dev-all", false)
	if err != nil {
		t.Fatalf("reset lock remained held after action: %v", err)
	}
	if err := reacquired.release(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeWholeResetDryRunPlanLeavesOwnedPathsUntouched(t *testing.T) {
	root := t.TempDir()
	worktreeRoot := filepath.Join(root, "worktrees")
	stateRoot := filepath.Join(root, "state")
	payload := filepath.Join(worktreeRoot, "agent1", "payload")
	if err := os.MkdirAll(filepath.Dir(payload), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payload, []byte("preserve\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	plan, err := wtx.PlanNativeReset(wtx.NativeResetOptions{
		Project:      "dev-all",
		Repo:         "ouroboros-ide",
		Count:        3,
		WorktreeRoot: worktreeRoot,
		StateRoot:    stateRoot,
		DryRun:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinationRoot := filepath.Join(worktreeRoot, ".devkit", "git")
	lock, err := acquireNativeWholeResetLock(coordinationRoot, "dev-all", true)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	if err := executeNativeWholeResetBoundary(nativeWholeResetBoundary{
		inspectSlots:  func() error { return nil },
		lifecycleDown: func() error { return nil },
		stopSessions:  func() {},
		snapshotHistory: func() error {
			return nil
		},
		applyPlan: plan.Apply,
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(payload)
	if err != nil || string(data) != "preserve\n" {
		t.Fatalf("dry-run payload = %q, %v", data, err)
	}
	if _, err := os.Lstat(coordinationRoot); !os.IsNotExist(err) {
		t.Fatalf("dry-run created lock root: %v", err)
	}
}

func TestNativeWholeResetSnapshotFailurePreventsDestructiveApply(t *testing.T) {
	var events []string
	err := executeNativeWholeResetBoundary(nativeWholeResetBoundary{
		inspectSlots: func() error {
			events = append(events, "inspect")
			return nil
		},
		lifecycleDown: func() error {
			events = append(events, "lifecycle-down")
			return nil
		},
		stopSessions: func() { events = append(events, "stop-sessions") },
		snapshotHistory: func() error {
			events = append(events, "snapshot-history")
			return fmt.Errorf("snapshot failed")
		},
		applyPlan: func() error {
			events = append(events, "apply-plan")
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "snapshot failed") {
		t.Fatalf("snapshot failure = %v", err)
	}
	want := []string{"inspect", "lifecycle-down", "stop-sessions", "inspect", "snapshot-history"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("snapshot failure events = %v, want %v", events, want)
	}
}

func TestNativeWholeResetProcessAppearingDuringSnapshotPreventsDestructiveApply(t *testing.T) {
	var events []string
	inspections := 0
	err := executeNativeWholeResetBoundary(nativeWholeResetBoundary{
		inspectSlots: func() error {
			inspections++
			events = append(events, fmt.Sprintf("inspect-%d", inspections))
			if inspections == 3 {
				return fmt.Errorf("process appeared")
			}
			return nil
		},
		lifecycleDown: func() error {
			events = append(events, "lifecycle-down")
			return nil
		},
		stopSessions: func() { events = append(events, "stop-sessions") },
		snapshotHistory: func() error {
			events = append(events, "snapshot-history")
			return nil
		},
		applyPlan: func() error {
			events = append(events, "apply-plan")
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "process appeared") {
		t.Fatalf("post-history process failure = %v", err)
	}
	want := []string{"inspect-1", "lifecycle-down", "stop-sessions", "inspect-2", "snapshot-history", "inspect-3"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("post-history process events = %v, want %v", events, want)
	}
}

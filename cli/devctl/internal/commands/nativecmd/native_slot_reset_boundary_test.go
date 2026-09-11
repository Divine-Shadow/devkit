package nativecmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"devkit/cli/devctl/internal/codexhistory"
	"devkit/cli/devctl/internal/sqliteauthority"
)

func TestSelectedNativeSlotResetOrdersCustodyBeforeApply(t *testing.T) {
	var events []string
	err := executeNativeSlotDestructiveBoundary(nativeSlotDestructiveBoundary{
		stopSelected: func() error {
			events = append(events, "stop")
			return nil
		},
		inspectIdle: func() error {
			events = append(events, "inspect")
			return nil
		},
		snapshotHistory: func() error {
			events = append(events, "snapshot")
			return nil
		},
		applyPlan: func() error {
			events = append(events, "apply")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"stop", "inspect", "snapshot", "inspect", "apply"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("selected reset events = %v, want %v", events, want)
	}
}

func TestSelectedNativeSlotResetSnapshotFailureNeverApplies(t *testing.T) {
	var events []string
	err := executeNativeSlotDestructiveBoundary(nativeSlotDestructiveBoundary{
		stopSelected: func() error {
			events = append(events, "stop")
			return nil
		},
		inspectIdle: func() error {
			events = append(events, "inspect")
			return nil
		},
		snapshotHistory: func() error {
			events = append(events, "snapshot")
			return fmt.Errorf("invalid history")
		},
		applyPlan: func() error {
			events = append(events, "apply")
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid history") {
		t.Fatalf("snapshot failure = %v", err)
	}
	want := []string{"stop", "inspect", "snapshot"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("snapshot failure events = %v, want %v", events, want)
	}
}

func TestSelectedNativeSlotResetPreflightENOSPCNeverApplies(t *testing.T) {
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 is supplied by the Nix test closure")
	}
	restoreSQLite := sqliteauthority.SetExecutableForTesting(sqlite)
	t.Cleanup(restoreSQLite)
	restoreSpace := codexhistory.SetAvailableSpaceCheckForTesting(func(string, int64) error {
		return fmt.Errorf("deterministic storage exhaustion: %w", syscall.ENOSPC)
	})
	t.Cleanup(restoreSpace)

	root := t.TempDir()
	stateRoot := filepath.Join(root, ".devkit", "native-agents")
	if err := os.MkdirAll(filepath.Dir(stateRoot), 0o700); err != nil {
		t.Fatal(err)
	}
	workspaceRoot := filepath.Join(root, "agent-worktrees", "agent1")
	hostWorktree := filepath.Join(workspaceRoot, "ouroboros-ide")
	hostHome := filepath.Join(workspaceRoot, ".devhome-agent1")
	if err := os.MkdirAll(hostWorktree, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(hostHome, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	options := codexhistory.SnapshotOptions{
		Project:       "dev-all",
		ResetKind:     "selected-slot-reset",
		AgentIndex:    1,
		HostWorktree:  hostWorktree,
		HostHome:      hostHome,
		SandboxHome:   "/workspaces/dev/agent-worktrees/agent1/.devhome-agent1",
		StateRoot:     stateRoot,
		WorkspaceRoot: workspaceRoot,
	}

	var events []string
	err = executeNativeSlotDestructiveBoundary(nativeSlotDestructiveBoundary{
		stopSelected: func() error {
			events = append(events, "stop")
			return nil
		},
		inspectIdle: func() error {
			events = append(events, "inspect")
			return nil
		},
		snapshotHistory: func() error {
			events = append(events, "snapshot")
			_, captureErr := codexhistory.Capture(options)
			return captureErr
		},
		applyPlan: func() error {
			events = append(events, "apply")
			return nil
		},
	})
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("selected reset ENOSPC = %v", err)
	}
	want := []string{"stop", "inspect", "snapshot"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("ENOSPC reset events = %v, want %v", events, want)
	}
	custodyRoot, rootErr := codexhistory.WorkspaceCustodyRoot(workspaceRoot, "dev-all")
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	entries, readErr := os.ReadDir(filepath.Join(custodyRoot, "agent1"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("ENOSPC reset left committed or staging generation: %v", entries)
	}
}

func TestSelectedNativeSlotResetProcessAppearingDuringCaptureNeverApplies(t *testing.T) {
	var events []string
	inspections := 0
	err := executeNativeSlotDestructiveBoundary(nativeSlotDestructiveBoundary{
		stopSelected: func() error {
			events = append(events, "stop")
			return nil
		},
		inspectIdle: func() error {
			inspections++
			events = append(events, fmt.Sprintf("inspect-%d", inspections))
			if inspections == 2 {
				return fmt.Errorf("new process")
			}
			return nil
		},
		snapshotHistory: func() error {
			events = append(events, "snapshot")
			return nil
		},
		applyPlan: func() error {
			events = append(events, "apply")
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "new process") {
		t.Fatalf("process race failure = %v", err)
	}
	want := []string{"stop", "inspect-1", "snapshot", "inspect-2"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("process race events = %v, want %v", events, want)
	}
}

func TestSelectedNativeSlotPostHistoryRefusalPreservesCapturedCustody(t *testing.T) {
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 is supplied by the Nix test closure")
	}
	t.Cleanup(sqliteauthority.SetExecutableForTesting(sqlite))
	physicalProcRootFixture(t)
	selected := physicalSlotFixture(t, 3)
	codexRoot := filepath.Join(selected.hostHome, ".codex")
	if err := os.MkdirAll(codexRoot, 0700); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(codexRoot, "goals_1.sqlite")
	command := exec.Command(sqlite, database, "CREATE TABLE goals (goal TEXT); INSERT INTO goals VALUES ('protected-goal-sentinel');")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create history fixture: %v %s", err, output)
	}
	var captured codexhistory.Result
	var manifestBefore []byte
	var events []string
	refusal := executeNativeSlotDestructiveBoundary(nativeSlotDestructiveBoundary{
		stopSelected: func() error { events = append(events, "stop"); return nil },
		inspectIdle: func() error {
			events = append(events, "inspect")
			plan, err := planNativeSlotProcesses(selected, false)
			if err == nil && len(plan.pids) != 0 {
				t.Fatal("unexpected owned process")
			}
			return err
		},
		snapshotHistory: func() error {
			events = append(events, "snapshot")
			var err error
			captured, err = codexhistory.Capture(codexhistory.SnapshotOptions{
				Project: "dev-all", ResetKind: "selected-slot-reset", AgentIndex: selected.index,
				HostWorktree: selected.hostWorktree, HostHome: selected.hostHome,
				SandboxHome: selected.sandboxHome, StateRoot: filepath.Dir(selected.stateRoot),
				WorkspaceRoot: filepath.Dir(selected.hostWorktree),
			})
			if err != nil {
				return err
			}
			manifestBefore, err = os.ReadFile(captured.ManifestPath)
			if err != nil {
				return err
			}
			writePhysicalProc(t, 803, 73, "/", nil, selected.hostWorktree)
			return nil
		},
		applyPlan: func() error { events = append(events, "apply"); return nil },
	})
	if refusal == nil || !strings.Contains(refusal.Error(), "post-history process recheck") || !strings.Contains(refusal.Error(), "parent_pid=73") || !strings.Contains(refusal.Error(), "start_ticks=100803") {
		t.Fatalf("post-history refusal lost its captured identity: %v", refusal)
	}
	if !reflect.DeepEqual(events, []string{"stop", "inspect", "snapshot", "inspect"}) {
		t.Fatalf("destructive step followed refusal: %v", events)
	}
	if captured.Status != "captured" || captured.BundleSHA256 == "" || captured.FileCount != 1 {
		t.Fatalf("history did not commit before refusal: %+v", captured)
	}
	manifestAfter, err := os.ReadFile(captured.ManifestPath)
	if err != nil || !bytes.Equal(manifestBefore, manifestAfter) {
		t.Fatalf("committed history manifest changed after refusal: %v", err)
	}
	for _, path := range []string{database, filepath.Join(filepath.Dir(captured.ManifestPath), "payload", "goals_1.sqlite")} {
		out, err := exec.Command(sqlite, path, "SELECT goal FROM goals;").CombinedOutput()
		if err != nil || string(out) != "protected-goal-sentinel\n" {
			t.Fatalf("history lost after refusal: %v %s", err, out)
		}
	}
	if strings.Contains(refusal.Error(), "sentinel") {
		t.Fatalf("refusal disclosed history: %v", refusal)
	}
}

package nativecmd

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
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

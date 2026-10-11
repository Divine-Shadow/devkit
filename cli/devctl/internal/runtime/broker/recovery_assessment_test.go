package broker

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveryAssessmentRequiresExactInterruptedStopWitness(t *testing.T) {
	state := &State{PID: 42}
	status := Status{
		PID:          0,
		State:        state,
		StateDigest:  "state-digest",
		BindingError: "broker PID and state bindings disagree",
	}
	intent := &stopIntent{InProgress: true, RecordedPID: 42, StateSHA256: "state-digest", PIDSHA256: "original-pid-digest"}
	got := classifyRecovery(Config{}, status, intent, false)
	if got.Classification != "interrupted-stop-candidate" || !got.SnapshotOnly {
		t.Fatalf("exact retained intent was not classified: %#v", got)
	}
	for name, change := range map[string]func(*stopIntent){
		"not-in-progress":  func(x *stopIntent) { x.InProgress = false },
		"wrong-pid":        func(x *stopIntent) { x.RecordedPID++ },
		"wrong-state":      func(x *stopIntent) { x.StateSHA256 = "other" },
		"missing-pid-hash": func(x *stopIntent) { x.PIDSHA256 = "" },
	} {
		t.Run(name, func(t *testing.T) {
			modified := *intent
			change(&modified)
			got := classifyRecovery(Config{}, status, &modified, false)
			if got.Classification != "refused" || got.Reason != "pid-state-disagree-without-matching-intent" {
				t.Fatalf("mismatched intent admitted: %#v", got)
			}
		})
	}
	if got := classifyRecovery(Config{}, status, intent, true); got.Classification != "refused" {
		t.Fatalf("present PID file admitted absent-PID continuation: %#v", got)
	}
	status.PID = 99
	if got := classifyRecovery(Config{}, status, intent, true); got.Classification != "refused" {
		t.Fatalf("different PID admitted: %#v", got)
	}
}

func TestRecoveryAssessmentDoesNotGrantStop(t *testing.T) {
	status := Status{PID: 42, State: &State{PID: 42}, Running: true, SocketExists: true}
	got := classifyRecovery(Config{}, status, nil, true)
	if got.Classification != "ordinary-binding" || !got.SnapshotOnly || got.SchemaVersion == "" {
		t.Fatalf("ordinary snapshot is not bounded: %#v", got)
	}
	status.BindingError = "broker socket identity differs from recorded owner"
	got = classifyRecovery(Config{}, status, nil, true)
	if got.Classification != "refused" {
		t.Fatalf("foreign binding admitted: %#v", got)
	}
	status.BindingError = ""
	status.Running = false
	status.StaleState = true
	got = classifyRecovery(Config{}, status, nil, true)
	if got.Classification != "refused" || got.Reason != "selected-owner-not-running-or-stale" {
		t.Fatalf("stale dead PID described as ordinary: %#v", got)
	}
}

func TestAssessRecoveryReadsOwnedStaleMetadataWithoutChangingIt(t *testing.T) {
	root := t.TempDir()
	c := Normalize(Config{StateRoot: root, Socket: filepath.Join(root, "broker.sock")})
	c.Owner = &StationOwner{}
	state := stateFixture(c, 999999, 0)
	if err := writeState(c, state); err != nil {
		t.Fatal(err)
	}
	beforeState, err := os.ReadFile(StateFile(c))
	if err != nil {
		t.Fatal(err)
	}
	beforePID, err := os.ReadFile(PIDFile(c))
	if err != nil {
		t.Fatal(err)
	}
	got, err := AssessRecovery(c)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classification != "refused" || got.Reason != "selected-owner-not-running-or-stale" || !got.SnapshotOnly {
		t.Fatalf("owned stale metadata admitted: %#v", got)
	}
	afterState, err := os.ReadFile(StateFile(c))
	if err != nil || !bytes.Equal(beforeState, afterState) {
		t.Fatalf("assessment changed state: %v", err)
	}
	afterPID, err := os.ReadFile(PIDFile(c))
	if err != nil || !bytes.Equal(beforePID, afterPID) {
		t.Fatalf("assessment changed PID: %v", err)
	}
}

func TestAssessRecoveryRefusesChangedLegacyWitnessWithoutTouchingListener(t *testing.T) {
	c, pid := ownedLegacyStopFixture(t)
	beforeState, err := os.ReadFile(StateFile(c))
	if err != nil {
		t.Fatal(err)
	}
	beforePID, err := os.ReadFile(PIDFile(c))
	if err != nil {
		t.Fatal(err)
	}
	selected := *c.Owner
	witness := *selected.LegacyWitness
	witness.SocketInode++
	selected.LegacyWitness = &witness
	c.Owner = &selected
	got, err := AssessRecovery(c)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classification == "legacy-witness-candidate" || !got.SnapshotOnly {
		t.Fatalf("changed legacy socket witness admitted: %#v", got)
	}
	afterState, err := os.ReadFile(StateFile(c))
	if err != nil || !bytes.Equal(beforeState, afterState) {
		t.Fatalf("assessment changed legacy state: %v", err)
	}
	afterPID, err := os.ReadFile(PIDFile(c))
	if err != nil || !bytes.Equal(beforePID, afterPID) {
		t.Fatalf("assessment changed legacy PID: %v", err)
	}
	if !processRunning(pid) || !socketAcceptsConnections(c.Socket) {
		t.Fatal("assessment disturbed owned fixture listener")
	}
}

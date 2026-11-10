package fleetmode

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIAcceptsOnlyExplicitOperationalModesAndConfig(t *testing.T) {
	for _, args := range [][]string{nil, {"compute"}, {"vr"}, {"vr", "--config", "/missing", "extra"}, {"agent", "compute"}} {
		var out, err bytes.Buffer
		if code := Main(context.Background(), args, &out, &err); code != 2 || out.Len() != 0 {
			t.Fatalf("args=%v code=%d out=%q", args, code, out.String())
		}
	}
}

func TestLoadConfigRefusesUnknownFieldsAndTrailingRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fleet.json")
	for _, body := range []string{`{"schemaVersion":1,"stations":[],"authority":"invented"}`, `{} {}`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err == nil {
			t.Fatal("ambiguous configuration accepted")
		}
	}
}

func TestFileStateRecordsVRIntentWithoutComputeRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intent", "mode.json")
	if err := (FileState{Path: path}).Record(context.Background(), "vr", []string{"Derp", "Talos", "Lich", "Queen"}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(body), `"mode":"vr"`) {
		t.Fatalf("mode intent %s %v", body, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("mode intent must be private", err)
	}
	if err := (FileState{Path: path}).Record(context.Background(), "vr", []string{"Derp", "Talos", "Lich", "Queen"}); err != nil {
		t.Fatal("repeat intent replacement failed", err)
	}
}

func TestCLIResultUsesVerifiedFactsDespiteMarkerWarning(t *testing.T) {
	r := StationResult{Mode: "vr", WSLOff: true, RestartInhibited: true, Error: "mode recording failed"}
	if !verifiedVRResult(r) {
		t.Fatal("marker-only warning hid verified shutdown")
	}
	for _, change := range []func(*StationResult){func(v *StationResult) { v.Mode = "status" }, func(v *StationResult) { v.WSLOff = false }, func(v *StationResult) { v.RestartInhibited = false }} {
		v := r
		change(&v)
		if verifiedVRResult(v) {
			t.Fatal("unverified VR result accepted")
		}
	}
}

func TestCLIStatusExitReportsObservationInsteadOfVRState(t *testing.T) {
	r := StationResult{Mode: "status"}
	if agentResultExit("status", r) != 0 || agentResultExit("vr", r) != 1 {
		t.Fatal("status and VR success conflated")
	}
	r.Error = "observation failed"
	if agentResultExit("status", r) != 1 {
		t.Fatal("failed status query passed")
	}
	result := Result{Mode: "status", Stations: make([]StationResult, 4)}
	if controllerResultExit("status", result) != 0 || controllerResultExit("vr", result) != 1 {
		t.Fatal("controller status and VR success conflated")
	}
	result.Stations[2].Error = "unreachable"
	if controllerResultExit("status", result) != 1 {
		t.Fatal("unverified station status passed")
	}
}

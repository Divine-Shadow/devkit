//go:build linux

package broker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func failedObservationFixture(t *testing.T, body string) Config {
	t.Helper()
	t.Setenv("INVOCATION_ID", "")
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	helper := filepath.Join(root, "fixed-observation-fixture")
	if err := os.WriteFile(helper, []byte("#!"+shell+"\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := Normalize(Config{StateRoot: root, Socket: filepath.Join(root, "broker.sock"), Binary: binary})
	c.Owner = &StationOwner{Service: StationOwnerService, SystemdRun: helper, ObservationExecutable: "/nix/store/fixture/bin/station-broker-observe"}
	if err := writeState(c, stateFixture(c, os.Getpid(), 1)); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestFailedStationObservationIsNotDifferentProcessEvidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"execution", "printf private-fixture-detail >&2; exit 3"},
		{"malformed-result", "printf malformed; exit 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := failedObservationFixture(t, tc.body)
			before, err := os.ReadFile(StateFile(c))
			if err != nil {
				t.Fatal(err)
			}
			status, err := Inspect(c)
			if err == nil || status.Running || strings.Contains(status.BindingError, "different process") {
				t.Fatalf("failed observation became different-process evidence: error=%v binding=%q", err, status.BindingError)
			}
			if strings.Contains(err.Error(), "private-fixture-detail") {
				t.Fatal("raw helper diagnostics were exposed")
			}
			if _, err := requestStationStart(context.Background(), c, true, false); err == nil {
				t.Fatal("failed observation admitted ordinary acquisition")
			}
			after, err := os.ReadFile(StateFile(c))
			if err != nil || string(before) != string(after) {
				t.Fatal("failed observation changed retained metadata")
			}
			if _, err := os.Stat(stopIntentFile(c)); !os.IsNotExist(err) {
				t.Fatal("failed observation created stop intent")
			}
		})
	}
}

func TestStationObservationSeparatesCompletedMismatchAndUnknownResult(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		mutate  func(*stationObservationResult)
		matched bool
		unknown bool
	}{
		{"matching", func(*stationObservationResult) {}, true, false},
		{"different-pid", func(r *stationObservationResult) { r.PID++ }, false, false},
		{"different-start", func(r *stationObservationResult) { r.StartTicks++ }, false, false},
		{"different-binary", func(r *stationObservationResult) { r.Binary += "-different" }, false, false},
		{"unsupported-schema", func(r *stationObservationResult) { r.SchemaVersion = "unsupported" }, false, true},
		{"zero-pid", func(r *stationObservationResult) { r.PID = 0 }, false, true},
		{"zero-start", func(r *stationObservationResult) { r.StartTicks = 0 }, false, true},
		{"empty-binary", func(r *stationObservationResult) { r.Binary = "" }, false, true},
		{"relative-binary", func(r *stationObservationResult) { r.Binary = "relative/broker" }, false, true},
		{"noncanonical-binary", func(r *stationObservationResult) { r.Binary = "/nix/store/fixture/../broker" }, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := stationObservationResult{SchemaVersion: stationObservationSchema, PID: os.Getpid(), StartTicks: 1, Binary: binary}
			tc.mutate(&result)
			data, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			body := "printf '%s' '" + strings.ReplaceAll(string(data), "'", "'\"'\"'") + "'"
			c := failedObservationFixture(t, body)
			matched, err := observedStationProcess(c, &State{PID: os.Getpid(), StartTicks: 1})
			if matched != tc.matched || errors.Is(err, errStationObservationUnavailable) != tc.unknown || (err != nil) != tc.unknown {
				t.Fatalf("completed observation matched=%v error=%v", matched, err)
			}
		})
	}
}

func TestIncompleteStationObservationTupleIsUnknown(t *testing.T) {
	for _, payload := range []string{
		`{"schemaVersion":"devkit-native-broker-observation/v1"}`,
		`{"schemaVersion":"devkit-native-broker-observation/v1","pid":null,"startTicks":null,"binary":null}`,
	} {
		c := failedObservationFixture(t, "printf '%s' '"+payload+"'")
		status, err := Inspect(c)
		if !errors.Is(err, errStationObservationUnavailable) || status.Running || strings.Contains(status.BindingError, "different process") {
			t.Fatalf("incomplete tuple became mismatch evidence: error=%v binding=%q", err, status.BindingError)
		}
	}
}

func TestStationObservationExcessiveOutputIsUnknownAndBounded(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			body := "printf '%05000d' 0"
			if stream == "stderr" {
				body += " >&2"
			}
			c := failedObservationFixture(t, body)
			matched, err := observedStationProcess(c, &State{PID: os.Getpid(), StartTicks: 1})
			if matched || !errors.Is(err, errStationObservationUnavailable) || !strings.Contains(err.Error(), "phase=output-limit") {
				t.Fatalf("excessive %s result=%v error=%v", stream, matched, err)
			}
			if len(err.Error()) > 1024 {
				t.Fatal("observation failure exposed excessive output")
			}
		})
	}
}

func TestStationObservationTimeoutStaysUnknown(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	c := failedObservationFixture(t, "exec '"+strings.ReplaceAll(sleep, "'", "'\"'\"'")+"' 30")
	started := time.Now()
	matched, err := observedStationProcess(c, &State{PID: os.Getpid(), StartTicks: 1})
	if matched || !errors.Is(err, errStationObservationUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline observation matched=%v error=%v", matched, err)
	}
	if time.Since(started) > 8*time.Second {
		t.Fatal("fixed observation exceeded its owned deadline and pipe cleanup")
	}
}

//go:build linux

package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStationObservationRejectsMalformedOrUnboundedEvidence(t *testing.T) {
	valid := `{"schemaVersion":"devkit-native-broker-observation/v1","pid":123,"startTicks":456,"binary":"/nix/store/fixture/bin/broker"}`
	for _, data := range []string{"", valid[:len(valid)-1], valid + `{}`, strings.Replace(valid, `"pid":123`, `"pid":"123"`, 1), strings.Replace(valid, `"pid":123`, `"unselected":123`, 1), strings.Repeat(" ", stationObservationLimit+1)} {
		var value stationObservationResult
		if err := decodeStationObservation([]byte(data), &value); err == nil {
			t.Fatalf("invalid observation admitted: %q", data[:min(len(data), 160)])
		}
	}
	var got stationObservationResult
	if err := decodeStationObservation([]byte(valid+"\n"), &got); err != nil || got.PID != 123 || got.StartTicks != 456 {
		t.Fatalf("valid retained tuple lost: %+v, %v", got, err)
	}
}

func TestStationObservationOutputBoundIsEnforcedDuringCopy(t *testing.T) {
	var b stationObservationBuffer
	if _, err := io.Copy(&b, bytes.NewReader(bytes.Repeat([]byte("x"), stationObservationLimit+1))); err == nil || b.Len() > stationObservationLimit {
		t.Fatalf("unbounded observation buffered: bytes=%d error=%v", b.Len(), err)
	}
}

func TestStationObservationSelectsOnlyImmutableFixedJob(t *testing.T) {
	_, owner := requestedOwnerFixture(t)
	state := &State{PID: 123, StartTicks: 456}
	command := stationObservationCommand(context.Background(), &owner, state)
	want := []string{owner.SystemdRun, "--user", "--quiet", "--wait", "--pipe", "--collect", "--service-type=exec", "--property=NoNewPrivileges=yes", "--property=RuntimeMaxSec=5s", "--property=KillMode=control-group", "--property=TimeoutStopSec=1s", "--property=SendSIGKILL=yes", "--", owner.ObservationExecutable, "123", "456"}
	if !reflect.DeepEqual(command.Args, want) || command.WaitDelay != time.Second || len(command.Env) != 4 || command.Env[0] != "PATH=/no-ambient-path" {
		t.Fatalf("fixed observation job widened: argv=%q env=%q", command.Args, command.Env)
	}
}

func TestSelectedOwnerRejectsUnselectedObservationArtifacts(t *testing.T) {
	for _, mutate := range []func(*StationOwner){
		func(o *StationOwner) { o.SystemdRun = "" },
		func(o *StationOwner) { o.ObservationExecutable = "/tmp/observer" },
		func(o *StationOwner) { o.SystemdRun = "/nix/store/tool/../bin/systemd-run" },
	} {
		c, owner := requestedOwnerFixture(t)
		mutate(&owner)
		if _, err := bindStationOwner(c, owner); err == nil {
			t.Fatal("unselected observation capability admitted")
		}
	}
}

func TestObservationHelperRefusesUnselectedPolicyAndInvalidTuple(t *testing.T) {
	for _, args := range [][]string{nil, {"1"}, {"0", "1"}, {"1", "0"}, {"01", "1"}, {"1", "01"}, {"1", "1", "extra"}, {"1", "1"}} {
		var stdout bytes.Buffer
		if err := ObserveSelectedStationProcess(args, &stdout); err == nil || stdout.Len() != 0 {
			t.Fatalf("unselected observation emitted evidence: %q %s %v", args, stdout.Bytes(), err)
		}
	}
	// Keep the result's representation explicit for the packaged VM caller.
	data, err := json.Marshal(stationObservationResult{SchemaVersion: stationObservationSchema, PID: 123, StartTicks: 456, Binary: "/nix/store/fixture/bin/broker"})
	if err != nil || !bytes.Contains(data, []byte(`"startTicks":456`)) {
		t.Fatal("observation result lost retained identity")
	}
}

func TestStationObservationPipeCopyCannotBypassByteBound(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	written := make(chan error, 1)
	go func() {
		_, err := w.Write(bytes.Repeat([]byte("x"), stationObservationLimit+1))
		w.Close()
		written <- err
	}()
	var b stationObservationBuffer
	if _, err := io.Copy(&b, r); err == nil || b.Len() > stationObservationLimit {
		t.Fatalf("pipe bypassed observation bound: bytes=%d error=%v", b.Len(), err)
	}
	r.Close()
	select {
	case <-written:
	case <-time.After(time.Second):
		t.Fatal("owned pipe writer was not reaped")
	}
}

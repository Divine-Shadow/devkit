package fleetmode

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type transportFunc func(context.Context, Station, string) (StationResult, error)

func (f transportFunc) Call(c context.Context, s Station, m string) (StationResult, error) {
	return f(c, s, m)
}

type stateFunc func(context.Context, string, []string) error

func (f stateFunc) Record(c context.Context, m string, n []string) error { return f(c, m, n) }

func testConfig() Config {
	c := Config{SchemaVersion: 1, StatePath: "/unused/mode.json"}
	for i, n := range []string{"Derp", "Talos", "Lich", "Queen"} {
		c.Stations = append(c.Stations, Station{Name: n, Host: fmt.Sprintf("100.64.0.%d", i+1), Port: 22, User: "operator", ExpectedHostname: n + "-PC", IdentityFile: "/fake/identity", KnownHostsFile: "/fake/known-hosts"})
	}
	return c
}

func TestDuplicatePhysicalStationConfigRefusesBeforeAnyEffect(t *testing.T) {
	for _, kind := range []string{"hostname-case", "same-endpoint", "mapped-ip", "ipv6-canonical", "dns-case-and-dot"} {
		t.Run(kind, func(t *testing.T) {
			cfg := testConfig()
			switch kind {
			case "hostname-case":
				cfg.Stations[1].ExpectedHostname = strings.ToLower(cfg.Stations[0].ExpectedHostname)
			case "same-endpoint":
				cfg.Stations[1].Host = cfg.Stations[0].Host
			case "mapped-ip":
				cfg.Stations[1].Host = "::ffff:100.64.0.1"
			case "ipv6-canonical":
				cfg.Stations[0].Host = "fd00::1"
				cfg.Stations[1].Host = "fd00:0:0:0:0:0:0:1"
			case "dns-case-and-dot":
				cfg.Stations[0].Host = "DERP.example.test"
				cfg.Stations[1].Host = "derp.example.test."
			}
			var calls, records atomic.Int32
			r := RunController(context.Background(), "vr", cfg, transportFunc(func(_ context.Context, s Station, _ string) (StationResult, error) {
				calls.Add(1)
				return verified(s), nil
			}), stateFunc(func(context.Context, string, []string) error { records.Add(1); return nil }))
			if r.Error == "" || r.ShutdownSucceeded || calls.Load() != 0 || records.Load() != 0 {
				t.Fatalf("duplicate physical station dispatched or recorded intent: %+v", r)
			}
		})
	}
}
func verified(s Station) StationResult {
	return StationResult{Hostname: s.ExpectedHostname, Mode: "vr", WSLOff: true, RestartInhibited: true}
}

func TestControllerDispatchesAllFourInParallelDespiteMarkerFailure(t *testing.T) {
	started := make(chan string, 4)
	release := make(chan struct{})
	done := make(chan Result, 1)
	transport := transportFunc(func(c context.Context, s Station, m string) (StationResult, error) {
		started <- s.Name
		select {
		case <-release:
			return verified(s), nil
		case <-c.Done():
			return StationResult{}, c.Err()
		}
	})
	go func() {
		done <- RunController(context.Background(), "vr", testConfig(), transport, stateFunc(func(context.Context, string, []string) error { return errors.New("disk unavailable") }))
	}()
	for i := 0; i < 4; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("station dispatch serialized")
		}
	}
	close(release)
	r := <-done
	if !r.ShutdownSucceeded || r.ModeRecorded || !strings.Contains(r.Error, "recording failed") {
		t.Fatalf("marker failure prevented verified shutdown: %+v", r)
	}
}

func TestControllerDeadlineAndUnreachableNeverMeanOff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	transport := transportFunc(func(c context.Context, s Station, m string) (StationResult, error) {
		switch s.Name {
		case "Derp":
			<-c.Done()
			return StationResult{}, c.Err()
		case "Talos":
			return verified(s), errors.New("disconnected")
		default:
			return verified(s), nil
		}
	})
	began := time.Now()
	r := RunController(ctx, "vr", testConfig(), transport, stateFunc(func(context.Context, string, []string) error { return nil }))
	if time.Since(began) > time.Second || r.ShutdownSucceeded {
		t.Fatalf("unbounded/false success: %+v", r)
	}
	for _, s := range r.Stations {
		if (s.Name == "Derp" || s.Name == "Talos") && (s.WSLOff || s.RestartInhibited || s.Error == "") {
			t.Fatalf("unverified station claimed off: %+v", s)
		}
	}
}

func TestWrongHostAndPartialResultRefuseSuccess(t *testing.T) {
	for _, kind := range []string{"wrong-host", "missing-off", "wrong-mode"} {
		t.Run(kind, func(t *testing.T) {
			r := RunController(context.Background(), "vr", testConfig(), transportFunc(func(_ context.Context, s Station, _ string) (StationResult, error) {
				v := verified(s)
				if s.Name == "Queen" {
					switch kind {
					case "wrong-host":
						v.Hostname = "ANOTHER-PC"
					case "missing-off":
						v.WSLOff = false
					case "wrong-mode":
						v.Mode = "compute"
					}
				}
				return v, nil
			}), stateFunc(func(context.Context, string, []string) error { return nil }))
			if r.ShutdownSucceeded {
				t.Fatalf("false shutdown success: %+v", r)
			}
		})
	}
}

func TestStatusNeverRecordsOrRestoresAndComputeIsRefused(t *testing.T) {
	var calls, records atomic.Int32
	tr := transportFunc(func(_ context.Context, s Station, m string) (StationResult, error) {
		if m != "status" {
			t.Errorf("unexpected verb %s", m)
		}
		calls.Add(1)
		return verified(s), nil
	})
	st := stateFunc(func(context.Context, string, []string) error { records.Add(1); return nil })
	r := RunController(context.Background(), "status", testConfig(), tr, st)
	if !r.ShutdownSucceeded || calls.Load() != 4 || records.Load() != 0 {
		t.Fatalf("status mutated mode: %+v", r)
	}
	r = RunController(context.Background(), "compute", testConfig(), tr, st)
	if r.Error == "" || calls.Load() != 4 || records.Load() != 0 {
		t.Fatal("implicit compute restoration")
	}
}

func TestHostileConfigurationRefusedBeforeDispatch(t *testing.T) {
	for _, change := range []func(*Config){func(c *Config) { c.Stations[0].Host = "-oProxyCommand=evil" }, func(c *Config) { c.Stations[0].User = "user;evil" }, func(c *Config) { c.Stations[0].ExpectedHostname = "PC\nforged" }, func(c *Config) { c.Stations[0].KnownHostsFile = "/tmp/file\nforged" }, func(c *Config) { c.Stations[0].Name = "Queen" }} {
		cfg := testConfig()
		change(&cfg)
		called := false
		r := RunController(context.Background(), "vr", cfg, transportFunc(func(context.Context, Station, string) (StationResult, error) {
			called = true
			return StationResult{}, nil
		}), nil)
		if called || r.Error == "" || r.ShutdownSucceeded {
			t.Fatal("hostile config dispatched")
		}
	}
}

func TestAuthenticatedWarningPreservesFactsButNotWrongHost(t *testing.T) {
	for _, wrongHost := range []bool{false, true} {
		r := RunController(context.Background(), "vr", testConfig(), transportFunc(func(_ context.Context, s Station, _ string) (StationResult, error) {
			v := verified(s)
			v.Error = "mode marker unavailable"
			if wrongHost {
				v.Hostname = "WRONG-PC"
			}
			return v, nil
		}), stateFunc(func(context.Context, string, []string) error { return nil }))
		if r.ShutdownSucceeded == wrongHost {
			t.Fatalf("warning/fact distinction: %+v", r)
		}
		for _, s := range r.Stations {
			if s.Error == "" {
				t.Fatal("warning lost")
			}
			if wrongHost && (s.WSLOff || s.RestartInhibited) {
				t.Fatal("warning masked wrong hostname")
			}
		}
	}
}

func TestRunningStatusIsVerifiedWithoutClaimingShutdown(t *testing.T) {
	r := RunController(context.Background(), "status", testConfig(), transportFunc(func(_ context.Context, s Station, _ string) (StationResult, error) {
		return StationResult{Hostname: s.ExpectedHostname, Mode: "status"}, nil
	}), nil)
	if r.ShutdownSucceeded || controllerResultExit("status", r) != 0 {
		t.Fatalf("valid running observations lost or called VR: %+v", r)
	}
	for _, station := range r.Stations {
		if station.Error != "" || agentResultExit("status", station) != 0 {
			t.Fatalf("running query failed: %+v", station)
		}
	}
}

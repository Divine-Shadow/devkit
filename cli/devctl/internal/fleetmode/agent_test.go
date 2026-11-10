package fleetmode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type fakeAgentOps struct {
	host                                                     string
	record                                                   func(context.Context, string) error
	stop                                                     func(context.Context, AgentConfig) ([]Stage, bool)
	force                                                    func(context.Context) ([]Stage, bool)
	observe                                                  func(context.Context) (bool, error)
	keeperObserve                                            func(context.Context, AgentConfig) (bool, error)
	records, stops, forces, observations, keeperObservations atomic.Int32
}

func (f *fakeAgentOps) Hostname() (string, error) { return f.host, nil }
func (f *fakeAgentOps) RecordMode(c context.Context, m string) error {
	f.records.Add(1)
	if f.record != nil {
		return f.record(c, m)
	}
	return nil
}
func (f *fakeAgentOps) InhibitRestart(c context.Context, a AgentConfig) ([]Stage, bool) {
	f.stops.Add(1)
	if f.stop != nil {
		return f.stop(c, a)
	}
	return nil, true
}
func (f *fakeAgentOps) ForceWSLOff(c context.Context) ([]Stage, bool) {
	f.forces.Add(1)
	if f.force != nil {
		return f.force(c)
	}
	return []Stage{{Name: "force-command", ExitCode: 0}}, true
}
func (f *fakeAgentOps) ObserveOff(c context.Context) (bool, error) {
	f.observations.Add(1)
	if f.observe != nil {
		return f.observe(c)
	}
	return true, nil
}
func (f *fakeAgentOps) ObserveRestartInhibition(c context.Context, a AgentConfig) (bool, error) {
	f.keeperObservations.Add(1)
	if f.keeperObserve != nil {
		return f.keeperObserve(c, a)
	}
	return true, nil
}

func agentFixture(t *testing.T) AgentConfig {
	return AgentConfig{SchemaVersion: 1, Name: "Derp", ExpectedHostname: "DERP-PC", StatePath: filepath.Join(t.TempDir(), "mode.json")}
}

func TestAgentMarkerHangDoesNotDelayShutdown(t *testing.T) {
	cfg := agentFixture(t)
	release := make(chan struct{})
	forced := make(chan struct{})
	done := make(chan StationResult, 1)
	ops := &fakeAgentOps{host: cfg.ExpectedHostname, record: func(context.Context, string) error { <-release; return errors.New("marker unavailable") }, force: func(context.Context) ([]Stage, bool) { close(forced); return nil, true }}
	go func() { done <- runAgentWithOps(context.Background(), "vr", cfg, ops) }()
	select {
	case <-forced:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("hung marker prevented force shutdown")
	}
	close(release)
	select {
	case r := <-done:
		if !r.WSLOff || !r.RestartInhibited || r.ModeRecorded {
			t.Fatalf("marker failure changed observed shutdown: %+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("marker result wait unbounded")
	}
}

func TestAgentKeeperHasFinitePhaseAndForceContinuesOnFailure(t *testing.T) {
	cfg := agentFixture(t)
	ops := &fakeAgentOps{host: cfg.ExpectedHostname, stop: func(c context.Context, _ AgentConfig) ([]Stage, bool) {
		deadline, ok := c.Deadline()
		if !ok || time.Until(deadline) > 20*time.Second {
			t.Error("keeper phase lacks its 20s bound")
		}
		return []Stage{{Name: "keeper", ExitCode: 1}}, false
	}}
	r := runAgentWithOps(context.Background(), "vr", cfg, ops)
	if ops.forces.Load() != 1 || !r.WSLOff || !r.RestartInhibited || r.Error == "" {
		t.Fatalf("keeper failure prevented force or claimed ready: %+v", r)
	}
}

func TestAgentHungKeeperRespectsParentBudget(t *testing.T) {
	cfg := agentFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	ops := &fakeAgentOps{host: cfg.ExpectedHostname, stop: func(c context.Context, _ AgentConfig) ([]Stage, bool) { <-c.Done(); return nil, false }, force: func(c context.Context) ([]Stage, bool) { return nil, false }, observe: func(context.Context) (bool, error) { return false, context.DeadlineExceeded }}
	began := time.Now()
	r := runAgentWithOps(ctx, "vr", cfg, ops)
	if time.Since(began) > time.Second || ops.forces.Load() != 1 || r.WSLOff || r.Error == "" {
		t.Fatalf("keeper budget/force custody: %+v", r)
	}
}

func TestAgentWrongHostnamePerformsNoMutationOrObservation(t *testing.T) {
	cfg := agentFixture(t)
	ops := &fakeAgentOps{host: "SOMEONE-ELSE"}
	r := runAgentWithOps(context.Background(), "vr", cfg, ops)
	if r.Error == "" || r.WSLOff || ops.records.Load()+ops.stops.Load()+ops.forces.Load()+ops.observations.Load()+ops.keeperObservations.Load() != 0 {
		t.Fatalf("wrong host touched operations: %+v", r)
	}
}

func TestAgentStatusOnlyObservesAndPreservesIntent(t *testing.T) {
	cfg := agentFixture(t)
	original := []byte(`{"mode":"vr"}`)
	if err := os.WriteFile(cfg.StatePath, original, 0600); err != nil {
		t.Fatal(err)
	}
	ops := &fakeAgentOps{host: cfg.ExpectedHostname}
	r := runAgentWithOps(context.Background(), "status", cfg, ops)
	after, err := os.ReadFile(cfg.StatePath)
	if err != nil || string(after) != string(original) || ops.records.Load()+ops.stops.Load()+ops.forces.Load() != 0 || ops.observations.Load() != 1 || ops.keeperObservations.Load() != 1 || !r.WSLOff || !r.RestartInhibited || r.Mode != "vr" {
		t.Fatalf("status changed state or failed observation: %+v err=%v", r, err)
	}
}

func TestAgentRequiresObservedAbsenceAfterSuccessfulForceCommand(t *testing.T) {
	for _, observationErr := range []error{nil, errors.New("observation unavailable")} {
		t.Run(func() string {
			if observationErr == nil {
				return "still-running"
			}
			return "observation-error"
		}(), func(t *testing.T) {
			cfg := agentFixture(t)
			ops := &fakeAgentOps{host: cfg.ExpectedHostname, observe: func(context.Context) (bool, error) { return false, observationErr }}
			r := runAgentWithOps(context.Background(), "vr", cfg, ops)
			if ops.forces.Load() != 1 || ops.observations.Load() != 1 || r.WSLOff || r.Error == "" {
				t.Fatalf("command success substituted for absence: %+v", r)
			}
		})
	}
}

func TestAgentStatusDoesNotTreatObservationErrorAsAbsence(t *testing.T) {
	cfg := agentFixture(t)
	ops := &fakeAgentOps{host: cfg.ExpectedHostname, observe: func(context.Context) (bool, error) { return false, errors.New("unknown") }, keeperObserve: func(context.Context, AgentConfig) (bool, error) { return false, errors.New("unknown") }}
	r := runAgentWithOps(context.Background(), "status", cfg, ops)
	if r.WSLOff || r.RestartInhibited || r.Error == "" || ops.records.Load()+ops.stops.Load()+ops.forces.Load() != 0 {
		t.Fatalf("unknown observation became off: %+v", r)
	}
}

func TestAgentFinalInhibitionObservationOwnsFact(t *testing.T) {
	for _, kind := range []string{"disabled-but-running", "observation-error"} {
		t.Run(kind, func(t *testing.T) {
			cfg := agentFixture(t)
			ops := &fakeAgentOps{host: cfg.ExpectedHostname, keeperObserve: func(context.Context, AgentConfig) (bool, error) {
				if kind == "observation-error" {
					return true, errors.New("unavailable")
				}
				return false, nil
			}}
			r := runAgentWithOps(context.Background(), "vr", cfg, ops)
			if ops.keeperObservations.Load() != 1 || r.RestartInhibited || !r.WSLOff || r.Error == "" {
				t.Fatalf("phase acknowledgement replaced final inhibition observation: %+v", r)
			}
		})
	}
}

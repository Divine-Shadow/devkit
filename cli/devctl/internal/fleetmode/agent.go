package fleetmode

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

func LoadAgentConfig(path string) (AgentConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return AgentConfig{}, err
	}
	defer f.Close()
	var cfg AgentConfig
	d := json.NewDecoder(io.LimitReader(f, 65536))
	d.DisallowUnknownFields()
	if err := d.Decode(&cfg); err != nil {
		return cfg, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return cfg, errors.New("agent config contains trailing data")
	}
	if cfg.SchemaVersion != 1 || cfg.Name == "" || cfg.ExpectedHostname == "" || cfg.StatePath == "" {
		return cfg, errors.New("agent config needs schemaVersion1, name, expectedHostname and statePath")
	}
	return cfg, nil
}

func RunAgent(ctx context.Context, mode string, cfg AgentConfig) StationResult {
	ops, err := NewAgentOps(cfg)
	if err != nil {
		return StationResult{Name: cfg.Name, Mode: mode, Error: err.Error()}
	}
	return runAgentWithOps(ctx, mode, cfg, ops)
}

func runAgentWithOps(ctx context.Context, mode string, cfg AgentConfig, ops AgentOps) StationResult {
	started := time.Now()
	result := StationResult{Name: cfg.Name, Mode: mode}
	finish := func() StationResult { result.ElapsedMillis = time.Since(started).Milliseconds(); return result }
	if mode != "vr" && mode != "status" {
		result.Error = "agent supports vr or status"
		return finish()
	}
	host, err := ops.Hostname()
	result.Hostname = host
	if err != nil || !strings.EqualFold(host, cfg.ExpectedHostname) {
		result.Error = "station hostname does not match its installed configuration"
		return finish()
	}
	ctx, cancel := context.WithTimeout(ctx, AgentBudget)
	defer cancel()
	var failures []string
	if mode == "status" {
		var stored struct {
			Mode string `json:"mode"`
		}
		if raw, err := os.ReadFile(cfg.StatePath); err == nil && json.Unmarshal(raw, &stored) == nil {
			result.Mode = stored.Mode
			result.ModeRecorded = stored.Mode == "vr"
		}
		observedInhibition, err := ops.ObserveRestartInhibition(ctx, cfg)
		result.RestartInhibited = observedInhibition && err == nil
		if err != nil {
			failures = append(failures, "restart-inhibition observation: "+err.Error())
		}
		observedOff, err := ops.ObserveOff(ctx)
		result.WSLOff = observedOff && err == nil
		if err != nil {
			failures = append(failures, "WSL observation: "+err.Error())
		}
	} else {
		// A bookkeeping failure must never delay the customer's forced shutdown.
		marker := make(chan Stage, 1)
		go func() {
			phase := time.Now()
			err := ops.RecordMode(ctx, "vr")
			s := Stage{Name: "record-vr-mode", ElapsedMillis: time.Since(phase).Milliseconds()}
			if err != nil {
				s.ExitCode = 1
				s.Error = err.Error()
			}
			marker <- s
		}()
		keeperCtx, keeperCancel := context.WithTimeout(ctx, 20*time.Second)
		stages, stopped := ops.InhibitRestart(keeperCtx, cfg)
		keeperCancel()
		result.Stages = append(result.Stages, stages...)
		if !stopped {
			failures = append(failures, "restart-inhibition phase did not confirm success")
		}
		// Continue even after an unavailable keep-alive or full marker filesystem.
		stages, off := ops.ForceWSLOff(ctx)
		result.Stages = append(result.Stages, stages...)
		observedOff, observeErr := ops.ObserveOff(ctx)
		result.WSLOff = off && observedOff && observeErr == nil
		if !result.WSLOff {
			failures = append(failures, "WSL shutdown is not confirmed")
		}
		if observeErr != nil {
			failures = append(failures, "WSL observation: "+observeErr.Error())
		}
		// The final actual service/task observation owns this invariant. An
		// earlier phase failure remains a warning and cannot fabricate or erase
		// later verified inhibition.
		observedInhibition, inhibitionErr := ops.ObserveRestartInhibition(ctx, cfg)
		result.RestartInhibited = observedInhibition && inhibitionErr == nil
		if !result.RestartInhibited {
			failures = append(failures, "restart inhibition is not confirmed")
		}
		if inhibitionErr != nil {
			failures = append(failures, "restart-inhibition observation: "+inhibitionErr.Error())
		}
		markerWait := time.NewTimer(100 * time.Millisecond)
		select {
		case s := <-marker:
			result.Stages = append(result.Stages, s)
			result.ModeRecorded = s.ExitCode == 0
			if s.Error != "" {
				failures = append(failures, "mode recording: "+s.Error)
			}
		case <-markerWait.C:
			failures = append(failures, "mode recording not confirmed")
		case <-ctx.Done():
			failures = append(failures, "mode recording not confirmed")
		}
		markerWait.Stop()
	}
	if ctx.Err() != nil {
		failures = append(failures, ctx.Err().Error())
	}
	result.Error = strings.Join(failures, "; ")
	return finish()
}

package fleetmode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var safeHost = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)
var safeUser = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_. '\\-]*$`)

func LoadConfig(path string) (Config, error) {
	var cfg Config
	f, err := os.Open(path)
	if err != nil {
		return cfg, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if err = d.Decode(&cfg); err != nil {
		return cfg, err
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return cfg, errors.New("configuration must contain one JSON object")
	}
	return cfg, ValidateConfig(cfg)
}

func ValidateConfig(cfg Config) error {
	if cfg.SchemaVersion != 1 || len(cfg.Stations) != 4 {
		return errors.New("schemaVersion 1 and exactly Derp, Talos, Lich, Queen are required")
	}
	seen := map[string]bool{}
	seenHostnames := map[string]bool{}
	seenEndpoints := map[string]bool{}
	for _, s := range cfg.Stations {
		name := strings.ToLower(s.Name)
		if name != "derp" && name != "talos" && name != "lich" && name != "queen" {
			return fmt.Errorf("unknown station %q", s.Name)
		}
		if seen[name] {
			return fmt.Errorf("duplicate station %q", s.Name)
		}
		seen[name] = true
		if err := validateStation(s); err != nil {
			return err
		}
		hostname := strings.ToLower(s.ExpectedHostname)
		if seenHostnames[hostname] {
			return fmt.Errorf("duplicate expected hostname for %s", s.Name)
		}
		seenHostnames[hostname] = true
		host := strings.TrimSuffix(strings.ToLower(s.Host), ".")
		if ip := net.ParseIP(s.Host); ip != nil {
			host = ip.String()
		}
		endpoint := net.JoinHostPort(host, fmt.Sprint(s.Port))
		if seenEndpoints[endpoint] {
			return fmt.Errorf("duplicate SSH endpoint for %s", s.Name)
		}
		seenEndpoints[endpoint] = true
	}
	return nil
}

func validateStation(s Station) error {
	if !safeHost.MatchString(s.Host) && net.ParseIP(s.Host) == nil {
		return fmt.Errorf("invalid host for %s", s.Name)
	}
	if !safeHost.MatchString(s.ExpectedHostname) || !safeUser.MatchString(s.User) || s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("invalid connection or expected hostname for %s", s.Name)
	}
	for _, path := range []string{s.IdentityFile, s.KnownHostsFile} {
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
			return fmt.Errorf("absolute identity and pinned known-hosts paths required for %s", s.Name)
		}
	}
	return nil
}

func RunController(parent context.Context, mode string, cfg Config, transport Transport, state State) Result {
	started := time.Now()
	result := Result{Mode: mode, StartedUTC: started.UTC().Format(time.RFC3339Nano), DeadlineMillis: DispatchBudget.Milliseconds()}
	if mode != "vr" && mode != "status" {
		result.Error = "supported modes are vr and status; compute restoration is never implicit"
		return result
	}
	if err := ValidateConfig(cfg); err != nil {
		result.Error = err.Error()
		return result
	}
	if transport == nil {
		result.Error = "transport unavailable"
		return result
	}
	ctx, cancel := context.WithTimeout(parent, DispatchBudget)
	defer cancel()
	if deadline, ok := ctx.Deadline(); ok {
		result.DeadlineMillis = time.Until(deadline).Milliseconds()
	}
	type answer struct {
		index  int
		result StationResult
	}
	answers := make(chan answer, len(cfg.Stations))
	marker := make(chan error, 1)
	names := make([]string, len(cfg.Stations))
	result.Stations = make([]StationResult, len(cfg.Stations))
	for i, station := range cfg.Stations {
		names[i] = station.Name
		result.Stations[i] = StationResult{Name: station.Name, Error: "no verified remote result"}
		go func(index int, s Station) {
			r, err := transport.Call(ctx, s, mode)
			unverified := err != nil
			if err != nil {
				r.Error = joinError(r.Error, err.Error())
			}
			if !strings.EqualFold(r.Hostname, s.ExpectedHostname) {
				r.Error = joinError(r.Error, "remote hostname did not match configured station")
				unverified = true
			}
			if mode == "vr" && r.Mode != "vr" {
				r.Error = joinError(r.Error, "remote result did not confirm VR mode")
				unverified = true
			}
			if unverified {
				r.WSLOff = false
				r.RestartInhibited = false
			}
			r.Name = s.Name
			answers <- answer{index, r}
		}(i, station)
	}
	// Intent recording never serializes or prevents the four shutdown calls.
	if mode == "vr" {
		go func() {
			if state == nil {
				marker <- errors.New("mode state unavailable")
			} else {
				marker <- state.Record(ctx, mode, names)
			}
		}()
	}
	remaining := len(cfg.Stations)
	for remaining > 0 {
		select {
		case a := <-answers:
			result.Stations[a.index] = a.result
			remaining--
		case <-ctx.Done():
			result.Error = "dispatch deadline reached; missing results remain unverified"
			// SSH cancellation and its WaitDelay reap the owned local clients.
			// This short cleanup allowance stays inside the 180s outer budget.
			cleanup := time.NewTimer(3 * time.Second)
		drain:
			for remaining > 0 {
				select {
				case a := <-answers:
					result.Stations[a.index] = a.result
					remaining--
				case <-cleanup.C:
					break drain
				}
			}
			cleanup.Stop()
			remaining = 0
		}
	}
	if mode == "vr" {
		markerWait := time.NewTimer(100 * time.Millisecond)
		select {
		case err := <-marker:
			result.ModeRecorded = err == nil
			if err != nil {
				result.Error = joinError(result.Error, "mode intent recording failed: "+err.Error())
			}
		case <-markerWait.C:
			result.Error = joinError(result.Error, "mode intent recording not confirmed")
		case <-ctx.Done():
			result.Error = joinError(result.Error, "mode intent recording not confirmed")
		}
		markerWait.Stop()
	}
	result.ShutdownSucceeded = true
	for _, r := range result.Stations {
		if !r.WSLOff || !r.RestartInhibited {
			result.ShutdownSucceeded = false
		}
	}
	result.ElapsedMillis = time.Since(started).Milliseconds()
	return result
}

func joinError(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

type FileState struct{ Path string }

func (s FileState) Record(ctx context.Context, mode string, names []string) error {
	if !filepath.IsAbs(s.Path) {
		return errors.New("mode state path must be absolute")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.Path), ".fleet-mode-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(struct {
		Mode        string   `json:"mode"`
		Stations    []string `json:"stations"`
		RecordedUTC string   `json:"recordedUtc"`
	}{mode, names, time.Now().UTC().Format(time.RFC3339Nano)})
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.Path)
}

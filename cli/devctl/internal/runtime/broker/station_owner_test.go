package broker

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requestedOwnerFixture(t *testing.T) (Config, StationOwner) {
	t.Helper()
	root := t.TempDir()
	c := Config{DevkitRoot: root, StateRoot: filepath.Join(root, "owner"), Socket: filepath.Join(root, "owner", "broker.sock"), Upstream: DefaultUpstream, AllowedImages: []string{"postgres:latest", "testcontainers/ryuk:0.7.0"}, AllowPulls: true}
	normalized := Normalize(c)
	owner := StationOwner{SchemaVersion: "devkit-native-broker-owner/v1", Service: StationOwnerService, Systemctl: "/nix/store/fixture-systemd/bin/systemctl", SystemdRun: "/nix/store/fixture-systemd/bin/systemd-run", ObservationExecutable: "/nix/store/fixture-observer/bin/station-broker-observe", HostRoot: root, StateRoot: c.StateRoot, Socket: c.Socket, Binary: "/nix/store/fixture-broker/bin/postgres-broker", Upstream: c.Upstream, AllowedImages: c.AllowedImages, SocketAliases: normalized.SocketBindAliases, AllowPulls: true, LogLevel: "info"}
	return c, owner
}

func TestSelectedOwnerRefusesSameEndpointIncompatibleSourcePolicy(t *testing.T) {
	c, owner := requestedOwnerFixture(t)
	for _, tc := range []struct {
		name   string
		mutate func(*Config)
	}{
		{"images", func(c *Config) { c.AllowedImages = []string{"postgres:latest", "samcli/build-python3.13"} }},
		{"upstream", func(c *Config) { c.Upstream = "unix:///foreign/docker.sock" }},
		{"pulls", func(c *Config) { c.AllowPulls = false }},
		{"alias", func(c *Config) { c.SocketBindAliases = []string{"/foreign/broker.sock"} }},
		{"logging", func(c *Config) { c.LogLevel = "debug" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requested := c
			tc.mutate(&requested)
			if _, err := bindStationOwner(requested, owner); err == nil || !strings.Contains(err.Error(), "differs") {
				t.Fatalf("same endpoint incompatible policy admitted: %v", err)
			}
		})
	}
}

func TestSelectedOwnerOrdinaryAcquisitionWaitsForOverlappingInspection(t *testing.T) {
	c, owner := requestedOwnerFixture(t)
	selected, err := bindStationOwner(c, owner)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := lockLifecycle(context.Background(), selected, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if guard != nil {
			guard.Close()
		}
	}()
	done := make(chan error, 1)
	go func() { _, err := requestStationStart(context.Background(), selected, true, false); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("contended ordinary acquisition returned before lock release: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	guard.Close()
	guard = nil
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("bounded ordinary acquisition failed after lock release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ordinary acquisition failed to reuse serialized state")
	}
	if selected.StartTimeout != 5*time.Second {
		t.Fatalf("normalized start timeout lost: %s", selected.StartTimeout)
	}
}

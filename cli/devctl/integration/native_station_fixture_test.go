package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/runtime/broker"
	"gopkg.in/yaml.v3"
)

// These external CLI consequences require the same immutable owner and real
// private user manager. native-cli-fixed-owner-consequences executes them;
// the source-only checkout cannot supply that namespace and never claims it.
const nativeStationFixtureEnvironment = "DEVKIT_TEST_NATIVE_STATION_OWNER_MANIFEST"

func nativeStationFixture(t *testing.T) *broker.StationOwner {
	t.Helper()
	path := os.Getenv(nativeStationFixtureEnvironment)
	if path == "" {
		return nil
	}
	if !strings.HasPrefix(path, "/nix/store/") || filepath.Clean(path) != path {
		t.Fatal("native station fixture must come from the immutable Nix check")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var owner broker.StationOwner
	if err := json.Unmarshal(data, &owner); err != nil || owner.SchemaVersion != "devkit-native-broker-owner/v1" || owner.Service != broker.StationOwnerService {
		t.Fatalf("invalid native station fixture: %v", err)
	}
	return &owner
}

func requireNativeStationFixture(t *testing.T) {
	t.Helper()
	owner := nativeStationFixture(t)
	if owner == nil {
		t.Skip("executed by required native-cli-fixed-owner-consequences check with its real immutable station owner")
	}
	c := broker.Config{DevkitRoot: owner.HostRoot, StateRoot: owner.StateRoot, Socket: owner.Socket, Binary: owner.Binary, Upstream: owner.Upstream, AllowedImages: owner.AllowedImages, AllowPulls: owner.AllowPulls, LogLevel: owner.LogLevel, Owner: owner}
	assertReady := func() {
		t.Helper()
		status, err := broker.Inspect(c)
		if err != nil || !status.Running || status.State == nil || status.State.OwnerService != broker.StationOwnerService {
			t.Fatalf("real station fixture is not admitted: %+v %v", status, err)
		}
	}
	assertReady()
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	state, pid := read(broker.StateFile(c)), read(broker.PIDFile(c))
	socket, err := os.Lstat(c.Socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !bytes.Equal(state, read(broker.StateFile(c))) || !bytes.Equal(pid, read(broker.PIDFile(c))) {
			t.Fatal("target consequence changed shared owner metadata/generation")
		}
		now, err := os.Lstat(c.Socket)
		if err != nil || !os.SameFile(socket, now) {
			t.Fatalf("target consequence changed shared owner socket: %v", err)
		}
		assertReady()
	})
}

// Project fixtures select the supplier's complete existing policy. Only the
// broker section changes; relative target paths and SSH authorities stay intact.
func bindNativeStationFixture(t *testing.T, root string) {
	t.Helper()
	owner := nativeStationFixture(t)
	if owner == nil {
		return
	}
	path := filepath.Join(root, "overlays", "dev-all", "devkit.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var overlay map[string]any
	if err := yaml.Unmarshal(data, &overlay); err != nil {
		t.Fatal(err)
	}
	overlay["broker"] = map[string]any{"socket": owner.Socket, "upstream": owner.Upstream, "allowed_images": owner.AllowedImages, "allow_pulls": owner.AllowPulls, "log_level": owner.LogLevel}
	data, err = yaml.Marshal(overlay)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

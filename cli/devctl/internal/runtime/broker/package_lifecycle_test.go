package broker

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

func shortFixture(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "db-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}
func packageFixture(t *testing.T) Config {
	t.Helper()
	binary := os.Getenv("DEVKIT_BROKER_PACKAGE_TEST_BINARY")
	if binary == "" {
		t.Skip("actual package fixture is selected by the Nix check")
	}
	root := shortFixture(t)
	upstream := filepath.Join(root, "docker.sock")
	listener, err := net.Listen("unix", upstream)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_ping" {
			w.WriteHeader(404)
			return
		}
		w.Write([]byte("OK"))
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	cfg := Normalize(Config{StateRoot: filepath.Join(root, "owner"), Socket: filepath.Join(root, "owner", "broker.sock"),
		Upstream: "unix://" + upstream, Binary: binary, AllowedImages: []string{"postgres:latest"}, StartTimeout: time.Second})
	t.Cleanup(func() { _, _ = Stop(cfg, false) })
	return cfg
}
func packageProxy(t *testing.T, c Config, index int) (func() error, string) {
	t.Helper()
	endpoint := filepath.Join(filepath.Dir(c.StateRoot), fmt.Sprintf("agent%d.sock", index))
	cleanup, err := launch.StartPostgresEndpointProxy(nativeplan.Plan{
		Agent:          nativeagent.Spec{ID: nativeagent.ID{Project: "dev-all", Repo: "fixture", Index: index}},
		BrokerEndpoint: c.Socket, PostgresDockerSocket: endpoint,
		Env: map[string]string{"DEVKIT_RUNTIME_BROKER_BINARY": c.Binary},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup() })
	return cleanup, endpoint
}

func TestActualPackageDeadStaleLaunchAndHealthyReuse(t *testing.T) {
	c := packageFixture(t)
	if err := os.MkdirAll(c.StateRoot, 0700); err != nil {
		t.Fatal(err)
	}
	address := &net.UnixAddr{Name: c.Socket, Net: "unix"}
	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	info, err := socketInfo(c.Socket)
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	st := info.Sys().(*syscall.Stat_t)
	state := stateFixture(c, 999999, 1)
	state.SocketDevice = uint64(st.Dev)
	state.SocketInode = st.Ino
	if err := writeState(c, state); err != nil {
		t.Fatal(err)
	}
	// Reproduce the original per-agent failure against a dead shared endpoint.
	failedEndpoint := filepath.Join(filepath.Dir(c.StateRoot), "failed.sock")
	_, err = launch.StartPostgresEndpointProxy(nativeplan.Plan{
		Agent:          nativeagent.Spec{ID: nativeagent.ID{Project: "dev-all", Repo: "fixture", Index: 9}},
		BrokerEndpoint: c.Socket, PostgresDockerSocket: failedEndpoint,
		Env: map[string]string{"DEVKIT_RUNTIME_BROKER_BINARY": c.Binary},
	})
	if err == nil {
		t.Fatal("dead shared broker was admitted by per-agent proxy")
	}
	if _, err = os.Lstat(failedEndpoint); !os.IsNotExist(err) {
		t.Fatalf("failed proxy residue: %v", err)
	}
	before := time.Now()
	ready, err := EnsureReady(context.Background(), c, false)
	if err != nil {
		t.Fatal(err)
	}
	if !ready.Running || ready.PID == state.PID || time.Since(before) > 2*time.Second {
		t.Fatalf("unbounded or absent owner: %#v", ready)
	}
	_, sibling := packageProxy(t, c, 1)
	oldState, _ := os.ReadFile(StateFile(c))
	oldPID, _ := os.ReadFile(PIDFile(c))
	results := make(chan Status, 6)
	failures := make(chan error, 6)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			project := c
			project.DevkitRoot = fmt.Sprintf("/isolated-project-%d/devkit", index)
			result, e := EnsureReady(context.Background(), project, false)
			if e != nil {
				failures <- e
				return
			}
			results <- result
		}(i)
	}
	wg.Wait()
	close(results)
	close(failures)
	for e := range failures {
		t.Fatal(e)
	}
	for result := range results {
		if result.PID != ready.PID || result.State.StartTicks != ready.State.StartTicks ||
			result.State.SocketDevice != ready.State.SocketDevice || result.State.SocketInode != ready.State.SocketInode {
			t.Fatal("concurrent project acquisition split ownership")
		}
	}
	nowState, _ := os.ReadFile(StateFile(c))
	nowPID, _ := os.ReadFile(PIDFile(c))
	if string(nowState) != string(oldState) || string(nowPID) != string(oldPID) {
		t.Fatal("healthy reuse mutated custody")
	}
	if err := brokerPing(sibling, nil); err != nil {
		t.Fatalf("sibling readiness failed: %v", err)
	}
	// Endpoint disposal represents one target's rollback; the owner remains.
	dispose, target := packageProxy(t, c, 2)
	if err := dispose(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("target endpoint not disposed")
	}
	if err := brokerPing(sibling, nil); err != nil {
		t.Fatal("target disposal broke sibling")
	}
	t.Logf("actual package repaired absent PID/stale inode; owner pid=%d start=%d socket=%d:%d; six project acquisitions reused unchanged; real sibling ping OK", ready.PID, ready.State.StartTicks, ready.State.SocketDevice, ready.State.SocketInode)
}

func TestAbsentOwnerStopInhibitsAutomaticAcquisitionAndDryRun(t *testing.T) {
	executable, _ := os.Executable()
	root := shortFixture(t)
	c := Normalize(Config{StateRoot: filepath.Join(root, "owner"), Socket: filepath.Join(root, "owner", "broker.sock"), Binary: executable, StartTimeout: time.Second})
	if _, err := Start(context.Background(), c, true); err != nil {
		t.Fatal(err)
	}
	if _, err := Stop(c, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(c.StateRoot); !os.IsNotExist(err) {
		t.Fatal("dry-run created owner root")
	}
	if _, err := Stop(c, false); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureReady(context.Background(), c, false); err == nil || !strings.Contains(err.Error(), "inhibited") {
		t.Fatalf("deliberate absent-owner stop crossed: %v", err)
	}
	t.Setenv(brokerTestHelperMode, "listen")
	result, err := Start(context.Background(), c, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Running {
		t.Fatal("explicit owner start failed")
	}
	if _, err := Stop(c, false); err != nil {
		t.Fatal(err)
	}
}

func TestForeignSocketAndSymlinkAncestryAreUntouched(t *testing.T) {
	root := shortFixture(t)
	executable, _ := os.Executable()
	c := Normalize(Config{StateRoot: filepath.Join(root, "owner"), Socket: filepath.Join(root, "owner", "broker.sock"), Binary: executable})
	os.MkdirAll(c.StateRoot, 0700)
	foreign, err := net.Listen("unix", c.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	before, _ := os.Lstat(c.Socket)
	if _, err := Start(context.Background(), c, false); err == nil {
		t.Fatal("foreign socket admitted")
	}
	if _, err := Stop(c, false); err == nil {
		t.Fatal("foreign socket stopped")
	}
	after, _ := os.Lstat(c.Socket)
	if !os.SameFile(before, after) {
		t.Fatal("foreign socket replaced")
	}
	if _, err := os.Stat(stopIntentFile(c)); !os.IsNotExist(err) {
		t.Fatal("foreign refusal wrote stop intent")
	}
	target := filepath.Join(root, "untouched")
	os.Mkdir(target, 0700)
	alias := filepath.Join(root, "alias")
	os.Symlink(target, alias)
	unsafe := Normalize(Config{StateRoot: filepath.Join(alias, "owner"), Socket: filepath.Join(alias, "owner", "broker.sock"), Binary: executable})
	if _, err := Start(context.Background(), unsafe, false); err == nil {
		t.Fatal("symlinked ancestry admitted")
	}
	entries, _ := os.ReadDir(target)
	if len(entries) != 0 {
		t.Fatal("symlinked destination mutated")
	}
}

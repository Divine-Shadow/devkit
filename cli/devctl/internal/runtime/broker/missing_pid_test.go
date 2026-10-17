package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func missingPIDFixture(t *testing.T) Config {
	t.Helper()
	root := shortFixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := Normalize(Config{StateRoot: root, Socket: filepath.Join(root, "broker.sock"), Binary: executable})
	child := startBrokerTestHelper(t, "listen", "BROKER_LISTEN=unix://"+c.Socket)
	waitForBrokerTestSocket(t, c.Socket)
	ticks, err := processStart(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	info, err := socketInfo(c.Socket)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	state := stateFixture(c, child.Process.Pid, ticks)
	state.OwnerService, state.SocketDevice, state.SocketInode = StationOwnerService, uint64(st.Dev), st.Ino
	if err := writeState(c, state); err != nil {
		t.Fatal(err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	// A killed multithreaded helper can expose a retired leader before its
	// listener descriptors have closed. Admit only the fully retired fixture;
	// production reconciliation must continue refusing an accepting socket.
	for (processRunning(state.PID) || socketAcceptsConnections(c.Socket)) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if processRunning(state.PID) {
		t.Fatal("fixture process did not retire")
	}
	if socketAcceptsConnections(c.Socket) {
		t.Fatal("fixture listener did not retire")
	}
	if err := os.Remove(PIDFile(c)); err != nil {
		t.Fatal(err)
	}
	c.Owner = &StationOwner{Service: StationOwnerService, Systemctl: filepath.Join(root, "manager")}
	writeMissingPIDManager(t, c, "MainPID=0\nActiveState=inactive\n", "")
	return c
}

func writeMissingPIDManager(t *testing.T, c Config, identity, mutation string) {
	t.Helper()
	body := "#!/bin/sh\nset -eu\ncase \"$3\" in\nshow)\n" + mutation + "\nprintf '%s' '" + identity + "';;\nstop) exit 0;;\n*) exit 23;;\nesac\n"
	if err := os.WriteFile(c.Owner.Systemctl, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
}

func mutateMissingPIDState(t *testing.T, c Config, mutate func(*State)) {
	t.Helper()
	raw, err := os.ReadFile(StateFile(c))
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	mutate(&state)
	raw, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(StateFile(c), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestMissingPIDRetiredStationRecovery(t *testing.T) {
	for _, phase := range []string{"inactive", "failed"} {
		t.Run(phase, func(t *testing.T) {
			c := missingPIDFixture(t)
			writeMissingPIDManager(t, c, "MainPID=0\nActiveState="+phase+"\n", "")
			before, _ := os.ReadFile(StateFile(c))
			info, _ := socketInfo(c.Socket)
			observed, err := Inspect(c)
			if err != nil || observed.BindingError != "broker PID and state bindings disagree" {
				t.Fatalf("strict inspection changed: %+v %v", observed, err)
			}
			for _, action := range []func() (Status, error){
				func() (Status, error) { return requestStationStart(context.Background(), c, true, false) },
				func() (Status, error) { return requestStationStop(c, true) },
			} {
				if _, err := action(); err != nil {
					t.Fatal(err)
				}
			}
			after, _ := os.ReadFile(StateFile(c))
			now, _ := socketInfo(c.Socket)
			if string(before) != string(after) || !os.SameFile(info, now) {
				t.Fatal("dry run mutated retired state")
			}
			result, err := requestStationStop(c, false)
			if err != nil || result.State != nil || result.SocketExists {
				t.Fatalf("ordinary stop failed retired reconciliation: %+v %v", result, err)
			}
			for _, path := range []string{StateFile(c), PIDFile(c), c.Socket} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("retired residue %s: %v", path, err)
				}
			}
			if _, err := requestStationStart(context.Background(), c, false, false); err == nil {
				t.Fatal("stop did not inhibit automatic acquisition")
			}
		})
	}
}

func TestMissingPIDRefusesForeignLifetimes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, *Config)
	}{
		{"no-owner", func(t *testing.T, c *Config) { c.Owner = nil }},
		{"different-service", func(t *testing.T, c *Config) { c.Owner.Service = "other.service" }},
		{"foreign-state-service", func(t *testing.T, c *Config) {
			mutateMissingPIDState(t, *c, func(s *State) { s.OwnerService = "other.service" })
		}},
		{"missing-state-service", func(t *testing.T, c *Config) { mutateMissingPIDState(t, *c, func(s *State) { s.OwnerService = "" }) }},
		{"unknown-start", func(t *testing.T, c *Config) { mutateMissingPIDState(t, *c, func(s *State) { s.StartTicks = 0 }) }},
		{"live-process", func(t *testing.T, c *Config) {
			ticks, _ := processStart(os.Getpid())
			mutateMissingPIDState(t, *c, func(s *State) { s.PID, s.StartTicks = os.Getpid(), ticks })
		}},
		{"reused-pid", func(t *testing.T, c *Config) {
			ticks, _ := processStart(os.Getpid())
			mutateMissingPIDState(t, *c, func(s *State) { s.PID, s.StartTicks = os.Getpid(), ticks+1 })
		}},
		{"unknown-socket", func(t *testing.T, c *Config) { mutateMissingPIDState(t, *c, func(s *State) { s.SocketInode = 0 }) }},
		{"foreign-policy", func(t *testing.T, c *Config) {
			mutateMissingPIDState(t, *c, func(s *State) { s.AllowedImages = []string{"foreign:latest"} })
		}},
		{"malformed", func(t *testing.T, c *Config) {
			if err := os.WriteFile(StateFile(*c), []byte("{"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"unsafe-state", func(t *testing.T, c *Config) {
			if err := os.Chmod(StateFile(*c), 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{"active-unit", func(t *testing.T, c *Config) { writeMissingPIDManager(t, *c, "MainPID=0\nActiveState=active\n", "") }},
		{"different-unit-pid", func(t *testing.T, c *Config) {
			writeMissingPIDManager(t, *c, fmt.Sprintf("MainPID=%d\nActiveState=inactive\n", os.Getpid()), "")
		}},
		{"outside-activating-unit", func(t *testing.T, c *Config) {
			writeMissingPIDManager(t, *c, "MainPID=0\nActiveState=activating\n", "")
		}},
		{"deactivating-unit", func(t *testing.T, c *Config) {
			writeMissingPIDManager(t, *c, "MainPID=0\nActiveState=deactivating\n", "")
		}},
		{"missing-unit-identity", func(t *testing.T, c *Config) { writeMissingPIDManager(t, *c, "ActiveState=inactive\n", "") }},
		{"different-pid-file", func(t *testing.T, c *Config) {
			if err := os.WriteFile(PIDFile(*c), []byte(fmt.Sprint(os.Getpid())), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"deliberate-stop", func(t *testing.T, c *Config) {
			raw, _ := json.Marshal(stopIntent{Generation: 1})
			if err := os.WriteFile(stopIntentFile(*c), raw, 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"in-progress-stop", func(t *testing.T, c *Config) {
			raw, _ := json.Marshal(stopIntent{Generation: 1, InProgress: true})
			if err := os.WriteFile(stopIntentFile(*c), raw, 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"accepting-replacement", func(t *testing.T, c *Config) {
			if err := os.Remove(c.Socket); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("unix", c.Socket)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { listener.Close() })
		}},
		{"same-recorded-accepting-socket", func(t *testing.T, c *Config) {
			if err := os.Remove(c.Socket); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("unix", c.Socket)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { listener.Close() })
			info, _ := socketInfo(c.Socket)
			st := info.Sys().(*syscall.Stat_t)
			mutateMissingPIDState(t, *c, func(s *State) { s.SocketDevice, s.SocketInode = uint64(st.Dev), st.Ino })
		}},
		{"foreign-socket-path", func(t *testing.T, c *Config) {
			if err := os.Remove(c.Socket); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(StateFile(*c), c.Socket); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := missingPIDFixture(t)
			tc.change(t, &c)
			before, _ := os.ReadFile(StateFile(c))
			info, _ := os.Lstat(c.Socket)
			for _, action := range []func() (Status, error){
				func() (Status, error) { return Start(context.Background(), c, false) },
				func() (Status, error) { return Stop(c, false) },
			} {
				if _, err := action(); err == nil {
					t.Fatal("foreign or incomplete lifetime admitted")
				}
			}
			after, _ := os.ReadFile(StateFile(c))
			now, _ := os.Lstat(c.Socket)
			if string(before) != string(after) || !os.SameFile(info, now) || !processRunning(os.Getpid()) {
				t.Fatal("refusal disturbed retained state, socket or foreign process")
			}
		})
	}
}

func TestMissingPIDRejectsMutationDuringUnitObservation(t *testing.T) {
	tool := func(name string) string {
		t.Helper()
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		return "'" + path + "'"
	}
	cp, chmod, mv, rm := tool("cp"), tool("chmod"), tool("mv"), tool("rm")
	for _, mutation := range []string{"state-digest", "state-replacement", "pid-created", "stop-created", "socket-replaced"} {
		t.Run(mutation, func(t *testing.T) {
			c := missingPIDFixture(t)
			// Paths come only from this owned fixture, never production state.
			commands := map[string]string{
				"state-digest":      "printf ' ' >> '" + StateFile(c) + "'",
				"state-replacement": cp + " '" + StateFile(c) + "' '" + StateFile(c) + ".new'; " + chmod + " 600 '" + StateFile(c) + ".new'; " + mv + " '" + StateFile(c) + ".new' '" + StateFile(c) + "'",
				"pid-created":       "printf '1' > '" + PIDFile(c) + "'; " + chmod + " 600 '" + PIDFile(c) + "'",
				"stop-created":      "printf '{\"generation\":1}' > '" + stopIntentFile(c) + "'; " + chmod + " 600 '" + stopIntentFile(c) + "'",
				"socket-replaced":   rm + " '" + c.Socket + "'; printf 'foreign' > '" + c.Socket + "'",
			}
			writeMissingPIDManager(t, c, "MainPID=0\nActiveState=inactive\n", commands[mutation])
			_, err := requestStationStart(context.Background(), c, false, false)
			if err == nil || !strings.Contains(err.Error(), "changed") {
				t.Fatalf("concurrent %s admitted: %v", mutation, err)
			}
			if _, err := os.Lstat(StateFile(c)); err != nil {
				t.Fatal("mutated state removed", err)
			}
			if _, err := os.Lstat(c.Socket); err != nil {
				t.Fatal("mutated socket removed", err)
			}
		})
	}
}

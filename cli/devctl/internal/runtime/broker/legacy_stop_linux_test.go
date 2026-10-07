//go:build linux

package broker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func ownedLegacyStopFixture(t *testing.T) (Config, int) {
	return ownedLegacyStopFixtureMode(t, "listen")
}
func ownedLegacyStopFixtureMode(t *testing.T, mode string) (Config, int) {
	t.Helper()
	root, err := os.MkdirTemp("", "lb-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := Normalize(Config{StateRoot: root, Socket: filepath.Join(root, "broker.sock"), Binary: executable})
	child := startBrokerTestHelper(t, mode, "BROKER_LISTEN=unix://"+c.Socket)
	deadline := time.Now().Add(2 * time.Second)
	for !socketAcceptsConnections(c.Socket) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !socketAcceptsConnections(c.Socket) {
		t.Fatal("owned listener did not start")
	}
	state := stateFixture(c, child.Process.Pid, 0)
	if err = writeState(c, state); err != nil {
		t.Fatal(err)
	}
	digest := func(path string) string {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	info, err := socketInfo(c.Socket)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	manager := filepath.Join(root, "inactive-manager")
	if err = os.WriteFile(manager, []byte("#!/bin/sh\nprintf 'MainPID=0\\nActiveState=inactive\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	c.Owner = &StationOwner{Systemctl: manager, LegacyWitness: &LegacySocketWitness{
		Binary: executable, StateSHA256: digest(StateFile(c)), PIDSHA256: digest(PIDFile(c)),
		SocketDevice: uint64(st.Dev), SocketInode: st.Ino, SocketCTimeSec: st.Ctim.Sec, SocketCTimeNsec: st.Ctim.Nsec,
	}}
	c.Binary = "/nix/store/selected-new-broker/bin/postgres-broker"
	return c, child.Process.Pid
}

func TestExplicitLegacyStopRetiresOwnedListenerAndPreservesInhibition(t *testing.T) {
	c, pid := ownedLegacyStopFixture(t)
	if _, err := requestStationStart(context.Background(), c, false, false); err == nil {
		t.Fatal("ordinary readiness admitted legacy owner")
	}
	if !processRunning(pid) {
		t.Fatal("ordinary readiness retired legacy owner")
	}
	status, err := requestStationStop(c, false)
	if err != nil {
		t.Fatal(err)
	}
	if status.Running || status.PID != 0 || status.SocketExists || processRunning(pid) {
		t.Fatalf("legacy producer not retired: %+v", status)
	}
	for _, path := range []string{StateFile(c), PIDFile(c), c.Socket} {
		if _, err = os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("retained metadata remains: %s: %v", path, err)
		}
	}
	g, err := lockLifecycle(context.Background(), c, false)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	intent, err := readStopIntent(c, g)
	if err != nil || intent == nil || intent.InProgress {
		t.Fatalf("completed stop intent absent: %+v %v", intent, err)
	}
	g.Close()
	if _, err = requestStationStart(context.Background(), c, false, false); err == nil || !strings.Contains(err.Error(), "inhibit") {
		t.Fatalf("automatic restart ignored deliberate stop: %v", err)
	}
}

func TestExplicitLegacyStopDryRunPreservesOwnedProducer(t *testing.T) {
	c, pid := ownedLegacyStopFixture(t)
	before, err := os.ReadFile(StateFile(c))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = requestStationStop(c, true); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(StateFile(c))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || !processRunning(pid) || !socketAcceptsConnections(c.Socket) {
		t.Fatal("dry run changed retained producer")
	}
	if _, err = os.Stat(stopIntentFile(c)); !os.IsNotExist(err) {
		t.Fatal("dry run wrote stop intent")
	}
}

func TestExplicitLegacyStopPreservesRefusedProducer(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"missing-witness", func(c *Config) { c.Owner.LegacyWitness = nil }},
		{"state-digest", func(c *Config) { c.Owner.LegacyWitness.StateSHA256 = strings.Repeat("0", 64) }},
		{"pid-digest", func(c *Config) { c.Owner.LegacyWitness.PIDSHA256 = strings.Repeat("0", 64) }},
		{"socket-inode", func(c *Config) { c.Owner.LegacyWitness.SocketInode++ }},
		{"socket-ctime", func(c *Config) { c.Owner.LegacyWitness.SocketCTimeNsec++ }},
		{"policy", func(c *Config) { c.Upstream = "unix:///other/docker.sock" }},
		{"executable", func(c *Config) { c.Owner.LegacyWitness.Binary = "/nix/store/other-broker/bin/broker" }},
		{"active-selected-owner", func(c *Config) {
			if err := os.WriteFile(c.Owner.Systemctl, []byte("#!/bin/sh\nprintf 'MainPID=1\\nActiveState=active\\n'\n"), 0700); err != nil {
				panic(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, pid := ownedLegacyStopFixture(t)
			before, err := os.ReadFile(StateFile(c))
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(&c)
			if _, err = requestStationStop(c, false); err == nil {
				t.Fatal("foreign or incomplete subject admitted")
			}
			after, err := os.ReadFile(StateFile(c))
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) || !processRunning(pid) || !socketAcceptsConnections(c.Socket) {
				t.Fatal("refused producer disturbed")
			}
			if _, err = os.Stat(stopIntentFile(c)); !os.IsNotExist(err) {
				t.Fatal("refusal wrote stop intent")
			}
		})
	}
}

func TestLegacySocketPeerRejectsDifferentOwnedProcess(t *testing.T) {
	c, _ := ownedLegacyStopFixture(t)
	if err := legacySocketPeer(c.Socket, os.Getpid()); err == nil {
		t.Fatal("different listener peer admitted")
	}
}

func TestLegacySignalBoundaryRejectsReplacementListener(t *testing.T) {
	c, pid := ownedLegacyStopFixture(t)
	identity, err := socketInfo(c.Socket)
	if err != nil {
		t.Fatal(err)
	}
	if err = legacySignalSocketBinding(c.Socket, identity, c.Owner.LegacyWitness, pid, false); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(c.Socket); err != nil {
		t.Fatal(err)
	}
	replacement := startBrokerTestHelper(t, "listen", "BROKER_LISTEN=unix://"+c.Socket)
	deadline := time.Now().Add(2 * time.Second)
	for !socketAcceptsConnections(c.Socket) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !socketAcceptsConnections(c.Socket) {
		t.Fatal("replacement owned listener did not start")
	}
	if err = legacySignalSocketBinding(c.Socket, identity, c.Owner.LegacyWitness, pid, false); err == nil {
		t.Fatal("replacement listener acquired retained signal custody")
	}
	if !processRunning(pid) || !processRunning(replacement.Process.Pid) {
		t.Fatal("signal proof disturbed an owned producer")
	}
}

func TestInterruptedLegacyStopRejectsChangedOrUnknownLifetime(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			c, pid := ownedLegacyStopFixture(t)
			ticks, err := processStart(pid)
			if err != nil {
				t.Fatal(err)
			}
			ticks++
			if missing {
				ticks = 0
			}
			g, err := lockLifecycle(context.Background(), c, false)
			if err != nil {
				t.Fatal(err)
			}
			intent := stopIntent{Generation: 23, InProgress: true, RecordedPID: pid, StateSHA256: c.Owner.LegacyWitness.StateSHA256, PIDSHA256: c.Owner.LegacyWitness.PIDSHA256, LegacyStartTicks: ticks}
			err = writeStopIntent(c, g, intent)
			g.Close()
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(stopIntentFile(c))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = requestStationStop(c, false); err == nil {
				t.Fatal("interrupted stop adopted another lifetime")
			}
			after, err := os.ReadFile(stopIntentFile(c))
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) || !processRunning(pid) || !socketAcceptsConnections(c.Socket) {
				t.Fatal("refused retry disturbed retained producer or intent")
			}
		})
	}
}

func TestInterruptedLegacyStopCompletesOriginalClosedListenerLifetime(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(fmt.Sprint(replacement), func(t *testing.T) {
			c, pid := ownedLegacyStopFixtureMode(t, "listen-close")
			ticks, err := processStart(pid)
			if err != nil {
				t.Fatal(err)
			}
			g, err := lockLifecycle(context.Background(), c, false)
			if err != nil {
				t.Fatal(err)
			}
			intent := stopIntent{Generation: 29, InProgress: true, RecordedPID: pid, StateSHA256: c.Owner.LegacyWitness.StateSHA256, PIDSHA256: c.Owner.LegacyWitness.PIDSHA256, LegacyStartTicks: ticks}
			err = writeStopIntent(c, g, intent)
			g.Close()
			if err != nil {
				t.Fatal(err)
			}
			if err = syscall.Kill(pid, syscall.SIGUSR1); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(2 * time.Second)
			for socketAcceptsConnections(c.Socket) && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if socketAcceptsConnections(c.Socket) || !processRunning(pid) {
				t.Fatal("owned closed-listener lifetime fixture failed")
			}
			if replacement {
				other := startBrokerTestHelper(t, "listen", "BROKER_LISTEN=unix://"+c.Socket)
				deadline = time.Now().Add(2 * time.Second)
				for !socketAcceptsConnections(c.Socket) && time.Now().Before(deadline) {
					time.Sleep(10 * time.Millisecond)
				}
				before, _ := os.ReadFile(stopIntentFile(c))
				if _, err = requestStationStop(c, false); err == nil {
					t.Fatal("replacement listener admitted on interrupted retry")
				}
				after, _ := os.ReadFile(stopIntentFile(c))
				if string(before) != string(after) || !processRunning(pid) || !processRunning(other.Process.Pid) {
					t.Fatal("refused retry disturbed producer, replacement or intent")
				}
				return
			}
			if _, err = requestStationStop(c, false); err != nil {
				t.Fatal(err)
			}
			if processRunning(pid) {
				t.Fatal("original interrupted lifetime remained live")
			}
			g, err = lockLifecycle(context.Background(), c, false)
			if err != nil {
				t.Fatal(err)
			}
			defer g.Close()
			done, err := readStopIntent(c, g)
			if err != nil || done == nil || done.InProgress || done.Generation != intent.Generation || done.LegacyStartTicks != ticks {
				t.Fatalf("retry lost admitted lifetime: %+v %v", done, err)
			}
		})
	}
}

func TestLegacyStopEscalatesRetainedGracefulClosedListener(t *testing.T) {
	c, pid := ownedLegacyStopFixtureMode(t, "listen-term-drain")
	if _, err := requestStationStop(c, false); err != nil {
		t.Fatal(err)
	}
	if processRunning(pid) {
		t.Fatal("retained graceful shutdown did not retire")
	}
}

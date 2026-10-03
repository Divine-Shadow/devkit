package broker

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestStationStopRetainsUnlinkedCustody(t *testing.T) {
	root, err := os.MkdirTemp("", "stop-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	c := Normalize(Config{StateRoot: root, Socket: filepath.Join(root, "broker.sock")})
	g, err := lockLifecycle(context.Background(), c, true)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if err = g.write(PIDFile(c), []byte("17\n")); err != nil {
		t.Fatal(err)
	}
	file, err := g.retainFile(PIDFile(c))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	listener, err := net.Listen("unix", c.Socket)
	if err != nil {
		t.Fatal(err)
	}
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	defer listener.Close()
	sock, identity, err := g.retainSocket(c.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer sock.Close()
	if err = g.unlock(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(PIDFile(c)); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(PIDFile(c), []byte("17\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(c.Socket); err != nil {
		t.Fatal(err)
	}
	replacement, err := net.Listen("unix", c.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	next, err := lockLifecycle(context.Background(), c, false)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if sameRetainedFile(g.files[PIDFile(c)], next.files[PIDFile(c)]) {
		t.Fatal("replacement metadata acquired old custody")
	}
	if err = removeRetainedSocket(c.Socket, identity); err == nil {
		t.Fatal("replacement socket was removed")
	}
	if _, err = os.Lstat(c.Socket); err != nil {
		t.Fatal("replacement socket lost", err)
	}
	data := make([]byte, 3)
	if _, err = file.Read(data); err != nil || string(data) != "17\n" {
		t.Fatal("original metadata handle lost", err)
	}
}
func TestStationStopRejectsReusedOrUnknownPID(t *testing.T) {
	ticks, err := processStart(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	for _, start := range []uint64{0, ticks + 1} {
		retired, err := recordedProcessRetired(Status{PID: os.Getpid(), State: &State{PID: os.Getpid(), StartTicks: start}})
		if err == nil || retired {
			t.Fatal("foreign or unknown identity treated as retired")
		}
	}
	retired, err := recordedProcessRetired(Status{PID: os.Getpid(), State: &State{PID: os.Getpid(), StartTicks: ticks}})
	if err != nil || retired {
		t.Fatal("live exact process treated as retired", err)
	}
}

package fleetmode

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeHeldServiceProcess struct {
	identity                   serviceProcessIdentity
	events                     *[]string
	terminated, waited, closed int
	identityErr, terminateErr  error
	wait                       func(context.Context) error
}

func (p *fakeHeldServiceProcess) Identity(context.Context) (serviceProcessIdentity, error) {
	*p.events = append(*p.events, "identity")
	return p.identity, p.identityErr
}
func (p *fakeHeldServiceProcess) Terminate(context.Context) error {
	*p.events = append(*p.events, "terminate-held")
	p.terminated++
	return p.terminateErr
}
func (p *fakeHeldServiceProcess) Wait(ctx context.Context) error {
	*p.events = append(*p.events, "wait-held")
	p.waited++
	if p.wait != nil {
		return p.wait(ctx)
	}
	return nil
}
func (p *fakeHeldServiceProcess) Close() error {
	*p.events = append(*p.events, "close-held")
	p.closed++
	return nil
}

type fakeWSLServiceOps struct {
	snapshots []wslServiceSnapshot
	read      func(int) error
	reads     int
	opened    int
	process   *fakeHeldServiceProcess
	off       bool
	offErr    error
	events    []string
}

func (o *fakeWSLServiceOps) ReadWSLService(context.Context) (wslServiceSnapshot, error) {
	o.events = append(o.events, "scm")
	index := o.reads
	o.reads++
	if o.read != nil {
		if err := o.read(index); err != nil {
			return wslServiceSnapshot{}, err
		}
	}
	if index >= len(o.snapshots) {
		index = len(o.snapshots) - 1
	}
	return o.snapshots[index], nil
}
func (o *fakeWSLServiceOps) OpenWSLServiceProcess(_ context.Context, pid uint32) (heldServiceProcess, error) {
	o.events = append(o.events, "open-running")
	o.opened++
	return o.process, nil
}
func (o *fakeWSLServiceOps) ObserveOff(context.Context) (bool, error) {
	o.events = append(o.events, "observe-real-off")
	return o.off, o.offErr
}

func serviceRemedyFixture() (*fakeWSLServiceOps, *heldWSLService, wslServiceSnapshot) {
	s := wslServiceSnapshot{StartType: serviceDisabled, ConfigType: serviceOwnProcess,
		StatusType: serviceOwnProcess, State: serviceStopPending, PID: 42,
		ImageCommand: `"C:\Program Files\WSL\wslservice.exe"`}
	o := &fakeWSLServiceOps{snapshots: []wslServiceSnapshot{s}, off: true}
	p := &fakeHeldServiceProcess{identity: serviceProcessIdentity{PID: 42, Created: 1234, Image: `C:\Program Files\WSL\wslservice.exe`}, events: &o.events}
	o.process = p
	image, _ := configuredWSLServiceImage(s.ImageCommand)
	return o, &heldWSLService{process: p, identity: p.identity, image: image}, s
}

func TestPendingWSLServicePinsRunningProcessAndRequiresRealReadbacks(t *testing.T) {
	o, _, pending := serviceRemedyFixture()
	running := pending
	running.State = serviceRunning
	o.snapshots = []wslServiceSnapshot{running, running}
	held, err := captureWSLServiceProcess(context.Background(), o)
	if err != nil || held == nil || o.process.closed != 0 {
		t.Fatalf("capture = %v, %v", held, err)
	}
	stopped := pending
	stopped.State, stopped.PID = serviceStopped, 0
	o.snapshots, o.reads = []wslServiceSnapshot{pending, pending, pending, stopped}, 0
	o.process.wait = func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Error("held-process wait lacks its bounded remaining deadline")
		}
		return nil
	}
	if err := finishPendingWSLService(context.Background(), o, held); err != nil {
		t.Fatal(err)
	}
	_ = held.process.Close()
	want := []string{"scm", "open-running", "identity", "scm", "scm", "observe-real-off", "identity", "scm", "terminate-held", "wait-held", "scm", "scm", "close-held"}
	if !reflect.DeepEqual(o.events, want) || o.opened != 1 || o.process.terminated != 1 || o.process.closed != 1 {
		t.Fatalf("process custody/order: %v", o.events)
	}
}

func TestCaptureWSLServiceRefusesUnknownRunningIdentity(t *testing.T) {
	for _, kind := range []string{"already-pending", "shared-config", "shared-status", "enabled", "zero-pid", "ambiguous-image", "other-executable", "wrong-process-pid", "wrong-process-image", "unknown-creation", "exited-process", "scm-changed"} {
		t.Run(kind, func(t *testing.T) {
			o, _, s := serviceRemedyFixture()
			s.State = serviceRunning
			switch kind {
			case "already-pending":
				s.State = serviceStopPending
			case "shared-config":
				s.ConfigType = 0x20
			case "shared-status":
				s.StatusType = 0x20
			case "enabled":
				s.StartType = 2
			case "zero-pid":
				s.PID = 0
			case "ambiguous-image":
				s.ImageCommand = `C:\Program Files\WSL\wslservice.exe`
			case "other-executable":
				s.ImageCommand = `C:\Windows\System32\svchost.exe`
			case "wrong-process-pid":
				o.process.identity.PID++
			case "wrong-process-image":
				o.process.identity.Image = `C:\Other\wslservice.exe`
			case "unknown-creation":
				o.process.identity.Created = 0
			case "exited-process":
				o.process.identity.Exited = true
			}
			o.snapshots = []wslServiceSnapshot{s, s}
			if kind == "scm-changed" {
				o.snapshots[1].State = serviceStopPending
			}
			held, err := captureWSLServiceProcess(context.Background(), o)
			if err == nil || held != nil || o.process.terminated != 0 || (o.opened != 0 && o.process.closed != 1) {
				t.Fatalf("unknown identity accepted/leaked: held=%v err=%v events=%v", held, err, o.events)
			}
		})
	}
}

func TestPendingWSLServiceRefusesChangedOrUnobservedTargets(t *testing.T) {
	for _, kind := range []string{"no-retained-reference", "running", "shared", "enabled", "zero-pid", "other-pid", "configured-image-changed", "still-wsl", "off-unavailable", "creation-changed", "process-image-changed", "scm-recheck-changed", "scm-unavailable", "canceled-before-kill"} {
		t.Run(kind, func(t *testing.T) {
			o, held, s := serviceRemedyFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "no-retained-reference":
				held = nil
			case "running":
				s.State = serviceRunning
			case "shared":
				s.ConfigType = 0x20
			case "enabled":
				s.StartType = 2
			case "zero-pid":
				s.PID = 0
			case "other-pid":
				s.PID++
			case "configured-image-changed":
				s.ImageCommand = `C:\Other\wslservice.exe`
			case "still-wsl":
				o.off = false
			case "off-unavailable":
				o.offErr = errors.New("HCS unavailable")
			case "creation-changed":
				o.process.identity.Created++
			case "process-image-changed":
				o.process.identity.Image = `C:\Other\wslservice.exe`
			case "scm-unavailable":
				o.read = func(int) error { return errors.New("SCM unavailable") }
			case "canceled-before-kill":
				o.read = func(index int) error {
					if index == 1 {
						cancel()
					}
					return nil
				}
			}
			o.snapshots = []wslServiceSnapshot{s, s}
			if kind == "scm-recheck-changed" {
				o.snapshots[1].PID++
			}
			if err := finishPendingWSLService(ctx, o, held); err == nil || o.process.terminated != 0 || o.opened != 0 {
				t.Fatalf("refusal failed: err=%v events=%v", err, o.events)
			}
		})
	}
}

func TestPendingWSLServiceAlreadyStoppedIsReadOnly(t *testing.T) {
	o, _, s := serviceRemedyFixture()
	s.State, s.PID = serviceStopped, 0
	o.snapshots = []wslServiceSnapshot{s}
	if err := finishPendingWSLService(context.Background(), o, nil); err != nil || o.opened != 0 || o.process.terminated != 0 || len(o.events) != 1 {
		t.Fatalf("already stopped changed state: %v %v", err, o.events)
	}
}

func TestRunningWSLServiceNormalStopReleasesCaptureWithoutTermination(t *testing.T) {
	o, _, s := serviceRemedyFixture()
	s.State = serviceRunning
	o.snapshots = []wslServiceSnapshot{s, s}
	held, err := captureWSLServiceProcess(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	s.State, s.PID = serviceStopped, 0
	o.snapshots, o.reads = []wslServiceSnapshot{s}, 0
	if err := finishPendingWSLService(context.Background(), o, held); err != nil {
		t.Fatal(err)
	}
	_ = held.process.Close()
	if o.process.terminated != 0 || o.process.waited != 0 || o.process.closed != 1 || o.opened != 1 {
		t.Fatalf("normal stop changed process custody: %v", o.events)
	}
}

func TestExitedRetainedWSLProcessNeverSelectsAReplacementByPID(t *testing.T) {
	o, held, s := serviceRemedyFixture()
	original := o.process
	original.identity.Exited = true
	// Even if a provider offered another object for the same number, finish
	// must never call Open again or act on that replacement.
	o.process = &fakeHeldServiceProcess{identity: serviceProcessIdentity{PID: 42, Created: 9999, Image: `C:\Other\wslservice.exe`}, events: &o.events}
	after := s
	after.State, after.PID = serviceStopped, 0
	o.snapshots = []wslServiceSnapshot{s, s, after}
	if err := finishPendingWSLService(context.Background(), o, held); err != nil {
		t.Fatal(err)
	}
	_ = held.process.Close()
	if original.terminated != 0 || original.waited != 1 || original.closed != 1 || o.opened != 0 || o.process.terminated != 0 {
		t.Fatalf("selected replacement or fabricated exit: %v", o.events)
	}
}

func TestPendingWSLServiceDoesNotConvertTerminationIntoStoppedEvidence(t *testing.T) {
	for _, kind := range []string{"terminate-refused", "wait-timeout", "pending-timeout", "stopped-but-enabled"} {
		t.Run(kind, func(t *testing.T) {
			o, held, s := serviceRemedyFixture()
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			switch kind {
			case "terminate-refused":
				o.process.terminateErr = errors.New("access denied")
			case "wait-timeout":
				o.process.wait = func(c context.Context) error { <-c.Done(); return c.Err() }
			case "stopped-but-enabled":
				after := s
				after.State, after.StartType = serviceStopped, 2
				o.snapshots = []wslServiceSnapshot{s, s, after}
			}
			if err := finishPendingWSLService(ctx, o, held); err == nil {
				t.Fatal("unverified stop was treated as success")
			}
		})
	}
}

func TestConfiguredWSLServiceImageRefusesAmbiguousAndNonlocalCommands(t *testing.T) {
	for _, image := range []string{`"C:\Program Files\WSL\wslservice.exe" --extra`, `C:\Program Files\WSL\wslservice.exe`, `\\server\WSL\wslservice.exe`, `C:\WSL\..\wslservice.exe`, `C:\WSL\other.exe`, `C:relative\wslservice.exe`, `"unterminated`, `""`} {
		if _, err := configuredWSLServiceImage(image); err == nil {
			t.Errorf("accepted %q", image)
		}
	}
	for _, image := range []string{`"C:\Program Files\WSL\wslservice.exe"`, `C:\WSL\wslservice.exe`, `"\\?\C:\Program Files\WSL\WSLSERVICE.EXE"`} {
		if _, err := configuredWSLServiceImage(image); err != nil {
			t.Errorf("refused %q: %v", image, err)
		}
	}
}

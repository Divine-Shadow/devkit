package nativecmd

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	wtx "devkit/cli/devctl/internal/worktrees"
)

func testSelectedControllerLease(t *testing.T) (nativeplan.Plan, wtx.NativeOptions, string, string, *os.File) {
	t.Helper()
	p, opts := freshSelectedFixture(t)
	p.Agent.ID.Repo = "ouroboros-ide"
	p.Agent.HostWorktree = filepath.Join(filepath.Dir(p.Agent.HostWorktree), "ouroboros-ide")
	opts.Repo = "ouroboros-ide"
	p.GUITargetConfig = &nativeplan.GUITargetConfigProjection{TargetID: "fixture-local-5", Kind: "local-wsl-devkit-agent"}
	root := filepath.Join(t.TempDir(), "gui-start-locks")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	name, err := selectedControllerLifecycleLockName(p)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return p, opts, root, path, f
}

func dupSelectedLease(t *testing.T, f *os.File) int {
	t.Helper()
	fd, err := syscall.Dup(int(f.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	return fd
}

func TestSelectedStagingLifecycleLeaseRequiresInheritedHeldDescription(t *testing.T) {
	p, _, root, path, parent := testSelectedControllerLease(t)
	if err := syscall.Flock(int(parent.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	lease, err := openSelectedControllerLifecycleLeaseAt(p, dupSelectedLease(t, parent), root)
	if err != nil {
		t.Fatal(err)
	}
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, lease.file.Fd(), syscall.F_GETFD, 0)
	if errno != 0 || flags&syscall.FD_CLOEXEC == 0 {
		t.Fatal("lifecycle descriptor may leak to helpers")
	}
	lease.close()
	probe, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	if err := syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
		t.Fatalf("child close released parent lifecycle lease: %v", err)
	}
}

func TestSelectedStagingLifecycleLeaseRejectsUnlockedOrIndependentlyOpenedFD(t *testing.T) {
	for _, held := range []bool{false, true} {
		t.Run(map[bool]string{false: "unlocked", true: "different-description"}[held], func(t *testing.T) {
			p, _, root, path, parent := testSelectedControllerLease(t)
			fd := dupSelectedLease(t, parent)
			if held {
				syscall.Close(fd)
				if err := syscall.Flock(int(parent.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
					t.Fatal(err)
				}
				var err error
				fd, err = syscall.Open(path, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
				if err != nil {
					t.Fatal(err)
				}
			}
			if lease, err := openSelectedControllerLifecycleLeaseAt(p, fd, root); err == nil {
				lease.close()
				t.Fatal("unproven lifecycle custody admitted")
			}
		})
	}
}

func TestSelectedStagingLifecycleLeaseRefusesNamedLockReplacement(t *testing.T) {
	p, _, root, path, parent := testSelectedControllerLease(t)
	if err := syscall.Flock(int(parent.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	lease, err := openSelectedControllerLifecycleLeaseAt(p, dupSelectedLease(t, parent), root)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.close()
	if err := os.Rename(path, path+".preserved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := lease.revalidate(); err == nil {
		t.Fatal("replacement lifecycle identity admitted")
	}
}

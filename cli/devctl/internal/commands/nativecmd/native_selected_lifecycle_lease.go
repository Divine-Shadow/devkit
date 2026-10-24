package nativecmd

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

const selectedControllerLifecycleLockRoot = "/run/fleet-controller-operation/gui-start-locks"

// selectedControllerLifecycleLease owns only the child's inherited descriptor.
// Closing it must NEVER issue LOCK_UN: Fleet retains the same open-file
// description and keeps its lifecycle lease through completion and launch.
type selectedControllerLifecycleLease struct {
	file      *os.File
	directory *os.File
	root      string
	name      string
}

func selectedControllerLifecycleLockName(p nativeplan.Plan) (string, error) {
	projection := p.GUITargetConfig
	if p.Agent.ID.Project != "dev-all" || p.Agent.ID.Index < 1 || projection == nil ||
		projection.Kind != "local-wsl-devkit-agent" ||
		!strings.HasSuffix(projection.TargetID, fmt.Sprintf("-local-%d", p.Agent.ID.Index)) ||
		!filepath.IsAbs(p.Agent.HostHome) || filepath.Clean(p.Agent.HostHome) != p.Agent.HostHome {
		return "", errors.New("selected staging requires an exact source-selected controller GUI target")
	}
	socket := filepath.Join(p.Agent.HostHome, ".codex", fmt.Sprintf("a%d-app.sock", p.Agent.ID.Index))
	digest := sha256.Sum256([]byte(socket))
	return fmt.Sprintf("%x.lock", digest), nil
}

func openSelectedControllerLifecycleLease(p nativeplan.Plan, fd int) (*selectedControllerLifecycleLease, error) {
	return openSelectedControllerLifecycleLeaseAt(p, fd, selectedControllerLifecycleLockRoot)
}

// The root argument is private test injection, never caller configuration.
// This takes ownership of fd, including on failure.
func openSelectedControllerLifecycleLeaseAt(p nativeplan.Plan, fd int, root string) (*selectedControllerLifecycleLease, error) {
	if fd < 3 {
		return nil, errors.New("selected staging requires an inherited lifecycle descriptor")
	}
	syscall.CloseOnExec(fd)
	lease := &selectedControllerLifecycleLease{file: os.NewFile(uintptr(fd), "selected-controller-lifecycle"), root: root}
	fail := func(err error) (*selectedControllerLifecycleLease, error) { lease.close(); return nil, err }
	name, err := selectedControllerLifecycleLockName(p)
	if err != nil {
		return fail(err)
	}
	lease.name = name
	lease.directory, err = openFreshPreparationDirectory(root, false)
	if err != nil {
		return fail(fmt.Errorf("open source-owned controller lifecycle directory: %w", err))
	}
	info, err := lease.directory.Stat()
	if err != nil {
		return fail(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || stat.Mode&07777 != 0700 || stat.Uid != uint32(os.Geteuid()) {
		return fail(errors.New("selected controller lifecycle directory is not private and effective-user owned"))
	}
	probe, err := lease.openBoundPath()
	if err != nil {
		return fail(err)
	}
	defer probe.Close()
	// An unlocked supplied fd could acquire the lock and falsely claim inherited
	// custody. An independent open must first prove the lock is already held.
	if err := syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		_ = syscall.Flock(int(probe.Fd()), syscall.LOCK_UN)
		return fail(errors.New("selected controller lifecycle descriptor was not already held"))
	} else if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
		return fail(fmt.Errorf("prove existing controller lifecycle lock: %w", err))
	}
	// Only the inherited same open-file description can succeed while that
	// independently opened description is excluded by the existing holder.
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fail(fmt.Errorf("selected controller lifecycle descriptor does not share the held lease: %w", err))
	}
	return lease, nil
}

func (lease *selectedControllerLifecycleLease) openBoundPath() (*os.File, error) {
	if lease == nil || lease.file == nil || lease.directory == nil {
		return nil, errors.New("selected controller lifecycle lease is missing")
	}
	heldDirectory, err := lease.directory.Stat()
	if err != nil {
		return nil, err
	}
	currentDirectory, err := openFreshPreparationDirectory(lease.root, false)
	if err != nil {
		return nil, err
	}
	currentInfo, err := currentDirectory.Stat()
	currentDirectory.Close()
	if err != nil || !os.SameFile(heldDirectory, currentInfo) {
		return nil, errors.New("selected controller lifecycle directory identity changed")
	}
	fd, err := syscall.Openat(int(lease.directory.Fd()), lease.name, syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open exact source-owned controller lifecycle lock: %w", err)
	}
	probe := os.NewFile(uintptr(fd), lease.name)
	info, err := probe.Stat()
	if err != nil {
		probe.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	held, heldErr := lease.file.Stat()
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 ||
		stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || heldErr != nil || !os.SameFile(info, held) {
		probe.Close()
		return nil, errors.New("selected controller lifecycle descriptor differs from its private source-owned lock")
	}
	return probe, nil
}

func (lease *selectedControllerLifecycleLease) revalidate() error {
	probe, err := lease.openBoundPath()
	if err != nil {
		return err
	}
	return probe.Close()
}

func (lease *selectedControllerLifecycleLease) close() {
	if lease == nil {
		return
	}
	if lease.file != nil {
		_ = lease.file.Close()
		lease.file = nil
	}
	if lease.directory != nil {
		_ = lease.directory.Close()
		lease.directory = nil
	}
}

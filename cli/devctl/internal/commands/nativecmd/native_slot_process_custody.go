package nativecmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Consumer path spellings are lookup hints, not custody. A historical alias
// must resolve to the selected host objects through that process's root.
func nativeProcHomeMatchesSlot(procPath, home string, identity nativeSlotProcessIdentity) (bool, error) {
	if home != strings.TrimSpace(home) || home != filepath.Clean(home) || !filepath.IsAbs(home) || home == "/" {
		return false, nil
	}
	hostHome, err := os.Stat(identity.hostHome)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect selected native home: %w", err)
	}
	observedHome, homeErr := os.Stat(procPath + "/root" + home)
	if homeErr != nil && !errors.Is(homeErr, os.ErrNotExist) {
		return false, fmt.Errorf("inspect native process home: %w", homeErr)
	}
	homeMatches := homeErr == nil && os.SameFile(hostHome, observedHome)
	relativeWorktree, err := filepath.Rel(identity.hostHome, identity.hostWorktree)
	if err != nil {
		return false, fmt.Errorf("derive selected native home/worktree geometry: %w", err)
	}
	worktree := filepath.Clean(filepath.Join(home, relativeWorktree))
	hostWorktree, err := os.Stat(identity.hostWorktree)
	if err != nil {
		if !homeMatches && errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("inspect selected native worktree: %w", err)
	}
	observedWorktree, worktreeErr := os.Stat(procPath + "/root" + worktree)
	if worktreeErr != nil && !errors.Is(worktreeErr, os.ErrNotExist) {
		return false, fmt.Errorf("inspect native process worktree: %w", worktreeErr)
	}
	worktreeMatches := worktreeErr == nil && os.SameFile(hostWorktree, observedWorktree)
	if homeMatches != worktreeMatches {
		return false, fmt.Errorf("native process home/worktree geometry has conflicting physical custody")
	}
	if !homeMatches {
		return false, nil
	}
	if !hostHome.IsDir() || !observedHome.IsDir() || !hostWorktree.IsDir() || !observedWorktree.IsDir() {
		return false, fmt.Errorf("native process home/worktree custody requires directories")
	}
	// The state bind may be absent in older consumers. An observable declared
	// state mapping must never contradict the selected physical slot.
	if identity.stateRoot != "" {
		hostState, stateErr := os.Stat(identity.stateRoot)
		if stateErr != nil && !errors.Is(stateErr, os.ErrNotExist) {
			return false, stateErr
		}
		if stateErr == nil {
			for _, state := range []string{identity.sandboxState, identity.stateRoot} {
				if !filepath.IsAbs(state) {
					continue
				}
				observed, err := os.Stat(procPath + "/root" + filepath.Clean(state))
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil {
					return false, fmt.Errorf("inspect native process state: %w", err)
				}
				if !os.SameFile(hostState, observed) {
					return false, fmt.Errorf("native process home matches selected slot but state has conflicting physical custody")
				}
			}
		}
	}
	return true, nil
}

func nativeProcHasSlotIdentity(procPath string, env map[string]string, identity nativeSlotProcessIdentity) (bool, error) {
	if env["DEVKIT_NATIVE_AGENT"] != strconv.Itoa(identity.index) {
		return false, nil
	}
	return nativeProcHomeMatchesSlot(procPath, env["HOME"], identity)
}

func nativeProcDescendantConflicts(procPath string, env map[string]string, inheritedHome string, identity nativeSlotProcessIdentity) (bool, error) {
	if agent := env["DEVKIT_NATIVE_AGENT"]; agent != "" && agent != strconv.Itoa(identity.index) {
		return true, nil
	}
	home := env["HOME"]
	if home == "" {
		home = inheritedHome
	}
	// Sanitizing environment does not erase physical contradictions. Resolve the
	// ancestor's proven lookup geometry in this descendant's own mount view.
	matches, err := nativeProcHomeMatchesSlot(procPath, home, identity)
	return !matches, err
}

func nativeSlotPhysicalTargets(identity nativeSlotProcessIdentity) ([]os.FileInfo, error) {
	var targets []os.FileInfo
	for _, path := range []string{identity.hostWorktree, identity.hostHome, identity.stateRoot} {
		if !filepath.IsAbs(path) {
			continue
		}
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect selected native process boundary: %w", err)
		}
		targets = append(targets, info)
	}
	return targets, nil
}

var nativeSlotOpenProcessDirectory = func(path string) (*os.File, error) {
	// A proc handle can change after Stat. Require a directory at the open
	// boundary so replacement by a FIFO can never block this observation.
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func nativePhysicalDirectoryTouches(path string, targets []os.FileInfo) (bool, error) {
	// Pin the observed directory. The target may close/reuse an fd or change
	// cwd while we inspect it; repeated /proc/PID/fd/N/.. lookups are not one
	// physical observation and can turn unrelated descriptor churn into a gate.
	current, err := nativeSlotOpenProcessDirectory(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = current.Close() }()
	for depth := 0; depth < 256; depth++ {
		info, err := current.Stat()
		if err != nil {
			return false, err
		}
		if !info.IsDir() {
			return false, nil
		}
		for _, target := range targets {
			if os.SameFile(info, target) {
				return true, nil
			}
		}
		fd, err := syscall.Openat(int(current.Fd()), "..", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
		if err != nil {
			return false, err
		}
		parent := os.NewFile(uintptr(fd), "native-process-parent")
		parentInfo, err := parent.Stat()
		if err != nil {
			_ = parent.Close()
			return false, err
		}
		if os.SameFile(info, parentInfo) {
			_ = parent.Close()
			return false, nil
		}
		_ = current.Close()
		current = parent
	}
	return false, fmt.Errorf("native process directory ancestry exceeded its bound")
}

func nativeProcLinkTouches(procPath, link string, identity nativeSlotProcessIdentity, targets []os.FileInfo) (bool, error) {
	handle := filepath.Join(procPath, link)
	visible, err := os.Readlink(handle)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, nil
	} // Unrelated inaccessible processes grant no ownership.
	if !filepath.IsAbs(visible) {
		return false, nil
	} // sockets and anonymous descriptors
	visible = filepath.Clean(strings.TrimSuffix(visible, " (deleted)"))
	lexical := nativePathTouchesTargets(visible, []string{identity.hostWorktree, identity.hostHome, identity.stateRoot, identity.sandboxWorktree, identity.sandboxHome, identity.sandboxState})
	info, err := os.Stat(handle)
	if err != nil {
		if lexical {
			return false, fmt.Errorf("cannot establish physical custody of selected native process link %s: %w", link, err)
		}
		return false, nil
	}
	if info.IsDir() {
		touches, err := nativePhysicalDirectoryTouches(handle, targets)
		if err != nil && !lexical && (errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.ENOTDIR)) {
			return false, nil
		}
		return touches, err
	}
	// Resolve a file's parent in its own namespace, and verify that the visible
	// file still denotes the observed descriptor before trusting that ancestry.
	visiblePath := procPath + "/root" + visible
	resolved, resolveErr := os.Stat(visiblePath)
	if resolveErr == nil && !os.SameFile(info, resolved) {
		if lexical {
			return false, fmt.Errorf("selected native process link %s has ambiguous physical custody", link)
		}
		return false, nil
	}
	parentTouches, parentErr := nativePhysicalDirectoryTouches(filepath.Dir(visiblePath), targets)
	if parentErr != nil {
		if lexical {
			return false, fmt.Errorf("cannot establish selected native process link %s ancestry: %w", link, parentErr)
		}
		return false, nil
	}
	if parentTouches && resolveErr != nil && !errors.Is(resolveErr, os.ErrNotExist) {
		return false, resolveErr
	}
	return parentTouches, nil
}

func nativeProcTouchesTargets(procPath string, identity nativeSlotProcessIdentity, targets []os.FileInfo) (bool, error) {
	for _, name := range []string{"cwd", "root", "exe"} {
		touches, err := nativeProcLinkTouches(procPath, name, identity, targets)
		if touches || err != nil {
			return touches, err
		}
	}
	entries, err := os.ReadDir(filepath.Join(procPath, "fd"))
	if err != nil {
		return false, nil
	}
	for _, entry := range entries {
		touches, err := nativeProcLinkTouches(procPath, filepath.Join("fd", entry.Name()), identity, targets)
		if touches || err != nil {
			return touches, err
		}
	}
	return false, nil
}

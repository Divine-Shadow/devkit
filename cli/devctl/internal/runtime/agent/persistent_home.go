package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// RequireLegacyHomesAbsent prevents new storage selection from hiding existing
// application data, and prevents reset from deleting a displaced legacy home.
// It never moves, imports, repairs or removes that data.

func RequireLegacyHomesAbsent(worktree, executionStateRoot string, index int) error {
	for indexInList, legacy := range []string{PersistentDevAllHome(worktree, index), filepath.Join(executionStateRoot, "home")} {
		_, err := os.Lstat(legacy)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect legacy native home %s: %w", legacy, err)
		}
		if indexInList == 0 {
			empty, err := EmptyNativeHomeMountpoint(legacy)
			if err != nil {
				return fmt.Errorf("inspect legacy native home %s: %w", legacy, err)
			}
			if empty {
				continue
			}
		}
		return fmt.Errorf("legacy native home %s requires environment-owned migration to persistent storage before entry or reset; data was left unchanged", legacy)
	}
	return nil
}

// EmptyNativeHomeMountpoint recognizes only data-free bind scaffolding at a
// caller's already source-derived worktree home. Callers must never use this
// allowance for execution-state homes or arbitrary disposal paths. No mutation
// is performed, and the owning lifecycle still enforces its mount/process/lock
// and repeated destructive-boundary checks.
func EmptyNativeHomeMountpoint(path string) (bool, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false, nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	safe := func(i os.FileInfo) bool {
		stat, ok := i.Sys().(*syscall.Stat_t)
		return nativeHomeMountpointModeAllowed(i.Mode()) && ok && stat.Uid == uint32(os.Geteuid())
	}
	if !safe(info) {
		return false, nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, err
	}
	if resolved != path {
		return false, nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer directory.Close()
	opened, err := directory.Stat()
	if err != nil {
		return false, err
	}
	if !safe(opened) || !os.SameFile(info, opened) {
		return false, fmt.Errorf("native home mountpoint changed while opening")
	}
	names, readErr := directory.Readdirnames(1)
	if len(names) > 0 {
		return false, nil
	}
	if readErr != io.EOF {
		return false, fmt.Errorf("read native home mountpoint: %w", readErr)
	}
	after, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if !safe(after) || !os.SameFile(opened, after) || info.Mode() != after.Mode() || !info.ModTime().Equal(after.ModTime()) {
		return false, fmt.Errorf("native home mountpoint changed while inspecting")
	}
	return true, nil
}

func nativeHomeMountpointModeAllowed(mode os.FileMode) bool {
	return mode == (os.ModeDir|0700) || mode == (os.ModeDir|0755)
}

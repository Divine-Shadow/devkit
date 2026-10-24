package nativecmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	wtx "devkit/cli/devctl/internal/worktrees"
)

type selectedStagingDirectory struct {
	path   string
	parent *os.File
	held   *os.File
	mode   os.FileMode
}

type selectedStagingRetirement struct {
	entries []*selectedStagingDirectory
}

func (retirement *selectedStagingRetirement) close() {
	if retirement == nil {
		return
	}
	for _, entry := range retirement.entries {
		entry.close()
	}
	retirement.entries = nil
}

var inspectSelectedStagingProtection = func(p nativeplan.Plan, roots []string, heldTargets []os.FileInfo) error {
	identity := nativeSlotProcessIdentity{index: p.Agent.ID.Index,
		hostWorktree: p.Agent.HostWorktree, sandboxWorktree: p.Agent.SandboxWorktree,
		hostHome: p.Agent.HostHome, sandboxHome: p.Agent.SandboxHome,
		stateRoot: p.Agent.StateRoot, sandboxState: p.Agent.SandboxStateRoot,
		stagingTargets: heldTargets}
	processes, err := planNativeSlotProcesses(identity, false)
	if err != nil {
		return fmt.Errorf("selected staging process custody: %w", err)
	}
	if len(processes.pids) != 0 {
		return fmt.Errorf("selected staging still has processes: %v", processes.pids)
	}
	data, err := readNativeProcProofFile("/proc/self/mountinfo", 1<<20)
	if err != nil {
		return fmt.Errorf("selected staging mount evidence: %w", err)
	}
	return rejectSelectedStagingMounts([]byte(data), roots)
}

func rejectSelectedStagingMounts(data []byte, roots []string) error {
	if len(data) == 0 || len(data) > 1<<20 || data[len(data)-1] != '\n' {
		return errors.New("selected staging mount evidence is empty, partial or excessive")
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		fields := strings.Fields(line)
		separator := -1
		for i, field := range fields {
			if field == "-" {
				separator = i
				break
			}
		}
		if len(fields) < 10 || separator < 6 || len(fields) < separator+4 {
			return errors.New("selected staging mount evidence is malformed")
		}
		for _, field := range fields[:2] {
			id, err := strconv.ParseUint(field, 10, 64)
			if err != nil || id == 0 {
				return errors.New("selected staging mount identity is malformed")
			}
		}
		device := strings.Split(fields[2], ":")
		if len(device) != 2 {
			return errors.New("selected staging mount device is malformed")
		}
		for _, field := range device {
			if _, err := strconv.ParseUint(field, 10, 32); err != nil {
				return errors.New("selected staging mount device is malformed")
			}
		}
		mount := strings.NewReplacer("\\040", " ", "\\011", "\t", "\\012", "\n", "\\134", "\\").Replace(fields[4])
		if !filepath.IsAbs(mount) || filepath.Clean(mount) != mount || strings.Contains(mount, "\\") {
			return errors.New("selected staging mount path is noncanonical or undecodable")
		}
		for _, root := range roots {
			if mount == root || strings.HasPrefix(mount, root+"/") {
				return fmt.Errorf("selected staging has a mount at or below %s", root)
			}
		}
	}
	return nil
}

func (entry *selectedStagingDirectory) close() {
	if entry.held != nil {
		entry.held.Close()
	}
	if entry.parent != nil {
		entry.parent.Close()
	}
}

func inspectEmptySelectedStaging(root string, lane bool) (*selectedStagingDirectory, error) {
	parent, err := openFreshPreparationDirectory(filepath.Dir(root), false)
	if errors.Is(err, syscall.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fd, err := syscall.Openat(int(parent.Fd()), filepath.Base(root), syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if errors.Is(err, syscall.ENOENT) {
		parent.Close()
		return nil, nil
	}
	if err != nil {
		parent.Close()
		return nil, fmt.Errorf("selected staging root is foreign or unreadable: %s: %w", root, err)
	}
	entry := &selectedStagingDirectory{path: root, parent: parent, held: os.NewFile(uintptr(fd), root)}
	fail := func(err error) (*selectedStagingDirectory, error) { entry.close(); return nil, err }
	info, err := entry.held.Stat()
	if err != nil {
		return fail(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || stat.Uid != uint32(os.Geteuid()) || stat.Gid != uint32(os.Getegid()) ||
		(stat.Mode&07777 != 0700 && !(lane && stat.Mode&07777 == 0755)) {
		return fail(fmt.Errorf("selected staging root is not a correctly-owned expected-mode directory: %s", root))
	}
	entry.mode = info.Mode()
	if err := entry.revalidateEmpty(); err != nil {
		return fail(err)
	}
	return entry, nil
}

func (entry *selectedStagingDirectory) revalidateEmpty() error {
	parent, err := openFreshPreparationDirectory(filepath.Dir(entry.path), false)
	if err != nil {
		return err
	}
	parentInfo, err := parent.Stat()
	parent.Close()
	heldParent, heldParentErr := entry.parent.Stat()
	if err != nil || heldParentErr != nil || !os.SameFile(parentInfo, heldParent) {
		return fmt.Errorf("selected staging parent identity changed: %s", entry.path)
	}
	current, err := openFreshPreparationDirectory(entry.path, false)
	if err != nil {
		return err
	}
	defer current.Close()
	info, err := current.Stat()
	held, heldErr := entry.held.Stat()
	if err != nil || heldErr != nil || !os.SameFile(info, held) || info.Mode() != entry.mode {
		return fmt.Errorf("selected staging root identity or mode changed: %s", entry.path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || stat.Gid != uint32(os.Getegid()) {
		return fmt.Errorf("selected staging root owner changed: %s", entry.path)
	}
	children, err := current.ReadDir(-1)
	if err != nil {
		return fmt.Errorf("selected staging contents unreadable: %s: %w", entry.path, err)
	}
	if len(children) != 0 {
		return fmt.Errorf("nonempty selected staging is protected and requires a separate owned disposition: %s", entry.path)
	}
	return nil
}

func selectedStagingUnlinkat(parentFD int, name string, directory bool) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return errors.New("selected staging unlink requires one validated directory entry")
	}
	pointer, err := syscall.BytePtrFromString(name)
	if err != nil {
		return err
	}
	flags := uintptr(0)
	if directory {
		flags = 0x200 // Linux AT_REMOVEDIR; never recursive pathname removal.
	}
	_, _, errno := syscall.Syscall(syscall.SYS_UNLINKAT, uintptr(parentFD), uintptr(unsafe.Pointer(pointer)), flags)
	if errno != 0 {
		return errno
	}
	return nil
}

// Called only while BOTH the verified inherited GUI lease and native reset
// lock remain held. Every root is classified before the first selected effect.
func retireSelectedStaging(p nativeplan.Plan, opts wtx.NativeOptions, roots []string, lifecycle *selectedControllerLifecycleLease) (*selectedStagingRetirement, error) {
	if lifecycle == nil || p.Agent.ID.Project != "dev-all" || p.Agent.ID.Repo != "ouroboros-ide" || opts.Repo != "ouroboros-ide" {
		return nil, errors.New("selected staging retirement lacks its exact Product construction binding")
	}
	if err := lifecycle.revalidate(); err != nil {
		return nil, err
	}
	retirement := &selectedStagingRetirement{}
	fail := func(err error) (*selectedStagingRetirement, error) { retirement.close(); return nil, err }
	for i, root := range roots {
		entry, err := inspectEmptySelectedStaging(root, i == 0)
		if err != nil {
			return fail(err)
		}
		if entry != nil {
			retirement.entries = append(retirement.entries, entry)
		}
	}
	// Classification is read-only under both locks. Pin every eligible root,
	// including the workspace/Git parents missing from ordinary worktree/home
	// process scope, before invoking the existing custody reader. Keep these
	// identities even after unlink so deleted-directory contacts remain visible.
	heldTargets := []os.FileInfo{}
	for _, entry := range retirement.entries {
		info, err := entry.held.Stat()
		if err != nil {
			return fail(err)
		}
		heldTargets = append(heldTargets, info)
	}
	protect := func() error {
		if err := lifecycle.revalidate(); err != nil {
			return err
		}
		return inspectSelectedStagingProtection(p, roots, heldTargets)
	}
	if err := protect(); err != nil {
		return fail(err)
	}
	for _, entry := range retirement.entries {
		if err := protect(); err != nil {
			return fail(err)
		}
		if err := entry.revalidateEmpty(); err != nil {
			return fail(err)
		}
		if err := selectedStagingUnlinkat(int(entry.parent.Fd()), filepath.Base(entry.path), true); err != nil {
			return fail(fmt.Errorf("retire exact empty selected staging root %s: %w", entry.path, err))
		}
	}
	if err := protect(); err != nil {
		return fail(err)
	}
	// Keep retired inode descriptors alive until exclusive fresh reservation;
	// the filesystem cannot recycle those identities into the new receipt.
	return retirement, nil
}

package nativecmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const nativeProcSuperMagic = 0x9fa0

// A stale procfs chroot can retain a readable inode while directory traversal
// returns ESRCH. This proof is deliberately narrower than an unavailable-path
// exception: it identifies the exact live mount attachment through a peer in
// the same pinned mount namespace, then uses the ordinary physical ancestry
// walker on that attachment. Missing evidence preserves the original refusal.
func nativeProcProvesNonselectedProcDirectory(procPath, link string, observed os.FileInfo, identity nativeSlotProcessIdentity, targets []os.FileInfo, inventory []os.DirEntry) bool {
	if (link != "cwd" && link != "root") || len(inventory) == 0 || len(inventory) > 65536 {
		return false
	}
	subject, err := pinNativeProcProof(procPath)
	if err != nil {
		return false
	}
	defer subject.close()
	cwd, err := pinNativeProcObject(filepath.Join(procPath, "cwd"))
	if err != nil {
		return false
	}
	defer cwd.Close()
	cwdInfo, err := cwd.Stat()
	if err != nil || !os.SameFile(observed, cwdInfo) || !os.SameFile(cwdInfo, subject.rootInfo) {
		return false
	}
	mount, err := nativePinnedProcMount(cwd)
	if err != nil {
		return false
	}
	rootMount, err := nativePinnedProcMount(subject.root)
	if err != nil || mount != rootMount || mount.fsType != nativeProcSuperMagic {
		return false
	}
	// Before the procfs attachment root, every ancestor stays on this exact
	// filesystem. A selected object on that device makes exclusion ambiguous.
	boundaries, err := pinNativeProcBoundaries(identity, targets, mount.device)
	if err != nil {
		return false
	}
	defer boundaries.close()
	for _, entry := range inventory {
		if pid, err := strconv.Atoi(entry.Name()); err != nil || pid < 1 || entry.Name() == filepath.Base(procPath) {
			continue
		}
		peerPath := filepath.Join(nativeSlotProcRoot, entry.Name())
		peer, err := pinNativeProcProof(peerPath)
		if err != nil {
			continue
		}
		if !os.SameFile(subject.namespaceInfo, peer.namespaceInfo) {
			peer.close()
			continue
		}
		attachment, err := nativeProcMountAttachment(peerPath, mount.id)
		if err != nil {
			peer.close()
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
				continue
			}
			return false
		}
		proven := proveNativeProcAttachment(peer, subject, cwdInfo, mount, attachment, boundaries, targets)
		peer.close()
		// Once a usable representative was chosen, conflicting or drifting
		// evidence cannot be retried against a different representative.
		return proven
	}
	return false
}

type nativeProcProof struct {
	path, start             string
	parent                  int
	root, namespace         *os.File
	rootInfo, namespaceInfo os.FileInfo
}

func pinNativeProcObject(path string) (*os.File, error) {
	fd, err := syscall.Open(path, 0x200000|syscall.O_CLOEXEC, 0) // O_PATH; never enters a namespace.
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func pinNativeProcProof(path string) (*nativeProcProof, error) {
	parent, start := readNativeProcIdentity(path)
	if start == "" {
		return nil, fmt.Errorf("process identity unavailable")
	}
	p := &nativeProcProof{path: path, parent: parent, start: start}
	var err error
	p.root, err = pinNativeProcObject(filepath.Join(path, "root"))
	if err == nil {
		p.rootInfo, err = p.root.Stat()
	}
	if err == nil && !p.rootInfo.IsDir() {
		err = fmt.Errorf("process root is not a directory")
	}
	if err == nil {
		p.namespace, err = pinNativeProcObject(filepath.Join(path, "ns", "mnt"))
	}
	if err == nil {
		p.namespaceInfo, err = p.namespace.Stat()
	}
	if err != nil || !p.stable() {
		p.close()
		return nil, fmt.Errorf("process proof unavailable or changed")
	}
	return p, nil
}

func (p *nativeProcProof) close() {
	if p.root != nil {
		_ = p.root.Close()
	}
	if p.namespace != nil {
		_ = p.namespace.Close()
	}
}

func (p *nativeProcProof) stable() bool {
	parent, start := readNativeProcIdentity(p.path)
	root, rootErr := os.Stat(filepath.Join(p.path, "root"))
	namespace, nsErr := os.Stat(filepath.Join(p.path, "ns", "mnt"))
	return start == p.start && parent == p.parent && rootErr == nil && nsErr == nil && os.SameFile(root, p.rootInfo) && os.SameFile(namespace, p.namespaceInfo)
}

type nativeProcMount struct {
	id, device uint64
	fsType     int64
}

func nativePinnedProcMount(file *os.File) (nativeProcMount, error) {
	info, err := file.Stat()
	if err != nil {
		return nativeProcMount{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() {
		return nativeProcMount{}, fmt.Errorf("directory identity unavailable")
	}
	var fs syscall.Statfs_t
	if err := syscall.Fstatfs(int(file.Fd()), &fs); err != nil {
		return nativeProcMount{}, err
	}
	data, err := readNativeProcProofFile(fmt.Sprintf("/proc/self/fdinfo/%d", file.Fd()), 4096)
	if err != nil {
		return nativeProcMount{}, err
	}
	var id uint64
	for _, line := range strings.Split(data, "\n") {
		if value, ok := strings.CutPrefix(line, "mnt_id:"); ok {
			if id != 0 {
				return nativeProcMount{}, fmt.Errorf("duplicate mount identity")
			}
			id, err = strconv.ParseUint(strings.TrimSpace(value), 10, 64)
			if err != nil {
				return nativeProcMount{}, err
			}
		}
	}
	if id == 0 {
		return nativeProcMount{}, fmt.Errorf("mount identity unavailable")
	}
	return nativeProcMount{id: id, device: uint64(stat.Dev), fsType: int64(fs.Type)}, nil
}

func readNativeProcProofFile(path string, limit int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > limit {
		return "", fmt.Errorf("process proof exceeded its bound")
	}
	return string(data), nil
}

type nativeProcAttachment struct{ row, mountpoint, snapshot string }

func nativeProcMountAttachment(procPath string, id uint64) (nativeProcAttachment, error) {
	data, err := readNativeProcProofFile(filepath.Join(procPath, "mountinfo"), 1<<20)
	if err != nil {
		return nativeProcAttachment{}, err
	}
	var attachment nativeProcAttachment
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != strconv.FormatUint(id, 10) {
			continue
		}
		before, after, found := strings.Cut(line, " - ")
		left, right := strings.Fields(before), strings.Fields(after)
		if attachment.row != "" || !found || len(left) < 6 || len(right) < 3 || left[3] != "/" || right[0] != "proc" {
			return nativeProcAttachment{}, fmt.Errorf("ambiguous procfs attachment")
		}
		// Escaped or noncanonical names are not needed for this bounded class.
		point := left[4]
		if !filepath.IsAbs(point) || filepath.Clean(point) != point || strings.Contains(point, "\\") {
			return nativeProcAttachment{}, fmt.Errorf("ambiguous procfs mountpoint")
		}
		attachment = nativeProcAttachment{row: line, mountpoint: point, snapshot: data}
	}
	if attachment.row == "" {
		return attachment, os.ErrNotExist
	}
	return attachment, nil
}

type nativeProcBoundaryPin struct {
	path, absentPath string
	file             *os.File
	info             os.FileInfo
}
type nativeProcBoundaryPins []nativeProcBoundaryPin

func (pins nativeProcBoundaryPins) close() {
	for _, p := range pins {
		_ = p.file.Close()
	}
}
func (pins nativeProcBoundaryPins) stable() bool {
	for _, p := range pins {
		info, err := os.Stat(p.path)
		if err != nil || !os.SameFile(info, p.info) {
			return false
		}
		if p.absentPath != "" {
			if _, err := os.Stat(p.absentPath); !errors.Is(err, syscall.ENOENT) {
				return false
			}
		}
	}
	return true
}

func pinNativeProcBoundaries(identity nativeSlotProcessIdentity, targets []os.FileInfo, procDevice uint64) (nativeProcBoundaryPins, error) {
	var pins nativeProcBoundaryPins
	for _, path := range []string{identity.hostWorktree, identity.hostHome, identity.stateRoot} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			pins.close()
			return nil, fmt.Errorf("selected boundary is not canonical")
		}
		file, err := pinNativeProcObject(path)
		witnessPath, absentPath := path, ""
		if errors.Is(err, syscall.ENOENT) {
			// Fresh/surplus lanes can have no physical targets yet. Preserve
			// that known absence, pinned to its nearest existing parent, rather
			// than treating an empty target set as an exclusion shortcut.
			absentPath = path
			for depth := 0; depth < 256 && errors.Is(err, syscall.ENOENT); depth++ {
				parent := filepath.Dir(witnessPath)
				if parent == witnessPath {
					break
				}
				witnessPath = parent
				file, err = pinNativeProcObject(witnessPath)
			}
		}
		if err != nil {
			pins.close()
			return nil, err
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			pins.close()
			return nil, err
		}
		pins = append(pins, nativeProcBoundaryPin{path: witnessPath, absentPath: absentPath, file: file, info: info})
		stat, ok := info.Sys().(*syscall.Stat_t)
		matched := absentPath != "" && info.IsDir()
		for _, target := range targets {
			matched = matched || os.SameFile(info, target)
		}
		if !ok || !matched || uint64(stat.Dev) == procDevice {
			pins.close()
			return nil, fmt.Errorf("selected boundary cannot be excluded")
		}
	}
	if !pins.stable() {
		pins.close()
		return nil, fmt.Errorf("selected boundary observation changed")
	}
	for _, target := range targets {
		matched := false
		for _, pin := range pins {
			matched = matched || (pin.absentPath == "" && os.SameFile(target, pin.info))
		}
		if !matched {
			pins.close()
			return nil, fmt.Errorf("selected boundary changed")
		}
	}
	return pins, nil
}

func openNativeProcAttachment(peer *nativeProcProof, point string) (*os.File, error) {
	fd, err := syscall.Openat(int(peer.root.Fd()), strings.TrimPrefix(point, "/"), 0x200000|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "procfs-attachment"), nil
}

func proveNativeProcAttachment(peer, subject *nativeProcProof, cwdInfo os.FileInfo, mount nativeProcMount, attachment nativeProcAttachment, boundaries nativeProcBoundaryPins, targets []os.FileInfo) bool {
	root, err := openNativeProcAttachment(peer, attachment.mountpoint)
	if err != nil {
		return false
	}
	defer root.Close()
	rootMount, err := nativePinnedProcMount(root)
	if err != nil || rootMount != mount {
		return false
	}
	rootInfo, err := root.Stat()
	if err != nil {
		return false
	}
	// A full Linux procfs attachment has the kernel's root inode 1. An
	// arbitrary same-mount subtree is not an attachment-root witness.
	if stat, ok := rootInfo.Sys().(*syscall.Stat_t); !ok || stat.Ino != 1 {
		return false
	}
	// This is the existing complete physical walker, anchored to a held mount
	// root rather than repeatedly resolving the stale process's cwd/root link.
	touches, err := nativePhysicalDirectoryTouches(fmt.Sprintf("/proc/self/fd/%d", root.Fd()), targets)
	if err != nil || touches {
		return false
	}
	currentAttachment, err := nativeProcMountAttachment(peer.path, mount.id)
	if err != nil || currentAttachment != attachment || !peer.stable() || !subject.stable() || !boundaries.stable() {
		return false
	}
	currentRoot, err := openNativeProcAttachment(peer, attachment.mountpoint)
	if err != nil {
		return false
	}
	defer currentRoot.Close()
	currentMount, err := nativePinnedProcMount(currentRoot)
	if err != nil || currentMount != mount {
		return false
	}
	currentRootInfo, err := currentRoot.Stat()
	if err != nil || !os.SameFile(currentRootInfo, rootInfo) {
		return false
	}
	currentCwd, err := pinNativeProcObject(filepath.Join(subject.path, "cwd"))
	if err != nil {
		return false
	}
	defer currentCwd.Close()
	currentCwdInfo, err := currentCwd.Stat()
	if err != nil || !os.SameFile(currentCwdInfo, cwdInfo) {
		return false
	}
	currentMount, err = nativePinnedProcMount(currentCwd)
	if err != nil || currentMount != mount {
		return false
	}
	currentSubjectRoot, err := pinNativeProcObject(filepath.Join(subject.path, "root"))
	if err != nil {
		return false
	}
	defer currentSubjectRoot.Close()
	currentMount, err = nativePinnedProcMount(currentSubjectRoot)
	return err == nil && currentMount == mount && peer.stable() && subject.stable() && boundaries.stable()
}

package broker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// One shared directory descriptor serializes lifecycle and detects replacement.
// Inspection and dry-run create no coordination object.
type lifecycleGuard struct {
	root      string
	directory *os.File
	identity  os.FileInfo
	files     map[string]os.FileInfo
}

func lockLifecycle(ctx context.Context, c Config, create bool) (*lifecycleGuard, error) {

	directory, err := openDirectoryNoFollow(c.StateRoot, create)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	before, err := directory.Stat()
	if err != nil {
		directory.Close()
		return nil, err
	}
	st, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.IsDir() || st.Uid != uint32(os.Getuid()) || before.Mode().Perm()&0022 != 0 {
		directory.Close()
		return nil, fmt.Errorf("broker state root has foreign or unsafe identity: %s", c.StateRoot)
	}
	fd := int(directory.Fd())
	after, err := directory.Stat()
	if err != nil || !os.SameFile(before, after) {
		directory.Close()
		return nil, fmt.Errorf("broker state root changed while opening")
	}
	deadline := time.Now().Add(c.StartTimeout)
	for {
		err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			directory.Close()
			return nil, err
		}
		if time.Now().After(deadline) {
			directory.Close()
			return nil, fmt.Errorf("broker lifecycle lock deadline exceeded")
		}
		select {
		case <-ctx.Done():
			directory.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	g := &lifecycleGuard{root: c.StateRoot, directory: directory, identity: after, files: map[string]os.FileInfo{}}
	if err := g.checkRoot(); err != nil {
		g.Close()
		return nil, err
	}
	for _, path := range []string{PIDFile(c), StateFile(c), stopIntentFile(c)} {
		info, err := os.Lstat(g.heldPath(path))
		if errors.Is(err, os.ErrNotExist) {
			g.files[path] = nil
			continue
		}
		if err != nil {
			g.Close()
			return nil, err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 || info.Mode().Perm()&0077 != 0 {
			g.Close()
			return nil, fmt.Errorf("broker metadata has foreign or unsafe identity: %s", path)
		}
		g.files[path] = info
	}
	return g, nil
}
func (g *lifecycleGuard) Close() {
	if g != nil && g.directory != nil {
		_ = syscall.Flock(int(g.directory.Fd()), syscall.LOCK_UN)
		_ = g.directory.Close()
	}
}

func (g *lifecycleGuard) heldPath(path string) string {
	return filepath.Join("/proc/self/fd", strconv.Itoa(int(g.directory.Fd())), filepath.Base(path))
}
func (g *lifecycleGuard) checkRoot() error {
	directory, err := openDirectoryNoFollow(g.root, false)
	if err != nil {
		return fmt.Errorf("broker state directory identity changed: %w", err)
	}
	defer directory.Close()
	now, err := directory.Stat()
	if err != nil || !os.SameFile(g.identity, now) {
		return fmt.Errorf("broker state directory identity changed")
	}
	return nil
}
func (g *lifecycleGuard) checkFile(path string) error {
	if err := g.checkRoot(); err != nil {
		return err
	}
	now, err := os.Lstat(g.heldPath(path))
	before := g.files[path]
	if before == nil && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || before == nil || !os.SameFile(before, now) {
		return fmt.Errorf("broker metadata identity changed: %s", path)
	}
	return nil
}
func (g *lifecycleGuard) read(path string) ([]byte, error) {
	if err := g.checkFile(path); err != nil {
		return nil, err
	}
	if g.files[path] == nil {
		return nil, os.ErrNotExist
	}
	fd, err := syscall.Openat(int(g.directory.Fd()), filepath.Base(path), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !os.SameFile(info, g.files[path]) {
		return nil, fmt.Errorf("broker metadata changed while opening: %s", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, 32769))
	if len(data) > 32768 {
		return nil, fmt.Errorf("broker metadata exceeds bounded schema size")
	}
	return data, err
}
func (g *lifecycleGuard) write(path string, data []byte) error {
	if err := g.checkFile(path); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Join("/proc/self/fd", strconv.Itoa(int(g.directory.Fd()))), ".broker-metadata-")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = g.checkFile(path); err != nil {
		return err
	}
	if err = syscall.Renameat(int(g.directory.Fd()), filepath.Base(temp), int(g.directory.Fd()), filepath.Base(path)); err != nil {
		return err
	}
	g.files[path], err = os.Lstat(g.heldPath(path))
	return err
}
func (g *lifecycleGuard) remove(path string) error {
	if err := g.checkFile(path); err != nil {
		return err
	}
	if g.files[path] == nil {
		return nil
	}
	if err := syscall.Unlinkat(int(g.directory.Fd()), filepath.Base(path)); err != nil {
		return err
	}
	g.files[path] = nil
	return nil
}
func stopIntentFile(c Config) string { return filepath.Join(c.StateRoot, "broker.stopped.json") }

// Walk every parent without following links. All mutations stay relative to
// held descriptors, so replacing a named parent cannot redirect an operation.
func openDirectoryNoFollow(path string, create bool) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("broker directory is not canonical absolute")
	}
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" {
			continue
		}
		next, e := syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if errors.Is(e, syscall.ENOENT) && create {
			e = syscall.Mkdirat(fd, part, 0700)
			if e == nil || errors.Is(e, syscall.EEXIST) {
				next, e = syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
			}
		}
		syscall.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), path), nil
}
func removeRetainedSocket(path string, retained os.FileInfo) error {
	parent, err := openDirectoryNoFollow(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer parent.Close()
	held := filepath.Join("/proc/self/fd", strconv.Itoa(int(parent.Fd())), filepath.Base(path))
	current, err := os.Lstat(held)
	if err != nil || !os.SameFile(retained, current) {
		return fmt.Errorf("broker socket changed before retained unlink")
	}
	return syscall.Unlinkat(int(parent.Fd()), filepath.Base(path))
}

// Unlock without dropping the retained root descriptor during a fixed job.
func (g *lifecycleGuard) unlock() error {
	return syscall.Flock(int(g.directory.Fd()), syscall.LOCK_UN)
}
func (g *lifecycleGuard) retainFile(path string) (*os.File, error) {
	if err := g.checkFile(path); err != nil {
		return nil, err
	}
	if g.files[path] == nil {
		return nil, nil
	}
	fd, err := syscall.Openat(int(g.directory.Fd()), filepath.Base(path), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !os.SameFile(g.files[path], info) {
		f.Close()
		return nil, fmt.Errorf("broker metadata changed while retaining custody")
	}
	return f, nil
}
func (g *lifecycleGuard) retainSocket(path string) (*os.File, os.FileInfo, error) {
	if filepath.Dir(path) != g.root {
		return nil, nil, fmt.Errorf("station socket is outside retained root")
	}
	if err := g.checkRoot(); err != nil {
		return nil, nil, err
	}
	before, err := socketInfo(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	// Linux O_PATH retains a socket inode without connecting or opening a peer.
	const oPath = 0x200000
	fd, err := syscall.Openat(int(g.directory.Fd()), filepath.Base(path), oPath|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !os.SameFile(before, info) {
		f.Close()
		return nil, nil, fmt.Errorf("broker socket changed while retaining custody")
	}
	return f, info, nil
}

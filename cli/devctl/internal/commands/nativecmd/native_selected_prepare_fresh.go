package nativecmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	wtx "devkit/cli/devctl/internal/worktrees"
)

const selectedPreparationReceiptName = "selected-preparation.json"

type selectedPreparationRoot struct {
	Path   string `json:"path"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type selectedPreparationReceipt struct {
	Schema  string                    `json:"schema"`
	Project string                    `json:"project"`
	Repo    string                    `json:"repo"`
	Index   int                       `json:"index"`
	Roots   []selectedPreparationRoot `json:"roots"`
}

type freshSelectedClaim struct {
	dryRun      bool
	lock        *nativeSlotResetLock
	directories []*os.File
}

func freshPreparationPaths(p nativeplan.Plan, opts wtx.NativeOptions) ([]string, error) {
	lane := filepath.Join(opts.WorktreeRoot, fmt.Sprintf("agent%d", opts.Index))
	git := filepath.Join(opts.WorktreeRoot, ".devkit", "git", fmt.Sprintf("agent%d", opts.Index))
	if opts.Index < 1 || p.Agent.ID.Index != opts.Index || p.Agent.ID.Project != "dev-all" ||
		p.Agent.HostWorktree != filepath.Join(lane, opts.Repo) ||
		!strings.HasPrefix(p.Agent.HostHome, lane+string(filepath.Separator)) {
		return nil, fmt.Errorf("fresh selected preparation requires exact source-owned lane geometry")
	}
	roots := []string{lane, p.Agent.StateRoot, git}
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == "/" {
			return nil, fmt.Errorf("fresh selected preparation requires canonical absolute roots")
		}
		for _, other := range roots[:i] {
			if root == other || strings.HasPrefix(root, other+"/") || strings.HasPrefix(other, root+"/") {
				return nil, fmt.Errorf("fresh selected preparation roots overlap")
			}
		}
	}
	return roots, nil
}

// Open every component relative to its held directory. A symlink is never a
// source-owned construction parent. Only missing parents are materialized.
func openFreshPreparationDirectory(path string, create bool) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("noncanonical fresh preparation directory")
	}
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if component == "" {
			continue
		}
		next, openErr := syscall.Openat(fd, component, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if errors.Is(openErr, syscall.ENOENT) && create {
			if mkErr := syscall.Mkdirat(fd, component, 0700); mkErr != nil && !errors.Is(mkErr, syscall.EEXIST) {
				syscall.Close(fd)
				return nil, mkErr
			}
			next, openErr = syscall.Openat(fd, component, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		}
		syscall.Close(fd)
		if openErr != nil {
			return nil, openErr
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), path), nil
}

func requireFreshPreparationAbsent(root string) error {
	parent, err := openFreshPreparationDirectory(filepath.Dir(root), false)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("fresh preparation parent %s: %w", root, err)
	}
	defer parent.Close()
	fd, err := syscall.Openat(int(parent.Fd()), filepath.Base(root), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err == nil {
		syscall.Close(fd)
	}
	return fmt.Errorf("fresh selected preparation refuses occupied or inaccessible root %s", root)
}

func claimFreshSelectedPreparation(p nativeplan.Plan, opts wtx.NativeOptions, dryRun bool) (*freshSelectedClaim, error) {
	roots, err := freshPreparationPaths(p, opts)
	if err != nil {
		return nil, err
	}
	if p.GUITargetConfig != nil && p.GUITargetConfig.RetainedHistoryManifest != "" {
		return nil, fmt.Errorf("fresh selected preparation refuses predecessor history projection")
	}
	claim := &freshSelectedClaim{dryRun: dryRun}
	if dryRun {
		return claim, nil
	}
	// Refuse foreign ancestors before creating even coordination metadata.
	for _, root := range roots {
		if err := requireFreshPreparationAbsent(root); err != nil {
			return nil, err
		}
	}
	coordination, err := nativeSlotResetCoordinationRoot(opts.WorktreeRoot)
	if err != nil {
		return nil, err
	}
	coordinationDirectory, err := openFreshPreparationDirectory(coordination, true)
	if err != nil {
		return nil, err
	}
	defer coordinationDirectory.Close()
	claim.lock, err = acquireNativeSlotResetLock(coordination, p.Agent.ID.Project)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*freshSelectedClaim, error) { claim.close(); return nil, err }
	// Check all roots before reserving any. A later collision keeps only our
	// partial claim; no conflicting allocation is overwritten or cleaned up.
	for _, root := range roots {
		if err := requireFreshPreparationAbsent(root); err != nil {
			return fail(err)
		}
	}
	for _, root := range roots {
		parent, err := openFreshPreparationDirectory(filepath.Dir(root), true)
		if err != nil {
			return fail(err)
		}
		err = syscall.Mkdirat(int(parent.Fd()), filepath.Base(root), 0700)
		if err != nil {
			parent.Close()
			return fail(fmt.Errorf("reserve fresh selected root %s: %w", root, err))
		}
		fd, err := syscall.Openat(int(parent.Fd()), filepath.Base(root), syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		parent.Close()
		if err != nil {
			return fail(err)
		}
		claim.directories = append(claim.directories, os.NewFile(uintptr(fd), root))
	}
	return claim, nil
}

func (claim *freshSelectedClaim) close() {
	if claim == nil {
		return
	}
	for _, dir := range claim.directories {
		_ = dir.Close()
	}
	claim.directories = nil
	if claim.lock != nil {
		_ = claim.lock.release()
		claim.lock = nil
	}
}

func (claim *freshSelectedClaim) complete(p nativeplan.Plan) error {
	if claim.dryRun {
		return nil
	}
	if err := claim.verifyNamespace(); err != nil {
		return err
	}
	receipt := selectedPreparationReceipt{Schema: "devkit/selected-preparation/v1", Project: p.Agent.ID.Project, Repo: p.Agent.ID.Repo, Index: p.Agent.ID.Index}
	paths := []string{}
	for _, held := range claim.directories {
		info, err := held.Stat()
		if err != nil {
			return err
		}
		current, err := os.Lstat(held.Name())
		if err != nil || !os.SameFile(info, current) {
			return fmt.Errorf("fresh selected namespace identity changed")
		}
		paths = append(paths, held.Name())
	}
	paths = append(paths, p.Agent.HostWorktree, p.Agent.HostHome, filepath.Join(claim.directories[2].Name(), p.Agent.ID.Repo+".git"))
	for _, path := range paths {
		dir, err := openFreshPreparationDirectory(path, false)
		if err != nil {
			return fmt.Errorf("fresh selected completion root %s: %w", path, err)
		}
		info, err := dir.Stat()
		dir.Close()
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("fresh selected completion lacks native identity")
		}
		receipt.Roots = append(receipt.Roots, selectedPreparationRoot{Path: path, Device: uint64(stat.Dev), Inode: stat.Ino})
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	fd, err := syscall.Openat(int(claim.directories[1].Fd()), selectedPreparationReceiptName, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return fmt.Errorf("create fresh selected completion: %w", err)
	}
	file := os.NewFile(uintptr(fd), selectedPreparationReceiptName)
	_, writeErr := file.Write(append(data, '\n'))
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr, claim.directories[1].Sync())
}

// The held reset coordination and owning GUI target lease serialize supported
// constructors/lifecycle effects. Identity checks reject unexpected namespace
// substitution at each preparation phase; they are not a filesystem sandbox
// against a privileged actor that ignores those ownership protocols.
func (claim *freshSelectedClaim) verifyNamespace() error {
	if claim.dryRun {
		return nil
	}
	for _, held := range claim.directories {
		expected, err := held.Stat()
		if err != nil {
			return err
		}
		current, err := openFreshPreparationDirectory(held.Name(), false)
		if err != nil {
			return fmt.Errorf("fresh selected namespace changed before effect: %w", err)
		}
		actual, err := current.Stat()
		current.Close()
		if err != nil || !os.SameFile(expected, actual) {
			return fmt.Errorf("fresh selected namespace identity changed before effect")
		}
	}
	return nil
}
func (claim *freshSelectedClaim) runPhase(effect func() error) error {
	if err := claim.verifyNamespace(); err != nil {
		return err
	}
	if err := effect(); err != nil {
		return err
	}
	return claim.verifyNamespace()
}

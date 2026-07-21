package productruntime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"devkit/cli/devctl/internal/productadapter"
)

type candidateClaim struct {
	root       string
	parentFile *os.File
	rootFile   *os.File
	device     uint64
	inode      uint64
}

func claimCandidate(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
) (*candidateClaim, productadapter.Geometry, error) {
	parent := filepath.Dir(consumer.CandidateRoot)
	leaf := filepath.Base(consumer.CandidateRoot)
	if leaf == "" || leaf == "." || leaf == ".." || strings.ContainsRune(leaf, filepath.Separator) {
		return nil, productadapter.Geometry{}, fmt.Errorf("Product candidate leaf is invalid")
	}
	if err := requirePlainDirectory(parent); err != nil {
		return nil, productadapter.Geometry{}, fmt.Errorf("validate Product candidate parent: %w", err)
	}
	parentFile, err := os.Open(parent)
	if err != nil {
		return nil, productadapter.Geometry{}, err
	}
	parentFD := int(parentFile.Fd())
	created := true
	if err := syscall.Mkdirat(parentFD, leaf, 0o700); err != nil {
		if errors.Is(err, syscall.EEXIST) {
			created = false
		} else {
			parentFile.Close()
			return nil, productadapter.Geometry{}, err
		}
	}
	if created {
		parentFile.Close()
		return nil, productadapter.Geometry{}, fmt.Errorf("Product construction requires the canonical offline-seeded candidate boundary")
	}
	if err := validateOfflineSeededCandidate(authority, consumer); err != nil {
		parentFile.Close()
		return nil, productadapter.Geometry{}, err
	}
	rootFD, err := syscall.Openat(parentFD, leaf, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		parentFile.Close()
		return nil, productadapter.Geometry{}, fmt.Errorf("open claimed Product candidate: %w", err)
	}
	rootFile := os.NewFile(uintptr(rootFD), consumer.CandidateRoot)
	info, err := rootFile.Stat()
	if err != nil {
		rootFile.Close()
		parentFile.Close()
		return nil, productadapter.Geometry{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != consumer.UID || int(stat.Gid) != consumer.GID || info.Mode().Perm() != 0o700 {
		rootFile.Close()
		parentFile.Close()
		return nil, productadapter.Geometry{}, fmt.Errorf("offline-seeded Product candidate has unsafe root identity")
	}
	claim := &candidateClaim{
		root:       consumer.CandidateRoot,
		parentFile: parentFile,
		rootFile:   rootFile,
		device:     uint64(stat.Dev),
		inode:      stat.Ino,
	}
	return claim, geometryForConsumer(consumer), nil
}

func (claim *candidateClaim) Close() {
	if claim.rootFile != nil {
		_ = claim.rootFile.Close()
	}
	if claim.parentFile != nil {
		_ = claim.parentFile.Close()
	}
}

func (claim *candidateClaim) Validate(phase string) error {
	if claim.rootFile == nil || claim.parentFile == nil {
		return fmt.Errorf("Product candidate claim is closed during %s", phase)
	}
	info, err := claim.rootFile.Stat()
	if err != nil {
		return fmt.Errorf("revalidate Product candidate FD during %s: %w", phase, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || uint64(stat.Dev) != claim.device || stat.Ino != claim.inode {
		return fmt.Errorf("Product candidate FD identity changed during %s", phase)
	}
	pathFD, err := syscall.Openat(
		int(claim.parentFile.Fd()),
		filepath.Base(claim.root),
		syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,
		0,
	)
	if err != nil {
		return fmt.Errorf("revalidate Product candidate path during %s: %w", phase, err)
	}
	defer syscall.Close(pathFD)
	var pathStat syscall.Stat_t
	if err := syscall.Fstat(pathFD, &pathStat); err != nil {
		return fmt.Errorf("inspect Product candidate path during %s: %w", phase, err)
	}
	if uint64(pathStat.Dev) != claim.device || pathStat.Ino != claim.inode ||
		pathStat.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		return fmt.Errorf("Product candidate path identity changed during %s", phase)
	}
	return nil
}

func (claim *candidateClaim) WriteInitialReceipt(receipt productadapter.Receipt) error {
	if err := claim.Validate("initial receipt"); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Dir(receipt.Geometry.Receipt), 0o700); err != nil {
		return fmt.Errorf("create Product candidate state root: %w", err)
	}
	if err := productadapter.WriteReceipt(receipt.Geometry.Receipt, receipt, true); err != nil {
		return fmt.Errorf(
			"claimed Product candidate %s but could not persist its typed failed-candidate receipt: %w",
			claim.root,
			err,
		)
	}
	return nil
}

func geometryForConsumer(consumer productadapter.ConsumerManifest) productadapter.Geometry {
	return productadapter.Geometry{
		CandidateRoot: consumer.CandidateRoot,
		AgentRoot:     consumer.AgentRoot,
		Worktree:      consumer.WorktreePath,
		CommonDir:     consumer.CommonDirPath,
		Home:          consumer.HomePath,
		State:         consumer.StateRoot,
		Receipt:       consumer.ReceiptPath,
	}
}

func requirePlainDirectory(path string) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || clean != path {
		return fmt.Errorf("directory path is not exact and absolute")
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return err
	}
	if resolved != clean {
		return fmt.Errorf("directory path traverses a symlink")
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path is not a plain directory")
	}
	return nil
}

package launch

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

func prepareGitBacklink(p nativeplan.Plan) error {
	if err := nativeplan.ValidateGitBacklinkProjection(p); err != nil {
		return err
	}
	if p.GitBacklink == nil {
		return nil
	}
	projection := p.GitBacklink
	parent, err := openCanonicalDirectoryNoFollow(filepath.Dir(projection.Source))
	if err != nil {
		return err
	}
	defer parent.Close()
	_, uid, err := openedFileIdentityAndUID(parent)
	if err != nil {
		return err
	}
	if uid != os.Getuid() {
		return fmt.Errorf("native Git projection state has foreign owner")
	}
	if err := verifyGitBacklink(p, true); err != nil {
		return err
	}
	// A canonical existing file already contains the immutable recipe. Never
	// truncate it: an older native mount may still hold its inode.
	if _, err := os.Lstat(projection.Source); err == nil {
		return nil
	}
	name := filepath.Base(projection.Source)
	stagingName := fmt.Sprintf(".native-git-backlink-%d-%d", os.Getpid(), time.Now().UnixNano())
	fd, err := syscall.Openat(int(parent.Fd()), stagingName, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	staging := os.NewFile(uintptr(fd), stagingName)
	defer staging.Close()
	defer syscall.Unlinkat(int(parent.Fd()), stagingName)
	if _, err := staging.WriteString(projection.Value); err != nil {
		return err
	}
	if err := staging.Sync(); err != nil {
		return err
	}
	if err := staging.Close(); err != nil {
		return err
	}
	if err := nativeplan.ValidateGitBacklinkProjection(p); err != nil {
		return err
	}
	if err := syscall.Renameat(int(parent.Fd()), stagingName, int(parent.Fd()), name); err != nil {
		return err
	}
	if err := parent.Sync(); err != nil {
		return err
	}
	return verifyGitBacklink(p, false)
}

func verifyGitBacklink(p nativeplan.Plan, allowMissing bool) error {
	if p.GitBacklink == nil {
		return nil
	}
	projection := p.GitBacklink
	parent, err := openCanonicalDirectoryNoFollow(filepath.Dir(projection.Source))
	if allowMissing && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.Close()
	file, err := openRegularFileAtNoFollow(parent, filepath.Base(projection.Source), projection.Source)
	if allowMissing && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	_, uid, err := openedFileIdentityAndUID(file)
	if err != nil {
		return err
	}
	if uid != os.Getuid() {
		return fmt.Errorf("native Git backlink projection has foreign owner")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(len(projection.Value)+1)))
	if err != nil {
		return err
	}
	if !bytes.Equal(data, []byte(projection.Value)) {
		return fmt.Errorf("native Git backlink projection differs from selected recipe")
	}
	return nil
}

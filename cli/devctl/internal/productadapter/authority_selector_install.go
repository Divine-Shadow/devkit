package productadapter

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type authoritySelectorInstallOptions struct {
	SelectorPath string
	StoreRoot    string
	RequiredUID  int
	CurrentEUID  func() int
}

// InstallAuthoritySelector atomically routes Product consumers to one exact
// immutable runtime manifest. It verifies identity bytes but never derives,
// rewrites, or supplements the authoritative manifest.
func InstallAuthoritySelector(manifestPath, manifestSHA256 string) error {
	return installAuthoritySelector(manifestPath, manifestSHA256, authoritySelectorInstallOptions{
		SelectorPath: canonicalAuthoritySelector,
		StoreRoot:    "/nix/store",
		RequiredUID:  0,
		CurrentEUID:  os.Geteuid,
	})
}

func installAuthoritySelector(manifestPath, manifestSHA256 string, options authoritySelectorInstallOptions) error {
	if options.CurrentEUID == nil || options.CurrentEUID() != options.RequiredUID {
		return fmt.Errorf("Product authority selector installation requires uid %d", options.RequiredUID)
	}
	selectorPath := filepath.Clean(options.SelectorPath)
	if selectorPath != options.SelectorPath || !filepath.IsAbs(selectorPath) || filepath.Base(selectorPath) != "authority-selector.json" {
		return fmt.Errorf("Product authority selector destination is invalid")
	}
	storeRoot := filepath.Clean(options.StoreRoot)
	if manifestPath != strings.TrimSpace(manifestPath) || manifestPath != filepath.Clean(manifestPath) {
		return fmt.Errorf("Product authority manifest path must be exact and canonical")
	}
	if !filepath.IsAbs(storeRoot) || manifestPath == storeRoot || !strings.HasPrefix(manifestPath, storeRoot+string(os.PathSeparator)) {
		return fmt.Errorf("Product authority manifest must be one immutable store artifact")
	}
	if len(manifestSHA256) != 64 || strings.ToLower(manifestSHA256) != manifestSHA256 {
		return fmt.Errorf("Product authority manifest digest is invalid")
	}
	if _, err := hex.DecodeString(manifestSHA256); err != nil {
		return fmt.Errorf("Product authority manifest digest is invalid")
	}
	manifest, err := openImmutableManifestForInstall(manifestPath, options.RequiredUID)
	if err != nil {
		return err
	}
	payload, err := io.ReadAll(io.LimitReader(manifest, (8<<20)+1))
	closeErr := manifest.Close()
	if err != nil || closeErr != nil || len(payload) > 8<<20 {
		return fmt.Errorf("read Product authority manifest: bounded read failed")
	}
	actual := sha256.Sum256(payload)
	if hex.EncodeToString(actual[:]) != manifestSHA256 {
		return fmt.Errorf("Product authority manifest digest does not match immutable bytes")
	}
	selectorBytes, err := json.Marshal(authoritySelector{
		SchemaVersion:  authoritySelectorSchema,
		ManifestPath:   manifestPath,
		ManifestSHA256: manifestSHA256,
	})
	if err != nil {
		return err
	}
	selectorBytes = append(selectorBytes, '\n')
	return replaceProtectedSelector(selectorPath, selectorBytes, options.RequiredUID)
}

func openImmutableManifestForInstall(path string, requiredUID int) (*os.File, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("resolve Product authority manifest: %w", err)
	}
	if resolved != path {
		return nil, fmt.Errorf("Product authority manifest must not traverse symlinks")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open Product authority manifest: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("hold Product authority manifest")
	}
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFREG ||
		int(stat.Uid) != requiredUID || stat.Mode&0o222 != 0 {
		_ = file.Close()
		return nil, fmt.Errorf("Product authority manifest is not an immutable uid-%d regular file", requiredUID)
	}
	return file, nil
}

func replaceProtectedSelector(selectorPath string, payload []byte, requiredUID int) error {
	parent := filepath.Dir(selectorPath)
	parentFD, err := syscall.Open(parent, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open Product authority selector parent: %w", err)
	}
	defer syscall.Close(parentFD)
	var parentStat syscall.Stat_t
	if err := syscall.Fstat(parentFD, &parentStat); err != nil ||
		parentStat.Mode&syscall.S_IFMT != syscall.S_IFDIR ||
		int(parentStat.Uid) != requiredUID || parentStat.Mode&0o022 != 0 {
		return fmt.Errorf("Product authority selector parent is not uid-%d owned and protected", requiredUID)
	}
	base := filepath.Base(selectorPath)
	if err := verifyExistingSelector(parentFD, base, requiredUID); err != nil {
		return err
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return fmt.Errorf("create Product authority selector generation: %w", err)
	}
	tempName := ".authority-selector." + hex.EncodeToString(random)
	tempFD, err := syscall.Openat(parentFD, tempName, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("create Product authority selector generation: %w", err)
	}
	tempPath := filepath.Join(parent, tempName)
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	temp := os.NewFile(uintptr(tempFD), tempPath)
	if temp == nil {
		_ = syscall.Close(tempFD)
		return fmt.Errorf("hold Product authority selector generation")
	}
	if err := temp.Chown(requiredUID, -1); err != nil {
		_ = temp.Close()
		return fmt.Errorf("own Product authority selector generation: %w", err)
	}
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("protect Product authority selector generation: %w", err)
	}
	if _, err := temp.Write(payload); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write Product authority selector generation: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync Product authority selector generation: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close Product authority selector generation: %w", err)
	}
	if err := syscall.Renameat(parentFD, tempName, parentFD, base); err != nil {
		return fmt.Errorf("commit Product authority selector generation: %w", err)
	}
	committed = true
	if err := syscall.Fsync(parentFD); err != nil {
		return fmt.Errorf("sync Product authority selector parent: %w", err)
	}
	return verifyExistingSelector(parentFD, base, requiredUID)
}

func verifyExistingSelector(parentFD int, base string, requiredUID int) error {
	fd, err := syscall.Openat(parentFD, base, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open existing Product authority selector without symlink traversal: %w", err)
	}
	defer syscall.Close(fd)
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFREG ||
		int(stat.Uid) != requiredUID || stat.Mode&0o777 != 0o600 {
		return fmt.Errorf("existing Product authority selector is not an uid-%d plain 0600 file", requiredUID)
	}
	return nil
}

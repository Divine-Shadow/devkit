//go:build !devkitintegration

package productadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	canonicalAuthoritySelector = "/var/lib/product-runtime/authority-selector.json"
	authoritySelectorSchema    = "devkit/product-runtime-authority-selector/v1"
)

type authoritySelector struct {
	SchemaVersion  string `json:"schemaVersion"`
	ManifestPath   string `json:"manifestPath"`
	ManifestSHA256 string `json:"manifestSha256"`
}

func authorityLocator() string {
	return canonicalAuthoritySelector
}

// openAuthorityManifest opens one root-owned selector generation, decodes it
// strictly from the held descriptor, then opens and holds the exact immutable
// manifest it names. The selector routes to identity; it does not derive or
// rewrite any identity field.
func openAuthorityManifest(locator string) (*os.File, string, error) {
	if filepath.Clean(locator) != canonicalAuthoritySelector {
		return nil, "", fmt.Errorf("Product adapter authority selector must be %s", canonicalAuthoritySelector)
	}
	selectorFile, err := openProtectedSelector(canonicalAuthoritySelector)
	if err != nil {
		return nil, "", err
	}
	defer selectorFile.Close()
	payload, err := io.ReadAll(io.LimitReader(selectorFile, 4097))
	if err != nil || len(payload) > 4096 {
		return nil, "", fmt.Errorf("read Product authority selector: bounded read failed")
	}
	selector, err := parseAuthoritySelector(payload)
	if err != nil {
		return nil, "", err
	}
	if err := validateImmutableLeaf(selector.ManifestPath); err != nil {
		return nil, "", err
	}
	fd, err := syscall.Open(selector.ManifestPath, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", fmt.Errorf("open selected Product authority manifest: %w", err)
	}
	file := os.NewFile(uintptr(fd), selector.ManifestPath)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, "", fmt.Errorf("hold selected Product authority manifest")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, "", fmt.Errorf("selected Product authority manifest is not a regular file")
	}
	manifest, err := io.ReadAll(io.LimitReader(file, (8<<20)+1))
	if err != nil || len(manifest) > 8<<20 {
		_ = file.Close()
		return nil, "", fmt.Errorf("read selected Product authority manifest: bounded read failed")
	}
	sum := sha256.Sum256(manifest)
	if hex.EncodeToString(sum[:]) != selector.ManifestSHA256 {
		_ = file.Close()
		return nil, "", fmt.Errorf("selected Product authority manifest digest does not match")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, "", err
	}
	return file, selector.ManifestPath, nil
}

func parseAuthoritySelector(payload []byte) (authoritySelector, error) {
	var selector authoritySelector
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&selector); err != nil {
		return authoritySelector{}, fmt.Errorf("decode Product authority selector: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return authoritySelector{}, fmt.Errorf("Product authority selector contains trailing data")
	}
	if selector.SchemaVersion != authoritySelectorSchema ||
		len(selector.ManifestSHA256) != 64 ||
		strings.ToLower(selector.ManifestSHA256) != selector.ManifestSHA256 {
		return authoritySelector{}, fmt.Errorf("Product authority selector is invalid")
	}
	if _, err := hex.DecodeString(selector.ManifestSHA256); err != nil {
		return authoritySelector{}, fmt.Errorf("Product authority selector digest is invalid")
	}
	if filepath.Clean(selector.ManifestPath) != selector.ManifestPath || !strings.HasPrefix(selector.ManifestPath, "/nix/store/") {
		return authoritySelector{}, fmt.Errorf("Product authority selector manifest path is invalid")
	}
	return selector, nil
}

func openProtectedSelector(path string) (*os.File, error) {
	parent := filepath.Dir(path)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return nil, err
	}
	parentStat, ok := parentInfo.Sys().(*syscall.Stat_t)
	if !ok || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 ||
		parentStat.Uid != 0 || parentInfo.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("Product authority selector parent is not root-owned and protected")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open Product authority selector: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("hold Product authority selector")
	}
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil ||
		stat.Mode&syscall.S_IFMT != syscall.S_IFREG || stat.Uid != 0 || stat.Mode&0o777 != 0o600 {
		_ = file.Close()
		return nil, fmt.Errorf("Product authority selector is not a root-owned plain 0600 file")
	}
	return file, nil
}

func validateImmutableLeaf(path string) error {
	clean := filepath.Clean(path)
	if clean != path || !strings.HasPrefix(clean, "/nix/store/") {
		return fmt.Errorf("Product authority artifact must resolve beneath /nix/store")
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return err
	}
	if resolved != clean {
		return fmt.Errorf("Product authority artifact must not traverse store symlinks")
	}
	return nil
}

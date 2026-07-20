//go:build !devkitintegration

package productadapter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const canonicalAuthorityLocator = "/etc/fleet/dev-all-runtime-bundle/authority.json"

func authorityLocator() string {
	return canonicalAuthorityLocator
}

// openAuthorityManifest walks the protected locator through held directory
// descriptors. The final environment.etc leaf may be a root-owned symlink, but
// the descriptor it resolves to must be an immutable, regular Nix-store leaf.
func openAuthorityManifest(locator string) (*os.File, string, error) {
	if filepath.Clean(locator) != canonicalAuthorityLocator {
		return nil, "", fmt.Errorf("Product adapter authority locator must be %s", canonicalAuthorityLocator)
	}
	components := strings.Split(strings.TrimPrefix(canonicalAuthorityLocator, "/"), "/")
	if len(components) < 2 {
		return nil, "", fmt.Errorf("Product adapter authority locator has no protected parent")
	}
	parent, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", fmt.Errorf("open Product authority locator root: %w", err)
	}
	defer func() {
		_ = syscall.Close(parent)
	}()
	if err := validateProtectedDirectoryFD(parent, "/"); err != nil {
		return nil, "", err
	}
	current := "/"
	for _, component := range components[:len(components)-1] {
		next, openErr := syscall.Openat(parent, component, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if openErr != nil {
			return nil, "", fmt.Errorf("open Product authority locator parent %s: %w", filepath.Join(current, component), openErr)
		}
		syscall.Close(parent)
		parent = next
		current = filepath.Join(current, component)
		if err := validateProtectedDirectoryFD(parent, current); err != nil {
			return nil, "", err
		}
	}

	leafInfo, err := os.Lstat(canonicalAuthorityLocator)
	if err != nil {
		return nil, "", fmt.Errorf("inspect Product authority locator leaf: %w", err)
	}
	if leafInfo.Mode()&os.ModeSymlink == 0 && !leafInfo.Mode().IsRegular() {
		return nil, "", fmt.Errorf("Product authority locator leaf must be a regular file or NixOS environment symlink")
	}
	if leafInfo.Mode()&os.ModeSymlink != 0 {
		if err := validateRootOwned(leafInfo, "Product authority locator leaf"); err != nil {
			return nil, "", err
		}
	} else if err := validateRootOwnedNonWritable(leafInfo, "Product authority locator leaf"); err != nil {
		return nil, "", err
	}

	manifestFD, err := syscall.Openat(parent, components[len(components)-1], syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", fmt.Errorf("open Product authority manifest: %w", err)
	}
	manifestFile := os.NewFile(uintptr(manifestFD), canonicalAuthorityLocator)
	info, err := manifestFile.Stat()
	if err != nil {
		_ = manifestFile.Close()
		return nil, "", fmt.Errorf("inspect opened Product authority manifest: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = manifestFile.Close()
		return nil, "", fmt.Errorf("opened Product authority manifest is not a regular file")
	}
	if err := validateRootOwnedNonWritable(info, "opened Product authority manifest"); err != nil {
		_ = manifestFile.Close()
		return nil, "", err
	}
	resolved, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", manifestFD))
	if err != nil {
		_ = manifestFile.Close()
		return nil, "", fmt.Errorf("resolve opened Product authority manifest: %w", err)
	}
	resolved = filepath.Clean(resolved)
	if err := validateImmutableLeaf(resolved); err != nil {
		_ = manifestFile.Close()
		return nil, "", err
	}
	return manifestFile, resolved, nil
}

func validateProtectedDirectoryFD(fd int, path string) error {
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("inspect Product authority locator parent %s: %w", path, err)
	}
	if stat.Mode&syscall.S_IFMT != syscall.S_IFDIR || stat.Uid != 0 || stat.Mode&0o022 != 0 {
		return fmt.Errorf("Product authority locator parent %s must be a root-owned non-writable directory", path)
	}
	return nil
}

func validateRootOwnedNonWritable(info os.FileInfo, label string) error {
	if err := validateRootOwned(info, label); err != nil {
		return err
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is group/world writable", label)
	}
	return nil
}

func validateRootOwned(info os.FileInfo, label string) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return fmt.Errorf("%s is not root-owned", label)
	}
	return nil
}

func validateImmutableLeaf(path string) error {
	clean := filepath.Clean(path)
	if !strings.HasPrefix(clean, "/nix/store/") {
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

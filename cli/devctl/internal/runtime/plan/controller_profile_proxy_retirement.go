package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Devkit admits the declared file identity, not a predecessor or an effect.
// The receiving Fleet package validates the bounded record and live process.
func validateControllerProductGUIProxyRetirementPolicy(ref *ControllerProfileFileIdentity) error {
	if ref == nil {
		return nil
	}
	const label = "Product GUI proxy retirement policy"
	path := ref.Path
	if path != strings.TrimSpace(path) || path != filepath.Clean(path) ||
		!filepath.IsAbs(path) || filepath.Dir(path) != filepath.Clean(ControllerProfileStoreRoot) ||
		!validControllerSHA256(ref.SHA256) {
		return fmt.Errorf("Management controller %s projection is not canonical", label)
	}
	if err := validateControllerStoreFile(label, path); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return fmt.Errorf("Management controller %s projection resolves through an alias", label)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm()&0o222 != 0 {
		return fmt.Errorf("Management controller %s projection is unavailable or writable", label)
	}
	actual, err := hashControllerFile(path)
	if err != nil {
		return fmt.Errorf("validate %s: %w", label, err)
	}
	if actual != ref.SHA256 {
		return fmt.Errorf("%s SHA-256 = %q, want %q", label, actual, ref.SHA256)
	}
	return nil
}

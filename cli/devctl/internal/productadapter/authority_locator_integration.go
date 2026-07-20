//go:build devkitintegration

package productadapter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var testAuthorityLocator string

func authorityLocator() string {
	return strings.TrimSpace(testAuthorityLocator)
}

func openAuthorityManifest(locator string) (*os.File, string, error) {
	clean := filepath.Clean(strings.TrimSpace(locator))
	if !filepath.IsAbs(clean) {
		return nil, "", fmt.Errorf("Product authority fixture locator must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return nil, "", fmt.Errorf("resolve Product authority fixture locator: %w", err)
	}
	if resolved != clean {
		return nil, "", fmt.Errorf("Product authority fixture locator must not traverse symlinks")
	}
	if err := validateImmutableLeaf(clean); err != nil {
		return nil, "", err
	}
	file, err := os.Open(clean)
	if err != nil {
		return nil, "", err
	}
	return file, clean, nil
}

func validateImmutableLeaf(path string) error {
	clean := filepath.Clean(strings.TrimSpace(path))
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return err
	}
	if resolved != clean {
		return fmt.Errorf("Product authority fixture artifact must not traverse symlinks")
	}
	return nil
}

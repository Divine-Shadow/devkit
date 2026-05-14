package devkitpaths

import (
	"os"
	"path/filepath"
	"strings"
)

type Paths struct {
	Root         string
	Kit          string
	OverlayPaths []string
}

func DetectPathsFromExe(exePath string) (Paths, error) {
	root := os.Getenv("DEVKIT_ROOT")
	if root == "" {
		// Binary is expected under devkit/kit/bin/devctl
		binDir := filepath.Dir(exePath)
		root = filepath.Clean(filepath.Join(binDir, "..", ".."))
	}
	root = filepath.Clean(root)
	kit := filepath.Join(root, "kit")
	overlayOverride := strings.TrimSpace(os.Getenv("DEVKIT_OVERLAYS_DIR"))
	var overlays []string
	if overlayOverride != "" {
		overlays = append(overlays, splitOverlayPaths(root, overlayOverride)...)
	} else {
		overlays = append(overlays, filepath.Join(root, "overlays"))
	}
	return Paths{Root: root, Kit: kit, OverlayPaths: uniquePaths(overlays)}, nil
}

func splitOverlayPaths(root, override string) []string {
	parts := strings.Split(override, string(os.PathListSeparator))
	out := make([]string, 0, len(parts))
	for _, raw := range parts {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if !filepath.IsAbs(v) {
			v = filepath.Join(root, v)
		}
		out = append(out, filepath.Clean(v))
	}
	return out
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		c := filepath.Clean(p)
		if !seen[c] {
			seen[c] = true
			result = append(result, c)
		}
	}
	return result
}

// FindOverlayDir returns the first directory containing the given overlay project.
func FindOverlayDir(paths []string, project string) string {
	if strings.TrimSpace(project) == "" {
		return ""
	}
	for _, root := range paths {
		candidate := filepath.Join(root, project)
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
	}
	return ""
}

// MergeOverlayPaths appends extra overlay search roots, preserving order and removing duplicates.
func MergeOverlayPaths(base []string, extra ...string) []string {
	combined := append([]string{}, base...)
	combined = append(combined, extra...)
	return uniquePaths(combined)
}

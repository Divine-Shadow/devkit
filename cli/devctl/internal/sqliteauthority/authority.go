package sqliteauthority

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// packageExecutable is injected by the immutable Devkit package build. It has
// no source default: destructive lifecycle must never discover an ambient
// sqlite3 executable from PATH.
var packageExecutable string

// Package returns the exact package-owned sqlite3 executable.
func Package() (string, error) {
	executable := strings.TrimSpace(packageExecutable)
	if executable == "" {
		return "", fmt.Errorf("package-owned sqlite3 executable is not bound")
	}
	if !filepath.IsAbs(executable) {
		return "", fmt.Errorf("package-owned sqlite3 executable must be absolute: %s", executable)
	}
	executable = filepath.Clean(executable)
	info, err := os.Stat(executable)
	if err != nil {
		return "", fmt.Errorf("validate package-owned sqlite3 executable %s: %w", executable, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("package-owned sqlite3 executable is not executable: %s", executable)
	}
	return executable, nil
}

// SetExecutableForTesting is unavailable to production binaries.
func SetExecutableForTesting(executable string) func() {
	if !strings.HasSuffix(filepath.Base(os.Args[0]), ".test") {
		panic("sqlite3 authority test override is available only to Go test binaries")
	}
	previous := packageExecutable
	packageExecutable = executable
	return func() { packageExecutable = previous }
}

package gitauthority

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// packageExecutable is replaced at link time in the production Devctl.
var packageExecutable = "git"

// Executable returns the single compiled Git executable authority. Source
// tests use the conventional name; installed production commands call
// RequirePackage before effects and therefore admit only an absolute package
// path.
func Executable() string {
	executable := strings.TrimSpace(packageExecutable)
	if executable == "" {
		return "git"
	}
	return executable
}

// RequirePackage proves the compiled production authority before an installed
// native lifecycle begins effects.
func RequirePackage() (string, error) {
	executable := Executable()
	if !filepath.IsAbs(executable) {
		return "", fmt.Errorf("native source acquisition requires a package-owned absolute Git executable")
	}
	info, err := os.Stat(executable)
	if err != nil {
		return "", fmt.Errorf("validate package-owned Git executable %s: %w", executable, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("package-owned Git executable is not executable: %s", executable)
	}
	return executable, nil
}

// SetExecutableForTesting is unavailable to production binaries. It lets
// cross-package integration tests exercise hostile PATH with the same absolute
// executable contract that the Nix linker supplies.
func SetExecutableForTesting(executable string) func() {
	if !strings.HasSuffix(filepath.Base(os.Args[0]), ".test") {
		panic("Git authority test override is available only to Go test binaries")
	}
	previous := packageExecutable
	packageExecutable = executable
	return func() { packageExecutable = previous }
}

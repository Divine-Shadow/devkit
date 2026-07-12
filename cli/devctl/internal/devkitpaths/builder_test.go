package devkitpaths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectPathsFromExeKeepsRuntimeAuthorityOnExecutableRoot(t *testing.T) {
	trustedRoot := filepath.Join(t.TempDir(), "trusted-devkit")
	hostileRoot := filepath.Join(t.TempDir(), "hostile-devkit")
	executable := filepath.Join(trustedRoot, "kit", "bin", "devctl")
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("test binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVKIT_ROOT", hostileRoot)

	paths, err := DetectPathsFromExe(executable)
	if err != nil {
		t.Fatalf("DetectPathsFromExe: %v", err)
	}
	if paths.Root != hostileRoot {
		t.Fatalf("config root = %q, want explicit DEVKIT_ROOT %q", paths.Root, hostileRoot)
	}
	if paths.RuntimeAuthorityRoot != trustedRoot {
		t.Fatalf("runtime authority root = %q, want executable-derived %q", paths.RuntimeAuthorityRoot, trustedRoot)
	}
	if paths.Kit != filepath.Join(hostileRoot, "kit") {
		t.Fatalf("kit root = %q", paths.Kit)
	}
}

func TestDetectPathsFromExeRejectsUnresolvableExecutable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", "kit", "bin", "devctl")
	if _, err := DetectPathsFromExe(missing); err == nil {
		t.Fatal("missing executable unexpectedly produced runtime authority")
	}
}

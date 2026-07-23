package testsupport

import (
	"os"
	"path/filepath"
	"testing"
)

// UnixSocketDir returns a deliberately short disposable directory for tests
// that bind AF_UNIX sockets. Go's testing.TempDir includes the full test name;
// under Nix shells that can exceed sockaddr_un.sun_path before the production
// code is exercised.
func UnixSocketDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "dks-")
	if err != nil {
		t.Fatalf("create short Unix-socket test directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove short Unix-socket test directory: %v", err)
		}
	})
	return dir
}

// UnixSocketPath returns a short socket path and rejects test names that would
// approach the smallest supported Unix sockaddr path limit.
func UnixSocketPath(t testing.TB, name string) string {
	t.Helper()
	path := filepath.Join(UnixSocketDir(t), name)
	if len(path) >= 100 {
		t.Fatalf("Unix-socket test path is too long: %s", path)
	}
	return path
}

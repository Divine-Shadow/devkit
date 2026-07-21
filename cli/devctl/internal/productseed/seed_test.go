package productseed

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadProtectedIdentityRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "identity")
	writeProtectedTestIdentity(t, target, []byte("private-key"))
	link := filepath.Join(directory, "identity-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if file, _, err := readProtectedIdentity(link, testIdentityOwner(t, target)); err == nil {
		file.Close()
		t.Fatal("symlink source identity was accepted")
	}
}

func TestReadProtectedIdentityHoldsOpenedGenerationAcrossPathSwap(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "identity")
	original := []byte("original-private-key")
	writeProtectedTestIdentity(t, path, original)
	owner := testIdentityOwner(t, path)
	file, payload, err := readProtectedIdentity(path, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if string(payload) != string(original) {
		t.Fatalf("unexpected opened identity %q", payload)
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	writeProtectedTestIdentity(t, path, []byte("replacement-private-key"))
	if os.Geteuid() == 0 {
		if err := os.Chown(path, owner, -1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	held, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(held) != string(original) {
		t.Fatalf("held descriptor followed path replacement: %q", held)
	}
}

func TestValidateProtectedIdentityStatRejectsWrongOwnerAndMode(t *testing.T) {
	valid := syscall.Stat_t{Mode: syscall.S_IFREG | 0o600, Uid: 1000}
	if err := validateProtectedIdentityStat(valid, 1000); err != nil {
		t.Fatal(err)
	}
	wrongOwner := valid
	wrongOwner.Uid = 1001
	if err := validateProtectedIdentityStat(wrongOwner, 1000); err == nil {
		t.Fatal("wrong owner was accepted")
	}
	wrongMode := valid
	wrongMode.Mode = syscall.S_IFREG | 0o640
	if err := validateProtectedIdentityStat(wrongMode, 1000); err == nil {
		t.Fatal("wrong mode was accepted")
	}
}

func writeProtectedTestIdentity(t *testing.T, path string, payload []byte) {
	t.Helper()
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

func testIdentityOwner(t *testing.T, path string) int {
	t.Helper()
	owner := os.Geteuid()
	if owner == 0 {
		owner = 1000
		if err := os.Chown(path, owner, -1); err != nil {
			t.Fatal(err)
		}
	}
	return owner
}

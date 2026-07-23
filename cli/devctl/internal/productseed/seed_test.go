package productseed

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

type pathSnapshot struct {
	mode os.FileMode
	uid  uint32
	gid  uint32
	data []byte
}

func snapshotPath(t *testing.T, path string) pathSnapshot {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	result := pathSnapshot{mode: info.Mode(), uid: stat.Uid, gid: stat.Gid}
	if info.Mode().IsRegular() {
		result.data, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	} else if info.IsDir() {
		var tree bytes.Buffer
		err = filepath.WalkDir(path, func(candidate string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, relErr := filepath.Rel(path, candidate)
			if relErr != nil {
				return relErr
			}
			entryInfo, infoErr := os.Lstat(candidate)
			if infoErr != nil {
				return infoErr
			}
			entryStat := entryInfo.Sys().(*syscall.Stat_t)
			_, _ = fmt.Fprintf(&tree, "%q %s %d %d\n", relative, entryInfo.Mode(), entryStat.Uid, entryStat.Gid)
			if entryInfo.Mode().IsRegular() {
				payload, readErr := os.ReadFile(candidate)
				if readErr != nil {
					return readErr
				}
				tree.Write(payload)
				tree.WriteByte('\n')
			} else if entryInfo.Mode()&os.ModeSymlink != 0 {
				target, linkErr := os.Readlink(candidate)
				if linkErr != nil {
					return linkErr
				}
				tree.WriteString(target)
				tree.WriteByte('\n')
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		result.data = tree.Bytes()
	}
	return result
}

func assertPathSnapshot(t *testing.T, path string, before pathSnapshot) {
	t.Helper()
	after := snapshotPath(t, path)
	if before.mode != after.mode || before.uid != after.uid || before.gid != after.gid || !bytes.Equal(before.data, after.data) {
		t.Fatalf("outside path changed: before=%+v after=%+v", before, after)
	}
}

func openTestProjection(t *testing.T, path string) *os.File {
	t.Helper()
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), path)
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func TestOpenProjectionRootRejectsSymlinkWithoutOutsideEffect(t *testing.T) {
	parent := t.TempDir()
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	before := snapshotPath(t, outside)
	if err := os.Symlink(outside, filepath.Join(parent, "projection")); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(parent); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)
	if root, err := openProjectionRoot("projection"); err == nil {
		root.Close()
		t.Fatal("symlink projection root was accepted")
	}
	assertPathSnapshot(t, outside, before)
}

func TestWriteOwnedAtRejectsEverySymlinkAncestorWithoutOutsideEffect(t *testing.T) {
	for _, ancestor := range []string{"home", "home/.ssh", "home/.codex"} {
		t.Run(ancestor, func(t *testing.T) {
			root := t.TempDir()
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.Mkdir(outside, 0o755); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(outside, "sentinel")
			if err := os.WriteFile(sentinel, []byte("outside"), 0o640); err != nil {
				t.Fatal(err)
			}
			beforeDirectory := snapshotPath(t, outside)
			beforeSentinel := snapshotPath(t, sentinel)
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, ancestor)), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(root, ancestor)); err != nil {
				t.Fatal(err)
			}
			target := "home/.ssh/id_ed25519"
			if ancestor == "home/.codex" {
				target = "home/.codex/auth.json"
			}
			if err := writeOwnedAt(openTestProjection(t, root), target, []byte("seed"), 0o600, os.Geteuid(), os.Getegid()); err == nil {
				t.Fatal("symlink ancestor was accepted")
			}
			assertPathSnapshot(t, outside, beforeDirectory)
			assertPathSnapshot(t, sentinel, beforeSentinel)
		})
	}
}

func TestWriteOwnedAtRejectsEverySeedLeafSymlinkWithoutOutsideEffect(t *testing.T) {
	for _, leaf := range []string{
		"home/.ssh/id_ed25519",
		"home/.ssh/id_ed25519.pub",
		"home/.ssh/known_hosts",
		"home/.ssh/config",
		"home/.codex/auth.json",
		".devkit-product-offline-seed.json",
	} {
		t.Run(leaf, func(t *testing.T) {
			root := t.TempDir()
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("outside"), 0o640); err != nil {
				t.Fatal(err)
			}
			before := snapshotPath(t, outside)
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, leaf)), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(root, leaf)); err != nil {
				t.Fatal(err)
			}
			if err := writeOwnedAt(openTestProjection(t, root), leaf, []byte("seed"), 0o600, os.Geteuid(), os.Getegid()); err == nil {
				t.Fatal("symlink leaf was accepted")
			}
			assertPathSnapshot(t, outside, before)
		})
	}
}

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

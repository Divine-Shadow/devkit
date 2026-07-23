package productseed

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func prepareTestAuthParent(t *testing.T, rootPath string) string {
	t.Helper()
	parent := filepath.Join(rootPath, "home", ".codex")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(rootPath, "home"), parent} {
		if err := os.Chmod(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return parent
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

func TestReadCodexAuthAcceptsOneBoundedJSONDocumentWithoutNormalization(t *testing.T) {
	input := []byte("{\"tokens\":{\"access_token\":\"secret\"}}\n")
	payload, err := readCodexAuth(bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, input) {
		t.Fatalf("Codex auth bytes were normalized: %q", payload)
	}
	wipe(payload)
	if !bytes.Equal(payload, make([]byte, len(payload))) {
		t.Fatal("Codex auth payload was not wiped")
	}
	for _, invalid := range [][]byte{
		nil,
		[]byte(" \n"),
		[]byte("not-json"),
		bytes.Repeat([]byte("x"), maxCodexAuthBytes+1),
	} {
		if _, err := readCodexAuth(bytes.NewReader(invalid)); err == nil {
			t.Fatalf("invalid Codex auth payload was accepted: %d bytes", len(invalid))
		}
	}
}

func TestWriteOwnedAtomicAtIsCreateOnlyExactAndResidueFree(t *testing.T) {
	rootPath := t.TempDir()
	root := openTestProjection(t, rootPath)
	parent := prepareTestAuthParent(t, rootPath)
	uid, gid := os.Geteuid(), os.Getegid()
	first := []byte("{\"fixture\":1}\n")
	second := []byte("{\"fixture\":2}\n")
	target := "home/.codex/auth.json"
	if err := writeOwnedAtomicAt(root, target, first, 0o600, uid, gid); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(rootPath, target)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 ||
		int(stat.Uid) != uid || int(stat.Gid) != gid {
		t.Fatalf("unexpected installed identity: mode=%s uid=%d gid=%d", info.Mode(), stat.Uid, stat.Gid)
	}
	if err := writeOwnedAtomicAt(root, target, second, 0o600, uid, gid); err == nil {
		t.Fatal("duplicate Codex auth seed replaced the accepted target")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, first) {
		t.Fatalf("duplicate seed changed target: %q", payload)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "auth.json" {
		t.Fatalf("temporary seed residue remained: %+v", entries)
	}
}

func TestWriteOwnedAtomicAtRejectsSymlinkAndWrongDirectoryIdentity(t *testing.T) {
	uid, gid := os.Geteuid(), os.Getegid()
	t.Run("symlink-leaf", func(t *testing.T) {
		rootPath := t.TempDir()
		root := openTestProjection(t, rootPath)
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		before := snapshotPath(t, outside)
		parent := filepath.Join(rootPath, "home", ".codex")
		if err := os.MkdirAll(parent, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(parent, "auth.json")); err != nil {
			t.Fatal(err)
		}
		if err := writeOwnedAtomicAt(root, "home/.codex/auth.json", []byte("{}\n"), 0o600, uid, gid); err == nil {
			t.Fatal("symlink target was accepted")
		}
		assertPathSnapshot(t, outside, before)
		entries, err := os.ReadDir(parent)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != "auth.json" {
			t.Fatalf("temporary residue remained after symlink rejection: %+v", entries)
		}
	})
	t.Run("wrong-parent-mode", func(t *testing.T) {
		rootPath := t.TempDir()
		root := openTestProjection(t, rootPath)
		parent := filepath.Join(rootPath, "home")
		if err := os.Mkdir(parent, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(parent, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeOwnedAtomicAt(root, "home/.codex/auth.json", []byte("{}\n"), 0o600, uid, gid); err == nil {
			t.Fatal("wrong parent mode was repaired instead of rejected")
		}
		if _, err := os.Lstat(filepath.Join(parent, ".codex")); !os.IsNotExist(err) {
			t.Fatalf("wrong parent admission left effects: %v", err)
		}
	})
}

func TestWriteOwnedAtomicAtConcurrentSeedHasOneWinner(t *testing.T) {
	rootPath := t.TempDir()
	uid, gid := os.Geteuid(), os.Getegid()
	prepareTestAuthParent(t, rootPath)
	const writers = 8
	results := make(chan error, writers)
	var start sync.WaitGroup
	start.Add(1)
	for index := 0; index < writers; index++ {
		go func(index int) {
			rootFD, err := syscall.Open(
				rootPath,
				syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,
				0,
			)
			if err != nil {
				results <- err
				return
			}
			root := os.NewFile(uintptr(rootFD), rootPath)
			defer root.Close()
			start.Wait()
			results <- writeOwnedAtomicAt(
				root,
				"home/.codex/auth.json",
				[]byte(fmt.Sprintf("{\"writer\":%d}\n", index)),
				0o600,
				uid,
				gid,
			)
		}(index)
	}
	start.Done()
	successes := 0
	for index := 0; index < writers; index++ {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent create-only seed successes = %d, want 1", successes)
	}
	entries, err := os.ReadDir(filepath.Join(rootPath, "home", ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "auth.json" ||
		strings.Contains(entries[0].Name(), ".seed-") {
		t.Fatalf("concurrent seed residue remained: %+v", entries)
	}
}

func TestWriteOwnedAtomicAtKeepsGenerationAnonymousUntilExactInstall(t *testing.T) {
	rootPath := t.TempDir()
	parent := prepareTestAuthParent(t, rootPath)
	root := openTestProjection(t, rootPath)
	uid, gid := os.Geteuid(), os.Getegid()
	payload := []byte("{\"fixture\":\"anonymous\"}\n")
	observedBeforeLink := false
	err := writeOwnedAtomicAtWithHooks(
		root,
		"home/.codex/auth.json",
		payload,
		0o600,
		uid,
		gid,
		atomicWriteHooks{
			beforeLink: func(parentFD, anonymousFD int, leaf string) error {
				observedBeforeLink = true
				entries, err := os.ReadDir(fmt.Sprintf("/proc/self/fd/%d", parentFD))
				if err != nil {
					return err
				}
				if len(entries) != 0 {
					return fmt.Errorf("anonymous generation was discoverable before link: %+v", entries)
				}
				if info, err := os.Stat(fmt.Sprintf("/proc/self/fd/%d", anonymousFD)); err != nil ||
					!info.Mode().IsRegular() {
					return fmt.Errorf("anonymous generation descriptor was not held: %v", err)
				}
				var stat syscall.Stat_t
				if err := syscall.Fstat(anonymousFD, &stat); err != nil {
					return err
				}
				if stat.Mode&syscall.S_IFMT != syscall.S_IFREG ||
					stat.Mode&0o777 != 0o600 ||
					int(stat.Uid) != uid ||
					int(stat.Gid) != gid ||
					stat.Size != int64(len(payload)) {
					return fmt.Errorf(
						"anonymous generation lacked final identity before link: mode=%o uid=%d gid=%d size=%d",
						stat.Mode&0o777,
						stat.Uid,
						stat.Gid,
						stat.Size,
					)
				}
				return nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !observedBeforeLink {
		t.Fatal("before-link sabotage was not exercised")
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "auth.json" {
		t.Fatalf("unexpected final generation entries: %+v", entries)
	}
	actual, err := os.ReadFile(filepath.Join(parent, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, payload) {
		t.Fatalf("installed bytes changed: %q", actual)
	}
}

func TestWriteOwnedAtomicAtClassifiesAttemptedEffectAndAmbiguousFailures(t *testing.T) {
	uid, gid := os.Geteuid(), os.Getegid()
	payload := []byte("{\"fixture\":\"classification\"}\n")
	assertFailure := func(
		t *testing.T,
		hooks atomicWriteHooks,
		wantOutcome CodexAuthInstallOutcome,
		wantReconciliation string,
	) string {
		t.Helper()
		rootPath := t.TempDir()
		parent := prepareTestAuthParent(t, rootPath)
		root := openTestProjection(t, rootPath)
		err := writeOwnedAtomicAtWithHooks(
			root,
			"home/.codex/auth.json",
			payload,
			0o600,
			uid,
			gid,
			hooks,
		)
		var failure *CodexAuthInstallFailure
		if !errors.As(err, &failure) {
			t.Fatalf("failure was not typed: %v", err)
		}
		if failure.SchemaVersion != CodexAuthSeedFailureSchema ||
			failure.Status != "failed" ||
			failure.Outcome != wantOutcome ||
			failure.Reconciliation != wantReconciliation {
			t.Fatalf("unexpected typed failure: %+v", failure)
		}
		entries, readErr := os.ReadDir(parent)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, entry := range entries {
			if strings.Contains(entry.Name(), ".seed-") {
				t.Fatalf("named secret generation residue remained: %+v", entries)
			}
		}
		return filepath.Join(parent, "auth.json")
	}

	t.Run("attempted-before-link", func(t *testing.T) {
		target := assertFailure(
			t,
			atomicWriteHooks{
				beforeLink: func(parentFD, anonymousFD int, leaf string) error {
					return fmt.Errorf("injected pre-link failure")
				},
			},
			CodexAuthInstallAttempted,
			"target-not-created",
		)
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			t.Fatalf("attempted failure left final effect: %v", err)
		}
	})

	t.Run("effect-after-link", func(t *testing.T) {
		target := assertFailure(
			t,
			atomicWriteHooks{
				beforeParentSync: func(parentFD, anonymousFD int, leaf string) error {
					return fmt.Errorf("injected post-link durability failure")
				},
			},
			CodexAuthInstallEffect,
			"exact-target-installed",
		)
		actual, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(actual, payload) {
			t.Fatalf("typed effect did not preserve exact bytes: %q", actual)
		}
	})

	t.Run("ambiguous-in-place-mutation", func(t *testing.T) {
		target := assertFailure(
			t,
			atomicWriteHooks{
				afterLink: func(parentFD, anonymousFD int, leaf string) error {
					fd, err := syscall.Openat(
						parentFD,
						leaf,
						syscall.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,
						0,
					)
					if err != nil {
						return err
					}
					file := os.NewFile(uintptr(fd), leaf)
					if file == nil {
						_ = syscall.Close(fd)
						return fmt.Errorf("hold sabotage target")
					}
					defer file.Close()
					replacement := bytes.Repeat([]byte("x"), len(payload))
					if _, err := file.WriteAt(replacement, 0); err != nil {
						return err
					}
					return file.Sync()
				},
			},
			CodexAuthInstallAmbiguous,
			"installed-bytes-changed",
		)
		actual, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(actual, payload) {
			t.Fatal("in-place sabotage was not applied")
		}
	})

	t.Run("ambiguous-immediate-generation-swap", func(t *testing.T) {
		target := assertFailure(
			t,
			atomicWriteHooks{
				afterLink: func(parentFD, anonymousFD int, leaf string) error {
					if err := syscall.Unlinkat(parentFD, leaf); err != nil {
						return err
					}
					fd, err := syscall.Openat(
						parentFD,
						leaf,
						syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|
							syscall.O_NOFOLLOW|syscall.O_CLOEXEC,
						0o600,
					)
					if err != nil {
						return err
					}
					file := os.NewFile(uintptr(fd), leaf)
					if file == nil {
						_ = syscall.Close(fd)
						return fmt.Errorf("hold replacement generation")
					}
					defer file.Close()
					if err := writeAll(file, payload); err != nil {
						return err
					}
					return file.Sync()
				},
			},
			CodexAuthInstallAmbiguous,
			"target-generation-changed",
		)
		actual, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(actual, payload) {
			t.Fatalf("replacement sabotage bytes changed unexpectedly: %q", actual)
		}
	})
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

package nativecmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

type procfsCustodyFixture struct {
	selected      nativeSlotProcessIdentity
	subject, peer string
	attachment    nativeProcAttachment
	mount         nativeProcMount
	beforeWalk    func()
}

func newProcfsCustodyFixture(t *testing.T) *procfsCustodyFixture {
	t.Helper()
	physicalProcRootFixture(t)
	f := &procfsCustodyFixture{selected: physicalSlotFixture(t, 1)}
	proc, err := pinNativeProcObject("/proc")
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Close()
	f.mount, err = nativePinnedProcMount(proc)
	if err != nil || f.mount.fsType != nativeProcSuperMagic {
		t.Fatalf("actual procfs mount: %v %v", f.mount, err)
	}
	f.attachment, err = nativeProcMountAttachment("/proc/self", f.mount.id)
	if err != nil {
		t.Fatal(err)
	}
	// Real procfs pins exercise fstatfs, fdinfo mount identity and attachment
	// ancestry. Only the failing directory-open syscall is injected.
	f.subject = writePhysicalProc(t, 80001, 1, "/proc/self/fdinfo", map[string]string{}, "/proc/self/fdinfo")
	f.peer = writePhysicalProc(t, 80002, 1, "/", map[string]string{}, t.TempDir())
	for _, path := range []string{f.subject, f.peer} {
		if err := os.Mkdir(filepath.Join(path, "ns"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/proc/self/ns/mnt", filepath.Join(path, "ns", "mnt")); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(f.peer, "mountinfo"), []byte(f.attachment.row+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	saved := nativeSlotOpenProcessDirectory
	t.Cleanup(func() { nativeSlotOpenProcessDirectory = saved })
	nativeSlotOpenProcessDirectory = func(path string) (*os.File, error) {
		if path == filepath.Join(f.subject, "cwd") || path == filepath.Join(f.subject, "root") {
			return nil, syscall.ESRCH
		}
		if strings.HasPrefix(path, "/proc/self/fd/") && f.beforeWalk != nil {
			action := f.beforeWalk
			f.beforeWalk = nil
			action()
		}
		return saved(path)
	}
	return f
}

func (f *procfsCustodyFixture) observe(t *testing.T, link string) (bool, error) {
	t.Helper()
	targets, err := nativeSlotPhysicalTargets(f.selected)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(nativeSlotProcRoot)
	if err != nil {
		t.Fatal(err)
	}
	return nativeProcLinkTouchesInInventory(f.subject, link, f.selected, targets, entries)
}

func replaceProcProofLink(t *testing.T, path, target string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func TestNativeProcfsCustodyExactAttachmentExcludesOnlyProvenDirectory(t *testing.T) {
	for _, foreignFD := range []bool{false, true} {
		t.Run(fmt.Sprintf("foreign-fd-%t", foreignFD), func(t *testing.T) {
			f := newProcfsCustodyFixture(t)
			for _, link := range []string{"cwd", "root"} {
				if touches, err := f.observe(t, link); touches || err != nil {
					t.Fatalf("proven %s touch=%v error=%v", link, touches, err)
				}
			}
			if foreignFD {
				file := filepath.Join(f.selected.hostWorktree, "foreign.txt")
				if err := os.WriteFile(file, []byte("selected source"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(file, filepath.Join(f.subject, "fd", "3")); err != nil {
					t.Fatal(err)
				}
			}
			plan, err := planNativeSlotProcesses(f.selected, true)
			if foreignFD {
				if err == nil || plan != nil || !strings.Contains(err.Error(), "fd/3") {
					t.Fatalf("remaining descriptor plan=%v error=%v", plan, err)
				}
			} else if err != nil || len(plan.pids) != 0 {
				t.Fatalf("proven procfs plan=%v error=%v", plan, err)
			}
		})
	}
}

func TestNativeProcfsCustodyUnavailableOrAmbiguousProofRefuses(t *testing.T) {
	for _, kind := range []string{"no-peer", "wrong-namespace", "wrong-mount", "wrong-attachment-mountpoint", "subtree-attachment", "non-proc-attachment", "unreadable-attachment", "duplicate-attachment", "wrong-peer-root", "selected-proc-device", "selected-attachment-ancestor"} {
		t.Run(kind, func(t *testing.T) {
			f := newProcfsCustodyFixture(t)
			writeMount := func(row string) {
				if err := os.WriteFile(filepath.Join(f.peer, "mountinfo"), []byte(row+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "no-peer":
				if err := os.RemoveAll(f.peer); err != nil {
					t.Fatal(err)
				}
			case "wrong-namespace":
				replaceProcProofLink(t, filepath.Join(f.peer, "ns", "mnt"), t.TempDir())
			case "wrong-mount":
				fields := strings.Fields(f.attachment.row)
				fields[0] = strconv.FormatUint(f.mount.id+100000, 10)
				writeMount(strings.Join(fields, " "))
			case "subtree-attachment":
				fields := strings.Fields(f.attachment.row)
				fields[3] = "/subtree"
				writeMount(strings.Join(fields, " "))
			case "wrong-attachment-mountpoint":
				fields := strings.Fields(f.attachment.row)
				fields[4] = "/tmp"
				writeMount(strings.Join(fields, " "))
			case "non-proc-attachment":
				writeMount(strings.Replace(f.attachment.row, " - proc ", " - tmpfs ", 1))
			case "unreadable-attachment":
				if err := os.Remove(filepath.Join(f.peer, "mountinfo")); err != nil {
					t.Fatal(err)
				}
			case "duplicate-attachment":
				writeMount(f.attachment.row + "\n" + f.attachment.row)
			case "wrong-peer-root":
				replaceProcProofLink(t, filepath.Join(f.peer, "root"), t.TempDir())
			case "selected-proc-device", "selected-attachment-ancestor":
				alias := filepath.Join(t.TempDir(), "selected-alias")
				target := "/proc/sys"
				if kind == "selected-attachment-ancestor" {
					target = "/"
				}
				if err := os.Symlink(target, alias); err != nil {
					t.Fatal(err)
				}
				f.selected.hostWorktree = alias
			}
			if touches, err := f.observe(t, "cwd"); touches || err == nil {
				t.Fatalf("%s touch=%v error=%v", kind, touches, err)
			}
		})
	}
}

func TestNativeProcfsCustodyObservationDriftRefuses(t *testing.T) {
	for _, kind := range []string{"subject-start", "peer-start", "subject-root", "cwd", "namespace", "peer-root", "mount-row", "boundary"} {
		t.Run(kind, func(t *testing.T) {
			f := newProcfsCustodyFixture(t)
			f.beforeWalk = func() {
				switch kind {
				case "subject-start", "peer-start":
					path := f.subject
					if kind == "peer-start" {
						path = f.peer
					}
					stat := filepath.Join(path, "stat")
					data, err := os.ReadFile(stat)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(stat, []byte(strings.Replace(string(data), "18000", "28000", 1)), 0600); err != nil {
						t.Fatal(err)
					}
				case "subject-root":
					replaceProcProofLink(t, filepath.Join(f.subject, "root"), t.TempDir())
				case "cwd":
					replaceProcProofLink(t, filepath.Join(f.subject, "cwd"), t.TempDir())
				case "namespace":
					replaceProcProofLink(t, filepath.Join(f.subject, "ns", "mnt"), t.TempDir())
				case "peer-root":
					replaceProcProofLink(t, filepath.Join(f.peer, "root"), t.TempDir())
				case "mount-row":
					if err := os.WriteFile(filepath.Join(f.peer, "mountinfo"), []byte(f.attachment.row+" changed\n"), 0600); err != nil {
						t.Fatal(err)
					}
				case "boundary":
					// Move only the fixture's selected state directory while its
					// original inode stays pinned by the observation.
					if err := os.Rename(f.selected.stateRoot, f.selected.stateRoot+"-moved"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(f.selected.stateRoot, 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			if touches, err := f.observe(t, "cwd"); touches || err == nil {
				t.Fatalf("%s touch=%v error=%v", kind, touches, err)
			}
		})
	}
}

func TestNativeProcfsCustodyAbsentBoundariesRequireStableWholeSet(t *testing.T) {
	for _, kind := range []string{"all-absent", "creation-drift", "ancestor-replacement", "ancestor-type-change", "ancestor-removal", "permission", "lexical-absent", "invalid-worktree", "missing-home-declaration", "invalid-state"} {
		t.Run(kind, func(t *testing.T) {
			f := newProcfsCustodyFixture(t)
			for _, path := range []string{f.selected.hostHome, f.selected.hostWorktree, f.selected.stateRoot} {
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "creation-drift":
				f.beforeWalk = func() {
					if err := os.MkdirAll(f.selected.stateRoot, 0700); err != nil {
						t.Fatal(err)
					}
				}
			case "ancestor-replacement", "ancestor-type-change", "ancestor-removal", "permission":
				parent := filepath.Dir(f.selected.stateRoot)
				if kind == "permission" && os.Geteuid() == 0 {
					t.Skip("root bypasses directory permissions")
				}
				f.beforeWalk = func() {
					switch kind {
					case "ancestor-replacement":
						if err := os.Rename(parent, parent+"-moved"); err != nil {
							t.Fatal(err)
						}
						if err := os.Mkdir(parent, 0700); err != nil {
							t.Fatal(err)
						}
					case "ancestor-type-change":
						if err := os.Remove(parent); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(parent, nil, 0600); err != nil {
							t.Fatal(err)
						}
					case "ancestor-removal":
						if err := os.Remove(parent); err != nil {
							t.Fatal(err)
						}
					case "permission":
						if err := os.Chmod(parent, 0); err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { _ = os.Chmod(parent, 0700) })
					}
				}
			case "lexical-absent":
				replaceProcProofLink(t, filepath.Join(f.subject, "cwd"), f.selected.hostWorktree)
			case "invalid-worktree":
				f.selected.hostWorktree = "relative/worktree"
			case "missing-home-declaration":
				f.selected.hostHome = ""
			case "invalid-state":
				f.selected.stateRoot += "/unresolved/.."
			}
			touches, err := f.observe(t, "cwd")
			if touches || (err != nil) != (kind != "all-absent") {
				t.Fatalf("%s touch=%v error=%v", kind, touches, err)
			}
		})
	}
}

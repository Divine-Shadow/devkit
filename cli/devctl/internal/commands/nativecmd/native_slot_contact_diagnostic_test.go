package nativecmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func contactFixture(t *testing.T) (nativeSlotProcessIdentity, string) {
	t.Helper()
	id, proc, _ := custodyObservationFixture(t)
	if err := os.WriteFile(filepath.Join(proc, "environ"), []byte("PRIVATE=environment-private-sentinel\x00"), 0600); err != nil {
		t.Fatal(err)
	}
	refusalExecutableFixture(t, proc)
	return id, proc
}
func contactForFixture(t *testing.T, id nativeSlotProcessIdentity, proc string) nativeProcessContact {
	t.Helper()
	targets, err := nativeSlotPhysicalTargets(id)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(nativeSlotProcRoot)
	if err != nil {
		t.Fatal(err)
	}
	physical := &nativePhysicalContact{}
	defer physical.close()
	touches, kind, err := nativeProcTouchesTargetsObserved(proc, id, targets, entries, physical)
	if err != nil || !touches {
		t.Fatalf("missing physical touch: %v %v", touches, err)
	}
	parent, start := readNativeProcIdentity(proc)
	return observeNativeProcessContact(proc, parent, start, kind, id, targets, physical)
}
func TestNativeContactPhysicalClassesAndOverlaps(t *testing.T) {
	for _, kind := range []string{"worktree", "home", "state", "overlap", "deeper-alias"} {
		t.Run(kind, func(t *testing.T) {
			id, proc := contactFixture(t)
			target := id.hostWorktree
			want := []string{"worktree"}
			switch kind {
			case "home":
				target = id.hostHome
				want = []string{"home"}
			case "state":
				target = id.stateRoot
				want = []string{"state"}
			case "overlap":
				id.hostHome = id.hostWorktree
				id.stateRoot = id.hostWorktree
				want = []string{"worktree", "home", "state"}
			case "deeper-alias":
				deep := filepath.Join(id.hostWorktree, "deep", "directory")
				if err := os.MkdirAll(deep, 0700); err != nil {
					t.Fatal(err)
				}
				target = filepath.Join(t.TempDir(), "not-a-selected-spelling")
				if err := os.Symlink(deep, target); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Remove(filepath.Join(proc, "cwd")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(proc, "cwd")); err != nil {
				t.Fatal(err)
			}
			c := contactForFixture(t, id, proc)
			if c.Status != "stable" || !reflect.DeepEqual(c.RootClasses, want) || c.MatchedRoot == nil || c.TouchKind != "cwd" {
				t.Fatalf("bad contact: %+v", c)
			}
			if c.Executable.StoreIdentity != "unknown" || c.Executable.Object == nil {
				t.Fatalf("user executable was exported: %+v", c)
			}
			for _, n := range []string{"pid", "mnt", "user"} {
				if c.NamespaceComparison[n] != "same" {
					t.Fatalf("namespace: %+v", c)
				}
			}
		})
	}
}
func TestNativeContactIdentityNamespaceAndSelectedObjectDrift(t *testing.T) {
	for _, kind := range []string{"start", "parent", "comm", "namespace", "selected-root", "cwd", "exe", "unavailable-namespace"} {
		t.Run(kind, func(t *testing.T) {
			id, proc := contactFixture(t)
			if kind == "unavailable-namespace" {
				if err := os.Remove(filepath.Join(proc, "ns", "mnt")); err != nil {
					t.Fatal(err)
				}
			}
			saved := nativeContactBeforeRevalidate
			t.Cleanup(func() { nativeContactBeforeRevalidate = saved })
			nativeContactBeforeRevalidate = func(string) {
				switch kind {
				case "start", "parent", "comm":
					p := filepath.Join(proc, "stat")
					b, _ := os.ReadFile(p)
					old, new := "100801", "200801"
					if kind == "parent" {
						old, new = "S 71", "S 99"
					}
					if kind == "comm" {
						old, new = "physical-fixture", "invalid comm"
					}
					if err := os.WriteFile(p, []byte(strings.ReplaceAll(string(b), old, new)), 0600); err != nil {
						t.Fatal(err)
					}
				case "namespace":
					_ = os.Remove(filepath.Join(proc, "ns", "mnt"))
					_ = os.Symlink("mnt:[999]", filepath.Join(proc, "ns", "mnt"))
				case "selected-root":
					if err := os.Rename(id.hostWorktree, id.hostWorktree+"-held"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(id.hostWorktree, 0700); err != nil {
						t.Fatal(err)
					}
				case "cwd":
					_ = os.Remove(filepath.Join(proc, "cwd"))
					_ = os.Symlink(t.TempDir(), filepath.Join(proc, "cwd"))
				case "exe":
					_ = os.Remove(filepath.Join(proc, "exe"))
					refusalExecutableFixture(t, proc)
				}
			}
			c := contactForFixture(t, id, proc)
			want := "unstable"
			if kind == "unavailable-namespace" {
				want = "unavailable"
			}
			if c.Status != want || len(c.RootClasses) != 0 || c.MatchedRoot != nil || c.Executable.StoreIdentity != "unknown" {
				t.Fatalf("drift promoted: %+v", c)
			}
		})
	}
}
func TestNativeContactReadOnlyObserverIncludesUnmarkedContactAndPreservesPrivacy(t *testing.T) {
	id, proc := contactFixture(t)
	if err := os.WriteFile(filepath.Join(proc, "cmdline"), []byte("user-argv-private-sentinel\x00"), 0600); err != nil {
		t.Fatal(err)
	}
	r := observeNativeSlotCustody(custodySpec(id))
	if len(r.Observations) != 0 || len(r.PhysicalContacts) != 1 || r.PhysicalContacts[0].Status != "stable" || r.PhysicalContactsAreAbsenceProof {
		t.Fatalf("unmarked contact omitted: %+v", r)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-sentinel", "private-executable-sentinel", id.hostWorktree, id.hostHome, "cmdline", "PRIVATE"} {
		if strings.Contains(string(b), private) {
			t.Fatalf("private export %q: %s", private, b)
		}
	}
	if plan, err := planNativeSlotProcesses(id, false); err == nil || plan != nil {
		t.Fatalf("observed foreign process acquired custody: %v %v", plan, err)
	}
	// Absence is never established by an empty, capped, unavailable or ordinary report.
	if err := os.RemoveAll(proc); err != nil {
		t.Fatal(err)
	}
	if observeNativeSlotCustody(custodySpec(id)).PhysicalContactsAreAbsenceProof {
		t.Fatal("empty report became absence proof")
	}
}
func TestNativeContactObserverDoesNotRequireReadableEnvironment(t *testing.T) {
	id, proc := contactFixture(t)
	if err := os.Remove(filepath.Join(proc, "environ")); err != nil {
		t.Fatal(err)
	}
	r := observeNativeSlotCustody(custodySpec(id))
	if len(r.PhysicalContacts) != 1 || r.PhysicalContacts[0].Status != "stable" {
		t.Fatalf("environment hid physical contact: %+v", r)
	}
}
func TestNativeContactStoreGrammarRejectsPrivateAndMalformedPaths(t *testing.T) {
	prefix := "/nix/store/" + strings.Repeat("0", 32) + "-package-1.0/"
	for _, path := range []string{prefix + "bin/tool", prefix + "libexec/tool/helper"} {
		if !nativeSafeStoreExecutable(path) {
			t.Fatalf("valid store executable rejected: %s", path)
		}
	}
	for _, path := range []string{"/home/user/private/bin/tool", prefix + "bin/../secret", prefix + "bin/./tool", prefix + "bin/tool (deleted)", prefix + "bin/tool\nsecret", prefix + "bin/\"secret\"", prefix + "share/tool", "/nix/store/eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee-secret/bin/tool", prefix + "bin/" + strings.Repeat("a", 513)} {
		if nativeSafeStoreExecutable(path) {
			t.Fatalf("unsafe identity accepted: %q", path)
		}
	}
}
func TestNativeContactExactImmutableStoreExecutable(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	shell, err = filepath.EvalSymlinks(shell)
	if err != nil {
		t.Fatal(err)
	}
	if !nativeSafeStoreExecutable(shell) {
		t.Skip("requires immutable Nix test toolchain")
	}
	id, proc := contactFixture(t)
	parent := writePhysicalProc(t, 71, 1, "/", nil, t.TempDir())
	stat, err := os.ReadFile(filepath.Join(proc, "stat"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "stat"), []byte(strings.Replace(string(stat), "S 1", "S 71", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(proc, "exe"))
	if err := os.Symlink(shell, filepath.Join(proc, "exe")); err != nil {
		t.Fatal(err)
	}
	refusalExecutableFixture(t, parent)
	_ = os.Remove(filepath.Join(parent, "exe"))
	if err := os.Symlink(shell, filepath.Join(parent, "exe")); err != nil {
		t.Fatal(err)
	}
	c := contactForFixture(t, id, proc)
	if c.Status != "stable" || c.Executable.Status != "stable" || c.Executable.StoreIdentity != shell || c.Comm != "physical-fixture" || c.ParentExecutable.Status != "stable" || c.ParentExecutable.StoreIdentity != shell {
		t.Fatalf("exact store identity missing: %+v", c)
	}
}

func TestNativeContactParentExecutableDriftDoesNotRetainIdentity(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	shell, err = filepath.EvalSymlinks(shell)
	if err != nil || !nativeSafeStoreExecutable(shell) {
		t.Skip("requires immutable Nix test toolchain")
	}
	id, proc := contactFixture(t)
	parent := writePhysicalProc(t, 71, 1, "/", nil, t.TempDir())
	stat, err := os.ReadFile(filepath.Join(proc, "stat"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "stat"), []byte(strings.Replace(string(stat), "S 1", "S 71", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	refusalExecutableFixture(t, parent)
	_ = os.Remove(filepath.Join(parent, "exe"))
	if err := os.Symlink(shell, filepath.Join(parent, "exe")); err != nil {
		t.Fatal(err)
	}
	saved := nativeContactBeforeRevalidate
	t.Cleanup(func() { nativeContactBeforeRevalidate = saved })
	nativeContactBeforeRevalidate = func(string) {
		_ = os.Remove(filepath.Join(parent, "exe"))
		_ = os.Symlink(filepath.Join(t.TempDir(), "replacement"), filepath.Join(parent, "exe"))
	}
	c := contactForFixture(t, id, proc)
	if c.Status != "stable" || c.ParentStatus != "unstable" || c.ParentExecutable.Status != "unavailable" || c.ParentExecutable.StoreIdentity != "unknown" {
		t.Fatalf("stale parent executable retained: %+v", c)
	}
}

func TestNativeContactParentIdentityAndDifferentNamespaceRemainExplicit(t *testing.T) {
	for _, drift := range []bool{false, true} {
		t.Run(map[bool]string{false: "stable-parent", true: "parent-drift"}[drift], func(t *testing.T) {
			id, proc := contactFixture(t)
			parent := writePhysicalProc(t, 71, 1, "/", nil, t.TempDir())
			ns := filepath.Join(proc, "ns")
			_ = os.Remove(filepath.Join(ns, "mnt"))
			if err := os.WriteFile(filepath.Join(ns, "mnt:[456]"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("mnt:[456]", filepath.Join(ns, "mnt")); err != nil {
				t.Fatal(err)
			}
			saved := nativeContactBeforeRevalidate
			t.Cleanup(func() { nativeContactBeforeRevalidate = saved })
			if drift {
				nativeContactBeforeRevalidate = func(string) {
					p := filepath.Join(parent, "stat")
					b, _ := os.ReadFile(p)
					_ = os.WriteFile(p, []byte(strings.ReplaceAll(string(b), "100071", "200071")), 0600)
				}
			}
			c := contactForFixture(t, id, proc)
			want := "stable"
			if drift {
				want = "unstable"
			}
			if c.Status != "stable" || c.ParentStart != "100071" || c.ParentStatus != want || c.NamespaceComparison["mnt"] != "different" {
				t.Fatalf("provenance hidden: %+v", c)
			}
		})
	}
}
func TestNativeContactReadOnlyReportCapDoesNotClaimAbsence(t *testing.T) {
	id, proc := contactFixture(t)
	for i := 0; i < 33; i++ {
		writePhysicalProc(t, 900+i, 1, "/", nil, id.hostWorktree)
	}
	r := observeNativeSlotCustody(custodySpec(id))
	if len(r.PhysicalContacts) != 32 || r.PhysicalContactsStatus != "truncated" || r.PhysicalContactsAreAbsenceProof {
		t.Fatalf("unbounded/complete contact report: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(proc, "cwd")); err != nil {
		t.Fatalf("observer changed process fixture: %v", err)
	}
	for _, c := range r.PhysicalContacts {
		b, err := json.Marshal(c)
		if err != nil || len(b) > 4096 {
			t.Fatalf("unbounded contact metadata: %d %v", len(b), err)
		}
	}
}

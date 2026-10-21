package nativecmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func refusalExecutableFixture(t *testing.T, proc string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "private-executable-sentinel")
	if err := os.WriteFile(file, []byte("not inspected"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, filepath.Join(proc, "exe")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	object := custodyObject(info)
	return fmt.Sprintf("%x:%x", object.Device, object.Inode)
}

func TestNativeUnownedWitnessRetainsStableExecutableAndParentWithoutCustody(t *testing.T) {
	physicalProcRootFixture(t)
	selected := physicalSlotFixture(t, 1)
	parent := writePhysicalProc(t, 91, 1, "/", nil, t.TempDir())
	proc := writePhysicalProc(t, 901, 91, "/", map[string]string{"PRIVATE": "environment-sentinel"}, selected.hostWorktree)
	parentObject := refusalExecutableFixture(t, parent)
	object := refusalExecutableFixture(t, proc)
	plan, err := planNativeSlotProcesses(selected, false)
	if plan != nil || err == nil {
		t.Fatalf("unowned process gained custody: %v %v", plan, err)
	}
	text := err.Error()
	for _, want := range []string{"w1=cwd/S:" + object, fmt.Sprintf("/S:%x:%s/", 100091, parentObject)} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if len(text) > 512 || strings.Contains(text, "sentinel") || strings.Contains(text, selected.hostWorktree) {
		t.Fatalf("unbounded/private witness: %s", text)
	}
	// Formatting an already-returned refusal must not borrow later PID evidence.
	if err := os.RemoveAll(proc); err != nil {
		t.Fatal(err)
	}
	writePhysicalProc(t, 901, 99, "/", nil, t.TempDir())
	if text != err.Error() {
		t.Fatal("refusal reread a reused PID")
	}
}

func TestNativeUnownedWitnessRaceAndUnavailableEvidenceNeverGrantsIdentity(t *testing.T) {
	for _, kind := range []string{"child-reuse", "child-parent", "child-exec", "parent-reuse", "parent-exec", "missing-exe", "missing-parent"} {
		t.Run(kind, func(t *testing.T) {
			physicalProcRootFixture(t)
			parent := writePhysicalProc(t, 92, 1, "/", nil, t.TempDir())
			proc := writePhysicalProc(t, 902, 92, "/", nil, t.TempDir())
			refusalExecutableFixture(t, parent)
			refusalExecutableFixture(t, proc)
			if kind == "missing-exe" {
				if err := os.Remove(filepath.Join(proc, "exe")); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "missing-parent" {
				if err := os.RemoveAll(parent); err != nil {
					t.Fatal(err)
				}
			}
			saved := nativeRefusalBeforeRevalidate
			t.Cleanup(func() { nativeRefusalBeforeRevalidate = saved })
			nativeRefusalBeforeRevalidate = func(path string) {
				selected := proc
				if strings.HasPrefix(kind, "parent-") {
					selected = parent
				}
				if path != selected {
					return
				}
				switch kind {
				case "child-reuse", "parent-reuse", "child-parent":
					stat, err := os.ReadFile(filepath.Join(path, "stat"))
					if err != nil {
						t.Fatal(err)
					}
					old, new := []byte("100902"), []byte("200902")
					if kind == "parent-reuse" {
						old, new = []byte("100092"), []byte("200092")
					}
					if kind == "child-parent" {
						old, new = []byte("S 92"), []byte("S 93")
					}
					if err := os.WriteFile(filepath.Join(path, "stat"), bytes.ReplaceAll(stat, old, new), 0600); err != nil {
						t.Fatal(err)
					}
				case "child-exec", "parent-exec":
					if err := os.Remove(filepath.Join(path, "exe")); err != nil {
						t.Fatal(err)
					}
					refusalExecutableFixture(t, path)
				}
			}
			text := observeNativeUnownedProcessWitness(proc, 92, "100902", "private-touch-sentinel")
			parts := strings.Split(strings.TrimPrefix(text, "w1="), "/")
			index, state := 1, "X:"
			if strings.HasPrefix(kind, "parent-") {
				index = 2
			}
			if kind == "missing-exe" {
				state = "U:"
			}
			if kind == "missing-parent" {
				index, state = 2, "U:"
			}
			if len(parts) != 4 || !strings.HasPrefix(parts[index], state) || strings.Contains(text, "sentinel") {
				t.Fatalf("race/private evidence promoted: %s", text)
			}
		})
	}
}

func TestNativeUnownedWitnessJointBoundaryRetainsExecutablePins(t *testing.T) {
	for _, kind := range []string{"child-metadata", "parent-metadata", "child-exec", "parent-exit"} {
		t.Run(kind, func(t *testing.T) {
			physicalProcRootFixture(t)
			parent := writePhysicalProc(t, 93, 1, "/", nil, t.TempDir())
			proc := writePhysicalProc(t, 903, 93, "/", nil, t.TempDir())
			refusalExecutableFixture(t, parent)
			refusalExecutableFixture(t, proc)
			saved := nativeRefusalBeforeJointRevalidate
			t.Cleanup(func() { nativeRefusalBeforeJointRevalidate = saved })
			nativeRefusalBeforeJointRevalidate = func(childPath, parentPath string) {
				switch kind {
				case "child-metadata", "parent-metadata":
					path := childPath
					if kind == "parent-metadata" {
						path = parentPath
					}
					// Same device/inode, changed metadata after both captures.
					// A fresh numeric-inode comparison would miss this change.
					if err := os.Chmod(filepath.Join(path, "exe"), 0600); err != nil {
						t.Fatal(err)
					}
				case "child-exec":
					if err := os.Remove(filepath.Join(childPath, "exe")); err != nil {
						t.Fatal(err)
					}
					refusalExecutableFixture(t, childPath)
				case "parent-exit":
					if err := os.RemoveAll(parentPath); err != nil {
						t.Fatal(err)
					}
				}
			}
			parts := strings.Split(strings.TrimPrefix(observeNativeUnownedProcessWitness(proc, 93, "100903", "fd"), "w1="), "/")
			index := 1
			if strings.HasPrefix(kind, "parent-") {
				index = 2
			}
			if len(parts) != 4 || !strings.HasPrefix(parts[index], "X:") {
				t.Fatalf("late drift promoted: %v", parts)
			}
		})
	}
}

func TestNativeUnownedWitnessMaximumRefusalEnvelope(t *testing.T) {
	maximum := ^uint64(0)
	maxInt := int(^uint(0) >> 1)
	start := strconv.FormatUint(maximum, 10)
	object := fmt.Sprintf("%x:%x", maximum, maximum)
	witness := nativeRefusalExecutable{status: "stable", start: start, object: object}
	encoded := formatNativeUnownedProcessWitness(strings.Repeat("private-touch-sentinel/", 100), witness, witness, maxInt, start)
	err := nativeUnownedProcessRefusal(maxInt, nativeSlotProcessIdentity{index: maxInt}, nativeProc{ppid: maxInt, start: start, refusalWitness: encoded})
	text := err.Error()
	if len(text) > 512 || !strings.HasSuffix(text, encoded) || !strings.Contains(text, object) || strings.Contains(text, "sentinel") {
		t.Fatalf("receiving 512-byte envelope exceeded or witness truncated: bytes=%d %s", len(text), text)
	}
}

func TestNativeUnownedWitnessLiveProcessFixture(t *testing.T) {
	if os.Getenv("DEVKIT_REFUSAL_FIXTURE") == "owned-test-child" {
		fmt.Fprintln(os.Stdout, "ready")
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	for _, role := range []string{"unmarked-helper", "foreign-agent-same-uid"} {
		t.Run(role, func(t *testing.T) {
			physicalProcRootFixture(t)
			selected := physicalSlotFixture(t, 1)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			command := exec.Command(executable, "-test.run=^TestNativeUnownedWitnessLiveProcessFixture$")
			command.Dir = selected.hostWorktree
			command.Env = []string{"DEVKIT_REFUSAL_FIXTURE=owned-test-child"}
			if role == "foreign-agent-same-uid" {
				command.Env = append(command.Env, "DEVKIT_NATIVE_AGENT=2")
			}
			input, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			output, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = input.Close()
				done := make(chan error, 1)
				go func() { done <- command.Wait() }()
				select {
				case err := <-done:
					if err != nil {
						t.Errorf("fixture exit: %v", err)
					}
				case <-time.After(5 * time.Second):
					_ = command.Process.Kill()
					<-done
					t.Error("fixture did not exit after EOF")
				}
			})
			ready := make(chan string, 1)
			go func() {
				b := make([]byte, 6)
				_, err := io.ReadFull(output, b)
				if err != nil {
					ready <- err.Error()
					return
				}
				ready <- string(b)
			}()
			select {
			case got := <-ready:
				if got != "ready\n" {
					t.Fatalf("fixture startup: %q", got)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("fixture startup timeout")
			}
			// Restrict the process census to these two actually owned test PIDs.
			// This fixture never scans or signals other host processes.
			for _, pid := range []int{command.Process.Pid, os.Getpid()} {
				if err := os.Symlink(filepath.Join("/proc", strconv.Itoa(pid)), filepath.Join(nativeSlotProcRoot, strconv.Itoa(pid))); err != nil {
					t.Fatal(err)
				}
			}
			plan, err := planNativeSlotProcesses(selected, false)
			if plan != nil || err == nil {
				t.Fatalf("helper/same UID granted custody: %v %v", plan, err)
			}
			for _, want := range []string{"physical_touch=true", "selected_identity=false", "descendant_owned=false", "w1=cwd/S:", "/S:", fmt.Sprintf("/%x:", os.Getpid()), fmt.Sprintf("parent_pid=%d", os.Getpid())} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("missing actual fixture witness %q: %s", want, err)
				}
			}
		})
	}
}

package nativecmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	"devkit/cli/devctl/internal/sqliteauthority"
	wtx "devkit/cli/devctl/internal/worktrees"
)

func TestSelectedNativeSlotManifestPreservesUnrelatedSuffixAndUsableSelectedHistory(t *testing.T) {
	fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Fatal("native test closure must supply SQLite")
	}
	t.Cleanup(sqliteauthority.SetExecutableForTesting(sqlite))
	selected := fixture.actual.Agents[1]
	workspace := filepath.Dir(selected.HostWorktree)
	if err := os.MkdirAll(filepath.Join(selected.HostHome, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(selected.HostWorktree, 0700); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(selected.HostHome, ".codex", "goals_1.sqlite")
	if out, err := exec.Command(sqlite, database, "CREATE TABLE goals (goal TEXT); INSERT INTO goals VALUES ('selected-goal-custody');").CombinedOutput(); err != nil {
		t.Fatalf("create selected history: %v %s", err, out)
	}
	// This suffix belongs to another operation. Its history is intentionally
	// inaccessible, and its other protected bytes are never reset here.
	surplus := fixture.actual.Agents[3]
	sentinels := map[string]string{}
	for _, root := range []string{surplus.HostWorktree, surplus.HostHome, surplus.StateRoot} {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "preserve-surplus")
		sentinels[path] = "unrelated owned state\n"
		if err := os.WriteFile(path, []byte(sentinels[path]), 0600); err != nil {
			t.Fatal(err)
		}
	}
	history := filepath.Join(surplus.HostHome, ".codex")
	if err := os.MkdirAll(history, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(history, 0700) })
	// A global history destination remains read-only, as in the actual caller.
	globalHistory := filepath.Join(fixture.opts.HostRoot, ".devkit", "codex-gui-history")
	if err := os.MkdirAll(globalHistory, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(globalHistory, 0700) })
	before, err := os.ReadFile(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	captureNativeManifestShrinkSlotHistory = func(string, string, nativeSlotProcessIdentity, string, string, bool) error {
		t.Fatal("selected reset tried to acquire unrelated surplus history")
		return errors.New("unrelated history")
	}
	existed, err := validateSelectedNativeSlotManifest(fixture.path, fixture.expected, fixture.opts)
	if err != nil || !existed {
		t.Fatalf("compatible selected manifest: existed=%v err=%v", existed, err)
	}
	resetOptions := nativeSlotManifestShrinkResetOptions(fixture)
	resetOptions.Index, resetOptions.Count = 2, 3
	reset, err := wtx.PlanNativeSlotReset(resetOptions)
	if err != nil {
		t.Fatal(err)
	}
	beforeDatabase, err := os.Stat(database)
	if err != nil {
		t.Fatal(err)
	}
	identity := nativeManifestShrinkIdentity(selected)
	err = executeNativeSlotDestructiveBoundary(nativeSlotDestructiveBoundary{
		stopSelected: func() error { return nil },
		inspectIdle: func() error {
			_, err := validateSelectedNativeSlotManifest(fixture.path, fixture.expected, fixture.opts)
			return err
		},
		snapshotHistory: func() error {
			return captureNativeSlotHistory("dev-all", "selected-slot-reset", identity, fixture.opts.StateRoot, workspace, false)
		},
		applyPlan: reset.Apply,
	})
	if err != nil {
		t.Fatal(err)
	}
	afterDatabase, err := os.Stat(database)
	if err != nil || !os.SameFile(beforeDatabase, afterDatabase) {
		t.Fatal("selected reset replaced or lost the usable database")
	}
	if out, err := exec.Command(sqlite, database, "PRAGMA integrity_check; SELECT goal FROM goals; INSERT INTO goals VALUES ('later-goal'); SELECT count(*) FROM goals;").CombinedOutput(); err != nil || string(out) != "ok\nselected-goal-custody\n2\n" {
		t.Fatalf("selected history cannot be read and updated after reset: %v %s", err, out)
	}
	if _, err := os.Lstat(filepath.Join(workspace, ".devkit", "codex-gui-history")); !os.IsNotExist(err) {
		t.Fatal("retained-in-place reset created an unnecessary cold history copy")
	}
	after, err := os.ReadFile(fixture.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("selected reset rewrote the shared manifest")
	}
	for path, want := range sentinels {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("surplus state changed: %s %v", path, err)
		}
	}
	if _, err := os.Lstat(nativeManifestShrinkTransactionPath(fixture.path)); !os.IsNotExist(err) {
		t.Fatalf("selected reset created a broad transaction: %v", err)
	}
}

func TestSelectedNativeSlotManifestRequiresCanonicalCompatibleGeometry(t *testing.T) {
	for _, name := range []string{"exact", "compatible-suffix", "growth", "foreign-home", "surplus-drift", "malformed", "symlink", "absent"} {
		t.Run(name, func(t *testing.T) {
			fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
			switch name {
			case "exact":
				if err := nativeagent.WriteManifest(fixture.path, fixture.expected, false); err != nil {
					t.Fatal(err)
				}
			case "growth":
				fixture.expected, fixture.actual = fixture.actual, fixture.expected
				if err := nativeagent.WriteManifest(fixture.path, fixture.actual, false); err != nil {
					t.Fatal(err)
				}
			case "foreign-home":
				fixture.actual.Agents[1].HostHome += "-foreign"
				if err := nativeagent.WriteManifest(fixture.path, fixture.actual, false); err != nil {
					t.Fatal(err)
				}
			case "surplus-drift":
				fixture.actual.Agents[3].HostHome += "-foreign"
				if err := nativeagent.WriteManifest(fixture.path, fixture.actual, false); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				if err := os.WriteFile(fixture.path, []byte("{invalid"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(fixture.path, fixture.path+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(fixture.path+".real", fixture.path); err != nil {
					t.Fatal(err)
				}
			case "absent":
				if err := os.Remove(fixture.path); err != nil {
					t.Fatal(err)
				}
			}
			existed, err := validateSelectedNativeSlotManifest(fixture.path, fixture.expected, fixture.opts)
			if name == "exact" || name == "compatible-suffix" || name == "absent" {
				if err != nil || existed != (name != "absent") {
					t.Fatalf("valid geometry: existed=%v err=%v", existed, err)
				}
			} else if err == nil {
				t.Fatal("uncertain geometry admitted")
			}
		})
	}
}

func TestSelectedNativeSlotManifestRefusesUnresolvedShrinkWithoutRecovery(t *testing.T) {
	for _, missingManifest := range []bool{false, true} {
		for _, name := range []string{"prepared", "committed", "malformed", "symlink", "directory"} {
			t.Run(fmt.Sprintf("missing-manifest=%t/%s", missingManifest, name), func(t *testing.T) {
				fixture := newNativeSlotManifestShrinkFixture(t, 4, 3)
				journal := nativeManifestShrinkTransactionPath(fixture.path)
				data := []byte(fmt.Sprintf(`{"schema":%q,"status":%q,"manifestPath":%q}`, nativeManifestShrinkTransactionSchema, name, fixture.path))
				if name == "malformed" {
					data = []byte("not-json")
				}
				if name == "directory" {
					if err := os.Mkdir(journal, 0700); err != nil {
						t.Fatal(err)
					}
				} else if name == "symlink" {
					if err := os.WriteFile(journal+".real", data, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(journal+".real", journal); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(journal, data, 0600); err != nil {
					t.Fatal(err)
				}
				if missingManifest {
					if err := os.Remove(fixture.path); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := validateSelectedNativeSlotManifest(fixture.path, fixture.expected, fixture.opts); err == nil {
					t.Fatal("pending/unknown broad transaction admitted")
				}
				if name != "directory" && name != "symlink" {
					after, err := os.ReadFile(journal)
					if err != nil || !bytes.Equal(after, data) {
						t.Fatal("selected reset modified the broad transaction")
					}
				}
			})
		}
	}
}

func TestRetainedNativeHistoryRefusesExternalPayloadBeforeResetApply(t *testing.T) {
	f := newNativeSlotManifestShrinkFixture(t, 4, 3)
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sqliteauthority.SetExecutableForTesting(sqlite))
	selected := f.actual.Agents[1]
	external := filepath.Join(selected.HostWorktree, "external-history.jsonl")
	if err := os.MkdirAll(filepath.Dir(external), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(external, []byte("protected external history\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(selected.HostHome, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(selected.HostHome, ".codex", "state_5.sqlite")
	sql := "CREATE TABLE threads (id TEXT PRIMARY KEY, rollout_path TEXT, source TEXT, title TEXT); INSERT INTO threads VALUES ('gui','" + strings.ReplaceAll(external, "'", "''") + "','vscode','protected');"
	if out, err := exec.Command(sqlite, database, sql).CombinedOutput(); err != nil {
		t.Fatalf("create actual reference: %v %s", err, out)
	}
	before, _ := os.ReadFile(database)
	applied := false
	err = executeNativeSlotDestructiveBoundary(nativeSlotDestructiveBoundary{
		stopSelected: func() error { return nil }, inspectIdle: func() error { return nil },
		snapshotHistory: func() error {
			return captureNativeSlotHistory("dev-all", "selected-slot-reset", nativeManifestShrinkIdentity(selected), f.opts.StateRoot, filepath.Dir(selected.HostWorktree), false)
		},
		applyPlan: func() error { applied = true; return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "escapes") || applied {
		t.Fatalf("retained-home branch omitted payload guard: applied=%v err=%v", applied, err)
	}
	after, _ := os.ReadFile(database)
	payload, _ := os.ReadFile(external)
	if !bytes.Equal(before, after) || string(payload) != "protected external history\n" {
		t.Fatal("refusal changed protected data")
	}
}

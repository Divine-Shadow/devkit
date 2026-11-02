package nativecmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

func nativeHomeMigrationFixture(t *testing.T) (nativeplan.Plan, string) {
	t.Helper()
	p, _ := freshSelectedFixture(t)
	legacy := nativeagent.PersistentDevAllHome(p.Agent.HostWorktree, p.Agent.ID.Index)
	if err := os.MkdirAll(filepath.Join(legacy, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, ".codex", "history"), []byte("retained human history\n"), 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := exec.LookPath("mv")
	if err != nil {
		t.Fatal("native test closure must supply coreutils mv")
	}
	previous := nativeHomeMoveExecutable
	nativeHomeMoveExecutable = binary
	t.Cleanup(func() { nativeHomeMoveExecutable = previous })
	return p, legacy
}

func TestNativeHomeMigrationPreservesUsableSQLiteWALSHMAndUpdates(t *testing.T) {
	p, legacy := nativeHomeMigrationFixture(t)
	database := filepath.Join(legacy, ".codex", "state_5.sqlite")
	cmd := exec.Command("sqlite3", database)
	cmd.Stdin = strings.NewReader("PRAGMA journal_mode=WAL;\n.filectrl persist_wal 1\nCREATE TABLE goals(value TEXT); INSERT INTO goals VALUES('retained goal');\n")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create persistent SQLite WAL fixture: %v %s", err, output)
	}
	before := map[string]os.FileInfo{}
	for _, name := range []string{".codex/state_5.sqlite", ".codex/state_5.sqlite-wal", ".codex/state_5.sqlite-shm", ".codex/history"} {
		info, err := os.Stat(filepath.Join(legacy, name))
		if err != nil {
			t.Fatalf("required actual SQLite companion %s: %v", name, err)
		}
		before[name] = info
	}
	checks := 0
	result, err := migrateNativeHome(p, false, func() error { checks++; return nil })
	if err != nil || result.Status != "moved" || checks < 2 {
		t.Fatalf("migration: %+v %v idle checks=%d", result, err, checks)
	}
	if _, err := os.Lstat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy namespace still exists after completed rename")
	}
	for name, original := range before {
		current, err := os.Stat(filepath.Join(p.Agent.HostHome, name))
		if err != nil || !os.SameFile(original, current) {
			t.Fatalf("migration replaced protected object %s: %v", name, err)
		}
	}
	query := "PRAGMA integrity_check; SELECT value FROM goals; INSERT INTO goals VALUES('later goal'); SELECT count(*) FROM goals;"
	if output, err := exec.Command("sqlite3", filepath.Join(p.Agent.HostHome, ".codex", "state_5.sqlite"), query).CombinedOutput(); err != nil || string(output) != "ok\nretained goal\n2\n" {
		t.Fatalf("migrated actual database is not usable: %v %s", err, output)
	}
	if err := launch.ValidateNativePersistentHome(p); err != nil {
		t.Fatal(err)
	}
	if result, err := migrateNativeHome(p, false, func() error { t.Fatal("completed migration repeated a lifecycle operation"); return nil }); err != nil || result.Status != "no-legacy-home" {
		t.Fatalf("completed migration observation: %+v %v", result, err)
	}
}

func TestNativeHomeMigrationRefusesUnsettledDataWithoutOverwrite(t *testing.T) {
	for _, name := range []string{"active", "two-legacy-homes", "occupied-empty", "source-symlink", "late-destination", "dry-run"} {
		t.Run(name, func(t *testing.T) {
			p, legacy := nativeHomeMigrationFixture(t)
			history := filepath.Join(legacy, ".codex", "history")
			before, err := os.Stat(history)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "two-legacy-homes":
				if err := os.MkdirAll(filepath.Join(p.Agent.StateRoot, "home"), 0700); err != nil {
					t.Fatal(err)
				}
			case "occupied-empty":
				if err := os.MkdirAll(p.Agent.HostHome, 0700); err != nil {
					t.Fatal(err)
				}
			case "source-symlink":
				if err := os.Rename(legacy, legacy+"-preserved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(legacy+"-preserved", legacy); err != nil {
					t.Fatal(err)
				}
				history = filepath.Join(legacy+"-preserved", ".codex", "history")
			}
			calls := 0
			result, err := migrateNativeHome(p, name == "dry-run", func() error {
				calls++
				if name == "active" {
					return fmt.Errorf("active owned GUI remains")
				}
				if name == "late-destination" && calls == 2 {
					return os.Mkdir(p.Agent.HostHome, 0700)
				}
				return nil
			})
			if name == "dry-run" {
				if err != nil || result.Status != "planned" {
					t.Fatalf("dry-run: %+v %v", result, err)
				}
				if _, err := os.Lstat(nativeagent.PersistentHomesRoot(p.HostStateRoot)); !os.IsNotExist(err) {
					t.Fatal("dry-run created persistent storage")
				}
			} else if err == nil {
				t.Fatalf("unsettled %s was admitted: %+v", name, result)
			}
			after, statErr := os.Stat(history)
			data, readErr := os.ReadFile(history)
			if statErr != nil || !os.SameFile(before, after) || readErr != nil || string(data) != "retained human history\n" {
				t.Fatal("refusal changed protected legacy data")
			}
		})
	}
}

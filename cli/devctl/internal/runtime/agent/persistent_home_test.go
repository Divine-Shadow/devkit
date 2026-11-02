package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeLegacyDataGuardDistinguishesEmptyWorktreeMountpoints(t *testing.T) {
	for _, kind := range []string{"empty-private", "empty-public", "hidden-data", "symlink", "unsafe-mode", "sticky", "state-home"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			worktree := filepath.Join(root, "worktree", "ouroboros-ide")
			state := filepath.Join(root, "state")
			home := PersistentDevAllHome(worktree, 1)
			if kind == "state-home" {
				home = filepath.Join(state, "home")
			}
			if err := os.MkdirAll(home, 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "empty-public":
				if err := os.Chmod(home, 0755); err != nil {
					t.Fatal(err)
				}
			case "sticky":
				if err := os.Chmod(home, os.ModeSticky|0755); err != nil {
					t.Fatal(err)
				}
			case "unsafe-mode":
				if err := os.Chmod(home, 0777); err != nil {
					t.Fatal(err)
				}
			case "hidden-data":
				if err := os.WriteFile(filepath.Join(home, ".keep-history"), []byte("protected"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				outside := filepath.Join(root, "outside")
				if err := os.Mkdir(outside, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(home); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, home); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(home)
			if err != nil {
				t.Fatal(err)
			}
			err = RequireLegacyHomesAbsent(worktree, state, 1)
			allowed := kind == "empty-private" || kind == "empty-public"
			if allowed && err != nil {
				t.Fatalf("empty worktree bind scaffold refused: %v", err)
			}
			if !allowed && (err == nil || !strings.Contains(err.Error(), "requires environment-owned migration")) {
				t.Fatalf("protected home admitted: %v", err)
			}
			after, err := os.Lstat(home)
			if err != nil || !os.SameFile(before, after) {
				t.Fatal("entry changed legacy home")
			}
			if kind == "hidden-data" {
				data, err := os.ReadFile(filepath.Join(home, ".keep-history"))
				if err != nil || string(data) != "protected" {
					t.Fatal("entry changed history")
				}
			}
		})
	}
}

func TestNativeHomeMountpointModeRejectsEverySpecialBit(t *testing.T) {
	// Nix builders intentionally deny chmod with setuid/setgid. Exercise the
	// complete mode guard without requesting those privileges; real filesystem
	// coverage above still checks empty, populated, sticky and redirected homes.
	for _, permissions := range []os.FileMode{0700, 0755} {
		mode := os.ModeDir | permissions
		if !nativeHomeMountpointModeAllowed(mode) {
			t.Fatalf("ordinary directory mode refused: %v", mode)
		}
		for bit := os.FileMode(1 << 9); bit != 0; bit <<= 1 {
			if bit == os.ModeDir {
				continue
			}
			if nativeHomeMountpointModeAllowed(mode | bit) {
				t.Fatalf("directory mode with special bit admitted: %v", mode|bit)
			}
		}
		if nativeHomeMountpointModeAllowed(permissions) {
			t.Fatalf("non-directory mode admitted: %v", permissions)
		}
	}
}

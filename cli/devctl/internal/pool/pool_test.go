package pool

import (
    "os"
    "path/filepath"
    "testing"
)

func TestDiscover(t *testing.T) {
    dir := t.TempDir()
    // dirs
    for _, n := range []string{"slot2", "slot1", "Z", "A"} {
        if err := os.MkdirAll(filepath.Join(dir, n), 0o755); err != nil { t.Fatal(err) }
    }
    // file (ignored)
    if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(""), 0o644); err != nil { t.Fatal(err) }
    slots, err := Discover(dir)
    if err != nil { t.Fatal(err) }
    if len(slots) != 4 { t.Fatalf("want 4 slots, got %d", len(slots)) }
    names := []string{slots[0].Name, slots[1].Name, slots[2].Name, slots[3].Name}
    want := []string{"A", "Z", "slot1", "slot2"}
    for i := range want { if names[i] != want[i] { t.Fatalf("order mismatch: got=%v want=%v", names, want) } }
}


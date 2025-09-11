package pool

import (
	"os"
	"path/filepath"
	"sort"
)

// Slot represents a single credential slot directory mounted in the container.
type Slot struct {
	Name string // directory name
	Path string // full path, e.g. /var/codex-pool/slot1
}

// Discover lists immediate subdirectories under root and returns them as slots,
// sorted by directory name. Non-directories are ignored. Missing root yields an
// empty slice and no error (callers can fall back to host seeding).
func Discover(root string) ([]Slot, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		// If the directory does not exist, treat as empty without error.
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	slots := make([]Slot, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			name := e.Name()
			slots = append(slots, Slot{Name: name, Path: filepath.Join(root, name)})
		}
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].Name < slots[j].Name })
	return slots, nil
}

package assign

import (
	poolpkg "devkit/cli/devctl/internal/pool"
	"testing"
)

func mkSlots(n int) []poolpkg.Slot {
	out := make([]poolpkg.Slot, n)
	for i := 0; i < n; i++ {
		out[i] = poolpkg.Slot{Name: string(rune('A' + i))}
	}
	return out
}

func TestByIndex(t *testing.T) {
	slots := mkSlots(3)
	a := ByIndex{}
	got := []string{
		a.Assign(slots, 1, 3).Name,
		a.Assign(slots, 2, 3).Name,
		a.Assign(slots, 3, 3).Name,
		a.Assign(slots, 4, 3).Name,
	}
	want := []string{"A", "B", "C", "A"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("i=%d got=%v want=%v", i, got, want)
		}
	}
}

func TestShuffleDeterministic(t *testing.T) {
	slots := mkSlots(4)
	s1 := NewShuffle(len(slots), 42)
	s2 := NewShuffle(len(slots), 42)
	for i := 1; i <= 6; i++ {
		if s1.Assign(slots, i, 6).Name != s2.Assign(slots, i, 6).Name {
			t.Fatalf("determinism failed at i=%d", i)
		}
	}
}

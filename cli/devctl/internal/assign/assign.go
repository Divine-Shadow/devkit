package assign

import (
    "math/rand"
    "time"

    poolpkg "devkit/cli/devctl/internal/pool"
)

// Assigner picks a slot for an agent index.
type Assigner interface {
    Assign(slots []poolpkg.Slot, agentIndex, agentCount int) poolpkg.Slot
}

// ByIndex assigns using (agentIndex-1) % len(slots).
type ByIndex struct{}

func (ByIndex) Assign(slots []poolpkg.Slot, agentIndex, agentCount int) poolpkg.Slot {
    if len(slots) == 0 { return poolpkg.Slot{} }
    i := (agentIndex - 1) % len(slots)
    if i < 0 { i = 0 }
    return slots[i]
}

// Shuffle assigns from a per-run permutation of slots.
type Shuffle struct{ perm []int }

// NewShuffle constructs a Shuffle with a deterministic permutation when seed != 0.
func NewShuffle(n int, seed int) Shuffle {
    r := rand.New(rand.NewSource(time.Now().UnixNano()))
    if seed != 0 { r = rand.New(rand.NewSource(int64(seed))) }
    perm := r.Perm(n)
    return Shuffle{perm: perm}
}

func (s Shuffle) Assign(slots []poolpkg.Slot, agentIndex, agentCount int) poolpkg.Slot {
    if len(slots) == 0 { return poolpkg.Slot{} }
    if len(s.perm) != len(slots) {
        // fallback to identity if mismatch
        return ByIndex{}.Assign(slots, agentIndex, agentCount)
    }
    i := (agentIndex - 1) % len(slots)
    if i < 0 { i = 0 }
    return slots[s.perm[i]]
}


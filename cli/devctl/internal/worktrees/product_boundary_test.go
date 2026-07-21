package worktrees

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateNativeSlotExplicitBoundaryContainsSiblingHomeAndState(t *testing.T) {
	root := t.TempDir()
	boundary := filepath.Join(root, "candidate")
	agent := filepath.Join(boundary, "agent1")
	worktree := filepath.Join(agent, "ouroboros-ide")
	common := filepath.Join(agent, ".devkit", "git", "ouroboros-ide.git")
	home := filepath.Join(boundary, "home")
	state := filepath.Join(boundary, "state")
	for _, path := range []string{worktree, common, home, state} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	options := NativeSlotValidationOptions{
		Repo:           "ouroboros-ide",
		Origin:         "ssh://git@ssh.github.com:443/example/ouroboros-ide.git",
		Index:          1,
		WorktreeRoot:   boundary,
		BoundaryRoot:   boundary,
		HostHome:       home,
		StateRoot:      state,
		SourceRevision: strings.Repeat("a", 40),
	}
	if err := ValidateNativeSlot(options); err == nil ||
		!strings.Contains(err.Error(), "inspect selected worktree metadata") {
		t.Fatalf("sibling home/state did not pass explicit boundary validation: %v", err)
	}

	outsideHome := filepath.Join(root, "outside-home")
	if err := os.Mkdir(outsideHome, 0o700); err != nil {
		t.Fatal(err)
	}
	options.HostHome = outsideHome
	if err := ValidateNativeSlot(options); err == nil ||
		!strings.Contains(err.Error(), "escapes disposable boundary") {
		t.Fatalf("home escape was not rejected at explicit boundary: %v", err)
	}
}

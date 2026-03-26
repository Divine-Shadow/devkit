package wtutil

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestViewerWindowNameAndLockPath(t *testing.T) {
	if got, want := ViewerWindowName("devkit:codex8"), "devkit-wt-devkit_codex8"; got != want {
		t.Fatalf("ViewerWindowName mismatch: got %q want %q", got, want)
	}
	gotPath := ViewerLockPath("devkit:codex8")
	wantSuffix := filepath.Join("devkit-wt", "devkit_codex8.lock")
	if filepath.Base(filepath.Dir(gotPath)) != filepath.Base(filepath.Dir(wantSuffix)) || filepath.Base(gotPath) != filepath.Base(wantSuffix) {
		t.Fatalf("ViewerLockPath mismatch: got %q want suffix %q", gotPath, wantSuffix)
	}
}

func TestNewTabArgs(t *testing.T) {
	got := NewTabArgs("devkit-wt-sess", TabSpec{
		Title:   "codex-1",
		Command: `exec tmux attach-session -t 'sess-linked-codex-1'`,
	})
	want := []string{
		"-w", "devkit-wt-sess",
		"new-tab",
		"--title", "codex-1",
		"bash", "-lc", `exec tmux attach-session -t 'sess-linked-codex-1'`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NewTabArgs mismatch: got %#v want %#v", got, want)
	}
}

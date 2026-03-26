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
	got := NewTabArgs("devkit-wt-sess", "/mnt/c/Windows/system32/wsl.exe", "NixOS", TabSpec{
		Title:   "codex-1",
		Command: `exec tmux attach-session -t 'sess-linked-codex-1'`,
	})
	want := []string{
		"-w", "devkit-wt-sess",
		"new-tab",
		"--title", "codex-1",
		"--",
		`C:\Windows\system32\wsl.exe`,
		"-d", "NixOS",
		"zsh", "-lic", `exec tmux attach-session -t 'sess-linked-codex-1'`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NewTabArgs mismatch: got %#v want %#v", got, want)
	}
}

func TestWindowsPath(t *testing.T) {
	if got, want := windowsPath("/mnt/c/Windows/System32/wsl.exe"), `C:\Windows\System32\wsl.exe`; got != want {
		t.Fatalf("windowsPath mismatch: got %q want %q", got, want)
	}
	if got, want := windowsPath("/run/current-system/sw/bin/zsh"), "/run/current-system/sw/bin/zsh"; got != want {
		t.Fatalf("windowsPath unexpected conversion: got %q want %q", got, want)
	}
}

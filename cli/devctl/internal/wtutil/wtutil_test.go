package wtutil

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestNewTabsArgsPreservesOrder(t *testing.T) {
	got := NewTabsArgs("devkit-wt-sess", "/mnt/c/Windows/system32/wsl.exe", "NixOS", []TabSpec{
		{Title: "codex-1", Command: "first"},
		{Title: "codex-2", Command: "second"},
	})
	want := []string{
		"-w", "devkit-wt-sess",
		"new-tab",
		"--title", "codex-1",
		"--",
		`C:\Windows\system32\wsl.exe`,
		"-d", "NixOS",
		"zsh", "-lic", "first",
		";",
		"new-tab",
		"--title", "codex-2",
		"--",
		`C:\Windows\system32\wsl.exe`,
		"-d", "NixOS",
		"zsh", "-lic", "second",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NewTabsArgs mismatch: got %#v want %#v", got, want)
	}
}

func TestNewTabsArgsSupportsExecArgs(t *testing.T) {
	got := NewTabsArgs("devkit-wt-sess", "/mnt/c/Windows/system32/wsl.exe", "NixOS", []TabSpec{
		{Title: "agent-5", Args: []string{
			"/home/bayesartre/dev/devkit/kit/bin/devctl",
			"-p", "dev-all",
			"exec-cd", "5",
			"/workspaces/dev/agent-worktrees/agent5/ouroboros-ide",
			"zsh", "-i",
		}},
	})
	want := []string{
		"-w", "devkit-wt-sess",
		"new-tab",
		"--title", "agent-5",
		"--",
		`C:\Windows\system32\wsl.exe`,
		"-d", "NixOS",
		"--exec",
		"/home/bayesartre/dev/devkit/kit/bin/devctl",
		"-p", "dev-all",
		"exec-cd", "5",
		"/workspaces/dev/agent-worktrees/agent5/ouroboros-ide",
		"zsh", "-i",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NewTabsArgs exec mismatch: got %#v want %#v", got, want)
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

func TestViewerLockRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "viewer.lock")
	lock := NewViewerLock("devkit/kit/scripts/devkit", "dev-all", "devkit_codex8")

	if err := WriteViewerLock(path, lock); err != nil {
		t.Fatalf("WriteViewerLock error: %v", err)
	}
	got, err := ReadViewerLock(path)
	if err != nil {
		t.Fatalf("ReadViewerLock error: %v", err)
	}
	if got.Project != "dev-all" || got.Session != "devkit_codex8" {
		t.Fatalf("unexpected lock metadata: %#v", got)
	}
	if got.ReleaseCommand != "devkit/kit/scripts/devkit -p dev-all wt-release --session devkit_codex8" {
		t.Fatalf("unexpected release command: %q", got.ReleaseCommand)
	}
	if strings.TrimSpace(got.CreatedAt) == "" {
		t.Fatalf("expected CreatedAt to be populated: %#v", got)
	}
}

func TestParseViewerLockSupportsLegacySessionOnlyFormat(t *testing.T) {
	got, err := ParseViewerLock([]byte("devkit_codex8\n"))
	if err != nil {
		t.Fatalf("ParseViewerLock error: %v", err)
	}
	if got.Session != "devkit_codex8" {
		t.Fatalf("unexpected legacy session: %#v", got)
	}
	if got.Project != "" || got.ReleaseCommand != "" {
		t.Fatalf("legacy lock should not infer metadata: %#v", got)
	}
}

func TestDefaultReleaseCommandFallsBackToCanonicalWrapper(t *testing.T) {
	got := DefaultReleaseCommand("", "dev-all", "devkit_codex8")
	if got != "devkit/kit/scripts/devkit -p dev-all wt-release --session devkit_codex8" {
		t.Fatalf("DefaultReleaseCommand mismatch: %q", got)
	}
}

func TestWriteViewerLockRejectsMissingSession(t *testing.T) {
	err := WriteViewerLock(filepath.Join(t.TempDir(), "viewer.lock"), ViewerLock{})
	if err == nil || !strings.Contains(err.Error(), "session is required") {
		t.Fatalf("expected missing session error, got %v", err)
	}
}

func TestReadViewerLockRejectsEmptyContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "viewer.lock")
	if err := os.WriteFile(path, []byte("\n"), 0o644); err != nil {
		t.Fatalf("write empty lock: %v", err)
	}
	errMsg := ""
	if _, err := ReadViewerLock(path); err != nil {
		errMsg = err.Error()
	}
	if !strings.Contains(errMsg, "wt viewer lock is empty") {
		t.Fatalf("expected empty lock error, got %q", errMsg)
	}
}

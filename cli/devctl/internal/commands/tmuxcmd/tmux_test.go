package tmuxcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/wtutil"
)

func TestHandleWTOpenLaunchesOneTabPerWindow(t *testing.T) {
	defaultSessionName = func(project string) string { return "devkit:" + project }
	hasTmuxSession = func(session string) bool { return session == "devkit:codex8" }
	_ = os.Remove(wtutil.ViewerLockPath("devkit:codex8"))
	t.Cleanup(func() { _ = os.Remove(wtutil.ViewerLockPath("devkit:codex8")) })
	dir := t.TempDir()
	wtLog := filepath.Join(dir, "wt.log")
	writeStub(t, filepath.Join(dir, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 0 ;;
  list-windows) printf '0	codex-1\n1	codex-2\n'; exit 0 ;;
esac
exit 0
`)
	writeStub(t, filepath.Join(dir, "wt.exe"), "#!/bin/sh\nprintf '%s\n' \"$*\" >> \"$WT_LOG\"\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WT_LOG", wtLog)

	ctx := &cmdregistry.Context{Project: "codex8"}
	if err := handleWTOpen(ctx); err != nil {
		t.Fatalf("handleWTOpen error: %v", err)
	}

	data, err := os.ReadFile(wtLog)
	if err != nil {
		t.Fatalf("read wt log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 wt launches, got %d: %q", len(lines), string(data))
	}
	if !strings.Contains(lines[0], "devkit-wt-devkit_codex8 new-tab --title codex-1 bash -lc exec tmux attach-session -t 'devkit-wt-devkit_codex8-codex-1'") {
		t.Fatalf("unexpected first wt launch: %q", lines[0])
	}
	if !strings.Contains(lines[1], "devkit-wt-devkit_codex8 new-tab --title codex-2 bash -lc exec tmux attach-session -t 'devkit-wt-devkit_codex8-codex-2'") {
		t.Fatalf("unexpected second wt launch: %q", lines[1])
	}
	if _, err := os.Stat(wtutil.ViewerLockPath("devkit:codex8")); err != nil {
		t.Fatalf("expected wt lock: %v", err)
	}
}

func TestHandleWTOpenFailsWithoutWT(t *testing.T) {
	defaultSessionName = func(project string) string { return "devkit:" + project }
	hasTmuxSession = func(session string) bool { return session == "devkit:codex8" }
	_ = os.Remove(wtutil.ViewerLockPath("devkit:codex8"))
	dir := t.TempDir()
	writeStub(t, filepath.Join(dir, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 0 ;;
  list-windows) printf '0	codex-1\n'; exit 0 ;;
esac
exit 0
`)
	t.Setenv("PATH", dir)

	err := handleWTOpen(&cmdregistry.Context{Project: "codex8"})
	if err == nil || !strings.Contains(err.Error(), "Windows Terminal not found") {
		t.Fatalf("expected missing wt error, got %v", err)
	}
}

func TestHandleWTOpenFailsWhenSessionMissing(t *testing.T) {
	defaultSessionName = func(project string) string { return "devkit:" + project }
	hasTmuxSession = func(session string) bool { return false }
	_ = os.Remove(wtutil.ViewerLockPath("devkit:codex8"))
	dir := t.TempDir()
	writeStub(t, filepath.Join(dir, "tmux"), "#!/bin/sh\nexit 1\n")
	writeStub(t, filepath.Join(dir, "wt.exe"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := handleWTOpen(&cmdregistry.Context{Project: "codex8"})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected missing tmux session error, got %v", err)
	}
}

func TestHandleWTOpenFailsOnExistingLock(t *testing.T) {
	defaultSessionName = func(project string) string { return "devkit:" + project }
	hasTmuxSession = func(session string) bool { return session == "devkit:codex8" }
	dir := t.TempDir()
	writeStub(t, filepath.Join(dir, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 0 ;;
  list-windows) printf '0	codex-1\n'; exit 0 ;;
esac
exit 0
`)
	writeStub(t, filepath.Join(dir, "wt.exe"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	lockPath := wtutil.ViewerLockPath("devkit:codex8")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("locked"), 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(lockPath) })

	err := handleWTOpen(&cmdregistry.Context{Project: "codex8"})
	if err == nil || !strings.Contains(err.Error(), "wt viewer lock exists") {
		t.Fatalf("expected lock error, got %v", err)
	}
}

func TestHandleWTReleaseRemovesLock(t *testing.T) {
	defaultSessionName = func(project string) string { return "devkit:" + project }
	hasTmuxSession = func(session string) bool { return true }
	lockPath := wtutil.ViewerLockPath("devkit:codex8")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("locked"), 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	if err := handleWTRelease(&cmdregistry.Context{Project: "codex8"}); err != nil {
		t.Fatalf("handleWTRelease error: %v", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected lock removed, stat err=%v", err)
	}
}

func TestResolveWTBinaryHonorsConfiguredPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom-wt.exe")
	writeStub(t, path, "#!/bin/sh\nexit 0\n")
	t.Setenv("DEVKIT_WT_PATH", path)

	got, err := resolveWTBinary()
	if err != nil {
		t.Fatalf("resolveWTBinary error: %v", err)
	}
	if got != path {
		t.Fatalf("resolveWTBinary mismatch: got %q want %q", got, path)
	}
}

func TestTabSessionName(t *testing.T) {
	if got, want := tabSessionName("devkit:codex8", "codex-1"), "devkit-wt-devkit_codex8-codex-1"; got != want {
		t.Fatalf("tabSessionName mismatch: got %q want %q", got, want)
	}
}

func writeStub(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", path, err)
	}
}

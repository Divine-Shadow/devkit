package tmuxcmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/tmuxnotify"
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
	writeStub(t, filepath.Join(dir, "wsl.exe"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WT_LOG", wtLog)
	t.Setenv("WSL_DISTRO_NAME", "NixOS")

	ctx := &cmdregistry.Context{Project: "codex8", Exe: "devkit/kit/scripts/devkit"}
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
	if !strings.Contains(lines[0], "devkit-wt-devkit_codex8 new-tab --title codex-1 -- "+filepath.Join(dir, "wsl.exe")+" -d NixOS zsh -lic exec tmux attach-session -t 'devkit-wt-devkit_codex8-codex-1'") {
		t.Fatalf("unexpected first wt launch: %q", lines[0])
	}
	if !strings.Contains(lines[1], "devkit-wt-devkit_codex8 new-tab --title codex-2 -- "+filepath.Join(dir, "wsl.exe")+" -d NixOS zsh -lic exec tmux attach-session -t 'devkit-wt-devkit_codex8-codex-2'") {
		t.Fatalf("unexpected second wt launch: %q", lines[1])
	}
	lock, err := wtutil.ReadViewerLock(wtutil.ViewerLockPath("devkit:codex8"))
	if err != nil {
		t.Fatalf("expected structured wt lock: %v", err)
	}
	if lock.Project != "codex8" || lock.Session != "devkit:codex8" {
		t.Fatalf("unexpected lock metadata: %#v", lock)
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
	writeStub(t, filepath.Join(dir, "wsl.exe"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir)
	t.Setenv("WSL_DISTRO_NAME", "NixOS")

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
	writeStub(t, filepath.Join(dir, "wsl.exe"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WSL_DISTRO_NAME", "NixOS")

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
	writeStub(t, filepath.Join(dir, "wsl.exe"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WSL_DISTRO_NAME", "NixOS")
	lockPath := wtutil.ViewerLockPath("devkit:codex8")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("locked"), 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(lockPath) })

	err := handleWTOpen(&cmdregistry.Context{Project: "codex8", Exe: "devkit/kit/scripts/devkit"})
	if err == nil || !strings.Contains(err.Error(), "wt viewer lock exists") {
		t.Fatalf("expected lock error, got %v", err)
	}
	if !strings.Contains(err.Error(), "devkit/kit/scripts/devkit -p codex8 wt-release --session devkit:codex8") {
		t.Fatalf("expected canonical release command in error, got %v", err)
	}
}

func TestHandleWTOpenPlainLaunchesDirectExecTabs(t *testing.T) {
	defaultSessionName = func(project string) string { return "devkit:" + project }
	hasTmuxSession = func(session string) bool { return false }
	originalDetectPlainProject := detectPlainProject
	detectPlainProject = func(ctx *cmdregistry.Context) string { return "dev-all" }
	t.Cleanup(func() { detectPlainProject = originalDetectPlainProject })
	mustAtoi = func(value string) int {
		switch value {
		case "2":
			return 2
		default:
			t.Fatalf("unexpected mustAtoi input %q", value)
			return 0
		}
	}
	lockPath := wtutil.ViewerLockPath("devkit:codex8-plain")
	_ = os.Remove(lockPath)
	t.Cleanup(func() { _ = os.Remove(lockPath) })

	dir := t.TempDir()
	wtLog := filepath.Join(dir, "wt.log")
	writeStub(t, filepath.Join(dir, "wt.exe"), "#!/bin/sh\nprintf '%s\n' \"$*\" >> \"$WT_LOG\"\n")
	writeStub(t, filepath.Join(dir, "wsl.exe"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WT_LOG", wtLog)
	t.Setenv("WSL_DISTRO_NAME", "NixOS")

	ctx := &cmdregistry.Context{
		Project: "codex",
		Exe:     "/home/bayesartre/dev/devkit/kit/bin/devctl",
		Args:    []string{"--plain", "--count", "2", "--cd", "/workspace"},
	}
	if err := handleWTOpen(ctx); err != nil {
		t.Fatalf("handleWTOpen plain error: %v", err)
	}

	data, err := os.ReadFile(wtLog)
	if err != nil {
		t.Fatalf("read wt log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 wt launch, got %d: %q", len(lines), string(data))
	}
	if strings.Contains(lines[0], "zsh -lic") {
		t.Fatalf("plain wt launch should not use nested zsh -lic: %q", lines[0])
	}
	if !strings.Contains(lines[0], "--exec /home/bayesartre/dev/devkit/kit/bin/devctl -p dev-all exec-cd 1 /workspace zsh -i ; new-tab --title agent-2 -- "+filepath.Join(dir, "wsl.exe")+" -d NixOS --exec /home/bayesartre/dev/devkit/kit/bin/devctl -p dev-all exec-cd 2 /workspace zsh -i") {
		t.Fatalf("unexpected plain wt launch: %q", lines[0])
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("plain wt-open should not create viewer lock, stat err=%v", err)
	}
}

func TestPlainTabArgsUsesDetectedNativeProject(t *testing.T) {
	originalDetectPlainProject := detectPlainProject
	detectPlainProject = func(ctx *cmdregistry.Context) string { return "dev-all" }
	t.Cleanup(func() { detectPlainProject = originalDetectPlainProject })

	ctx := &cmdregistry.Context{
		Project: "codex",
		Exe:     "/home/bayesartre/dev/devkit/kit/bin/devctl",
		Paths:   cmdregistry.Context{}.Paths,
	}
	got := strings.Join(plainTabArgs(ctx, "5", ""), " ")
	want := "/home/bayesartre/dev/devkit/kit/bin/devctl -p dev-all exec-cd 5 /workspaces/dev/agent-worktrees/agent5/ouroboros-ide zsh -i"
	if got != want {
		t.Fatalf("plainTabArgs mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestHandleWTOpenPlainIndexLaunchesSingleDirectExecTab(t *testing.T) {
	defaultSessionName = func(project string) string { return "devkit:" + project }
	hasTmuxSession = func(session string) bool { return false }
	originalDetectPlainProject := detectPlainProject
	detectPlainProject = func(ctx *cmdregistry.Context) string { return "dev-all" }
	t.Cleanup(func() { detectPlainProject = originalDetectPlainProject })
	mustAtoi = func(value string) int {
		switch value {
		case "5":
			return 5
		default:
			t.Fatalf("unexpected mustAtoi input %q", value)
			return 0
		}
	}

	dir := t.TempDir()
	wtLog := filepath.Join(dir, "wt.log")
	writeStub(t, filepath.Join(dir, "wt.exe"), "#!/bin/sh\nprintf '%s\n' \"$*\" >> \"$WT_LOG\"\n")
	writeStub(t, filepath.Join(dir, "wsl.exe"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WT_LOG", wtLog)
	t.Setenv("WSL_DISTRO_NAME", "NixOS")

	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Exe:     "/home/bayesartre/dev/devkit/kit/bin/devctl",
		Args:    []string{"--plain", "--index", "5"},
	}
	if err := handleWTOpen(ctx); err != nil {
		t.Fatalf("handleWTOpen plain index error: %v", err)
	}

	data, err := os.ReadFile(wtLog)
	if err != nil {
		t.Fatalf("read wt log: %v", err)
	}
	line := strings.TrimSpace(string(data))
	if strings.Contains(line, "agent-1") || strings.Contains(line, "agent-4") {
		t.Fatalf("index launch should only target agent-5: %q", line)
	}
	if strings.Contains(line, "zsh -lic") {
		t.Fatalf("plain index launch should not use nested zsh -lic: %q", line)
	}
	want := "new-tab --title agent-5 -- " + filepath.Join(dir, "wsl.exe") + " -d NixOS --exec /home/bayesartre/dev/devkit/kit/bin/devctl -p dev-all exec-cd 5 /workspaces/dev/agent-worktrees/agent5/ouroboros-ide zsh -i"
	if !strings.Contains(line, want) {
		t.Fatalf("unexpected plain index wt launch:\n got: %q\nwant substring: %q", line, want)
	}
}

func TestHandleWTOpenPlainRejectsIndexAndCount(t *testing.T) {
	defaultSessionName = func(project string) string { return "devkit:" + project }
	hasTmuxSession = func(session string) bool { return false }
	mustAtoi = func(value string) int { return 5 }
	dir := t.TempDir()
	writeStub(t, filepath.Join(dir, "wt.exe"), "#!/bin/sh\nexit 0\n")
	writeStub(t, filepath.Join(dir, "wsl.exe"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WSL_DISTRO_NAME", "NixOS")

	err := handleWTOpen(&cmdregistry.Context{
		Project: "dev-all",
		Args:    []string{"--plain", "--index", "5", "--count", "5"},
	})
	if err == nil || !strings.Contains(err.Error(), "either --index or --count") {
		t.Fatalf("expected index/count validation error, got %v", err)
	}
}

func TestHandleWTReleaseRemovesLock(t *testing.T) {
	defaultSessionName = func(project string) string { return "devkit:" + project }
	hasTmuxSession = func(session string) bool { return true }
	lockPath := wtutil.ViewerLockPath("devkit:codex8")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	if err := wtutil.WriteViewerLock(lockPath, wtutil.NewViewerLock("devkit/kit/scripts/devkit", "codex8", "devkit:codex8")); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	if err := handleWTRelease(&cmdregistry.Context{Project: "codex8"}); err != nil {
		t.Fatalf("handleWTRelease error: %v", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected lock removed, stat err=%v", err)
	}
}

func TestHandleWTReleaseRecoversProjectFromStructuredLock(t *testing.T) {
	defaultSessionName = func(project string) string { return "devkit:" + project }
	hasTmuxSession = func(session string) bool { return true }
	dir := t.TempDir()
	writeStub(t, filepath.Join(dir, "tmux"), `#!/bin/sh
case "$1" in
  list-windows) printf '0	codex-1\n'; exit 0 ;;
  kill-session) exit 0 ;;
esac
exit 0
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	lockPath := wtutil.ViewerLockPath("devkit_codex8")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	if err := wtutil.WriteViewerLock(lockPath, wtutil.NewViewerLock("devkit/kit/scripts/devkit", "dev-all", "devkit_codex8")); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	if err := handleWTRelease(&cmdregistry.Context{
		Args: []string{"--session", "devkit_codex8"},
		Exe:  "devkit/kit/scripts/devkit",
	}); err != nil {
		t.Fatalf("handleWTRelease error: %v", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected lock removed, stat err=%v", err)
	}
}

func TestHandleWTReleaseFailsWithoutProjectMetadataInLegacyLock(t *testing.T) {
	lockPath := wtutil.ViewerLockPath("devkit_codex8")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("devkit_codex8\n"), 0o644); err != nil {
		t.Fatalf("write legacy lock: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(lockPath) })

	err := handleWTRelease(&cmdregistry.Context{
		Args: []string{"--session", "devkit_codex8"},
		Exe:  "devkit/kit/scripts/devkit",
	})
	if err == nil || !strings.Contains(err.Error(), "has no project metadata") {
		t.Fatalf("expected project metadata error, got %v", err)
	}
	if !strings.Contains(err.Error(), "devkit/kit/scripts/devkit -p <project> wt-release --session devkit_codex8") {
		t.Fatalf("expected corrective command in error, got %v", err)
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

func TestResolveWSLBinaryAndDistro(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wsl.exe")
	writeStub(t, path, "#!/bin/sh\nexit 0\n")
	t.Setenv("DEVKIT_WSL_PATH", path)
	t.Setenv("DEVKIT_WSL_DISTRO", "NixOS")

	gotPath, err := resolveWSLBinary()
	if err != nil {
		t.Fatalf("resolveWSLBinary error: %v", err)
	}
	if gotPath != path {
		t.Fatalf("resolveWSLBinary mismatch: got %q want %q", gotPath, path)
	}
	gotDistro, err := resolveWSLDistro()
	if err != nil {
		t.Fatalf("resolveWSLDistro error: %v", err)
	}
	if gotDistro != "NixOS" {
		t.Fatalf("resolveWSLDistro mismatch: got %q want %q", gotDistro, "NixOS")
	}
}

func TestTabSessionName(t *testing.T) {
	if got, want := tabSessionName("devkit:codex8", "codex-1"), "devkit-wt-devkit_codex8-codex-1"; got != want {
		t.Fatalf("tabSessionName mismatch: got %q want %q", got, want)
	}
}

func TestHandleTMUXBellInstallWritesExpectedTmuxCommands(t *testing.T) {
	defaultSessionName = func(project string) string { return "devkit:codex8" }
	hasTmuxSession = func(session string) bool { return session == "devkit:codex8" }
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	exePath := filepath.Join(dir, "devctl")
	writeStub(t, exePath, "#!/bin/sh\nexit 0\n")
	writeStub(t, filepath.Join(dir, "tmux"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TMUX_LOG\"\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX_LOG", logPath)

	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Exe:     exePath,
		Args:    []string{"--session", "devkit:codex8", "--backend", "file", "--file", filepath.Join(dir, "bells.jsonl"), "--debounce-ms", "0"},
	}
	if err := handleTMUXBellInstall(ctx); err != nil {
		t.Fatalf("handleTMUXBellInstall error: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read tmux log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 tmux commands, got %d: %q", len(lines), string(data))
	}
	if lines[0] != "set-window-option -g monitor-bell on" {
		t.Fatalf("unexpected first tmux command: %q", lines[0])
	}
	if !strings.Contains(lines[1], "set-hook -g alert-bell run-shell -b") {
		t.Fatalf("unexpected second tmux command: %q", lines[1])
	}
	if !strings.Contains(lines[1], "tmux-notify-bell") || !strings.Contains(lines[1], "#{session_name}") {
		t.Fatalf("hook command missing notifier payload: %q", lines[1])
	}
}

func TestHandleTMUXNotifyBellWritesEventFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bells.jsonl")
	ctx := &cmdregistry.Context{
		Project: "dev-all",
		Args: []string{
			"--backend", "file",
			"--file", path,
			"--debounce-ms", "0",
			"--session", "devkit_codex8",
			"--window-index", "4",
			"--window-name", "codex-4",
			"--pane-id", "%2",
			"--pane-title", "codex",
		},
	}
	if err := handleTMUXNotifyBell(ctx); err != nil {
		t.Fatalf("handleTMUXNotifyBell error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read notify file: %v", err)
	}
	var event tmuxnotify.Event
	if err := json.Unmarshal(data[:len(data)-1], &event); err != nil {
		t.Fatalf("unmarshal notify event: %v", err)
	}
	if event.WindowName != "codex-4" || event.Message != "tmux bell: devkit_codex8 / 4:codex-4" {
		t.Fatalf("unexpected notify event: %#v", event)
	}
}

func writeStub(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", path, err)
	}
}

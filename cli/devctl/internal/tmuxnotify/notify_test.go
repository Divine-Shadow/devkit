package tmuxnotify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHookCommand(t *testing.T) {
	got := HookCommand("/tmp/devctl", "dev-all", Config{
		Backend:    BackendFile,
		FilePath:   "/tmp/bells.jsonl",
		DebounceMS: 2500,
	})
	expects := []string{
		"'/tmp/devctl'",
		"'-p' 'dev-all'",
		"'tmux-notify-bell'",
		"'--backend' 'file'",
		"'--file' '/tmp/bells.jsonl'",
		"'--debounce-ms' '2500'",
		"'--session' '#{session_name}'",
		"'--window-name' '#{window_name}'",
	}
	for _, expect := range expects {
		if !strings.Contains(got, expect) {
			t.Fatalf("hook command missing %q: %s", expect, got)
		}
	}
}

func TestNotifyFileWritesJSONLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bells.jsonl")
	err := Notify(Config{Backend: BackendFile, FilePath: path, DebounceMS: 0}, Event{
		Session:     "devkit_codex8",
		WindowIndex: "6",
		WindowName:  "codex-6",
		PaneID:      "%1",
		PaneTitle:   "codex",
	})
	if err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read notify file: %v", err)
	}
	var event Event
	if err := json.Unmarshal(data[:len(data)-1], &event); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if event.Message != "tmux bell: devkit_codex8 / 6:codex-6" {
		t.Fatalf("unexpected message: %q", event.Message)
	}
	if event.Timestamp == "" {
		t.Fatalf("expected timestamp")
	}
}

func TestNotifyDebouncesBySessionAndWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bells.jsonl")
	config := Config{Backend: BackendFile, FilePath: path, DebounceMS: 10000}
	event := Event{Session: "sess", WindowIndex: "1", WindowName: "agent-1"}
	if err := Notify(config, event); err != nil {
		t.Fatalf("first Notify error: %v", err)
	}
	if err := Notify(config, event); err != nil {
		t.Fatalf("second Notify error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read notify file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one line after debounce, got %d: %q", len(lines), string(data))
	}
}

func TestPowerShellCommandUsesConfiguredBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "powershell.exe")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write powershell stub: %v", err)
	}
	t.Setenv("DEVKIT_POWERSHELL_PATH", path)
	args, err := PowerShellCommand(Event{Message: "tmux bell: sess / 1:agent-1"})
	if err != nil {
		t.Fatalf("PowerShellCommand error: %v", err)
	}
	if args[0] != path {
		t.Fatalf("unexpected powershell path: %q", args[0])
	}
	if len(args) < 7 || args[len(args)-2] != "-Command" {
		t.Fatalf("unexpected powershell args: %#v", args)
	}
}

func TestDefaultConfigReadsEnvironment(t *testing.T) {
	t.Setenv("DEVKIT_TMUX_NOTIFY_BACKEND", BackendFile)
	t.Setenv("DEVKIT_TMUX_NOTIFY_FILE", "/tmp/test.jsonl")
	t.Setenv("DEVKIT_TMUX_NOTIFY_DEBOUNCE_MS", "42")
	config := DefaultConfig()
	if config.Backend != BackendFile || config.FilePath != "/tmp/test.jsonl" || config.DebounceMS != 42 {
		t.Fatalf("unexpected default config: %#v", config)
	}
}

func TestAllowEventExpires(t *testing.T) {
	config := Config{Backend: BackendFile, FilePath: filepath.Join(t.TempDir(), "bells.jsonl"), DebounceMS: 10}
	event := Event{Session: "sess", WindowIndex: "2", WindowName: "agent-2"}
	now := time.Now()
	allowed, err := allowEvent(config, event, now)
	if err != nil || !allowed {
		t.Fatalf("first allowEvent = %v, %v", allowed, err)
	}
	allowed, err = allowEvent(config, event, now.Add(5*time.Millisecond))
	if err != nil || allowed {
		t.Fatalf("second allowEvent = %v, %v", allowed, err)
	}
	allowed, err = allowEvent(config, event, now.Add(20*time.Millisecond))
	if err != nil || !allowed {
		t.Fatalf("third allowEvent = %v, %v", allowed, err)
	}
}

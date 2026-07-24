package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTMUXBellIntegration(t *testing.T) {
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux not available")
	}
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}

	bin := filepath.Join(t.TempDir(), "devctl")
	build := exec.Command("go", "build", "-trimpath", "-o", bin, "./")
	build.Dir = filepath.Join("..")
	build.Env = append(os.Environ(), "GO111MODULE=on")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	dir := t.TempDir()
	socketName := "devctl-bell-test"
	tmuxWrapper := filepath.Join(dir, "tmux")
	tmuxLog := filepath.Join(dir, "tmux-wrapper.log")
	wrapper := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellEscape(tmuxLog) + "\nexec " + shellEscape(realTmux) + " -L " + shellEscape(socketName) + " \"$@\"\n"
	if err := os.WriteFile(tmuxWrapper, []byte(wrapper), 0o755); err != nil {
		t.Fatalf("write tmux wrapper: %v", err)
	}
	env := withEnvOverride(os.Environ(), "PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	session := "bellsess"
	eventFile := filepath.Join(dir, "bells.jsonl")

	newSession := exec.Command(tmuxWrapper, "new-session", "-d", "-s", session, shellPath)
	newSession.Env = env
	if out, err := newSession.CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session failed: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		kill := exec.Command(tmuxWrapper, "kill-server")
		kill.Env = env
		_, _ = kill.CombinedOutput()
	})

	install := exec.Command(bin, "-p", "dev-all", "tmux-bell-install", "--session", session, "--backend", "file", "--file", eventFile, "--debounce-ms", "0")
	install.Env = env
	if out, err := install.CombinedOutput(); err != nil {
		wrapperLog, _ := os.ReadFile(tmuxLog)
		t.Fatalf("tmux-bell-install failed: %v\n%s\nwrapper log:\n%s", err, out, wrapperLog)
	}

	send := exec.Command(tmuxWrapper, "send-keys", "-t", session+":0", "printf '\\007'", "C-m")
	send.Env = env
	if out, err := send.CombinedOutput(); err != nil {
		t.Fatalf("tmux send-keys failed: %v\n%s", err, out)
	}

	var data []byte
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err = os.ReadFile(eventFile)
		if err == nil && len(bytes.TrimSpace(data)) > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		t.Fatalf("expected bell event file %s to be populated", eventFile)
	}
	line := strings.Split(strings.TrimSpace(string(data)), "\n")[0]
	var event struct {
		Session     string `json:"session"`
		WindowIndex string `json:"window_index"`
		WindowName  string `json:"window_name"`
		Message     string `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("unmarshal bell event: %v\n%s", err, line)
	}
	if event.Session != session {
		t.Fatalf("unexpected session: %#v", event)
	}
	if event.WindowName == "" && event.WindowIndex == "" {
		t.Fatalf("expected window metadata: %#v", event)
	}
	if !strings.Contains(event.Message, "tmux bell: "+session) {
		t.Fatalf("unexpected bell message: %#v", event)
	}
}

func shellEscape(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func withEnvOverride(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			if !replaced {
				out = append(out, prefix+value)
				replaced = true
			}
			continue
		}
		out = append(out, item)
	}
	if !replaced {
		out = append(out, prefix+value)
	}
	return out
}

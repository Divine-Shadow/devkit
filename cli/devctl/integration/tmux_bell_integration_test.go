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
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script not available")
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
	wrapper := "#!/bin/sh\nexec " + shellEscape(realTmux) + " -L " + shellEscape(socketName) + " \"$@\"\n"
	if err := os.WriteFile(tmuxWrapper, []byte(wrapper), 0o755); err != nil {
		t.Fatalf("write tmux wrapper: %v", err)
	}
	env := append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	session := "bellsess"
	eventFile := filepath.Join(dir, "bells.jsonl")

	newSession := exec.Command(tmuxWrapper, "new-session", "-d", "-s", session)
	newSession.Env = env
	if out, err := newSession.CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session failed: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		kill := exec.Command(tmuxWrapper, "kill-server")
		kill.Env = env
		_, _ = kill.CombinedOutput()
	})

	attachScript := exec.Command("script", "-q", filepath.Join(dir, "client.log"), "-c", tmuxWrapper+" attach -t "+session)
	attachScript.Env = env
	if err := attachScript.Start(); err != nil {
		t.Fatalf("start attached tmux client: %v", err)
	}
	t.Cleanup(func() {
		_ = attachScript.Process.Kill()
		_, _ = attachScript.Process.Wait()
	})
	time.Sleep(500 * time.Millisecond)

	install := exec.Command(bin, "-p", "dev-all", "tmux-bell-install", "--session", session, "--backend", "file", "--file", eventFile, "--debounce-ms", "0")
	install.Env = env
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("tmux-bell-install failed: %v\n%s", err, out)
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

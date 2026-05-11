package tmuxnotify

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	BackendWindowsNotify = "windows-notify"
	BackendFile          = "file"
	defaultDebounceMS    = 5000
)

type Config struct {
	Backend    string
	FilePath   string
	DebounceMS int
}

type Event struct {
	Session     string `json:"session"`
	WindowIndex string `json:"window_index"`
	WindowName  string `json:"window_name"`
	PaneID      string `json:"pane_id"`
	PaneTitle   string `json:"pane_title"`
	Message     string `json:"message"`
	Timestamp   string `json:"timestamp"`
}

func DefaultConfig() Config {
	return Config{
		Backend:    firstNonEmpty(strings.TrimSpace(os.Getenv("DEVKIT_TMUX_NOTIFY_BACKEND")), BackendWindowsNotify),
		FilePath:   strings.TrimSpace(os.Getenv("DEVKIT_TMUX_NOTIFY_FILE")),
		DebounceMS: envInt("DEVKIT_TMUX_NOTIFY_DEBOUNCE_MS", defaultDebounceMS),
	}
}

func (c Config) Normalize() Config {
	c.Backend = strings.TrimSpace(c.Backend)
	if c.Backend == "" {
		c.Backend = BackendWindowsNotify
	}
	if c.DebounceMS < 0 {
		c.DebounceMS = 0
	}
	return c
}

func (c Config) Validate() error {
	switch c.Backend {
	case BackendWindowsNotify:
		return nil
	case BackendFile:
		if strings.TrimSpace(c.FilePath) == "" {
			return fmt.Errorf("file backend requires --file or DEVKIT_TMUX_NOTIFY_FILE")
		}
		return nil
	default:
		return fmt.Errorf("unsupported tmux notify backend %q", c.Backend)
	}
}

func BuildMessage(event Event) string {
	window := strings.TrimSpace(event.WindowName)
	if window == "" {
		window = strings.TrimSpace(event.WindowIndex)
	}
	if strings.TrimSpace(event.WindowIndex) != "" && strings.TrimSpace(event.WindowName) != "" {
		window = event.WindowIndex + ":" + event.WindowName
	}
	session := strings.TrimSpace(event.Session)
	if session == "" {
		return "tmux bell"
	}
	if window == "" {
		return "tmux bell: " + session
	}
	return "tmux bell: " + session + " / " + window
}

func Notify(config Config, event Event) error {
	config = config.Normalize()
	if err := config.Validate(); err != nil {
		return err
	}
	event.Message = firstNonEmpty(strings.TrimSpace(event.Message), BuildMessage(event))
	event.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	allowed, err := allowEvent(config, event, time.Now())
	if err != nil {
		return err
	}
	if !allowed {
		return nil
	}
	switch config.Backend {
	case BackendFile:
		return notifyFile(config.FilePath, event)
	case BackendWindowsNotify:
		return notifyWindows(event)
	default:
		return fmt.Errorf("unsupported tmux notify backend %q", config.Backend)
	}
}

func HookCommand(exe, project string, config Config) string {
	config = config.Normalize()
	args := []string{
		exe,
		"-p", project,
		"tmux-notify-bell",
		"--backend", config.Backend,
		"--debounce-ms", strconv.Itoa(config.DebounceMS),
	}
	if strings.TrimSpace(config.FilePath) != "" {
		args = append(args, "--file", config.FilePath)
	}
	args = append(args,
		"--session", "#{session_name}",
		"--window-index", "#{window_index}",
		"--window-name", "#{window_name}",
		"--pane-id", "#{pane_id}",
		"--pane-title", "#{pane_title}",
	)
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func PowerShellCommand(event Event) ([]string, error) {
	powershell, err := resolvePowerShell()
	if err != nil {
		return nil, err
	}
	title := "devkit tmux bell"
	text := event.Message
	if strings.TrimSpace(text) == "" {
		text = BuildMessage(event)
	}
	script := strings.Join([]string{
		"Add-Type -AssemblyName System.Windows.Forms",
		"Add-Type -AssemblyName System.Drawing",
		"$n = New-Object System.Windows.Forms.NotifyIcon",
		"$n.Icon = [System.Drawing.SystemIcons]::Information",
		"$n.Visible = $true",
		"$n.BalloonTipTitle = '" + powerShellSingleQuote(title) + "'",
		"$n.BalloonTipText = '" + powerShellSingleQuote(text) + "'",
		"$n.ShowBalloonTip(5000)",
		"Start-Sleep -Milliseconds 5500",
		"$n.Dispose()",
	}, "; ")
	return []string{
		powershell,
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-Command", script,
	}, nil
}

func notifyWindows(event Event) error {
	args, err := PowerShellCommand(event)
	if err != nil {
		return err
	}
	cmd := exec.Command(args[0], args[1:]...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("windows notification failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func notifyFile(path string, event Event) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create notify file directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open notify file: %w", err)
	}
	defer f.Close()
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal notify event: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write notify file: %w", err)
	}
	return nil
}

func allowEvent(config Config, event Event, now time.Time) (bool, error) {
	if config.DebounceMS <= 0 {
		return true, nil
	}
	scopeHash := sha256.Sum256([]byte(config.Backend + "\x00" + config.FilePath))
	keyPath := filepath.Join(os.TempDir(), "devkit-tmux-notify", fmt.Sprintf("%x", scopeHash[:8])+"__"+sanitize(event.Session)+"__"+sanitize(event.WindowIndex)+"__"+sanitize(event.WindowName)+".stamp")
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o755); err != nil {
		return false, fmt.Errorf("create debounce directory: %w", err)
	}
	if data, err := os.ReadFile(keyPath); err == nil {
		if nanos, parseErr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); parseErr == nil {
			last := time.Unix(0, nanos)
			if now.Sub(last) < time.Duration(config.DebounceMS)*time.Millisecond {
				return false, nil
			}
		}
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("read debounce state: %w", err)
	}
	data := []byte(strconv.FormatInt(now.UnixNano(), 10))
	if err := os.WriteFile(keyPath, data, 0o644); err != nil {
		return false, fmt.Errorf("write debounce state: %w", err)
	}
	return true, nil
}

func resolvePowerShell() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("DEVKIT_POWERSHELL_PATH")); configured != "" {
		if _, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("DEVKIT_POWERSHELL_PATH %s not accessible: %w", configured, err)
		}
		return configured, nil
	}
	for _, candidate := range []string{
		"powershell.exe",
		"/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe",
		"/mnt/c/Windows/system32/WindowsPowerShell/v1.0/powershell.exe",
	} {
		if filepath.IsAbs(candidate) {
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
			continue
		}
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("powershell.exe not found. Set DEVKIT_POWERSHELL_PATH to the full powershell.exe path")
}

func powerShellSingleQuote(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func sanitize(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = "default"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return '_'
		}
	}, value)
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

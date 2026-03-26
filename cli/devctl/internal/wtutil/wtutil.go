package wtutil

import (
	"os"
	"path/filepath"
	"strings"
)

// TabSpec describes one Windows Terminal tab to launch.
type TabSpec struct {
	Title   string
	Command string
}

// ViewerWindowName returns the stable Windows Terminal window name for a tmux session.
func ViewerWindowName(session string) string {
	return "devkit-wt-" + sanitize(session)
}

// ViewerLockPath returns the session-specific lock path used to prevent duplicate viewers.
func ViewerLockPath(session string) string {
	return filepath.Join(os.TempDir(), "devkit-wt", sanitize(session)+".lock")
}

// NewTabArgs builds args for a single `wt` invocation that opens one tab in the named window via WSL.
func NewTabArgs(windowName, wslPath, distro string, tab TabSpec) []string {
	windowsWSLPath := windowsPath(wslPath)
	return []string{
		"-w", windowName,
		"new-tab",
		"--title", tab.Title,
		"--",
		windowsWSLPath,
		"-d", distro,
		"zsh", "-lic", tab.Command,
	}
}

func windowsPath(path string) string {
	value := strings.TrimSpace(path)
	lower := strings.ToLower(value)
	const prefix = "/mnt/"
	if !strings.HasPrefix(lower, prefix) || len(value) < len(prefix)+2 {
		return value
	}
	drive := value[len(prefix)]
	if value[len(prefix)+1] != '/' {
		return value
	}
	rest := value[len(prefix)+2:]
	if rest == "" {
		return value
	}
	rest = strings.ReplaceAll(rest, "/", `\`)
	if drive >= 'a' && drive <= 'z' {
		drive = drive - 'a' + 'A'
	}
	return string(drive) + `:\` + rest
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

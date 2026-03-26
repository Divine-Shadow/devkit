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

// NewTabArgs builds args for a single `wt` invocation that opens one tab in the named window.
func NewTabArgs(windowName string, tab TabSpec) []string {
	return []string{
		"-w", windowName,
		"new-tab",
		"--title", tab.Title,
		"bash", "-lc", tab.Command,
	}
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

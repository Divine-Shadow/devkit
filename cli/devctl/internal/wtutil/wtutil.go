package wtutil

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TabSpec describes one Windows Terminal tab to launch.
type TabSpec struct {
	Title   string
	Command string
}

// ViewerLock captures the metadata needed to recover a WT viewer session.
type ViewerLock struct {
	Version        int    `json:"version,omitempty"`
	Project        string `json:"project,omitempty"`
	Session        string `json:"session"`
	ReleaseCommand string `json:"release_command,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
}

// ViewerWindowName returns the stable Windows Terminal window name for a tmux session.
func ViewerWindowName(session string) string {
	return "devkit-wt-" + sanitize(session)
}

// ViewerLockPath returns the session-specific lock path used to prevent duplicate viewers.
func ViewerLockPath(session string) string {
	return filepath.Join(os.TempDir(), "devkit-wt", sanitize(session)+".lock")
}

// DefaultReleaseCommand renders the canonical wrapper invocation for wt-release.
func DefaultReleaseCommand(exePath, project, session string) string {
	exe := strings.TrimSpace(exePath)
	if exe == "" {
		exe = "devkit/kit/scripts/devkit"
	}
	cmd := []string{exe}
	if strings.TrimSpace(project) != "" {
		cmd = append(cmd, "-p", project)
	} else {
		cmd = append(cmd, "-p", "<project>")
	}
	cmd = append(cmd, "wt-release", "--session", session)
	return strings.Join(cmd, " ")
}

// NewViewerLock returns normalized lock metadata for the given project/session pair.
func NewViewerLock(exePath, project, session string) ViewerLock {
	return ViewerLock{
		Version:        1,
		Project:        strings.TrimSpace(project),
		Session:        strings.TrimSpace(session),
		ReleaseCommand: DefaultReleaseCommand(exePath, project, session),
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
}

// ReadViewerLock reads structured or legacy WT viewer lock content from disk.
func ReadViewerLock(path string) (ViewerLock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ViewerLock{}, err
	}
	return ParseViewerLock(data)
}

// ParseViewerLock decodes a structured JSON lock file or the legacy session-only format.
func ParseViewerLock(data []byte) (ViewerLock, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return ViewerLock{}, fmt.Errorf("wt viewer lock is empty")
	}

	var lock ViewerLock
	if err := json.Unmarshal([]byte(trimmed), &lock); err == nil && strings.TrimSpace(lock.Session) != "" {
		lock.Project = strings.TrimSpace(lock.Project)
		lock.Session = strings.TrimSpace(lock.Session)
		lock.ReleaseCommand = strings.TrimSpace(lock.ReleaseCommand)
		lock.CreatedAt = strings.TrimSpace(lock.CreatedAt)
		return lock, nil
	}

	return ViewerLock{Session: trimmed}, nil
}

// WriteViewerLock persists structured WT viewer lock metadata.
func WriteViewerLock(path string, lock ViewerLock) error {
	if strings.TrimSpace(lock.Session) == "" {
		return fmt.Errorf("wt viewer lock session is required")
	}
	payload, err := json.Marshal(lock)
	if err != nil {
		return fmt.Errorf("marshal wt viewer lock: %w", err)
	}
	return os.WriteFile(path, payload, 0o644)
}

// NewTabArgs builds args for a single `wt` invocation that opens one tab in the named window via WSL.
func NewTabArgs(windowName, wslPath, distro string, tab TabSpec) []string {
	return NewTabsArgs(windowName, wslPath, distro, []TabSpec{tab})
}

// NewTabsArgs builds args for a single `wt` invocation that opens multiple tabs
// in the named window via WSL, preserving the provided order.
func NewTabsArgs(windowName, wslPath, distro string, tabs []TabSpec) []string {
	windowsWSLPath := windowsPath(wslPath)
	args := []string{"-w", windowName}
	for i, tab := range tabs {
		if i > 0 {
			args = append(args, ";")
		}
		args = append(args,
			"new-tab",
			"--title", tab.Title,
			"--",
			windowsWSLPath,
			"-d", distro,
			"zsh", "-lic", tab.Command,
		)
	}
	return args
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

package agentexec

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

type NativeCommandOpts struct {
	Exe     string
	Project string
	Index   string
	Repo    string
	Dest    string
}

func BuildNativeCommand(opts NativeCommandOpts) (string, error) {
	exe := strings.TrimSpace(opts.Exe)
	if exe == "" {
		exe = "devkit"
	}
	project := strings.TrimSpace(opts.Project)
	if project == "" {
		project = "dev-all"
	}
	idx := strings.TrimSpace(opts.Index)
	if idx == "" {
		idx = "1"
	}
	idxInt, err := strconv.Atoi(idx)
	if err != nil || idxInt < 1 {
		idx = "1"
		idxInt = 1
	}
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		repo = nativeRepoFromDest(opts.Dest)
	}
	if repo == "" {
		repo = "ouroboros-ide"
	}
	dest := nativeDest(project, idxInt, repo, opts.Dest)
	shell := "set -e; cd " + shSingleQuote(dest) + "; if command -v zsh >/dev/null 2>&1; then exec zsh -i; fi; exec bash"
	return strings.Join([]string{
		shSingleQuote(exe),
		"-p", shSingleQuote(project),
		"exec", idx,
		"--repo", shSingleQuote(repo),
		"--", "bash", "-lc", shSingleQuote(shell),
	}, " "), nil
}

func nativeDest(project string, index int, repo string, dest string) string {
	if index < 1 {
		index = 1
	}
	repo = strings.TrimSpace(repo)
	if repo == "" {
		repo = "ouroboros-ide"
	}
	base := nativeBase(project, index, repo)
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return base
	}
	if !strings.HasPrefix(dest, "/") {
		parts := strings.Split(filepath.Clean(dest), string(filepath.Separator))
		if len(parts) >= 3 && parts[0] == "agent-worktrees" && strings.HasPrefix(parts[1], "agent") && parts[2] == repo {
			target := filepath.Join("/workspaces/dev/agent-worktrees", parts[1], repo)
			if len(parts) == 3 {
				return target
			}
			return filepath.Join(append([]string{target}, parts[3:]...)...)
		}
		if len(parts) >= 1 && parts[0] == repo {
			if len(parts) == 1 {
				return base
			}
			return filepath.Join(append([]string{base}, parts[1:]...)...)
		}
		return filepath.Join(base, dest)
	}
	normalized := filepath.Clean(dest)
	if strings.HasPrefix(normalized, "/worktrees/") {
		return normalized
	}
	devPrefix := "/workspaces/dev/"
	if strings.HasPrefix(normalized, devPrefix) {
		rel := strings.TrimPrefix(normalized, devPrefix)
		parts := strings.Split(rel, "/")
		if len(parts) >= 3 && parts[0] == "agent-worktrees" && strings.HasPrefix(parts[1], "agent") {
			return normalized
		}
		if len(parts) >= 1 && parts[0] == repo {
			if len(parts) == 1 {
				return base
			}
			return filepath.Join(append([]string{base}, parts[1:]...)...)
		}
	}
	return normalized
}

func nativeBase(project string, index int, repo string) string {
	if strings.TrimSpace(project) == "dev-all" && index == 1 {
		return filepath.Join("/workspaces/dev", repo)
	}
	if strings.TrimSpace(project) == "dev-all" {
		return filepath.Join("/workspaces/dev/agent-worktrees", fmt.Sprintf("agent%d", index), repo)
	}
	return filepath.Join("/worktrees", fmt.Sprintf("agent%d", index), repo)
}

func nativeRepoFromDest(dest string) string {
	dest = filepath.Clean(strings.TrimSpace(dest))
	if dest == "." || dest == "" {
		return ""
	}
	for _, prefix := range []string{"/worktrees/", "/workspaces/dev/agent-worktrees/"} {
		if strings.HasPrefix(dest, prefix) {
			rel := strings.TrimPrefix(dest, prefix)
			parts := strings.Split(rel, "/")
			if len(parts) >= 2 && strings.HasPrefix(parts[0], "agent") {
				return parts[1]
			}
		}
	}
	if strings.HasPrefix(dest, "/workspaces/dev/") {
		rel := strings.TrimPrefix(dest, "/workspaces/dev/")
		parts := strings.Split(rel, "/")
		if len(parts) >= 1 {
			return parts[0]
		}
	}
	return ""
}

// shSingleQuote wraps s in POSIX-safe single quotes.
func shSingleQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

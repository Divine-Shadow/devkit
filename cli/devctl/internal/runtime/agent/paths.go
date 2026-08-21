package agent

import (
	"fmt"
	"path/filepath"
	"strings"

	pth "devkit/cli/devctl/internal/paths"
)

type PathConfig struct {
	DevkitRoot            string
	HostRoot              string
	Project               string
	Repo                  string
	Index                 int
	WorktreeRoot          string
	StateRoot             string
	WorktreeContainerRoot string
	StateContainerRoot    string
	AgentStatePrefix      string
	DedicatedWorktree     bool
}

type Paths struct {
	DevRoot               string
	HostWorktreeRoot      string
	HostStateRoot         string
	HostWorktree          string
	HostAgentStateRoot    string
	HostHome              string
	SandboxWorktreeRoot   string
	SandboxStateRoot      string
	SandboxWorktree       string
	SandboxAgentStateRoot string
	SandboxHome           string
}

func ResolvePaths(cfg PathConfig) (Paths, error) {
	devkitRoot := strings.TrimSpace(cfg.DevkitRoot)
	if devkitRoot == "" {
		return Paths{}, fmt.Errorf("devkit root is required")
	}
	devkitRoot = filepath.Clean(devkitRoot)
	devRoot := filepath.Clean(filepath.Join(devkitRoot, ".."))
	if hostRoot := strings.TrimSpace(cfg.HostRoot); hostRoot != "" {
		if !filepath.IsAbs(hostRoot) {
			return Paths{}, fmt.Errorf("native host root must be absolute")
		}
		devRoot = filepath.Clean(hostRoot)
	}
	project := strings.TrimSpace(cfg.Project)
	if project == "" {
		project = "dev-all"
	}
	repo := strings.TrimSpace(cfg.Repo)
	if repo == "" {
		repo = "ouroboros-ide"
	}
	workspaceRoot := IsWorkspaceRootRepo(repo)
	index := cfg.Index
	if index < 1 {
		index = 1
	}

	hostWorktreeRoot := resolveHostPath(cfg.WorktreeRoot, devkitRoot, filepath.Join(devRoot, pth.AgentWorktreesDir))
	hostStateRoot := resolveHostPath(cfg.StateRoot, devkitRoot, filepath.Join(devRoot, ".devkit", "native-agents"))
	sandboxWorktreeRoot := cleanContainerRoot(cfg.WorktreeContainerRoot, "/worktrees")
	sandboxStateRoot := cleanContainerRoot(cfg.StateContainerRoot, "/agent-state")

	agentDir := fmt.Sprintf("agent%d", index)
	agentStatePrefix := strings.TrimSpace(cfg.AgentStatePrefix)
	if agentStatePrefix == "" {
		agentStatePrefix = project
	}
	if agentStatePrefix == "." || agentStatePrefix == ".." || filepath.Base(agentStatePrefix) != agentStatePrefix {
		return Paths{}, fmt.Errorf("native agent state prefix must be one path component: %q", cfg.AgentStatePrefix)
	}
	agentName := fmt.Sprintf("%s-agent%d", agentStatePrefix, index)

	hostWorktree := filepath.Join(devRoot, repo)
	sandboxWorktree := filepath.Join("/workspaces/dev", repo)
	if workspaceRoot {
		hostWorktree = devRoot
		sandboxWorktree = "/workspaces/dev"
	} else if cfg.DedicatedWorktree || index > 1 {
		hostWorktree = filepath.Join(hostWorktreeRoot, agentDir, repo)
		sandboxWorktree = filepath.Join(sandboxWorktreeRoot, agentDir, repo)
	}

	hostAgentStateRoot := filepath.Join(hostStateRoot, agentName)
	sandboxAgentStateRoot := filepath.Join(sandboxStateRoot, agentName)
	hostHome := filepath.Join(hostAgentStateRoot, "home")
	sandboxHome := filepath.Join(sandboxAgentStateRoot, "home")
	if project == "dev-all" && !workspaceRoot {
		suffix := fmt.Sprintf(".devhome-agent%d", index)
		if index == 1 {
			hostHome = filepath.Join(hostWorktree, suffix)
			sandboxHome = filepath.Join(sandboxWorktree, suffix)
		} else {
			hostHome = filepath.Join(filepath.Dir(hostWorktree), suffix)
			sandboxHome = filepath.Join(filepath.Dir(sandboxWorktree), suffix)
		}
	}
	return Paths{
		DevRoot:               devRoot,
		HostWorktreeRoot:      hostWorktreeRoot,
		HostStateRoot:         hostStateRoot,
		HostWorktree:          hostWorktree,
		HostAgentStateRoot:    hostAgentStateRoot,
		HostHome:              hostHome,
		SandboxWorktreeRoot:   sandboxWorktreeRoot,
		SandboxStateRoot:      sandboxStateRoot,
		SandboxWorktree:       sandboxWorktree,
		SandboxAgentStateRoot: sandboxAgentStateRoot,
		SandboxHome:           sandboxHome,
	}, nil
}

func IsWorkspaceRootRepo(repo string) bool {
	repo = filepath.Clean(strings.TrimSpace(repo))
	return repo == "." || repo == string(filepath.Separator)
}

func resolveHostPath(value, devkitRoot, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return filepath.Clean(fallback)
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(devkitRoot, value))
}

func cleanContainerRoot(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join("/", value)
	}
	return filepath.Clean(value)
}

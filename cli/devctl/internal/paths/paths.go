package paths

import (
    "path/filepath"
)

// AgentRepoPath returns the working directory path inside the container
// for a given project overlay, agent index, and repo name.
// - dev-all: /workspaces/dev/<repo> or /workspaces/dev/agentN/<repo>
// - codex  : /workspace
func AgentRepoPath(project, idx, repo string) string {
    if project == "dev-all" {
        base := "/workspaces/dev"
        if idx == "1" {
            return filepath.Join(base, repo)
        }
        return filepath.Join(base, "agent"+idx, repo)
    }
    // codex overlay (single mount at /workspace)
    return "/workspace"
}

// AgentHomePath returns the per-agent HOME inside the container for a project/index/repo.
// - dev-all: /workspaces/dev/.devhomes/agentN (safe: outside any repo to avoid accidental commits)
// - codex  : /workspace/.devhome-agentN
func AgentHomePath(project, idx, repo string) string {
    if project == "dev-all" {
        return filepath.Join("/workspaces/dev", ".devhomes", "agent"+idx)
    }
    return filepath.Join("/workspace", ".devhome-agent"+idx)
}

// AgentEnv returns HOME and related XDG/Codex variables for the agent.
func AgentEnv(project, idx, repo string) map[string]string {
    home := AgentHomePath(project, idx, repo)
    return map[string]string{
        "HOME":             home,
        "CODEX_HOME":       filepath.Join(home, ".codex"),
        "CODEX_ROLLOUT_DIR": filepath.Join(home, ".codex", "rollouts"),
        "XDG_CACHE_HOME":   filepath.Join(home, ".cache"),
        "XDG_CONFIG_HOME":  filepath.Join(home, ".config"),
    }
}

package launch

import (
	"fmt"
	"path/filepath"
	"strings"

	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

// Git consumes the current execution's connector through its environment,
// ahead of any saved core.sshCommand. Constructing this command does not open
// or change the existing SSH configuration, identities, or credential homes.
func managedRuntimeGitSSHCommand(p nativeplan.Plan) (string, error) {
	if p.IsolationProfile != nativeplan.IsolationProfileWorkspaceEgress || strings.TrimSpace(p.Proxy.AllowlistPath) == "" {
		return "", nil
	}
	for _, key := range []string{"GIT_SSH_COMMAND", "GIT_SSH_VARIANT"} {
		if _, supplied := p.Env[key]; supplied {
			return "", fmt.Errorf("managed native Git transport refuses a caller %s override", key)
		}
	}
	home := strings.TrimSpace(p.Agent.SandboxHome)
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("managed native Git transport requires an absolute sandbox home")
	}
	proxyCommand, err := gitManagedProxyCommand(p)
	if err != nil {
		return "", err
	}
	authority, err := resolvePackageSSHAuthority()
	if err != nil {
		return "", err
	}
	standard, err := standardManagedGitPlan(p)
	if err != nil {
		return "", err
	}
	var command string
	if standard {
		for _, key := range []string{"DEVKIT_MANAGED_GIT_HOME", "DEVKIT_MANAGED_GIT_KNOWN_HOSTS"} {
			if _, supplied := p.Env[key]; supplied {
				return "", fmt.Errorf("managed native Git transport refuses a caller %s override", key)
			}
		}
		if callerHome, supplied := p.Env["HOME"]; supplied && callerHome != home {
			return "", fmt.Errorf("managed native Git transport refuses a caller HOME override")
		}
		command, err = authority.ManagedGitCommand(home)
	} else {
		command, err = authority.Command(filepath.Join(home, ".ssh", "config"))
	}
	if err != nil {
		return "", err
	}
	// Every execution retains its current connector. Standard Product GitHub
	// paths/trust use immutable policy; other hosts retain saved public policy.
	// Nonstandard native classes retain their existing SSH selection.
	command += " -o " + shellQuote("ProxyCommand="+proxyCommand)
	return command, nil
}

// This is a policy consumer, not new target admission. Only the already
// source-selected standard Product class receives the canonical GitHub policy.
func standardManagedGitPlan(p nativeplan.Plan) (bool, error) {
	if p.GUITargetConfig == nil ||
		(p.GUITargetConfig.Kind != "local-wsl-devkit-agent" && p.GUITargetConfig.Kind != "devkit-agent") ||
		p.Agent.ID.Project != "dev-all" || p.Agent.ID.Repo != "ouroboros-ide" {
		return false, nil
	}
	index := p.Agent.ID.Index
	workspace := fmt.Sprintf("/home/bayesartre/dev/agent-worktrees/agent%d", index)
	sandbox := "/workspaces/dev"
	repo := workspace + "/ouroboros-ide"
	homeName := fmt.Sprintf(".devhome-agent%d", index)
	hostHomeRoot, sandboxHomeRoot := workspace, sandbox
	if index == 1 {
		hostHomeRoot, sandboxHomeRoot = repo, sandbox+"/ouroboros-ide"
	}
	if index < 1 || p.GUITargetConfig.ExpectedExecutionHost == "" ||
		p.HostWorkspaceRoot != workspace || p.SandboxWorkspaceRoot != sandbox ||
		p.Agent.HostWorktree != repo || p.Agent.HostHome != hostHomeRoot+"/"+homeName ||
		p.Agent.SandboxWorktree != sandbox+"/ouroboros-ide" ||
		p.Agent.SandboxHome != sandboxHomeRoot+"/"+homeName {
		return false, fmt.Errorf("managed Git SSH policy plan differs from the source-selected standard native geometry")
	}
	return true, nil
}

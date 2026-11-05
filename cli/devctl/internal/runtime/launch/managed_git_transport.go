package launch

import (
	"fmt"
	"path/filepath"
	"strings"

	nativeagent "devkit/cli/devctl/internal/runtime/agent"
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
	// The admitted plan owns an independent state root; a canonical worktree
	// does not imply that physical retained storage lives below the same root.
	slotState := filepath.Join(p.HostStateRoot, fmt.Sprintf("dev-all-agent%d", index))
	hostHome := filepath.Join(nativeagent.PersistentHomesRoot(p.HostStateRoot), filepath.Base(slotState), "ouroboros-ide")
	sandboxHomeRoot := sandbox
	if index == 1 {
		sandboxHomeRoot = sandbox + "/ouroboros-ide"
	}
	if index < 1 || p.GUITargetConfig.ExpectedExecutionHost == "" ||
		!filepath.IsAbs(p.HostStateRoot) || filepath.Clean(p.HostStateRoot) != p.HostStateRoot ||
		p.Agent.StateRoot != slotState ||
		p.HostWorkspaceRoot != workspace || p.SandboxWorkspaceRoot != sandbox ||
		p.Agent.HostWorktree != repo || p.Agent.HostHome != hostHome ||
		p.Agent.SandboxWorktree != sandbox+"/ouroboros-ide" ||
		p.Agent.SandboxHome != sandboxHomeRoot+"/"+homeName {
		// Independent native allocations are admitted by the same fixed immutable
		// GUI projection, not by a caller-selected path or a different SSH class.
		// Reselect immediately before the bootstrap consumes the plan.
		if _, err := nativeplan.ReadValidatedGUITargetConfigSource(p); err != nil {
			return false, fmt.Errorf("managed Git SSH policy plan differs from the source-selected standard native geometry: %w", err)
		}
		if err := sourceSelectedManagedGitGeometry(p); err != nil {
			return false, fmt.Errorf("managed Git SSH policy plan differs from the source-selected standard native geometry: %w", err)
		}
	}
	return true, nil
}

// Source selection binds the physical workspace and home. These relationships
// additionally retain the normal lane/state and the two existing constructor
// projections; source admission does not authorize arbitrary sandbox aliases.
func sourceSelectedManagedGitGeometry(p nativeplan.Plan) error {
	index := p.Agent.ID.Index
	workspace := p.HostWorkspaceRoot
	if workspace == "" {
		workspace = filepath.Dir(p.Agent.HostWorktree)
	}
	if index < 1 || p.GUITargetConfig == nil || p.GUITargetConfig.ExpectedExecutionHost == "" || !filepath.IsAbs(workspace) || filepath.Clean(workspace) != workspace ||
		filepath.Base(workspace) != fmt.Sprintf("agent%d", index) ||
		filepath.Base(filepath.Dir(workspace)) != "agent-worktrees" ||
		p.Agent.HostWorktree != filepath.Join(workspace, "ouroboros-ide") ||
		!filepath.IsAbs(p.HostStateRoot) || filepath.Clean(p.HostStateRoot) != p.HostStateRoot {
		return fmt.Errorf("invalid independent workspace or state root")
	}
	slot := fmt.Sprintf("dev-all-agent%d", index)
	if p.Agent.StateRoot != filepath.Join(p.HostStateRoot, slot) ||
		p.Agent.HostHome != filepath.Join(nativeagent.PersistentHomesRoot(p.HostStateRoot), slot, "ouroboros-ide") {
		return fmt.Errorf("independent lane state or persistent home mismatch")
	}
	sandboxWorktree := "/workspaces/dev/ouroboros-ide"
	if p.HostWorkspaceRoot == "" {
		if p.SandboxWorkspaceRoot != "" {
			return fmt.Errorf("generic native plan has a foreign workspace projection")
		}
		sandboxWorktree = fmt.Sprintf("/workspaces/dev/agent-worktrees/agent%d/ouroboros-ide", index)
	} else if p.SandboxWorkspaceRoot != "/workspaces/dev" {
		return fmt.Errorf("GUI native plan has a foreign workspace projection")
	}
	if p.Agent.SandboxWorktree != sandboxWorktree ||
		p.Agent.SandboxHome != nativeagent.PersistentDevAllHome(sandboxWorktree, index) {
		return fmt.Errorf("independent sandbox worktree or home mismatch")
	}
	return nil
}

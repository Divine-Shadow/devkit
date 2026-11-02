package plan

import (
	"fmt"
	"strings"

	"devkit/cli/devctl/internal/runtime/agent"
)

// selectGUITargetConfig keeps reset observation and actual launch on the same
// immutable, exact-geometry selection. It does not inspect disposable Git data.
func selectGUITargetConfig(opts BuildOptions, geometry guiTargetGeometry) (*GUITargetConfigProjection, error) {
	if opts.GUITargetID != "" && opts.RequireGUIConfig {
		return nil, fmt.Errorf("GUI target selection cannot combine an explicit target ID with required geometry selection")
	}
	if opts.GUITargetID != "" {
		guiTargetID := strings.TrimSpace(opts.GUITargetID)
		if guiTargetID == "" || guiTargetID != opts.GUITargetID {
			return nil, fmt.Errorf("GUI target ID must be non-empty and canonical")
		}
		projection, err := loadGUITargetConfigProjection(guiTargetID, geometry)
		if err != nil {
			return nil, err
		}
		return &projection, nil
	} else if opts.RequireGUIConfig {
		projection, err := loadUniqueGUITargetConfigProjectionForGeometry(geometry)
		if err != nil {
			return nil, err
		}
		return &projection, nil
	}
	return nil, nil
}

// ObserveNativeResetSlotGeometry supplies only the source-declared process and
// history boundary for a dev-all reset. It cannot construct a runnable plan or
// admit the old Git metadata that reconstruction will discard. Actual execution
// continues through Build and its full bind/backlink validation.
func ObserveNativeResetSlotGeometry(opts BuildOptions) (agent.Spec, error) {
	if strings.TrimSpace(opts.Project) != "dev-all" || opts.Index < 1 {
		return agent.Spec{}, fmt.Errorf("native reset geometry requires dev-all and a positive source slot")
	}
	project, repo, index, paths, workspaceRoot, err := resolvePlanGeometry(opts)
	if err != nil {
		return agent.Spec{}, err
	}
	if agent.IsWorkspaceRootRepo(repo) {
		return agent.Spec{}, fmt.Errorf("native reset geometry requires a dedicated repository")
	}
	isolation := normalizeIsolationProfile(opts.IsolationProfile)
	if isolation != "" && isolation != IsolationProfileWorkspaceEgress {
		return agent.Spec{}, fmt.Errorf("unknown isolation profile %q", opts.IsolationProfile)
	}
	if isolation == "" && strings.TrimSpace(opts.EgressAllowlist) != "" {
		return agent.Spec{}, fmt.Errorf("egress allowlist requires an isolation profile")
	}
	if workspaceRoot != "" && isolation != IsolationProfileWorkspaceEgress {
		return agent.Spec{}, fmt.Errorf("Management workspace root requires workspace-egress isolation")
	}
	if isolation == IsolationProfileWorkspaceEgress && strings.TrimSpace(opts.EgressAllowlist) == "" {
		return agent.Spec{}, fmt.Errorf("workspace-egress isolation requires an egress allowlist")
	}
	spec := agent.Spec{
		ID:           agent.ID{Project: project, Repo: repo, Index: index},
		HostWorktree: paths.HostWorktree, SandboxWorktree: paths.SandboxWorktree,
		HostHome: paths.HostHome, SandboxHome: paths.SandboxHome,
		StateRoot: paths.HostAgentStateRoot, SandboxStateRoot: paths.SandboxAgentStateRoot,
	}
	_, err = selectGUITargetConfig(opts, guiTargetGeometryForPlan(Plan{Agent: spec, HostWorkspaceRoot: workspaceRoot}))
	if err != nil {
		return agent.Spec{}, err
	}
	return spec, nil
}

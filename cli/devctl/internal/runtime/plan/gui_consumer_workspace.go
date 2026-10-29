package plan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// GUIConsumerWorkspace is geometry of the existing consumer, not a launch plan.
type GUIConsumerWorkspace struct {
	TargetID string `json:"targetId"`
	Worktree string `json:"worktree"`
	Home     string `json:"home"`
}

// ResolveGUIConsumerWorkspace derives logical geometry from the same fixed
// immutable projection used at launch. No caller path can replace that input.
func ResolveGUIConsumerWorkspace(project, repo, targetID string, index int) (GUIConsumerWorkspace, error) {
	return resolveGUIConsumerWorkspaceFrom(guiCodexConfigProjectionManifestPath, guiCodexConfigProjectionStoreRoot, project, repo, targetID, index)
}

func resolveGUIConsumerWorkspaceFrom(manifestPath, storeRoot, project, repo, targetID string, index int) (GUIConsumerWorkspace, error) {
	var empty GUIConsumerWorkspace
	if project != "dev-all" || repo != "ouroboros-ide" || index < 1 || targetID == "" {
		return empty, fmt.Errorf("current-worktree readiness requires a declared dev-all Product GUI consumer")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return empty, err
	}
	var manifest guiCodexConfigProjectionManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return empty, err
	}
	for _, record := range manifest.Projections {
		if record.TargetID != targetID {
			continue
		}
		geometry := guiTargetGeometry{Project: project, Repo: repo, AgentIndex: index,
			WorkspaceRoot: record.WorkspaceRoot, HostWorktree: record.HostWorktree, HostHome: record.HostHome}
		// Reuse strict schema/duplicate/source hash and geometry validation.
		if _, err := loadGUITargetConfigProjectionFrom(manifestPath, storeRoot, targetID, geometry); err != nil {
			return empty, err
		}
		if record.Kind != "devkit-agent" && record.Kind != "local-wsl-devkit-agent" {
			return empty, fmt.Errorf("current-worktree readiness requires a Product GUI target")
		}
		if !filepath.IsAbs(record.WorkspaceRoot) || filepath.Clean(record.WorkspaceRoot) != record.WorkspaceRoot ||
			filepath.Dir(record.HostWorktree) != record.WorkspaceRoot || filepath.Base(record.HostWorktree) != repo {
			return empty, fmt.Errorf("GUI consumer workspace geometry is not canonical")
		}
		worktree, ok := projectSandboxPath(record.HostWorktree, record.WorkspaceRoot, "/workspaces/dev")
		if !ok || worktree != filepath.Join("/workspaces/dev", repo) {
			return empty, fmt.Errorf("GUI consumer worktree is not the declared v4 projection")
		}
		home, ok := projectSandboxPath(record.HostHome, record.WorkspaceRoot, "/workspaces/dev")
		if !ok {
			return empty, fmt.Errorf("GUI consumer home is outside its declared projection")
		}
		return GUIConsumerWorkspace{TargetID: targetID, Worktree: worktree, Home: home}, nil
	}
	return empty, fmt.Errorf("GUI consumer target %q is not declared", targetID)
}

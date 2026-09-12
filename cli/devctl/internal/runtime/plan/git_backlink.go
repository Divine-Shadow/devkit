package plan

import (
	"fmt"
	"os"
	"path/filepath"

	"devkit/cli/devctl/internal/worktrees"
)

// GitBacklinkProjection adapts only the native view of a validated host
// registration. The common repository, index, HEAD and refs remain shared.
type GitBacklinkProjection struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Value  string `json:"value"`
}

func gitBacklinkProjection(p Plan) (*GitBacklinkProjection, error) {
	if p.HostWorkspaceRoot == "" || p.Agent.ID.Project != "dev-all" || p.Agent.ID.Repo != "ouroboros-ide" {
		return nil, nil
	}
	info, err := os.Lstat(filepath.Join(p.Agent.HostWorktree, ".git"))
	if os.IsNotExist(err) {
		return nil, nil
	} // A plan may precede source-owned preparation.
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, nil
	} // Standalone repositories have no reverse pointer.
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("native Git projection requires regular .git")
	}
	lane := fmt.Sprintf("agent%d", p.Agent.ID.Index)
	if p.HostWorkspaceRoot != filepath.Join(p.HostWorktreeRoot, lane) || p.Agent.HostWorktree != filepath.Join(p.HostWorkspaceRoot, p.Agent.ID.Repo) || p.Agent.SandboxWorktree != "/workspaces/dev/ouroboros-ide" || p.IsolationProfile != IsolationProfileWorkspaceEgress {
		return nil, fmt.Errorf("native Git projection requires exact isolated lane geometry")
	}
	metadata, err := worktrees.InspectNativeLaneGitMetadata(p.Agent.HostWorktree, p.HostWorktreeRoot, p.Agent.ID.Repo, p.Agent.ID.Index)
	if err != nil {
		return nil, err
	}
	relativeCommon, err := filepath.Rel(p.HostWorktreeRoot, metadata.CommonDir)
	if err != nil {
		return nil, err
	}
	nativeCommon := filepath.Join("/workspaces", relativeCommon)
	commonBind := Bind{Source: metadata.CommonDir, Target: nativeCommon, Mode: "rw", Required: true}
	count := 0
	for _, bind := range p.Binds {
		if bind.Target == nativeCommon {
			if bind != commonBind {
				return nil, fmt.Errorf("native Git projection has conflicting common repository bind")
			}
			count++
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("native Git projection requires one exact common repository bind")
	}
	relativeGitdir, err := filepath.Rel(metadata.CommonDir, metadata.GitDir)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(p.Agent.StateRoot) || filepath.Clean(p.Agent.StateRoot) != p.Agent.StateRoot {
		return nil, fmt.Errorf("native Git projection requires canonical selected state root")
	}
	return &GitBacklinkProjection{
		Source: filepath.Join(p.Agent.StateRoot, "native-git-backlink"),
		Target: filepath.Join(nativeCommon, relativeGitdir, "gitdir"),
		Value:  filepath.Join(p.Agent.SandboxWorktree, ".git") + "\n",
	}, nil
}

// ValidateGitBacklinkProjection re-derives the recipe before preparation and
// command construction. A removed or substituted recipe cannot bypass it.
func ValidateGitBacklinkProjection(p Plan) error {
	expected, err := gitBacklinkProjection(p)
	if err != nil {
		return err
	}
	if expected == nil {
		if p.GitBacklink != nil {
			return fmt.Errorf("unexpected native Git backlink projection")
		}
		return nil
	}
	if p.GitBacklink == nil || *p.GitBacklink != *expected {
		return fmt.Errorf("native Git backlink projection changed or missing")
	}
	required := Bind{Source: expected.Source, Target: expected.Target, Mode: "ro", Required: true}
	count := 0
	for i, bind := range p.Binds {
		if bind == required {
			count++
			// No later mount may obscure the protected read-only pointer.
			for _, later := range p.Binds[i+1:] {
				if pathWithinRoot(later.Target, expected.Target) || pathWithinRoot(expected.Target, later.Target) {
					return fmt.Errorf("native Git backlink projection is shadowed")
				}
			}
		} else if bind.Target == expected.Target || bind.Source == expected.Source {
			return fmt.Errorf("native Git backlink has conflicting mount")
		}
	}
	if count != 1 {
		return fmt.Errorf("native Git backlink requires one read-only bind")
	}
	return nil
}

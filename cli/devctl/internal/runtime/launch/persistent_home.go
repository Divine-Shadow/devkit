package launch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"devkit/cli/devctl/internal/runtime/agent"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

// ValidateNativePersistentHome is a read-only check before the first home
// mutation. Old data must not be silently replaced by a new empty home.
func ValidateNativePersistentHome(p nativeplan.Plan) error {
	if p.Agent.ID.Project != "dev-all" || agent.IsWorkspaceRootRepo(p.Agent.ID.Repo) {
		return nil
	}
	if p.HostStateRoot == "" {
		return fmt.Errorf("persistent native home requires its source-selected state root")
	}
	if filepath.Dir(p.Agent.StateRoot) != p.HostStateRoot {
		return fmt.Errorf("persistent native home requires its source-selected agent state root")
	}
	want := filepath.Join(agent.PersistentHomesRoot(p.HostStateRoot), filepath.Base(p.Agent.StateRoot), p.Agent.ID.Repo)
	if p.Agent.HostHome != want {
		return fmt.Errorf("native home %s is not the source-selected persistent location %s", p.Agent.HostHome, want)
	}
	if err := agent.RequireLegacyHomesAbsent(p.Agent.HostWorktree, p.Agent.StateRoot, p.Agent.ID.Index); err != nil {
		return err
	}
	for current := want; current != filepath.Dir(current); current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect persistent native home %s: %w", current, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("persistent native home traverses a symlink or non-directory: %s", current)
		}
	}
	return nil
}

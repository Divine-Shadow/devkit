package launch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"devkit/cli/devctl/internal/gitauthority"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

// Sandbox entry depends on its containment/configuration contract, not on a
// broker-issued identity or a warm host service. Host operations connect to the
// protected adapter only when called.
func validateManagementControllerProfilePlan(p nativeplan.Plan) error {
	_, err := managementControllerProfileForLaunch(p)
	return err
}

func managementControllerProfileForLaunch(p nativeplan.Plan) (nativeplan.ManagementControllerProfile, error) {
	var empty nativeplan.ManagementControllerProfile
	if err := validateControllerServiceOwnerClientPlan(p); err != nil {
		return empty, err
	}
	if strings.TrimSpace(p.ControllerProfile) == "" {
		return empty, nil
	}
	if p.ControllerProfile != nativeplan.ManagementControllerProfileIdentity ||
		p.ControllerManifest != nativeplan.ManagementControllerProfileManifestPath ||
		p.MountPolicyIdentity != nativeplan.ManagementControllerMountPolicyIdentity ||
		p.Env[nativeplan.ManagementControllerProfileEnvironment] != nativeplan.ManagementControllerProfileIdentity ||
		p.Env[nativeplan.ControllerOperationHandleEnvironment] != nativeplan.ControllerOperationHandleRequiredValue ||
		p.Env[nativeplan.WorkspaceControllerWorkLedgerEnvironment] != nativeplan.WorkspaceControllerWorkLedgerPath {
		return empty, fmt.Errorf("Management controller plan does not match the named v1 capability profile")
	}
	profile, err := nativeplan.LoadManagementControllerProfile(p.ControllerManifest)
	if err != nil {
		return empty, err
	}
	activeManagementRoot := profile.SourceRoots.Management
	activeManagementRoot.BackingPath = p.Agent.HostWorktree
	activeWSLNixRoot := profile.SourceRoots.WSLNix
	if strings.TrimSpace(p.HostWorkspaceRoot) != "" {
		activeWSLNixRoot.BackingPath = filepath.Join(filepath.Clean(p.HostWorkspaceRoot), "wsl-nix")
	}
	for _, expected := range []nativeplan.Bind{
		{Source: activeWSLNixRoot.BackingPath, Target: activeWSLNixRoot.LogicalPath, Mode: "rw", Required: true},
		{Source: nativeplan.ManagementControllerProfileManifestPath, Target: nativeplan.ManagementControllerProfileManifestPath, Mode: "ro", Required: true},
		{Source: nativeplan.WorkspaceControllerOperationSocket, Target: nativeplan.WorkspaceControllerOperationSocket, Mode: "ro", Required: false},
		{Source: nativeplan.WorkspaceControllerWorkLedgerDirectory, Target: nativeplan.WorkspaceControllerWorkLedgerDirectory, Mode: "rw", Required: true},
	} {
		if !planHasExactBind(p.Binds, expected) {
			return empty, fmt.Errorf("Management controller plan lacks exact required bind %+v", expected)
		}
	}
	if err := validateControllerSourceWorktree("active Management", activeManagementRoot); err != nil {
		return empty, err
	}
	if err := validateControllerSourceWorktree("WSL/Nix", activeWSLNixRoot); err != nil {
		return empty, err
	}
	return profile, nil
}

func planHasExactBind(binds []nativeplan.Bind, expected nativeplan.Bind) bool {
	for _, bind := range binds {
		if filepath.Clean(bind.Source) == filepath.Clean(expected.Source) &&
			filepath.Clean(bind.Target) == filepath.Clean(expected.Target) &&
			bind.Mode == expected.Mode && bind.Required == expected.Required {
			return true
		}
	}
	return false
}

func validateControllerSourceWorktree(label string, root nativeplan.ControllerProfileSourceRoot) error {
	info, err := os.Lstat(root.BackingPath)
	if err != nil {
		return fmt.Errorf("inspect %s controller source root %s: %w", label, root.BackingPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s controller source root %s must be a real non-symlink directory", label, root.BackingPath)
	}
	if err := requireGitWorktree(root.BackingPath); err != nil {
		return fmt.Errorf("%s controller source: %w", label, err)
	}
	for description, args := range map[string][]string{
		"HEAD":       {"rev-parse", "HEAD"},
		"origin URL": {"remote", "get-url", "origin"},
		"status":     {"status", "--porcelain=v1", "--untracked-files=all"},
	} {
		output, err := exec.Command(gitauthority.Executable(), append([]string{"-C", root.BackingPath}, args...)...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("read %s controller source %s: %w: %s", label, description, err, strings.TrimSpace(string(output)))
		}
		value := strings.TrimSpace(string(output))
		switch description {
		case "HEAD":
			if value != root.Revision {
				return fmt.Errorf("%s controller source HEAD = %q, want %q", label, value, root.Revision)
			}
		case "origin URL":
			if value != root.Origin {
				return fmt.Errorf("%s controller source origin = %q, want %q", label, value, root.Origin)
			}
		case "status":
			if value != "" {
				return fmt.Errorf("%s controller source root %s is dirty", label, root.BackingPath)
			}
		}
	}
	return nil
}

func hashRegularNoFollowFile(path string, allowNixETCSymlink bool) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 && !allowNixETCSymlink {
		return "", fmt.Errorf("%s is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

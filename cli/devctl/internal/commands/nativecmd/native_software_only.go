package nativecmd

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

const softwareOnlyGUITarget = "shadow-throne-local-3"

func softwareOnlyNativeAgentIndex(targetID string) int {
	switch targetID {
	case "shadow-throne-local-2":
		return 2
	case softwareOnlyGUITarget:
		return 3
	default:
		return 0
	}
}

func softwareOnlyNativeHostWorkspace(index int) string {
	return "/home/bayesartre/dev/agent-worktrees/agent" + strconv.Itoa(index)
}

const softwareOnlySandboxWorkspace = "/workspaces/dev"

// This selected-system executable is source-owned authority. Native callers
// cannot substitute it through an argument, environment variable or PATH.
var softwareOnlySelectedCodexExecutable = "/run/current-system/sw/bin/codex"

func validateSoftwareOnlyNativeAppServer(enabled bool, targetID, project string, managed bool, command []string) error {
	if !enabled {
		return nil
	}
	if softwareOnlyNativeAgentIndex(targetID) == 0 || project != "dev-all" || managed {
		return fmt.Errorf("software-only startup requires the selected Local2 or Local3 dev-all app-server")
	}
	if len(command) < 4 || command[0] != "/run/current-system/sw/bin/fleet-governed-app-server" || command[1] != "--" {
		return fmt.Errorf("software-only startup permits only the selected governed Codex app-server command")
	}
	selected, err := filepath.EvalSymlinks(softwareOnlySelectedCodexExecutable)
	if err != nil {
		return fmt.Errorf("resolve source-selected software-only Codex executable: %w", err)
	}
	return validateSoftwareOnlyNativeAppServerCommand(command, selected, targetID)
}

func validateSoftwareOnlyNativeAppServerCommand(command []string, selected, targetID string) error {
	index := softwareOnlyNativeAgentIndex(targetID)
	if index == 0 {
		return fmt.Errorf("software-only startup requires the selected Local2 or Local3 target")
	}
	hostWorkspace := softwareOnlyNativeHostWorkspace(index)
	if len(command) < 4 || command[0] != "/run/current-system/sw/bin/fleet-governed-app-server" || command[1] != "--" {
		return fmt.Errorf("software-only startup permits only the selected governed Codex app-server command")
	}
	if !strings.HasPrefix(selected, "/nix/store/") || filepath.Clean(selected) != selected || command[2] != selected {
		return fmt.Errorf("software-only startup Codex executable differs from the selected system package")
	}
	// These are the unchanged Management trust paths for this exact target.
	// Exact equality rejects alternative listeners, home/config overrides,
	// arbitrary -c values and additional executable arguments.
	expected := []string{selected, "app-server"}
	for _, path := range []string{softwareOnlySandboxWorkspace + "/ouroboros-ide", hostWorkspace + "/ouroboros-ide"} {
		expected = append(expected, "-c", "projects."+strconv.Quote(path)+`.trust_level="trusted"`)
	}
	expected = append(expected, "--listen", "unix://"+softwareOnlySandboxWorkspace+"/.devhome-agent"+strconv.Itoa(index)+"/.codex/a"+strconv.Itoa(index)+"-app.sock", "--analytics-default-enabled")
	if !reflect.DeepEqual(command[2:], expected) {
		return fmt.Errorf("software-only startup requires the exact source-owned Local2 or Local3 app-server argv")
	}
	return nil
}

func validateSoftwareOnlyNativePlan(enabled bool, p nativeplan.Plan) error {
	if !enabled {
		return nil
	}
	if p.GUITargetConfig == nil {
		return fmt.Errorf("software-only startup lacks the source-selected GUI config")
	}
	index := softwareOnlyNativeAgentIndex(p.GUITargetConfig.TargetID)
	hostWorkspace := softwareOnlyNativeHostWorkspace(index)
	homeName := "/.devhome-agent" + strconv.Itoa(index)
	if index == 0 ||
		p.Agent.ID.Project != "dev-all" || p.Agent.ID.Repo != "ouroboros-ide" || p.Agent.ID.Index != index ||
		p.HostWorkspaceRoot != hostWorkspace || p.SandboxWorkspaceRoot != softwareOnlySandboxWorkspace ||
		p.Agent.HostWorktree != hostWorkspace+"/ouroboros-ide" ||
		p.Agent.HostHome != hostWorkspace+homeName ||
		p.Agent.SandboxWorktree != softwareOnlySandboxWorkspace+"/ouroboros-ide" ||
		p.Agent.SandboxHome != softwareOnlySandboxWorkspace+homeName ||
		p.IsolationProfile != "workspace-egress" {
		return fmt.Errorf("software-only startup plan differs from the selected Local2 or Local3 config and geometry")
	}
	return nil
}

func prepareNativeExec(p nativeplan.Plan, softwareOnly bool) error {
	if softwareOnly {
		return launch.PrepareSoftwareOnly(p)
	}
	return launch.Prepare(p)
}

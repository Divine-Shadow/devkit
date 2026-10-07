package nativecmd

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

const softwareOnlyGUITarget = "shadow-throne-local-3"
const softwareOnlyRemoteGUITarget = "davidlich-1"

func softwareOnlyNativeHostWorkspace(index int) string {
	return "/home/bayesartre/dev/agent-worktrees/agent" + strconv.Itoa(index)
}

const softwareOnlySandboxWorkspace = "/workspaces/dev"

func softwareOnlyNativeHomes(index int) (string, string) {
	host, sandbox := softwareOnlyNativeHostWorkspace(index), softwareOnlySandboxWorkspace
	if index == 1 {
		host, sandbox = filepath.Join(host, "ouroboros-ide"), filepath.Join(sandbox, "ouroboros-ide")
	}
	home := ".devhome-agent" + strconv.Itoa(index)
	return filepath.Join(host, home), filepath.Join(sandbox, home)
}

// This selected-system executable is source-owned authority. Native callers
// cannot substitute it through an argument, environment variable or PATH.
var softwareOnlySelectedCodexExecutable = "/run/current-system/sw/bin/codex"

// Reject other executable effects before constructing a plan. Target class,
// execution host and complete argv are proved from the immutable selected plan.
func validateSoftwareOnlyNativeAppServer(enabled bool, targetID, project string, managed bool, command []string) error {
	if !enabled {
		return nil
	}
	if targetID == "" || targetID != strings.TrimSpace(targetID) || project != "dev-all" || managed {
		return fmt.Errorf("software-only startup requires a source-selected native dev-all app-server")
	}
	if len(command) < 4 || command[0] != "/run/current-system/sw/bin/fleet-governed-app-server" || command[1] != "--" || command[3] != "app-server" {
		return fmt.Errorf("software-only startup permits only the selected governed Codex app-server command")
	}
	return nil
}

func validateSoftwareOnlyNativePreparedCommand(enabled bool, p nativeplan.Plan, command []string) error {
	if !enabled {
		return nil
	}
	selected, err := filepath.EvalSymlinks(softwareOnlySelectedCodexExecutable)
	if err != nil {
		return fmt.Errorf("resolve source-selected software-only Codex executable: %w", err)
	}
	if err := validateSoftwareOnlyNativeAppServerCommand(command, selected, p); err != nil {
		return err
	}
	if p.GUITargetConfig.Kind == "devkit-agent" {
		current := filepath.Join(p.Agent.HostHome, ".codex", "packages", "standalone", "current", "bin", "codex")
		immediate, err := os.Readlink(current)
		if err != nil || immediate != selected {
			return fmt.Errorf("software-only remote current does not directly target selected Codex")
		}
		resolved, err := filepath.EvalSymlinks(current)
		if err != nil || resolved != selected {
			return fmt.Errorf("software-only remote current does not resolve to selected Codex")
		}
	}
	return nil
}

func validateSoftwareOnlyNativeAppServerCommand(command []string, selected string, p nativeplan.Plan) error {
	if p.GUITargetConfig == nil || (p.GUITargetConfig.Kind != "local-wsl-devkit-agent" && p.GUITargetConfig.Kind != "devkit-agent") {
		return fmt.Errorf("software-only startup lacks the source-selected standard native target class")
	}
	if len(command) < 4 || command[0] != "/run/current-system/sw/bin/fleet-governed-app-server" || command[1] != "--" {
		return fmt.Errorf("software-only startup permits only the selected governed Codex app-server command")
	}
	runtimeExecutable := selected
	if p.GUITargetConfig.Kind == "devkit-agent" {
		runtimeExecutable = filepath.Join(p.Agent.SandboxHome, ".codex", "packages", "standalone", "current", "bin", "codex")
	}
	if !strings.HasPrefix(selected, "/nix/store/") || filepath.Clean(selected) != selected || command[2] != runtimeExecutable {
		return fmt.Errorf("software-only startup Codex executable differs from the selected system package")
	}
	// Exact source-owned trust paths and listener reject config/home overrides.
	expected := []string{runtimeExecutable, "app-server"}
	for _, trustPath := range []string{p.Agent.SandboxWorktree, p.Agent.HostWorktree} {
		expected = append(expected, "-c", "projects."+strconv.Quote(trustPath)+`.trust_level="trusted"`)
	}
	expected = append(expected, "--listen", "unix://"+filepath.Join(p.Agent.SandboxHome, ".codex", "a"+strconv.Itoa(p.Agent.ID.Index)+"-app.sock"), "--analytics-default-enabled")
	if !reflect.DeepEqual(command[2:], expected) {
		return fmt.Errorf("software-only startup requires the exact source-owned native app-server argv")
	}
	return nil
}

func validateSoftwareOnlyNativePlan(enabled bool, p nativeplan.Plan) error {
	if !enabled {
		return nil
	}
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("read software-only execution hostname: %w", err)
	}
	return validateSoftwareOnlyNativePlanOnHost(p, hostname)
}

func validateSoftwareOnlyNativePlanOnHost(p nativeplan.Plan, hostname string) error {
	if p.GUITargetConfig == nil {
		return fmt.Errorf("software-only startup lacks the source-selected GUI config")
	}
	selected := p.GUITargetConfig
	if (selected.Kind != "local-wsl-devkit-agent" && selected.Kind != "devkit-agent") ||
		selected.ExpectedExecutionHost == "" || selected.ExpectedExecutionHost != strings.TrimSpace(selected.ExpectedExecutionHost) ||
		hostname != selected.ExpectedExecutionHost {
		return fmt.Errorf("software-only startup target class or execution host differs from the immutable selected projection")
	}
	index := p.Agent.ID.Index
	hostWorkspace := softwareOnlyNativeHostWorkspace(index)
	hostHome, sandboxHome := softwareOnlyNativeHomes(index)
	if index < 1 || p.Agent.ID.Project != "dev-all" || p.Agent.ID.Repo != "ouroboros-ide" ||
		p.HostWorkspaceRoot != hostWorkspace || p.SandboxWorkspaceRoot != softwareOnlySandboxWorkspace ||
		p.Agent.HostWorktree != hostWorkspace+"/ouroboros-ide" || p.Agent.HostHome != hostHome ||
		p.Agent.SandboxWorktree != softwareOnlySandboxWorkspace+"/ouroboros-ide" || p.Agent.SandboxHome != sandboxHome ||
		p.IsolationProfile != "workspace-egress" {
		return fmt.Errorf("software-only startup plan differs from the source-selected standard native geometry")
	}
	return nil
}

func prepareNativeExec(p nativeplan.Plan, softwareOnly bool) error {
	if softwareOnly {
		return launch.PrepareSoftwareOnly(p)
	}
	return launch.Prepare(p)
}

package nativecmd

import (
	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	wtx "devkit/cli/devctl/internal/worktrees"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

const terraformToolsPreparationPhase = "tf1-tools-provider-deferred"

func validateTerraformPreparationPhase(phase string) error {
	if phase != "" && phase != terraformToolsPreparationPhase {
		return fmt.Errorf("unknown Terraform preparation phase %q", phase)
	}
	return nil
}
func terraformToolsProviderHydration(phase string) string {
	if phase == terraformToolsPreparationPhase {
		return "deferred"
	}
	return ""
}
func terraformToolsProviderReadiness(phase string) string {
	if phase == terraformToolsPreparationPhase {
		return "unassessed"
	}
	return ""
}
func terraformPreparationCallback(phase string) func(nativeplan.Plan) error {
	if phase == terraformToolsPreparationPhase {
		return launch.PrepareTerraformTools
	}
	return launch.Prepare
}
func prepareSelectedNativeExec(p nativeplan.Plan, softwareOnly bool, phase string) error {
	if phase != "" {
		return launch.PrepareTerraformTools(p)
	}
	return prepareNativeExec(p, softwareOnly)
}
func validateTerraformToolsExecSelection(phase string, softwareOnly, managed bool, project, target string, command []string) error {
	if err := validateTerraformPreparationPhase(phase); err != nil {
		return err
	}
	if phase == "" {
		return nil
	}
	if softwareOnly || managed || project != "ouroboros-terraform" || target != "terraform-ouro-1" || len(command) < 4 || command[0] != "/run/current-system/sw/bin/ouro-terra-runtime" || command[1] != "--" || command[3] != "app-server" {
		return fmt.Errorf("Terraform tools phase permits only exact TF1 immutable app-server startup")
	}
	return nil
}
func validateTerraformToolsPreparedCommand(phase string, p nativeplan.Plan, command []string) error {
	if phase == "" {
		return nil
	}
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	if err := launch.ValidateTerraformToolsPlan(p, hostname); err != nil {
		return err
	}
	exists, err := wtx.ValidateTerraformNativeCheckout(wtx.NativeOptions{Repo: "ouroboros-terraform", Origin: terraformDeclaredOrigin, Index: 1, Count: 1, BaseBranch: "main", BranchPrefix: "agent", WorktreeRoot: "/home/bayesartre/dev/control-plane-worktrees", WorkspaceRoot: p.HostWorkspaceRoot, RequireSSHOrigin: true})
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("Terraform tools startup requires acquired canonical TF1 worktree")
	}
	selected, err := filepath.EvalSymlinks("/run/current-system/sw/bin/codex")
	if err != nil {
		return err
	}
	return validateTerraformToolsAppServerCommand(p, command, selected)
}
func validateTerraformToolsAppServerCommand(p nativeplan.Plan, command []string, selected string) error {
	if !strings.HasPrefix(selected, "/nix/store/") || filepath.Clean(selected) != selected {
		return fmt.Errorf("Terraform tools Codex must be source-selected immutable executable")
	}
	expected := []string{"/run/current-system/sw/bin/ouro-terra-runtime", "--", selected, "app-server"}
	for _, trustPath := range []string{p.Agent.SandboxWorktree, p.Agent.HostWorktree} {
		expected = append(expected, "-c", "projects."+strconv.Quote(trustPath)+`.trust_level="trusted"`)
	}
	expected = append(expected, "--listen", "unix://"+filepath.Join(p.Agent.SandboxHome, ".codex", "a1-app.sock"), "--analytics-default-enabled")
	if !reflect.DeepEqual(command, expected) {
		return fmt.Errorf("Terraform tools startup requires exact source-owned app-server argv")
	}
	return nil
}

func terraformToolsReadinessStatus(phase string, ready bool, full string) string {
	if phase == terraformToolsPreparationPhase && ready {
		return "tools-ready"
	}
	return full
}

// Deferred startup may execute only the package's unmodified runtime checks.
// Ordinary source commands retain their existing source-development behavior.
func validateTerraformToolsSource(phase string, ctx *cmdregistry.Context, cfg config.OverlayConfig) error {
	if phase == "" {
		return nil
	}
	if err := validateTerraformPreparationPhase(phase); err != nil {
		return err
	}
	args := ctx.Args
	if len(args) > 0 && args[0] == "1" {
		args = append([]string{"exec"}, args...)
	}
	if err := validateTerraformToolsArguments(args); err != nil {
		return err
	}
	root := ctx.Paths.RuntimeAuthorityRoot
	if !strings.HasPrefix(root, "/nix/store/") || filepath.Clean(root) != root || ctx.Paths.Root != root || len(ctx.Paths.OverlayPaths) != 1 || ctx.Paths.OverlayPaths[0] != filepath.Join(root, "overlays") || cfg.SourceDir != filepath.Join(root, "overlays", "ouroboros-terraform") || ctx.Project != "ouroboros-terraform" || cfg.Defaults.Agents != 1 || cfg.Defaults.Origin != terraformDeclaredOrigin || cfg.Defaults.BaseBranch != "main" || cfg.Defaults.BranchPrefix != "agent" {
		return fmt.Errorf("Terraform tools startup requires the exact immutable package overlay, checks and origin/branch")
	}
	return nil
}

func validateTerraformToolsArguments(args []string) error {
	if len(args) == 0 || (args[0] != "prepare" && args[0] != "readiness" && args[0] != "exec") {
		return fmt.Errorf("Terraform tools phase is a selected startup operation")
	}
	allowed := map[string]bool{"--preparation-phase": true, "--repo": true, "--index": true, "--worktree-root": true, "--state-root": true, "--agent-state-root": true, "--workspace-root": true, "--gui-target-id": true, "--format": true, "--isolation-profile": true}
	for i := 1; i < len(args); i++ {
		if args[i] == "--" {
			if args[0] != "exec" {
				return fmt.Errorf("Terraform tools phase forbids custom check commands")
			}
			return nil
		}
		if args[i] == "--runtime-only" && args[0] == "readiness" {
			continue
		}
		if args[0] == "exec" && i == 1 && args[i] == "1" {
			continue
		}
		if !allowed[args[i]] || i+1 >= len(args) {
			return fmt.Errorf("Terraform tools startup refuses option %q", args[i])
		}
		i++
	}
	if args[0] == "exec" {
		return fmt.Errorf("Terraform tools exec requires exact native command separator")
	}
	return nil
}

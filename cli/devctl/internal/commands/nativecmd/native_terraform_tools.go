package nativecmd

import (
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

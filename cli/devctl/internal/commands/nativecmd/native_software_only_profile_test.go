package nativecmd

import "testing"

func TestSoftwareOnlyPackageProfileRequiresExactSelectedTuple(t *testing.T) {
	previousWrapper, previousTarget := softwareOnlyGovernedAppServerExecutable, softwareOnlySelectedTargetID
	t.Cleanup(func() {
		softwareOnlyGovernedAppServerExecutable, softwareOnlySelectedTargetID = previousWrapper, previousTarget
	})
	softwareOnlyGovernedAppServerExecutable = "/nix/store/qualified-profile/fleet-governed-app-server"
	softwareOnlySelectedTargetID = "shadow-throne-local-2"
	selected := "/nix/store/qualified-codex/bin/codex"
	p := testSoftwareOnlyNativePlan("local-wsl-devkit-agent", softwareOnlySelectedTargetID, "shadow-throne", 2)
	command := []string{softwareOnlyGovernedAppServerExecutable, "--", selected, "app-server",
		"-c", `projects."/workspaces/dev/ouroboros-ide".trust_level="trusted"`,
		"-c", `projects."/home/bayesartre/dev/agent-worktrees/agent2/ouroboros-ide".trust_level="trusted"`,
		"--listen", "unix:///workspaces/dev/.devhome-agent2/.codex/a2-app.sock", "--analytics-default-enabled"}
	if err := validateSoftwareOnlyNativeAppServer(true, softwareOnlySelectedTargetID, "dev-all", false, command); err != nil {
		t.Fatal(err)
	}
	if err := validateSoftwareOnlyNativeAppServerCommand(command, selected, p); err != nil {
		t.Fatal(err)
	}
	for _, wrapper := range []string{previousWrapper, "/nix/store/unrelated-immutable/fleet-governed-app-server", "fleet-governed-app-server"} {
		invalid := append([]string{}, command...)
		invalid[0] = wrapper
		if err := validateSoftwareOnlyNativeAppServer(true, softwareOnlySelectedTargetID, "dev-all", false, invalid); err == nil {
			t.Fatalf("admitted unselected wrapper %q", wrapper)
		}
		if err := validateSoftwareOnlyNativeAppServerCommand(invalid, selected, p); err == nil {
			t.Fatalf("prepared command admitted unselected wrapper %q", wrapper)
		}
	}
	if err := validateSoftwareOnlyNativeAppServer(true, "shadow-throne-local-3", "dev-all", false, command); err == nil {
		t.Fatal("admitted sibling target")
	}
	p.GUITargetConfig.TargetID = "shadow-throne-local-3"
	if err := validateSoftwareOnlyNativeAppServerCommand(command, selected, p); err == nil {
		t.Fatal("prepared command admitted sibling target")
	}
	p.GUITargetConfig.TargetID = softwareOnlySelectedTargetID
	if err := validateSoftwareOnlyNativeAppServerCommand(append(command, "--config", "model=caller"), selected, p); err == nil {
		t.Fatal("admitted caller config")
	}
}

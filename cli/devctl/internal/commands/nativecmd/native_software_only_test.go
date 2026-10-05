package nativecmd

import (
	"devkit/cli/devctl/internal/cmdregistry"
	"strings"
	"testing"
)

func TestSoftwareOnlyNativeStartupRejectsOtherEffectsBeforePreparation(t *testing.T) {
	selected := "/nix/store/owned-selected-codex/bin/codex"
	accepted := []string{"/run/current-system/sw/bin/fleet-governed-app-server", "--", selected, "app-server",
		"-c", `projects."/workspaces/dev/ouroboros-ide".trust_level="trusted"`,
		"-c", `projects."/home/bayesartre/dev/agent-worktrees/agent3/ouroboros-ide".trust_level="trusted"`,
		"--listen", "unix:///workspaces/dev/.devhome-agent3/.codex/a3-app.sock", "--analytics-default-enabled"}
	if err := validateSoftwareOnlyNativeAppServerCommand(accepted, selected); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func([]string) []string{
		func(c []string) []string { c[2] = "/tmp/codex"; return c },
		func(c []string) []string { c[2] = "codex"; return c },
		func(c []string) []string { c[2] = "/nix/store/other/bin/codex"; return c },
		func(c []string) []string { c[3] = "exec"; return c },
		func(c []string) []string { c[5] = `approval_policy="never"`; return c },
		func(c []string) []string { c[9] = "unix:///tmp/other.sock"; return c },
		func(c []string) []string { return append(c, "--config", "model=caller-selected") },
		func(c []string) []string { return append(c, "--home", "/caller-selected") },
		func(c []string) []string { return c[2:] },
	} {
		invalid := mutate(append([]string{}, accepted...))
		if err := validateSoftwareOnlyNativeAppServerCommand(invalid, selected); err == nil {
			t.Fatalf("accepted caller effect: %v", invalid)
		}
	}
	for _, test := range []struct {
		target, project string
		managed         bool
	}{
		{"shadow-throne-local-2", "dev-all", false},
		{softwareOnlyGUITarget, "dev-workspace", false},
		{softwareOnlyGUITarget, "dev-all", true},
	} {
		if err := validateSoftwareOnlyNativeAppServer(true, test.target, test.project, test.managed, accepted); err == nil {
			t.Fatalf("accepted other target/profile: %#v", test)
		}
	}
	ctx := &cmdregistry.Context{Project: "dev-all"}
	if err := runTopExec(ctx, topExecArgs{softwareOnly: true, guiTargetID: softwareOnlyGUITarget}, []string{"bash"}); err == nil || !strings.Contains(err.Error(), "permits only") {
		t.Fatalf("non-app-server reached preparation: %v", err)
	}
	if _, err := parsePlanArgs(&cmdregistry.Context{Args: []string{"prepare", "--software-only"}}, false, false); err == nil {
		t.Fatal("prepare accepted software-only exec selector")
	}
	if _, err := parseTopExecArgs(&cmdregistry.Context{Args: []string{"3", "--software-only"}}, true); err == nil {
		t.Fatal("attach accepted software-only exec selector")
	}
}

func TestSoftwareOnlySelectorReachesBothNativeExecParsers(t *testing.T) {
	plan, err := parsePlanArgs(&cmdregistry.Context{Args: []string{"exec", "--software-only", "--gui-target-id", softwareOnlyGUITarget, "--", "/run/current-system/sw/bin/fleet-governed-app-server", "--", "/nix/store/owned-selected-codex/bin/codex", "app-server"}}, true, false)
	if err != nil || !plan.softwareOnly {
		t.Fatalf("native exec selector: %#v %v", plan, err)
	}
	top, err := parseTopExecArgs(&cmdregistry.Context{Args: []string{"3", "--software-only", "--gui-target-id", softwareOnlyGUITarget, "--", "/run/current-system/sw/bin/fleet-governed-app-server", "--", "/nix/store/owned-selected-codex/bin/codex", "app-server"}}, false)
	if err != nil || !top.softwareOnly {
		t.Fatalf("top exec selector: %#v %v", top, err)
	}
}

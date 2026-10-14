package launch

import (
	"devkit/cli/devctl/internal/devkitpaths"
	"devkit/cli/devctl/internal/gitauthority"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	wtx "devkit/cli/devctl/internal/worktrees"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Real local bare Git is an injected transport fixture, not production origin
// authority. It exercises acquisition -> native plan -> actual launch builder.
func TestTerraformAcquisitionPlanLaunchConsequences(t *testing.T) {
	root := t.TempDir()
	dev := filepath.Join(root, "dev")
	repo := "ouroboros-terraform"
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	restore := gitauthority.SetExecutableForTesting(git)
	defer restore()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command(git, args...)
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		b, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, b)
		}
	}
	primary := filepath.Join(dev, repo)
	origin := filepath.Join(root, "remote.git")
	if err := os.MkdirAll(primary, 0700); err != nil {
		t.Fatal(err)
	}
	run("init", "--bare", origin)
	run("-C", primary, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(primary, "proof.txt"), []byte("canonical fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("-C", primary, "add", "proof.txt")
	run("-C", primary, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-m", "fixture")
	run("-C", primary, "remote", "add", "origin", origin)
	run("-C", primary, "push", "origin", "main")
	wr := filepath.Join(dev, "control-plane-worktrees")
	workspace := filepath.Join(wr, "terraform-ouro-1")
	opts := wtx.NativeOptions{DevkitRoot: filepath.Join(dev, "devkit"), Repo: repo, Origin: origin, Index: 1, Count: 1, BaseBranch: "main", BranchPrefix: "agent", WorktreeRoot: wr, WorkspaceRoot: workspace}
	if err := wtx.SetupNative(opts); err != nil {
		t.Fatal(err)
	}
	// The launch builder checks presence of the immutable runtime connector.
	// This inert, explicitly injected fixture is never executed and proves no
	// installed runtime/Auth effect; actual package checks supply that warrant.
	runtimeRoot := filepath.Join(root, "runtime-fixture")
	helper := filepath.Join(runtimeRoot, "kit", "bin", "devctl")
	if err := os.MkdirAll(filepath.Dir(helper), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	runtimeLauncher := runtimeLauncherFixture(t)
	bubblewrap := runtimeLauncherFixture(t)
	plan, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{Paths: devkitpaths.Paths{Root: opts.DevkitRoot, RuntimeAuthorityRoot: runtimeRoot}, Project: repo, Repo: repo, Index: 1, RuntimeLauncher: runtimeLauncher, BubblewrapBinary: bubblewrap, WorktreeRoot: wr, WorkspaceRoot: workspace, IsolationProfile: nativeplan.IsolationProfileWorkspaceEgress, EgressAllowlist: filepath.Join(root, "allowlist.txt")})
	if err != nil {
		t.Fatal(err)
	}
	command, err := BuildBubblewrap(plan, []string{"codex", "app-server"})
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(command.Args, " ")
	if plan.Agent.HostWorktree != filepath.Join(workspace, repo) || plan.Agent.SandboxWorktree != "/workspaces/dev/"+repo || !strings.Contains(argv, workspace+" /workspaces/dev") || command.Path != bubblewrap || !strings.Contains(argv, runtimeLauncher) || !strings.Contains(argv, "codex") || !strings.Contains(argv, "app-server") {
		t.Fatalf("wrong acquisition/launch geometry: %#v %s", plan.Agent, argv)
	}
	if strings.Contains(argv, wr+" /workspaces/dev") || strings.Contains(argv, "terraform-ouro-2") {
		t.Fatalf("broad or sibling launch: %s", argv)
	}
}

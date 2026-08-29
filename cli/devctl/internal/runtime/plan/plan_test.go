package plan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
	"devkit/cli/devctl/internal/runtime/agent"
)

func TestBuildDevAllPlan(t *testing.T) {
	paths := devkitpaths.Paths{
		Root:                 "/home/bayesartre/dev/devkit",
		Kit:                  "/home/bayesartre/dev/devkit/kit",
		RuntimeAuthorityRoot: "/nix/store/source-derived-devkit",
	}
	p, err := BuildDevAll(BuildOptions{
		Paths:    paths,
		Project:  "dev-all",
		Index:    2,
		Repo:     "ouroboros-ide",
		Flake:    ".#ouroboros-dev-agent",
		Launcher: "bubblewrap",
	})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	if p.RuntimeAuthorityRoot != paths.RuntimeAuthorityRoot {
		t.Fatalf("runtime authority root = %q, want independent source authority %q", p.RuntimeAuthorityRoot, paths.RuntimeAuthorityRoot)
	}
	if p.Agent.HostWorktree != "/home/bayesartre/dev/agent-worktrees/agent2/ouroboros-ide" {
		t.Fatalf("host worktree = %q", p.Agent.HostWorktree)
	}
	if p.Agent.SandboxWorktree != "/workspaces/dev/agent-worktrees/agent2/ouroboros-ide" {
		t.Fatalf("sandbox worktree = %q", p.Agent.SandboxWorktree)
	}
	if p.Agent.HostHome != "/home/bayesartre/dev/agent-worktrees/agent2/.devhome-agent2" {
		t.Fatalf("host home = %q", p.Agent.HostHome)
	}
	if p.Agent.SandboxHome != "/workspaces/dev/agent-worktrees/agent2/.devhome-agent2" {
		t.Fatalf("sandbox home = %q", p.Agent.SandboxHome)
	}
	if p.Env["CODEX_HOME"] != filepath.Join(p.Agent.SandboxHome, ".codex") {
		t.Fatalf("CODEX_HOME = %q", p.Env["CODEX_HOME"])
	}
	if p.Env["SBT_GLOBAL_BASE"] != filepath.Join(p.Agent.SandboxHome, ".sbt") {
		t.Fatalf("SBT_GLOBAL_BASE = %q", p.Env["SBT_GLOBAL_BASE"])
	}
	if p.Env["SBT_BOOT_DIR"] != filepath.Join(p.Agent.SandboxHome, ".sbt", "boot") {
		t.Fatalf("SBT_BOOT_DIR = %q", p.Env["SBT_BOOT_DIR"])
	}
	if p.Env["SBT_IVY_HOME"] != "/workspaces/dev/.cache/shared/ivy2" {
		t.Fatalf("SBT_IVY_HOME = %q", p.Env["SBT_IVY_HOME"])
	}
	if p.Env["COURSIER_CACHE"] != "/workspaces/dev/.cache/shared/coursier" {
		t.Fatalf("COURSIER_CACHE = %q", p.Env["COURSIER_CACHE"])
	}
	wantServerSystemProperties := strings.Join([]string{
		"-Dsbt.global.base=" + p.Env["SBT_GLOBAL_BASE"],
		"-Dsbt.boot.directory=" + p.Env["SBT_BOOT_DIR"],
		"-Dsbt.ivy.home=" + p.Env["SBT_IVY_HOME"],
	}, "\n")
	if got := p.Env["SBT_CONTROL_PLANE_SERVER_SYSTEM_PROPERTIES"]; got != wantServerSystemProperties {
		t.Fatalf("SBT_CONTROL_PLANE_SERVER_SYSTEM_PROPERTIES = %q, want %q", got, wantServerSystemProperties)
	} else if strings.Contains(got, "/home/bayesartre") {
		t.Fatalf("spawned SBT server identity retained host-home authority: %q", got)
	}
	if p.Env["DEVKIT_NATIVE_AGENT"] != "2" {
		t.Fatalf("DEVKIT_NATIVE_AGENT = %q", p.Env["DEVKIT_NATIVE_AGENT"])
	}
	if p.Env["AWS_CONFIG_FILE"] != filepath.Join(p.Agent.SandboxHome, ".aws", "config") {
		t.Fatalf("AWS_CONFIG_FILE = %q", p.Env["AWS_CONFIG_FILE"])
	}
	if p.Env["AWS_SHARED_CREDENTIALS_FILE"] != filepath.Join(p.Agent.SandboxHome, ".aws", "credentials") {
		t.Fatalf("AWS_SHARED_CREDENTIALS_FILE = %q", p.Env["AWS_SHARED_CREDENTIALS_FILE"])
	}
	if p.Env["AWS_SDK_LOAD_CONFIG"] != "1" {
		t.Fatalf("AWS_SDK_LOAD_CONFIG = %q", p.Env["AWS_SDK_LOAD_CONFIG"])
	}
	for _, want := range []string{
		"-Xmx6G",
		"-XX:+UseG1GC",
		"-Djava.awt.headless=true",
		"-Dsbt.global.base=" + p.Env["SBT_GLOBAL_BASE"],
		"-Dsbt.boot.directory=" + p.Env["SBT_BOOT_DIR"],
		"-Dsbt.ivy.home=" + p.Env["SBT_IVY_HOME"],
		"-Dsbt.coursier.home=" + p.Env["COURSIER_CACHE"],
	} {
		if !strings.Contains(p.Env["SBT_OPTS"], want) {
			t.Fatalf("SBT_OPTS missing %q: %q", want, p.Env["SBT_OPTS"])
		}
	}
	if p.Env["DOCKER_HOST"] != "unix:///run/devkit/test-container-broker.sock" {
		t.Fatalf("DOCKER_HOST = %q", p.Env["DOCKER_HOST"])
	}
	if p.Env["DOCKER_API_VERSION"] != "1.52" {
		t.Fatalf("DOCKER_API_VERSION = %q", p.Env["DOCKER_API_VERSION"])
	}
	if p.Env["JAVA_TOOL_OPTIONS"] != "-Dapi.version=1.52 -Ddocker.api.version=1.52 -Ddocker.host=unix:///run/devkit/test-container-broker.sock" {
		t.Fatalf("JAVA_TOOL_OPTIONS = %q", p.Env["JAVA_TOOL_OPTIONS"])
	}
	if p.Env["HTTP_PROXY"] != "" || p.Env["HTTPS_PROXY"] != "" {
		t.Fatalf("native plan should not default to retired proxy env: %#v", p.Env)
	}
	if p.DirectDockerSocket {
		t.Fatalf("native plan must not expose direct Docker socket")
	}
	if !hasBind(p.Binds, "/home/bayesartre/dev", "/workspaces/dev") {
		t.Fatalf("native plan must mount dev root at /workspaces/dev: %#v", p.Binds)
	}
	if !hasBind(p.Binds, "/home/bayesartre/dev", "/home/bayesartre/dev") {
		t.Fatalf("native plan must mount dev root at its host path for git metadata: %#v", p.Binds)
	}
	if len(p.LauncherArgs) == 0 || p.LauncherArgs[0] != "<immutable-bubblewrap-required>" {
		t.Fatalf("launcher args = %#v", p.LauncherArgs)
	}
}

func TestWorkspaceEgressReportsMountPolicyAndRejectsWindowsMounts(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	worktree := filepath.Join(devRoot, "agent-worktrees", "agent2", "ouroboros-ide")
	for _, dir := range []string{devkitRoot, worktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p, err := Build(BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot},
		Project:          "dev-all",
		Index:            2,
		Repo:             "ouroboros-ide",
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(root, "allowlist.txt"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if p.IsolationProfile != IsolationProfileWorkspaceEgress ||
		p.MountPolicyIdentity != "devkit/workspace-egress/v3" ||
		p.WindowsMountsVisible {
		t.Fatalf("isolation readback = profile=%q policy=%q windows=%v", p.IsolationProfile, p.MountPolicyIdentity, p.WindowsMountsVisible)
	}
	if p.Env["DEVKIT_NATIVE_WINDOWS_MOUNTS_VISIBLE"] != "false" {
		t.Fatalf("windows mount env = %q", p.Env["DEVKIT_NATIVE_WINDOWS_MOUNTS_VISIBLE"])
	}

	_, err = Build(BuildOptions{
		Paths:            devkitpaths.Paths{Root: "/mnt/c/retired/devkit"},
		Project:          "dev-workspace",
		Repo:             ".",
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  "/mnt/c/retired/allowlist.txt",
	})
	if err == nil || !strings.Contains(err.Error(), "rejects Windows-mounted") {
		t.Fatalf("Windows mount must fail closed, got %v", err)
	}
}

func TestManagementWorkspaceRootProjectsOnlySelectedLane(t *testing.T) {
	t.Setenv(ManagementControllerProfileEnvironment, "")
	for _, index := range []int{1, 2} {
		t.Run(fmt.Sprintf("agent%d", index), func(t *testing.T) {
			devRoot := t.TempDir()
			devkitRoot := filepath.Join(devRoot, "devkit")
			worktreeRoot := filepath.Join(devRoot, "control-plane-worktrees")
			workspaceRoot := filepath.Join(worktreeRoot, fmt.Sprintf("agent%d", index))
			hostWorktree := filepath.Join(workspaceRoot, "shadow-throne-management")
			for _, dir := range []string{devkitRoot, hostWorktree} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}

			p, err := Build(BuildOptions{
				Paths:            devkitpaths.Paths{Root: devkitRoot},
				Project:          "dev-workspace",
				Index:            index,
				Repo:             "shadow-throne-management",
				WorkspaceRoot:    workspaceRoot,
				WorktreeRoot:     worktreeRoot,
				IsolationProfile: IsolationProfileWorkspaceEgress,
				EgressAllowlist:  filepath.Join(devkitRoot, "kit", "proxy", "allowlist.txt"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if p.HostWorkspaceRoot != workspaceRoot || p.SandboxWorkspaceRoot != "/workspaces/dev" {
				t.Fatalf("workspace projection = host %q sandbox %q", p.HostWorkspaceRoot, p.SandboxWorkspaceRoot)
			}
			if p.Agent.HostWorktree != hostWorktree || p.Agent.SandboxWorktree != "/workspaces/dev/shadow-throne-management" {
				t.Fatalf("worktree projection = host %q sandbox %q", p.Agent.HostWorktree, p.Agent.SandboxWorktree)
			}
			if p.MountPolicyIdentity != ManagementControllerMountPolicyIdentity || p.WindowsMountsVisible {
				t.Fatalf("mount identity = %q windows=%t", p.MountPolicyIdentity, p.WindowsMountsVisible)
			}
			if !hasExactBind(p.Binds, Bind{Source: workspaceRoot, Target: "/workspaces/dev", Mode: "rw", Required: true}) ||
				!hasExactBind(p.Binds, Bind{Source: hostWorktree, Target: "/workspace", Mode: "rw", Required: true}) {
				t.Fatalf("missing exact lane/workspace binds: %#v", p.Binds)
			}
			for _, target := range []string{
				"/workspaces/dev/.devkit/ouro8-governance-env.sh",
				"/workspaces/dev/.devkit/ouro8-governance-repo-env.json",
				"/workspaces/dev/.devkit/governance-control-plane",
			} {
				found := false
				for _, bind := range p.Binds {
					if bind.Target == target {
						found = true
						if !pathWithinRoot(workspaceRoot, bind.Source) {
							t.Fatalf("runtime support source escaped lane root: %#v", bind)
						}
					}
				}
				if !found {
					t.Fatalf("missing lane-local runtime support target %s: %#v", target, p.Binds)
				}
			}
			for _, bind := range p.Binds {
				if bind.Source == devRoot || bind.Source == worktreeRoot ||
					(bind.Source == hostWorktree && bind.Target == "/workspaces/dev/shadow-throne-management") {
					t.Fatalf("lane plan retained broad or redundant bind: %#v", bind)
				}
			}
		})
	}
}

func TestProductWorkspaceRootProjectsOnlySelectedLane(t *testing.T) {
	devRoot := t.TempDir()
	devkitRoot := filepath.Join(devRoot, "devkit")
	worktreeRoot := filepath.Join(devRoot, "agent-worktrees")
	workspaceRoot := filepath.Join(worktreeRoot, "agent4")
	hostWorktree := filepath.Join(workspaceRoot, "ouroboros-ide")
	for _, dir := range []string{devkitRoot, hostWorktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	p, err := Build(BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot},
		Project:          "dev-all",
		Index:            4,
		Repo:             "ouroboros-ide",
		WorkspaceRoot:    workspaceRoot,
		WorktreeRoot:     worktreeRoot,
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(devkitRoot, "kit", "proxy", "allowlist.txt"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.HostWorkspaceRoot != workspaceRoot || p.SandboxWorkspaceRoot != "/workspaces/dev" {
		t.Fatalf("workspace projection = host %q sandbox %q", p.HostWorkspaceRoot, p.SandboxWorkspaceRoot)
	}
	if p.Agent.HostWorktree != hostWorktree || p.Agent.SandboxWorktree != "/workspaces/dev/ouroboros-ide" {
		t.Fatalf("worktree projection = host %q sandbox %q", p.Agent.HostWorktree, p.Agent.SandboxWorktree)
	}
	if p.MountPolicyIdentity != ManagementControllerMountPolicyIdentity || p.WindowsMountsVisible {
		t.Fatalf("mount identity = %q windows=%t", p.MountPolicyIdentity, p.WindowsMountsVisible)
	}
	if !hasExactBind(p.Binds, Bind{Source: workspaceRoot, Target: "/workspaces/dev", Mode: "rw", Required: true}) ||
		!hasExactBind(p.Binds, Bind{Source: hostWorktree, Target: "/workspace", Mode: "rw", Required: true}) {
		t.Fatalf("missing exact lane/workspace binds: %#v", p.Binds)
	}

	otherLane := filepath.Join(worktreeRoot, "agent3")
	if err := os.MkdirAll(filepath.Join(otherLane, "ouroboros-ide"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot},
		Project:          "dev-all",
		Index:            4,
		Repo:             "ouroboros-ide",
		WorkspaceRoot:    otherLane,
		WorktreeRoot:     worktreeRoot,
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(devkitRoot, "kit", "proxy", "allowlist.txt"),
	}); err == nil {
		t.Fatal("mismatched Product lane root was accepted")
	}
}

func TestManagementWorkspaceRootFailsClosed(t *testing.T) {
	t.Setenv(ManagementControllerProfileEnvironment, "")
	devRoot := t.TempDir()
	devkitRoot := filepath.Join(devRoot, "devkit")
	worktreeRoot := filepath.Join(devRoot, "control-plane-worktrees")
	workspaceRoot := filepath.Join(worktreeRoot, "agent1")
	hostWorktree := filepath.Join(workspaceRoot, "shadow-throne-management")
	for _, dir := range []string{devkitRoot, hostWorktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	base := BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot},
		Project:          "dev-workspace",
		Index:            1,
		Repo:             "shadow-throne-management",
		WorkspaceRoot:    workspaceRoot,
		WorktreeRoot:     worktreeRoot,
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(devkitRoot, "kit", "proxy", "allowlist.txt"),
	}

	for name, mutate := range map[string]func(BuildOptions) BuildOptions{
		"non-canonical": func(opts BuildOptions) BuildOptions {
			opts.WorkspaceRoot += string(filepath.Separator) + ".." + string(filepath.Separator) + "agent1"
			return opts
		},
		"shared dev root": func(opts BuildOptions) BuildOptions {
			opts.WorkspaceRoot = devRoot
			return opts
		},
		"wholesale worktree parent": func(opts BuildOptions) BuildOptions {
			opts.WorkspaceRoot = worktreeRoot
			return opts
		},
		"other lane": func(opts BuildOptions) BuildOptions {
			opts.WorkspaceRoot = filepath.Join(worktreeRoot, "agent2")
			return opts
		},
		"other project": func(opts BuildOptions) BuildOptions {
			opts.Project = "dev-all"
			return opts
		},
		"other repo": func(opts BuildOptions) BuildOptions {
			opts.Repo = "wsl-nix"
			return opts
		},
		"other agent": func(opts BuildOptions) BuildOptions {
			opts.Index = 3
			opts.WorkspaceRoot = filepath.Join(worktreeRoot, "agent3")
			return opts
		},
		"without workspace egress": func(opts BuildOptions) BuildOptions {
			opts.IsolationProfile = ""
			opts.EgressAllowlist = ""
			return opts
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Build(mutate(base)); err == nil {
				t.Fatalf("%s workspace root was accepted", name)
			}
		})
	}

	mnt := base
	mnt.Paths.Root = "/mnt/c/dev/devkit"
	mnt.WorktreeRoot = "/mnt/c/dev/control-plane-worktrees"
	mnt.WorkspaceRoot = "/mnt/c/dev/control-plane-worktrees/agent1"
	if _, err := Build(mnt); err == nil || !strings.Contains(err.Error(), "rejects /mnt") {
		t.Fatalf("/mnt workspace root was accepted: %v", err)
	}
}

func TestManagementWorkspaceRootRejectsSymlinkEscapeAndExternalGitMetadata(t *testing.T) {
	t.Setenv(ManagementControllerProfileEnvironment, "")
	devRoot := t.TempDir()
	devkitRoot := filepath.Join(devRoot, "devkit")
	worktreeRoot := filepath.Join(devRoot, "control-plane-worktrees")
	workspaceRoot := filepath.Join(worktreeRoot, "agent1")
	external := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(workspaceRoot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, workspaceRoot); err != nil {
		t.Fatal(err)
	}
	opts := BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot},
		Project:          "dev-workspace",
		Index:            1,
		Repo:             "shadow-throne-management",
		WorkspaceRoot:    workspaceRoot,
		WorktreeRoot:     worktreeRoot,
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(devkitRoot, "kit", "proxy", "allowlist.txt"),
	}
	if _, err := Build(opts); err == nil || !strings.Contains(err.Error(), "rejects symlinked path") {
		t.Fatalf("symlinked lane root was accepted: %v", err)
	}
	if err := os.Remove(workspaceRoot); err != nil {
		t.Fatal(err)
	}
	hostWorktree := filepath.Join(workspaceRoot, "shadow-throne-management")
	if err := os.MkdirAll(hostWorktree, 0o755); err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(external, "git", "worktrees", "management")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostWorktree, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(opts); err == nil || !strings.Contains(err.Error(), "Git metadata outside the lane root") {
		t.Fatalf("external Git metadata was accepted: %v", err)
	}
}

func TestManagementWorkspaceRootDerivesActiveWSLNixSourceFromSelectedLane(t *testing.T) {
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "control-plane-worktrees", "agent2")
	paths := agent.Paths{
		DevRoot:          root,
		HostWorktreeRoot: filepath.Join(root, "control-plane-worktrees"),
		HostWorktree:     filepath.Join(workspaceRoot, "shadow-throne-management"),
		HostHome:         filepath.Join(root, ".devkit", "native-agents", "dev-workspace-agent2", "home"),
		SandboxHome:      "/agent-state/dev-workspace-agent2/home",
		SandboxWorktree:  "/workspaces/dev/shadow-throne-management",
	}
	profile := &ManagementControllerProfile{
		SourceRoots: ControllerProfileSourceRoots{
			WSLNix: ControllerProfileSourceRoot{
				LogicalPath: "/workspaces/dev/wsl-nix",
				BackingPath: filepath.Join(root, "control-plane-worktrees", "agent1", "wsl-nix"),
			},
		},
	}
	binds, err := workspaceEgressBinds(
		paths,
		"dev-workspace",
		2,
		"shadow-throne-management",
		workspaceRoot,
		filepath.Join(root, "devkit"),
		filepath.Join(root, "devkit"),
		filepath.Join(root, ".devkit", "native-broker", "broker.sock"),
		filepath.Join(root, ".devkit", "native-agents", "dev-workspace-agent2", "resolv.conf"),
		profile,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := Bind{Source: filepath.Join(workspaceRoot, "wsl-nix"), Target: "/workspaces/dev/wsl-nix", Mode: "rw", Required: true}
	if !hasExactBind(binds, want) {
		t.Fatalf("missing active lane WSL/Nix bind %#v in %#v", want, binds)
	}
	if hasBind(binds, profile.SourceRoots.WSLNix.BackingPath, "/workspaces/dev/wsl-nix") {
		t.Fatalf("selected W2 lane retained W1 WSL/Nix source: %#v", binds)
	}
}

func TestGovernedWorkspaceProjectsImmutableGovernanceEnvelopeAndCarriesBrokerSelector(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	worktree := filepath.Join(devRoot, "agent-worktrees", "agent1", "ouroboros-ide")
	for _, dir := range []string{devkitRoot, worktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	governanceSource := filepath.Join(root, "run", "current-system", "etc", "fleet", "product-governance.env")
	previousSource := WorkspaceProductGovernanceEnvSource
	WorkspaceProductGovernanceEnvSource = governanceSource
	t.Cleanup(func() { WorkspaceProductGovernanceEnvSource = previousSource })
	brokerBinary := "/nix/store/example-devkit-postgres-broker/bin/postgres-broker"
	p, err := Build(BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot},
		Project:          "dev-all",
		Index:            1,
		Repo:             "ouroboros-ide",
		BrokerBinary:     brokerBinary,
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(root, "allowlist.txt"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	expected := Bind{
		Source:   governanceSource,
		Target:   WorkspaceProductGovernanceEnvTarget,
		Mode:     "ro",
		Required: true,
	}
	if !hasExactBind(p.Binds, expected) {
		t.Fatalf("missing exact Product governance environment projection %#v in %#v", expected, p.Binds)
	}
	if got := p.Env["DEVKIT_RUNTIME_BROKER_BINARY"]; got != brokerBinary {
		t.Fatalf("broker selector = %q, want %q", got, brokerBinary)
	}
	for _, bind := range p.Binds {
		if bind.Target == filepath.Dir(WorkspaceProductGovernanceEnvTarget) {
			t.Fatalf("governance projection exposed a mutable parent directory: %#v", bind)
		}
	}
}

func TestWorkspaceEgressProjectsOnlyPackageOwnedControllerCapabilities(t *testing.T) {
	t.Setenv(ManagementControllerProfileEnvironment, "")
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	execSocket := filepath.Join(root, "run", "fleet-controller-exec", "control.sock")
	if err := os.MkdirAll(filepath.Dir(execSocket), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(execSocket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	previousExecSocket := WorkspaceControllerExecSocket
	WorkspaceControllerExecSocket = execSocket
	t.Cleanup(func() { WorkspaceControllerExecSocket = previousExecSocket })
	if err := os.MkdirAll(filepath.Join(devRoot, "devkit"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := Build(BuildOptions{
		Paths:            devkitpaths.Paths{Root: filepath.Join(devRoot, "devkit")},
		Project:          "dev-workspace",
		Index:            2,
		Repo:             "shadow-throne-management",
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(root, "allowlist.txt"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, path := range []string{WorkspaceControllerSourceInventory, WorkspaceControllerGUIInventory, execSocket} {
		if !hasBind(p.Binds, path, path) {
			t.Fatalf("controller capability %s not projected: %#v", path, p.Binds)
		}
		for _, bind := range p.Binds {
			if bind.Source == path && (bind.Mode != "ro" || !bind.Required) {
				t.Fatalf("controller capability %s must be required read-only: %#v", path, bind)
			}
		}
	}
	for _, bind := range p.Binds {
		if bind.Source == "/etc/fleet/source" || bind.Target == "/etc/fleet/source" || bind.Source == "/home/bayesartre/.ssh/fleet-station-controller" {
			t.Fatalf("controller projection must not expose parent directories: %#v", bind)
		}
	}
	if p.Env["FLEET_EXEC_TRANSPORT_HANDLE"] != "required" {
		t.Fatalf("controller exec handle env = %q", p.Env["FLEET_EXEC_TRANSPORT_HANDLE"])
	}
	for _, bind := range p.Binds {
		if strings.Contains(bind.Source, "codex_tailnet_stations_ed25519") ||
			strings.Contains(bind.Target, "codex_tailnet_stations_ed25519") {
			t.Fatalf("raw Fleet SSH identity escaped into the sandbox: %#v", bind)
		}
	}
	devAll, err := Build(BuildOptions{
		Paths:            devkitpaths.Paths{Root: filepath.Join(devRoot, "devkit")},
		Project:          "dev-all",
		Index:            2,
		Repo:             "ouroboros-ide",
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(root, "allowlist.txt"),
	})
	if err != nil {
		t.Fatalf("Build dev-all: %v", err)
	}
	for _, path := range []string{WorkspaceControllerSourceInventory, WorkspaceControllerGUIInventory, execSocket} {
		if hasBind(devAll.Binds, path, path) {
			t.Fatalf("non-controller workspace must not receive %s", path)
		}
	}
}

func TestTI4CanonicalWorkspaceProjectsControllerHandleOnly(t *testing.T) {
	root := t.TempDir()
	execSocket := filepath.Join(root, "run", "fleet-controller-exec", "control.sock")
	if err := os.MkdirAll(filepath.Dir(execSocket), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(execSocket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	previousExecSocket := WorkspaceControllerExecSocket
	WorkspaceControllerExecSocket = execSocket
	t.Cleanup(func() { WorkspaceControllerExecSocket = previousExecSocket })
	devRoot := filepath.Join(root, "dev")
	hostWorktreeRoot := filepath.Join(devRoot, "control-plane-worktrees")
	hostWorktree := filepath.Join(hostWorktreeRoot, "agent1", "ti4-calculator")
	p, err := Build(BuildOptions{
		Paths:            devkitpaths.Paths{Root: filepath.Join(devRoot, "devkit"), RuntimeAuthorityRoot: filepath.Join(devRoot, "devkit")},
		HostRoot:         devRoot,
		WorktreeRoot:     hostWorktreeRoot,
		StateRoot:        filepath.Join(devRoot, ".devkit", "native-agents"),
		Project:          "dev-workspace",
		Index:            1,
		Repo:             "ti4-calculator",
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(root, "allowlist.txt"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !hasBind(p.Binds, hostWorktree, "/workspaces/dev/ti4-calculator") {
		t.Fatalf("canonical TI4 alias missing: %#v", p.Binds)
	}
	if !hasBind(p.Binds, execSocket, execSocket) || p.Env["FLEET_EXEC_TRANSPORT_HANDLE"] != "required" {
		t.Fatalf("TI4 controller handle missing: binds=%#v env=%#v", p.Binds, p.Env)
	}
	for _, bind := range p.Binds {
		if strings.Contains(bind.Source, "fleet-inventory.json") || strings.Contains(bind.Source, "fleet-codex-gui-inventory.json") {
			t.Fatalf("TI4 must not receive controller inventories: %#v", bind)
		}
	}
}

func TestWorkspaceEgressRewritesRepoFlakeOverrideToSandboxWorktree(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	worktree := filepath.Join(devRoot, "agent-worktrees", "agent1", "ouroboros-terraform")
	for _, dir := range []string{devkitRoot, worktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p, err := Build(BuildOptions{
		Paths:   devkitpaths.Paths{Root: devkitRoot},
		Project: "ouroboros-terraform",
		Index:   1,
		Repo:    "ouroboros-terraform",
		FlakeInputOverrides: map[string]string{
			"ouroboros-terraform": "path:" + filepath.Join(devRoot, "ouroboros-terraform"),
		},
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(root, "allowlist.txt"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := "path:" + p.Agent.SandboxWorktree
	if p.FlakeInputOverrides["ouroboros-terraform"] != want {
		t.Fatalf("flake input override = %q, want %q", p.FlakeInputOverrides["ouroboros-terraform"], want)
	}
}

func TestBuildDoesNotFallBackRuntimeAuthorityToConfigRoot(t *testing.T) {
	p, err := BuildDevAll(BuildOptions{
		Paths: devkitpaths.Paths{Root: "/caller-controlled/devkit"},
		Repo:  "ouroboros-ide",
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if p.RuntimeAuthorityRoot != "" {
		t.Fatalf("runtime authority root = %q, want no fallback to config root", p.RuntimeAuthorityRoot)
	}
}

func TestBuildDevAllPreservesExecutableRuntimeAuthorityAgainstHostileConfigRoot(t *testing.T) {
	trustedRoot := "/trusted/source/devkit"
	hostileRoot := "/tmp/hostile-devkit"
	p, err := BuildDevAll(BuildOptions{
		Paths: devkitpaths.Paths{
			Root:                 hostileRoot,
			Kit:                  filepath.Join(hostileRoot, "kit"),
			RuntimeAuthorityRoot: trustedRoot,
		},
		Repo:  "ouroboros-ide",
		Flake: "path:.?dir=overlays/dev-all#default",
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if p.DevkitHostRoot != hostileRoot {
		t.Fatalf("config root = %q, want %q", p.DevkitHostRoot, hostileRoot)
	}
	if p.RuntimeAuthorityRoot != trustedRoot {
		t.Fatalf("runtime authority root = %q, want %q", p.RuntimeAuthorityRoot, trustedRoot)
	}
	wantFlake := "path:/trusted/source/devkit?dir=overlays/dev-all#default"
	if p.Flake != wantFlake {
		t.Fatalf("runtime flake = %q, want immutable authority %q", p.Flake, wantFlake)
	}
	rendered := strings.Join(p.LauncherArgs, " ")
	if !strings.Contains(rendered, "<immutable-runtime-launcher-required>") {
		t.Fatalf("plan must fail closed without an immutable runtime launcher: %s", rendered)
	}
	if strings.Contains(rendered, wantFlake) || strings.Contains(rendered, "nix develop") {
		t.Fatalf("plan retained consumer-time runtime derivation: %s", rendered)
	}
}

func hasBind(binds []Bind, source, target string) bool {
	for _, bind := range binds {
		if bind.Source == source && bind.Target == target {
			return true
		}
	}
	return false
}

func hasExactBind(binds []Bind, expected Bind) bool {
	for _, bind := range binds {
		if bind == expected {
			return true
		}
	}
	return false
}

func TestBuildDevAllHonorsExplicitProxy(t *testing.T) {
	p, err := BuildDevAll(BuildOptions{
		Paths:   devkitpaths.Paths{Root: "/repo/devkit"},
		Project: "dev-all",
		Proxy:   "http://127.0.0.1:8899",
	})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	if p.Env["HTTP_PROXY"] != "http://127.0.0.1:8899" || p.Env["HTTPS_PROXY"] != "http://127.0.0.1:8899" {
		t.Fatalf("proxy env = %#v", p.Env)
	}
}

func TestBuildDevAllProxySocketUsesLocalProxy(t *testing.T) {
	p, err := BuildDevAll(BuildOptions{
		Paths:       devkitpaths.Paths{Root: "/repo/devkit"},
		Project:     "dev-all",
		ProxySocket: "/repo/.devkit/native-egress/proxy.sock",
	})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	if p.Env["HTTP_PROXY"] != "http://127.0.0.1:18888" || p.Env["HTTPS_PROXY"] != "http://127.0.0.1:18888" {
		t.Fatalf("proxy env = %#v", p.Env)
	}
	if p.Proxy.UnixSocket != "/repo/.devkit/native-egress/proxy.sock" {
		t.Fatalf("proxy socket = %q", p.Proxy.UnixSocket)
	}
	if !hasBind(p.Binds, "/repo/.devkit/native-egress/proxy.sock", "/repo/.devkit/native-egress/proxy.sock") {
		t.Fatalf("native plan must bind proxy socket: %#v", p.Binds)
	}
}

func TestBuildDevAllWorkspaceEgressUsesNarrowBindsAndPerAgentCaches(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	hostWorktree := filepath.Join(devRoot, "agent-worktrees", "agent3", "ouroboros-ide")
	commonGitDir := filepath.Join(devRoot, "ouroboros-ide", ".git")
	worktreeGitDir := filepath.Join(commonGitDir, "worktrees", "agent3")
	for _, dir := range []string{devkitRoot, hostWorktree, worktreeGitDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	relativeGitDir, err := filepath.Rel(hostWorktree, worktreeGitDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostWorktree, ".git"), []byte("gitdir: "+relativeGitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeGitDir, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := BuildDevAll(BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot},
		Project:          "dev-all",
		Index:            3,
		Repo:             "ouroboros-ide",
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(devRoot, "ouroboros-ide", "infra", "docker", "dev", "tinyproxy", "allowlist.txt"),
	})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	if !hasBind(p.Binds, hostWorktree, "/workspace") {
		t.Fatalf("workspace-egress must mount worktree at /workspace: %#v", p.Binds)
	}
	if !hasBind(p.Binds, hostWorktree, "/workspaces/dev/agent-worktrees/agent3/ouroboros-ide") {
		t.Fatalf("workspace-egress must preserve canonical worktree path: %#v", p.Binds)
	}
	if !hasBind(p.Binds, filepath.Join(devRoot, "agent-worktrees", "agent3", ".devhome-agent3"), "/workspaces/dev/agent-worktrees/agent3/.devhome-agent3") {
		t.Fatalf("workspace-egress must mount exact agent home: %#v", p.Binds)
	}
	if !hasBind(p.Binds, devkitRoot, "/workspaces/dev/devkit") {
		t.Fatalf("workspace-egress must mount exact devkit runtime root: %#v", p.Binds)
	}
	foundNSCD := false
	for _, bind := range p.Binds {
		if bind.Source == workspaceEgressNSCDSocket && bind.Target == workspaceEgressNSCDSocket {
			foundNSCD = bind.Mode == "ro" && bind.Required
		}
	}
	if !foundNSCD {
		t.Fatalf("workspace-egress must require a read-only nsncd socket bind: %#v", p.Binds)
	}
	if !hasBind(p.Binds, commonGitDir, filepath.Join("/workspaces/dev", "ouroboros-ide", ".git")) {
		t.Fatalf("workspace-egress must project relative common Git metadata into the canonical sandbox path: %#v", p.Binds)
	}
	if hasBind(p.Binds, commonGitDir, commonGitDir) || hasBind(p.Binds, hostWorktree, hostWorktree) {
		t.Fatalf("portable workspace-egress metadata must not require host-path aliases: %#v", p.Binds)
	}
	for _, forbidden := range [][2]string{
		{devRoot, "/workspaces/dev"},
		{devRoot, devRoot},
		{filepath.Join(devRoot, "agent-worktrees"), "/workspaces/dev/agent-worktrees"},
		{filepath.Join(devRoot, ".devkit", "native-agents"), "/agent-state"},
	} {
		if hasBind(p.Binds, forbidden[0], forbidden[1]) {
			t.Fatalf("workspace-egress must not include broad bind %s -> %s: %#v", forbidden[0], forbidden[1], p.Binds)
		}
	}
	if p.Env["SBT_IVY_HOME"] != filepath.Join(p.Agent.SandboxHome, ".cache", "ivy2") {
		t.Fatalf("SBT_IVY_HOME = %q", p.Env["SBT_IVY_HOME"])
	}
	if p.Env["COURSIER_CACHE"] != filepath.Join(p.Agent.SandboxHome, ".cache", "coursier") {
		t.Fatalf("COURSIER_CACHE = %q", p.Env["COURSIER_CACHE"])
	}
	if strings.Contains(strings.Join([]string{p.Env["SBT_OPTS"], p.Env["SBT_IVY_HOME"], p.Env["COURSIER_CACHE"]}, " "), "/workspaces/dev/.cache/shared") {
		t.Fatalf("workspace-egress env must not reference shared host caches: %#v", p.Env)
	}
	wantSocket := filepath.Join(devRoot, ".devkit", "native-egress", "dev-all-agent3-workspace-egress.sock")
	if p.Proxy.UnixSocket != wantSocket {
		t.Fatalf("proxy socket = %q, want %q", p.Proxy.UnixSocket, wantSocket)
	}
	if p.Proxy.AllowlistPath == "" {
		t.Fatalf("proxy allowlist not set")
	}
	if p.Env["HTTP_PROXY"] != "http://127.0.0.1:18888" || p.Env["http_proxy"] != "http://127.0.0.1:18888" {
		t.Fatalf("proxy env = %#v", p.Env)
	}
	if p.Env["DEVKIT_NATIVE_ISOLATION_PROFILE"] != "workspace-egress" {
		t.Fatalf("DEVKIT_NATIVE_ISOLATION_PROFILE = %q", p.Env["DEVKIT_NATIVE_ISOLATION_PROFILE"])
	}
	for _, want := range []string{
		"-Dhttp.proxyHost=127.0.0.1",
		"-Dhttp.proxyPort=18888",
		"-Dhttps.proxyHost=127.0.0.1",
		"-Dhttps.proxyPort=18888",
	} {
		if !strings.Contains(p.Env["JAVA_TOOL_OPTIONS"], want) {
			t.Fatalf("JAVA_TOOL_OPTIONS missing %q: %q", want, p.Env["JAVA_TOOL_OPTIONS"])
		}
	}
}

func TestBuildDevAllInstalledRuntimeUsesDeclaredHostRootForEveryMutableEffect(t *testing.T) {
	root := t.TempDir()
	hostRoot := filepath.Join(root, "host-dev")
	hostWorktree := filepath.Join(hostRoot, "agent-worktrees", "agent1", "ouroboros-ide")
	for _, path := range []string{
		hostWorktree,
		filepath.Join(hostRoot, ".devkit", "governance-control-plane"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"ouro8-governance-env.sh", "ouro8-governance-repo-env.json"} {
		if err := os.WriteFile(filepath.Join(hostRoot, ".devkit", name), []byte("fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	packageRoot := "/nix/store/example-devkit-devctl"
	p, err := BuildDevAll(BuildOptions{
		Paths: devkitpaths.Paths{
			Root:                 packageRoot,
			RuntimeAuthorityRoot: packageRoot,
		},
		HostRoot:              hostRoot,
		Project:               "dev-all",
		Index:                 1,
		Repo:                  "ouroboros-ide",
		WorktreeRoot:          filepath.Join(hostRoot, "agent-worktrees"),
		StateRoot:             filepath.Join(hostRoot, ".devkit", "native-agents"),
		WorktreeContainerRoot: "/workspaces/dev/agent-worktrees",
		StateContainerRoot:    "/agent-state",
		BrokerEndpoint:        filepath.Join(hostRoot, ".devkit", "native-broker", "broker.sock"),
		IsolationProfile:      IsolationProfileWorkspaceEgress,
		EgressAllowlist:       filepath.Join(packageRoot, "kit", "proxy", "allowlist.txt"),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantProxy := filepath.Join(hostRoot, ".devkit", "native-egress", "dev-all-agent1-workspace-egress.sock")
	if p.Proxy.UnixSocket != wantProxy {
		t.Fatalf("proxy socket = %q, want %q", p.Proxy.UnixSocket, wantProxy)
	}
	for _, want := range []Bind{
		{Source: filepath.Join(hostRoot, ".devkit", "ouro8-governance-env.sh"), Target: "/workspaces/dev/.devkit/ouro8-governance-env.sh", Mode: "ro", Required: true},
		{Source: filepath.Join(hostRoot, ".devkit", "ouro8-governance-repo-env.json"), Target: "/workspaces/dev/.devkit/ouro8-governance-repo-env.json", Mode: "ro", Required: true},
		{Source: filepath.Join(hostRoot, ".devkit", "governance-control-plane"), Target: "/workspaces/dev/.devkit/governance-control-plane", Mode: "rw", Required: true},
		{Source: wantProxy, Target: wantProxy, Mode: "rw", Required: true},
	} {
		if !hasExactBind(p.Binds, want) {
			t.Fatalf("missing declared host-root bind %#v in %#v", want, p.Binds)
		}
	}
	for _, bind := range p.Binds {
		if bind.Mode == "rw" && strings.HasPrefix(bind.Source, "/nix/store") {
			t.Fatalf("installed runtime exposed writable Nix-store bind: %#v", bind)
		}
	}
	if strings.Contains(strings.Join(p.LauncherArgs, " "), "/nix/store/.devkit/") {
		t.Fatalf("installed runtime launcher retained mutable Nix-store state: %s", strings.Join(p.LauncherArgs, " "))
	}
}

func TestBuildDevAllWorkspaceEgressProjectsPreparedRuntimeSupportExactly(t *testing.T) {
	devRoot := t.TempDir()
	devkitRoot := filepath.Join(devRoot, "devkit")
	worktree := filepath.Join(devRoot, "agent-worktrees", "agent1", "ouroboros-ide")
	home := filepath.Join(worktree, ".devhome-agent1")
	for _, path := range []string{
		devkitRoot,
		worktree,
		home,
		filepath.Join(devRoot, ".devkit", "governance-control-plane"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{
		"ouro8-governance-env.sh",
		"ouro8-governance-repo-env.json",
	} {
		if err := os.WriteFile(filepath.Join(devRoot, ".devkit", name), []byte("fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	p, err := BuildDevAll(BuildOptions{
		Paths: devkitpaths.Paths{
			Root:                 devkitRoot,
			RuntimeAuthorityRoot: "/nix/store/example-devkit-devctl",
		},
		Project:          "dev-all",
		Repo:             "ouroboros-ide",
		Index:            1,
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  "/nix/store/example-devkit-devctl/kit/proxy/allowlist.txt",
	})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	if !hasBind(p.Binds, "/nix/store/example-devkit-devctl", "/workspaces/dev/devkit") {
		t.Fatalf("source-derived runtime root was not projected into the canonical devkit sandbox path: %#v", p.Binds)
	}
	if hasBind(p.Binds, devkitRoot, "/workspaces/dev/devkit") {
		t.Fatalf("mutable caller-selected devkit root remained a runtime authority: %#v", p.Binds)
	}

	for _, expected := range []Bind{
		{
			Source:   filepath.Join(devRoot, ".devkit", "ouro8-governance-env.sh"),
			Target:   "/workspaces/dev/.devkit/ouro8-governance-env.sh",
			Mode:     "ro",
			Required: true,
		},
		{
			Source:   filepath.Join(devRoot, ".devkit", "ouro8-governance-repo-env.json"),
			Target:   "/workspaces/dev/.devkit/ouro8-governance-repo-env.json",
			Mode:     "ro",
			Required: true,
		},
		{
			Source:   filepath.Join(devRoot, ".devkit", "governance-control-plane"),
			Target:   "/workspaces/dev/.devkit/governance-control-plane",
			Mode:     "rw",
			Required: true,
		},
	} {
		if !hasExactBind(p.Binds, expected) {
			t.Fatalf("missing exact prepared runtime support bind %#v; got %#v", expected, p.Binds)
		}
	}
	for _, forbidden := range []Bind{
		{Source: devRoot, Target: "/workspaces/dev"},
		{Source: filepath.Join(devRoot, ".devkit"), Target: "/workspaces/dev/.devkit"},
	} {
		if hasBind(p.Binds, forbidden.Source, forbidden.Target) {
			t.Fatalf("runtime support projection broadened to forbidden bind %#v", forbidden)
		}
	}
}

func TestWorkspaceEgressIsolatedRelativeMetadataUsesNoHostAliases(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	worktreeRoot := filepath.Join(root, "isolated-product-a")
	hostWorktree := filepath.Join(worktreeRoot, "agent1", "ouroboros-ide")
	commonGitDir := filepath.Join(worktreeRoot, ".devkit", "git", "ouroboros-ide.git")
	worktreeGitDir := filepath.Join(commonGitDir, "worktrees", "isolated-product-a")
	for _, dir := range []string{devkitRoot, hostWorktree, worktreeGitDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	relativeGitDir, err := filepath.Rel(hostWorktree, worktreeGitDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostWorktree, ".git"), []byte("gitdir: "+relativeGitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeGitDir, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := BuildDevAll(BuildOptions{
		Paths:                 devkitpaths.Paths{Root: devkitRoot},
		Project:               "dev-all",
		Index:                 1,
		Repo:                  "ouroboros-ide",
		WorktreeRoot:          worktreeRoot,
		WorktreeContainerRoot: "/workspaces/dev/isolated-product-a",
		IsolationProfile:      IsolationProfileWorkspaceEgress,
		EgressAllowlist:       filepath.Join(root, "allowlist.txt"),
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if p.Agent.HostWorktree != hostWorktree {
		t.Fatalf("host worktree = %s, want %s", p.Agent.HostWorktree, hostWorktree)
	}
	if p.Agent.SandboxWorktree != "/workspaces/dev/isolated-product-a/agent1/ouroboros-ide" {
		t.Fatalf("sandbox worktree = %s", p.Agent.SandboxWorktree)
	}
	sandboxGitDir := resolveGitdirValue(relativeGitDir, p.Agent.SandboxWorktree)
	sandboxCommonDir := resolveCommondirValue("../..", sandboxGitDir)
	if !hasBind(p.Binds, commonGitDir, sandboxCommonDir) {
		t.Fatalf("isolated metadata missing canonical common-dir bind %s -> %s: %#v", commonGitDir, sandboxCommonDir, p.Binds)
	}
	if hasBind(p.Binds, hostWorktree, hostWorktree) || hasBind(p.Binds, commonGitDir, commonGitDir) {
		t.Fatalf("isolated normal consumer retained a host worktree/common-dir alias: %#v", p.Binds)
	}
	for _, bind := range p.Binds {
		if (bind.Source == hostWorktree || bind.Source == commonGitDir) && strings.Contains(bind.Target, root) {
			t.Fatalf("sandbox bind target retained host spelling %s: %#v", root, bind)
		}
		if strings.Contains(bind.Source, "isolated-product-b") || strings.Contains(bind.Target, "isolated-product-b") {
			t.Fatalf("isolated consumer exposed unrelated root: %#v", bind)
		}
	}
}

func TestWorkspaceEgressMaterializesOnlyEachLinkedWorktreeCanonicalAlias(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	commonGitDir := filepath.Join(devRoot, ".git")
	worktreeRoot := filepath.Join(devRoot, "control-plane-worktrees")
	repo := "shadow-throne-management"
	for _, dir := range []string{devkitRoot, commonGitDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	worktrees := []string{
		filepath.Join(worktreeRoot, "agent1", repo),
		filepath.Join(worktreeRoot, "agent2", repo),
	}
	for index, worktree := range worktrees {
		gitDir := filepath.Join(commonGitDir, "worktrees", fmt.Sprintf("management-%d", index+1))
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", gitDir, err)
		}
		if err := os.MkdirAll(worktree, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", worktree, err)
		}
		if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../..\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for index, worktree := range worktrees {
		p, err := Build(BuildOptions{
			Paths:            devkitpaths.Paths{Root: devkitRoot},
			Project:          "dev-workspace",
			Index:            index + 1,
			Repo:             repo,
			WorktreeRoot:     worktreeRoot,
			IsolationProfile: IsolationProfileWorkspaceEgress,
			EgressAllowlist:  filepath.Join(root, "allowlist.txt"),
		})
		if err != nil {
			t.Fatalf("Build agent%d: %v", index+1, err)
		}
		if !hasBind(p.Binds, worktree, p.Agent.SandboxWorktree) {
			t.Fatalf("agent%d missing sandbox-canonical worktree bind: %#v", index+1, p.Binds)
		}
		if !hasBind(p.Binds, worktree, worktree) {
			t.Fatalf("agent%d missing exact Git-recorded worktree alias: %#v", index+1, p.Binds)
		}
		if !hasBind(p.Binds, commonGitDir, commonGitDir) {
			t.Fatalf("agent%d missing exact common Git directory: %#v", index+1, p.Binds)
		}
		if hasBind(p.Binds, devRoot, devRoot) || hasBind(p.Binds, worktreeRoot, worktreeRoot) {
			t.Fatalf("agent%d exposed a broad host path: %#v", index+1, p.Binds)
		}
		other := worktrees[1-index]
		if hasBind(p.Binds, other, other) {
			t.Fatalf("agent%d exposed sibling linked worktree %s: %#v", index+1, other, p.Binds)
		}
	}
}

func TestBuildWorkspaceEgressRequiresAllowlist(t *testing.T) {
	_, err := BuildDevAll(BuildOptions{
		Paths:            devkitpaths.Paths{Root: "/repo/devkit"},
		Project:          "dev-all",
		IsolationProfile: IsolationProfileWorkspaceEgress,
	})
	if err == nil || !strings.Contains(err.Error(), "requires an egress allowlist") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildNativeSupportsOtherProjects(t *testing.T) {
	p, err := Build(BuildOptions{
		Paths:   devkitpaths.Paths{Root: "/repo/devkit"},
		Project: "codex",
		Repo:    "ouroboros-ide",
		Flake:   "./overlays/codex#default",
	})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if p.Agent.ID.Project != "codex" {
		t.Fatalf("project = %q", p.Agent.ID.Project)
	}
	if p.Flake != "./overlays/codex#default" {
		t.Fatalf("flake = %q", p.Flake)
	}
	if p.Agent.StateRoot != "/repo/.devkit/native-agents/codex-agent1" {
		t.Fatalf("state root = %q", p.Agent.StateRoot)
	}
	if !hasBind(p.Binds, "/repo/agent-worktrees/agent1/ouroboros-ide", "/workspace") {
		t.Fatalf("native plan must mount agent worktree at /workspace: %#v", p.Binds)
	}
}

func TestBuildNativeCarriesFlakeInputOverrides(t *testing.T) {
	p, err := Build(BuildOptions{
		Paths:   devkitpaths.Paths{Root: "/repo/devkit"},
		Project: "ouroboros-terraform",
		Repo:    "ouroboros-terraform",
		Flake:   "./overlays/ouroboros-terraform#default",
		FlakeInputOverrides: map[string]string{
			"ouroboros-terraform": "path:/repo/ouroboros-terraform",
		},
	})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if p.FlakeInputOverrides["ouroboros-terraform"] != "path:/repo/ouroboros-terraform" {
		t.Fatalf("flake input overrides = %#v", p.FlakeInputOverrides)
	}
	joined := strings.Join(p.LauncherArgs, " ")
	if strings.Contains(joined, "--override-input") || strings.Contains(joined, "nix develop") {
		t.Fatalf("launcher args retained consumer-time flake authority: %#v", p.LauncherArgs)
	}
}

func TestBuildDevAllDedicatedWorktreeUsesFanoutForAgentOne(t *testing.T) {
	p, err := BuildDevAll(BuildOptions{
		Paths:             devkitpaths.Paths{Root: "/repo/devkit"},
		Project:           "dev-all",
		Index:             1,
		Repo:              "ouroboros-ide",
		DedicatedWorktree: true,
	})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	if p.Agent.HostWorktree != "/repo/agent-worktrees/agent1/ouroboros-ide" {
		t.Fatalf("host worktree = %q", p.Agent.HostWorktree)
	}
	if p.Agent.SandboxWorktree != "/workspaces/dev/agent-worktrees/agent1/ouroboros-ide" {
		t.Fatalf("sandbox worktree = %q", p.Agent.SandboxWorktree)
	}
	if p.Agent.HostHome != "/repo/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1" {
		t.Fatalf("host home = %q", p.Agent.HostHome)
	}
}

func TestBuildDevAllProjectsSymlinkedWorktreeIntoMountedDevRoot(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	repoRoot := filepath.Join(devRoot, "ouroboros-ide")
	agentRoot := filepath.Join(devRoot, "agent-worktrees", "agent1")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(agentRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../ouroboros-ide", filepath.Join(agentRoot, "ouroboros-ide")); err != nil {
		t.Fatal(err)
	}

	p, err := BuildDevAll(BuildOptions{
		Paths:   devkitpaths.Paths{Root: devkitRoot},
		Project: "dev-all",
		Index:   1,
		Repo:    "ouroboros-ide",
	})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	if p.Agent.HostWorktree != filepath.Join(agentRoot, "ouroboros-ide") {
		t.Fatalf("host worktree = %q", p.Agent.HostWorktree)
	}
	if p.Agent.SandboxWorktree != "/workspaces/dev/ouroboros-ide" {
		t.Fatalf("sandbox worktree = %q", p.Agent.SandboxWorktree)
	}
	if p.Agent.HostHome != filepath.Join(repoRoot, ".devhome-agent1") {
		t.Fatalf("host home = %q", p.Agent.HostHome)
	}
	if p.Agent.SandboxHome != "/workspaces/dev/ouroboros-ide/.devhome-agent1" {
		t.Fatalf("sandbox home = %q", p.Agent.SandboxHome)
	}
	if p.Env["HOME"] != p.Agent.SandboxHome || p.Env["CODEX_HOME"] != filepath.Join(p.Agent.SandboxHome, ".codex") {
		t.Fatalf("env home mismatch: %#v", p.Env)
	}
}

func TestBuildDevAllUsesConfiguredRoots(t *testing.T) {
	p, err := BuildDevAll(BuildOptions{
		Paths:                 devkitpaths.Paths{Root: "/repo/devkit"},
		Project:               "dev-all",
		Index:                 2,
		Repo:                  "devkit",
		WorktreeRoot:          "/tmp/worktrees",
		StateRoot:             "/tmp/state",
		WorktreeContainerRoot: "/native-worktrees",
		StateContainerRoot:    "/native-state",
	})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	if p.Agent.HostWorktree != "/tmp/worktrees/agent2/devkit" {
		t.Fatalf("host worktree = %q", p.Agent.HostWorktree)
	}
	if p.Agent.StateRoot != "/tmp/state/dev-all-agent2" {
		t.Fatalf("state root = %q", p.Agent.StateRoot)
	}
	if p.Agent.SandboxWorktree != "/native-worktrees/agent2/devkit" {
		t.Fatalf("sandbox worktree = %q", p.Agent.SandboxWorktree)
	}
	if p.Agent.SandboxHome != "/native-worktrees/agent2/.devhome-agent2" {
		t.Fatalf("sandbox home = %q", p.Agent.SandboxHome)
	}
}

func TestBuildDevWorkspaceUsesWorkspaceRoot(t *testing.T) {
	p, err := Build(BuildOptions{
		Paths:   devkitpaths.Paths{Root: "/repo/devkit"},
		Project: "dev-workspace",
		Flake:   "./overlays/dev-workspace#default",
	})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if p.Agent.ID.Repo != "." {
		t.Fatalf("repo = %q", p.Agent.ID.Repo)
	}
	if p.Agent.HostWorktree != "/repo" {
		t.Fatalf("host worktree = %q", p.Agent.HostWorktree)
	}
	if p.Agent.SandboxWorktree != "/workspaces/dev" {
		t.Fatalf("sandbox worktree = %q", p.Agent.SandboxWorktree)
	}
	if p.Agent.HostHome != "/repo/.devkit/native-agents/dev-workspace-agent1/home" {
		t.Fatalf("host home = %q", p.Agent.HostHome)
	}
	if p.Agent.SandboxHome != "/agent-state/dev-workspace-agent1/home" {
		t.Fatalf("sandbox home = %q", p.Agent.SandboxHome)
	}
	if p.Env["CODEX_HOME"] != "/agent-state/dev-workspace-agent1/home/.codex" {
		t.Fatalf("CODEX_HOME = %q", p.Env["CODEX_HOME"])
	}
	if !hasBind(p.Binds, "/repo", "/workspace") {
		t.Fatalf("workspace-root plan must mount dev root at /workspace: %#v", p.Binds)
	}
	if !hasBind(p.Binds, "/repo", "/workspaces/dev") {
		t.Fatalf("workspace-root plan must mount dev root at /workspaces/dev: %#v", p.Binds)
	}
	joinedLauncher := strings.Join(p.LauncherArgs, " ")
	if !strings.Contains(joinedLauncher, "--symlink /run/current-system/sw/bin/bash /usr/bin/bash") {
		t.Fatalf("workspace-root launcher must provide /usr/bin/bash compatibility: %#v", p.LauncherArgs)
	}
	if !strings.Contains(joinedLauncher, "bash -c") {
		t.Fatalf("workspace-root launcher must preserve the immutable runtime environment: %#v", p.LauncherArgs)
	}
	if strings.Contains(joinedLauncher, "bash -lc") {
		t.Fatalf("workspace-root launcher must not reset the immutable runtime environment with a login shell: %#v", p.LauncherArgs)
	}
}

func TestBuildManifestIncludesEveryAgent(t *testing.T) {
	manifest, err := BuildManifest(BuildOptions{
		Paths:        devkitpaths.Paths{Root: "/repo/devkit"},
		Project:      "dev-all",
		Repo:         "devkit",
		BaseBranch:   "main",
		BranchPrefix: "native-agent",
	}, 2)
	if err != nil {
		t.Fatalf("BuildManifest error: %v", err)
	}
	if manifest.Runtime != "native" || manifest.Count != 2 {
		t.Fatalf("manifest = %#v", manifest)
	}
	if len(manifest.Agents) != 2 {
		t.Fatalf("agents = %#v", manifest.Agents)
	}
	if manifest.Agents[0].HostWorktree != "/repo/agent-worktrees/agent1/devkit" {
		t.Fatalf("agent1 worktree = %q", manifest.Agents[0].HostWorktree)
	}
	if manifest.Agents[0].StateRoot == manifest.Agents[1].StateRoot {
		t.Fatalf("state roots should be distinct: %#v", manifest.Agents)
	}
	if manifest.BaseBranch != "main" || manifest.BranchPrefix != "native-agent" {
		t.Fatalf("branch metadata = %#v", manifest)
	}
}

func TestRenderJSONRoundTrip(t *testing.T) {
	p, err := BuildDevAll(BuildOptions{Paths: devkitpaths.Paths{Root: "/repo/devkit"}})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	data, err := RenderJSON(p)
	if err != nil {
		t.Fatalf("RenderJSON error: %v", err)
	}
	var decoded Plan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json unmarshal: %v\n%s", err, data)
	}
	if decoded.Agent.ID.Project != "dev-all" {
		t.Fatalf("project = %q", decoded.Agent.ID.Project)
	}
}

func TestRenderTextIncludesSafetyFields(t *testing.T) {
	p, err := BuildDevAll(BuildOptions{Paths: devkitpaths.Paths{Root: "/repo/devkit"}})
	if err != nil {
		t.Fatalf("BuildDevAll error: %v", err)
	}
	text := RenderText(p)
	for _, want := range []string{
		"Native agent plan",
		"direct_docker_socket: false",
		"broker_endpoint: /run/devkit/test-container-broker.sock",
		"CODEX_HOME=",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q:\n%s", want, text)
		}
	}
}

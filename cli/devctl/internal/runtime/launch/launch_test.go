package launch

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
	"devkit/cli/devctl/internal/runtime/agent"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/sshauthority"
)

func TestMain(m *testing.M) {
	testExecutable, err := os.Executable()
	if err != nil {
		panic(err)
	}
	knownHosts, err := os.CreateTemp("", "devkit-launch-known-hosts-")
	if err != nil {
		panic(err)
	}
	knownHostsPath := knownHosts.Name()
	if _, err := knownHosts.WriteString("[ssh.github.com]:443 ssh-ed25519 fixture\n"); err != nil {
		panic(err)
	}
	if err := knownHosts.Close(); err != nil {
		panic(err)
	}
	defer os.Remove(knownHostsPath)
	testSSHAuthority, err := sshauthority.New(testExecutable, knownHostsPath)
	if err != nil {
		panic(err)
	}
	resolvePackageSSHAuthority = func() (sshauthority.Authority, error) {
		return testSSHAuthority, nil
	}
	os.Exit(m.Run())
}

func TestWorkspaceControllerCapabilityValidationRejectsSymlinkAndWrongSocketMode(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "devkit-exec-handle-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "control.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := os.Chmod(socket, 0o600); err != nil {
		t.Fatal(err)
	}
	previousExecSocket := nativeplan.WorkspaceControllerExecSocket
	nativeplan.WorkspaceControllerExecSocket = socket
	t.Cleanup(func() { nativeplan.WorkspaceControllerExecSocket = previousExecSocket })
	if err := validateWorkspaceControllerCapability(socket, socket); err != nil {
		t.Fatalf("valid protected exec socket rejected: %v", err)
	}
	if err := os.Chmod(socket, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := validateWorkspaceControllerCapability(socket, socket); err == nil {
		t.Fatal("wrong exec socket mode must fail closed")
	}
	symlink := filepath.Join(root, "control-link.sock")
	if err := os.Symlink(socket, symlink); err != nil {
		t.Fatal(err)
	}
	nativeplan.WorkspaceControllerExecSocket = symlink
	if err := validateWorkspaceControllerCapability(symlink, symlink); err == nil {
		t.Fatal("symlink exec socket must fail closed")
	}
}

func TestWorkspaceControllerCapabilityValidationRequiresRegularInventory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "inventory")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateWorkspaceControllerCapability(dir, "/etc/fleet/source/fleet-inventory.json"); err == nil {
		t.Fatal("directory inventory must fail closed")
	}
}

func TestWorkspaceControllerCapabilityBindingRejectsOverridesAndWritableMode(t *testing.T) {
	root := t.TempDir()
	socket := filepath.Join(root, "control.sock")
	if err := os.WriteFile(socket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	previousExecSocket := nativeplan.WorkspaceControllerExecSocket
	nativeplan.WorkspaceControllerExecSocket = socket
	t.Cleanup(func() { nativeplan.WorkspaceControllerExecSocket = previousExecSocket })
	if err := validateWorkspaceControllerCapabilityBinding(socket, socket, "rw"); err == nil {
		t.Fatal("writable controller capability must fail closed")
	}
	if err := validateWorkspaceControllerCapabilityBinding(filepath.Join(root, "other"), socket, "ro"); err == nil {
		t.Fatal("caller source override must fail closed")
	}
}

func testPackageSSHCommand(t *testing.T, configPath string) string {
	t.Helper()
	authority, err := resolvePackageSSHAuthority()
	if err != nil {
		t.Fatal(err)
	}
	command, err := authority.Command(configPath)
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func devkitRootFromPackage(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "..")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("resolve devkit root: %v", err)
	}
	return abs
}

func initTestGitWorktree(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir worktree parent: %v", err)
	}
	runTestCommand(t, "", "git", "init", path)
}

func TestDevWorkspaceRuntimeExportsNestedCodexConfigSource(t *testing.T) {
	root := devkitRootFromPackage(t)
	runtimeNix := readTestFile(t, filepath.Join(root, "overlays", "dev-workspace", "runtime.nix"))
	for _, want := range []string{
		`pkgs.python3Packages.pyyaml`,
		`[ -n "''${CODEX_HOME:-}" ]`,
		`[ -r "$CODEX_HOME/config.toml" ]`,
		`export DEVKIT_CODEX_CONFIG_SOURCE="$CODEX_HOME/config.toml"`,
	} {
		if !strings.Contains(runtimeNix, want) {
			t.Fatalf("dev-workspace runtime missing %q:\n%s", want, runtimeNix)
		}
	}
}

func runtimeLauncherFixture(t *testing.T) string {
	t.Helper()
	launcher := filepath.Join(t.TempDir(), "dev-all-runtime-shell")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return launcher
}

func TestShellCommandPreservesImmutableRuntimePathAgainstHostileLoginProfile(t *testing.T) {
	tmp := t.TempDir()
	runtimeBin := filepath.Join(tmp, "runtime-bin")
	home := filepath.Join(tmp, "home")
	workdir := filepath.Join(tmp, "work")
	for _, dir := range []string{runtimeBin, home, workdir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bashPath, filepath.Join(runtimeBin, "bash")); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(tmp, "node-ran")
	node := filepath.Join(runtimeBin, "node")
	if err := os.WriteFile(node, []byte("#!/bin/sh\nprintf preserved > \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte("export PATH=/usr/bin:/bin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(tmp, "runtime-launcher")
	launcherBody := "#!/bin/sh\nexport PATH=" + shellQuote(runtimeBin) + ":/usr/bin:/bin\nexec \"$@\"\n"
	if err := os.WriteFile(launcher, []byte(launcherBody), 0o755); err != nil {
		t.Fatal(err)
	}

	args := shellCommand("", "", workdir, []string{"node", output}, nativeplan.ProxyConfig{}, nil, false)
	cmd := exec.Command(launcher, args...)
	cmd.Env = append(os.Environ(), "HOME="+home)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runtime launcher command failed: %v\n%s", err, combined)
	}
	if got, err := os.ReadFile(output); err != nil || string(got) != "preserved" {
		t.Fatalf("immutable runtime node was not selected: content=%q err=%v", got, err)
	}
}

func TestBuildBubblewrapUsesBrokerAndNoHostDockerSocket(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	repoRoot := filepath.Join(devRoot, "ouroboros-ide")
	for _, dir := range []string{devkitRoot, repoRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	brokerSocket := filepath.Join(tmp, "broker.sock")
	if err := os.WriteFile(brokerSocket, []byte("socket placeholder"), 0o600); err != nil {
		t.Fatalf("write broker socket placeholder: %v", err)
	}
	runtimeLauncher := runtimeLauncherFixture(t)
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Repo:             "ouroboros-ide",
		Flake:            ".#runtime-test-agent",
		RuntimeLauncher:  runtimeLauncher,
		BubblewrapBinary: runtimeLauncherFixture(t),
		BrokerEndpoint:   brokerSocket,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	initTestGitWorktree(t, p.Agent.HostWorktree)
	if err := os.MkdirAll(p.Agent.HostHome, 0o700); err != nil {
		t.Fatalf("mkdir host home: %v", err)
	}
	if err := os.MkdirAll(p.Agent.StateRoot, 0o700); err != nil {
		t.Fatalf("mkdir state root: %v", err)
	}
	if err := os.WriteFile(p.DNS.ResolvConf, []byte("nameserver 127.0.0.1\n"), 0o644); err != nil {
		t.Fatalf("write resolv: %v", err)
	}

	cmd, err := BuildBubblewrap(p, []string{"git", "--version"})
	if err != nil {
		t.Fatalf("BuildBubblewrap: %v", err)
	}
	joined := ShellString(cmd)
	for _, want := range []string{
		"'--bind' '" + devRoot + "' '/workspaces/dev'",
		"'--symlink' '/run/current-system/sw/bin/env' '/usr/bin/env'",
		"'--symlink' '/run/current-system/sw/bin/bash' '/usr/bin/bash'",
		"'--symlink' '/run/current-system/sw/bin/sh' '/bin/sh'",
		"'--bind' '" + brokerSocket + "' '" + brokerSocket + "'",
		"'--setenv' 'COURSIER_CACHE' '/workspaces/dev/.cache/shared/coursier'",
		"'--setenv' 'DOCKER_HOST' 'unix://" + brokerSocket + "'",
		"'--setenv' 'OURO_NIX_SANDBOX' '1'",
		"'--setenv' 'SBT_IVY_HOME' '/workspaces/dev/.cache/shared/ivy2'",
		"'--setenv' 'TMPDIR' '/tmp'",
		"'--setenv' 'XDG_CACHE_HOME' '/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1/.cache'",
		"'" + runtimeLauncher + "' 'bash' '-c'",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command missing %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(cmd.Args[len(cmd.Args)-1], "export XDG_CACHE_HOME='/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1/.cache'") {
		t.Fatalf("agent shell does not restore XDG_CACHE_HOME:\n%#v", cmd.Args)
	}
	if !strings.Contains(cmd.Args[len(cmd.Args)-1], "export COURSIER_CACHE='/workspaces/dev/.cache/shared/coursier'") {
		t.Fatalf("agent shell does not export shared coursier cache:\n%#v", cmd.Args)
	}
	if !strings.Contains(cmd.Args[len(cmd.Args)-1], "export OURO_NIX_SANDBOX='1'") {
		t.Fatalf("agent shell does not export Nix sandbox marker:\n%#v", cmd.Args)
	}
	if !strings.Contains(cmd.Args[len(cmd.Args)-1], "export SBT_BOOT_DIR='/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1/.sbt/boot'") {
		t.Fatalf("agent shell does not export per-agent SBT boot dir:\n%#v", cmd.Args)
	}
	serverSystemProperties := p.Env["SBT_CONTROL_PLANE_SERVER_SYSTEM_PROPERTIES"]
	if !strings.Contains(cmd.Args[len(cmd.Args)-1], "export SBT_CONTROL_PLANE_SERVER_SYSTEM_PROPERTIES="+shellQuote(serverSystemProperties)) {
		t.Fatalf("agent shell does not bind the spawned SBT server to prepared slot paths:\n%#v", cmd.Args)
	}
	if strings.Contains(serverSystemProperties, p.Agent.HostHome) || strings.Contains(serverSystemProperties, "/home/bayesartre") {
		t.Fatalf("spawned SBT server properties contain host-home authority: %q", serverSystemProperties)
	}
	for _, optionalHostPath := range []string{"/etc/static", "/etc/ssl", "/etc/pki"} {
		if _, err := os.Stat(optionalHostPath); err != nil {
			continue
		}
		want := "'--ro-bind' '" + optionalHostPath + "' '" + optionalHostPath + "'"
		if !strings.Contains(joined, want) {
			t.Fatalf("command missing optional host bind %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "'--dir' '/tmp'") {
		t.Fatalf("launcher must not create /tmp after mounting it as tmpfs:\n%s", joined)
	}
	if len(cmd.Args) < 1 || !strings.Contains(cmd.Args[len(cmd.Args)-1], "cd '/workspaces/dev/agent-worktrees/agent1/ouroboros-ide' && exec 'git' '--version'") {
		t.Fatalf("launch script did not cd and exec command:\n%#v", cmd.Args)
	}
	if strings.Contains(joined, "/var/run/docker.sock") {
		t.Fatalf("native launcher must not expose /var/run/docker.sock:\n%s", joined)
	}
}

func TestBuildBubblewrapUsesImmutableRuntimeLauncherWithoutConsumerFlakeEvaluation(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	repoRoot := filepath.Join(devRoot, "ouroboros-terraform")
	for _, dir := range []string{devkitRoot, repoRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	runtimeLauncher := runtimeLauncherFixture(t)
	p, err := nativeplan.Build(nativeplan.BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Project:          "ouroboros-terraform",
		Repo:             "ouroboros-terraform",
		Flake:            "./overlays/ouroboros-terraform#default",
		RuntimeLauncher:  runtimeLauncher,
		BubblewrapBinary: runtimeLauncherFixture(t),
		FlakeInputOverrides: map[string]string{
			"ouroboros-terraform": "path:" + repoRoot,
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	initTestGitWorktree(t, p.Agent.HostWorktree)
	if err := os.MkdirAll(p.Agent.HostHome, 0o700); err != nil {
		t.Fatalf("mkdir host home: %v", err)
	}
	if err := os.MkdirAll(p.Agent.StateRoot, 0o700); err != nil {
		t.Fatalf("mkdir state root: %v", err)
	}
	if err := os.WriteFile(p.DNS.ResolvConf, []byte("nameserver 127.0.0.1\n"), 0o644); err != nil {
		t.Fatalf("write resolv: %v", err)
	}
	cmd, err := BuildBubblewrap(p, []string{"true"})
	if err != nil {
		t.Fatalf("BuildBubblewrap: %v", err)
	}
	joined := ShellString(cmd)
	if !strings.Contains(joined, "'"+runtimeLauncher+"' 'bash' '-c'") {
		t.Fatalf("command did not select the immutable runtime launcher:\n%s", joined)
	}
	if strings.Contains(joined, "'"+runtimeLauncher+"' 'bash' '-lc'") {
		t.Fatalf("command selected a login shell after the immutable runtime launcher:\n%s", joined)
	}
	for _, forbidden := range []string{
		"nix' 'develop",
		"--override-input",
		"./overlays/ouroboros-terraform#default",
		"path:" + repoRoot,
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("command retained consumer flake authority %q:\n%s", forbidden, joined)
		}
	}
}

func TestBuildBubblewrapRejectsMissingOrUntrustedRuntimeLauncher(t *testing.T) {
	tmp := t.TempDir()
	devkitRoot := filepath.Join(tmp, "devkit")
	worktree := filepath.Join(tmp, "worktree")
	for _, dir := range []string{devkitRoot, worktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, launcher := range []string{"", "relative/dev-all-runtime-shell"} {
		p, err := nativeplan.Build(nativeplan.BuildOptions{
			Paths:           devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
			Repo:            "ouroboros-ide",
			RuntimeLauncher: launcher,
			WorktreeRoot:    filepath.Dir(worktree),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := BuildBubblewrap(p, []string{"true"}); err == nil ||
			!strings.Contains(err.Error(), "immutable native runtime launcher") {
			t.Fatalf("runtime launcher %q was not rejected: %v", launcher, err)
		}
	}
	for _, bubblewrap := range []string{"", "relative/bwrap"} {
		p, err := nativeplan.Build(nativeplan.BuildOptions{
			Paths:            devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
			Repo:             "ouroboros-ide",
			RuntimeLauncher:  runtimeLauncherFixture(t),
			BubblewrapBinary: bubblewrap,
			WorktreeRoot:     filepath.Dir(worktree),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := BuildBubblewrap(p, []string{"true"}); err == nil ||
			!strings.Contains(err.Error(), "immutable native bubblewrap binary") {
			t.Fatalf("bubblewrap binary %q was not rejected: %v", bubblewrap, err)
		}
	}
}

func TestBuildBubblewrapProxySocketUnsharesNetworkAndStartsBridge(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	repoRoot := filepath.Join(devRoot, "ouroboros-ide")
	for _, dir := range []string{devkitRoot, repoRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	proxySocket := filepath.Join(tmp, "egress.sock")
	if err := os.WriteFile(proxySocket, []byte("socket placeholder"), 0o600); err != nil {
		t.Fatalf("write proxy socket placeholder: %v", err)
	}
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Repo:             "ouroboros-ide",
		Flake:            ".#runtime-test-agent",
		RuntimeLauncher:  runtimeLauncherFixture(t),
		BubblewrapBinary: runtimeLauncherFixture(t),
		ProxySocket:      proxySocket,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	cmd, err := BuildBubblewrap(p, []string{"curl", "-I", "https://api.openai.com"})
	if err != nil {
		t.Fatalf("BuildBubblewrap: %v", err)
	}
	joined := ShellString(cmd)
	for _, want := range []string{
		"'--unshare-net'",
		"'--bind' '" + proxySocket + "' '" + proxySocket + "'",
		"native proxy-bridge --listen 127.0.0.1:18888",
		"'--setenv' 'HTTP_PROXY' 'http://127.0.0.1:18888'",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command missing %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "proxy-bridge") || !strings.Contains(joined, proxySocket) {
		t.Fatalf("command missing proxy socket bridge:\n%s", joined)
	}
	if strings.Contains(joined, "'--share-net'") {
		t.Fatalf("proxy socket launches must not share host network:\n%s", joined)
	}
}

func TestBuildBubblewrapManagementFleetUsesOnlyTypedExecHandle(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	worktree := filepath.Join(devRoot, "control-plane-worktrees", "shadow-throne-management")
	for _, dir := range []string{devkitRoot, worktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	proxySocket := filepath.Join(tmp, "egress.sock")
	if err := os.WriteFile(proxySocket, []byte("socket placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	execRoot, err := os.MkdirTemp("/tmp", "devkit-exec-handle-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(execRoot) })
	if err := os.Chmod(execRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	execSocket := filepath.Join(execRoot, "control.sock")
	listener, err := net.Listen("unix", execSocket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := os.Chmod(execSocket, 0o600); err != nil {
		t.Fatal(err)
	}
	previousExecSocket := nativeplan.WorkspaceControllerExecSocket
	nativeplan.WorkspaceControllerExecSocket = execSocket
	t.Cleanup(func() { nativeplan.WorkspaceControllerExecSocket = previousExecSocket })
	p, err := nativeplan.Build(nativeplan.BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Project:          "dev-workspace",
		Index:            2,
		Repo:             "shadow-throne-management",
		Flake:            ".#dev-workspace",
		RuntimeLauncher:  runtimeLauncherFixture(t),
		BubblewrapBinary: runtimeLauncherFixture(t),
		IsolationProfile: nativeplan.IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(devRoot, "allowlist.txt"),
		ProxySocket:      proxySocket,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// The test host has no immutable inventory projections. Keep only the
	// protected exec handle and ordinary workspace capabilities.
	filtered := p.Binds[:0]
	for _, bind := range p.Binds {
		if isWorkspaceControllerCapabilityTarget(bind.Target) && bind.Target != execSocket {
			continue
		}
		filtered = append(filtered, bind)
	}
	p.Binds = filtered
	fleetCommand := []string{"/run/current-system/sw/bin/fleet-control", "pressure", "--inventory", "/etc/fleet/source/fleet-inventory.json", "--json", "derpinator"}
	cmd, err := BuildBubblewrap(p, fleetCommand)
	if err != nil {
		t.Fatalf("BuildBubblewrap Fleet: %v", err)
	}
	joined := ShellString(cmd)
	if !strings.Contains(joined, "'--unshare-net'") || strings.Contains(joined, "'--share-net'") {
		t.Fatalf("Management Fleet command must remain network-isolated: %s", joined)
	}
	if !strings.Contains(joined, "'--ro-bind' '"+execSocket+"' '"+execSocket+"'") ||
		!strings.Contains(joined, "'--setenv' 'FLEET_EXEC_TRANSPORT_HANDLE' 'required'") {
		t.Fatalf("Management Fleet command lacks the exact typed exec handle: %s", joined)
	}
	for _, nonFleet := range [][]string{{"/bin/bash", "-lc", "curl https://example.invalid"}, {"/run/current-system/sw/bin/fleet", "pressure"}} {
		other, err := BuildBubblewrap(p, nonFleet)
		if err != nil {
			t.Fatalf("BuildBubblewrap non-Fleet: %v", err)
		}
		otherText := ShellString(other)
		if !strings.Contains(otherText, "'--unshare-net'") || strings.Contains(otherText, "'--share-net'") {
			t.Fatalf("Management child received host network: %s", otherText)
		}
	}
}

func TestBuildBubblewrapWorkspaceEgressUsesNarrowBinds(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
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
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Project:          "dev-all",
		Index:            3,
		Repo:             "ouroboros-ide",
		Flake:            ".#runtime-test-agent",
		RuntimeLauncher:  runtimeLauncherFixture(t),
		BubblewrapBinary: runtimeLauncherFixture(t),
		IsolationProfile: nativeplan.IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(devRoot, "ouroboros-ide", "infra", "docker", "dev", "tinyproxy", "allowlist.txt"),
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	cmd, err := BuildBubblewrap(p, []string{"true"})
	if err != nil {
		t.Fatalf("BuildBubblewrap: %v", err)
	}
	joined := ShellString(cmd)
	for _, want := range []string{
		"'--unshare-net'",
		"'--ro-bind' '/var/run/nscd/socket' '/var/run/nscd/socket'",
		"'--bind' '" + hostWorktree + "' '/workspace'",
		"'--bind' '" + hostWorktree + "' '/workspaces/dev/agent-worktrees/agent3/ouroboros-ide'",
		"'--bind' '" + commonGitDir + "' '/workspaces/dev/ouroboros-ide/.git'",
		"'--bind' '" + filepath.Join(devRoot, "agent-worktrees", "agent3", ".devhome-agent3") + "' '/workspaces/dev/agent-worktrees/agent3/.devhome-agent3'",
		"'--ro-bind' '" + devkitRoot + "' '/workspaces/dev/devkit'",
		"native proxy-bridge --listen 127.0.0.1:18888",
		"'--setenv' 'COURSIER_CACHE' '/workspaces/dev/agent-worktrees/agent3/.devhome-agent3/.cache/coursier'",
		"'--setenv' 'SBT_IVY_HOME' '/workspaces/dev/agent-worktrees/agent3/.devhome-agent3/.cache/ivy2'",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command missing %q:\n%s", want, joined)
		}
	}
	for _, forbidden := range []string{
		"'--share-net'",
		"'--bind' '" + devRoot + "' '/workspaces/dev'",
		"'--bind' '" + devRoot + "' '" + devRoot + "'",
		"'--bind' '" + hostWorktree + "' '" + hostWorktree + "'",
		"'--bind' '" + commonGitDir + "' '" + commonGitDir + "'",
		"'--bind' '" + filepath.Join(devRoot, "agent-worktrees") + "' '/workspaces/dev/agent-worktrees'",
		"'--bind' '" + filepath.Join(devRoot, ".devkit", "native-agents") + "' '/agent-state'",
		"/workspaces/dev/.cache/shared",
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("workspace-egress command contains forbidden %q:\n%s", forbidden, joined)
		}
	}
}

func TestPrepareRequiresExistingWorktree(t *testing.T) {
	tmp := t.TempDir()
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: filepath.Join(tmp, "devkit"), RuntimeAuthorityRoot: filepath.Join(tmp, "devkit")},
		Repo:  "missing",
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if err := Prepare(p); err == nil {
		t.Fatalf("expected missing worktree error")
	}
}

func TestPrepareRejectsPlainDirectoryAsReadyWorktree(t *testing.T) {
	tmp := t.TempDir()
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: filepath.Join(tmp, "devkit"), RuntimeAuthorityRoot: filepath.Join(tmp, "devkit")},
		Repo:  "ouroboros-ide",
		Index: 2,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if err := os.MkdirAll(p.Agent.HostWorktree, 0o755); err != nil {
		t.Fatalf("mkdir plain worktree path: %v", err)
	}
	if err := Prepare(p); err == nil || !strings.Contains(err.Error(), "is not a Git worktree") {
		t.Fatalf("Prepare plain directory error = %v", err)
	}
}

func TestPrepareGitBootstrapUsesPackageOwnedConsumerIdentityAndProxy(t *testing.T) {
	tmp := t.TempDir()
	hostUserHome := filepath.Join(tmp, "host-user")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519"), "private")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519.pub"), "public")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "known_hosts"), "github.com ssh-ed25519 key")
	t.Setenv("HOME", hostUserHome)

	hostHome := filepath.Join(tmp, "agent-home")
	runtimeRoot := filepath.Join(tmp, "runtime-authority")
	devctlPath := filepath.Join(runtimeRoot, "kit", "bin", "devctl")
	writeTestFile(t, devctlPath, "#!/bin/sh\nexit 99\n")
	if err := os.Chmod(devctlPath, 0o755); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(tmp, "native-egress", "product-a.sock")
	p := nativeplan.Plan{
		IsolationProfile:     nativeplan.IsolationProfileWorkspaceEgress,
		RuntimeAuthorityRoot: runtimeRoot,
		Agent: agent.Spec{
			ID:          agent.ID{Project: "dev-all", Index: 1, Repo: "ouroboros-ide"},
			HostHome:    hostHome,
			SandboxHome: "/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1",
		},
		Proxy: nativeplan.ProxyConfig{
			HTTPProxy:  "http://127.0.0.1:18888",
			UnixSocket: socketPath,
		},
	}
	sshCommand, err := PrepareGitBootstrap(p)
	if err != nil {
		t.Fatalf("PrepareGitBootstrap: %v", err)
	}
	expectedCommand := testPackageSSHCommand(t, filepath.Join(hostHome, ".ssh", "config"))
	if sshCommand != expectedCommand {
		t.Fatalf("bootstrap SSH command = %q, want %q", sshCommand, expectedCommand)
	}
	cfg := readTestFile(t, filepath.Join(hostHome, ".ssh", "config"))
	for _, want := range []string{
		"Host github.com ssh.github.com",
		"  HostName ssh.github.com",
		"  Port 443",
		"  ProxyCommand '" + devctlPath + "' -p 'dev-all' native proxy-connect --socket '" + socketPath + "' --target %h:%p",
		"  IdentityFile " + filepath.Join(hostHome, ".ssh", "id_ed25519"),
		"  IdentitiesOnly yes",
		"  BatchMode yes",
		"  StrictHostKeyChecking yes",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("bootstrap SSH config missing %q:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, p.Agent.SandboxHome) {
		t.Fatalf("host bootstrap SSH config selected sandbox-only paths:\n%s", cfg)
	}
	for _, forbidden := range []string{"127.0.0.1:18888", "ProxyCommand nc ", "https://"} {
		if strings.Contains(cfg, forbidden) {
			t.Fatalf("host bootstrap SSH config selected forbidden transport %q:\n%s", forbidden, cfg)
		}
	}
}

func TestPrepareGitBootstrapRejectsMissingPackageOwnedProxyHelper(t *testing.T) {
	tmp := t.TempDir()
	hostUserHome := filepath.Join(tmp, "host-user")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519"), "private")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519.pub"), "public")
	t.Setenv("HOME", hostUserHome)

	p := nativeplan.Plan{
		IsolationProfile:     nativeplan.IsolationProfileWorkspaceEgress,
		RuntimeAuthorityRoot: filepath.Join(tmp, "missing-runtime"),
		Agent: agent.Spec{
			ID:       agent.ID{Project: "dev-all", Index: 1, Repo: "ouroboros-ide"},
			HostHome: filepath.Join(tmp, "agent-home"),
		},
		Proxy: nativeplan.ProxyConfig{UnixSocket: filepath.Join(tmp, "proxy.sock")},
	}
	_, err := PrepareGitBootstrap(p)
	if err == nil || !strings.Contains(err.Error(), "package-owned proxy helper") {
		t.Fatalf("PrepareGitBootstrap error = %v, want package-owned proxy helper rejection", err)
	}
}

func TestPrepareGitBootstrapRejectsMissingIdentity(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmp, "empty-host-home"))
	p := nativeplan.Plan{
		Agent: agent.Spec{
			HostHome: filepath.Join(tmp, "agent-home"),
		},
	}
	_, err := PrepareGitBootstrap(p)
	if err == nil || !strings.Contains(err.Error(), "requires a seeded SSH identity") {
		t.Fatalf("PrepareGitBootstrap error = %v, want missing identity rejection", err)
	}
}

func TestSeedSSHSeedsOnlyCallerIdentityAndNeverCallerKnownHosts(t *testing.T) {
	hostUserHome := t.TempDir()
	srcSSH := filepath.Join(hostUserHome, ".ssh")
	if err := os.MkdirAll(srcSSH, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"id_ed25519":     "private",
		"id_ed25519.pub": "public",
		"known_hosts":    "github.com key",
	} {
		if err := os.WriteFile(filepath.Join(srcSSH, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", hostUserHome)

	nativeHome := filepath.Join(t.TempDir(), "native-home")
	if err := SeedSSH(nativeHome, false); err != nil {
		t.Fatalf("SeedSSH: %v", err)
	}
	for name, wantMode := range map[string]os.FileMode{
		"id_ed25519":     0o600,
		"id_ed25519.pub": 0o644,
	} {
		info, err := os.Stat(filepath.Join(nativeHome, ".ssh", name))
		if err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
		if got := info.Mode().Perm(); got != wantMode {
			t.Fatalf("%s mode = %v, want %v", name, got, wantMode)
		}
	}
	if _, err := os.Lstat(filepath.Join(nativeHome, ".ssh", "known_hosts")); !os.IsNotExist(err) {
		t.Fatalf("SeedSSH copied caller known_hosts: %v", err)
	}
}

func TestSeedCodexConfigMaterializesAndRefreshesNixAuthoredSource(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "nix-store", "codex-config.toml")
	first := "# source = nixos-wsl codex config\nmodel_provider = \"openai\"\n"
	writeTestFile(t, source, first)
	t.Setenv("DEVKIT_CODEX_CONFIG_SOURCE", source)
	home := filepath.Join(root, "consumer-home")

	if err := SeedCodexConfig(home); err != nil {
		t.Fatalf("SeedCodexConfig first materialization: %v", err)
	}
	target := filepath.Join(home, ".codex", "config.toml")
	if got := readTestFile(t, target); got != first {
		t.Fatalf("materialized Codex config = %q, want %q", got, first)
	}
	if info, err := os.Lstat(target); err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("materialized Codex config mode = %v error=%v", info, err)
	}

	second := first + "[profiles.openai]\nmodel_provider = \"openai\"\n"
	if err := os.WriteFile(source, []byte(second), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SeedCodexConfig(home); err != nil {
		t.Fatalf("SeedCodexConfig refresh: %v", err)
	}
	if got := readTestFile(t, target); got != second {
		t.Fatalf("refreshed Codex config = %q, want %q", got, second)
	}
}

func TestSeedCodexConfigRejectsRelativeSymlinkAndNonRegularSources(t *testing.T) {
	home := filepath.Join(t.TempDir(), "consumer-home")
	t.Setenv("DEVKIT_CODEX_CONFIG_SOURCE", "relative-config.toml")
	if err := SeedCodexConfig(home); err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("relative config source error = %v", err)
	}

	root := t.TempDir()
	realSource := filepath.Join(root, "real-config.toml")
	writeTestFile(t, realSource, "model_provider = \"openai\"\n")
	symlinkSource := filepath.Join(root, "config-link.toml")
	if err := os.Symlink(realSource, symlinkSource); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVKIT_CODEX_CONFIG_SOURCE", symlinkSource)
	if err := SeedCodexConfig(home); err == nil || !strings.Contains(err.Error(), "regular non-symlink") {
		t.Fatalf("symlink config source error = %v", err)
	}

	t.Setenv("DEVKIT_CODEX_CONFIG_SOURCE", root)
	if err := SeedCodexConfig(home); err == nil || !strings.Contains(err.Error(), "regular non-symlink") {
		t.Fatalf("directory config source error = %v", err)
	}
}

func TestSeedAWSSyncsConfigAndCaches(t *testing.T) {
	srcAWS := filepath.Join(t.TempDir(), ".aws")
	for _, dir := range []string{
		srcAWS,
		filepath.Join(srcAWS, "sso", "cache"),
		filepath.Join(srcAWS, "cli", "cache"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	files := map[string]string{
		"config":                                "[profile ouroboros]\nsso_session = mysesh\n",
		"credentials":                           "",
		filepath.Join("sso", "cache", "a.json"): `{"accessToken":"redacted"}`,
		filepath.Join("cli", "cache", "b.json"): `{"Credentials":{"AccessKeyId":"redacted"}}`,
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(srcAWS, rel), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	t.Setenv("DEVKIT_AWS_HOME", srcAWS)

	nativeHome := filepath.Join(t.TempDir(), "native-home")
	if err := SeedAWS(nativeHome, false); err != nil {
		t.Fatalf("SeedAWS: %v", err)
	}
	for rel, want := range files {
		if got := readTestFile(t, filepath.Join(nativeHome, ".aws", rel)); got != want {
			t.Fatalf("%s = %q, want %q", rel, got, want)
		}
		info, err := os.Stat(filepath.Join(nativeHome, ".aws", rel))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s mode = %v, want 0600", rel, got)
		}
	}
	for _, rel := range []string{".aws", filepath.Join(".aws", "sso"), filepath.Join(".aws", "sso", "cache"), filepath.Join(".aws", "cli"), filepath.Join(".aws", "cli", "cache")} {
		info, err := os.Stat(filepath.Join(nativeHome, rel))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("%s mode = %v, want 0700", rel, got)
		}
	}
}

func TestSeedAWSMaterializesExistingExternalTargetSymlink(t *testing.T) {
	srcAWS := filepath.Join(t.TempDir(), ".aws")
	writeTestFile(t, filepath.Join(srcAWS, "config"), "[profile ouroboros]\nregion = us-east-2\n")
	writeTestFile(t, filepath.Join(srcAWS, "sso", "cache", "session.json"), `{"accessToken":"redacted"}`)
	t.Setenv("DEVKIT_AWS_HOME", srcAWS)

	externalAWS := filepath.Join(t.TempDir(), "windows-aws")
	writeTestFile(t, filepath.Join(externalAWS, "sentinel"), "must-remain")
	nativeHome := filepath.Join(t.TempDir(), "native-home")
	if err := os.MkdirAll(nativeHome, 0o700); err != nil {
		t.Fatalf("mkdir native home: %v", err)
	}
	targetAWS := filepath.Join(nativeHome, ".aws")
	if err := os.Symlink(externalAWS, targetAWS); err != nil {
		t.Fatalf("symlink target AWS home: %v", err)
	}

	if err := SeedAWS(nativeHome, false); err != nil {
		t.Fatalf("SeedAWS: %v", err)
	}
	info, err := os.Lstat(targetAWS)
	if err != nil {
		t.Fatalf("lstat materialized AWS home: %v", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("target AWS home was not materialized: mode=%v", info.Mode())
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("target AWS home mode = %v, want 0700", got)
	}
	if got := readTestFile(t, filepath.Join(targetAWS, "config")); got != "[profile ouroboros]\nregion = us-east-2\n" {
		t.Fatalf("materialized config = %q", got)
	}
	if got := readTestFile(t, filepath.Join(targetAWS, "sso", "cache", "session.json")); got != `{"accessToken":"redacted"}` {
		t.Fatalf("materialized cache = %q", got)
	}
	if got := readTestFile(t, filepath.Join(externalAWS, "sentinel")); got != "must-remain" {
		t.Fatalf("external AWS target was modified: %q", got)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func countFiles(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk %s: %v", root, err)
	}
	return count
}

func runTestCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

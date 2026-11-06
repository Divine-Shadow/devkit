package launch

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"devkit/cli/devctl/internal/devkitpaths"
	"devkit/cli/devctl/internal/gitauthority"
	"devkit/cli/devctl/internal/runtime/agent"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/sshauthority"
)

func TestMain(m *testing.M) {
	if os.Getenv("DEVKIT_OWNED_GIT_PROXY_FIXTURE") == "1" {
		runOwnedGitProxyHelper()
		os.Exit(1)
	}
	if os.Getenv("DEVKIT_LAUNCH_AGENT_PROXY_HELPER") == "1" {
		os.Exit(runPostgresEndpointProxyHelper())
	}
	gitExecutable, err := exec.LookPath("git")
	if err != nil {
		panic(err)
	}
	restoreGit := gitauthority.SetExecutableForTesting(gitExecutable)
	defer restoreGit()
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
	managedConfigPath, err := filepath.Abs("../../../../../nix/managed-git-ssh-config")
	if err != nil {
		panic(err)
	}
	testSSHAuthority, err := sshauthority.NewManaged(testExecutable, knownHostsPath, managedConfigPath)
	if err != nil {
		panic(err)
	}
	resolvePackageSSHAuthority = func() (sshauthority.Authority, error) {
		return testSSHAuthority, nil
	}
	os.Exit(m.Run())
}

func TestStartPostgresEndpointProxyRejectsPreexistingSocket(t *testing.T) {
	tmp := shortSocketTempDir(t)
	socket := filepath.Join(tmp, "postgres.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err := StartPostgresEndpointProxy(postgresEndpointProxyPlan(t, socket)); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("pre-existing endpoint socket accepted: %v", err)
	}
	if info, err := os.Lstat(socket); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("pre-existing socket was modified: %#v, %v", info, err)
	}
}

func TestStartPostgresEndpointProxyRequiresTypedPingAndCleansFailedSocket(t *testing.T) {
	t.Setenv("DEVKIT_LAUNCH_AGENT_PROXY_HELPER", "1")
	t.Setenv("DEVKIT_LAUNCH_AGENT_PROXY_MODE", "path-only")
	socket := filepath.Join(shortSocketTempDir(t), "postgres.sock")
	if _, err := StartPostgresEndpointProxy(postgresEndpointProxyPlan(t, socket)); err == nil || !strings.Contains(err.Error(), "ready per-agent socket") {
		t.Fatalf("pathname-only endpoint admitted: %v", err)
	}
	if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed endpoint proxy left socket: %v", err)
	}
}

func TestStartPostgresEndpointProxyBoundsSilentAcceptedPingAndCleansSocket(t *testing.T) {
	t.Setenv("DEVKIT_LAUNCH_AGENT_PROXY_HELPER", "1")
	t.Setenv("DEVKIT_LAUNCH_AGENT_PROXY_MODE", "silent")
	socket := filepath.Join(shortSocketTempDir(t), "postgres.sock")
	started := time.Now()
	if _, err := StartPostgresEndpointProxy(postgresEndpointProxyPlan(t, socket)); err == nil || !strings.Contains(err.Error(), "ready per-agent socket") {
		t.Fatalf("silent accepted endpoint admitted: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("silent accepted endpoint cleanup exceeded bounded readiness window: %s", elapsed)
	}
	if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("silent accepted endpoint cleanup left socket: %v", err)
	}
}

func TestStartPostgresEndpointProxyPingsAndCleansExactSocket(t *testing.T) {
	t.Setenv("DEVKIT_LAUNCH_AGENT_PROXY_HELPER", "1")
	t.Setenv("DEVKIT_LAUNCH_AGENT_PROXY_MODE", "ready")
	socket := filepath.Join(shortSocketTempDir(t), "postgres.sock")
	cleanup, err := StartPostgresEndpointProxy(postgresEndpointProxyPlan(t, socket))
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(socket); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("ready endpoint socket = %#v, %v", info, err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup retained endpoint socket: %v", err)
	}
}

func postgresEndpointProxyPlan(t *testing.T, socket string) nativeplan.Plan {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return nativeplan.Plan{
		Agent:                agent.Spec{ID: agent.ID{Project: "dev-all", Repo: "ouroboros-ide", Index: 1}},
		BrokerEndpoint:       filepath.Join(t.TempDir(), "broker.sock"),
		PostgresDockerSocket: socket,
		Env:                  map[string]string{"DEVKIT_RUNTIME_BROKER_BINARY": binary},
	}
}

func shortSocketTempDir(t *testing.T) string {
	t.Helper()
	tmp, err := os.MkdirTemp("", "dke-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	return tmp
}

func runPostgresEndpointProxyHelper() int {
	if len(os.Args) != 5 || os.Args[1] != "agent-proxy" {
		return 2
	}
	socket := os.Args[2]
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		return 2
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return 2
	}
	defer listener.Close()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return 0
		}
		go func() {
			defer conn.Close()
			mode := os.Getenv("DEVKIT_LAUNCH_AGENT_PROXY_MODE")
			if mode == "silent" {
				select {}
			}
			if mode != "ready" {
				return
			}
			if _, err := http.ReadRequest(bufio.NewReader(conn)); err != nil {
				return
			}
			_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK")
		}()
	}
}

func TestSandboxPathToHostUsesDeclaredWorkspaceRoot(t *testing.T) {
	p := nativeplan.Plan{
		DevkitHostRoot:    "/nix/store/example-devkit",
		HostWorkspaceRoot: "/home/bayesartre/dev/control-plane-worktrees/agent2",
	}
	host, ok := sandboxPathToHost(p, "/workspaces/dev/wsl-nix/.cache/coursier")
	if !ok || host != "/home/bayesartre/dev/control-plane-worktrees/agent2/wsl-nix/.cache/coursier" {
		t.Fatalf("sandbox workspace translation = %q, %t", host, ok)
	}
	if _, ok := sandboxPathToHost(p, "/workspaces/other"); ok {
		t.Fatal("path outside the workspace projection was translated")
	}
}

func TestConfigureWorktreeGitSSHUsesPackageGitUnderHostilePath(t *testing.T) {
	worktree := filepath.Join(t.TempDir(), "worktree")
	gitExecutable := gitauthority.Executable()
	command := exec.Command(gitExecutable, "init", "--initial-branch=main", worktree)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	originalPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", filepath.Join(t.TempDir(), "hostile-empty-path")); err != nil {
		t.Fatal(err)
	}
	configureErr := configureWorktreeGitSSH(worktree, "/nix/store/example-openssh/bin/ssh -F /tmp/config")
	if err := os.Setenv("PATH", originalPath); err != nil {
		t.Fatal(err)
	}
	if configureErr != nil {
		t.Fatalf("configureWorktreeGitSSH under hostile PATH: %v", configureErr)
	}
	output, err := exec.Command(gitExecutable, "-C", worktree, "config", "--worktree", "--get", "core.sshCommand").CombinedOutput()
	if err != nil {
		t.Fatalf("read worktree SSH command: %v\n%s", err, output)
	}
	if got, want := strings.TrimSpace(string(output)), "/nix/store/example-openssh/bin/ssh -F /tmp/config"; got != want {
		t.Fatalf("core.sshCommand = %q, want %q", got, want)
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

func prepareNativeFixture(t *testing.T, devRoot, hostHome string) nativeplan.Plan {
	t.Helper()
	t.Setenv("CODEX_AUTH_JSON", filepath.Join(t.TempDir(), "missing-auth.json"))
	t.Setenv("DEVKIT_AWS_HOME", filepath.Join(t.TempDir(), "missing-aws"))
	t.Setenv("HOME", filepath.Join(t.TempDir(), "empty-home"))
	worktree := filepath.Join(devRoot, "worktrees", "agent1", "example")
	initTestGitWorktree(t, worktree)
	return nativeplan.Plan{
		Agent: agent.Spec{
			ID:           agent.ID{Project: "standalone", Index: 1, Repo: "example"},
			HostWorktree: worktree, SandboxWorktree: "/workspace",
			HostHome: hostHome, SandboxHome: "/agent-state/standalone-agent1/home",
			StateRoot: filepath.Join(devRoot, ".devkit", "agents", "standalone-agent1"),
		},
		DevkitHostRoot: filepath.Join(devRoot, "devkit"),
		Binds:          []nativeplan.Bind{{Source: devRoot, Target: "/workspaces/dev", Mode: "rw"}},
		Env:            map[string]string{},
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
		"'--bind' '" + p.PostgresDockerSocket + "' '" + p.PostgresDockerSocket + "'",
		"'--setenv' 'COURSIER_CACHE' '/workspaces/dev/.cache/shared/coursier'",
		"'--setenv' 'DOCKER_HOST' 'unix://" + p.PostgresDockerSocket + "'",
		"'--setenv' 'DEVKIT_GUI_TARGET_ID' ''",
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
	if !strings.Contains(cmd.Args[len(cmd.Args)-1], "export DEVKIT_GUI_TARGET_ID=''") {
		t.Fatalf("agent shell does not erase ambient GUI target identity:\n%#v", cmd.Args)
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
	if strings.Contains(joined, brokerSocket) {
		t.Fatalf("native launcher must not expose the shared broker socket:\n%s", joined)
	}
	p.Env[nativeplan.GUITargetIDEnvironment] = "hostile-ambient-target"
	if _, err := BuildBubblewrap(p, []string{"true"}); err == nil || !strings.Contains(err.Error(), "undeclared GUI target identity") {
		t.Fatalf("ordinary launcher accepted hostile GUI target identity: %v", err)
	}
	p.GUITargetConfig = &nativeplan.GUITargetConfigProjection{TargetID: "inventory-target"}
	p.Env[nativeplan.GUITargetIDEnvironment] = "inventory-target"
	registrySource := filepath.Join(tmp, "selected-registry.json")
	if err := os.WriteFile(registrySource, []byte("constructor fixture registry"), 0o444); err != nil {
		t.Fatal(err)
	}
	registryTarget := "/etc/devkit/codex-config-projections.json"
	p.Binds = append(p.Binds, nativeplan.Bind{Source: registrySource, Target: registryTarget, Mode: "ro", Required: true})
	selected, err := BuildBubblewrap(p, []string{"true"})
	if err != nil {
		t.Fatalf("GUI launcher rejected matching target identity: %v", err)
	}
	selectedJoined := ShellString(selected)
	if strings.Count(selectedJoined, "'--ro-bind' '"+registrySource+"' '"+registryTarget+"'") != 1 ||
		strings.Contains(selectedJoined, "'--bind' '"+registrySource+"' '"+registryTarget+"'") ||
		strings.Contains(selectedJoined, "'--ro-bind' '/etc' '/etc'") ||
		strings.Contains(selectedJoined, "'--ro-bind' '/etc/devkit' '/etc/devkit'") {
		t.Fatalf("selected launcher lost narrow readonly registry projection: %s", selectedJoined)
	}
	if !strings.Contains(selectedJoined, "'--setenv' 'DEVKIT_GUI_TARGET_ID' 'inventory-target'") ||
		!strings.Contains(selected.Args[len(selected.Args)-1], "export DEVKIT_GUI_TARGET_ID='inventory-target'") {
		t.Fatalf("GUI launcher did not carry exact validated target identity:\n%s", selectedJoined)
	}
	p.Env[nativeplan.GUITargetIDEnvironment] = "hostile-ambient-target"
	if _, err := BuildBubblewrap(p, []string{"true"}); err == nil || !strings.Contains(err.Error(), "does not match validated projection") {
		t.Fatalf("GUI launcher accepted mismatched target identity: %v", err)
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
	if _, err := BuildManagedAppServerBubblewrap(p, []string{"/run/current-system/sw/bin/codex", "app-server"}); err == nil || !strings.Contains(err.Error(), "workspace-egress") {
		t.Fatalf("non-workspace-egress plan accepted managed app-server: %v", err)
	}
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
	// The runtime plan now also consumes the package-owned Git connector.
	connector := filepath.Join(devkitRoot, "kit", "bin", "devctl")
	writeTestFile(t, connector, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(connector, 0o755); err != nil {
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
	if !strings.Contains(joined, "'--die-with-parent'") {
		t.Fatalf("ordinary native exec must retain parent-death coupling:\n%s", joined)
	}
	managed, err := BuildManagedAppServerBubblewrap(p, []string{"/run/current-system/sw/bin/codex", "app-server"})
	if err != nil {
		t.Fatalf("BuildManagedAppServerBubblewrap: %v", err)
	}
	managedJoined := ShellString(managed)
	if strings.Contains(managedJoined, "'--die-with-parent'") {
		t.Fatalf("managed app-server must omit only parent-death coupling:\n%s", managedJoined)
	}
	for _, want := range []string{"'--tmpfs' '/mnt'", "'--unshare-net'", "'--bind' '" + hostWorktree + "' '/workspace'"} {
		if !strings.Contains(managedJoined, want) {
			t.Fatalf("managed app-server lost workspace-egress control %q:\n%s", want, managedJoined)
		}
	}
	for _, want := range []string{
		"'--tmpfs' '/mnt'",
		"'--unshare-net'",
		"'--ro-bind' '/var/run/nscd/socket' '/var/run/nscd/socket'",
		"'--bind' '" + hostWorktree + "' '/workspace'",
		"'--bind' '" + hostWorktree + "' '/workspaces/dev/agent-worktrees/agent3/ouroboros-ide'",
		"'--bind' '" + commonGitDir + "' '/workspaces/dev/ouroboros-ide/.git'",
		"'--bind' '" + filepath.Join(devRoot, ".devkit", "native-agents", ".homes", "dev-all-agent3", "ouroboros-ide") + "' '/workspaces/dev/agent-worktrees/agent3/.devhome-agent3'",
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

func TestSeedTerraformProviderCredentialsUsesFixedHydratorAndExactConsumer(t *testing.T) {
	root := t.TempDir()
	credentialRoot := filepath.Join(root, "provider-credentials")
	hydrator := filepath.Join(root, "hydrate")
	script := fmt.Sprintf(`#!/bin/sh
set -eu
install -d -m 0700 %q
printf 'fixture-google-adc' > %q
printf 'fixture-dnsimple-env' > %q
chmod 0600 %q %q
`, credentialRoot,
		filepath.Join(credentialRoot, "google-adc.json"),
		filepath.Join(credentialRoot, "dnsimple-provider.env"),
		filepath.Join(credentialRoot, "google-adc.json"),
		filepath.Join(credentialRoot, "dnsimple-provider.env"))
	if err := os.WriteFile(hydrator, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	previousHydrator := terraformProviderCredentialHydrator
	previousRoot := terraformProviderCredentialRoot
	terraformProviderCredentialHydrator = hydrator
	terraformProviderCredentialRoot = credentialRoot
	t.Cleanup(func() {
		terraformProviderCredentialHydrator = previousHydrator
		terraformProviderCredentialRoot = previousRoot
	})

	home := filepath.Join(root, "agent1-home")
	p := nativeplan.Plan{Agent: agent.Spec{
		ID:       agent.ID{Project: "ouroboros-terraform", Repo: "ouroboros-terraform", Index: 1},
		HostHome: home,
	}}
	if err := SeedTerraformProviderCredentials(p); err != nil {
		t.Fatalf("SeedTerraformProviderCredentials: %v", err)
	}
	for _, path := range []string{
		filepath.Join(home, ".config", "gcloud", "application_default_credentials.d", "ouroboros-ai-498921-codex-terraform.json"),
		filepath.Join(home, ".config", "ouroboros", "dnsimple-provider.env"),
	} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatalf("credential target mode = %v", info.Mode())
		}
	}

	nonOwnerHome := filepath.Join(root, "agent2-home")
	nonOwner := nativeplan.Plan{Agent: agent.Spec{
		ID:       agent.ID{Project: "ouroboros-terraform", Repo: "ouroboros-terraform", Index: 2},
		HostHome: nonOwnerHome,
	}}
	if err := SeedTerraformProviderCredentials(nonOwner); err != nil {
		t.Fatalf("non-owner seed: %v", err)
	}
	if _, err := os.Stat(nonOwnerHome); !os.IsNotExist(err) {
		t.Fatalf("non-owner consumer received provider credentials")
	}
}

func TestSeedTerraformProviderCredentialsReportsOnlyApprovedHydratorReason(t *testing.T) {
	root := t.TempDir()
	hydrator := filepath.Join(root, "hydrate")
	if err := os.WriteFile(hydrator, []byte(`#!/bin/sh
printf '%s\n' 'Terraform provider credential hydration failed: approved DNSimple token source is unavailable' >&2
exit 1
`), 0o700); err != nil {
		t.Fatal(err)
	}
	previousHydrator := terraformProviderCredentialHydrator
	terraformProviderCredentialHydrator = hydrator
	t.Cleanup(func() { terraformProviderCredentialHydrator = previousHydrator })

	p := nativeplan.Plan{Agent: agent.Spec{
		ID:       agent.ID{Project: "ouroboros-terraform", Repo: "ouroboros-terraform", Index: 1},
		HostHome: filepath.Join(root, "agent1-home"),
	}}
	err := SeedTerraformProviderCredentials(p)
	if err == nil || !strings.Contains(err.Error(), "approved DNSimple token source is unavailable") {
		t.Fatalf("approved hydrator reason error = %v", err)
	}
}

func TestSeedTerraformProviderCredentialsRedactsUnapprovedHydratorStderr(t *testing.T) {
	root := t.TempDir()
	hydrator := filepath.Join(root, "hydrate")
	if err := os.WriteFile(hydrator, []byte(`#!/bin/sh
printf '%s\n' 'secret-bearing provider output' >&2
exit 1
`), 0o700); err != nil {
		t.Fatal(err)
	}
	previousHydrator := terraformProviderCredentialHydrator
	terraformProviderCredentialHydrator = hydrator
	t.Cleanup(func() { terraformProviderCredentialHydrator = previousHydrator })

	p := nativeplan.Plan{Agent: agent.Spec{
		ID:       agent.ID{Project: "ouroboros-terraform", Repo: "ouroboros-terraform", Index: 1},
		HostHome: filepath.Join(root, "agent1-home"),
	}}
	err := SeedTerraformProviderCredentials(p)
	if err == nil || err.Error() != "source-derived Terraform provider credential hydration failed" {
		t.Fatalf("unapproved hydrator stderr was not redacted: %v", err)
	}
}

func TestCopyProtectedProviderCredentialRejectsSymlinkAndUnsafeMode(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(source, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyProtectedProviderCredential(source, target); err == nil || !strings.Contains(err.Error(), "unsafe mode") {
		t.Fatalf("unsafe source mode error = %v", err)
	}
	if err := os.Chmod(source, 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(root, "source-link")
	if err := os.Symlink(source, symlink); err != nil {
		t.Fatal(err)
	}
	if err := copyProtectedProviderCredential(symlink, target); err == nil || !strings.Contains(err.Error(), "non-symlink") {
		t.Fatalf("symlink source error = %v", err)
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

	hostStateRoot := filepath.Join(tmp, "state")
	hostHome := filepath.Join(hostStateRoot, ".homes", "dev-all-agent1", "ouroboros-ide")
	runtimeRoot := filepath.Join(tmp, "runtime-authority")
	devctlPath := filepath.Join(runtimeRoot, "kit", "bin", "devctl")
	writeTestFile(t, devctlPath, "#!/bin/sh\nexit 99\n")
	if err := os.Chmod(devctlPath, 0o755); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(tmp, "native-egress", "product-a.sock")
	p := nativeplan.Plan{
		IsolationProfile:     nativeplan.IsolationProfileWorkspaceEgress,
		HostStateRoot:        hostStateRoot,
		RuntimeAuthorityRoot: runtimeRoot,
		Agent: agent.Spec{
			ID:           agent.ID{Project: "dev-all", Index: 1, Repo: "ouroboros-ide"},
			HostHome:     hostHome,
			HostWorktree: filepath.Join(tmp, "worktrees", "agent1", "ouroboros-ide"),
			StateRoot:    filepath.Join(hostStateRoot, "dev-all-agent1"),
			SandboxHome:  "/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1",
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

func TestWriteGitSSHConfigForPlanUsesManagedUnixConnectorAndSandboxIdentity(t *testing.T) {
	tmp := t.TempDir()
	hostHome := filepath.Join(tmp, "agent-home")
	writeTestFile(t, filepath.Join(hostHome, ".ssh", "work_key"), "private")
	runtimeRoot := filepath.Join(tmp, "runtime-authority")
	devctlPath := filepath.Join(runtimeRoot, "kit", "bin", "devctl")
	writeTestFile(t, devctlPath, "#!/bin/sh\nexit 99\n")
	if err := os.Chmod(devctlPath, 0o755); err != nil {
		t.Fatal(err)
	}
	sandboxHome := "/agent-state/ouroboros-ide-agent1/home"
	socketPath := filepath.Join(tmp, "native-egress", "ouroboros-ide-agent1.sock")
	p := nativeplan.Plan{
		IsolationProfile:     nativeplan.IsolationProfileWorkspaceEgress,
		RuntimeAuthorityRoot: runtimeRoot,
		Agent: agent.Spec{
			ID:          agent.ID{Project: "dev-all", Index: 1, Repo: "ouroboros-ide"},
			HostHome:    hostHome,
			SandboxHome: sandboxHome,
		},
		Proxy: nativeplan.ProxyConfig{
			HTTPProxy:  "http://127.0.0.1:18888",
			UnixSocket: socketPath,
		},
	}

	if err := WriteGitSSHConfigForPlan(p, []string{"work_key"}); err != nil {
		t.Fatalf("WriteGitSSHConfigForPlan: %v", err)
	}
	cfg := readTestFile(t, filepath.Join(hostHome, ".ssh", "config"))
	for _, want := range []string{
		"Host github.com ssh.github.com",
		"  HostName ssh.github.com",
		"  Port 443",
		"  ProxyCommand '" + devctlPath + "' -p 'dev-all' native proxy-connect --socket '" + socketPath + "' --target %h:%p",
		"  IdentityFile " + filepath.Join(sandboxHome, ".ssh", "work_key"),
		"  StrictHostKeyChecking yes",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("plan-aware SSH config missing %q:\n%s", want, cfg)
		}
	}
	for _, forbidden := range []string{
		"ProxyCommand nc ",
		"127.0.0.1:18888",
		filepath.Join(hostHome, ".ssh", "work_key"),
	} {
		if strings.Contains(cfg, forbidden) {
			t.Fatalf("plan-aware SSH config retained forbidden transport or identity %q:\n%s", forbidden, cfg)
		}
	}
}

func TestEnsureGitSSHConfigPreservesManagedUnixConnectorForWorkspaceEgress(t *testing.T) {
	tmp := t.TempDir()
	hostHome := filepath.Join(tmp, "agent-home")
	writeTestFile(t, filepath.Join(hostHome, ".ssh", "id_ed25519"), "private")

	runtimeRoot := filepath.Join(tmp, "runtime-authority")
	devctlPath := filepath.Join(runtimeRoot, "kit", "bin", "devctl")
	writeTestFile(t, devctlPath, "#!/bin/sh\nexit 99\n")
	if err := os.Chmod(devctlPath, 0o755); err != nil {
		t.Fatal(err)
	}

	worktree := filepath.Join(tmp, "dev", "agent-worktrees", "agent1", "ouroboros-terraform")
	initTestGitWorktree(t, worktree)
	sandboxHome := "/agent-state/ouroboros-terraform-agent1/home"
	socketPath := filepath.Join(tmp, "native-egress", "ouroboros-terraform-agent1.sock")
	p := nativeplan.Plan{
		IsolationProfile:     nativeplan.IsolationProfileWorkspaceEgress,
		RuntimeAuthorityRoot: runtimeRoot,
		Agent: agent.Spec{
			ID:           agent.ID{Project: "ouroboros-terraform", Index: 1, Repo: "ouroboros-terraform"},
			HostHome:     hostHome,
			SandboxHome:  sandboxHome,
			HostWorktree: worktree,
		},
		Proxy: nativeplan.ProxyConfig{
			HTTPProxy:  "http://127.0.0.1:18888",
			UnixSocket: socketPath,
		},
	}
	authority, err := resolvePackageSSHAuthority()
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureGitSSHConfig(p, authority); err != nil {
		t.Fatalf("ensureGitSSHConfig: %v", err)
	}

	configPath := filepath.Join(hostHome, ".ssh", "config")
	cfg := readTestFile(t, configPath)
	for _, want := range []string{
		"  ProxyCommand '" + devctlPath + "' -p 'ouroboros-terraform' native proxy-connect --socket '" + socketPath + "' --target %h:%p",
		"  IdentityFile " + filepath.Join(sandboxHome, ".ssh", "id_ed25519"),
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("runtime SSH config missing %q:\n%s", want, cfg)
		}
	}
	for _, forbidden := range []string{"ProxyCommand nc ", "127.0.0.1:18888", filepath.Join(hostHome, ".ssh", "id_ed25519")} {
		if strings.Contains(cfg, forbidden) {
			t.Fatalf("runtime SSH config retained forbidden transport or identity %q:\n%s", forbidden, cfg)
		}
	}

	output, err := exec.Command(gitauthority.Executable(), "-C", worktree, "config", "--worktree", "--get", "core.sshCommand").CombinedOutput()
	if err != nil {
		t.Fatalf("read worktree SSH command: %v\n%s", err, output)
	}
	wantSSHCommand := testPackageSSHCommand(t, filepath.Join(sandboxHome, ".ssh", "config"))
	if got := strings.TrimSpace(string(output)); got != wantSSHCommand {
		t.Fatalf("core.sshCommand = %q, want %q", got, wantSSHCommand)
	}
}

func TestPrepareGitBootstrapRejectsMissingPackageOwnedProxyHelper(t *testing.T) {
	tmp := t.TempDir()
	hostUserHome := filepath.Join(tmp, "host-user")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519"), "private")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519.pub"), "public")
	t.Setenv("HOME", hostUserHome)

	hostStateRoot := filepath.Join(tmp, "state")
	p := nativeplan.Plan{
		IsolationProfile:     nativeplan.IsolationProfileWorkspaceEgress,
		HostStateRoot:        hostStateRoot,
		RuntimeAuthorityRoot: filepath.Join(tmp, "missing-runtime"),
		Agent: agent.Spec{
			ID:           agent.ID{Project: "dev-all", Index: 1, Repo: "ouroboros-ide"},
			HostHome:     filepath.Join(hostStateRoot, ".homes", "dev-all-agent1", "ouroboros-ide"),
			HostWorktree: filepath.Join(tmp, "worktrees", "agent1", "ouroboros-ide"),
			StateRoot:    filepath.Join(hostStateRoot, "dev-all-agent1"),
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

func TestSeedCodexConfigDataAtomicallyMaterializesAndRefreshes0600Target(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "consumer-home")
	first := []byte("# source = immutable GUI config\nmodel_provider = \"openai\"\n")
	if err := seedCodexConfigData(home, first); err != nil {
		t.Fatalf("first materialization: %v", err)
	}
	target := filepath.Join(home, ".codex", "config.toml")
	if got := readTestFile(t, target); got != string(first) {
		t.Fatalf("materialized Codex config = %q, want %q", got, first)
	}
	if info, err := os.Lstat(target); err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("materialized Codex config mode = %v error=%v", info, err)
	} else if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Geteuid() {
		t.Fatalf("materialized Codex config owner = %#v, want uid %d", info.Sys(), os.Geteuid())
	}

	second := append(append([]byte{}, first...), []byte("[profiles.openai]\nmodel_provider = \"openai\"\n")...)
	if err := seedCodexConfigData(home, second); err != nil {
		t.Fatalf("atomic refresh: %v", err)
	}
	if got := readTestFile(t, target); got != string(second) {
		t.Fatalf("refreshed Codex config = %q, want %q", got, second)
	}
	if matches, err := filepath.Glob(filepath.Join(home, ".codex", ".config.toml.new-*")); err != nil || len(matches) != 0 {
		t.Fatalf("atomic refresh staging residue = %#v, %v", matches, err)
	}
}

func TestSeedCodexConfigDataRejectsSymlinkedDestinationParents(t *testing.T) {
	data := []byte("# immutable GUI config\n")
	t.Run("host-home", func(t *testing.T) {
		root := t.TempDir()
		realHome := filepath.Join(root, "real-home")
		if err := os.Mkdir(realHome, 0o700); err != nil {
			t.Fatal(err)
		}
		linkedHome := filepath.Join(root, "linked-home")
		if err := os.Symlink(realHome, linkedHome); err != nil {
			t.Fatal(err)
		}
		if err := seedCodexConfigData(linkedHome, data); err == nil || !strings.Contains(err.Error(), "without following links") {
			t.Fatalf("symlinked host home rejection = %v", err)
		}
		if _, err := os.Lstat(filepath.Join(realHome, ".codex", "config.toml")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("symlinked host home was mutated: %v", err)
		}
	})

	t.Run("codex-directory", func(t *testing.T) {
		root := t.TempDir()
		home := filepath.Join(root, "home")
		outside := filepath.Join(root, "outside")
		if err := os.Mkdir(home, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(outside, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(home, ".codex")); err != nil {
			t.Fatal(err)
		}
		if err := seedCodexConfigData(home, data); err == nil || !strings.Contains(err.Error(), "without following links") {
			t.Fatalf("symlinked .codex rejection = %v", err)
		}
		if _, err := os.Lstat(filepath.Join(outside, "config.toml")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("symlinked .codex target was mutated: %v", err)
		}
	})
}

func TestSeedCodexConfigDataRejectsForeignOwnedDestination(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	foreignUID := os.Geteuid() + 1
	err := seedCodexConfigDataForUID(home, []byte("# immutable GUI config\n"), foreignUID)
	if err == nil || !strings.Contains(err.Error(), "owned by uid") {
		t.Fatalf("foreign-owner rejection = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".codex", "config.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("foreign-owned destination was mutated: %v", err)
	}
}

func TestSeedCodexConfigDataRejectsNonCanonicalHostHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	nonCanonical := home + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(home)
	if err := seedCodexConfigData(nonCanonical, []byte("# immutable GUI config\n")); err == nil || !strings.Contains(err.Error(), "absolute canonical") {
		t.Fatalf("non-canonical host home rejection = %v", err)
	}
}

func TestProjectedCodexConfigVerificationRejectsSameBytePathReplacement(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	data := []byte("# immutable GUI config\n")
	if err := seedCodexConfigData(home, data); err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(home, ".codex")
	target := filepath.Join(codexPath, "config.toml")
	original, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := openedFileIdentityAndUID(original)
	if closeErr := original.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(target, target+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
	directory, err := os.Open(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := verifyProjectedCodexConfigAt(directory, "config.toml", target, data, os.Geteuid(), &identity); err == nil || !strings.Contains(err.Error(), "changed identity") {
		t.Fatalf("same-byte path replacement verification = %v", err)
	}
}

func TestSeedCodexConfigIgnoresHostileLegacyAmbientSourceForOrdinaryPlan(t *testing.T) {
	root := t.TempDir()
	hostile := filepath.Join(root, "hostile-config.toml")
	writeTestFile(t, hostile, "hostile = true\n")
	t.Setenv("DEVKIT_CODEX_CONFIG_SOURCE", hostile)
	home := filepath.Join(root, "consumer-home")
	p := nativeplan.Plan{Agent: agent.Spec{HostHome: home}}
	if err := SeedCodexConfig(p); err != nil {
		t.Fatalf("ordinary plan config seeding: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".codex", "config.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy ambient source was materialized: %v", err)
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
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(srcAWS, "credentials"))

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
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(srcAWS, "credentials"))

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
	if _, err := os.Stat(filepath.Join(targetAWS, "credentials")); !os.IsNotExist(err) {
		t.Fatalf("owned absent credential source was replaced by an ambient source: %v", err)
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

func TestSeedTerraformProviderFailureEmitsOnlyFiniteDiagnosticAndNeverProjects(t *testing.T) {
	root := t.TempDir()
	hydrator := filepath.Join(root, "hydrate")
	diagnostic := terraformProviderAWSDiagnostic{SchemaVersion: "terraform-provider-aws-diagnostic/v1", Stage: "token", ErrorCode: "ExpiredToken", FailureClass: "expired_token"}
	script := "#!/bin/sh\nprintf '%s\\n' 'PRIVATE_STDOUT'\nprintf '%s\\n' 'PRIVATE_STDERR' '" + diagnostic.wireLine() + "' 'Terraform provider credential hydration failed: approved DNSimple token source is unavailable' >&2\nexit 1\n"
	if err := os.WriteFile(hydrator, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	previousHydrator, previousRoot := terraformProviderCredentialHydrator, terraformProviderCredentialRoot
	terraformProviderCredentialHydrator, terraformProviderCredentialRoot = hydrator, filepath.Join(root, "absent-source")
	t.Cleanup(func() {
		terraformProviderCredentialHydrator, terraformProviderCredentialRoot = previousHydrator, previousRoot
	})
	home := filepath.Join(root, "consumer")
	p := nativeplan.Plan{Agent: agent.Spec{ID: agent.ID{Project: "ouroboros-terraform", Repo: "ouroboros-terraform", Index: 1}, HostHome: home}}
	err := SeedTerraformProviderCredentials(p)
	if err == nil || strings.Contains(err.Error(), "PRIVATE") || !strings.Contains(err.Error(), "approved DNSimple token source is unavailable") {
		t.Fatalf("unsafe or missing failure evidence: %v", err)
	}
	if got := parseTerraformProviderAWSDiagnostic([]byte(err.Error())); got == nil || got.ErrorCode != "ExpiredToken" {
		t.Fatalf("diagnostic lost: %v", err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("failed source read created consumer projection")
	}
}

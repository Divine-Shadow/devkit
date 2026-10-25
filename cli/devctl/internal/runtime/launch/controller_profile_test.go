package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"devkit/cli/devctl/internal/devkitpaths"
	"devkit/cli/devctl/internal/runtime/egressproxy"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

func initControllerSourceWorktree(t *testing.T, path, origin string) string {
	t.Helper()
	initTestGitWorktree(t, path)
	writeTestFile(t, filepath.Join(path, "README.md"), "source\n")
	_, source, _, _ := runtime.Caller(0)
	posture, err := os.ReadFile(filepath.Join(filepath.Dir(source), "../../../../../kit/docs/execution-posture.md"))
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(path, "AGENTS.md"), string(posture))
	runTestCommand(t, "", "git", "-C", path, "add", "README.md", "AGENTS.md")
	runTestCommand(t, "", "git", "-C", path, "-c", "user.name=Devkit Test", "-c", "user.email=devkit@example.invalid", "commit", "-m", "fixture")
	runTestCommand(t, "", "git", "-C", path, "remote", "add", "origin", origin)
	output, err := exec.Command("git", "-C", path, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("read fixture HEAD: %v: %s", err, output)
	}
	return strings.TrimSpace(string(output))
}

func hashControllerTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func listenControllerTestSocket(t *testing.T, path string) net.Listener {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func TestManagementEntryWithoutIssuerPreservesContainment(t *testing.T) {
	withProductGovernanceEnvironmentFixture(t)
	root, err := os.MkdirTemp("", "devkit-v4-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(nativeplan.ManagementControllerProfileEnvironment, "")
	base, err := nativeplan.Build(nativeplan.BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Project:          "dev-workspace",
		Index:            2,
		Repo:             "shadow-throne-management",
		RuntimeLauncher:  runtimeLauncherFixture(t),
		BubblewrapBinary: runtimeLauncherFixture(t),
		IsolationProfile: nativeplan.IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(root, "allowlist.txt"),
	})
	if err != nil {
		t.Fatalf("Build base: %v", err)
	}
	managementOrigin := "git@github.com:example/management.git"
	managementRevision := initControllerSourceWorktree(t, base.Agent.HostWorktree, managementOrigin)
	wslRoot := filepath.Join(root, "wsl-nix-lease")
	wslOrigin := "git@github.com:example/wsl-nix.git"
	wslRevision := initControllerSourceWorktree(t, wslRoot, wslOrigin)

	manifestPath := filepath.Join(root, "management-controller-convergence.json")
	operationSocket := filepath.Join(root, "run", "operation", "control.sock")
	workLedgerDirectory := filepath.Join(root, "fleet-work")
	execSocket := filepath.Join(root, "run", "exec", "control.sock")
	previousManifest := nativeplan.ManagementControllerProfileManifestPath
	previousOperationSocket := nativeplan.WorkspaceControllerOperationSocket
	previousWorkLedgerDirectory := nativeplan.WorkspaceControllerWorkLedgerDirectory
	previousWorkLedgerPath := nativeplan.WorkspaceControllerWorkLedgerPath
	previousExecSocket := nativeplan.WorkspaceControllerExecSocket
	previousFleetInventory := nativeplan.WorkspaceControllerSourceInventory
	previousGUIInventory := nativeplan.WorkspaceControllerGUIInventory
	previousTI4Source := nativeplan.WorkspaceEgressCanonicalTI4Source
	previousNSCDSource := nativeplan.WorkspaceEgressNSCDSource
	previousIdentityOwner := nativeplan.ManagementControllerSocketExpectedOwner
	previousIdentityGroup := nativeplan.ManagementControllerSocketExpectedGroup
	fleetInventory := filepath.Join(root, "fleet-inventory.json")
	guiInventory := filepath.Join(root, "fleet-codex-gui-inventory.json")
	ti4Source := filepath.Join(root, "ti4-calculator")
	nscdSource := filepath.Join(root, "run", "nscd.sock")
	resolvConf := filepath.Join(root, "resolv.conf")
	currentUser, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	currentGroup, err := user.LookupGroupId(currentUser.Gid)
	if err != nil {
		t.Fatal(err)
	}
	nativeplan.ManagementControllerProfileManifestPath = manifestPath
	nativeplan.WorkspaceControllerOperationSocket = operationSocket
	nativeplan.WorkspaceControllerWorkLedgerDirectory = workLedgerDirectory
	nativeplan.WorkspaceControllerWorkLedgerPath = filepath.Join(workLedgerDirectory, "work.sqlite")
	nativeplan.WorkspaceControllerExecSocket = execSocket
	nativeplan.WorkspaceControllerSourceInventory = fleetInventory
	nativeplan.WorkspaceControllerGUIInventory = guiInventory
	nativeplan.WorkspaceEgressCanonicalTI4Source = ti4Source
	nativeplan.WorkspaceEgressNSCDSource = nscdSource
	nativeplan.ManagementControllerSocketExpectedOwner = currentUser.Username
	nativeplan.ManagementControllerSocketExpectedGroup = currentGroup.Name
	t.Cleanup(func() {
		nativeplan.ManagementControllerProfileManifestPath = previousManifest
		nativeplan.WorkspaceControllerOperationSocket = previousOperationSocket
		nativeplan.WorkspaceControllerWorkLedgerDirectory = previousWorkLedgerDirectory
		nativeplan.WorkspaceControllerWorkLedgerPath = previousWorkLedgerPath
		nativeplan.WorkspaceControllerExecSocket = previousExecSocket
		nativeplan.WorkspaceControllerSourceInventory = previousFleetInventory
		nativeplan.WorkspaceControllerGUIInventory = previousGUIInventory
		nativeplan.WorkspaceEgressCanonicalTI4Source = previousTI4Source
		nativeplan.WorkspaceEgressNSCDSource = previousNSCDSource
		nativeplan.ManagementControllerSocketExpectedOwner = previousIdentityOwner
		nativeplan.ManagementControllerSocketExpectedGroup = previousIdentityGroup
	})
	operationListener := listenControllerTestSocket(t, operationSocket)
	_ = operationListener
	listenControllerTestSocket(t, execSocket)
	listenControllerTestSocket(t, nscdSource)
	writeTestFile(t, fleetInventory, "fleet\n")
	writeTestFile(t, guiInventory, "gui\n")
	writeTestFile(t, resolvConf, "nameserver 127.0.0.1\n")
	if err := os.MkdirAll(workLedgerDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(workLedgerDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ti4Source, 0o755); err != nil {
		t.Fatal(err)
	}

	profile := nativeplan.ManagementControllerProfile{
		SchemaVersion:       nativeplan.ManagementControllerProfileSchema,
		ProfileIdentity:     nativeplan.ManagementControllerProfileIdentity,
		MountPolicyIdentity: nativeplan.ManagementControllerMountPolicyIdentity,
		OperationHandle: nativeplan.ControllerProfileOperationHandle{
			Environment:   nativeplan.ControllerOperationHandleEnvironment,
			RequiredValue: nativeplan.ControllerOperationHandleRequiredValue,
			SocketPath:    operationSocket,
			SocketOwner:   nativeplan.ManagementControllerSocketExpectedOwner,
			SocketGroup:   nativeplan.ManagementControllerSocketExpectedGroup,
			NoFollow:      true,
		},
		SourceRoots: nativeplan.ControllerProfileSourceRoots{
			Management: nativeplan.ControllerProfileSourceRoot{
				LogicalPath: nativeplan.ManagementControllerLogicalRoot,
				BackingPath: base.Agent.HostWorktree,
				Origin:      managementOrigin,
				Revision:    managementRevision,
			},
			WSLNix: nativeplan.ControllerProfileSourceRoot{
				LogicalPath: nativeplan.ManagementControllerWSLNixLogicalRoot,
				BackingPath: wslRoot,
				Origin:      wslOrigin,
				Revision:    wslRevision,
			},
		},
		Inventories: nativeplan.ControllerProfileInventories{
			Fleet: nativeplan.ControllerProfileFileIdentity{
				Path:   fleetInventory,
				SHA256: hashControllerTestFile(t, fleetInventory),
			},
			GUI: nativeplan.ControllerProfileFileIdentity{
				Path:   guiInventory,
				SHA256: hashControllerTestFile(t, guiInventory),
			},
		},
		Targets: nativeplan.ControllerProfileTargets{
			Controller:       nativeplan.ManagementControllerNode,
			ControllerGUI:    nativeplan.ManagementControllerGUI,
			ProductAgentHost: nativeplan.ManagementControllerNode,
		},
		Schemas: nativeplan.ControllerProfileSchemas{
			Request:  nativeplan.ControllerOperationRequestSchema,
			Accepted: nativeplan.ControllerOperationAcceptedSchema,
			Event:    nativeplan.ControllerOperationEventSchema,
			Receipt:  nativeplan.ControllerOperationReceiptSchema,
		},
		Kinds: []string{"app-rpc.exec", "auth.exec", "fleet.exec", "nixos.deploy-closure", "nixos.wsl-sentinel", "gui.replace-controller", "gui.start-app-server", "product-agent.reconcile-absence"},
	}
	operationStoreRoot := filepath.Join(root, "nix", "store")
	previousProfileStoreRoot := nativeplan.ControllerProfileStoreRoot
	nativeplan.ControllerProfileStoreRoot = operationStoreRoot
	t.Cleanup(func() {
		nativeplan.ControllerProfileStoreRoot = previousProfileStoreRoot
	})
	writeExecutable := func(path string) string {
		writeTestFile(t, path, "#!/bin/sh\nexit 0\n")
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	profile.Broker = nativeplan.ControllerProfileBroker{
		Executable:     writeExecutable(filepath.Join(operationStoreRoot, "fleet-control", "bin", "fleet-control")),
		SourceRevision: strings.Repeat("c", 40),
		StateDirectory: "/var/lib/fleet-controller-operation",
	}
	sshConfigPath := filepath.Join(operationStoreRoot, "management-controller-github-ssh-config")
	writeTestFile(t, sshConfigPath, "Host github.com\n")
	profile.SourceAcquisition = nativeplan.ControllerProfileSourceAcquisition{
		GitExecutable: writeExecutable(filepath.Join(operationStoreRoot, "git", "bin", "git")),
		SSHExecutable: writeExecutable(filepath.Join(operationStoreRoot, "openssh", "bin", "ssh")),
		SSHConfigPath: sshConfigPath,
	}
	profile.ProductAgentLifecycle = nativeplan.ControllerProfileProductAgentLifecycle{
		SocketPath:         "/run/fleet-product-agent-lifecycle/control.sock",
		Executable:         writeExecutable(filepath.Join(operationStoreRoot, "product-agent", "bin", "product-agent")),
		Operation:          "cycle-test",
		ReconcileOperation: "reconcile-absence",
		RequestSchema:      "fleet-control/product-agent-local-request/v2",
		EventSchema:        "fleet-control/product-agent-local-event/v2",
		Mode:               "0660",
		Owner:              "root",
		Group:              "product-agent-operators",
		NoFollow:           true,
	}
	profile.ProductStationReset = nativeplan.ControllerProfileProductStationReset{
		SocketPath:    "/run/fleet-product-station-reset-effect/control.sock",
		Executable:    writeExecutable(filepath.Join(operationStoreRoot, "devkit", "kit", "bin", "devctl")),
		Operation:     "product.station-reset",
		RequestSchema: "fleet-control/controller-local-product-station-reset-request/v1",
		EventSchema:   "fleet-control/controller-local-product-station-reset-event/v1",
		ServiceUser:   nativeplan.ManagementControllerSocketExpectedOwner,
		Mode:          "0660",
		Owner:         "root",
		Group:         "product-station-reset-operators",
		NoFollow:      true,
	}
	profile.FleetRecovery = nativeplan.ControllerProfileFleetRecovery{
		Controller:     nativeplan.ManagementControllerNode,
		Target:         "manifest-selected-station",
		PackagePath:    filepath.Join(operationStoreRoot, "fleet-recovery"),
		ExecutablePath: writeExecutable(filepath.Join(operationStoreRoot, "fleet-recovery", "bin", "devops-fleet-recovery")),
		SourceRevision: strings.Repeat("f", 40),
	}
	profile.NixOSDeployment = nativeplan.ControllerProfileNixOSDeployment{
		SocketPath:    "/run/fleet-nixos-deploy-effect/control.sock",
		Operation:     "nixos.deploy-closure",
		RequestSchema: "fleet-control/nixos-deploy-local-request/v1",
		EventSchema:   "fleet-control/nixos-deploy-local-event/v1",
		ServiceUser:   nativeplan.ManagementControllerSocketExpectedOwner,
		Mode:          "0660",
		Owner:         "root",
		Group:         "fleet-deployment-operators",
		NoFollow:      true,
	}
	profile.AuthRefresh = nativeplan.ControllerProfileAuthRefresh{
		IdentityRoot:     "/home/bayesartre/.codex-identities",
		CodexExecutable:  writeExecutable(filepath.Join(operationStoreRoot, "codex", "bin", "codex")),
		Model:            "gpt-6.1-sol",
		ReasoningEffort:  "medium",
		ServiceTier:      "default",
		WorkingDirectory: "/tmp",
	}
	remoteWorkspaceRoot := filepath.Dir(base.Agent.HostWorktree)
	remoteHostHome := filepath.Join(remoteWorkspaceRoot, ".devhome-agent2")
	remoteHome := filepath.Join(remoteHostHome, ".codex")
	profile.RemoteProductGUI = nativeplan.ControllerProfileRemoteProductGUI{
		SchemaVersion: "wsl-nix/remote-product-gui-authority/v2",
		Targets: []nativeplan.ControllerProfileRemoteProductGUITarget{{
			Agent: 2, ConfigPath: filepath.Join(remoteHome, "config.toml"), GovernanceWorkspaceID: "agent2",
			Home: nativeplan.ControllerProfilePathPair{Host: remoteHostHome, Remote: remoteHostHome},
			ID:   "test-2", Kind: "devkit-agent", RemoteCodexHome: remoteHome,
			RuntimeProfile: "dev-all", SocketName: "a2-app.sock", Station: "test",
			Transport: nativeplan.ControllerProfileRemoteProductGUITransport{
				Address: "100.64.0.2", AddressFamily: "AF_INET", ClientIdentityHandle: "identity-fleet",
				Connector: "direct", HostKeyAlias: "test", Port: 22, PreferredRoute: "tailnet-direct",
				ServerHostKeyFingerprint: "SHA256:test", User: nativeplan.ManagementControllerSocketExpectedOwner,
				WorkerAlias: "test-nix",
			},
			WorkspaceRoot: remoteWorkspaceRoot,
			Worktree:      nativeplan.ControllerProfilePathPair{Host: base.Agent.HostWorktree, Remote: base.Agent.HostWorktree},
		}},
		Transport: nativeplan.ControllerProfileRemoteProductGUIAuthorityTransport{
			ProtectedIdentityHandles: []nativeplan.ControllerProfileProtectedIdentityHandle{{
				ID: "identity-fleet", RuntimePath: "/run/credentials/fleet-controller-operation.service/identity-fleet",
				PublicKeyFingerprint: "SHA256:client", TargetIDs: []string{"test-2"},
				RegularFile: true, NoFollow: true, ReadableByService: true,
			}},
			ServiceNetwork: nativeplan.ControllerProfileServiceNetwork{
				TargetAddressFamilies: []string{"AF_INET"},
				BrokerAddressFamilies: []string{"AF_UNIX", "AF_INET", "AF_NETLINK"},
			},
			SSHExecutable:       profile.SourceAcquisition.SSHExecutable,
			SSHKeygenExecutable: profile.SourceAcquisition.SSHExecutable,
		},
	}
	codexConfigPath := filepath.Join(operationStoreRoot, "codex-config.toml")
	writeTestFile(t, codexConfigPath, "# permission_contract = wsl-nix/codex-granular-custom/v1\n")
	profile.CodexPermissions = nativeplan.ControllerProfileCodexPermissions{
		ApprovalPolicy:    nativeplan.ControllerProfileCodexApprovalPolicy{Granular: nativeplan.ControllerProfileCodexGranularPolicy{}},
		ApprovalsReviewer: "user", Contract: "wsl-nix/codex-granular-custom/v1", DestinationMode: "0600",
		Mode: "custom", SandboxMode: "danger-full-access", Source: "config.toml",
		SourcePath: codexConfigPath, SourceSHA256: hashControllerTestFile(t, codexConfigPath),
		TargetProjection: nativeplan.ControllerProfileCodexTargetProjection{
			ByteEqualToSource: true, Group: nativeplan.ManagementControllerSocketExpectedGroup, Mode: "0600",
			NoFollow: true, Owner: nativeplan.ManagementControllerSocketExpectedOwner,
			PathRule: "remoteProductGUI.targets[].configPath", RegularFile: true,
		},
	}
	manifestBytes, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, manifestPath, string(manifestBytes)+"\n")

	// A new consumer reads configuration directly, retaining strict decoding
	// of the protected host adapters without any issued runtime identity.
	if decoded, err := nativeplan.LoadManagementControllerProfile(manifestPath); err != nil {
		t.Fatalf("fresh consumer plan decoder rejected authRefresh: %v", err)
	} else if decoded.AuthRefresh != profile.AuthRefresh {
		t.Fatalf("fresh consumer plan decoder changed authRefresh: %#v", decoded.AuthRefresh)
	}
	manifestObject := strings.TrimSpace(string(manifestBytes))
	writeManifest := func(t *testing.T, data []byte) {
		t.Helper()
		if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, testCase := range []struct {
		name string
		data []byte
		want string
	}{
		{name: "unknown field", data: append(append([]byte{}, manifestObject[:len(manifestObject)-1]...), []byte(`,"unexpected":true}`)...), want: "unknown field"},
		{name: "trailing JSON", data: append(append([]byte{}, manifestBytes...), []byte(`{}`)...), want: "trailing JSON value"},
		{name: "malformed JSON", data: []byte(`{"authRefresh":`), want: "unexpected EOF"},
	} {
		t.Run("fresh consumer plan decoder rejects "+testCase.name, func(t *testing.T) {
			writeManifest(t, testCase.data)
			t.Cleanup(func() { writeManifest(t, append(manifestBytes, '\n')) })
			if _, err := nativeplan.LoadManagementControllerProfile(manifestPath); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("manifest error = %v, want %q", err, testCase.want)
			}
		})
	}
	writeManifest(t, append(manifestBytes, '\n'))

	skillsStoreRoot := withManagementSkillsStoreRoot(t)
	skills := writeManagementSkillsFixture(t, skillsStoreRoot, "aaaaaaaa-management-skills", strings.Repeat("d", 40), map[string]string{
		"fleet-health-hypervisor/SKILL.md": "immutable\n",
	})
	t.Setenv("DEVKIT_MANAGEMENT_SKILLS_ROOT", skills.SkillsRoot)
	t.Setenv("DEVKIT_MANAGEMENT_SOURCE_REV", skills.SourceRev)
	t.Setenv("DEVKIT_MANAGEMENT_SKILL_SHA256", skills.PrimarySHA)
	t.Setenv("DEVKIT_CODEX_CONFIG_SOURCE", "")
	t.Setenv("CODEX_AUTH_JSON", filepath.Join(root, "missing-auth.json"))
	t.Setenv("DEVKIT_AWS_HOME", filepath.Join(root, "missing-aws"))
	t.Setenv("HOME", filepath.Join(root, "empty-home"))
	t.Setenv(nativeplan.ManagementControllerProfileEnvironment, nativeplan.ManagementControllerProfileIdentity)
	writeTestFile(t, filepath.Join(devRoot, ".devkit", "ouro8-governance-env.sh"), "export TEST=1\n")
	writeTestFile(t, filepath.Join(devRoot, ".devkit", "ouro8-governance-repo-env.json"), "{}\n")
	if err := os.MkdirAll(filepath.Join(devRoot, ".devkit", "governance-control-plane"), 0o755); err != nil {
		t.Fatal(err)
	}
	proxySocket := filepath.Join(root, "proxy.sock")
	receiving := os.Getenv("DEVKIT_TEST_RECEIVING_DEVCTL") != ""
	if receiving {
		_, source, _, _ := runtime.Caller(0)
		allowlist, err := os.ReadFile(filepath.Join(filepath.Dir(source), "../../../../../kit/proxy/allowlist.txt"))
		if err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(root, "allowlist.txt"), string(allowlist))
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- egressproxy.Serve(ctx, egressproxy.Config{SocketPath: proxySocket, AllowlistPath: filepath.Join(root, "allowlist.txt")})
		}()
		t.Cleanup(func() {
			cancel()
			if err := <-done; err != nil {
				t.Error(err)
			}
		})
		deadline := time.Now().Add(5 * time.Second)
		for {
			if info, err := os.Stat(proxySocket); err == nil && info.Mode()&os.ModeSocket != 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("candidate egress proxy did not start")
			}
			time.Sleep(10 * time.Millisecond)
		}
	} else {
		listenControllerTestSocket(t, proxySocket)
	}
	// The runtime plan now also consumes the package-owned Git connector.
	connector := filepath.Join(devkitRoot, "kit", "bin", "devctl")
	launcher, bwrap := runtimeLauncherFixture(t), runtimeLauncherFixture(t)
	if receiving {
		candidate := os.Getenv("DEVKIT_TEST_RECEIVING_DEVCTL")
		launcher = os.Getenv("DEVKIT_TEST_RECEIVING_RUNTIME")
		bwrap = os.Getenv("DEVKIT_TEST_RECEIVING_BWRAP")
		for _, path := range []string{candidate, launcher, bwrap} {
			if !strings.HasPrefix(path, "/nix/store/") || !isExecutable(path) {
				t.Fatalf("receiving requires built immutable tool: %q", path)
			}
		}
		if err := os.MkdirAll(filepath.Dir(connector), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(candidate, connector); err != nil {
			t.Fatal(err)
		}
	} else {
		writeTestFile(t, connector, "#!/bin/sh\nexit 0\n")
		if err := os.Chmod(connector, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p, err := nativeplan.Build(nativeplan.BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Project:          "dev-workspace",
		Index:            2,
		Repo:             "shadow-throne-management",
		RuntimeLauncher:  launcher,
		BubblewrapBinary: bwrap,
		IsolationProfile: nativeplan.IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(root, "allowlist.txt"),
		ProxySocket:      proxySocket,
		DNSResolvConf:    resolvConf,
	})
	if err != nil {
		t.Fatalf("Build v4: %v", err)
	}
	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare v4: %v", err)
	}
	command, err := BuildBubblewrap(p, []string{"/run/current-system/sw/bin/fleet", "operation", "status", "op-1"})
	if err != nil {
		t.Fatalf("BuildBubblewrap v4: %v", err)
	}
	text := ShellString(command)
	for _, want := range []string{
		"'--unshare-net'",
		"'--ro-bind' '/nix/store' '/nix/store'",
		"'--ro-bind' '/nix/var/nix' '/nix/var/nix'",
		"'--bind' '" + wslRoot + "' '/workspaces/dev/wsl-nix'",
		"'--ro-bind' '" + operationSocket + "' '" + operationSocket + "'",
		"'--bind' '" + workLedgerDirectory + "' '" + workLedgerDirectory + "'",
		"'--ro-bind' '" + fleetInventory + "' '" + fleetInventory + "'",
		"'--ro-bind' '" + guiInventory + "' '" + guiInventory + "'",
		"'--bind' '" + ti4Source + "' '/workspaces/dev/ti4-calculator'",
		"'--setenv' 'DEVKIT_MANAGEMENT_CONTROLLER_PROFILE' 'management-controller-convergence/v1'",
		"'--setenv' 'DEVKIT_NATIVE_MOUNT_POLICY_IDENTITY' 'devkit/workspace-egress/v4'",
		"'--setenv' 'FLEET_CONTROLLER_OPERATION_HANDLE' 'required'",
		"'--setenv' 'FLEET_WORK_DB' '" + nativeplan.WorkspaceControllerWorkLedgerPath + "'",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("v4 command missing %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{
		"/home/bayesartre/.ssh",
		"/home/bayesartre/.codex-identities",
		"/mnt/",
		"/var/lib/fleet-controller-operation",
		execSocket,
		"FLEET_EXEC_TRANSPORT_HANDLE",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("v4 command exposes forbidden host authority %q:\n%s", forbidden, text)
		}
	}
	if err := os.Chmod(workLedgerDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildBubblewrap(p, []string{"true"}); err == nil ||
		!strings.Contains(err.Error(), "non-symlink mode 0700 directory") {
		t.Fatalf("unprotected work ledger error = %v, want fail-closed capability rejection", err)
	}
	if err := os.Chmod(workLedgerDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	dirtyPath := filepath.Join(wslRoot, "unexpected-untracked")
	writeTestFile(t, dirtyPath, "dirty\n")
	if _, err := BuildBubblewrap(p, []string{"true"}); err == nil || !strings.Contains(err.Error(), "WSL/Nix controller source root") || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("dirty WSL source error = %v, want fail-closed source identity rejection", err)
	}
	if err := os.Remove(dirtyPath); err != nil {
		t.Fatal(err)
	}
	if err := operationListener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(operationSocket); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	coldListener := listenControllerTestSocket(t, operationSocket).(*net.UnixListener)
	// A replaced socket and absent issuer identity no longer block entry.
	if _, err := BuildBubblewrap(p, []string{"true"}); err != nil {
		t.Fatal(err)
	}
	if err := coldListener.SetDeadline(time.Now().Add(25 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	connection, err := coldListener.Accept()
	if err == nil {
		connection.Close()
		t.Fatal("sandbox entry activated a host provider")
	}
	if e, ok := err.(net.Error); !ok || !e.Timeout() {
		t.Fatalf("accept: %v", err)
	}
	if err := coldListener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildBubblewrap(p, []string{"true"}); err != nil {
		t.Fatalf("absent optional host adapter blocks work: %v", err)
	}
	if receiving {
		runControllerCandidateReceiving(t, p, root)
	}
	// A supplied socket is still a protected host capability.
	listenControllerTestSocket(t, operationSocket)
	if err := os.Chmod(operationSocket, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildBubblewrap(p, []string{"true"}); err == nil {
		t.Fatal("unprotected host socket admitted")
	}
}

// This receiving exercise uses the real source-built Devctl proxy, runtime shell,
// Bubblewrap and egress server. It does not launch Codex or touch any live lane.
func runControllerCandidateReceiving(t *testing.T, p nativeplan.Plan, root string) {
	t.Helper()
	protected := filepath.Join(root, "protected-host-data")
	writeTestFile(t, protected, "preserve\n")
	sibling := filepath.Join(p.HostWorktreeRoot, "unbound-sibling", "protected-data")
	writeTestFile(t, sibling, "preserve sibling\n")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("host-only\n"))
	})}
	go server.Serve(listener)
	t.Cleanup(func() { _ = server.Close() })
	hostURL := "http://" + listener.Addr().String()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	response, err := client.Get(hostURL)
	if err != nil {
		t.Fatalf("host canary must be reachable before entry: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("host canary status: %s", response.Status)
	}
	workload := `set -eu
        test ! -e /run/fleet-controller-operation/identity.json
        test ! -e /mnt/c
        test ! -e /var/run/docker.sock
        test ! -e ` + shellQuote(protected) + `
        test ! -e ` + shellQuote(sibling) + `
        test ! -e ` + shellQuote(filepath.Join(p.SandboxWorktreeRoot, "unbound-sibling", "protected-data")) + `
        grep -F 'Entry no longer reads or mounts a broker-issued identity' AGENTS.md
        cat > /tmp/Arithmetic.java <<'JAVA'
class Arithmetic {
    public static void main(String[] args) {
        int result = Math.addExact(-2, 3);
        if (result != 1) throw new AssertionError(result);
        System.out.println("candidate-result=" + result);
    }
}
JAVA
        javac -d /tmp /tmp/Arithmetic.java
        java -cp /tmp Arithmetic | tee "$HOME/receiving-result"
        code=$(curl -s --max-time 5 -o /tmp/denied-response -w '%{http_code}' http://denied.invalid)
        test "$code" = 403
        if curl --noproxy '*' -s --connect-timeout 1 --max-time 2 ` + shellQuote(hostURL) + `; then exit 1; fi
        printf '%s\n' candidate-containment-and-egress-ok
    `
	run := func(args []string) string {
		t.Helper()
		command, err := BuildBubblewrap(p, args)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, command.Path, command.Args...)
		cmd.Dir = command.Dir
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("candidate receiving: %v\n%s", err, output)
		}
		return string(output)
	}
	output := run([]string{"bash", "-c", workload})
	if !strings.Contains(output, "candidate-result=1") || !strings.Contains(output, "candidate-containment-and-egress-ok") {
		t.Fatalf("incomplete receiving result: %s", output)
	}
	next := run([]string{"bash", "-c", "cat \"$HOME/receiving-result\""})
	if !strings.Contains(next, "candidate-result=1") {
		t.Fatalf("subsequent invocation lost result: %s", next)
	}
	if data, err := os.ReadFile(protected); err != nil || string(data) != "preserve\n" {
		t.Fatalf("protected host data changed: %v", err)
	}
	if data, err := os.ReadFile(sibling); err != nil || string(data) != "preserve sibling\n" {
		t.Fatalf("protected sibling data changed: %v", err)
	}
	t.Logf("actual candidate receiving and subsequent invocation:\n%s%s", output, next)
}

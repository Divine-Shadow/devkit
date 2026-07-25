package launch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"devkit/cli/devctl/internal/devkitpaths"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

func initControllerSourceWorktree(t *testing.T, path, origin string) string {
	t.Helper()
	initTestGitWorktree(t, path)
	writeTestFile(t, filepath.Join(path, "README.md"), "source\n")
	runTestCommand(t, "", "git", "-C", path, "add", "README.md")
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

func writeControllerOperationIdentityFixture(t *testing.T, profile nativeplan.ManagementControllerProfile, storeRoot string) {
	t.Helper()
	authorityPath := filepath.Join(storeRoot, "authority", "authority.json")
	writeTestFile(t, authorityPath, `{"schemaVersion":"fleet-control/controller-operation-authority/v1"}`+"\n")
	executablePath := profile.Broker.Executable
	packagePath := filepath.Dir(filepath.Dir(executablePath))
	socketInfo, err := os.Lstat(nativeplan.WorkspaceControllerOperationSocket)
	if err != nil {
		t.Fatal(err)
	}
	socketStat := socketInfo.Sys().(*syscall.Stat_t)
	identity := controllerOperationIdentity{
		SchemaVersion: nativeplan.ControllerOperationIdentitySchema,
		State:         "ready",
		AuthorityManifest: controllerOperationFileIdentity{
			Path: authorityPath, SHA256: hashControllerTestFile(t, authorityPath),
		},
		Package: controllerOperationPackageIdentity{
			Path: packagePath, ExecutablePath: executablePath,
		},
		Socket: controllerOperationSocketIdentity{
			Path:           nativeplan.WorkspaceControllerOperationSocket,
			Device:         uint64(socketStat.Dev),
			Inode:          socketStat.Ino,
			ChangeTimeSec:  socketStat.Ctim.Sec,
			ChangeTimeNsec: socketStat.Ctim.Nsec,
			UID:            uint32(socketStat.Uid),
			GID:            uint32(socketStat.Gid),
			Mode:           uint32(socketInfo.Mode().Perm()),
		},
		SourceInventories: controllerOperationInventories{
			Fleet: controllerOperationFileIdentity{
				Path: profile.Inventories.Fleet.Path, SHA256: profile.Inventories.Fleet.SHA256,
			},
			GUI: controllerOperationFileIdentity{
				Path: profile.Inventories.GUI.Path, SHA256: profile.Inventories.GUI.SHA256,
			},
		},
		SourceRoots: controllerOperationSourceRoots{
			ManagementHostPath:     profile.SourceRoots.Management.BackingPath,
			ManagementConsumerPath: profile.SourceRoots.Management.LogicalPath,
			WSLHostPath:            profile.SourceRoots.WSLNix.BackingPath,
			WSLConsumerPath:        profile.SourceRoots.WSLNix.LogicalPath,
			WSLOrigin:              profile.SourceRoots.WSLNix.Origin,
			WSLRevision:            profile.SourceRoots.WSLNix.Revision,
		},
		Targets: controllerOperationTargets{
			ControllerNode: profile.Targets.Controller,
			ControllerGUI:  profile.Targets.ControllerGUI,
			DrTalos:        profile.Targets.DrTalos,
		},
		Schemas:   profile.Schemas,
		Kinds:     append([]string(nil), profile.Kinds...),
		ServerPID: os.Getpid(),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, nativeplan.WorkspaceControllerOperationIdentity, string(data)+"\n")
	if err := os.Chmod(nativeplan.WorkspaceControllerOperationIdentity, 0o400); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareAndBubblewrapUseExactManagementControllerV4Profile(t *testing.T) {
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
	operationIdentity := filepath.Join(root, "run", "operation", "identity.json")
	execSocket := filepath.Join(root, "run", "exec", "control.sock")
	previousManifest := nativeplan.ManagementControllerProfileManifestPath
	previousOperationSocket := nativeplan.WorkspaceControllerOperationSocket
	previousOperationIdentity := nativeplan.WorkspaceControllerOperationIdentity
	previousExecSocket := nativeplan.WorkspaceControllerExecSocket
	previousFleetInventory := nativeplan.WorkspaceControllerSourceInventory
	previousGUIInventory := nativeplan.WorkspaceControllerGUIInventory
	previousTI4Source := nativeplan.WorkspaceEgressCanonicalTI4Source
	previousNSCDSource := nativeplan.WorkspaceEgressNSCDSource
	previousIdentityMode := nativeplan.ManagementControllerIdentityExpectedMode
	previousIdentityOwner := nativeplan.ManagementControllerIdentityExpectedOwner
	previousIdentityGroup := nativeplan.ManagementControllerIdentityExpectedGroup
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
	nativeplan.WorkspaceControllerOperationIdentity = operationIdentity
	nativeplan.WorkspaceControllerExecSocket = execSocket
	nativeplan.WorkspaceControllerSourceInventory = fleetInventory
	nativeplan.WorkspaceControllerGUIInventory = guiInventory
	nativeplan.WorkspaceEgressCanonicalTI4Source = ti4Source
	nativeplan.WorkspaceEgressNSCDSource = nscdSource
	nativeplan.ManagementControllerIdentityExpectedMode = "0400"
	nativeplan.ManagementControllerIdentityExpectedOwner = currentUser.Username
	nativeplan.ManagementControllerIdentityExpectedGroup = currentGroup.Name
	t.Cleanup(func() {
		nativeplan.ManagementControllerProfileManifestPath = previousManifest
		nativeplan.WorkspaceControllerOperationSocket = previousOperationSocket
		nativeplan.WorkspaceControllerOperationIdentity = previousOperationIdentity
		nativeplan.WorkspaceControllerExecSocket = previousExecSocket
		nativeplan.WorkspaceControllerSourceInventory = previousFleetInventory
		nativeplan.WorkspaceControllerGUIInventory = previousGUIInventory
		nativeplan.WorkspaceEgressCanonicalTI4Source = previousTI4Source
		nativeplan.WorkspaceEgressNSCDSource = previousNSCDSource
		nativeplan.ManagementControllerIdentityExpectedMode = previousIdentityMode
		nativeplan.ManagementControllerIdentityExpectedOwner = previousIdentityOwner
		nativeplan.ManagementControllerIdentityExpectedGroup = previousIdentityGroup
	})
	operationListener := listenControllerTestSocket(t, operationSocket)
	_ = operationListener
	listenControllerTestSocket(t, execSocket)
	listenControllerTestSocket(t, nscdSource)
	writeTestFile(t, fleetInventory, "fleet\n")
	writeTestFile(t, guiInventory, "gui\n")
	writeTestFile(t, resolvConf, "nameserver 127.0.0.1\n")
	if err := os.MkdirAll(ti4Source, 0o755); err != nil {
		t.Fatal(err)
	}

	profile := nativeplan.ManagementControllerProfile{
		SchemaVersion:       nativeplan.ManagementControllerProfileSchema,
		ProfileIdentity:     nativeplan.ManagementControllerProfileIdentity,
		MountPolicyIdentity: nativeplan.ManagementControllerMountPolicyIdentity,
		OperationHandle: nativeplan.ControllerProfileOperationHandle{
			Environment:    nativeplan.ControllerOperationHandleEnvironment,
			RequiredValue:  nativeplan.ControllerOperationHandleRequiredValue,
			SocketPath:     operationSocket,
			IdentityPath:   operationIdentity,
			IdentitySchema: nativeplan.ControllerOperationIdentitySchema,
			IdentityMode:   nativeplan.ManagementControllerIdentityExpectedMode,
			IdentityOwner:  nativeplan.ManagementControllerIdentityExpectedOwner,
			IdentityGroup:  nativeplan.ManagementControllerIdentityExpectedGroup,
			NoFollow:       true,
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
			Controller:    nativeplan.ManagementControllerNode,
			ControllerGUI: nativeplan.ManagementControllerGUI,
			DrTalos:       nativeplan.ManagementControllerDrTalos,
		},
		Schemas: nativeplan.ControllerProfileSchemas{
			Request:  nativeplan.ControllerOperationRequestSchema,
			Accepted: nativeplan.ControllerOperationAcceptedSchema,
			Event:    nativeplan.ControllerOperationEventSchema,
			Receipt:  nativeplan.ControllerOperationReceiptSchema,
		},
		Kinds: []string{"fleet.exec", "nixos.deploy-closure", "gui.replace-controller"},
	}
	operationStoreRoot := filepath.Join(root, "nix", "store")
	previousOperationStoreRoot := controllerOperationStoreRoot
	previousProfileStoreRoot := nativeplan.ControllerProfileStoreRoot
	controllerOperationStoreRoot = operationStoreRoot
	nativeplan.ControllerProfileStoreRoot = operationStoreRoot
	t.Cleanup(func() {
		controllerOperationStoreRoot = previousOperationStoreRoot
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
	manifestBytes, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, manifestPath, string(manifestBytes)+"\n")

	writeControllerOperationIdentityFixture(t, profile, operationStoreRoot)

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
	listenControllerTestSocket(t, proxySocket)
	p, err := nativeplan.Build(nativeplan.BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Project:          "dev-workspace",
		Index:            2,
		Repo:             "shadow-throne-management",
		RuntimeLauncher:  runtimeLauncherFixture(t),
		BubblewrapBinary: runtimeLauncherFixture(t),
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
		"'--ro-bind' '" + operationIdentity + "' '" + operationIdentity + "'",
		"'--ro-bind' '" + fleetInventory + "' '" + fleetInventory + "'",
		"'--ro-bind' '" + guiInventory + "' '" + guiInventory + "'",
		"'--bind' '" + ti4Source + "' '/workspaces/dev/ti4-calculator'",
		"'--setenv' 'DEVKIT_MANAGEMENT_CONTROLLER_PROFILE' 'management-controller-convergence/v1'",
		"'--setenv' 'DEVKIT_NATIVE_MOUNT_POLICY_IDENTITY' 'devkit/workspace-egress/v4'",
		"'--setenv' 'FLEET_CONTROLLER_OPERATION_HANDLE' 'required'",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("v4 command missing %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{
		"/home/bayesartre/.ssh",
		"/mnt/",
		"/var/lib/fleet-controller-operation",
		execSocket,
		"FLEET_EXEC_TRANSPORT_HANDLE",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("v4 command exposes forbidden host authority %q:\n%s", forbidden, text)
		}
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
	listenControllerTestSocket(t, operationSocket)
	if _, err := BuildBubblewrap(p, []string{"true"}); err == nil || !strings.Contains(err.Error(), "live socket generation") {
		t.Fatalf("replaced operation socket error = %v, want generation rejection", err)
	}
}

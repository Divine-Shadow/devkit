package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
)

func controllerProfileFileIdentity(t *testing.T, path, content string) ControllerProfileFileIdentity {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(content))
	return ControllerProfileFileIdentity{Path: path, SHA256: hex.EncodeToString(digest[:])}
}

func TestManagementControllerProfileUsesSingleV8AuthorityPath(t *testing.T) {
	if ManagementControllerProfileManifestPath != "/etc/fleet/controller-operation-authority.json" {
		t.Fatalf("Management controller manifest path = %q", ManagementControllerProfileManifestPath)
	}
	if WorkspaceControllerWorkLedgerDirectory != "/home/bayesartre/dev/.agent/fleet-work" {
		t.Fatalf("Management controller work ledger directory = %q", WorkspaceControllerWorkLedgerDirectory)
	}
	if WorkspaceControllerWorkLedgerPath != "/home/bayesartre/dev/.agent/fleet-work/work.sqlite" {
		t.Fatalf("Management controller work ledger path = %q", WorkspaceControllerWorkLedgerPath)
	}
}

func writeControllerProfileManifest(t *testing.T, path, managementRoot, wslRoot string) ManagementControllerProfile {
	t.Helper()
	writeExecutable := func(path string) string {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	writeRegular := func(path string) string {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("Host github.com\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	codexConfig := controllerProfileFileIdentity(t, filepath.Join(ControllerProfileStoreRoot, "codex-config.toml"), "# permission_contract = wsl-nix/codex-granular-custom/v1\n")
	remoteHome := filepath.Join(managementRoot, ".devhome-agent1")
	remoteWorktree := filepath.Join(managementRoot, "ouroboros-ide")
	profile := ManagementControllerProfile{
		SchemaVersion:       ManagementControllerProfileSchema,
		ProfileIdentity:     ManagementControllerProfileIdentity,
		MountPolicyIdentity: ManagementControllerMountPolicyIdentity,
		OperationHandle: ControllerProfileOperationHandle{
			Environment:    ControllerOperationHandleEnvironment,
			RequiredValue:  ControllerOperationHandleRequiredValue,
			SocketPath:     WorkspaceControllerOperationSocket,
			IdentityPath:   WorkspaceControllerOperationIdentity,
			IdentitySchema: ControllerOperationIdentitySchema,
			IdentityMode:   ManagementControllerIdentityExpectedMode,
			IdentityOwner:  ManagementControllerIdentityExpectedOwner,
			IdentityGroup:  ManagementControllerIdentityExpectedGroup,
			NoFollow:       true,
		},
		SourceRoots: ControllerProfileSourceRoots{
			Management: ControllerProfileSourceRoot{
				LogicalPath: ManagementControllerLogicalRoot,
				BackingPath: managementRoot,
				Origin:      "git@github.com:example/management.git",
				Revision:    strings.Repeat("a", 40),
			},
			WSLNix: ControllerProfileSourceRoot{
				LogicalPath: ManagementControllerWSLNixLogicalRoot,
				BackingPath: wslRoot,
				Origin:      "git@github.com:example/wsl-nix.git",
				Revision:    strings.Repeat("b", 40),
			},
		},
		Inventories: ControllerProfileInventories{
			Fleet: controllerProfileFileIdentity(t, WorkspaceControllerSourceInventory, "fleet\n"),
			GUI:   controllerProfileFileIdentity(t, WorkspaceControllerGUIInventory, "gui\n"),
		},
		Targets: ControllerProfileTargets{
			Controller:       ManagementControllerNode,
			ControllerGUI:    ManagementControllerGUI,
			ProductAgentHost: ManagementControllerNode,
		},
		Schemas: ControllerProfileSchemas{
			Request:  ControllerOperationRequestSchema,
			Accepted: ControllerOperationAcceptedSchema,
			Event:    ControllerOperationEventSchema,
			Receipt:  ControllerOperationReceiptSchema,
		},
		Kinds: []string{"app-rpc.exec", "auth.exec", "fleet.exec", "nixos.deploy-closure", "nixos.wsl-sentinel", "gui.replace-controller", "gui.start-app-server", "product-agent.reconcile-absence"},
		Broker: ControllerProfileBroker{
			Executable:     writeExecutable(filepath.Join(ControllerProfileStoreRoot, "fleet-control", "bin", "fleet-control")),
			SourceRevision: strings.Repeat("c", 40),
			StateDirectory: controllerOperationStateDirectory,
		},
		SourceAcquisition: ControllerProfileSourceAcquisition{
			GitExecutable: writeExecutable(filepath.Join(ControllerProfileStoreRoot, "git", "bin", "git")),
			SSHExecutable: writeExecutable(filepath.Join(ControllerProfileStoreRoot, "openssh", "bin", "ssh")),
			SSHConfigPath: writeRegular(filepath.Join(ControllerProfileStoreRoot, "management-controller-github-ssh-config")),
		},
		ProductAgentLifecycle: ControllerProfileProductAgentLifecycle{
			SocketPath:         controllerProductAgentSocket,
			Executable:         writeExecutable(filepath.Join(ControllerProfileStoreRoot, "product-agent", "bin", "product-agent")),
			Operation:          controllerProductAgentOperation,
			ReconcileOperation: "reconcile-absence",
			RequestSchema:      controllerProductAgentRequestSchema,
			EventSchema:        controllerProductAgentEventSchema,
			Mode:               "0660",
			Owner:              "root",
			Group:              "product-agent-operators",
			NoFollow:           true,
		},
		ProductStationReset: ControllerProfileProductStationReset{
			SocketPath:    controllerProductStationResetSocket,
			Executable:    writeExecutable(filepath.Join(ControllerProfileStoreRoot, "devkit", "kit", "bin", "devctl")),
			Operation:     controllerProductStationResetOperation,
			RequestSchema: controllerProductStationResetRequest,
			EventSchema:   controllerProductStationResetEvent,
			ServiceUser:   ManagementControllerIdentityExpectedOwner,
			Mode:          "0660",
			Owner:         "root",
			Group:         "product-station-reset-operators",
			NoFollow:      true,
		},
		FleetRecovery: ControllerProfileFleetRecovery{
			Controller:     ManagementControllerNode,
			Target:         "manifest-selected-station",
			PackagePath:    filepath.Join(ControllerProfileStoreRoot, "fleet-recovery"),
			ExecutablePath: writeExecutable(filepath.Join(ControllerProfileStoreRoot, "fleet-recovery", "bin", "devops-fleet-recovery")),
			SourceRevision: strings.Repeat("d", 40),
		},
		NixOSDeployment: ControllerProfileNixOSDeployment{
			SocketPath:    controllerNixOSDeploymentSocket,
			Operation:     controllerNixOSDeploymentOperation,
			RequestSchema: controllerNixOSDeploymentRequestSchema,
			EventSchema:   controllerNixOSDeploymentEventSchema,
			ServiceUser:   ManagementControllerIdentityExpectedOwner,
			Mode:          "0660",
			Owner:         "root",
			Group:         "fleet-deployment-operators",
			NoFollow:      true,
		},
		RemoteProductGUI: ControllerProfileRemoteProductGUI{
			SchemaVersion: controllerRemoteProductGUISchema,
			Targets: []ControllerProfileRemoteProductGUITarget{{
				Agent: 1, ConfigPath: filepath.Join(remoteHome, ".codex", "config.toml"),
				GovernanceWorkspaceID: "agent1", Home: ControllerProfilePathPair{Host: remoteHome, Remote: remoteHome},
				ID: "test-1", Kind: "devkit-agent", RemoteCodexHome: filepath.Join(remoteHome, ".codex"),
				RuntimeProfile: "dev-all", SocketName: "a1-app.sock", Station: "test",
				Transport: ControllerProfileRemoteProductGUITransport{
					Address: "100.64.0.1", AddressFamily: "AF_INET", ClientIdentityHandle: "identity-fleet",
					Connector: "direct", HostKeyAlias: "test", Port: 22, PreferredRoute: "tailnet-direct",
					ServerHostKeyFingerprint: "SHA256:test", User: ManagementControllerIdentityExpectedOwner,
					WorkerAlias: "test-nix",
				},
				WorkspaceRoot: managementRoot,
				Worktree:      ControllerProfilePathPair{Host: remoteWorktree, Remote: remoteWorktree},
			}},
			Transport: ControllerProfileRemoteProductGUIAuthorityTransport{
				ProtectedIdentityHandles: []ControllerProfileProtectedIdentityHandle{{
					ID: "identity-fleet", RuntimePath: "/run/credentials/fleet-controller-operation.service/identity-fleet",
					PublicKeyFingerprint: "SHA256:client", TargetIDs: []string{"test-1"},
					RegularFile: true, NoFollow: true, ReadableByService: true,
				}},
				ServiceNetwork: ControllerProfileServiceNetwork{
					TargetAddressFamilies: []string{"AF_INET"},
					BrokerAddressFamilies: []string{"AF_UNIX", "AF_INET", "AF_NETLINK"},
				},
				SSHExecutable:       filepath.Join(ControllerProfileStoreRoot, "openssh", "bin", "ssh"),
				SSHKeygenExecutable: writeExecutable(filepath.Join(ControllerProfileStoreRoot, "openssh", "bin", "ssh-keygen")),
			},
		},
		CodexPermissions: ControllerProfileCodexPermissions{
			ApprovalPolicy:    ControllerProfileCodexApprovalPolicy{Granular: ControllerProfileCodexGranularPolicy{}},
			ApprovalsReviewer: "user", Contract: controllerCodexPermissionContract, DestinationMode: "0600",
			Mode: "custom", SandboxMode: "danger-full-access", Source: "config.toml",
			SourcePath: codexConfig.Path, SourceSHA256: codexConfig.SHA256,
			TargetProjection: ControllerProfileCodexTargetProjection{
				ByteEqualToSource: true, Group: ManagementControllerIdentityExpectedGroup, Mode: "0600",
				NoFollow: true, Owner: ManagementControllerIdentityExpectedOwner,
				PathRule: "remoteProductGUI.targets[].configPath", RegularFile: true,
			},
		},
		LinuxDesktopAuth: ControllerProfileLinuxDesktopAuth{
			TargetID:         managementControllerLinuxDesktopTarget,
			CodexHome:        managementControllerLinuxDesktopHome,
			SocketName:       managementControllerLinuxDesktopSocket,
			ServiceName:      managementControllerLinuxDesktopService,
			ReloadExecutable: writeExecutable(filepath.Join(ControllerProfileStoreRoot, "codex-linux-desktop-reload", "bin", "codex-linux-desktop-reload")),
		},
	}
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return profile
}

func TestManagementControllerProfileRecognizesV8Capabilities(t *testing.T) {
	profile := ManagementControllerProfile{
		ProductStationReset: ControllerProfileProductStationReset{
			Operation: controllerProductStationResetOperation,
		},
		RemoteProductGUI: ControllerProfileRemoteProductGUI{
			SchemaVersion: controllerRemoteProductGUISchema,
			Targets:       []ControllerProfileRemoteProductGUITarget{{ID: "station-1"}},
		},
		CodexPermissions: ControllerProfileCodexPermissions{
			Mode: "custom", Contract: controllerCodexPermissionContract,
			ApprovalPolicy: ControllerProfileCodexApprovalPolicy{Granular: ControllerProfileCodexGranularPolicy{}},
		},
	}
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var decoded ManagementControllerProfile
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("v8 controller fields rejected: %v", err)
	}
	if decoded.CodexPermissions.Mode != "custom" ||
		decoded.RemoteProductGUI.SchemaVersion != controllerRemoteProductGUISchema ||
		decoded.ProductStationReset.Operation != controllerProductStationResetOperation {
		t.Fatalf("v8 controller fields decoded incorrectly: %#v", decoded)
	}
}

func TestManagementControllerProfileRecognizesAndValidatesLinuxDesktopAuth(t *testing.T) {
	root := t.TempDir()
	previousStoreRoot := ControllerProfileStoreRoot
	ControllerProfileStoreRoot = filepath.Join(root, "nix", "store")
	t.Cleanup(func() { ControllerProfileStoreRoot = previousStoreRoot })
	reloadExecutable := filepath.Join(ControllerProfileStoreRoot, "codex-linux-desktop-reload", "bin", "codex-linux-desktop-reload")
	if err := os.MkdirAll(filepath.Dir(reloadExecutable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reloadExecutable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	auth := ControllerProfileLinuxDesktopAuth{
		TargetID:         managementControllerLinuxDesktopTarget,
		CodexHome:        managementControllerLinuxDesktopHome,
		SocketName:       managementControllerLinuxDesktopSocket,
		ServiceName:      managementControllerLinuxDesktopService,
		ReloadExecutable: reloadExecutable,
	}
	if err := validateControllerLinuxDesktopAuth(ControllerProfileLinuxDesktopAuth{}); err != nil {
		t.Fatalf("older v7 profile without Linux Desktop auth was rejected: %v", err)
	}
	for _, testCase := range []struct {
		name   string
		mutate func(*ControllerProfileLinuxDesktopAuth)
	}{
		{name: "target", mutate: func(candidate *ControllerProfileLinuxDesktopAuth) { candidate.TargetID = "other" }},
		{name: "home", mutate: func(candidate *ControllerProfileLinuxDesktopAuth) { candidate.CodexHome = "/tmp/codex" }},
		{name: "socket", mutate: func(candidate *ControllerProfileLinuxDesktopAuth) { candidate.SocketName = "other.sock" }},
		{name: "service", mutate: func(candidate *ControllerProfileLinuxDesktopAuth) { candidate.ServiceName = "other.service" }},
		{name: "reload executable", mutate: func(candidate *ControllerProfileLinuxDesktopAuth) { candidate.ReloadExecutable = "/tmp/reload" }},
	} {
		t.Run("rejects "+testCase.name, func(t *testing.T) {
			candidate := auth
			testCase.mutate(&candidate)
			if err := validateControllerLinuxDesktopAuth(candidate); err == nil {
				t.Fatalf("invalid Linux Desktop auth passed: %#v", candidate)
			}
		})
	}
	if err := validateControllerLinuxDesktopAuth(auth); err != nil {
		t.Fatalf("valid Linux Desktop auth rejected: %v", err)
	}
	data, err := json.Marshal(struct {
		LinuxDesktopAuth ControllerProfileLinuxDesktopAuth `json:"linuxDesktopAuth"`
	}{LinuxDesktopAuth: auth})
	if err != nil {
		t.Fatal(err)
	}
	var decoded ManagementControllerProfile
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("Linux Desktop auth field rejected: %v", err)
	}
}

func TestManagementControllerProfileRecognizesTypedFleetRecovery(t *testing.T) {
	var profile ManagementControllerProfile
	decoder := json.NewDecoder(strings.NewReader(`{
		"fleetRecovery": {
			"controller": "shadow-throne",
			"target": "manifest-selected-station",
			"packagePath": "/nix/store/example-fleet-recovery",
			"executablePath": "/nix/store/example-fleet-recovery/bin/devops-fleet-recovery",
			"sourceRevision": "dce48fd684d298616940ec72615326b43cbb6319"
		}
	}`))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profile); err != nil {
		t.Fatalf("typed Fleet recovery profile field rejected: %v", err)
	}
	if profile.FleetRecovery.Controller != ManagementControllerNode ||
		profile.FleetRecovery.Target != "manifest-selected-station" ||
		profile.FleetRecovery.ExecutablePath != "/nix/store/example-fleet-recovery/bin/devops-fleet-recovery" {
		t.Fatalf("typed Fleet recovery profile decoded incorrectly: %#v", profile.FleetRecovery)
	}
}

func TestManagementControllerProfileRecognizesTypedSensitiveIngress(t *testing.T) {
	var profile ManagementControllerProfile
	decoder := json.NewDecoder(strings.NewReader(`{
		"sensitiveInputIngress": {
			"operation": "sensitive-input.ingress",
			"packagePath": "/nix/store/example-sensitive-input-ingress",
			"executablePath": "/nix/store/example-sensitive-input-ingress/bin/devops-sensitive-input-ingress",
			"sourceRevision": "dce48fd684d298616940ec72615326b43cbb6319",
			"requestSchema": "ouroboros-sensitive-input-ingress/request/v1",
			"acceptedSchema": "ouroboros-sensitive-input-ingress/accepted/v1",
			"receiptSchema": "ouroboros-sensitive-input-ingress/receipt/v1",
			"sourceWindowsRoot": "C:\\Users\\Shadow's Throne\\Downloads",
			"sourceLocalRoot": "/mnt/c/Users/Shadow's Throne/Downloads",
			"destinationLogicalRoot": "/workspaces/dev/.sensitive-inputs",
			"destinationLocalRoot": "/home/bayesartre/dev/.sensitive-inputs",
			"directoryMode": "0700",
			"fileMode": "0600",
			"pdfOnly": true,
			"overwrite": false,
			"contentLogging": false
		}
	}`))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profile); err != nil {
		t.Fatalf("typed sensitive-input ingress profile field rejected: %v", err)
	}
	if profile.SensitiveInputIngress.Operation != "sensitive-input.ingress" ||
		profile.SensitiveInputIngress.ContentLogging ||
		profile.SensitiveInputIngress.Overwrite ||
		!profile.SensitiveInputIngress.PDFOnly {
		t.Fatalf("typed sensitive-input ingress profile decoded incorrectly: %#v", profile.SensitiveInputIngress)
	}
}

func TestManagementControllerProfileRecognizesTypedSensitiveOutputEgress(t *testing.T) {
	var profile ManagementControllerProfile
	decoder := json.NewDecoder(strings.NewReader(`{
		"sensitiveOutputEgress": {
			"operation": "sensitive-output.egress",
			"packagePath": "/nix/store/example-sensitive-output-egress",
			"executablePath": "/nix/store/example-sensitive-output-egress/bin/devops-sensitive-input-ingress",
			"sourceRevision": "9c74d3d9c706c53a004d321126d2af9864951361",
			"requestSchema": "ouroboros-sensitive-output-egress/request/v1",
			"acceptedSchema": "ouroboros-sensitive-output-egress/accepted/v1",
			"receiptSchema": "ouroboros-sensitive-output-egress/receipt/v1",
			"sourceLogicalRoot": "/workspaces/dev/.sensitive-outputs/demotrax-redacted-20260803",
			"sourceLocalRoot": "/home/bayesartre/dev/.sensitive-outputs/demotrax-redacted-20260803",
			"destinationWindowsRoot": "C:\\Users\\Shadow's Throne\\Downloads\\Demotrax Redacted 2026-08-03",
			"destinationLocalRoot": "/mnt/c/Users/Shadow's Throne/Downloads/Demotrax Redacted 2026-08-03",
			"directoryMode": "0700",
			"fileMode": "0600",
			"pdfOnly": true,
			"overwrite": false,
			"contentLogging": false,
			"preserveSource": true
		}
	}`))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profile); err != nil {
		t.Fatalf("typed sensitive-output egress profile field rejected: %v", err)
	}
	if profile.SensitiveOutputEgress.Operation != "sensitive-output.egress" ||
		profile.SensitiveOutputEgress.ContentLogging ||
		profile.SensitiveOutputEgress.Overwrite ||
		!profile.SensitiveOutputEgress.PDFOnly ||
		!profile.SensitiveOutputEgress.PreserveSource {
		t.Fatalf("typed sensitive-output egress profile decoded incorrectly: %#v", profile.SensitiveOutputEgress)
	}
}

func TestManagementControllerProfilePromotesOnlyExactManagementConsumerToV4(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	base, err := Build(BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot},
		Project:          "dev-workspace",
		Index:            2,
		Repo:             "shadow-throne-management",
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(root, "allowlist.txt"),
	})
	if err != nil {
		t.Fatalf("Build base: %v", err)
	}
	manifestPath := filepath.Join(root, "management-controller-convergence.json")
	operationSocket := filepath.Join(root, "run", "operation", "control.sock")
	operationIdentity := filepath.Join(root, "run", "operation", "identity.json")
	execSocket := filepath.Join(root, "run", "exec", "control.sock")
	previousManifest := ManagementControllerProfileManifestPath
	previousOperationSocket := WorkspaceControllerOperationSocket
	previousOperationIdentity := WorkspaceControllerOperationIdentity
	previousExecSocket := WorkspaceControllerExecSocket
	previousFleetInventory := WorkspaceControllerSourceInventory
	previousGUIInventory := WorkspaceControllerGUIInventory
	previousStoreRoot := ControllerProfileStoreRoot
	ManagementControllerProfileManifestPath = manifestPath
	WorkspaceControllerOperationSocket = operationSocket
	WorkspaceControllerOperationIdentity = operationIdentity
	WorkspaceControllerExecSocket = execSocket
	WorkspaceControllerSourceInventory = filepath.Join(root, "fleet-inventory.json")
	WorkspaceControllerGUIInventory = filepath.Join(root, "gui-inventory.json")
	ControllerProfileStoreRoot = filepath.Join(root, "nix", "store")
	t.Cleanup(func() {
		ManagementControllerProfileManifestPath = previousManifest
		WorkspaceControllerOperationSocket = previousOperationSocket
		WorkspaceControllerOperationIdentity = previousOperationIdentity
		WorkspaceControllerExecSocket = previousExecSocket
		WorkspaceControllerSourceInventory = previousFleetInventory
		WorkspaceControllerGUIInventory = previousGUIInventory
		ControllerProfileStoreRoot = previousStoreRoot
	})
	profile := writeControllerProfileManifest(t, manifestPath, base.Agent.HostWorktree, filepath.Join(root, "wsl-nix-lease"))
	t.Setenv(ManagementControllerProfileEnvironment, ManagementControllerProfileIdentity)
	profile.Targets.ControllerGUI = ManagementControllerPrimaryGUI
	if err := validateManagementControllerProfile(profile); err != nil {
		t.Fatalf("manifest-selected primary Management GUI was rejected: %v", err)
	}
	profile.Targets.ControllerGUI = "shadow-throne-management-unmanaged"
	if err := validateManagementControllerProfile(profile); err == nil ||
		!strings.Contains(err.Error(), "compiled inventory identities") {
		t.Fatalf("uncompiled Management GUI was not rejected: %v", err)
	}
	profile.Targets.ControllerGUI = ManagementControllerGUI
	if profile.FleetRecovery.Target != "manifest-selected-station" {
		t.Fatalf("generic manifest-selected Fleet recovery target was not preserved: %#v", profile.FleetRecovery)
	}
	for _, testCase := range []struct {
		name   string
		mutate func(*ControllerProfileRemoteProductGUITarget)
	}{
		{
			name: "missing workspace root",
			mutate: func(target *ControllerProfileRemoteProductGUITarget) {
				target.WorkspaceRoot = ""
			},
		},
		{
			name: "shared workspace root",
			mutate: func(target *ControllerProfileRemoteProductGUITarget) {
				target.WorkspaceRoot = "/home/bayesartre/dev"
			},
		},
		{
			name: "filesystem root",
			mutate: func(target *ControllerProfileRemoteProductGUITarget) {
				target.WorkspaceRoot = "/"
			},
		},
		{
			name: "noncanonical workspace root",
			mutate: func(target *ControllerProfileRemoteProductGUITarget) {
				target.WorkspaceRoot += "/../" + filepath.Base(target.WorkspaceRoot)
			},
		},
		{
			name: "Windows-mounted workspace root",
			mutate: func(target *ControllerProfileRemoteProductGUITarget) {
				target.WorkspaceRoot = "/mnt/c/workspaces/agent1"
			},
		},
		{
			name: "worktree outside workspace root",
			mutate: func(target *ControllerProfileRemoteProductGUITarget) {
				target.Worktree.Host = filepath.Join(filepath.Dir(target.WorkspaceRoot), "other", "ouroboros-ide")
			},
		},
		{
			name: "home outside workspace root",
			mutate: func(target *ControllerProfileRemoteProductGUITarget) {
				target.Home.Host = filepath.Join(filepath.Dir(target.WorkspaceRoot), "other", "home")
			},
		},
	} {
		t.Run("rejects remote Product GUI "+testCase.name, func(t *testing.T) {
			candidate := profile
			candidate.RemoteProductGUI.Targets = append(
				[]ControllerProfileRemoteProductGUITarget(nil),
				profile.RemoteProductGUI.Targets...,
			)
			testCase.mutate(&candidate.RemoteProductGUI.Targets[0])
			if err := validateManagementControllerProfile(candidate); err == nil ||
				!strings.Contains(err.Error(), "workspace root") {
				t.Fatalf("invalid remote Product GUI workspace root passed: %#v err=%v", candidate.RemoteProductGUI.Targets[0], err)
			}
		})
	}

	for _, testCase := range []struct {
		name   string
		mutate func(*ManagementControllerProfile)
	}{
		{
			name: "controller mismatch",
			mutate: func(candidate *ManagementControllerProfile) {
				candidate.FleetRecovery.Controller = "other-controller"
			},
		},
		{
			name: "unsafe target token",
			mutate: func(candidate *ManagementControllerProfile) {
				candidate.FleetRecovery.Target = "station,other"
			},
		},
		{
			name: "mutable package path",
			mutate: func(candidate *ManagementControllerProfile) {
				candidate.FleetRecovery.PackagePath = filepath.Join(root, "mutable-fleet-recovery")
			},
		},
		{
			name: "nested store package path",
			mutate: func(candidate *ManagementControllerProfile) {
				candidate.FleetRecovery.PackagePath = filepath.Join(ControllerProfileStoreRoot, "owner", "fleet-recovery")
				candidate.FleetRecovery.ExecutablePath = filepath.Join(candidate.FleetRecovery.PackagePath, "bin", "devops-fleet-recovery")
			},
		},
		{
			name: "executable outside package",
			mutate: func(candidate *ManagementControllerProfile) {
				candidate.FleetRecovery.ExecutablePath = candidate.Broker.Executable
			},
		},
		{
			name: "malformed source revision",
			mutate: func(candidate *ManagementControllerProfile) {
				candidate.FleetRecovery.SourceRevision = strings.Repeat("D", 40)
			},
		},
	} {
		t.Run("rejects "+testCase.name, func(t *testing.T) {
			candidate := profile
			testCase.mutate(&candidate)
			if err := validateManagementControllerProfile(candidate); err == nil ||
				!strings.Contains(err.Error(), "Fleet recovery") {
				t.Fatalf("invalid Fleet recovery identity passed: %#v err=%v", candidate.FleetRecovery, err)
			}
		})
	}

	p, err := Build(BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot},
		Project:          "dev-workspace",
		Index:            2,
		Repo:             "shadow-throne-management",
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(root, "allowlist.txt"),
	})
	if err != nil {
		t.Fatalf("Build v4: %v", err)
	}
	if p.ControllerProfile != ManagementControllerProfileIdentity ||
		p.ControllerManifest != manifestPath ||
		p.MountPolicyIdentity != ManagementControllerMountPolicyIdentity ||
		p.Env[ControllerOperationHandleEnvironment] != ControllerOperationHandleRequiredValue ||
		p.Env[WorkspaceControllerWorkLedgerEnvironment] != WorkspaceControllerWorkLedgerPath {
		t.Fatalf("v4 readback mismatch: profile=%q manifest=%q policy=%q env=%#v", p.ControllerProfile, p.ControllerManifest, p.MountPolicyIdentity, p.Env)
	}
	if _, ok := p.Env["FLEET_EXEC_TRANSPORT_HANDLE"]; ok {
		t.Fatalf("v4 must not expose the legacy direct exec handle: %#v", p.Env)
	}
	for _, want := range []Bind{
		{Source: profile.SourceRoots.WSLNix.BackingPath, Target: profile.SourceRoots.WSLNix.LogicalPath, Mode: "rw", Required: true},
		{Source: manifestPath, Target: manifestPath, Mode: "ro", Required: true},
		{Source: operationSocket, Target: operationSocket, Mode: "ro", Required: true},
		{Source: operationIdentity, Target: operationIdentity, Mode: "ro", Required: true},
		{Source: WorkspaceControllerWorkLedgerDirectory, Target: WorkspaceControllerWorkLedgerDirectory, Mode: "rw", Required: true},
	} {
		if !hasBind(p.Binds, want.Source, want.Target) {
			t.Fatalf("v4 bind absent: %+v from %#v", want, p.Binds)
		}
	}
	if hasBind(p.Binds, execSocket, execSocket) {
		t.Fatalf("v4 must not project the legacy direct exec socket: %#v", p.Binds)
	}
	withoutReconcile := profile
	withoutReconcile.ProductAgentLifecycle.ReconcileOperation = ""
	if err := validateManagementControllerProfile(withoutReconcile); err == nil ||
		!strings.Contains(err.Error(), "Product-agent lifecycle") {
		t.Fatalf("profile without Product-agent reconciliation operation was not rejected: %v", err)
	}
	withoutStationReset := profile
	withoutStationReset.ProductStationReset.Operation = ""
	if err := validateManagementControllerProfile(withoutStationReset); err == nil ||
		!strings.Contains(err.Error(), "Product station reset") {
		t.Fatalf("profile without typed Product station reset was not rejected: %v", err)
	}
	withoutControllerRead := profile
	withoutControllerRead.Kinds = []string{
		"fleet.exec",
		"nixos.deploy-closure",
		"gui.replace-controller",
		"gui.start-app-server",
	}
	if err := validateManagementControllerProfile(withoutControllerRead); err == nil ||
		!strings.Contains(err.Error(), "operation kinds") {
		t.Fatalf("profile without controller read-only app-rpc was not rejected: %v", err)
	}

	product, err := Build(BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot},
		Project:          "dev-all",
		Index:            2,
		Repo:             "ouroboros-ide",
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(root, "allowlist.txt"),
	})
	if err != nil {
		t.Fatalf("Build unrelated Product profile: %v", err)
	}
	if product.ControllerProfile != "" || product.MountPolicyIdentity != "devkit/workspace-egress/v3" {
		t.Fatalf("unrelated Product profile was promoted: %+v", product)
	}
	if _, ok := product.Env[WorkspaceControllerWorkLedgerEnvironment]; ok ||
		hasBind(product.Binds, WorkspaceControllerWorkLedgerDirectory, WorkspaceControllerWorkLedgerDirectory) {
		t.Fatalf("unrelated Product profile received controller work-ledger authority: env=%#v binds=%#v", product.Env, product.Binds)
	}

	mutableConfig := filepath.Join(root, "mutable-ssh-config")
	if err := os.WriteFile(mutableConfig, []byte("Host github.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	profile.SourceAcquisition.SSHConfigPath = mutableConfig
	if err := validateManagementControllerProfile(profile); err == nil || !strings.Contains(err.Error(), "SSH config") {
		t.Fatalf("mutable SSH config path was not rejected: %v", err)
	}
}

func TestManagementControllerProfileRejectsManifestMismatchBeforePlan(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "management-controller-convergence.json")
	operationSocket := filepath.Join(root, "operation.sock")
	operationIdentity := filepath.Join(root, "identity.json")
	previousManifest := ManagementControllerProfileManifestPath
	previousOperationSocket := WorkspaceControllerOperationSocket
	previousOperationIdentity := WorkspaceControllerOperationIdentity
	previousFleetInventory := WorkspaceControllerSourceInventory
	previousGUIInventory := WorkspaceControllerGUIInventory
	previousStoreRoot := ControllerProfileStoreRoot
	ManagementControllerProfileManifestPath = manifestPath
	WorkspaceControllerOperationSocket = operationSocket
	WorkspaceControllerOperationIdentity = operationIdentity
	WorkspaceControllerSourceInventory = filepath.Join(root, "fleet-inventory.json")
	WorkspaceControllerGUIInventory = filepath.Join(root, "gui-inventory.json")
	ControllerProfileStoreRoot = filepath.Join(root, "nix", "store")
	t.Cleanup(func() {
		ManagementControllerProfileManifestPath = previousManifest
		WorkspaceControllerOperationSocket = previousOperationSocket
		WorkspaceControllerOperationIdentity = previousOperationIdentity
		WorkspaceControllerSourceInventory = previousFleetInventory
		WorkspaceControllerGUIInventory = previousGUIInventory
		ControllerProfileStoreRoot = previousStoreRoot
	})
	profile := writeControllerProfileManifest(t, manifestPath, filepath.Join(root, "management"), filepath.Join(root, "wsl"))
	profile.OperationHandle.SocketPath = filepath.Join(root, "caller-selected.sock")
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ManagementControllerProfileEnvironment, ManagementControllerProfileIdentity)
	devkitRoot := filepath.Join(root, "dev", "devkit")
	if err := os.MkdirAll(devkitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = Build(BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot},
		Project:          "dev-workspace",
		Repo:             "shadow-throne-management",
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(root, "allowlist.txt"),
	})
	if err == nil || !strings.Contains(err.Error(), "fail-closed contract") {
		t.Fatalf("Build error = %v, want fail-closed manifest rejection", err)
	}
}

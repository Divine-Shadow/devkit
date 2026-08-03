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
		Kinds: []string{"app-rpc.exec", "auth.exec", "fleet.exec", "nixos.deploy-closure", "gui.replace-controller", "gui.start-app-server"},
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
			SocketPath:    controllerProductAgentSocket,
			Executable:    writeExecutable(filepath.Join(ControllerProfileStoreRoot, "product-agent", "bin", "product-agent")),
			Operation:     controllerProductAgentOperation,
			RequestSchema: controllerProductAgentRequestSchema,
			EventSchema:   controllerProductAgentEventSchema,
			Mode:          "0660",
			Owner:         "root",
			Group:         "product-agent-operators",
			NoFollow:      true,
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
		p.Env[ControllerOperationHandleEnvironment] != ControllerOperationHandleRequiredValue {
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
	} {
		if !hasBind(p.Binds, want.Source, want.Target) {
			t.Fatalf("v4 bind absent: %+v from %#v", want, p.Binds)
		}
	}
	if hasBind(p.Binds, execSocket, execSocket) {
		t.Fatalf("v4 must not project the legacy direct exec socket: %#v", p.Binds)
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

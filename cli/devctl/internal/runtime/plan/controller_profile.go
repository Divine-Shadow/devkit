package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ManagementControllerProfileIdentity     = "management-controller-convergence/v1"
	ManagementControllerMountPolicyIdentity = "devkit/workspace-egress/v4"
	ManagementControllerProfileSchema       = "wsl-nix-management-controller-convergence/v7"
	ControllerOperationIdentitySchema       = "fleet-control/controller-operation-identity/v7"
	ControllerOperationRequestSchema        = "fleet-control/controller-operation-request/v1"
	ControllerOperationAcceptedSchema       = "fleet-control/controller-operation-accepted/v1"
	ControllerOperationEventSchema          = "fleet-control/controller-operation-event/v1"
	ControllerOperationReceiptSchema        = "fleet-control/controller-operation-receipt/v1"
	ManagementControllerProfileEnvironment  = "DEVKIT_MANAGEMENT_CONTROLLER_PROFILE"
	ControllerOperationHandleEnvironment    = "FLEET_CONTROLLER_OPERATION_HANDLE"
	ControllerOperationHandleRequiredValue  = "required"
	ManagementControllerLogicalRoot         = "/workspace"
	ManagementControllerWSLNixLogicalRoot   = "/workspaces/dev/wsl-nix"
	ManagementControllerNode                = "shadow-throne"
	ManagementControllerGUI                 = "shadow-throne-management-2"
	ManagementControllerPrimaryGUI          = "shadow-throne-management"
	controllerOperationStateDirectory       = "/var/lib/fleet-controller-operation"
	controllerProductAgentSocket            = "/run/fleet-product-agent-lifecycle/control.sock"
	controllerProductAgentOperation         = "cycle-test"
	controllerProductAgentRequestSchema     = "fleet-control/product-agent-local-request/v1"
	controllerProductAgentEventSchema       = "fleet-control/product-agent-local-event/v1"
	controllerNixOSDeploymentSocket         = "/run/fleet-nixos-deploy-effect/control.sock"
	controllerNixOSDeploymentOperation      = "nixos.deploy-closure"
	controllerNixOSDeploymentRequestSchema  = "fleet-control/nixos-deploy-local-request/v1"
	controllerNixOSDeploymentEventSchema    = "fleet-control/nixos-deploy-local-event/v1"
	controllerRemoteProductGUISchema        = "wsl-nix/remote-product-gui-authority/v2"
	controllerCodexPermissionContract       = "wsl-nix/codex-granular-custom/v1"
)

var (
	ManagementControllerProfileManifestPath = "/etc/fleet/management-controller-convergence.json"
	WorkspaceControllerOperationSocket      = "/run/fleet-controller-operation/control.sock"
	WorkspaceControllerOperationIdentity    = "/run/fleet-controller-operation/identity.json"
	ControllerProfileStoreRoot              = "/nix/store"
	// These remain compiled defaults. They are variables only so hermetic
	// cross-package tests can reproduce the same ownership validation under
	// the isolated Nix build user; no runtime input can change them.
	ManagementControllerIdentityExpectedMode  = "0400"
	ManagementControllerIdentityExpectedOwner = "bayesartre"
	ManagementControllerIdentityExpectedGroup = "users"
)

type ControllerProfileSourceRoot struct {
	LogicalPath string `json:"logicalPath"`
	BackingPath string `json:"backingPath"`
	Origin      string `json:"origin"`
	Revision    string `json:"revision"`
}

type ControllerProfileSourceRoots struct {
	Management ControllerProfileSourceRoot `json:"management"`
	WSLNix     ControllerProfileSourceRoot `json:"wslNix"`
}

type ControllerProfileOperationHandle struct {
	Environment    string `json:"environment"`
	RequiredValue  string `json:"requiredValue"`
	SocketPath     string `json:"socketPath"`
	IdentityPath   string `json:"identityPath"`
	IdentitySchema string `json:"identitySchema"`
	IdentityMode   string `json:"identityMode"`
	IdentityOwner  string `json:"identityOwner"`
	IdentityGroup  string `json:"identityGroup"`
	NoFollow       bool   `json:"noFollow"`
}

type ControllerProfileFileIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ControllerProfileInventories struct {
	Fleet ControllerProfileFileIdentity `json:"fleet"`
	GUI   ControllerProfileFileIdentity `json:"gui"`
}

type ControllerProfileTargets struct {
	Controller       string `json:"controller"`
	ControllerGUI    string `json:"controllerGui"`
	ProductAgentHost string `json:"productAgentHost"`
}

type ControllerProfileSchemas struct {
	Request  string `json:"request"`
	Accepted string `json:"accepted"`
	Event    string `json:"event"`
	Receipt  string `json:"receipt"`
}

type ControllerProfileBroker struct {
	Executable     string `json:"executable"`
	SourceRevision string `json:"sourceRevision"`
	StateDirectory string `json:"stateDirectory"`
}

type ControllerProfileSourceAcquisition struct {
	GitExecutable string `json:"gitExecutable"`
	SSHExecutable string `json:"sshExecutable"`
	SSHConfigPath string `json:"sshConfigPath"`
}

type ControllerProfileProductAgentLifecycle struct {
	SocketPath         string `json:"socketPath"`
	Executable         string `json:"executable"`
	Operation          string `json:"operation"`
	ReconcileOperation string `json:"reconcileOperation"`
	RequestSchema      string `json:"requestSchema"`
	EventSchema        string `json:"eventSchema"`
	Mode               string `json:"mode"`
	Owner              string `json:"owner"`
	Group              string `json:"group"`
	NoFollow           bool   `json:"noFollow"`
}

type ControllerProfileNixOSDeployment struct {
	SocketPath    string `json:"socketPath"`
	Operation     string `json:"operation"`
	RequestSchema string `json:"requestSchema"`
	EventSchema   string `json:"eventSchema"`
	ServiceUser   string `json:"serviceUser"`
	Mode          string `json:"mode"`
	Owner         string `json:"owner"`
	Group         string `json:"group"`
	NoFollow      bool   `json:"noFollow"`
}

type ControllerProfileSensitiveInputIngress struct {
	Operation              string `json:"operation"`
	PackagePath            string `json:"packagePath"`
	ExecutablePath         string `json:"executablePath"`
	SourceRevision         string `json:"sourceRevision"`
	RequestSchema          string `json:"requestSchema"`
	AcceptedSchema         string `json:"acceptedSchema"`
	ReceiptSchema          string `json:"receiptSchema"`
	SourceWindowsRoot      string `json:"sourceWindowsRoot"`
	SourceLocalRoot        string `json:"sourceLocalRoot"`
	DestinationLogicalRoot string `json:"destinationLogicalRoot"`
	DestinationLocalRoot   string `json:"destinationLocalRoot"`
	DirectoryMode          string `json:"directoryMode"`
	FileMode               string `json:"fileMode"`
	PDFOnly                bool   `json:"pdfOnly"`
	Overwrite              bool   `json:"overwrite"`
	ContentLogging         bool   `json:"contentLogging"`
}

type ControllerProfileSensitiveOutputEgress struct {
	Operation              string `json:"operation"`
	PackagePath            string `json:"packagePath"`
	ExecutablePath         string `json:"executablePath"`
	SourceRevision         string `json:"sourceRevision"`
	RequestSchema          string `json:"requestSchema"`
	AcceptedSchema         string `json:"acceptedSchema"`
	ReceiptSchema          string `json:"receiptSchema"`
	SourceLogicalRoot      string `json:"sourceLogicalRoot"`
	SourceLocalRoot        string `json:"sourceLocalRoot"`
	DestinationWindowsRoot string `json:"destinationWindowsRoot"`
	DestinationLocalRoot   string `json:"destinationLocalRoot"`
	DirectoryMode          string `json:"directoryMode"`
	FileMode               string `json:"fileMode"`
	PDFOnly                bool   `json:"pdfOnly"`
	Overwrite              bool   `json:"overwrite"`
	ContentLogging         bool   `json:"contentLogging"`
	PreserveSource         bool   `json:"preserveSource"`
}

type ControllerProfilePathPair struct {
	Host   string `json:"host"`
	Remote string `json:"remote"`
}

type ControllerProfileRemoteProductGUITransport struct {
	Address                  string `json:"address"`
	AddressFamily            string `json:"addressFamily"`
	ClientIdentityHandle     string `json:"clientIdentityHandle"`
	Connector                string `json:"connector"`
	HostKeyAlias             string `json:"hostKeyAlias"`
	Port                     int    `json:"port"`
	PreferredRoute           string `json:"preferredRoute"`
	ServerHostKeyFingerprint string `json:"serverHostKeyFingerprint"`
	User                     string `json:"user"`
	WorkerAlias              string `json:"workerAlias"`
}

type ControllerProfileRemoteProductGUITarget struct {
	Agent                 int                                        `json:"agent"`
	ConfigPath            string                                     `json:"configPath"`
	GovernanceWorkspaceID string                                     `json:"governanceWorkspaceId"`
	Home                  ControllerProfilePathPair                  `json:"home"`
	ID                    string                                     `json:"id"`
	Kind                  string                                     `json:"kind"`
	RemoteCodexHome       string                                     `json:"remoteCodexHome"`
	RuntimeProfile        string                                     `json:"runtimeProfile"`
	SocketName            string                                     `json:"socketName"`
	Station               string                                     `json:"station"`
	Transport             ControllerProfileRemoteProductGUITransport `json:"transport"`
	Worktree              ControllerProfilePathPair                  `json:"worktree"`
}

type ControllerProfileProtectedIdentityHandle struct {
	ID                   string   `json:"id"`
	RuntimePath          string   `json:"runtimePath"`
	PublicKeyFingerprint string   `json:"publicKeyFingerprint"`
	TargetIDs            []string `json:"targetIds"`
	RegularFile          bool     `json:"regularFile"`
	NoFollow             bool     `json:"noFollow"`
	ReadableByService    bool     `json:"readableByService"`
}

type ControllerProfileServiceNetwork struct {
	BrokerAddressFamilies []string `json:"brokerAddressFamilies"`
	TargetAddressFamilies []string `json:"targetAddressFamilies"`
	PrivateNetwork        bool     `json:"privateNetwork"`
}

type ControllerProfileRemoteProductGUIAuthorityTransport struct {
	ProtectedIdentityHandles []ControllerProfileProtectedIdentityHandle `json:"protectedIdentityHandles"`
	ServiceNetwork           ControllerProfileServiceNetwork            `json:"serviceNetwork"`
	SSHExecutable            string                                     `json:"sshExecutable"`
	SSHKeygenExecutable      string                                     `json:"sshKeygenExecutable"`
}

type ControllerProfileRemoteProductGUI struct {
	SchemaVersion string                                              `json:"schemaVersion"`
	Targets       []ControllerProfileRemoteProductGUITarget           `json:"targets"`
	Transport     ControllerProfileRemoteProductGUIAuthorityTransport `json:"transport"`
}

type ControllerProfileCodexGranularPolicy struct {
	MCPElicitations    bool `json:"mcp_elicitations"`
	RequestPermissions bool `json:"request_permissions"`
	Rules              bool `json:"rules"`
	SandboxApproval    bool `json:"sandbox_approval"`
	SkillApproval      bool `json:"skill_approval"`
}

type ControllerProfileCodexApprovalPolicy struct {
	Granular ControllerProfileCodexGranularPolicy `json:"granular"`
}

type ControllerProfileCodexTargetProjection struct {
	ByteEqualToSource bool   `json:"byteEqualToSource"`
	Group             string `json:"group"`
	Mode              string `json:"mode"`
	NoFollow          bool   `json:"noFollow"`
	Owner             string `json:"owner"`
	PathRule          string `json:"pathRule"`
	RegularFile       bool   `json:"regularFile"`
}

type ControllerProfileCodexPermissions struct {
	ApprovalPolicy    ControllerProfileCodexApprovalPolicy   `json:"approvalPolicy"`
	ApprovalsReviewer string                                 `json:"approvalsReviewer"`
	Contract          string                                 `json:"contract"`
	DestinationMode   string                                 `json:"destinationMode"`
	Mode              string                                 `json:"mode"`
	SandboxMode       string                                 `json:"sandboxMode"`
	Source            string                                 `json:"source"`
	SourcePath        string                                 `json:"sourcePath"`
	SourceSHA256      string                                 `json:"sourceSha256"`
	TargetProjection  ControllerProfileCodexTargetProjection `json:"targetProjection"`
}

type ManagementControllerProfile struct {
	SchemaVersion         string                                 `json:"schemaVersion"`
	ProfileIdentity       string                                 `json:"profileIdentity"`
	MountPolicyIdentity   string                                 `json:"mountPolicyIdentity"`
	OperationHandle       ControllerProfileOperationHandle       `json:"operationHandle"`
	ProductAgentLifecycle ControllerProfileProductAgentLifecycle `json:"productAgentLifecycle"`
	NixOSDeployment       ControllerProfileNixOSDeployment       `json:"nixosDeployment"`
	SourceRoots           ControllerProfileSourceRoots           `json:"sourceRoots"`
	Inventories           ControllerProfileInventories           `json:"inventories"`
	Targets               ControllerProfileTargets               `json:"targets"`
	Schemas               ControllerProfileSchemas               `json:"schemas"`
	Kinds                 []string                               `json:"kinds"`
	Broker                ControllerProfileBroker                `json:"broker"`
	SourceAcquisition     ControllerProfileSourceAcquisition     `json:"sourceAcquisition"`
	SensitiveInputIngress ControllerProfileSensitiveInputIngress `json:"sensitiveInputIngress"`
	SensitiveOutputEgress ControllerProfileSensitiveOutputEgress `json:"sensitiveOutputEgress"`
	RemoteProductGUI      ControllerProfileRemoteProductGUI      `json:"remoteProductGUI"`
	CodexPermissions      ControllerProfileCodexPermissions      `json:"codexPermissions"`
}

func LoadManagementControllerProfile(path string) (ManagementControllerProfile, error) {
	var profile ManagementControllerProfile
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || !filepath.IsAbs(path) {
		return profile, fmt.Errorf("Management controller profile manifest path must be absolute")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return profile, fmt.Errorf("read Management controller profile manifest %s: %w", path, err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profile); err != nil {
		return profile, fmt.Errorf("decode Management controller profile manifest %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return profile, fmt.Errorf("decode Management controller profile manifest %s: trailing JSON value", path)
		}
		return profile, fmt.Errorf("decode Management controller profile manifest %s: trailing JSON content: %w", path, err)
	}
	if err := validateManagementControllerProfile(profile); err != nil {
		return profile, err
	}
	return profile, nil
}

func validateManagementControllerProfile(profile ManagementControllerProfile) error {
	if profile.SchemaVersion != ManagementControllerProfileSchema {
		return fmt.Errorf("Management controller profile schema = %q, want %q", profile.SchemaVersion, ManagementControllerProfileSchema)
	}
	if profile.ProfileIdentity != ManagementControllerProfileIdentity {
		return fmt.Errorf("Management controller profile identity = %q, want %q", profile.ProfileIdentity, ManagementControllerProfileIdentity)
	}
	if profile.MountPolicyIdentity != ManagementControllerMountPolicyIdentity {
		return fmt.Errorf("Management controller mount policy = %q, want %q", profile.MountPolicyIdentity, ManagementControllerMountPolicyIdentity)
	}
	handle := profile.OperationHandle
	if handle.Environment != ControllerOperationHandleEnvironment ||
		handle.RequiredValue != ControllerOperationHandleRequiredValue ||
		filepath.Clean(handle.SocketPath) != filepath.Clean(WorkspaceControllerOperationSocket) ||
		filepath.Clean(handle.IdentityPath) != filepath.Clean(WorkspaceControllerOperationIdentity) ||
		handle.IdentitySchema != ControllerOperationIdentitySchema ||
		handle.IdentityMode != ManagementControllerIdentityExpectedMode ||
		handle.IdentityOwner != ManagementControllerIdentityExpectedOwner ||
		handle.IdentityGroup != ManagementControllerIdentityExpectedGroup ||
		!handle.NoFollow {
		return fmt.Errorf("Management controller operation handle does not match the compiled fail-closed contract")
	}
	if err := validateControllerProfileSourceRoot("Management", profile.SourceRoots.Management, ManagementControllerLogicalRoot); err != nil {
		return err
	}
	if err := validateControllerProfileSourceRoot("WSL/Nix", profile.SourceRoots.WSLNix, ManagementControllerWSLNixLogicalRoot); err != nil {
		return err
	}
	for name, identity := range map[string]ControllerProfileFileIdentity{
		"Fleet": profile.Inventories.Fleet,
		"GUI":   profile.Inventories.GUI,
	} {
		if !filepath.IsAbs(identity.Path) || !validControllerSHA256(identity.SHA256) {
			return fmt.Errorf("%s inventory identity is invalid", name)
		}
		actual, err := hashControllerFile(identity.Path)
		if err != nil {
			return fmt.Errorf("validate %s inventory: %w", name, err)
		}
		if actual != identity.SHA256 {
			return fmt.Errorf("%s inventory SHA-256 = %q, want %q", name, actual, identity.SHA256)
		}
	}
	if filepath.Clean(profile.Inventories.Fleet.Path) != filepath.Clean(WorkspaceControllerSourceInventory) ||
		filepath.Clean(profile.Inventories.GUI.Path) != filepath.Clean(WorkspaceControllerGUIInventory) {
		return fmt.Errorf("Management controller inventories do not use the package-owned projection paths")
	}
	if profile.Targets.Controller != ManagementControllerNode ||
		!validManagementControllerGUI(profile.Targets.ControllerGUI) ||
		profile.Targets.ProductAgentHost != ManagementControllerNode {
		return fmt.Errorf("Management controller targets do not match the compiled inventory identities")
	}
	if err := validateControllerRemoteProductGUI(profile.RemoteProductGUI); err != nil {
		return err
	}
	if err := validateControllerCodexPermissions(profile.CodexPermissions); err != nil {
		return err
	}
	if profile.Schemas != (ControllerProfileSchemas{
		Request:  ControllerOperationRequestSchema,
		Accepted: ControllerOperationAcceptedSchema,
		Event:    ControllerOperationEventSchema,
		Receipt:  ControllerOperationReceiptSchema,
	}) {
		return fmt.Errorf("Management controller operation schemas do not match the compiled contract")
	}
	expectedKinds := []string{"app-rpc.exec", "auth.exec", "fleet.exec", "gui.replace-controller", "gui.start-app-server", "nixos.deploy-closure", "nixos.wsl-sentinel", "product-agent.reconcile-absence"}
	actualKinds := append([]string(nil), profile.Kinds...)
	sort.Strings(actualKinds)
	if strings.Join(actualKinds, "\x00") != strings.Join(expectedKinds, "\x00") {
		return fmt.Errorf("Management controller operation kinds = %v, want %v", profile.Kinds, expectedKinds)
	}
	productAgent := profile.ProductAgentLifecycle
	if filepath.Clean(productAgent.SocketPath) != controllerProductAgentSocket ||
		productAgent.Operation != controllerProductAgentOperation ||
		productAgent.ReconcileOperation != "reconcile-absence" ||
		productAgent.RequestSchema != controllerProductAgentRequestSchema ||
		productAgent.EventSchema != controllerProductAgentEventSchema ||
		productAgent.Mode != "0660" ||
		productAgent.Owner != "root" ||
		productAgent.Group != "product-agent-operators" ||
		!productAgent.NoFollow {
		return fmt.Errorf("Management controller Product-agent lifecycle does not match the compiled fail-closed contract")
	}
	if err := validateControllerStoreExecutable("Product-agent lifecycle", productAgent.Executable); err != nil {
		return err
	}
	deployment := profile.NixOSDeployment
	if filepath.Clean(deployment.SocketPath) != controllerNixOSDeploymentSocket ||
		deployment.Operation != controllerNixOSDeploymentOperation ||
		deployment.RequestSchema != controllerNixOSDeploymentRequestSchema ||
		deployment.EventSchema != controllerNixOSDeploymentEventSchema ||
		deployment.ServiceUser != ManagementControllerIdentityExpectedOwner ||
		deployment.Mode != "0660" ||
		deployment.Owner != "root" ||
		deployment.Group != "fleet-deployment-operators" ||
		!deployment.NoFollow {
		return fmt.Errorf("Management controller NixOS deployment effect does not match the compiled fail-closed contract")
	}
	if err := validateControllerStoreExecutable("broker", profile.Broker.Executable); err != nil {
		return err
	}
	if profile.Broker.StateDirectory != controllerOperationStateDirectory || !validControllerGitRevision(profile.Broker.SourceRevision) {
		return fmt.Errorf("Management controller broker identity does not match the compiled contract")
	}
	if err := validateControllerStoreExecutable("Git", profile.SourceAcquisition.GitExecutable); err != nil {
		return err
	}
	if err := validateControllerStoreExecutable("SSH", profile.SourceAcquisition.SSHExecutable); err != nil {
		return err
	}
	if err := validateControllerStoreFile("SSH config", profile.SourceAcquisition.SSHConfigPath); err != nil {
		return err
	}
	return nil
}

func validManagementControllerGUI(target string) bool {
	return target == ManagementControllerPrimaryGUI || target == ManagementControllerGUI
}

func validateControllerRemoteProductGUI(remote ControllerProfileRemoteProductGUI) error {
	if remote.SchemaVersion != controllerRemoteProductGUISchema || len(remote.Targets) == 0 {
		return fmt.Errorf("Management controller remote Product GUI authority does not match the compiled contract")
	}
	seen := make(map[string]struct{}, len(remote.Targets))
	targetHandles := make(map[string]string, len(remote.Targets))
	for _, target := range remote.Targets {
		if target.Agent <= 0 || strings.TrimSpace(target.ID) == "" || strings.TrimSpace(target.Station) == "" ||
			target.Kind != "devkit-agent" || target.RuntimeProfile != "dev-all" ||
			target.GovernanceWorkspaceID != fmt.Sprintf("agent%d", target.Agent) ||
			target.SocketName != fmt.Sprintf("a%d-app.sock", target.Agent) {
			return fmt.Errorf("Management controller remote Product GUI target %q has invalid identity", target.ID)
		}
		if _, exists := seen[target.ID]; exists {
			return fmt.Errorf("Management controller remote Product GUI target %q is duplicated", target.ID)
		}
		seen[target.ID] = struct{}{}
		for label, path := range map[string]string{
			"host home": target.Home.Host, "remote home": target.Home.Remote,
			"host worktree": target.Worktree.Host, "remote worktree": target.Worktree.Remote,
			"remote Codex home": target.RemoteCodexHome, "config": target.ConfigPath,
		} {
			if !filepath.IsAbs(path) || strings.HasPrefix(filepath.ToSlash(path), "/mnt/") {
				return fmt.Errorf("Management controller remote Product GUI target %q %s path is invalid", target.ID, label)
			}
		}
		if filepath.Clean(target.RemoteCodexHome) != filepath.Join(filepath.Clean(target.Home.Remote), ".codex") ||
			filepath.Clean(target.ConfigPath) != filepath.Join(filepath.Clean(target.RemoteCodexHome), "config.toml") {
			return fmt.Errorf("Management controller remote Product GUI target %q Codex paths are inconsistent", target.ID)
		}
		transport := target.Transport
		if strings.TrimSpace(transport.Address) == "" || transport.AddressFamily != "AF_INET" ||
			transport.Connector != "direct" || transport.Port != 22 ||
			!validControllerAuthorityToken(transport.HostKeyAlias) ||
			!strings.HasPrefix(transport.ServerHostKeyFingerprint, "SHA256:") ||
			!strings.HasPrefix(transport.ClientIdentityHandle, "identity-") ||
			!validControllerAuthorityToken(transport.ClientIdentityHandle) ||
			(transport.PreferredRoute != "tailnet-direct" && transport.PreferredRoute != "wireguard-direct") ||
			transport.User != ManagementControllerIdentityExpectedOwner ||
			strings.TrimSpace(transport.WorkerAlias) == "" {
			return fmt.Errorf("Management controller remote Product GUI target %q transport is invalid", target.ID)
		}
		targetHandles[target.ID] = transport.ClientIdentityHandle
	}
	if err := validateControllerStoreExecutable("remote Product GUI SSH", remote.Transport.SSHExecutable); err != nil {
		return err
	}
	if err := validateControllerStoreExecutable("remote Product GUI SSH keygen", remote.Transport.SSHKeygenExecutable); err != nil {
		return err
	}
	targetFamilies := append([]string(nil), remote.Transport.ServiceNetwork.TargetAddressFamilies...)
	brokerFamilies := append([]string(nil), remote.Transport.ServiceNetwork.BrokerAddressFamilies...)
	sort.Strings(targetFamilies)
	sort.Strings(brokerFamilies)
	if remote.Transport.ServiceNetwork.PrivateNetwork || strings.Join(targetFamilies, "\x00") != "AF_INET" ||
		strings.Join(brokerFamilies, "\x00") != "AF_INET\x00AF_NETLINK\x00AF_UNIX" ||
		len(remote.Transport.ProtectedIdentityHandles) == 0 {
		return fmt.Errorf("Management controller remote Product GUI transport does not match the compiled contract")
	}
	seenHandles := make(map[string]struct{}, len(remote.Transport.ProtectedIdentityHandles))
	boundTargets := make(map[string]struct{}, len(remote.Targets))
	for _, handle := range remote.Transport.ProtectedIdentityHandles {
		if !strings.HasPrefix(handle.ID, "identity-") || !validControllerAuthorityToken(handle.ID) ||
			handle.RuntimePath != filepath.Join("/run/credentials/fleet-controller-operation.service", handle.ID) ||
			!strings.HasPrefix(handle.PublicKeyFingerprint, "SHA256:") || len(handle.TargetIDs) == 0 ||
			!handle.RegularFile || !handle.NoFollow || !handle.ReadableByService {
			return fmt.Errorf("Management controller remote Product GUI protected identity handle is invalid")
		}
		if _, exists := seenHandles[handle.ID]; exists {
			return fmt.Errorf("Management controller remote Product GUI protected identity handle %q is duplicated", handle.ID)
		}
		seenHandles[handle.ID] = struct{}{}
		for _, targetID := range handle.TargetIDs {
			if targetHandles[targetID] != handle.ID {
				return fmt.Errorf("Management controller remote Product GUI protected identity handle %q has invalid target %q", handle.ID, targetID)
			}
			if _, exists := boundTargets[targetID]; exists {
				return fmt.Errorf("Management controller remote Product GUI target %q has multiple protected identity handles", targetID)
			}
			boundTargets[targetID] = struct{}{}
		}
	}
	if len(boundTargets) != len(targetHandles) {
		return fmt.Errorf("Management controller remote Product GUI protected identity handles do not cover every target")
	}
	return nil
}

func validControllerAuthorityToken(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '.' || ch == '_' || ch == '-' {
			continue
		}
		return false
	}
	return true
}

func validateControllerCodexPermissions(permissions ControllerProfileCodexPermissions) error {
	projection := permissions.TargetProjection
	if permissions.Mode != "custom" || permissions.Source != "config.toml" ||
		permissions.Contract != controllerCodexPermissionContract || permissions.DestinationMode != "0600" ||
		permissions.ApprovalsReviewer != "user" || permissions.SandboxMode != "danger-full-access" ||
		permissions.ApprovalPolicy.Granular != (ControllerProfileCodexGranularPolicy{}) ||
		projection != (ControllerProfileCodexTargetProjection{
			ByteEqualToSource: true,
			Group:             ManagementControllerIdentityExpectedGroup,
			Mode:              "0600",
			NoFollow:          true,
			Owner:             ManagementControllerIdentityExpectedOwner,
			PathRule:          "remoteProductGUI.targets[].configPath",
			RegularFile:       true,
		}) || !validControllerSHA256(permissions.SourceSHA256) {
		return fmt.Errorf("Management controller Codex permissions do not match the compiled custom contract")
	}
	if err := validateControllerStoreFile("Codex permission source", permissions.SourcePath); err != nil {
		return err
	}
	actual, err := hashControllerFile(permissions.SourcePath)
	if err != nil {
		return fmt.Errorf("validate Codex permission source: %w", err)
	}
	if actual != permissions.SourceSHA256 {
		return fmt.Errorf("Codex permission source SHA-256 = %q, want %q", actual, permissions.SourceSHA256)
	}
	return nil
}

func validateControllerProfileSourceRoot(label string, root ControllerProfileSourceRoot, logicalPath string) error {
	if filepath.Clean(root.LogicalPath) != logicalPath {
		return fmt.Errorf("%s controller source logical path = %q, want %q", label, root.LogicalPath, logicalPath)
	}
	if !filepath.IsAbs(root.BackingPath) || strings.HasPrefix(filepath.ToSlash(root.BackingPath), "/mnt/") {
		return fmt.Errorf("%s controller source backing path %q must be an absolute non-Windows path", label, root.BackingPath)
	}
	if strings.TrimSpace(root.Origin) == "" {
		return fmt.Errorf("%s controller source origin is empty", label)
	}
	if !validControllerGitRevision(root.Revision) {
		return fmt.Errorf("%s controller source revision %q is not a full Git revision", label, root.Revision)
	}
	return nil
}

func validControllerGitRevision(value string) bool {
	if len(value) != 40 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateControllerStoreExecutable(label, path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	rel, err := filepath.Rel(filepath.Clean(ControllerProfileStoreRoot), path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("Management controller %s executable %s is outside %s", label, path, ControllerProfileStoreRoot)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("Management controller %s executable %s is unavailable or non-executable", label, path)
	}
	return nil
}

func validateControllerStoreFile(label, path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	rel, err := filepath.Rel(filepath.Clean(ControllerProfileStoreRoot), path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("Management controller %s %s is outside %s", label, path, ControllerProfileStoreRoot)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("Management controller %s %s is unavailable or not a real regular file", label, path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve Management controller %s %s: %w", label, path, err)
	}
	rel, err = filepath.Rel(filepath.Clean(ControllerProfileStoreRoot), filepath.Clean(resolved))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("Management controller %s %s resolves outside %s", label, path, ControllerProfileStoreRoot)
	}
	return nil
}

func hashControllerFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		resolved, resolveErr := filepath.EvalSymlinks(path)
		expected := filepath.Join("/etc/static", strings.TrimPrefix(filepath.Clean(path), "/etc/"))
		expectedResolved, expectedErr := filepath.EvalSymlinks(expected)
		if resolveErr != nil || expectedErr != nil || filepath.Clean(resolved) != filepath.Clean(expectedResolved) {
			return "", fmt.Errorf("%s must resolve only to its immutable /etc/static projection", path)
		}
		info, err = os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("inspect resolved %s: %w", path, err)
		}
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s must resolve to a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func validControllerSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

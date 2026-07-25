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
	ManagementControllerProfileSchema       = "wsl-nix-management-controller-convergence/v1"
	ControllerOperationIdentitySchema       = "fleet-control/controller-operation-identity/v2"
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
	ManagementControllerDrTalos             = "drtalos"
	controllerOperationStateDirectory       = "/var/lib/fleet-controller-operation"
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
	Controller    string `json:"controller"`
	ControllerGUI string `json:"controllerGui"`
	DrTalos       string `json:"drtalos"`
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

type ManagementControllerProfile struct {
	SchemaVersion       string                             `json:"schemaVersion"`
	ProfileIdentity     string                             `json:"profileIdentity"`
	MountPolicyIdentity string                             `json:"mountPolicyIdentity"`
	OperationHandle     ControllerProfileOperationHandle   `json:"operationHandle"`
	SourceRoots         ControllerProfileSourceRoots       `json:"sourceRoots"`
	Inventories         ControllerProfileInventories       `json:"inventories"`
	Targets             ControllerProfileTargets           `json:"targets"`
	Schemas             ControllerProfileSchemas           `json:"schemas"`
	Kinds               []string                           `json:"kinds"`
	Broker              ControllerProfileBroker            `json:"broker"`
	SourceAcquisition   ControllerProfileSourceAcquisition `json:"sourceAcquisition"`
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
		profile.Targets.ControllerGUI != ManagementControllerGUI ||
		profile.Targets.DrTalos != ManagementControllerDrTalos {
		return fmt.Errorf("Management controller targets do not match the compiled inventory identities")
	}
	if profile.Schemas != (ControllerProfileSchemas{
		Request:  ControllerOperationRequestSchema,
		Accepted: ControllerOperationAcceptedSchema,
		Event:    ControllerOperationEventSchema,
		Receipt:  ControllerOperationReceiptSchema,
	}) {
		return fmt.Errorf("Management controller operation schemas do not match the compiled contract")
	}
	expectedKinds := []string{"fleet.exec", "gui.replace-controller", "nixos.deploy-closure"}
	actualKinds := append([]string(nil), profile.Kinds...)
	sort.Strings(actualKinds)
	if strings.Join(actualKinds, "\x00") != strings.Join(expectedKinds, "\x00") {
		return fmt.Errorf("Management controller operation kinds = %v, want %v", profile.Kinds, expectedKinds)
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

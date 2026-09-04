package launch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"devkit/cli/devctl/internal/gitauthority"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

type controllerOperationFileIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type controllerOperationPackageIdentity struct {
	Path           string `json:"path"`
	ExecutablePath string `json:"executablePath"`
}

type controllerOperationSocketIdentity struct {
	Path           string `json:"path"`
	Device         uint64 `json:"device"`
	Inode          uint64 `json:"inode"`
	ChangeTimeSec  int64  `json:"changeTimeSec"`
	ChangeTimeNsec int64  `json:"changeTimeNsec"`
	UID            uint32 `json:"uid"`
	GID            uint32 `json:"gid"`
	Mode           uint32 `json:"mode"`
}

type controllerOperationInventories struct {
	Fleet controllerOperationFileIdentity `json:"fleet"`
	GUI   controllerOperationFileIdentity `json:"gui"`
}

type controllerOperationSourceRoots struct {
	ManagementHostPath     string `json:"managementHostPath"`
	ManagementConsumerPath string `json:"managementConsumerPath"`
	WSLHostPath            string `json:"wslHostPath"`
	WSLConsumerPath        string `json:"wslConsumerPath"`
	WSLOrigin              string `json:"wslOrigin"`
	WSLRevision            string `json:"wslRevision"`
}

type controllerOperationTargets struct {
	ControllerNode   string `json:"controllerNode"`
	ControllerGUI    string `json:"controllerGui"`
	ProductAgentHost string `json:"productAgentHost"`
}

type controllerOperationProductAgentLifecycle struct {
	SocketPath         string `json:"socketPath"`
	Executable         string `json:"executable"`
	Operation          string `json:"operation"`
	ReconcileOperation string `json:"reconcileOperation"`
	RequestSchema      string `json:"requestSchema"`
	EventSchema        string `json:"eventSchema"`
}

type controllerOperationProductStationReset struct {
	SocketPath    string `json:"socketPath"`
	Executable    string `json:"executable"`
	Operation     string `json:"operation"`
	RequestSchema string `json:"requestSchema"`
	EventSchema   string `json:"eventSchema"`
	ServiceUser   string `json:"serviceUser"`
}

type controllerOperationNixOSDeployment struct {
	SocketPath    string `json:"socketPath"`
	Operation     string `json:"operation"`
	RequestSchema string `json:"requestSchema"`
	EventSchema   string `json:"eventSchema"`
	ServiceUser   string `json:"serviceUser"`
}

type controllerOperationIdentity struct {
	SchemaVersion         string                                   `json:"schemaVersion"`
	State                 string                                   `json:"state"`
	AuthorityManifest     controllerOperationFileIdentity          `json:"authorityManifest"`
	Package               controllerOperationPackageIdentity       `json:"package"`
	Socket                controllerOperationSocketIdentity        `json:"socket"`
	ProductAgentLifecycle controllerOperationProductAgentLifecycle `json:"productAgentLifecycle"`
	ProductStationReset   controllerOperationProductStationReset   `json:"productStationReset"`
	NixOSDeployment       controllerOperationNixOSDeployment       `json:"nixosDeployment"`
	SourceInventories     controllerOperationInventories           `json:"sourceInventories"`
	SourceRoots           controllerOperationSourceRoots           `json:"sourceRoots"`
	Targets               controllerOperationTargets               `json:"targets"`
	Schemas               nativeplan.ControllerProfileSchemas      `json:"schemas"`
	Kinds                 []string                                 `json:"kinds"`
	ServerPID             int                                      `json:"serverPid"`
	StartedAt             string                                   `json:"startedAt"`
}

var controllerOperationStoreRoot = "/nix/store"

func validateManagementControllerProfilePlan(p nativeplan.Plan) error {
	if strings.TrimSpace(p.ControllerProfile) == "" {
		return nil
	}
	if p.ControllerProfile != nativeplan.ManagementControllerProfileIdentity ||
		p.ControllerManifest != nativeplan.ManagementControllerProfileManifestPath ||
		p.MountPolicyIdentity != nativeplan.ManagementControllerMountPolicyIdentity ||
		p.Env[nativeplan.ManagementControllerProfileEnvironment] != nativeplan.ManagementControllerProfileIdentity ||
		p.Env[nativeplan.ControllerOperationHandleEnvironment] != nativeplan.ControllerOperationHandleRequiredValue ||
		p.Env[nativeplan.WorkspaceControllerWorkLedgerEnvironment] != nativeplan.WorkspaceControllerWorkLedgerPath {
		return fmt.Errorf("Management controller plan does not match the named v1 capability profile")
	}
	profile, err := nativeplan.LoadManagementControllerProfile(p.ControllerManifest)
	if err != nil {
		return err
	}
	ownerPreparation, err := nativeplan.LoadEMDROwnerPreparationCapability(nativeplan.EMDROwnerPreparationManifestPath)
	if err != nil {
		return err
	}
	if p.Env[nativeplan.EMDROwnerPreparationHandleEnvironment] != nativeplan.EMDROwnerPreparationHandleRequired {
		return fmt.Errorf("Management controller plan lacks the required EMDR owner preparation handle")
	}
	activeManagementRoot := profile.SourceRoots.Management
	activeManagementRoot.BackingPath = p.Agent.HostWorktree
	activeWSLNixRoot := profile.SourceRoots.WSLNix
	if strings.TrimSpace(p.HostWorkspaceRoot) != "" {
		activeWSLNixRoot.BackingPath = filepath.Join(filepath.Clean(p.HostWorkspaceRoot), "wsl-nix")
	}
	for _, expected := range []nativeplan.Bind{
		{Source: activeWSLNixRoot.BackingPath, Target: activeWSLNixRoot.LogicalPath, Mode: "rw", Required: true},
		{Source: nativeplan.ManagementControllerProfileManifestPath, Target: nativeplan.ManagementControllerProfileManifestPath, Mode: "ro", Required: true},
		{Source: nativeplan.WorkspaceControllerOperationSocket, Target: nativeplan.WorkspaceControllerOperationSocket, Mode: "ro", Required: true},
		{Source: nativeplan.WorkspaceControllerOperationIdentity, Target: nativeplan.WorkspaceControllerOperationIdentity, Mode: "ro", Required: true},
		{Source: nativeplan.EMDROwnerPreparationManifestPath, Target: nativeplan.EMDROwnerPreparationManifestPath, Mode: "ro", Required: true},
		{Source: ownerPreparation.SocketPath, Target: nativeplan.EMDROwnerPreparationSocketPath, Mode: "ro", Required: true},
		{Source: ownerPreparation.IdentityPath, Target: nativeplan.EMDROwnerPreparationIdentityPath, Mode: "ro", Required: true},
		{Source: nativeplan.WorkspaceControllerWorkLedgerDirectory, Target: nativeplan.WorkspaceControllerWorkLedgerDirectory, Mode: "rw", Required: true},
	} {
		if !planHasExactBind(p.Binds, expected) {
			return fmt.Errorf("Management controller plan lacks exact required bind %+v", expected)
		}
	}
	if err := validateControllerSourceWorktree("active Management", activeManagementRoot); err != nil {
		return err
	}
	if err := validateControllerSourceWorktree("WSL/Nix", activeWSLNixRoot); err != nil {
		return err
	}
	if err := validateControllerOperationIdentity(profile); err != nil {
		return err
	}
	return validateEMDROwnerPreparationIdentity(ownerPreparation)
}

func planHasExactBind(binds []nativeplan.Bind, expected nativeplan.Bind) bool {
	for _, bind := range binds {
		if filepath.Clean(bind.Source) == filepath.Clean(expected.Source) &&
			filepath.Clean(bind.Target) == filepath.Clean(expected.Target) &&
			bind.Mode == expected.Mode && bind.Required == expected.Required {
			return true
		}
	}
	return false
}

func validateControllerSourceWorktree(label string, root nativeplan.ControllerProfileSourceRoot) error {
	info, err := os.Lstat(root.BackingPath)
	if err != nil {
		return fmt.Errorf("inspect %s controller source root %s: %w", label, root.BackingPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s controller source root %s must be a real non-symlink directory", label, root.BackingPath)
	}
	if err := requireGitWorktree(root.BackingPath); err != nil {
		return fmt.Errorf("%s controller source: %w", label, err)
	}
	for description, args := range map[string][]string{
		"HEAD":       {"rev-parse", "HEAD"},
		"origin URL": {"remote", "get-url", "origin"},
		"status":     {"status", "--porcelain=v1", "--untracked-files=all"},
	} {
		output, err := exec.Command(gitauthority.Executable(), append([]string{"-C", root.BackingPath}, args...)...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("read %s controller source %s: %w: %s", label, description, err, strings.TrimSpace(string(output)))
		}
		value := strings.TrimSpace(string(output))
		switch description {
		case "HEAD":
			if value != root.Revision {
				return fmt.Errorf("%s controller source HEAD = %q, want %q", label, value, root.Revision)
			}
		case "origin URL":
			if value != root.Origin {
				return fmt.Errorf("%s controller source origin = %q, want %q", label, value, root.Origin)
			}
		case "status":
			if value != "" {
				return fmt.Errorf("%s controller source root %s is dirty", label, root.BackingPath)
			}
		}
	}
	return nil
}

func validateControllerOperationIdentity(profile nativeplan.ManagementControllerProfile) error {
	socketInfo, err := os.Lstat(nativeplan.WorkspaceControllerOperationSocket)
	if err != nil {
		return fmt.Errorf("inspect controller operation socket: %w", err)
	}
	if socketInfo.Mode()&os.ModeSymlink != 0 || socketInfo.Mode()&os.ModeSocket == 0 || socketInfo.Mode().Perm() != 0o600 {
		return fmt.Errorf("controller operation socket must be a non-symlink mode 0600 Unix socket")
	}
	socketStat, ok := socketInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("controller operation socket lacks stat identity")
	}
	identityInfo, err := os.Lstat(nativeplan.WorkspaceControllerOperationIdentity)
	if err != nil {
		return fmt.Errorf("inspect controller operation identity: %w", err)
	}
	if identityInfo.Mode()&os.ModeSymlink != 0 || !identityInfo.Mode().IsRegular() || identityInfo.Mode().Perm() != 0o400 {
		return fmt.Errorf("controller operation identity must be a regular non-symlink mode 0400 file")
	}
	current, err := user.Current()
	if err != nil {
		return fmt.Errorf("resolve launching user: %w", err)
	}
	expectedGroup, err := user.LookupGroup(profile.OperationHandle.IdentityGroup)
	if err != nil {
		return fmt.Errorf("resolve controller operation identity group %q: %w", profile.OperationHandle.IdentityGroup, err)
	}
	expectedUID, err := strconv.ParseUint(current.Uid, 10, 32)
	if err != nil {
		return fmt.Errorf("parse launching UID: %w", err)
	}
	expectedGID, err := strconv.ParseUint(expectedGroup.Gid, 10, 32)
	if err != nil {
		return fmt.Errorf("parse controller identity GID: %w", err)
	}
	if current.Username != profile.OperationHandle.IdentityOwner ||
		uint64(socketStat.Uid) != expectedUID ||
		uint64(socketStat.Gid) != expectedGID {
		return fmt.Errorf("controller operation socket ownership does not match the manifest")
	}
	identityStat, ok := identityInfo.Sys().(*syscall.Stat_t)
	if !ok || uint64(identityStat.Uid) != expectedUID || uint64(identityStat.Gid) != expectedGID {
		return fmt.Errorf("controller operation identity ownership does not match the manifest")
	}
	data, err := os.ReadFile(nativeplan.WorkspaceControllerOperationIdentity)
	if err != nil {
		return fmt.Errorf("read controller operation identity: %w", err)
	}
	var identity controllerOperationIdentity
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&identity); err != nil {
		return fmt.Errorf("decode controller operation identity: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode controller operation identity: trailing JSON value")
		}
		return fmt.Errorf("decode controller operation identity: trailing JSON content: %w", err)
	}
	if identity.SchemaVersion != nativeplan.ControllerOperationIdentitySchema || identity.State != "ready" {
		return fmt.Errorf("controller operation identity is not ready on the required schema")
	}
	if identity.Socket != (controllerOperationSocketIdentity{
		Path:           nativeplan.WorkspaceControllerOperationSocket,
		Device:         uint64(socketStat.Dev),
		Inode:          socketStat.Ino,
		ChangeTimeSec:  socketStat.Ctim.Sec,
		ChangeTimeNsec: socketStat.Ctim.Nsec,
		UID:            uint32(socketStat.Uid),
		GID:            uint32(socketStat.Gid),
		Mode:           uint32(socketInfo.Mode().Perm()),
	}) {
		return fmt.Errorf("controller operation identity does not match the live socket generation")
	}
	if identity.SourceInventories.Fleet != (controllerOperationFileIdentity{
		Path: profile.Inventories.Fleet.Path, SHA256: profile.Inventories.Fleet.SHA256,
	}) || identity.SourceInventories.GUI != (controllerOperationFileIdentity{
		Path: profile.Inventories.GUI.Path, SHA256: profile.Inventories.GUI.SHA256,
	}) {
		return fmt.Errorf("controller operation identity inventory digests do not match the profile")
	}
	if identity.SourceRoots != (controllerOperationSourceRoots{
		ManagementHostPath:     profile.SourceRoots.Management.BackingPath,
		ManagementConsumerPath: profile.SourceRoots.Management.LogicalPath,
		WSLHostPath:            profile.SourceRoots.WSLNix.BackingPath,
		WSLConsumerPath:        profile.SourceRoots.WSLNix.LogicalPath,
		WSLOrigin:              profile.SourceRoots.WSLNix.Origin,
		WSLRevision:            profile.SourceRoots.WSLNix.Revision,
	}) {
		return fmt.Errorf("controller operation identity source roots do not match the profile")
	}
	if identity.Targets != (controllerOperationTargets{
		ControllerNode:   profile.Targets.Controller,
		ControllerGUI:    profile.Targets.ControllerGUI,
		ProductAgentHost: profile.Targets.ProductAgentHost,
	}) || identity.Schemas != profile.Schemas {
		return fmt.Errorf("controller operation identity targets or schemas do not match the profile")
	}
	if identity.ProductAgentLifecycle != (controllerOperationProductAgentLifecycle{
		SocketPath:         profile.ProductAgentLifecycle.SocketPath,
		Executable:         profile.ProductAgentLifecycle.Executable,
		Operation:          profile.ProductAgentLifecycle.Operation,
		ReconcileOperation: profile.ProductAgentLifecycle.ReconcileOperation,
		RequestSchema:      profile.ProductAgentLifecycle.RequestSchema,
		EventSchema:        profile.ProductAgentLifecycle.EventSchema,
	}) {
		return fmt.Errorf("controller operation identity Product-agent lifecycle does not match the profile")
	}
	if identity.ProductStationReset != (controllerOperationProductStationReset{
		SocketPath:    profile.ProductStationReset.SocketPath,
		Executable:    profile.ProductStationReset.Executable,
		Operation:     profile.ProductStationReset.Operation,
		RequestSchema: profile.ProductStationReset.RequestSchema,
		EventSchema:   profile.ProductStationReset.EventSchema,
		ServiceUser:   profile.ProductStationReset.ServiceUser,
	}) {
		return fmt.Errorf("controller operation identity Product station reset effect does not match the profile")
	}
	if identity.NixOSDeployment != (controllerOperationNixOSDeployment{
		SocketPath:    profile.NixOSDeployment.SocketPath,
		Operation:     profile.NixOSDeployment.Operation,
		RequestSchema: profile.NixOSDeployment.RequestSchema,
		EventSchema:   profile.NixOSDeployment.EventSchema,
		ServiceUser:   profile.NixOSDeployment.ServiceUser,
	}) {
		return fmt.Errorf("controller operation identity NixOS deployment effect does not match the profile")
	}
	expectedKinds := append([]string(nil), profile.Kinds...)
	actualKinds := append([]string(nil), identity.Kinds...)
	sort.Strings(expectedKinds)
	sort.Strings(actualKinds)
	if strings.Join(actualKinds, "\x00") != strings.Join(expectedKinds, "\x00") {
		return fmt.Errorf("controller operation identity kinds do not match the profile")
	}
	if filepath.Clean(identity.Package.ExecutablePath) != filepath.Clean(profile.Broker.Executable) {
		return fmt.Errorf("controller operation executable does not match the manifest-bound broker")
	}
	if err := validateControllerAuthorityPackage(identity); err != nil {
		return err
	}
	if identity.ServerPID <= 0 {
		return fmt.Errorf("controller operation identity has invalid server PID")
	}
	if _, err := time.Parse(time.RFC3339, identity.StartedAt); err != nil {
		return fmt.Errorf("controller operation identity startedAt is invalid: %w", err)
	}
	return nil
}

func validateControllerAuthorityPackage(identity controllerOperationIdentity) error {
	manifestPath := filepath.Clean(identity.AuthorityManifest.Path)
	packagePath := filepath.Clean(identity.Package.Path)
	executablePath := filepath.Clean(identity.Package.ExecutablePath)
	executableRel, err := filepath.Rel(filepath.Clean(controllerOperationStoreRoot), executablePath)
	if err != nil {
		return fmt.Errorf("resolve controller operation executable package: %w", err)
	}
	executableParts := strings.Split(filepath.ToSlash(executableRel), "/")
	if len(executableParts) < 2 || executableParts[0] == "" ||
		packagePath != filepath.Join(filepath.Clean(controllerOperationStoreRoot), executableParts[0]) {
		return fmt.Errorf("controller operation package path does not own the manifest-bound executable")
	}
	for label, path := range map[string]string{
		"authority manifest": manifestPath,
		"package":            packagePath,
		"executable":         executablePath,
	} {
		rel, err := filepath.Rel(filepath.Clean(controllerOperationStoreRoot), path)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("controller operation %s %s is outside %s", label, path, controllerOperationStoreRoot)
		}
	}
	if rel, err := filepath.Rel(packagePath, executablePath); err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("controller operation executable is outside its package")
	}
	manifestSHA, err := hashRegularNoFollowFile(manifestPath, false)
	if err != nil {
		return fmt.Errorf("validate controller operation authority manifest: %w", err)
	}
	if manifestSHA != identity.AuthorityManifest.SHA256 {
		return fmt.Errorf("controller operation authority manifest SHA-256 = %q, want %q", manifestSHA, identity.AuthorityManifest.SHA256)
	}
	info, err := os.Lstat(executablePath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("controller operation executable %s is not a real executable file", executablePath)
	}
	return nil
}

func hashRegularNoFollowFile(path string, allowNixETCSymlink bool) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 && !allowNixETCSymlink {
		return "", fmt.Errorf("%s is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

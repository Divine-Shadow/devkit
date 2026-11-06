package plan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"devkit/cli/devctl/internal/runtime/agent"
	"devkit/cli/devctl/internal/worktrees"
)

const (
	controllerProfileEnvironment = "DEVKIT_MANAGEMENT_CONTROLLER_PROFILE"
	controllerProfileIdentity    = "management-controller-convergence/v1"
	controllerHandleEnvironment  = "FLEET_CONTROLLER_OPERATION_HANDLE"
	controllerManifestTarget     = "/etc/fleet/controller-operation-authority.json"
	controllerWSLTarget          = "/workspaces/dev/wsl-nix"
)

// Production locations are fixed. Package tests replace this private value;
// neither native arguments nor environment variables can choose these paths.
var controllerLocations = struct {
	manifest, store, devRoot, socket string
	hostname                         func() (string, error)
}{
	controllerManifestTarget, "/nix/store", "/home/bayesartre/dev",
	"/run/fleet-controller-operation/control.sock", os.Hostname,
}

// ControllerProjection describes only the fixed consumer projection. It is not
// a broker authority/credential grant or a replacement-controller goal record.
// The unchanged Fleet broker validates the complete v9 document and every effect.
type ControllerProjection struct {
	TargetID       string `json:"targetId"`
	ManifestSource string `json:"manifestSource"`
	ManifestSHA256 string `json:"manifestSha256"`
	WSLLaneSource  string `json:"wslLaneSource"`
	captured       *controllerProjectionSnapshot
}

type controllerProjectionSnapshot struct {
	geometry                                          guiTargetGeometry
	gui                                               GUITargetConfigProjection
	manifestInfo, socketInfo, managementInfo, wslInfo os.FileInfo
	manifest                                          controllerProjectionManifest
	binds                                             []Bind
}

type controllerSourceRoot struct {
	LogicalPath string `json:"logicalPath"`
	BackingPath string `json:"backingPath"`
	Origin      string `json:"origin"`
	Revision    string `json:"revision"`
}
type controllerProjectionManifest struct {
	SchemaVersion       string `json:"schemaVersion"`
	ProfileIdentity     string `json:"profileIdentity"`
	MountPolicyIdentity string `json:"mountPolicyIdentity"`
	OperationHandle     struct {
		Environment   string `json:"environment"`
		RequiredValue string `json:"requiredValue"`
		SocketPath    string `json:"socketPath"`
		SocketOwner   string `json:"socketOwner"`
		SocketGroup   string `json:"socketGroup"`
		NoFollow      bool   `json:"noFollow"`
	} `json:"operationHandle"`
	SourceRoots struct {
		Management controllerSourceRoot `json:"management"`
		WSLNix     controllerSourceRoot `json:"wslNix"`
	} `json:"sourceRoots"`
	Targets struct {
		Controller              string `json:"controller"`
		ControllerGUI           string `json:"controllerGui"`
		ControllerAdditionalGUI string `json:"controllerAdditionalGui,omitempty"`
		ProductAgentHost        string `json:"productAgentHost"`
	} `json:"targets"`
	Kinds []string `json:"kinds"`
	// Preserve the existing full document; these sections are validated by Fleet,
	// not reinterpreted or synthesized by this filesystem projection adapter.
	Broker                          json.RawMessage `json:"broker"`
	SourceAcquisition               json.RawMessage `json:"sourceAcquisition"`
	LinuxDesktopAuth                json.RawMessage `json:"linuxDesktopAuth"`
	AuthRefresh                     json.RawMessage `json:"authRefresh"`
	RemoteProductGUI                json.RawMessage `json:"remoteProductGUI"`
	CodexPermissions                json.RawMessage `json:"codexPermissions"`
	ProductAgentLifecycle           json.RawMessage `json:"productAgentLifecycle"`
	ProductStationReset             json.RawMessage `json:"productStationReset"`
	FleetRecovery                   json.RawMessage `json:"fleetRecovery"`
	GUIThreadControl                json.RawMessage `json:"guiThreadControl,omitempty"`
	SensitiveInputIngress           json.RawMessage `json:"sensitiveInputIngress"`
	SensitiveOutputEgress           json.RawMessage `json:"sensitiveOutputEgress"`
	NixOSDeployment                 json.RawMessage `json:"nixosDeployment"`
	Inventories                     json.RawMessage `json:"inventories"`
	Schemas                         json.RawMessage `json:"schemas"`
	ProductGUIProxyRetirementPolicy json.RawMessage `json:"productGUIProxyRetirementPolicy,omitempty"`
}

func controllerRefusal() error {
	return fmt.Errorf("source-selected controller capability projection refused")
}

func controllerPaths(opts BuildOptions) (agent.Paths, error) {
	return agent.ResolvePaths(agent.PathConfig{DevkitRoot: opts.Paths.Root, HostRoot: opts.HostRoot, Project: opts.Project, Repo: opts.Repo, Index: opts.Index, WorktreeRoot: opts.WorktreeRoot, StateRoot: opts.StateRoot, WorktreeContainerRoot: opts.WorktreeContainerRoot, StateContainerRoot: opts.StateContainerRoot, AgentStatePrefix: opts.AgentStatePrefix, DedicatedWorktree: true})
}

func selectControllerProjection(opts BuildOptions) (*ControllerProjection, error) {
	marker := os.Getenv(controllerProfileEnvironment)
	if marker == "" {
		return nil, nil
	}
	if marker != controllerProfileIdentity || opts.Project != "dev-workspace" || opts.Repo != "shadow-throne-management" || (opts.Index != 1 && opts.Index != 2) || normalizeIsolationProfile(opts.IsolationProfile) != IsolationProfileWorkspaceEgress || opts.GUITargetID == "" || opts.RequireGUIConfig {
		return nil, controllerRefusal()
	}
	paths, err := controllerPaths(opts)
	if err != nil {
		return nil, controllerRefusal()
	}
	expected := filepath.Join(controllerLocations.devRoot, "control-plane-worktrees", fmt.Sprintf("agent%d", opts.Index))
	if paths.DevRoot != controllerLocations.devRoot || opts.WorkspaceRoot != expected || paths.HostWorktree != filepath.Join(expected, opts.Repo) {
		return nil, controllerRefusal()
	}
	geometry := guiTargetGeometry{Project: opts.Project, Repo: opts.Repo, AgentIndex: opts.Index, WorkspaceRoot: expected, HostWorktree: paths.HostWorktree, HostHome: paths.HostHome, SandboxHome: paths.SandboxHome}
	gui, err := loadGUITargetConfigProjection(opts.GUITargetID, geometry)
	if err != nil {
		return nil, controllerRefusal()
	}
	return captureControllerProjection(geometry, gui)
}

func captureControllerProjection(geometry guiTargetGeometry, gui GUITargetConfigProjection) (*ControllerProjection, error) {
	expectedID := "shadow-throne-management"
	if geometry.AgentIndex == 2 {
		expectedID += "-2"
	}
	host, err := controllerLocations.hostname()
	if err != nil || host != "shadow-throne" || gui.TargetID != expectedID || gui.Kind != "local-wsl-management" || gui.ExpectedExecutionHost != host || gui.RetainedHistoryManifest != "" {
		return nil, controllerRefusal()
	}
	source, err := filepath.EvalSymlinks(controllerLocations.manifest)
	if err != nil || !filepath.IsAbs(source) || filepath.Clean(source) != source || !strings.HasPrefix(source, controllerLocations.store+string(filepath.Separator)) {
		return nil, controllerRefusal()
	}
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0222 != 0 || info.Size() > 1<<20 {
		return nil, controllerRefusal()
	}
	file, err := os.Open(source)
	if err != nil {
		return nil, controllerRefusal()
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, controllerRefusal()
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(info, after) {
		return nil, controllerRefusal()
	}
	var manifest controllerProjectionManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, controllerRefusal()
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, controllerRefusal()
	}
	handle := manifest.OperationHandle
	if manifest.SchemaVersion != "wsl-nix-management-controller-convergence/v9" || manifest.ProfileIdentity != controllerProfileIdentity || manifest.MountPolicyIdentity != "devkit/workspace-egress/v4" || handle.Environment != controllerHandleEnvironment || handle.RequiredValue != "required" || handle.SocketPath != controllerLocations.socket || handle.SocketOwner != "bayesartre" || handle.SocketGroup != "users" || !handle.NoFollow || manifest.Targets.Controller != host {
		return nil, controllerRefusal()
	}
	allowed := func(id string) bool { return id == "shadow-throne-management" || id == "shadow-throne-management-2" }
	if !allowed(manifest.Targets.ControllerGUI) || (manifest.Targets.ControllerAdditionalGUI != "" && (!allowed(manifest.Targets.ControllerAdditionalGUI) || manifest.Targets.ControllerAdditionalGUI == manifest.Targets.ControllerGUI)) || (gui.TargetID != manifest.Targets.ControllerGUI && gui.TargetID != manifest.Targets.ControllerAdditionalGUI) {
		return nil, controllerRefusal()
	}
	kinds := append([]string(nil), manifest.Kinds...)
	sort.Strings(kinds)
	if !reflect.DeepEqual(kinds, []string{"app-rpc.exec", "auth.exec", "fleet.exec", "gui.replace-controller", "gui.start-app-server", "nixos.deploy-closure", "nixos.wsl-sentinel", "product-agent.reconcile-absence"}) {
		return nil, controllerRefusal()
	}
	if manifest.SourceRoots.Management.LogicalPath != "/workspaces/dev/shadow-throne-management" || manifest.SourceRoots.WSLNix.LogicalPath != controllerWSLTarget {
		return nil, controllerRefusal()
	}
	socket, err := os.Lstat(controllerLocations.socket)
	if err != nil || socket.Mode()&os.ModeSymlink != 0 || socket.Mode()&os.ModeSocket == 0 || socket.Mode().Perm() != 0600 {
		return nil, controllerRefusal()
	}
	stat, ok := socket.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return nil, controllerRefusal()
	}
	// Existing GUI source materialization is lane-local, even when the manifest
	// separately records a shared WSL backing checkout. Never mount that shared root.
	wsl := filepath.Join(geometry.WorkspaceRoot, "wsl-nix")
	managementInfo, err := controllerSourceIdentity(geometry.HostWorktree, manifest.SourceRoots.Management)
	if err != nil {
		return nil, controllerRefusal()
	}
	wslInfo, err := controllerSourceIdentity(wsl, manifest.SourceRoots.WSLNix)
	if err != nil {
		return nil, controllerRefusal()
	}
	sum := sha256.Sum256(data)
	return &ControllerProjection{TargetID: gui.TargetID, ManifestSource: source, ManifestSHA256: hex.EncodeToString(sum[:]), WSLLaneSource: wsl, captured: &controllerProjectionSnapshot{geometry: geometry, gui: gui, manifestInfo: info, socketInfo: socket, managementInfo: managementInfo, wslInfo: wslInfo, manifest: manifest}}, nil
}

func controllerSourceIdentity(path string, root controllerSourceRoot) (os.FileInfo, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || root.Origin == "" || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(root.Revision) {
		return nil, controllerRefusal()
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return nil, controllerRefusal()
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return nil, controllerRefusal()
	}
	git := func(args ...string) (string, error) {
		command := append([]string{"--no-replace-objects", "-C", path}, args...)
		executable, argv, err := worktrees.NativeSourceGitInvocation(false, "", command...)
		if err != nil {
			return "", err
		}
		output, err := exec.Command(executable, argv...).Output()
		return strings.TrimSpace(string(output)), err
	}
	checks := [][]string{{"rev-parse", "--show-toplevel"}, {"rev-parse", "HEAD"}, {"remote", "get-url", "origin"}, {"status", "--porcelain=v1", "--untracked-files=all"}}
	want := []string{path, root.Revision, root.Origin, ""}
	for i, args := range checks {
		got, err := git(args...)
		if err != nil || got != want[i] {
			return nil, controllerRefusal()
		}
	}
	flags, err := git("ls-files", "-v")
	if err != nil {
		return nil, controllerRefusal()
	}
	for _, line := range strings.Split(flags, "\n") {
		if line != "" && line[0] != 'H' {
			return nil, controllerRefusal()
		}
	}
	return info, nil
}

func applyControllerProjection(p *Plan, projection *ControllerProjection) error {
	if projection == nil {
		return nil
	}
	if p.GUITargetConfig == nil || *p.GUITargetConfig != projection.captured.gui || guiTargetGeometryForPlan(*p) != projection.captured.geometry {
		return controllerRefusal()
	}
	p.Controller = projection
	p.MountPolicyIdentity = "devkit/workspace-egress/v4"
	p.Env["DEVKIT_NATIVE_MOUNT_POLICY_IDENTITY"] = p.MountPolicyIdentity
	p.Env[controllerProfileEnvironment] = controllerProfileIdentity
	p.Env[controllerHandleEnvironment] = "required"
	p.Binds = append(p.Binds, controllerProjectionBinds(projection)...)
	projection.captured.binds = append([]Bind(nil), p.Binds...)
	return nil
}

func controllerProjectionBinds(p *ControllerProjection) []Bind {
	return []Bind{{Source: p.ManifestSource, Target: controllerManifestTarget, Mode: "ro", Required: true}, {Source: controllerLocations.socket, Target: controllerLocations.socket, Mode: "rw", Required: true}, {Source: p.WSLLaneSource, Target: controllerWSLTarget, Mode: "rw", Required: true}}
}

// ValidateControllerProjection repeats the fixed source selection immediately
// before preparation and process construction. Serialized/caller-made Plans do
// not contain the private captured premise and cannot mint this capability.
func ValidateControllerProjection(p Plan) error {
	if p.Controller == nil {
		if p.Env[controllerProfileEnvironment] != "" || p.Env[controllerHandleEnvironment] != "" {
			return controllerRefusal()
		}
		for _, b := range p.Binds {
			if b.Target == controllerManifestTarget || b.Target == controllerLocations.socket {
				return controllerRefusal()
			}
		}
		return nil
	}
	before := p.Controller
	if before.captured == nil || p.GUITargetConfig == nil || p.IsolationProfile != IsolationProfileWorkspaceEgress || p.WindowsMountsVisible || p.MountPolicyIdentity != "devkit/workspace-egress/v4" || !reflect.DeepEqual(p.Binds, before.captured.binds) || p.Env["DEVKIT_NATIVE_WINDOWS_MOUNTS_VISIBLE"] != "false" || p.Env[GUITargetIDEnvironment] != before.TargetID || p.Env["DEVKIT_NATIVE_MOUNT_POLICY_IDENTITY"] != "devkit/workspace-egress/v4" || p.Env[controllerProfileEnvironment] != controllerProfileIdentity || p.Env[controllerHandleEnvironment] != "required" || guiTargetGeometryForPlan(p) != before.captured.geometry {
		return controllerRefusal()
	}
	gui, err := loadGUITargetConfigProjection(before.TargetID, guiTargetGeometryForPlan(p))
	if err != nil || gui != before.captured.gui || gui != *p.GUITargetConfig {
		return controllerRefusal()
	}
	after, err := captureControllerProjection(guiTargetGeometryForPlan(p), gui)
	if err != nil {
		return controllerRefusal()
	}
	if before.ManifestSource != after.ManifestSource || before.ManifestSHA256 != after.ManifestSHA256 || before.WSLLaneSource != after.WSLLaneSource || !os.SameFile(before.captured.manifestInfo, after.captured.manifestInfo) || !os.SameFile(before.captured.socketInfo, after.captured.socketInfo) || !os.SameFile(before.captured.managementInfo, after.captured.managementInfo) || !os.SameFile(before.captured.wslInfo, after.captured.wslInfo) {
		return controllerRefusal()
	}
	for _, expected := range controllerProjectionBinds(before) {
		count := 0
		for _, actual := range p.Binds {
			if actual.Target == expected.Target {
				if actual != expected {
					return controllerRefusal()
				}
				count++
			}
		}
		if count != 1 {
			return controllerRefusal()
		}
	}
	return nil
}

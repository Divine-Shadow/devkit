package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	CallbackReturnSchema              = "fleet/callback-return/v1"
	CallbackReturnProfileIdentity     = "devkit/workspace-egress-callback-return/v1"
	CallbackReturnManifestSource      = "/run/current-system/etc/fleet/callback-return.json"
	CallbackReturnManifestTarget      = "/etc/fleet/callback-return.json"
	CallbackReturnManifestEnvironment = "FLEET_CALLBACK_RETURN_MANIFEST"
	CallbackReturnClientEnvironment   = "FLEET_CALLBACK_RETURN_CLIENT"
	CallbackReturnProfileEnvironment  = "FLEET_CALLBACK_RETURN_PROFILE_IDENTITY"
	CallbackReturnStableParent        = "/run/fleet-cbr01-app-rpc"
	callbackReturnExpectedHost        = "darksteel"
	callbackReturnExpectedProject     = "dev-all"
	callbackReturnExpectedRepo        = "ouroboros-ide"
)

// These are package-owned paths. Tests may substitute a fixture; callers have
// no argument or environment override for the callback-return authority.
var callbackReturnManifestSource = CallbackReturnManifestSource
var callbackReturnStoreRoot = "/nix/store"
var callbackReturnExpectedHandleDirectory = CallbackReturnStableParent

type CallbackReturnProjection struct {
	TargetID        string `json:"target_id"`
	AgentIndex      int    `json:"agent_index"`
	HandleDirectory string `json:"handle_directory"`
	HandleParent    string `json:"handle_parent"`
	FleetExecutable string `json:"fleet_executable"`
}

type callbackReturnManifest struct {
	SchemaVersion   string                 `json:"schemaVersion"`
	ProfileIdentity string                 `json:"profileIdentity"`
	Host            string                 `json:"host"`
	Project         string                 `json:"project"`
	Repo            string                 `json:"repo"`
	Directory       string                 `json:"directory"`
	FleetExecutable string                 `json:"fleetExecutable"`
	Targets         []callbackReturnTarget `json:"targets"`
}
type callbackReturnTarget struct {
	ID              string `json:"id"`
	Agent           int    `json:"agent"`
	HandleDirectory string `json:"handleDirectory"`
}

func loadCallbackReturnProjection(p Plan) (*CallbackReturnProjection, error) {
	// This is intentionally a selected-GUI-target variant only. Ordinary plans
	// and all other targets retain their existing v3/v4 profiles even when the
	// host has no callback-return manifest.
	if p.GUITargetConfig == nil || (p.GUITargetConfig.TargetID != "darksteel-2" && p.GUITargetConfig.TargetID != "darksteel-3") {
		return nil, nil
	}
	data, err := os.ReadFile(callbackReturnManifestSource)
	if err != nil {
		return nil, fmt.Errorf("read callback-return manifest %s: %w", callbackReturnManifestSource, err)
	}
	if err := validateCallbackReturnManifestSource(callbackReturnManifestSource); err != nil {
		return nil, err
	}
	var manifest callbackReturnManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode callback-return manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON value")
		}
		return nil, fmt.Errorf("decode callback-return manifest: %w", err)
	}
	if manifest.SchemaVersion != CallbackReturnSchema || manifest.ProfileIdentity != CallbackReturnProfileIdentity || manifest.Host != callbackReturnExpectedHost || manifest.Project != callbackReturnExpectedProject || manifest.Repo != callbackReturnExpectedRepo || manifest.Directory != callbackReturnExpectedHandleDirectory {
		return nil, fmt.Errorf("callback-return manifest does not match the bounded CBR-01 profile")
	}
	if err := validateCallbackReturnStoreExecutable(manifest.FleetExecutable); err != nil {
		return nil, err
	}
	if err := validateCallbackReturnParent(manifest.Directory); err != nil {
		return nil, err
	}
	if len(manifest.Targets) != 2 {
		return nil, fmt.Errorf("callback-return manifest targets must contain exactly darksteel-2 and darksteel-3")
	}
	seen := map[string]bool{}
	var selected *callbackReturnTarget
	for i := range manifest.Targets {
		t := &manifest.Targets[i]
		if (t.ID != "darksteel-2" && t.ID != "darksteel-3") || t.Agent < 2 || t.Agent > 3 || t.ID != fmt.Sprintf("darksteel-%d", t.Agent) || t.HandleDirectory != filepath.Join(manifest.Directory, t.ID) || seen[t.ID] {
			return nil, fmt.Errorf("callback-return manifest target %d is invalid", i)
		}
		seen[t.ID] = true
		if t.ID == p.GUITargetConfig.TargetID {
			selected = t
		}
	}
	if !seen["darksteel-2"] || !seen["darksteel-3"] || selected == nil || selected.Agent != p.Agent.ID.Index {
		return nil, fmt.Errorf("callback-return manifest target does not match selected GUI target geometry")
	}
	return &CallbackReturnProjection{TargetID: selected.ID, AgentIndex: selected.Agent, HandleDirectory: selected.HandleDirectory, HandleParent: manifest.Directory, FleetExecutable: manifest.FleetExecutable}, nil
}

func validateCallbackReturnManifestSource(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve callback-return manifest: %w", err)
	}
	root := filepath.Clean(callbackReturnStoreRoot)
	if !pathWithinRoot(root, resolved) {
		return fmt.Errorf("callback-return manifest must resolve beneath %s", root)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("inspect callback-return manifest: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o222 != 0 {
		return fmt.Errorf("callback-return manifest must resolve to an immutable regular file")
	}
	return nil
}

func validateCallbackReturnStoreExecutable(path string) error {
	if filepath.Clean(path) != path || !filepath.IsAbs(path) || !strings.HasPrefix(path, filepath.Clean(callbackReturnStoreRoot)+string(filepath.Separator)) {
		return fmt.Errorf("callback-return fleet executable must be an exact Nix store path")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect callback-return fleet executable: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("callback-return fleet executable must be a regular executable Nix store file")
	}
	return nil
}

func validateCallbackReturnParent(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect callback-return stable parent: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("callback-return stable parent must be a non-symlink mode 0700 directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("callback-return stable parent owner does not match launching user")
	}
	return nil
}

package plan

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// These are package-owned paths, not CLI or environment options. Variables
// permit hermetic tests without replacing a host's installed configuration.
var ControllerServiceOwnerConfig = "/etc/ouroboros-controller-service-owner/owner.json"
var ControllerServiceOwnerStaticConfig = "/etc/static/ouroboros-controller-service-owner/owner.json"
var ControllerServiceOwnerSocket = "/run/ouroboros-controller-service-owner/control.sock"

func IsControllerServiceOwnerClient(project string, index int, repo, profile string) bool {
	return project == "dev-workspace" && index == 1 && repo == "shadow-throne-management" && profile == ManagementControllerProfileIdentity
}

// Resolve only the installed immutable Source role, preserving its symlink
// semantics for Client.sourceOwned's Nix-store real-path check.
func ResolveControllerServiceOwnerConfig() (string, error) {
	info, err := os.Lstat(ControllerServiceOwnerConfig)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", fmt.Errorf("controller service owner role must be an immutable Source symlink")
	}
	resolved, err := filepath.EvalSymlinks(ControllerServiceOwnerConfig)
	if err != nil {
		return "", err
	}
	expected, err := filepath.EvalSymlinks(ControllerServiceOwnerStaticConfig)
	if err != nil || resolved != expected || !strings.HasPrefix(resolved, filepath.Clean(ControllerProfileStoreRoot)+string(os.PathSeparator)) {
		return "", fmt.Errorf("controller service owner role differs from installed immutable Source projection")
	}
	f, err := os.Open(resolved)
	if err != nil {
		return "", err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > 4096 || stat.Mode().Perm()&0222 != 0 {
		return "", fmt.Errorf("controller service owner role is not a bounded immutable file")
	}
	var role struct {
		SchemaVersion int    `json:"schemaVersion"`
		PeerUser      string `json:"peerUser"`
		OwnerUser     string `json:"ownerUser"`
	}
	decoder := json.NewDecoder(io.LimitReader(f, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&role); err != nil {
		return "", fmt.Errorf("controller service owner role: %w", err)
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || role.SchemaVersion != 1 || role.PeerUser != "bayesartre" || role.OwnerUser != "root" {
		return "", fmt.Errorf("controller service owner role is not the fixed existing OS peer binding")
	}
	return resolved, nil
}

func controllerServiceOwnerClientBinds(project string, index int, repo string, profile *ManagementControllerProfile) ([]Bind, error) {
	if profile == nil || !IsControllerServiceOwnerClient(project, index, repo, profile.ProfileIdentity) {
		return nil, nil
	}
	if _, err := os.Lstat(ControllerServiceOwnerConfig); os.IsNotExist(err) {
		return nil, nil
	}
	if _, err := ResolveControllerServiceOwnerConfig(); err != nil {
		return nil, err
	}
	return []Bind{
		{Source: ControllerServiceOwnerSocket, Target: ControllerServiceOwnerSocket, Mode: "ro", Required: true},
		{Source: ControllerServiceOwnerConfig, Target: ControllerServiceOwnerConfig, Mode: "ro", Required: true},
	}, nil
}

package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Supplied only by the authoritative composition's linker. There is no
// caller-selected manifest, unit or service-control executable.
var packageOwnerManifest string

const StationOwnerService = "devkit-native-broker.service"

type LegacySocketWitness struct {
	Binary          string `json:"binary"`
	StateSHA256     string `json:"stateSHA256"`
	PIDSHA256       string `json:"pidSHA256"`
	SocketDevice    uint64 `json:"socketDevice"`
	SocketInode     uint64 `json:"socketInode"`
	SocketCTimeSec  int64  `json:"socketCTimeSec"`
	SocketCTimeNsec int64  `json:"socketCTimeNsec"`
}

type StationOwner struct {
	LegacyWitness *LegacySocketWitness `json:"legacyWitness,omitempty"`

	SchemaVersion string   `json:"schemaVersion"`
	Service       string   `json:"service"`
	Systemctl     string   `json:"systemctl"`
	HostRoot      string   `json:"hostRoot"`
	StateRoot     string   `json:"stateRoot"`
	Socket        string   `json:"socket"`
	Binary        string   `json:"binary"`
	Upstream      string   `json:"upstream"`
	AllowedImages []string `json:"allowedImages"`
	SocketAliases []string `json:"socketAliases"`
	AllowPulls    bool     `json:"allowPulls"`
	LogLevel      string   `json:"logLevel"`
}

func SelectConfig(c Config) (Config, error) { return selectedStationOwner(c) }

func selectedStationOwner(c Config) (Config, error) {
	if packageOwnerManifest == "" {
		return Normalize(c), nil
	}
	if !filepath.IsAbs(packageOwnerManifest) || !strings.HasPrefix(packageOwnerManifest, "/nix/store/") {
		return c, fmt.Errorf("station broker owner manifest is not an immutable absolute path")
	}
	data, err := os.ReadFile(packageOwnerManifest)
	if err != nil {
		return c, fmt.Errorf("read source-selected station broker owner: %w", err)
	}
	var owner StationOwner
	if err := json.Unmarshal(data, &owner); err != nil {
		return c, fmt.Errorf("decode source-selected station broker owner: %w", err)
	}
	return bindStationOwner(c, owner)
}

func bindStationOwner(c Config, owner StationOwner) (Config, error) {
	if owner.SchemaVersion != "devkit-native-broker-owner/v1" || owner.Service != StationOwnerService {
		return c, fmt.Errorf("station broker owner identity is unsupported")
	}
	for _, path := range []string{owner.Systemctl, owner.HostRoot, owner.StateRoot, owner.Socket, owner.Binary} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return c, fmt.Errorf("station broker owner contains a noncanonical absolute path")
		}
	}
	if !strings.HasPrefix(owner.Systemctl, "/nix/store/") || !strings.HasPrefix(owner.Binary, "/nix/store/") || filepath.Dir(owner.Socket) != owner.StateRoot {
		return c, fmt.Errorf("station broker executable or state geometry is not source selected")
	}
	// Project declarations must select this station endpoint. The complete
	// service policy is fixed by the composition and never by CLI overrides.
	requested := Normalize(c)
	if requested.Socket != owner.Socket || requested.Upstream != owner.Upstream ||
		requested.StateRoot != owner.StateRoot || requested.AllowPulls != owner.AllowPulls || requested.LogLevel != owner.LogLevel ||
		!sameStrings(requested.AllowedImages, owner.AllowedImages) || !sameStrings(requested.SocketBindAliases, owner.SocketAliases) {
		return c, fmt.Errorf("requested broker binding differs from source-selected station owner")
	}
	c = requested
	c.DevkitRoot = owner.HostRoot
	c.StateRoot, c.Socket, c.Binary, c.Upstream = owner.StateRoot, owner.Socket, owner.Binary, owner.Upstream
	c.AllowedImages = append([]string{}, owner.AllowedImages...)
	c.SocketBindAliases = append([]string{}, owner.SocketAliases...)
	c.AllowPulls, c.LogLevel = owner.AllowPulls, owner.LogLevel
	c.Owner = &owner
	return c, nil
}

func stationOwnerCgroup(cgroup string) bool {
	for _, line := range strings.Split(cgroup, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		for _, component := range strings.Split(parts[2], "/") {
			if component == StationOwnerService {
				return true
			}
		}
	}
	return false
}

func inStationOwner() bool {
	if strings.TrimSpace(os.Getenv("INVOCATION_ID")) == "" {
		return false
	}
	data, err := os.ReadFile("/proc/self/cgroup")
	return err == nil && stationOwnerCgroup(string(data))
}

// EnsureEndpointReady acquires only the linker-selected station prerequisite.
// The complete requested policy comes from the project source; no caller can
// select the fixed owner service or its authoritative policy.
func EnsureEndpointReady(ctx context.Context, requested Config, dryRun bool) (string, error) {
	if packageOwnerManifest == "" {
		return "", fmt.Errorf("source-selected station broker owner is required before native endpoint admission")
	}
	cfg, err := selectedStationOwner(requested)
	if err != nil {
		return "", err
	}
	_, err = EnsureReady(ctx, cfg, dryRun)
	return cfg.Binary, err
}

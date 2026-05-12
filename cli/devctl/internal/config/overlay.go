package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Hooks struct {
	Warm     string
	Maintain string
}

type Defaults struct {
	// Default number of agents to start (for reset/bootstrap flows)
	Agents int `yaml:"agents"`
	// Default repo name under the dev root (e.g., ouroboros-ide)
	Repo string `yaml:"repo"`
	// Base branch to track from origin (e.g., main)
	BaseBranch string `yaml:"base_branch"`
	// Prefix for per-agent branch names (e.g., agent -> agent1, agent2, ...)
	BranchPrefix string `yaml:"branch_prefix"`
	// Default compose profiles to apply (comma-separated)
	Profiles string `yaml:"profiles"`
	// Auto-run readiness (ssh+warm+validation) after lifecycle commands.
	AutoReady *bool `yaml:"auto_ready"`
	// Require warm hook for readiness (fail if missing).
	RequireWarm *bool `yaml:"require_warm"`
}

type Runtime struct {
	// Canonical marks this overlay as the one supported repo/container pairing
	// for Defaults.Repo. Legacy build shims can set this false.
	Canonical *bool `yaml:"canonical"`
	// Image is legacy Compose metadata; native runtime metadata should use Flake.
	Image string `yaml:"image"`
	// Flake is the native Nix runtime surface for this overlay.
	Flake string `yaml:"flake"`
	// CodexVersion is the expected `codex --version` semantic version.
	CodexVersion string `yaml:"codex_version"`
	// CoreCheck documents the command used to prove the mounted repo still builds.
	CoreCheck string `yaml:"core_check"`
	// Notes captures short operator context without overloading image names.
	Notes string `yaml:"notes"`
}

type Broker struct {
	Socket        string   `yaml:"socket"`
	Upstream      string   `yaml:"upstream"`
	AllowedImages []string `yaml:"allowed_images"`
	AllowPulls    *bool    `yaml:"allow_pulls"`
	LogLevel      string   `yaml:"log_level"`
}

type RepoCheck struct {
	Name    string `yaml:"name"`
	Command string `yaml:"command"`
}

type RuntimeCheck struct {
	Name    string `yaml:"name"`
	Command string `yaml:"command"`
}

type Readiness struct {
	RuntimeChecks []RuntimeCheck `yaml:"runtime_checks"`
	RepoChecks    []RepoCheck    `yaml:"repo_checks"`
}

type Native struct {
	WorktreeRoot          string `yaml:"worktree_root"`
	StateRoot             string `yaml:"state_root"`
	WorktreeContainerRoot string `yaml:"worktree_container_root"`
	StateContainerRoot    string `yaml:"state_container_root"`
}

type OverlayConfig struct {
	Workspace string            `yaml:"workspace"`
	Env       map[string]string `yaml:"env"`
	EnvFiles  []string          `yaml:"env_files"`
	Hooks     Hooks             `yaml:"hooks"`
	Defaults  Defaults          `yaml:"defaults"`
	Runtime   Runtime           `yaml:"runtime"`
	Broker    Broker            `yaml:"broker"`
	Readiness Readiness         `yaml:"readiness"`
	Native    Native            `yaml:"native"`
	// Default service name for this overlay (e.g., dev-agent, frontend)
	Service string `yaml:"service"`
	// Optional HTTPS ingress configuration for this overlay.
	Ingress *IngressConfig `yaml:"ingress"`
}

// IngressConfig describes an optional HTTPS reverse proxy service attached to an overlay.
type IngressConfig struct {
	Kind   string            `yaml:"kind"`
	Config string            `yaml:"config"`
	Routes []IngressRoute    `yaml:"routes"`
	Certs  []IngressCert     `yaml:"certs"`
	Hosts  []string          `yaml:"hosts"`
	Env    map[string]string `yaml:"env"`
}

// IngressRoute describes a single host → service:port mapping for generated configs.
type IngressRoute struct {
	Host    string `yaml:"host"`
	Path    string `yaml:"path"`
	Service string `yaml:"service"`
	Port    int    `yaml:"port"`
	Cert    string `yaml:"cert"`
	Key     string `yaml:"key"`
}

// IngressCert points at a certificate or key file to mount into the ingress container.
type IngressCert struct {
	Path string `yaml:"path"`
}

// ReadHooks parses overlays/<project>/devkit.yaml and returns warm/maintain hooks.
// It ignores other fields for backwards compatibility with existing callers.
func ReadHooks(overlays []string, project string) (Hooks, error) {
	cfg, _, _ := ReadAll(overlays, project)
	return cfg.Hooks, nil
}

// ReadAll parses overlays/<project>/devkit.yaml and returns the full overlay config.
func ReadAll(overlays []string, project string) (OverlayConfig, string, error) {
	var out OverlayConfig
	if strings.TrimSpace(project) == "" {
		return out, "", nil
	}
	for _, root := range overlays {
		candidate := filepath.Join(root, project, "devkit.yaml")
		data, err := os.ReadFile(candidate)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return out, "", err
		}
		if err := yaml.Unmarshal(data, &out); err != nil {
			return out, filepath.Dir(candidate), err
		}
		if out.Env == nil {
			out.Env = map[string]string{}
		}
		return out, filepath.Dir(candidate), nil
	}
	return out, "", nil
}

// HasRuntimeFlake reports whether an overlay declares a native Nix runtime.
func HasRuntimeFlake(cfg OverlayConfig) bool {
	return strings.TrimSpace(cfg.Runtime.Flake) != ""
}

// ResolveWorkspace returns the absolute workspace path for an overlay configuration.
// If no workspace is configured, it returns an empty string.
func ResolveWorkspace(cfg OverlayConfig, overlayDir string, root string) string {
	ws := strings.TrimSpace(cfg.Workspace)
	if ws == "" {
		return ""
	}
	resolved := ws
	if !filepath.IsAbs(ws) {
		base := strings.TrimSpace(overlayDir)
		if base == "" {
			base = root
		}
		resolved = filepath.Clean(filepath.Join(base, ws))
	}
	return resolved
}

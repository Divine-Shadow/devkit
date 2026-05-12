package plan

import (
	"path/filepath"
	"sort"
	"strings"

	"devkit/cli/devctl/internal/compose"
	"devkit/cli/devctl/internal/runtime/agent"
)

type Bind struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Mode     string `json:"mode"`
	Required bool   `json:"required"`
}

type ProxyConfig struct {
	HTTPProxy  string `json:"http_proxy"`
	HTTPSProxy string `json:"https_proxy"`
	NoProxy    string `json:"no_proxy"`
}

type DNSConfig struct {
	ResolvConf string `json:"resolv_conf"`
	ManagedBy  string `json:"managed_by"`
}

type ResourceLimits struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
	Pids   string `json:"pids,omitempty"`
}

type Plan struct {
	Agent               agent.Spec        `json:"agent"`
	DevkitHostRoot      string            `json:"devkit_host_root"`
	DevkitSandboxRoot   string            `json:"devkit_sandbox_root"`
	HostWorktreeRoot    string            `json:"host_worktree_root"`
	HostStateRoot       string            `json:"host_state_root"`
	SandboxWorktreeRoot string            `json:"sandbox_worktree_root"`
	SandboxStateRoot    string            `json:"sandbox_state_root"`
	Flake               string            `json:"flake"`
	Launcher            string            `json:"launcher"`
	LauncherArgs        []string          `json:"launcher_args"`
	Binds               []Bind            `json:"binds"`
	Env                 map[string]string `json:"env"`
	Proxy               ProxyConfig       `json:"proxy"`
	DNS                 DNSConfig         `json:"dns"`
	BrokerEndpoint      string            `json:"broker_endpoint"`
	DirectDockerSocket  bool              `json:"direct_docker_socket"`
	ResourceLimits      ResourceLimits    `json:"resource_limits"`
	Notes               []string          `json:"notes,omitempty"`
}

type BuildOptions struct {
	Paths                 compose.Paths
	Project               string
	Index                 int
	Repo                  string
	Flake                 string
	Launcher              string
	WorktreeRoot          string
	StateRoot             string
	WorktreeContainerRoot string
	StateContainerRoot    string
	BaseBranch            string
	BranchPrefix          string
	BrokerEndpoint        string
	Proxy                 string
	DNSResolvConf         string
	DedicatedWorktree     bool
}

func Build(opts BuildOptions) (Plan, error) {
	project := strings.TrimSpace(opts.Project)
	if project == "" {
		project = "dev-all"
	}
	index := opts.Index
	if index < 1 {
		index = 1
	}
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		repo = defaultRepo(project)
	}
	paths, err := agent.ResolvePaths(agent.PathConfig{
		DevkitRoot:            opts.Paths.Root,
		Project:               project,
		Repo:                  repo,
		Index:                 index,
		WorktreeRoot:          opts.WorktreeRoot,
		StateRoot:             opts.StateRoot,
		WorktreeContainerRoot: opts.WorktreeContainerRoot,
		StateContainerRoot:    opts.StateContainerRoot,
		DedicatedWorktree:     true,
	})
	if err != nil {
		return Plan{}, err
	}
	if sandboxWorktree, ok := sandboxPathForHostWorktree(paths); ok {
		paths.SandboxWorktree = sandboxWorktree
	}
	flake := strings.TrimSpace(opts.Flake)
	if flake == "" {
		flake = ".#" + project
	}
	launcher := strings.TrimSpace(opts.Launcher)
	if launcher == "" {
		launcher = "bubblewrap"
	}
	broker := strings.TrimSpace(opts.BrokerEndpoint)
	if broker == "" {
		broker = "/run/devkit/test-container-broker.sock"
	}
	proxyURL := strings.TrimSpace(opts.Proxy)
	resolvConf := strings.TrimSpace(opts.DNSResolvConf)
	if resolvConf == "" {
		resolvConf = filepath.Join(paths.HostAgentStateRoot, "resolv.conf")
	}

	env := map[string]string{
		"HOME":                         paths.SandboxHome,
		"CODEX_HOME":                   filepath.Join(paths.SandboxHome, ".codex"),
		"CODEX_ROLLOUT_DIR":            filepath.Join(paths.SandboxHome, ".codex", "rollouts"),
		"XDG_CACHE_HOME":               filepath.Join(paths.SandboxHome, ".cache"),
		"XDG_CONFIG_HOME":              filepath.Join(paths.SandboxHome, ".config"),
		"SBT_GLOBAL_BASE":              filepath.Join(paths.SandboxHome, ".sbt"),
		"NO_PROXY":                     "localhost,127.0.0.1",
		"DOCKER_HOST":                  "unix://" + broker,
		"TESTCONTAINERS_RYUK_DISABLED": "true",
		"DEVKIT_NATIVE_AGENT":          "1",
	}
	if proxyURL != "" {
		env["HTTP_PROXY"] = proxyURL
		env["HTTPS_PROXY"] = proxyURL
	}

	p := Plan{
		Agent: agent.Spec{
			ID: agent.ID{
				Project: project,
				Index:   index,
				Repo:    repo,
			},
			HostWorktree:     paths.HostWorktree,
			SandboxWorktree:  paths.SandboxWorktree,
			HostHome:         paths.HostHome,
			SandboxHome:      paths.SandboxHome,
			StateRoot:        paths.HostAgentStateRoot,
			SandboxStateRoot: paths.SandboxAgentStateRoot,
		},
		DevkitHostRoot:      opts.Paths.Root,
		DevkitSandboxRoot:   filepath.Join("/workspaces/dev", filepath.Base(opts.Paths.Root)),
		HostWorktreeRoot:    paths.HostWorktreeRoot,
		HostStateRoot:       paths.HostStateRoot,
		SandboxWorktreeRoot: paths.SandboxWorktreeRoot,
		SandboxStateRoot:    paths.SandboxStateRoot,
		Flake:               flake,
		Launcher:            launcher,
		Binds: []Bind{
			{Source: paths.DevRoot, Target: "/workspaces/dev", Mode: "rw", Required: true},
			{Source: paths.DevRoot, Target: paths.DevRoot, Mode: "rw", Required: true},
			{Source: paths.HostWorktree, Target: "/workspace", Mode: "rw", Required: true},
			{Source: paths.HostWorktreeRoot, Target: paths.SandboxWorktreeRoot, Mode: "rw", Required: false},
			{Source: paths.HostStateRoot, Target: paths.SandboxStateRoot, Mode: "rw", Required: true},
			{Source: "/nix/store", Target: "/nix/store", Mode: "ro", Required: true},
			{Source: broker, Target: broker, Mode: "rw", Required: false},
			{Source: resolvConf, Target: "/etc/resolv.conf", Mode: "ro", Required: false},
		},
		Env: env,
		Proxy: ProxyConfig{
			HTTPProxy:  proxyURL,
			HTTPSProxy: proxyURL,
			NoProxy:    env["NO_PROXY"],
		},
		DNS: DNSConfig{
			ResolvConf: resolvConf,
			ManagedBy:  "devkit-host-service",
		},
		BrokerEndpoint:     broker,
		DirectDockerSocket: false,
		ResourceLimits: ResourceLimits{
			CPU:    "2",
			Memory: "4G",
			Pids:   "1024",
		},
		Notes: []string{
			"plan only: no sandbox is launched by this command",
			"standard agents must use brokered OCI access only",
		},
	}
	p.LauncherArgs = launcherArgs(p)
	return p, nil
}

func BuildDevAll(opts BuildOptions) (Plan, error) {
	return Build(opts)
}

func defaultRepo(project string) string {
	if strings.TrimSpace(project) == "dev-all" {
		return "ouroboros-ide"
	}
	if strings.TrimSpace(project) != "" {
		return strings.TrimSpace(project)
	}
	return "ouroboros-ide"
}

func sandboxPathForHostWorktree(paths agent.Paths) (string, bool) {
	hostWorktree := filepath.Clean(paths.HostWorktree)
	resolved, err := filepath.EvalSymlinks(hostWorktree)
	if err != nil {
		return "", false
	}
	resolved = filepath.Clean(resolved)
	if resolved == hostWorktree {
		return "", false
	}
	if sandbox, ok := projectSandboxPath(resolved, paths.DevRoot, "/workspaces/dev"); ok {
		return sandbox, true
	}
	if sandbox, ok := projectSandboxPath(resolved, paths.HostWorktreeRoot, paths.SandboxWorktreeRoot); ok {
		return sandbox, true
	}
	return "", false
}

func projectSandboxPath(hostPath, hostRoot, sandboxRoot string) (string, bool) {
	hostRoot = filepath.Clean(hostRoot)
	rel, err := filepath.Rel(hostRoot, hostPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", false
	}
	return filepath.Join(sandboxRoot, rel), true
}

func launcherArgs(p Plan) []string {
	switch p.Launcher {
	case "systemd-run":
		return []string{
			"systemd-run",
			"--user",
			"--scope",
			"--unit", p.Agent.ID.Name(),
			"nix", "--extra-experimental-features", "nix-command flakes", "develop", p.Flake, "--command", "bash", "-lc",
			"cd " + shellQuote(p.Agent.SandboxWorktree) + " && exec ${SHELL:-bash}",
		}
	default:
		args := []string{"bwrap"}
		for _, bind := range p.Binds {
			switch bind.Mode {
			case "ro":
				args = append(args, "--ro-bind", bind.Source, bind.Target)
			default:
				args = append(args, "--bind", bind.Source, bind.Target)
			}
		}
		args = append(args,
			"--dir", "/usr",
			"--dir", "/usr/bin",
			"--dir", "/bin",
			"--symlink", "/run/current-system/sw/bin/env", "/usr/bin/env",
			"--symlink", "/run/current-system/sw/bin/bash", "/bin/bash",
			"--symlink", "/run/current-system/sw/bin/sh", "/bin/sh",
		)
		keys := make([]string, 0, len(p.Env))
		for key := range p.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			args = append(args, "--setenv", key, p.Env[key])
		}
		args = append(args, "nix", "--extra-experimental-features", "nix-command flakes", "develop", p.Flake, "--command", "bash", "-lc", "cd "+shellQuote(p.Agent.SandboxWorktree)+" && exec ${SHELL:-bash}")
		return args
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

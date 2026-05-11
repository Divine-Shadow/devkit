package plan

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"devkit/cli/devctl/internal/compose"
	pth "devkit/cli/devctl/internal/paths"
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
	Agent              agent.Spec        `json:"agent"`
	DevkitHostRoot     string            `json:"devkit_host_root"`
	DevkitSandboxRoot  string            `json:"devkit_sandbox_root"`
	Flake              string            `json:"flake"`
	Launcher           string            `json:"launcher"`
	LauncherArgs       []string          `json:"launcher_args"`
	Binds              []Bind            `json:"binds"`
	Env                map[string]string `json:"env"`
	Proxy              ProxyConfig       `json:"proxy"`
	DNS                DNSConfig         `json:"dns"`
	BrokerEndpoint     string            `json:"broker_endpoint"`
	DirectDockerSocket bool              `json:"direct_docker_socket"`
	ResourceLimits     ResourceLimits    `json:"resource_limits"`
	Notes              []string          `json:"notes,omitempty"`
}

type BuildOptions struct {
	Paths             compose.Paths
	Project           string
	Index             int
	Repo              string
	Flake             string
	Launcher          string
	WorktreeRoot      string
	StateRoot         string
	BrokerEndpoint    string
	Proxy             string
	DNSResolvConf     string
	DedicatedWorktree bool
}

func BuildDevAll(opts BuildOptions) (Plan, error) {
	project := strings.TrimSpace(opts.Project)
	if project == "" {
		project = "dev-all"
	}
	if project != "dev-all" {
		return Plan{}, fmt.Errorf("native plan currently supports dev-all only, got %s", project)
	}
	index := opts.Index
	if index < 1 {
		index = 1
	}
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		repo = "ouroboros-ide"
	}
	devRoot := filepath.Clean(filepath.Join(opts.Paths.Root, ".."))
	worktreeRoot := strings.TrimSpace(opts.WorktreeRoot)
	if worktreeRoot == "" {
		worktreeRoot = filepath.Join(devRoot, pth.AgentWorktreesDir)
	}
	hostWorktree := filepath.Join(devRoot, repo)
	if index > 1 || opts.DedicatedWorktree {
		hostWorktree = filepath.Join(worktreeRoot, fmt.Sprintf("agent%d", index), repo)
	}
	hostHome := filepath.Join(hostWorktree, fmt.Sprintf(".devhome-agent%d", index))
	if index > 1 || opts.DedicatedWorktree {
		hostHome = filepath.Join(worktreeRoot, fmt.Sprintf("agent%d", index), fmt.Sprintf(".devhome-agent%d", index))
	}

	sandboxWorktree := pth.AgentRepoPath(project, fmt.Sprintf("%d", index), repo)
	sandboxHome := pth.AgentHomePath(project, fmt.Sprintf("%d", index), repo)
	if opts.DedicatedWorktree {
		agentDir := fmt.Sprintf("agent%d", index)
		sandboxWorktree = filepath.Join("/workspaces/dev", pth.AgentWorktreesDir, agentDir, repo)
		sandboxHome = filepath.Join("/workspaces/dev", pth.AgentWorktreesDir, agentDir, fmt.Sprintf(".devhome-agent%d", index))
	}
	stateRoot := strings.TrimSpace(opts.StateRoot)
	if stateRoot == "" {
		stateRoot = filepath.Join(devRoot, ".devkit", "native-agents", fmt.Sprintf("%s-agent%d", project, index))
	}
	flake := strings.TrimSpace(opts.Flake)
	if flake == "" {
		flake = ".#dev-all"
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
	if proxyURL == "" {
		proxyURL = "http://127.0.0.1:8888"
	}
	resolvConf := strings.TrimSpace(opts.DNSResolvConf)
	if resolvConf == "" {
		resolvConf = filepath.Join(stateRoot, "resolv.conf")
	}

	env := map[string]string{
		"HOME":                         sandboxHome,
		"CODEX_HOME":                   filepath.Join(sandboxHome, ".codex"),
		"CODEX_ROLLOUT_DIR":            filepath.Join(sandboxHome, ".codex", "rollouts"),
		"XDG_CACHE_HOME":               filepath.Join(sandboxHome, ".cache"),
		"XDG_CONFIG_HOME":              filepath.Join(sandboxHome, ".config"),
		"SBT_GLOBAL_BASE":              filepath.Join(sandboxHome, ".sbt"),
		"HTTP_PROXY":                   proxyURL,
		"HTTPS_PROXY":                  proxyURL,
		"NO_PROXY":                     "localhost,127.0.0.1",
		"DOCKER_HOST":                  "unix://" + broker,
		"TESTCONTAINERS_RYUK_DISABLED": "true",
		"DEVKIT_NATIVE_AGENT":          "1",
	}

	p := Plan{
		Agent: agent.Spec{
			ID: agent.ID{
				Project: project,
				Index:   index,
				Repo:    repo,
			},
			HostWorktree:    hostWorktree,
			SandboxWorktree: sandboxWorktree,
			HostHome:        hostHome,
			SandboxHome:     sandboxHome,
			StateRoot:       stateRoot,
		},
		DevkitHostRoot:    opts.Paths.Root,
		DevkitSandboxRoot: filepath.Join("/workspaces/dev", filepath.Base(opts.Paths.Root)),
		Flake:             flake,
		Launcher:          launcher,
		Binds: []Bind{
			{Source: devRoot, Target: "/workspaces/dev", Mode: "rw", Required: true},
			{Source: worktreeRoot, Target: "/worktrees", Mode: "rw", Required: false},
			{Source: hostHome, Target: sandboxHome, Mode: "rw", Required: true},
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

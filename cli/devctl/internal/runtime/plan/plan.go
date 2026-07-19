package plan

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"devkit/cli/devctl/internal/devkitpaths"
	"devkit/cli/devctl/internal/runtime/agent"
)

type Bind struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Mode     string `json:"mode"`
	Required bool   `json:"required"`
}

type ProxyConfig struct {
	HTTPProxy     string `json:"http_proxy"`
	HTTPSProxy    string `json:"https_proxy"`
	NoProxy       string `json:"no_proxy"`
	UnixSocket    string `json:"unix_socket,omitempty"`
	AllowlistPath string `json:"allowlist_path,omitempty"`
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
	Agent                agent.Spec        `json:"agent"`
	DevkitHostRoot       string            `json:"devkit_host_root"`
	RuntimeAuthorityRoot string            `json:"runtime_authority_root"`
	DevkitSandboxRoot    string            `json:"devkit_sandbox_root"`
	HostWorktreeRoot     string            `json:"host_worktree_root"`
	HostStateRoot        string            `json:"host_state_root"`
	SandboxWorktreeRoot  string            `json:"sandbox_worktree_root"`
	SandboxStateRoot     string            `json:"sandbox_state_root"`
	Flake                string            `json:"flake"`
	FlakeInputOverrides  map[string]string `json:"flake_input_overrides,omitempty"`
	Launcher             string            `json:"launcher"`
	LauncherArgs         []string          `json:"launcher_args"`
	IsolationProfile     string            `json:"isolation_profile,omitempty"`
	MountPolicyIdentity  string            `json:"mount_policy_identity"`
	WindowsMountsVisible bool              `json:"windows_mounts_visible"`
	Binds                []Bind            `json:"binds"`
	Env                  map[string]string `json:"env"`
	Proxy                ProxyConfig       `json:"proxy"`
	DNS                  DNSConfig         `json:"dns"`
	BrokerEndpoint       string            `json:"broker_endpoint"`
	DirectDockerSocket   bool              `json:"direct_docker_socket"`
	ResourceLimits       ResourceLimits    `json:"resource_limits"`
	Notes                []string          `json:"notes,omitempty"`
}

type BuildOptions struct {
	Paths                 devkitpaths.Paths
	Project               string
	Index                 int
	Repo                  string
	Flake                 string
	FlakeInputOverrides   map[string]string
	Launcher              string
	WorktreeRoot          string
	StateRoot             string
	WorktreeContainerRoot string
	StateContainerRoot    string
	BaseBranch            string
	BranchPrefix          string
	BrokerEndpoint        string
	Proxy                 string
	ProxySocket           string
	IsolationProfile      string
	EgressAllowlist       string
	DNSResolvConf         string
	DedicatedWorktree     bool
}

const (
	IsolationProfileWorkspaceEgress = "workspace-egress"
	workspaceEgressNSCDSocket       = "/var/run/nscd/socket"
)

var workspaceEgressNSCDSource = workspaceEgressNSCDSocket

func resolveRuntimeAuthorityFlake(flake, runtimeAuthorityRoot string) string {
	const rootedPrefix = "path:.?"
	if runtimeAuthorityRoot == "" || !strings.HasPrefix(flake, rootedPrefix) {
		return flake
	}
	return "path:" + runtimeAuthorityRoot + "?" + strings.TrimPrefix(flake, rootedPrefix)
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
		if project == "dev-all" && !agent.IsWorkspaceRootRepo(repo) {
			suffix := fmt.Sprintf(".devhome-agent%d", index)
			if index == 1 {
				paths.SandboxHome = filepath.Join(paths.SandboxWorktree, suffix)
				if resolved, err := filepath.EvalSymlinks(paths.HostWorktree); err == nil {
					paths.HostHome = filepath.Join(filepath.Clean(resolved), suffix)
				}
			} else {
				paths.SandboxHome = filepath.Join(filepath.Dir(paths.SandboxWorktree), suffix)
			}
		}
	}
	flake := strings.TrimSpace(opts.Flake)
	if flake == "" {
		flake = ".#" + project
	}
	launcher := strings.TrimSpace(opts.Launcher)
	if launcher == "" {
		launcher = "bubblewrap"
	}
	runtimeAuthorityRoot := strings.TrimSpace(opts.Paths.RuntimeAuthorityRoot)
	if runtimeAuthorityRoot != "" {
		runtimeAuthorityRoot = filepath.Clean(runtimeAuthorityRoot)
	}
	flake = resolveRuntimeAuthorityFlake(flake, runtimeAuthorityRoot)
	broker := strings.TrimSpace(opts.BrokerEndpoint)
	if broker == "" {
		broker = "/run/devkit/test-container-broker.sock"
	}
	dockerHost := "unix://" + broker
	dockerAPIVersion := "1.52"
	javaOptions := []string{
		"-Dapi.version=" + dockerAPIVersion,
		"-Ddocker.api.version=" + dockerAPIVersion,
		"-Ddocker.host=" + dockerHost,
	}
	proxyURL := strings.TrimSpace(opts.Proxy)
	proxySocket := strings.TrimSpace(opts.ProxySocket)
	isolationProfile := normalizeIsolationProfile(opts.IsolationProfile)
	if isolationProfile != "" && isolationProfile != IsolationProfileWorkspaceEgress {
		return Plan{}, fmt.Errorf("unknown isolation profile %q", opts.IsolationProfile)
	}
	egressAllowlist := strings.TrimSpace(opts.EgressAllowlist)
	if isolationProfile == "" && egressAllowlist != "" {
		return Plan{}, fmt.Errorf("egress allowlist requires an isolation profile")
	}
	if isolationProfile == IsolationProfileWorkspaceEgress {
		if egressAllowlist == "" {
			return Plan{}, fmt.Errorf("workspace-egress isolation requires an egress allowlist")
		}
		if proxySocket == "" {
			agentName := agent.ID{Project: project, Index: index, Repo: repo}.Name()
			proxySocket = filepath.Join(paths.DevRoot, ".devkit", "native-egress", agentName+"-"+IsolationProfileWorkspaceEgress+".sock")
		}
	}
	if proxySocket != "" && proxyURL == "" {
		proxyURL = "http://127.0.0.1:18888"
	}
	if proxyURL != "" {
		javaOptions = append(javaOptions, javaProxyOptions(proxyURL)...)
	}
	javaToolOptions := strings.Join(javaOptions, " ")
	resolvConf := strings.TrimSpace(opts.DNSResolvConf)
	if resolvConf == "" {
		resolvConf = filepath.Join(paths.HostAgentStateRoot, "resolv.conf")
	}

	sharedCacheRoot := filepath.Join("/workspaces", "dev", ".cache", "shared")
	sbtGlobalBase := filepath.Join(paths.SandboxHome, ".sbt")
	sbtBootDir := filepath.Join(sbtGlobalBase, "boot")
	sbtIvyHome := filepath.Join(sharedCacheRoot, "ivy2")
	coursierCache := filepath.Join(sharedCacheRoot, "coursier")
	if isolationProfile == IsolationProfileWorkspaceEgress {
		sbtIvyHome = filepath.Join(paths.SandboxHome, ".cache", "ivy2")
		coursierCache = filepath.Join(paths.SandboxHome, ".cache", "coursier")
	}
	sbtOpts := strings.Join([]string{
		"-Xmx6G",
		"-XX:+UseG1GC",
		"-Djava.awt.headless=true",
		"-Dsbt.global.base=" + sbtGlobalBase,
		"-Dsbt.boot.directory=" + sbtBootDir,
		"-Dsbt.ivy.home=" + sbtIvyHome,
		"-Dsbt.coursier.home=" + coursierCache,
	}, " ")
	sbtControlPlaneServerSystemProperties := strings.Join([]string{
		"-Dsbt.global.base=" + sbtGlobalBase,
		"-Dsbt.boot.directory=" + sbtBootDir,
		"-Dsbt.ivy.home=" + sbtIvyHome,
	}, "\n")

	env := map[string]string{
		"HOME":                         paths.SandboxHome,
		"CODEX_HOME":                   filepath.Join(paths.SandboxHome, ".codex"),
		"CODEX_ROLLOUT_DIR":            filepath.Join(paths.SandboxHome, ".codex", "rollouts"),
		"XDG_CACHE_HOME":               filepath.Join(paths.SandboxHome, ".cache"),
		"XDG_CONFIG_HOME":              filepath.Join(paths.SandboxHome, ".config"),
		"SBT_GLOBAL_BASE":              sbtGlobalBase,
		"SBT_BOOT_DIR":                 sbtBootDir,
		"SBT_IVY_HOME":                 sbtIvyHome,
		"COURSIER_CACHE":               coursierCache,
		"SBT_OPTS":                     sbtOpts,
		"OURO_NIX_SANDBOX":             "1",
		"TMPDIR":                       "/tmp",
		"NO_PROXY":                     "localhost,127.0.0.1",
		"DOCKER_HOST":                  dockerHost,
		"DOCKER_API_VERSION":           dockerAPIVersion,
		"JAVA_TOOL_OPTIONS":            javaToolOptions,
		"TESTCONTAINERS_RYUK_DISABLED": "true",
		"DEVKIT_NATIVE_AGENT":          fmt.Sprintf("%d", index),
		"AWS_CONFIG_FILE":              filepath.Join(paths.SandboxHome, ".aws", "config"),
		"AWS_SHARED_CREDENTIALS_FILE":  filepath.Join(paths.SandboxHome, ".aws", "credentials"),
		"AWS_SDK_LOAD_CONFIG":          "1",
	}
	env["SBT_CONTROL_PLANE_SERVER_SYSTEM_PROPERTIES"] = sbtControlPlaneServerSystemProperties
	if proxyURL != "" {
		env["HTTP_PROXY"] = proxyURL
		env["HTTPS_PROXY"] = proxyURL
		env["http_proxy"] = proxyURL
		env["https_proxy"] = proxyURL
		env["no_proxy"] = env["NO_PROXY"]
	}
	if isolationProfile != "" {
		env["DEVKIT_NATIVE_ISOLATION_PROFILE"] = isolationProfile
	}

	binds := []Bind{
		{Source: paths.DevRoot, Target: "/workspaces/dev", Mode: "rw", Required: true},
		{Source: paths.DevRoot, Target: paths.DevRoot, Mode: "rw", Required: true},
		{Source: paths.HostWorktree, Target: "/workspace", Mode: "rw", Required: true},
		{Source: paths.HostWorktreeRoot, Target: paths.SandboxWorktreeRoot, Mode: "rw", Required: false},
		{Source: paths.HostStateRoot, Target: paths.SandboxStateRoot, Mode: "rw", Required: true},
		{Source: "/nix/store", Target: "/nix/store", Mode: "ro", Required: true},
		{Source: broker, Target: broker, Mode: "rw", Required: false},
		{Source: resolvConf, Target: "/etc/resolv.conf", Mode: "ro", Required: false},
	}
	notes := []string{
		"plan only: no sandbox is launched by this command",
		"standard agents must use brokered OCI access only",
	}
	if isolationProfile == IsolationProfileWorkspaceEgress {
		binds = workspaceEgressBinds(paths, project, repo, opts.Paths.Root, broker, resolvConf)
		if err := validateWorkspaceEgressMountPolicy(binds, egressAllowlist); err != nil {
			return Plan{}, err
		}
		notes = append(notes,
			"isolation profile workspace-egress: network is proxy-only through the configured egress allowlist",
			"isolation profile workspace-egress: filesystem binds are limited to the worktree, per-agent home, exact Git metadata, runtime support, and capability sockets",
		)
	}
	mountPolicyIdentity := "devkit/native-broad/v1"
	windowsMountsVisible := true
	if isolationProfile == IsolationProfileWorkspaceEgress {
		mountPolicyIdentity = "devkit/workspace-egress/v3"
		windowsMountsVisible = false
		env["DEVKIT_NATIVE_MOUNT_POLICY_IDENTITY"] = mountPolicyIdentity
		env["DEVKIT_NATIVE_WINDOWS_MOUNTS_VISIBLE"] = "false"
	}

	flakeInputOverrides, err := normalizeFlakeInputOverridesForSandbox(
		opts.FlakeInputOverrides,
		isolationProfile,
		paths,
		repo,
	)
	if err != nil {
		return Plan{}, err
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
		DevkitHostRoot:       opts.Paths.Root,
		RuntimeAuthorityRoot: runtimeAuthorityRoot,
		DevkitSandboxRoot:    filepath.Join("/workspaces/dev", filepath.Base(opts.Paths.Root)),
		HostWorktreeRoot:     paths.HostWorktreeRoot,
		HostStateRoot:        paths.HostStateRoot,
		SandboxWorktreeRoot:  paths.SandboxWorktreeRoot,
		SandboxStateRoot:     paths.SandboxStateRoot,
		Flake:                flake,
		FlakeInputOverrides:  flakeInputOverrides,
		Launcher:             launcher,
		IsolationProfile:     isolationProfile,
		MountPolicyIdentity:  mountPolicyIdentity,
		WindowsMountsVisible: windowsMountsVisible,
		Binds:                binds,
		Env:                  env,
		Proxy: ProxyConfig{
			HTTPProxy:     proxyURL,
			HTTPSProxy:    proxyURL,
			NoProxy:       env["NO_PROXY"],
			UnixSocket:    proxySocket,
			AllowlistPath: egressAllowlist,
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
		Notes: notes,
	}
	if proxySocket != "" {
		p.Binds = append(p.Binds, Bind{Source: proxySocket, Target: proxySocket, Mode: "rw", Required: true})
	}
	p.LauncherArgs = launcherArgs(p)
	return p, nil
}

func normalizeIsolationProfile(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "none", "default":
		return ""
	case "workspace-egress", "workspace_egress":
		return IsolationProfileWorkspaceEgress
	default:
		return strings.TrimSpace(value)
	}
}

func javaProxyOptions(proxyURL string) []string {
	u, err := url.Parse(proxyURL)
	if err != nil || strings.TrimSpace(u.Hostname()) == "" {
		return nil
	}
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}
	host := u.Hostname()
	return []string{
		"-Dhttp.proxyHost=" + host,
		"-Dhttp.proxyPort=" + port,
		"-Dhttps.proxyHost=" + host,
		"-Dhttps.proxyPort=" + port,
	}
}

func workspaceEgressBinds(paths agent.Paths, project, repo, devkitRoot string, broker string, resolvConf string) []Bind {
	binds := []Bind{}
	add := func(source, target, mode string, required bool) {
		source = filepath.Clean(strings.TrimSpace(source))
		target = filepath.Clean(strings.TrimSpace(target))
		if source == "" || source == "." || target == "" || target == "." {
			return
		}
		for _, bind := range binds {
			if bind.Source == source && bind.Target == target {
				return
			}
		}
		binds = append(binds, Bind{Source: source, Target: target, Mode: mode, Required: required})
	}
	add(paths.HostWorktree, "/workspace", "rw", true)
	add(paths.HostWorktree, paths.SandboxWorktree, "rw", true)
	add(paths.HostHome, paths.SandboxHome, "rw", true)
	add(devkitRoot, filepath.Join("/workspaces/dev", filepath.Base(devkitRoot)), "ro", true)
	if isGovernedRuntimePlan(project, repo) {
		// launch.Prepare materializes the immutable runtime identity projection
		// and governance catalog beneath the controller dev root before every
		// governed sandbox invocation. Readiness and the governed launcher
		// consume those exact files; they must not depend on a broad
		// /workspaces/dev bind or regenerate a second identity in the consumer.
		hostRuntimeSupportRoot := filepath.Join(paths.DevRoot, ".devkit")
		sandboxRuntimeSupportRoot := filepath.Join("/workspaces/dev", ".devkit")
		add(
			filepath.Join(hostRuntimeSupportRoot, "ouro8-governance-env.sh"),
			filepath.Join(sandboxRuntimeSupportRoot, "ouro8-governance-env.sh"),
			"ro",
			true,
		)
		add(
			filepath.Join(hostRuntimeSupportRoot, "ouro8-governance-repo-env.json"),
			filepath.Join(sandboxRuntimeSupportRoot, "ouro8-governance-repo-env.json"),
			"ro",
			true,
		)
		add(
			filepath.Join(hostRuntimeSupportRoot, "governance-control-plane"),
			filepath.Join(sandboxRuntimeSupportRoot, "governance-control-plane"),
			"rw",
			true,
		)
	}
	for _, bind := range gitMetadataBinds(paths.HostWorktree, paths.SandboxWorktree) {
		add(bind.Source, bind.Target, bind.Mode, bind.Required)
	}
	add("/nix/store", "/nix/store", "ro", true)
	// The isolated network namespace cannot reach the host loopback resolver.
	// nsncd exposes DNS through this narrowly scoped Unix capability; network
	// egress remains restricted to the managed proxy.
	add(workspaceEgressNSCDSource, workspaceEgressNSCDSocket, "ro", true)
	add(broker, broker, "rw", false)
	add(resolvConf, "/etc/resolv.conf", "ro", false)
	return binds
}

func isGovernedRuntimePlan(project, repo string) bool {
	switch strings.TrimSpace(repo) {
	case "ouroboros-ide", "ouroboros-terraform":
		return true
	}
	switch strings.TrimSpace(project) {
	case "dev-workspace", "ouro-integration", "ouroboros-terraform":
		return true
	default:
		return false
	}
}

func validateWorkspaceEgressMountPolicy(binds []Bind, egressAllowlist string) error {
	if isWindowsFilesystemPath(egressAllowlist) {
		return fmt.Errorf("workspace-egress isolation rejects Windows-mounted egress allowlist: %s", egressAllowlist)
	}
	for _, bind := range binds {
		if isWindowsFilesystemPath(bind.Source) || isWindowsFilesystemPath(bind.Target) {
			return fmt.Errorf("workspace-egress isolation rejects Windows-mounted bind %s -> %s", bind.Source, bind.Target)
		}
	}
	return nil
}

func isWindowsFilesystemPath(value string) bool {
	cleaned := strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	lower := strings.ToLower(cleaned)
	if len(lower) >= 3 &&
		lower[0] >= 'a' && lower[0] <= 'z' &&
		lower[1] == ':' && lower[2] == '/' {
		return true
	}
	if !strings.HasPrefix(lower, "/mnt/") || len(lower) < len("/mnt/x") {
		return false
	}
	drive := lower[len("/mnt/")]
	if drive < 'a' || drive > 'z' {
		return false
	}
	return len(lower) == len("/mnt/x") || lower[len("/mnt/x")] == '/'
}

func gitMetadataBinds(hostWorktree, sandboxWorktree string) []Bind {
	gitPath := filepath.Join(hostWorktree, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return nil
	}
	if info.IsDir() {
		return nil
	}
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return nil
	}
	gitdirValue := parseGitdirValue(string(data))
	if gitdirValue == "" {
		return nil
	}
	gitDir := resolveGitdirValue(gitdirValue, hostWorktree)
	sandboxGitDir := resolveGitdirValue(gitdirValue, sandboxWorktree)
	commondirValue := readCommondirValue(gitDir)
	commonDir := resolveCommondirValue(commondirValue, gitDir)
	if !filepath.IsAbs(gitdirValue) {
		if commonDir != "" {
			// Relative linked-worktree metadata is intentionally portable
			// across the host dev root and its /workspaces/dev projection.
			// Mount the common repository only at the sandbox-resolved target;
			// no host-path alias is part of the normal contract.
			sandboxCommonDir := resolveCommondirValue(commondirValue, sandboxGitDir)
			if sandboxCommonDir != "" {
				return []Bind{{Source: commonDir, Target: sandboxCommonDir, Mode: "rw", Required: true}}
			}
		}
		return []Bind{{Source: gitDir, Target: sandboxGitDir, Mode: "rw", Required: true}}
	}

	// A linked source can keep common metadata outside the canonical dev root.
	// Such a worktree retains absolute metadata, so materialize only its exact
	// worktree and metadata paths; never expose their parents or siblings.
	binds := []Bind{{Source: hostWorktree, Target: hostWorktree, Mode: "rw", Required: true}}
	if commonDir != "" {
		return append(binds, Bind{Source: commonDir, Target: commonDir, Mode: "rw", Required: true})
	}
	return append(binds, Bind{Source: gitDir, Target: gitDir, Mode: "rw", Required: true})
}

func parseGitdirValue(data string) string {
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "gitdir:") {
			continue
		}
		value := strings.TrimSpace(line[len("gitdir:"):])
		if value == "" {
			return ""
		}
		return filepath.Clean(value)
	}
	return ""
}

func resolveGitdirValue(value, worktree string) string {
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(worktree, value))
}

func readCommondirValue(gitDir string) string {
	data, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return ""
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return ""
	}
	return filepath.Clean(value)
}

func resolveCommondirValue(value, gitDir string) string {
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(gitDir, value))
}

func BuildDevAll(opts BuildOptions) (Plan, error) {
	return Build(opts)
}

func normalizeFlakeInputOverrides(overrides map[string]string) map[string]string {
	if len(overrides) == 0 {
		return nil
	}
	out := map[string]string{}
	for name, value := range overrides {
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || value == "" {
			continue
		}
		out[name] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeFlakeInputOverridesForSandbox(overrides map[string]string, isolationProfile string, paths agent.Paths, repo string) (map[string]string, error) {
	normalized := normalizeFlakeInputOverrides(overrides)
	if isolationProfile != IsolationProfileWorkspaceEgress || len(normalized) == 0 {
		return normalized, nil
	}

	canonicalRepo := filepath.Clean(filepath.Join(paths.DevRoot, repo))
	hostWorktree := filepath.Clean(paths.HostWorktree)
	for name, value := range normalized {
		if !strings.HasPrefix(value, "path:") {
			continue
		}
		hostPath := filepath.Clean(strings.TrimPrefix(value, "path:"))
		if isWindowsFilesystemPath(hostPath) {
			return nil, fmt.Errorf("workspace-egress isolation rejects Windows-mounted flake input %s=%s", name, value)
		}
		switch hostPath {
		case canonicalRepo, hostWorktree:
			normalized[name] = "path:" + filepath.Clean(paths.SandboxWorktree)
		default:
			return nil, fmt.Errorf("workspace-egress isolation rejects unbound flake input %s=%s", name, value)
		}
	}
	return normalized, nil
}

func defaultRepo(project string) string {
	switch strings.TrimSpace(project) {
	case "dev-workspace":
		return "."
	case "dev-all":
		return "ouroboros-ide"
	}
	if strings.TrimSpace(project) != "" {
		return strings.TrimSpace(project)
	}
	return "ouroboros-ide"
}

func sandboxPathForHostWorktree(paths agent.Paths) (string, bool) {
	hostWorktree := filepath.Clean(paths.HostWorktree)
	resolved := ""
	if resolvedPath, err := filepath.EvalSymlinks(hostWorktree); err == nil {
		resolved = filepath.Clean(resolvedPath)
		if sandbox, ok := projectSandboxPath(resolved, paths.DevRoot, "/workspaces/dev"); ok {
			return sandbox, true
		}
	}
	if sandbox, ok := projectSandboxPath(hostWorktree, paths.DevRoot, "/workspaces/dev"); ok {
		return sandbox, true
	}
	if resolved != "" {
		if sandbox, ok := projectSandboxPath(resolved, paths.HostWorktreeRoot, paths.SandboxWorktreeRoot); ok {
			return sandbox, true
		}
	}
	if sandbox, ok := projectSandboxPath(hostWorktree, paths.HostWorktreeRoot, paths.SandboxWorktreeRoot); ok {
		return sandbox, true
	}
	return "", false
}

func projectSandboxPath(hostPath, hostRoot, sandboxRoot string) (string, bool) {
	hostRoot = filepath.Clean(hostRoot)
	rel, err := filepath.Rel(hostRoot, hostPath)
	if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", false
	}
	if rel == "." {
		return filepath.Clean(sandboxRoot), true
	}
	return filepath.Join(sandboxRoot, rel), true
}

func launcherArgs(p Plan) []string {
	switch p.Launcher {
	case "systemd-run":
		nixArgs := nixDevelopArgs(p)
		args := []string{
			"systemd-run",
			"--user",
			"--scope",
			"--unit", p.Agent.ID.Name(),
		}
		args = append(args, appendNixShellArgs(nixArgs, "cd "+shellQuote(p.Agent.SandboxWorktree)+" && exec ${SHELL:-bash}")...)
		return args
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
			"--symlink", "/run/current-system/sw/bin/bash", "/usr/bin/bash",
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
		args = append(args, appendNixShellArgs(nixDevelopArgs(p), "cd "+shellQuote(p.Agent.SandboxWorktree)+" && exec ${SHELL:-bash}")...)
		return args
	}
}

func nixDevelopArgs(p Plan) []string {
	args := []string{"nix", "--extra-experimental-features", "nix-command flakes", "--option", "flake-registry", "", "develop"}
	names := make([]string, 0, len(p.FlakeInputOverrides))
	for name := range p.FlakeInputOverrides {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		args = append(args, "--override-input", name, p.FlakeInputOverrides[name])
	}
	args = append(args, p.Flake, "--output-lock-file", "/dev/null")
	return args
}

func appendNixShellArgs(nixArgs []string, script string) []string {
	args := append([]string{}, nixArgs...)
	args = append(args, "--command", "bash", "-lc", script)
	return args
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

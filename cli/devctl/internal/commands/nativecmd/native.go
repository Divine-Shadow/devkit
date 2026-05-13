package nativecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	runtimebroker "devkit/cli/devctl/internal/runtime/broker"
	"devkit/cli/devctl/internal/runtime/capacity"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/runtime/readiness"
	wtx "devkit/cli/devctl/internal/worktrees"
)

// Register adds Nix-native runtime commands.
func Register(r *cmdregistry.Registry) {
	r.Register("native", handle)
	for _, name := range []string{"up", "down", "restart", "status", "logs"} {
		cmd := name
		r.Register(cmd, func(ctx *cmdregistry.Context) error {
			return handleLifecycle(ctx, cmd)
		})
	}
	r.Register("scale", handleLifecycleScale)
	r.Register("ensure-ready", handleLifecycleEnsureReady)
	r.Register("exec", handleTopExec)
	r.Register("attach", handleTopAttach)
}

func handle(ctx *cmdregistry.Context) error {
	if len(ctx.Args) == 0 {
		return fmt.Errorf("Usage: native plan|prepare|exec|readiness|capacity --repo REPO [--index N] [--flake REF]")
	}
	switch ctx.Args[0] {
	case "plan":
		return handlePlan(ctx)
	case "prepare":
		return handlePrepare(ctx)
	case "exec":
		return handleExec(ctx)
	case "readiness":
		return handleReadiness(ctx)
	case "capacity":
		return handleCapacity(ctx)
	case "shell":
		return handleShell(ctx)
	default:
		return fmt.Errorf("unknown native command %s", ctx.Args[0])
	}
}

type planArgs struct {
	opts               nativeplan.BuildOptions
	format             string
	dryRun             bool
	count              int
	baseBranch         string
	branchPrefix       string
	dedicatedWorktrees bool
	skipRepoChecks     bool
	repoCheck          string
	readinessMode      string
	readinessModeSet   bool
	command            []string
}

type lifecycleArgs struct {
	repo                    string
	flake                   string
	count                   int
	format                  string
	brokerSocket            string
	brokerStateRoot         string
	brokerBinary            string
	brokerAllowPulls        *bool
	brokerAllowImage        []string
	worktreeRoot            string
	agentStateRoot          string
	worktreeContainerRoot   string
	agentStateContainerRoot string
	baseBranch              string
	branchPrefix            string
	repoCheck               string
	skipBroker              bool
	skipPrepare             bool
	skipReady               bool
	ready                   bool
	skipRepoChecks          bool
	readinessMode           string
	readinessModeSet        bool
	tailLines               int
}

type topExecArgs struct {
	index                   int
	repo                    string
	flake                   string
	brokerSocket            string
	worktreeRoot            string
	agentStateRoot          string
	worktreeContainerRoot   string
	agentStateContainerRoot string
	command                 []string
}

func parsePlanArgs(ctx *cmdregistry.Context, allowCommand bool, allowReadinessMode bool) (planArgs, error) {
	opts := nativeplan.BuildOptions{
		Paths:             ctx.Paths,
		Project:           ctx.Project,
		Index:             1,
		Launcher:          "bubblewrap",
		DedicatedWorktree: true,
	}
	parsed := planArgs{opts: opts, format: "text"}
	for i := 1; i < len(ctx.Args); i++ {
		switch ctx.Args[i] {
		case "--":
			if !allowCommand {
				return parsed, fmt.Errorf("-- is not valid for native plan")
			}
			parsed.command = append([]string{}, ctx.Args[i+1:]...)
			return parsed, nil
		case "--repo":
			if i+1 < len(ctx.Args) && !strings.HasPrefix(ctx.Args[i+1], "--") {
				parsed.opts.Repo = ctx.Args[i+1]
				i++
				break
			}
			if !allowReadinessMode {
				return parsed, fmt.Errorf("--repo requires a value")
			}
			if err := setPlanReadinessMode(&parsed, "--repo", config.ReadinessModeRepo); err != nil {
				return parsed, err
			}
		case "--repo-readiness", "--full":
			if !allowReadinessMode {
				return parsed, fmt.Errorf("%s is only valid for native readiness and native capacity", ctx.Args[i])
			}
			if err := setPlanReadinessMode(&parsed, ctx.Args[i], config.ReadinessModeRepo); err != nil {
				return parsed, err
			}
		case "--runtime-only":
			if !allowReadinessMode {
				return parsed, fmt.Errorf("--runtime-only is only valid for native readiness and native capacity")
			}
			if err := setPlanReadinessMode(&parsed, "--runtime-only", config.ReadinessModeRuntimeOnly); err != nil {
				return parsed, err
			}
		case "--index":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--index requires a value")
			}
			idx, err := strconv.Atoi(ctx.Args[i+1])
			if err != nil || idx < 1 {
				return parsed, fmt.Errorf("--index must be a positive integer")
			}
			parsed.opts.Index = idx
			i++
		case "--flake":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--flake requires a value")
			}
			parsed.opts.Flake = ctx.Args[i+1]
			i++
		case "--launcher":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--launcher requires a value")
			}
			parsed.opts.Launcher = ctx.Args[i+1]
			i++
		case "--format":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--format requires a value")
			}
			parsed.format = strings.TrimSpace(ctx.Args[i+1])
			i++
		case "--dry-run":
			parsed.dryRun = true
		case "--count":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--count requires a value")
			}
			count, err := strconv.Atoi(ctx.Args[i+1])
			if err != nil || count < 1 {
				return parsed, fmt.Errorf("--count must be a positive integer")
			}
			parsed.count = count
			i++
		case "--base-branch":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--base-branch requires a value")
			}
			parsed.baseBranch = ctx.Args[i+1]
			i++
		case "--branch-prefix":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--branch-prefix requires a value")
			}
			parsed.branchPrefix = ctx.Args[i+1]
			i++
		case "--dedicated-worktrees":
			parsed.dedicatedWorktrees = true
			parsed.opts.DedicatedWorktree = true
		case "--skip-repo-checks":
			if allowReadinessMode {
				if err := setPlanReadinessMode(&parsed, "--skip-repo-checks", config.ReadinessModeRuntimeOnly); err != nil {
					return parsed, err
				}
			} else {
				parsed.skipRepoChecks = true
			}
		case "--repo-check":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--repo-check requires a value")
			}
			parsed.repoCheck = ctx.Args[i+1]
			i++
		case "--worktree-root":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--worktree-root requires a value")
			}
			parsed.opts.WorktreeRoot = ctx.Args[i+1]
			i++
		case "--state-root":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--state-root requires a value")
			}
			parsed.opts.StateRoot = ctx.Args[i+1]
			i++
		case "--worktree-container-root":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--worktree-container-root requires a value")
			}
			parsed.opts.WorktreeContainerRoot = ctx.Args[i+1]
			i++
		case "--state-container-root":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--state-container-root requires a value")
			}
			parsed.opts.StateContainerRoot = ctx.Args[i+1]
			i++
		case "--broker-endpoint":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--broker-endpoint requires a value")
			}
			parsed.opts.BrokerEndpoint = ctx.Args[i+1]
			i++
		case "--proxy":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--proxy requires a value")
			}
			parsed.opts.Proxy = ctx.Args[i+1]
			i++
		case "--resolv-conf":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--resolv-conf requires a value")
			}
			parsed.opts.DNSResolvConf = ctx.Args[i+1]
			i++
		default:
			return parsed, fmt.Errorf("unknown native arg %s", ctx.Args[i])
		}
	}
	if allowReadinessMode {
		if err := applyPlanReadinessMode(&parsed); err != nil {
			return parsed, err
		}
	}
	return parsed, nil
}

func setPlanReadinessMode(parsed *planArgs, flag string, mode string) error {
	return setReadinessMode(&parsed.readinessMode, &parsed.readinessModeSet, flag, mode)
}

func setLifecycleReadinessMode(parsed *lifecycleArgs, flag string, mode string) error {
	return setReadinessMode(&parsed.readinessMode, &parsed.readinessModeSet, flag, mode)
}

func setReadinessMode(current *string, set *bool, flag string, mode string) error {
	normalized, ok := config.NormalizeReadinessMode(mode)
	if !ok {
		return fmt.Errorf("unknown readiness mode %q", mode)
	}
	if *set && *current != normalized {
		return fmt.Errorf("%s conflicts with readiness mode %s", flag, *current)
	}
	*current = normalized
	*set = true
	return nil
}

func applyPlanReadinessMode(parsed *planArgs) error {
	if strings.TrimSpace(parsed.repoCheck) != "" {
		if parsed.readinessModeSet && parsed.readinessMode == config.ReadinessModeRuntimeOnly {
			return fmt.Errorf("--repo-check conflicts with runtime-only readiness")
		}
		parsed.readinessMode = config.ReadinessModeRepo
		parsed.readinessModeSet = true
	}
	if !parsed.readinessModeSet {
		parsed.readinessMode = config.ReadinessModeRepo
	}
	parsed.skipRepoChecks = parsed.readinessMode == config.ReadinessModeRuntimeOnly
	return nil
}

func applyLifecycleReadinessMode(parsed *lifecycleArgs, cfg config.OverlayConfig) error {
	if strings.TrimSpace(parsed.repoCheck) != "" {
		if parsed.readinessModeSet && parsed.readinessMode == config.ReadinessModeRuntimeOnly {
			return fmt.Errorf("--repo-check conflicts with runtime-only readiness")
		}
		parsed.readinessMode = config.ReadinessModeRepo
		parsed.readinessModeSet = true
	}
	if !parsed.readinessModeSet {
		mode, ok := config.NormalizeReadinessMode(cfg.Readiness.DefaultMode)
		if !ok {
			return fmt.Errorf("readiness.default_mode must be runtime-only or repo")
		}
		parsed.readinessMode = mode
	}
	parsed.skipRepoChecks = parsed.readinessMode == config.ReadinessModeRuntimeOnly
	return nil
}

func parseLifecycleArgs(ctx *cmdregistry.Context) (lifecycleArgs, error) {
	parsed := lifecycleArgs{format: "text", tailLines: 200}
	for i := 0; i < len(ctx.Args); i++ {
		switch ctx.Args[i] {
		case "--repo":
			if i+1 < len(ctx.Args) && !strings.HasPrefix(ctx.Args[i+1], "--") {
				parsed.repo = ctx.Args[i+1]
				i++
				break
			}
			if err := setLifecycleReadinessMode(&parsed, "--repo", config.ReadinessModeRepo); err != nil {
				return parsed, err
			}
		case "--repo-readiness", "--full":
			if err := setLifecycleReadinessMode(&parsed, ctx.Args[i], config.ReadinessModeRepo); err != nil {
				return parsed, err
			}
		case "--runtime-only":
			if err := setLifecycleReadinessMode(&parsed, "--runtime-only", config.ReadinessModeRuntimeOnly); err != nil {
				return parsed, err
			}
		case "--flake":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--flake requires a value")
			}
			parsed.flake = ctx.Args[i+1]
			i++
		case "--count":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--count requires a value")
			}
			count, err := strconv.Atoi(ctx.Args[i+1])
			if err != nil || count < 1 {
				return parsed, fmt.Errorf("--count must be a positive integer")
			}
			parsed.count = count
			i++
		case "--format":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--format requires a value")
			}
			parsed.format = strings.TrimSpace(ctx.Args[i+1])
			i++
		case "--broker-socket", "--socket":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("%s requires a value", ctx.Args[i])
			}
			parsed.brokerSocket = ctx.Args[i+1]
			i++
		case "--broker-state-root":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--broker-state-root requires a value")
			}
			parsed.brokerStateRoot = ctx.Args[i+1]
			i++
		case "--broker-binary":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--broker-binary requires a value")
			}
			parsed.brokerBinary = ctx.Args[i+1]
			i++
		case "--allow-image":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--allow-image requires a value")
			}
			parsed.brokerAllowImage = append(parsed.brokerAllowImage, ctx.Args[i+1])
			i++
		case "--allow-pulls":
			v := true
			parsed.brokerAllowPulls = &v
		case "--no-allow-pulls":
			v := false
			parsed.brokerAllowPulls = &v
		case "--worktree-root":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--worktree-root requires a value")
			}
			parsed.worktreeRoot = ctx.Args[i+1]
			i++
		case "--agent-state-root":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--agent-state-root requires a value")
			}
			parsed.agentStateRoot = ctx.Args[i+1]
			i++
		case "--worktree-container-root":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--worktree-container-root requires a value")
			}
			parsed.worktreeContainerRoot = ctx.Args[i+1]
			i++
		case "--agent-state-container-root":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--agent-state-container-root requires a value")
			}
			parsed.agentStateContainerRoot = ctx.Args[i+1]
			i++
		case "--base-branch":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--base-branch requires a value")
			}
			parsed.baseBranch = ctx.Args[i+1]
			i++
		case "--branch-prefix":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--branch-prefix requires a value")
			}
			parsed.branchPrefix = ctx.Args[i+1]
			i++
		case "--repo-check":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--repo-check requires a value")
			}
			parsed.repoCheck = ctx.Args[i+1]
			i++
		case "--skip-broker":
			parsed.skipBroker = true
		case "--skip-prepare":
			parsed.skipPrepare = true
		case "--skip-ready":
			parsed.skipReady = true
		case "--ready":
			parsed.ready = true
		case "--skip-repo-checks":
			if err := setLifecycleReadinessMode(&parsed, "--skip-repo-checks", config.ReadinessModeRuntimeOnly); err != nil {
				return parsed, err
			}
		case "--tail":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--tail requires a value")
			}
			tail, err := strconv.Atoi(ctx.Args[i+1])
			if err != nil || tail < 1 {
				return parsed, fmt.Errorf("--tail must be a positive integer")
			}
			parsed.tailLines = tail
			i++
		case "--service":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--service requires a value")
			}
			i++
		default:
			return parsed, fmt.Errorf("unknown native lifecycle arg %s", ctx.Args[i])
		}
	}
	return parsed, nil
}

type lifecycleStatus struct {
	Command       string                   `json:"command"`
	Runtime       string                   `json:"runtime"`
	Repo          string                   `json:"repo,omitempty"`
	Count         int                      `json:"count,omitempty"`
	ReadinessMode string                   `json:"readiness_mode,omitempty"`
	Broker        *runtimebroker.Status    `json:"broker,omitempty"`
	Capacity      *capacity.Summary        `json:"capacity,omitempty"`
	Agents        []preparedLifecycleAgent `json:"agents,omitempty"`
	ManifestPath  string                   `json:"manifest_path,omitempty"`
	LogPath       string                   `json:"log_path,omitempty"`
	Message       string                   `json:"message,omitempty"`
}

type preparedLifecycleAgent struct {
	Index            int    `json:"index"`
	HostWorktree     string `json:"host_worktree"`
	SandboxWorktree  string `json:"sandbox_worktree"`
	HostHome         string `json:"host_home"`
	SandboxHome      string `json:"sandbox_home"`
	StateRoot        string `json:"state_root"`
	SandboxStateRoot string `json:"sandbox_state_root"`
}

func handleLifecycle(ctx *cmdregistry.Context, command string) error {
	if err := ensureNativeLifecycleProject(ctx); err != nil {
		return err
	}
	parsed, err := parseLifecycleArgs(ctx)
	if err != nil {
		return err
	}
	switch command {
	case "up":
		return lifecycleUp(ctx, parsed, "up")
	case "scale":
		return lifecycleUp(ctx, parsed, "scale")
	case "down":
		return lifecycleDown(ctx, parsed)
	case "restart":
		if err := lifecycleStopOnly(ctx, parsed); err != nil {
			return err
		}
		return lifecycleUp(ctx, parsed, "restart")
	case "status":
		return lifecycleStatusCommand(ctx, parsed)
	case "logs":
		return lifecycleLogs(ctx, parsed)
	default:
		return fmt.Errorf("unknown native lifecycle command %s", command)
	}
}

func handleLifecycleScale(ctx *cmdregistry.Context) error {
	if len(ctx.Args) == 0 {
		return fmt.Errorf("Usage: scale N [--repo REPO] [--broker-socket PATH] [--runtime-only|--repo-readiness] [--skip-ready]")
	}
	if strings.HasPrefix(ctx.Args[0], "--") {
		return fmt.Errorf("scale requires a count")
	}
	next := *ctx
	next.Args = append([]string{"--count", ctx.Args[0]}, ctx.Args[1:]...)
	return handleLifecycle(&next, "scale")
}

func handleLifecycleEnsureReady(ctx *cmdregistry.Context) error {
	if err := ensureNativeLifecycleProject(ctx); err != nil {
		return err
	}
	parsed, err := parseLifecycleArgs(ctx)
	if err != nil {
		return err
	}
	cfg, repo, count, _, _, err := lifecycleDefaults(ctx, parsed)
	if err != nil {
		return err
	}
	if err := applyLifecycleReadinessMode(&parsed, cfg); err != nil {
		return err
	}
	brokerCfg := lifecycleBrokerConfig(ctx, cfg, parsed)
	status := lifecycleStatus{Command: "ensure-ready", Runtime: "native", Repo: repo, Count: count, ReadinessMode: parsed.readinessMode}
	brokerCfg, brokerStatus, err := lifecycleEnsureReadyBroker(ctx, parsed, brokerCfg, runtimebroker.Start)
	if err != nil {
		return err
	}
	if brokerStatus != nil {
		status.Broker = brokerStatus
	}
	opts := lifecyclePlanOptions(ctx, cfg, parsed, repo, brokerCfg)
	if ctx.DryRun {
		status.Message = "dry run: broker action was planned; readiness was not launched"
		return printLifecycleStatus(status, parsed.format)
	}
	summary, err := lifecycleCapacity(ctx, parsed, opts, count)
	if err != nil {
		return err
	}
	status.Capacity = &summary
	if err := printLifecycleStatus(status, parsed.format); err != nil {
		return err
	}
	return lifecycleReadinessError(summary, parsed)
}

type lifecycleBrokerStarter func(context.Context, runtimebroker.Config, bool) (runtimebroker.Status, error)

func lifecycleEnsureReadyBroker(ctx *cmdregistry.Context, parsed lifecycleArgs, brokerCfg runtimebroker.Config, start lifecycleBrokerStarter) (runtimebroker.Config, *runtimebroker.Status, error) {
	if parsed.skipBroker {
		return brokerCfg, nil, nil
	}
	status, err := start(context.Background(), brokerCfg, ctx.DryRun)
	brokerCfg = lifecycleBrokerConfigWithStatusSocket(brokerCfg, status)
	return brokerCfg, &status, err
}

func lifecycleBrokerConfigWithStatusSocket(brokerCfg runtimebroker.Config, status runtimebroker.Status) runtimebroker.Config {
	if strings.TrimSpace(status.Socket) != "" {
		brokerCfg.Socket = status.Socket
	}
	return brokerCfg
}

func lifecycleReadinessError(summary capacity.Summary, parsed lifecycleArgs) error {
	if summary.CapacityAvailable != summary.Total {
		return fmt.Errorf("native capacity is not fully available")
	}
	if !parsed.skipRepoChecks && summary.RepoReady != summary.Total {
		return fmt.Errorf("native repo readiness is not fully available")
	}
	return nil
}

func handleTopExec(ctx *cmdregistry.Context) error {
	parsed, err := parseTopExecArgs(ctx, false)
	if err != nil {
		return err
	}
	if len(parsed.command) == 0 {
		return fmt.Errorf("Usage: exec <index> <command...>")
	}
	return runTopExec(ctx, parsed, parsed.command)
}

func handleTopAttach(ctx *cmdregistry.Context) error {
	parsed, err := parseTopExecArgs(ctx, true)
	if err != nil {
		return err
	}
	return runTopExec(ctx, parsed, nil)
}

func parseTopExecArgs(ctx *cmdregistry.Context, attach bool) (topExecArgs, error) {
	var parsed topExecArgs
	if len(ctx.Args) == 0 {
		if attach {
			return parsed, fmt.Errorf("Usage: attach <index> [--repo REPO]")
		}
		return parsed, fmt.Errorf("Usage: exec <index> <command...>")
	}
	index, err := strconv.Atoi(ctx.Args[0])
	if err != nil || index < 1 {
		return parsed, fmt.Errorf("index must be a positive integer")
	}
	parsed.index = index
	for i := 1; i < len(ctx.Args); i++ {
		switch ctx.Args[i] {
		case "--repo":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--repo requires a value")
			}
			parsed.repo = ctx.Args[i+1]
			i++
		case "--flake":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--flake requires a value")
			}
			parsed.flake = ctx.Args[i+1]
			i++
		case "--broker-socket", "--broker-endpoint":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("%s requires a value", ctx.Args[i])
			}
			parsed.brokerSocket = ctx.Args[i+1]
			i++
		case "--worktree-root":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--worktree-root requires a value")
			}
			parsed.worktreeRoot = ctx.Args[i+1]
			i++
		case "--agent-state-root":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--agent-state-root requires a value")
			}
			parsed.agentStateRoot = ctx.Args[i+1]
			i++
		case "--":
			parsed.command = append([]string{}, ctx.Args[i+1:]...)
			return parsed, nil
		default:
			if attach {
				return parsed, fmt.Errorf("unknown attach arg %s", ctx.Args[i])
			}
			parsed.command = append([]string{}, ctx.Args[i:]...)
			return parsed, nil
		}
	}
	return parsed, nil
}

func runTopExec(ctx *cmdregistry.Context, parsed topExecArgs, command []string) error {
	if err := ensureNativeLifecycleProject(ctx); err != nil {
		return err
	}
	lifecycleParsed := lifecycleArgs{
		repo:                    parsed.repo,
		flake:                   parsed.flake,
		brokerSocket:            parsed.brokerSocket,
		worktreeRoot:            parsed.worktreeRoot,
		agentStateRoot:          parsed.agentStateRoot,
		worktreeContainerRoot:   parsed.worktreeContainerRoot,
		agentStateContainerRoot: parsed.agentStateContainerRoot,
		skipRepoChecks:          true,
	}
	cfg, repo, _, _, _, err := lifecycleDefaults(ctx, lifecycleParsed)
	if err != nil {
		return err
	}
	brokerCfg := lifecycleBrokerConfig(ctx, cfg, lifecycleParsed)
	opts := lifecyclePlanOptions(ctx, cfg, lifecycleParsed, repo, brokerCfg)
	opts.Index = parsed.index
	p, err := nativeplan.BuildDevAll(opts)
	if err != nil {
		return err
	}
	if !ctx.DryRun {
		if err := launch.Prepare(p); err != nil {
			return err
		}
	}
	cmdSpec, err := launch.BuildBubblewrap(p, command)
	if err != nil {
		return err
	}
	if ctx.DryRun {
		fmt.Fprintln(os.Stdout, launch.ShellString(cmdSpec))
		return nil
	}
	if _, err := exec.LookPath(cmdSpec.Path); err != nil {
		return fmt.Errorf("%s not found; install bubblewrap or run native plan for a non-launching plan", cmdSpec.Path)
	}
	cmd := exec.Command(cmdSpec.Path, cmdSpec.Args...)
	cmd.Dir = cmdSpec.Dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return runCommandPreservingExit(cmd)
}

func ensureNativeLifecycleProject(ctx *cmdregistry.Context) error {
	project := strings.TrimSpace(ctx.Project)
	if project == "" {
		return fmt.Errorf("-p <project> is required")
	}
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, project)
	if err != nil {
		return err
	}
	if !config.HasRuntimeFlake(cfg) {
		return fmt.Errorf("native lifecycle requires runtime.flake for -p %s; use 'devctl -p %s compose <command>' for legacy Compose overlays", project, project)
	}
	return nil
}

func lifecycleDefaults(ctx *cmdregistry.Context, parsed lifecycleArgs) (config.OverlayConfig, string, int, string, string, error) {
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, ctx.Project)
	if err != nil {
		return cfg, "", 0, "", "", err
	}
	repo := strings.TrimSpace(parsed.repo)
	if repo == "" {
		repo = strings.TrimSpace(cfg.Defaults.Repo)
	}
	if repo == "" {
		repo = defaultRepoForProject(ctx.Project)
	}
	count := parsed.count
	if count < 1 {
		count = cfg.Defaults.Agents
	}
	if count < 1 {
		count = 1
	}
	baseBranch := strings.TrimSpace(parsed.baseBranch)
	if baseBranch == "" {
		baseBranch = strings.TrimSpace(cfg.Defaults.BaseBranch)
	}
	if baseBranch == "" {
		baseBranch = "main"
	}
	branchPrefix := strings.TrimSpace(parsed.branchPrefix)
	if branchPrefix == "" {
		branchPrefix = strings.TrimSpace(cfg.Defaults.BranchPrefix)
	}
	if branchPrefix == "" {
		branchPrefix = "native-agent"
	}
	return cfg, repo, count, baseBranch, branchPrefix, nil
}

func lifecycleBrokerConfig(ctx *cmdregistry.Context, cfg config.OverlayConfig, parsed lifecycleArgs) runtimebroker.Config {
	brokerCfg := runtimebroker.Config{
		DevkitRoot:    ctx.Paths.Root,
		Socket:        resolveNativeRoot(ctx.Paths.Root, strings.TrimSpace(cfg.Broker.Socket)),
		Upstream:      strings.TrimSpace(cfg.Broker.Upstream),
		AllowedImages: append([]string{}, cfg.Broker.AllowedImages...),
		LogLevel:      strings.TrimSpace(cfg.Broker.LogLevel),
		StateRoot:     strings.TrimSpace(parsed.brokerStateRoot),
		Binary:        strings.TrimSpace(parsed.brokerBinary),
	}
	if cfg.Broker.AllowPulls != nil {
		brokerCfg.AllowPulls = *cfg.Broker.AllowPulls
	}
	if strings.TrimSpace(parsed.brokerSocket) != "" {
		brokerCfg.Socket = resolveNativeRoot(ctx.Paths.Root, parsed.brokerSocket)
		if strings.TrimSpace(parsed.brokerStateRoot) == "" {
			brokerCfg.StateRoot = filepath.Dir(brokerCfg.Socket)
		}
	}
	if len(parsed.brokerAllowImage) > 0 {
		brokerCfg.AllowedImages = append([]string{}, parsed.brokerAllowImage...)
	}
	if parsed.brokerAllowPulls != nil {
		brokerCfg.AllowPulls = *parsed.brokerAllowPulls
	}
	return runtimebroker.Normalize(brokerCfg)
}

func lifecyclePlanOptions(ctx *cmdregistry.Context, cfg config.OverlayConfig, parsed lifecycleArgs, repo string, brokerCfg runtimebroker.Config) nativeplan.BuildOptions {
	return nativeplan.BuildOptions{
		Paths:                 ctx.Paths,
		Project:               ctx.Project,
		Repo:                  repo,
		Flake:                 firstNonEmpty(parsed.flake, cfg.Runtime.Flake),
		Launcher:              "bubblewrap",
		WorktreeRoot:          resolveNativeRoot(ctx.Paths.Root, firstNonEmpty(parsed.worktreeRoot, cfg.Native.WorktreeRoot)),
		StateRoot:             resolveNativeRoot(ctx.Paths.Root, firstNonEmpty(parsed.agentStateRoot, cfg.Native.StateRoot)),
		WorktreeContainerRoot: firstNonEmpty(parsed.worktreeContainerRoot, cfg.Native.WorktreeContainerRoot),
		StateContainerRoot:    firstNonEmpty(parsed.agentStateContainerRoot, cfg.Native.StateContainerRoot),
		BaseBranch:            strings.TrimSpace(parsed.baseBranch),
		BranchPrefix:          strings.TrimSpace(parsed.branchPrefix),
		BrokerEndpoint:        brokerCfg.Socket,
		DedicatedWorktree:     true,
	}
}

func applyNativeConfigDefaults(ctx *cmdregistry.Context, cfg config.OverlayConfig, opts *nativeplan.BuildOptions) {
	if strings.TrimSpace(opts.Repo) == "" {
		opts.Repo = strings.TrimSpace(cfg.Defaults.Repo)
	}
	if strings.TrimSpace(opts.Repo) == "" {
		opts.Repo = defaultRepoForProject(ctx.Project)
	}
	if strings.TrimSpace(opts.Flake) == "" {
		opts.Flake = strings.TrimSpace(cfg.Runtime.Flake)
	}
	if strings.TrimSpace(opts.WorktreeRoot) == "" {
		opts.WorktreeRoot = resolveNativeRoot(ctx.Paths.Root, cfg.Native.WorktreeRoot)
	}
	if strings.TrimSpace(opts.StateRoot) == "" {
		opts.StateRoot = resolveNativeRoot(ctx.Paths.Root, cfg.Native.StateRoot)
	}
	if strings.TrimSpace(opts.WorktreeContainerRoot) == "" {
		opts.WorktreeContainerRoot = strings.TrimSpace(cfg.Native.WorktreeContainerRoot)
	}
	if strings.TrimSpace(opts.StateContainerRoot) == "" {
		opts.StateContainerRoot = strings.TrimSpace(cfg.Native.StateContainerRoot)
	}
	if strings.TrimSpace(opts.BrokerEndpoint) == "" {
		opts.BrokerEndpoint = resolveNativeRoot(ctx.Paths.Root, cfg.Broker.Socket)
	}
	if strings.TrimSpace(opts.BaseBranch) == "" {
		opts.BaseBranch = strings.TrimSpace(cfg.Defaults.BaseBranch)
	}
	if strings.TrimSpace(opts.BranchPrefix) == "" {
		opts.BranchPrefix = strings.TrimSpace(cfg.Defaults.BranchPrefix)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func defaultRepoForProject(project string) string {
	project = strings.TrimSpace(project)
	if project == "" || project == "dev-all" {
		return "ouroboros-ide"
	}
	return project
}

func resolveNativeRoot(devkitRoot, value string) string {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Clean(filepath.Join(devkitRoot, value))
}

func writeNativeManifest(ctx *cmdregistry.Context, opts nativeplan.BuildOptions, count int, dryRun bool) (nativeagent.Manifest, string, error) {
	manifest, err := nativeplan.BuildManifest(opts, count)
	if err != nil {
		return nativeagent.Manifest{}, "", err
	}
	path := nativeagent.ManifestPath(manifest.HostStateRoot, ctx.Project)
	if err := nativeagent.WriteManifest(path, manifest, dryRun); err != nil {
		return nativeagent.Manifest{}, "", err
	}
	return manifest, path, nil
}

func lifecycleUp(ctx *cmdregistry.Context, parsed lifecycleArgs, command string) error {
	cfg, repo, count, baseBranch, branchPrefix, err := lifecycleDefaults(ctx, parsed)
	if err != nil {
		return err
	}
	if !parsed.skipReady {
		if err := applyLifecycleReadinessMode(&parsed, cfg); err != nil {
			return err
		}
	}
	brokerCfg := lifecycleBrokerConfig(ctx, cfg, parsed)
	status := lifecycleStatus{Command: command, Runtime: "native", Repo: repo, Count: count, ReadinessMode: parsed.readinessMode}
	if !parsed.skipBroker {
		brokerStatus, err := runtimebroker.Start(context.Background(), brokerCfg, ctx.DryRun)
		if err != nil {
			return err
		}
		brokerCfg = lifecycleBrokerConfigWithStatusSocket(brokerCfg, brokerStatus)
		status.Broker = &brokerStatus
	}
	parsed.baseBranch = baseBranch
	parsed.branchPrefix = branchPrefix
	planOpts := lifecyclePlanOptions(ctx, cfg, parsed, repo, brokerCfg)
	if !parsed.skipPrepare {
		if err := wtx.SetupNative(wtx.NativeOptions{
			DevkitRoot:   ctx.Paths.Root,
			Repo:         repo,
			Count:        count,
			BaseBranch:   baseBranch,
			BranchPrefix: branchPrefix,
			WorktreeRoot: planOpts.WorktreeRoot,
			DryRun:       ctx.DryRun,
		}); err != nil {
			return err
		}
		for i := 1; i <= count; i++ {
			opts := planOpts
			opts.Index = i
			p, err := nativeplan.BuildDevAll(opts)
			if err != nil {
				return err
			}
			if !ctx.DryRun {
				if err := launch.Prepare(p); err != nil {
					return err
				}
			}
			status.Agents = append(status.Agents, preparedLifecycleAgent{
				Index:            i,
				HostWorktree:     p.Agent.HostWorktree,
				SandboxWorktree:  p.Agent.SandboxWorktree,
				HostHome:         p.Agent.HostHome,
				SandboxHome:      p.Agent.SandboxHome,
				StateRoot:        p.Agent.StateRoot,
				SandboxStateRoot: p.Agent.SandboxStateRoot,
			})
		}
		_, manifestPath, err := writeNativeManifest(ctx, planOpts, count, ctx.DryRun)
		if err != nil {
			return err
		}
		status.ManifestPath = manifestPath
	}
	if !parsed.skipReady && !ctx.DryRun {
		summary, err := lifecycleCapacity(ctx, parsed, planOpts, count)
		if err != nil {
			return err
		}
		status.Capacity = &summary
		if err := lifecycleReadinessError(summary, parsed); err != nil {
			printLifecycleStatus(status, parsed.format)
			return err
		}
	}
	if ctx.DryRun && !parsed.skipReady {
		status.Message = "dry run: broker/worktree actions were planned; readiness was not launched"
	}
	return printLifecycleStatus(status, parsed.format)
}

func lifecycleDown(ctx *cmdregistry.Context, parsed lifecycleArgs) error {
	if err := lifecycleStopOnly(ctx, parsed); err != nil {
		return err
	}
	cfg, repo, count, _, _, err := lifecycleDefaults(ctx, parsed)
	if err != nil {
		return err
	}
	brokerCfg := lifecycleBrokerConfig(ctx, cfg, parsed)
	brokerStatus, err := runtimebroker.Inspect(brokerCfg)
	if err != nil {
		return err
	}
	status := lifecycleStatus{Command: "down", Runtime: "native", Repo: repo, Count: count, Broker: &brokerStatus}
	return printLifecycleStatus(status, parsed.format)
}

func lifecycleStopOnly(ctx *cmdregistry.Context, parsed lifecycleArgs) error {
	cfg, _, _, _, _, err := lifecycleDefaults(ctx, parsed)
	if err != nil {
		return err
	}
	brokerCfg := lifecycleBrokerConfig(ctx, cfg, parsed)
	_, err = runtimebroker.Stop(brokerCfg, ctx.DryRun)
	return err
}

func lifecycleStatusCommand(ctx *cmdregistry.Context, parsed lifecycleArgs) error {
	cfg, repo, count, _, _, err := lifecycleDefaults(ctx, parsed)
	if err != nil {
		return err
	}
	brokerCfg := lifecycleBrokerConfig(ctx, cfg, parsed)
	brokerStatus, err := runtimebroker.Inspect(brokerCfg)
	if err != nil {
		return err
	}
	status := lifecycleStatus{Command: "status", Runtime: "native", Repo: repo, Count: count, Broker: &brokerStatus}
	if lifecycleStatusRunsReadiness(parsed) {
		if err := applyLifecycleReadinessMode(&parsed, cfg); err != nil {
			return err
		}
		status.ReadinessMode = parsed.readinessMode
		brokerCfg = lifecycleBrokerConfigWithStatusSocket(brokerCfg, brokerStatus)
		planOpts := lifecyclePlanOptions(ctx, cfg, parsed, repo, brokerCfg)
		summary, err := lifecycleCapacity(ctx, parsed, planOpts, count)
		if err == nil {
			status.Capacity = &summary
		} else {
			status.Message = err.Error()
		}
	}
	return printLifecycleStatus(status, parsed.format)
}

func lifecycleStatusRunsReadiness(parsed lifecycleArgs) bool {
	return parsed.ready && !parsed.skipReady
}

func lifecycleLogs(ctx *cmdregistry.Context, parsed lifecycleArgs) error {
	cfg, _, _, _, _, err := lifecycleDefaults(ctx, parsed)
	if err != nil {
		return err
	}
	brokerCfg := lifecycleBrokerConfig(ctx, cfg, parsed)
	status, err := runtimebroker.Inspect(brokerCfg)
	if err != nil {
		return err
	}
	logPath := status.LogPath
	out := lifecycleStatus{Command: "logs", Runtime: "native", Broker: &status, LogPath: logPath}
	if strings.TrimSpace(logPath) == "" {
		out.Message = "broker log path is not available"
		return printLifecycleStatus(out, parsed.format)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		out.Message = err.Error()
		return printLifecycleStatus(out, parsed.format)
	}
	lines := tailLines(strings.TrimRight(string(data), "\n"), parsed.tailLines)
	switch parsed.format {
	case "", "text":
		if len(lines) > 0 {
			fmt.Fprintln(os.Stdout, strings.Join(lines, "\n"))
		}
	case "json":
		payload := struct {
			lifecycleStatus
			Lines []string `json:"lines"`
		}{lifecycleStatus: out, Lines: lines}
		encoded, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(encoded))
	default:
		return fmt.Errorf("--format must be text or json")
	}
	return nil
}

func lifecycleCapacity(ctx *cmdregistry.Context, parsed lifecycleArgs, opts nativeplan.BuildOptions, count int) (capacity.Summary, error) {
	planArgs := planArgs{
		opts:           opts,
		repoCheck:      parsed.repoCheck,
		skipRepoChecks: parsed.skipRepoChecks,
	}
	runtimeChecks, repoChecks, err := readinessChecksFor(ctx, planArgs)
	if err != nil {
		return capacity.Summary{}, err
	}
	reports := make(map[int]readiness.Report, count)
	for i := 1; i <= count; i++ {
		next := opts
		next.Index = i
		p, err := nativeplan.BuildDevAll(next)
		if err != nil {
			return capacity.Summary{}, err
		}
		reports[i] = runReadinessReport(p, runtimeChecks, repoChecks)
	}
	return capacity.Build(reports), nil
}

func printLifecycleStatus(status lifecycleStatus, format string) error {
	switch format {
	case "", "text":
		fmt.Fprintf(os.Stdout, "runtime: %s\n", status.Runtime)
		fmt.Fprintf(os.Stdout, "command: %s\n", status.Command)
		if status.Repo != "" {
			fmt.Fprintf(os.Stdout, "repo: %s\n", status.Repo)
		}
		if status.Count > 0 {
			fmt.Fprintf(os.Stdout, "count: %d\n", status.Count)
		}
		if status.ReadinessMode != "" {
			fmt.Fprintf(os.Stdout, "readiness_mode: %s\n", status.ReadinessMode)
		}
		if status.Broker != nil {
			fmt.Fprintf(os.Stdout, "broker_running: %t\n", status.Broker.Running)
			fmt.Fprintf(os.Stdout, "broker_socket: %s\n", status.Broker.Socket)
			if status.Broker.Message != "" {
				fmt.Fprintf(os.Stdout, "broker_message: %s\n", status.Broker.Message)
			}
		}
		if status.Capacity != nil {
			fmt.Fprintf(os.Stdout, "capacity_available: %d/%d\n", status.Capacity.CapacityAvailable, status.Capacity.Total)
			fmt.Fprintf(os.Stdout, "runtime_ready: %d/%d\n", status.Capacity.RuntimeReady, status.Capacity.Total)
			fmt.Fprintf(os.Stdout, "repo_ready: %d/%d\n", status.Capacity.RepoReady, status.Capacity.Total)
		}
		if status.ManifestPath != "" {
			fmt.Fprintf(os.Stdout, "manifest: %s\n", status.ManifestPath)
		}
		for _, agent := range status.Agents {
			fmt.Fprintf(os.Stdout, "agent%d: worktree=%s home=%s state=%s\n", agent.Index, agent.HostWorktree, agent.HostHome, agent.StateRoot)
		}
		if status.Message != "" {
			fmt.Fprintf(os.Stdout, "message: %s\n", status.Message)
		}
	case "json":
		data, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(data))
	default:
		return fmt.Errorf("--format must be text or json")
	}
	return nil
}

func tailLines(text string, count int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if count > 0 && len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return lines
}

func handlePlan(ctx *cmdregistry.Context) error {
	parsed, err := parsePlanArgs(ctx, false, false)
	if err != nil {
		return err
	}
	if parsed.repoCheck != "" {
		return fmt.Errorf("--repo-check is only valid for native readiness and native capacity")
	}
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, ctx.Project)
	if err != nil {
		return err
	}
	parsed.opts.BaseBranch = parsed.baseBranch
	parsed.opts.BranchPrefix = parsed.branchPrefix
	applyNativeConfigDefaults(ctx, cfg, &parsed.opts)
	p, err := nativeplan.BuildDevAll(parsed.opts)
	if err != nil {
		return err
	}
	if p.Launcher == "bubblewrap" {
		cmdSpec, err := launch.BuildBubblewrap(p, nil)
		if err != nil {
			return err
		}
		p.LauncherArgs = append([]string{cmdSpec.Path}, cmdSpec.Args...)
	}
	switch parsed.format {
	case "", "text":
		fmt.Fprint(os.Stdout, nativeplan.RenderText(p))
	case "json":
		data, err := nativeplan.RenderJSON(p)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(data))
	default:
		return fmt.Errorf("--format must be text or json")
	}
	return nil
}

func handlePrepare(ctx *cmdregistry.Context) error {
	parsed, err := parsePlanArgs(ctx, false, false)
	if err != nil {
		return err
	}
	if parsed.repoCheck != "" {
		return fmt.Errorf("--repo-check is only valid for native readiness and native capacity")
	}
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, ctx.Project)
	if err != nil {
		return err
	}
	repo := strings.TrimSpace(parsed.opts.Repo)
	if repo == "" {
		repo = strings.TrimSpace(cfg.Defaults.Repo)
	}
	if repo == "" {
		repo = defaultRepoForProject(ctx.Project)
	}
	count := parsed.count
	if count < 1 {
		count = cfg.Defaults.Agents
	}
	if count < 1 {
		count = 1
	}
	baseBranch := strings.TrimSpace(parsed.baseBranch)
	if baseBranch == "" {
		baseBranch = strings.TrimSpace(cfg.Defaults.BaseBranch)
	}
	if baseBranch == "" {
		baseBranch = "main"
	}
	branchPrefix := strings.TrimSpace(parsed.branchPrefix)
	if branchPrefix == "" {
		branchPrefix = strings.TrimSpace(cfg.Defaults.BranchPrefix)
	}
	if branchPrefix == "" {
		branchPrefix = "native-agent"
	}
	parsed.opts.Repo = repo
	parsed.opts.BaseBranch = baseBranch
	parsed.opts.BranchPrefix = branchPrefix
	applyNativeConfigDefaults(ctx, cfg, &parsed.opts)
	if err := wtx.SetupNative(wtx.NativeOptions{
		DevkitRoot:   ctx.Paths.Root,
		Repo:         repo,
		Count:        count,
		BaseBranch:   baseBranch,
		BranchPrefix: branchPrefix,
		WorktreeRoot: parsed.opts.WorktreeRoot,
		DryRun:       ctx.DryRun || parsed.dryRun,
	}); err != nil {
		return err
	}
	type preparedAgent struct {
		Index            int    `json:"index"`
		HostWorktree     string `json:"host_worktree"`
		SandboxWorktree  string `json:"sandbox_worktree"`
		HostHome         string `json:"host_home"`
		SandboxHome      string `json:"sandbox_home"`
		StateRoot        string `json:"state_root"`
		SandboxStateRoot string `json:"sandbox_state_root"`
	}
	out := struct {
		Repo         string          `json:"repo"`
		Count        int             `json:"count"`
		ManifestPath string          `json:"manifest_path"`
		Agents       []preparedAgent `json:"agents"`
	}{Repo: repo, Count: count}
	for i := 1; i <= count; i++ {
		opts := parsed.opts
		opts.Index = i
		opts.Repo = repo
		opts.DedicatedWorktree = true
		p, err := nativeplan.BuildDevAll(opts)
		if err != nil {
			return err
		}
		if !ctx.DryRun && !parsed.dryRun {
			if err := launch.Prepare(p); err != nil {
				return err
			}
		}
		out.Agents = append(out.Agents, preparedAgent{
			Index:            i,
			HostWorktree:     p.Agent.HostWorktree,
			SandboxWorktree:  p.Agent.SandboxWorktree,
			HostHome:         p.Agent.HostHome,
			SandboxHome:      p.Agent.SandboxHome,
			StateRoot:        p.Agent.StateRoot,
			SandboxStateRoot: p.Agent.SandboxStateRoot,
		})
	}
	_, manifestPath, err := writeNativeManifest(ctx, parsed.opts, count, ctx.DryRun || parsed.dryRun)
	if err != nil {
		return err
	}
	out.ManifestPath = manifestPath
	switch parsed.format {
	case "", "text":
		fmt.Fprintf(os.Stdout, "repo: %s\ncount: %d\n", out.Repo, out.Count)
		fmt.Fprintf(os.Stdout, "manifest: %s\n", out.ManifestPath)
		for _, agent := range out.Agents {
			fmt.Fprintf(os.Stdout, "agent%d: worktree=%s home=%s state=%s\n", agent.Index, agent.HostWorktree, agent.HostHome, agent.StateRoot)
		}
	case "json":
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(data))
	default:
		return fmt.Errorf("--format must be text or json")
	}
	return nil
}

func handleShell(ctx *cmdregistry.Context) error {
	ctx.Args[0] = "exec"
	return handleExec(ctx)
}

func handleExec(ctx *cmdregistry.Context) error {
	parsed, err := parsePlanArgs(ctx, true, false)
	if err != nil {
		return err
	}
	if parsed.repoCheck != "" {
		return fmt.Errorf("--repo-check is only valid for native readiness and native capacity")
	}
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, ctx.Project)
	if err != nil {
		return err
	}
	parsed.opts.BaseBranch = parsed.baseBranch
	parsed.opts.BranchPrefix = parsed.branchPrefix
	applyNativeConfigDefaults(ctx, cfg, &parsed.opts)
	p, err := nativeplan.BuildDevAll(parsed.opts)
	if err != nil {
		return err
	}
	if p.Launcher != "bubblewrap" {
		return fmt.Errorf("native exec currently supports --launcher bubblewrap only")
	}
	dryRun := ctx.DryRun || parsed.dryRun
	if !dryRun {
		if err := launch.Prepare(p); err != nil {
			return err
		}
	}
	cmdSpec, err := launch.BuildBubblewrap(p, parsed.command)
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Fprintln(os.Stdout, launch.ShellString(cmdSpec))
		return nil
	}
	if _, err := exec.LookPath(cmdSpec.Path); err != nil {
		return fmt.Errorf("%s not found; install bubblewrap or run native plan for a non-launching plan", cmdSpec.Path)
	}
	cmd := exec.Command(cmdSpec.Path, cmdSpec.Args...)
	cmd.Dir = cmdSpec.Dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return runCommandPreservingExit(cmd)
}

func runCommandPreservingExit(cmd *exec.Cmd) error {
	err := cmd.Run()
	if code, ok := exitCodeFromError(err); ok {
		os.Exit(code)
	}
	return err
}

func exitCodeFromError(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		if code >= 0 {
			return code, true
		}
	}
	return 0, false
}

func handleReadiness(ctx *cmdregistry.Context) error {
	parsed, err := parsePlanArgs(ctx, false, true)
	if err != nil {
		return err
	}
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, ctx.Project)
	if err != nil {
		return err
	}
	parsed.opts.BaseBranch = parsed.baseBranch
	parsed.opts.BranchPrefix = parsed.branchPrefix
	applyNativeConfigDefaults(ctx, cfg, &parsed.opts)
	p, err := nativeplan.BuildDevAll(parsed.opts)
	if err != nil {
		return err
	}
	runtimeChecks, repoChecks, err := readinessChecksFor(ctx, parsed)
	if err != nil {
		return err
	}
	report := runReadinessReport(p, runtimeChecks, repoChecks)
	switch parsed.format {
	case "", "json":
		data, err := json.MarshalIndent(struct {
			ReadinessMode     string            `json:"readiness_mode"`
			RuntimeReady      bool              `json:"runtime_ready"`
			RepoReady         bool              `json:"repo_ready"`
			CapacityAvailable bool              `json:"capacity_available"`
			Checks            []readiness.Check `json:"checks"`
		}{
			ReadinessMode:     parsed.readinessMode,
			RuntimeReady:      report.RuntimeReady(),
			RepoReady:         report.RepoReady(),
			CapacityAvailable: report.CapacityAvailable(),
			Checks:            report.Checks,
		}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(data))
	case "text":
		fmt.Fprintf(os.Stdout, "readiness_mode: %s\n", parsed.readinessMode)
		fmt.Fprintf(os.Stdout, "runtime_ready: %t\n", report.RuntimeReady())
		fmt.Fprintf(os.Stdout, "repo_ready: %t\n", report.RepoReady())
		fmt.Fprintf(os.Stdout, "capacity_available: %t\n", report.CapacityAvailable())
		for _, check := range report.Checks {
			fmt.Fprintf(os.Stdout, "%s.%s: ok=%t retryable=%t required_for_capacity=%t", check.Phase, check.Name, check.OK, check.Retryable, check.RequiredForCapacity)
			if check.Detail != "" {
				fmt.Fprintf(os.Stdout, " detail=%q", check.Detail)
			}
			fmt.Fprintln(os.Stdout)
		}
	default:
		return fmt.Errorf("--format must be text or json")
	}
	if !report.RuntimeReady() {
		return fmt.Errorf("native runtime is not ready")
	}
	if !parsed.skipRepoChecks && !report.RepoReady() {
		return fmt.Errorf("native repo readiness is not fully available")
	}
	return nil
}

func handleCapacity(ctx *cmdregistry.Context) error {
	parsed, err := parsePlanArgs(ctx, false, true)
	if err != nil {
		return err
	}
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, ctx.Project)
	if err != nil {
		return err
	}
	count := parsed.count
	if count < 1 {
		count = cfg.Defaults.Agents
	}
	if count < 1 {
		count = 1
	}
	repo := strings.TrimSpace(parsed.opts.Repo)
	if repo == "" {
		repo = strings.TrimSpace(cfg.Defaults.Repo)
	}
	if repo == "" {
		repo = defaultRepoForProject(ctx.Project)
	}
	parsed.opts.Repo = repo
	parsed.opts.BaseBranch = parsed.baseBranch
	parsed.opts.BranchPrefix = parsed.branchPrefix
	applyNativeConfigDefaults(ctx, cfg, &parsed.opts)
	runtimeChecks, repoChecks, err := readinessChecksFor(ctx, parsed)
	if err != nil {
		return err
	}
	reports := make(map[int]readiness.Report, count)
	for i := 1; i <= count; i++ {
		opts := parsed.opts
		opts.Index = i
		opts.Repo = repo
		p, err := nativeplan.BuildDevAll(opts)
		if err != nil {
			return err
		}
		reports[i] = runReadinessReport(p, runtimeChecks, repoChecks)
	}
	summary := capacity.Build(reports)
	switch parsed.format {
	case "", "json":
		data, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(data))
	case "text":
		fmt.Fprintf(os.Stdout, "total: %d\n", summary.Total)
		fmt.Fprintf(os.Stdout, "runtime_ready: %d\n", summary.RuntimeReady)
		fmt.Fprintf(os.Stdout, "repo_ready: %d\n", summary.RepoReady)
		fmt.Fprintf(os.Stdout, "capacity_available: %d\n", summary.CapacityAvailable)
		for _, agent := range summary.Agents {
			fmt.Fprintf(os.Stdout, "agent%d: runtime_ready=%t repo_ready=%t capacity_available=%t\n", agent.Index, agent.RuntimeReady, agent.RepoReady, agent.CapacityAvailable)
		}
	default:
		return fmt.Errorf("--format must be text or json")
	}
	if summary.CapacityAvailable != summary.Total {
		return fmt.Errorf("native capacity is not fully available")
	}
	return nil
}

type repoCheck struct {
	Name    string
	Command string
}

type runtimeCheck struct {
	Name    string
	Command string
}

func readinessChecksFor(ctx *cmdregistry.Context, parsed planArgs) ([]runtimeCheck, []repoCheck, error) {
	runtimeChecks, err := runtimeChecksFor(ctx)
	if err != nil {
		return nil, nil, err
	}
	repoChecks, err := repoChecksFor(ctx, parsed)
	if err != nil {
		return nil, nil, err
	}
	return runtimeChecks, repoChecks, nil
}

func runtimeChecksFor(ctx *cmdregistry.Context) ([]runtimeCheck, error) {
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, ctx.Project)
	if err != nil {
		return nil, err
	}
	checks := make([]runtimeCheck, 0, len(cfg.Readiness.RuntimeChecks))
	seen := map[string]struct{}{}
	for i, check := range cfg.Readiness.RuntimeChecks {
		command := strings.TrimSpace(check.Command)
		if command == "" {
			continue
		}
		name := strings.TrimSpace(check.Name)
		if name == "" {
			name = fmt.Sprintf("runtime-check-%d", i+1)
		}
		seen[name] = struct{}{}
		checks = append(checks, runtimeCheck{Name: name, Command: command})
	}
	if version := strings.TrimSpace(cfg.Runtime.CodexVersion); version != "" {
		if _, ok := seen["codex-version"]; !ok {
			command := "test \"$(codex --version | awk '{print $NF}' | sed 's/^v//')\" = " + strconv.Quote(version)
			checks = append(checks, runtimeCheck{Name: "codex-version", Command: command})
		}
	}
	return checks, nil
}

func repoChecksFor(ctx *cmdregistry.Context, parsed planArgs) ([]repoCheck, error) {
	if strings.TrimSpace(parsed.repoCheck) != "" {
		return []repoCheck{{Name: "repo-check", Command: parsed.repoCheck}}, nil
	}
	if parsed.skipRepoChecks {
		return nil, nil
	}
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, ctx.Project)
	if err != nil {
		return nil, err
	}
	structured := make([]repoCheck, 0, len(cfg.Readiness.RepoChecks))
	seen := map[string]struct{}{}
	for i, check := range cfg.Readiness.RepoChecks {
		command := strings.TrimSpace(check.Command)
		if command == "" {
			continue
		}
		name := strings.TrimSpace(check.Name)
		if name == "" {
			name = fmt.Sprintf("repo-check-%d", i+1)
		}
		seen[name] = struct{}{}
		structured = append(structured, repoCheck{Name: name, Command: command})
	}
	checks := make([]repoCheck, 0, len(structured)+2)
	if warm := strings.TrimSpace(cfg.Hooks.Warm); warm != "" {
		if _, ok := seen["warm-hook"]; !ok {
			checks = append(checks, repoCheck{Name: "warm-hook", Command: warm})
		}
	}
	checks = append(checks, structured...)
	if core := strings.TrimSpace(cfg.Runtime.CoreCheck); core != "" {
		if _, ok := seen["core-check"]; !ok {
			checks = append(checks, repoCheck{Name: "core-check", Command: core})
		}
	}
	return checks, nil
}

func runReadinessReport(p nativeplan.Plan, runtimeChecks []runtimeCheck, repoChecks []repoCheck) readiness.Report {
	var report readiness.Report
	if err := launch.Prepare(p); err != nil {
		report.AddRuntime("prepare-state", false, err.Error())
		return report
	}
	report.AddRuntime("prepare-state", true, "")

	runtimeScript := strings.Join([]string{
		`test "${DEVKIT_NATIVE_AGENT:-}" = "1"`,
		`test -d "$HOME"`,
		`test -d "$CODEX_ROLLOUT_DIR"`,
		`test -x /usr/bin/env`,
		`test -x /bin/sh`,
		`test "${DOCKER_HOST:-}" = "unix://` + p.BrokerEndpoint + `"`,
		`test "${DOCKER_HOST:-}" != "unix:///var/run/docker.sock"`,
		`test ! -e /var/run/docker.sock`,
		`pwd >/dev/null`,
	}, " && ")
	out, err := runSandboxCommand(p, []string{"bash", "-lc", runtimeScript})
	report.AddRuntime("sandbox-command", err == nil, detail(err, out))
	if err != nil {
		return report
	}
	brokerScript := strings.Join([]string{
		`broker="${DOCKER_HOST#unix://}"`,
		`test -n "$broker"`,
		`test "$broker" != "$DOCKER_HOST"`,
		`test -S "$broker"`,
		`curl --unix-socket "$broker" -fsS http://docker/_ping | grep -qx OK`,
	}, " && ")
	out, err = runSandboxCommand(p, []string{"bash", "-lc", brokerScript})
	report.AddRuntime("broker-socket", err == nil, detail(err, out))
	for _, check := range runtimeChecks {
		if strings.TrimSpace(check.Command) == "" {
			continue
		}
		out, err = runSandboxCommand(p, []string{"bash", "-lc", check.Command})
		report.AddRuntime(check.Name, err == nil, detail(err, out))
	}
	for _, check := range repoChecks {
		if strings.TrimSpace(check.Command) == "" {
			continue
		}
		out, err = runSandboxCommand(p, []string{"bash", "-lc", check.Command})
		report.AddRepo(check.Name, err == nil, detail(err, out))
	}
	return report
}

func runSandboxCommand(p nativeplan.Plan, command []string) (string, error) {
	cmdSpec, err := launch.BuildBubblewrap(p, command)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(cmdSpec.Path, cmdSpec.Args...)
	cmd.Dir = cmdSpec.Dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func detail(err error, out string) string {
	trimmed := strings.TrimSpace(out)
	if err == nil {
		return trimmed
	}
	if trimmed == "" {
		return err.Error()
	}
	return err.Error() + ": " + trimmed
}

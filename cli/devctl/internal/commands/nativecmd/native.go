package nativecmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/runtime/capacity"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/runtime/readiness"
	wtx "devkit/cli/devctl/internal/worktrees"
)

// Register adds Nix-native runtime commands.
func Register(r *cmdregistry.Registry) {
	r.Register("native", handle)
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
	command            []string
}

func parsePlanArgs(ctx *cmdregistry.Context, allowCommand bool) (planArgs, error) {
	opts := nativeplan.BuildOptions{
		Paths:    ctx.Paths,
		Project:  ctx.Project,
		Index:    1,
		Launcher: "bubblewrap",
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
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--repo requires a value")
			}
			parsed.opts.Repo = ctx.Args[i+1]
			i++
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
			parsed.skipRepoChecks = true
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
	return parsed, nil
}

func handlePlan(ctx *cmdregistry.Context) error {
	parsed, err := parsePlanArgs(ctx, false)
	if err != nil {
		return err
	}
	if parsed.repoCheck != "" {
		return fmt.Errorf("--repo-check is only valid for native readiness and native capacity")
	}
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
	parsed, err := parsePlanArgs(ctx, false)
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
		repo = "ouroboros-ide"
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
		branchPrefix = "native-agent"
	}
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
		Index        int    `json:"index"`
		HostWorktree string `json:"host_worktree"`
		HostHome     string `json:"host_home"`
		StateRoot    string `json:"state_root"`
	}
	out := struct {
		Repo   string          `json:"repo"`
		Count  int             `json:"count"`
		Agents []preparedAgent `json:"agents"`
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
			Index:        i,
			HostWorktree: p.Agent.HostWorktree,
			HostHome:     p.Agent.HostHome,
			StateRoot:    p.Agent.StateRoot,
		})
	}
	switch parsed.format {
	case "", "text":
		fmt.Fprintf(os.Stdout, "repo: %s\ncount: %d\n", out.Repo, out.Count)
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
	parsed, err := parsePlanArgs(ctx, true)
	if err != nil {
		return err
	}
	if parsed.repoCheck != "" {
		return fmt.Errorf("--repo-check is only valid for native readiness and native capacity")
	}
	p, err := nativeplan.BuildDevAll(parsed.opts)
	if err != nil {
		return err
	}
	if p.Launcher != "bubblewrap" {
		return fmt.Errorf("native exec currently supports --launcher bubblewrap only")
	}
	if !parsed.dryRun {
		if err := launch.Prepare(p); err != nil {
			return err
		}
	}
	cmdSpec, err := launch.BuildBubblewrap(p, parsed.command)
	if err != nil {
		return err
	}
	if parsed.dryRun {
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
	return cmd.Run()
}

func handleReadiness(ctx *cmdregistry.Context) error {
	parsed, err := parsePlanArgs(ctx, false)
	if err != nil {
		return err
	}
	p, err := nativeplan.BuildDevAll(parsed.opts)
	if err != nil {
		return err
	}
	repoChecks, err := repoChecksFor(ctx, parsed)
	if err != nil {
		return err
	}
	report := runReadinessReport(p, repoChecks)
	switch parsed.format {
	case "", "json":
		data, err := json.MarshalIndent(struct {
			RuntimeReady      bool              `json:"runtime_ready"`
			RepoReady         bool              `json:"repo_ready"`
			CapacityAvailable bool              `json:"capacity_available"`
			Checks            []readiness.Check `json:"checks"`
		}{
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
	return nil
}

func handleCapacity(ctx *cmdregistry.Context) error {
	parsed, err := parsePlanArgs(ctx, false)
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
		repo = "ouroboros-ide"
	}
	repoChecks, err := repoChecksFor(ctx, parsed)
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
		reports[i] = runReadinessReport(p, repoChecks)
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
	checks := make([]repoCheck, 0, len(cfg.Readiness.RepoChecks)+2)
	for i, check := range cfg.Readiness.RepoChecks {
		command := strings.TrimSpace(check.Command)
		if command == "" {
			continue
		}
		name := strings.TrimSpace(check.Name)
		if name == "" {
			name = fmt.Sprintf("repo-check-%d", i+1)
		}
		checks = append(checks, repoCheck{Name: name, Command: command})
	}
	if len(checks) > 0 {
		return checks, nil
	}
	if warm := strings.TrimSpace(cfg.Hooks.Warm); warm != "" {
		checks = append(checks, repoCheck{Name: "warm-hook", Command: warm})
	}
	if core := strings.TrimSpace(cfg.Runtime.CoreCheck); core != "" {
		checks = append(checks, repoCheck{Name: "core-check", Command: core})
	}
	return checks, nil
}

func runReadinessReport(p nativeplan.Plan, repoChecks []repoCheck) readiness.Report {
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
		`test -n "${HTTP_PROXY:-}"`,
		`test -n "${HTTPS_PROXY:-}"`,
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

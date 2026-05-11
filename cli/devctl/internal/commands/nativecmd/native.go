package nativecmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/runtime/readiness"
)

// Register adds Nix-native runtime commands.
func Register(r *cmdregistry.Registry) {
	r.Register("native", handle)
}

func handle(ctx *cmdregistry.Context) error {
	if len(ctx.Args) == 0 {
		return fmt.Errorf("Usage: native plan|exec --repo REPO [--index N] [--flake REF] [--launcher bubblewrap|systemd-run]")
	}
	switch ctx.Args[0] {
	case "plan":
		return handlePlan(ctx)
	case "exec":
		return handleExec(ctx)
	case "readiness":
		return handleReadiness(ctx)
	case "shell":
		return handleShell(ctx)
	default:
		return fmt.Errorf("unknown native command %s", ctx.Args[0])
	}
}

type planArgs struct {
	opts      nativeplan.BuildOptions
	format    string
	dryRun    bool
	repoCheck string
	command   []string
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
		return fmt.Errorf("--repo-check is only valid for native readiness")
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
		return fmt.Errorf("--repo-check is only valid for native readiness")
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
	report := runReadinessReport(p, parsed.repoCheck)
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

func runReadinessReport(p nativeplan.Plan, repoCheck string) readiness.Report {
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
	if strings.TrimSpace(repoCheck) == "" {
		return report
	}
	out, err = runSandboxCommand(p, []string{"bash", "-lc", repoCheck})
	report.AddRepo("repo-check", err == nil, detail(err, out))
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

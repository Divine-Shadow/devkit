package nativecmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

type currentWorktreeReadinessArgs struct {
	repo, name, format string
}

func parseCurrentWorktreeReadinessArgs(args []string) (currentWorktreeReadinessArgs, error) {
	var parsed currentWorktreeReadinessArgs
	flags := flag.NewFlagSet("native readiness --current-worktree", flag.ContinueOnError)
	current := flags.Bool("current-worktree", false, "use this accepted GUI worktree without constructing a boundary")
	flags.StringVar(&parsed.repo, "repo", "", "declared repository")
	flags.StringVar(&parsed.name, "repo-check-name", "", "one immutable overlay repo check")
	flags.StringVar(&parsed.format, "format", "json", "json or text")
	if err := flags.Parse(args); err != nil {
		return parsed, err
	}
	if !*current || flags.NArg() != 0 || parsed.repo != "ouroboros-ide" || parsed.name == "" ||
		parsed.name != strings.TrimSpace(parsed.name) || (parsed.format != "json" && parsed.format != "text") {
		return parsed, fmt.Errorf("current-worktree readiness requires --repo ouroboros-ide --repo-check-name NAME [--format json|text]")
	}
	return parsed, nil
}

func currentWorktreeRepoCheck(cfg config.OverlayConfig, name string) (repoCheck, error) {
	var selected repoCheck
	count := 0
	for _, check := range cfg.Readiness.RepoChecks {
		if check.Name == name {
			selected = repoCheck{Name: check.Name, Command: check.Command}
			count++
		}
	}
	if count != 1 || strings.TrimSpace(selected.Command) == "" {
		return repoCheck{}, fmt.Errorf("immutable repo check %q must have exactly one nonempty definition", name)
	}
	return selected, nil
}

func validateCurrentGUIReadinessEnvironment(getenv func(string) string, consumer nativeplan.GUIConsumerWorkspace) error {
	for name, expected := range map[string]string{
		"DEVKIT_NATIVE_ISOLATION_PROFILE":      "workspace-egress",
		"DEVKIT_NATIVE_MOUNT_POLICY_IDENTITY":  "devkit/workspace-egress/v4",
		"DEVKIT_NATIVE_WINDOWS_MOUNTS_VISIBLE": "false",
		"OURO_NIX_SANDBOX":                     "1",
		"HOME":                                 consumer.Home,
		"CODEX_HOME":                           filepath.Join(consumer.Home, ".codex"),
	} {
		if getenv(name) != expected {
			return fmt.Errorf("current-worktree readiness requires the declared GUI consumer %s", name)
		}
	}
	return nil
}

func validateCurrentGUIWorktree(base, cwd string) error {
	// Both probes must discover the repository from their directory. Preserve
	// transport/security configuration, but refuse ambient repository selectors.
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_IMPLICIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT"} {
		if _, present := os.LookupEnv(name); present {
			return fmt.Errorf("current-worktree readiness rejects repository override %s", name)
		}
	}
	if !filepath.IsAbs(cwd) || filepath.Clean(cwd) != cwd {
		return fmt.Errorf("current worktree is not canonical")
	}
	rel, err := filepath.Rel(base, cwd)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("current worktree is outside the declared GUI consumer")
	}
	resolved, err := filepath.EvalSymlinks(cwd)
	if err != nil || resolved != cwd {
		return fmt.Errorf("current worktree must not traverse a symlink")
	}
	rootCommand := exec.Command("git", "rev-parse", "--show-toplevel")
	rootCommand.Dir = cwd
	root, err := rootCommand.Output()
	if err != nil || strings.TrimSpace(string(root)) != cwd {
		return fmt.Errorf("current cwd must be the exact Git worktree root")
	}
	registeredCommand := exec.Command("git", "worktree", "list", "--porcelain", "-z")
	registeredCommand.Dir = base
	registered, err := registeredCommand.Output()
	if err != nil {
		return fmt.Errorf("read declared lane Git worktrees: %w", err)
	}
	for _, field := range strings.Split(string(registered), "\x00") {
		if field == "worktree "+cwd {
			return nil
		}
	}
	return fmt.Errorf("current worktree is not registered in the declared GUI lane")
}

func executeCurrentRepoCheck(cwd string, check repoCheck) (int, string, string) {
	// Use the same finite command timeout as ordinary repo readiness. Inherit
	// this accepted consumer's environment; do not construct or seed a sandbox.
	command := exec.Command("timeout", "1200", "bash", "-c", check.Command)
	command.Dir = cwd
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if err == nil {
		return 0, stdout.String(), stderr.String()
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), stdout.String(), stderr.String()
	}
	return 127, stdout.String(), stderr.String() + err.Error()
}

func runCurrentRepoCheck(cwd string, check repoCheck, dryRun bool) (string, int, string, string) {
	if dryRun {
		return "planned", 0, "", ""
	}
	code, out, stderr := executeCurrentRepoCheck(cwd, check)
	if code != 0 {
		return "fail", code, out, stderr
	}
	return "pass", code, out, stderr
}

func handleCurrentWorktreeReadiness(ctx *cmdregistry.Context) error {
	parsed, err := parseCurrentWorktreeReadinessArgs(ctx.Args[1:])
	if err != nil {
		return err
	}
	if ctx.Project != "dev-all" || ctx.Profile != "" {
		return fmt.Errorf("current-worktree readiness is declared only for dev-all")
	}
	authority := ctx.Paths.RuntimeAuthorityRoot
	if !filepath.IsAbs(authority) || filepath.Clean(authority) != authority || !strings.HasPrefix(authority, "/nix/store/") {
		return fmt.Errorf("current-worktree readiness requires executable-owned immutable runtime authority")
	}
	// Host/ambient overlay paths cannot substitute a preparation recipe.
	cfg, dir, err := config.ReadAll([]string{filepath.Join(authority, "overlays")}, ctx.Project)
	if err != nil {
		return err
	}
	check, err := currentWorktreeRepoCheck(cfg, parsed.name)
	if err != nil {
		return err
	}
	index, err := strconv.Atoi(os.Getenv("DEVKIT_NATIVE_AGENT"))
	if err != nil || strconv.Itoa(index) != os.Getenv("DEVKIT_NATIVE_AGENT") {
		return fmt.Errorf("current-worktree readiness requires the inherited GUI agent index")
	}
	consumer, err := nativeplan.ResolveGUIConsumerWorkspace(ctx.Project, parsed.repo, os.Getenv(nativeplan.GUITargetIDEnvironment), index)
	if err != nil {
		return err
	}
	if err := validateCurrentGUIReadinessEnvironment(os.Getenv, consumer); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := validateCurrentGUIWorktree(consumer.Worktree, cwd); err != nil {
		return err
	}
	profile, err := os.ReadFile(filepath.Join(dir, "devkit.yaml"))
	if err != nil {
		return err
	}
	commandSHA, profileSHA := sha256.Sum256([]byte(check.Command)), sha256.Sum256(profile)
	status, exitCode, stdout, stderr := runCurrentRepoCheck(cwd, check, ctx.DryRun)
	report := map[string]any{"scope": "current-worktree-repo-check", "status": status, "check": check.Name,
		"cwd": cwd, "consumer": consumer, "runtimeAuthorityRoot": authority,
		"profileSha256": hex.EncodeToString(profileSHA[:]), "commandSha256": hex.EncodeToString(commandSHA[:]),
		"exitCode": exitCode, "stdout": stdout, "stderr": stderr}
	if parsed.format == "json" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(data))
	} else {
		fmt.Fprintf(os.Stdout, "repo.%s: %s cwd=%s exit=%d\n%s%s", check.Name, status, cwd, exitCode, stdout, stderr)
	}
	if exitCode != 0 {
		return fmt.Errorf("repo check %s failed with exit %d", check.Name, exitCode)
	}
	return nil
}

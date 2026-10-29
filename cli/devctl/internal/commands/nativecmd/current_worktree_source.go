package nativecmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/sshauthority"
	"devkit/cli/devctl/internal/worktrees"
)

func validateCurrentSourceTransport(prefix, helper, hostRoot, command string) error {
	// A current connector has a launcher-owned PID, but no arbitrary command or
	// socket selector is accepted. Reconstruct the complete existing recipe.
	match := regexp.MustCompile(`\.managed-egress-([1-9][0-9]*)\.sock`).FindAllStringSubmatch(command, -1)
	if len(match) != 1 {
		return fmt.Errorf("source preparation requires one current managed connector")
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	socket := filepath.Join(hostRoot, ".devkit", "native-egress", ".managed-egress-"+match[0][1]+".sock")
	proxy := quote(helper) + " -p 'dev-all' native proxy-connect --socket " + quote(socket) + " --target %h:%p"
	if command != prefix+" -o "+quote("ProxyCommand="+proxy) {
		return fmt.Errorf("source preparation rejects a replacement Git transport")
	}
	return nil
}

func parseCurrentSourceArgs(args []string) error {
	flags := flag.NewFlagSet("native prepare --current-worktree --source-only", flag.ContinueOnError)
	current := flags.Bool("current-worktree", false, "prepare the existing GUI writer")
	source := flags.Bool("source-only", false, "acquire accepted-source metadata without integration")
	repo := flags.String("repo", "", "declared repository")
	format := flags.String("format", "json", "json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !*current || !*source || *repo != "ouroboros-ide" || *format != "json" || flags.NArg() != 0 {
		return fmt.Errorf("source preparation requires --current-worktree --source-only --repo ouroboros-ide [--format json]")
	}
	return nil
}

func handleCurrentWorktreeSourcePreparation(ctx *cmdregistry.Context) error {
	if err := parseCurrentSourceArgs(ctx.Args[1:]); err != nil {
		return err
	}
	if ctx.Project != "dev-all" || ctx.Profile != "" {
		return fmt.Errorf("current source preparation requires dev-all")
	}
	authority := ctx.Paths.RuntimeAuthorityRoot
	if !filepath.IsAbs(authority) || filepath.Clean(authority) != authority || !strings.HasPrefix(authority, "/nix/store/") {
		return fmt.Errorf("source preparation requires executable-owned immutable runtime authority")
	}
	cfg, _, err := config.ReadAll([]string{filepath.Join(authority, "overlays")}, ctx.Project)
	if err != nil {
		return err
	}
	index, err := strconv.Atoi(os.Getenv("DEVKIT_NATIVE_AGENT"))
	if err != nil || strconv.Itoa(index) != os.Getenv("DEVKIT_NATIVE_AGENT") {
		return fmt.Errorf("source preparation requires the inherited GUI agent index")
	}
	consumer, err := nativeplan.ResolveGUIConsumerWorkspace(ctx.Project, "ouroboros-ide", os.Getenv(nativeplan.GUITargetIDEnvironment), index)
	if err != nil {
		return err
	}
	if err := validateCurrentGUIReadinessEnvironment(os.Getenv, consumer); err != nil {
		return err
	}
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_IMPLICIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT"} {
		if _, present := os.LookupEnv(key); present {
			return fmt.Errorf("source preparation rejects repository override %s", key)
		}
	}
	// Retain the current accepted execution's protected connector. Its SSH and
	// immutable trust/config prefix must be the package-owned managed Git policy;
	// this operation cannot supply a replacement command, home or credential.
	sshAuthority, err := sshauthority.Package()
	if err != nil {
		return err
	}
	prefix, err := sshAuthority.ManagedGitCommand(consumer.Home)
	if err != nil {
		return err
	}
	ssh := os.Getenv("GIT_SSH_COMMAND")
	if os.Getenv("GIT_SSH_VARIANT") != "ssh" {
		return fmt.Errorf("source preparation requires the existing managed Git transport")
	}
	// The live consumer's mounted immutable profile owns its existing connector.
	// A newly supplied preparation executable must not require restarting that
	// consumer merely to change the connector's helper path to its own package.
	helper, err := filepath.EvalSymlinks("/run/current-system/sw/kit/bin/devctl")
	if err != nil || !strings.HasPrefix(helper, "/nix/store/") || filepath.Clean(helper) != helper {
		return fmt.Errorf("source preparation requires the current consumer's immutable Devctl helper")
	}
	if err := validateCurrentSourceTransport(prefix, helper, resolveNativeHostRoot(ctx.Paths.Root, cfg), ssh); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	result, prepErr := worktrees.PrepareCurrentSource(worktrees.CurrentSource{Worktree: cwd, BaseWorktree: consumer.Worktree, Home: consumer.Home, Origin: cfg.Defaults.Origin, Index: index}, true, ssh, ctx.DryRun)
	status := "prepared"
	if ctx.DryRun {
		status = "planned"
	}
	if prepErr != nil {
		status = "failed"
	}
	report := map[string]any{"scope": "current-worktree-source-preparation", "status": status, "cwd": cwd, "consumer": consumer, "runtimeAuthorityRoot": authority, "source": result, "requiresCurrentPolicyReevaluation": true}
	if prepErr != nil {
		report["error"] = prepErr.Error()
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(data))
	return prepErr
}

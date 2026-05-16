package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	agentexec "devkit/cli/devctl/internal/agentexec"
	"devkit/cli/devctl/internal/cmdregistry"
	allowcmd "devkit/cli/devctl/internal/commands/allow"
	brokercmd "devkit/cli/devctl/internal/commands/brokercmd"
	hookcmd "devkit/cli/devctl/internal/commands/hooks"
	hostscmd "devkit/cli/devctl/internal/commands/hosts"
	nativecmd "devkit/cli/devctl/internal/commands/nativecmd"
	networkcmd "devkit/cli/devctl/internal/commands/network"
	preflightcmd "devkit/cli/devctl/internal/commands/preflight"
	runtimematrixcmd "devkit/cli/devctl/internal/commands/runtimematrix"
	tmuxcmd "devkit/cli/devctl/internal/commands/tmuxcmd"
	verifyallcmd "devkit/cli/devctl/internal/commands/verifyall"
	"devkit/cli/devctl/internal/config"
	poolcfg "devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/devkitpaths"
	"devkit/cli/devctl/internal/execx"
	"devkit/cli/devctl/internal/layout"
	"devkit/cli/devctl/internal/netutil"
	pth "devkit/cli/devctl/internal/paths"
	runner "devkit/cli/devctl/internal/runner"
	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	nativelaunch "devkit/cli/devctl/internal/runtime/launch"
	"devkit/cli/devctl/internal/tmuxutil"
	wtx "devkit/cli/devctl/internal/worktrees"
	"devkit/cli/devctl/internal/wtutil"
)

var tmuxForceOverride bool

func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// gitIdentityFromHost discovers a sensible git author/committer identity from the host.
// Priority:
// 1) DEVKIT_GIT_USER_NAME / DEVKIT_GIT_USER_EMAIL
// 2) GIT_AUTHOR_NAME / GIT_AUTHOR_EMAIL (falling back to COMMITTER_*)
// 3) `git config --global user.name` / `git config --global user.email`
func gitIdentityFromHost() (name, email string) {
	// Explicit override via env
	if v := strings.TrimSpace(os.Getenv("DEVKIT_GIT_USER_NAME")); v != "" {
		name = v
	}
	if v := strings.TrimSpace(os.Getenv("DEVKIT_GIT_USER_EMAIL")); v != "" {
		email = v
	}
	// Generic git envs
	if name == "" {
		if v := strings.TrimSpace(os.Getenv("GIT_AUTHOR_NAME")); v != "" {
			name = v
		}
		if name == "" {
			if v := strings.TrimSpace(os.Getenv("GIT_COMMITTER_NAME")); v != "" {
				name = v
			}
		}
	}
	if email == "" {
		if v := strings.TrimSpace(os.Getenv("GIT_AUTHOR_EMAIL")); v != "" {
			email = v
		}
		if email == "" {
			if v := strings.TrimSpace(os.Getenv("GIT_COMMITTER_EMAIL")); v != "" {
				email = v
			}
		}
	}
	// Host git config (best effort)
	if name == "" {
		if out, r := execx.Capture(context.Background(), "git", "config", "--global", "user.name"); r.Code == 0 {
			v := strings.TrimSpace(out)
			if v != "" {
				name = v
			}
		}
	}
	if email == "" {
		if out, r := execx.Capture(context.Background(), "git", "config", "--global", "user.email"); r.Code == 0 {
			v := strings.TrimSpace(out)
			if v != "" {
				email = v
			}
		}
	}
	return name, email
}

func gitIdentityForAgent(project, idx string) (name, email string, err error) {
	if strings.TrimSpace(project) == "dev-all" {
		agent := strings.TrimSpace(idx)
		if agent == "" {
			agent = "1"
		}
		return fmt.Sprintf("Agent %s of BayeSartre", agent), fmt.Sprintf("agent+%s@ouroboros-ai.com", agent), nil
	}
	name, email = gitIdentityFromHost()
	if strings.TrimSpace(name) == "" || strings.TrimSpace(email) == "" {
		return "", "", fmt.Errorf("git identity not configured. Set DEVKIT_GIT_USER_NAME and DEVKIT_GIT_USER_EMAIL, or configure host git --global user.name/user.email")
	}
	return name, email, nil
}

// shSingleQuote wraps s in single quotes and escapes any embedded single quotes for POSIX shells.
func shSingleQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func applyOverlayEnv(cfg config.OverlayConfig, overlayDir string, root string) {
	applyOverlayEnvInternal(cfg, overlayDir, root, false, nil)
}

func pushOverlayEnv(cfg config.OverlayConfig, overlayDir string, root string, force bool) func() {
	changed := map[string]*string{}
	applyOverlayEnvInternal(cfg, overlayDir, root, force, changed)
	if len(changed) == 0 {
		return nil
	}
	return func() {
		keys := make([]string, 0, len(changed))
		for k := range changed {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			prev := changed[key]
			if prev == nil {
				_ = os.Unsetenv(key)
				continue
			}
			_ = os.Setenv(key, *prev)
		}
	}
}

func pushEnvMap(env map[string]string) func() {
	if len(env) == 0 {
		return nil
	}
	type pair struct {
		key string
		val string
	}
	pairs := make([]pair, 0, len(env))
	for k, v := range env {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		pairs = append(pairs, pair{key: key, val: v})
	}
	if len(pairs) == 0 {
		return nil
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].key < pairs[j].key
	})
	changed := map[string]*string{}
	for _, p := range pairs {
		if _, recorded := changed[p.key]; !recorded {
			if cur, ok := os.LookupEnv(p.key); ok {
				val := cur
				changed[p.key] = &val
			} else {
				changed[p.key] = nil
			}
		}
		_ = os.Setenv(p.key, expandValue(p.val))
	}
	return func() {
		for i := len(pairs) - 1; i >= 0; i-- {
			key := pairs[i].key
			prev := changed[key]
			if prev == nil {
				_ = os.Unsetenv(key)
				continue
			}
			_ = os.Setenv(key, *prev)
		}
	}
}

func combineRestorers(restorers ...func()) func() {
	active := make([]func(), 0, len(restorers))
	for _, fn := range restorers {
		if fn != nil {
			active = append(active, fn)
		}
	}
	if len(active) == 0 {
		return nil
	}
	return func() {
		for i := len(active) - 1; i >= 0; i-- {
			active[i]()
		}
	}
}

func applyOverlayEnvInternal(cfg config.OverlayConfig, overlayDir string, root string, force bool, changed map[string]*string) {
	base := strings.TrimSpace(overlayDir)
	if base == "" {
		base = root
	}
	if _, ok := os.LookupEnv("DEVKIT_WORKTREE_CONTAINER_ROOT"); !ok {
		_ = os.Setenv("DEVKIT_WORKTREE_CONTAINER_ROOT", "/worktrees")
	}
	if _, ok := os.LookupEnv("DEVKIT_WORKTREE_ROOT"); !ok {
		defaultRoot := filepath.Join(filepath.Clean(filepath.Join(root, "..")), pth.AgentWorktreesDir)
		if strings.TrimSpace(defaultRoot) != "" {
			_ = os.Setenv("DEVKIT_WORKTREE_ROOT", defaultRoot)
		}
	}
	setEnv := func(key string, raw string, resolved string, expand bool) {
		k := strings.TrimSpace(key)
		if k == "" {
			return
		}
		if !force && !shouldSetEnv(k, raw) {
			return
		}
		if changed != nil {
			if _, recorded := changed[k]; !recorded {
				if cur, ok := os.LookupEnv(k); ok {
					val := cur
					changed[k] = &val
				} else {
					changed[k] = nil
				}
			}
		}
		val := resolved
		if expand {
			val = expandValue(resolved)
		}
		_ = os.Setenv(k, val)
	}
	ws := strings.TrimSpace(cfg.Workspace)
	if ws != "" {
		resolved := ws
		if !filepath.IsAbs(ws) {
			resolved = filepath.Clean(filepath.Join(base, ws))
		}
		setEnv("WORKSPACE_DIR", cfg.Workspace, resolved, false)
	}
	for _, file := range cfg.EnvFiles {
		path := strings.TrimSpace(file)
		if path == "" {
			continue
		}
		resolved := path
		if !filepath.IsAbs(path) {
			resolved = filepath.Join(base, path)
		}
		for k, v := range readEnvFile(resolved) {
			setEnv(k, v, v, true)
		}
	}
	for key, val := range cfg.Env {
		setEnv(key, val, val, true)
	}
}

func readEnvFile(path string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		parts := strings.SplitN(trim, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "" {
			continue
		}
		out[key] = val
	}
	return out
}

func resolveHostOverlayPaths(raw []string, baseDir string, root string) []string {
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		trim := strings.TrimSpace(p)
		if trim == "" {
			continue
		}
		resolved := expandHome(trim)
		if !filepath.IsAbs(resolved) {
			anchor := root
			if baseDir != "" {
				anchor = baseDir
			}
			resolved = filepath.Join(anchor, resolved)
		}
		out = append(out, filepath.Clean(resolved))
	}
	return out
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		house, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(house, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

func shouldSetEnv(key, value string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	cur, exists := os.LookupEnv(key)
	if !exists || cur == "" {
		return true
	}
	needle1 := "$" + key
	needle2 := "${" + key + "}"
	return strings.Contains(value, needle1) || strings.Contains(value, needle2)
}

func expandValue(raw string) string {
	return os.Expand(raw, func(name string) string {
		return os.Getenv(name)
	})
}

// resolveService returns the default service for a project overlay, falling back to dev-agent.
func resolveService(project string, overlayPaths []string) string {
	svc := "dev-agent"
	if strings.TrimSpace(project) == "" {
		return svc
	}
	if cfg, _, err := config.ReadAll(overlayPaths, project); err == nil {
		if s := strings.TrimSpace(cfg.Service); s != "" {
			svc = s
		}
	}
	return svc
}

func defaultDevAllRepoMain(overlayPaths []string) string {
	cfg, _, err := config.ReadAll(overlayPaths, "dev-all")
	if err == nil {
		if repo := strings.TrimSpace(cfg.Defaults.Repo); repo != "" {
			return repo
		}
	}
	return "ouroboros-ide"
}

func usage() {
	fmt.Fprintf(os.Stderr, `devctl - Nix/native runtime CLI
Usage: devctl -p <project> [--profile <profiles>] <command> [args]

Commands:
  up, down, restart, status [--ready], logs   (native for overlays with runtime.flake)
  broker start|status|stop [--socket PATH] [--allow-image IMAGE] [--format text|json]
  scale N [--repo REPO] [--broker-socket PATH] [--runtime-only|--repo-readiness] [--skip-ready]
  ensure-ready [--count N] [--repo REPO] [--runtime-only|--repo-readiness] [--broker-socket PATH] [--skip-broker]
  exec <n> <cmd...>, attach <n>              (native for overlays with runtime.flake)
  codex-auth reseed <n> [--service NAME]
  codex-auth reseed-all [indexes...] [--service NAME]
  allow <domain>, warm, maintain, check-net, check-codex, check-sts
  hosts [print|apply|check] [--target host|agents|all] [--index N] [--all-agents]
  proxy {tinyproxy|envoy}
  tmux-shells [N] [--plain], open [N] [--plain], fresh-open [N] [--plain]
  exec-cd <index> <subpath> [cmd...], attach-cd <index> <subpath>
  tmux-sync [--session NAME] [--count N] [--name-prefix PFX] [--cd PATH] [--service NAME]
  tmux-add-cd <index> <subpath> [--session NAME] [--name NAME] [--service NAME]
  tmux-apply-layout --file <layout.yaml> [--session NAME] [--attach]
  wt-open [--session NAME] [--plain] [--index N|--count N] [--service NAME] [--cd PATH], wt-release [--session NAME]
  tmux-bell-install [--session NAME] [--backend windows-notify|file] [--file PATH] [--debounce-ms N]
  tmux-bell-show-config [--backend windows-notify|file] [--file PATH] [--debounce-ms N]
  native plan --repo REPO [--index N] [--flake REF] [--launcher bubblewrap|systemd-run] [--format text|json]
  native prepare --repo REPO [--count N] [--base-branch BRANCH] [--branch-prefix PFX] [--format text|json]
  native exec --repo REPO [--index N] [--flake REF] [--proxy-socket SOCK] [--dry-run] [-- COMMAND...]
  native readiness --repo REPO [--index N] [--flake REF] [--runtime-only|--repo-readiness] [--repo-check CMD] [--format text|json]
  native capacity --repo REPO [--count N] [--flake REF] [--runtime-only|--repo-readiness] [--format text|json]
  native egress-proxy [--socket SOCK] [--allowlist FILE]
  layout-apply --file <layout.yaml> [--attach]   (bring up overlays, run warm hooks, then attach tmux)
  layout-validate --file <layout.yaml>                (static checks; exits non-zero on errors)
  layout-generate [--service NAME] [--session NAME] [--output PATH]
  ssh-setup [--key path] [--index N], ssh-test [N]
  repo-config-ssh <repo> [--index N], repo-push-ssh <repo> [--index N]
  repo-config-https <repo> [--index N], repo-push-https <repo> [--index N]
  worktrees-init <repo> <count> [--base agent] [--branch main]
  worktrees-setup <repo> <count> [--base agent] [--branch main]  (flake-backed overlays)
  worktrees-branch <repo> <index> <branch>   (flake-backed overlays)
  worktrees-status <repo> [--all|--index N]  (flake-backed overlays)
  worktrees-sync <repo> (--pull|--push) [--all|--index N]  (flake-backed overlays)
  worktrees-tmux <repo> <count> [--plain]    (flake-backed overlays)
  reset [N]                                  (alias: fresh-open)
  bootstrap <repo> <count>                   (flake-backed overlays)
  runtime-matrix [--check] [--all]           (repo to runtime pairing report)
  verify                                     (ssh + codex + worktrees)
  verify-all                                 (run verify for codex and dev-all)
  preflight                                  (host checks: nix, bubblewrap, tmux, ssh keys, ~/.codex, broker Docker)

Flags:
  -p, --project   overlay project name (required for most)
  --profile       comma-separated: hardened,dns,envoy (default: dns)
  --tmux          force tmux integration even if DEVKIT_NO_TMUX=1

Environment:
  DEVKIT_DEBUG=1  print executed commands
`)
}

func main() {
	var project string
	var profile string
	var dryRun bool
	var noTmux bool
	var forceTmux bool
	var noSeed bool
	var reSeed bool

	// rudimentary -p/--project and --profile parsing before subcmd
	args := os.Args[1:]
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-p", "--project":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "-p requires value")
				os.Exit(2)
			}
			project = args[i+1]
			i++
		case "--profile":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "--profile requires value")
				os.Exit(2)
			}
			profile = args[i+1]
			i++
		case "--compose-project":
			fmt.Fprintln(os.Stderr, "--compose-project is retired; native flakes do not use project-name overrides")
			os.Exit(2)
		case "--dry-run":
			dryRun = true
		case "--no-tmux":
			noTmux = true
		case "--tmux":
			forceTmux = true
		case "--no-seed":
			noSeed = true
		case "--reseed":
			reSeed = true
		case "-h", "--help", "help":
			usage()
			return
		default:
			out = append(out, a)
		}
	}
	args = out
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	exe, _ := os.Executable()
	paths, _ := devkitpaths.DetectPathsFromExe(exe)
	hostCfg, hostCfgDir, hostErr := config.ReadHostConfig()
	if hostErr != nil {
		fmt.Fprintf(os.Stderr, "[devctl] warning: failed to parse host config: %v\n", hostErr)
	}
	if url := strings.TrimSpace(hostCfg.CLI.DownloadURL); url != "" {
		if _, ok := os.LookupEnv("DEVKIT_CLI_DOWNLOAD_URL"); !ok {
			_ = os.Setenv("DEVKIT_CLI_DOWNLOAD_URL", url)
		}
	}
	for key, val := range hostCfg.Env {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if _, ok := os.LookupEnv(key); ok {
			continue
		}
		_ = os.Setenv(key, val)
	}
	if len(hostCfg.OverlayPaths) > 0 {
		extra := resolveHostOverlayPaths(hostCfg.OverlayPaths, hostCfgDir, paths.Root)
		paths.OverlayPaths = devkitpaths.MergeOverlayPaths(paths.OverlayPaths, extra...)
	}
	overlayDir := devkitpaths.FindOverlayDir(paths.OverlayPaths, project)
	overlayCfg, cfgDir, cfgErr := config.ReadAll(paths.OverlayPaths, project)
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "[devctl] warning: failed to parse devkit.yaml for %s: %v\n", project, cfgErr)
	}
	if cfgDir == "" {
		cfgDir = overlayDir
	}
	applyOverlayEnv(overlayCfg, cfgDir, paths.Root)
	tmuxForceOverride = forceTmux
	if forceTmux {
		_ = os.Unsetenv("DEVKIT_NO_TMUX")
	}
	cmd := args[0]
	sub := args[1:]
	isNativeRuntime := strings.TrimSpace(project) == "dev-all" || nativeRuntimeConfigured(project, overlayCfg)

	// Preflight: choose a non-overlapping internal subnet and DNS IP if not explicitly set
	cidr, dns := netutil.PickInternalSubnet()
	_ = os.Setenv("DEVKIT_INTERNAL_SUBNET", cidr)
	_ = os.Setenv("DEVKIT_DNS_IP", dns)
	if os.Getenv("DEVKIT_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "[devctl] internal subnet=%s dns_ip=%s\n", cidr, dns)
	}

	// honor --no-tmux by setting env used by skipTmux()
	if noTmux {
		_ = os.Setenv("DEVKIT_NO_TMUX", "1")
	}

	// Read optional credential pool config from env (defaults preserve host behavior)
	pconf := poolcfg.ReadPoolConfig()
	registry := cmdregistry.New()
	allowcmd.Register(registry)
	brokercmd.Register(registry)
	hostscmd.Register(registry)
	hookcmd.Register(registry)
	networkcmd.Register(registry)
	preflightcmd.Register(registry)
	runtimematrixcmd.Register(registry)
	nativecmd.Register(registry)
	verifyallcmd.Register(registry)
	tmuxcmd.Register(registry, defaultSessionName, mustAtoi, hasTmuxSession)
	ctx := &cmdregistry.Context{
		DryRun:  dryRun,
		Project: project,
		Profile: profile,
		Args:    sub,
		Paths:   paths,
		Pool:    pconf,
		Exe:     exe,
	}
	if handler, ok := registry.Lookup(cmd); ok {
		if err := handler(ctx); err != nil {
			die(err.Error())
		}
		return
	}
	if requiresRuntimeFlakeCommand(cmd) && !isNativeRuntime {
		die(fmt.Sprintf("%s requires runtime.flake for -p %s", cmd, project))
	}
	switch cmd {
	case "compose":
		die("retired runtime namespace; use native lifecycle commands backed by runtime.flake")
	case "up", "down", "restart", "status", "logs":
		die("native lifecycle command was not registered")
	case "scale", "ensure-ready":
		die("native lifecycle command was not registered")
	case "layout-apply":
		layoutPath := ""
		doAttach := false
		for i := 0; i < len(sub); i++ {
			if sub[i] == "--file" && i+1 < len(sub) {
				layoutPath = sub[i+1]
				i++
			} else if sub[i] == "--attach" {
				doAttach = true
			}
		}
		if strings.TrimSpace(layoutPath) == "" {
			die("Usage: layout-apply --file <layout.yaml>")
		}
		lf, err := layout.Read(layoutPath)
		if err != nil {
			die(err.Error())
		}
		if layoutIsNativeRuntime(paths, lf, project) {
			repo, count := layoutNativeRepoAndCount(paths, project, lf)
			runner.Host(dryRun, exe, "-p", project, "up", "--repo", repo, "--count", fmt.Sprintf("%d", count))
			if skipTmux() {
				fmt.Fprintln(os.Stderr, "[layout] tmux skipped via DEVKIT_NO_TMUX")
				break
			}
			sessName := strings.TrimSpace(lf.Session)
			if sessName == "" {
				sessName = defaultSessionName(project)
			}
			if len(lf.Windows) == 0 {
				break
			}
			createdSession := false
			if !hasTmuxSession(sessName) {
				w := lf.Windows[0]
				idx := w.Index
				if idx < 1 {
					idx = 1
				}
				name := strings.TrimSpace(w.Name)
				if name == "" {
					name = fmt.Sprintf("agent-%d", idx)
				}
				cmdStr := mustBuildNativeWindowCmd(exe, project, repo, idx, w.Path)
				runner.Host(dryRun, "tmux", tmuxutil.NewSession(sessName, cmdStr)...)
				runner.Host(dryRun, "tmux", tmuxutil.RenameWindow(sessName+":0", name)...)
				createdSession = true
			}
			start := 0
			if createdSession {
				start = 1
			}
			for _, w := range lf.Windows[start:] {
				idx := w.Index
				if idx < 1 {
					idx = 1
				}
				name := strings.TrimSpace(w.Name)
				if name == "" {
					name = fmt.Sprintf("agent-%d", idx)
				}
				cmdStr := mustBuildNativeWindowCmd(exe, project, repo, idx, w.Path)
				runner.Host(dryRun, "tmux", tmuxutil.NewWindow(sessName, name, cmdStr)...)
			}
			if doAttach {
				if !stdoutIsTTY() {
					fmt.Fprintln(os.Stderr, "layout-apply: --attach skipped because stdout is not a TTY")
				} else {
					runner.HostInteractive(dryRun, "tmux", tmuxutil.Attach(sessName)...)
				}
			}
			break
		}
		die("layout-apply only supports native single-overlay layouts; mixed retired-runtime layouts are not supported")
	case "layout-validate":
		layoutPath := ""
		for i := 0; i < len(sub); i++ {
			if sub[i] == "--file" && i+1 < len(sub) {
				layoutPath = sub[i+1]
				i++
			}
		}
		if strings.TrimSpace(layoutPath) == "" {
			die("Usage: layout-validate --file <layout.yaml>")
		}
		lf, err := layout.Read(layoutPath)
		if err != nil {
			die(err.Error())
		}
		warns, errs := layout.Validate(lf, project)
		for _, msg := range warns {
			fmt.Println("[warn]", msg)
		}
		if len(errs) > 0 {
			for _, msg := range errs {
				fmt.Fprintln(os.Stderr, "[error]", msg)
			}
			os.Exit(2)
		}
	case "layout-generate":
		mustProject(project)
		// Usage: layout-generate [--service NAME] [--session NAME] [--output PATH]
		service := "dev-agent"
		sessName := ""
		output := ""
		for i := 0; i < len(sub); i++ {
			switch sub[i] {
			case "--service":
				if i+1 < len(sub) {
					service = sub[i+1]
					i++
				}
			case "--session":
				if i+1 < len(sub) {
					sessName = sub[i+1]
					i++
				}
			case "--output":
				if i+1 < len(sub) {
					output = sub[i+1]
					i++
				}
			}
		}
		if strings.TrimSpace(sessName) == "" {
			sessName = defaultSessionName("mixed")
		}
		if !isNativeRuntime {
			die("layout-generate requires runtime.flake")
		}
		if strings.TrimSpace(service) != "" && service != "dev-agent" {
			die("layout-generate only supports native dev-agent layouts")
		}
		{
			repo := readDefaultRepo(project, paths)
			count := nativeDefaultAgentCount(paths, project, 1)
			var b strings.Builder
			fmt.Fprintf(&b, "session: %s\n\nwindows:\n", sessName)
			for i := 1; i <= count; i++ {
				path := filepath.Join("/workspaces/dev/agent-worktrees", fmt.Sprintf("agent%d", i), repo)
				if i == 1 && project == "dev-all" {
					path = filepath.Join("/workspaces/dev", repo)
				}
				fmt.Fprintf(&b, "  - index: %d\n    project: %s\n    service: dev-agent\n    path: %s\n    name: agent-%d\n", i, project, path, i)
			}
			yml := b.String()
			if strings.TrimSpace(output) == "" {
				fmt.Fprint(os.Stdout, yml)
			} else {
				if err := os.WriteFile(output, []byte(yml), 0644); err != nil {
					die(err.Error())
				}
				fmt.Fprintf(os.Stderr, "wrote %s\n", output)
			}
			break
		}
	case "codex-auth", "creds":
		mustProject(project)
		if !isNativeRuntime {
			die("codex-auth requires runtime.flake")
		}
		if len(sub) == 0 {
			die("Usage: codex-auth reseed <index> [--service NAME]\n       codex-auth reseed-all [indexes...] [--service NAME]")
		}
		action := strings.TrimSpace(sub[0])
		switch action {
		case "reseed":
			idx := "1"
			seenIndex := false
			for i := 1; i < len(sub); i++ {
				switch sub[i] {
				case "--service":
					if i+1 >= len(sub) {
						die("--service requires a value")
					}
					i++
				default:
					if seenIndex {
						die("Usage: codex-auth reseed <index> [--service NAME]\n       codex-auth reseed-all [indexes...] [--service NAME]")
					}
					idx = sub[i]
					seenIndex = true
				}
			}
			repo := readDefaultRepo(project, paths)
			seedNativeCodexAuth(dryRun, paths, project, repo, mustAtoi(idx))
			fmt.Printf("reseeded Codex auth for %s agent %s\n", project, idx)
		case "reseed-all":
			indexes := []string{}
			for i := 1; i < len(sub); i++ {
				switch sub[i] {
				case "--service":
					if i+1 >= len(sub) {
						die("--service requires a value")
					}
					i++
				default:
					indexes = append(indexes, strings.TrimSpace(sub[i]))
				}
			}
			if len(indexes) == 0 {
				count := nativeDefaultAgentCount(paths, project, 1)
				for i := 1; i <= count; i++ {
					indexes = append(indexes, fmt.Sprintf("%d", i))
				}
			}
			repo := readDefaultRepo(project, paths)
			for _, idx := range indexes {
				seedNativeCodexAuth(dryRun, paths, project, repo, mustAtoi(idx))
				fmt.Printf("reseeded Codex auth for %s agent %s\n", project, idx)
			}
		default:
			die("Usage: codex-auth reseed <index> [--service NAME]\n       codex-auth reseed-all [indexes...] [--service NAME]")
		}
	case "attach":
		die("native lifecycle command was not registered")
	case "proxy":
		mustProject(project)
		which := "tinyproxy"
		if len(sub) > 0 && strings.TrimSpace(sub[0]) != "" {
			which = sub[0]
		}
		switch which {
		case "tinyproxy":
			fmt.Println("Switching agent env to tinyproxy... (ensure overlay uses HTTP(S)_PROXY=http://tinyproxy:8888)")
		case "envoy":
			fmt.Println("Enable envoy profile: add --profile envoy to up/restart commands")
		default:
			die("unknown proxy: " + which)
		}
	case "codex-test":
		mustProject(project)
		if !isNativeRuntime {
			die("codex-test requires runtime.flake")
		}
		// Parse optional args: [index] [repo]
		idx := "1"
		var repo string
		if len(sub) > 0 {
			if _, err := strconv.Atoi(sub[0]); err == nil {
				idx = sub[0]
				if len(sub) > 1 {
					repo = sub[1]
				}
			}
			if _, err := strconv.Atoi(sub[0]); err != nil {
				repo = sub[0]
				if len(sub) > 1 {
					idx = sub[1]
				}
			}
		}
		if strings.TrimSpace(repo) == "" {
			repo = readDefaultRepo(project, paths)
		}
		script := "set -euo pipefail; if codex exec 'reply with: ok' 2>&1 | tr -d '\\r' | grep -m1 -x ok >/dev/null; then echo ok; else echo 'codex-test failed'; exit 1; fi"
		nativeExecScriptCommand(dryRun, exe, project, repo, mustAtoi(idx), script)
	case "doctor-runtime":
		mustProject(project)
		if !isNativeRuntime {
			die("doctor-runtime requires runtime.flake")
		}
		repo := readDefaultRepo(project, paths)
		count := nativeDefaultAgentCount(paths, project, 1)
		runner.Host(dryRun, exe, "-p", project, "ensure-ready", "--repo", repo, "--count", fmt.Sprintf("%d", count), "--runtime-only")
	case "verify":
		mustProject(project)
		if !isNativeRuntime {
			die("verify requires runtime.flake")
		}
		repo := readDefaultRepo(project, paths)
		count := nativeDefaultAgentCount(paths, project, 1)
		runner.Host(dryRun, exe, "-p", project, "ensure-ready", "--repo", repo, "--count", fmt.Sprintf("%d", count), "--repo-readiness")
		fmt.Println("verify completed")
	case "codex-debug":
		mustProject(project)
		if !isNativeRuntime {
			die("codex-debug requires runtime.flake")
		}
		idx := "1"
		if len(sub) > 0 {
			idx = sub[0]
		}
		repo := readDefaultRepo(project, paths)
		script := `set -e
echo "HOME=$HOME"; echo "CODEX_HOME=$CODEX_HOME"
echo "-- locations --"
for p in "$HOME/.codex/auth.json" "$CODEX_HOME/auth.json"; do
  [ -n "$p" ] || continue; echo -n "$p : "; [ -f "$p" ] && wc -c < "$p" || echo "(missing)"; done
exit 0`
		nativeExecScriptCommand(dryRun, exe, project, repo, mustAtoi(idx), script)
	case "check-sts":
		mustProject(project)
		which := "envoy"
		if len(sub) > 0 {
			which = strings.TrimSpace(sub[0])
		}
		var px string
		switch which {
		case "envoy":
			px = "http://envoy:3128"
		case "tinyproxy":
			px = "http://tinyproxy:8888"
		default:
			die("Usage: check-sts [envoy|tinyproxy]")
		}
		if !isNativeRuntime {
			die("check-sts requires runtime.flake")
		}
		repo := readDefaultRepo(project, paths)
		script := strings.Join([]string{
			"HTTPS_PROXY='" + px + "' HTTP_PROXY='" + px + "' curl -sSvo /dev/null -w '%{http_code}\\n' https://sts.amazonaws.com || true",
			"HTTPS_PROXY='" + px + "' HTTP_PROXY='" + px + "' aws sts get-caller-identity || true",
			"curl -sSvo /dev/null -w '%{http_code}\\n' https://sts.amazonaws.com || true",
			"aws sts get-caller-identity || true",
		}, "\n")
		nativeExecScriptCommand(dryRun, exe, project, repo, 1, script)
	case "exec-cd":
		mustProject(project)
		if !isNativeRuntime {
			die("exec-cd requires runtime.flake")
		}
		if len(sub) < 2 {
			die("Usage: exec-cd <index> <subpath> [cmd...]")
		}
		idx := sub[0]
		subpath := sub[1]
		repo := nativeRepoForSubpath(paths, project, subpath)
		index := mustAtoi(idx)
		dest := nativeSandboxDest(index, repo, subpath)
		cmdstr := "bash"
		if len(sub) > 2 {
			cmdstr = strings.Join(sub[2:], " ")
		}
		script := "set -e; cd " + shSingleQuote(dest) + "; exec " + cmdstr
		nativeExecScriptCommandInteractive(dryRun, exe, project, repo, index, script)
	case "attach-cd":
		mustProject(project)
		if !isNativeRuntime {
			die("attach-cd requires runtime.flake")
		}
		if len(sub) < 2 {
			die("Usage: attach-cd <index> <subpath>")
		}
		idx := sub[0]
		subpath := sub[1]
		repo := nativeRepoForSubpath(paths, project, subpath)
		index := mustAtoi(idx)
		dest := nativeSandboxDest(index, repo, subpath)
		script := "set -e; cd " + shSingleQuote(dest) + "; exec bash"
		nativeExecScriptCommandInteractive(dryRun, exe, project, repo, index, script)
	case "tmux-shells":
		mustProject(project)
		if !isNativeRuntime {
			die("tmux-shells requires runtime.flake")
		}
		n := 2
		plain := false
		for _, arg := range sub {
			if arg == "--plain" {
				plain = true
				continue
			}
			n = mustAtoi(arg)
		}
		{
			repo := readDefaultRepo(project, paths)
			sess := "devkit-shells"
			if plain {
				tabs := make([]wtutil.TabSpec, 0, n)
				for i := 1; i <= n; i++ {
					tabs = append(tabs, wtutil.TabSpec{
						Title:   fmt.Sprintf("agent-%d", i),
						Command: mustBuildNativeWindowCmd(exe, project, repo, i, ""),
					})
				}
				if err := launchPlainTabs(dryRun, project, sess, tabs); err != nil {
					die(err.Error())
				}
			} else if !skipTmux() {
				cmd := mustBuildNativeWindowCmd(exe, project, repo, 1, "")
				runner.Host(dryRun, "tmux", tmuxutil.NewSession(sess, cmd)...)
				runner.Host(dryRun, "tmux", tmuxutil.RenameWindow(sess+":0", "agent-1")...)
				for i := 2; i <= n; i++ {
					wcmd := mustBuildNativeWindowCmd(exe, project, repo, i, "")
					runner.Host(dryRun, "tmux", tmuxutil.NewWindow(sess, fmt.Sprintf("agent-%d", i), wcmd)...)
				}
				runner.HostInteractive(dryRun, "tmux", tmuxutil.Attach(sess)...)
			}
			break
		}
	case "open":
		mustProject(project)
		if !isNativeRuntime {
			die("open requires runtime.flake")
		}
		n := 2
		plain := false
		for _, arg := range sub {
			if arg == "--plain" {
				plain = true
				continue
			}
			n = mustAtoi(arg)
		}
		{
			repo := readDefaultRepo(project, paths)
			sess := "devkit-open"
			if plain {
				tabs := make([]wtutil.TabSpec, 0, n)
				for i := 1; i <= n; i++ {
					tabs = append(tabs, wtutil.TabSpec{
						Title:   fmt.Sprintf("agent-%d", i),
						Command: mustBuildNativeWindowCmd(exe, project, repo, i, ""),
					})
				}
				if err := launchPlainTabs(dryRun, project, sess, tabs); err != nil {
					die(err.Error())
				}
			} else if !skipTmux() {
				cmd := mustBuildNativeWindowCmd(exe, project, repo, 1, "")
				runner.Host(dryRun, "tmux", tmuxutil.NewSession(sess, cmd)...)
				runner.Host(dryRun, "tmux", tmuxutil.RenameWindow(sess+":0", "agent-1")...)
				for i := 2; i <= n; i++ {
					wcmd := mustBuildNativeWindowCmd(exe, project, repo, i, "")
					runner.Host(dryRun, "tmux", tmuxutil.NewWindow(sess, fmt.Sprintf("agent-%d", i), wcmd)...)
				}
				runner.HostInteractive(dryRun, "tmux", tmuxutil.Attach(sess)...)
			}
			break
		}
	case "fresh-open":
		mustProject(project)
		if !isNativeRuntime {
			die("fresh-open requires runtime.flake")
		}
		n := 3
		plain := false
		for _, arg := range sub {
			if arg == "--plain" {
				plain = true
				continue
			}
			n = mustAtoi(arg)
		}
		repo := readDefaultRepo(project, paths)
		nativeLifecycleCommand(dryRun, exe, project, "down", repo, n)
		if !skipTmux() {
			killNativeAgentSessions(dryRun)
		}
		nativeLifecycleCommand(dryRun, exe, project, "up", repo, n)
		openNativeAgentSession(dryRun, exe, project, repo, "devkit-open", n, plain)
	case "reset":
		// Alias to fresh-open with identical behavior
		mustProject(project)
		if !isNativeRuntime {
			die("reset requires runtime.flake")
		}
		n := 3
		if len(sub) > 0 {
			n = mustAtoi(sub[0])
		}
		repo := readDefaultRepo(project, paths)
		nativeLifecycleCommand(dryRun, exe, project, "down", repo, n)
		if !skipTmux() {
			killNativeAgentSessions(dryRun)
		}
		nativeLifecycleCommand(dryRun, exe, project, "up", repo, n)
		openNativeAgentSession(dryRun, exe, project, repo, "devkit-open", n, false)
	case "ssh-setup":
		mustProject(project)
		if !isNativeRuntime {
			die("ssh-setup requires runtime.flake")
		}
		// Parse flags: [--key path] [--index N]
		idx := "1"
		keyfile := ""
		for i := 0; i < len(sub); i++ {
			switch sub[i] {
			case "--key":
				if i+1 < len(sub) {
					keyfile = sub[i+1]
					i++
				}
			case "--index":
				if i+1 < len(sub) {
					idx = sub[i+1]
					i++
				}
			default:
				if keyfile == "" {
					keyfile = sub[i]
				}
			}
		}
		repo := readDefaultRepo(project, paths)
		seedNativeSSH(dryRun, paths, project, repo, mustAtoi(idx), keyfile)
	case "ssh-test":
		mustProject(project)
		if project == "dev-all" {
			runner.Host(dryRun, "bash", "-lc", "ssh -T github.com -o BatchMode=yes || true")
			break
		}
		if !isNativeRuntime {
			die("ssh-test requires runtime.flake")
		}
		idx := "1"
		if len(sub) > 0 {
			idx = sub[0]
		}
		repo := readDefaultRepo(project, paths)
		resolved, err := nativeResolvedAgentPaths(paths, project, repo, mustAtoi(idx))
		if err != nil {
			die(err.Error())
		}
		cmd := "set -euo pipefail; HOME=" + shSingleQuote(resolved.HostHome) + "; cfg=\"$HOME/.ssh/config\"; if [ -s \"$cfg\" ]; then ssh -F \"$cfg\" -T github.com -o BatchMode=yes || true; else ssh -T github.com -o BatchMode=yes || true; fi"
		runner.Host(dryRun, "bash", "-lc", cmd)
	case "repo-config-ssh":
		mustProject(project)
		if !isNativeRuntime {
			die("repo-config-ssh requires runtime.flake")
		}
		if len(sub) < 1 {
			die("Usage: repo-config-ssh <repo-path> [--index N]")
		}
		repo := sub[0]
		idx := "1"
		if len(sub) >= 3 && sub[1] == "--index" {
			idx = sub[2]
		}
		repoName := nativeRepoForRepoArg(paths, project, repo)
		path := nativeHostWorktree(paths, project, repoName, mustAtoi(idx))
		cmd := "set -euo pipefail; cd " + shSingleQuote(path) + "; url=$(git remote get-url origin 2>/dev/null || true); if [ -z \"$url\" ]; then echo 'No origin remote configured' >&2; exit 1; fi; if [[ \"$url\" =~ ^https://github.com/([^/]+)/([^/.]+)(\\.git)?$ ]]; then newurl=git@github.com:${BASH_REMATCH[1]}/${BASH_REMATCH[2]}.git; echo Setting SSH origin to \"$newurl\"; git remote set-url origin \"$newurl\"; else echo \"Origin already SSH: $url\"; fi"
		runner.Host(dryRun, "bash", "-lc", cmd)
	case "repo-config-https":
		mustProject(project)
		if !isNativeRuntime {
			die("repo-config-https requires runtime.flake")
		}
		if len(sub) < 1 {
			die("Usage: repo-config-https <repo-path> [--index N]")
		}
		repo := sub[0]
		idx := "1"
		if len(sub) >= 3 && sub[1] == "--index" {
			idx = sub[2]
		}
		repoName := nativeRepoForRepoArg(paths, project, repo)
		path := nativeHostWorktree(paths, project, repoName, mustAtoi(idx))
		cmd := "set -euo pipefail; cd " + shSingleQuote(path) + "; url=$(git remote get-url origin 2>/dev/null || true); if [ -z \"$url\" ]; then echo 'No origin remote configured' >&2; exit 1; fi; if [[ \"$url\" =~ ^git@github.com:([^/]+)/([^/.]+)(\\.git)?$ ]]; then newurl=https://github.com/${BASH_REMATCH[1]}/${BASH_REMATCH[2]}.git; echo Setting HTTPS origin to \"$newurl\"; git remote set-url origin \"$newurl\"; else echo \"Origin already HTTPS: $url\"; fi"
		runner.Host(dryRun, "bash", "-lc", cmd)
	case "repo-push-ssh":
		mustProject(project)
		if !isNativeRuntime {
			die("repo-push-ssh requires runtime.flake")
		}
		if len(sub) < 1 {
			die("Usage: repo-push-ssh <repo-path> [--index N]")
		}
		repo := sub[0]
		idx := "1"
		for i := 1; i+1 < len(sub); i++ {
			if sub[i] == "--index" {
				idx = sub[i+1]
			}
		}
		repoName := nativeRepoForRepoArg(paths, project, repo)
		path := nativeHostWorktree(paths, project, repoName, mustAtoi(idx))
		cmd := "set -euo pipefail; cd " + shSingleQuote(path) + "; cur=$(git rev-parse --abbrev-ref HEAD); url=$(git remote get-url origin 2>/dev/null || true); if [ -z \"$url\" ]; then echo 'No origin remote configured' >&2; exit 1; fi; if [[ \"$url\" =~ ^https://github.com/([^/]+)/([^/.]+)(\\.git)?$ ]]; then newurl=git@github.com:${BASH_REMATCH[1]}/${BASH_REMATCH[2]}.git; echo Setting SSH origin to \"$newurl\"; git remote set-url origin \"$newurl\"; fi; echo Pushing branch \"$cur\" to origin...; git push -u origin HEAD"
		runner.Host(dryRun, "bash", "-lc", cmd)
	case "repo-push-https":
		mustProject(project)
		if !isNativeRuntime {
			die("repo-push-https requires runtime.flake")
		}
		if len(sub) < 1 {
			die("Usage: repo-push-https <repo-path> [--index N]")
		}
		repo := sub[0]
		idx := "1"
		if len(sub) >= 3 && sub[1] == "--index" {
			idx = sub[2]
		}
		repoName := nativeRepoForRepoArg(paths, project, repo)
		path := nativeHostWorktree(paths, project, repoName, mustAtoi(idx))
		runner.Host(dryRun, "bash", "-lc", "set -euo pipefail; cd "+shSingleQuote(path)+"; echo Pushing branch $(git rev-parse --abbrev-ref HEAD) to origin...; git push -u origin HEAD")
	case "worktrees-init":
		mustProject(project)
		if len(sub) < 2 {
			die("Usage: worktrees-init <repo> <count> [--base agent] [--branch main]")
		}
		repo := sub[0]
		count := sub[1]
		base := "agent"
		branch := "main"
		for i := 2; i+1 < len(sub); i++ {
			if sub[i] == "--base" {
				base = sub[i+1]
			} else if sub[i] == "--branch" {
				branch = sub[i+1]
			}
		}
		// create worktrees on host filesystem
		// primary at /workspaces/dev/<repo>, others at /workspaces/dev/agentN/<repo>
		// Here we just print guidance; actual creation may be outside scope.
		fmt.Printf("Initialize worktrees for %s: base=%s branch=%s (1..%s) on host (manual)\n", repo, base, branch, count)
	case "worktrees-setup":
		// Create per-agent branches and worktrees rooted in the dev root (dev-all overlay pattern)
		mustProject(project)
		if !isNativeRuntime {
			die("worktrees-setup requires runtime.flake")
		}
		if len(sub) < 2 {
			die("Usage: worktrees-setup <repo> <count> [--base agent] [--branch main]")
		}
		repo := sub[0]
		n := mustAtoi(sub[1])
		branchPrefix := "agent"
		baseBranch := "main"
		for i := 2; i+1 < len(sub); i++ {
			if sub[i] == "--base" {
				branchPrefix = sub[i+1]
			} else if sub[i] == "--branch" {
				baseBranch = sub[i+1]
			}
		}
		if err := wtx.SetupNative(wtx.NativeOptions{
			DevkitRoot:   paths.Root,
			Repo:         repo,
			Count:        n,
			BaseBranch:   baseBranch,
			BranchPrefix: branchPrefix,
			WorktreeRoot: nativeWorktreeRoot(paths, project),
			DryRun:       dryRun,
		}); err != nil {
			die(err.Error())
		}
	case "run":
		// Idempotent end-to-end launcher: ensures worktrees, scales up, and opens tmux across N agents
		mustProject(project)
		if !isNativeRuntime {
			die("run requires runtime.flake")
		}
		if len(sub) < 2 {
			die("Usage: run <repo> <count>")
		}
		repo := sub[0]
		n := mustAtoi(sub[1])
		if noSeed || reSeed {
			fmt.Fprintln(os.Stderr, "[run] native agents use per-agent state; --no-seed/--reseed are ignored")
		}
		nativeLifecycleCommand(dryRun, exe, project, "up", repo, n)
		sess := "devkit-worktrees"
		if !skipTmux() {
			runner.HostBestEffort(dryRun, "tmux", "kill-session", "-t", sess)
		}
		openNativeAgentSession(dryRun, exe, project, repo, sess, n, false)
	case "worktrees-branch":
		mustProject(project)
		if !isNativeRuntime {
			die("worktrees-branch requires runtime.flake")
		}
		if len(sub) < 3 {
			die("Usage: -p dev-all worktrees-branch <repo> <index> <branch>")
		}
		repo := sub[0]
		idx := sub[1]
		branch := sub[2]
		path := nativeHostWorktree(paths, project, repo, mustAtoi(idx))
		runner.Host(dryRun, "git", "-C", path, "checkout", "-b", branch)
	case "worktrees-status":
		mustProject(project)
		if !isNativeRuntime {
			die("worktrees-status requires runtime.flake")
		}
		if len(sub) < 1 {
			die("Usage: -p dev-all worktrees-status <repo> [--all|--index N]")
		}
		repo := sub[0]
		idx := ""
		if len(sub) >= 3 && sub[1] == "--index" {
			idx = sub[2]
		}
		if idx != "" {
			path := nativeHostWorktree(paths, project, repo, mustAtoi(idx))
			runner.Host(dryRun, "git", "-C", path, "status", "-sb")
		} else {
			count := nativeDefaultAgentCount(paths, project, 2)
			for i := 1; i <= count; i++ {
				path := nativeHostWorktree(paths, project, repo, i)
				if dryRun {
					runner.Host(dryRun, "git", "-C", path, "status", "-sb")
					continue
				}
				if _, err := os.Stat(path); err == nil {
					runner.Host(false, "git", "-C", path, "status", "-sb")
				}
			}
		}
	case "worktrees-sync":
		mustProject(project)
		if !isNativeRuntime {
			die("worktrees-sync requires runtime.flake")
		}
		if len(sub) < 2 {
			die("Usage: worktrees-sync <repo> (--pull|--push) [--all|--index N]")
		}
		repo := sub[0]
		op := sub[1]
		idx := ""
		if len(sub) >= 4 && sub[2] == "--index" {
			idx = sub[3]
		}
		gitcmd := []string{"pull", "--ff-only"}
		if op == "--push" {
			gitcmd = []string{"push", "origin", "HEAD:main"}
		} else if op != "--pull" {
			die("Usage: worktrees-sync <repo> (--pull|--push) [--all|--index N]")
		}
		if idx != "" {
			path := nativeHostWorktree(paths, project, repo, mustAtoi(idx))
			runner.Host(dryRun, "git", append([]string{"-C", path}, gitcmd...)...)
		} else {
			count := nativeDefaultAgentCount(paths, project, 6)
			for i := 1; i <= count; i++ {
				path := nativeHostWorktree(paths, project, repo, i)
				if dryRun {
					runner.Host(dryRun, "git", append([]string{"-C", path}, gitcmd...)...)
					continue
				}
				if _, err := os.Stat(path); err == nil {
					runner.Host(false, "git", append([]string{"-C", path}, gitcmd...)...)
				}
			}
		}
	case "worktrees-tmux":
		mustProject(project)
		if !isNativeRuntime {
			die("worktrees-tmux requires runtime.flake")
		}
		if len(sub) < 2 {
			die("Usage: -p dev-all worktrees-tmux <repo> <count> [--plain]")
		}
		repo := sub[0]
		n := mustAtoi(sub[1])
		plain := hasArgFlag(sub[2:], "--plain")
		sess := "devkit-worktrees"
		if plain {
			tabs := make([]wtutil.TabSpec, 0, n)
			for i := 1; i <= n; i++ {
				tabs = append(tabs, wtutil.TabSpec{
					Title:   fmt.Sprintf("agent-%d", i),
					Command: mustBuildNativeWindowCmd(exe, project, repo, i, ""),
				})
			}
			if err := launchPlainTabs(dryRun, project, sess, tabs); err != nil {
				die(err.Error())
			}
		} else if !skipTmux() {
			cmd := mustBuildNativeWindowCmd(exe, project, repo, 1, "")
			runner.Host(dryRun, "tmux", tmuxutil.NewSession(sess, cmd)...)
			runner.Host(dryRun, "tmux", tmuxutil.RenameWindow(sess+":0", "agent-1")...)
			for i := 2; i <= n; i++ {
				wcmd := mustBuildNativeWindowCmd(exe, project, repo, i, "")
				runner.Host(dryRun, "tmux", tmuxutil.NewWindow(sess, fmt.Sprintf("agent-%d", i), wcmd)...)
			}
			// tmux attach is long-lived: no timeout
			runner.HostInteractive(dryRun, "tmux", tmuxutil.Attach(sess)...)
		}
	case "bootstrap":
		// Opinionated: set up worktrees and open tmux with defaults if args omitted
		mustProject(project)
		if !isNativeRuntime {
			die("bootstrap requires runtime.flake")
		}
		var repo string
		var n int
		if len(sub) >= 2 {
			repo = sub[0]
			n = mustAtoi(sub[1])
		} else {
			// Try overlay defaults
			cfg, _, _ := config.ReadAll(paths.OverlayPaths, project)
			if strings.TrimSpace(cfg.Defaults.Repo) == "" || cfg.Defaults.Agents < 1 {
				die("Usage: -p dev-all bootstrap <repo> <count> (or set defaults in overlays/dev-all/devkit.yaml)")
			}
			repo = cfg.Defaults.Repo
			n = cfg.Defaults.Agents
		}
		nativeLifecycleCommand(dryRun, exe, project, "up", repo, n)
		sess := "devkit-worktrees"
		if !skipTmux() {
			runner.HostBestEffort(dryRun, "tmux", "kill-session", "-t", sess)
		}
		openNativeAgentSession(dryRun, exe, project, repo, sess, n, false)
	default:
		usage()
		os.Exit(2)
	}
}

func die(msg string) { fmt.Fprintln(os.Stderr, msg); os.Exit(2) }

func requiresRuntimeFlakeCommand(cmd string) bool {
	switch cmd {
	case "codex-auth", "creds", "codex-debug", "codex-test", "doctor-runtime", "verify",
		"check-sts", "exec-cd", "attach-cd", "tmux-shells", "open", "fresh-open",
		"reset", "ssh-setup", "ssh-test", "repo-config-ssh", "repo-config-https",
		"repo-push-ssh", "repo-push-https", "worktrees-setup", "run",
		"worktrees-branch", "worktrees-status", "worktrees-sync", "worktrees-tmux",
		"bootstrap":
		return true
	default:
		return false
	}
}

func nativeRuntimeConfigured(project string, cfg config.OverlayConfig) bool {
	if strings.TrimSpace(project) == "" {
		return false
	}
	return config.HasRuntimeFlake(cfg)
}

func mustProject(p string) {
	if strings.TrimSpace(p) == "" {
		die("-p <project> is required")
	}
}

func skipTmux() bool {
	if tmuxForceOverride {
		return false
	}
	return os.Getenv("DEVKIT_NO_TMUX") == "1"
}
func mustAtoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		die("count must be a positive integer")
	}
	return n
}

// defaultSessionName chooses a stable default tmux session per overlay.
func defaultSessionName(project string) string {
	p := strings.TrimSpace(project)
	if p == "" {
		p = "layout"
	}
	return "devkit:" + p
}

// hasTmuxSession returns true if the tmux session exists.
func hasTmuxSession(session string) bool {
	// Use Capture to avoid printing benign tmux errors like
	// "no server running on /tmp/tmux-<uid>/default" when probing.
	_, res := execx.Capture(context.Background(), "tmux", tmuxutil.HasSession(session)...)
	return res.Code == 0
}

func resolveWTBinary() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("DEVKIT_WT_PATH")); configured != "" {
		if _, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("DEVKIT_WT_PATH %s not accessible: %w", configured, err)
		}
		return configured, nil
	}
	for _, candidate := range []string{"wt.exe", "wt"} {
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("Windows Terminal not found. On WSL, either add wt.exe to PATH or set DEVKIT_WT_PATH to the full wt.exe path")
}

func resolveWSLBinary() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("DEVKIT_WSL_PATH")); configured != "" {
		if _, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("DEVKIT_WSL_PATH %s not accessible: %w", configured, err)
		}
		return configured, nil
	}
	for _, candidate := range []string{"wsl.exe", "/mnt/c/Windows/System32/wsl.exe", "/mnt/c/Windows/system32/wsl.exe"} {
		if filepath.IsAbs(candidate) {
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
			continue
		}
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("wsl.exe not found. Set DEVKIT_WSL_PATH to the full wsl.exe path")
}

func resolveWSLDistro() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("DEVKIT_WSL_DISTRO")); configured != "" {
		return configured, nil
	}
	if detected := strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME")); detected != "" {
		return detected, nil
	}
	return "", fmt.Errorf("WSL distro not detected. Set DEVKIT_WSL_DISTRO explicitly")
}

func launchPlainTabs(dry bool, project, session string, tabs []wtutil.TabSpec) error {
	wtBinary, err := resolveWTBinary()
	if err != nil {
		return err
	}
	wslBinary, err := resolveWSLBinary()
	if err != nil {
		return err
	}
	wslDistro, err := resolveWSLDistro()
	if err != nil {
		return err
	}
	windowName := wtutil.ViewerWindowName(session)
	args := wtutil.NewTabsArgs(windowName, wslBinary, wslDistro, tabs)
	if dry {
		fmt.Fprintln(os.Stderr, "+ "+wtBinary+" "+strings.Join(args, " "))
		return nil
	}
	runCtx, cancel := execx.WithTimeout(2 * time.Minute)
	res := execx.RunCtx(runCtx, wtBinary, args...)
	cancel()
	if res.Code != 0 {
		return fmt.Errorf("wt tab launch failed for session %s", session)
	}
	return nil
}

// listTmuxWindows returns a set of window names for a session.
func listTmuxWindows(session string) map[string]struct{} {
	out, r := execx.Capture(context.Background(), "tmux", tmuxutil.ListWindows(session)...)
	if r.Code != 0 {
		return map[string]struct{}{}
	}
	m := map[string]struct{}{}
	for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
		s := strings.TrimSpace(ln)
		if s != "" {
			m[s] = struct{}{}
		}
	}
	return m
}

func mustBuildNativeWindowCmd(exe, project, repo string, index int, dest string) string {
	cmd, err := agentexec.BuildNativeCommand(agentexec.NativeCommandOpts{
		Exe:     exe,
		Project: project,
		Index:   fmt.Sprintf("%d", index),
		Repo:    repo,
		Dest:    dest,
	})
	if err != nil {
		die(err.Error())
	}
	return cmd
}

func nativeLifecycleCommand(dry bool, exe, project, command, repo string, count int, extra ...string) {
	if count < 1 {
		count = 1
	}
	args := []string{"-p", project, command, "--repo", repo, "--count", fmt.Sprintf("%d", count)}
	args = append(args, extra...)
	runner.Host(dry, exe, args...)
}

func nativeExecScriptCommand(dry bool, exe, project, repo string, index int, script string) {
	if index < 1 {
		index = 1
	}
	runner.Host(dry, exe, "-p", project, "exec", fmt.Sprintf("%d", index), "--repo", repo, "--", "bash", "-lc", script)
}

func nativeExecScriptCommandInteractive(dry bool, exe, project, repo string, index int, script string) {
	if index < 1 {
		index = 1
	}
	runner.HostInteractive(dry, exe, "-p", project, "exec", fmt.Sprintf("%d", index), "--repo", repo, "--", "bash", "-lc", script)
}

func killNativeAgentSessions(dry bool) {
	for _, sess := range []string{"devkit-open", "devkit-shells", "devkit-worktrees"} {
		runner.HostBestEffort(dry, "tmux", "kill-session", "-t", sess)
	}
}

func openNativeAgentSession(dry bool, exe, project, repo, sess string, count int, plain bool) {
	if count < 1 {
		count = 1
	}
	if plain {
		tabs := make([]wtutil.TabSpec, 0, count)
		for i := 1; i <= count; i++ {
			tabs = append(tabs, wtutil.TabSpec{
				Title:   fmt.Sprintf("agent-%d", i),
				Command: mustBuildNativeWindowCmd(exe, project, repo, i, ""),
			})
		}
		if err := launchPlainTabs(dry, project, sess, tabs); err != nil {
			die(err.Error())
		}
		return
	}
	if skipTmux() {
		return
	}
	cmd := mustBuildNativeWindowCmd(exe, project, repo, 1, "")
	runner.Host(dry, "tmux", tmuxutil.NewSession(sess, cmd)...)
	runner.Host(dry, "tmux", tmuxutil.RenameWindow(sess+":0", "agent-1")...)
	for i := 2; i <= count; i++ {
		wcmd := mustBuildNativeWindowCmd(exe, project, repo, i, "")
		runner.Host(dry, "tmux", tmuxutil.NewWindow(sess, fmt.Sprintf("agent-%d", i), wcmd)...)
	}
	runner.HostInteractive(dry, "tmux", tmuxutil.Attach(sess)...)
}

func nativeDefaultAgentCount(paths devkitpaths.Paths, project string, fallback int) int {
	cfg, _, err := config.ReadAll(paths.OverlayPaths, project)
	if err == nil && cfg.Defaults.Agents > 0 {
		return cfg.Defaults.Agents
	}
	if fallback < 1 {
		return 1
	}
	return fallback
}

func nativeWorktreeRoot(paths devkitpaths.Paths, project string) string {
	cfg, _, err := config.ReadAll(paths.OverlayPaths, project)
	if err == nil {
		value := strings.TrimSpace(cfg.Native.WorktreeRoot)
		if value != "" {
			if filepath.IsAbs(value) {
				return filepath.Clean(value)
			}
			return filepath.Clean(filepath.Join(paths.Root, value))
		}
	}
	return filepath.Clean(filepath.Join(paths.Root, "..", pth.AgentWorktreesDir))
}

func nativeHostWorktree(paths devkitpaths.Paths, project, repo string, index int) string {
	if index < 1 {
		index = 1
	}
	return filepath.Join(nativeWorktreeRoot(paths, project), fmt.Sprintf("agent%d", index), repo)
}

func nativeResolvedAgentPaths(paths devkitpaths.Paths, project, repo string, index int) (nativeagent.Paths, error) {
	cfg, _, _ := config.ReadAll(paths.OverlayPaths, project)
	return nativeagent.ResolvePaths(nativeagent.PathConfig{
		DevkitRoot:            paths.Root,
		Project:               project,
		Repo:                  repo,
		Index:                 index,
		WorktreeRoot:          nativeWorktreeRoot(paths, project),
		StateRoot:             nativeStateRoot(paths, project),
		WorktreeContainerRoot: cfg.Native.WorktreeContainerRoot,
		StateContainerRoot:    cfg.Native.StateContainerRoot,
		DedicatedWorktree:     true,
	})
}

func seedNativeCodexAuth(dry bool, paths devkitpaths.Paths, project, repo string, index int) {
	resolved, err := nativeResolvedAgentPaths(paths, project, repo, index)
	if err != nil {
		die(err.Error())
	}
	if dry {
		fmt.Fprintf(os.Stderr, "+ seed codex auth %s\n", filepath.Join(resolved.HostHome, ".codex", "auth.json"))
		return
	}
	if err := nativelaunch.SeedCodexAuth(resolved.HostHome, true); err != nil {
		die(err.Error())
	}
}

func seedNativeSSH(dry bool, paths devkitpaths.Paths, project, repo string, index int, keyfile string) {
	resolved, err := nativeResolvedAgentPaths(paths, project, repo, index)
	if err != nil {
		die(err.Error())
	}
	sshDir := filepath.Join(resolved.HostHome, ".ssh")
	if dry {
		fmt.Fprintf(os.Stderr, "+ seed ssh %s\n", sshDir)
		if strings.TrimSpace(keyfile) != "" {
			fmt.Fprintf(os.Stderr, "+ seed ssh key %s -> %s\n", keyfile, filepath.Join(sshDir, filepath.Base(keyfile)))
		}
		return
	}
	if strings.TrimSpace(keyfile) == "" {
		if err := nativelaunch.SeedSSH(resolved.HostHome, true); err != nil {
			die(err.Error())
		}
		if err := writeNativeSSHConfig(resolved.HostHome, nil); err != nil {
			die(err.Error())
		}
		return
	}
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		die(err.Error())
	}
	keyName := filepath.Base(keyfile)
	if err := copyNativeSSHFile(keyfile, filepath.Join(sshDir, keyName), 0o600); err != nil {
		die(err.Error())
	}
	if _, err := os.Stat(keyfile + ".pub"); err == nil {
		if err := copyNativeSSHFile(keyfile+".pub", filepath.Join(sshDir, keyName+".pub"), 0o644); err != nil {
			die(err.Error())
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		known := filepath.Join(home, ".ssh", "known_hosts")
		if _, err := os.Stat(known); err == nil {
			if err := copyNativeSSHFile(known, filepath.Join(sshDir, "known_hosts"), 0o644); err != nil {
				die(err.Error())
			}
		}
	}
	if err := writeNativeSSHConfig(resolved.HostHome, []string{keyName}); err != nil {
		die(err.Error())
	}
}

func copyNativeSSHFile(src, dest string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.WriteFile(dest, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	return nil
}

func writeNativeSSHConfig(hostHome string, identityNames []string) error {
	hostHome = strings.TrimSpace(hostHome)
	if hostHome == "" {
		return nil
	}
	sshDir := filepath.Join(hostHome, ".ssh")
	if len(identityNames) == 0 {
		for _, name := range []string{"id_ed25519", "id_rsa"} {
			if _, err := os.Stat(filepath.Join(sshDir, name)); err == nil {
				identityNames = append(identityNames, name)
			}
		}
	}
	if len(identityNames) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("Host github.com\n")
	b.WriteString("  HostName github.com\n")
	b.WriteString("  User git\n")
	for _, name := range identityNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		b.WriteString("  IdentityFile " + filepath.Join(hostHome, ".ssh", name) + "\n")
	}
	b.WriteString("  IdentitiesOnly yes\n")
	b.WriteString("  StrictHostKeyChecking accept-new\n")
	b.WriteString("  UserKnownHostsFile " + filepath.Join(hostHome, ".ssh", "known_hosts") + "\n")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", sshDir, err)
	}
	target := filepath.Join(sshDir, "config")
	if err := os.WriteFile(target, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return nil
}

func nativeStateRoot(paths devkitpaths.Paths, project string) string {
	cfg, _, err := config.ReadAll(paths.OverlayPaths, project)
	if err == nil {
		value := strings.TrimSpace(cfg.Native.StateRoot)
		if value != "" {
			if filepath.IsAbs(value) {
				return filepath.Clean(value)
			}
			return filepath.Clean(filepath.Join(paths.Root, value))
		}
	}
	return filepath.Clean(filepath.Join(paths.Root, "..", ".devkit", "native-agents"))
}

func nativeRepoForRepoArg(paths devkitpaths.Paths, project, repoArg string) string {
	cleaned := filepath.Clean(strings.TrimSpace(repoArg))
	if cleaned == "." || cleaned == "" {
		return readDefaultRepo(project, paths)
	}
	if !strings.HasPrefix(cleaned, "/") && !strings.Contains(cleaned, string(filepath.Separator)) {
		return cleaned
	}
	return nativeRepoForSubpath(paths, project, repoArg)
}

func nativeRepoForSubpath(paths devkitpaths.Paths, project, subpath string) string {
	def := readDefaultRepo(project, paths)
	cleaned := filepath.Clean(strings.TrimSpace(subpath))
	if cleaned == "." || cleaned == "" {
		return def
	}
	for _, prefix := range []string{"/worktrees/", "/workspaces/dev/agent-worktrees/"} {
		if strings.HasPrefix(cleaned, prefix) {
			rel := strings.TrimPrefix(cleaned, prefix)
			parts := strings.Split(rel, "/")
			if len(parts) >= 2 && strings.HasPrefix(parts[0], "agent") {
				return parts[1]
			}
		}
	}
	if strings.HasPrefix(cleaned, "/workspaces/dev/") {
		rel := strings.TrimPrefix(cleaned, "/workspaces/dev/")
		parts := strings.Split(rel, "/")
		if len(parts) >= 1 && strings.TrimSpace(parts[0]) != "" {
			return parts[0]
		}
	}
	if !strings.HasPrefix(cleaned, "/") {
		parts := strings.Split(cleaned, string(filepath.Separator))
		if len(parts) > 1 && strings.TrimSpace(parts[0]) != "" {
			devRootRepo := filepath.Join(paths.Root, "..", parts[0])
			if info, err := os.Stat(devRootRepo); err == nil && info.IsDir() {
				return parts[0]
			}
		}
	}
	return def
}

func nativeSandboxDest(index int, repo, subpath string) string {
	if index < 1 {
		index = 1
	}
	base := filepath.Join("/workspaces/dev/agent-worktrees", fmt.Sprintf("agent%d", index), repo)
	if index == 1 {
		base = filepath.Join("/workspaces/dev", repo)
	}
	cleaned := filepath.Clean(strings.TrimSpace(subpath))
	if cleaned == "." || cleaned == "" {
		return base
	}
	if !strings.HasPrefix(cleaned, "/") {
		parts := strings.Split(cleaned, string(filepath.Separator))
		if len(parts) >= 3 && parts[0] == pth.AgentWorktreesDir && strings.HasPrefix(parts[1], "agent") && parts[2] == repo {
			return filepath.Join(append([]string{"/workspaces/dev", parts[0], parts[1], parts[2]}, parts[3:]...)...)
		}
		if len(parts) > 1 && parts[0] == repo {
			return filepath.Join(append([]string{base}, parts[1:]...)...)
		}
		return filepath.Join(base, cleaned)
	}
	if strings.HasPrefix(cleaned, "/worktrees/") {
		return cleaned
	}
	devPrefix := "/workspaces/dev/"
	if strings.HasPrefix(cleaned, devPrefix) {
		rel := strings.TrimPrefix(cleaned, devPrefix)
		parts := strings.Split(rel, "/")
		if len(parts) >= 3 && parts[0] == pth.AgentWorktreesDir && strings.HasPrefix(parts[1], "agent") {
			return cleaned
		}
		if len(parts) >= 1 && parts[0] == repo {
			return filepath.Join(append([]string{base}, parts[1:]...)...)
		}
	}
	return cleaned
}

func layoutIsNativeRuntime(paths devkitpaths.Paths, lf layout.File, defaultProject string) bool {
	defaultProject = strings.TrimSpace(defaultProject)
	if defaultProject == "" {
		return false
	}
	defaultCfg, _, err := config.ReadAll(paths.OverlayPaths, defaultProject)
	if err != nil || !config.HasRuntimeFlake(defaultCfg) {
		return false
	}
	for _, ov := range lf.Overlays {
		proj := strings.TrimSpace(ov.Project)
		if proj == "" {
			proj = defaultProject
		}
		if proj != defaultProject {
			return false
		}
		if svc := strings.TrimSpace(ov.Service); svc != "" && svc != "dev-agent" {
			return false
		}
	}
	for _, w := range lf.Windows {
		proj := strings.TrimSpace(w.Project)
		if proj == "" {
			proj = defaultProject
		}
		if proj != defaultProject {
			return false
		}
		if svc := strings.TrimSpace(w.Service); svc != "" && svc != "dev-agent" {
			return false
		}
	}
	return true
}

func layoutNativeRepoAndCount(paths devkitpaths.Paths, project string, lf layout.File) (string, int) {
	cfg, _, _ := config.ReadAll(paths.OverlayPaths, project)
	repo := strings.TrimSpace(cfg.Defaults.Repo)
	count := cfg.Defaults.Agents
	for _, ov := range lf.Overlays {
		if ov.Worktrees != nil {
			if strings.TrimSpace(ov.Worktrees.Repo) != "" {
				repo = strings.TrimSpace(ov.Worktrees.Repo)
			}
			if ov.Worktrees.Count > count {
				count = ov.Worktrees.Count
			}
		}
		if ov.Count > count {
			count = ov.Count
		}
	}
	for _, w := range lf.Windows {
		if w.Index > count {
			count = w.Index
		}
	}
	if repo == "" {
		repo = readDefaultRepo(project, paths)
	}
	if count < 1 {
		count = 1
	}
	return repo, count
}

func hasArgFlag(args []string, flag string) bool {
	flag = strings.TrimSpace(flag)
	if flag == "" {
		return false
	}
	for _, a := range args {
		if strings.TrimSpace(a) == flag {
			return true
		}
	}
	return false
}

func readDefaultRepo(project string, paths devkitpaths.Paths) string {
	project = strings.TrimSpace(project)
	if cfg, _, err := config.ReadAll(paths.OverlayPaths, project); err == nil {
		if strings.TrimSpace(cfg.Defaults.Repo) != "" {
			return strings.TrimSpace(cfg.Defaults.Repo)
		}
	}
	if project == "dev-all" || project == "" {
		return "ouroboros-ide"
	}
	return project
}

func gitIdentityForRepoCommand(home, repoPath, name, email string) string {
	return fmt.Sprintf(
		`set -e; if [ -d %[1]s/.git ] || git -C %[1]s rev-parse --git-dir >/dev/null 2>&1; then git -C %[1]s config extensions.worktreeConfig true; git -C %[1]s config --worktree user.name %[3]s; git -C %[1]s config --worktree user.email %[4]s; git -C %[1]s config --worktree core.sshCommand %[2]s; fi`,
		shSingleQuote(repoPath),
		shSingleQuote("ssh -F "+filepath.Join(home, ".ssh", "config")),
		shSingleQuote(name),
		shSingleQuote(email),
	)
}

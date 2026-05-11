package tmuxcmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	agentexec "devkit/cli/devctl/internal/agentexec"
	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/compose"
	"devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/execx"
	"devkit/cli/devctl/internal/layout"
	pth "devkit/cli/devctl/internal/paths"
	runner "devkit/cli/devctl/internal/runner"
	"devkit/cli/devctl/internal/tmuxnotify"
	"devkit/cli/devctl/internal/tmuxutil"
	"devkit/cli/devctl/internal/wtutil"
)

// Register adds tmux-related commands to the registry. Helpers from the legacy
// main.go are injected so we can reuse them without moving everything at once.
func Register(
	r *cmdregistry.Registry,
	syncFn func(bool, compose.Paths, string, []string, string, string, string, int, string),
	ensureWindowFn func(bool, compose.Paths, string, []string, string, string, string, string, string),
	defaultSessionFn func(string) string,
	atoiFn func(string) int,
	listFn func([]string, string) []string,
	buildFn func([]string, string, string, string, string, string, *agentexec.SeedTracker) (string, error),
	trackerFn func() *agentexec.SeedTracker,
	hasFn func(string) bool,
) {
	doSyncTmux = syncFn
	ensureTmuxSessionWithWindow = ensureWindowFn
	defaultSessionName = defaultSessionFn
	mustAtoi = atoiFn
	listServiceNames = listFn
	buildWindowCmd = buildFn
	newSeedTracker = trackerFn
	hasTmuxSession = hasFn

	r.Register("tmux-sync", handleSync)
	r.Register("tmux-add-cd", handleAddCD)
	r.Register("tmux-apply-layout", handleApplyLayout)
	r.Register("wt-open", handleWTOpen)
	r.Register("wt-release", handleWTRelease)
	r.Register("tmux-bell-install", handleTMUXBellInstall)
	r.Register("tmux-bell-show-config", handleTMUXBellShowConfig)
	r.Register("tmux-notify-bell", handleTMUXNotifyBell)
}

func handleSync(ctx *cmdregistry.Context) error {
	if err := ensureProject(ctx); err != nil {
		return err
	}
	sessName := ""
	count := 0
	namePrefix := "agent-"
	cdPath := ""
	service := "dev-agent"
	for i := 0; i < len(ctx.Args); i++ {
		switch ctx.Args[i] {
		case "--session":
			if i+1 < len(ctx.Args) {
				sessName = ctx.Args[i+1]
				i++
			}
		case "--count":
			if i+1 < len(ctx.Args) {
				count = mustAtoi(ctx.Args[i+1])
				i++
			}
		case "--name-prefix":
			if i+1 < len(ctx.Args) {
				namePrefix = ctx.Args[i+1]
				i++
			}
		case "--cd":
			if i+1 < len(ctx.Args) {
				cdPath = ctx.Args[i+1]
				i++
			}
		case "--service":
			if i+1 < len(ctx.Args) {
				service = ctx.Args[i+1]
				i++
			}
		}
	}
	if count <= 0 {
		if ctx.Project == "dev-all" {
			cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, ctx.Project)
			if err != nil {
				return err
			}
			count = cfg.Defaults.Agents
			if count <= 0 {
				return fmt.Errorf("tmux-sync requires --count for native dev-all when defaults.agents is unset")
			}
		}
	}
	if ctx.Project == "dev-all" {
		return nativeSyncTmux(ctx, sessName, namePrefix, cdPath, count)
	}
	if count <= 0 {
		count = len(listServiceNames(ctx.Files, service))
		if count == 0 {
			return fmt.Errorf("no dev-agent containers running; use up/scale first or provide --count")
		}
	}
	doSyncTmux(ctx.DryRun, ctx.Paths, ctx.Project, ctx.Files, sessName, namePrefix, cdPath, count, service)
	return nil
}

func handleAddCD(ctx *cmdregistry.Context) error {
	if err := ensureProject(ctx); err != nil {
		return err
	}
	if len(ctx.Args) < 2 {
		return fmt.Errorf("Usage: tmux-add-cd <index> <subpath> [--session NAME] [--name NAME] [--service NAME]")
	}
	idx := ctx.Args[0]
	subpath := ctx.Args[1]
	sessName := ""
	winName := ""
	service := "dev-agent"
	for i := 2; i < len(ctx.Args); i++ {
		switch ctx.Args[i] {
		case "--session":
			if i+1 < len(ctx.Args) {
				sessName = ctx.Args[i+1]
				i++
			}
		case "--name":
			if i+1 < len(ctx.Args) {
				winName = ctx.Args[i+1]
				i++
			}
		case "--service":
			if i+1 < len(ctx.Args) {
				service = ctx.Args[i+1]
				i++
			}
		}
	}
	if sessName == "" {
		sessName = defaultSessionName(ctx.Project)
	}
	if ctx.Project == "dev-all" {
		return nativeEnsureTmuxSessionWithWindow(ctx, sessName, idx, subpath, winName)
	}
	ensureTmuxSessionWithWindow(ctx.DryRun, ctx.Paths, ctx.Project, ctx.Files, sessName, idx, subpath, winName, service)
	return nil
}

func handleApplyLayout(ctx *cmdregistry.Context) error {
	if err := ensureProject(ctx); err != nil {
		return err
	}
	layoutPath := ""
	sessName := ""
	doAttach := false
	for i := 0; i < len(ctx.Args); i++ {
		switch ctx.Args[i] {
		case "--file":
			if i+1 < len(ctx.Args) {
				layoutPath = ctx.Args[i+1]
				i++
			}
		case "--session":
			if i+1 < len(ctx.Args) {
				sessName = ctx.Args[i+1]
				i++
			}
		case "--attach":
			doAttach = true
		}
	}
	if strings.TrimSpace(layoutPath) == "" {
		return fmt.Errorf("Usage: tmux-apply-layout --file <layout.yaml> [--session NAME]")
	}
	lf, err := layout.Read(layoutPath)
	if err != nil {
		return err
	}
	if sessName == "" {
		if strings.TrimSpace(lf.Session) != "" {
			sessName = lf.Session
		} else {
			sessName = defaultSessionName(ctx.Project)
		}
	}
	if nativeLayoutEligible(ctx.Project, lf) {
		return nativeApplyLayout(ctx, lf, sessName, doAttach)
	}
	sessExists := hasTmuxSession(sessName)
	tracker := newSeedTracker()
	if tracker == nil {
		tracker = agentexec.NewSeedTracker()
	}
	if !sessExists && len(lf.Windows) > 0 {
		w := lf.Windows[0]
		idx := fmt.Sprintf("%d", w.Index)
		winProj := ctx.Project
		if strings.TrimSpace(w.Project) != "" {
			winProj = w.Project
		}
		fargs, err := compose.Files(ctx.Paths, winProj, ctx.Profile)
		if err != nil {
			return err
		}
		dest := layout.CleanPath(winProj, w.Path)
		svc := w.Service
		if strings.TrimSpace(svc) == "" {
			svc = "dev-agent"
		}
		name := w.Name
		if strings.TrimSpace(name) == "" {
			name = "agent-" + idx
		}
		composeProject := strings.TrimSpace(w.ComposeProject)
		cmdStr, err := buildWindowCmd(fargs, winProj, idx, dest, svc, composeProject, tracker)
		if err != nil {
			return err
		}
		runner.Host(ctx.DryRun, "tmux", tmuxutil.NewSession(sessName, cmdStr)...)
		runner.Host(ctx.DryRun, "tmux", tmuxutil.RenameWindow(sessName+":0", name)...)
		sessExists = true
	}
	start := 0
	if sessExists && len(lf.Windows) > 0 {
		start = 1
	}
	for _, w := range lf.Windows[start:] {
		idx := fmt.Sprintf("%d", w.Index)
		winProj := ctx.Project
		if strings.TrimSpace(w.Project) != "" {
			winProj = w.Project
		}
		fargs, err := compose.Files(ctx.Paths, winProj, ctx.Profile)
		if err != nil {
			return err
		}
		dest := layout.CleanPath(winProj, w.Path)
		svc := w.Service
		if strings.TrimSpace(svc) == "" {
			svc = "dev-agent"
		}
		name := w.Name
		if strings.TrimSpace(name) == "" {
			name = "agent-" + idx
		}
		composeProject := strings.TrimSpace(w.ComposeProject)
		cmdStr, err := buildWindowCmd(fargs, winProj, idx, dest, svc, composeProject, tracker)
		if err != nil {
			return err
		}
		runner.Host(ctx.DryRun, "tmux", tmuxutil.NewWindow(sessName, name, cmdStr)...)
	}
	if doAttach {
		runner.HostInteractive(ctx.DryRun, "tmux", tmuxutil.Attach(sessName)...)
	}
	return nil
}

func nativeSyncTmux(ctx *cmdregistry.Context, session, namePrefix, cdPath string, count int) error {
	if session == "" {
		session = defaultSessionName(ctx.Project)
	}
	if namePrefix == "" {
		namePrefix = "agent-"
	}
	present := map[string]struct{}{}
	sessExists := hasTmuxSession(session)
	if sessExists {
		present = listTmuxWindows(session)
	}
	start := 1
	if !sessExists {
		idx := "1"
		cmdStr, err := nativeWindowCommand(ctx, idx, cdPath)
		if err != nil {
			return err
		}
		runner.Host(ctx.DryRun, "tmux", tmuxutil.NewSession(session, cmdStr)...)
		runner.Host(ctx.DryRun, "tmux", tmuxutil.RenameWindow(session+":0", namePrefix+idx)...)
		present[namePrefix+idx] = struct{}{}
		start = 2
	}
	for i := start; i <= count; i++ {
		idx := fmt.Sprintf("%d", i)
		wname := namePrefix + idx
		if _, ok := present[wname]; ok {
			continue
		}
		cmdStr, err := nativeWindowCommand(ctx, idx, cdPath)
		if err != nil {
			return err
		}
		runner.Host(ctx.DryRun, "tmux", tmuxutil.NewWindow(session, wname, cmdStr)...)
	}
	return nil
}

func listTmuxWindows(session string) map[string]struct{} {
	out := map[string]struct{}{}
	tabs, err := tmuxWindowTabs(session)
	if err != nil {
		return out
	}
	for _, tab := range tabs {
		name := strings.TrimSpace(tab.Title)
		if name != "" {
			out[name] = struct{}{}
		}
	}
	return out
}

func nativeEnsureTmuxSessionWithWindow(ctx *cmdregistry.Context, session, idx, subpath, winName string) error {
	if winName == "" {
		winName = "agent-" + idx
	}
	cmdStr, err := nativeWindowCommand(ctx, idx, subpath)
	if err != nil {
		return err
	}
	if !hasTmuxSession(session) {
		runner.Host(ctx.DryRun, "tmux", tmuxutil.NewSession(session, cmdStr)...)
		runner.Host(ctx.DryRun, "tmux", tmuxutil.RenameWindow(session+":0", winName)...)
		runner.HostInteractive(ctx.DryRun, "tmux", tmuxutil.Attach(session)...)
		return nil
	}
	runner.Host(ctx.DryRun, "tmux", tmuxutil.NewWindow(session, winName, cmdStr)...)
	return nil
}

func nativeLayoutEligible(defaultProject string, lf layout.File) bool {
	for _, w := range lf.Windows {
		winProj := strings.TrimSpace(w.Project)
		if winProj == "" {
			winProj = defaultProject
		}
		if winProj != "dev-all" {
			return false
		}
		if svc := strings.TrimSpace(w.Service); svc != "" && svc != "dev-agent" {
			return false
		}
	}
	return strings.TrimSpace(defaultProject) == "dev-all"
}

func nativeApplyLayout(ctx *cmdregistry.Context, lf layout.File, session string, attach bool) error {
	sessExists := hasTmuxSession(session)
	start := 0
	if !sessExists && len(lf.Windows) > 0 {
		w := lf.Windows[0]
		idx := fmt.Sprintf("%d", w.Index)
		name := w.Name
		if strings.TrimSpace(name) == "" {
			name = "agent-" + idx
		}
		cmdStr, err := nativeWindowCommand(ctx, idx, w.Path)
		if err != nil {
			return err
		}
		runner.Host(ctx.DryRun, "tmux", tmuxutil.NewSession(session, cmdStr)...)
		runner.Host(ctx.DryRun, "tmux", tmuxutil.RenameWindow(session+":0", name)...)
		start = 1
	}
	for _, w := range lf.Windows[start:] {
		idx := fmt.Sprintf("%d", w.Index)
		name := w.Name
		if strings.TrimSpace(name) == "" {
			name = "agent-" + idx
		}
		cmdStr, err := nativeWindowCommand(ctx, idx, w.Path)
		if err != nil {
			return err
		}
		runner.Host(ctx.DryRun, "tmux", tmuxutil.NewWindow(session, name, cmdStr)...)
	}
	if attach {
		runner.HostInteractive(ctx.DryRun, "tmux", tmuxutil.Attach(session)...)
	}
	return nil
}

func nativeWindowCommand(ctx *cmdregistry.Context, idx, dest string) (string, error) {
	repo := nativeRepo(ctx)
	return agentexec.BuildNativeCommand(agentexec.NativeCommandOpts{
		Exe:     ctx.Exe,
		Project: ctx.Project,
		Index:   idx,
		Repo:    repo,
		Dest:    dest,
	})
}

func nativeRepo(ctx *cmdregistry.Context) string {
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, ctx.Project)
	if err == nil && strings.TrimSpace(cfg.Defaults.Repo) != "" {
		return strings.TrimSpace(cfg.Defaults.Repo)
	}
	return "ouroboros-ide"
}

func handleWTOpen(ctx *cmdregistry.Context) error {
	if err := ensureProject(ctx); err != nil {
		return err
	}
	session := ""
	plain := false
	count := 0
	index := 0
	indexSet := false
	service := "dev-agent"
	cdPath := ""
	for i := 0; i < len(ctx.Args); i++ {
		switch ctx.Args[i] {
		case "--session":
			if i+1 < len(ctx.Args) {
				session = ctx.Args[i+1]
				i++
			}
		case "--plain":
			plain = true
		case "--count":
			if i+1 < len(ctx.Args) {
				count = mustAtoi(ctx.Args[i+1])
				i++
			}
		case "--index":
			if i+1 < len(ctx.Args) {
				index = mustAtoi(ctx.Args[i+1])
				indexSet = true
				i++
			}
		case "--service":
			if i+1 < len(ctx.Args) {
				service = ctx.Args[i+1]
				i++
			}
		case "--cd":
			if i+1 < len(ctx.Args) {
				cdPath = ctx.Args[i+1]
				i++
			}
		}
	}
	if strings.TrimSpace(session) == "" {
		session = defaultSessionName(ctx.Project)
		if plain {
			session += "-plain"
		}
	}
	if !plain && !hasTmuxSession(session) {
		return fmt.Errorf("tmux session %s does not exist; create it first", session)
	}
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
	tabs := []wtutil.TabSpec{}
	if plain {
		if strings.TrimSpace(service) == "" {
			service = "dev-agent"
		}
		if service != "dev-agent" {
			return fmt.Errorf("wt-open --plain currently supports only --service dev-agent")
		}
		if indexSet && count > 0 {
			return fmt.Errorf("wt-open --plain accepts either --index or --count, not both")
		}
		if indexSet && index <= 0 {
			return fmt.Errorf("wt-open --plain --index must be positive")
		}
		if indexSet {
			idx := fmt.Sprintf("%d", index)
			tabs = append(tabs, wtutil.TabSpec{
				Title: fmt.Sprintf("agent-%d", index),
				Args:  plainTabArgs(ctx, idx, cdPath),
			})
		} else {
			if count <= 0 {
				count = len(listServiceNames(ctx.Files, service))
				if count == 0 {
					return fmt.Errorf("no running %s containers found; use up/scale first or provide --count", service)
				}
			}
			for i := 1; i <= count; i++ {
				idx := fmt.Sprintf("%d", i)
				tabs = append(tabs, wtutil.TabSpec{
					Title: fmt.Sprintf("agent-%d", i),
					Args:  plainTabArgs(ctx, idx, cdPath),
				})
			}
		}
	} else {
		tabs, err = tmuxWindowTabs(session)
		if err != nil {
			return err
		}
	}
	lockPath := wtutil.ViewerLockPath(session)
	if !plain {
		if lock, err := wtutil.ReadViewerLock(lockPath); err == nil {
			releaseCommand := strings.TrimSpace(lock.ReleaseCommand)
			if releaseCommand == "" {
				project := strings.TrimSpace(lock.Project)
				if project == "" {
					project = ctx.Project
				}
				releaseCommand = wtutil.DefaultReleaseCommand(ctx.Exe, project, session)
			}
			return fmt.Errorf("wt viewer lock exists for %s at %s; close the tabs and run: %s", session, lockPath, releaseCommand)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("read wt viewer lock %s: %w", lockPath, err)
		}
	} else if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale wt viewer lock %s: %w", lockPath, err)
	}
	if !plain && ctx.DryRun {
		for _, tab := range tabs {
			fmt.Fprintln(os.Stderr, "+ tmux new-session -d -t "+shellSingleQuote(session)+" -s "+shellSingleQuote(tabSessionName(session, tab.Title)))
			args := wtutil.NewTabArgs(wtutil.ViewerWindowName(session), wslBinary, wslDistro, tab)
			fmt.Fprintln(os.Stderr, "+ "+wtBinary+" "+strings.Join(args, " "))
		}
		return nil
	}
	if plain && ctx.DryRun {
		args := wtutil.NewTabsArgs(wtutil.ViewerWindowName(session), wslBinary, wslDistro, tabs)
		fmt.Fprintln(os.Stderr, "+ "+wtBinary+" "+strings.Join(args, " "))
		return nil
	}
	if !plain {
		if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
			return fmt.Errorf("create wt viewer lock directory: %w", err)
		}
		if err := wtutil.WriteViewerLock(lockPath, wtutil.NewViewerLock(ctx.Exe, ctx.Project, session)); err != nil {
			return fmt.Errorf("create wt viewer lock: %w", err)
		}
	}
	createdSessions := make([]string, 0, len(tabs))
	cleanupCreated := func() {
		for _, linked := range createdSessions {
			_, _ = execx.Capture(context.Background(), "tmux", "kill-session", "-t", linked)
		}
		if !plain {
			_ = os.Remove(lockPath)
		}
	}
	for _, tab := range tabs {
		if plain {
			break
		}
		linkedSession := tabSessionName(session, tab.Title)
		if hasTmuxSession(linkedSession) {
			cleanupCreated()
			return fmt.Errorf("linked wt tmux session %s already exists; run wt-release --session %s before reopening", linkedSession, session)
		}
		runCtx, cancel := execx.WithTimeout(2 * time.Minute)
		createRes := execx.RunCtx(runCtx, "tmux", "new-session", "-d", "-t", session, "-s", linkedSession)
		cancel()
		if createRes.Code != 0 {
			cleanupCreated()
			return fmt.Errorf("failed to create linked tmux session %s", linkedSession)
		}
		runCtx, cancel = execx.WithTimeout(2 * time.Minute)
		selectRes := execx.RunCtx(runCtx, "tmux", "select-window", "-t", linkedSession+":"+tab.Title)
		cancel()
		if selectRes.Code != 0 {
			cleanupCreated()
			return fmt.Errorf("failed to select %s in linked tmux session %s", tab.Title, linkedSession)
		}
		createdSessions = append(createdSessions, linkedSession)
		args := wtutil.NewTabArgs(wtutil.ViewerWindowName(session), wslBinary, wslDistro, tab)
		runCtx, cancel = execx.WithTimeout(2 * time.Minute)
		res := execx.RunCtx(runCtx, wtBinary, args...)
		cancel()
		if res.Code != 0 {
			cleanupCreated()
			return fmt.Errorf("wt tab launch failed for %s", tab.Title)
		}
	}
	if plain {
		args := wtutil.NewTabsArgs(wtutil.ViewerWindowName(session), wslBinary, wslDistro, tabs)
		runCtx, cancel := execx.WithTimeout(2 * time.Minute)
		res := execx.RunCtx(runCtx, wtBinary, args...)
		cancel()
		if res.Code != 0 {
			cleanupCreated()
			return fmt.Errorf("wt tab launch failed for session %s", session)
		}
	}
	return nil
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

func handleWTRelease(ctx *cmdregistry.Context) error {
	session := ""
	for i := 0; i < len(ctx.Args); i++ {
		switch ctx.Args[i] {
		case "--session":
			if i+1 < len(ctx.Args) {
				session = ctx.Args[i+1]
				i++
			}
		}
	}
	project := strings.TrimSpace(ctx.Project)
	if strings.TrimSpace(session) == "" {
		if project == "" {
			return fmt.Errorf("wt-release requires either -p <project> or --session NAME")
		}
		session = defaultSessionName(project)
	}
	lockPath := wtutil.ViewerLockPath(session)
	if project == "" {
		lock, err := wtutil.ReadViewerLock(lockPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("wt viewer lock does not exist for %s at %s", session, lockPath)
			}
			return fmt.Errorf("read wt viewer lock %s: %w", lockPath, err)
		}
		project = strings.TrimSpace(lock.Project)
		if project == "" {
			return fmt.Errorf("wt viewer lock for %s at %s has no project metadata; rerun %s", session, lockPath, wtutil.DefaultReleaseCommand(ctx.Exe, "", session))
		}
	}
	tabs, tabsErr := tmuxWindowTabs(session)
	if tabsErr == nil {
		for _, tab := range tabs {
			linkedSession := tabSessionName(session, tab.Title)
			if hasTmuxSession(linkedSession) {
				_, _ = execx.Capture(context.Background(), "tmux", "kill-session", "-t", linkedSession)
			}
		}
	}
	if err := os.Remove(lockPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("wt viewer lock does not exist for %s at %s", session, lockPath)
		}
		return fmt.Errorf("remove wt viewer lock %s: %w", lockPath, err)
	}
	return nil
}

func handleTMUXBellInstall(ctx *cmdregistry.Context) error {
	if err := ensureProject(ctx); err != nil {
		return err
	}
	session, config, err := parseBellArgs(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(session) == "" {
		session = defaultSessionName(ctx.Project)
	}
	if !hasTmuxSession(session) {
		return fmt.Errorf("tmux session %s does not exist; create it first", session)
	}
	if err := config.Validate(); err != nil {
		return err
	}
	hook := "run-shell -b " + shellSingleQuote(tmuxnotify.HookCommand(ctx.Exe, ctx.Project, config))
	runner.Host(ctx.DryRun, "tmux", tmuxutil.SetWindowOptionGlobal("monitor-bell", "on")...)
	runner.Host(ctx.DryRun, "tmux", tmuxutil.SetHookGlobal("alert-bell", hook)...)
	return nil
}

func handleTMUXBellShowConfig(ctx *cmdregistry.Context) error {
	if err := ensureProject(ctx); err != nil {
		return err
	}
	_, config, err := parseBellArgs(ctx)
	if err != nil {
		return err
	}
	if err := config.Validate(); err != nil {
		return err
	}
	hook := "run-shell -b " + shellSingleQuote(tmuxnotify.HookCommand(ctx.Exe, ctx.Project, config))
	fmt.Println(strings.Join(append([]string{"tmux"}, tmuxutil.SetWindowOptionGlobal("monitor-bell", "on")...), " "))
	fmt.Println(strings.Join(append([]string{"tmux"}, tmuxutil.SetHookGlobal("alert-bell", hook)...), " "))
	return nil
}

func handleTMUXNotifyBell(ctx *cmdregistry.Context) error {
	if err := ensureProject(ctx); err != nil {
		return err
	}
	_, config, event, err := parseNotifyArgs(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(event.Session) == "" {
		return fmt.Errorf("--session is required")
	}
	if strings.TrimSpace(event.WindowIndex) == "" && strings.TrimSpace(event.WindowName) == "" {
		return fmt.Errorf("--window-index or --window-name is required")
	}
	if err := tmuxnotify.Notify(config, event); err != nil {
		return err
	}
	return nil
}

func tmuxWindowTabs(session string) ([]wtutil.TabSpec, error) {
	out, res := execx.Capture(context.Background(), "tmux", tmuxutil.ListWindowsDetailed(session)...)
	if res.Code != 0 {
		return nil, fmt.Errorf("list tmux windows for %s failed", session)
	}
	var tabs []wtutil.TabSpec
	for _, rawLine := range strings.Split(strings.TrimSpace(out), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("unexpected tmux window entry %q", line)
		}
		name := strings.TrimSpace(parts[1])
		if name == "" {
			return nil, fmt.Errorf("unexpected tmux window entry %q", line)
		}
		linkedSession := tabSessionName(session, name)
		tabs = append(tabs, wtutil.TabSpec{
			Title:   name,
			Command: "exec tmux attach-session -t " + shellSingleQuote(linkedSession),
		})
	}
	if len(tabs) == 0 {
		return nil, fmt.Errorf("tmux session %s has no windows", session)
	}
	return tabs, nil
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func plainTabArgs(ctx *cmdregistry.Context, idx, cdPath string) []string {
	project := detectPlainProject(ctx)
	dest := strings.TrimSpace(cdPath)
	if dest == "" && project == "dev-all" {
		repo := defaultDevAllRepo(ctx)
		dest = pth.AgentRepoPath(project, idx, repo)
	}
	exe := strings.TrimSpace(ctx.Exe)
	if exe == "" {
		if strings.TrimSpace(ctx.Paths.Kit) != "" {
			exe = filepath.Join(ctx.Paths.Kit, "scripts", "devkit")
		} else {
			exe = "devkit/kit/scripts/devkit"
		}
	}
	args := []string{exe}
	if strings.TrimSpace(project) != "" {
		args = append(args, "-p", project)
	}
	if strings.TrimSpace(ctx.ComposeProject) != "" {
		args = append(args, "--compose-project", ctx.ComposeProject)
	}
	if dest != "" {
		args = append(args, "exec-cd", idx, dest, "zsh", "-i")
	} else {
		args = append(args, "exec", idx, "zsh", "-i")
	}
	return args
}

func defaultDevAllRepo(ctx *cmdregistry.Context) string {
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, "dev-all")
	if err == nil && strings.TrimSpace(cfg.Defaults.Repo) != "" {
		return strings.TrimSpace(cfg.Defaults.Repo)
	}
	return "ouroboros-ide"
}

var detectPlainProject = func(ctx *cmdregistry.Context) string {
	project := strings.TrimSpace(ctx.Project)
	if project == "" {
		return project
	}
	if project == "dev-all" {
		return project
	}
	composeProject := strings.TrimSpace(ctx.ComposeProject)
	if composeProject == "" {
		return project
	}
	containerName := agentexec.ResolveContainerName(composeProject, "dev-agent", 1)
	if strings.TrimSpace(containerName) == "" {
		return project
	}
	runCtx, cancel := execx.WithTimeout(10 * time.Second)
	defer cancel()
	out, res := execx.Capture(runCtx, "docker", "inspect", "--format", "{{range .Mounts}}{{println .Destination}}{{end}}", containerName)
	if res.Code == 0 && strings.Contains(out, "/workspaces/dev") {
		return "dev-all"
	}
	return project
}

func tabSessionName(session, tabTitle string) string {
	return wtutil.ViewerWindowName(session + "-" + tabTitle)
}

func ensureProject(ctx *cmdregistry.Context) error {
	if strings.TrimSpace(ctx.Project) == "" {
		return fmt.Errorf("-p <project> is required")
	}
	return nil
}

func parseBellArgs(ctx *cmdregistry.Context) (string, tmuxnotify.Config, error) {
	session := ""
	config := tmuxnotify.DefaultConfig()
	for i := 0; i < len(ctx.Args); i++ {
		switch ctx.Args[i] {
		case "--session":
			if i+1 >= len(ctx.Args) {
				return "", config, fmt.Errorf("--session requires a value")
			}
			session = ctx.Args[i+1]
			i++
		case "--backend":
			if i+1 >= len(ctx.Args) {
				return "", config, fmt.Errorf("--backend requires a value")
			}
			config.Backend = ctx.Args[i+1]
			i++
		case "--file":
			if i+1 >= len(ctx.Args) {
				return "", config, fmt.Errorf("--file requires a value")
			}
			config.FilePath = ctx.Args[i+1]
			i++
		case "--debounce-ms":
			if i+1 >= len(ctx.Args) {
				return "", config, fmt.Errorf("--debounce-ms requires a value")
			}
			value, err := strconv.Atoi(ctx.Args[i+1])
			if err != nil {
				return "", config, fmt.Errorf("invalid --debounce-ms value %q", ctx.Args[i+1])
			}
			config.DebounceMS = value
			i++
		default:
			return "", config, fmt.Errorf("unknown tmux bell arg %s", ctx.Args[i])
		}
	}
	return session, config.Normalize(), nil
}

func parseNotifyArgs(ctx *cmdregistry.Context) (string, tmuxnotify.Config, tmuxnotify.Event, error) {
	session, config, err := parseBellArgs(&cmdregistry.Context{Args: nil})
	if err != nil {
		return "", config, tmuxnotify.Event{}, err
	}
	event := tmuxnotify.Event{}
	for i := 0; i < len(ctx.Args); i++ {
		switch ctx.Args[i] {
		case "--session":
			if i+1 >= len(ctx.Args) {
				return "", config, event, fmt.Errorf("--session requires a value")
			}
			event.Session = ctx.Args[i+1]
			i++
		case "--window-index":
			if i+1 >= len(ctx.Args) {
				return "", config, event, fmt.Errorf("--window-index requires a value")
			}
			event.WindowIndex = ctx.Args[i+1]
			i++
		case "--window-name":
			if i+1 >= len(ctx.Args) {
				return "", config, event, fmt.Errorf("--window-name requires a value")
			}
			event.WindowName = ctx.Args[i+1]
			i++
		case "--pane-id":
			if i+1 >= len(ctx.Args) {
				return "", config, event, fmt.Errorf("--pane-id requires a value")
			}
			event.PaneID = ctx.Args[i+1]
			i++
		case "--pane-title":
			if i+1 >= len(ctx.Args) {
				return "", config, event, fmt.Errorf("--pane-title requires a value")
			}
			event.PaneTitle = ctx.Args[i+1]
			i++
		case "--message":
			if i+1 >= len(ctx.Args) {
				return "", config, event, fmt.Errorf("--message requires a value")
			}
			event.Message = ctx.Args[i+1]
			i++
		case "--backend":
			if i+1 >= len(ctx.Args) {
				return "", config, event, fmt.Errorf("--backend requires a value")
			}
			config.Backend = ctx.Args[i+1]
			i++
		case "--file":
			if i+1 >= len(ctx.Args) {
				return "", config, event, fmt.Errorf("--file requires a value")
			}
			config.FilePath = ctx.Args[i+1]
			i++
		case "--debounce-ms":
			if i+1 >= len(ctx.Args) {
				return "", config, event, fmt.Errorf("--debounce-ms requires a value")
			}
			value, err := strconv.Atoi(ctx.Args[i+1])
			if err != nil {
				return "", config, event, fmt.Errorf("invalid --debounce-ms value %q", ctx.Args[i+1])
			}
			config.DebounceMS = value
			i++
		default:
			return session, config, event, fmt.Errorf("unknown tmux notify arg %s", ctx.Args[i])
		}
	}
	return session, config.Normalize(), event, nil
}

var (
	doSyncTmux                  func(bool, compose.Paths, string, []string, string, string, string, int, string)
	ensureTmuxSessionWithWindow func(bool, compose.Paths, string, []string, string, string, string, string, string)
	defaultSessionName          func(string) string
	mustAtoi                    func(string) int
	listServiceNames            func([]string, string) []string
	buildWindowCmd              func([]string, string, string, string, string, string, *agentexec.SeedTracker) (string, error)
	newSeedTracker              func() *agentexec.SeedTracker
	hasTmuxSession              func(string) bool
)

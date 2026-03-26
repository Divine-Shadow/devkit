package tmuxcmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	agentexec "devkit/cli/devctl/internal/agentexec"
	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/compose"
	"devkit/cli/devctl/internal/execx"
	"devkit/cli/devctl/internal/layout"
	runner "devkit/cli/devctl/internal/runner"
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
	for _, w := range lf.Windows {
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

func handleWTOpen(ctx *cmdregistry.Context) error {
	if err := ensureProject(ctx); err != nil {
		return err
	}
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
	if strings.TrimSpace(session) == "" {
		session = defaultSessionName(ctx.Project)
	}
	if !hasTmuxSession(session) {
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
	tabs, err := tmuxWindowTabs(session)
	if err != nil {
		return err
	}
	lockPath := wtutil.ViewerLockPath(session)
	if _, err := os.Stat(lockPath); err == nil {
		return fmt.Errorf("wt viewer lock exists for %s at %s; run wt-release --session %s after closing the tabs", session, lockPath, session)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read wt viewer lock %s: %w", lockPath, err)
	}
	if ctx.DryRun {
		for _, tab := range tabs {
			fmt.Fprintln(os.Stderr, "+ tmux new-session -d -t "+shellSingleQuote(session)+" -s "+shellSingleQuote(tabSessionName(session, tab.Title)))
			args := wtutil.NewTabArgs(wtutil.ViewerWindowName(session), wslBinary, wslDistro, tab)
			fmt.Fprintln(os.Stderr, "+ wt "+strings.Join(args, " "))
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return fmt.Errorf("create wt viewer lock directory: %w", err)
	}
	if err := os.WriteFile(lockPath, []byte(session+"\n"), 0o644); err != nil {
		return fmt.Errorf("create wt viewer lock: %w", err)
	}
	createdSessions := make([]string, 0, len(tabs))
	cleanupCreated := func() {
		for _, linked := range createdSessions {
			_, _ = execx.Capture(context.Background(), "tmux", "kill-session", "-t", linked)
		}
		_ = os.Remove(lockPath)
	}
	for _, tab := range tabs {
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
	if err := ensureProject(ctx); err != nil {
		return err
	}
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
	if strings.TrimSpace(session) == "" {
		session = defaultSessionName(ctx.Project)
	}
	lockPath := wtutil.ViewerLockPath(session)
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

func tabSessionName(session, tabTitle string) string {
	return wtutil.ViewerWindowName(session + "-" + tabTitle)
}

func ensureProject(ctx *cmdregistry.Context) error {
	if strings.TrimSpace(ctx.Project) == "" {
		return fmt.Errorf("-p <project> is required")
	}
	return nil
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

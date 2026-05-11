package agentexec

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"devkit/cli/devctl/internal/execx"
	"devkit/cli/devctl/internal/seed"
)

const defaultService = "dev-agent"

// SeedTracker tracks which containers have already been seeded during a command invocation.
type SeedTracker struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

// NewSeedTracker constructs a tracker for container seeding decisions.
func NewSeedTracker() *SeedTracker {
	return &SeedTracker{seen: map[string]struct{}{}}
}

// ShouldSeed returns true if the given container should run the seeding step.
func (t *SeedTracker) ShouldSeed(containerName string) bool {
	if t == nil {
		return true
	}
	name := strings.TrimSpace(containerName)
	if name == "" {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.seen[name]; ok {
		return false
	}
	t.seen[name] = struct{}{}
	return true
}

// CommandOpts describes how to build a docker exec command for a tmux window.
type CommandOpts struct {
	Files          []string
	Project        string
	Index          string
	Dest           string
	Service        string
	ComposeProject string
	ContainerName  string
	Tracker        *SeedTracker
	GitName        string
	GitEmail       string
}

type NativeCommandOpts struct {
	Exe     string
	Project string
	Index   string
	Repo    string
	Dest    string
}

func BuildNativeCommand(opts NativeCommandOpts) (string, error) {
	exe := strings.TrimSpace(opts.Exe)
	if exe == "" {
		exe = "devkit"
	}
	project := strings.TrimSpace(opts.Project)
	if project == "" {
		project = "dev-all"
	}
	idx := strings.TrimSpace(opts.Index)
	if idx == "" {
		idx = "1"
	}
	idxInt, err := strconv.Atoi(idx)
	if err != nil || idxInt < 1 {
		idx = "1"
		idxInt = 1
	}
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		repo = nativeRepoFromDest(opts.Dest)
	}
	if repo == "" {
		repo = "ouroboros-ide"
	}
	dest := nativeDest(idxInt, repo, opts.Dest)
	shell := "set -e; cd " + shSingleQuote(dest) + " 2>/dev/null || cd " + shSingleQuote(nativeDest(idxInt, repo, "")) + "; if command -v zsh >/dev/null 2>&1; then exec zsh -i; fi; exec bash"
	return strings.Join([]string{
		shSingleQuote(exe),
		"-p", shSingleQuote(project),
		"exec", idx,
		"--repo", shSingleQuote(repo),
		"--", "bash", "-lc", shSingleQuote(shell),
	}, " "), nil
}

func nativeDest(index int, repo string, dest string) string {
	if index < 1 {
		index = 1
	}
	repo = strings.TrimSpace(repo)
	if repo == "" {
		repo = "ouroboros-ide"
	}
	base := filepath.Join("/worktrees", fmt.Sprintf("agent%d", index), repo)
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return base
	}
	if !strings.HasPrefix(dest, "/") {
		return filepath.Join(base, dest)
	}
	normalized := filepath.Clean(dest)
	if strings.HasPrefix(normalized, "/worktrees/") {
		return normalized
	}
	devPrefix := "/workspaces/dev/"
	if strings.HasPrefix(normalized, devPrefix) {
		rel := strings.TrimPrefix(normalized, devPrefix)
		parts := strings.Split(rel, "/")
		if len(parts) >= 3 && parts[0] == "agent-worktrees" && strings.HasPrefix(parts[1], "agent") {
			return filepath.Join(append([]string{"/worktrees", parts[1]}, parts[2:]...)...)
		}
		if len(parts) >= 1 && parts[0] == repo {
			if len(parts) == 1 {
				return base
			}
			return filepath.Join(append([]string{base}, parts[1:]...)...)
		}
	}
	return normalized
}

func nativeRepoFromDest(dest string) string {
	dest = filepath.Clean(strings.TrimSpace(dest))
	if dest == "." || dest == "" {
		return ""
	}
	for _, prefix := range []string{"/worktrees/", "/workspaces/dev/agent-worktrees/"} {
		if strings.HasPrefix(dest, prefix) {
			rel := strings.TrimPrefix(dest, prefix)
			parts := strings.Split(rel, "/")
			if len(parts) >= 2 && strings.HasPrefix(parts[0], "agent") {
				return parts[1]
			}
		}
	}
	if strings.HasPrefix(dest, "/workspaces/dev/") {
		rel := strings.TrimPrefix(dest, "/workspaces/dev/")
		parts := strings.Split(rel, "/")
		if len(parts) >= 1 {
			return parts[0]
		}
	}
	return ""
}

// BuildCommand returns a docker exec invocation that anchors HOME, optionally seeds Codex,
// and opens an interactive bash shell at the requested destination.
func BuildCommand(opts CommandOpts) (string, error) {
	gitName := strings.TrimSpace(opts.GitName)
	gitEmail := strings.TrimSpace(opts.GitEmail)
	if gitName == "" || gitEmail == "" {
		return "", fmt.Errorf("git identity not configured")
	}

	project := strings.TrimSpace(opts.Project)
	service := strings.TrimSpace(opts.Service)
	if service == "" {
		service = defaultService
	}

	idx := strings.TrimSpace(opts.Index)
	if idx == "" {
		idx = "1"
	}
	idxInt, err := strconv.Atoi(idx)
	if err != nil || idxInt < 1 {
		idxInt = 1
	}

	composeProject := strings.TrimSpace(opts.ComposeProject)
	if composeProject == "" {
		composeProject = ComposeProjectName(project)
	}

	containerName := strings.TrimSpace(opts.ContainerName)
	if containerName == "" {
		containerName = ResolveContainerName(composeProject, service, idxInt)
	}

	shouldSeed := true
	if opts.Tracker != nil {
		shouldSeed = opts.Tracker.ShouldSeed(containerName)
	}

	anchor := AnchorHome(project)
	base := AnchorBase(project)
	scripts := seed.BuildAnchorScripts(seed.AnchorConfig{Anchor: anchor, Base: base, SeedCodex: shouldSeed})

	var b strings.Builder
	b.WriteString("set -e; ")
	if joined := seed.JoinScripts(scripts); joined != "" {
		b.WriteString(joined)
		b.WriteString("; ")
	}
	fmt.Fprintf(&b, "export HOME=%q CODEX_HOME=%q CODEX_ROLLOUT_DIR=%q XDG_CACHE_HOME=%q XDG_CONFIG_HOME=%q SBT_GLOBAL_BASE=%q; ",
		anchor,
		filepath.Join(anchor, ".codex"),
		filepath.Join(anchor, ".codex", "rollouts"),
		filepath.Join(anchor, ".cache"),
		filepath.Join(anchor, ".config"),
		filepath.Join(anchor, ".sbt"),
	)
	fmt.Fprintf(&b, "git config --global user.name %s && git config --global user.email %s; ", shSingleQuote(gitName), shSingleQuote(gitEmail))
	fmt.Fprintf(&b, "cd %q 2>/dev/null || true; if command -v zsh >/dev/null 2>&1; then exec zsh -i; fi; exec bash", opts.Dest)
	shell := b.String()

	script := shSingleQuote(shell)
	if containerName != "" {
		return "docker exec -it " + shSingleQuote(containerName) + " bash -lc " + script, nil
	}

	lookup := buildLookupLoop(opts.Files, service, idx, composeProject)
	msg := "No container for service " + service + " yet."
	if strings.TrimSpace(composeProject) != "" {
		msg = "No container for " + composeProject + "/" + service + " yet."
	}
	return lookup + "if [ -z \"$name\" ]; then echo " + shSingleQuote(msg) + "; exec bash; fi; " +
		"docker exec -it \"$name\" bash -lc " + script, nil
}

func buildLookupLoop(fileArgs []string, service, idx, composeProject string) string {
	clauses := make([]string, 0, 6)
	if cp := strings.TrimSpace(composeProject); cp != "" {
		clauses = append(clauses,
			fmt.Sprintf("name=$(docker ps --filter label=com.docker.compose.project=%s --filter label=com.docker.compose.service=%s --format '{{.Names}}' | sed -n '%sp')", shSingleQuote(cp), shSingleQuote(service), idx),
			fmt.Sprintf("name=$(docker ps --filter label=com.docker.compose.project=%s --filter label=com.docker.compose.service=%s --format '{{.Names}}' | sed -n '1p')", shSingleQuote(cp), shSingleQuote(service)),
		)
	}
	if len(fileArgs) > 0 {
		files := strings.Join(fileArgs, " ")
		clauses = append(clauses,
			fmt.Sprintf("name=$(docker compose %s ps --format '{{.Name}}' %s | sed -n '%sp')", files, shSingleQuote(service), idx),
			fmt.Sprintf("name=$(docker compose %s ps --format '{{.Name}}' %s | sed -n '1p')", files, shSingleQuote(service)),
		)
	}
	clauses = append(clauses,
		fmt.Sprintf("name=$(docker ps --filter label=com.docker.compose.service=%s --format '{{.Names}}' | sed -n '%sp')", shSingleQuote(service), idx),
		fmt.Sprintf("name=$(docker ps --filter label=com.docker.compose.service=%s --format '{{.Names}}' | sed -n '1p')", shSingleQuote(service)),
	)

	var loop strings.Builder
	loop.WriteString("name=''; for i in $(seq 1 120); do ")
	for _, clause := range clauses {
		loop.WriteString(clause)
		loop.WriteString("; if [ -n \"$name\" ]; then break; fi; ")
	}
	loop.WriteString("sleep 0.5; done; ")
	return loop.String()
}

// ResolveContainerName returns the container name for the given compose project/service/index.
func ResolveContainerName(composeProject, service string, index int) string {
	composeProject = strings.TrimSpace(composeProject)
	service = strings.TrimSpace(service)
	if index < 1 {
		index = 1
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		var names []string
		if composeProject != "" {
			names = listServiceNamesProject(composeProject, service)
		} else {
			names = listServiceNamesAny(service)
		}
		if len(names) > 0 {
			if index <= len(names) {
				return names[index-1]
			}
			return names[0]
		}
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// AnchorHome returns the anchor symlink path for a project overlay.
func AnchorHome(project string) string {
	if strings.TrimSpace(project) == "dev-all" {
		return "/workspaces/dev/.devhome"
	}
	return "/workspace/.devhome"
}

// AnchorBase returns the directory holding per-container homes for a project overlay.
func AnchorBase(project string) string {
	if strings.TrimSpace(project) == "dev-all" {
		return "/workspaces/dev/.devhomes"
	}
	return "/workspace/.devhomes"
}

// ComposeProjectName returns the compose project name for an overlay.
func ComposeProjectName(project string) string {
	if v := strings.TrimSpace(os.Getenv("COMPOSE_PROJECT_NAME")); v != "" {
		return v
	}
	p := strings.TrimSpace(project)
	switch p {
	case "", "codex":
		return "devkit"
	default:
		return "devkit-" + p
	}
}

func listServiceNamesProject(project, service string) []string {
	project = strings.TrimSpace(project)
	if project == "" {
		return listServiceNamesAny(service)
	}
	ctx, cancel := execx.WithTimeout(30 * time.Second)
	defer cancel()
	args := []string{"ps", "--filter", "label=com.docker.compose.project=" + project}
	if strings.TrimSpace(service) != "" {
		args = append(args, "--filter", "label=com.docker.compose.service="+service)
	}
	args = append(args, "--format", "{{.Names}}")
	out, _ := execx.Capture(ctx, "docker", args...)
	return parseNames(out)
}

func listServiceNamesAny(service string) []string {
	ctx, cancel := execx.WithTimeout(30 * time.Second)
	defer cancel()
	svc := strings.TrimSpace(service)
	if svc == "" {
		svc = defaultService
	}
	out, _ := execx.Capture(ctx, "docker", "ps", "--filter", "label=com.docker.compose.service="+svc, "--format", "{{.Names}}")
	return parseNames(out)
}

func parseNames(out string) []string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	names := make([]string, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			names = append(names, ln)
		}
	}
	sort.Strings(names)
	return names
}

// shSingleQuote wraps s in POSIX-safe single quotes.
func shSingleQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

package nativecmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/config"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/worktrees"
)

func TestRegisteredReadinessUsesActualCurrentConsumer(t *testing.T) {
	root := t.TempDir()
	base, cwd, home := filepath.Join(root, "product"), filepath.Join(root, "product-review"), filepath.Join(root, "role-home")
	git := func(dir string, args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = dir
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(root, "init", base)
	git(base, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", "fixture")
	git(base, "worktree", "add", "--detach", cwd, "HEAD")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	binding := worktrees.SourceRegistrationBinding{
		SchemaVersion: "devkit/current-source-registration/v1", WorkspaceID: "product-review",
		Worktree: cwd, BaseWorktree: base, Home: home, CodexHome: filepath.Join(home, ".codex"),
		TargetID: "product-worker-2", AgentIndex: 1, RegistrationSHA256: strings.Repeat("a", 64),
		GitDirectory:    git(cwd, "rev-parse", "--path-format=absolute", "--git-dir"),
		CommonDirectory: git(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir"),
	}
	env := map[string]string{"HOME": home, "CODEX_HOME": binding.CodexHome}
	getenv := func(key string) string { return env[key] }
	lookup := func(_ context.Context, actual string) (worktrees.SourceRegistrationBinding, error) {
		if actual != cwd {
			t.Fatalf("lookup received a substitute root: %s", actual)
		}
		return binding, nil
	}
	observed, err := observeRegisteredReadinessConsumer(cwd, getenv, lookup)
	if err != nil || observed != binding {
		t.Fatalf("registered disposable sibling: %#v %v", observed, err)
	}
	t.Run("selected-materialized-helper-home", func(t *testing.T) {
		original := binding.CodexHome
		defer func() { binding.CodexHome, env["CODEX_HOME"] = original, original }()
		binding.CodexHome = filepath.Join(root, "selected-private-helper-home")
		env["CODEX_HOME"] = binding.CodexHome
		if _, err := observeRegisteredReadinessConsumer(cwd, getenv, lookup); err != nil {
			t.Fatalf("owning selected helper home refused: %v", err)
		}
		env["CODEX_HOME"] = original
		if _, err := observeRegisteredReadinessConsumer(cwd, getenv, lookup); err == nil {
			t.Fatal("different actual helper home accepted")
		}
	})
	t.Run("real-command-and-fresh-result", func(t *testing.T) {
		status, code, out, _, err := runRegisteredRepoCheck(cwd, repoCheck{Name: "fixture", Command: "printf actual-check; touch warm-ran"}, false, binding, getenv, lookup)
		if err != nil || status != "pass" || code != 0 || out != "actual-check" {
			t.Fatalf("real command result: %s %d %q %v", status, code, out, err)
		}
		if _, err := os.Stat(filepath.Join(cwd, "warm-ran")); err != nil {
			t.Fatal("check was not actually executed", err)
		}
		status, code, _, _, err = runRegisteredRepoCheck(cwd, repoCheck{Name: "fixture", Command: "exit 7"}, false, binding, getenv, lookup)
		if err != nil || status != "fail" || code != 7 {
			t.Fatalf("command failure lost: %s %d %v", status, code, err)
		}
		changed := binding
		changed.RegistrationSHA256 = strings.Repeat("b", 64)
		status, _, _, _, err = runRegisteredRepoCheck(cwd, repoCheck{Name: "fixture", Command: "printf checked"}, false, binding, getenv, func(context.Context, string) (worktrees.SourceRegistrationBinding, error) { return changed, nil })
		if err == nil || status == "pass" {
			t.Fatal("changed registration produced a pass result")
		}
	})
	// This is readiness in the already admitted consumer, not an expansion of
	// the GUI/source-only sibling constructor. The primary is also a valid cwd.
	primary := binding
	primary.Worktree, primary.GitDirectory = base, filepath.Join(base, ".git")
	if _, err := observeRegisteredReadinessConsumer(base, getenv, func(context.Context, string) (worktrees.SourceRegistrationBinding, error) { return primary, nil }); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(){
		"cwd":              func() { binding.Worktree = base },
		"home":             func() { binding.Home = root },
		"codex-home":       func() { binding.CodexHome = root },
		"schema":           func() { binding.SchemaVersion = "foreign" },
		"git-directory":    func() { binding.GitDirectory = primary.GitDirectory },
		"common-directory": func() { binding.CommonDirectory = root },
	} {
		t.Run(name, func(t *testing.T) {
			original := binding
			defer func() { binding = original }()
			change()
			if _, err := observeRegisteredReadinessConsumer(cwd, getenv, lookup); err == nil {
				t.Fatal("mismatched consumer accepted")
			}
		})
	}
	t.Run("reader-refusal", func(t *testing.T) {
		_, err := observeRegisteredReadinessConsumer(cwd, getenv, func(context.Context, string) (worktrees.SourceRegistrationBinding, error) {
			return binding, errors.New("owning reader refused")
		})
		if err == nil || !strings.Contains(err.Error(), "owning reader refused") {
			t.Fatalf("reader refusal lost: %v", err)
		}
	})
	t.Run("ambient-root-override", func(t *testing.T) {
		t.Setenv("GIT_DIR", primary.GitDirectory)
		if _, err := observeRegisteredReadinessConsumer(cwd, getenv, lookup); err == nil {
			t.Fatal("ambient Git override accepted")
		}
	})
}

func TestCurrentWorktreeReadinessSelectsOnlyNamedImmutableCheck(t *testing.T) {
	cfg := config.OverlayConfig{}
	cfg.Hooks.Warm = "must not run"
	cfg.Runtime.CoreCheck = "must not run SBT"
	cfg.Readiness.RepoChecks = []config.RepoCheck{{Name: "frontend-warm", Command: "selected"}, {Name: "other", Command: "must not run"}}
	got, err := currentWorktreeRepoCheck(cfg, "frontend-warm")
	if err != nil || got.Command != "selected" {
		t.Fatalf("selected %#v, %v", got, err)
	}
	if _, err := currentWorktreeRepoCheck(cfg, "missing"); err == nil {
		t.Fatal("unknown check accepted")
	}
	cfg.Readiness.RepoChecks = append(cfg.Readiness.RepoChecks, cfg.Readiness.RepoChecks[0])
	if _, err := currentWorktreeRepoCheck(cfg, "frontend-warm"); err == nil {
		t.Fatal("duplicate check accepted")
	}
}

func TestCurrentWorktreeReadinessRejectsOverrides(t *testing.T) {
	base := []string{"--current-worktree", "--repo", "ouroboros-ide", "--repo-check-name", "frontend-warm"}
	if _, err := parseCurrentWorktreeReadinessArgs(base); err != nil {
		t.Fatal(err)
	}
	for _, extra := range [][]string{{"--repo-check", "touch foreign"}, {"--workspace-root", "/foreign"}, {"--gui-target-id", "foreign"}, {"--flake", "foreign"}, {"--index", "2"}, {"--skip-repo-checks"}, {"--", "bash"}} {
		if _, err := parseCurrentWorktreeReadinessArgs(append(append([]string{}, base...), extra...)); err == nil {
			t.Fatalf("override accepted: %v", extra)
		}
	}
}

func TestCurrentWorktreeReadinessRequiresExistingGUIEnvironment(t *testing.T) {
	consumer := nativeplan.GUIConsumerWorkspace{Home: "/workspaces/dev/ouroboros-ide/.devhome-agent1"}
	env := map[string]string{"DEVKIT_NATIVE_ISOLATION_PROFILE": "workspace-egress", "DEVKIT_NATIVE_MOUNT_POLICY_IDENTITY": "devkit/workspace-egress/v4", "DEVKIT_NATIVE_WINDOWS_MOUNTS_VISIBLE": "false", "OURO_NIX_SANDBOX": "1", "HOME": consumer.Home, "CODEX_HOME": filepath.Join(consumer.Home, ".codex")}
	get := func(k string) string { return env[k] }
	if err := validateCurrentGUIReadinessEnvironment(get, consumer); err != nil {
		t.Fatal(err)
	}
	for key, value := range env {
		env[key] = "foreign"
		if err := validateCurrentGUIReadinessEnvironment(get, consumer); err == nil {
			t.Fatalf("accepted substituted %s", key)
		}
		env[key] = value
	}
}

func TestCurrentWorktreeReadinessBindsRegisteredNestedWriter(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "ouroboros-ide")
	git := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git(root, "init", base)
	git(base, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", "fixture")
	nested := filepath.Join(base, ".devhome-agent1", ".codex", "worktrees", "writer-frontend-continuation", "ouroboros-ide")
	git(base, "worktree", "add", "--detach", nested, "HEAD")
	if err := validateCurrentGUIWorktree(base, nested); err != nil {
		t.Fatal(err)
	}
	if err := validateCurrentGUIWorktree(base, base); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(nested, "frontend")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	if err := validateCurrentGUIWorktree(base, sub); err == nil {
		t.Fatal("subdirectory accepted")
	}
	foreign := filepath.Join(base, "foreign")
	git(base, "init", foreign)
	if err := validateCurrentGUIWorktree(base, foreign); err == nil {
		t.Fatal("foreign repository accepted")
	}
	t.Run("ambient repository cannot impersonate lane registration", func(t *testing.T) {
		t.Setenv("GIT_DIR", filepath.Join(foreign, ".git"))
		if err := validateCurrentGUIWorktree(base, foreign); err == nil || !strings.Contains(err.Error(), "GIT_DIR") {
			t.Fatalf("ambient foreign repository accepted: %v", err)
		}
	})
	if err := validateCurrentGUIWorktree(base, root); err == nil {
		t.Fatal("outside lane accepted")
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(nested, alias); err != nil {
		t.Fatal(err)
	}
	if err := validateCurrentGUIWorktree(base, alias); err == nil {
		t.Fatal("symlink writer accepted")
	}
	code, out, stderr := executeCurrentRepoCheck(nested, repoCheck{Name: "fixture", Command: "pwd; printf prepared > frontend/prepared"})
	if code != 0 || strings.TrimSpace(out) != nested || stderr != "" {
		t.Fatalf("execution %d %q %q", code, out, stderr)
	}
	if _, err := os.Stat(filepath.Join(nested, "frontend", "prepared")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "frontend", "prepared")); !os.IsNotExist(err) {
		t.Fatal("base mutated")
	}
	code, _, stderr = executeCurrentRepoCheck(nested, repoCheck{Name: "fixture", Command: "echo failure >&2; exit 9"})
	if code != 9 || !strings.Contains(stderr, "failure") {
		t.Fatalf("lost failure: %d %q", code, stderr)
	}
}

func TestCurrentWorktreeReadinessDryRunDoesNotExecute(t *testing.T) {
	cwd := t.TempDir()
	status, code, out, stderr := runCurrentRepoCheck(cwd, repoCheck{Name: "fixture", Command: "touch must-not-exist; exit 9"}, true)
	if status != "planned" || code != 0 || out != "" || stderr != "" {
		t.Fatalf("dry-run result: %s %d %q %q", status, code, out, stderr)
	}
	if _, err := os.Stat(filepath.Join(cwd, "must-not-exist")); !os.IsNotExist(err) {
		t.Fatal("dry-run executed the preparation")
	}
}

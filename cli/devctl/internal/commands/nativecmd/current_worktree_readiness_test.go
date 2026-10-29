package nativecmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/config"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

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

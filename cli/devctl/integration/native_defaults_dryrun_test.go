package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func buildDevctlForNativeDefaults(t *testing.T) string {
	t.Helper()
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return buildDevctlForNativeDefaultsWithSSHAndTags(t, testExecutable)
}

func buildDevctlForNativeDefaultsWithTags(t *testing.T, tags ...string) string {
	t.Helper()
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return buildDevctlForNativeDefaultsWithSSHAndTags(t, testExecutable, tags...)
}

func buildDevctlForNativeDefaultsWithSSHAndTags(t *testing.T, sshExecutable string, tags ...string) string {
	t.Helper()
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(knownHosts, []byte("fixture.invalid ssh-ed25519 fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return buildDevctlForNativeDefaultsWithSSHKnownHostsAndTags(t, sshExecutable, knownHosts, tags...)
}

func buildDevctlForNativeDefaultsWithSSHKnownHostsAndTags(
	t *testing.T,
	sshExecutable string,
	knownHosts string,
	tags ...string,
) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "runtime", "kit", "bin", "devctl")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"build", "-trimpath"}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	args = append(
		args,
		"-ldflags",
		strings.Join([]string{
			"-X=devkit/cli/devctl/internal/sshauthority.packageExecutable=" + sshExecutable,
			"-X=devkit/cli/devctl/internal/sshauthority.packageKnownHosts=" + knownHosts,
		}, " "),
	)
	args = append(args, "-o", bin, "./")
	cmd := exec.Command("go", args...)
	cmd.Dir = filepath.Join("..")
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("go build failed: %v\n%s", err, out)
	}
	return bin
}

func nativeDefaultsRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "overlays/dev-all/devkit.yaml"), `
defaults:
  repo: ouroboros-ide
  origin: ssh://git@ssh.github.com:443/Divine-Shadow/ouroboros-ide.git
  agents: 2
runtime:
  flake: ./overlays/dev-all#default
readiness:
  default_mode: runtime-only
native:
  worktree_root: ../agent-worktrees
  state_root: ../.devkit/native-agents
  worktree_container_root: /worktrees
  state_container_root: /agent-state
hooks:
  warm: echo warm-native
`)
	return root
}

func writeNixCodexConfigSource(t *testing.T, root string) {
	t.Helper()
	config := strings.Join([]string{
		"# source = nixos-wsl codex config",
		`model = "gpt-5.5"`,
		`model_provider = "openai"`,
		"",
		"[profiles.openai]",
		`model = "gpt-5.5"`,
		`model_provider = "openai"`,
		"",
	}, "\n")
	for _, base := range []string{root, filepath.Dir(root)} {
		path := filepath.Join(base, ".devkit", "nix-codex-config.toml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func flakeOverlayRoot(t *testing.T, project, repo, flake string) string {
	t.Helper()
	root := t.TempDir()
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "overlays", project, "devkit.yaml"), `
workspace: ../../definitely-missing-workspace
defaults:
  repo: `+repo+`
  agents: 1
  base_branch: main
  branch_prefix: agent
runtime:
  flake: `+flake+`
readiness:
  default_mode: runtime-only
hooks:
  warm: echo warm-native
  maintain: echo maintain-native
native:
  worktree_root: ../agent-worktrees
  state_root: ../.devkit/native-agents
  worktree_container_root: /worktrees
  state_container_root: /agent-state
`)
	return root
}

func nonFlakeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "overlays/codex/devkit.yaml"), "service: dev-agent\n")
	return root
}

func createNativeAgentWorktree(t *testing.T, root string) {
	createNativeAgentWorktreeForRepo(t, root, "ouroboros-ide")
}

func createNativeAgentWorktreeForRepo(t *testing.T, root string, repo string) {
	t.Helper()
	devRoot := filepath.Clean(filepath.Join(root, ".."))
	worktree := filepath.Join(devRoot, "agent-worktrees", "agent1", repo)
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
}

func fakeBwrapDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "bwrap")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runNativeDefaultDryRun(t *testing.T, bin, root string, args ...string) (string, error) {
	t.Helper()
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, append([]string{"--dry-run", "-p", "dev-all"}, args...)...)
	cmd.Env = isolatedNativeFixtureEnv(
		"DEVKIT_ROOT="+root,
		"DEVKIT_NO_TMUX=1",
		"DEVKIT_RUNTIME_BROKER_BINARY="+testExecutable,
		"DEVKIT_RUNTIME_BWRAP_BINARY="+testExecutable,
		"DEVKIT_RUNTIME_SHELL_LAUNCHER="+testExecutable,
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func isolatedNativeFixtureEnv(extra ...string) []string {
	blocked := map[string]bool{
		"DEVKIT_ROOT":                          true,
		"DEVKIT_OVERLAYS_DIR":                  true,
		"DEVKIT_NATIVE_AGENT":                  true,
		"DEVKIT_NATIVE_ISOLATION_PROFILE":      true,
		"DEVKIT_NATIVE_MOUNT_POLICY_IDENTITY":  true,
		"DEVKIT_NATIVE_WINDOWS_MOUNTS_VISIBLE": true,
		"DEVKIT_CODEX_CONFIG_SOURCE":           true,
		"DEVKIT_RUNTIME_BROKER_BINARY":         true,
		"DEVKIT_RUNTIME_BWRAP_BINARY":          true,
		"DEVKIT_RUNTIME_SHELL_LAUNCHER":        true,
		"HTTP_PROXY":                           true,
		"HTTPS_PROXY":                          true,
		"http_proxy":                           true,
		"https_proxy":                          true,
	}
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !blocked[name] {
			env = append(env, entry)
		}
	}
	if testExecutable, err := os.Executable(); err == nil {
		env = append(
			env,
			"DEVKIT_RUNTIME_BROKER_BINARY="+testExecutable,
			"DEVKIT_RUNTIME_BWRAP_BINARY="+testExecutable,
			"DEVKIT_RUNTIME_SHELL_LAUNCHER="+testExecutable,
		)
	}
	return append(env, extra...)
}

func runProjectDryRun(t *testing.T, bin, root, project string, args ...string) (string, error) {
	t.Helper()
	return runProjectDryRunWithEnv(t, bin, root, project, nil, args...)
}

func runProjectDryRunWithEnv(t *testing.T, bin, root, project string, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, append([]string{"--dry-run", "-p", project}, args...)...)
	cmd.Env = isolatedNativeFixtureEnv(
		"DEVKIT_ROOT="+root,
		"DEVKIT_NO_TMUX=1",
		"DEVKIT_GIT_USER_NAME=Devkit Test",
		"DEVKIT_GIT_USER_EMAIL=devkit-test@example.com",
		"DEVKIT_RUNTIME_BROKER_BINARY="+testExecutable,
		"DEVKIT_RUNTIME_BWRAP_BINARY="+testExecutable,
		"DEVKIT_RUNTIME_SHELL_LAUNCHER="+testExecutable,
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func fakeWindowsTerminalEnv(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	wt := filepath.Join(dir, "wt.exe")
	wsl := filepath.Join(dir, "wsl.exe")
	for _, path := range []string{wt, wsl} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return []string{
		"DEVKIT_WT_PATH=" + wt,
		"DEVKIT_WSL_PATH=" + wsl,
		"DEVKIT_WSL_DISTRO=NixOS",
	}
}

func assertNoDockerCommand(t *testing.T, output string) {
	t.Helper()
	for _, forbidden := range []string{"+ docker " + "compose", "+ docker " + "exec", "docker " + "exec -it"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("native dev-all dry-run emitted %q:\n%s", forbidden, output)
		}
	}
}

func TestNonFlakeTopLevelAliasesRequireRuntimeFlakeDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nonFlakeRoot(t)

	for _, args := range [][]string{{"up"}, {"status"}, {"logs", "--tail", "1"}, {"down"}, {"scale", "2"}} {
		out, err := runProjectDryRun(t, bin, root, "codex", args...)
		if err == nil {
			t.Fatalf("%v unexpectedly succeeded:\n%s", args, out)
		}
		assertNoDockerCommand(t, out)
		if !strings.Contains(out, "runtime.flake") {
			t.Fatalf("%v did not require runtime.flake:\n%s", args, out)
		}
	}
}

func TestNonFlakeTopLevelExecRequiresRuntimeFlakeDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nonFlakeRoot(t)

	out, err := runProjectDryRun(t, bin, root, "codex", "exec", "1", "echo", "hi")
	if err == nil {
		t.Fatalf("non-flake exec unexpectedly succeeded:\n%s", out)
	}
	assertNoDockerCommand(t, out)
	if !strings.Contains(out, "runtime.flake") {
		t.Fatalf("non-flake exec did not require runtime.flake:\n%s", out)
	}
}

func TestFlakeBackedNonDevAllTopLevelAliasesUseNativeDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	overlays := []struct {
		project string
		repo    string
		flake   string
	}{
		{project: "_template", repo: "your-repo-name", flake: "./overlays/_template#default"},
		{project: "ouroboros-static-front-end", repo: "ouroboros-static-front-end", flake: "./overlays/ouroboros-static-front-end#default"},
		{project: "ouroboros-terraform", repo: "ouroboros-terraform", flake: "./overlays/ouroboros-terraform#default"},
		{project: "pokeemerald", repo: "pokeemerald", flake: "./overlays/pokeemerald#default"},
	}
	commands := [][]string{
		{"up"},
		{"status"},
		{"logs", "--tail", "1"},
		{"down"},
		{"scale", "1"},
		{"ensure-ready"},
		{"exec", "1", "--", "echo", "hi"},
		{"attach", "1"},
		{"warm"},
		{"maintain"},
	}
	for _, overlay := range overlays {
		t.Run(overlay.project, func(t *testing.T) {
			root := flakeOverlayRoot(t, overlay.project, overlay.repo, overlay.flake)
			for _, args := range commands {
				out, err := runProjectDryRun(t, bin, root, overlay.project, args...)
				if err != nil {
					t.Fatalf("%s %v failed: %v\n%s", overlay.project, args, err, out)
				}
				assertNoDockerCommand(t, out)
				if strings.Contains(out, "workspace directory") {
					t.Fatalf("%s %v still required container workspace validation:\n%s", overlay.project, args, out)
				}
				switch args[0] {
				case "exec", "attach":
					if !strings.Contains(out, "--die-with-parent") ||
						strings.Contains(out, "nix develop") ||
						strings.Contains(out, "--override-input") {
						t.Fatalf("%s %v did not use the immutable native runtime command:\n%s", overlay.project, args, out)
					}
				case "warm", "maintain":
					want := " -p " + overlay.project + " exec 1 --repo " + overlay.repo + " -- bash -lc"
					if !strings.Contains(out, want) {
						t.Fatalf("%s %v missing native hook exec %q:\n%s", overlay.project, args, want, out)
					}
				default:
					if !strings.Contains(out, "runtime: native") {
						t.Fatalf("%s %v missing native lifecycle status:\n%s", overlay.project, args, out)
					}
				}
			}
		})
	}
}

func TestFlakeBackedNonDevAllHelperCommandsUseNativeDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	project := "ouroboros-static-front-end"
	repo := "ouroboros-static-front-end"
	root := flakeOverlayRoot(t, project, repo, "./overlays/ouroboros-static-front-end#default")
	layoutPath := filepath.Join(root, "native-layout.yaml")
	if err := os.WriteFile(layoutPath, []byte(`
session: native-front
overlays:
  - project: ouroboros-static-front-end
    count: 1
windows:
  - index: 1
    project: ouroboros-static-front-end
    service: dev-agent
    path: .
    name: agent-1
`), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "check-net", args: []string{"check-net"}, want: " -p " + project + " exec 1 --repo " + repo + " -- bash -lc"},
		{name: "check-codex", args: []string{"check-codex"}, want: " -p " + project + " exec 1 --repo " + repo + " -- bash -lc"},
		{name: "check-sts", args: []string{"check-sts", "tinyproxy"}, want: " -p " + project + " exec 1 --repo " + repo + " -- bash -lc"},
		{name: "codex-auth", args: []string{"codex-auth", "reseed", "1"}, want: "seed codex auth"},
		{name: "creds", args: []string{"creds", "reseed-all"}, want: "seed codex auth"},
		{name: "codex-test", args: []string{"codex-test"}, want: " -p " + project + " exec 1 --repo " + repo + " -- zsh -ic"},
		{name: "codex-debug", args: []string{"codex-debug"}, want: " -p " + project + " exec 1 --repo " + repo + " -- bash -lc"},
		{name: "verify", args: []string{"verify"}, want: " -p " + project + " ensure-ready --repo " + repo + " --count 1 --repo-readiness"},
		{name: "doctor-runtime", args: []string{"doctor-runtime"}, want: " -p " + project + " ensure-ready --repo " + repo + " --count 1 --runtime-only"},
		{name: "exec-cd", args: []string{"exec-cd", "1", ".", "echo", "hi"}, want: " -p " + project + " exec 1 --repo " + repo + " -- bash -lc"},
		{name: "attach-cd", args: []string{"attach-cd", "1", "."}, want: " -p " + project + " exec 1 --repo " + repo + " -- bash -lc"},
		{name: "layout-apply", args: []string{"layout-apply", "--file", layoutPath}, want: " -p " + project + " up --repo " + repo + " --count 1"},
		{name: "layout-apply-skip-broker-ready", args: []string{"layout-apply", "--file", layoutPath, "--skip-broker", "--skip-ready"}, want: " -p " + project + " up --repo " + repo + " --count 1 --skip-broker --skip-ready"},
		{name: "layout-validate", args: []string{"layout-validate", "--file", layoutPath}, want: ""},
		{name: "layout-generate", args: []string{"layout-generate"}, want: "project: " + project},
		{name: "tmux-sync", args: []string{"tmux-sync", "--count", "1"}, want: " -p " + project + " exec 1 --repo " + repo},
		{name: "tmux-add-cd", args: []string{"tmux-add-cd", "1", ".", "--session", "native-front", "--name", "agent-1"}, want: " -p " + project + " exec 1 --repo " + repo},
		{name: "tmux-apply-layout", args: []string{"tmux-apply-layout", "--file", layoutPath}, want: " -p " + project + " exec 1 --repo " + repo},
		{name: "worktrees-init", args: []string{"worktrees-init", repo, "1"}, want: "Initialize worktrees for " + repo},
		{name: "worktrees-setup", args: []string{"worktrees-setup", repo, "1"}, want: "GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 git -C"},
		{name: "run", args: []string{"run", repo, "1"}, want: " -p " + project + " up --repo " + repo + " --count 1"},
		{name: "worktrees-branch", args: []string{"worktrees-branch", repo, "1", "agent-test"}, want: "git -C"},
		{name: "worktrees-status", args: []string{"worktrees-status", repo, "--index", "1"}, want: "git -C"},
		{name: "worktrees-sync", args: []string{"worktrees-sync", repo, "--pull", "--index", "1"}, want: "git -C"},
		{name: "bootstrap", args: []string{"bootstrap"}, want: " -p " + project + " up --repo " + repo + " --count 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runProjectDryRun(t, bin, root, project, tt.args...)
			if err != nil {
				t.Fatalf("%s failed: %v\n%s", tt.name, err, out)
			}
			assertNoDockerCommand(t, out)
			if strings.Contains(out, "workspace directory") {
				t.Fatalf("%s still required container workspace validation:\n%s", tt.name, out)
			}
			normalized := strings.ReplaceAll(out, "'", "")
			if tt.want != "" && !strings.Contains(normalized, tt.want) {
				t.Fatalf("%s missing %q:\n%s", tt.name, tt.want, out)
			}
		})
	}
}

func TestFlakeBackedNonDevAllRegistryCommandsAvoidContainerWorkspaceValidationDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	project := "ouroboros-static-front-end"
	repo := "ouroboros-static-front-end"
	root := flakeOverlayRoot(t, project, repo, "./overlays/ouroboros-static-front-end#default")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "allow", args: []string{"allow", "example.test"}, want: "Added to proxy allowlist: example.test"},
		{name: "broker", args: []string{"broker", "status", "--format", "text"}, want: "running:"},
		{name: "runtime-matrix", args: []string{"runtime-matrix", "--all"}, want: "./overlays/ouroboros-static-front-end#default"},
		{name: "tmux-bell-show-config", args: []string{"tmux-bell-show-config"}, want: "monitor-bell"},
		{name: "verify-all", args: []string{"verify-all"}, want: " -p dev-all verify"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runProjectDryRun(t, bin, root, project, tt.args...)
			if err != nil {
				t.Fatalf("%s failed: %v\n%s", tt.name, err, out)
			}
			assertNoDockerCommand(t, out)
			if strings.Contains(out, "workspace directory") {
				t.Fatalf("%s still required container workspace validation:\n%s", tt.name, out)
			}
			if !strings.Contains(out, tt.want) {
				t.Fatalf("%s missing %q:\n%s", tt.name, tt.want, out)
			}
		})
	}
}

func TestFlakeBackedReadinessCommandsAvoidRetiredRuntimeDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	project := "ouroboros-static-front-end"
	repo := "ouroboros-static-front-end"
	root := flakeOverlayRoot(t, project, repo, "./overlays/ouroboros-static-front-end#default")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "ensure-ready", args: []string{"ensure-ready"}, want: "readiness_mode: runtime-only"},
		{name: "verify", args: []string{"verify"}, want: " -p " + project + " ensure-ready --repo " + repo + " --count 1 --repo-readiness"},
		{name: "doctor-runtime", args: []string{"doctor-runtime"}, want: " -p " + project + " ensure-ready --repo " + repo + " --count 1 --runtime-only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runProjectDryRun(t, bin, root, project, tt.args...)
			if err != nil {
				t.Fatalf("%s failed: %v\n%s", tt.name, err, out)
			}
			assertNoDockerCommand(t, out)
			if strings.Contains(out, "workspace directory") {
				t.Fatalf("%s loaded retired runtime files before native readiness dispatch:\n%s", tt.name, out)
			}
			if !strings.Contains(out, tt.want) {
				t.Fatalf("%s missing %q:\n%s", tt.name, tt.want, out)
			}
		})
	}
}

func TestFlakeBackedLifecycleUsesReadinessDefaultDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	project := "ouroboros-static-front-end"
	repo := "ouroboros-static-front-end"
	root := flakeOverlayRoot(t, project, repo, "./overlays/ouroboros-static-front-end#default")

	for _, args := range [][]string{{"up"}, {"scale", "1"}} {
		out, err := runProjectDryRun(t, bin, root, project, args...)
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
		assertNoDockerCommand(t, out)
		if !strings.Contains(out, "readiness_mode: runtime-only") {
			t.Fatalf("%v did not use overlay readiness default:\n%s", args, out)
		}
	}
}

func TestFlakeBackedHostsCommandUsesNativeHostOnlyDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	project := "ouroboros-static-front-end"
	repo := "ouroboros-static-front-end"
	root := flakeOverlayRoot(t, project, repo, "./overlays/ouroboros-static-front-end#default")

	out, err := runProjectDryRun(t, bin, root, project, "hosts", "print", "--target", "host")
	if err != nil {
		t.Fatalf("flake-backed hosts command failed: %v\n%s", err, out)
	}
	assertNoDockerCommand(t, out)
	if strings.Contains(out, "workspace directory") {
		t.Fatalf("hosts still required container workspace validation:\n%s", out)
	}
	if !strings.Contains(out, "No ingress.hosts configured") {
		t.Fatalf("unexpected hosts output:\n%s", out)
	}
}

func TestFlakeBackedNonDevAllWTOpenAndWorktreeTmuxPlainUseNativeDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	project := "ouroboros-static-front-end"
	repo := "ouroboros-static-front-end"
	root := flakeOverlayRoot(t, project, repo, "./overlays/ouroboros-static-front-end#default")
	env := fakeWindowsTerminalEnv(t)

	for _, args := range [][]string{
		{"wt-open", "--plain", "--count", "1"},
		{"worktrees-tmux", repo, "1", "--plain"},
	} {
		out, err := runProjectDryRunWithEnv(t, bin, root, project, env, args...)
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
		assertNoDockerCommand(t, out)
		if strings.Contains(out, "--compose-project") {
			t.Fatalf("%v leaked retired project override into native WT launch:\n%s", args, out)
		}
		normalized := strings.ReplaceAll(out, "'", "")
		if !strings.Contains(normalized, " -p "+project+" exec-cd 1") && !strings.Contains(normalized, " -p "+project+" exec 1 --repo "+repo) {
			t.Fatalf("%v missing native project command:\n%s", args, out)
		}
	}
}

func TestFlakeBackedNonDevAllSSHAndRepoHelpersUseNativeDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	project := "ouroboros-static-front-end"
	repo := "ouroboros-static-front-end"
	root := flakeOverlayRoot(t, project, repo, "./overlays/ouroboros-static-front-end#default")
	nativePathPart := filepath.Join(".devkit", "native-agents", project+"-agent1", "home")
	worktreePathPart := filepath.Join("agent-worktrees", "agent1", repo)

	tests := []struct {
		name  string
		args  []string
		wants []string
	}{
		{name: "ssh-setup", args: []string{"ssh-setup", "--index", "1"}, wants: []string{"seed ssh", nativePathPart}},
		{name: "ssh-test", args: []string{"ssh-test", "1"}, wants: []string{" -F ", "-T github.com", "/agent-state/" + project + "-agent1/home/.ssh/config", "-p " + project + " exec 1", "--repo " + repo}},
		{name: "repo-config-ssh", args: []string{"repo-config-ssh", repo, "--index", "1"}, wants: []string{worktreePathPart, "git remote set-url origin"}},
		{name: "repo-config-https", args: []string{"repo-config-https", repo, "--index", "1"}, wants: []string{worktreePathPart, "git remote set-url origin"}},
		{name: "repo-push-ssh", args: []string{"repo-push-ssh", repo, "--index", "1"}, wants: []string{worktreePathPart, "git push -u origin HEAD"}},
		{name: "repo-push-https", args: []string{"repo-push-https", repo, "--index", "1"}, wants: []string{worktreePathPart, "git push -u origin HEAD"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runProjectDryRun(t, bin, root, project, tt.args...)
			if err != nil {
				t.Fatalf("%s failed: %v\n%s", tt.name, err, out)
			}
			assertNoDockerCommand(t, out)
			if strings.Contains(out, "workspace directory") {
				t.Fatalf("%s still required container workspace validation:\n%s", tt.name, out)
			}
			for _, want := range tt.wants {
				if !strings.Contains(out, want) {
					t.Fatalf("%s missing %q:\n%s", tt.name, want, out)
				}
			}
		})
	}
}

func TestNonFlakeSSHAndRepoHelpersRefuseRetiredRuntimeDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nonFlakeRoot(t)

	for _, args := range [][]string{
		{"ssh-test", "1"},
		{"repo-config-ssh", ".", "--index", "1"},
		{"repo-config-https", ".", "--index", "1"},
		{"repo-push-ssh", ".", "--index", "1"},
		{"repo-push-https", ".", "--index", "1"},
	} {
		out, err := runProjectDryRun(t, bin, root, "codex", args...)
		if err == nil {
			t.Fatalf("%v unexpectedly succeeded:\n%s", args, out)
		}
		assertNoDockerCommand(t, out)
		if !strings.Contains(out, "retired") && !strings.Contains(out, "runtime.flake") {
			t.Fatalf("%v did not report native-only refusal:\n%s", args, out)
		}
	}
}

func TestRetiredRuntimeNamespaceIsRejectedForNativeProjectDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nativeDefaultsRoot(t)

	out, err := runNativeDefaultDryRun(t, bin, root, "compose", "up")
	if err == nil {
		t.Fatalf("retired runtime namespace unexpectedly succeeded:\n%s", out)
	}
	assertNoDockerCommand(t, out)
	if !strings.Contains(out, "retired runtime namespace") {
		t.Fatalf("unexpected retired runtime namespace error:\n%s", out)
	}
}

func TestRetiredRuntimeNamespaceRefusesBeforeRuntimeFilesDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := flakeOverlayRoot(t, "dev-all", "ouroboros-ide", "./overlays/dev-all#default")

	out, err := runProjectDryRun(t, bin, root, "dev-all", "compose", "up")
	if err == nil {
		t.Fatalf("retired runtime namespace unexpectedly succeeded:\n%s", out)
	}
	assertNoDockerCommand(t, out)
	if strings.Contains(out, "workspace directory") {
		t.Fatalf("retired runtime namespace loaded runtime files before refusing:\n%s", out)
	}
	if !strings.Contains(out, "retired runtime namespace") {
		t.Fatalf("unexpected retired runtime namespace error:\n%s", out)
	}
}

func TestRetiredRuntimeNamespaceIsRejectedForNonFlakeProjectDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nonFlakeRoot(t)

	out, err := runProjectDryRun(t, bin, root, "codex", "compose", "up")
	if err == nil {
		t.Fatalf("retired runtime namespace unexpectedly succeeded:\n%s", out)
	}
	assertNoDockerCommand(t, out)
	if !strings.Contains(out, "retired runtime namespace") {
		t.Fatalf("unexpected retired runtime namespace error:\n%s", out)
	}
}

func TestNativeTopLevelExecAndAttachPreserveSandboxExitCode(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	bwrapDir := fakeBwrapDir(t)
	socketRoot, err := os.MkdirTemp("", "dkx-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketRoot) })
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
	}{
		{name: "exec", args: []string{"-p", "dev-all", "exec", "1", "--repo", "test-repo", "--flake", ".#runtime-test-agent", "--", "true"}},
		{name: "attach", args: []string{"-p", "dev-all", "attach", "1", "--repo", "test-repo", "--flake", ".#runtime-test-agent"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := nativeDefaultsRoot(t)
			writeNixCodexConfigSource(t, root)
			createNativeAgentWorktreeForRepo(t, root, "test-repo")
			worktree := filepath.Join(filepath.Clean(filepath.Join(root, "..")), "agent-worktrees", "agent1", "test-repo")
			if out, err := exec.Command("git", "-C", worktree, "init").CombinedOutput(); err != nil {
				t.Fatalf("initialize clean Git worktree fixture: %v\n%s", err, out)
			}
			proxySocket := filepath.Join(socketRoot, tt.name+".sock")
			if err := os.WriteFile(proxySocket, []byte("fixture bind source\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			proxySocketArgs := []string{"--proxy-socket", proxySocket}
			args := append([]string(nil), tt.args...)
			inserted := false
			for i, arg := range args {
				if arg == "--" {
					args = append(args[:i], append(proxySocketArgs, args[i:]...)...)
					inserted = true
					break
				}
			}
			if !inserted {
				args = append(args, proxySocketArgs...)
			}
			cmd := exec.Command(bin, args...)
			cmd.Env = append(os.Environ(),
				"DEVKIT_ROOT="+root,
				"DEVKIT_NO_TMUX=1",
				"CODEX_AUTH_JSON="+filepath.Join(root, "missing-auth.json"),
				"DEVKIT_RUNTIME_BROKER_BINARY="+testExecutable,
				"DEVKIT_RUNTIME_BWRAP_BINARY="+filepath.Join(bwrapDir, "bwrap"),
				"DEVKIT_RUNTIME_SHELL_LAUNCHER=/bin/sh",
				"PATH="+bwrapDir+string(os.PathListSeparator)+os.Getenv("PATH"),
			)
			out, err := cmd.CombinedOutput()
			exitErr, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("%s returned %T, want exit status 7\n%s", tt.name, err, out)
			}
			if code := exitErr.ExitCode(); code != 7 {
				t.Fatalf("%s exit code = %d, want 7\n%s", tt.name, code, out)
			}
		})
	}
}

func TestNativeTopLevelExecProjectsStdoutAndCleansProxyOnEveryExit(t *testing.T) {
	bin := buildDevctlForNativeDefaultsWithTags(t, "devkitintegration")
	root := nativeDefaultsRoot(t)
	writeNixCodexConfigSource(t, root)
	createNativeAgentWorktreeForRepo(t, root, "test-repo")
	worktree := filepath.Join(filepath.Clean(filepath.Join(root, "..")), "agent-worktrees", "agent1", "test-repo")
	if out, err := exec.Command("git", "-C", worktree, "init").CombinedOutput(); err != nil {
		t.Fatalf("initialize clean Git worktree fixture: %v\n%s", err, out)
	}

	bwrapDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(bwrapDir, "bwrap"),
		[]byte("#!/bin/sh\nprintf '%s' '__DEVKIT_RESULT__=PASS'\nexit \"${DEVKIT_TEST_BWRAP_EXIT:-0}\"\n"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	allowlistPath := filepath.Join(root, "allowlist.txt")
	if err := os.WriteFile(allowlistPath, []byte("github.com\nssh.github.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolvConfPath := filepath.Join(root, "..", ".devkit", "native-agents", "dev-all-agent1", "resolv.conf")
	if err := os.MkdirAll(filepath.Dir(resolvConfPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resolvConfPath, []byte("nameserver 127.0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	socketRoot, err := os.MkdirTemp("", "dke-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketRoot) })
	nscdSource := filepath.Join(root, "integration-nscd.socket")
	if err := os.WriteFile(nscdSource, []byte("integration-only bind source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, exitCode := range []int{0, 7} {
		t.Run(fmt.Sprintf("exit-%d", exitCode), func(t *testing.T) {
			sharedSocket := filepath.Join(socketRoot, fmt.Sprintf("shared-%d.sock", exitCode))
			cmd := exec.Command(
				bin,
				"-p", "dev-all",
				"exec", "1",
				"--repo", "test-repo",
				"--flake", ".#runtime-test-agent",
				"--isolation-profile", "workspace-egress",
				"--egress-allowlist", allowlistPath,
				"--proxy-socket", sharedSocket,
				"--", "true",
			)
			cmd.Env = append(os.Environ(),
				"DEVKIT_ROOT="+root,
				"DEVKIT_NO_TMUX=1",
				"CODEX_AUTH_JSON="+filepath.Join(root, "missing-auth.json"),
				"DEVKIT_TEST_BWRAP_EXIT="+strconv.Itoa(exitCode),
				"DEVKIT_INTEGRATION_NSCD_SOURCE="+nscdSource,
				"DEVKIT_RUNTIME_BWRAP_BINARY="+filepath.Join(bwrapDir, "bwrap"),
				"DEVKIT_RUNTIME_SHELL_LAUNCHER=/bin/sh",
				"PATH="+bwrapDir+string(os.PathListSeparator)+os.Getenv("PATH"),
			)
			out, err := cmd.CombinedOutput()
			if exitCode == 0 {
				if err != nil {
					t.Fatalf("native exec error = %v, want success\n%s", err, out)
				}
			} else {
				exitErr, ok := err.(*exec.ExitError)
				if !ok || exitErr.ExitCode() != exitCode {
					t.Fatalf("native exec error = %v, want exit %d\n%s", err, exitCode, out)
				}
			}
			if !strings.Contains(string(out), "__DEVKIT_RESULT__=PASS") {
				t.Fatalf("native exec suppressed child stdout:\n%s", out)
			}
			socketMatches, err := filepath.Glob(filepath.Join(filepath.Dir(sharedSocket), ".managed-egress-*.sock"))
			if err != nil {
				t.Fatal(err)
			}
			if len(socketMatches) != 0 {
				t.Fatalf("native exec left managed proxy sockets after exit %d: %v", exitCode, socketMatches)
			}
		})
	}
}

func TestNativeTopLevelPrepareAndExecUseIsolatedRelativeMetadata(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nativeDefaultsRoot(t)
	writeNixCodexConfigSource(t, root)
	devRoot := filepath.Dir(root)
	repo := filepath.Join(devRoot, "test-repo")
	bare := filepath.Join(root, "fixture-remotes", "test-repo.git")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	run("git", "init", "--bare", bare)
	run("git", "init", repo)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("isolated fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".devhome-agent*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "-C", repo, "add", "README.md", ".gitignore")
	run("git", "-C", repo, "-c", "user.name=Devkit Test", "-c", "user.email=devkit-test@example.com", "commit", "-m", "fixture")
	run("git", "-C", repo, "branch", "-M", "main")
	run("git", "-C", repo, "remote", "add", "origin", bare)
	run("git", "-C", repo, "push", "-u", "origin", "main")
	fixtureOrigin := "ssh://git@fixture.invalid" + bare
	run("git", "-C", repo, "remote", "set-url", "origin", fixtureOrigin)
	overlayPath := filepath.Join(root, "overlays", "dev-all", "devkit.yaml")
	overlayBytes, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatal(err)
	}
	overlayBytes = []byte(strings.ReplaceAll(
		string(overlayBytes),
		"ssh://git@ssh.github.com:443/Divine-Shadow/ouroboros-ide.git",
		fixtureOrigin,
	))
	if err := os.WriteFile(overlayPath, overlayBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	sshDir := t.TempDir()
	sshLog := filepath.Join(sshDir, "invocations.log")
	sshScript := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + strconv.Quote(sshLog) + "\n" +
		"if [ \"${1:-}\" = -G ]; then exit 0; fi\n" +
		"last=\n" +
		"for arg in \"$@\"; do last=$arg; done\n" +
		"case \"$last\" in\n" +
		"  \"git-upload-pack \"*) exec sh -c \"$last\" ;;\n" +
		"esac\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(sshDir, "ssh"), []byte(sshScript), 0o755); err != nil {
		t.Fatal(err)
	}
	bin = buildDevctlForNativeDefaultsWithSSHAndTags(t, filepath.Join(sshDir, "ssh"))

	hostHome := filepath.Join(root, "host-home")
	if err := os.MkdirAll(filepath.Join(hostHome, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostHome, ".ssh", "id_ed25519"), []byte("integration private fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostHome, ".ssh", "id_ed25519.pub"), []byte("integration public fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	isolatedRoot, err := os.MkdirTemp("", "devkit-isolated-topology-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(isolatedRoot) })
	resolvConf := filepath.Join(root, "..", ".devkit", "native-agents", "dev-all-agent1", "resolv.conf")
	if err := os.MkdirAll(filepath.Dir(resolvConf), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resolvConf, []byte("nameserver 127.0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepare := exec.Command(
		bin,
		"-p", "dev-all",
		"native", "prepare",
		"--repo", "test-repo",
		"--count", "1",
		"--base-branch", "main",
		"--branch-prefix", "isolated-agent",
		"--worktree-root", isolatedRoot,
		"--worktree-container-root", "/workspaces/dev/isolated-top-level",
		"--resolv-conf", resolvConf,
		"--format", "json",
	)
	prepare.Env = isolatedNativeFixtureEnv(
		"DEVKIT_ROOT="+root,
		"DEVKIT_NO_TMUX=1",
		"HOME="+hostHome,
		"CODEX_AUTH_JSON="+filepath.Join(root, "missing-auth.json"),
		"PATH="+sshDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	prepareOut, err := prepare.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("native prepare: %v\n%s", err, exitErr.Stderr)
		}
		t.Fatal(err)
	}
	if !strings.Contains(string(prepareOut), `"sandbox_worktree": "/workspaces/dev/isolated-top-level/agent1/test-repo"`) {
		t.Fatalf("native prepare omitted isolated sandbox projection:\n%s", prepareOut)
	}
	sshInvocations, err := os.ReadFile(sshLog)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapConfig := filepath.Join(isolatedRoot, "agent1", "test-repo", ".devhome-agent1", ".ssh", "config")
	if strings.Count(string(sshInvocations), "git-upload-pack") != 1 ||
		!strings.Contains(string(sshInvocations), "-F "+bootstrapConfig) ||
		strings.Contains(string(sshInvocations), "/dev/null") {
		t.Fatalf("native prepare did not use exactly one package-owned SSH fetch:\n%s", sshInvocations)
	}

	worktree := filepath.Join(isolatedRoot, "agent1", "test-repo")
	gitFile, err := os.ReadFile(filepath.Join(worktree, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	gitdirValue := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(gitFile)), "gitdir:"))
	if gitdirValue == "" || filepath.IsAbs(gitdirValue) {
		t.Fatalf("native prepare emitted non-portable .git metadata: %q", gitFile)
	}
	gitdir := filepath.Clean(filepath.Join(worktree, gitdirValue))
	commondir, err := os.ReadFile(filepath.Join(gitdir, "commondir"))
	if err != nil {
		t.Fatal(err)
	}
	if value := strings.TrimSpace(string(commondir)); value == "" || filepath.IsAbs(value) {
		t.Fatalf("native prepare emitted non-portable commondir: %q", commondir)
	}
	commonDir := filepath.Clean(filepath.Join(gitdir, strings.TrimSpace(string(commondir))))
	wantCommonDir := filepath.Join(isolatedRoot, ".devkit", "git", "test-repo.git")
	if commonDir != wantCommonDir {
		t.Fatalf("native prepare common directory = %s, want package-owned %s", commonDir, wantCommonDir)
	}
	reverseGitdir, err := os.ReadFile(filepath.Join(gitdir, "gitdir"))
	if err != nil {
		t.Fatal(err)
	}
	if value := strings.TrimSpace(string(reverseGitdir)); value == "" || filepath.IsAbs(value) {
		t.Fatalf("native prepare emitted non-portable reverse gitdir: %q", reverseGitdir)
	}
	if got := filepath.Clean(filepath.Join(gitdir, strings.TrimSpace(string(reverseGitdir)))); got != filepath.Join(worktree, ".git") {
		t.Fatalf("native prepare reverse gitdir = %s, want %s", got, filepath.Join(worktree, ".git"))
	}
	for path, content := range map[string][]byte{
		filepath.Join(worktree, ".git"):    gitFile,
		filepath.Join(gitdir, "commondir"): commondir,
		filepath.Join(gitdir, "gitdir"):    reverseGitdir,
	} {
		if strings.Contains(string(content), devRoot) || strings.Contains(string(content), repo) {
			t.Fatalf("%s retained a source/host alias: %s", path, content)
		}
	}
	run("git", "-C", worktree, "update-ref", "refs/devkit/top-level-prepare-proof", "HEAD")
	run("git", "-C", worktree, "update-ref", "-d", "refs/devkit/top-level-prepare-proof")

	bwrapDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(bwrapDir, "bwrap"),
		[]byte("#!/bin/sh\nprintf '%s' '__DEVKIT_ISOLATED_EXEC__=PASS'\n"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	execCmd := exec.Command(
		bin,
		"-p", "dev-all",
		"exec", "1",
		"--repo", "test-repo",
		"--worktree-root", isolatedRoot,
		"--worktree-container-root", "/workspaces/dev/isolated-top-level",
		"--", "true",
	)
	execCmd.Env = isolatedNativeFixtureEnv(
		"DEVKIT_ROOT="+root,
		"DEVKIT_NO_TMUX=1",
		"HOME="+hostHome,
		"CODEX_AUTH_JSON="+filepath.Join(root, "missing-auth.json"),
		"DEVKIT_RUNTIME_BWRAP_BINARY="+filepath.Join(bwrapDir, "bwrap"),
		"DEVKIT_RUNTIME_SHELL_LAUNCHER=/bin/sh",
		"PATH="+bwrapDir+string(os.PathListSeparator)+sshDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	gitProbe := exec.Command("git", "-C", worktree, "rev-parse", "--show-toplevel")
	gitProbe.Env = execCmd.Env
	if probeOut, probeErr := gitProbe.CombinedOutput(); probeErr != nil {
		configProbe := exec.Command("git", "-C", worktree, "config", "--show-origin", "--list")
		configProbe.Env = execCmd.Env
		configOut, _ := configProbe.CombinedOutput()
		t.Fatalf("isolated worktree failed under exec environment: %v\n%s\nconfig:\n%s", probeErr, probeOut, configOut)
	}
	execOut, err := execCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native exec after isolated prepare: %v\n%s", err, execOut)
	}
	if !strings.Contains(string(execOut), "__DEVKIT_ISOLATED_EXEC__=PASS") {
		t.Fatalf("native exec suppressed isolated result projection:\n%s", execOut)
	}

	projectedRoot := filepath.Join(t.TempDir(), "unrelated", "sandbox", "depth", "product-root")
	if err := os.MkdirAll(filepath.Dir(projectedRoot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(isolatedRoot, projectedRoot); err != nil {
		t.Fatalf("project owned native root %s -> %s: %v", isolatedRoot, projectedRoot, err)
	}
	projectedWorktree := filepath.Join(projectedRoot, "agent1", "test-repo")
	run("git", "-C", projectedWorktree, "rev-parse", "--show-toplevel")
	run("git", "-C", projectedWorktree, "update-ref", "refs/devkit/projected-top-level-proof", "HEAD")
	run("git", "-C", projectedWorktree, "update-ref", "-d", "refs/devkit/projected-top-level-proof")
}

func TestGlobalDryRunNativeExecDoesNotPrepareOrRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nativeDefaultsRoot(t)

	out, err := runNativeDefaultDryRun(t, bin, root, "native", "exec", "--repo", "ouroboros-ide", "--flake", ".#runtime-test-agent", "--", "echo", "hi")
	if err != nil {
		t.Fatalf("global dry-run native exec failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "--die-with-parent") ||
		!strings.Contains(out, "exec") ||
		!strings.Contains(out, "echo") ||
		!strings.Contains(out, "hi") ||
		strings.Contains(out, "nix develop") ||
		strings.Contains(out, "--override-input") {
		t.Fatalf("global dry-run did not print immutable native command:\n%s", out)
	}
}

func TestNativeTopLevelExecProxySocketDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nativeDefaultsRoot(t)
	proxySocket := filepath.Join(root, ".devkit", "native-egress", "proxy.sock")

	out, err := runNativeDefaultDryRun(t, bin, root, "exec", "1", "--repo", "ouroboros-ide", "--flake", ".#runtime-test-agent", "--proxy-socket", proxySocket, "--", "curl", "-I", "https://api.openai.com")
	if err != nil {
		t.Fatalf("native exec proxy-socket dry-run failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "--unshare-net") || !strings.Contains(out, "native proxy-bridge --listen 127.0.0.1:18888") || !strings.Contains(out, proxySocket) {
		t.Fatalf("proxy-socket dry-run did not print isolated native command:\n%s", out)
	}
	if strings.Contains(out, "--share-net") {
		t.Fatalf("proxy-socket dry-run still shared host network:\n%s", out)
	}
}

func TestDevAllFreshOpenAndResetUseNativeLifecycleDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nativeDefaultsRoot(t)

	freshOutput, err := runNativeDefaultDryRun(t, bin, root, "fresh-open", "2")
	if err != nil {
		t.Fatalf("fresh-open failed: %v\n%s", err, freshOutput)
	}
	assertNoDockerCommand(t, freshOutput)
	if !strings.Contains(freshOutput, " -p dev-all down --repo ouroboros-ide --count 2") ||
		!strings.Contains(freshOutput, " -p dev-all up --repo ouroboros-ide --count 2") {
		t.Fatalf("fresh-open missing native lifecycle:\n%s", freshOutput)
	}

	resetOutput, err := runNativeDefaultDryRun(t, bin, root, "reset", "2")
	if err != nil {
		t.Fatalf("reset failed: %v\n%s", err, resetOutput)
	}
	assertNoDockerCommand(t, resetOutput)
	for _, required := range []string{"command: down", "command: reset", "+ reset-owned"} {
		if !strings.Contains(resetOutput, required) {
			t.Fatalf("reset missing exact destructive lifecycle marker %q:\n%s", required, resetOutput)
		}
	}
}

func TestDevAllResetReconstructsThreeSlotsThroughPackageSSHAuthority(t *testing.T) {
	base, err := os.MkdirTemp("", "dvr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	root := filepath.Join(base, "devkit")
	packageDevctl := filepath.Join(root, "kit", "bin", "devctl")
	if err := os.MkdirAll(filepath.Dir(packageDevctl), 0o755); err != nil {
		t.Fatal(err)
	}
	builtDevctl := buildDevctlForNativeDefaultsWithTags(t, "devkitintegration")
	devctlBytes, err := os.ReadFile(builtDevctl)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packageDevctl, devctlBytes, 0o755); err != nil {
		t.Fatal(err)
	}

	allowlistPath := filepath.Join(root, "kit", "proxy", "allowlist.txt")
	brokerSocket := filepath.Join(base, ".devkit", "native-broker", "broker.sock")
	remoteRepo := filepath.Join(base, "remote.git")
	overlay := fmt.Sprintf(`
defaults:
  repo: ouroboros-ide
  origin: ssh://git@github.com%s
  agents: 3
  base_branch: main
  branch_prefix: agent
runtime:
  flake: ./overlays/dev-all#default
readiness:
  default_mode: runtime-only
  runtime_checks:
    - name: package-runtime-tools
      command: command -v git >/dev/null && command -v codex >/dev/null
    - name: governance-provenance
      command: test -r /workspaces/dev/.devkit/ouro8-governance-env.sh && test -r /workspaces/dev/.devkit/ouro8-governance-repo-env.json
    - name: codex-provider-config
      command: test -r "$CODEX_HOME/config.toml"
    - name: app-server-preparation
      command: test -r "$CODEX_HOME/config.toml" && test -d "$CODEX_ROLLOUT_DIR"
broker:
  socket: %s
  upstream: unix:///fixture-docker.sock
native:
  worktree_root: ../agent-worktrees
  state_root: ../.devkit/native-agents
  worktree_container_root: /worktrees
  state_container_root: /agent-state
  required_isolation_profile: workspace-egress
  isolation_profiles:
    workspace-egress:
      filesystem: workspace-only
      egress_allowlist: %s
`, remoteRepo, brokerSocket, allowlistPath)
	overlayPath := filepath.Join(root, "overlays", "dev-all", "devkit.yaml")
	if err := os.MkdirAll(filepath.Dir(overlayPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlayPath, []byte(overlay), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(allowlistPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(allowlistPath, []byte("ssh.github.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeNixCodexConfigSource(t, root)

	sourceRepo := filepath.Join(base, "ouroboros-ide")
	runNativeFixtureCommand(t, "git", "init", "--bare", "--initial-branch=main", remoteRepo)
	runNativeFixtureCommand(t, "git", "init", "--initial-branch=main", sourceRepo)
	runNativeFixtureCommand(t, "git", "-C", sourceRepo, "config", "user.name", "Devkit Reset Regression")
	runNativeFixtureCommand(t, "git", "-C", sourceRepo, "config", "user.email", "devkit-reset@example.invalid")
	if err := os.WriteFile(filepath.Join(sourceRepo, "README.md"), []byte("reset fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRepo, ".gitignore"), []byte(".devhome-agent*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runNativeFixtureCommand(t, "git", "-C", sourceRepo, "add", ".")
	runNativeFixtureCommand(t, "git", "-C", sourceRepo, "commit", "-m", "reset fixture")
	runNativeFixtureCommand(t, "git", "-C", sourceRepo, "remote", "add", "origin", remoteRepo)
	runNativeFixtureCommand(t, "git", "-C", sourceRepo, "push", "-u", "origin", "main")
	runNativeFixtureCommand(t, "git", "-C", sourceRepo, "remote", "set-url", "origin", "ssh://git@github.com"+remoteRepo)

	sshDir := filepath.Join(base, "fixture-bin")
	sshLog := filepath.Join(base, "ssh.log")
	sshAllow := filepath.Join(base, "allow-ssh-fetch")
	readinessFail := filepath.Join(base, "fail-readiness")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sshScript := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + strconv.Quote(sshLog) + "\n" +
		"if [ \"${1:-}\" = -G ]; then exit 0; fi\n" +
		"if [ ! -f " + strconv.Quote(sshAllow) + " ]; then exit 72; fi\n" +
		"last=\n" +
		"for arg in \"$@\"; do last=$arg; done\n" +
		"case \"$last\" in\n" +
		"  \"git-upload-pack \"*) exec sh -c \"$last\" ;;\n" +
		"esac\n" +
		"exit 71\n"
	if err := os.WriteFile(filepath.Join(sshDir, "ssh"), []byte(sshScript), 0o755); err != nil {
		t.Fatal(err)
	}
	builtDevctl = buildDevctlForNativeDefaultsWithSSHAndTags(t, filepath.Join(sshDir, "ssh"), "devkitintegration")
	devctlBytes, err = os.ReadFile(builtDevctl)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packageDevctl, devctlBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	bwrapScript := `#!/bin/sh
set -eu
if [ -f ` + strconv.Quote(readinessFail) + ` ]; then
  printf '__DEVKIT_READINESS_CHECK__\truntime\tsandbox-command\t1\tcmVhZGluZXNzIGZhaWx1cmU=\n'
  exit 0
fi
governance_env=
governance_catalog=
governance_state=
host_home=
host_worktree=
mount_policy=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --ro-bind|--bind)
      source="$2"
      target="$3"
      case "$target" in
        /workspaces/dev/.devkit/ouro8-governance-env.sh) governance_env="$source" ;;
        /workspaces/dev/.devkit/ouro8-governance-repo-env.json) governance_catalog="$source" ;;
        /workspaces/dev/.devkit/governance-control-plane) governance_state="$source" ;;
        /workspaces/dev/agent-worktrees/agent*/ouroboros-ide)
          case "$source" in
            */agent-worktrees/agent*/ouroboros-ide) host_worktree="$source" ;;
          esac
          ;;
        /workspaces/dev/agent-worktrees/agent*/ouroboros-ide/.devhome-agent*|\
        /workspaces/dev/agent-worktrees/agent*/.devhome-agent*) host_home="$source" ;;
      esac
      shift 3
      ;;
    --setenv)
      if [ "$2" = DEVKIT_NATIVE_MOUNT_POLICY_IDENTITY ]; then
        mount_policy="$3"
      fi
      shift 3
      ;;
    *)
      shift
      ;;
  esac
done
emit() {
  name="$1"
  rc="$2"
  detail="$3"
  encoded="$(printf '%s' "$detail" | base64 -w0 2>/dev/null || printf '%s' "$detail" | base64 | tr -d '\n')"
  printf '__DEVKIT_READINESS_CHECK__\truntime\t%s\t%s\t%s\n' "$name" "$rc" "$encoded"
}
emit sandbox-command 0 ''
emit broker-socket 0 ''
if [ "$mount_policy" = devkit/workspace-egress/v3 ] && [ -n "$host_worktree" ] &&
   command git -C "$host_worktree" diff --quiet &&
   command git -C "$host_worktree" diff --cached --quiet &&
   [ "$(command git -C "$host_worktree" rev-parse HEAD)" = "$(command git -C "$host_worktree" rev-parse refs/remotes/origin/main)" ]; then
  emit package-runtime-tools 0 ''
else
  emit package-runtime-tools 1 'package runtime identity or clean current Product worktree is missing'
fi
if [ -r "$governance_env" ] && [ -r "$governance_catalog" ] && [ -d "$governance_state" ]; then
  emit governance-provenance 0 ''
else
  emit governance-provenance 1 'prepared governance runtime support is not projected into the sandbox'
fi
if [ -r "$host_home/.codex/config.toml" ]; then
  emit codex-provider-config 0 ''
  emit app-server-preparation 0 ''
else
  emit codex-provider-config 1 'Nix-authored Codex config is missing'
  emit app-server-preparation 1 'source-defined app-server preparation is missing'
fi
`
	if err := os.WriteFile(filepath.Join(sshDir, "bwrap"), []byte(bwrapScript), 0o755); err != nil {
		t.Fatal(err)
	}

	brokerPackage := filepath.Join(base, "fixture-broker")
	brokerExecutable := filepath.Join(brokerPackage, "bin", "postgres-broker")
	if err := os.MkdirAll(filepath.Dir(brokerExecutable), 0o755); err != nil {
		t.Fatal(err)
	}
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	testBinary, err := os.ReadFile(testExecutable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(brokerExecutable, testBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	controllerHome := filepath.Join(base, "controller-home")
	if err := os.MkdirAll(filepath.Join(controllerHome, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"id_ed25519":     "integration private identity\n",
		"id_ed25519.pub": "integration public identity\n",
		"known_hosts":    "github.com fixture-host-key\n",
	} {
		mode := os.FileMode(0o600)
		if name == "id_ed25519.pub" || name == "known_hosts" {
			mode = 0o644
		}
		if err := os.WriteFile(filepath.Join(controllerHome, ".ssh", name), []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	nscdSource := filepath.Join(base, "integration-nscd.socket")
	if err := os.WriteFile(nscdSource, []byte("integration-only bind source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolverSource := filepath.Join(base, "integration-resolv.conf")
	if err := os.WriteFile(resolverSource, []byte("nameserver 192.0.2.53\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleHome := filepath.Join(base, "agent-worktrees", "agent2", "ouroboros-ide", ".devhome-agent2")
	if err := os.MkdirAll(staleHome, 0o700); err != nil {
		t.Fatal(err)
	}
	staleMarker := filepath.Join(staleHome, "preexisting-custody")
	if err := os.WriteFile(staleMarker, []byte("preserve\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env := isolatedNativeFixtureEnv(
		"DEVKIT_ROOT="+root,
		"DEVKIT_NO_TMUX=1",
		"DEVKIT_RUNTIME_BROKER_BINARY="+brokerExecutable,
		"DEVKIT_RUNTIME_BWRAP_BINARY="+filepath.Join(sshDir, "bwrap"),
		"DEVKIT_RUNTIME_SHELL_LAUNCHER="+testExecutable,
		"DEVKIT_INTEGRATION_BROKER_HELPER=1",
		"DEVKIT_INTEGRATION_NSCD_SOURCE="+nscdSource,
		"DEVKIT_INTEGRATION_RESOLV_SOURCE="+resolverSource,
		"HOME="+controllerHome,
		"CODEX_AUTH_JSON="+filepath.Join(base, "missing-auth.json"),
		"PATH="+sshDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	runReset := func() ([]byte, error) {
		command := exec.Command(packageDevctl, "-p", "dev-all", "reset", "3")
		command.Env = env
		return command.CombinedOutput()
	}
	t.Cleanup(func() {
		command := exec.Command(packageDevctl, "-p", "dev-all", "down", "--repo", "ouroboros-ide", "--count", "3")
		command.Env = env
		_ = command.Run()
	})

	output, err := runReset()
	if err == nil {
		t.Fatalf("installed top-level reset accepted a failed package SSH fetch:\n%s", output)
	}
	if !strings.Contains(string(output), "fetch --all --prune") {
		t.Fatalf("failed package SSH fetch was not classified at bootstrap: %v\n%s", err, output)
	}
	if _, readErr := os.Lstat(staleMarker); !errors.Is(readErr, os.ErrNotExist) {
		t.Fatalf("failed reset did not dispose stale package-owned payload: %v", readErr)
	}
	for _, path := range []string{
		filepath.Join(base, "agent-worktrees", "agent1"),
		filepath.Join(base, "agent-worktrees", "agent3"),
		filepath.Join(base, "agent-worktrees", ".devkit", "git", "ouroboros-ide.git"),
		brokerSocket,
		filepath.Join(base, ".devkit", "native-broker", "broker.pid"),
		filepath.Join(base, ".devkit", "native-broker", "broker.json"),
	} {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			t.Fatalf("failed reset left package lifecycle residue %s: %v", path, statErr)
		}
	}
	partialBootstrap, err := filepath.Glob(filepath.Join(base, "agent-worktrees", ".devkit", "git", ".ouroboros-ide.bootstrap-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(partialBootstrap) != 0 {
		t.Fatalf("failed reset left partial bootstrap repositories: %v", partialBootstrap)
	}
	assertNoNativeResetProxyResidue(t, base)
	if err := os.WriteFile(sshAllow, []byte("allow fixture fetch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sshLog, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	output, err = runReset()
	if err != nil {
		t.Fatalf("installed top-level reset failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"capacity_available: 3/3",
		"runtime_ready: 3/3",
		"repo_ready: 3/3",
		"agent1_status: status=ready worktree=ready broker=ready sandbox=ready tooling=ready",
		"agent2_status: status=ready worktree=ready broker=ready sandbox=ready tooling=ready",
		"agent3_status: status=ready worktree=ready broker=ready sandbox=ready tooling=ready",
	} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("installed reset omitted accepted readiness %q:\n%s", want, output)
		}
	}
	worktreesRoot := filepath.Join(base, "agent-worktrees")
	wantCommonDir := filepath.Join(worktreesRoot, ".devkit", "git", "ouroboros-ide.git")
	for index := 1; index <= 3; index++ {
		worktree := filepath.Join(worktreesRoot, fmt.Sprintf("agent%d", index), "ouroboros-ide")
		gitFile, err := os.ReadFile(filepath.Join(worktree, ".git"))
		if err != nil {
			t.Fatalf("agent%d linked worktree missing: %v", index, err)
		}
		if value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(gitFile)), "gitdir:")); value == "" || filepath.IsAbs(value) {
			t.Fatalf("agent%d gitdir is not package-relative: %q", index, gitFile)
		}
		gotCommonDir := strings.TrimSpace(runNativeFixtureCommand(t, "git", "-C", worktree, "rev-parse", "--git-common-dir"))
		if !filepath.IsAbs(gotCommonDir) {
			gotCommonDir = filepath.Join(worktree, gotCommonDir)
		}
		if got := filepath.Clean(gotCommonDir); got != wantCommonDir {
			t.Fatalf("agent%d common repository = %q, want package-owned %q", index, got, wantCommonDir)
		}
		gitdir := strings.TrimSpace(runNativeFixtureCommand(t, "git", "-C", worktree, "rev-parse", "--git-dir"))
		if !filepath.IsAbs(gitdir) {
			gitdir = filepath.Join(worktree, gitdir)
		}
		for name, path := range map[string]string{
			"commondir": filepath.Join(gitdir, "commondir"),
			"reverse":   filepath.Join(gitdir, "gitdir"),
		} {
			value, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("agent%d %s metadata missing: %v", index, name, err)
			}
			if filepath.IsAbs(strings.TrimSpace(string(value))) {
				t.Fatalf("agent%d %s metadata is not portable: %q", index, name, value)
			}
		}
		if out := runNativeFixtureCommand(t, "git", "-C", worktree, "status", "--porcelain=v1"); strings.TrimSpace(out) != "" {
			t.Fatalf("agent%d reset worktree is dirty: %s", index, out)
		}
		hostHome := filepath.Join(filepath.Dir(worktree), fmt.Sprintf(".devhome-agent%d", index))
		if index == 1 {
			hostHome = filepath.Join(worktree, ".devhome-agent1")
		}
		if _, err := os.Stat(filepath.Join(hostHome, ".codex", "config.toml")); err != nil {
			t.Fatalf("agent%d source-defined app-server config missing: %v", index, err)
		}
	}
	if _, err := os.Lstat(staleMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale package-owned agent home survived destructive reset: %v", err)
	}
	sshInvocations, err := os.ReadFile(sshLog)
	if err != nil {
		t.Fatal(err)
	}
	wantConfig := filepath.Join(worktreesRoot, "agent1", "ouroboros-ide", ".devhome-agent1", ".ssh", "config")
	if strings.Count(string(sshInvocations), "git-upload-pack") != 1 ||
		!strings.Contains(string(sshInvocations), "-F "+wantConfig) {
		t.Fatalf("reset did not use exactly one package-owned SSH bootstrap:\n%s", sshInvocations)
	}
	manifest := filepath.Join(base, ".devkit", "native-agents", "manifests", "dev-all.json")
	manifestData, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("reset manifest missing: %v", err)
	}
	for index := 1; index <= 3; index++ {
		if !strings.Contains(string(manifestData), fmt.Sprintf(`"index": %d`, index)) {
			t.Fatalf("reset manifest omitted agent%d:\n%s", index, manifestData)
		}
	}
	assertNoNativeResetProxyResidue(t, base)

	foreignRoot := filepath.Join(base, "foreign-common.git", "worktrees", "protected")
	if err := os.MkdirAll(foreignRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	foreignSentinel := filepath.Join(foreignRoot, "accepted-business-data")
	if err := os.WriteFile(foreignSentinel, []byte("outside reset custody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	foreignGitFile := filepath.Join(worktreesRoot, "agent3", "ouroboros-ide", ".git")
	foreignContent := []byte("gitdir: " + foreignRoot + "\n")
	if err := os.WriteFile(foreignGitFile, foreignContent, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreesRoot, "agent3", "ouroboros-ide", "stale-payload"), []byte("discard\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	beforeForeignSSH := append([]byte(nil), sshInvocations...)
	output, err = runReset()
	if err != nil {
		t.Fatalf("reset did not reconstruct opaque in-prefix foreign metadata: %v\n%s", err, output)
	}
	if after, readErr := os.ReadFile(foreignGitFile); readErr != nil ||
		string(after) == string(foreignContent) ||
		filepath.IsAbs(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(after)), "gitdir:"))) {
		t.Fatalf("foreign in-prefix Git pointer was not replaced by portable package metadata: %q %v", after, readErr)
	}
	if got, readErr := os.ReadFile(foreignSentinel); readErr != nil || string(got) != "outside reset custody\n" {
		t.Fatalf("reset acquired custody of foreign Git metadata: %q %v", got, readErr)
	}
	if after, readErr := os.ReadFile(sshLog); readErr != nil ||
		strings.Count(string(after), "git-upload-pack") != strings.Count(string(beforeForeignSSH), "git-upload-pack")+1 {
		t.Fatalf("foreign payload reconstruction did not use one fresh package SSH bootstrap: %q %v", after, readErr)
	}
	assertNoNativeResetProxyResidue(t, base)

	assertFailedResetClean := func(label string, output []byte, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s unexpectedly succeeded:\n%s", label, output)
		}
		for _, path := range []string{
			filepath.Join(base, "agent-worktrees", "agent1"),
			filepath.Join(base, "agent-worktrees", "agent2"),
			filepath.Join(base, "agent-worktrees", "agent3"),
			filepath.Join(base, "agent-worktrees", ".devkit", "git", "ouroboros-ide.git"),
			filepath.Join(base, ".devkit", "native-agents", "dev-all-agent1"),
			filepath.Join(base, ".devkit", "native-agents", "dev-all-agent2"),
			filepath.Join(base, ".devkit", "native-agents", "dev-all-agent3"),
			filepath.Join(base, ".devkit", "native-agents", "manifests", "dev-all.json"),
			brokerSocket,
			filepath.Join(base, ".devkit", "native-broker", "broker.pid"),
			filepath.Join(base, ".devkit", "native-broker", "broker.json"),
		} {
			if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("%s left package lifecycle residue %s: %v", label, path, statErr)
			}
		}
		assertNoNativeResetProxyResidue(t, base)
		if got, readErr := os.ReadFile(foreignSentinel); readErr != nil || string(got) != "outside reset custody\n" {
			t.Fatalf("%s touched outside-prefix data: %q %v", label, got, readErr)
		}
	}

	if err := os.Remove(sshAllow); err != nil {
		t.Fatal(err)
	}
	output, err = runReset()
	assertFailedResetClean("fetch failure", output, err)
	if !strings.Contains(string(output), "fetch --all --prune") {
		t.Fatalf("fetch failure did not reach package SSH bootstrap:\n%s", output)
	}
	if err := os.WriteFile(sshAllow, []byte("allow fixture fetch\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(nscdSource); err != nil {
		t.Fatal(err)
	}
	output, err = runReset()
	assertFailedResetClean("preparation failure", output, err)
	if !strings.Contains(string(output), nscdSource) {
		t.Fatalf("preparation failure was not attributed to the source-defined required bind:\n%s", output)
	}
	if err := os.WriteFile(nscdSource, []byte("integration-only bind source\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(readinessFail, []byte("fail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err = runReset()
	assertFailedResetClean("readiness failure", output, err)
	if !strings.Contains(string(output), "native capacity is not fully available") {
		t.Fatalf("readiness failure did not reach the source-defined readiness gate:\n%s", output)
	}
	if err := os.Remove(readinessFail); err != nil {
		t.Fatal(err)
	}

	output, err = runReset()
	if err != nil {
		t.Fatalf("reset did not recover cleanly after failure cleanup: %v\n%s", err, output)
	}
	assertNoNativeResetProxyResidue(t, base)
}

func TestInstalledRuntimeEmptyRootReconstructsThreeSlotsWithRealReadiness(t *testing.T) {
	runtimeDevctl := strings.TrimSpace(os.Getenv("DEVKIT_TEST_INSTALLED_RUNTIME_DEVCTL"))
	runtimeOverlays := strings.TrimSpace(os.Getenv("DEVKIT_TEST_INSTALLED_RUNTIME_OVERLAYS"))
	runtimeBroker := strings.TrimSpace(os.Getenv("DEVKIT_TEST_INSTALLED_RUNTIME_BROKER"))
	runtimeShell := strings.TrimSpace(os.Getenv("DEVKIT_TEST_INSTALLED_RUNTIME_SHELL"))
	runtimeBwrap := strings.TrimSpace(os.Getenv("DEVKIT_TEST_INSTALLED_RUNTIME_BWRAP"))
	runtimeSSH := strings.TrimSpace(os.Getenv("DEVKIT_TEST_INSTALLED_SSH_EXECUTABLE"))
	runtimeKnownHosts := strings.TrimSpace(os.Getenv("DEVKIT_TEST_INSTALLED_KNOWN_HOSTS"))
	runtimeCodexConfig := strings.TrimSpace(os.Getenv("DEVKIT_TEST_INSTALLED_CODEX_CONFIG_SOURCE"))
	if runtimeDevctl == "" || runtimeOverlays == "" || runtimeBroker == "" ||
		runtimeShell == "" || runtimeBwrap == "" || runtimeSSH == "" ||
		runtimeKnownHosts == "" || runtimeCodexConfig == "" {
		t.Skip("installed immutable runtime inputs are not supplied")
	}
	for label, path := range map[string]string{
		"devctl":     runtimeDevctl,
		"broker":     runtimeBroker,
		"shell":      runtimeShell,
		"bubblewrap": runtimeBwrap,
		"ssh":        runtimeSSH,
	} {
		if info, err := os.Stat(path); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			t.Fatalf("%s runtime executable is not usable: %s: %v", label, path, err)
		}
	}
	if info, err := os.Stat(runtimeOverlays); err != nil || !info.IsDir() {
		t.Fatalf("runtime overlays are not usable: %s: %v", runtimeOverlays, err)
	}
	if info, err := os.Stat(runtimeCodexConfig); err != nil || info.IsDir() {
		t.Fatalf("runtime Codex config source is not usable: %s: %v", runtimeCodexConfig, err)
	}
	expectedKnownHosts, err := os.ReadFile(runtimeKnownHosts)
	if err != nil || len(expectedKnownHosts) == 0 {
		t.Fatalf("runtime known-hosts source is not usable: %s: %v", runtimeKnownHosts, err)
	}

	fixtureRoot, err := os.MkdirTemp("/tmp", "drr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(fixtureRoot) })
	seed := filepath.Join(fixtureRoot, "seed")
	remote := filepath.Join(fixtureRoot, "product.git")
	runNativeFixtureCommand(t, "git", "init", "--initial-branch=main", seed)
	runNativeFixtureCommand(t, "git", "-C", seed, "config", "user.name", "Installed Runtime Regression")
	runNativeFixtureCommand(t, "git", "-C", seed, "config", "user.email", "installed-runtime@example.invalid")
	for path, content := range map[string]string{
		"README.md":  "installed runtime fixture\n",
		".gitignore": ".devhome-agent*\n",
	} {
		if err := os.WriteFile(filepath.Join(seed, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runNativeFixtureCommand(t, "git", "-C", seed, "add", ".")
	runNativeFixtureCommand(t, "git", "-C", seed, "commit", "-m", "installed runtime fixture")
	runNativeFixtureCommand(t, "git", "init", "--bare", "--initial-branch=main", remote)
	runNativeFixtureCommand(t, "git", "-C", seed, "push", remote, "main:main")
	expectedHead := strings.TrimSpace(runNativeFixtureCommand(t, "git", "-C", seed, "rev-parse", "HEAD"))

	consumerCount := 2
	if strings.TrimSpace(os.Getenv("DEVKIT_TEST_SINGLE_FRESH_CONSUMER")) == "1" {
		consumerCount = 1
	}
	for consumer := 1; consumer <= consumerCount; consumer++ {
		consumer := consumer
		t.Run(fmt.Sprintf("consumer-%d", consumer), func(t *testing.T) {
			root := filepath.Join(fixtureRoot, fmt.Sprintf("consumer-%d", consumer))
			devkitRoot := filepath.Join(root, "devkit")
			controllerHome := filepath.Join(root, "controller-home")
			fixtureBin := filepath.Join(root, "fixture-bin")
			for _, dir := range []string{
				devkitRoot,
				filepath.Join(controllerHome, ".ssh"),
				fixtureBin,
			} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if entries, err := os.ReadDir(devkitRoot); err != nil || len(entries) != 0 {
				t.Fatalf("controller Devkit root is not initially empty: entries=%v error=%v", entries, err)
			}
			for name, content := range map[string]string{
				"id_ed25519":     "fixture private identity\n",
				"id_ed25519.pub": "fixture public identity\n",
				"known_hosts":    "[ssh.github.com]:443 ambient-wrong-host-key\n",
			} {
				mode := os.FileMode(0o600)
				if name != "id_ed25519" {
					mode = 0o644
				}
				if err := os.WriteFile(filepath.Join(controllerHome, ".ssh", name), []byte(content), mode); err != nil {
					t.Fatal(err)
				}
			}
			nscdSource := filepath.Join(root, "fixture-nscd.socket")
			if err := os.WriteFile(nscdSource, []byte("fixture bind source\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			resolverSource := filepath.Join(root, "fixture-resolv.conf")
			if err := os.WriteFile(resolverSource, []byte("nameserver 192.0.2.53\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			sshLog := filepath.Join(root, "ssh.log")
			ambientSSHSentinel := filepath.Join(root, "ambient-ssh-selected")
			sshScript := "#!/bin/sh\nprintf 'ambient ssh selected\\n' > " +
				strconv.Quote(ambientSSHSentinel) + "\nexit 97\n"
			if err := os.WriteFile(filepath.Join(fixtureBin, "ssh"), []byte(sshScript), 0o755); err != nil {
				t.Fatal(err)
			}
			proxyUsed := filepath.Join(root, "package-proxy-used")
			appServerBoundaryLog := filepath.Join(root, "app-server-boundary.log")
			outerProxy, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			proxyTarget := make(chan string, 1)
			proxyErr := make(chan error, 1)
			go func() {
				defer outerProxy.Close()
				client, err := outerProxy.Accept()
				if err != nil {
					proxyErr <- err
					return
				}
				defer client.Close()
				req, err := http.ReadRequest(bufio.NewReader(client))
				if err != nil {
					proxyErr <- err
					return
				}
				proxyTarget <- req.Host
				if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
					proxyErr <- err
					return
				}
				_, err = io.Copy(io.Discard, client)
				proxyErr <- err
			}()
			env := isolatedNativeFixtureEnv(
				"HOME="+controllerHome,
				"DEVKIT_ROOT="+devkitRoot,
				"DEVKIT_OVERLAYS_DIR="+runtimeOverlays,
				"DEVKIT_NO_TMUX=1",
				"DEVKIT_RUNTIME_BROKER_BINARY="+runtimeBroker,
				"DEVKIT_RUNTIME_BWRAP_BINARY="+runtimeBwrap,
				"DEVKIT_RUNTIME_SHELL_LAUNCHER="+runtimeShell,
				"DEVKIT_CODEX_CONFIG_SOURCE="+runtimeCodexConfig,
				"DEVKIT_INTEGRATION_NSCD_SOURCE="+nscdSource,
				"DEVKIT_INTEGRATION_RESOLV_SOURCE="+resolverSource,
				"DEVKIT_TEST_PRODUCT_REMOTE="+remote,
				"DEVKIT_TEST_PRODUCT_SSH_LOG="+sshLog,
				"DEVKIT_TEST_PRODUCT_PROXY_USED="+proxyUsed,
				"DEVKIT_TEST_APP_SERVER_BOUNDARY_LOG="+appServerBoundaryLog,
				"DEVKIT_NATIVE_ISOLATION_PROFILE=workspace-egress",
				"HTTPS_PROXY=http://"+outerProxy.Addr().String(),
				"HTTP_PROXY=http://"+outerProxy.Addr().String(),
				"CODEX_AUTH_JSON="+filepath.Join(root, "missing-auth.json"),
				"PATH="+fixtureBin+string(os.PathListSeparator)+os.Getenv("PATH"),
			)
			run := func(args ...string) ([]byte, error) {
				command := exec.Command(runtimeDevctl, args...)
				command.Env = env
				return command.CombinedOutput()
			}
			t.Cleanup(func() {
				_, _ = run("-p", "dev-all", "down", "--repo", "ouroboros-ide", "--count", "3")
			})

			output, err := run("-p", "dev-all", "reset", "3")
			if err != nil {
				proxyDetail := "outer proxy was not reached"
				select {
				case target := <-proxyTarget:
					proxyDetail = "outer proxy target=" + target
				default:
				}
				if invocations, readErr := os.ReadFile(sshLog); readErr == nil {
					proxyDetail += "\nSSH invocations:\n" + string(invocations)
				}
				t.Fatalf("installed empty-root reset failed: %v\n%s\n%s", err, output, proxyDetail)
			}
			for _, want := range []string{
				"capacity_available: 3/3",
				"runtime_ready: 3/3",
				"repo_ready: 3/3",
				"agent1_status: status=ready worktree=ready broker=ready sandbox=ready tooling=ready",
				"agent2_status: status=ready worktree=ready broker=ready sandbox=ready tooling=ready",
				"agent3_status: status=ready worktree=ready broker=ready sandbox=ready tooling=ready",
			} {
				if !strings.Contains(string(output), want) {
					t.Fatalf("installed reset omitted %q:\n%s", want, output)
				}
			}
			if _, err := os.Stat(filepath.Join(devkitRoot, "flake.nix")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("installed reset acquired a mutable Devkit flake: %v", err)
			}
			if _, err := os.Lstat(ambientSSHSentinel); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("installed reset selected hostile ambient ssh: %v", err)
			}
			if _, err := os.Stat(proxyUsed); err != nil {
				t.Fatalf("package SSH executable did not traverse the source-selected proxy: %v", err)
			}
			select {
			case target := <-proxyTarget:
				if target != "ssh.github.com:443" {
					t.Fatalf("package proxy CONNECT target = %q", target)
				}
			case <-time.After(time.Second):
				t.Fatal("package proxy did not reach the local hermetic CONNECT fixture")
			}
			if err := <-proxyErr; err != nil {
				t.Fatalf("hermetic outer proxy: %v", err)
			}
			sshInvocations, err := os.ReadFile(sshLog)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(sshInvocations), "git-upload-pack") != 1 ||
				!strings.Contains(string(sshInvocations), "-F ") {
				t.Fatalf("installed reset did not use one package-owned Product checkout fetch:\n%s", sshInvocations)
			}
			worktreesRoot := filepath.Join(root, "agent-worktrees")
			wantCommonDir := filepath.Join(worktreesRoot, ".devkit", "git", "ouroboros-ide.git")
			for index := 1; index <= 3; index++ {
				worktree := filepath.Join(worktreesRoot, fmt.Sprintf("agent%d", index), "ouroboros-ide")
				if got := strings.TrimSpace(runNativeFixtureCommand(t, "git", "-C", worktree, "rev-parse", "HEAD")); got != expectedHead {
					t.Fatalf("agent%d HEAD = %s, want %s", index, got, expectedHead)
				}
				if got := strings.TrimSpace(runNativeFixtureCommand(t, "git", "-C", worktree, "status", "--porcelain=v1")); got != "" {
					t.Fatalf("agent%d is dirty: %s", index, got)
				}
				gitFile, err := os.ReadFile(filepath.Join(worktree, ".git"))
				if err != nil || filepath.IsAbs(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(gitFile)), "gitdir:"))) {
					t.Fatalf("agent%d Git metadata is not portable: %q %v", index, gitFile, err)
				}
				gotCommonDir := strings.TrimSpace(runNativeFixtureCommand(t, "git", "-C", worktree, "rev-parse", "--git-common-dir"))
				if !filepath.IsAbs(gotCommonDir) {
					gotCommonDir = filepath.Join(worktree, gotCommonDir)
				}
				if filepath.Clean(gotCommonDir) != wantCommonDir {
					t.Fatalf("agent%d common directory = %q, want %q", index, gotCommonDir, wantCommonDir)
				}
				gitDir := strings.TrimSpace(runNativeFixtureCommand(t, "git", "-C", worktree, "rev-parse", "--git-dir"))
				if !filepath.IsAbs(gitDir) {
					gitDir = filepath.Join(worktree, gitDir)
				}
				for name, path := range map[string]string{
					"commondir": filepath.Join(gitDir, "commondir"),
					"reverse":   filepath.Join(gitDir, "gitdir"),
				} {
					value, err := os.ReadFile(path)
					if err != nil {
						t.Fatalf("agent%d %s metadata missing: %v", index, name, err)
					}
					if filepath.IsAbs(strings.TrimSpace(string(value))) {
						t.Fatalf("agent%d %s metadata is not relative: %q", index, name, value)
					}
				}
				hostHome := filepath.Join(filepath.Dir(worktree), fmt.Sprintf(".devhome-agent%d", index))
				if index == 1 {
					hostHome = filepath.Join(worktree, ".devhome-agent1")
				}
				if _, err := os.Stat(filepath.Join(hostHome, ".codex", "config.toml")); err != nil {
					t.Fatalf("agent%d package app-server configuration is absent: %v", index, err)
				}
				sandboxWorktree := filepath.Join(
					"/workspaces/dev/agent-worktrees",
					fmt.Sprintf("agent%d", index),
					"ouroboros-ide",
				)
				sandboxHome := filepath.Join(filepath.Dir(sandboxWorktree), fmt.Sprintf(".devhome-agent%d", index))
				if index == 1 {
					sandboxHome = filepath.Join(sandboxWorktree, ".devhome-agent1")
				}
				identity, err := os.ReadFile(filepath.Join(hostHome, ".ssh", "id_ed25519"))
				if err != nil || string(identity) != "fixture private identity\n" {
					t.Fatalf("agent%d did not preserve the protected source identity: %q %v", index, identity, err)
				}
				knownHosts, err := os.ReadFile(filepath.Join(hostHome, ".ssh", "known_hosts"))
				if err != nil || string(knownHosts) != string(expectedKnownHosts) {
					t.Fatalf(
						"agent%d known-hosts = %q error=%v, want exact immutable package material %q",
						index,
						knownHosts,
						err,
						expectedKnownHosts,
					)
				}
				sshConfig, err := os.ReadFile(filepath.Join(hostHome, ".ssh", "config"))
				if err != nil {
					t.Fatalf("agent%d source-derived SSH config missing: %v", index, err)
				}
				for _, want := range []string{
					"IdentityFile " + filepath.Join(sandboxHome, ".ssh", "id_ed25519"),
					"ProxyCommand nc -X connect -x 127.0.0.1:18888 %h %p",
				} {
					if !strings.Contains(string(sshConfig), want) {
						t.Fatalf("agent%d SSH config missing %q:\n%s", index, want, sshConfig)
					}
				}
				wantSSHCommand := "'" + runtimeSSH + "' -F '" + filepath.Join(sandboxHome, ".ssh", "config") + "'"
				if got := strings.TrimSpace(runNativeFixtureCommand(
					t,
					"git",
					"config",
					"--file",
					filepath.Join(hostHome, ".gitconfig"),
					"--get",
					"core.sshCommand",
				)); got != wantSSHCommand {
					t.Fatalf("agent%d global core.sshCommand = %q, want %q", index, got, wantSSHCommand)
				}
				if got := strings.TrimSpace(runNativeFixtureCommand(
					t,
					"git",
					"-C",
					worktree,
					"config",
					"--worktree",
					"--get",
					"core.sshCommand",
				)); got != wantSSHCommand {
					t.Fatalf("agent%d worktree core.sshCommand = %q, want %q", index, got, wantSSHCommand)
				}
				runNativeFixtureCommand(t, "git", "-C", worktree, "config", "--worktree", "devkit.fixtureWritable", "true")
				if got := strings.TrimSpace(runNativeFixtureCommand(
					t,
					"git",
					"-C",
					worktree,
					"config",
					"--worktree",
					"--get",
					"devkit.fixtureWritable",
				)); got != "true" {
					t.Fatalf("agent%d worktree config was not writable: %q", index, got)
				}
				runNativeFixtureCommand(t, "git", "-C", worktree, "config", "--worktree", "--unset", "devkit.fixtureWritable")
				runNativeFixtureCommand(t, "git", "-C", worktree, "update-ref", "refs/devkit/fresh-consumer-proof", "HEAD")
				runNativeFixtureCommand(t, "git", "-C", worktree, "update-ref", "-d", "refs/devkit/fresh-consumer-proof")
			}

			planOutput, err := run("-p", "dev-all", "native", "plan", "--repo", "ouroboros-ide", "--index", "1", "--format", "json")
			if err != nil {
				t.Fatalf("installed plan failed: %v\n%s", err, planOutput)
			}
			var plan struct {
				RuntimeLauncher  string `json:"runtime_launcher"`
				BubblewrapBinary string `json:"bubblewrap_binary"`
			}
			if err := json.Unmarshal(planOutput, &plan); err != nil {
				t.Fatalf("decode installed plan: %v\n%s", err, planOutput)
			}
			if plan.RuntimeLauncher != runtimeShell || plan.BubblewrapBinary != runtimeBwrap {
				t.Fatalf("installed plan runtime executables = %#v, want shell=%s bwrap=%s", plan, runtimeShell, runtimeBwrap)
			}
			appOutput, err := run("-p", "dev-all", "exec", "1", "--repo", "ouroboros-ide", "--", "true")
			if err != nil {
				t.Fatalf("installed app-server/task launch boundary failed: %v\n%s", err, appOutput)
			}
			if !strings.Contains(string(appOutput), "__DEVKIT_APP_SERVER_BOUNDARY__=PASS") {
				t.Fatalf("installed lifecycle did not reach the app-server/task launch boundary:\n%s", appOutput)
			}
			boundary, err := os.ReadFile(appServerBoundaryLog)
			if err != nil {
				t.Fatalf("app-server/task launch boundary receipt missing: %v", err)
			}
			for _, want := range []string{
				"HOME=/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1",
				"CODEX_HOME=/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1/.codex",
				"CONFIG_PRESENT=1",
			} {
				if !strings.Contains(string(boundary), want) {
					t.Fatalf("app-server/task launch boundary missing %q:\n%s", want, boundary)
				}
			}

			brokerPIDPath := filepath.Join(root, ".devkit", "native-broker", "broker.pid")
			brokerPIDData, err := os.ReadFile(brokerPIDPath)
			if err != nil {
				t.Fatalf("running package broker PID receipt missing: %v", err)
			}
			brokerPID, err := strconv.Atoi(strings.TrimSpace(string(brokerPIDData)))
			if err != nil || brokerPID <= 0 {
				t.Fatalf("invalid running package broker PID %q: %v", brokerPIDData, err)
			}
			downOutput, err := run("-p", "dev-all", "down", "--repo", "ouroboros-ide", "--count", "3")
			if err != nil {
				t.Fatalf("installed down failed: %v\n%s", err, downOutput)
			}
			for _, residue := range []string{
				filepath.Join(root, ".devkit", "native-broker", "broker.sock"),
				filepath.Join(root, ".devkit", "native-broker", "broker.pid"),
				filepath.Join(root, ".devkit", "native-broker", "broker.json"),
			} {
				if _, err := os.Lstat(residue); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("installed lifecycle left residue %s: %v", residue, err)
				}
			}
			if err := syscall.Kill(brokerPID, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatalf("installed lifecycle left broker process %d: %v", brokerPID, err)
			}
			assertNoNativeResetProxyResidue(t, root)
			if err := os.RemoveAll(root); err != nil {
				t.Fatalf("remove disposable fresh consumer: %v", err)
			}
			if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("fresh-consumer gate left checkout or metadata residue: %v", err)
			}
		})
	}
}

func TestNativePrepareRejectsUnavailablePackageSSHBeforeEffects(t *testing.T) {
	boundSSH := filepath.Join(t.TempDir(), "package-owned", "bin", "ssh")
	bin := buildDevctlForNativeDefaultsWithSSHAndTags(t, boundSSH)
	root := nativeDefaultsRoot(t)
	controllerHome := filepath.Join(t.TempDir(), "controller-home")
	if err := os.MkdirAll(controllerHome, 0o700); err != nil {
		t.Fatal(err)
	}
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := isolatedNativeFixtureEnv(
		"HOME="+controllerHome,
		"DEVKIT_ROOT="+root,
		"DEVKIT_NO_TMUX=1",
		"DEVKIT_RUNTIME_BROKER_BINARY="+testExecutable,
		"DEVKIT_RUNTIME_BWRAP_BINARY="+testExecutable,
		"DEVKIT_RUNTIME_SHELL_LAUNCHER="+testExecutable,
	)
	run := func() []byte {
		t.Helper()
		command := exec.Command(
			bin,
			"-p", "dev-all",
			"native", "prepare",
			"--repo", "ouroboros-ide",
			"--count", "1",
		)
		command.Env = env
		output, err := command.CombinedOutput()
		if err == nil {
			t.Fatalf("native prepare accepted unavailable package SSH:\n%s", output)
		}
		if !strings.Contains(string(output), "package-owned OpenSSH executable") {
			t.Fatalf("native prepare did not identify the unavailable package SSH authority: %v\n%s", err, output)
		}
		return output
	}
	assertNoEffects := func(label string) {
		t.Helper()
		for _, path := range []string{
			filepath.Join(filepath.Dir(root), "agent-worktrees"),
			filepath.Join(filepath.Dir(root), ".devkit", "native-agents"),
			filepath.Join(filepath.Dir(root), ".devkit", "native-broker"),
			filepath.Join(controllerHome, ".ssh"),
		} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s package SSH failure left effect %s: %v", label, path, err)
			}
		}
		assertNoNativeResetProxyResidue(t, filepath.Dir(root))
	}

	run()
	assertNoEffects("missing")
	if err := os.MkdirAll(filepath.Dir(boundSSH), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(boundSSH, []byte("#!/bin/sh\nexit 99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run()
	assertNoEffects("non-executable")
}

func TestMain(m *testing.M) {
	if os.Getenv("DEVKIT_INTEGRATION_BROKER_HELPER") == "1" {
		os.Exit(runNativeResetBrokerHelper())
	}
	os.Exit(m.Run())
}

func runNativeResetBrokerHelper() int {
	listen := strings.TrimSpace(os.Getenv("BROKER_LISTEN"))
	if listen == "" {
		return 2
	}
	socket := strings.TrimPrefix(listen, "unix://")
	if socket == listen || strings.TrimSpace(socket) == "" {
		return 2
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0o755); err != nil {
		return 2
	}
	_ = os.Remove(socket)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return 2
	}
	defer listener.Close()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return 0
		}
		_ = conn.Close()
	}
}

func assertNoNativeResetProxyResidue(t *testing.T, root string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, ".devkit", "native-egress", "*.sock"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("reset left managed egress proxy sockets: %v", matches)
	}
}

func TestDevAllCheckAndHookHelpersUseNativeExecDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nativeDefaultsRoot(t)

	for _, args := range [][]string{{"check-net"}, {"check-codex"}, {"check-sts", "tinyproxy"}, {"warm"}} {
		out, err := runNativeDefaultDryRun(t, bin, root, args...)
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
		assertNoDockerCommand(t, out)
		if !strings.Contains(out, " -p dev-all exec 1 --repo ouroboros-ide -- bash -lc") {
			t.Fatalf("%v missing native exec:\n%s", args, out)
		}
	}
}

func TestDevAllLayoutApplyRejectsUnsafeOrUnknownArgsDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nativeDefaultsRoot(t)
	layout := filepath.Join(root, "layout.yaml")
	if err := os.WriteFile(layout, []byte(`
session: native-front
windows:
  - index: 1
    project: dev-all
    service: dev-agent
    path: /workspaces/dev/ouroboros-ide
`), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "skip broker without skip ready",
			args: []string{"layout-apply", "--file", layout, "--skip-broker"},
			want: "--skip-broker requires --skip-ready",
		},
		{
			name: "unknown argument",
			args: []string{"layout-apply", "--file", layout, "--definitely-unknown"},
			want: "layout-apply: unknown argument --definitely-unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runNativeDefaultDryRun(t, bin, root, tt.args...)
			if err == nil {
				t.Fatalf("%s unexpectedly succeeded:\n%s", tt.name, out)
			}
			assertNoDockerCommand(t, out)
			if !strings.Contains(out, tt.want) {
				t.Fatalf("%s did not include %q:\n%s", tt.name, tt.want, out)
			}
		})
	}
}

func TestDevAllMixedLayoutRefusesNonNativeFallbackDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nativeDefaultsRoot(t)
	layout := filepath.Join(root, "mixed-layout.yaml")
	if err := os.WriteFile(layout, []byte(`
session: mixed
windows:
  - index: 1
    project: front
    service: dev-agent
    path: /workspace
`), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runNativeDefaultDryRun(t, bin, root, "layout-apply", "--file", layout)
	if err == nil {
		t.Fatalf("mixed dev-all layout unexpectedly succeeded:\n%s", out)
	}
	assertNoDockerCommand(t, out)
	if !strings.Contains(out, "only supports native single-overlay layouts") {
		t.Fatalf("unexpected mixed-layout failure:\n%s", out)
	}
}

func TestNonNativeLayoutCannotTargetDevAllDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nonFlakeRoot(t)
	layout := filepath.Join(root, "non-native-devall-layout.yaml")
	if err := os.WriteFile(layout, []byte(`
session: non-native-devall
windows:
  - index: 1
    project: dev-all
    service: dev-agent
    path: /workspaces/dev/ouroboros-ide
`), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runProjectDryRun(t, bin, root, "codex", "layout-apply", "--file", layout)
	if err == nil {
		t.Fatalf("non-native layout targeting dev-all unexpectedly succeeded:\n%s", out)
	}
	assertNoDockerCommand(t, out)
	if !strings.Contains(out, "only supports native single-overlay layouts") {
		t.Fatalf("unexpected non-native layout failure:\n%s", out)
	}
}

func TestNativePrepareCarriesDelayedPackThroughActualOpenSSHProxyCommand(t *testing.T) {
	base, err := os.MkdirTemp("", "dvc-ssh-chain-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	root := filepath.Join(base, "devkit")
	packageDevctl := filepath.Join(root, "kit", "bin", "devctl")
	if err := os.MkdirAll(filepath.Dir(packageDevctl), 0o755); err != nil {
		t.Fatal(err)
	}
	sshExecutable, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	sshExecutable, err = filepath.Abs(sshExecutable)
	if err != nil {
		t.Fatal(err)
	}
	overlay := `
defaults:
  repo: test-repo
  agents: 1
  base_branch: main
  branch_prefix: agent
runtime:
  flake: ./overlays/dev-all#default
readiness:
  default_mode: runtime-only
native:
  worktree_root: ../agent-worktrees
  state_root: ../.devkit/native-agents
  worktree_container_root: /worktrees
  state_container_root: /agent-state
`
	overlayPath := filepath.Join(root, "overlays", "dev-all", "devkit.yaml")
	if err := os.MkdirAll(filepath.Dir(overlayPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlayPath, []byte(overlay), 0o644); err != nil {
		t.Fatal(err)
	}
	allowlistPath := filepath.Join(root, "kit", "proxy", "allowlist.txt")
	if err := os.MkdirAll(filepath.Dir(allowlistPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(allowlistPath, []byte("ssh.github.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sourceRepo := filepath.Join(base, "test-repo")
	remoteRepo := filepath.Join(base, "remote.git")
	runNativeFixtureCommand(t, "git", "init", "--initial-branch=main", sourceRepo)
	runNativeFixtureCommand(t, "git", "-C", sourceRepo, "config", "user.name", "Devkit OpenSSH Regression")
	runNativeFixtureCommand(t, "git", "-C", sourceRepo, "config", "user.email", "devkit-openssh@example.invalid")
	payload := make([]byte, 2<<20)
	for index := range payload {
		payload[index] = byte((index*131 + 17) % 251)
	}
	if err := os.WriteFile(filepath.Join(sourceRepo, "pack.bin"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRepo, ".gitignore"), []byte(".devhome-agent*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runNativeFixtureCommand(t, "git", "-C", sourceRepo, "add", ".")
	runNativeFixtureCommand(t, "git", "-C", sourceRepo, "commit", "-m", "delayed pack fixture")
	runNativeFixtureCommand(t, "git", "clone", "--bare", sourceRepo, remoteRepo)

	clientHome := filepath.Join(base, "client-home")
	clientKey := filepath.Join(clientHome, ".ssh", "id_ed25519")
	if err := os.MkdirAll(filepath.Dir(clientKey), 0o700); err != nil {
		t.Fatal(err)
	}
	runNativeFixtureCommand(t, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", clientKey)
	hostKey := filepath.Join(base, "sshd-host-ed25519")
	runNativeFixtureCommand(t, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", hostKey)
	hostPublicKey, err := os.ReadFile(hostKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	hostKeyFields := strings.Fields(string(hostPublicKey))
	if len(hostKeyFields) < 2 {
		t.Fatalf("invalid fixture host public key: %q", hostPublicKey)
	}
	matchingKnownHosts := filepath.Join(base, "package-known-hosts")
	if err := os.WriteFile(
		matchingKnownHosts,
		[]byte("[ssh.github.com]:443 "+hostKeyFields[0]+" "+hostKeyFields[1]+"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	wrongHostKey := filepath.Join(base, "wrong-sshd-host-ed25519")
	runNativeFixtureCommand(t, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", wrongHostKey)
	wrongPublicKey, err := os.ReadFile(wrongHostKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	wrongKeyFields := strings.Fields(string(wrongPublicKey))
	if len(wrongKeyFields) < 2 {
		t.Fatalf("invalid wrong fixture host public key: %q", wrongPublicKey)
	}
	mismatchingKnownHosts := filepath.Join(base, "mismatching-package-known-hosts")
	if err := os.WriteFile(
		mismatchingKnownHosts,
		[]byte("[ssh.github.com]:443 "+wrongKeyFields[0]+" "+wrongKeyFields[1]+"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(clientHome, ".ssh", "known_hosts"),
		[]byte("[ssh.github.com]:443 "+wrongKeyFields[0]+" "+wrongKeyFields[1]+"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	matchingDevctl := buildDevctlForNativeDefaultsWithSSHKnownHostsAndTags(
		t,
		sshExecutable,
		matchingKnownHosts,
		"devkitintegration",
	)
	mismatchingDevctl := buildDevctlForNativeDefaultsWithSSHKnownHostsAndTags(
		t,
		sshExecutable,
		mismatchingKnownHosts,
		"devkitintegration",
	)
	installPackageDevctl := func(source string) {
		t.Helper()
		devctlBytes, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(packageDevctl, devctlBytes, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	installPackageDevctl(mismatchingDevctl)
	publicKey, err := os.ReadFile(clientKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	remoteHelper := filepath.Join(base, "delayed-upload-pack")
	gitExecutable, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	gitExecutable, err = filepath.Abs(gitExecutable)
	if err != nil {
		t.Fatal(err)
	}
	helperScript := fmt.Sprintf(
		"#!/bin/sh\nexec %s -test.run=^TestNativeOpenSSHDelayedUploadPackHelper$ -- %s %s\n",
		shellSingleQuoteForNativeFixture(os.Args[0]),
		shellSingleQuoteForNativeFixture(remoteRepo),
		shellSingleQuoteForNativeFixture(gitExecutable),
	)
	if err := os.WriteFile(remoteHelper, []byte(helperScript), 0o700); err != nil {
		t.Fatal(err)
	}
	authorizedKeys := filepath.Join(base, "authorized_keys")
	authorizedLine := fmt.Sprintf(
		"command=\"%s\",no-agent-forwarding,no-port-forwarding,no-pty,no-user-rc %s",
		strings.ReplaceAll(remoteHelper, `"`, `\"`),
		string(publicKey),
	)
	if err := os.WriteFile(authorizedKeys, []byte(authorizedLine), 0o600); err != nil {
		t.Fatal(err)
	}
	sshdAddress := reserveNativeFixtureAddress(t)
	currentUser := strings.TrimSpace(runNativeFixtureCommand(t, "id", "-un"))
	sshdConfig := strings.Join([]string{
		"ListenAddress " + strings.Split(sshdAddress, ":")[0],
		"Port " + strings.Split(sshdAddress, ":")[1],
		"HostKey " + hostKey,
		"PidFile " + filepath.Join(base, "sshd.pid"),
		"AuthorizedKeysFile " + authorizedKeys,
		"StrictModes no",
		"PubkeyAuthentication yes",
		"PasswordAuthentication no",
		"KbdInteractiveAuthentication no",
		"UsePAM no",
		"UseDNS no",
		"AllowUsers " + currentUser,
		"LogLevel VERBOSE",
	}, "\n") + "\n"
	sshdConfigPath := filepath.Join(base, "sshd_config")
	if err := os.WriteFile(sshdConfigPath, []byte(sshdConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	sshdExecutable, err := exec.LookPath("sshd")
	if err != nil {
		t.Fatal(err)
	}
	sshdExecutable, err = filepath.Abs(sshdExecutable)
	if err != nil {
		t.Fatal(err)
	}
	var sshdOutput strings.Builder
	sshd := exec.Command(sshdExecutable, "-D", "-e", "-f", sshdConfigPath)
	sshd.Env = os.Environ()
	if nssWrapper := strings.TrimSpace(os.Getenv("DEVKIT_TEST_NSS_WRAPPER")); nssWrapper != "" {
		currentUID := strings.TrimSpace(runNativeFixtureCommand(t, "id", "-u"))
		currentGID := strings.TrimSpace(runNativeFixtureCommand(t, "id", "-g"))
		loginShell, err := exec.LookPath("sh")
		if err != nil {
			t.Fatal(err)
		}
		loginShell, err = filepath.Abs(loginShell)
		if err != nil {
			t.Fatal(err)
		}
		passwdPath := filepath.Join(base, "nss-passwd")
		groupPath := filepath.Join(base, "nss-group")
		if err := os.WriteFile(
			passwdPath,
			[]byte(fmt.Sprintf("%s:x:%s:%s:Devkit fixture:%s:%s\n", currentUser, currentUID, currentGID, clientHome, loginShell)),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			groupPath,
			[]byte(fmt.Sprintf("%s:x:%s:\n", currentUser, currentGID)),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
		sshd.Env = append(sshd.Env,
			"LD_PRELOAD="+nssWrapper,
			"NSS_WRAPPER_PASSWD="+passwdPath,
			"NSS_WRAPPER_GROUP="+groupPath,
		)
	}
	sshd.Stdout = &sshdOutput
	sshd.Stderr = &sshdOutput
	if err := sshd.Start(); err != nil {
		t.Fatalf("start sshd: %v", err)
	}
	t.Cleanup(func() {
		if sshd.Process != nil {
			_ = sshd.Process.Kill()
		}
		_ = sshd.Wait()
	})
	waitForNativeFixtureTCP(t, sshdAddress, &sshdOutput)

	outerProxy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outerProxy.Close() })
	connectTarget := make(chan string, 2)
	proxyErr := make(chan error, 2)
	go func() {
		for attempt := 0; attempt < 2; attempt++ {
			client, err := outerProxy.Accept()
			if err != nil {
				proxyErr <- err
				return
			}
			reader := bufio.NewReader(client)
			req, err := http.ReadRequest(reader)
			if err != nil {
				_ = client.Close()
				proxyErr <- err
				return
			}
			connectTarget <- req.Host
			server, err := net.Dial("tcp", sshdAddress)
			if err != nil {
				_ = client.Close()
				proxyErr <- err
				return
			}
			if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
				_ = server.Close()
				_ = client.Close()
				proxyErr <- err
				return
			}
			proxyErr <- relayNativeFixtureTunnel(client, reader, server)
			_ = server.Close()
			_ = client.Close()
		}
	}()

	remoteURL := "ssh://" + currentUser + "@github.com" + remoteRepo
	runNativeFixtureCommand(t, "git", "-C", sourceRepo, "remote", "add", "origin", remoteURL)
	isolatedRoot := filepath.Join(base, "consumer")
	proxySocket := filepath.Join(base, "egress.sock")
	resolvConf := filepath.Join(base, "resolv.conf")
	if err := os.WriteFile(resolvConf, []byte("nameserver 127.0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nscdSource := filepath.Join(base, "integration-nscd.socket")
	if err := os.WriteFile(nscdSource, []byte("integration-only bind source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	newPrepare := func(worktreeRoot, socketPath string) *exec.Cmd {
		command := exec.Command(
			packageDevctl,
			"-p", "dev-all",
			"native", "prepare",
			"--repo", "test-repo",
			"--count", "1",
			"--base-branch", "main",
			"--branch-prefix", "agent",
			"--worktree-root", worktreeRoot,
			"--worktree-container-root", "/workspaces/dev/installed-chain",
			"--isolation-profile", "workspace-egress",
			"--proxy-socket", socketPath,
			"--egress-allowlist", allowlistPath,
			"--resolv-conf", resolvConf,
			"--format", "json",
		)
		command.Env = isolatedNativeFixtureEnv(
			"DEVKIT_ROOT="+root,
			"DEVKIT_NO_TMUX=1",
			"DEVKIT_NATIVE_ISOLATION_PROFILE=workspace-egress",
			"HTTPS_PROXY=http://"+outerProxy.Addr().String(),
			"HTTP_PROXY=http://"+outerProxy.Addr().String(),
			"HOME="+clientHome,
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_NOSYSTEM=1",
			"DEVKIT_INTEGRATION_NSCD_SOURCE="+nscdSource,
		)
		return command
	}
	mismatchingRoot := filepath.Join(base, "mismatching-consumer")
	mismatchingSocket := filepath.Join(base, "mismatching-egress.sock")
	mismatchingOutput, mismatchingErr := newPrepare(mismatchingRoot, mismatchingSocket).CombinedOutput()
	if mismatchingErr == nil {
		t.Fatalf("mismatching package host key unexpectedly created a consumer:\n%s", mismatchingOutput)
	}
	if !strings.Contains(string(mismatchingOutput), "Host key verification failed") &&
		!strings.Contains(string(mismatchingOutput), "REMOTE HOST IDENTIFICATION HAS CHANGED") {
		t.Fatalf("mismatching package host key failure was not explicit: %v\n%s", mismatchingErr, mismatchingOutput)
	}
	if entries, err := os.ReadDir(mismatchingRoot); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspect mismatching bootstrap root: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("mismatching package host key left consumer payload in declared empty root: %v", entries)
	}
	for _, residue := range []string{
		mismatchingSocket,
		filepath.Join(mismatchingRoot, "agent1", "test-repo"),
		filepath.Join(mismatchingRoot, ".devkit", "git", "test-repo.git"),
	} {
		if _, err := os.Lstat(residue); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("mismatching package host key left bootstrap residue %s: %v", residue, err)
		}
	}
	installPackageDevctl(matchingDevctl)
	prepare := newPrepare(isolatedRoot, proxySocket)
	started := time.Now()
	output, err := prepare.CombinedOutput()
	if err != nil {
		t.Fatalf(
			"installed Git/OpenSSH/ProxyCommand chain failed after %s: %v\n%s\nsshd:\n%s",
			time.Since(started),
			err,
			output,
			sshdOutput.String(),
		)
	}
	if elapsed := time.Since(started); elapsed <= 10*time.Second {
		t.Fatalf("delayed pack completed before old universal deadline: %s", elapsed)
	}
	if _, err := os.Stat(filepath.Join(isolatedRoot, "agent1", "test-repo", ".git")); err != nil {
		t.Fatalf("installed-chain worktree missing after fetch: %v", err)
	}
	var installedKnownHosts []string
	if err := filepath.WalkDir(isolatedRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Base(path) == "known_hosts" && filepath.Base(filepath.Dir(path)) == ".ssh" {
			installedKnownHosts = append(installedKnownHosts, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(installedKnownHosts) != 1 {
		t.Fatalf("installed package host-key files = %v, want exactly one consumer authority", installedKnownHosts)
	}
	gotKnownHosts, err := os.ReadFile(installedKnownHosts[0])
	if err != nil {
		t.Fatal(err)
	}
	wantKnownHosts, err := os.ReadFile(matchingKnownHosts)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotKnownHosts) != string(wantKnownHosts) {
		t.Fatalf("consumer known-hosts did not exactly match immutable package material:\ngot %q\nwant %q", gotKnownHosts, wantKnownHosts)
	}
	for attempt := 0; attempt < 2; attempt++ {
		select {
		case target := <-connectTarget:
			if target != "ssh.github.com:443" {
				t.Fatalf("outer CONNECT target = %q", target)
			}
		case <-time.After(time.Second):
			t.Fatal("outer proxy did not receive installed ProxyCommand CONNECT")
		}
		if err := <-proxyErr; err != nil {
			t.Fatalf("outer proxy tunnel: %v", err)
		}
	}
	if _, err := os.Lstat(proxySocket); !os.IsNotExist(err) {
		t.Fatalf("managed proxy socket remained after installed-chain prepare: %v", err)
	}
}

func TestNativeOpenSSHDelayedUploadPackHelper(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SSH_ORIGINAL_COMMAND")) == "" {
		return
	}
	fail := func(code int, format string, values ...any) {
		_, _ = fmt.Fprintf(os.Stderr, "delayed upload-pack helper: "+format+"\n", values...)
		os.Exit(code)
	}
	arguments := nativeFixtureArgumentsAfterDoubleDash()
	if len(arguments) != 2 {
		fail(91, "expected repository and Git executable arguments, got %d", len(arguments))
	}
	uploadPack := exec.Command(arguments[1], "upload-pack", arguments[0])
	stdin, err := uploadPack.StdinPipe()
	if err != nil {
		fail(92, "stdin pipe: %v", err)
	}
	stdout, err := uploadPack.StdoutPipe()
	if err != nil {
		fail(93, "stdout pipe: %v", err)
	}
	uploadPack.Stderr = os.Stderr
	if err := uploadPack.Start(); err != nil {
		fail(94, "start upload-pack: %v", err)
	}
	go func() {
		_, _ = io.Copy(stdin, os.Stdin)
		_ = stdin.Close()
	}()
	reader := bufio.NewReader(stdout)
	for {
		header := make([]byte, 4)
		if _, err := io.ReadFull(reader, header); err != nil {
			fail(95, "read advertisement header: %v", err)
		}
		if _, err := os.Stdout.Write(header); err != nil {
			fail(96, "write advertisement header: %v", err)
		}
		if string(header) == "0000" {
			break
		}
		length, err := strconv.ParseInt(string(header), 16, 32)
		if err != nil || length < 4 {
			fail(97, "parse advertisement header %q: length=%d err=%v", header, length, err)
		}
		if _, err := io.CopyN(os.Stdout, reader, length-4); err != nil {
			fail(98, "write advertisement payload: %v", err)
		}
	}
	nextHeader := make([]byte, 4)
	if _, err := io.ReadFull(reader, nextHeader); err != nil {
		fail(99, "read response header: %v", err)
	}
	time.Sleep(11 * time.Second)
	if _, err := os.Stdout.Write(nextHeader); err != nil {
		fail(100, "write response header: %v", err)
	}
	if _, err := io.Copy(os.Stdout, reader); err != nil {
		fail(101, "write response: %v", err)
	}
	if err := uploadPack.Wait(); err != nil {
		fail(102, "upload-pack wait: %v", err)
	}
	os.Exit(0)
}

func nativeFixtureArgumentsAfterDoubleDash() []string {
	for index, argument := range os.Args {
		if argument == "--" {
			return os.Args[index+1:]
		}
	}
	return nil
}

func runNativeFixtureCommand(t *testing.T, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, output)
	}
	return string(output)
}

func shellSingleQuoteForNativeFixture(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func reserveNativeFixtureAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func waitForNativeFixtureTCP(t *testing.T, address string, output *strings.Builder) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("sshd did not listen on %s: %v\n%s", address, err, output.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func relayNativeFixtureTunnel(client net.Conn, clientReader io.Reader, server net.Conn) error {
	results := make(chan error, 2)
	go func() {
		_, err := io.Copy(server, clientReader)
		if conn, ok := server.(interface{ CloseWrite() error }); ok {
			err = errorsJoinNativeFixture(err, conn.CloseWrite())
		}
		results <- err
	}()
	go func() {
		_, err := io.Copy(client, server)
		if conn, ok := client.(interface{ CloseWrite() error }); ok {
			err = errorsJoinNativeFixture(err, conn.CloseWrite())
		}
		results <- err
	}()
	first := <-results
	if first != nil {
		_ = client.Close()
		_ = server.Close()
	}
	second := <-results
	return errorsJoinNativeFixture(first, second)
}

func errorsJoinNativeFixture(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

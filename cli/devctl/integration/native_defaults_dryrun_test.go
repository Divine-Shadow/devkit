package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func nativeFixtureTreeIdentity(root string) (string, error) {
	root = filepath.Clean(root)
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		_, _ = hash.Write([]byte(relative))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(info.Mode().String()))
		_, _ = hash.Write([]byte{0})
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			_, _ = hash.Write([]byte(strconv.FormatUint(stat.Ino, 10)))
		}
		_, _ = hash.Write([]byte{0})
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_, _ = hash.Write([]byte(target))
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, _ = hash.Write(data)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func nativeFixtureTreeIdentities(paths []string) (map[string]string, error) {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	result := make(map[string]string, len(sorted))
	for _, path := range sorted {
		identity, err := nativeFixtureTreeIdentity(path)
		if err != nil {
			return nil, err
		}
		result[path] = identity
	}
	return result, nil
}

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
	gitExecutable, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	gitExecutable, err = filepath.Abs(gitExecutable)
	if err != nil {
		t.Fatal(err)
	}
	envExecutable, err := exec.LookPath("env")
	if err != nil {
		t.Fatal(err)
	}
	envExecutable, err = filepath.Abs(envExecutable)
	if err != nil {
		t.Fatal(err)
	}
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
			"-X=devkit/cli/devctl/internal/worktrees.packageGitExecutable=" + gitExecutable,
			"-X=devkit/cli/devctl/internal/worktrees.packageEnvExecutable=" + envExecutable,
			"-X=devkit/cli/devctl/internal/runtime/launch.packageGitExecutable=" + gitExecutable,
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

func writeRuntimeAuthorityFixture(t *testing.T, root, productRevision string) string {
	t.Helper()
	path := filepath.Join(root, "runtime-authority.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"schemaVersion":"fleet-runtime-authority/v1","sources":{"ouroboros-ide":{"rev":%q}}}`, productRevision)
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
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
	return runNativeDefaultDryRunWithFixtureEnv(t, bin, root, nil, args...)
}

func runNativeDefaultDryRunWithFixtureEnv(
	t *testing.T,
	bin string,
	root string,
	extraEnv []string,
	args ...string,
) (string, error) {
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
	cmd.Env = append(cmd.Env, extraEnv...)
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
		"DEVKIT_RUNTIME_AUTHORITY_FILE":        true,
		"DEVKIT_PRODUCT_SOURCE_REV":            true,
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
	if !strings.Contains(out, "raw devctl refuses Product") {
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
	if !strings.Contains(out, "raw devctl refuses Product") {
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

func TestDevAllFreshOpenAndResetRefuseLegacyProductConstruction(t *testing.T) {
	bin := buildDevctlForNativeDefaultsWithTags(t, "devkitintegration")
	root := nativeDefaultsRoot(t)
	run := func(args ...string) (string, error) {
		return runNativeDefaultDryRunWithFixtureEnv(
			t,
			bin,
			root,
			nil,
			args...,
		)
	}

	freshOutput, err := run("fresh-open", "2")
	if err == nil || !strings.Contains(freshOutput, "raw devctl refuses Product") {
		t.Fatalf("fresh-open remained an alternate Product construction surface: %v\n%s", err, freshOutput)
	}

	resetOutput, err := run("reset", "2")
	if err == nil || !strings.Contains(resetOutput, "raw devctl refuses Product") {
		t.Fatalf("reset remained an alternate Product construction surface: %v\n%s", err, resetOutput)
	}
}

func TestDevAllProductMutationDispatchIsDeniedBeforeEffects(t *testing.T) {
	built := buildDevctlForNativeDefaultsWithTags(t, "devkitintegration")
	root := nativeDefaultsRoot(t)
	bin := filepath.Join(root, "kit", "bin", "devctl")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(built)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, data, 0o755); err != nil {
		t.Fatal(err)
	}
	controllerHome := filepath.Join(root, "controller-home")
	sentinel := filepath.Join(root, "dispatch-sentinel")
	if err := os.WriteFile(sentinel, []byte("unchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	type dispatch struct {
		name string
		args []string
	}
	denied := []dispatch{
		{name: "up", args: []string{"up"}},
		{name: "down", args: []string{"down"}},
		{name: "restart", args: []string{"restart"}},
		{name: "scale", args: []string{"scale", "2"}},
		{name: "ensure-ready", args: []string{"ensure-ready"}},
		{name: "status-ready", args: []string{"status", "--ready"}},
		{name: "native-readiness", args: []string{"native", "readiness"}},
		{name: "native-capacity", args: []string{"native", "capacity"}},
		{name: "native-governance-env", args: []string{"native", "governance-env"}},
		{name: "native-egress-proxy", args: []string{"native", "egress-proxy"}},
		{name: "native-proxy-bridge", args: []string{"native", "proxy-bridge"}},
		{name: "native-proxy-connect", args: []string{"native", "proxy-connect"}},
		{name: "native-down", args: []string{"native", "down"}},
		{name: "legacy-prepare", args: []string{"native", "prepare"}},
		{name: "indexed-prepare-override", args: []string{"native", "prepare", "--index", "2", "--worktree-root", filepath.Join(root, "other")}},
		{name: "exec-override", args: []string{"exec", "2", "--proxy-socket", filepath.Join(root, "caller.sock"), "--", "true"}},
		{name: "layout-apply", args: []string{"layout-apply"}},
		{name: "layout-generate", args: []string{"layout-generate"}},
		{name: "run", args: []string{"run"}},
		{name: "bootstrap", args: []string{"bootstrap"}},
		{name: "reset", args: []string{"reset"}},
		{name: "verify", args: []string{"verify"}},
		{name: "verify-all", args: []string{"verify-all"}},
		{name: "doctor-runtime", args: []string{"doctor-runtime"}},
		{name: "codex-auth", args: []string{"codex-auth"}},
		{name: "creds", args: []string{"creds"}},
		{name: "warm", args: []string{"warm"}},
		{name: "maintain", args: []string{"maintain"}},
		{name: "check-net", args: []string{"check-net"}},
		{name: "check-codex", args: []string{"check-codex"}},
		{name: "check-sts", args: []string{"check-sts"}},
		{name: "codex-test", args: []string{"codex-test"}},
		{name: "codex-debug", args: []string{"codex-debug"}},
		{name: "allow", args: []string{"allow"}},
		{name: "broker-start", args: []string{"broker", "start"}},
		{name: "broker-stop", args: []string{"broker", "stop"}},
		{name: "hosts-apply", args: []string{"hosts", "apply"}},
		{name: "management-refresh", args: []string{"management-inspect", "refresh"}},
		{name: "management-shell", args: []string{"management-inspect", "shell"}},
		{name: "management-exec", args: []string{"management-inspect", "exec"}},
		{name: "proxy", args: []string{"proxy"}},
		{name: "tmux-shells", args: []string{"tmux-shells"}},
		{name: "open", args: []string{"open"}},
		{name: "fresh-open", args: []string{"fresh-open"}},
		{name: "exec-cd", args: []string{"exec-cd"}},
		{name: "attach-cd", args: []string{"attach-cd"}},
		{name: "tmux-sync", args: []string{"tmux-sync"}},
		{name: "tmux-add-cd", args: []string{"tmux-add-cd"}},
		{name: "tmux-apply-layout", args: []string{"tmux-apply-layout"}},
		{name: "wt-open", args: []string{"wt-open"}},
		{name: "wt-release", args: []string{"wt-release"}},
		{name: "tmux-bell-install", args: []string{"tmux-bell-install"}},
		{name: "tmux-notify-bell", args: []string{"tmux-notify-bell"}},
		{name: "ssh-setup", args: []string{"ssh-setup"}},
		{name: "repo-config-ssh", args: []string{"repo-config-ssh"}},
		{name: "repo-config-https", args: []string{"repo-config-https"}},
		{name: "repo-push-ssh", args: []string{"repo-push-ssh"}},
		{name: "repo-push-https", args: []string{"repo-push-https"}},
		{name: "worktrees-init", args: []string{"worktrees-init"}},
		{name: "worktrees-setup", args: []string{"worktrees-setup"}},
		{name: "worktrees-branch", args: []string{"worktrees-branch"}},
		{name: "worktrees-sync", args: []string{"worktrees-sync"}},
		{name: "worktrees-tmux", args: []string{"worktrees-tmux"}},
	}
	effectPaths := []string{
		filepath.Join(filepath.Dir(root), "agent-worktrees"),
		filepath.Join(filepath.Dir(root), ".devkit", "native-agents"),
		filepath.Join(filepath.Dir(root), ".devkit", "native-broker"),
		filepath.Join(filepath.Dir(root), ".devkit", "native-egress"),
		filepath.Join(controllerHome, ".codex"),
		filepath.Join(controllerHome, ".ssh"),
	}
	authorityPath := strings.TrimSpace(os.Getenv("DEVKIT_TEST_IMMUTABLE_RUNTIME_AUTHORITY"))
	if authorityPath == "" {
		authorityPath = writeRuntimeAuthorityFixture(
			t,
			root,
			"87f5631f87f0be3a731e3d41aa98f9ac6d7d90d3",
		)
	} else if !strings.HasPrefix(authorityPath, "/nix/store/") {
		t.Fatalf("immutable Product dispatch authority fixture is not in the Nix store: %s", authorityPath)
	}
	for _, test := range denied {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command(bin, append([]string{"-p", "dev-all"}, test.args...)...)
			command.Env = isolatedNativeFixtureEnv(
				"DEVKIT_ROOT="+root,
				"DEVKIT_RUNTIME_AUTHORITY_FILE="+authorityPath,
				"HOME="+controllerHome,
				"PATH="+filepath.Join(root, "hostile-path"),
			)
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "raw devctl refuses Product") {
				t.Fatalf("Product mutation was not denied at dispatch: %v\n%s", err, output)
			}
			for _, path := range effectPaths {
				if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("denied Product dispatch created %s: %v", path, statErr)
				}
			}
			if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "unchanged\n" {
				t.Fatalf("denied Product dispatch changed sentinel: %q %v", got, readErr)
			}
		})
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
	command.Env = append(
		os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_OPTIONAL_LOCKS=0",
	)
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

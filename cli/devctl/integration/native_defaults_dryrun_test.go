package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func buildDevctlForNativeDefaults(t *testing.T) string {
	t.Helper()
	return buildDevctlForNativeDefaultsWithTags(t)
}

func buildDevctlForNativeDefaultsWithTags(t *testing.T, tags ...string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "devctl")
	args := []string{"build", "-trimpath"}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
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
	cmd := exec.Command(bin, append([]string{"--dry-run", "-p", "dev-all"}, args...)...)
	cmd.Env = append(os.Environ(), "DEVKIT_ROOT="+root, "DEVKIT_NO_TMUX=1")
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
	}
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !blocked[name] {
			env = append(env, entry)
		}
	}
	return append(env, extra...)
}

func runProjectDryRun(t *testing.T, bin, root, project string, args ...string) (string, error) {
	t.Helper()
	return runProjectDryRunWithEnv(t, bin, root, project, nil, args...)
}

func runProjectDryRunWithEnv(t *testing.T, bin, root, project string, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"--dry-run", "-p", project}, args...)...)
	cmd.Env = append(os.Environ(),
		"DEVKIT_ROOT="+root,
		"DEVKIT_NO_TMUX=1",
		"DEVKIT_GIT_USER_NAME=Devkit Test",
		"DEVKIT_GIT_USER_EMAIL=devkit-test@example.com",
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
					if !strings.Contains(out, "bwrap") || !strings.Contains(out, overlay.flake) {
						t.Fatalf("%s %v missing native sandbox command:\n%s", overlay.project, args, out)
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
		{name: "ssh-test", args: []string{"ssh-test", "1"}, wants: []string{"ssh -T github.com", "-p " + project + " exec 1", "--repo " + repo}},
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
			cmd := exec.Command(bin, tt.args...)
			cmd.Env = append(os.Environ(),
				"DEVKIT_ROOT="+root,
				"DEVKIT_NO_TMUX=1",
				"CODEX_AUTH_JSON="+filepath.Join(root, "missing-auth.json"),
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
	run("git", "-C", repo, "remote", "set-url", "origin", "ssh://git@fixture.invalid"+bare)

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

	cmd := exec.Command(bin, "--dry-run", "-p", "dev-all", "native", "exec", "--repo", "ouroboros-ide", "--flake", ".#runtime-test-agent", "--", "echo", "hi")
	cmd.Env = append(os.Environ(), "DEVKIT_ROOT="+root, "DEVKIT_NO_TMUX=1")
	outBytes, err := cmd.CombinedOutput()
	out := string(outBytes)
	if err != nil {
		t.Fatalf("global dry-run native exec failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "bwrap") || !strings.Contains(out, "exec") || !strings.Contains(out, "echo") || !strings.Contains(out, "hi") {
		t.Fatalf("global dry-run did not print native command:\n%s", out)
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

	for _, args := range [][]string{{"fresh-open", "2"}, {"reset", "2"}} {
		out, err := runNativeDefaultDryRun(t, bin, root, args...)
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
		assertNoDockerCommand(t, out)
		if !strings.Contains(out, " -p dev-all down --repo ouroboros-ide --count 2") {
			t.Fatalf("%v missing native down:\n%s", args, out)
		}
		if !strings.Contains(out, " -p dev-all up --repo ouroboros-ide --count 2") {
			t.Fatalf("%v missing native up:\n%s", args, out)
		}
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
	builtDevctl := buildDevctlForNativeDefaultsWithTags(t, "devkitintegration")
	devctlBytes, err := os.ReadFile(builtDevctl)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packageDevctl, devctlBytes, 0o755); err != nil {
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
	connectTarget := make(chan string, 1)
	proxyErr := make(chan error, 1)
	go func() {
		client, err := outerProxy.Accept()
		if err != nil {
			proxyErr <- err
			return
		}
		defer client.Close()
		reader := bufio.NewReader(client)
		req, err := http.ReadRequest(reader)
		if err != nil {
			proxyErr <- err
			return
		}
		connectTarget <- req.Host
		server, err := net.Dial("tcp", sshdAddress)
		if err != nil {
			proxyErr <- err
			return
		}
		defer server.Close()
		if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			proxyErr <- err
			return
		}
		proxyErr <- relayNativeFixtureTunnel(client, reader, server)
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
	prepare := exec.Command(
		packageDevctl,
		"-p", "dev-all",
		"native", "prepare",
		"--repo", "test-repo",
		"--count", "1",
		"--base-branch", "main",
		"--branch-prefix", "agent",
		"--worktree-root", isolatedRoot,
		"--worktree-container-root", "/workspaces/dev/installed-chain",
		"--isolation-profile", "workspace-egress",
		"--proxy-socket", proxySocket,
		"--egress-allowlist", allowlistPath,
		"--resolv-conf", resolvConf,
		"--format", "json",
	)
	prepare.Env = isolatedNativeFixtureEnv(
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

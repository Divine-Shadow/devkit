package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildDevctlForNativeDefaults(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "devctl")
	cmd := exec.Command("go", "build", "-trimpath", "-o", bin, "./")
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
	t.Helper()
	devRoot := filepath.Clean(filepath.Join(root, ".."))
	worktree := filepath.Join(devRoot, "agent-worktrees", "agent1", "ouroboros-ide")
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
		{name: "codex-test", args: []string{"codex-test"}, want: " -p " + project + " exec 1 --repo " + repo + " -- bash -lc"},
		{name: "codex-debug", args: []string{"codex-debug"}, want: " -p " + project + " exec 1 --repo " + repo + " -- bash -lc"},
		{name: "verify", args: []string{"verify"}, want: " -p " + project + " ensure-ready --repo " + repo + " --count 1 --repo-readiness"},
		{name: "doctor-runtime", args: []string{"doctor-runtime"}, want: " -p " + project + " ensure-ready --repo " + repo + " --count 1 --runtime-only"},
		{name: "exec-cd", args: []string{"exec-cd", "1", ".", "echo", "hi"}, want: " -p " + project + " exec 1 --repo " + repo + " -- bash -lc"},
		{name: "attach-cd", args: []string{"attach-cd", "1", "."}, want: " -p " + project + " exec 1 --repo " + repo + " -- bash -lc"},
		{name: "layout-apply", args: []string{"layout-apply", "--file", layoutPath}, want: " -p " + project + " up --repo " + repo + " --count 1"},
		{name: "layout-validate", args: []string{"layout-validate", "--file", layoutPath}, want: ""},
		{name: "layout-generate", args: []string{"layout-generate"}, want: "project: " + project},
		{name: "tmux-sync", args: []string{"tmux-sync", "--count", "1"}, want: " -p " + project + " exec 1 --repo " + repo},
		{name: "tmux-add-cd", args: []string{"tmux-add-cd", "1", ".", "--session", "native-front", "--name", "agent-1"}, want: " -p " + project + " exec 1 --repo " + repo},
		{name: "tmux-apply-layout", args: []string{"tmux-apply-layout", "--file", layoutPath}, want: " -p " + project + " exec 1 --repo " + repo},
		{name: "worktrees-init", args: []string{"worktrees-init", repo, "1"}, want: "Initialize worktrees for " + repo},
		{name: "worktrees-setup", args: []string{"worktrees-setup", repo, "1"}, want: "git -c core.sshCommand=ssh -C"},
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
		{name: "image-matrix", args: []string{"image-matrix", "--all"}, want: "./overlays/ouroboros-static-front-end#default"},
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
			t.Fatalf("%v leaked compose project into native WT launch:\n%s", args, out)
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
		{name: "ssh-test", args: []string{"ssh-test", "1"}, wants: []string{"ssh -T github.com", nativePathPart}},
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

func TestDevAllComposeNamespaceIsRetiredDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := nativeDefaultsRoot(t)

	out, err := runNativeDefaultDryRun(t, bin, root, "compose", "up")
	if err == nil {
		t.Fatalf("dev-all compose unexpectedly succeeded:\n%s", out)
	}
	assertNoDockerCommand(t, out)
	if !strings.Contains(out, "Compose runtime is retired") {
		t.Fatalf("unexpected dev-all compose error:\n%s", out)
	}
}

func TestDevAllComposeNamespaceRefusesBeforeComposeFilesDryRun(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	root := flakeOverlayRoot(t, "dev-all", "ouroboros-ide", "./overlays/dev-all#default")

	out, err := runProjectDryRun(t, bin, root, "dev-all", "compose", "up")
	if err == nil {
		t.Fatalf("dev-all compose unexpectedly succeeded:\n%s", out)
	}
	assertNoDockerCommand(t, out)
	if strings.Contains(out, "workspace directory") {
		t.Fatalf("dev-all compose loaded retired runtime files before refusing:\n%s", out)
	}
	if !strings.Contains(out, "Compose runtime is retired") {
		t.Fatalf("unexpected dev-all compose error:\n%s", out)
	}
}

func TestNativeTopLevelExecAndAttachPreserveSandboxExitCode(t *testing.T) {
	bin := buildDevctlForNativeDefaults(t)
	bwrapDir := fakeBwrapDir(t)

	tests := []struct {
		name string
		args []string
	}{
		{name: "exec", args: []string{"-p", "dev-all", "exec", "1", "--repo", "ouroboros-ide", "--flake", ".#runtime-test-agent", "--", "true"}},
		{name: "attach", args: []string{"-p", "dev-all", "attach", "1", "--repo", "ouroboros-ide", "--flake", ".#runtime-test-agent"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := nativeDefaultsRoot(t)
			createNativeAgentWorktree(t, root)
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

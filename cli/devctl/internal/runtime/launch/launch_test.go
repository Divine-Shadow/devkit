package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/compose"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

func TestBuildBubblewrapUsesBrokerAndNoHostDockerSocket(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	repoRoot := filepath.Join(devRoot, "ouroboros-ide")
	for _, dir := range []string{devkitRoot, repoRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	brokerSocket := filepath.Join(tmp, "broker.sock")
	if err := os.WriteFile(brokerSocket, []byte("socket placeholder"), 0o600); err != nil {
		t.Fatalf("write broker socket placeholder: %v", err)
	}
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths:          compose.Paths{Root: devkitRoot},
		Repo:           "ouroboros-ide",
		Flake:          ".#runtime-test-agent",
		BrokerEndpoint: brokerSocket,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if err := os.MkdirAll(p.Agent.HostWorktree, 0o755); err != nil {
		t.Fatalf("mkdir host worktree: %v", err)
	}
	if err := os.MkdirAll(p.Agent.HostHome, 0o700); err != nil {
		t.Fatalf("mkdir host home: %v", err)
	}
	if err := os.MkdirAll(p.Agent.StateRoot, 0o700); err != nil {
		t.Fatalf("mkdir state root: %v", err)
	}
	if err := os.WriteFile(p.DNS.ResolvConf, []byte("nameserver 127.0.0.1\n"), 0o644); err != nil {
		t.Fatalf("write resolv: %v", err)
	}

	cmd, err := BuildBubblewrap(p, []string{"git", "--version"})
	if err != nil {
		t.Fatalf("BuildBubblewrap: %v", err)
	}
	joined := ShellString(cmd)
	for _, want := range []string{
		"'--bind' '" + devRoot + "' '/workspaces/dev'",
		"'--symlink' '/run/current-system/sw/bin/env' '/usr/bin/env'",
		"'--symlink' '/run/current-system/sw/bin/sh' '/bin/sh'",
		"'--bind' '" + brokerSocket + "' '" + brokerSocket + "'",
		"'--setenv' 'DOCKER_HOST' 'unix://" + brokerSocket + "'",
		"'--setenv' 'TMPDIR' '/tmp'",
		"'/run/current-system/sw/bin/nix' '--extra-experimental-features' 'nix-command flakes' 'develop' '.#runtime-test-agent' '--no-write-lock-file'",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command missing %q:\n%s", want, joined)
		}
	}
	for _, optionalHostPath := range []string{"/etc/static", "/etc/ssl", "/etc/pki"} {
		if _, err := os.Stat(optionalHostPath); err != nil {
			continue
		}
		want := "'--ro-bind' '" + optionalHostPath + "' '" + optionalHostPath + "'"
		if !strings.Contains(joined, want) {
			t.Fatalf("command missing optional host bind %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "'--dir' '/tmp'") {
		t.Fatalf("launcher must not create /tmp after mounting it as tmpfs:\n%s", joined)
	}
	if len(cmd.Args) < 1 || !strings.Contains(cmd.Args[len(cmd.Args)-1], "cd '/worktrees/agent1/ouroboros-ide' && exec 'git' '--version'") {
		t.Fatalf("launch script did not cd and exec command:\n%#v", cmd.Args)
	}
	if strings.Contains(joined, "/var/run/docker.sock") {
		t.Fatalf("native launcher must not expose /var/run/docker.sock:\n%s", joined)
	}
}

func TestPrepareRequiresExistingWorktree(t *testing.T) {
	tmp := t.TempDir()
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: compose.Paths{Root: filepath.Join(tmp, "devkit")},
		Repo:  "missing",
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if err := Prepare(p); err == nil {
		t.Fatalf("expected missing worktree error")
	}
}

func TestSeedSSHSeedsHostKeysAndKnownHosts(t *testing.T) {
	hostUserHome := t.TempDir()
	srcSSH := filepath.Join(hostUserHome, ".ssh")
	if err := os.MkdirAll(srcSSH, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"id_ed25519":     "private",
		"id_ed25519.pub": "public",
		"known_hosts":    "github.com key",
	} {
		if err := os.WriteFile(filepath.Join(srcSSH, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", hostUserHome)

	nativeHome := filepath.Join(t.TempDir(), "native-home")
	if err := SeedSSH(nativeHome, false); err != nil {
		t.Fatalf("SeedSSH: %v", err)
	}
	for name, wantMode := range map[string]os.FileMode{
		"id_ed25519":     0o600,
		"id_ed25519.pub": 0o644,
		"known_hosts":    0o644,
	} {
		info, err := os.Stat(filepath.Join(nativeHome, ".ssh", name))
		if err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
		if got := info.Mode().Perm(); got != wantMode {
			t.Fatalf("%s mode = %v, want %v", name, got, wantMode)
		}
	}
}

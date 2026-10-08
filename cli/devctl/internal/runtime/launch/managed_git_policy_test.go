package launch

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/sshauthority"
)

func standardManagedGitFixture(t *testing.T, index int, kind string) nativeplan.Plan {
	t.Helper()
	p := managedGitFixturePlan(t, "/workspaces/dev/ouroboros-ide/.devhome-agent1", filepath.Join(t.TempDir(), "owned-current.sock"))
	p.Agent.ID.Repo = "ouroboros-ide"
	p.Agent.ID.Index = index
	p.HostWorkspaceRoot = fmt.Sprintf("/home/bayesartre/dev/agent-worktrees/agent%d", index)
	p.SandboxWorkspaceRoot = "/workspaces/dev"
	p.Agent.HostWorktree = p.HostWorkspaceRoot + "/ouroboros-ide"
	p.Agent.HostHome = fmt.Sprintf("%s/.devhome-agent%d", p.HostWorkspaceRoot, index)
	p.Agent.SandboxHome = fmt.Sprintf("/workspaces/dev/.devhome-agent%d", index)
	if index == 1 {
		p.Agent.HostHome = p.Agent.HostWorktree + "/.devhome-agent1"
		p.Agent.SandboxHome = "/workspaces/dev/ouroboros-ide/.devhome-agent1"
	}
	p.Agent.SandboxWorktree = "/workspaces/dev/ouroboros-ide"
	p.GUITargetConfig = &nativeplan.GUITargetConfigProjection{Kind: kind, ExpectedExecutionHost: "source-selected-fixture-host"}
	return p
}

func TestStandardManagedGitPolicySelectionAndRefusals(t *testing.T) {
	p := standardManagedGitFixture(t, 1, "local-wsl-devkit-agent")
	command, err := managedRuntimeGitSSHCommand(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"DEVKIT_MANAGED_GIT_HOME='/workspaces/dev/ouroboros-ide/.devhome-agent1'", "DEVKIT_MANAGED_GIT_KNOWN_HOSTS=", "managed-git-ssh-config", "ProxyCommand="} {
		if !strings.Contains(command, want) {
			t.Fatalf("source-owned policy omitted %q: %s", want, command)
		}
	}
	for _, key := range []string{"GIT_SSH_COMMAND", "GIT_SSH_VARIANT", "DEVKIT_MANAGED_GIT_HOME", "DEVKIT_MANAGED_GIT_KNOWN_HOSTS", "HOME"} {
		p.Env[key] = "hostile"
		if _, err := managedRuntimeGitSSHCommand(p); err == nil || !strings.Contains(err.Error(), "caller "+key+" override") {
			t.Fatalf("accepted %s override: %v", key, err)
		}
		delete(p.Env, key)
	}
	p.Agent.SandboxHome = "/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1"
	if _, err := managedRuntimeGitSSHCommand(p); err == nil || !strings.Contains(err.Error(), "standard native geometry") {
		t.Fatalf("accepted mismatched standard home: %v", err)
	}
	p = standardManagedGitFixture(t, 1, "local-wsl-devkit-agent")
	p.GUITargetConfig = nil
	legacy, err := managedRuntimeGitSSHCommand(p)
	if err != nil || strings.Contains(legacy, "DEVKIT_MANAGED_GIT_HOME=") {
		t.Fatalf("unselected class gained canonical policy: %v %s", err, legacy)
	}
}

func realManagedGitAuthority(t *testing.T, known string) sshauthority.Authority {
	t.Helper()
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := filepath.Abs("../../../../../nix/managed-git-ssh-config")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := sshauthority.NewManaged(ssh, known, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return authority
}

// Only the two fields already projected by the runtime launcher/MCP cross
// this env-i equivalent boundary. The leaf's inline bindings defeat inherited
// home/host-key values without expanding the MCP environment contract.
func realManagedSSH(t *testing.T, command string, argv ...string) ([]byte, error) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bash, append([]string{"-c", command + " \"$@\"", "owned-ssh"}, argv...)...)
	cmd.Env = []string{"GIT_SSH_COMMAND=" + command, "GIT_SSH_VARIANT=ssh"}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("owned SSH exceeded absolute deadline: %v", ctx.Err())
	}
	if len(out) > 65536 {
		t.Fatal("owned SSH output exceeded byte bound")
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		t.Fatal("owned SSH child not reaped")
	}
	return out, err
}

func TestStandardManagedGitRealOpenSSHConfigAndTwoFieldProjection(t *testing.T) {
	home := filepath.Join(t.TempDir(), "current home with ' quote")
	stale := filepath.Join(t.TempDir(), "expired-agent-worktrees", "agent1", "ouroboros-ide", ".devhome-agent1")
	cfg := buildGitSSHConfigWithProxyCommand(stale, []string{"id_ed25519"}, "expired-connector")
	cfg += "Host other.example\n HostName alternate.example\n Port 2222\n User fixture-user\n IdentityFile /owned-public-fixture/other-key\n UserKnownHostsFile /owned-public-fixture/other-hosts\n StrictHostKeyChecking yes\n BatchMode yes\n IdentitiesOnly yes\n"
	writeTestFile(t, filepath.Join(home, ".ssh", "config"), cfg)
	known := filepath.Join(t.TempDir(), "immutable-known-hosts")
	writeTestFile(t, known, "[ssh.github.com]:443 ssh-ed25519 owned-public-fixture\n")
	authority := realManagedGitAuthority(t, known)
	command, err := authority.ManagedGitCommand(home)
	if err != nil {
		t.Fatal(err)
	}
	command += " -o " + shellQuote("ProxyCommand=owned-current-connector --target %h:%p")
	for _, host := range []string{"github.com", "ssh.github.com", "other.example"} {
		out, err := realManagedSSH(t, command, "-G", host)
		if err != nil {
			t.Fatalf("real -G %s: %v %s", host, err, out)
		}
		if host != "other.example" {
			for _, want := range []string{"hostname ssh.github.com", "port 443", "user git", "batchmode yes", "identitiesonly yes", "stricthostkeychecking true", "userknownhostsfile " + known, "identityfile ${DEVKIT_MANAGED_GIT_HOME}/.ssh/id_ed25519", "identityfile ${DEVKIT_MANAGED_GIT_HOME}/.ssh/id_rsa"} {
				if !strings.Contains(string(out), want) {
					t.Fatalf("real GitHub config omitted %q: %s", want, out)
				}
			}
			if strings.Contains(string(out), stale) {
				t.Fatalf("old identity policy escaped inactive Include: %s", out)
			}
		} else {
			for _, want := range []string{"hostname alternate.example", "port 2222", "user fixture-user", "identityfile /owned-public-fixture/other-key", "userknownhostsfile /owned-public-fixture/other-hosts", "stricthostkeychecking true", "batchmode yes", "identitiesonly yes"} {
				if !strings.Contains(string(out), want) {
					t.Fatalf("other-host policy changed %q: %s", want, out)
				}
			}
		}
		if !strings.Contains(string(out), "proxycommand owned-current-connector --target %h:%p") {
			t.Fatalf("current connector lost: %s", out)
		}
	}
	if b, err := os.ReadFile(filepath.Join(home, ".ssh", "config")); err != nil || string(b) != cfg {
		t.Fatal("saved public config changed")
	}
	for _, home := range []string{"", "relative", "/absolute\nextra", "/absolute\x00extra"} {
		if _, err := authority.ManagedGitCommand(home); err == nil {
			t.Fatalf("invalid home %q accepted", home)
		}
	}
}

// The only generated private keys are owned server host-key fixtures, never
// client/account credentials. No TCP listener or network route exists. Real
// SSH and sshd speak over the current owned Unix connector. The positive case
// must reach the deliberate none-auth refusal after exact host-key admission.
func TestStandardManagedGitRealHostKeyAdmissionAndIdentityExpansion(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	sshd := filepath.Join(filepath.Dir(ssh), "sshd")
	keygen := filepath.Join(filepath.Dir(ssh), "ssh-keygen")
	base := shortSocketTempDir(t)
	home := filepath.Join(base, "current home with ' quote")
	stale := filepath.Join(base, "expired-agent-worktrees", "agent1", "ouroboros-ide", ".devhome-agent1")
	cfg := buildGitSSHConfigWithProxyCommand(stale, []string{"id_ed25519"}, "expired-connector")
	writeTestFile(t, filepath.Join(home, ".ssh", "config"), cfg)
	for _, name := range []string{"host-key", "wrong-host-key"} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		out, err := exec.CommandContext(ctx, keygen, "-q", "-t", "ed25519", "-N", "", "-f", filepath.Join(base, name)).CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("owned server host-key fixture: %v %s", err, out)
		}
	}
	known := filepath.Join(base, "trusted-hosts")
	wrong := filepath.Join(base, "wrong-hosts")
	for _, pair := range [][2]string{{"host-key", known}, {"wrong-host-key", wrong}} {
		b, err := os.ReadFile(filepath.Join(base, pair[0]+".pub"))
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Fields(string(b))
		if len(fields) < 2 {
			t.Fatal("owned host public key malformed")
		}
		writeTestFile(t, pair[1], "[ssh.github.com]:443 "+fields[0]+" "+fields[1]+"\n")
	}
	serverConfig := filepath.Join(base, "server-config")
	writeTestFile(t, serverConfig, fmt.Sprintf("HostKey %s\nPidFile %s\nUsePAM no\nUseDNS no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPubkeyAuthentication no\nHostbasedAuthentication no\nAuthorizedKeysFile none\nAuthorizedKeysCommand none\nLogLevel VERBOSE\n", filepath.Join(base, "host-key"), filepath.Join(base, "server.pid")))
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"old-missing-trust", "canonical-owned-trust", "wrong-key-refusal"} {
		t.Run(name, func(t *testing.T) {
			socket := filepath.Join(base, fmt.Sprintf("current-%d.sock", i))
			p := managedGitFixturePlan(t, home, socket)
			proxy := filepath.Join(p.RuntimeAuthorityRoot, "kit", "bin", "devctl")
			script := "#!" + bash + "\nexport DEVKIT_OWNED_GIT_PROXY_FIXTURE=1\nexec " + shellQuote(testBinary) + " -test.run='^TestManagedRuntimeGitOwnedProxyHelper$' -- \"$@\"\n"
			if err := os.WriteFile(proxy, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			proxyCommand, err := gitManagedProxyCommand(p)
			if err != nil {
				t.Fatal(err)
			}
			authority := realManagedGitAuthority(t, known)
			command, err := authority.ManagedGitCommand(home)
			if name == "old-missing-trust" {
				command, err = authority.Command(filepath.Join(home, ".ssh", "config"))
			}
			if name == "wrong-key-refusal" {
				command, err = realManagedGitAuthority(t, wrong).ManagedGitCommand(home)
			}
			if err != nil {
				t.Fatal(err)
			}
			command += " -o " + shellQuote("ProxyCommand="+proxyCommand)
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			if err := listener.(*net.UnixListener).SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
				t.Fatal(err)
			}
			serverDone := make(chan error, 1)
			serverCtx, serverCancel := context.WithTimeout(context.Background(), 10*time.Second)
			joined := false
			joinServer := func() error {
				if joined {
					return nil
				}
				select {
				case err := <-serverDone:
					joined = true
					return err
				case <-time.After(12 * time.Second):
					return fmt.Errorf("owned server did not terminate and reap")
				}
			}
			t.Cleanup(func() {
				_ = listener.Close()
				serverCancel()
				if err := joinServer(); err != nil {
					t.Errorf("owned server all-branch cleanup: %v", err)
				}
			})
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					serverDone <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				server := exec.CommandContext(serverCtx, sshd, "-i", "-e", "-f", serverConfig)
				server.Stdin, server.Stdout = conn, conn
				var log bytes.Buffer
				server.Stderr = &log
				err = server.Run()
				if serverCtx.Err() != nil {
					serverDone <- serverCtx.Err()
					return
				}
				if server.ProcessState == nil || !server.ProcessState.Exited() {
					serverDone <- fmt.Errorf("owned sshd not reaped")
					return
				}
				if len(log.Bytes()) > 65536 {
					serverDone <- fmt.Errorf("owned sshd output bound exceeded")
					return
				}
				if err != nil {
					exit, ok := err.(*exec.ExitError)
					if !ok || exit.ExitCode() != 255 {
						serverDone <- fmt.Errorf("unexpected owned sshd exit: %v %s", err, log.String())
						return
					}
				}
				serverDone <- nil
			}()
			out, err := realManagedSSH(t, command, "-v", "-o", "PreferredAuthentications=none", "-o", "IdentityAgent=none", "-l", "devkit-owned-absent-user", "github.com", "true")
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 255 {
				t.Fatalf("owned none-auth transport exit: %v %s", err, out)
			}
			if name == "canonical-owned-trust" {
				for _, want := range []string{"is known and matches the ED25519 host key", "Found key in " + known + ":1", "Permission denied", "identity file " + home + "/.ssh/id_ed25519", "identity file " + home + "/.ssh/id_rsa"} {
					if !strings.Contains(string(out), want) {
						t.Fatalf("real hostkey/identity stage omitted %q: %s", want, out)
					}
				}
				if strings.Contains(string(out), "Host key verification failed") {
					t.Fatalf("trusted key refused: %s", out)
				}
			} else {
				if !strings.Contains(string(out), "Host key verification failed") || strings.Contains(string(out), "Permission denied") {
					t.Fatalf("strict refusal stage differs: %s", out)
				}
				if name == "wrong-key-refusal" && !strings.Contains(string(out), "REMOTE HOST IDENTIFICATION HAS CHANGED") {
					t.Fatalf("wrong host key was not rejected: %s", out)
				}
			}
			if err := joinServer(); err != nil {
				t.Fatalf("owned server cleanup: %v", err)
			}
			t.Logf("real OpenSSH case=%s exact_stage_verified=true authentication_success=false owned_ssh_and_sshd_reaped=true", name)
		})
	}
	if b, err := os.ReadFile(filepath.Join(home, ".ssh", "config")); err != nil || string(b) != cfg {
		t.Fatal("saved config changed")
	}
}

func TestStandardManagedGitPolicyPreservesAllExistingStandardLaneGeometries(t *testing.T) {
	for _, row := range []struct {
		index int
		kind  string
	}{
		{1, "local-wsl-devkit-agent"}, {2, "local-wsl-devkit-agent"}, {3, "local-wsl-devkit-agent"},
		{1, "devkit-agent"}, {2, "devkit-agent"},
	} {
		t.Run(fmt.Sprintf("%s-agent%d", row.kind, row.index), func(t *testing.T) {
			p := standardManagedGitFixture(t, row.index, row.kind)
			command, err := managedRuntimeGitSSHCommand(p)
			if err != nil || !strings.Contains(command, "DEVKIT_MANAGED_GIT_HOME="+shellQuote(p.Agent.SandboxHome)) {
				t.Fatalf("valid standard geometry refused: %v %s", err, command)
			}
			p.Agent.SandboxHome += "/changed"
			if _, err := managedRuntimeGitSSHCommand(p); err == nil {
				t.Fatal("changed standard geometry admitted")
			}
			p = standardManagedGitFixture(t, row.index, row.kind)
			p.Agent.HostHome += "/changed"
			if _, err := managedRuntimeGitSSHCommand(p); err == nil {
				t.Fatal("changed host home admitted")
			}
		})
	}
}

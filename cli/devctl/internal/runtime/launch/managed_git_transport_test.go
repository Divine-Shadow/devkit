package launch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devkit/cli/devctl/internal/gitauthority"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/sshauthority"
)

func managedGitFixturePlan(t *testing.T, home, socket string) nativeplan.Plan {
	t.Helper()
	root := t.TempDir()
	devctl := filepath.Join(root, "kit", "bin", "devctl")
	writeTestFile(t, devctl, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(devctl, 0o755); err != nil {
		t.Fatal(err)
	}
	p := nativeplan.Plan{RuntimeAuthorityRoot: root, IsolationProfile: nativeplan.IsolationProfileWorkspaceEgress,
		DevkitSandboxRoot: home, RuntimeLauncher: runtimeLauncherFixture(t), BubblewrapBinary: runtimeLauncherFixture(t),
		Proxy: nativeplan.ProxyConfig{UnixSocket: socket, AllowlistPath: filepath.Join(root, "allowlist")},
		Binds: []nativeplan.Bind{{Source: socket, Target: socket, Mode: "ro", Required: true}}, Env: map[string]string{"OWNED_FIXTURE": "unchanged"}}
	p.Agent.ID.Project = "dev-all"
	p.Agent.SandboxHome = home
	p.Agent.HostHome = home
	return p
}

func managedGitEnvironment(t *testing.T, p nativeplan.Plan) string {
	t.Helper()
	spec, err := BuildBubblewrap(p, []string{"git", "ls-remote"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+2 < len(spec.Args); i++ {
		if spec.Args[i] == "--setenv" && spec.Args[i+1] == "GIT_SSH_COMMAND" {
			return spec.Args[i+2]
		}
	}
	t.Fatal("managed sandbox omitted its source-owned Git command")
	return ""
}

func TestManagedRuntimeGitSSHCommandOverridesExpiredConfigWithRealOpenSSH(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	base, err := resolvePackageSSHAuthority()
	if err != nil {
		t.Fatal(err)
	}
	authority, err := sshauthority.New(ssh, base.KnownHostsFile())
	if err != nil {
		t.Fatal(err)
	}
	prior := resolvePackageSSHAuthority
	resolvePackageSSHAuthority = func() (sshauthority.Authority, error) { return authority, nil }
	t.Cleanup(func() { resolvePackageSSHAuthority = prior })
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprint("nested-home-", nested), func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home")
			if nested {
				home = filepath.Join(home, "ouroboros-ide", ".devhome-agent1")
			}
			old := filepath.Join(t.TempDir(), "expired.sock")
			current := filepath.Join(t.TempDir(), ".managed-egress-123.sock")
			p := managedGitFixturePlan(t, home, current)
			cfg := buildGitSSHConfigWithProxyCommand(home, []string{"id_ed25519", "id_rsa"}, "expired-connector --socket "+old)
			writeTestFile(t, filepath.Join(home, ".ssh", "config"), cfg)
			command := managedGitEnvironment(t, p)
			bash, err := exec.LookPath("bash")
			if err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(bash, "-c", command+" -G github.com").CombinedOutput()
			if err != nil {
				t.Fatalf("real SSH config: %v: %s", err, out)
			}
			for _, want := range []string{"hostname ssh.github.com", "port 443", "strictHostKeyChecking yes", "batchmode yes", "identitiesonly yes", "userknownhostsfile " + filepath.Join(home, ".ssh", "known_hosts"), "identityfile " + filepath.Join(home, ".ssh", "id_ed25519"), "--socket " + current} {
				if !strings.Contains(strings.ToLower(string(out)), strings.ToLower(want)) {
					t.Fatalf("real OpenSSH omitted %q: %s", want, out)
				}
			}
			if strings.Contains(string(out), old) {
				t.Fatalf("real SSH selected expired connector: %s", out)
			}
			got, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
			if err != nil || string(got) != cfg {
				t.Fatal("existing SSH config changed")
			}
			if _, ok := p.Env["GIT_SSH_COMMAND"]; ok {
				t.Fatal("sandbox rendering mutated shared plan environment")
			}
			q := p
			q.Proxy.UnixSocket = filepath.Join(filepath.Dir(current), ".managed-egress-124.sock")
			other := managedGitEnvironment(t, q)
			if strings.Contains(other, current) || !strings.Contains(other, q.Proxy.UnixSocket) || managedGitEnvironment(t, p) != command {
				t.Fatal("readiness/sibling connector replaced active execution")
			}
			for _, key := range []string{"GIT_SSH_COMMAND", "GIT_SSH_VARIANT"} {
				p.Env[key] = "hostile"
				if _, err := BuildBubblewrap(p, []string{"true"}); err == nil || !strings.Contains(err.Error(), "caller "+key+" override") {
					t.Fatalf("accepted caller override: %v", err)
				}
				delete(p.Env, key)
			}
		})
	}
}

// This account-free helper models Git's SSH transport over an owned Unix
// connector. Real OpenSSH option precedence is checked separately above.
func TestManagedRuntimeGitOwnedProxyHelper(t *testing.T) {
	if os.Getenv("DEVKIT_OWNED_GIT_PROXY_FIXTURE") != "1" {
		return
	}
	var socket string
	for i, a := range os.Args {
		if a == "--socket" && i+1 < len(os.Args) {
			socket = os.Args[i+1]
		}
	}
	c, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	u := c.(*net.UnixConn)
	_ = u.SetDeadline(time.Now().Add(10 * time.Second))
	input := make(chan error, 1)
	go func() {
		_, e := io.Copy(u, os.Stdin)
		if closeErr := u.CloseWrite(); e == nil {
			e = closeErr
		}
		input <- e
	}()
	_, outputErr := io.Copy(os.Stdout, u)
	inputErr := <-input
	closeErr := u.Close()
	if outputErr != nil || inputErr != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, "owned connector copy failed", outputErr, inputErr, closeErr)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestManagedRuntimeGitUsesCurrentOwnedConnectorForOrdinaryGit(t *testing.T) {
	git := gitauthority.Executable()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	base, err := resolvePackageSSHAuthority()
	if err != nil {
		t.Fatal(err)
	}
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprint("nested-home-", nested), func(t *testing.T) {
			short, err := os.MkdirTemp("", "owned-git-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(short) })
			home := filepath.Join(short, "home")
			if nested {
				home = filepath.Join(home, "ouroboros-ide", ".devhome-agent1")
			}
			expired := filepath.Join(short, "expired.sock")
			current := filepath.Join(short, "current.sock")
			p := managedGitFixturePlan(t, home, current)
			proxy := filepath.Join(p.RuntimeAuthorityRoot, "kit", "bin", "devctl")
			if err := os.WriteFile(proxy, []byte("#!"+bash+"\nexec "+shellQuote(testBinary)+" -test.run='^TestManagedRuntimeGitOwnedProxyHelper$' -- \"$@\"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			ssh := filepath.Join(short, "fixture-ssh")
			script := "#!" + bash + "\nproxy=\"$DEVKIT_OWNED_EXPIRED_PROXY\"\nwhile [ \"$#\" -gt 0 ]; do\n if [ \"$1\" = '-o' ]; then shift; case \"$1\" in ProxyCommand=*) proxy=\"${1#ProxyCommand=}\";; esac; fi\n shift\ndone\nproxy=\"${proxy//%h/ssh.github.com}\"\nproxy=\"${proxy//%p/443}\"\nexec " + shellQuote(bash) + " -c \"$proxy\"\n"
			if err := os.WriteFile(ssh, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			authority, err := sshauthority.New(ssh, base.KnownHostsFile())
			if err != nil {
				t.Fatal(err)
			}
			prior := resolvePackageSSHAuthority
			resolvePackageSSHAuthority = func() (sshauthority.Authority, error) { return authority, nil }
			defer func() { resolvePackageSSHAuthority = prior }()
			expiredCommand := shellQuote(proxy) + " -p dev-all native proxy-connect --socket " + shellQuote(expired) + " --target %h:%p"
			cfg := buildGitSSHConfigWithProxyCommand(home, []string{"id_ed25519"}, expiredCommand)
			writeTestFile(t, filepath.Join(home, ".ssh", "config"), cfg)
			repo := filepath.Join(short, "remote")
			if out, err := exec.Command(git, "init", "--bare", repo).CombinedOutput(); err != nil {
				t.Fatalf("owned Git init: %v %s", err, out)
			}
			blob := exec.Command(git, "-C", repo, "hash-object", "-w", "--stdin")
			blob.Stdin = strings.NewReader("owned public Git fixture\n")
			oid, err := blob.Output()
			if err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(git, "-C", repo, "update-ref", "refs/tags/owned", strings.TrimSpace(string(oid))).CombinedOutput(); err != nil {
				t.Fatalf("owned ref: %v %s", err, out)
			}
			legacy, err := authority.Command(filepath.Join(home, ".ssh", "config"))
			if err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(git, "-C", repo, "config", "core.sshCommand", legacy).CombinedOutput(); err != nil {
				t.Fatalf("owned config: %v %s", err, out)
			}
			listener, err := net.Listen("unix", current)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			served := make(chan error, 1)
			go func() {
				c, e := listener.Accept()
				if e != nil {
					served <- e
					return
				}
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(10 * time.Second))
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, git, "upload-pack", repo)
				cmd.Stdin = c
				cmd.Stdout = c
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				e = cmd.Run()
				if e != nil {
					e = fmt.Errorf("upload-pack: %w %s", e, stderr.String())
				}
				served <- e
			}()
			t.Setenv("DEVKIT_OWNED_GIT_PROXY_FIXTURE", "1")
			t.Setenv("DEVKIT_OWNED_EXPIRED_PROXY", expiredCommand)
			command := managedGitEnvironment(t, p)
			run := func(useCurrent bool) ([]byte, error) {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				c := exec.CommandContext(ctx, git, "-C", repo, "ls-remote", "ssh://git@github.com/owned")
				c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_SSH_VARIANT=ssh")
				if useCurrent {
					c.Env = append(c.Env, "GIT_SSH_COMMAND="+command)
				}
				return c.CombinedOutput()
			}
			if out, err := run(false); err == nil || !strings.Contains(string(out), expired) {
				t.Fatalf("expired saved connector unexpectedly accepted: %v %s", err, out)
			}
			out, err := run(true)
			if err != nil || !strings.Contains(string(out), strings.TrimSpace(string(oid))+"\trefs/tags/owned") {
				t.Fatalf("ordinary Git on current connector: %v %s", err, out)
			}
			select {
			case err := <-served:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("owned upload-pack not reaped")
			}
			got, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
			if err != nil || string(got) != cfg {
				t.Fatal("saved config changed")
			}
		})
	}
}

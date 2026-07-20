//go:build devkitintegration

package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProductAdapterConstructsAndExecutesTwoAbsentConsumersThroughPackageAuthority(t *testing.T) {
	gitExecutable := requireProductAdapterInput(t, "DEVKIT_TEST_REAL_SLOT_GIT")
	envExecutable := requireProductAdapterInput(t, "DEVKIT_TEST_REAL_SLOT_ENV")
	sshExecutable := requireProductAdapterInput(t, "DEVKIT_TEST_REAL_SLOT_SSH")
	sshdExecutable := requireProductAdapterInput(t, "DEVKIT_TEST_REAL_SLOT_SSHD")
	sshKeygen := requireProductAdapterInput(t, "DEVKIT_TEST_REAL_SLOT_SSH_KEYGEN")
	gitUploadPack := requireProductAdapterInput(t, "DEVKIT_TEST_REAL_SLOT_UPLOAD_PACK")
	shellExecutable := requireProductAdapterInput(t, "DEVKIT_TEST_REAL_SLOT_SHELL")
	nssWrapper := requireProductAdapterInput(t, "DEVKIT_TEST_NSS_WRAPPER")
	codexExecutable := requireProductAdapterInput(t, "DEVKIT_TEST_CODEX")
	firstExecutable := requireProductAdapterInput(t, "DEVKIT_TEST_PRINTF")

	root, err := os.MkdirTemp("/tmp", "dpa-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	remote, revision := createProductAdapterRemote(t, root, gitExecutable)
	clientKey, knownHosts, sshdAddress := startProductAdapterSSHD(
		t,
		root,
		sshExecutable,
		sshdExecutable,
		sshKeygen,
		gitUploadPack,
		shellExecutable,
		nssWrapper,
		remote,
	)

	first := runProductAdapterConsumer(
		t, filepath.Join(root, "consumer-a"), remote, revision,
		gitExecutable, envExecutable, sshExecutable, shellExecutable,
		clientKey, knownHosts, sshdAddress, codexExecutable, firstExecutable,
	)
	second := runProductAdapterConsumer(
		t, filepath.Join(root, "consumer-b"), remote, revision,
		gitExecutable, envExecutable, sshExecutable, shellExecutable,
		clientKey, knownHosts, sshdAddress, codexExecutable, firstExecutable,
	)
	if first == second {
		t.Fatal("independent Product adapter consumers reused one boundary")
	}
}

func requireProductAdapterInput(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Skipf("%s is not supplied", name)
	}
	return value
}

func createProductAdapterRemote(t *testing.T, root, gitExecutable string) (string, string) {
	t.Helper()
	seed := filepath.Join(root, "seed")
	remote := filepath.Join(root, "ouroboros-ide.git")
	runNativeFixtureCommand(t, gitExecutable, "init", "--initial-branch=main", seed)
	runNativeFixtureCommand(t, gitExecutable, "-C", seed, "config", "user.name", "Product Adapter Fixture")
	runNativeFixtureCommand(t, gitExecutable, "-C", seed, "config", "user.email", "adapter@example.invalid")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("adapter fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runNativeFixtureCommand(t, gitExecutable, "-C", seed, "add", ".")
	runNativeFixtureCommand(t, gitExecutable, "-C", seed, "commit", "-m", "fixture")
	revision := strings.TrimSpace(runNativeFixtureCommand(t, gitExecutable, "-C", seed, "rev-parse", "HEAD"))
	runNativeFixtureCommand(t, gitExecutable, "init", "--bare", "--initial-branch=main", remote)
	runNativeFixtureCommand(t, gitExecutable, "-C", seed, "push", remote, "main:main")
	return remote, revision
}

func startProductAdapterSSHD(
	t *testing.T,
	root, sshExecutable, sshdExecutable, sshKeygen, gitUploadPack, shellExecutable, nssWrapper, remote string,
) (string, string, string) {
	t.Helper()
	hostKey := filepath.Join(root, "sshd-host-key")
	clientKey := filepath.Join(root, "client-key")
	for label, path := range map[string]string{"host": hostKey, "client": clientKey} {
		command := exec.Command(sshKeygen, "-q", "-t", "ed25519", "-N", "", "-C", "ephemeral-"+label, "-f", path)
		command.Env = []string{"PATH=" + filepath.Dir(sshKeygen)}
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("generate %s key: %v\n%s", label, err, output)
		}
	}
	clientPublic, err := os.ReadFile(clientKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	hostPublic, err := os.ReadFile(hostKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	hostFields := strings.Fields(string(hostPublic))
	if len(hostFields) < 2 {
		t.Fatalf("invalid host key: %q", hostPublic)
	}
	knownHosts := filepath.Join(root, "known_hosts")
	if err := os.WriteFile(
		knownHosts,
		[]byte("[ssh.github.com]:443 "+hostFields[0]+" "+hostFields[1]+"\n"),
		0o444,
	); err != nil {
		t.Fatal(err)
	}
	currentUser, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	forcedCommand := filepath.Join(root, "upload-pack")
	forced := "#!" + shellExecutable + "\n" +
		"test \"${SSH_ORIGINAL_COMMAND:-}\" = \"git-upload-pack '" + strings.ReplaceAll(remote, "'", "'\\''") + "'\" || exit 91\n" +
		"exec " + shellSingleQuoteForNativeFixture(gitUploadPack) + " " + shellSingleQuoteForNativeFixture(remote) + "\n"
	if err := os.WriteFile(forcedCommand, []byte(forced), 0o755); err != nil {
		t.Fatal(err)
	}
	authorizedKeys := filepath.Join(root, "authorized_keys")
	if err := os.WriteFile(
		authorizedKeys,
		[]byte("restrict,command="+strconv.Quote(forcedCommand)+" "+strings.TrimSpace(string(clientPublic))+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	address := reserveNativeFixtureAddress(t)
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	passwd := filepath.Join(root, "passwd")
	group := filepath.Join(root, "group")
	if err := os.WriteFile(passwd, []byte(fmt.Sprintf(
		"%s:x:%s:%s:fixture:%s:%s\n",
		currentUser.Username, currentUser.Uid, currentUser.Gid, root, shellExecutable,
	)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(group, []byte(fmt.Sprintf(
		"fixture:x:%s:%s\n", currentUser.Gid, currentUser.Username,
	)), 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "sshd_config")
	if err := os.WriteFile(config, []byte(strings.Join([]string{
		"ListenAddress 127.0.0.1",
		"Port " + port,
		"HostKey " + hostKey,
		"AuthorizedKeysFile " + authorizedKeys,
		"StrictModes no",
		"PasswordAuthentication no",
		"KbdInteractiveAuthentication no",
		"PubkeyAuthentication yes",
		"UsePAM no",
	}, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "sshd.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	sshd := exec.Command(sshdExecutable, "-D", "-e", "-f", config)
	sshd.Stdout = logFile
	sshd.Stderr = logFile
	sshd.Env = append(os.Environ(),
		"LD_PRELOAD="+nssWrapper,
		"NSS_WRAPPER_PASSWD="+passwd,
		"NSS_WRAPPER_GROUP="+group,
	)
	if err := sshd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sshd.Process.Kill()
		_, _ = sshd.Process.Wait()
		_ = logFile.Close()
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		connection, err := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			break
		}
		if time.Now().After(deadline) {
			logs, _ := os.ReadFile(logPath)
			t.Fatalf("sshd readiness: %v\n%s", err, logs)
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = sshExecutable
	return clientKey, knownHosts, address
}

func runProductAdapterConsumer(
	t *testing.T,
	base, remote, revision, gitExecutable, envExecutable, sshExecutable, shellExecutable,
	clientKey, knownHosts, sshdAddress string,
	codexExecutable, firstExecutable string,
) string {
	t.Helper()
	resourcesRoot := filepath.Join(base, "resources")
	runtimeRoot := filepath.Join(resourcesRoot, "runtime")
	for _, path := range []string{resourcesRoot, runtimeRoot} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	allowlist := writeProductAdapterFile(t, filepath.Join(resourcesRoot, "allowlist"), "ssh.github.com\n", 0o444)
	codexConfig := writeProductAdapterFile(t, filepath.Join(resourcesRoot, "codex-config"), strings.Join([]string{
		"# source = nixos-wsl codex config",
		`model = "gpt-5.5"`,
		`model_provider = "openai"`,
		"",
		"[profiles.openai]",
		`model = "gpt-5.5"`,
		`model_provider = "openai"`,
		"",
	}, "\n"), 0o444)
	codexAuthSecret := "fixture-codex-auth-secret-must-not-enter-receipt"
	codexAuth := writeProductAdapterFile(t, filepath.Join(resourcesRoot, "codex-auth"), codexAuthSecret+"\n", 0o600)
	governanceEnv := writeProductAdapterFile(t, filepath.Join(resourcesRoot, "governance.env"), "GOVERNANCE_MODE=fixture\n", 0o444)
	governanceRepo := writeProductAdapterFile(t, filepath.Join(resourcesRoot, "governance-repo.json"), "{}\n", 0o444)
	governanceRules := writeProductAdapterFile(t, filepath.Join(resourcesRoot, "governance.rules"), "prefix_rule(pattern=[\"git\"], decision=\"allow\")\n", 0o444)
	shellHook := writeProductAdapterFile(t, filepath.Join(resourcesRoot, "shell-hook"), "export DEVKIT_PRODUCT_FIXTURE=1\n", 0o444)
	resolv := writeProductAdapterFile(t, filepath.Join(resourcesRoot, "resolv.conf"), "nameserver 192.0.2.53\n", 0o444)
	bwrapLog := filepath.Join(base, "bwrap.log")
	bwrap := writeProductAdapterFile(t, filepath.Join(resourcesRoot, "bwrap"), "#!"+shellExecutable+`
set -eu
printf 'called\n' >> `+shellSingleQuoteForNativeFixture(bwrapLog)+`
exit 0
`, 0o555)
	manifestPath := filepath.Join(base, "authority.json")
	adapterPath := filepath.Join(runtimeRoot, "kit", "bin", "product-adapter")
	proxyPath := filepath.Join(runtimeRoot, "kit", "bin", "product-proxy")
	readinessPath := filepath.Join(runtimeRoot, "kit", "bin", "product-readiness")
	buildProductAdapterBinary(
		t, readinessPath, "./cmd/product-readiness", manifestPath,
		gitExecutable, envExecutable, sshExecutable, knownHosts,
		shellExecutable, bwrap, shellExecutable, runtimeRoot, allowlist, codexConfig,
		governanceEnv, governanceRepo, governanceRules, shellHook,
		codexExecutable, readinessPath, firstExecutable, proxyPath,
	)
	buildProductAdapterBinary(
		t, adapterPath, "./cmd/product-adapter", manifestPath,
		gitExecutable, envExecutable, sshExecutable, knownHosts,
		shellExecutable, bwrap, shellExecutable, runtimeRoot, allowlist, codexConfig,
		governanceEnv, governanceRepo, governanceRules, shellHook,
		codexExecutable, readinessPath, firstExecutable, proxyPath,
	)
	buildProductAdapterBinary(
		t, proxyPath, "./cmd/product-proxy", manifestPath,
		gitExecutable, envExecutable, sshExecutable, knownHosts,
		shellExecutable, bwrap, shellExecutable, runtimeRoot, allowlist, codexConfig,
		governanceEnv, governanceRepo, governanceRules, shellHook,
		codexExecutable, readinessPath, firstExecutable, proxyPath,
	)
	if err := os.Chmod(runtimeRoot, 0o555); err != nil {
		t.Fatal(err)
	}
	proxyURL, connectTarget, proxyDone := startProductAdapterOuterProxy(t, sshdAddress)
	currentUser, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	currentUID := os.Geteuid()
	candidateRoot := filepath.Join(base, "candidate")
	agentRoot := filepath.Join(candidateRoot, "agent2")
	worktree := filepath.Join(agentRoot, "ouroboros-ide")
	home := filepath.Join(agentRoot, ".devhome-agent2")
	stateRoot := filepath.Join(candidateRoot, "state")
	receiptPath := filepath.Join(stateRoot, "product-construction-receipt.json")
	proxyPathname := filepath.Join(stateRoot, "product-egress.sock")
	dummyRoot := filepath.Join(base, "unused-candidate")
	dummyAgent := filepath.Join(dummyRoot, "agent1")
	dummyState := filepath.Join(dummyRoot, "state")
	binds := []map[string]any{
		{"source": runtimeRoot, "target": "/run/devkit/runtime", "mode": "ro", "required": true},
		{"source": worktree, "target": "/workspaces/dev", "mode": "rw", "required": true},
		{"source": home, "target": "/home/product", "mode": "rw", "required": true},
		{"source": stateRoot, "target": "/agent-state/product", "mode": "rw", "required": true},
	}
	manifest := map[string]any{
		"schemaVersion": "wsl-nix-dev-all-runtime-authority/v1",
		"sources":       map[string]any{"product": map[string]any{"rev": revision}},
		"devkitProductAdapter": map[string]any{
			"schemaVersion":            "wsl-nix-devkit-product-adapter/v1",
			"executablePath":           adapterPath,
			"proxyHelperPath":          proxyPath,
			"gitPath":                  gitExecutable,
			"envPath":                  envExecutable,
			"sshPath":                  sshExecutable,
			"knownHostsPath":           knownHosts,
			"runtimeLauncherPath":      shellExecutable,
			"bubblewrapPath":           bwrap,
			"brokerPath":               shellExecutable,
			"runtimeRoot":              runtimeRoot,
			"egressAllowlistPath":      allowlist,
			"codexConfigPath":          codexConfig,
			"governanceEnvPath":        governanceEnv,
			"governanceRepoConfigPath": governanceRepo,
			"governanceRulesPath":      governanceRules,
			"shellHookPath":            shellHook,
			"codexExecutablePath":      codexExecutable,
			"readinessExecutablePath":  readinessPath,
			"firstExecutablePath":      firstExecutable,
			"productOrigin":            "ssh://" + currentUser.Username + "@ssh.github.com:443" + remote,
			"count":                    2,
			"baseBranch":               "main",
			"branchPrefix":             "agent",
			"upstreamProxyUrl":         proxyURL,
			"resolvConfPath":           resolv,
			"nscdSocketPath":           "",
			"mountPolicyIdentity":      "devkit/workspace-egress/v3",
			"runtimeEnvironment":       map[string]string{},
			"consumers": []map[string]any{{
				"index":                      1,
				"uid":                        currentUID + 1,
				"candidateRoot":              dummyRoot,
				"agentRoot":                  dummyAgent,
				"worktreePath":               filepath.Join(dummyAgent, "ouroboros-ide"),
				"commonDirPath":              filepath.Join(dummyAgent, ".devkit", "git", "ouroboros-ide.git"),
				"homePath":                   filepath.Join(dummyAgent, ".devhome-agent1"),
				"stateRoot":                  dummyState,
				"receiptPath":                filepath.Join(dummyState, "product-construction-receipt.json"),
				"proxySocketPath":            filepath.Join(dummyState, "product-egress.sock"),
				"brokerSocketPath":           filepath.Join(dummyState, "postgres.sock"),
				"sandboxWorktreePath":        "/workspaces/dev",
				"sandboxHomePath":            "/home/product",
				"sandboxStateRoot":           "/agent-state/product",
				"sandboxProxySocketPath":     "/agent-state/product/product-egress.sock",
				"sandboxBrokerSocketPath":    "/agent-state/product/postgres.sock",
				"governanceEnvTarget":        filepath.Join(dummyState, "governance.env"),
				"governanceRepoConfigTarget": filepath.Join(dummyState, "governance-repo.json"),
				"governanceStateRoot":        filepath.Join(dummyState, "governance"),
				"sshIdentityPath":            filepath.Join(base, "unused-client-key"),
				"sshPublicKeyPath":           filepath.Join(base, "unused-client-key-public"),
				"codexAuthPath":              filepath.Join(base, "unused-codex-auth"),
				"binds":                      []map[string]any{},
			}, {
				"index":                      2,
				"uid":                        currentUID,
				"candidateRoot":              candidateRoot,
				"agentRoot":                  agentRoot,
				"worktreePath":               worktree,
				"commonDirPath":              filepath.Join(agentRoot, ".devkit", "git", "ouroboros-ide.git"),
				"homePath":                   home,
				"stateRoot":                  stateRoot,
				"receiptPath":                receiptPath,
				"proxySocketPath":            proxyPathname,
				"brokerSocketPath":           filepath.Join(stateRoot, "postgres.sock"),
				"sandboxWorktreePath":        "/workspaces/dev",
				"sandboxHomePath":            "/home/product",
				"sandboxStateRoot":           "/agent-state/product",
				"sandboxProxySocketPath":     "/agent-state/product/product-egress.sock",
				"sandboxBrokerSocketPath":    "/agent-state/product/postgres.sock",
				"governanceEnvTarget":        filepath.Join(stateRoot, "governance.env"),
				"governanceRepoConfigTarget": filepath.Join(stateRoot, "governance-repo.json"),
				"governanceStateRoot":        filepath.Join(stateRoot, "governance"),
				"sshIdentityPath":            clientKey,
				"sshPublicKeyPath":           clientKey + ".pub",
				"codexAuthPath":              codexAuth,
				"binds":                      binds,
			}},
			"artifactDigests": productAdapterArtifactDigests(t, map[string]string{
				"adapter": adapterPath, "proxy_helper": proxyPath,
				"git": gitExecutable, "env": envExecutable, "ssh": sshExecutable,
				"known_hosts": knownHosts, "runtime_launcher": shellExecutable,
				"bubblewrap": bwrap, "broker": shellExecutable,
				"egress_allowlist": allowlist, "codex_config": codexConfig,
				"governance_env": governanceEnv, "governance_repo": governanceRepo,
				"governance_rules": governanceRules, "shell_hook": shellHook,
				"codex_executable":     codexExecutable,
				"readiness_executable": readinessPath,
				"first_executable":     firstExecutable,
				"resolv_conf":          resolv,
			}),
		},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(data, '\n'), 0o444); err != nil {
		t.Fatal(err)
	}
	hostilePath := filepath.Join(base, "hostile-path")
	if err := os.MkdirAll(hostilePath, 0o700); err != nil {
		t.Fatal(err)
	}
	hostileUsed := filepath.Join(base, "hostile-used")
	hostileBashEnv := writeProductAdapterFile(
		t,
		filepath.Join(base, "hostile-bash-env"),
		"printf '%s\\n' BASH_ENV >> "+shellSingleQuoteForNativeFixture(hostileUsed)+"\n",
		0o600,
	)
	for _, name := range []string{"git", "env", "ssh"} {
		if err := os.WriteFile(filepath.Join(hostilePath, name), []byte(
			"#!"+shellExecutable+"\nprintf '%s\\n' "+name+" >> "+shellSingleQuoteForNativeFixture(hostileUsed)+"\nexit 99\n",
		), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command(adapterPath, "prepare", "--count", "2", "--index", "2")
	command.Env = []string{
		"PATH=" + hostilePath,
		"BASH_ENV=" + hostileBashEnv,
		"HOME=" + filepath.Join(base, "hostile-home"),
		"XDG_CONFIG_HOME=" + filepath.Join(base, "hostile-xdg-config"),
		"XDG_DATA_HOME=" + filepath.Join(base, "hostile-xdg-data"),
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.sshCommand",
		"GIT_CONFIG_VALUE_0=" + filepath.Join(hostilePath, "ssh"),
		"GIT_COMMON_DIR=" + filepath.Join(base, "hostile-common-dir"),
		"GIT_OBJECT_DIRECTORY=" + filepath.Join(base, "hostile-object-dir"),
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=" + filepath.Join(base, "hostile-alternates"),
		"GIT_INDEX_FILE=" + filepath.Join(base, "hostile-index"),
		"GIT_DIR=" + filepath.Join(base, "hostile-git-dir"),
		"GIT_WORK_TREE=" + filepath.Join(base, "hostile-worktree"),
		"GIT_SSH=" + filepath.Join(hostilePath, "ssh"),
		"GIT_SSH_COMMAND=" + filepath.Join(hostilePath, "ssh"),
		"DEVKIT_ROOT=" + filepath.Join(base, "hostile-root"),
		"DEVKIT_OVERLAYS_DIR=" + filepath.Join(base, "hostile-overlays"),
		"DEVKIT_RUNTIME_AUTHORITY_FILE=" + filepath.Join(base, "caller-authority"),
		"DEVKIT_RUNTIME_SHELL_LAUNCHER=" + filepath.Join(base, "caller-shell"),
		"DEVKIT_RUNTIME_BWRAP_BINARY=" + filepath.Join(base, "caller-bwrap"),
		"DEVKIT_INTEGRATION_RESOLV_SOURCE=" + resolv,
	}
	directProxy := exec.Command(proxyPath, "stdio", "--count", "2", "--index", "2")
	directProxy.Env = command.Env
	if output, err := directProxy.CombinedOutput(); err == nil ||
		!strings.Contains(string(output), "Product receipt") {
		t.Fatalf("private Product proxy accepted direct use without a receipt: %v\n%s", err, output)
	}
	select {
	case target := <-connectTarget:
		t.Fatalf("direct Product proxy misuse reached network target %q", target)
	default:
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	output, runErr := command.Output()
	if runErr != nil {
		t.Fatalf("Product adapter prepare: %v\nstderr=%s\nstdout=%s", runErr, stderr.String(), output)
	}
	select {
	case target := <-connectTarget:
		if target != "ssh.github.com:443" {
			t.Fatalf("CONNECT target = %q", target)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("package proxy recorded no CONNECT: %s", stderr.String())
	}
	if err := <-proxyDone; err != nil {
		t.Fatalf("outer proxy: %v", err)
	}
	var receipt struct {
		SchemaVersion string `json:"schema_version"`
		Status        string `json:"status"`
		Index         int    `json:"index"`
		ProxyCleanup  string `json:"proxy_cleanup"`
	}
	if err := json.Unmarshal(output, &receipt); err != nil {
		t.Fatalf("decode adapter receipt: %v\n%s", err, output)
	}
	if receipt.SchemaVersion != "devkit/product-construction-receipt/v1" ||
		receipt.Status != "ready" || receipt.Index != 2 || receipt.ProxyCleanup != "absent" {
		t.Fatalf("adapter receipt = %+v", receipt)
	}
	if got := strings.TrimSpace(runNativeFixtureCommand(t, gitExecutable, "-C", worktree, "rev-parse", "HEAD")); got != revision {
		t.Fatalf("Product HEAD = %s, want %s", got, revision)
	}
	gitFile, err := os.ReadFile(filepath.Join(worktree, ".git"))
	if err != nil || filepath.IsAbs(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(gitFile)), "gitdir:"))) {
		t.Fatalf("Product Git metadata is not relative: %q %v", gitFile, err)
	}
	if _, err := os.Lstat(hostileUsed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("adapter selected a hostile caller executable: %v", err)
	}
	execCommand := exec.Command(adapterPath, "exec", "--count", "2", "--index", "2", "--", "true")
	execCommand.Env = command.Env
	if execOutput, err := execCommand.CombinedOutput(); err != nil {
		t.Fatalf("Product adapter exec: %v\n%s", err, execOutput)
	}
	if data, err := os.ReadFile(bwrapLog); err != nil || !strings.Contains(string(data), "called") {
		t.Fatalf("adapter did not reach packaged bwrap: %q %v", data, err)
	}
	if _, err := os.Lstat(proxyPathname); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("adapter left proxy residue: %v", err)
	}
	receiptBytes, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var receiptDocument map[string]any
	if err := json.Unmarshal(receiptBytes, &receiptDocument); err != nil {
		t.Fatal(err)
	}
	geometry, ok := receiptDocument["geometry"].(map[string]any)
	if !ok {
		t.Fatalf("receipt geometry is not an object: %#v", receiptDocument["geometry"])
	}
	receiptHome, ok := geometry["home"].(string)
	if !ok || receiptHome == "" {
		t.Fatalf("receipt home is invalid: %#v", geometry["home"])
	}
	geometry["home"] = filepath.Join(base, "caller-home")
	tamperedReceipt, err := json.MarshalIndent(receiptDocument, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, append(tamperedReceipt, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	assertProductAdapterExecRefusesBeforeProxy(t, adapterPath, command.Env, proxyPathname, "geometry")
	if err := os.WriteFile(receiptPath, receiptBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	codexTarget := filepath.Join(home, ".codex", "config.toml")
	codexBytes, err := os.ReadFile(codexTarget)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexTarget, []byte("same-inode-mutation\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertProductAdapterExecRefusesBeforeProxy(t, adapterPath, command.Env, proxyPathname, "changed in place")
	if err := os.WriteFile(codexTarget, codexBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proxyPathname, []byte("foreign\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertProductAdapterExecRefusesBeforeProxy(t, adapterPath, command.Env, proxyPathname, "preexisting path")
	if got, err := os.ReadFile(proxyPathname); err != nil || string(got) != "foreign\n" {
		t.Fatalf("adapter changed foreign proxy pathname: %q %v", got, err)
	}
	if err := os.Remove(proxyPathname); err != nil {
		t.Fatal(err)
	}
	gitPointer := filepath.Join(worktree, ".git")
	gitPointerBytes, err := os.ReadFile(gitPointer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gitPointer); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "caller-gitdir"), gitPointer); err != nil {
		t.Fatal(err)
	}
	assertProductAdapterExecRefusesBeforeProxy(t, adapterPath, command.Env, proxyPathname, "plain file")
	if err := os.Remove(gitPointer); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gitPointer, gitPointerBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	savedHome := receiptHome + ".saved"
	if err := os.Rename(receiptHome, savedHome); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(receiptHome, 0o700); err != nil {
		t.Fatal(err)
	}
	assertProductAdapterExecRefusesBeforeProxy(t, adapterPath, command.Env, proxyPathname, "no such file")
	if err := os.Remove(receiptHome); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(savedHome, receiptHome); err != nil {
		t.Fatal(err)
	}
	retry := exec.Command(adapterPath, "prepare", "--count", "2", "--index", "2")
	retry.Env = command.Env
	if retryOutput, err := retry.CombinedOutput(); err == nil ||
		!strings.Contains(string(retryOutput), "requires absent whole-candidate boundary") {
		t.Fatalf("adapter reused an existing candidate: %v\n%s", err, retryOutput)
	}
	if bytes.Contains(output, []byte(codexAuthSecret)) || bytes.Contains(receiptBytes, []byte(codexAuthSecret)) {
		t.Fatal("Product adapter leaked secret contents into receipt output")
	}
	return base
}

func assertProductAdapterExecRefusesBeforeProxy(
	t *testing.T,
	adapterPath string,
	environment []string,
	proxyPath string,
	detail string,
) {
	t.Helper()
	command := exec.Command(adapterPath, "exec", "--count", "2", "--index", "2", "--", "true")
	command.Env = environment
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), detail) {
		t.Fatalf("Product adapter exec did not fail closed on %s: %v\n%s", detail, err, output)
	}
	if _, statErr := os.Lstat(proxyPath); statErr == nil {
		if !strings.Contains(detail, "preexisting") {
			t.Fatalf("failed Product adapter exec created proxy path %s", proxyPath)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("inspect Product adapter proxy after refusal: %v", statErr)
	}
}

func writeProductAdapterFile(t *testing.T, path, content string, mode os.FileMode) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func buildProductAdapterBinary(
	t *testing.T,
	output, subpackage, manifestPath, gitExecutable, envExecutable, sshExecutable, knownHosts,
	runtimeLauncher, bubblewrap, broker, runtimeRoot, allowlist, codexConfig,
	governanceEnv, governanceRepo, governanceRules, shellHook,
	codexExecutable, readinessPath, firstExecutable, proxyPath string,
) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatal(err)
	}
	ldflags := strings.Join([]string{
		"-X=devkit/cli/devctl/internal/sshauthority.packageExecutable=" + sshExecutable,
		"-X=devkit/cli/devctl/internal/sshauthority.packageKnownHosts=" + knownHosts,
		"-X=devkit/cli/devctl/internal/worktrees.packageGitExecutable=" + gitExecutable,
		"-X=devkit/cli/devctl/internal/worktrees.packageEnvExecutable=" + envExecutable,
		"-X=devkit/cli/devctl/internal/runtime/launch.packageGitExecutable=" + gitExecutable,
		"-X=devkit/cli/devctl/internal/productadapter.packageMode=composed",
		"-X=devkit/cli/devctl/internal/productadapter.packageGitExecutable=" + gitExecutable,
		"-X=devkit/cli/devctl/internal/productadapter.packageEnvExecutable=" + envExecutable,
		"-X=devkit/cli/devctl/internal/productadapter.packageSSHExecutable=" + sshExecutable,
		"-X=devkit/cli/devctl/internal/productadapter.packageKnownHosts=" + knownHosts,
		"-X=devkit/cli/devctl/internal/productadapter.packageRuntimeLauncher=" + runtimeLauncher,
		"-X=devkit/cli/devctl/internal/productadapter.packageBubblewrapExecutable=" + bubblewrap,
		"-X=devkit/cli/devctl/internal/productadapter.packageBrokerExecutable=" + broker,
		"-X=devkit/cli/devctl/internal/productadapter.packageRuntimeRoot=" + runtimeRoot,
		"-X=devkit/cli/devctl/internal/productadapter.packageEgressAllowlist=" + allowlist,
		"-X=devkit/cli/devctl/internal/productadapter.packageCodexConfig=" + codexConfig,
		"-X=devkit/cli/devctl/internal/productadapter.packageGovernanceEnv=" + governanceEnv,
		"-X=devkit/cli/devctl/internal/productadapter.packageGovernanceRepoConfig=" + governanceRepo,
		"-X=devkit/cli/devctl/internal/productadapter.packageGovernanceRules=" + governanceRules,
		"-X=devkit/cli/devctl/internal/productadapter.packageShellHook=" + shellHook,
		"-X=devkit/cli/devctl/internal/productadapter.packageCodexExecutable=" + codexExecutable,
		"-X=devkit/cli/devctl/internal/productadapter.packageReadinessExecutable=" + readinessPath,
		"-X=devkit/cli/devctl/internal/productadapter.packageFirstExecutable=" + firstExecutable,
		"-X=devkit/cli/devctl/internal/productadapter.packageProductProxyExecutable=" + proxyPath,
		"-X=devkit/cli/devctl/internal/productadapter.testAuthorityLocator=" + manifestPath,
	}, " ")
	command := exec.Command("go", "build", "-trimpath", "-tags", "devkitintegration", "-ldflags", ldflags, "-o", output, subpackage)
	command.Dir = filepath.Join("..")
	command.Env = append(os.Environ(), "GO111MODULE=on")
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", subpackage, err, data)
	}
	if err := os.Chmod(output, 0o555); err != nil {
		t.Fatal(err)
	}
}

func productAdapterArtifactDigests(t *testing.T, paths map[string]string) map[string]string {
	t.Helper()
	result := make(map[string]string, len(paths))
	for name, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		result[name] = hex.EncodeToString(sum[:])
	}
	return result
}

func startProductAdapterOuterProxy(t *testing.T, sshdAddress string) (string, <-chan string, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	target := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		client, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer client.Close()
		request, err := http.ReadRequest(bufio.NewReader(client))
		if err != nil {
			done <- err
			return
		}
		target <- request.Host
		server, err := net.Dial("tcp", sshdAddress)
		if err != nil {
			done <- err
			return
		}
		defer server.Close()
		if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			done <- err
			return
		}
		copyErrors := make(chan error, 2)
		go func() {
			_, err := io.Copy(server, client)
			copyErrors <- err
		}()
		go func() {
			_, err := io.Copy(client, server)
			copyErrors <- err
		}()
		done <- errors.Join(<-copyErrors, <-copyErrors)
	}()
	return "http://" + listener.Addr().String(), target, done
}

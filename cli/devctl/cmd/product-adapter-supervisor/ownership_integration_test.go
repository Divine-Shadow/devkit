//go:build devkitintegration

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"devkit/cli/devctl/internal/productadapter"
	"devkit/cli/devctl/internal/productsession"
	"devkit/cli/devctl/internal/runtime/egressproxy"
)

type productionLifecycleIdentity struct {
	AppServerPID       int    `json:"app_server_pid"`
	AppServerStartTime uint64 `json:"app_server_start_time"`
}

func TestSupervisorRunLoadBootstrapHelper(t *testing.T) {
	if os.Getenv("DEVKIT_TEST_SUPERVISOR_RUN_LOAD_HELPER") != "1" {
		return
	}
	manifest := os.Getenv("DEVKIT_TEST_SUPERVISOR_AUTHORITY_FIXTURE")
	if err := productadapter.ConfigureIntegrationPackageFromManifest(manifest); err != nil {
		t.Fatal(err)
	}
	attestation, err := productadapter.AttestInitialNamespaces(productadapter.RoleSupervisor)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(
		[]string{"serve", "--count", "2", "--index", "1"},
		attestation,
	); err != nil {
		t.Fatal(err)
	}
}

// TestSupervisorBootsBeforeCredentialSeedingAndGatesPrepare executes the exact
// compiled run -> productadapter.Load -> serve path. The immutable fixture has
// full production authority shape and exact current-generation executable
// identity, but begins with an absent disposable consumer and credential set.
// Reintroducing load-time supervisor credential validation therefore kills the
// helper before it can create the real control socket.
func TestSupervisorBootsBeforeCredentialSeedingAndGatesPrepare(t *testing.T) {
	baseManifest := os.Getenv("DEVKIT_TEST_PRODUCT_AUTHORITY_MANIFEST")
	if baseManifest == "" {
		t.Fatal("DEVKIT_TEST_PRODUCT_AUTHORITY_MANIFEST is required by the packaged integration gate")
	}
	root, err := os.MkdirTemp("/tmp", "devkit-supervisor-load-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(executable, 0o555); err != nil {
		t.Fatal(err)
	}
	manifest, consumer := writeSupervisorAuthorityFixture(t, baseManifest, executable, root)

	var output bytes.Buffer
	helper := exec.Command(
		executable,
		"-test.run=^TestSupervisorRunLoadBootstrapHelper$",
		"-test.v",
	)
	helper.Env = append(
		os.Environ(),
		"DEVKIT_TEST_SUPERVISOR_RUN_LOAD_HELPER=1",
		"DEVKIT_TEST_SUPERVISOR_AUTHORITY_FIXTURE="+manifest,
	)
	helper.Stdout = &output
	helper.Stderr = &output
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- helper.Wait() }()
	finished := false
	t.Cleanup(func() {
		if finished {
			return
		}
		_ = helper.Process.Kill()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})
	if err := waitForTestUnixSocket(consumer.SupervisorSocketPath, done, 5*time.Second); err != nil {
		t.Fatalf("run -> Load did not reach a live pre-seed supervisor socket: %v\n%s", err, output.String())
	}

	first := requestIntegrationSupervisor(t, consumer.SupervisorSocketPath, productsession.PrepareToken)
	if first.Failure == nil ||
		first.Failure.Phase != string(productsession.FailurePrepare) ||
		!strings.Contains(first.Failure.Message, "supervisor effect boundary") {
		t.Fatalf("pre-seed prepare did not fail at the credential boundary: %+v", first)
	}
	if _, err := os.Lstat(consumer.SupervisorSocketPath); err != nil {
		t.Fatalf("pre-seed credential refusal stopped the loaded supervisor: %v", err)
	}

	for path, mode := range map[string]os.FileMode{
		consumer.SSHIdentityPath:  0o600,
		consumer.SSHPublicKeyPath: 0o644,
		consumer.CodexAuthPath:    0o600,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	second := requestIntegrationSupervisor(t, consumer.SupervisorSocketPath, productsession.PrepareToken)
	if second.Failure == nil ||
		second.Failure.Phase != string(productsession.FailurePrepare) ||
		!strings.Contains(second.Failure.Message, "canonical Product preparation failed") ||
		strings.Contains(second.Failure.Message, "credential") {
		t.Fatalf("post-seed prepare did not cross the real credential gate: %+v", second)
	}
	if _, err := os.Lstat(consumer.SupervisorSocketPath); err != nil {
		t.Fatalf("post-seed typed preparation failure stopped the loaded supervisor: %v", err)
	}

	if err := helper.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatalf("loaded supervisor did not stop cleanly: %v\n%s", err, output.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("loaded supervisor did not stop after SIGTERM\n%s", output.String())
	}
	if _, err := os.Lstat(consumer.SupervisorSocketPath); !os.IsNotExist(err) {
		t.Fatalf("loaded supervisor left its control socket after shutdown: %v", err)
	}
}

func writeSupervisorAuthorityFixture(
	t *testing.T,
	baseManifest string,
	supervisorExecutable string,
	root string,
) (string, productadapter.ConsumerManifest) {
	t.Helper()
	payload, err := os.ReadFile(baseManifest)
	if err != nil {
		t.Fatal(err)
	}
	var authority map[string]any
	if err := json.Unmarshal(payload, &authority); err != nil {
		t.Fatal(err)
	}
	adapter := requiredFixtureObject(t, authority, "devkitProductAdapter")
	digests := requiredFixtureObject(t, adapter, "artifactDigests")
	sum := sha256.Sum256(mustReadFile(t, supervisorExecutable))
	adapter["supervisorExecutablePath"] = supervisorExecutable
	digests["supervisor"] = fmt.Sprintf("%x", sum[:])

	rawConsumers, ok := adapter["consumers"].([]any)
	if !ok || len(rawConsumers) != 2 {
		t.Fatal("production-shaped fixture does not declare exactly two consumers")
	}
	var selected productadapter.ConsumerManifest
	currentUID, currentGID := os.Geteuid(), os.Getegid()
	for offset, raw := range rawConsumers {
		consumer, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("consumer %d is not an object", offset+1)
		}
		index := offset + 1
		uid, gid := currentUID, currentGID
		if index == 2 {
			uid++
			gid++
		}
		candidate := filepath.Join(root, fmt.Sprintf("consumer-%d", index), "slot")
		agent := filepath.Join(candidate, fmt.Sprintf("agent%d", index))
		worktree := filepath.Join(agent, productadapter.ProductRepo)
		common := filepath.Join(agent, ".devkit", "git", productadapter.ProductRepo+".git")
		home := filepath.Join(candidate, "home")
		state := filepath.Join(candidate, "state")
		control := filepath.Join(root, "supervisors", fmt.Sprintf("control-%d", index))
		consumer["uid"] = uid
		consumer["gid"] = gid
		consumer["candidateRoot"] = candidate
		consumer["agentRoot"] = agent
		consumer["worktreePath"] = worktree
		consumer["commonDirPath"] = common
		consumer["homePath"] = home
		consumer["stateRoot"] = state
		consumer["receiptPath"] = filepath.Join(state, "product-construction-receipt.json")
		consumer["proxySocketPath"] = filepath.Join(state, "product-egress.sock")
		consumer["brokerSocketPath"] = filepath.Join(state, "postgres.sock")
		consumer["supervisorSocketPath"] = filepath.Join(control, "product-supervisor.sock")
		consumer["appServerSocketPath"] = filepath.Join(state, "app-server.sock")
		consumer["governanceEnvTarget"] = filepath.Join(state, "governance.env")
		consumer["governanceRepoConfigTarget"] = filepath.Join(state, "governance-repo.json")
		consumer["governanceStateRoot"] = filepath.Join(state, "governance")
		consumer["sshIdentityPath"] = filepath.Join(home, ".ssh", "id_ed25519")
		consumer["sshPublicKeyPath"] = filepath.Join(home, ".ssh", "id_ed25519.pub")
		consumer["codexAuthPath"] = filepath.Join(home, ".codex", "auth.json")
		for _, rawBind := range consumer["binds"].([]any) {
			bind := rawBind.(map[string]any)
			switch bind["target"] {
			case "/workspaces/dev":
				bind["source"] = worktree
			case "/home/product":
				bind["source"] = home
			case "/agent-state/product":
				bind["source"] = state
			}
		}
		if index == 1 {
			if err := os.MkdirAll(control, 0o700); err != nil {
				t.Fatal(err)
			}
			selected = productadapter.ConsumerManifest{
				Index:                index,
				UID:                  uid,
				GID:                  gid,
				CandidateRoot:        candidate,
				HomePath:             home,
				StateRoot:            state,
				ReceiptPath:          consumer["receiptPath"].(string),
				SupervisorSocketPath: consumer["supervisorSocketPath"].(string),
				SSHIdentityPath:      consumer["sshIdentityPath"].(string),
				SSHPublicKeyPath:     consumer["sshPublicKeyPath"].(string),
				CodexAuthPath:        consumer["codexAuthPath"].(string),
			}
		}
	}
	encoded, err := json.Marshal(authority)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "authority.json")
	if err := os.WriteFile(manifest, encoded, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(manifest, 0o444); err != nil {
		t.Fatal(err)
	}
	return manifest, selected
}

func requiredFixtureObject(t *testing.T, object map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := object[key].(map[string]any)
	if !ok {
		t.Fatalf("fixture field %s is not an object", key)
	}
	return value
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func requestIntegrationSupervisor(t *testing.T, path, original string) productsession.Response {
	t.Helper()
	connection, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := productsession.WriteFrame(connection, productsession.Request{
		SchemaVersion:   productsession.RequestSchema,
		Count:           2,
		Index:           1,
		OriginalCommand: original,
	}); err != nil {
		t.Fatal(err)
	}
	var response productsession.Response
	if err := productsession.ReadFrame(connection, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

// TestPinnedCodexRejectsSameUIDDecoyListener is the packaged sabotage gate for
// the exact acceptance predicate used by the supervisor. A real pinned Codex
// app-server remains alive while a different process with the same uid owns
// the declared listener. Filesystem ownership and process identity alone must
// not let the pinned process inherit the decoy's socket.
func TestPinnedCodexRejectsSameUIDDecoyListener(t *testing.T) {
	pinnedCodex := os.Getenv("DEVKIT_TEST_PINNED_CODEX")
	if pinnedCodex == "" {
		t.Fatal("DEVKIT_TEST_PINNED_CODEX is required by the packaged integration gate")
	}

	home := t.TempDir()
	codex := exec.Command(pinnedCodex, "app-server", "--stdio")
	codex.Env = []string{
		"HOME=" + home,
		"CODEX_HOME=" + filepath.Join(home, ".codex"),
	}
	codexStdin, err := codex.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer codexStdin.Close()
	codex.Stdout = io.Discard
	codex.Stderr = io.Discard
	if err := codex.Start(); err != nil {
		t.Fatal(err)
	}
	defer stopTestProcess(t, codex)

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := processStartTime(codex.Process.Pid); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pinned Codex app-server did not remain alive")
		}
		time.Sleep(10 * time.Millisecond)
	}

	path := filepath.Join(t.TempDir(), "same-uid-decoy.sock")
	decoy := exec.Command(os.Args[0], "-test.run=^TestUnixSocketOwnerHelper$")
	decoy.Env = append(
		os.Environ(),
		"DEVKIT_TEST_SOCKET_OWNER_HELPER=1",
		"DEVKIT_TEST_SOCKET_OWNER_PATH="+path,
	)
	decoyStdin, err := decoy.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	decoyStdout, err := decoy.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	decoy.Stderr = os.Stderr
	if err := decoy.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = decoyStdin.Close()
		stopTestProcess(t, decoy)
	}()
	ready := bufio.NewScanner(decoyStdout)
	if !ready.Scan() || ready.Text() != "ready" {
		t.Fatalf("same-uid decoy was not ready: %q %v", ready.Text(), ready.Err())
	}
	if codex.Process.Pid == decoy.Process.Pid {
		t.Fatal("test setup did not produce distinct processes")
	}

	codexStart, err := processStartTime(codex.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proveProcessOwnsListeningUnixSocket(
		codex.Process.Pid,
		codexStart,
		path,
		path,
	); err == nil {
		t.Fatal("pinned Codex inherited a same-uid decoy listener")
	}
}

// TestProductionAdapterProxyLifecycleHelper is the adapter side of the
// supervisor shutdown integration. It uses the production proxy server and a
// real pinned Codex app-server. The parent test drives the production
// supervisor's exact child-first stop path; this helper does not unlink either
// socket itself.
func TestProductionAdapterProxyLifecycleHelper(t *testing.T) {
	if os.Getenv("DEVKIT_TEST_PRODUCTION_ADAPTER_LIFECYCLE") != "1" {
		return
	}
	pinnedCodex := os.Getenv("DEVKIT_TEST_PINNED_CODEX")
	root := os.Getenv("DEVKIT_TEST_PRODUCTION_LIFECYCLE_ROOT")
	if pinnedCodex == "" || root == "" {
		t.Fatal("production adapter lifecycle helper requires pinned Codex and an exact root")
	}
	proxySocket := filepath.Join(root, "product-egress.sock")
	appServerSocket := filepath.Join(root, "app-server.sock")
	allowlist := filepath.Join(root, "allowlist")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(allowlist, []byte("example.invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	proxyContext, cancelProxy := context.WithCancel(context.Background())
	proxyDone := make(chan error, 1)
	go func() {
		proxyDone <- egressproxy.Serve(proxyContext, egressproxy.Config{
			SocketPath:    proxySocket,
			AllowlistPath: allowlist,
		})
	}()
	if err := waitForTestUnixSocket(proxySocket, nil, 5*time.Second); err != nil {
		cancelProxy()
		t.Fatal(err)
	}

	appServer := exec.Command(
		pinnedCodex,
		"-c", "features.code_mode_host=true",
		"app-server", "--listen", "unix://"+appServerSocket,
	)
	appServer.Env = []string{
		"HOME=" + home,
		"CODEX_HOME=" + filepath.Join(home, ".codex"),
		"TMPDIR=" + root,
	}
	appServer.Stdout = io.Discard
	appServer.Stderr = io.Discard
	if err := appServer.Start(); err != nil {
		cancelProxy()
		<-proxyDone
		t.Fatal(err)
	}
	if err := waitForTestUnixSocket(appServerSocket, nil, 10*time.Second); err != nil {
		_ = appServer.Process.Kill()
		_ = appServer.Wait()
		cancelProxy()
		<-proxyDone
		t.Fatal(err)
	}
	startTime, err := processStartTime(appServer.Process.Pid)
	if err != nil {
		_ = appServer.Process.Kill()
		_ = appServer.Wait()
		cancelProxy()
		<-proxyDone
		t.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(productionLifecycleIdentity{
		AppServerPID:       appServer.Process.Pid,
		AppServerStartTime: startTime,
	}); err != nil {
		t.Fatal(err)
	}

	// The production supervisor sends SIGTERM to this exact app-server PID.
	// Only after it exits does the adapter unwind its production proxy.
	_ = appServer.Wait()
	cancelProxy()
	if err := <-proxyDone; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(proxySocket); !os.IsNotExist(err) {
		t.Fatalf("production proxy left socket residue: %v", err)
	}
}

func TestSupervisorChildFirstStopCleansProductionAdapterAndProxy(t *testing.T) {
	pinnedCodex := os.Getenv("DEVKIT_TEST_PINNED_CODEX")
	if pinnedCodex == "" {
		t.Fatal("DEVKIT_TEST_PINNED_CODEX is required by the packaged integration gate")
	}
	root, err := os.MkdirTemp("/tmp", "devkit-product-lifecycle-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	proxySocket := filepath.Join(root, "product-egress.sock")
	appServerSocket := filepath.Join(root, "app-server.sock")

	adapter := exec.Command(
		os.Args[0],
		"-test.run=^TestProductionAdapterProxyLifecycleHelper$",
		"-test.v",
	)
	adapter.Env = append(
		os.Environ(),
		"DEVKIT_TEST_PRODUCTION_ADAPTER_LIFECYCLE=1",
		"DEVKIT_TEST_PRODUCTION_LIFECYCLE_ROOT="+root,
	)
	adapter.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := adapter.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	adapter.Stderr = os.Stderr
	if err := adapter.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- adapter.Wait() }()
	cleanupFailure := func() {
		_ = syscall.Kill(-adapter.Process.Pid, syscall.SIGKILL)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}

	var identity productionLifecycleIdentity
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if json.Unmarshal(scanner.Bytes(), &identity) == nil && identity.AppServerPID > 0 {
			break
		}
	}
	if identity.AppServerPID == 0 {
		cleanupFailure()
		t.Fatalf("production adapter did not report an app-server identity: %v", scanner.Err())
	}
	observedStart, err := processStartTime(identity.AppServerPID)
	if err != nil || observedStart != identity.AppServerStartTime {
		cleanupFailure()
		t.Fatalf(
			"reported app-server identity is not live and exact: pid=%d start=%d observed=%d err=%v",
			identity.AppServerPID,
			identity.AppServerStartTime,
			observedStart,
			err,
		)
	}
	appServerOwnership, err := proveProcessOwnsListeningUnixSocket(
		identity.AppServerPID,
		identity.AppServerStartTime,
		appServerSocket,
		appServerSocket,
	)
	if err != nil {
		cleanupFailure()
		t.Fatal(err)
	}
	if _, err := os.Lstat(proxySocket); err != nil {
		cleanupFailure()
		t.Fatalf("production proxy socket was not live before shutdown: %v", err)
	}

	service := &supervisor{
		command:            adapter,
		done:               done,
		processGroup:       adapter.Process.Pid,
		commandStartTime:   mustProcessStartTime(t, adapter.Process.Pid),
		appServerPID:       identity.AppServerPID,
		appServerStartTime: identity.AppServerStartTime,
		appServerSocket:    appServerOwnership,
		consumer: productadapter.ConsumerManifest{
			ProxySocketPath:     proxySocket,
			AppServerSocketPath: appServerSocket,
		},
	}
	if err := service.stopTargetLocked(); err != nil {
		cleanupFailure()
		t.Fatal(err)
	}
	if service.command != nil || service.processGroup != 0 ||
		service.appServerPID != 0 || service.appServerStartTime != 0 {
		t.Fatal("supervisor retained managed process identity after production cleanup")
	}
	for _, path := range []string{proxySocket, appServerSocket} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("production shutdown left Unix socket residue at %s: %v", path, err)
		}
	}
}

func waitForTestUnixSocket(path string, failure <-chan error, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return nil
		}
		if failure != nil {
			select {
			case err := <-failure:
				return err
			default:
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return os.ErrDeadlineExceeded
}

func mustProcessStartTime(t *testing.T, pid int) uint64 {
	t.Helper()
	value, err := processStartTime(pid)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func stopTestProcess(t *testing.T, command *exec.Cmd) {
	t.Helper()
	if command.Process == nil {
		return
	}
	_ = command.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = command.Process.Kill()
		<-done
	}
}

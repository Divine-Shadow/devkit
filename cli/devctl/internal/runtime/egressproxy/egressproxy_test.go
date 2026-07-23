package egressproxy

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devkit/cli/devctl/internal/testsupport"
)

func TestAllowlistAllowsExactAndSubdomains(t *testing.T) {
	allowlist := Allowlist{domains: map[string]bool{"example.com": true}}
	for _, host := range []string{"example.com", "www.example.com", "api.www.example.com:443"} {
		if !allowlist.Allowed(host) {
			t.Fatalf("expected %s to be allowed", host)
		}
	}
}

func TestAllowlistRejectsSiblingSuffixesAndIPs(t *testing.T) {
	allowlist := Allowlist{domains: map[string]bool{"example.com": true}}
	for _, host := range []string{"badexample.com", "example.org", "93.184.216.34", "[::1]:443"} {
		if allowlist.Allowed(host) {
			t.Fatalf("expected %s to be rejected", host)
		}
	}
}

func TestRepositoryAllowlistScopesSessionManagerToDeclaredRegion(t *testing.T) {
	allowlistPath := filepath.Join("..", "..", "..", "..", "..", "kit", "proxy", "allowlist.txt")
	allowlist, err := LoadAllowlist(allowlistPath)
	if err != nil {
		t.Fatalf("load repository allowlist: %v", err)
	}

	for _, host := range []string{
		"ssm.us-east-2.amazonaws.com",
		"ssm.us-east-2.amazonaws.com:443",
		"ssmmessages.us-east-2.amazonaws.com",
		"ssmmessages.us-east-2.amazonaws.com:443",
	} {
		if !allowlist.Allowed(host) {
			t.Fatalf("expected declared Session Manager endpoint %s to be allowed", host)
		}
	}

	for _, host := range []string{
		"ssm.us-east-1.amazonaws.com",
		"ssmmessages.us-east-1.amazonaws.com",
		"ec2messages.us-east-2.amazonaws.com",
		"evil-ssm.us-east-2.amazonaws.com",
		"ssm.us-east-2.amazonaws.com.attacker.invalid",
		"169.254.169.254",
	} {
		if allowlist.Allowed(host) {
			t.Fatalf("expected unrelated Session Manager endpoint %s to be rejected", host)
		}
	}

	dnsPath := filepath.Join("..", "..", "..", "..", "..", "kit", "dns", "dnsmasq.conf")
	dnsData, err := os.ReadFile(dnsPath)
	if err != nil {
		t.Fatalf("load repository DNS policy: %v", err)
	}
	dnsPolicy := string(dnsData)
	for _, line := range []string{
		"server=/ssm.us-east-2.amazonaws.com/1.1.1.1",
		"server=/ssmmessages.us-east-2.amazonaws.com/1.1.1.1",
	} {
		if strings.Count(dnsPolicy, line) != 1 {
			t.Fatalf("expected declared DNS policy line exactly once: %s", line)
		}
	}
	for _, fragment := range []string{
		"server=/ssm.us-east-1.amazonaws.com/",
		"server=/ssmmessages.us-east-1.amazonaws.com/",
		"server=/ec2messages.us-east-2.amazonaws.com/",
	} {
		if strings.Contains(dnsPolicy, fragment) {
			t.Fatalf("expected unrelated DNS policy fragment to remain absent: %s", fragment)
		}
	}
}

func TestServeChainsConnectThroughConfiguredUpstreamProxy(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	requestHost := make(chan string, 1)
	go func() {
		conn, err := upstream.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		requestHost <- req.Host
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		payload := make([]byte, 4)
		if _, err := io.ReadFull(conn, payload); err == nil && string(payload) == "ping" {
			_, _ = io.WriteString(conn, "pong")
		}
	}()

	tmp := testsupport.UnixSocketDir(t)
	allowlistPath := filepath.Join(tmp, "allowlist.txt")
	if err := os.WriteFile(allowlistPath, []byte("example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(tmp, "egress.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve(ctx, Config{
			SocketPath:       socketPath,
			AllowlistPath:    allowlistPath,
			UpstreamProxyURL: "http://" + upstream.Addr().String(),
		})
	}()

	var conn net.Conn
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial egress proxy: %v", err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %s", resp.Status)
	}
	if _, err := io.WriteString(conn, "ping"); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 4)
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatal(err)
	}
	if string(payload) != "pong" {
		t.Fatalf("tunnel payload = %q", payload)
	}
	select {
	case got := <-requestHost:
		if got != "example.com:443" {
			t.Fatalf("upstream CONNECT host = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("upstream proxy did not receive CONNECT")
	}
}

func TestConnectUsesExactUnixSocketAndPreservesImmediateTunnelBytes(t *testing.T) {
	tmp := testsupport.UnixSocketDir(t)
	socketPath := filepath.Join(tmp, "consumer.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	targetCh := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		targetCh <- req.Host
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\nSSH-2.0-test\r\n")
		payload := make([]byte, 4)
		if _, err := io.ReadFull(conn, payload); err == nil && string(payload) == "ping" {
			_, _ = io.WriteString(conn, "pong")
		}
	}()

	var output bytes.Buffer
	if err := Connect(context.Background(), socketPath, "ssh.github.com:443", strings.NewReader("ping"), &output); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := output.String(); got != "SSH-2.0-test\r\npong" {
		t.Fatalf("tunnel output = %q", got)
	}
	select {
	case got := <-targetCh:
		if got != "ssh.github.com:443" {
			t.Fatalf("CONNECT target = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("Unix proxy did not receive CONNECT")
	}
}

func TestConnectNeverTouchesHostileFixedLoopbackBridge(t *testing.T) {
	hostile, err := net.Listen("tcp", "127.0.0.1:18888")
	if err != nil {
		t.Skipf("fixed loopback port unavailable for hostile-listener test: %v", err)
	}
	defer hostile.Close()
	if tcpListener, ok := hostile.(*net.TCPListener); ok {
		_ = tcpListener.SetDeadline(time.Now().Add(250 * time.Millisecond))
	}
	hostileAccepted := make(chan error, 1)
	go func() {
		conn, acceptErr := hostile.Accept()
		if acceptErr == nil {
			_ = conn.Close()
		}
		hostileAccepted <- acceptErr
	}()

	tmp := testsupport.UnixSocketDir(t)
	socketPath := filepath.Join(tmp, "consumer.sock")
	managed, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close()
	go func() {
		conn, err := managed.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = http.ReadRequest(bufio.NewReader(conn))
		_, _ = io.WriteString(conn, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
	}()
	err = Connect(context.Background(), socketPath, "ssh.github.com:443", strings.NewReader(""), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "403 Forbidden") {
		t.Fatalf("Connect error = %v, want managed Unix proxy rejection", err)
	}
	if acceptErr := <-hostileAccepted; acceptErr == nil {
		t.Fatal("package-owned CONNECT helper touched hostile 127.0.0.1:18888 listener")
	}
}

func TestConnectFailsClosedOnProxyRejection(t *testing.T) {
	for _, status := range []string{"403 Forbidden", "502 Bad Gateway"} {
		t.Run(status, func(t *testing.T) {
			tmp := testsupport.UnixSocketDir(t)
			socketPath := filepath.Join(tmp, "consumer.sock")
			listener, err := net.Listen("unix", socketPath)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_, _ = http.ReadRequest(bufio.NewReader(conn))
				_, _ = io.WriteString(conn, "HTTP/1.1 "+status+"\r\nContent-Length: 0\r\n\r\n")
			}()

			var output bytes.Buffer
			err = Connect(context.Background(), socketPath, "forbidden.invalid:443", strings.NewReader("payload"), &output)
			if err == nil || !strings.Contains(err.Error(), status) {
				t.Fatalf("Connect error = %v, want %s rejection", err, status)
			}
			if output.Len() != 0 {
				t.Fatalf("rejected CONNECT emitted tunnel bytes %q", output.String())
			}
		})
	}
}

func TestConnectFailsClosedWhenExactUnixSocketIsMissing(t *testing.T) {
	var output bytes.Buffer
	err := Connect(
		context.Background(),
		testsupport.UnixSocketPath(t, "missing.sock"),
		"ssh.github.com:443",
		strings.NewReader("payload"),
		&output,
	)
	if err == nil || !strings.Contains(err.Error(), "dial managed egress proxy") {
		t.Fatalf("Connect error = %v, want missing socket rejection", err)
	}
	if output.Len() != 0 {
		t.Fatalf("missing socket emitted tunnel bytes %q", output.String())
	}
}

func TestConnectCancellationClosesTunnelWithoutWaitingForOpenInput(t *testing.T) {
	tmp := testsupport.UnixSocketDir(t)
	socketPath := filepath.Join(tmp, "consumer.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	responseWritten := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = http.ReadRequest(bufio.NewReader(conn))
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		close(responseWritten)
		_, _ = io.Copy(io.Discard, conn)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	input, inputWriter := io.Pipe()
	defer inputWriter.Close()
	result := make(chan error, 1)
	go func() {
		result <- Connect(ctx, socketPath, "ssh.github.com:443", input, io.Discard)
	}()
	select {
	case <-responseWritten:
	case <-time.After(time.Second):
		t.Fatal("managed CONNECT response was not written")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
			t.Fatalf("Connect cancellation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Connect did not propagate cancellation while input remained open")
	}
}

func TestDialConnectTargetPreservesBannerBufferedWithUpstreamResponse(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	go func() {
		conn, err := upstream.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = http.ReadRequest(bufio.NewReader(conn))
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\nSSH-BANNER")
	}()

	proxyURL, err := parseUpstreamProxyURL("http://" + upstream.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn, err := dialConnectTarget("ssh.github.com:443", proxyURL)
	if err != nil {
		t.Fatalf("dialConnectTarget: %v", err)
	}
	defer conn.Close()
	payload := make([]byte, len("SSH-BANNER"))
	if _, err := io.ReadFull(conn, payload); err != nil {
		t.Fatalf("read buffered banner: %v", err)
	}
	if got := string(payload); got != "SSH-BANNER" {
		t.Fatalf("buffered banner = %q", got)
	}
}

func TestServeRefusesExistingSocketAuthority(t *testing.T) {
	tmp := testsupport.UnixSocketDir(t)
	socketPath := filepath.Join(tmp, "consumer.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	allowlistPath := filepath.Join(tmp, "allowlist.txt")
	if err := os.WriteFile(allowlistPath, []byte("ssh.github.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = Serve(context.Background(), Config{SocketPath: socketPath, AllowlistPath: allowlistPath})
	if err == nil || !strings.Contains(err.Error(), "refusing existing proxy socket path") {
		t.Fatalf("Serve error = %v, want existing authority rejection", err)
	}
}

func TestRemoveOwnedSocketNeverRemovesReplacement(t *testing.T) {
	path := testsupport.UnixSocketPath(t, "consumer.sock")
	if err := os.WriteFile(path, []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	owned, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	ownedFile, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ownedFile.Close()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}

	removeOwnedSocket(path, owned)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("replacement was removed: %v", err)
	}
	if string(content) != "replacement" {
		t.Fatalf("replacement content = %q", content)
	}
}

func TestServeDrainsFullPackAfterClientHalfClose(t *testing.T) {
	pack := make([]byte, 2<<20)
	if _, err := rand.New(rand.NewSource(20260719)).Read(pack); err != nil {
		t.Fatal(err)
	}
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	upstreamErr := make(chan error, 1)
	go func() {
		conn, err := upstream.Accept()
		if err != nil {
			upstreamErr <- err
			return
		}
		defer conn.Close()
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			upstreamErr <- err
			return
		}
		if req.Host != "ssh.github.com:443" {
			upstreamErr <- fmt.Errorf("CONNECT target = %q", req.Host)
			return
		}
		if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			upstreamErr <- err
			return
		}
		request, err := io.ReadAll(conn)
		if err != nil {
			upstreamErr <- err
			return
		}
		if string(request) != "git-pack-request" {
			upstreamErr <- fmt.Errorf("tunnel request = %q", request)
			return
		}
		for remaining := pack; len(remaining) > 0; {
			chunk := remaining
			if len(chunk) > 16<<10 {
				chunk = chunk[:16<<10]
			}
			n, err := conn.Write(chunk)
			if err != nil {
				upstreamErr <- err
				return
			}
			remaining = remaining[n:]
		}
		if conn, ok := conn.(*net.TCPConn); ok {
			if err := conn.CloseWrite(); err != nil {
				upstreamErr <- err
				return
			}
		}
		upstreamErr <- nil
	}()

	tmp := testsupport.UnixSocketDir(t)
	socketPath := filepath.Join(tmp, "consumer.sock")
	allowlistPath := filepath.Join(tmp, "allowlist.txt")
	if err := os.WriteFile(allowlistPath, []byte("ssh.github.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- Serve(ctx, Config{
			SocketPath:       socketPath,
			AllowlistPath:    allowlistPath,
			UpstreamProxyURL: "http://" + upstream.Addr().String(),
		})
	}()
	conn := dialUnixSocket(t, socketPath)
	if _, err := io.WriteString(conn, "CONNECT ssh.github.com:443 HTTP/1.1\r\nHost: ssh.github.com:443\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %s", resp.Status)
	}
	if _, err := io.WriteString(conn, "git-pack-request"); err != nil {
		t.Fatal(err)
	}
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		t.Fatalf("managed connection type = %T", conn)
	}
	if err := unixConn.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, pack) {
		t.Fatalf("drained pack length = %d, want %d", len(got), len(pack))
	}
	_ = conn.Close()
	if err := <-upstreamErr; err != nil {
		t.Fatalf("upstream tunnel: %v", err)
	}
	cancel()
	if err := <-serveErr; err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if _, err := os.Lstat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("managed proxy socket remained after shutdown: %v", err)
	}
}

func TestRelayFullDuplexPropagatesPeerWriteFailure(t *testing.T) {
	left, leftPeer := net.Pipe()
	right, rightPeer := net.Pipe()
	defer leftPeer.Close()
	defer rightPeer.Close()
	result := make(chan error, 1)
	go func() {
		result <- relayFullDuplex(left, left, failingWriteConn{Conn: right}, right)
	}()
	go func() {
		_, _ = io.WriteString(leftPeer, "pack")
		_ = leftPeer.Close()
	}()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "left-to-right tunnel copy") {
			t.Fatalf("relay error = %v, want directional write failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("relay did not terminate after destination write failure")
	}
}

func TestServeAndConnectCarryCompleteGitSmartProtocolFetch(t *testing.T) {
	tmp := testsupport.UnixSocketDir(t)
	sourceRepo := filepath.Join(tmp, "source")
	remoteRepo := filepath.Join(tmp, "remote.git")
	consumerRepo := filepath.Join(tmp, "consumer")
	runGit(t, "", "init", "--initial-branch=main", sourceRepo)
	runGit(t, sourceRepo, "config", "user.email", "git-pack-regression@example.invalid")
	runGit(t, sourceRepo, "config", "user.name", "git-pack-regression")
	random := rand.New(rand.NewSource(20260719))
	for index := 0; index < 4; index++ {
		payload := make([]byte, 512<<10)
		if _, err := random.Read(payload); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sourceRepo, fmt.Sprintf("payload-%d.bin", index)), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, sourceRepo, "add", ".")
	runGit(t, sourceRepo, "commit", "-m", "nontrivial smart protocol pack")
	sourceHead := strings.TrimSpace(runGit(t, sourceRepo, "rev-parse", "HEAD"))
	runGit(t, "", "clone", "--bare", sourceRepo, remoteRepo)

	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	connectTarget := make(chan string, 1)
	upstreamErr := make(chan error, 1)
	go func() {
		conn, err := upstream.Accept()
		if err != nil {
			upstreamErr <- err
			return
		}
		defer conn.Close()
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			upstreamErr <- err
			return
		}
		connectTarget <- req.Host
		if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			upstreamErr <- err
			return
		}
		var stderr bytes.Buffer
		uploadPack := exec.Command("git", "upload-pack", remoteRepo)
		uploadPack.Stdin = conn
		uploadPack.Stdout = conn
		uploadPack.Stderr = &stderr
		if err := uploadPack.Run(); err != nil {
			upstreamErr <- fmt.Errorf("git upload-pack: %w: %s", err, stderr.String())
			return
		}
		if conn, ok := conn.(*net.TCPConn); ok {
			_ = conn.CloseWrite()
		}
		upstreamErr <- nil
	}()

	socketPath := filepath.Join(tmp, "consumer.sock")
	allowlistPath := filepath.Join(tmp, "allowlist.txt")
	if err := os.WriteFile(allowlistPath, []byte("ssh.github.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- Serve(ctx, Config{
			SocketPath:       socketPath,
			AllowlistPath:    allowlistPath,
			UpstreamProxyURL: "http://" + upstream.Addr().String(),
		})
	}()
	waitForUnixSocket(t, socketPath)

	sshHelper := filepath.Join(tmp, "package-owned-git-ssh")
	helperScript := fmt.Sprintf(
		"#!/bin/sh\nexec %s -test.run=^TestGitSSHProxyHelperProcess$\n",
		shellSingleQuote(os.Args[0]),
	)
	if err := os.WriteFile(sshHelper, []byte(helperScript), 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, "", "init", "--initial-branch=main", consumerRepo)
	runGit(t, consumerRepo, "config", "fetch.unpackLimit", "1")
	runGit(t, consumerRepo, "remote", "add", "origin", "ssh://git@ssh.github.com:443/repository.git")
	fetch := exec.Command("git", "-C", consumerRepo, "fetch", "--all", "--prune")
	fetch.Env = append(os.Environ(),
		"GO_WANT_GIT_SSH_PROXY_HELPER=1",
		"DEVKIT_TEST_PROXY_SOCKET="+socketPath,
		"DEVKIT_TEST_PROXY_TARGET=ssh.github.com:443",
		"GIT_SSH_COMMAND="+sshHelper,
		"GIT_SSH_VARIANT=ssh",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	var fetchOutput bytes.Buffer
	fetch.Stdout = &fetchOutput
	fetch.Stderr = &fetchOutput
	if err := fetch.Run(); err != nil {
		t.Fatalf("git fetch: %v\n%s", err, fetchOutput.String())
	}
	consumerHead := strings.TrimSpace(runGit(t, consumerRepo, "rev-parse", "refs/remotes/origin/main"))
	if consumerHead != sourceHead {
		t.Fatalf("fetched head = %s, want %s", consumerHead, sourceHead)
	}
	packs, err := filepath.Glob(filepath.Join(consumerRepo, ".git", "objects", "pack", "*.pack"))
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 1 {
		t.Fatalf("fetched pack count = %d, want 1", len(packs))
	}
	packInfo, err := os.Stat(packs[0])
	if err != nil {
		t.Fatal(err)
	}
	if packInfo.Size() < 1<<20 {
		t.Fatalf("fetched pack size = %d, want nontrivial pack", packInfo.Size())
	}
	select {
	case target := <-connectTarget:
		if target != "ssh.github.com:443" {
			t.Fatalf("CONNECT target = %q", target)
		}
	case <-time.After(time.Second):
		t.Fatal("upstream proxy did not receive package-owned CONNECT")
	}
	if err := <-upstreamErr; err != nil {
		t.Fatalf("upstream smart protocol: %v", err)
	}
	cancel()
	if err := <-serveErr; err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if _, err := os.Lstat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("managed proxy socket remained after smart-protocol fetch: %v", err)
	}
}

func TestGitSSHProxyHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_GIT_SSH_PROXY_HELPER") != "1" {
		return
	}
	err := Connect(
		context.Background(),
		os.Getenv("DEVKIT_TEST_PROXY_SOCKET"),
		os.Getenv("DEVKIT_TEST_PROXY_TARGET"),
		os.Stdin,
		os.Stdout,
	)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "package-owned proxy helper: %v\n", err)
		os.Exit(97)
	}
	os.Exit(0)
}

func waitForUnixSocket(t *testing.T, socketPath string) {
	t.Helper()
	conn := dialUnixSocket(t, socketPath)
	_ = conn.Close()
}

func dialUnixSocket(t *testing.T, socketPath string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			return conn
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial managed Unix proxy %s: %v", socketPath, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func runGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	if directory != "" {
		command.Dir = directory
	}
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

type failingWriteConn struct {
	net.Conn
}

func (failingWriteConn) Write([]byte) (int, error) {
	return 0, fmt.Errorf("forced peer write failure")
}

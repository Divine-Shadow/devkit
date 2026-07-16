package egressproxy

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
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

	tmp := t.TempDir()
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

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCanonicalDomain(t *testing.T) {
	t.Parallel()
	for _, invalid := range []string{
		"api", "api.openai.com.", "127.0.0.1", "[::1]", "*.openai.com",
		"api_openai.com", "-api.openai.com", "api..openai.com",
	} {
		if _, err := canonicalDomain(invalid); err == nil {
			t.Fatalf("accepted invalid domain %q", invalid)
		}
	}
	if value, err := canonicalDomain("API.OpenAI.com"); err != nil || value != "api.openai.com" {
		t.Fatalf("canonicalDomain = %q, %v", value, err)
	}
}

func TestLoadAllowlistIsExactAndStrict(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "allowlist")
	if err := os.WriteFile(path, []byte("# policy\napi.openai.com\nauth.openai.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	values, err := loadAllowlist(path)
	if err != nil || len(values) != 2 {
		t.Fatalf("loadAllowlist = %#v, %v", values, err)
	}
	for _, content := range []string{
		"*.openai.com\n",
		"127.0.0.1\n",
		"API.openai.com\n",
		" api.openai.com\n",
		"api.openai.com\r\n",
		"api.openai.com\napi.openai.com\n",
	} {
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o400); err != nil {
			t.Fatal(err)
		}
		if _, err := loadAllowlist(path); err == nil {
			t.Fatalf("accepted invalid allowlist %q", content)
		}
	}
}

func TestLoadAllowlistRejectsMutableAndIndirectFiles(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	policy := filepath.Join(directory, "policy")
	if err := os.WriteFile(policy, []byte("api.openai.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAllowlist("relative-policy"); err == nil {
		t.Fatal("accepted a relative policy path")
	}
	if _, err := loadAllowlist(policy); err == nil {
		t.Fatal("accepted an owner-writable policy")
	}
	if err := os.Chmod(policy, 0o400); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "policy-link")
	if err := os.Symlink(policy, link); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAllowlist(link); err == nil {
		t.Fatal("accepted a symlink policy")
	}
}

func TestPublicAddressRejectsPrivateAndReservedSpace(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"0.0.0.1", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.1.1",
		"172.16.0.1", "192.0.2.1", "192.168.1.1", "198.18.0.1",
		"198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1",
		"::1", "64:ff9b::1", "64:ff9b:1::1", "100::1", "2001:db8::1", "3ffe::1",
		"fc00::1", "fe80::1", "fec0::1", "ff00::1",
	} {
		if _, valid := publicAddress(net.ParseIP(value)); valid {
			t.Fatalf("accepted forbidden address %s", value)
		}
	}
	for _, value := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if _, valid := publicAddress(net.ParseIP(value)); !valid {
			t.Fatalf("rejected public address %s", value)
		}
	}
}

func request(t *testing.T, value string) *httpRequestResult {
	t.Helper()
	server, client := net.Pipe()
	result := make(chan httpRequestResult, 1)
	go func() {
		req, early, err := readRequest(server)
		result <- httpRequestResult{request: req, early: early, err: err}
		_ = server.Close()
	}()
	if _, err := io.WriteString(client, value); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	valueResult := <-result
	return &valueResult
}

type httpRequestResult struct {
	request *http.Request
	early   []byte
	err     error
}

func TestConnectContract(t *testing.T) {
	t.Parallel()
	allow := map[string]struct{}{"api.openai.com": {}}
	valid := request(t, "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\nTLS")
	if valid.err != nil {
		t.Fatal(valid.err)
	}
	if host, err := validateConnect(valid.request, allow); err != nil || host != "api.openai.com" || string(valid.early) != "TLS" {
		t.Fatalf("validateConnect = %q, %v, early=%q", host, err, valid.early)
	}

	for _, raw := range []string{
		"GET https://api.openai.com/ HTTP/1.1\r\nHost: api.openai.com\r\n\r\n",
		"CONNECT api.openai.com:80 HTTP/1.1\r\nHost: api.openai.com:80\r\n\r\n",
		"CONNECT github.com:443 HTTP/1.1\r\nHost: github.com:443\r\n\r\n",
		"CONNECT 1.1.1.1:443 HTTP/1.1\r\nHost: 1.1.1.1:443\r\n\r\n",
		"CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\nContent-Length: 1\r\n\r\nx",
		"CONNECT api.openai.com:443 HTTP/1.0\r\nHost: api.openai.com:443\r\n\r\n",
	} {
		parsed := request(t, raw)
		if parsed.err != nil {
			t.Fatal(parsed.err)
		}
		if _, err := validateConnect(parsed.request, allow); err == nil {
			t.Fatalf("accepted forbidden request %q", raw)
		}
	}
}

func testClientHello(serverNames ...string) []byte {
	var names bytes.Buffer
	for _, serverName := range serverNames {
		names.WriteByte(0)
		_ = binary.Write(&names, binary.BigEndian, uint16(len(serverName)))
		names.WriteString(serverName)
	}
	var extensions bytes.Buffer
	if len(serverNames) != 0 {
		var serverNameExtension bytes.Buffer
		_ = binary.Write(&serverNameExtension, binary.BigEndian, uint16(names.Len()))
		serverNameExtension.Write(names.Bytes())
		_ = binary.Write(&extensions, binary.BigEndian, uint16(0))
		_ = binary.Write(&extensions, binary.BigEndian, uint16(serverNameExtension.Len()))
		extensions.Write(serverNameExtension.Bytes())
	}
	var hello bytes.Buffer
	hello.Write([]byte{3, 3})
	hello.Write(make([]byte, 32))
	hello.WriteByte(0)
	hello.Write([]byte{0, 2, 0x13, 0x01})
	hello.Write([]byte{1, 0})
	_ = binary.Write(&hello, binary.BigEndian, uint16(extensions.Len()))
	hello.Write(extensions.Bytes())

	handshake := []byte{1, byte(hello.Len() >> 16), byte(hello.Len() >> 8), byte(hello.Len())}
	handshake = append(handshake, hello.Bytes()...)
	record := []byte{22, 3, 1, byte(len(handshake) >> 8), byte(len(handshake))}
	return append(record, handshake...)
}

func fragmentedClientHello(hello []byte, firstRecordPayload int) []byte {
	handshake := hello[5:]
	first := []byte{22, 3, 1, byte(firstRecordPayload >> 8), byte(firstRecordPayload)}
	first = append(first, handshake[:firstRecordPayload]...)
	remainder := handshake[firstRecordPayload:]
	second := []byte{22, 3, 1, byte(len(remainder) >> 8), byte(len(remainder))}
	return append(first, append(second, remainder...)...)
}

func checkClientHelloBytes(t *testing.T, payload []byte, expectedHost string) ([]byte, error) {
	t.Helper()
	server, client := net.Pipe()
	writeDone := make(chan struct{})
	go func() {
		_, _ = client.Write(payload)
		_ = client.Close()
		close(writeDone)
	}()
	result, err := readClientHello(server, nil, expectedHost)
	_ = server.Close()
	<-writeDone
	return result, err
}

func TestClientHelloBindsTLSNameToConnectAuthority(t *testing.T) {
	t.Parallel()
	valid := testClientHello("api.openai.com")
	result, err := checkClientHelloBytes(t, valid, "api.openai.com")
	if err != nil || !bytes.Equal(result, valid) {
		t.Fatalf("valid ClientHello = %x, %v", result, err)
	}
	fragmented := fragmentedClientHello(valid, 8)
	result, err = checkClientHelloBytes(t, fragmented, "api.openai.com")
	if err != nil || !bytes.Equal(result, fragmented) {
		t.Fatalf("bounded fragmented ClientHello = %x, %v", result, err)
	}
	for name, payload := range map[string][]byte{
		"mismatch":  testClientHello("cohosted.example"),
		"absent":    testClientHello(),
		"duplicate": testClientHello("api.openai.com", "api.openai.com"),
		"non-TLS":   []byte("arbitrary tunnel payload"),
		"oversized": {22, 3, 1, 0x49, 0x00},
		"too-many-fragments": func() []byte {
			handshake := valid[5:]
			result := make([]byte, 0, 6*(maximumTLSRecords+1))
			for index := 0; index <= maximumTLSRecords; index++ {
				result = append(result, 22, 3, 1, 0, 1, handshake[index])
			}
			return result
		}(),
	} {
		name, payload := name, payload
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := checkClientHelloBytes(t, payload, "api.openai.com"); err == nil {
				t.Fatal("accepted TLS payload without one exact matching server name")
			}
		})
	}
}

func TestClientHelloPreservesBoundedEarlyBytes(t *testing.T) {
	t.Parallel()
	hello := testClientHello("api.openai.com")
	early := append(append([]byte(nil), hello...), []byte("subsequent encrypted bytes")...)
	server, client := net.Pipe()
	result, err := readClientHello(server, early, "api.openai.com")
	_ = server.Close()
	_ = client.Close()
	if err != nil || !bytes.Equal(result, early) {
		t.Fatalf("early TLS payload = %x, %v", result, err)
	}
}

func TestRequestHeaderLimit(t *testing.T) {
	t.Parallel()
	parsed := request(t, "CONNECT api.openai.com:443 HTTP/1.1\r\nX-Fill: "+strings.Repeat("x", maximumHeaderBytes)+"\r\n\r\n")
	if parsed.err == nil {
		t.Fatal("accepted request headers over the fixed byte limit")
	}
}

func TestConnectPublicRejectsMixedPrivateDNSWithoutDial(t *testing.T) {
	t.Parallel()
	dialed := false
	service := proxy{
		lookup: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}, {IP: net.ParseIP("127.0.0.1")}}, nil
		},
		dial: func(context.Context, string) (net.Conn, error) {
			dialed = true
			return nil, nil
		},
	}
	if _, err := service.connectPublic("api.openai.com"); err == nil || dialed {
		t.Fatalf("mixed private resolution was not rejected before dial: err=%v dialed=%v", err, dialed)
	}
}

func TestConnectPublicRejectsUnboundedOrScopedDNSWithoutDial(t *testing.T) {
	t.Parallel()
	for name, addresses := range map[string][]net.IPAddr{
		"too-many": func() []net.IPAddr {
			values := make([]net.IPAddr, maximumDNSAddresses+1)
			for index := range values {
				values[index] = net.IPAddr{IP: net.ParseIP("1.1.1.1")}
			}
			return values
		}(),
		"scoped": {{IP: net.ParseIP("2606:4700:4700::1111"), Zone: "unexpected"}},
	} {
		name, addresses := name, addresses
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var dialed atomic.Bool
			service := proxy{
				lookup: func(context.Context, string) ([]net.IPAddr, error) { return addresses, nil },
				dial: func(context.Context, string) (net.Conn, error) {
					dialed.Store(true)
					return nil, nil
				},
			}
			if _, err := service.connectPublic("api.openai.com"); err == nil || dialed.Load() {
				t.Fatalf("unsafe DNS response was not rejected before dial: err=%v dialed=%v", err, dialed.Load())
			}
		})
	}
}

func TestConnectPublicHonorsServiceCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dialed := false
	service := proxy{
		context: ctx,
		lookup: func(ctx context.Context, _ string) ([]net.IPAddr, error) {
			return nil, ctx.Err()
		},
		dial: func(context.Context, string) (net.Conn, error) {
			dialed = true
			return nil, nil
		},
	}
	if _, err := service.connectPublic("api.openai.com"); err == nil || dialed {
		t.Fatalf("canceled service was not stopped before dial: err=%v dialed=%v", err, dialed)
	}
}

func TestProxyTunnelsOnlyValidatedProvider(t *testing.T) {
	t.Parallel()
	upstreamServer, upstreamProxy := net.Pipe()
	service := proxy{
		allow: map[string]struct{}{"api.openai.com": {}},
		lookup: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}, nil
		},
		dial: func(_ context.Context, address string) (net.Conn, error) {
			if address != "1.1.1.1:443" {
				t.Fatalf("unexpected dial address %q", address)
			}
			return &shortWriteConnection{Conn: upstreamProxy, maximum: 7, once: true}, nil
		},
		permits: make(chan struct{}, 1),
	}
	clientProxy, client := net.Pipe()
	service.permits <- struct{}{}
	service.wg.Add(1)
	go service.handle(clientProxy)
	if _, err := io.WriteString(client, "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(client)
	status, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(status, "200") {
		t.Fatalf("status=%q err=%v", status, err)
	}
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	hello := testClientHello("api.openai.com")
	go func() {
		_, _ = client.Write(append(append([]byte(nil), hello...), []byte("provider request")...))
	}()
	payload := make([]byte, len(hello)+len("provider request"))
	if _, err := io.ReadFull(upstreamServer, payload); err != nil || !bytes.Equal(payload[:len(hello)], hello) || string(payload[len(hello):]) != "provider request" {
		t.Fatalf("upstream payload=%x err=%v", payload, err)
	}
	go func() { _, _ = io.WriteString(upstreamServer, "provider response") }()
	response := make([]byte, len("provider response"))
	if _, err := io.ReadFull(reader, response); err != nil || string(response) != "provider response" {
		t.Fatalf("client payload=%q err=%v", response, err)
	}
	_ = client.Close()
	_ = upstreamServer.Close()
	done := make(chan struct{})
	go func() { service.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("proxy handler did not stop")
	}
}

type shortWriteConnection struct {
	net.Conn
	maximum   int
	once      bool
	shortened bool
}

func (connection *shortWriteConnection) Write(payload []byte) (int, error) {
	if connection.once && connection.shortened {
		return connection.Conn.Write(payload)
	}
	if len(payload) > connection.maximum {
		connection.shortened = true
		payload = payload[:connection.maximum]
	}
	return connection.Conn.Write(payload)
}

func TestWriteAllHandlesRepeatedShortWrites(t *testing.T) {
	t.Parallel()
	server, client := net.Pipe()
	connection := &shortWriteConnection{Conn: client, maximum: 3}
	payload := []byte("complete validated ClientHello bytes")
	result := make(chan error, 1)
	go func() { result <- writeAll(connection, payload) }()
	received := make([]byte, len(payload))
	if _, err := io.ReadFull(server, received); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil || !bytes.Equal(received, payload) {
		t.Fatalf("writeAll received=%q err=%v", received, err)
	}
	_ = client.Close()
	_ = server.Close()
}

func TestProxyRejectsMismatchedSNIAtCohostedAddress(t *testing.T) {
	t.Parallel()
	upstreamServer, upstreamProxy := net.Pipe()
	service := proxy{
		allow: map[string]struct{}{"api.openai.com": {}},
		lookup: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}, nil
		},
		dial:    func(context.Context, string) (net.Conn, error) { return upstreamProxy, nil },
		permits: make(chan struct{}, 1),
	}
	clientProxy, client := net.Pipe()
	service.permits <- struct{}{}
	service.wg.Add(1)
	go service.handle(clientProxy)
	if _, err := io.WriteString(client, "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(client)
	if status, err := reader.ReadString('\n'); err != nil || !strings.Contains(status, "200") {
		t.Fatalf("status=%q err=%v", status, err)
	}
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write(testClientHello("cohosted.example")); err != nil {
		t.Fatal(err)
	}
	if err := upstreamServer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if payload, err := io.ReadAll(upstreamServer); err != nil || len(payload) != 0 {
		t.Fatalf("mismatched SNI reached co-hosted address: payload=%x err=%v", payload, err)
	}
	_ = client.Close()
	_ = upstreamServer.Close()
	service.wg.Wait()
}

func TestCloseActiveClosesExistingAndFutureConnections(t *testing.T) {
	t.Parallel()
	service := proxy{active: make(map[net.Conn]struct{})}
	existing, existingPeer := net.Pipe()
	if !service.track(existing) {
		t.Fatal("failed to track an active connection")
	}
	service.closeActive()
	if _, err := existingPeer.Read(make([]byte, 1)); err == nil {
		t.Fatal("shutdown left an existing connection open")
	}
	_ = existingPeer.Close()

	future, futurePeer := net.Pipe()
	if service.track(future) {
		t.Fatal("shutdown accepted a future connection")
	}
	if _, err := futurePeer.Read(make([]byte, 1)); err == nil {
		t.Fatal("rejected future connection was not closed")
	}
	_ = futurePeer.Close()
}

func TestUnixSocketCustodyAndCleanup(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "proxy.sock")
	listener, cleanup, err := listenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := os.Lstat(path)
	if err != nil || metadata.Mode()&os.ModeSocket == 0 || metadata.Mode().Perm() != 0o600 {
		t.Fatalf("socket metadata = %#v, %v", metadata, err)
	}
	_ = listener.Close()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanup()
	if payload, err := os.ReadFile(path); err != nil || string(payload) != "replacement" {
		t.Fatalf("cleanup removed a path it did not own: payload=%q err=%v", payload, err)
	}
	ownedPath := filepath.Join(directory, "owned.sock")
	_, ownedCleanup, err := listenUnix(ownedPath)
	if err != nil {
		t.Fatal(err)
	}
	ownedCleanup()
	if _, err := os.Lstat(ownedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup left its owned socket behind: %v", err)
	}
}

func TestUnixSocketRejectsUnsafePaths(t *testing.T) {
	t.Parallel()
	if _, _, err := listenUnix("relative.sock"); err == nil {
		t.Fatal("accepted a relative socket path")
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, _, err := listenUnix(filepath.Join(directory, "proxy.sock")); err == nil {
		t.Fatal("accepted a socket beneath a writable directory")
	}
}

func TestServeEnforcesConnectionBoundAndStopsCleanly(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "proxy.sock")
	listener, cleanup, err := listenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	service := proxy{
		allow:   map[string]struct{}{"api.openai.com": {}},
		permits: make(chan struct{}, 1),
		active:  make(map[net.Conn]struct{}),
	}
	finished := make(chan error, 1)
	go func() { finished <- service.serve(listener) }()
	first, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	deadline := time.Now().Add(time.Second)
	for len(service.permits) != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(service.permits) != 1 {
		t.Fatal("first connection did not consume the only permit")
	}
	second, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	response, err := bufio.NewReader(second).ReadString('\n')
	_ = second.Close()
	if err != nil || !strings.Contains(response, "503 Service Unavailable") {
		t.Fatalf("bounded connection response=%q err=%v", response, err)
	}
	_ = listener.Close()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	service.closeActive()
	service.wg.Wait()
}

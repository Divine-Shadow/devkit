package egressproxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Config struct {
	SocketPath       string
	AllowlistPath    string
	UpstreamProxyURL string
}

type Allowlist struct {
	domains map[string]bool
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *bufferedConn) CloseRead() error {
	if conn, ok := c.Conn.(interface{ CloseRead() error }); ok {
		return conn.CloseRead()
	}
	return nil
}

func (c *bufferedConn) CloseWrite() error {
	if conn, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return conn.CloseWrite()
	}
	return nil
}

func LoadAllowlist(path string) (Allowlist, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Allowlist{}, err
	}
	domains := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if fields := strings.Fields(line); len(fields) > 0 {
			line = fields[0]
		}
		line = strings.TrimPrefix(strings.ToLower(line), ".")
		if line != "" {
			domains[line] = true
		}
	}
	return Allowlist{domains: domains}, nil
}

func (a Allowlist) Allowed(hostport string) bool {
	host := strings.TrimSpace(hostport)
	if host == "" {
		return false
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	if net.ParseIP(host) != nil {
		return false
	}
	if a.domains[host] {
		return true
	}
	for domain := range a.domains {
		if strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func (a Allowlist) Domains() []string {
	domains := make([]string, 0, len(a.domains))
	for domain := range a.domains {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains
}

func Serve(ctx context.Context, cfg Config) error {
	socketPath := strings.TrimSpace(cfg.SocketPath)
	if socketPath == "" {
		return fmt.Errorf("proxy socket path is required")
	}
	allowlistPath := strings.TrimSpace(cfg.AllowlistPath)
	if allowlistPath == "" {
		return fmt.Errorf("allowlist path is required")
	}
	allowlist, err := LoadAllowlist(allowlistPath)
	if err != nil {
		return fmt.Errorf("read allowlist %s: %w", allowlistPath, err)
	}
	upstreamProxy, err := parseUpstreamProxyURL(cfg.UpstreamProxyURL)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(socketPath), err)
	}
	if _, err := os.Lstat(socketPath); err == nil {
		return fmt.Errorf("refusing existing proxy socket path %s", socketPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect proxy socket %s: %w", socketPath, err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", socketPath, err)
	}
	defer listener.Close()
	if err := os.Chmod(socketPath, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", socketPath, err)
	}
	ownedSocket, err := os.Lstat(socketPath)
	if err != nil {
		return fmt.Errorf("inspect owned proxy socket %s: %w", socketPath, err)
	}
	defer removeOwnedSocket(socketPath, ownedSocket)
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go handleConn(conn, allowlist, upstreamProxy)
	}
}

func removeOwnedSocket(path string, owned os.FileInfo) {
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(owned, current) {
		return
	}
	_ = os.Remove(path)
}

// Connect opens one fail-closed stdio CONNECT tunnel through the exact
// package-owned Unix proxy socket. It deliberately has no TCP, ambient proxy,
// or protocol fallback.
func Connect(ctx context.Context, socketPath string, target string, input io.Reader, output io.Writer) error {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return fmt.Errorf("proxy socket path is required")
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("CONNECT target is required")
	}
	if input == nil || output == nil {
		return fmt.Errorf("CONNECT input and output are required")
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("dial managed egress proxy %s: %w", socketPath, err)
	}
	defer conn.Close()
	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{},
		Host:   target,
		Header: make(http.Header),
	}
	if err := req.Write(conn); err != nil {
		return fmt.Errorf("write managed CONNECT: %w", err)
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		return fmt.Errorf("read managed CONNECT response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return fmt.Errorf("managed CONNECT returned %s", resp.Status)
	}

	tunnelDone := make(chan struct{})
	defer close(tunnelDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-tunnelDone:
		}
	}()
	inputErr := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(conn, input)
		_ = closeWrite(conn)
		inputErr <- copyErr
	}()
	_, outputErr := io.Copy(output, reader)
	_ = conn.Close()
	select {
	case copyErr := <-inputErr:
		return errors.Join(outputErr, copyErr)
	default:
		if ctx.Err() != nil {
			return errors.Join(outputErr, ctx.Err())
		}
		return outputErr
	}
}

func Bridge(ctx context.Context, listenAddr string, socketPath string) error {
	listenAddr = strings.TrimSpace(listenAddr)
	if listenAddr == "" {
		listenAddr = "127.0.0.1:18888"
	}
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return fmt.Errorf("proxy socket path is required")
	}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", listenAddr, err)
	}
	defer listener.Close()
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go bridgeConn(conn, socketPath)
	}
}

func bridgeConn(client net.Conn, socketPath string) {
	defer client.Close()
	upstream, err := net.Dial("unix", socketPath)
	if err != nil {
		return
	}
	defer upstream.Close()
	_ = relayFullDuplex(client, client, upstream, upstream)
}

func handleConn(conn net.Conn, allowlist Allowlist, upstreamProxy *url.URL) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		writeProxyError(conn, http.StatusBadRequest, "bad request")
		return
	}
	req.Close = true
	if strings.EqualFold(req.Method, http.MethodConnect) {
		handleConnect(conn, reader, req, allowlist, upstreamProxy)
		return
	}
	handleHTTP(conn, req, allowlist, upstreamProxy)
}

func handleConnect(client net.Conn, reader *bufio.Reader, req *http.Request, allowlist Allowlist, upstreamProxy *url.URL) {
	target := req.Host
	if !allowlist.Allowed(target) {
		writeProxyError(client, http.StatusForbidden, "forbidden by devkit native egress allowlist")
		return
	}
	if !strings.Contains(target, ":") {
		target += ":443"
	}
	upstream, err := dialConnectTarget(target, upstreamProxy)
	if err != nil {
		writeProxyError(client, http.StatusBadGateway, err.Error())
		return
	}
	defer upstream.Close()
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	_ = relayFullDuplex(client, reader, upstream, upstream)
}

type relayResult struct {
	direction string
	err       error
}

// relayFullDuplex preserves both halves of a CONNECT tunnel until each source
// reaches EOF. A clean EOF in one direction half-closes only the destination's
// write side, allowing an already-negotiated response or Git pack to drain in
// the opposite direction. A real copy failure closes both peers so the other
// copy cannot outlive the failed tunnel.
func relayFullDuplex(left net.Conn, leftReader io.Reader, right net.Conn, rightReader io.Reader) error {
	results := make(chan relayResult, 2)
	copyDirection := func(direction string, destination net.Conn, source io.Reader) {
		_, err := io.Copy(destination, source)
		if err == nil {
			err = closeWrite(destination)
		}
		results <- relayResult{direction: direction, err: err}
	}
	go copyDirection("left-to-right", right, leftReader)
	go copyDirection("right-to-left", left, rightReader)

	first := <-results
	if first.err != nil {
		_ = left.Close()
		_ = right.Close()
	}
	second := <-results
	return errors.Join(
		relayDirectionError(first),
		relayDirectionError(second),
	)
}

func relayDirectionError(result relayResult) error {
	if result.err == nil || errors.Is(result.err, net.ErrClosed) {
		return nil
	}
	return fmt.Errorf("%s tunnel copy: %w", result.direction, result.err)
}

func closeWrite(conn net.Conn) error {
	if conn, ok := conn.(interface{ CloseWrite() error }); ok {
		return conn.CloseWrite()
	}
	return nil
}

func handleHTTP(client net.Conn, req *http.Request, allowlist Allowlist, upstreamProxy *url.URL) {
	target := req.URL.Host
	if target == "" {
		target = req.Host
	}
	if !allowlist.Allowed(target) {
		writeProxyError(client, http.StatusForbidden, "forbidden by devkit native egress allowlist")
		return
	}
	if req.URL.Scheme == "" {
		req.URL.Scheme = "http"
	}
	req.RequestURI = ""
	req.Header.Del("Proxy-Connection")
	transport := &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) { return upstreamProxy, nil },
		DialContext: (&net.Dialer{
			Timeout: 20 * time.Second,
		}).DialContext,
	}
	defer transport.CloseIdleConnections()
	resp, err := transport.RoundTrip(req)
	if err != nil {
		writeProxyError(client, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	_ = resp.Write(client)
}

func parseUpstreamProxyURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse upstream proxy URL: %w", err)
	}
	if parsed.Scheme != "http" || parsed.Hostname() == "" {
		return nil, fmt.Errorf("upstream proxy URL must use http with a host")
	}
	if parsed.Port() == "" {
		parsed.Host = net.JoinHostPort(parsed.Hostname(), "80")
	}
	return parsed, nil
}

func dialConnectTarget(target string, upstreamProxy *url.URL) (net.Conn, error) {
	if upstreamProxy == nil {
		return net.DialTimeout("tcp", target, 20*time.Second)
	}
	conn, err := net.DialTimeout("tcp", upstreamProxy.Host, 20*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial upstream proxy: %w", err)
	}
	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{},
		Host:   target,
		Header: make(http.Header),
	}
	if upstreamProxy.User != nil {
		password, _ := upstreamProxy.User.Password()
		req.SetBasicAuth(upstreamProxy.User.Username(), password)
		req.Header.Set("Proxy-Authorization", req.Header.Get("Authorization"))
		req.Header.Del("Authorization")
	}
	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write upstream CONNECT: %w", err)
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read upstream CONNECT response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		conn.Close()
		return nil, fmt.Errorf("upstream CONNECT returned %s", resp.Status)
	}
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

func writeProxyError(w io.Writer, code int, msg string) {
	body := http.StatusText(code)
	if msg != "" {
		body = msg
	}
	_, _ = fmt.Fprintf(w, "HTTP/1.1 %d %s\r\nContent-Type: text/plain\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", code, http.StatusText(code), len(body), body)
}

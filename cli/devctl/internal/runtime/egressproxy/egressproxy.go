package egressproxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Config struct {
	SocketPath    string
	AllowlistPath string
}

type Allowlist struct {
	domains map[string]bool
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
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(socketPath), err)
	}
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale proxy socket %s: %w", socketPath, err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", socketPath, err)
	}
	defer listener.Close()
	defer os.Remove(socketPath)
	if err := os.Chmod(socketPath, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", socketPath, err)
	}
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
		go handleConn(conn, allowlist)
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
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, client)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		done <- struct{}{}
	}()
	<-done
}

func handleConn(conn net.Conn, allowlist Allowlist) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		writeProxyError(conn, http.StatusBadRequest, "bad request")
		return
	}
	req.Close = true
	if strings.EqualFold(req.Method, http.MethodConnect) {
		handleConnect(conn, reader, req, allowlist)
		return
	}
	handleHTTP(conn, req, allowlist)
}

func handleConnect(client net.Conn, reader *bufio.Reader, req *http.Request, allowlist Allowlist) {
	target := req.Host
	if !allowlist.Allowed(target) {
		writeProxyError(client, http.StatusForbidden, "forbidden by devkit native egress allowlist")
		return
	}
	if !strings.Contains(target, ":") {
		target += ":443"
	}
	upstream, err := net.DialTimeout("tcp", target, 20*time.Second)
	if err != nil {
		writeProxyError(client, http.StatusBadGateway, err.Error())
		return
	}
	defer upstream.Close()
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, reader)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		done <- struct{}{}
	}()
	<-done
}

func handleHTTP(client net.Conn, req *http.Request, allowlist Allowlist) {
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
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		writeProxyError(client, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	_ = resp.Write(client)
}

func writeProxyError(w io.Writer, code int, msg string) {
	body := http.StatusText(code)
	if msg != "" {
		body = msg
	}
	_, _ = fmt.Fprintf(w, "HTTP/1.1 %d %s\r\nContent-Type: text/plain\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", code, http.StatusText(code), len(body), body)
}

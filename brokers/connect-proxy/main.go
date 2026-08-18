package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maximumHeaderBytes  = 16 * 1024
	maximumConnections  = 16
	maximumDNSAddresses = 16
	maximumTLSRecords   = 8
	maximumClientHello  = 64 * 1024
	requestTimeout      = 10 * time.Second
	clientHelloTimeout  = 10 * time.Second
	connectTimeout      = 15 * time.Second
	dialTimeout         = 10 * time.Second
)

var forbiddenPrefixes = mustPrefixes(
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.168.0.0/16",
	"192.88.99.0/24",
	"192.175.48.0/24",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"::/128",
	"::1/128",
	"64:ff9b::/96",
	"64:ff9b:1::/48",
	"100::/64",
	"2001::/23",
	"2001:db8::/32",
	"2002::/16",
	"3fff::/20",
	"3ffe::/16",
	"5f00::/16",
	"fc00::/7",
	"fe80::/10",
	"fec0::/10",
	"ff00::/8",
)

type lookupIP func(context.Context, string) ([]net.IPAddr, error)
type dialTCP func(context.Context, string) (net.Conn, error)

type proxy struct {
	context  context.Context
	allow    map[string]struct{}
	lookup   lookupIP
	dial     dialTCP
	permits  chan struct{}
	wg       sync.WaitGroup
	active   map[net.Conn]struct{}
	activeM  sync.Mutex
	stopping bool
}

func mustPrefixes(values ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefixes = append(prefixes, netip.MustParsePrefix(value))
	}
	return prefixes
}

func canonicalDomain(value string) (string, error) {
	if value == "" || value != strings.TrimSpace(value) || strings.HasSuffix(value, ".") {
		return "", errors.New("host is not a canonical domain")
	}
	host := strings.ToLower(value)
	if len(host) > 253 || net.ParseIP(host) != nil {
		return "", errors.New("host is not a domain")
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return "", errors.New("host is not a fully qualified domain")
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("host has an invalid label")
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') &&
				(character < '0' || character > '9') && character != '-' {
				return "", errors.New("host is not an ASCII DNS name")
			}
		}
	}
	return host, nil
}

func loadAllowlist(path string) (map[string]struct{}, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("allowlist path must be absolute")
	}
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open allowlist: %w", err)
	}
	handle := os.NewFile(uintptr(descriptor), "allowlist")
	if handle == nil {
		_ = syscall.Close(descriptor)
		return nil, errors.New("open allowlist")
	}
	defer handle.Close()
	metadata, err := handle.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect allowlist: %w", err)
	}
	if !metadata.Mode().IsRegular() || metadata.Mode().Perm()&0o222 != 0 {
		return nil, errors.New("allowlist must be a non-writable regular file")
	}
	if metadata.Size() <= 0 || metadata.Size() > 64*1024 {
		return nil, errors.New("allowlist has an invalid size")
	}

	payload, err := io.ReadAll(io.LimitReader(handle, 64*1024+1))
	if err != nil || int64(len(payload)) != metadata.Size() || bytes.ContainsRune(payload, '\r') {
		return nil, errors.New("allowlist content is unstable or non-canonical")
	}
	result := make(map[string]struct{})
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line != strings.TrimSpace(line) {
			return nil, errors.New("allowlist entries must not contain surrounding whitespace")
		}
		if strings.ContainsAny(line, "*/:") {
			return nil, errors.New("allowlist entries must be exact DNS names")
		}
		host, err := canonicalDomain(line)
		if err != nil || host != line {
			return nil, errors.New("allowlist entries must be canonical lowercase DNS names")
		}
		if _, exists := result[host]; exists {
			return nil, errors.New("allowlist contains a duplicate DNS name")
		}
		result[host] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read allowlist: %w", err)
	}
	if len(result) == 0 {
		return nil, errors.New("allowlist must contain at least one DNS name")
	}
	return result, nil
}

func publicAddress(ip net.IP) (netip.Addr, bool) {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Addr{}, false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() ||
		address.IsMulticast() || address.IsUnspecified() {
		return netip.Addr{}, false
	}
	for _, prefix := range forbiddenPrefixes {
		if prefix.Contains(address) {
			return netip.Addr{}, false
		}
	}
	return address, true
}

func readRequest(connection net.Conn) (*http.Request, []byte, error) {
	if err := connection.SetReadDeadline(time.Now().Add(requestTimeout)); err != nil {
		return nil, nil, err
	}
	buffer := make([]byte, 0, 4096)
	temporary := make([]byte, 4096)
	for {
		count, err := connection.Read(temporary)
		if count > 0 {
			buffer = append(buffer, temporary[:count]...)
			if end := bytes.Index(buffer, []byte("\r\n\r\n")); end >= 0 {
				end += 4
				if end > maximumHeaderBytes {
					return nil, nil, errors.New("request headers exceed the fixed limit")
				}
				request, parseErr := http.ReadRequest(bufio.NewReader(bytes.NewReader(buffer[:end])))
				return request, append([]byte(nil), buffer[end:]...), parseErr
			}
			if len(buffer) > maximumHeaderBytes {
				return nil, nil, errors.New("request headers exceed the fixed limit")
			}
		}
		if err != nil {
			return nil, nil, err
		}
	}
}

func validateConnect(request *http.Request, allow map[string]struct{}) (string, error) {
	if request.Method != http.MethodConnect || request.ProtoMajor != 1 || request.ProtoMinor != 1 {
		return "", errors.New("only HTTP/1.1 CONNECT is supported")
	}
	if request.ContentLength > 0 || len(request.TransferEncoding) != 0 || request.URL.Scheme != "" {
		return "", errors.New("CONNECT request has a forbidden body or scheme")
	}
	host, port, err := net.SplitHostPort(request.RequestURI)
	if err != nil || port != "443" || request.Host != request.RequestURI {
		return "", errors.New("CONNECT authority must be an exact host on port 443")
	}
	host, err = canonicalDomain(host)
	if err != nil {
		return "", err
	}
	if _, permitted := allow[host]; !permitted {
		return "", errors.New("host is not allowlisted")
	}
	return host, nil
}

func readClientHello(connection net.Conn, initial []byte, expectedHost string) ([]byte, error) {
	if len(initial) > maximumClientHello+5*maximumTLSRecords {
		return nil, errors.New("early TLS payload exceeds the fixed limit")
	}
	if err := connection.SetReadDeadline(time.Now().Add(clientHelloTimeout)); err != nil {
		return nil, err
	}
	prefix := bytes.NewReader(initial)
	reader := io.MultiReader(prefix, connection)
	raw := make([]byte, 0, 4096)
	handshake := make([]byte, 0, 4096)
	for record := 0; record < maximumTLSRecords; record++ {
		header := make([]byte, 5)
		if _, err := io.ReadFull(reader, header); err != nil {
			return nil, errors.New("TLS ClientHello is incomplete")
		}
		if header[0] != 22 || header[1] != 3 || header[2] < 1 || header[2] > 4 {
			return nil, errors.New("first tunnel payload is not a TLS handshake")
		}
		recordLength := int(binary.BigEndian.Uint16(header[3:5]))
		if recordLength == 0 || recordLength > 18*1024 || len(raw)+5+recordLength > maximumClientHello+5*maximumTLSRecords {
			return nil, errors.New("TLS ClientHello record exceeds the fixed limit")
		}
		payload := make([]byte, recordLength)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, errors.New("TLS ClientHello is incomplete")
		}
		raw = append(raw, header...)
		raw = append(raw, payload...)
		handshake = append(handshake, payload...)
		if len(handshake) < 4 {
			continue
		}
		if handshake[0] != 1 {
			return nil, errors.New("first TLS handshake message is not ClientHello")
		}
		helloLength := int(handshake[1])<<16 | int(handshake[2])<<8 | int(handshake[3])
		if helloLength == 0 || helloLength > maximumClientHello {
			return nil, errors.New("TLS ClientHello exceeds the fixed limit")
		}
		if len(handshake) < 4+helloLength {
			continue
		}
		serverName, err := clientHelloServerName(handshake[4 : 4+helloLength])
		if err != nil || serverName != expectedHost {
			return nil, errors.New("TLS server name does not match CONNECT authority")
		}
		if prefix.Len() != 0 {
			raw = append(raw, initial[len(initial)-prefix.Len():]...)
		}
		return raw, nil
	}
	return nil, errors.New("TLS ClientHello used too many records")
}

func clientHelloServerName(hello []byte) (string, error) {
	offset := 0
	if len(hello) < 2+32 || hello[0] != 3 || hello[1] < 1 || hello[1] > 4 {
		return "", errors.New("TLS ClientHello is truncated")
	}
	offset += 2 + 32
	var ok bool
	var sessionID []byte
	if sessionID, offset, ok = takeVector8(hello, offset); !ok || len(sessionID) > 32 {
		return "", errors.New("TLS session ID is malformed")
	}
	var cipherSuites []byte
	if cipherSuites, offset, ok = takeVector16(hello, offset); !ok || len(cipherSuites) == 0 || len(cipherSuites)%2 != 0 {
		return "", errors.New("TLS cipher suites are malformed")
	}
	var compression []byte
	if compression, offset, ok = takeVector8(hello, offset); !ok || len(compression) != 1 || compression[0] != 0 {
		return "", errors.New("TLS compression methods are malformed")
	}
	extensions, offset, ok := takeVector16(hello, offset)
	if !ok || offset != len(hello) {
		return "", errors.New("TLS extensions are malformed")
	}
	serverName := ""
	serverNameExtensionSeen := false
	for len(extensions) != 0 {
		if len(extensions) < 4 {
			return "", errors.New("TLS extension header is malformed")
		}
		extensionType := binary.BigEndian.Uint16(extensions[:2])
		extensionLength := int(binary.BigEndian.Uint16(extensions[2:4]))
		extensions = extensions[4:]
		if extensionLength > len(extensions) {
			return "", errors.New("TLS extension body is malformed")
		}
		extension := extensions[:extensionLength]
		extensions = extensions[extensionLength:]
		if extensionType != 0 {
			continue
		}
		if serverNameExtensionSeen || len(extension) < 2 || int(binary.BigEndian.Uint16(extension[:2])) != len(extension)-2 {
			return "", errors.New("TLS server-name extension is malformed")
		}
		serverNameExtensionSeen = true
		names := extension[2:]
		for len(names) != 0 {
			if len(names) < 3 {
				return "", errors.New("TLS server-name entry is malformed")
			}
			nameType := names[0]
			nameLength := int(binary.BigEndian.Uint16(names[1:3]))
			names = names[3:]
			if nameLength == 0 || nameLength > len(names) {
				return "", errors.New("TLS server-name value is malformed")
			}
			name := string(names[:nameLength])
			names = names[nameLength:]
			if nameType != 0 {
				return "", errors.New("TLS server-name type is unsupported")
			}
			if serverName != "" {
				return "", errors.New("TLS ClientHello has duplicate host names")
			}
			canonical, err := canonicalDomain(name)
			if err != nil {
				return "", errors.New("TLS server name is not a canonical domain")
			}
			serverName = canonical
		}
	}
	if serverName == "" {
		return "", errors.New("TLS ClientHello has no host name")
	}
	return serverName, nil
}

func takeVector8(value []byte, offset int) ([]byte, int, bool) {
	if offset >= len(value) {
		return nil, offset, false
	}
	length := int(value[offset])
	offset++
	if length > len(value)-offset {
		return nil, offset, false
	}
	return value[offset : offset+length], offset + length, true
}

func takeVector16(value []byte, offset int) ([]byte, int, bool) {
	if offset > len(value)-2 {
		return nil, offset, false
	}
	length := int(binary.BigEndian.Uint16(value[offset : offset+2]))
	offset += 2
	if length > len(value)-offset {
		return nil, offset, false
	}
	return value[offset : offset+length], offset + length, true
}

func genericResponse(connection net.Conn, status int) {
	message := "Bad Request"
	switch status {
	case http.StatusForbidden:
		message = "Forbidden"
	case http.StatusBadGateway:
		message = "Bad Gateway"
	case http.StatusServiceUnavailable:
		message = "Service Unavailable"
	}
	_, _ = fmt.Fprintf(
		connection,
		"HTTP/1.1 %d %s\r\nConnection: close\r\nContent-Length: 0\r\n\r\n",
		status,
		message,
	)
}

func (service *proxy) connectPublic(host string) (net.Conn, error) {
	baseContext := service.context
	if baseContext == nil {
		baseContext = context.Background()
	}
	connectionContext, cancelConnection := context.WithTimeout(baseContext, connectTimeout)
	defer cancelConnection()
	addresses, err := service.lookup(connectionContext, host)
	if err != nil || len(addresses) == 0 || len(addresses) > maximumDNSAddresses {
		return nil, errors.New("DNS resolution failed")
	}
	validated := make([]netip.Addr, 0, len(addresses))
	seen := make(map[netip.Addr]struct{})
	for _, candidate := range addresses {
		if candidate.Zone != "" {
			return nil, errors.New("DNS returned a scoped address")
		}
		address, public := publicAddress(candidate.IP)
		if !public {
			return nil, errors.New("DNS returned a forbidden address")
		}
		if _, exists := seen[address]; !exists {
			validated = append(validated, address)
			seen[address] = struct{}{}
		}
	}
	sort.Slice(validated, func(left, right int) bool {
		return validated[left].String() < validated[right].String()
	})
	var lastError error
	for _, address := range validated {
		connection, dialErr := service.dial(
			connectionContext,
			net.JoinHostPort(address.String(), "443"),
		)
		if dialErr == nil {
			return connection, nil
		}
		lastError = dialErr
	}
	return nil, fmt.Errorf("connect to resolved provider: %w", lastError)
}

func closeWrite(connection net.Conn) {
	if writer, ok := connection.(interface{ CloseWrite() error }); ok {
		_ = writer.CloseWrite()
	}
}

func writeAll(connection net.Conn, payload []byte) error {
	for len(payload) != 0 {
		written, err := connection.Write(payload)
		if written > 0 {
			payload = payload[written:]
		}
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrNoProgress
		}
	}
	return nil
}

func (service *proxy) track(connection net.Conn) bool {
	service.activeM.Lock()
	if service.stopping {
		service.activeM.Unlock()
		_ = connection.Close()
		return false
	}
	if service.active == nil {
		service.active = make(map[net.Conn]struct{})
	}
	service.active[connection] = struct{}{}
	service.activeM.Unlock()
	return true
}

func (service *proxy) untrack(connection net.Conn) {
	service.activeM.Lock()
	defer service.activeM.Unlock()
	delete(service.active, connection)
}

func (service *proxy) closeActive() {
	service.activeM.Lock()
	service.stopping = true
	connections := make([]net.Conn, 0, len(service.active))
	for connection := range service.active {
		connections = append(connections, connection)
	}
	service.activeM.Unlock()
	for _, connection := range connections {
		_ = connection.Close()
	}
}

func (service *proxy) handle(connection net.Conn) {
	defer service.wg.Done()
	defer func() { <-service.permits }()
	defer service.untrack(connection)
	defer connection.Close()

	request, earlyPayload, err := readRequest(connection)
	if err != nil {
		genericResponse(connection, http.StatusBadRequest)
		return
	}
	host, err := validateConnect(request, service.allow)
	if err != nil {
		genericResponse(connection, http.StatusForbidden)
		return
	}
	upstream, err := service.connectPublic(host)
	if err != nil {
		genericResponse(connection, http.StatusBadGateway)
		return
	}
	if !service.track(upstream) {
		return
	}
	defer service.untrack(upstream)
	defer upstream.Close()
	if _, err := io.WriteString(connection, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	clientHello, err := readClientHello(connection, earlyPayload, host)
	if err != nil {
		return
	}
	if err := upstream.SetWriteDeadline(time.Now().Add(clientHelloTimeout)); err != nil {
		return
	}
	if err := writeAll(upstream, clientHello); err != nil {
		return
	}
	_ = connection.SetReadDeadline(time.Time{})
	_ = upstream.SetWriteDeadline(time.Time{})

	clientToUpstream := make(chan struct{})
	go func() {
		_, _ = io.Copy(upstream, connection)
		closeWrite(upstream)
		close(clientToUpstream)
	}()
	_, _ = io.Copy(connection, upstream)
	closeWrite(connection)
	_ = connection.Close()
	<-clientToUpstream
}

func (service *proxy) serve(listener net.Listener) error {
	for {
		connection, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		select {
		case service.permits <- struct{}{}:
			if !service.track(connection) {
				<-service.permits
				continue
			}
			service.wg.Add(1)
			go service.handle(connection)
		default:
			genericResponse(connection, http.StatusServiceUnavailable)
			_ = connection.Close()
		}
	}
}

func listenUnix(path string) (net.Listener, func(), error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
		return nil, nil, errors.New("socket path must be absolute")
	}
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return nil, nil, errors.New("socket parent is unavailable")
	}
	if parent.Mode()&os.ModeSymlink != 0 || !parent.IsDir() || parent.Mode().Perm()&0o022 != 0 {
		return nil, nil, errors.New("socket parent must be a non-writable real directory")
	}
	if _, err := os.Lstat(path); err == nil {
		return nil, nil, errors.New("refusing to replace an existing socket path")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("inspect socket path: %w", err)
	}
	unixListener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, nil, err
	}
	unixListener.SetUnlinkOnClose(false)
	listener := net.Listener(unixListener)
	ownedSocket, err := os.Lstat(path)
	if err != nil || ownedSocket.Mode()&os.ModeSocket == 0 {
		_ = listener.Close()
		return nil, nil, errors.New("listener did not create an owned Unix socket")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		removeOwnedSocket(path, ownedSocket)
		return nil, nil, err
	}
	cleanup := func() {
		_ = listener.Close()
		removeOwnedSocket(path, ownedSocket)
	}
	return listener, cleanup, nil
}

func removeOwnedSocket(path string, owned os.FileInfo) {
	current, err := os.Lstat(path)
	if err == nil && current.Mode()&os.ModeSocket != 0 && os.SameFile(owned, current) {
		_ = os.Remove(path)
	}
}

func run(arguments []string) error {
	flags := flag.NewFlagSet("devkit-connect-proxy", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	socket := flags.String("socket", "", "absolute Unix listener path")
	allowlist := flags.String("allowlist", "", "absolute exact-host allowlist path")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *socket == "" || *allowlist == "" {
		return errors.New("usage: devkit-connect-proxy --socket ABSOLUTE_PATH --allowlist ABSOLUTE_PATH")
	}
	allow, err := loadAllowlist(*allowlist)
	if err != nil {
		return err
	}
	listener, cleanup, err := listenUnix(*socket)
	if err != nil {
		return err
	}
	defer cleanup()

	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	serviceContext, cancelService := context.WithCancel(context.Background())
	defer cancelService()
	service := &proxy{
		context: serviceContext,
		allow:   allow,
		lookup:  net.DefaultResolver.LookupIPAddr,
		dial: func(ctx context.Context, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", address)
		},
		permits: make(chan struct{}, maximumConnections),
		active:  make(map[net.Conn]struct{}),
	}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(interrupt)
	finished := make(chan error, 1)
	go func() { finished <- service.serve(listener) }()
	select {
	case err := <-finished:
		cancelService()
		service.closeActive()
		service.wg.Wait()
		return err
	case <-interrupt:
		cancelService()
		_ = listener.Close()
		err := <-finished
		service.closeActive()
		service.wg.Wait()
		return err
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		// This is deliberately content-free: no target, request, path, or payload
		// is included in lifecycle-visible logs.
		fmt.Fprintln(os.Stderr, "devkit-connect-proxy: startup or policy failure (class "+strconv.Itoa(classify(err))+")")
		os.Exit(1)
	}
}

func classify(err error) int {
	if err == nil {
		return 0
	}
	return 1
}

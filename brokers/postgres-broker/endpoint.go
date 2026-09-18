package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	endpointLeasePrefix   = "/_devkit/postgres/lease/"
	endpointConnectPrefix = "/_devkit/postgres/connect/"
	runHeader             = "X-Devkit-Run-ID"
	credentialHeader      = "X-Devkit-Endpoint-Credential"
	leaseHeader           = "X-Devkit-Postgres-Lease"
)

// handleEndpointLease is deliberately outside the Docker API namespace.  The
// only supported endpoint is a broker-managed Postgres 5432 lease; it is not a
// general TCP or SOCKS forwarding facility.
func (rc *requestContext) handleEndpointLease(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && strings.HasPrefix(path, endpointLeasePrefix):
		id := strings.TrimPrefix(path, endpointLeasePrefix)
		rec, err := rc.containers.issueLease(id, r.Header.Get(runHeader), r.Header.Get(credentialHeader))
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return true
		}
		if _, err := rc.postgresHostPort(rec.ID); err != nil {
			http.Error(w, "postgres endpoint unavailable", http.StatusConflict)
			return true
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"container_id": rec.ID, "lease": rec.Lease, "port": "5432"})
		return true
	case r.Method == http.MethodConnect && strings.HasPrefix(path, endpointConnectPrefix):
		id := strings.TrimPrefix(path, endpointConnectPrefix)
		rec, err := rc.containers.consumeLease(id, r.Header.Get(runHeader), r.Header.Get(credentialHeader), r.Header.Get(leaseHeader))
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return true
		}
		rc.connectPostgres(w, r, rec)
		return true
	default:
		return false
	}
}

func (rc *requestContext) postgresHostPort(containerID string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, rc.target.ResolveReference(&url.URL{Path: "/containers/" + containerID + "/json"}).String(), nil)
	if err != nil {
		return "", err
	}
	req.Host = "docker"
	resp, err := rc.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("container inspect status %d", resp.StatusCode)
	}
	var inspected struct {
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
		NetworkSettings struct {
			Ports map[string][]struct {
				HostPort string `json:"HostPort"`
			} `json:"Ports"`
		} `json:"NetworkSettings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&inspected); err != nil {
		return "", err
	}
	if !inspected.State.Running {
		return "", fmt.Errorf("container is not running")
	}
	bindings := inspected.NetworkSettings.Ports["5432/tcp"]
	if len(bindings) != 1 || strings.TrimSpace(bindings[0].HostPort) == "" {
		return "", fmt.Errorf("container does not expose exactly one postgres 5432 binding")
	}
	if _, err := strconv.ParseUint(bindings[0].HostPort, 10, 16); err != nil {
		return "", fmt.Errorf("invalid postgres host port")
	}
	return bindings[0].HostPort, nil
}

func (rc *requestContext) connectPostgres(w http.ResponseWriter, r *http.Request, rec containerRecord) {
	port, err := rc.postgresHostPort(rec.ID)
	if err != nil {
		http.Error(w, "postgres endpoint unavailable", http.StatusGone)
		return
	}
	dial := rc.dialPostgres
	if dial == nil {
		dial = net.Dial
	}
	target, err := dial("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		http.Error(w, "postgres endpoint unavailable", http.StatusBadGateway)
		return
	}
	defer target.Close()
	// Re-check after the potentially slow inspect/dial boundary. Deletion or a
	// same-name replacement invalidates the lease before any tunnel byte is
	// admitted.
	if _, err := rc.containers.consumeLease(rec.ID, rec.RunID, rec.Credential, rec.Lease); err != nil {
		http.Error(w, "postgres endpoint unavailable", http.StatusGone)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "broker cannot upgrade connection", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hj.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	var once sync.Once
	closeBoth := func() { once.Do(func() { _ = client.Close(); _ = target.Close() }) }
	unregister, ok := rc.containers.registerStream(rec, closeBoth)
	if !ok {
		closeBoth()
		return
	}
	defer unregister()
	if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err := buffered.Flush(); err != nil {
		return
	}
	go func() { _, _ = io.Copy(target, buffered); closeBoth() }()
	_, _ = io.Copy(client, target)
	closeBoth()
}

// endpointAgentProxy is a host-owned, per-sandbox capability.  It adds the
// non-exported credential to Docker requests and only publishes an opaque Unix
// stream endpoint into the sandbox.
type endpointAgentProxy struct {
	broker     string
	runID      string
	credential string
	client     *http.Client
	target     *url.URL
}

func runAgentProxy(listen, broker, runID string) error {
	credential, err := newOpaqueLease()
	if err != nil {
		return err
	}
	client, target, err := buildClient("unix://" + broker)
	if err != nil {
		return err
	}
	p := &endpointAgentProxy{broker: broker, runID: runID, credential: credential, client: client, target: target}
	if err := ensureSocketAbsent(listen); err != nil {
		return err
	}
	dockerListener, err := net.Listen("unix", listen)
	if err != nil {
		return err
	}
	if err := os.Chmod(listen, 0o600); err != nil {
		_ = dockerListener.Close()
		return err
	}
	return http.Serve(dockerListener, http.HandlerFunc(p.handleDocker))
}

func (p *endpointAgentProxy) handleDocker(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect && strings.HasPrefix(r.URL.Path, endpointConnectPrefix) {
		p.proxyConnect(w, r)
		return
	}
	clone := r.Clone(r.Context())
	clone.URL = p.target.ResolveReference(&url.URL{Path: r.URL.Path, RawQuery: r.URL.RawQuery})
	clone.Host, clone.RequestURI = "docker", ""
	clone.Header = r.Header.Clone()
	clone.Header.Set(runHeader, p.runID)
	clone.Header.Set(credentialHeader, p.credential)
	resp, err := p.client.Do(clone)
	if err != nil {
		http.Error(w, "broker unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "broker response", http.StatusBadGateway)
		return
	}
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

func (p *endpointAgentProxy) proxyConnect(w http.ResponseWriter, r *http.Request) {
	broker, err := net.Dial("unix", p.broker)
	if err != nil {
		http.Error(w, "broker unavailable", http.StatusBadGateway)
		return
	}
	defer broker.Close()
	request := "CONNECT " + r.URL.Path + " HTTP/1.1\r\nHost: docker\r\n" + runHeader + ": " + p.runID + "\r\n" + credentialHeader + ": " + p.credential + "\r\n" + leaseHeader + ": " + r.Header.Get(leaseHeader) + "\r\n\r\n"
	if _, err := io.WriteString(broker, request); err != nil {
		return
	}
	buffered := bufio.NewReader(broker)
	response, err := http.ReadResponse(buffered, nil)
	if err != nil || response.StatusCode != http.StatusOK {
		http.Error(w, "postgres connect rejected", http.StatusForbidden)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	client, clientBuffered, err := hj.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	if _, err := clientBuffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if clientBuffered.Flush() != nil {
		return
	}
	var once sync.Once
	closeBoth := func() { once.Do(func() { _ = client.Close(); _ = broker.Close() }) }
	go func() { _, _ = io.Copy(broker, clientBuffered); closeBoth() }()
	_, _ = io.Copy(client, buffered)
	closeBoth()
}

func runSandboxPostgresEndpoint(socket, containerID string) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	return runSandboxPostgresEndpointAt(socket, containerID, listener, os.Stdout)
}

func runSandboxPostgresEndpointAt(socket, containerID string, listener net.Listener, ready io.Writer) error {
	lease, err := sandboxLease(socket, containerID)
	if err != nil {
		_ = listener.Close()
		return err
	}
	defer listener.Close()
	if err := json.NewEncoder(ready).Encode(map[string]string{"host": "127.0.0.1", "port": strconv.Itoa(listener.Addr().(*net.TCPAddr).Port), "container_id": containerID}); err != nil {
		return err
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer conn.Close()
			upstream, buffered, err := sandboxConnect(socket, containerID, lease)
			if err != nil {
				return
			}
			defer upstream.Close()
			go io.Copy(upstream, conn)
			_, _ = io.Copy(conn, buffered)
		}()
	}
}

func sandboxLease(socket, id string) (string, error) {
	client, target, err := buildClient("unix://" + socket)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, target.ResolveReference(&url.URL{Path: endpointLeasePrefix + id}).String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("postgres lease rejected: %s", resp.Status)
	}
	var grant struct {
		Lease string `json:"lease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&grant); err != nil || grant.Lease == "" {
		return "", fmt.Errorf("invalid postgres lease")
	}
	return grant.Lease, nil
}

func sandboxConnect(socket, id, lease string) (net.Conn, *bufio.Reader, error) {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, nil, err
	}
	request := "CONNECT " + endpointConnectPrefix + id + " HTTP/1.1\r\nHost: docker\r\n" + leaseHeader + ": " + lease + "\r\n\r\n"
	if _, err := io.WriteString(conn, request); err != nil {
		conn.Close()
		return nil, nil, err
	}
	buffered := bufio.NewReader(conn)
	response, err := http.ReadResponse(buffered, nil)
	if err != nil || response.StatusCode != http.StatusOK {
		conn.Close()
		return nil, nil, fmt.Errorf("postgres connect rejected")
	}
	return conn, buffered, nil
}

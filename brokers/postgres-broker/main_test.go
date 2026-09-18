package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestEndpointAgentProxyStreamsDockerResponsesWithoutBuffering(t *testing.T) {
	readyFrames := [][]byte{
		dockerRawStreamFrame(1, []byte("database ready\n")),
		dockerRawStreamFrame(2, []byte("migration ready\n")),
	}
	releaseFollow := make(chan struct{})
	var releaseFollowOnce sync.Once
	releaseFollowStream := func() { releaseFollowOnce.Do(func() { close(releaseFollow) }) }
	upstreamCanceled := make(chan struct{})
	upstreamRequests := make(chan *http.Request, 8)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamRequests <- r.Clone(r.Context())
		switch r.URL.Path {
		case "/v1.52/containers/managed/logs":
			w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
			w.Header().Set("X-Upstream-Status", "follow")
			w.WriteHeader(http.StatusPartialContent)
			for _, frame := range readyFrames {
				writeSplitDockerRawStreamFrame(w, frame)
			}
			select {
			case <-releaseFollow:
			case <-r.Context().Done():
				return
			}
			_, _ = w.Write(dockerRawStreamFrame(1, []byte("complete\n")))
		case "/v1.52/containers/finite/logs":
			w.Header().Set("X-Upstream-Status", "finite")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("finite response\n"))
		case "/v1.52/containers/cancel/logs":
			w.WriteHeader(http.StatusOK)
			writeSplitDockerRawStreamFrame(w, dockerRawStreamFrame(1, []byte("cancel ready\n")))
			<-r.Context().Done()
			close(upstreamCanceled)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	// Run before httptest.Server.Close on failure, so a pre-release assertion
	// failure cannot leave the open follow handler blocking server teardown.
	defer releaseFollowStream()

	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	rc := &requestContext{
		policy:     mustPolicy(t, []string{"postgres:latest"}, true),
		client:     upstream.Client(),
		target:     target,
		containers: newContainerRegistry(),
	}
	for _, id := range []string{"managed", "finite", "cancel"} {
		rc.containers.add(id, id, "agent-run", "agent-credential")
	}

	tmp := t.TempDir()
	brokerSocket := filepath.Join(tmp, "broker.sock")
	serveEndpointProxy(t, brokerSocket, http.HandlerFunc(rc.handle))
	brokerClient, brokerTarget, err := buildClient("unix://" + brokerSocket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(brokerClient.CloseIdleConnections)
	agentSocket := filepath.Join(tmp, "agent.sock")
	agent := &endpointAgentProxy{
		broker:     brokerSocket,
		runID:      "agent-run",
		credential: "agent-credential",
		client:     brokerClient,
		target:     brokerTarget,
	}
	serveEndpointProxy(t, agentSocket, http.HandlerFunc(agent.handleDocker))
	agentClient, agentTarget, err := buildClient("unix://" + agentSocket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(agentClient.CloseIdleConnections)

	request := func(ctx context.Context, proxyTarget *url.URL, id string) *http.Request {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyTarget.ResolveReference(&url.URL{
			Path:     "/v1.52/containers/" + id + "/logs",
			RawQuery: "follow=1&stdout=1",
		}).String(), nil)
		if err != nil {
			t.Fatal(err)
		}
		// The per-agent proxy must overwrite caller-supplied capability values.
		req.Header.Set(runHeader, "hostile-run")
		req.Header.Set(credentialHeader, "hostile-credential")
		return req
	}
	assertForwarded := func(path string) {
		t.Helper()
		select {
		case got := <-upstreamRequests:
			if got.URL.Path != path || got.URL.RawQuery != "follow=1&stdout=1" {
				t.Fatalf("forwarded request = %s?%s", got.URL.Path, got.URL.RawQuery)
			}
			if got.Method != http.MethodGet || got.Header.Get(runHeader) != "agent-run" || got.Header.Get(credentialHeader) != "agent-credential" {
				t.Fatalf("forwarded request lost proxy authority/method: method=%s run=%q credential=%q", got.Method, got.Header.Get(runHeader), got.Header.Get(credentialHeader))
			}
		case <-time.After(time.Second):
			t.Fatalf("upstream did not receive %s", path)
		}
	}

	followCtx, followCancel := context.WithTimeout(context.Background(), time.Second)
	defer followCancel()
	follow, err := agentClient.Do(request(followCtx, agentTarget, "managed"))
	if err != nil {
		t.Fatal(err)
	}
	if follow.StatusCode != http.StatusPartialContent || follow.Header.Get("X-Upstream-Status") != "follow" {
		t.Fatalf("follow response = %s headers=%v", follow.Status, follow.Header)
	}
	for i, want := range [][]byte{[]byte("database ready\n"), []byte("migration ready\n")} {
		stream, gotReady, err := readDockerRawStreamFrame(follow.Body)
		if err != nil || stream != byte(i+1) || !bytes.Equal(gotReady, want) {
			t.Fatalf("follow frame %d = stream=%d payload=%q err=%v", i, stream, gotReady, err)
		}
	}
	assertForwarded("/v1.52/containers/managed/logs")
	releaseFollowStream()
	stream, complete, err := readDockerRawStreamFrame(follow.Body)
	if err != nil || stream != 1 || string(complete) != "complete\n" {
		t.Fatalf("follow completion = stream=%d payload=%q err=%v", stream, complete, err)
	}
	_ = follow.Body.Close()

	finite, err := agentClient.Do(request(context.Background(), agentTarget, "finite"))
	if err != nil {
		t.Fatal(err)
	}
	finiteBody, finiteErr := io.ReadAll(finite.Body)
	_ = finite.Body.Close()
	if finiteErr != nil || finite.StatusCode != http.StatusCreated || finite.Header.Get("X-Upstream-Status") != "finite" || string(finiteBody) != "finite response\n" {
		t.Fatalf("finite response = status=%s headers=%v body=%q err=%v", finite.Status, finite.Header, finiteBody, finiteErr)
	}
	assertForwarded("/v1.52/containers/finite/logs")

	unknown, err := agentClient.Do(request(context.Background(), agentTarget, "unknown"))
	if err != nil {
		t.Fatal(err)
	}
	_ = unknown.Body.Close()
	if unknown.StatusCode != http.StatusForbidden {
		t.Fatalf("unknown response status = %d", unknown.StatusCode)
	}
	wrongSocket := filepath.Join(tmp, "wrong-agent.sock")
	wrongAgent := &endpointAgentProxy{
		broker:     brokerSocket,
		runID:      "agent-run",
		credential: "wrong-credential",
		client:     brokerClient,
		target:     brokerTarget,
	}
	serveEndpointProxy(t, wrongSocket, http.HandlerFunc(wrongAgent.handleDocker))
	wrongClient, wrongTarget, err := buildClient("unix://" + wrongSocket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(wrongClient.CloseIdleConnections)
	unauthorized, err := wrongClient.Do(request(context.Background(), wrongTarget, "managed"))
	if err != nil {
		t.Fatal(err)
	}
	_ = unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusForbidden {
		t.Fatalf("unauthorized response status = %d", unauthorized.StatusCode)
	}
	select {
	case got := <-upstreamRequests:
		t.Fatalf("denied request reached upstream: %s", got.URL.Path)
	default:
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	canceled, err := agentClient.Do(request(cancelCtx, agentTarget, "cancel"))
	if err != nil {
		t.Fatal(err)
	}
	stream, gotReady, err := readDockerRawStreamFrame(canceled.Body)
	if err != nil || stream != 1 || string(gotReady) != "cancel ready\n" {
		t.Fatalf("cancel readiness = stream=%d payload=%q err=%v", stream, gotReady, err)
	}
	assertForwarded("/v1.52/containers/cancel/logs")
	cancel()
	_ = canceled.Body.Close()
	select {
	case <-upstreamCanceled:
	case <-time.After(time.Second):
		t.Fatal("upstream follow request survived client cancellation")
	}
}

func dockerRawStreamFrame(stream byte, payload []byte) []byte {
	frame := make([]byte, 8+len(payload))
	frame[0] = stream
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(payload)))
	copy(frame[8:], payload)
	return frame
}

func writeSplitDockerRawStreamFrame(w http.ResponseWriter, frame []byte) {
	flusher := w.(http.Flusher)
	for _, chunk := range [][]byte{frame[:3], frame[3:8], frame[8:10], frame[10:]} {
		if len(chunk) == 0 {
			continue
		}
		_, _ = w.Write(chunk)
		flusher.Flush()
	}
}

func readDockerRawStreamFrame(r io.Reader) (byte, []byte, error) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}
	if header[1] != 0 || header[2] != 0 || header[3] != 0 {
		return 0, nil, fmt.Errorf("invalid Docker raw-stream header %v", header[:4])
	}
	payload := make([]byte, binary.BigEndian.Uint32(header[4:]))
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return header[0], payload, nil
}

func serveEndpointProxy(t *testing.T, socket string, handler http.Handler) {
	t.Helper()
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})
}

func TestSandboxPostgresEndpointsAreConcurrentAndContainerBound(t *testing.T) {
	echoA, closeA := endpointEcho(t, "A:")
	defer closeA()
	echoB, closeB := endpointEcho(t, "B:")
	defer closeB()
	ports := map[string]string{"postgres-a": echoA, "postgres-b": echoB}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/containers/"), "/json")
		port := ports[id]
		if port == "" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"State":{"Running":true},"NetworkSettings":{"Ports":{"5432/tcp":[{"HostPort":"` + port + `"}]}}}`))
	}))
	defer upstream.Close()
	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	rc := &requestContext{client: upstream.Client(), target: target, containers: newContainerRegistry()}
	rc.containers.add("postgres-a", "same-name-a", "run-a", "credential-a")
	rc.containers.add("postgres-b", "same-name-b", "run-a", "credential-a")

	tmp := t.TempDir()
	brokerSocket := filepath.Join(tmp, "broker.sock")
	brokerListener, err := net.Listen("unix", brokerSocket)
	if err != nil {
		t.Fatal(err)
	}
	defer brokerListener.Close()
	go http.Serve(brokerListener, http.HandlerFunc(rc.handle))
	agentSocket := filepath.Join(tmp, "agent.sock")
	agentListener, err := net.Listen("unix", agentSocket)
	if err != nil {
		t.Fatal(err)
	}
	proxyClient, proxyTarget, err := buildClient("unix://" + brokerSocket)
	if err != nil {
		t.Fatal(err)
	}
	proxy := &endpointAgentProxy{broker: brokerSocket, runID: "run-a", credential: "credential-a", client: proxyClient, target: proxyTarget}
	go http.Serve(agentListener, http.HandlerFunc(proxy.handleDocker))

	endpointA, closeHelperA := startSandboxEndpoint(t, agentSocket, "postgres-a")
	defer closeHelperA()
	endpointB, closeHelperB := startSandboxEndpoint(t, agentSocket, "postgres-b")
	defer closeHelperB()
	if endpointA == endpointB {
		t.Fatalf("helpers reused endpoint %s", endpointA)
	}
	if got := endpointRoundTrip(t, endpointA, "one"); got != "A:one" {
		t.Fatalf("A target = %q", got)
	}
	if got := endpointRoundTrip(t, endpointB, "two"); got != "B:two" {
		t.Fatalf("B target = %q", got)
	}

	// A delete plus same-name replacement invalidates A's old helper/lease but
	// cannot disturb B's independent helper.
	rc.containers.remove("postgres-a")
	rc.containers.add("postgres-a-replacement", "same-name-a", "run-b", "credential-b")
	if got, err := endpointRoundTripResult(endpointA, "stale"); err == nil && got != "" {
		t.Fatalf("stale A helper reached a target: %q", got)
	}
	if got := endpointRoundTrip(t, endpointB, "still-b"); got != "B:still-b" {
		t.Fatalf("B after A replacement = %q", got)
	}

	// The credential is injected only by the still-running agent proxy. A
	// hostile caller at the real broker endpoint cannot substitute it.
	req, err := http.NewRequest(http.MethodPost, "http://docker"+endpointLeasePrefix+"postgres-b", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(runHeader, "run-a")
	req.Header.Set(credentialHeader, "hostile")
	resp, err := proxyClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("hostile credential status = %d", resp.StatusCode)
	}
	_ = agentListener.Close()
	if _, err := sandboxLease(agentSocket, "postgres-b"); err == nil {
		t.Fatal("agent-proxy exit retained endpoint access")
	}
}

func TestSandboxPostgresEndpointBoundsSilentAgentProxyBeforeReady(t *testing.T) {
	tmp := t.TempDir()
	socket := filepath.Join(tmp, "silent-agent.sock")
	agent, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	accepted := make(chan struct{})
	release := make(chan struct{})
	go func() {
		conn, acceptErr := agent.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		close(accepted)
		<-release
	}()
	defer close(release)

	endpoint, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var ready bytes.Buffer
	started := time.Now()
	err = runSandboxPostgresEndpointAtWithin(socket, "managed-postgres", endpoint, &ready, 50*time.Millisecond)
	if !errors.Is(err, ErrSandboxEndpointLeaseTimeout) {
		t.Fatalf("silent endpoint error = %v, want lease timeout", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("silent endpoint lease was not bounded: %s", elapsed)
	}
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("silent agent proxy did not receive the bounded lease request")
	}
	if ready.Len() != 0 {
		t.Fatalf("helper published readiness before lease admission: %q", ready.String())
	}
	if _, acceptErr := endpoint.Accept(); acceptErr == nil {
		t.Fatal("endpoint listener survived failed lease admission")
	}
}

func endpointEcho(t *testing.T, prefix string) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				b := make([]byte, 256)
				n, _ := conn.Read(b)
				_, _ = conn.Write(append([]byte(prefix), b[:n]...))
			}()
		}
	}()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port), func() { _ = listener.Close(); <-done }
}

func startSandboxEndpoint(t *testing.T, socket, id string) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- runSandboxPostgresEndpointAt(socket, id, listener, writer); _ = writer.Close() }()
	var ready map[string]string
	if err := json.NewDecoder(reader).Decode(&ready); err != nil {
		t.Fatal(err)
	}
	endpoint := net.JoinHostPort(ready["host"], ready["port"])
	return endpoint, func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("sandbox endpoint did not stop")
		}
	}
}

func endpointRoundTrip(t *testing.T, endpoint, message string) string {
	t.Helper()
	got, err := endpointRoundTripResult(endpoint, message)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func endpointRoundTripResult(endpoint, message string) (string, error) {
	conn, err := net.DialTimeout("tcp", endpoint, time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := io.WriteString(conn, message); err != nil {
		return "", err
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
	}
	got, err := io.ReadAll(conn)
	return string(got), err
}

func TestEndpointLeaseRejectsCrossSandboxAndStaleContainer(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/json") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"State":{"Running":true},"NetworkSettings":{"Ports":{"5432/tcp":[{"HostPort":"15432"}]}}}`))
	}))
	defer upstream.Close()
	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	rc := &requestContext{client: upstream.Client(), target: target, containers: newContainerRegistry()}
	rc.containers.add("postgres-a", "postgres-a", "agent-a", "credential-a")
	server := httptest.NewServer(http.HandlerFunc(rc.handle))
	defer server.Close()

	lease := func(run, credential, id string) *http.Response {
		req, err := http.NewRequest(http.MethodPost, server.URL+endpointLeasePrefix+id, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(runHeader, run)
		req.Header.Set(credentialHeader, credential)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	foreign := lease("agent-b", "credential-b", "postgres-a")
	if foreign.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-sandbox lease status = %d", foreign.StatusCode)
	}
	_ = foreign.Body.Close()
	for _, id := range []string{"", "post", "postgres"} {
		resp := lease("agent-a", "credential-a", id)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("non-exact endpoint id %q status = %d", id, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
	owned := lease("agent-a", "credential-a", "postgres-a")
	if owned.StatusCode != http.StatusOK {
		t.Fatalf("owner lease status = %d", owned.StatusCode)
	}
	var grant map[string]string
	if err := json.NewDecoder(owned.Body).Decode(&grant); err != nil {
		t.Fatal(err)
	}
	_ = owned.Body.Close()
	if grant["lease"] == "" || grant["port"] != "5432" {
		t.Fatalf("invalid lease grant: %#v", grant)
	}

	// Deletion invalidates even a previously issued opaque capability; a
	// replacement with the same visible name receives different custody.
	rc.containers.remove("postgres-a")
	rc.containers.add("postgres-a", "postgres-a", "agent-b", "credential-b")
	stale, err := http.NewRequest(http.MethodConnect, server.URL+endpointConnectPrefix+"postgres-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	stale.Header.Set(runHeader, "agent-a")
	stale.Header.Set(credentialHeader, "credential-a")
	stale.Header.Set(leaseHeader, grant["lease"])
	staleResp, err := server.Client().Do(stale)
	if err != nil {
		t.Fatal(err)
	}
	defer staleResp.Body.Close()
	if staleResp.StatusCode != http.StatusForbidden {
		t.Fatalf("stale replacement lease status = %d", staleResp.StatusCode)
	}
}

func TestDeleteDrainsRegisteredEndpointStreamBeforeUpstreamReturns(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"State":{"Running":true},"NetworkSettings":{"Ports":{"5432/tcp":[{"HostPort":"15432"}]}}}`))
		case http.MethodDelete:
			close(started)
			<-release
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	rc := &requestContext{client: upstream.Client(), target: target, containers: newContainerRegistry()}
	rc.containers.add("full-id", "alias", "run", "credential")
	rec, err := rc.containers.issueLease("full-id", "run", "credential")
	if err != nil {
		t.Fatal(err)
	}
	upstreamConn, peer := net.Pipe()
	defer peer.Close()
	rc.dialPostgres = func(_, _ string) (net.Conn, error) { return upstreamConn, nil }
	server := httptest.NewServer(http.HandlerFunc(rc.handle))
	defer server.Close()
	client, reader, response := rawEndpointConnect(t, server.URL, "full-id", "run", "credential", rec.Lease)
	defer client.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d", response.StatusCode)
	}

	done := make(chan struct{})
	go func() {
		request, err := http.NewRequest(http.MethodDelete, server.URL+"/containers/alias", nil)
		if err != nil {
			t.Errorf("delete request: %v", err)
			close(done)
			return
		}
		request.Header.Set(runHeader, "run")
		request.Header.Set(credentialHeader, "credential")
		response, err := server.Client().Do(request)
		if err != nil {
			t.Errorf("delete: %v", err)
		} else {
			_ = response.Body.Close()
		}
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("delete did not reach upstream")
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	_, err = reader.Read(make([]byte, 1))
	if !closedEndpointStream(err) {
		t.Fatalf("open endpoint stream survived delete admission: %v", err)
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	_, err = peer.Read(make([]byte, 1))
	if !closedEndpointStream(err) {
		t.Fatalf("upstream stream survived delete admission: %v", err)
	}
	select {
	case <-done:
		t.Fatal("delete returned before upstream release")
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("delete did not finish")
	}
}

func TestDialBarrierDeleteReplacementRejectsBeforeTunnelRegistration(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"State":{"Running":true},"NetworkSettings":{"Ports":{"5432/tcp":[{"HostPort":"1"}]}}}`))
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	rc := &requestContext{client: upstream.Client(), target: target, containers: newContainerRegistry()}
	rc.containers.add("full-id", "alias", "run", "credential")
	rec, err := rc.containers.issueLease("full-id", "run", "credential")
	if err != nil {
		t.Fatal(err)
	}
	rc.dialPostgres = func(_, _ string) (net.Conn, error) {
		close(entered)
		<-release
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			buf := make([]byte, 1)
			if n, _ := b.Read(buf); n > 0 {
				delivered <- struct{}{}
			}
		}()
		return a, nil
	}
	server := httptest.NewServer(http.HandlerFunc(rc.handle))
	defer server.Close()
	// The second validation after dial is the race boundary: deletion and a
	// same-name replacement happen while the real CONNECT is held, so no tunnel
	// can register or deliver a byte to the replacement endpoint.
	result := make(chan struct {
		response *http.Response
		err      error
	}, 1)
	go func() {
		client, _, response, err := rawEndpointConnectResult(server.URL, "full-id", "run", "credential", rec.Lease)
		if client != nil {
			defer client.Close()
		}
		result <- struct {
			response *http.Response
			err      error
		}{response, err}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("CONNECT did not reach dial barrier")
	}
	rc.containers.remove("alias")
	rc.containers.add("replacement", "alias", "other", "other")
	close(release)
	select {
	case got := <-result:
		if got.err == nil && got.response != nil && got.response.StatusCode == http.StatusOK {
			t.Fatal("replacement race admitted a tunnel")
		}
	case <-time.After(time.Second):
		t.Fatal("CONNECT did not reject after replacement")
	}
	select {
	case <-delivered:
		t.Fatal("replacement received tunnel payload")
	case <-time.After(50 * time.Millisecond):
	}
}

func closedEndpointStream(err error) bool {
	if err == nil {
		return false
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		return false
	}
	return errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.ECONNRESET)
}

func rawEndpointConnect(t *testing.T, serverURL, id, runID, credential, lease string) (net.Conn, *bufio.Reader, *http.Response) {
	t.Helper()
	conn, reader, response, err := rawEndpointConnectResult(serverURL, id, runID, credential, lease)
	if err != nil {
		t.Fatal(err)
	}
	return conn, reader, response
}

func rawEndpointConnectResult(serverURL, id, runID, credential, lease string) (net.Conn, *bufio.Reader, *http.Response, error) {
	endpoint, err := url.Parse(serverURL)
	if err != nil {
		return nil, nil, nil, err
	}
	conn, err := net.Dial("tcp", endpoint.Host)
	if err != nil {
		return nil, nil, nil, err
	}
	request, err := http.NewRequest(http.MethodConnect, "http://"+endpoint.Host+endpointConnectPrefix+id, nil)
	if err != nil {
		_ = conn.Close()
		return nil, nil, nil, err
	}
	request.Header.Set(runHeader, runID)
	request.Header.Set(credentialHeader, credential)
	request.Header.Set(leaseHeader, lease)
	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n%s: %s\r\n%s: %s\r\n%s: %s\r\n\r\n", request.URL.RequestURI(), endpoint.Host, runHeader, runID, credentialHeader, credential, leaseHeader, lease); err != nil {
		_ = conn.Close()
		return nil, nil, nil, err
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		_ = conn.Close()
		return nil, nil, nil, err
	}
	return conn, reader, response, nil
}

func TestStripVersionPrefix(t *testing.T) {
	cases := map[string]string{
		"/_ping":                    "/_ping",
		"/v1.41/_ping":              "/_ping",
		"/v999.1/containers/create": "/containers/create",
	}

	for input, expected := range cases {
		if got := stripVersionPrefix(input); got != expected {
			t.Fatalf("expected %s got %s", expected, got)
		}
	}
}

func TestShouldProxyStreamingRequest_OnlyContainerAttach(t *testing.T) {
	attachReq, err := http.NewRequest(http.MethodPost, "http://unix/v1.52/containers/abc123/attach?stream=1&stdout=1&stderr=1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !shouldProxyStreamingRequest(attachReq) {
		t.Fatal("expected container attach to use streaming proxy")
	}

	startReq, err := http.NewRequest(http.MethodPost, "http://unix/v1.52/containers/abc123/start", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shouldProxyStreamingRequest(startReq) {
		t.Fatal("expected container start to use normal forwarding path")
	}

	logsReq, err := http.NewRequest(http.MethodGet, "http://unix/v1.52/containers/abc123/logs?follow=1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shouldProxyStreamingRequest(logsReq) {
		t.Fatal("expected container logs to use normal forwarding path")
	}
}

func TestAuthorizeContainerCreate_AllowsWhitelistedImage(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, true)}

	body := []byte(`{"Image":"postgres:latest","HostConfig":{}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeContainerCreate_BlocksOtherImages(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, true)}

	body := []byte(`{"Image":"redis:latest","HostConfig":{}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected error but got nil")
	}
}

func TestAuthorizeContainerCreate_AllowsMinioPorts(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"minio/minio:latest"}, true)}
	body := []byte(`{"Image":"minio/minio:latest","HostConfig":{"PortBindings":{"9000/tcp":[{"HostPort":""}],"9001/tcp":[{"HostPort":""}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeContainerCreate_AllowsRyukPort(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"testcontainers/ryuk:0.7.0"}, true)}
	body := []byte(`{"Image":"testcontainers/ryuk:0.7.0","HostConfig":{"PortBindings":{"8080/tcp":[{"HostPort":""}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeContainerCreate_AllowsRyukPrivilegedMode(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"testcontainers/ryuk:0.7.0"}, true)}
	body := []byte(`{"Image":"testcontainers/ryuk:0.7.0","HostConfig":{"Privileged":true,"PortBindings":{"8080/tcp":[{"HostPort":""}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeContainerCreate_AllowsRyukBrokerSocketBind(t *testing.T) {
	rc := &requestContext{
		policy:     mustPolicy(t, []string{"testcontainers/ryuk:0.7.0"}, true),
		brokerSock: "/workspaces/dev/.devkit/native-broker/broker.sock",
	}
	body := []byte(`{"Image":"testcontainers/ryuk:0.7.0","HostConfig":{"Privileged":true,"Binds":["/workspaces/dev/.devkit/native-broker/broker.sock:/var/run/docker.sock:rw"],"PortBindings":{"8080/tcp":[{"HostPort":""}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeContainerCreate_AllowsRyukSandboxBrokerSocketAlias(t *testing.T) {
	rc := &requestContext{
		policy:            mustPolicy(t, []string{"testcontainers/ryuk:0.7.0"}, true),
		brokerSock:        "/home/bayesartre/dev/.devkit/native-broker/broker.sock",
		brokerSockAliases: []string{"/workspaces/dev/.devkit/native-broker/broker.sock"},
	}
	body := []byte(`{"Image":"testcontainers/ryuk:0.7.0","HostConfig":{"Privileged":true,"Binds":["/workspaces/dev/.devkit/native-broker/broker.sock:/var/run/docker.sock:rw"],"PortBindings":{"8080/tcp":[{"HostPort":""}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeContainerCreate_BlocksPrivilegedModeForOtherImages(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, true)}
	body := []byte(`{"Image":"postgres:latest","HostConfig":{"Privileged":true}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block for privileged non-Ryuk container")
	}
}

func TestAuthorizeContainerCreate_BlocksRawDockerSocketBindForRyuk(t *testing.T) {
	rc := &requestContext{
		policy:     mustPolicy(t, []string{"testcontainers/ryuk:0.7.0"}, true),
		brokerSock: "/workspaces/dev/.devkit/native-broker/broker.sock",
	}
	body := []byte(`{"Image":"testcontainers/ryuk:0.7.0","HostConfig":{"Privileged":true,"Binds":["/var/run/docker.sock:/var/run/docker.sock:rw"],"PortBindings":{"8080/tcp":[{"HostPort":""}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block for raw Docker socket bind")
	}
}

func TestAuthorizeContainerCreate_BlocksExtraRyukBind(t *testing.T) {
	rc := &requestContext{
		policy:     mustPolicy(t, []string{"testcontainers/ryuk:0.7.0"}, true),
		brokerSock: "/workspaces/dev/.devkit/native-broker/broker.sock",
	}
	body := []byte(`{"Image":"testcontainers/ryuk:0.7.0","HostConfig":{"Privileged":true,"Binds":["/workspaces/dev/.devkit/native-broker/broker.sock:/var/run/docker.sock:rw","/tmp:/tmp:rw"],"PortBindings":{"8080/tcp":[{"HostPort":""}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block for extra Ryuk bind")
	}
}

func TestAuthorizeContainerCreate_BlocksBrokerSocketBindForOtherImages(t *testing.T) {
	rc := &requestContext{
		policy:     mustPolicy(t, []string{"postgres:latest"}, true),
		brokerSock: "/workspaces/dev/.devkit/native-broker/broker.sock",
	}
	body := []byte(`{"Image":"postgres:latest","HostConfig":{"Binds":["/workspaces/dev/.devkit/native-broker/broker.sock:/var/run/docker.sock:rw"]}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block for non-Ryuk broker socket bind")
	}
}

func TestAuthorizeContainerCreate_AllowsSAMPythonBuildBinds(t *testing.T) {
	image := "public.ecr.aws/sam/build-python3.12@sha256:e1c008f2626266024c538c9c31cbeb122e9c1c42e1a3ccd4e7b1380f6e9b7497"
	rc := &requestContext{policy: mustPolicy(t, []string{image}, true)}
	body := []byte(`{"Image":"` + image + `","HostConfig":{"Binds":["/workspaces/dev/agent-worktrees/agent2/ouroboros-terraform-track3-rotation-encryption-prep-20260711/modules/db_credentials_rotation/lambda_artifact:/src:ro","/workspaces/dev/agent-worktrees/agent2/ouroboros-terraform-track3-rotation-encryption-prep-20260711/.local/db_credentials_rotation_lambda:/out"]}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeContainerCreate_BlocksSAMPythonWritableSource(t *testing.T) {
	image := "public.ecr.aws/sam/build-python3.12@sha256:e1c008f2626266024c538c9c31cbeb122e9c1c42e1a3ccd4e7b1380f6e9b7497"
	rc := &requestContext{policy: mustPolicy(t, []string{image}, true)}
	body := []byte(`{"Image":"` + image + `","HostConfig":{"Binds":["/workspaces/dev/agent-worktrees/agent2/ouroboros-terraform-track3-rotation-encryption-prep-20260711/modules/db_credentials_rotation/lambda_artifact:/src:rw","/workspaces/dev/agent-worktrees/agent2/ouroboros-terraform-track3-rotation-encryption-prep-20260711/.local/db_credentials_rotation_lambda:/out"]}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block for writable source bind")
	}
}

func TestAuthorizeContainerCreate_BlocksSAMPythonOutputOutsideRepo(t *testing.T) {
	image := "public.ecr.aws/sam/build-python3.12@sha256:e1c008f2626266024c538c9c31cbeb122e9c1c42e1a3ccd4e7b1380f6e9b7497"
	rc := &requestContext{policy: mustPolicy(t, []string{image}, true)}
	body := []byte(`{"Image":"` + image + `","HostConfig":{"Binds":["/workspaces/dev/agent-worktrees/agent2/ouroboros-terraform-track3-rotation-encryption-prep-20260711/modules/db_credentials_rotation/lambda_artifact:/src:ro","/tmp/db_credentials_rotation_lambda:/out"]}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block for output outside repo")
	}
}

func TestAuthorizeContainerCreate_BlocksSAMPythonExtraBind(t *testing.T) {
	image := "public.ecr.aws/sam/build-python3.12@sha256:e1c008f2626266024c538c9c31cbeb122e9c1c42e1a3ccd4e7b1380f6e9b7497"
	rc := &requestContext{policy: mustPolicy(t, []string{image}, true)}
	body := []byte(`{"Image":"` + image + `","HostConfig":{"Binds":["/workspaces/dev/agent-worktrees/agent2/ouroboros-terraform-track3-rotation-encryption-prep-20260711/modules/db_credentials_rotation/lambda_artifact:/src:ro","/workspaces/dev/agent-worktrees/agent2/ouroboros-terraform-track3-rotation-encryption-prep-20260711/.local/db_credentials_rotation_lambda:/out","/tmp:/tmp:rw"]}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block for extra SAM bind")
	}
}

func TestAuthorizeContainerCreate_BlocksRyukHostPortMismatch(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"testcontainers/ryuk:0.7.0"}, true)}
	body := []byte(`{"Image":"testcontainers/ryuk:0.7.0","HostConfig":{"PortBindings":{"8080/tcp":[{"HostPort":"18080"}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block when host port is forced")
	}
}

func TestAuthorizeContainerCreate_BlocksMinioHostPortOverride(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"minio/minio:latest"}, true)}
	body := []byte(`{"Image":"minio/minio:latest","HostConfig":{"PortBindings":{"9000/tcp":[{"HostPort":"1234"}],"9001/tcp":[{"HostPort":""}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block when host port is forced")
	}
}

func TestAuthorizeAllowsNetworkList(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, true)}
	req, err := http.NewRequest(http.MethodGet, "http://unix/networks", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorize(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeAllowsNetworkInspectByID(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, true)}
	req, err := http.NewRequest(http.MethodGet, "http://unix/networks/04209c657e69", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorize(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeImageCreate_AllowsWhitelistedPull(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, true)}

	req, _ := http.NewRequest(http.MethodPost, "http://unix/images/create", nil)
	req.URL = &url.URL{RawQuery: "fromImage=postgres&tag=latest"}

	if err := rc.authorizeImageCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeImageCreate_AllowsRyukPull(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"testcontainers/ryuk:0.7.0"}, true)}

	req, _ := http.NewRequest(http.MethodPost, "http://unix/images/create", nil)
	req.URL = &url.URL{RawQuery: "fromImage=testcontainers/ryuk&tag=0.7.0"}

	if err := rc.authorizeImageCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeImageCreate_BlocksWhenDisabled(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, false)}

	req, _ := http.NewRequest(http.MethodPost, "http://unix/images/create", nil)
	req.URL = &url.URL{RawQuery: "fromImage=postgres&tag=latest"}

	if err := rc.authorizeImageCreate(req); err == nil {
		t.Fatal("expected block")
	}
}

func TestBuildClient_Unix(t *testing.T) {
	client, target, err := buildClient("unix:///var/run/docker.sock")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil || target.String() != "http://docker" {
		t.Fatalf("unexpected target: %v", target)
	}
}

func TestBuildClient_TCP(t *testing.T) {
	client, target, err := buildClient("tcp://docker:2375")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil || target.String() != "http://docker:2375" {
		t.Fatalf("unexpected target: %v", target)
	}
}

func TestPolicy_AllowsMultipleImages(t *testing.T) {
	p := mustPolicy(t, []string{"postgres:latest", "minio/minio:latest"}, true)
	if !p.matchesImage("minio/minio:latest") {
		t.Fatal("expected minio image to be allowed")
	}
	if !p.matchesNameAndTag("minio/minio", "latest") {
		t.Fatal("expected name/tag match for minio")
	}
	if p.matchesImage("redis:latest") {
		t.Fatal("expected redis to be blocked")
	}
}

func TestLoadConfig_ExplicitAllowedImagesDoNotAppendLegacyDefault(t *testing.T) {
	t.Setenv("BROKER_ALLOWED_IMAGES", "minio/minio:latest")
	unsetEnv(t, "BROKER_ALLOWED_IMAGE")
	unsetEnv(t, "BROKER_ALLOWED_TAG")

	cfg := loadConfig()
	if len(cfg.AllowedImages) != 1 || cfg.AllowedImages[0] != "minio/minio:latest" {
		t.Fatalf("allowed images = %#v", cfg.AllowedImages)
	}
}

func TestLoadConfig_AppendsExplicitLegacyImage(t *testing.T) {
	t.Setenv("BROKER_ALLOWED_IMAGES", "minio/minio:latest")
	t.Setenv("BROKER_ALLOWED_IMAGE", "postgres")
	t.Setenv("BROKER_ALLOWED_TAG", "15")

	cfg := loadConfig()
	if got, want := strings.Join(cfg.AllowedImages, ","), "minio/minio:latest,postgres:15,postgres"; got != want {
		t.Fatalf("allowed images = %q, want %q", got, want)
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, ok := os.LookupEnv(key)
	_ = os.Unsetenv(key)
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func TestValidatePortBindings_MinioAllowed(t *testing.T) {
	bindings := map[string][]portBinding{
		"9000/tcp": []portBinding{{HostPort: ""}},
		"9001/tcp": []portBinding{{HostPort: ""}},
	}
	if err := validatePortBindings(bindings, []string{"9000/tcp", "9001/tcp"}); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestValidatePortBindings_RejectsUnexpectedPort(t *testing.T) {
	bindings := map[string][]portBinding{
		"9000/tcp": []portBinding{{HostPort: ""}},
		"1234/tcp": []portBinding{{HostPort: ""}},
	}
	if err := validatePortBindings(bindings, []string{"9000/tcp", "9001/tcp"}); err == nil {
		t.Fatal("expected block for unexpected port")
	}
}

func mustPolicy(t *testing.T, images []string, allowPull bool) *policy {
	t.Helper()
	p, err := newPolicy(images, allowPull)
	if err != nil {
		t.Fatalf("newPolicy failed: %v", err)
	}
	return p
}

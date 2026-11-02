package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNativePostgresCreateSatisfiesStrictSingleBindingLease(t *testing.T) {
	var mu sync.Mutex
	var created map[string]json.RawMessage
	var bindings []portBinding
	started := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch stripVersionPrefix(r.URL.Path) {
		case "/containers/create":
			body, err := io.ReadAll(r.Body)
			if err != nil || int64(len(body)) != r.ContentLength {
				t.Errorf("invalid forwarded body length: length=%d header=%d err=%v", len(body), r.ContentLength, err)
			}
			if err := json.Unmarshal(body, &created); err != nil {
				t.Error(err)
			}
			var payload containerCreateRequest
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Error(err)
			}
			requested := payload.HostConfig.PortBindings["5432/tcp"]
			if len(requested) != 1 {
				t.Errorf("requested binding count = %d", len(requested))
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			// Model the actual owned Docker observation: an unspecified host
			// address publishes both families. Explicit loopback publishes one.
			if requested[0].HostIP == "" {
				bindings = []portBinding{{HostIP: "0.0.0.0", HostPort: "15432"}, {HostIP: "::", HostPort: "15432"}}
			} else {
				bindings = []portBinding{{HostIP: requested[0].HostIP, HostPort: "15432"}}
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprint(w, `{"Id":"native-postgres"}`)
		case "/containers/native-postgres/start":
			started = true
			w.WriteHeader(http.StatusNoContent)
		case "/containers/native-postgres/json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"State":           map[string]bool{"Running": started},
				"NetworkSettings": map[string]any{"Ports": map[string][]portBinding{"5432/tcp": bindings}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, true), client: upstream.Client(), target: target, containers: newContainerRegistry()}
	brokerSocket := filepath.Join(t.TempDir(), "broker.sock")
	serveEndpointProxy(t, brokerSocket, http.HandlerFunc(rc.handle))
	brokerClient, brokerTarget, err := buildClient("unix://" + brokerSocket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(brokerClient.CloseIdleConnections)
	proxy := &endpointAgentProxy{broker: brokerSocket, runID: "native-run", credential: "native-test-capability", client: brokerClient, target: brokerTarget}
	agentSocket := filepath.Join(t.TempDir(), "agent.sock")
	serveEndpointProxy(t, agentSocket, http.HandlerFunc(proxy.handleDocker))
	client, agentTarget, err := buildClient("unix://" + agentSocket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	do := func(path, body string, want int) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, agentTarget.ResolveReference(&url.URL{Path: path}).String(), strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != want {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s: status=%s body=%q", path, resp.Status, b)
		}
	}
	do("/v1.52/containers/create", `{"Image":"postgres:latest","Labels":{"fixture":"preserved"},"ExposedPorts":{"5432/tcp":{}},"HostConfig":{"Memory":9007199254740993,"LogConfig":{"Type":"json-file"},"PortBindings":{"5432/tcp":[{"HostIp":"","HostPort":"","Extra":"preserved"}]}}}`, http.StatusCreated)
	do("/v1.52/containers/native-postgres/start", "", http.StatusNoContent)
	if lease, err := sandboxLeaseWithin(agentSocket, "native-postgres", time.Second); err != nil || lease == "" {
		t.Fatalf("normal native create/start did not satisfy strict single-binding lease: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bindings) != 1 || bindings[0].HostIP != "127.0.0.1" {
		t.Fatalf("constructed bindings = %#v", bindings)
	}
	var host map[string]json.RawMessage
	if err := json.Unmarshal(created["HostConfig"], &host); err != nil {
		t.Fatal(err)
	}
	var ports map[string][]map[string]json.RawMessage
	if err := json.Unmarshal(host["PortBindings"], &ports); err != nil {
		t.Fatal(err)
	}
	if string(created["Labels"]) != `{"fixture":"preserved"}` || string(host["Memory"]) != "9007199254740993" || string(host["LogConfig"]) != `{"Type":"json-file"}` || string(ports["5432/tcp"][0]["Extra"]) != `"preserved"` {
		t.Fatalf("construction lost unrelated JSON values: create=%#v host=%#v ports=%#v", created, host, ports)
	}
}

func TestNativePostgresCreatePinningScopeAndSafety(t *testing.T) {
	for _, tt := range []struct {
		name, ip, run, credential, extra string
		wantIP                           string
		wantRefusal                      bool
	}{
		{name: "native-default", run: "run", credential: "capability", wantIP: "127.0.0.1"},
		{name: "legacy-host"},
		{name: "missing-run", credential: "capability"},
		{name: "missing-capability", run: "run"},
		{name: "explicit-ipv4", ip: "0.0.0.0", run: "run", credential: "capability", wantIP: "0.0.0.0"},
		{name: "explicit-ipv6", ip: "::", run: "run", credential: "capability", wantIP: "::"},
		{name: "caller-loopback-still-refused", ip: "127.0.0.1", run: "run", credential: "capability", wantRefusal: true},
		{name: "caller-foreign-address-still-refused", ip: "192.0.2.1", run: "run", credential: "capability", wantRefusal: true},
		{name: "privileged-still-refused", run: "run", credential: "capability", extra: `,"Privileged":true`, wantRefusal: true},
		{name: "publish-all-still-refused", run: "run", credential: "capability", extra: `,"PublishAllPorts":true`, wantRefusal: true},
		{name: "bind-still-refused", run: "run", credential: "capability", extra: `,"Binds":["/host:/container"]`, wantRefusal: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"Image":"postgres:latest","HostConfig":{"PortBindings":{"5432/tcp":[{"HostIp":%q,"HostPort":""}]}%s}}`, tt.ip, tt.extra)
			req := httptest.NewRequest(http.MethodPost, "/v1.52/containers/create", strings.NewReader(body))
			req.Header.Set(runHeader, tt.run)
			req.Header.Set(credentialHeader, tt.credential)
			rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, true)}
			err := rc.authorizeContainerCreate(req)
			if (err != nil) != tt.wantRefusal {
				t.Fatalf("authorization error=%v, wantRefusal=%v", err, tt.wantRefusal)
			}
			got, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantRefusal {
				if string(got) != body {
					t.Fatal("refused request was rewritten")
				}
				return
			}
			var parsed containerCreateRequest
			if err := json.Unmarshal(got, &parsed); err != nil {
				t.Fatal(err)
			}
			if actual := parsed.HostConfig.PortBindings["5432/tcp"][0].HostIP; actual != tt.wantIP {
				t.Fatalf("forwarded HostIp=%q, want %q", actual, tt.wantIP)
			}
			if tt.name != "native-default" && string(got) != body {
				t.Fatal("out-of-scope explicit/legacy request was rewritten")
			}
		})
	}
}

func TestNativePostgresCreateRefusesAmbiguousJSONBeforeReordering(t *testing.T) {
	for name, body := range map[string]string{
		"image-alias":           `{"image":"redis:latest","Image":"postgres:latest","HostConfig":{"PortBindings":{"5432/tcp":[{"HostIp":""}]}}}`,
		"host-config-alias":     `{"Image":"postgres:latest","hostconfig":{"Privileged":true},"HostConfig":{"Privileged":false,"PortBindings":{"5432/tcp":[{"HostIp":""}]}}}`,
		"duplicate-host-config": `{"Image":"postgres:latest","HostConfig":{"Memory":1},"HostConfig":{"PortBindings":{"5432/tcp":[{"HostIp":""}]}}}`,
		"binding-alias":         `{"Image":"postgres:latest","HostConfig":{"PortBindings":{"5432/tcp":[{"hostip":"192.0.2.1","HostIp":""}]}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/containers/create", strings.NewReader(body))
			req.Header.Set(runHeader, "native-run")
			req.Header.Set(credentialHeader, "native-capability")
			rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, true)}
			if err := rc.authorizeContainerCreate(req); err == nil {
				t.Fatal("ambiguous request could be reordered into different Docker semantics")
			}
			got, err := io.ReadAll(req.Body)
			if err != nil || string(got) != body {
				t.Fatalf("ambiguous request was changed: %q, %v", got, err)
			}
		})
	}
}

func TestNativePostgresCreateSupportsUnambiguousFieldSpellingAndOmittedHostIP(t *testing.T) {
	for _, body := range []string{
		`{"Image":"postgres:latest","HostConfig":{"PortBindings":{"5432/tcp":[{"HostPort":""}]}}}`,
		`{"image":"postgres:latest","hostconfig":{"portbindings":{"5432/tcp":[{"hostip":"","hostport":""}]}}}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/containers/create", strings.NewReader(body))
		req.Header.Set(runHeader, "native-run")
		req.Header.Set(credentialHeader, "native-capability")
		rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, true)}
		if err := rc.authorizeContainerCreate(req); err != nil {
			t.Fatal(err)
		}
		var payload containerCreateRequest
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if got := payload.HostConfig.PortBindings["5432/tcp"]; len(got) != 1 || got[0].HostIP != "127.0.0.1" {
			t.Fatalf("constructed binding = %#v", got)
		}
	}
}

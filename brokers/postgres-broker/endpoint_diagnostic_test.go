package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSandboxLeaseReportsBoundedInspectionPredicate(t *testing.T) {
	for _, tt := range []struct {
		name, body, diagnostic string
		status                 int
	}{
		{"inspect-status", "upstream-private-value", "inspect-status:503", 503},
		{"inspect-json", "invalid-json-private-value", "inspect-json", 200},
		{"not-running", `{"State":{"Running":false}}`, "container-not-running", 200},
		{"missing-binding", `{"State":{"Running":true}}`, "postgres-bindings:0", 200},
		{"multiple-bindings", `{"State":{"Running":true},"NetworkSettings":{"Ports":{"5432/tcp":[{"HostPort":"15432"},{"HostPort":"15432"}]}}}`, "postgres-bindings:2", 200},
		{"missing-port", `{"State":{"Running":true},"NetworkSettings":{"Ports":{"5432/tcp":[{"HostPort":""}]}}}`, "postgres-port-missing", 200},
		{"invalid-port", `{"State":{"Running":true},"NetworkSettings":{"Ports":{"5432/tcp":[{"HostPort":"private-value"}]}}}`, "postgres-port-invalid", 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = fmt.Fprint(w, tt.body)
			}))
			defer upstream.Close()
			target, err := url.Parse(upstream.URL)
			if err != nil {
				t.Fatal(err)
			}
			rc := &requestContext{client: upstream.Client(), target: target, containers: newContainerRegistry()}
			rc.containers.add("postgres-a", "postgres-a", "run-a", "credential-private-value")
			brokerSocket := filepath.Join(t.TempDir(), "broker.sock")
			serveEndpointProxy(t, brokerSocket, http.HandlerFunc(rc.handle))
			client, proxyTarget, err := buildClient("unix://" + brokerSocket)
			if err != nil {
				t.Fatal(err)
			}
			proxy := &endpointAgentProxy{broker: brokerSocket, runID: "run-a", credential: "credential-private-value", client: client, target: proxyTarget}
			agentSocket := filepath.Join(t.TempDir(), "agent.sock")
			serveEndpointProxy(t, agentSocket, http.HandlerFunc(proxy.handleDocker))
			grant, err := sandboxLeaseWithin(agentSocket, "postgres-a", time.Second)
			if err == nil || grant != "" {
				t.Fatalf("failed inspection granted a lease: %q, %v", grant, err)
			}
			if !strings.Contains(err.Error(), "409 Conflict ["+tt.diagnostic+"]") {
				t.Fatalf("missing bounded predicate %q: %v", tt.diagnostic, err)
			}
			if strings.Contains(err.Error(), "private-value") {
				t.Fatalf("private upstream data or credential returned: %v", err)
			}
		})
	}
}

func TestSandboxLeaseRejectsUnrecognizedOrExcessiveFailureDetails(t *testing.T) {
	for _, tt := range []struct{ name, body, diagnostic string }{
		{"known", "postgres endpoint unavailable [postgres-bindings:2]\n", "postgres-bindings:2"},
		{"private", "private-value", ""},
		{"injected", "postgres endpoint unavailable [inspect-json]\nprivate-value", ""},
		{"invalid-status", "postgres endpoint unavailable [inspect-status:999]", ""},
		{"excessive-count", "postgres endpoint unavailable [postgres-bindings:999999]", ""},
		{"excessive-body", "postgres endpoint unavailable [inspect-json]" + strings.Repeat(" ", 512), ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			socket := filepath.Join(t.TempDir(), "agent.sock")
			serveEndpointProxy(t, socket, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusConflict)
				_, _ = fmt.Fprint(w, tt.body)
			}))
			grant, err := sandboxLeaseWithin(socket, "postgres-a", time.Second)
			if err == nil || grant != "" || len(err.Error()) > 128 || strings.Contains(err.Error(), "private-value") {
				t.Fatalf("invalid rejection result: grant=%q, err=%v", grant, err)
			}
			expected := "postgres lease rejected: 409 Conflict"
			if tt.diagnostic != "" {
				expected += " [" + tt.diagnostic + "]"
			}
			if err.Error() != expected {
				t.Fatalf("error=%q, want %q", err, expected)
			}
		})
	}
}

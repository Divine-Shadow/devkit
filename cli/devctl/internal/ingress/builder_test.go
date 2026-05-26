package ingress

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/config"
)

func TestBuildFragmentFromConfigFile(t *testing.T) {
	dir := t.TempDir()
	overlay := filepath.Join(dir, "overlays", "proj")
	if err := os.MkdirAll(filepath.Join(overlay, "infra"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(overlay, "infra", "Caddyfile")
	if err := os.WriteFile(cfgPath, []byte(":443 {\n}"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.IngressConfig{
		Kind:   "caddy",
		Config: "infra/Caddyfile",
		Certs: []config.IngressCert{
			{Path: "/tmp/cert.pem"},
		},
	}
	frag, err := BuildFragment("proj", cfg, overlay, dir)
	if err != nil {
		t.Fatalf("BuildFragment error: %v", err)
	}
	if frag.Path == "" {
		t.Fatalf("missing config path")
	}
	if frag.Path != cfgPath {
		t.Fatalf("config path mismatch: got %q want %q", frag.Path, cfgPath)
	}
}

func TestBuildFragmentGeneratesConfigFromRoutes(t *testing.T) {
	dir := t.TempDir()
	project := "proj-routes"
	cfg := &config.IngressConfig{
		Kind: "caddy",
		Routes: []config.IngressRoute{
			{Host: "ouroboros.test", Service: "frontend", Port: 4173},
			{Host: "ouroboros-7.test", Service: "dev-agent@7", Port: 5173},
		},
	}
	frag, err := BuildFragment(project, cfg, "", dir)
	if err != nil {
		t.Fatalf("BuildFragment error: %v", err)
	}
	tmpDir := filepath.Join(os.TempDir(), "devkit-ingress", sanitize(project))
	genFile := filepath.Join(tmpDir, "Caddyfile.generated")
	if _, err := os.Stat(genFile); err != nil {
		t.Fatalf("expected generated config at %s: %v", genFile, err)
	}
	content, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	if !strings.Contains(string(content), "ouroboros.test") {
		t.Fatalf("generated config missing host: %s", string(content))
	}
	if !strings.Contains(string(content), "agent-7:5173") {
		t.Fatalf("generated config missing agent service: %s", string(content))
	}
	if frag.Path == "" {
		t.Fatalf("missing fragment path")
	}
}

func TestBuildFragmentGroupsPathRoutesByHost(t *testing.T) {
	dir := t.TempDir()
	project := "proj-path-routes"
	cfg := &config.IngressConfig{
		Kind: "caddy",
		Routes: []config.IngressRoute{
			{Host: "ouroboros-1.test", Service: "dev-agent@1", Port: 5173},
			{Host: "ouroboros-1.test", Path: "/_governance/*", Service: "dev-agent@1", Port: 7778},
		},
	}
	if _, err := BuildFragment(project, cfg, "", dir); err != nil {
		t.Fatalf("BuildFragment error: %v", err)
	}
	genFile := filepath.Join(os.TempDir(), "devkit-ingress", sanitize(project), "Caddyfile.generated")
	content, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	text := string(content)
	if strings.Count(text, "ouroboros-1.test {") != 1 {
		t.Fatalf("expected one grouped site block: %s", text)
	}
	if !strings.Contains(text, "handle_path /_governance/*") || !strings.Contains(text, "agent-1:7778") {
		t.Fatalf("generated config missing path route: %s", text)
	}
	if !strings.Contains(text, "handle {") || !strings.Contains(text, "agent-1:5173") {
		t.Fatalf("generated config missing fallback route: %s", text)
	}
}

func TestBuildFragmentSupportsExplicitHTTPRouteWithoutTLS(t *testing.T) {
	dir := t.TempDir()
	project := "proj-http-route"
	cfg := &config.IngressConfig{
		Kind: "caddy",
		Routes: []config.IngressRoute{
			{Host: "http://static-1.localhost", Service: "127.0.0.1", Port: 8001},
		},
	}
	if _, err := BuildFragment(project, cfg, "", dir); err != nil {
		t.Fatalf("BuildFragment error: %v", err)
	}
	genFile := filepath.Join(os.TempDir(), "devkit-ingress", sanitize(project), "Caddyfile.generated")
	content, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "http://static-1.localhost {") {
		t.Fatalf("generated config missing HTTP site address: %s", text)
	}
	if strings.Contains(text, "tls internal") {
		t.Fatalf("generated HTTP route should not emit TLS directive: %s", text)
	}
	if !strings.Contains(text, "127.0.0.1:8001") {
		t.Fatalf("generated config missing loopback route: %s", text)
	}
}

func TestBuildFragmentRejectsDirectoryCertPath(t *testing.T) {
	dir := t.TempDir()
	certDir := filepath.Join(dir, "ouroboros.test.pem")
	keyPath := filepath.Join(dir, "ouroboros.test-key.pem")
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("key"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.IngressConfig{
		Kind: "caddy",
		Routes: []config.IngressRoute{
			{Host: "ouroboros.test", Service: "frontend", Port: 4173, Cert: certDir, Key: keyPath},
		},
	}
	_, err := BuildFragment("proj-dir-cert", cfg, "", dir)
	if err == nil {
		t.Fatal("expected directory cert path error")
	}
	if !strings.Contains(err.Error(), "is a directory, expected a file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

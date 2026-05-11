package ingress

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"devkit/cli/devctl/internal/agentexec"
	"devkit/cli/devctl/internal/config"
)

const (
	defaultIngressImage = "caddy:2.7.6-alpine"
	defaultPortMapping  = "${DEVKIT_INGRESS_PORT:-8443}:443"
)

type renderedRoute struct {
	host     string
	path     string
	service  string
	port     int
	certPath string
	keyPath  string
}

// Fragment represents a generated docker compose fragment that wires an ingress service.
type Fragment struct {
	Path string
}

// BuildFragment renders the compose fragment for the provided ingress configuration.
// When cfg is nil, it returns an empty Fragment and no error.
func BuildFragment(project string, cfg *config.IngressConfig, overlayDir string, root string) (Fragment, error) {
	var out Fragment
	if cfg == nil {
		return out, nil
	}
	kind := strings.TrimSpace(strings.ToLower(cfg.Kind))
	if kind == "" || kind == "caddy" {
		return buildCaddyFragment(project, cfg, overlayDir, root)
	}
	return out, fmt.Errorf("ingress: unsupported kind %q", cfg.Kind)
}

func buildCaddyFragment(project string, cfg *config.IngressConfig, overlayDir string, root string) (Fragment, error) {
	var out Fragment
	tmpDir := filepath.Join(os.TempDir(), "devkit-ingress", sanitize(project))
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return out, err
	}
	mounted := map[string]string{}
	var volumes []string
	certTargetDir := "/ingress/certs"
	if strings.TrimSpace(cfg.Config) != "" {
		certTargetDir = "/ingress"
	}
	fallbackCert := ""
	fallbackKey := ""
	for idx, cert := range cfg.Certs {
		dest, err := mountCertFile(cert.Path, overlayDir, root, certTargetDir, mounted, &volumes)
		if err != nil {
			return out, err
		}
		if dest == "" {
			continue
		}
		if idx == 0 {
			fallbackCert = dest
		} else if idx == 1 {
			fallbackKey = dest
		}
	}
	configSrc, err := ensureConfigFile(project, tmpDir, cfg, overlayDir, root, mounted, &volumes, fallbackCert, fallbackKey)
	if err != nil {
		return out, err
	}
	volumes = append(volumes, fmt.Sprintf("%s:/ingress/Caddyfile:ro", configSrc))
	composePath := filepath.Join(tmpDir, "compose.ingress.yml")
	if err := writeCompose(composePath, volumes, cfg.Env); err != nil {
		return out, err
	}
	out.Path = composePath
	return out, nil
}

func writeCompose(path string, volumes []string, extraEnv map[string]string) error {
	var b strings.Builder
	b.WriteString("services:\n")
	b.WriteString("  ingress:\n")
	b.WriteString(fmt.Sprintf("    image: %s\n", defaultIngressImage))
	b.WriteString("    command: [\"caddy\", \"run\", \"--config\", \"/ingress/Caddyfile\"]\n")
	b.WriteString("    working_dir: /ingress\n")
	if len(volumes) > 0 {
		b.WriteString("    volumes:\n")
		for _, vol := range volumes {
			b.WriteString(fmt.Sprintf("      - %s\n", vol))
		}
	}
	if len(extraEnv) > 0 {
		b.WriteString("    environment:\n")
		keys := make([]string, 0, len(extraEnv))
		for k := range extraEnv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("      - %s=%s\n", k, extraEnv[k]))
		}
	}
	b.WriteString("    networks:\n")
	b.WriteString("      - dev-internal\n")
	b.WriteString("      - dev-egress\n")
	b.WriteString("    ports:\n")
	b.WriteString(fmt.Sprintf("      - \"%s\"\n", defaultPortMapping))
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return nil
}

func ensureConfigFile(project string, tmpDir string, cfg *config.IngressConfig, overlayDir, root string, mounted map[string]string, volumes *[]string, fallbackCert, fallbackKey string) (string, error) {
	if strings.TrimSpace(cfg.Config) != "" {
		return resolvePath(cfg.Config, overlayDir, root), nil
	}
	if len(cfg.Routes) == 0 {
		return "", fmt.Errorf("ingress: config path or routes required")
	}
	dest := filepath.Join(tmpDir, "Caddyfile.generated")
	routes := make([]renderedRoute, 0, len(cfg.Routes))
	for _, route := range cfg.Routes {
		host := strings.TrimSpace(route.Host)
		path := strings.TrimSpace(route.Path)
		svc := resolveServiceName(strings.TrimSpace(route.Service), project)
		if host == "" || svc == "" || route.Port <= 0 {
			return "", fmt.Errorf("ingress: invalid route %+v", route)
		}
		if path != "" && !strings.HasPrefix(path, "/") {
			return "", fmt.Errorf("ingress: route %s path must start with /", route.Host)
		}
		certPath, keyPath, err := resolveRouteCerts(route, overlayDir, root, mounted, volumes, fallbackCert, fallbackKey)
		if err != nil {
			return "", err
		}
		routes = append(routes, renderedRoute{
			host:     host,
			path:     path,
			service:  svc,
			port:     route.Port,
			certPath: certPath,
			keyPath:  keyPath,
		})
	}
	caddyfile, err := renderCaddyfile(routes)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, []byte(caddyfile), 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

func renderCaddyfile(routes []renderedRoute) (string, error) {
	type site struct {
		routes []renderedRoute
	}
	sites := map[string]*site{}
	order := make([]string, 0, len(routes))
	for _, route := range routes {
		if _, ok := sites[route.host]; !ok {
			sites[route.host] = &site{}
			order = append(order, route.host)
		}
		sites[route.host].routes = append(sites[route.host].routes, route)
	}

	var b strings.Builder
	for _, host := range order {
		siteRoutes := sites[host].routes
		if len(siteRoutes) == 1 && siteRoutes[0].path == "" {
			writeSimpleSite(&b, siteRoutes[0])
			continue
		}
		if err := writeRoutedSite(&b, host, siteRoutes); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

func writeSimpleSite(b *strings.Builder, route renderedRoute) {
	b.WriteString(fmt.Sprintf("%s {\n", route.host))
	writeTLS(b, route.certPath, route.keyPath)
	b.WriteString(fmt.Sprintf("  reverse_proxy %s:%d\n", route.service, route.port))
	b.WriteString("}\n\n")
}

func writeRoutedSite(b *strings.Builder, host string, routes []renderedRoute) error {
	certPath := routes[0].certPath
	keyPath := routes[0].keyPath
	for _, route := range routes[1:] {
		if route.certPath != certPath || route.keyPath != keyPath {
			return fmt.Errorf("ingress: routes for host %s must use the same cert/key", host)
		}
	}

	fallbackCount := 0
	b.WriteString(fmt.Sprintf("%s {\n", host))
	writeTLS(b, certPath, keyPath)
	for _, route := range routes {
		if route.path == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("  handle_path %s {\n", route.path))
		b.WriteString(fmt.Sprintf("    reverse_proxy %s:%d\n", route.service, route.port))
		b.WriteString("  }\n")
	}
	for _, route := range routes {
		if route.path != "" {
			continue
		}
		fallbackCount++
		if fallbackCount > 1 {
			return fmt.Errorf("ingress: multiple fallback routes for host %s", host)
		}
		b.WriteString("  handle {\n")
		b.WriteString(fmt.Sprintf("    reverse_proxy %s:%d\n", route.service, route.port))
		b.WriteString("  }\n")
	}
	b.WriteString("}\n\n")
	return nil
}

func writeTLS(b *strings.Builder, certPath string, keyPath string) {
	if certPath != "" && keyPath != "" {
		b.WriteString(fmt.Sprintf("  tls %s %s\n", certPath, keyPath))
	} else {
		b.WriteString("  tls internal\n")
	}
}

func resolveServiceName(service string, project string) string {
	if service == "" {
		return service
	}
	composeProject := strings.TrimSpace(agentexec.ComposeProjectName(project))
	if strings.Contains(service, "{project}") && composeProject != "" {
		return strings.ReplaceAll(service, "{project}", composeProject)
	}
	const agentPrefix = "dev-agent@"
	if strings.HasPrefix(service, agentPrefix) && composeProject != "" {
		index := strings.TrimSpace(strings.TrimPrefix(service, agentPrefix))
		if index != "" {
			return fmt.Sprintf("%s-dev-agent-%s", composeProject, index)
		}
	}
	return service
}

func resolveRouteCerts(route config.IngressRoute, overlayDir, root string, mounted map[string]string, volumes *[]string, fallbackCert, fallbackKey string) (string, string, error) {
	certRaw := strings.TrimSpace(route.Cert)
	keyRaw := strings.TrimSpace(route.Key)
	if certRaw != "" || keyRaw != "" {
		if certRaw == "" || keyRaw == "" {
			return "", "", fmt.Errorf("ingress: route %s missing cert or key path", route.Host)
		}
		certPath, err := mountCertFile(certRaw, overlayDir, root, "/ingress/certs", mounted, volumes)
		if err != nil {
			return "", "", err
		}
		keyPath, err := mountCertFile(keyRaw, overlayDir, root, "/ingress/certs", mounted, volumes)
		if err != nil {
			return "", "", err
		}
		if certPath == "" || keyPath == "" {
			return "", "", fmt.Errorf("ingress: route %s cert/key not found", route.Host)
		}
		return certPath, keyPath, nil
	}
	if fallbackCert != "" && fallbackKey != "" {
		return fallbackCert, fallbackKey, nil
	}
	return "", "", nil
}

func mountCertFile(raw string, overlayDir, root string, targetDir string, mounted map[string]string, volumes *[]string) (string, error) {
	resolved := resolvePath(raw, overlayDir, root)
	if resolved == "" {
		return "", nil
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("ingress: cert path %s not accessible: %w", resolved, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("ingress: cert path %s is a directory, expected a file", resolved)
	}
	if dest, ok := mounted[resolved]; ok {
		return dest, nil
	}
	base := filepath.Base(resolved)
	target := filepath.Join(targetDir, base)
	*volumes = append(*volumes, fmt.Sprintf("%s:%s:ro", resolved, target))
	mounted[resolved] = target
	return target, nil
}

func resolvePath(p string, overlayDir, root string) string {
	raw := strings.TrimSpace(p)
	if raw == "" {
		return ""
	}
	if filepath.IsAbs(raw) {
		return raw
	}
	base := strings.TrimSpace(overlayDir)
	if base == "" {
		base = root
	}
	return filepath.Clean(filepath.Join(base, raw))
}

func sanitize(project string) string {
	if strings.TrimSpace(project) == "" {
		project = "default"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return '_'
		}
	}, project)
}

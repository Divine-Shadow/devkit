package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"devkit/cli/devctl/internal/runtime/egressproxy"
)

const usage = "usage: devkit-source-transport serve --socket ABSOLUTE_PATH --allowlist ABSOLUTE_PATH\n" +
	"       devkit-source-transport connect --socket ABSOLUTE_PATH --target HOST:PORT"

// managedConnectBridgeURL is the only network effect available to the source
// transport server. It is projected by the workspace-egress sandbox and is
// deliberately not configurable through argv or environment.
const managedConnectBridgeURL = "http://127.0.0.1:18888"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "devkit-source-transport: %v\n", err)
		os.Exit(2)
	}
}

func run(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", usage)
	}
	switch args[0] {
	case "serve":
		return runServe(ctx, args[1:])
	case "connect":
		return runConnect(ctx, args[1:], input, output)
	default:
		return fmt.Errorf("unknown command %q\n%s", args[0], usage)
	}
}

func runServe(ctx context.Context, args []string) error {
	options, err := parseOptions(args, map[string]bool{
		"--socket":    true,
		"--allowlist": true,
	})
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	socketPath, err := absolutePath(options["--socket"], "proxy socket")
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	allowlistPath, err := absolutePath(options["--allowlist"], "allowlist")
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return egressproxy.Serve(ctx, sourceTransportServerConfig(socketPath, allowlistPath))
}

func sourceTransportServerConfig(socketPath, allowlistPath string) egressproxy.Config {
	return egressproxy.Config{
		SocketPath:       socketPath,
		AllowlistPath:    allowlistPath,
		UpstreamProxyURL: managedConnectBridgeURL,
	}
}

func runConnect(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	options, err := parseOptions(args, map[string]bool{
		"--socket": true,
		"--target": true,
	})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	socketPath, err := absolutePath(options["--socket"], "proxy socket")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	target, err := connectTarget(options["--target"])
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	return egressproxy.Connect(ctx, socketPath, target, input, output)
}

func parseOptions(args []string, expected map[string]bool) (map[string]string, error) {
	values := make(map[string]string, len(expected))
	for index := 0; index < len(args); index++ {
		name := args[index]
		_, allowed := expected[name]
		if !allowed {
			return nil, fmt.Errorf("unknown option %q", name)
		}
		if _, duplicate := values[name]; duplicate {
			return nil, fmt.Errorf("duplicate option %q", name)
		}
		if index+1 >= len(args) {
			return nil, fmt.Errorf("%s requires a value", name)
		}
		value := strings.TrimSpace(args[index+1])
		if value == "" || strings.HasPrefix(value, "--") {
			return nil, fmt.Errorf("%s requires a value", name)
		}
		values[name] = value
		index++
	}
	for name, required := range expected {
		if required && values[name] == "" {
			return nil, fmt.Errorf("%s is required", name)
		}
	}
	return values, nil
}

func absolutePath(value string, label string) (string, error) {
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("%s path must be absolute: %s", label, value)
	}
	return filepath.Clean(value), nil
}

func connectTarget(value string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil || strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" {
		return "", fmt.Errorf("CONNECT target must be HOST:PORT: %s", value)
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return "", fmt.Errorf("CONNECT target port must be between 1 and 65535: %s", value)
	}
	return net.JoinHostPort(host, port), nil
}

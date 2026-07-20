package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestRunRefusesUnknownCommand(t *testing.T) {
	err := run(context.Background(), []string{"acquire"}, strings.NewReader(""), io.Discard)
	if err == nil || !strings.Contains(err.Error(), `unknown command "acquire"`) {
		t.Fatalf("run error = %v, want unknown-command refusal", err)
	}
}

func TestRunServeRequiresExplicitAbsoluteTransportInputs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing-socket",
			args: []string{"serve", "--allowlist", "/nix/store/allowlist"},
			want: "--socket is required",
		},
		{
			name: "relative-socket",
			args: []string{"serve", "--socket", "proxy.sock", "--allowlist", "/nix/store/allowlist"},
			want: "proxy socket path must be absolute",
		},
		{
			name: "relative-allowlist",
			args: []string{"serve", "--socket", "/run/proxy.sock", "--allowlist", "allowlist"},
			want: "allowlist path must be absolute",
		},
		{
			name: "duplicate-socket",
			args: []string{"serve", "--socket", "/run/a.sock", "--socket", "/run/b.sock", "--allowlist", "/nix/store/allowlist"},
			want: `duplicate option "--socket"`,
		},
		{
			name: "ambient-option",
			args: []string{"serve", "--socket", "/run/proxy.sock", "--allowlist", "/nix/store/allowlist", "--runtime-manifest", "/etc/fleet/authority.json"},
			want: `unknown option "--runtime-manifest"`,
		},
		{
			name: "source-option",
			args: []string{"serve", "--socket", "/run/proxy.sock", "--allowlist", "/nix/store/allowlist", "--source", "/tmp/source"},
			want: `unknown option "--source"`,
		},
		{
			name: "revision-option",
			args: []string{"serve", "--socket", "/run/proxy.sock", "--allowlist", "/nix/store/allowlist", "--revision", "1111111111111111111111111111111111111111"},
			want: `unknown option "--revision"`,
		},
		{
			name: "alternate-upstream-option",
			args: []string{"serve", "--socket", "/run/proxy.sock", "--allowlist", "/nix/store/allowlist", "--upstream-proxy", "http://127.0.0.1:18888"},
			want: `unknown option "--upstream-proxy"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := run(context.Background(), test.args, strings.NewReader(""), io.Discard)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("run error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRunConnectRequiresExplicitAbsoluteSocketAndHostPort(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing-target",
			args: []string{"connect", "--socket", "/run/proxy.sock"},
			want: "--target is required",
		},
		{
			name: "relative-socket",
			args: []string{"connect", "--socket", "proxy.sock", "--target", "ssh.github.com:443"},
			want: "proxy socket path must be absolute",
		},
		{
			name: "missing-port",
			args: []string{"connect", "--socket", "/run/proxy.sock", "--target", "ssh.github.com"},
			want: "CONNECT target must be HOST:PORT",
		},
		{
			name: "invalid-port",
			args: []string{"connect", "--socket", "/run/proxy.sock", "--target", "ssh.github.com:0"},
			want: "CONNECT target port must be between 1 and 65535",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := run(context.Background(), test.args, strings.NewReader(""), io.Discard)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("run error = %v, want %q", err, test.want)
			}
		})
	}
}

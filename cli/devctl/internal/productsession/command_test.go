package productsession

import (
	"reflect"
	"testing"
)

func TestParseOriginalCommandNormalizesLeadingConfigurationBeforeMatching(t *testing.T) {
	tests := []struct {
		original   string
		kind       Kind
		normalized []string
	}{
		{PrepareToken, KindPrepare, []string{PrepareToken}},
		{"codex --version", KindVersion, []string{"codex", "--version"}},
		{"codex app-server --listen unix://", KindListen, []string{"codex", "app-server", "managed-listen"}},
		{"codex -c features.code_mode_host=true app-server --listen unix://", KindListen, []string{"codex", "-c", "features.code_mode_host=true", "app-server", "managed-listen"}},
		{"codex app-server proxy", KindProxy, []string{"codex", "app-server", "managed-proxy"}},
		{"codex -c features.code_mode_host=true app-server proxy", KindProxy, []string{"codex", "-c", "features.code_mode_host=true", "app-server", "managed-proxy"}},
	}
	for _, test := range tests {
		t.Run(test.original, func(t *testing.T) {
			command, err := ParseOriginalCommand(test.original)
			if err != nil {
				t.Fatal(err)
			}
			if command.SchemaVersion != CommandSchema || command.Kind != test.kind || len(command.OriginalSHA) != 64 || !reflect.DeepEqual(command.Normalized, test.normalized) {
				t.Fatalf("unexpected command %#v", command)
			}
		})
	}
}

func TestParseOriginalCommandRejectsBypassesAndLookalikes(t *testing.T) {
	invalid := []string{
		"",
		" codex app-server proxy",
		"codex  app-server proxy",
		"/nix/store/codex app-server proxy",
		"bash -lc codex app-server proxy",
		"codex -c features.code_mode_host=false app-server proxy",
		"codex -c features.code_mode_host=true -c features.code_mode_host=true app-server proxy",
		"codex app-server -c features.code_mode_host=true proxy",
		"codex -c=features.code_mode_host=true app-server proxy",
		"codex -q app-server proxy",
		"codex app-server --listen",
		"codex app-server --listen unix:///tmp/caller.sock",
		"codex app-server --listen unix:// app-server",
		"codex app-server proxy --sock /tmp/caller.sock",
		"codex app-server proxy;sh",
		"codex 'app-server' proxy",
		"devkit-product-prepare/v1 extra",
	}
	for _, original := range invalid {
		t.Run(original, func(t *testing.T) {
			if command, err := ParseOriginalCommand(original); err == nil {
				t.Fatalf("accepted bypass %#v", command)
			}
		})
	}
}

package launch

import (
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/productsourceacquire"
)

func TestSourceAcquisitionProfileSharesNetworkOnlyForExactZeroArgumentWorkload(t *testing.T) {
	manifest := productsourceacquire.Manifest{
		ExecutablePath: "/nix/store/acquirer/bin/devkit-product-source-acquire",
		Product: productsourceacquire.ProductBinding{
			LifecycleRoot: "/var/lib/devkit-product-lifecycle/run-1",
			IdentityPath:  "/run/credentials/devkit-product-git-identity",
		},
	}
	command := buildProductSourceAcquisitionBubblewrap("/nix/store/bwrap/bin/bwrap", manifest)
	joined := strings.Join(command.Args, "\x00")
	for _, required := range []string{
		"--unshare-all", "--share-net", "--clearenv",
		"--ro-bind\x00/nix/store\x00/nix/store",
		"--bind\x00/var/lib/devkit-product-lifecycle\x00/var/lib/devkit-product-lifecycle",
		"--ro-bind\x00/run/credentials/devkit-product-git-identity\x00/run/credentials/devkit-product-git-identity",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("source acquisition profile missing %q: %#v", required, command.Args)
		}
	}
	for _, forbidden := range []string{"--unshare-net", "127.0.0.1:18888", "--socket", "--allowlist", "--proxy"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("source acquisition profile contains caller/runtime override %q: %#v", forbidden, command.Args)
		}
	}
	if got := command.Args[len(command.Args)-1]; got != manifest.ExecutablePath {
		t.Fatalf("final workload = %q, want %q", got, manifest.ExecutablePath)
	}
	if command.Dir != filepath.Dir(manifest.Product.LifecycleRoot) {
		t.Fatalf("command dir = %q", command.Dir)
	}
}

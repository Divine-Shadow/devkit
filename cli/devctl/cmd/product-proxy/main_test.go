package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"devkit/cli/devctl/internal/productadapter"
)

func TestConsumeSupervisorCapabilityIsProtectedStrictAndOneShot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "capability.json")
	capability := productadapter.SupervisorCapability{
		SchemaVersion:        productadapter.SupervisorCapabilitySchema,
		Operation:            string(productadapter.CommandExec),
		AdapterExecutable:    "/nix/store/adapter/bin/product-adapter",
		BubblewrapExecutable: "/nix/store/bwrap/bin/bwrap",
		ManifestDigest:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Count:                2,
		Index:                1,
		ProxySocketPath:      productadapter.SandboxProxySocketPath,
		RuntimeArgs:          []string{"/nix/store/runtime/bin/runtime", "true"},
	}
	payload, err := json.Marshal(capability)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := consumeSupervisorCapability(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != capability.SchemaVersion || got.ManifestDigest != capability.ManifestDigest {
		t.Fatalf("consumed capability = %+v", got)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("one-shot capability remains after consumption: %v", err)
	}
	if _, err := consumeSupervisorCapability(path); err == nil {
		t.Fatal("consumed capability was reusable")
	}
}

func TestConsumeSupervisorCapabilityRefusesUnsafeFilesAndUnknownFields(t *testing.T) {
	t.Run("mode", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "capability.json")
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := consumeSupervisorCapability(path); err == nil {
			t.Fatal("world-readable capability was accepted")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "target.json")
		path := filepath.Join(root, "capability.json")
		if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if _, err := consumeSupervisorCapability(path); err == nil {
			t.Fatal("symlink capability was accepted")
		}
	})
	t.Run("unknown field", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "capability.json")
		if err := os.WriteFile(path, []byte(`{"unknown":true}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := consumeSupervisorCapability(path); err == nil {
			t.Fatal("unknown capability field was accepted")
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("invalid capability was not consumed: %v", err)
		}
	})
}

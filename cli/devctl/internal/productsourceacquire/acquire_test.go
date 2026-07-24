package productsourceacquire

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testRevision = "0123456789abcdef0123456789abcdef01234567"
const testTree = "89abcdef0123456789abcdef0123456789abcdef"

type fakeTransport struct {
	stops  int
	result transportStop
}

func (transport *fakeTransport) Stop() transportStop {
	transport.stops++
	return transport.result
}

type fakeBoundary struct {
	transport   *fakeTransport
	startErr    error
	failCommand string
	commands    []string
}

func (boundary *fakeBoundary) StartTransport(context.Context, Manifest) (transportHandle, error) {
	if boundary.transport == nil {
		boundary.transport = &fakeTransport{result: transportStop{processStopped: true, socketStopped: true}}
	}
	return boundary.transport, boundary.startErr
}

func (boundary *fakeBoundary) RunGit(_ context.Context, manifest Manifest, args ...string) (commandResult, error) {
	command := strings.Join(args, " ")
	boundary.commands = append(boundary.commands, command)
	if args[0] == "clone" {
		if err := os.MkdirAll(filepath.Join(manifest.Product.CheckoutPath, ".git"), 0o700); err != nil {
			return commandResult{}, err
		}
		if boundary.failCommand == "clone" {
			if err := os.WriteFile(filepath.Join(manifest.Product.CheckoutPath, "partial"), []byte("leave me"), 0o600); err != nil {
				return commandResult{}, err
			}
			return commandResult{}, errors.New("injected clone failure")
		}
	}
	if boundary.failCommand != "" && strings.Contains(command, boundary.failCommand) {
		return commandResult{}, errors.New("injected command failure")
	}
	switch {
	case strings.Contains(command, "remote get-url origin"):
		return commandResult{stdout: manifest.Product.Origin}, nil
	case strings.Contains(command, "rev-parse HEAD^{tree}"):
		return commandResult{stdout: testTree}, nil
	case strings.Contains(command, "rev-parse refs/devkit/source-acquisition-verification"):
		return commandResult{stdout: manifest.Product.Revision}, nil
	case strings.Contains(command, "rev-parse HEAD"):
		return commandResult{stdout: manifest.Product.Revision}, nil
	case strings.Contains(command, "status --porcelain=v1"):
		return commandResult{}, nil
	default:
		return commandResult{}, nil
	}
}

func testManifest(root string) Manifest {
	packagePath := "/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-devkit-product-source-acquirer"
	transportPath := "/nix/store/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-devkit-source-transport"
	gitPath := "/nix/store/cccccccccccccccccccccccccccccccc-git"
	sshPath := "/nix/store/dddddddddddddddddddddddddddddddd-openssh"
	return Manifest{
		SchemaVersion:  ManifestSchema,
		PackagePath:    packagePath,
		ExecutablePath: packagePath + "/bin/devkit-product-source-acquire",
		Product: ProductBinding{
			Origin:          "git@github.com:Divine-Shadow/ouroboros-ide.git",
			Revision:        testRevision,
			LifecycleRoot:   root,
			CheckoutPath:    filepath.Join(root, "product"),
			ReceiptPath:     filepath.Join(root, "source-acquisition-receipt.json"),
			TaintMarkerPath: filepath.Join(root, ".tainted"),
			IdentityPath:    "/run/credentials/devkit-product-git-identity",
		},
		Runtime: RuntimeBinding{
			GitExecutablePath:     gitPath + "/bin/git",
			OpenSSHExecutablePath: sshPath + "/bin/ssh",
			Path:                  gitPath + "/bin:" + sshPath + "/bin",
		},
		Transport: TransportBinding{
			SchemaVersion:        "devkit/source-transport/v4",
			NetworkMode:          "package-owned-direct-connect",
			ExecutablePath:       transportPath + "/bin/devkit-source-transport",
			GitSSHExecutablePath: transportPath + "/bin/devkit-source-git-ssh",
			SSHConfigPath:        transportPath + "/share/devkit-source-transport/ssh-config",
			KnownHostsPath:       transportPath + "/share/devkit-source-transport/github-ssh-known-hosts",
			AllowlistPath:        transportPath + "/share/devkit-source-transport/allowlist",
			NetworkContractPath:  transportPath + "/share/devkit-source-transport/network.json",
			ManagedConnectProxy:  "",
			SocketPath:           filepath.Join(root, "source-transport.sock"),
		},
	}
}

func TestManifestBindsExactLifecycleAndImmutableTools(t *testing.T) {
	manifest := testManifest("/var/lib/devkit-product-lifecycle")
	if err := validateManifest(manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Transport.ManagedConnectProxy = "http://127.0.0.1:18888"
	if err := validateManifest(manifest); err == nil {
		t.Fatal("source acquisition accepted an upstream proxy dependency")
	}
	manifest.Transport.ManagedConnectProxy = ""
	manifest.Transport.NetworkMode = "managed-loopback-connect"
	if err := validateManifest(manifest); err == nil {
		t.Fatal("source acquisition accepted the generic workspace network mode")
	}
	manifest.Transport.NetworkMode = "package-owned-direct-connect"
	manifest.Product.CheckoutPath = "/tmp/caller-selected"
	if err := validateManifest(manifest); err == nil {
		t.Fatal("caller-selected checkout escaped the fixed lifecycle geometry")
	}
}

func TestArgumentsAreRejectedBeforeManifestOrFilesystemAccess(t *testing.T) {
	var output strings.Builder
	exit := executeWithBoundary(context.Background(), []string{"--root", "/tmp/override"}, "", &output, &fakeBoundary{})
	if exit != 64 {
		t.Fatalf("exit=%d, want 64", exit)
	}
	if !strings.Contains(output.String(), `"code":"arguments_forbidden"`) {
		t.Fatalf("missing typed refusal: %s", output.String())
	}
}

func TestAcquireTransitionsAbsentRootDirectlyToVerifiedCheckout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "lifecycle")
	manifest := testManifest(root)
	boundary := &fakeBoundary{}
	receipt := newReceipt("/nix/store/manifest")
	if err := acquire(context.Background(), manifest, boundary, &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.RootOwnershipAcquired || !receipt.OriginVerified || !receipt.HeadVerified ||
		!receipt.CleanWorktreeVerified || !receipt.GitMetadataWriteProved {
		t.Fatalf("incomplete success receipt: %#v", receipt)
	}
	if receipt.ContentIdentity == nil || receipt.ContentIdentity.Tree != testTree {
		t.Fatalf("missing content identity: %#v", receipt.ContentIdentity)
	}
	if boundary.transport.stops != 1 || !receipt.OwnedRuntimeStop.TransportProcessStopped ||
		!receipt.OwnedRuntimeStop.TransportSocketStopped {
		t.Fatalf("transport was not stopped exactly once: stops=%d receipt=%#v", boundary.transport.stops, receipt.OwnedRuntimeStop)
	}
	if _, err := os.Stat(manifest.Product.CheckoutPath); err != nil {
		t.Fatalf("final checkout absent: %v", err)
	}
	if len(boundary.commands) == 0 || !strings.HasPrefix(boundary.commands[0], "clone --no-checkout --origin origin -- ") {
		t.Fatalf("first transition was not direct clone: %#v", boundary.commands)
	}
}

func TestAcquireRefusesExistingRootWithoutTouchingIt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "lifecycle")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "unexpected")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := testManifest(root)
	boundary := &fakeBoundary{}
	receipt := newReceipt("/nix/store/manifest")
	err := acquire(context.Background(), manifest, boundary, &receipt)
	assertTypedError(t, err, "precondition", "lifecycle_root_not_absent")
	if boundary.transport != nil || len(boundary.commands) != 0 {
		t.Fatal("existing root triggered an effect")
	}
	data, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(data) != "preserve" {
		t.Fatalf("existing material changed: data=%q err=%v", data, readErr)
	}
}

func TestFailureStopsOwnedRuntimeAndLeavesEntireRootForOuterDestruction(t *testing.T) {
	root := filepath.Join(t.TempDir(), "lifecycle")
	manifest := testManifest(root)
	boundary := &fakeBoundary{failCommand: "clone"}
	receipt := newReceipt("/nix/store/manifest")
	err := acquire(context.Background(), manifest, boundary, &receipt)
	assertTypedError(t, err, "git_clone", "command_failed")
	if boundary.transport.stops != 1 || !receipt.OwnedRuntimeStop.TransportProcessStopped ||
		!receipt.OwnedRuntimeStop.TransportSocketStopped {
		t.Fatalf("failed acquisition did not stop owned runtime: stops=%d receipt=%#v", boundary.transport.stops, receipt.OwnedRuntimeStop)
	}
	partial := filepath.Join(manifest.Product.CheckoutPath, "partial")
	if data, readErr := os.ReadFile(partial); readErr != nil || string(data) != "leave me" {
		t.Fatalf("partial state was deleted or rewritten: data=%q err=%v", data, readErr)
	}
}

func TestGitFailuresAreTypedAtTheirOperationSite(t *testing.T) {
	tests := []struct {
		fail  string
		stage string
		code  string
	}{
		{fail: " remote get-url origin", stage: "origin_verification", code: "mismatch"},
		{fail: " fetch --force", stage: "git_fetch", code: "command_failed"},
		{fail: " checkout --detach", stage: "git_checkout", code: "command_failed"},
		{fail: " rev-parse HEAD", stage: "head_verification", code: "mismatch"},
		{fail: " update-ref refs/devkit", stage: "git_metadata", code: "write_failed"},
	}
	for _, test := range tests {
		t.Run(test.stage, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "lifecycle")
			manifest := testManifest(root)
			boundary := &fakeBoundary{failCommand: test.fail}
			receipt := newReceipt("/nix/store/manifest")
			err := acquire(context.Background(), manifest, boundary, &receipt)
			assertTypedError(t, err, test.stage, test.code)
			if boundary.transport.stops != 1 {
				t.Fatalf("transport stops=%d, want 1", boundary.transport.stops)
			}
		})
	}
}

func TestCreateExclusiveNeverOverwritesUnexpectedMaterial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipt")
	if err := os.WriteFile(path, []byte("unexpected"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := createExclusive(path, []byte("replacement")); err == nil {
		t.Fatal("createExclusive overwrote an existing path")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "unexpected" {
		t.Fatalf("unexpected material changed: data=%q err=%v", data, err)
	}
}

func assertTypedError(t *testing.T, err error, stage, code string) {
	t.Helper()
	var typed typedError
	if !errors.As(err, &typed) || typed.stage != stage || typed.code != code {
		t.Fatalf("error=%v, want %s/%s", err, stage, code)
	}
}

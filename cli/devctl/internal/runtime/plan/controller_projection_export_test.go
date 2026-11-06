package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
)

// ControllerFixture is TEST ONLY. Its document models projection premises, not
// valid operational broker/service/credential authority. No real identity,
// credential, broker, provider, task or remote connection is created.
type ControllerFixture struct {
	Options       BuildOptions
	Manifest      string
	GUIManifest   string
	StoreManifest string
	Workspace     string
	Socket        string
	listener      net.Listener
}

func NewControllerFixture(t *testing.T, index int) *ControllerFixture {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "cp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	previous, previousGUI := controllerLocations, guiCodexConfigProjectionManifestPath
	t.Cleanup(func() { controllerLocations = previous; guiCodexConfigProjectionManifestPath = previousGUI })
	devRoot := filepath.Join(root, "dev")
	store := filepath.Join(root, "store")
	manifest := filepath.Join(root, "etc", "fleet", "controller-operation-authority.json")
	socket := filepath.Join(root, "run", "control.sock")
	for _, p := range []string{filepath.Join(devRoot, "devkit"), store, filepath.Dir(manifest), filepath.Dir(socket)} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	controllerLocations.manifest, controllerLocations.store, controllerLocations.devRoot, controllerLocations.socket = manifest, store, devRoot, socket
	controllerLocations.hostname = func() (string, error) { return "shadow-throne", nil }
	guiCodexConfigProjectionManifestPath = filepath.Join(root, "gui.json")
	target := "shadow-throne-management"
	if index == 2 {
		target += "-2"
	}
	opts := BuildOptions{Paths: devkitpaths.Paths{Root: filepath.Join(devRoot, "devkit"), RuntimeAuthorityRoot: filepath.Join(devRoot, "devkit")}, Project: "dev-workspace", Repo: "shadow-throne-management", Index: index, WorktreeRoot: filepath.Join(devRoot, "control-plane-worktrees"), StateRoot: filepath.Join(devRoot, ".devkit", "native-agents"), WorkspaceRoot: filepath.Join(devRoot, "control-plane-worktrees", "agent"+string(rune('0'+index))), GUITargetID: target, IsolationProfile: IsolationProfileWorkspaceEgress, EgressAllowlist: filepath.Join(root, "allowlist.txt"), ContainersDisabled: true}
	helper := filepath.Join(opts.Paths.RuntimeAuthorityRoot, "kit", "bin", "devctl")
	if err := os.MkdirAll(filepath.Dir(helper), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	opts.ProxySocket = filepath.Join(root, "managed-egress.sock")
	paths, err := controllerPaths(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.HostHome, 0700); err != nil {
		t.Fatal(err)
	}
	managementRev := controllerFixtureGit(t, paths.HostWorktree, "ssh://fixture.invalid/management.git")
	wsl := filepath.Join(opts.WorkspaceRoot, "wsl-nix")
	wslRev := controllerFixtureGit(t, wsl, "ssh://fixture.invalid/wsl-nix.git")
	record := guiCodexConfigProjectionRecord{TargetID: target, ConfigProfile: "management", Project: opts.Project, Repo: opts.Repo, AgentIndex: index, WorkspaceRoot: opts.WorkspaceRoot, HostWorktree: paths.HostWorktree, HostHome: paths.HostHome, SandboxHome: paths.SandboxHome, Source: "/nix/store/00000000000000000000000000000000-projection-fixture-config.toml", SourceSHA256: strings.Repeat("a", 64), Kind: "local-wsl-management", ExpectedExecutionHost: "shadow-throne"}
	writeGUIConfigManifest(t, guiCodexConfigProjectionManifestPath, []guiCodexConfigProjectionRecord{record})
	doc := map[string]any{
		"schemaVersion": "wsl-nix-management-controller-convergence/v9", "profileIdentity": controllerProfileIdentity, "mountPolicyIdentity": "devkit/workspace-egress/v4",
		"operationHandle": map[string]any{"environment": controllerHandleEnvironment, "requiredValue": "required", "socketPath": socket, "socketOwner": "bayesartre", "socketGroup": "users", "noFollow": true},
		"sourceRoots":     map[string]any{"management": controllerSourceRoot{LogicalPath: "/workspaces/dev/shadow-throne-management", BackingPath: filepath.Join(root, "shared-management-not-mounted"), Origin: "ssh://fixture.invalid/management.git", Revision: managementRev}, "wslNix": controllerSourceRoot{LogicalPath: controllerWSLTarget, BackingPath: filepath.Join(root, "shared-wsl-not-mounted"), Origin: "ssh://fixture.invalid/wsl-nix.git", Revision: wslRev}},
		"targets":         map[string]any{"controller": "shadow-throne", "controllerGui": "shadow-throne-management", "controllerAdditionalGui": "shadow-throne-management-2", "productAgentHost": "fixture-unused"},
		"kinds":           []string{"app-rpc.exec", "auth.exec", "fleet.exec", "gui.replace-controller", "gui.start-app-server", "nixos.deploy-closure", "nixos.wsl-sentinel", "product-agent.reconcile-absence"},
	}
	storeManifest := filepath.Join(store, "00000000000000000000000000000000-controller-projection.json")
	data, _ := json.Marshal(doc)
	if err := os.WriteFile(storeManifest, data, 0444); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(storeManifest, manifest); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(socket, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	t.Setenv(controllerProfileEnvironment, controllerProfileIdentity)
	return &ControllerFixture{Options: opts, Manifest: manifest, GUIManifest: guiCodexConfigProjectionManifestPath, StoreManifest: storeManifest, Workspace: opts.WorkspaceRoot, Socket: socket, listener: listener}
}

func controllerFixtureGit(t *testing.T, path, origin string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "LANG=C", "LC_ALL=C", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture Git failed: %v", err)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", path)
	if err := os.WriteFile(filepath.Join(path, "source.txt"), []byte("projection fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("-C", path, "add", "source.txt")
	run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "-C", path, "commit", "-q", "-m", "fixture")
	run("-C", path, "remote", "add", "origin", origin)
	return run("-C", path, "rev-parse", "HEAD")
}

func (f *ControllerFixture) RewriteManifest(t *testing.T, change func(map[string]any)) {
	t.Helper()
	data, err := os.ReadFile(f.StoreManifest)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	change(doc)
	data, _ = json.Marshal(doc)
	if err := os.Chmod(f.StoreManifest, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.StoreManifest, data, 0444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.StoreManifest, 0444); err != nil {
		t.Fatal(err)
	}
}
func (f *ControllerFixture) FileSHA(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.StoreManifest)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func (f *ControllerFixture) ReplaceSocket(t *testing.T) {
	t.Helper()
	if l, ok := f.listener.(*net.UnixListener); ok {
		l.SetUnlinkOnClose(false)
	}
	if err := f.listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.Socket, f.Socket+".retained"); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", f.Socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.Socket, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
}

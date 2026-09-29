package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/runtime/agent"
)

func TestCallbackReturnProjectionIsBoundedToSelectedDarksteelTargets(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "nix", "store")
	exe := filepath.Join(store, "aaaaaaaa-fleet", "bin", "fleet-control")
	parent := filepath.Join(root, "run", "fleet-cbr01-app-rpc")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("fixture"), 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(store, "bbbbbbbb-callback", "callback-return.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCallbackReturnManifest(t, manifest, exe, parent)
	if err := os.Chmod(manifest, 0o444); err != nil {
		t.Fatal(err)
	}
	previousSource, previousStore, previousParent := callbackReturnManifestSource, callbackReturnStoreRoot, callbackReturnExpectedHandleDirectory
	callbackReturnManifestSource, callbackReturnStoreRoot = manifest, store
	callbackReturnExpectedHandleDirectory = parent
	t.Cleanup(func() {
		callbackReturnManifestSource, callbackReturnStoreRoot, callbackReturnExpectedHandleDirectory = previousSource, previousStore, previousParent
	})

	p := Plan{Agent: structAgent(2), GUITargetConfig: &GUITargetConfigProjection{TargetID: "darksteel-2"}}
	got, err := loadCallbackReturnProjection(p)
	if err != nil {
		t.Fatalf("selected callback projection: %v", err)
	}
	if got == nil || got.TargetID != "darksteel-2" || got.AgentIndex != 2 || got.HandleDirectory != filepath.Join(parent, "darksteel-2") {
		t.Fatalf("projection = %#v", got)
	}
	p.GUITargetConfig.TargetID = "other-target"
	got, err = loadCallbackReturnProjection(p)
	if err != nil || got != nil {
		t.Fatalf("unselected target received projection: %#v, %v", got, err)
	}

	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(strings.Replace(string(data), `"agent":2`, `"agent":9`, 1)), 0o444); err != nil {
		t.Fatal(err)
	}
	p.GUITargetConfig.TargetID = "darksteel-2"
	if _, err := loadCallbackReturnProjection(p); err == nil {
		t.Fatal("malformed selected manifest was accepted")
	}
}

func TestCallbackReturnProjectionRejectsForeignAndSymlinkExecutables(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(root, "fleet-control")
	if err := os.WriteFile(foreign, []byte("fixture"), 0o555); err != nil {
		t.Fatal(err)
	}
	if err := validateCallbackReturnStoreExecutable(foreign); err == nil {
		t.Fatal("foreign executable accepted")
	}
	store := filepath.Join(root, "nix", "store")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(store, "fleet-link")
	if err := os.Symlink(foreign, link); err != nil {
		t.Fatal(err)
	}
	previous := callbackReturnStoreRoot
	callbackReturnStoreRoot = store
	t.Cleanup(func() { callbackReturnStoreRoot = previous })
	if err := validateCallbackReturnStoreExecutable(link); err == nil {
		t.Fatal("symlink executable accepted")
	}
}

func structAgent(index int) agent.Spec {
	return agent.Spec{ID: agent.ID{Project: "dev-all", Repo: "ouroboros-ide", Index: index}}
}

func writeCallbackReturnManifest(t *testing.T, path, executable, parent string) {
	t.Helper()
	manifest := callbackReturnManifest{SchemaVersion: CallbackReturnSchema, ProfileIdentity: CallbackReturnProfileIdentity, Host: callbackReturnExpectedHost, Project: callbackReturnExpectedProject, Repo: callbackReturnExpectedRepo, Directory: parent, FleetExecutable: executable, Targets: []callbackReturnTarget{{ID: "darksteel-2", Agent: 2, HandleDirectory: filepath.Join(parent, "darksteel-2")}, {ID: "darksteel-3", Agent: 3, HandleDirectory: filepath.Join(parent, "darksteel-3")}}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

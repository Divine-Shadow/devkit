package plan

import (
	"os"
	"path/filepath"
	"testing"
)

func ownerRoleFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	oldConfig, oldStatic, oldStore := ControllerServiceOwnerConfig, ControllerServiceOwnerStaticConfig, ControllerProfileStoreRoot
	t.Cleanup(func() {
		ControllerServiceOwnerConfig, ControllerServiceOwnerStaticConfig, ControllerProfileStoreRoot = oldConfig, oldStatic, oldStore
	})
	ControllerProfileStoreRoot = filepath.Join(root, "store")
	if err := os.MkdirAll(ControllerProfileStoreRoot, 0755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(ControllerProfileStoreRoot, "role.json")
	if err := os.WriteFile(source, []byte(`{"schemaVersion":1,"peerUser":"bayesartre","ownerUser":"root"}`), 0444); err != nil {
		t.Fatal(err)
	}
	ControllerServiceOwnerConfig, ControllerServiceOwnerStaticConfig = filepath.Join(root, "owner.json"), filepath.Join(root, "static-owner.json")
	for _, p := range []string{ControllerServiceOwnerConfig, ControllerServiceOwnerStaticConfig} {
		if err := os.Symlink(source, p); err != nil {
			t.Fatal(err)
		}
	}
	return source
}

func TestControllerServiceOwnerGrantIsOnlyRegisteredRuntime(t *testing.T) {
	ownerRoleFixture(t)
	profile := &ManagementControllerProfile{ProfileIdentity: ManagementControllerProfileIdentity}
	for _, row := range []struct {
		project string
		index   int
		repo    string
		profile *ManagementControllerProfile
		wanted  int
	}{
		{"dev-workspace", 1, "shadow-throne-management", profile, 2},
		{"dev-workspace", 2, "shadow-throne-management", profile, 0},
		{"dev-workspace", 1, "ti4-calculator", profile, 0},
		{"dev-all", 1, "ouroboros-ide", profile, 0},
		{"dev-workspace", 1, "shadow-throne-management", nil, 0},
	} {
		binds, err := controllerServiceOwnerClientBinds(row.project, row.index, row.repo, row.profile)
		if err != nil || len(binds) != row.wanted {
			t.Fatalf("%+v: binds=%+v err=%v", row, binds, err)
		}
		for _, bind := range binds {
			if bind.Mode != "ro" || !bind.Required || bind.Source != bind.Target {
				t.Fatalf("inexact bind %+v", bind)
			}
		}
	}
}

func TestControllerServiceOwnerRejectsMutableOrChangedRole(t *testing.T) {
	source := ownerRoleFixture(t)
	if resolved, err := ResolveControllerServiceOwnerConfig(); err != nil || resolved != source {
		t.Fatalf("immutable realpath: %s %v", resolved, err)
	}
	if err := os.Remove(ControllerServiceOwnerConfig); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ControllerServiceOwnerConfig, []byte(`{"schemaVersion":1,"peerUser":"bayesartre","ownerUser":"root"}`), 0444); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveControllerServiceOwnerConfig(); err == nil {
		t.Fatal("mutable role admitted")
	}
	if err := os.Remove(ControllerServiceOwnerConfig); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, ControllerServiceOwnerConfig); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"schemaVersion":1,"peerUser":"other","ownerUser":"root"}`,
		`{"schemaVersion":1,"peerUser":"bayesartre","ownerUser":"other"}`,
		`{"schemaVersion":1,"peerUser":"bayesartre","ownerUser":"root","force":true}`,
		`{"schemaVersion":1,"peerUser":"bayesartre","ownerUser":"root"} {}`,
	} {
		if err := os.Chmod(source, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source, []byte(body), 0444); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(source, 0444); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveControllerServiceOwnerConfig(); err == nil {
			t.Fatalf("changed role admitted: %s", body)
		}
	}
}

func TestControllerServiceOwnerAbsentSelectionDoesNotGrant(t *testing.T) {
	ownerRoleFixture(t)
	if err := os.Remove(ControllerServiceOwnerConfig); err != nil {
		t.Fatal(err)
	}
	binds, err := controllerServiceOwnerClientBinds("dev-workspace", 1, "shadow-throne-management", &ManagementControllerProfile{ProfileIdentity: ManagementControllerProfileIdentity})
	if err != nil || len(binds) != 0 {
		t.Fatalf("absent Source selection: %+v %v", binds, err)
	}
}

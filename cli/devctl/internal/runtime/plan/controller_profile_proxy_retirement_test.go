package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagementControllerProfileLoadsOptionalRetirementProjection(t *testing.T) {
	root := t.TempDir()
	oldStore, oldFleet, oldGUI := ControllerProfileStoreRoot, WorkspaceControllerSourceInventory, WorkspaceControllerGUIInventory
	ControllerProfileStoreRoot = filepath.Join(root, "nix", "store")
	WorkspaceControllerSourceInventory = filepath.Join(root, "fleet.json")
	WorkspaceControllerGUIInventory = filepath.Join(root, "gui.json")
	t.Cleanup(func() {
		ControllerProfileStoreRoot, WorkspaceControllerSourceInventory, WorkspaceControllerGUIInventory = oldStore, oldFleet, oldGUI
	})
	manifest := filepath.Join(root, "manifest.json")
	profile := writeControllerProfileManifest(t, manifest, filepath.Join(root, "management"), filepath.Join(root, "wsl"))
	if _, err := LoadManagementControllerProfile(manifest); err != nil {
		t.Fatalf("existing profile without optional projection: %v", err)
	}
	// Opaque bytes deliberately test only Devkit's file-identity responsibility.
	// Fleet's independent bounded-record and process checks are not exercised here.
	ref := controllerProfileFileIdentity(t, filepath.Join(ControllerProfileStoreRoot, "retirement-policy.json"), "opaque policy fixture\n")
	if err := os.Chmod(ref.Path, 0o444); err != nil {
		t.Fatal(err)
	}
	profile.ProductGUIProxyRetirementPolicy = &ref
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	write := func(b []byte) {
		t.Helper()
		if err := os.WriteFile(manifest, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(data)
	actual, err := LoadManagementControllerProfile(manifest)
	if err != nil || actual.ProductGUIProxyRetirementPolicy == nil || *actual.ProductGUIProxyRetirementPolicy != ref {
		t.Fatalf("actual loader lost/rejected typed projection: %v %#v", err, actual.ProductGUIProxyRetirementPolicy)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, projection string
		extra            bool
	}{
		{"unknown nested", `{"path":"` + ref.Path + `","sha256":"` + ref.SHA256 + `","command":"anything"}`, false},
		{"empty present", `{}`, false},
		{"wrong digest", `{"path":"` + ref.Path + `","sha256":"` + strings.Repeat("0", 64) + `"}`, false},
		{"unknown top-level", string(fields["productGUIProxyRetirementPolicy"]), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := make(map[string]json.RawMessage, len(fields)+1)
			for k, v := range fields {
				candidate[k] = v
			}
			candidate["productGUIProxyRetirementPolicy"] = json.RawMessage(test.projection)
			if test.extra {
				candidate["unrecognizedPolicyOverride"] = json.RawMessage(`true`)
			}
			b, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			write(b)
			if _, err := LoadManagementControllerProfile(manifest); err == nil {
				t.Fatal("malformed profile admitted by production loader")
			}
		})
	}
	write(append(append([]byte(nil), data...), []byte("\n{}")...))
	if _, err := LoadManagementControllerProfile(manifest); err == nil || !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("trailing value: %v", err)
	}
	fields["productGUIProxyRetirementPolicy"] = json.RawMessage(`null`)
	data, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	write(data)
	if _, err := LoadManagementControllerProfile(manifest); err != nil {
		t.Fatalf("null optional projection: %v", err)
	}
}

func TestControllerRetirementProjectionRejectsInvalidFileIdentity(t *testing.T) {
	root := t.TempDir()
	oldStore := ControllerProfileStoreRoot
	ControllerProfileStoreRoot = filepath.Join(root, "nix", "store")
	t.Cleanup(func() { ControllerProfileStoreRoot = oldStore })
	valid := controllerProfileFileIdentity(t, filepath.Join(ControllerProfileStoreRoot, "policy.json"), "opaque policy fixture\n")
	if err := os.Chmod(valid.Path, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := validateControllerProductGUIProxyRetirementPolicy(nil); err != nil {
		t.Fatal(err)
	}
	if err := validateControllerProductGUIProxyRetirementPolicy(&valid); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(ControllerProfileStoreRoot, "directory")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(ControllerProfileStoreRoot, "alias.json")
	if err := os.Symlink(valid.Path, alias); err != nil {
		t.Fatal(err)
	}
	nested := controllerProfileFileIdentity(t, filepath.Join(ControllerProfileStoreRoot, "package", "policy.json"), "opaque policy fixture\n")
	if err := os.Chmod(nested.Path, 0o444); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*ControllerProfileFileIdentity)
	}{
		{"empty path", func(x *ControllerProfileFileIdentity) { x.Path = "" }},
		{"relative path", func(x *ControllerProfileFileIdentity) { x.Path = "policy.json" }},
		{"outside store", func(x *ControllerProfileFileIdentity) { x.Path = filepath.Join(root, "policy.json") }},
		{"store directory", func(x *ControllerProfileFileIdentity) { x.Path = ControllerProfileStoreRoot }},
		{"nested file", func(x *ControllerProfileFileIdentity) { x.Path = nested.Path }},
		{"directory", func(x *ControllerProfileFileIdentity) { x.Path = directory }},
		{"whitespace", func(x *ControllerProfileFileIdentity) { x.Path += " " }},
		{"noncanonical", func(x *ControllerProfileFileIdentity) { x.Path = ControllerProfileStoreRoot + "/../store/policy.json" }},
		{"missing file", func(x *ControllerProfileFileIdentity) { x.Path += "-missing" }},
		{"symlink", func(x *ControllerProfileFileIdentity) { x.Path = alias }},
		{"empty digest", func(x *ControllerProfileFileIdentity) { x.SHA256 = "" }},
		{"uppercase digest", func(x *ControllerProfileFileIdentity) { x.SHA256 = strings.ToUpper(x.SHA256) }},
		{"bad digest encoding", func(x *ControllerProfileFileIdentity) { x.SHA256 = strings.Repeat("z", 64) }},
		{"mismatched digest", func(x *ControllerProfileFileIdentity) { x.SHA256 = strings.Repeat("0", 64) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			if err := validateControllerProductGUIProxyRetirementPolicy(&candidate); err == nil {
				t.Fatalf("invalid identity accepted: %#v", candidate)
			}
		})
	}
	if err := os.Chmod(valid.Path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateControllerProductGUIProxyRetirementPolicy(&valid); err == nil {
		t.Fatal("writable projection accepted")
	}
	if err := os.Chmod(valid.Path, 0o444); err != nil {
		t.Fatal(err)
	}
	parentAlias := filepath.Join(root, "store-alias")
	if err := os.Symlink(ControllerProfileStoreRoot, parentAlias); err != nil {
		t.Fatal(err)
	}
	ControllerProfileStoreRoot = parentAlias
	candidate := valid
	candidate.Path = filepath.Join(parentAlias, "policy.json")
	if err := validateControllerProductGUIProxyRetirementPolicy(&candidate); err == nil {
		t.Fatal("aliased store parent accepted")
	}
}

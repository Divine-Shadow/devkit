package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagementControllerProfileGUIThreadControlStrictDecode(t *testing.T) {
	const artifact = `{"controller":"shadow-throne","packagePath":"/nix/store/example-reader","executablePath":"/nix/store/example-reader/bin/devops-gui-thread-control","sourceRevision":"d99f7b73ad5b6f7739351c77bedc8ab87c42dec1"}`
	for _, test := range []struct {
		name, input string
		valid       bool
	}{
		{"absent", `{}`, true},
		{"typed artifact", `{"guiThreadControl":` + artifact + `}`, true},
		{"unknown nested field", `{"guiThreadControl":` + strings.TrimSuffix(artifact, "}") + `,"command":"anything"}}`, false},
		{"unknown top-level field", `{"guiThreadControl":` + artifact + `,"unrecognized":true}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var profile ManagementControllerProfile
			decoder := json.NewDecoder(strings.NewReader(test.input))
			decoder.DisallowUnknownFields()
			err := decoder.Decode(&profile)
			if (err == nil) != test.valid {
				t.Fatalf("decode valid=%v: %v", test.valid, err)
			}
			if test.name == "typed artifact" && (profile.GUIThreadControl == nil || profile.GUIThreadControl.SourceRevision != "d99f7b73ad5b6f7739351c77bedc8ab87c42dec1") {
				t.Fatalf("typed artifact was not preserved: %#v", profile.GUIThreadControl)
			}
		})
	}
}

func TestManagementControllerProfileGUIThreadControlValidation(t *testing.T) {
	root := t.TempDir()
	previousStoreRoot := ControllerProfileStoreRoot
	ControllerProfileStoreRoot = filepath.Join(root, "nix", "store")
	t.Cleanup(func() { ControllerProfileStoreRoot = previousStoreRoot })
	packagePath := filepath.Join(ControllerProfileStoreRoot, "reader")
	executablePath := filepath.Join(packagePath, "bin", "devops-gui-thread-control")
	if err := os.MkdirAll(filepath.Dir(executablePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executablePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	valid := ControllerProfileGUIThreadControl{
		Controller: ManagementControllerNode, PackagePath: packagePath,
		ExecutablePath: executablePath, SourceRevision: strings.Repeat("e", 40),
	}
	profile := ManagementControllerProfile{Targets: ControllerProfileTargets{Controller: ManagementControllerNode}}
	if err := validateControllerGUIThreadControl(profile); err != nil {
		t.Fatalf("absent optional artifact rejected: %v", err)
	}
	profile.GUIThreadControl = &valid
	if err := validateControllerGUIThreadControl(profile); err != nil {
		t.Fatalf("valid artifact rejected: %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*ControllerProfileGUIThreadControl)
	}{
		{"empty controller", func(r *ControllerProfileGUIThreadControl) { r.Controller = "" }},
		{"foreign controller", func(r *ControllerProfileGUIThreadControl) { r.Controller = "another" }},
		{"invalid source", func(r *ControllerProfileGUIThreadControl) { r.SourceRevision = "main" }},
		{"uppercase source", func(r *ControllerProfileGUIThreadControl) { r.SourceRevision = strings.Repeat("E", 40) }},
		{"mutable package", func(r *ControllerProfileGUIThreadControl) { r.PackagePath = root }},
		{"store root", func(r *ControllerProfileGUIThreadControl) { r.PackagePath = ControllerProfileStoreRoot }},
		{"nested package", func(r *ControllerProfileGUIThreadControl) { r.PackagePath += "/bin" }},
		{"noncanonical package", func(r *ControllerProfileGUIThreadControl) { r.PackagePath += "/." }},
		{"absent package", func(r *ControllerProfileGUIThreadControl) { r.PackagePath += "-missing" }},
		{"foreign executable", func(r *ControllerProfileGUIThreadControl) { r.ExecutablePath = "/bin/sh" }},
		{"alternate executable", func(r *ControllerProfileGUIThreadControl) { r.ExecutablePath += "-other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			profile.GUIThreadControl = &candidate
			if err := validateControllerGUIThreadControl(profile); err == nil {
				t.Fatalf("malformed present artifact accepted: %#v", candidate)
			}
		})
	}
	outside := filepath.Join(ControllerProfileStoreRoot, "other-reader")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(executablePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, executablePath); err != nil {
		t.Fatal(err)
	}
	profile.GUIThreadControl = &valid
	if err := validateControllerGUIThreadControl(profile); err == nil {
		t.Fatal("executable escaping its immutable package was accepted")
	}
}

package plan

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestControllerProjectionRequiresSourceSelectedRole(t *testing.T) {
	for _, change := range []string{"no-gui", "product", "ordinary-isolation", "unknown-profile", "foreign-root"} {
		t.Run(change, func(t *testing.T) {
			f := NewControllerFixture(t, 1)
			opts := f.Options
			switch change {
			case "no-gui":
				opts.GUITargetID = ""
			case "product":
				opts.Project = "dev-all"
			case "ordinary-isolation":
				opts.IsolationProfile = ""
			case "unknown-profile":
				t.Setenv(controllerProfileEnvironment, "unknown")
			case "foreign-root":
				opts.WorkspaceRoot += "-foreign"
			}
			if _, err := Build(opts); err == nil {
				t.Fatal("unbound role was accepted")
			}
		})
	}
}
func TestControllerProjectionRejectsMalformedOrUnselectedAuthority(t *testing.T) {
	for _, change := range []string{"schema", "profile", "policy", "target", "kinds", "socket", "source"} {
		t.Run(change, func(t *testing.T) {
			f := NewControllerFixture(t, 2)
			f.RewriteManifest(t, func(doc map[string]any) {
				switch change {
				case "schema":
					doc["schemaVersion"] = "wrong"
				case "profile":
					doc["profileIdentity"] = "wrong"
				case "policy":
					doc["mountPolicyIdentity"] = "devkit/workspace-egress/v3"
				case "target":
					doc["targets"].(map[string]any)["controllerAdditionalGui"] = ""
				case "kinds":
					doc["kinds"] = []string{"gui.start-app-server"}
				case "socket":
					doc["operationHandle"].(map[string]any)["socketPath"] = "/tmp/caller.sock"
				case "source":
					doc["sourceRoots"].(map[string]any)["wslNix"].(map[string]any)["logicalPath"] = "/workspaces/dev/other"
				}
			})
			if _, err := Build(f.Options); err == nil {
				t.Fatal("wrong authority accepted")
			}
		})
	}
}
func TestControllerProjectionRejectsMissingMutableAndSymlinkHandle(t *testing.T) {
	for _, change := range []string{"absent-manifest", "mutable-manifest", "absent-socket", "socket-mode", "socket-symlink"} {
		t.Run(change, func(t *testing.T) {
			f := NewControllerFixture(t, 1)
			switch change {
			case "absent-manifest":
				os.Remove(f.Manifest)
			case "mutable-manifest":
				os.Chmod(f.StoreManifest, 0600)
			case "absent-socket":
				f.listener.Close()
			case "socket-mode":
				os.Chmod(f.Socket, 0666)
			case "socket-symlink":
				f.listener.Close()
				os.Symlink("/tmp/foreign", f.Socket)
			}
			if _, err := Build(f.Options); err == nil {
				t.Fatal("unusable handle accepted")
			}
		})
	}
}
func TestControllerProjectionUsesLaneLocalSourceAndRejectsDrift(t *testing.T) {
	f := NewControllerFixture(t, 1)
	p, err := Build(f.Options)
	if err != nil {
		t.Fatal(err)
	}
	if p.Controller.WSLLaneSource != filepath.Join(f.Workspace, "wsl-nix") {
		t.Fatal("shared source selected")
	}
	if err := ValidateControllerProjection(p); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(f.Workspace, "wsl-nix", "source.txt")
	if err := os.WriteFile(source, []byte("dirty source"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateControllerProjection(p); err == nil {
		t.Fatal("dirty source accepted")
	}
}
func TestControllerProjectionRejectsCapturedManifestSocketAndBindingReplacement(t *testing.T) {
	for _, change := range []string{"manifest", "socket", "gui", "bind", "environment", "mount-environment", "gui-environment", "ancestor-bind", "windows-environment", "serialized"} {
		t.Run(change, func(t *testing.T) {
			f := NewControllerFixture(t, 2)
			p, err := Build(f.Options)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "manifest":
				f.RewriteManifest(t, func(doc map[string]any) { doc["broker"] = map[string]any{"changed": true} })
			case "socket":
				f.ReplaceSocket(t)
			case "gui":
				data, _ := os.ReadFile(f.GUIManifest)
				os.WriteFile(f.GUIManifest, []byte(strings.Replace(string(data), "management\"", "changed\"", 1)), 0600)
			case "bind":
				for i := range p.Binds {
					if p.Binds[i].Target == controllerWSLTarget {
						p.Binds[i].Source = "/tmp/shared"
					}
				}
			case "environment":
				p.Env[controllerHandleEnvironment] = "caller"
			case "mount-environment":
				p.Env["DEVKIT_NATIVE_MOUNT_POLICY_IDENTITY"] = "wrong"
			case "gui-environment":
				p.Env[GUITargetIDEnvironment] = "wrong"
			case "ancestor-bind":
				p.Binds = append(p.Binds, Bind{Source: "/tmp/foreign", Target: "/etc/fleet", Mode: "rw", Required: true})
			case "windows-environment":
				p.Env["DEVKIT_NATIVE_WINDOWS_MOUNTS_VISIBLE"] = "true"
			case "serialized":
				copy := *p.Controller
				copy.captured = nil
				p.Controller = &copy
			}
			if err := ValidateControllerProjection(p); err == nil {
				t.Fatal("changed projection accepted")
			}
		})
	}
}
func TestControllerProjectionDefaultPlansDoNotAcquireCapability(t *testing.T) {
	f := NewControllerFixture(t, 1)
	t.Setenv(controllerProfileEnvironment, "")
	opts := f.Options
	opts.WorkspaceRoot = ""
	opts.GUITargetID = ""
	p, err := Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	if p.Controller != nil || p.Env[controllerHandleEnvironment] != "" {
		t.Fatal("ordinary plan acquired controller")
	}
	before := append([]Bind(nil), p.Binds...)
	if err := ValidateControllerProjection(p); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, p.Binds) {
		t.Fatal("validation changed ordinary plan")
	}
	p.Env[controllerHandleEnvironment] = "required"
	if err := ValidateControllerProjection(p); err == nil {
		t.Fatal("ambient handle minted a projection")
	}
}

func TestControllerProjectionRejectsWrongOrHiddenSource(t *testing.T) {
	for _, change := range []string{"revision", "origin", "missing", "shared-symlink", "hidden-index", "unknown-field"} {
		t.Run(change, func(t *testing.T) {
			f := NewControllerFixture(t, 1)
			wsl := filepath.Join(f.Workspace, "wsl-nix")
			switch change {
			case "revision":
				f.RewriteManifest(t, func(doc map[string]any) {
					doc["sourceRoots"].(map[string]any)["wslNix"].(map[string]any)["revision"] = strings.Repeat("0", 40)
				})
			case "origin":
				f.RewriteManifest(t, func(doc map[string]any) {
					doc["sourceRoots"].(map[string]any)["wslNix"].(map[string]any)["origin"] = "ssh://fixture.invalid/foreign.git"
				})
			case "missing":
				if err := os.Rename(wsl, wsl+".preserved"); err != nil {
					t.Fatal(err)
				}
			case "shared-symlink":
				if err := os.Rename(wsl, wsl+".shared"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(wsl+".shared", wsl); err != nil {
					t.Fatal(err)
				}
			case "hidden-index":
				cmd := exec.Command("git", "-C", wsl, "update-index", "--assume-unchanged", "source.txt")
				cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
				if err := cmd.Run(); err != nil {
					t.Fatal(err)
				}
			case "unknown-field":
				f.RewriteManifest(t, func(doc map[string]any) { doc["callerMounts"] = []string{"/credentials"} })
			}
			if _, err := Build(f.Options); err == nil {
				t.Fatal("wrong/hidden source accepted")
			}
		})
	}
}

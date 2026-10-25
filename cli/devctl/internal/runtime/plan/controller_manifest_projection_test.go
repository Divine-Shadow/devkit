package plan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagementManifestProjectionKeepsInstalledArtifact(t *testing.T) {
	for _, scenario := range []string{"installed", "flattened", "different-static-artifact", "outside-store", "store-symlink-escape"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			oldManifest, oldStatic, oldStore := ManagementControllerProfileManifestPath, ManagementControllerProfileStaticManifestPath, ControllerProfileStoreRoot
			t.Cleanup(func() {
				ManagementControllerProfileManifestPath, ManagementControllerProfileStaticManifestPath, ControllerProfileStoreRoot = oldManifest, oldStatic, oldStore
			})
			ManagementControllerProfileManifestPath = filepath.Join(root, "public.json")
			ManagementControllerProfileStaticManifestPath = filepath.Join(root, "static.json")
			ControllerProfileStoreRoot = filepath.Join(root, "store")
			if err := os.Mkdir(ControllerProfileStoreRoot, 0755); err != nil {
				t.Fatal(err)
			}
			artifact := filepath.Join(ControllerProfileStoreRoot, "selected-manifest")
			other := filepath.Join(ControllerProfileStoreRoot, "different-artifact")
			outside := filepath.Join(root, "outside.json")
			for _, path := range []string{artifact, other, outside} {
				if err := os.WriteFile(path, []byte("same exact bytes\n"), 0444); err != nil {
					t.Fatal(err)
				}
			}
			selected, static := artifact, artifact
			switch scenario {
			case "different-static-artifact":
				static = other
			case "outside-store":
				selected, static = outside, outside
			case "store-symlink-escape":
				if err := os.Remove(artifact); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, artifact); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(static, ManagementControllerProfileStaticManifestPath); err != nil {
				t.Fatal(err)
			}
			if scenario == "flattened" {
				if err := os.WriteFile(ManagementControllerProfileManifestPath, []byte("same exact bytes\n"), 0444); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(selected, ManagementControllerProfileManifestPath); err != nil {
				t.Fatal(err)
			}
			resolved, err := ResolveManagementControllerProfileManifest()
			if scenario == "installed" {
				if err != nil || resolved != artifact {
					t.Fatalf("selected immutable artifact changed: %q, %v", resolved, err)
				}
			} else if err == nil {
				t.Fatalf("accepted %s despite identical bytes without installed provenance", scenario)
			}
		})
	}
}

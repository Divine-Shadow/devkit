package productadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func selectorInstallFixture(t *testing.T, contents string) (authoritySelectorInstallOptions, string, string) {
	t.Helper()
	root := t.TempDir()
	store := filepath.Join(root, "store")
	parent := filepath.Join(root, "var", "lib", "product-runtime")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(store, "manifest.json")
	if err := os.WriteFile(manifest, []byte(contents), 0o444); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(contents))
	uid := os.Geteuid()
	return authoritySelectorInstallOptions{
		SelectorPath: filepath.Join(parent, "authority-selector.json"),
		StoreRoot:    store,
		RequiredUID:  uid,
		CurrentEUID:  func() int { return uid },
	}, manifest, hex.EncodeToString(sum[:])
}

func TestInstallAuthoritySelectorAtomicallyCreatesAndReplacesExactGeneration(t *testing.T) {
	options, manifest, digest := selectorInstallFixture(t, "first manifest\n")
	if err := installAuthoritySelector(manifest, digest, options); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(options.SelectorPath)
	if err != nil {
		t.Fatal(err)
	}
	var selector authoritySelector
	if err := json.Unmarshal(first, &selector); err != nil {
		t.Fatal(err)
	}
	if selector.SchemaVersion != authoritySelectorSchema || selector.ManifestPath != manifest || selector.ManifestSHA256 != digest {
		t.Fatalf("selector = %#v", selector)
	}
	info, err := os.Stat(options.SelectorPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("selector mode = %v err=%v", info.Mode(), err)
	}
	secondManifest := filepath.Join(options.StoreRoot, "second.json")
	secondContents := []byte("second manifest\n")
	if err := os.WriteFile(secondManifest, secondContents, 0o444); err != nil {
		t.Fatal(err)
	}
	secondSum := sha256.Sum256(secondContents)
	if err := installAuthoritySelector(secondManifest, hex.EncodeToString(secondSum[:]), options); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(options.SelectorPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) == string(first) || !strings.Contains(string(second), secondManifest) {
		t.Fatalf("selector was not atomically replaced: %s", second)
	}
	entries, err := os.ReadDir(filepath.Dir(options.SelectorPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".authority-selector.") {
			t.Fatalf("temporary selector generation remained: %s", entry.Name())
		}
	}
}

func TestInstallAuthoritySelectorRejectsUntrustedIdentityAndFilesystemShapes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *authoritySelectorInstallOptions, *string, *string)
		want   string
	}{
		{
			name: "wrong digest",
			mutate: func(_ *testing.T, _ *authoritySelectorInstallOptions, _ *string, digest *string) {
				*digest = strings.Repeat("0", 64)
			},
			want: "does not match immutable bytes",
		},
		{
			name: "outside store",
			mutate: func(t *testing.T, options *authoritySelectorInstallOptions, manifest *string, digest *string) {
				outside := filepath.Join(filepath.Dir(options.StoreRoot), "outside.json")
				contents := []byte("outside\n")
				if err := os.WriteFile(outside, contents, 0o444); err != nil {
					t.Fatal(err)
				}
				*manifest = outside
				sum := sha256.Sum256(contents)
				*digest = hex.EncodeToString(sum[:])
			},
			want: "immutable store artifact",
		},
		{
			name: "noncanonical manifest path",
			mutate: func(_ *testing.T, _ *authoritySelectorInstallOptions, manifest *string, _ *string) {
				*manifest = filepath.Dir(*manifest) + "/./" + filepath.Base(*manifest)
			},
			want: "exact and canonical",
		},
		{
			name: "whitespace wrapped manifest path",
			mutate: func(_ *testing.T, _ *authoritySelectorInstallOptions, manifest *string, _ *string) {
				*manifest = " \t" + *manifest + "\n"
			},
			want: "exact and canonical",
		},
		{
			name: "writable manifest",
			mutate: func(t *testing.T, _ *authoritySelectorInstallOptions, manifest *string, _ *string) {
				if err := os.Chmod(*manifest, 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: "not an immutable",
		},
		{
			name: "manifest symlink",
			mutate: func(t *testing.T, _ *authoritySelectorInstallOptions, manifest *string, _ *string) {
				link := *manifest + ".link"
				if err := os.Symlink(*manifest, link); err != nil {
					t.Fatal(err)
				}
				*manifest = link
			},
			want: "must not traverse symlinks",
		},
		{
			name: "wrong effective uid",
			mutate: func(_ *testing.T, options *authoritySelectorInstallOptions, _ *string, _ *string) {
				options.CurrentEUID = func() int { return options.RequiredUID + 1 }
			},
			want: "requires uid",
		},
		{
			name: "destination symlink",
			mutate: func(t *testing.T, options *authoritySelectorInstallOptions, _ *string, _ *string) {
				target := filepath.Join(filepath.Dir(options.SelectorPath), "target")
				if err := os.WriteFile(target, []byte("do not replace"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, options.SelectorPath); err != nil {
					t.Fatal(err)
				}
			},
			want: "without symlink traversal",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options, manifest, digest := selectorInstallFixture(t, "manifest\n")
			test.mutate(t, &options, &manifest, &digest)
			if err := installAuthoritySelector(manifest, digest, options); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("wanted %q, got %v", test.want, err)
			}
		})
	}
}

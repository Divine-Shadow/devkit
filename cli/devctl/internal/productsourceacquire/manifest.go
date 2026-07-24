package productsourceacquire

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func loadManifest(path string) (Manifest, string, error) {
	if !immutableStorePath(path) {
		return Manifest{}, "", fmt.Errorf("manifest path is not immutable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, "", err
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, "", err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Manifest{}, "", fmt.Errorf("manifest contains trailing JSON")
	}
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, "", err
	}
	digest := sha256.Sum256(data)
	return manifest, hex.EncodeToString(digest[:]), nil
}

// LoadPackageForLaunch accepts only the immutable, package-owned acquisition
// executable. Every runtime path and network decision is then derived from its
// adjacent manifest; callers cannot supply them independently.
func LoadPackageForLaunch(executablePath string) (Manifest, error) {
	if !immutableStorePath(executablePath) || filepath.Base(executablePath) != "devkit-product-source-acquire" {
		return Manifest{}, fmt.Errorf("source acquisition executable is not one immutable package entrypoint")
	}
	packagePath := filepath.Dir(filepath.Dir(executablePath))
	manifestPath := filepath.Join(packagePath, "share", "devkit-product-source-acquisition", "manifest.json")
	manifest, _, err := loadManifest(manifestPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("load adjacent source acquisition manifest: %w", err)
	}
	if manifest.PackagePath != packagePath || manifest.ExecutablePath != executablePath {
		return Manifest{}, fmt.Errorf("source acquisition executable and adjacent manifest do not share package ownership")
	}
	if err := immutableRegularFile(executablePath, true); err != nil {
		return Manifest{}, fmt.Errorf("validate source acquisition executable: %w", err)
	}
	if err := immutableRegularFile(manifestPath, false); err != nil {
		return Manifest{}, fmt.Errorf("validate adjacent source acquisition manifest: %w", err)
	}
	return manifest, nil
}

func immutableRegularFile(path string, executable bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("immutable package path is not one non-writable regular file")
	}
	if executable && info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("immutable package executable has no execute bit")
	}
	return nil
}

func validateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != ManifestSchema {
		return fmt.Errorf("unsupported manifest schema")
	}
	for label, path := range map[string]string{
		"package":                  manifest.PackagePath,
		"executable":               manifest.ExecutablePath,
		"Git executable":           manifest.Runtime.GitExecutablePath,
		"OpenSSH executable":       manifest.Runtime.OpenSSHExecutablePath,
		"source transport":         manifest.Transport.ExecutablePath,
		"source transport Git SSH": manifest.Transport.GitSSHExecutablePath,
		"SSH config":               manifest.Transport.SSHConfigPath,
		"known hosts":              manifest.Transport.KnownHostsPath,
		"allowlist":                manifest.Transport.AllowlistPath,
		"network contract":         manifest.Transport.NetworkContractPath,
	} {
		if !immutableStorePath(path) {
			return fmt.Errorf("%s path is not immutable", label)
		}
	}
	if manifest.ExecutablePath != filepath.Join(manifest.PackagePath, "bin", "devkit-product-source-acquire") {
		return fmt.Errorf("executable does not belong to package")
	}
	if manifest.Transport.SchemaVersion != "devkit/source-transport/v4" {
		return fmt.Errorf("unsupported source transport schema")
	}
	if manifest.Transport.NetworkMode != "package-owned-direct-connect" {
		return fmt.Errorf("unsupported source transport network mode")
	}
	if manifest.Transport.ManagedConnectProxy != "" {
		return fmt.Errorf("Product source transport must not depend on an upstream proxy")
	}
	if !validProductOrigin(manifest.Product.Origin) {
		return fmt.Errorf("Product origin is not one exact GitHub SSH origin")
	}
	if !revisionPattern.MatchString(manifest.Product.Revision) {
		return fmt.Errorf("Product revision is not one exact lowercase 40-character commit")
	}
	root := manifest.Product.LifecycleRoot
	if !safeAbsoluteRuntimePath(root) || root == string(filepath.Separator) {
		return fmt.Errorf("lifecycle root is not one safe absolute path")
	}
	if manifest.Product.CheckoutPath != filepath.Join(root, "product") ||
		manifest.Product.ReceiptPath != filepath.Join(root, "source-acquisition-receipt.json") ||
		manifest.Product.TaintMarkerPath != filepath.Join(root, ".tainted") ||
		manifest.Transport.SocketPath != filepath.Join(root, "source-transport.sock") {
		return fmt.Errorf("lifecycle paths do not match the fixed package geometry")
	}
	if !safeAbsoluteRuntimePath(manifest.Product.IdentityPath) {
		return fmt.Errorf("identity path is not one safe absolute path")
	}
	if manifest.Runtime.Path == "" {
		return fmt.Errorf("immutable runtime PATH is empty")
	}
	for _, path := range strings.Split(manifest.Runtime.Path, string(os.PathListSeparator)) {
		if !immutableStorePath(path) {
			return fmt.Errorf("runtime PATH contains a mutable entry")
		}
	}
	return nil
}

func immutableStorePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path &&
		strings.HasPrefix(path, "/nix/store/") && !unsafePathCharacters(path)
}

func safeAbsoluteRuntimePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !unsafePathCharacters(path)
}

func unsafePathCharacters(path string) bool {
	return strings.ContainsAny(path, " \t\r\n'\"`$;&|<>\\")
}

func validProductOrigin(origin string) bool {
	if strings.ContainsAny(origin, " \t\r\n'\"`$;&|<>\\") || !strings.HasSuffix(origin, ".git") {
		return false
	}
	return strings.HasPrefix(origin, "git@github.com:") ||
		strings.HasPrefix(origin, "ssh://git@ssh.github.com:443/")
}

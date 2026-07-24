package launch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devkit/cli/devctl/internal/productsourceacquire"
)

var packageSourceAcquisitionBubblewrap string

// BuildProductSourceAcquisitionBubblewrap constructs the one exceptional
// direct-network profile. The executable's adjacent immutable manifest owns
// every path and the zero-argument workload; ordinary workspace profiles keep
// their existing network isolation.
func BuildProductSourceAcquisitionBubblewrap(executablePath string) (Command, error) {
	manifest, err := productsourceacquire.LoadPackageForLaunch(strings.TrimSpace(executablePath))
	if err != nil {
		return Command{}, err
	}
	bubblewrapPath := strings.TrimSpace(packageSourceAcquisitionBubblewrap)
	if !immutableExecutable(bubblewrapPath) {
		return Command{}, fmt.Errorf("source acquisition bubblewrap is not one immutable package executable")
	}
	return buildProductSourceAcquisitionBubblewrap(bubblewrapPath, manifest), nil
}

func buildProductSourceAcquisitionBubblewrap(bubblewrapPath string, manifest productsourceacquire.Manifest) Command {
	args := []string{
		"--die-with-parent",
		"--unshare-all",
		"--share-net",
		"--new-session",
		"--clearenv",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--ro-bind", "/nix/store", "/nix/store",
	}
	created := map[string]bool{"/": true, "/tmp": true, "/proc": true, "/dev": true, "/nix": true, "/nix/store": true}
	var addDir func(string)
	addDir = func(path string) {
		path = filepath.Clean(path)
		if path == "." || created[path] {
			return
		}
		addDir(filepath.Dir(path))
		args = append(args, "--dir", path)
		created[path] = true
	}
	for _, path := range []string{"/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf", "/etc/passwd", "/etc/group"} {
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
			addDir(filepath.Dir(path))
			args = append(args, "--ro-bind", path, path)
		}
	}
	lifecycleParent := filepath.Dir(manifest.Product.LifecycleRoot)
	addDir(lifecycleParent)
	args = append(args, "--bind", lifecycleParent, lifecycleParent)
	addDir(filepath.Dir(manifest.Product.IdentityPath))
	args = append(args, "--ro-bind", manifest.Product.IdentityPath, manifest.Product.IdentityPath)
	args = append(args,
		"--chdir", "/",
		manifest.ExecutablePath,
	)
	return Command{Path: bubblewrapPath, Args: args, Dir: lifecycleParent}
}

func immutableExecutable(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !strings.HasPrefix(path, "/nix/store/") {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 && info.Mode().Perm()&0o022 == 0
}

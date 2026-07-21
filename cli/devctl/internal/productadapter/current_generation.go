//go:build !devkitintegration

package productadapter

import (
	"fmt"
	"os"
	"path/filepath"
)

func holdCurrentGeneration(role Role, adapter AdapterManifest) ([]*os.File, error) {
	// The manifest has already selected the immutable executable. The
	// current-system handle is only an activated-generation equality check; it
	// never selects, derives, or rewrites Product runtime identity.
	expected := adapter.ExecutablePath
	name := "product-adapter"
	if role == RoleProxy {
		expected = adapter.ProxyHelperPath
		name = "product-proxy"
	} else if role == RoleSupervisor {
		expected = adapter.SupervisorExecutablePath
		name = "product-adapter-supervisor"
	} else if role == RoleSSHSession {
		expected = adapter.SSHSessionExecutablePath
		name = "product-ssh-session"
	} else if role == RoleSSHSetup {
		expected = adapter.SSHSetupExecutablePath
		name = "product-ssh-setup"
	}
	for _, namespace := range []string{"user", "mnt"} {
		self, err := os.Readlink("/proc/self/ns/" + namespace)
		if err != nil {
			return nil, fmt.Errorf("read Product adapter %s namespace: %w", namespace, err)
		}
		initial, err := os.Readlink("/proc/1/ns/" + namespace)
		if err != nil {
			return nil, fmt.Errorf("read initial %s namespace: %w", namespace, err)
		}
		if self != initial {
			return nil, fmt.Errorf("Product adapter refuses caller-created %s namespace", namespace)
		}
	}
	running, err := os.Open("/proc/self/exe")
	if err != nil {
		return nil, fmt.Errorf("open running Product executable: %w", err)
	}
	closeOnError := func(values ...*os.File) {
		for _, value := range values {
			if value != nil {
				_ = value.Close()
			}
		}
	}
	expectedFile, err := os.Open(expected)
	if err != nil {
		closeOnError(running)
		return nil, fmt.Errorf("open manifest Product executable: %w", err)
	}
	runningInfo, err := running.Stat()
	if err != nil {
		closeOnError(running, expectedFile)
		return nil, err
	}
	expectedInfo, err := expectedFile.Stat()
	if err != nil {
		closeOnError(running, expectedFile)
		return nil, err
	}
	if !os.SameFile(runningInfo, expectedInfo) {
		closeOnError(running, expectedFile)
		return nil, fmt.Errorf("running Product executable does not match manifest executable")
	}
	currentPath := filepath.Join("/run/current-system/sw/bin", name)
	currentFile, err := os.Open(currentPath)
	if err != nil {
		closeOnError(running, expectedFile)
		return nil, fmt.Errorf("open current-system Product executable %s: %w", currentPath, err)
	}
	currentInfo, err := currentFile.Stat()
	if err != nil {
		closeOnError(running, expectedFile, currentFile)
		return nil, err
	}
	if !os.SameFile(currentInfo, expectedInfo) {
		closeOnError(running, expectedFile, currentFile)
		return nil, fmt.Errorf("Product authority locator and current-system executable belong to different generations")
	}
	return []*os.File{running, expectedFile, currentFile}, nil
}

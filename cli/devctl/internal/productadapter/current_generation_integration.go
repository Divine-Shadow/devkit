//go:build devkitintegration

package productadapter

import (
	"fmt"
	"os"
)

func holdCurrentGeneration(role Role, adapter AdapterManifest) ([]*os.File, error) {
	expected := adapter.ExecutablePath
	if role == RoleProxy {
		expected = adapter.ProxyHelperPath
	}
	running, err := os.Open("/proc/self/exe")
	if err != nil {
		return nil, err
	}
	expectedFile, err := os.Open(expected)
	if err != nil {
		_ = running.Close()
		return nil, err
	}
	runningInfo, err := running.Stat()
	if err != nil {
		_ = running.Close()
		_ = expectedFile.Close()
		return nil, err
	}
	expectedInfo, err := expectedFile.Stat()
	if err != nil {
		_ = running.Close()
		_ = expectedFile.Close()
		return nil, err
	}
	if !os.SameFile(runningInfo, expectedInfo) {
		_ = running.Close()
		_ = expectedFile.Close()
		return nil, fmt.Errorf("running Product fixture executable does not match manifest executable")
	}
	return []*os.File{running, expectedFile}, nil
}

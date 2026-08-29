package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
)

const ManifestVersion = 1

type Manifest struct {
	Version               int    `json:"version"`
	Runtime               string `json:"runtime"`
	Project               string `json:"project"`
	Repo                  string `json:"repo"`
	Count                 int    `json:"count"`
	HostWorktreeRoot      string `json:"host_worktree_root"`
	HostStateRoot         string `json:"host_state_root"`
	SandboxWorktreeRoot   string `json:"sandbox_worktree_root"`
	SandboxStateRoot      string `json:"sandbox_state_root"`
	WorktreeContainerRoot string `json:"worktree_container_root"`
	StateContainerRoot    string `json:"state_container_root"`
	BaseBranch            string `json:"base_branch,omitempty"`
	BranchPrefix          string `json:"branch_prefix,omitempty"`
	BrokerEndpoint        string `json:"broker_endpoint,omitempty"`
	Agents                []Spec `json:"agents"`
}

func NewManifest(project, repo string, agents []Spec, paths Paths, baseBranch, branchPrefix, brokerEndpoint string) Manifest {
	project = strings.TrimSpace(project)
	if project == "" {
		project = "dev-all"
	}
	repo = strings.TrimSpace(repo)
	if repo == "" {
		repo = "ouroboros-ide"
	}
	copied := append([]Spec{}, agents...)
	return Manifest{
		Version:               ManifestVersion,
		Runtime:               "native",
		Project:               project,
		Repo:                  repo,
		Count:                 len(copied),
		HostWorktreeRoot:      paths.HostWorktreeRoot,
		HostStateRoot:         paths.HostStateRoot,
		SandboxWorktreeRoot:   paths.SandboxWorktreeRoot,
		SandboxStateRoot:      paths.SandboxStateRoot,
		WorktreeContainerRoot: paths.SandboxWorktreeRoot,
		StateContainerRoot:    paths.SandboxStateRoot,
		BaseBranch:            strings.TrimSpace(baseBranch),
		BranchPrefix:          strings.TrimSpace(branchPrefix),
		BrokerEndpoint:        strings.TrimSpace(brokerEndpoint),
		Agents:                copied,
	}
}

func ManifestPath(hostStateRoot, project string) string {
	hostStateRoot = strings.TrimSpace(hostStateRoot)
	if hostStateRoot == "" {
		hostStateRoot = "."
	}
	project = strings.TrimSpace(project)
	if project == "" {
		project = "dev-all"
	}
	return filepath.Join(hostStateRoot, "manifests", project+".json")
}

func writeManifestUnlocked(path string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir manifest dir %s: %w", filepath.Dir(path), err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".manifest-*")
	if err != nil {
		return fmt.Errorf("create temporary manifest beside %s: %w", path, err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set temporary manifest mode for %s: %w", path, err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary manifest for %s: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary manifest for %s: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary manifest for %s: %w", path, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install manifest %s atomically: %w", path, err)
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open manifest directory %s after install: %w", filepath.Dir(path), err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync manifest directory %s after install: %w", filepath.Dir(path), err)
	}
	return nil
}

func withManifestWriteLock(path string, effect func() error) error {
	lockPath := path + ".write.lock"
	fd, err := syscall.Open(lockPath, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("open native manifest write lock %s: %w", lockPath, err)
	}
	lock := os.NewFile(uintptr(fd), lockPath)
	if lock == nil {
		_ = syscall.Close(fd)
		return fmt.Errorf("open native manifest write lock %s: invalid file descriptor", lockPath)
	}
	defer lock.Close()
	info, err := lock.Stat()
	if err != nil {
		return fmt.Errorf("inspect native manifest write lock %s: %w", lockPath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("native manifest write lock %s must be a regular file", lockPath)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire native manifest write lock %s: %w", lockPath, err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return effect()
}

func readManifestForCAS(path string) (Manifest, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return Manifest{}, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return Manifest{}, fmt.Errorf("read native manifest %s: invalid file descriptor", path)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Manifest{}, err
	}
	if !info.Mode().IsRegular() {
		return Manifest{}, fmt.Errorf("native manifest %s must be a regular file", path)
	}
	var manifest Manifest
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON value")
		}
		return Manifest{}, err
	}
	return manifest, nil
}

func WriteManifest(path string, manifest Manifest, dryRun bool) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("manifest path is required")
	}
	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir manifest dir %s: %w", filepath.Dir(path), err)
	}
	return withManifestWriteLock(path, func() error {
		return writeManifestUnlocked(path, manifest)
	})
}

// CompareAndSwapManifest serializes every package-owned manifest writer on
// the same lock and installs replacement only when the current strict value
// still equals expected.
func CompareAndSwapManifest(path string, expected, replacement Manifest, dryRun bool) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("manifest path is required")
	}
	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir manifest dir %s: %w", filepath.Dir(path), err)
	}
	return withManifestWriteLock(path, func() error {
		actual, err := readManifestForCAS(path)
		if err != nil {
			return fmt.Errorf("read native manifest for compare-and-swap: %w", err)
		}
		if !reflect.DeepEqual(actual, expected) {
			return fmt.Errorf("native manifest changed before compare-and-swap")
		}
		if err := writeManifestUnlocked(path, replacement); err != nil {
			return err
		}
		installed, err := readManifestForCAS(path)
		if err != nil {
			return fmt.Errorf("verify native manifest compare-and-swap: %w", err)
		}
		if !reflect.DeepEqual(installed, replacement) {
			return fmt.Errorf("native manifest compare-and-swap did not verify")
		}
		return nil
	})
}

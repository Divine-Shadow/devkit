package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func WriteManifest(path string, manifest Manifest, dryRun bool) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("manifest path is required")
	}
	if dryRun {
		return nil
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir manifest dir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write manifest %s: %w", path, err)
	}
	return nil
}

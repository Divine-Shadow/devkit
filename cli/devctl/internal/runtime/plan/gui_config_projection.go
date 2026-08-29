package plan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	guiCodexConfigProjectionSchema    = "devkit/gui-codex-config-projections/v1"
	guiCodexConfigProjectionStoreRoot = "/nix/store"
)

// guiCodexConfigProjectionManifestPath is package-owned authority. It is a
// variable only so package tests can point at an isolated source-derived
// manifest; native callers cannot replace it with an argument or environment
// variable.
var guiCodexConfigProjectionManifestPath = "/etc/fleet/source/codex-config-projections.json"

// GUITargetConfigProjection is the immutable config identity selected for one
// GUI target. Geometry is validated while the plan is built and intentionally
// omitted here so launch consumers cannot reinterpret it.
type GUITargetConfigProjection struct {
	TargetID      string `json:"targetId"`
	ConfigProfile string `json:"configProfile"`
	Source        string `json:"source"`
	SourceSHA256  string `json:"sourceSha256"`
}

type guiCodexConfigProjectionManifest struct {
	SchemaVersion string                           `json:"schemaVersion"`
	Projections   []guiCodexConfigProjectionRecord `json:"projections"`
}

type guiCodexConfigProjectionRecord struct {
	TargetID      string `json:"targetId"`
	ConfigProfile string `json:"configProfile"`
	Project       string `json:"project"`
	Repo          string `json:"repo"`
	AgentIndex    int    `json:"agentIndex"`
	WorkspaceRoot string `json:"workspaceRoot"`
	HostWorktree  string `json:"hostWorktree"`
	HostHome      string `json:"hostHome"`
	Source        string `json:"source"`
	SourceSHA256  string `json:"sourceSha256"`
}

type guiTargetGeometry struct {
	Project       string
	Repo          string
	AgentIndex    int
	WorkspaceRoot string
	HostWorktree  string
	HostHome      string
}

func guiTargetGeometryForPlan(p Plan) guiTargetGeometry {
	workspaceRoot := p.HostWorkspaceRoot
	if workspaceRoot == "" && strings.TrimSpace(p.Agent.HostWorktree) != "" {
		workspaceRoot = filepath.Dir(p.Agent.HostWorktree)
	}
	return guiTargetGeometry{
		Project:       p.Agent.ID.Project,
		Repo:          p.Agent.ID.Repo,
		AgentIndex:    p.Agent.ID.Index,
		WorkspaceRoot: workspaceRoot,
		HostWorktree:  p.Agent.HostWorktree,
		HostHome:      p.Agent.HostHome,
	}
}

func loadGUITargetConfigProjection(targetID string, geometry guiTargetGeometry) (GUITargetConfigProjection, error) {
	return loadGUITargetConfigProjectionFrom(
		guiCodexConfigProjectionManifestPath,
		guiCodexConfigProjectionStoreRoot,
		targetID,
		geometry,
	)
}

func loadGUITargetConfigProjectionFrom(manifestPath, storeRoot, targetID string, geometry guiTargetGeometry) (GUITargetConfigProjection, error) {
	rawTargetID := targetID
	targetID = strings.TrimSpace(rawTargetID)
	if targetID == "" || targetID != rawTargetID {
		return GUITargetConfigProjection{}, fmt.Errorf("GUI target ID is empty or non-canonical")
	}
	manifestPath = filepath.Clean(strings.TrimSpace(manifestPath))
	if !filepath.IsAbs(manifestPath) {
		return GUITargetConfigProjection{}, fmt.Errorf("GUI Codex config projection manifest path must be absolute: %s", manifestPath)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return GUITargetConfigProjection{}, fmt.Errorf("read GUI Codex config projection manifest %s: %w", manifestPath, err)
	}
	var manifest guiCodexConfigProjectionManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return GUITargetConfigProjection{}, fmt.Errorf("decode GUI Codex config projection manifest %s: %w", manifestPath, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON value")
		}
		return GUITargetConfigProjection{}, fmt.Errorf("decode GUI Codex config projection manifest %s: %w", manifestPath, err)
	}
	if manifest.SchemaVersion != guiCodexConfigProjectionSchema {
		return GUITargetConfigProjection{}, fmt.Errorf(
			"GUI Codex config projection manifest %s schemaVersion = %q, want %q",
			manifestPath,
			manifest.SchemaVersion,
			guiCodexConfigProjectionSchema,
		)
	}

	seen := make(map[string]struct{}, len(manifest.Projections))
	var selected *guiCodexConfigProjectionRecord
	for i := range manifest.Projections {
		record := &manifest.Projections[i]
		if strings.TrimSpace(record.TargetID) == "" {
			return GUITargetConfigProjection{}, fmt.Errorf("GUI Codex config projection record %d has an empty targetId", i)
		}
		if record.TargetID != strings.TrimSpace(record.TargetID) {
			return GUITargetConfigProjection{}, fmt.Errorf("GUI Codex config projection record %d targetId is not canonical: %q", i, record.TargetID)
		}
		if _, ok := seen[record.TargetID]; ok {
			return GUITargetConfigProjection{}, fmt.Errorf("GUI Codex config projection manifest has duplicate targetId %q", record.TargetID)
		}
		seen[record.TargetID] = struct{}{}
		if record.TargetID == targetID {
			selected = record
		}
	}
	if selected == nil {
		return GUITargetConfigProjection{}, fmt.Errorf("GUI Codex config projection targetId %q is missing", targetID)
	}
	if err := validateGUITargetConfigProjectionRecord(*selected, geometry, storeRoot); err != nil {
		return GUITargetConfigProjection{}, err
	}
	return GUITargetConfigProjection{
		TargetID:      selected.TargetID,
		ConfigProfile: selected.ConfigProfile,
		Source:        selected.Source,
		SourceSHA256:  selected.SourceSHA256,
	}, nil
}

func validateGUITargetConfigProjectionRecord(record guiCodexConfigProjectionRecord, geometry guiTargetGeometry, storeRoot string) error {
	for _, field := range []struct {
		name string
		got  string
		want string
	}{
		{name: "project", got: record.Project, want: geometry.Project},
		{name: "repo", got: record.Repo, want: geometry.Repo},
		{name: "workspaceRoot", got: record.WorkspaceRoot, want: geometry.WorkspaceRoot},
		{name: "hostWorktree", got: record.HostWorktree, want: geometry.HostWorktree},
		{name: "hostHome", got: record.HostHome, want: geometry.HostHome},
	} {
		if field.got != field.want {
			return fmt.Errorf(
				"GUI Codex config projection targetId %q %s geometry mismatch: got %q, want %q",
				record.TargetID,
				field.name,
				field.got,
				field.want,
			)
		}
	}
	if record.AgentIndex != geometry.AgentIndex {
		return fmt.Errorf(
			"GUI Codex config projection targetId %q agentIndex geometry mismatch: got %d, want %d",
			record.TargetID,
			record.AgentIndex,
			geometry.AgentIndex,
		)
	}
	if strings.TrimSpace(record.ConfigProfile) == "" || record.ConfigProfile != strings.TrimSpace(record.ConfigProfile) {
		return fmt.Errorf("GUI Codex config projection targetId %q has an empty or non-canonical configProfile", record.TargetID)
	}
	if err := validateGUITargetConfigSourceIdentity(record.Source, record.SourceSHA256, storeRoot); err != nil {
		return fmt.Errorf("GUI Codex config projection targetId %q: %w", record.TargetID, err)
	}
	return nil
}

func validateGUITargetConfigSourceIdentity(source, expectedSHA256, storeRoot string) error {
	rawStoreRoot := storeRoot
	storeRoot = strings.TrimSpace(storeRoot)
	rawSource := source
	source = strings.TrimSpace(rawSource)
	if storeRoot == "" || storeRoot != rawStoreRoot || !filepath.IsAbs(storeRoot) || filepath.Clean(storeRoot) != storeRoot ||
		source != rawSource || !filepath.IsAbs(source) || filepath.Clean(source) != source {
		return fmt.Errorf("Codex config source must be an absolute canonical path beneath %s: %s", storeRoot, source)
	}
	rel, err := filepath.Rel(storeRoot, source)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("Codex config source must be beneath %s: %s", storeRoot, source)
	}
	if len(expectedSHA256) != sha256.Size*2 || strings.ToLower(expectedSHA256) != expectedSHA256 {
		return fmt.Errorf("Codex config sourceSha256 must be 64 lowercase hexadecimal characters")
	}
	if _, err := hex.DecodeString(expectedSHA256); err != nil {
		return fmt.Errorf("Codex config sourceSha256 must be 64 lowercase hexadecimal characters")
	}
	return nil
}

// ReadValidatedGUITargetConfigSource reselects the plan's target from the
// fixed manifest and verifies its immutable source immediately before launch.
func ReadValidatedGUITargetConfigSource(p Plan) ([]byte, error) {
	if p.GUITargetConfig == nil {
		return nil, nil
	}
	return readValidatedGUITargetConfigSourceFrom(
		p,
		guiCodexConfigProjectionManifestPath,
		guiCodexConfigProjectionStoreRoot,
	)
}

func readValidatedGUITargetConfigSourceFrom(p Plan, manifestPath, storeRoot string) ([]byte, error) {
	if p.GUITargetConfig == nil {
		return nil, nil
	}
	selected, err := loadGUITargetConfigProjectionFrom(
		manifestPath,
		storeRoot,
		p.GUITargetConfig.TargetID,
		guiTargetGeometryForPlan(p),
	)
	if err != nil {
		return nil, err
	}
	if selected != *p.GUITargetConfig {
		return nil, fmt.Errorf("GUI Codex config projection targetId %q changed after plan construction", p.GUITargetConfig.TargetID)
	}
	return readValidatedGUITargetConfigSource(selected, storeRoot)
}

func readValidatedGUITargetConfigSource(projection GUITargetConfigProjection, storeRoot string) ([]byte, error) {
	if err := validateGUITargetConfigSourceIdentity(projection.Source, projection.SourceSHA256, storeRoot); err != nil {
		return nil, err
	}
	source, err := openCanonicalRegularFileNoFollow(projection.Source)
	if err != nil {
		return nil, fmt.Errorf("inspect Codex config source %s: %w", projection.Source, err)
	}
	defer source.Close()
	data, err := io.ReadAll(source)
	if err != nil {
		return nil, fmt.Errorf("read Codex config source %s: %w", projection.Source, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("Codex config source %s is empty", projection.Source)
	}
	digest := sha256.Sum256(data)
	actualSHA256 := hex.EncodeToString(digest[:])
	if actualSHA256 != projection.SourceSHA256 {
		return nil, fmt.Errorf(
			"Codex config source %s SHA-256 mismatch: got %s, want %s",
			projection.Source,
			actualSHA256,
			projection.SourceSHA256,
		)
	}
	return data, nil
}

// openCanonicalRegularFileNoFollow resolves every component from the filesystem
// root with O_NOFOLLOW and returns the already-opened final file. Callers read
// from this handle instead of reopening the pathname, so an exchange or rename
// after validation cannot redirect the read to different bytes.
func openCanonicalRegularFileNoFollow(path string) (*os.File, error) {
	rawPath := path
	path = strings.TrimSpace(path)
	if path == "" || path != rawPath || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
		return nil, fmt.Errorf("path must be absolute and canonical: %s", path)
	}
	components := strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
	rootFD, err := syscall.Open(string(filepath.Separator), syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open filesystem root: %w", err)
	}
	current := os.NewFile(uintptr(rootFD), string(filepath.Separator))
	if current == nil {
		_ = syscall.Close(rootFD)
		return nil, fmt.Errorf("open filesystem root: invalid file descriptor")
	}
	for i, component := range components {
		if component == "" || component == "." || component == ".." {
			_ = current.Close()
			return nil, fmt.Errorf("path has a non-canonical component: %s", path)
		}
		flags := syscall.O_RDONLY | syscall.O_CLOEXEC | syscall.O_NOFOLLOW
		if i != len(components)-1 {
			flags |= syscall.O_DIRECTORY
		}
		nextFD, openErr := syscall.Openat(int(current.Fd()), component, flags, 0)
		_ = current.Close()
		if openErr != nil {
			if errors.Is(openErr, syscall.ELOOP) {
				return nil, fmt.Errorf("path component %q must be a regular non-symlink file or directory", component)
			}
			return nil, fmt.Errorf("open component %q without following links: %w", component, openErr)
		}
		current = os.NewFile(uintptr(nextFD), component)
		if current == nil {
			_ = syscall.Close(nextFD)
			return nil, fmt.Errorf("open component %q: invalid file descriptor", component)
		}
	}
	info, err := current.Stat()
	if err != nil {
		_ = current.Close()
		return nil, fmt.Errorf("stat opened file: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = current.Close()
		return nil, fmt.Errorf("must be a regular non-symlink file")
	}
	return current, nil
}

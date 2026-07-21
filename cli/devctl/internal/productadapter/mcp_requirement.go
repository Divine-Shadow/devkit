package productadapter

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const (
	MCPRequirementSchema   = "devkit/product-mcp-requirement/v1"
	maximumMCPArtifactSize = 64 * 1024
	maximumMCPServers      = 32
	maximumMCPTools        = 2048
)

// MCPRequirement is an immutable, constructor-supplied description of the
// complete MCP server/tool inventory required by a Product consumer. Devkit
// validates and compares this artifact; it does not choose Product tool names.
type MCPRequirement struct {
	SchemaVersion string                 `json:"schemaVersion"`
	Servers       []MCPServerRequirement `json:"servers"`
}

type MCPServerRequirement struct {
	Name  string   `json:"name"`
	Tools []string `json:"tools"`
}

// LoadMCPRequirement reads one bounded immutable artifact, verifies its exact
// byte digest from the authoritative manifest, and validates deterministic
// ordering so the same semantic inventory has one digest.
func LoadMCPRequirement(path, expectedDigest string) (MCPRequirement, string, error) {
	var requirement MCPRequirement
	if err := validateMCPRequirementPath(path); err != nil {
		return requirement, "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return requirement, "", fmt.Errorf("open Product MCP requirement: %w", err)
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maximumMCPArtifactSize+1))
	if err != nil {
		return requirement, "", fmt.Errorf("read Product MCP requirement: %w", err)
	}
	if len(payload) > maximumMCPArtifactSize {
		return requirement, "", fmt.Errorf("Product MCP requirement exceeds %d bytes", maximumMCPArtifactSize)
	}
	fileDigest := sha256.Sum256(payload)
	actualDigest := hex.EncodeToString(fileDigest[:])
	if len(expectedDigest) != 64 || actualDigest != expectedDigest {
		return requirement, "", fmt.Errorf("Product MCP requirement digest does not match authority")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&requirement); err != nil {
		return requirement, "", fmt.Errorf("decode Product MCP requirement: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return requirement, "", fmt.Errorf("Product MCP requirement contains trailing data")
	}
	if err := requirement.Validate(); err != nil {
		return requirement, "", err
	}
	semanticDigest, err := requirement.SemanticDigest()
	if err != nil {
		return requirement, "", err
	}
	return requirement, semanticDigest, nil
}

// The composed authority loader proves the artifact resolves to an immutable
// Nix-store leaf. The subordinate readiness process repeats the properties it
// can observe inside its sandbox and binds the bytes to the authority digest.
func validateMCPRequirementPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("Product MCP requirement path is not exact and absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect Product MCP requirement: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o222 != 0 {
		return fmt.Errorf("Product MCP requirement must be a read-only non-symlink regular file")
	}
	return nil
}

func (requirement MCPRequirement) Validate() error {
	if requirement.SchemaVersion != MCPRequirementSchema {
		return fmt.Errorf("Product MCP requirement schema is %q", requirement.SchemaVersion)
	}
	if len(requirement.Servers) == 0 || len(requirement.Servers) > maximumMCPServers {
		return fmt.Errorf("Product MCP requirement must contain 1..%d servers", maximumMCPServers)
	}
	toolCount := 0
	previousServer := ""
	for _, server := range requirement.Servers {
		if err := validateMCPName("server", server.Name); err != nil {
			return err
		}
		if previousServer != "" && server.Name <= previousServer {
			return fmt.Errorf("Product MCP requirement servers must be unique and byte-sorted")
		}
		previousServer = server.Name
		if len(server.Tools) == 0 {
			return fmt.Errorf("Product MCP requirement server %q has no tools", server.Name)
		}
		previousTool := ""
		for _, tool := range server.Tools {
			if err := validateMCPName("tool", tool); err != nil {
				return err
			}
			if previousTool != "" && tool <= previousTool {
				return fmt.Errorf("Product MCP requirement tools for %q must be unique and byte-sorted", server.Name)
			}
			previousTool = tool
			toolCount++
		}
	}
	if toolCount > maximumMCPTools {
		return fmt.Errorf("Product MCP requirement exceeds %d tools", maximumMCPTools)
	}
	return nil
}

func (requirement MCPRequirement) SemanticDigest() (string, error) {
	if err := requirement.Validate(); err != nil {
		return "", err
	}
	payload, err := json.Marshal(requirement)
	if err != nil {
		return "", fmt.Errorf("encode Product MCP requirement: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func (requirement MCPRequirement) Counts() (servers, tools int) {
	servers = len(requirement.Servers)
	for _, server := range requirement.Servers {
		tools += len(server.Tools)
	}
	return servers, tools
}

// NewObservedMCPRequirement converts a typed Codex observation into the same
// deterministic shape as the immutable requirement. It is intentionally
// generic: callers provide every server and tool name observed from Codex.
func NewObservedMCPRequirement(servers map[string][]string) (MCPRequirement, error) {
	result := MCPRequirement{SchemaVersion: MCPRequirementSchema}
	serverNames := make([]string, 0, len(servers))
	for name := range servers {
		serverNames = append(serverNames, name)
	}
	sort.Strings(serverNames)
	for _, name := range serverNames {
		tools := append([]string(nil), servers[name]...)
		sort.Strings(tools)
		result.Servers = append(result.Servers, MCPServerRequirement{Name: name, Tools: tools})
	}
	if err := result.Validate(); err != nil {
		return MCPRequirement{}, err
	}
	return result, nil
}

func validateMCPName(kind, value string) error {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 256 {
		return fmt.Errorf("Product MCP %s name is not exact", kind)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("Product MCP %s name contains a control character", kind)
		}
	}
	return nil
}

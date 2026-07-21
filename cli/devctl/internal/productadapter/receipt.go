package productadapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

type PathIdentity struct {
	Path   string `json:"path"`
	Mode   uint32 `json:"mode"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	Digest string `json:"digest,omitempty"`
}

type ProxyIdentity struct {
	Path   string `json:"path"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type BindIdentity struct {
	Source   PathIdentity `json:"source"`
	Target   string       `json:"target"`
	Mode     string       `json:"mode"`
	Required bool         `json:"required"`
}

type ReadinessEvidence struct {
	Name   string                  `json:"name"`
	Status string                  `json:"status"`
	Detail string                  `json:"detail,omitempty"`
	Result *ProductReadinessResult `json:"result,omitempty"`
}

type ProductReadinessResult struct {
	SchemaVersion           string `json:"schema_version"`
	AppServerAlive          bool   `json:"app_server_alive"`
	InitializeReady         bool   `json:"initialize_ready"`
	MCPStatusRead           bool   `json:"mcp_status_read"`
	MCPRequirementPath      string `json:"mcp_requirement_path"`
	MCPRequirementDigest    string `json:"mcp_requirement_digest"`
	MCPInventoryDigest      string `json:"mcp_inventory_digest"`
	MCPServerCount          int    `json:"mcp_server_count"`
	MCPToolCount            int    `json:"mcp_tool_count"`
	EphemeralThreadCreated  bool   `json:"ephemeral_thread_created"`
	EphemeralThreadReadBack bool   `json:"ephemeral_thread_read_back"`
	ApprovalPolicy          string `json:"approval_policy"`
	SandboxPolicy           string `json:"sandbox_policy"`
}

type Geometry struct {
	CandidateRoot string `json:"candidate_root"`
	AgentRoot     string `json:"agent_root"`
	Worktree      string `json:"worktree"`
	CommonDir     string `json:"common_dir"`
	GitDir        string `json:"git_dir"`
	Home          string `json:"home"`
	State         string `json:"state"`
	Receipt       string `json:"receipt"`
}

type Receipt struct {
	SchemaVersion           string                  `json:"schema_version"`
	Status                  string                  `json:"status"`
	ManifestPath            string                  `json:"manifest_path"`
	ManifestDigest          string                  `json:"manifest_digest"`
	AdapterExecutable       string                  `json:"adapter_executable"`
	ProxyHelper             string                  `json:"proxy_helper"`
	ProductRevision         string                  `json:"product_revision"`
	Repo                    string                  `json:"repo"`
	Origin                  string                  `json:"origin"`
	Count                   int                     `json:"count"`
	Index                   int                     `json:"index"`
	Geometry                Geometry                `json:"geometry"`
	Artifacts               map[string]string       `json:"artifacts"`
	Paths                   map[string]PathIdentity `json:"paths,omitempty"`
	Binds                   []BindIdentity          `json:"binds,omitempty"`
	Proxy                   ProxyIdentity           `json:"proxy"`
	ProxyCleanup            string                  `json:"proxy_cleanup"`
	GitMetadataDigest       string                  `json:"git_metadata_digest,omitempty"`
	MountPolicyIdentity     string                  `json:"mount_policy_identity"`
	ReadinessEvidence       []ReadinessEvidence     `json:"readiness_evidence,omitempty"`
	ReadinessRuntime        bool                    `json:"readiness_runtime"`
	ReadinessRepository     bool                    `json:"readiness_repository"`
	CredentialHandleDigests map[string]string       `json:"credential_handle_digests"`
}

func NewReceipt(
	authority Authority,
	consumer ConsumerManifest,
	count, index int,
	geometry Geometry,
) Receipt {
	adapter := authority.Adapter
	return Receipt{
		SchemaVersion:     ReceiptSchema,
		Status:            "claimed",
		ManifestPath:      authority.ManifestPath,
		ManifestDigest:    authority.ManifestDigest,
		AdapterExecutable: adapter.ExecutablePath,
		ProxyHelper:       adapter.ProxyHelperPath,
		ProductRevision:   authority.ProductRev,
		Repo:              ProductRepo,
		Origin:            adapter.ProductOrigin,
		Count:             count,
		Index:             index,
		Geometry:          geometry,
		Artifacts: map[string]string{
			"git":                   adapter.GitPath,
			"env":                   adapter.EnvPath,
			"ssh":                   adapter.SSHPath,
			"known_hosts":           adapter.KnownHostsPath,
			"runtime_launcher":      adapter.RuntimeLauncherPath,
			"bubblewrap":            adapter.BubblewrapPath,
			"broker":                adapter.BrokerPath,
			"egress_allowlist":      adapter.EgressAllowlistPath,
			"codex_config":          adapter.CodexConfigPath,
			"governance_env":        adapter.GovernanceEnvPath,
			"governance_repo":       adapter.GovernanceRepoConfigPath,
			"governance_rules":      adapter.GovernanceRulesPath,
			"shell_hook":            adapter.ShellHookPath,
			"codex_executable":      adapter.CodexExecutablePath,
			"readiness_probe":       adapter.ReadinessExecutablePath,
			"mcp_requirement":       adapter.MCPRequirementPath,
			"mount_policy_contract": adapter.MountPolicyContractPath,
			"ssh_identity":          consumer.SSHIdentityPath,
			"ssh_public_key":        consumer.SSHPublicKeyPath,
			"codex_auth":            consumer.CodexAuthPath,
		},
		MountPolicyIdentity: adapter.MountPolicyIdentity,
		Proxy: ProxyIdentity{
			Path: authority.ProxySocketPath(index),
		},
		ProxyCleanup: "not-started",
	}
}

func WriteReceipt(path string, receipt Receipt, createOnly bool) error {
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := writeFileAtomic(path, data, 0o600, createOnly); err != nil {
		return fmt.Errorf("persist Product construction receipt: %w", err)
	}
	return nil
}

func ReadReceipt(authority Authority, count, index int) (Receipt, error) {
	path := authority.ReceiptPath(index)
	if err := requirePlainPath(path, false); err != nil {
		return Receipt{}, fmt.Errorf("validate Product receipt path: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Receipt{}, fmt.Errorf("inspect Product construction receipt: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		return Receipt{}, fmt.Errorf("Product construction receipt must be a plain 0600 file")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Geteuid() {
		return Receipt{}, fmt.Errorf("Product construction receipt must be owned by the executing identity")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Receipt{}, err
	}
	var receipt Receipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return Receipt{}, fmt.Errorf("parse Product construction receipt: %w", err)
	}
	if err := ValidateReceipt(authority, count, index, receipt); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func ValidateReceipt(authority Authority, count, index int, receipt Receipt) error {
	if receipt.SchemaVersion != ReceiptSchema {
		return fmt.Errorf("Product construction receipt has invalid schema")
	}
	if receipt.ManifestPath != authority.ManifestPath ||
		receipt.ManifestDigest != authority.ManifestDigest ||
		receipt.AdapterExecutable != authority.Adapter.ExecutablePath ||
		receipt.ProxyHelper != authority.Adapter.ProxyHelperPath ||
		receipt.ProductRevision != authority.ProductRev ||
		receipt.Repo != ProductRepo ||
		receipt.Origin != authority.Adapter.ProductOrigin ||
		receipt.Count != count ||
		receipt.Index != index ||
		receipt.MountPolicyIdentity != authority.Adapter.MountPolicyIdentity {
		return fmt.Errorf("Product construction receipt identity does not match current authority")
	}
	if receipt.Geometry.Receipt != authority.ReceiptPath(index) ||
		receipt.Geometry.AgentRoot != authority.AgentRoot(index) ||
		receipt.Proxy.Path != authority.ProxySocketPath(index) {
		return fmt.Errorf("Product construction receipt geometry does not match current authority")
	}
	consumer, err := authority.Consumer(index)
	if err != nil {
		return err
	}
	expectedArtifacts := NewReceipt(authority, consumer, count, index, receipt.Geometry).Artifacts
	if len(receipt.Artifacts) != len(expectedArtifacts) {
		return fmt.Errorf("Product construction receipt artifact set is incomplete")
	}
	for name, expected := range expectedArtifacts {
		if receipt.Artifacts[name] != expected {
			return fmt.Errorf("Product construction receipt artifact %s does not match current authority", name)
		}
	}
	expectedCredentialDigests, err := CaptureCredentialHandleDigests(consumer)
	if err != nil {
		return err
	}
	if len(receipt.CredentialHandleDigests) != len(expectedCredentialDigests) {
		return fmt.Errorf("Product credential handle digest set is incomplete")
	}
	for name, expected := range expectedCredentialDigests {
		if receipt.CredentialHandleDigests[name] != expected {
			return fmt.Errorf("Product credential handle %s changed", name)
		}
	}
	return nil
}

func CaptureCredentialHandleDigests(consumer ConsumerManifest) (map[string]string, error) {
	result := make(map[string]string, 3)
	for name, path := range map[string]string{
		"ssh_identity":   consumer.SSHIdentityPath,
		"ssh_public_key": consumer.SSHPublicKeyPath,
		"codex_auth":     consumer.CodexAuthPath,
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read Product credential handle %s: %w", name, err)
		}
		result[name] = sha256Hex(data)
	}
	return result, nil
}

func CapturePathIdentity(path string, digest bool) (PathIdentity, error) {
	if err := requirePlainPath(path, true); err != nil {
		return PathIdentity{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return PathIdentity{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return PathIdentity{}, fmt.Errorf("path %s has no Unix identity", path)
	}
	identity := PathIdentity{
		Path:   filepath.Clean(path),
		Mode:   uint32(info.Mode()),
		Device: uint64(stat.Dev),
		Inode:  stat.Ino,
	}
	if digest && info.Mode().IsRegular() {
		data, err := os.ReadFile(path)
		if err != nil {
			return PathIdentity{}, err
		}
		identity.Digest = sha256Hex(data)
	}
	return identity, nil
}

func ValidatePathIdentity(identity PathIdentity) error {
	current, err := CapturePathIdentity(identity.Path, identity.Digest != "")
	if err != nil {
		return err
	}
	if current != identity {
		return fmt.Errorf("path identity changed at %s", identity.Path)
	}
	return nil
}

func SortedPathIdentities(values map[string]PathIdentity) []PathIdentity {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]PathIdentity, 0, len(keys))
	for _, key := range keys {
		result = append(result, values[key])
	}
	return result
}

func CaptureProxyIdentity(path string) (ProxyIdentity, error) {
	if err := requirePlainPath(path, true); err != nil {
		return ProxyIdentity{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return ProxyIdentity{}, err
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
		return ProxyIdentity{}, fmt.Errorf("Product proxy path %s is not a plain Unix socket", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ProxyIdentity{}, fmt.Errorf("Product proxy path %s has no Unix identity", path)
	}
	return ProxyIdentity{Path: filepath.Clean(path), Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}

func ValidateProxyIdentity(identity ProxyIdentity) error {
	current, err := CaptureProxyIdentity(identity.Path)
	if err != nil {
		return err
	}
	if current != identity {
		return fmt.Errorf("Product proxy identity changed at %s", identity.Path)
	}
	return nil
}

func requirePlainPath(path string, allowDirectory bool) error {
	clean := filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("path must be absolute")
	}
	current := string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(clean, current), string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path traverses symlink component %s", current)
		}
		if index < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("path ancestor %s is not a directory", current)
		}
		if index == len(parts)-1 && !allowDirectory && info.IsDir() {
			return fmt.Errorf("path %s must not be a directory", current)
		}
	}
	return nil
}

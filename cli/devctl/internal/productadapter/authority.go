package productadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	ManifestSchema  = "wsl-nix-dev-all-runtime-authority/v1"
	AdapterSchema   = "wsl-nix-devkit-product-adapter/v1"
	ReceiptSchema   = "devkit/product-construction-receipt/v1"
	ReadinessSchema = "devkit/product-codex-app-server-thread-readiness/v1"
)

// These values are package-linker inputs. Product mode is disabled in the
// general Devkit package. The WSL constructor enables it and binds subordinate
// immutable artifact identities; it does not compile a Product revision.
var (
	packageMode                   string
	packageAdapterExecutable      string
	packageGitExecutable          string
	packageEnvExecutable          string
	packageSSHExecutable          string
	packageKnownHosts             string
	packageRuntimeLauncher        string
	packageBubblewrapExecutable   string
	packageBrokerExecutable       string
	packageRuntimeRoot            string
	packageEgressAllowlist        string
	packageCodexConfig            string
	packageGovernanceEnv          string
	packageGovernanceRepoConfig   string
	packageGovernanceRules        string
	packageShellHook              string
	packageCodexExecutable        string
	packageReadinessExecutable    string
	packageFirstExecutable        string
	packageProductProxyExecutable string
)

type Manifest struct {
	SchemaVersion string `json:"schemaVersion"`
	Sources       struct {
		Product struct {
			Rev string `json:"rev"`
		} `json:"product"`
	} `json:"sources"`
	ProductAdapter AdapterManifest `json:"devkitProductAdapter"`
}

type AdapterManifest struct {
	SchemaVersion            string             `json:"schemaVersion"`
	ExecutablePath           string             `json:"executablePath"`
	ProxyHelperPath          string             `json:"proxyHelperPath"`
	GitPath                  string             `json:"gitPath"`
	EnvPath                  string             `json:"envPath"`
	SSHPath                  string             `json:"sshPath"`
	KnownHostsPath           string             `json:"knownHostsPath"`
	RuntimeLauncherPath      string             `json:"runtimeLauncherPath"`
	BubblewrapPath           string             `json:"bubblewrapPath"`
	BrokerPath               string             `json:"brokerPath"`
	RuntimeRoot              string             `json:"runtimeRoot"`
	EgressAllowlistPath      string             `json:"egressAllowlistPath"`
	CodexConfigPath          string             `json:"codexConfigPath"`
	GovernanceEnvPath        string             `json:"governanceEnvPath"`
	GovernanceRepoConfigPath string             `json:"governanceRepoConfigPath"`
	GovernanceRulesPath      string             `json:"governanceRulesPath"`
	ShellHookPath            string             `json:"shellHookPath"`
	CodexExecutablePath      string             `json:"codexExecutablePath"`
	ReadinessExecutablePath  string             `json:"readinessExecutablePath"`
	FirstExecutablePath      string             `json:"firstExecutablePath"`
	ProductOrigin            string             `json:"productOrigin"`
	Count                    int                `json:"count"`
	BaseBranch               string             `json:"baseBranch"`
	BranchPrefix             string             `json:"branchPrefix"`
	UpstreamProxyURL         string             `json:"upstreamProxyUrl"`
	ResolvConfPath           string             `json:"resolvConfPath"`
	NSCDSocketPath           string             `json:"nscdSocketPath"`
	MountPolicyIdentity      string             `json:"mountPolicyIdentity"`
	RuntimeEnvironment       map[string]string  `json:"runtimeEnvironment"`
	Consumers                []ConsumerManifest `json:"consumers"`
	ArtifactDigests          map[string]string  `json:"artifactDigests"`
}

type BindManifest struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Mode     string `json:"mode"`
	Required bool   `json:"required"`
}

type ConsumerManifest struct {
	Index                      int            `json:"index"`
	UID                        int            `json:"uid"`
	CandidateRoot              string         `json:"candidateRoot"`
	AgentRoot                  string         `json:"agentRoot"`
	WorktreePath               string         `json:"worktreePath"`
	CommonDirPath              string         `json:"commonDirPath"`
	HomePath                   string         `json:"homePath"`
	StateRoot                  string         `json:"stateRoot"`
	ReceiptPath                string         `json:"receiptPath"`
	ProxySocketPath            string         `json:"proxySocketPath"`
	BrokerSocketPath           string         `json:"brokerSocketPath"`
	SandboxWorktreePath        string         `json:"sandboxWorktreePath"`
	SandboxHomePath            string         `json:"sandboxHomePath"`
	SandboxStateRoot           string         `json:"sandboxStateRoot"`
	SandboxProxySocketPath     string         `json:"sandboxProxySocketPath"`
	SandboxBrokerSocketPath    string         `json:"sandboxBrokerSocketPath"`
	GovernanceEnvTarget        string         `json:"governanceEnvTarget"`
	GovernanceRepoConfigTarget string         `json:"governanceRepoConfigTarget"`
	GovernanceStateRoot        string         `json:"governanceStateRoot"`
	SSHIdentityPath            string         `json:"sshIdentityPath"`
	SSHPublicKeyPath           string         `json:"sshPublicKeyPath"`
	CodexAuthPath              string         `json:"codexAuthPath"`
	Binds                      []BindManifest `json:"binds"`
}

type Authority struct {
	ManifestPath   string
	ManifestDigest string
	ProductRev     string
	Adapter        AdapterManifest
	held           []*os.File
}

type Role string

const (
	RoleAdapter Role = "adapter"
	RoleProxy   Role = "proxy"
)

func Enabled() bool {
	return strings.TrimSpace(packageMode) == "composed"
}

func Load(role Role, consumerIndex int) (Authority, error) {
	if !Enabled() {
		return Authority{}, fmt.Errorf("uncomposed Devkit has no Product authority")
	}
	locator := authorityLocator()
	if strings.TrimSpace(locator) == "" {
		return Authority{}, fmt.Errorf("composed Product adapter has no canonical authority locator")
	}
	manifestFile, manifestPath, err := openAuthorityManifest(locator)
	if err != nil {
		return Authority{}, err
	}
	data, err := io.ReadAll(manifestFile)
	if err != nil {
		_ = manifestFile.Close()
		return Authority{}, fmt.Errorf("read Product authority manifest: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		_ = manifestFile.Close()
		return Authority{}, fmt.Errorf("parse Product authority manifest: %w", err)
	}
	if manifest.SchemaVersion != ManifestSchema {
		_ = manifestFile.Close()
		return Authority{}, fmt.Errorf("Product authority manifest schema is %q", manifest.SchemaVersion)
	}
	if manifest.ProductAdapter.SchemaVersion != AdapterSchema {
		_ = manifestFile.Close()
		return Authority{}, fmt.Errorf("Product adapter manifest schema is %q", manifest.ProductAdapter.SchemaVersion)
	}
	revision := strings.TrimSpace(manifest.Sources.Product.Rev)
	if !isExactRevision(revision) {
		_ = manifestFile.Close()
		return Authority{}, fmt.Errorf("Product authority manifest has invalid Product revision")
	}
	authority := Authority{
		ManifestPath:   manifestPath,
		ManifestDigest: sha256Hex(data),
		ProductRev:     revision,
		Adapter:        manifest.ProductAdapter,
		held:           []*os.File{manifestFile},
	}
	if err := authority.validate(role); err != nil {
		authority.Close()
		return Authority{}, err
	}
	consumer, err := authority.Consumer(consumerIndex)
	if err != nil {
		authority.Close()
		return Authority{}, err
	}
	if consumer.UID != os.Geteuid() {
		authority.Close()
		return Authority{}, fmt.Errorf(
			"Product consumer %d requires uid %d, executing uid is %d",
			consumer.Index,
			consumer.UID,
			os.Geteuid(),
		)
	}
	if err := validateSelectedCredentialHandles(consumer); err != nil {
		authority.Close()
		return Authority{}, err
	}
	held, err := holdCurrentGeneration(role, authority.Adapter)
	if err != nil {
		authority.Close()
		return Authority{}, err
	}
	authority.held = append(authority.held, held...)
	return authority, nil
}

// Close releases the immutable manifest/current-generation descriptors held
// through validation and effect start.
func (authority *Authority) Close() {
	for _, file := range authority.held {
		if file != nil {
			_ = file.Close()
		}
	}
	authority.held = nil
}

func (authority Authority) validate(role Role) error {
	adapter := authority.Adapter
	artifacts := map[string]struct {
		label      string
		path       string
		executable bool
	}{
		"adapter":              {label: "adapter executable", path: adapter.ExecutablePath, executable: true},
		"proxy_helper":         {label: "proxy helper", path: adapter.ProxyHelperPath, executable: true},
		"git":                  {label: "Git", path: adapter.GitPath, executable: true},
		"env":                  {label: "env", path: adapter.EnvPath, executable: true},
		"ssh":                  {label: "OpenSSH", path: adapter.SSHPath, executable: true},
		"known_hosts":          {label: "known-hosts", path: adapter.KnownHostsPath},
		"runtime_launcher":     {label: "runtime launcher", path: adapter.RuntimeLauncherPath, executable: true},
		"bubblewrap":           {label: "bubblewrap", path: adapter.BubblewrapPath, executable: true},
		"broker":               {label: "broker", path: adapter.BrokerPath, executable: true},
		"egress_allowlist":     {label: "egress allowlist", path: adapter.EgressAllowlistPath},
		"codex_config":         {label: "Codex config", path: adapter.CodexConfigPath},
		"governance_env":       {label: "governance environment", path: adapter.GovernanceEnvPath},
		"governance_repo":      {label: "governance repository config", path: adapter.GovernanceRepoConfigPath},
		"governance_rules":     {label: "governance rules", path: adapter.GovernanceRulesPath},
		"shell_hook":           {label: "shell hook", path: adapter.ShellHookPath},
		"codex_executable":     {label: "Codex executable", path: adapter.CodexExecutablePath, executable: true},
		"readiness_executable": {label: "readiness executable", path: adapter.ReadinessExecutablePath, executable: true},
		"first_executable":     {label: "first executable", path: adapter.FirstExecutablePath, executable: true},
		"resolv_conf":          {label: "resolv.conf", path: adapter.ResolvConfPath},
	}
	for _, artifact := range artifacts {
		if err := validateImmutableArtifact(artifact.label, artifact.path, artifact.executable, false); err != nil {
			return err
		}
	}
	if err := validateImmutableArtifact("runtime root", adapter.RuntimeRoot, false, true); err != nil {
		return err
	}
	expected := map[string][2]string{
		"proxy helper":     {adapter.ProxyHelperPath, packageProductProxyExecutable},
		"Git":              {adapter.GitPath, packageGitExecutable},
		"env":              {adapter.EnvPath, packageEnvExecutable},
		"OpenSSH":          {adapter.SSHPath, packageSSHExecutable},
		"known-hosts":      {adapter.KnownHostsPath, packageKnownHosts},
		"runtime launcher": {adapter.RuntimeLauncherPath, packageRuntimeLauncher},
		"bubblewrap":       {adapter.BubblewrapPath, packageBubblewrapExecutable},
		"broker":           {adapter.BrokerPath, packageBrokerExecutable},
		"runtime root":     {adapter.RuntimeRoot, packageRuntimeRoot},
		"egress allowlist": {adapter.EgressAllowlistPath, packageEgressAllowlist},
		"Codex config":     {adapter.CodexConfigPath, packageCodexConfig},
		"governance env":   {adapter.GovernanceEnvPath, packageGovernanceEnv},
		"governance repo":  {adapter.GovernanceRepoConfigPath, packageGovernanceRepoConfig},
		"governance rules": {adapter.GovernanceRulesPath, packageGovernanceRules},
		"shell hook":       {adapter.ShellHookPath, packageShellHook},
		"Codex executable": {adapter.CodexExecutablePath, packageCodexExecutable},
		"readiness probe":  {adapter.ReadinessExecutablePath, packageReadinessExecutable},
		"first executable": {adapter.FirstExecutablePath, packageFirstExecutable},
	}
	for label, pair := range expected {
		if filepath.Clean(strings.TrimSpace(pair[0])) != filepath.Clean(strings.TrimSpace(pair[1])) {
			return fmt.Errorf("Product manifest %s %q does not match composed package %q", label, pair[0], pair[1])
		}
	}
	if len(adapter.ArtifactDigests) != len(artifacts) {
		return fmt.Errorf("Product authority manifest artifact digest set is incomplete")
	}
	for name, artifact := range artifacts {
		expectedDigest := strings.TrimSpace(adapter.ArtifactDigests[name])
		if len(expectedDigest) != 64 {
			return fmt.Errorf("Product authority manifest artifact digest %s is invalid", name)
		}
		data, err := os.ReadFile(artifact.path)
		if err != nil {
			return fmt.Errorf("read Product authority artifact %s: %w", name, err)
		}
		if digest := sha256Hex(data); digest != expectedDigest {
			return fmt.Errorf("Product authority artifact digest %s does not match %s", name, artifact.path)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve running Product package executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("canonicalize running Product package executable: %w", err)
	}
	expectedExecutable := adapter.ExecutablePath
	if role == RoleProxy {
		expectedExecutable = adapter.ProxyHelperPath
	}
	expectedExecutable, err = filepath.EvalSymlinks(expectedExecutable)
	if err != nil {
		return fmt.Errorf("canonicalize manifest Product executable: %w", err)
	}
	if filepath.Clean(executable) != filepath.Clean(expectedExecutable) {
		return fmt.Errorf("running Product executable %s does not match manifest %s", executable, expectedExecutable)
	}
	if strings.TrimSpace(adapter.ProductOrigin) == "" || !authorityPermitsProductOrigin(adapter.ProductOrigin) {
		return fmt.Errorf("Product authority manifest requires the exact authenticated SSH origin")
	}
	if strings.TrimSpace(adapter.BaseBranch) == "" || strings.TrimSpace(adapter.BranchPrefix) == "" {
		return fmt.Errorf("Product authority manifest requires base branch and branch prefix")
	}
	if adapter.Count < 1 || len(adapter.Consumers) != adapter.Count {
		return fmt.Errorf("Product authority manifest requires one exact consumer record per count")
	}
	if adapter.MountPolicyIdentity != "devkit/workspace-egress/v3" {
		return fmt.Errorf("Product authority manifest requires workspace-egress/v3")
	}
	allowedEnvironment := map[string]bool{
		"JAVA_HOME":                true,
		"NIX_SSL_CERT_FILE":        true,
		"NODE_EXTRA_CA_CERTS":      true,
		"PLAYWRIGHT_BROWSERS_PATH": true,
		"SSL_CERT_FILE":            true,
	}
	for key := range adapter.RuntimeEnvironment {
		if !allowedEnvironment[key] {
			return fmt.Errorf("Product runtime environment key %s is not allowlisted", key)
		}
	}
	seen := map[int]bool{}
	seenUID := map[int]bool{}
	seenCredentialHandles := map[string]bool{}
	for _, consumer := range adapter.Consumers {
		if consumer.Index < 1 || consumer.Index > adapter.Count || seen[consumer.Index] {
			return fmt.Errorf("Product authority manifest consumer index set is invalid")
		}
		seen[consumer.Index] = true
		if consumer.UID < 1 || seenUID[consumer.UID] {
			return fmt.Errorf("Product authority manifest requires one distinct uid per consumer")
		}
		seenUID[consumer.UID] = true
		if err := validateConsumerGeometry(consumer); err != nil {
			return fmt.Errorf("validate Product consumer %d: %w", consumer.Index, err)
		}
		for _, path := range []string{
			consumer.SSHIdentityPath,
			consumer.SSHPublicKeyPath,
			consumer.CodexAuthPath,
		} {
			clean := filepath.Clean(strings.TrimSpace(path))
			if !filepath.IsAbs(clean) || clean != path || seenCredentialHandles[clean] {
				return fmt.Errorf("Product consumer credential handles must be exact and distinct")
			}
			seenCredentialHandles[clean] = true
		}
	}
	if raw := strings.TrimSpace(adapter.UpstreamProxyURL); raw != "" {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil {
			return fmt.Errorf("Product authority upstream proxy must be an exact unauthenticated HTTP endpoint")
		}
	}
	return nil
}

func validateConsumerGeometry(consumer ConsumerManifest) error {
	paths := map[string]string{
		"candidate root":                  consumer.CandidateRoot,
		"agent root":                      consumer.AgentRoot,
		"worktree":                        consumer.WorktreePath,
		"common directory":                consumer.CommonDirPath,
		"home":                            consumer.HomePath,
		"state root":                      consumer.StateRoot,
		"receipt":                         consumer.ReceiptPath,
		"proxy socket":                    consumer.ProxySocketPath,
		"broker socket":                   consumer.BrokerSocketPath,
		"sandbox worktree":                consumer.SandboxWorktreePath,
		"sandbox home":                    consumer.SandboxHomePath,
		"sandbox state root":              consumer.SandboxStateRoot,
		"sandbox proxy socket":            consumer.SandboxProxySocketPath,
		"sandbox broker socket":           consumer.SandboxBrokerSocketPath,
		"governance environment target":   consumer.GovernanceEnvTarget,
		"governance repo config target":   consumer.GovernanceRepoConfigTarget,
		"governance mutable state target": consumer.GovernanceStateRoot,
	}
	for label, path := range paths {
		clean := filepath.Clean(path)
		if !filepath.IsAbs(clean) || clean != path {
			return fmt.Errorf("%s is not an exact absolute path", label)
		}
	}
	for label, path := range map[string]string{
		"agent root":        consumer.AgentRoot,
		"worktree":          consumer.WorktreePath,
		"common directory":  consumer.CommonDirPath,
		"home":              consumer.HomePath,
		"state root":        consumer.StateRoot,
		"receipt":           consumer.ReceiptPath,
		"proxy socket":      consumer.ProxySocketPath,
		"broker socket":     consumer.BrokerSocketPath,
		"governance env":    consumer.GovernanceEnvTarget,
		"governance config": consumer.GovernanceRepoConfigTarget,
		"governance state":  consumer.GovernanceStateRoot,
	} {
		if !pathWithin(consumer.CandidateRoot, path) {
			return fmt.Errorf("%s escapes the candidate root", label)
		}
	}
	if filepath.Dir(consumer.ReceiptPath) != consumer.StateRoot ||
		filepath.Dir(consumer.ProxySocketPath) != consumer.StateRoot {
		return fmt.Errorf("receipt and proxy must be direct state-root children")
	}
	if filepath.Dir(consumer.SandboxProxySocketPath) != consumer.SandboxStateRoot ||
		filepath.Base(consumer.SandboxProxySocketPath) != filepath.Base(consumer.ProxySocketPath) ||
		filepath.Dir(consumer.SandboxBrokerSocketPath) != consumer.SandboxStateRoot ||
		filepath.Base(consumer.SandboxBrokerSocketPath) != filepath.Base(consumer.BrokerSocketPath) {
		return fmt.Errorf("sandbox socket projection does not preserve the declared state-root geometry")
	}
	if filepath.Join(consumer.AgentRoot, ProductRepo) != consumer.WorktreePath ||
		filepath.Join(consumer.AgentRoot, ".devkit", "git", ProductRepo+".git") != consumer.CommonDirPath {
		return fmt.Errorf("slot-private Git geometry is not canonical")
	}
	seenTargets := map[string]bool{}
	for _, bind := range consumer.Binds {
		if bind.Mode != "ro" && bind.Mode != "rw" {
			return fmt.Errorf("bind mode %q is invalid", bind.Mode)
		}
		if !filepath.IsAbs(bind.Source) || !filepath.IsAbs(bind.Target) ||
			filepath.Clean(bind.Source) != bind.Source ||
			filepath.Clean(bind.Target) != bind.Target ||
			seenTargets[bind.Target] {
			return fmt.Errorf("bind tuple is not exact and unique")
		}
		seenTargets[bind.Target] = true
	}
	return nil
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (authority Authority) Consumer(index int) (ConsumerManifest, error) {
	for _, consumer := range authority.Adapter.Consumers {
		if consumer.Index == index {
			return consumer, nil
		}
	}
	return ConsumerManifest{}, fmt.Errorf("Product consumer index %d is not declared", index)
}

func (authority Authority) ReceiptPath(index int) string {
	consumer, _ := authority.Consumer(index)
	return consumer.ReceiptPath
}

func (authority Authority) ProxySocketPath(index int) string {
	consumer, _ := authority.Consumer(index)
	return consumer.ProxySocketPath
}

func (authority Authority) AgentRoot(index int) string {
	consumer, _ := authority.Consumer(index)
	return consumer.AgentRoot
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func isExactRevision(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 40 || strings.ToLower(value) != value {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func isProductSSHOrigin(origin string) bool {
	origin = strings.TrimSpace(origin)
	if strings.HasPrefix(origin, "ssh://git@github.com/") ||
		strings.HasPrefix(origin, "ssh://git@ssh.github.com:443/") ||
		strings.HasPrefix(origin, "git@github.com:") {
		trimmed := strings.TrimSuffix(origin, ".git")
		return strings.HasSuffix(trimmed, "/"+ProductRepo) ||
			strings.HasSuffix(trimmed, ":"+ProductRepo)
	}
	return false
}

func validateImmutableArtifact(label, path string, executable, directory bool) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(path) {
		return fmt.Errorf("Product manifest %s must be an absolute immutable artifact", label)
	}
	if err := validateImmutableLeaf(path); err != nil {
		return fmt.Errorf("validate Product manifest %s: %w", label, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect Product manifest %s: %w", label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Product manifest %s must not be a symlink", label)
	}
	if directory {
		if !info.IsDir() {
			return fmt.Errorf("Product manifest %s must be a directory", label)
		}
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("Product manifest %s must be a regular file", label)
	}
	if info.Mode().Perm()&0o222 != 0 {
		return fmt.Errorf("Product manifest %s must be read-only", label)
	}
	if executable && info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("Product manifest %s must be executable", label)
	}
	return nil
}

func validateCredentialHandle(label, path string, uid int) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(path) {
		return fmt.Errorf("Product manifest %s must be an exact absolute credential handle", label)
	}
	if err := requirePlainPath(path, false); err != nil {
		return fmt.Errorf("validate Product manifest %s: %w", label, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect Product manifest %s: %w", label, err)
	}
	allowedMode := os.FileMode(0o600)
	if strings.Contains(strings.ToLower(label), "public") {
		allowedMode = 0o644
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&^allowedMode != 0 {
		return fmt.Errorf("Product manifest %s must be a non-symlink protected regular file", label)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != uid {
		return fmt.Errorf("Product manifest %s is not owned by consumer uid %d", label, uid)
	}
	return nil
}

func validateSelectedCredentialHandles(consumer ConsumerManifest) error {
	seen := map[[2]uint64]string{}
	for label, path := range map[string]string{
		"SSH private identity": consumer.SSHIdentityPath,
		"SSH public identity":  consumer.SSHPublicKeyPath,
		"Codex auth":           consumer.CodexAuthPath,
	} {
		if err := validateCredentialHandle(label, path, consumer.UID); err != nil {
			return fmt.Errorf("validate Product consumer %d: %w", consumer.Index, err)
		}
		device, inode, err := fileIdentity(path)
		if err != nil {
			return err
		}
		identity := [2]uint64{device, inode}
		if previous, exists := seen[identity]; exists {
			return fmt.Errorf("Product consumer credential handle %s aliases %s", path, previous)
		}
		seen[identity] = path
	}
	return nil
}

func fileIdentity(path string) (uint64, uint64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, fmt.Errorf("path %s has no Unix identity", path)
	}
	return uint64(stat.Dev), stat.Ino, nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode, createOnly bool) error {
	parent := filepath.Dir(path)
	temp, err := os.OpenFile(
		filepath.Join(parent, "."+filepath.Base(path)+".tmp"),
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		mode,
	)
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	success := false
	defer func() {
		_ = temp.Close()
		if !success {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if createOnly {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("refusing existing receipt %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	success = true
	directoryFile, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer directoryFile.Close()
	return directoryFile.Sync()
}

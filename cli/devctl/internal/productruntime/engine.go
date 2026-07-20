package productruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"devkit/cli/devctl/internal/productadapter"
	"devkit/cli/devctl/internal/runtime/egressproxy"
	wtx "devkit/cli/devctl/internal/worktrees"
)

// Run is the sole Product effect engine. It consumes one already-parsed
// request and one root-owned manifest authority. It never calls legacy Devkit
// preparation, evaluates a flake, or reads caller runtime/governance settings.
func Run(command productadapter.Command) error {
	authority, err := productadapter.Load(productadapter.RoleAdapter, command.Index)
	if err != nil {
		return err
	}
	defer authority.Close()
	if command.Count != authority.Adapter.Count {
		return fmt.Errorf("Product adapter --count does not match the authoritative manifest")
	}
	consumer, err := authority.Consumer(command.Index)
	if err != nil {
		return err
	}
	switch command.Kind {
	case productadapter.CommandPrepare:
		return prepare(authority, consumer, command)
	case productadapter.CommandExec:
		return execute(authority, consumer, command)
	default:
		return fmt.Errorf("unsupported Product adapter request %q", command.Kind)
	}
}

func prepare(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
	command productadapter.Command,
) (retErr error) {
	claim, geometry, err := claimCandidate(authority, consumer)
	if err != nil {
		return err
	}
	defer claim.Close()
	receipt := productadapter.NewReceipt(authority, consumer, command.Count, command.Index, geometry)
	receipt.CredentialHandleDigests, err = productadapter.CaptureCredentialHandleDigests(consumer)
	if err != nil {
		return err
	}
	if err := claim.WriteInitialReceipt(receipt); err != nil {
		return err
	}
	fail := func(cause error) error {
		receipt.Status = "failed"
		receipt.ProxyCleanup = proxyCleanupState(consumer.ProxySocketPath)
		return errors.Join(cause, productadapter.WriteReceipt(consumer.ReceiptPath, receipt, false))
	}
	if err := claim.Validate("before preparation"); err != nil {
		return fail(err)
	}
	if err := prepareConsumerFiles(authority, consumer, command); err != nil {
		return fail(err)
	}
	if err := claim.Validate("before proxy startup"); err != nil {
		return fail(err)
	}
	cleanupProxy, proxyIdentity, err := startManagedProxy(authority, consumer.ProxySocketPath)
	if err != nil {
		return fail(err)
	}
	receipt.Status = "constructing"
	receipt.Proxy = proxyIdentity
	receipt.ProxyCleanup = "listener-active"
	if err := productadapter.WriteReceipt(consumer.ReceiptPath, receipt, false); err != nil {
		_ = cleanupProxy()
		return err
	}
	proxyCleaned := false
	defer func() {
		if proxyCleaned {
			return
		}
		cleanupErr := cleanupProxy()
		receipt.ProxyCleanup = proxyCleanupState(consumer.ProxySocketPath)
		if cleanupErr != nil {
			receipt.Status = "failed"
		}
		retErr = errors.Join(retErr, cleanupErr, productadapter.WriteReceipt(consumer.ReceiptPath, receipt, false))
	}()
	if err := claim.Validate("before exact source acquisition"); err != nil {
		return fail(err)
	}
	sshCommand := exactGitSSHCommand(authority, consumer)
	if err := wtx.SetupNativeSlot(wtx.NativeSlotOptions{
		DevkitRoot:       authority.Adapter.RuntimeRoot,
		Repo:             productadapter.ProductRepo,
		Origin:           authority.Adapter.ProductOrigin,
		Count:            command.Count,
		Index:            command.Index,
		BaseBranch:       authority.Adapter.BaseBranch,
		SourceRevision:   authority.ProductRev,
		BranchPrefix:     authority.Adapter.BranchPrefix,
		WorktreeRoot:     filepath.Dir(consumer.AgentRoot),
		BootstrapHome:    consumer.HomePath,
		GitSSHCommand:    sshCommand,
		RequireSSHOrigin: true,
	}); err != nil {
		return fail(err)
	}
	if err := claim.Validate("after exact source acquisition"); err != nil {
		return fail(err)
	}
	evidence, err := validatePreparedConsumer(authority, consumer, command)
	if err != nil {
		return fail(err)
	}
	sandboxEvidence, err := runSandboxDiagnostic(authority, consumer, command)
	evidence = append(evidence, sandboxEvidence...)
	if err != nil {
		receipt.ReadinessEvidence = evidence
		return fail(err)
	}
	if err := fillReadyReceipt(authority, consumer, command, &receipt, evidence); err != nil {
		return fail(err)
	}
	if err := cleanupProxy(); err != nil {
		return fail(err)
	}
	proxyCleaned = true
	receipt.ProxyCleanup = proxyCleanupState(consumer.ProxySocketPath)
	if receipt.ProxyCleanup != "absent" {
		return fail(fmt.Errorf("Product construction proxy cleanup is not exact"))
	}
	receipt.Status = "ready"
	if err := productadapter.WriteReceipt(consumer.ReceiptPath, receipt, false); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(receipt)
}

func execute(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
	command productadapter.Command,
) (retErr error) {
	receipt, err := productadapter.ReadReceipt(authority, command.Count, command.Index)
	if err != nil {
		return err
	}
	if receipt.Status != "ready" || receipt.ProxyCleanup != "absent" {
		return fmt.Errorf("Product construction receipt is not ready and fully cleaned")
	}
	evidence, err := validatePreparedConsumer(authority, consumer, command)
	if err != nil {
		return err
	}
	if err := compareReceipt(authority, consumer, receipt, evidence, false); err != nil {
		return err
	}
	cleanupProxy, proxyIdentity, err := startManagedProxy(authority, consumer.ProxySocketPath)
	if err != nil {
		return err
	}
	receipt.Status = "executing"
	receipt.Proxy = proxyIdentity
	receipt.ProxyCleanup = "listener-active"
	if err := productadapter.WriteReceipt(consumer.ReceiptPath, receipt, false); err != nil {
		_ = cleanupProxy()
		return err
	}
	defer func() {
		cleanupErr := cleanupProxy()
		if receipt.Status == "executing" {
			receipt.Status = "ready"
		}
		receipt.ProxyCleanup = proxyCleanupState(consumer.ProxySocketPath)
		retErr = errors.Join(retErr, cleanupErr, productadapter.WriteReceipt(consumer.ReceiptPath, receipt, false))
	}()
	sandboxEvidence, err := runSandboxDiagnostic(authority, consumer, command)
	evidence = append(evidence, sandboxEvidence...)
	if err != nil {
		receipt.Status = "blocked"
		receipt.ReadinessEvidence = evidence
		receipt.ReadinessRuntime = false
		return err
	}
	if err := compareReceipt(authority, consumer, receipt, evidence, true); err != nil {
		receipt.Status = "blocked"
		return err
	}
	return runSandboxCommand(authority, consumer, command, command.Child)
}

func exactGitSSHCommand(authority productadapter.Authority, consumer productadapter.ConsumerManifest) string {
	return strings.Join([]string{
		authority.Adapter.SSHPath,
		"-F",
		filepath.Join(consumer.HomePath, ".ssh", "config"),
	}, " ")
}

func validatePreparedConsumer(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
	command productadapter.Command,
) ([]productadapter.ReadinessEvidence, error) {
	if err := validateCandidateBoundary(consumer); err != nil {
		return nil, err
	}
	if err := validatePreparedFiles(authority, consumer, command); err != nil {
		return nil, err
	}
	if err := wtx.ValidateNativeSlot(wtx.NativeSlotValidationOptions{
		Repo:           productadapter.ProductRepo,
		Origin:         authority.Adapter.ProductOrigin,
		Index:          command.Index,
		WorktreeRoot:   filepath.Dir(consumer.AgentRoot),
		HostHome:       consumer.HomePath,
		StateRoot:      consumer.StateRoot,
		SourceRevision: authority.ProductRev,
	}); err != nil {
		return nil, err
	}
	before, err := gitMetadataDigest(consumer)
	if err != nil {
		return nil, err
	}
	for _, args := range [][]string{
		{"rev-parse", "HEAD"},
		{"diff-index", "--quiet", "HEAD", "--"},
	} {
		output, err := runPackageGit(authority, consumer.WorktreePath, args...)
		if err != nil {
			return nil, fmt.Errorf("Product non-mutating Git diagnostic %v: %w: %s", args, err, output)
		}
		if args[0] == "rev-parse" && strings.TrimSpace(output) != authority.ProductRev {
			return nil, fmt.Errorf("Product Git diagnostic selected %s, want %s", strings.TrimSpace(output), authority.ProductRev)
		}
	}
	after, err := gitMetadataDigest(consumer)
	if err != nil {
		return nil, err
	}
	if before != after {
		return nil, fmt.Errorf("Product Git diagnostic mutated metadata")
	}
	return []productadapter.ReadinessEvidence{
		{Name: "prepared-files", Status: "ready"},
		{Name: "git-exact-revision", Status: "ready"},
		{Name: "git-nonmutating-clean", Status: "ready"},
	}, nil
}

func runPackageGit(
	authority productadapter.Authority,
	worktree string,
	args ...string,
) (string, error) {
	commandArgs := []string{
		"-i",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		authority.Adapter.GitPath,
		"--no-optional-locks",
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=/dev/null",
		"-C", worktree,
	}
	if filepath.Base(authority.Adapter.EnvPath) == "coreutils" {
		commandArgs = append([]string{"--coreutils-prog=env"}, commandArgs...)
	}
	commandArgs = append(commandArgs, args...)
	command := exec.Command(authority.Adapter.EnvPath, commandArgs...)
	command.Env = []string{}
	output, err := command.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func fillReadyReceipt(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
	command productadapter.Command,
	receipt *productadapter.Receipt,
	evidence []productadapter.ReadinessEvidence,
) error {
	paths, binds, gitDigest, err := capturePreparedEvidence(authority, consumer, command)
	if err != nil {
		return err
	}
	receipt.Paths = paths
	receipt.Binds = binds
	receipt.GitMetadataDigest = gitDigest
	receipt.MountPolicyIdentity = authority.Adapter.MountPolicyIdentity
	receipt.ReadinessEvidence = evidence
	receipt.ReadinessRuntime = evidenceReady(evidence, "codex-app-server-thread-and-standalone-executable")
	receipt.ReadinessRepository = evidenceReady(evidence, "git-exact-revision") &&
		evidenceReady(evidence, "git-nonmutating-clean")
	return nil
}

func compareReceipt(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
	receipt productadapter.Receipt,
	evidence []productadapter.ReadinessEvidence,
	requireRuntime bool,
) error {
	expectedGeometry := geometryForConsumer(consumer)
	if receipt.Geometry != expectedGeometry ||
		receipt.MountPolicyIdentity != authority.Adapter.MountPolicyIdentity {
		return fmt.Errorf("Product receipt geometry or mount policy does not match the current manifest")
	}
	paths, binds, gitDigest, err := capturePreparedEvidence(authority, consumer, productadapter.Command{
		Kind: productadapter.CommandExec, Count: receipt.Count, Index: receipt.Index,
	})
	if err != nil {
		return err
	}
	if len(paths) != len(receipt.Paths) {
		return fmt.Errorf("Product receipt prepared-path set is incomplete")
	}
	for name, identity := range paths {
		if receipt.Paths[name] != identity {
			return fmt.Errorf("Product prepared path %s changed in place", name)
		}
	}
	if len(binds) != len(receipt.Binds) {
		return fmt.Errorf("Product receipt bind tuple set is incomplete")
	}
	for index := range binds {
		if binds[index] != receipt.Binds[index] {
			return fmt.Errorf("Product bind tuple %d changed", index)
		}
	}
	if gitDigest != receipt.GitMetadataDigest {
		return fmt.Errorf("Product Git metadata changed after construction")
	}
	for _, required := range []string{"prepared-files", "git-exact-revision", "git-nonmutating-clean"} {
		if !evidenceReady(evidence, required) {
			return fmt.Errorf("Product typed readiness %s is not ready", required)
		}
	}
	if requireRuntime && !evidenceReady(evidence, "codex-app-server-thread-and-standalone-executable") {
		return fmt.Errorf("Product typed app-server thread/standalone-executable readiness is not ready")
	}
	return nil
}

func capturePreparedEvidence(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
	command productadapter.Command,
) (map[string]productadapter.PathIdentity, []productadapter.BindIdentity, string, error) {
	configured := preparedPathSet(authority, consumer, command)
	paths := make(map[string]productadapter.PathIdentity, len(configured))
	for name, path := range configured {
		digest := name != "codex_auth" && name != "ssh_private"
		identity, err := productadapter.CapturePathIdentity(path, digest)
		if err != nil {
			return nil, nil, "", err
		}
		paths[name] = identity
	}
	binds := make([]productadapter.BindIdentity, 0, len(consumer.Binds))
	for _, bind := range consumer.Binds {
		identity, err := productadapter.CapturePathIdentity(bind.Source, true)
		if err != nil {
			return nil, nil, "", err
		}
		binds = append(binds, productadapter.BindIdentity{
			Source: identity, Target: bind.Target, Mode: bind.Mode, Required: bind.Required,
		})
	}
	sort.Slice(binds, func(i, j int) bool {
		if binds[i].Target == binds[j].Target {
			return binds[i].Source.Path < binds[j].Source.Path
		}
		return binds[i].Target < binds[j].Target
	})
	gitDigest, err := gitMetadataDigest(consumer)
	return paths, binds, gitDigest, err
}

func evidenceReady(evidence []productadapter.ReadinessEvidence, name string) bool {
	for _, item := range evidence {
		if item.Name == name {
			return item.Status == "ready"
		}
	}
	return false
}

func gitMetadataDigest(consumer productadapter.ConsumerManifest) (string, error) {
	paths := []string{
		filepath.Join(consumer.WorktreePath, ".git"),
		filepath.Join(consumer.CommonDirPath, "config"),
		filepath.Join(consumer.CommonDirPath, "HEAD"),
		filepath.Join(consumer.CommonDirPath, "FETCH_HEAD"),
		filepath.Join(consumer.CommonDirPath, "packed-refs"),
	}
	var payload strings.Builder
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		payload.WriteString(path)
		payload.WriteByte(0)
		payload.Write(data)
		payload.WriteByte(0)
	}
	sum := sha256.Sum256([]byte(payload.String()))
	return hex.EncodeToString(sum[:]), nil
}

func proxyCleanupState(path string) string {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return "absent"
	}
	return "residue-present"
}

func startManagedProxy(
	authority productadapter.Authority,
	socketPath string,
) (func() error, productadapter.ProxyIdentity, error) {
	cleanup, err := startProxy(
		socketPath,
		authority.Adapter.EgressAllowlistPath,
		authority.Adapter.UpstreamProxyURL,
	)
	if err != nil {
		return nil, productadapter.ProxyIdentity{}, err
	}
	identity, err := productadapter.CaptureProxyIdentity(socketPath)
	if err != nil {
		_ = cleanup()
		return nil, productadapter.ProxyIdentity{}, err
	}
	return cleanup, identity, nil
}

var _ = egressproxy.Config{}

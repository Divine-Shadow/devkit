package launch

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"devkit/cli/devctl/internal/codexhistory"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"devkit/cli/devctl/internal/devkitpaths"
	"devkit/cli/devctl/internal/gitauthority"
	"devkit/cli/devctl/internal/runtime/agent"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/sshauthority"
)

type Command struct {
	Path string
	Args []string
	Dir  string
}

const postgresEndpointPingTimeout = 100 * time.Millisecond

// StartPostgresEndpointProxy starts the package-owned host helper before the
// sandbox is assembled. The shared Docker broker socket is intentionally never
// projected into the sandbox; only these per-agent sockets are bindable.
func StartPostgresEndpointProxy(p nativeplan.Plan) (func() error, error) {
	binary := strings.TrimSpace(p.Env["DEVKIT_RUNTIME_BROKER_BINARY"])
	if binary == "" || !filepath.IsAbs(binary) || !isExecutable(binary) {
		return nil, fmt.Errorf("package-owned postgres broker binary is required for endpoint lease")
	}
	if strings.TrimSpace(p.BrokerEndpoint) == "" || strings.TrimSpace(p.PostgresDockerSocket) == "" {
		return nil, fmt.Errorf("postgres endpoint lease paths are required")
	}
	if err := os.MkdirAll(filepath.Dir(p.PostgresDockerSocket), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(p.PostgresDockerSocket); err == nil {
		return nil, fmt.Errorf("postgres endpoint socket already exists: %s", p.PostgresDockerSocket)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect postgres endpoint socket: %w", err)
	}
	runID := p.Agent.ID.Name()
	cmd := exec.Command(binary, "agent-proxy", p.PostgresDockerSocket, p.BrokerEndpoint, runID)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start postgres endpoint proxy: %w", err)
	}
	var ownedSocket os.FileInfo
	cleanup := func() error {
		var cleanupErr error
		if cmd.Process != nil {
			if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("stop postgres endpoint proxy: %w", err))
			}
		}
		if err := cmd.Wait(); err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("wait for postgres endpoint proxy: %w", err))
			}
		}
		if ownedSocket != nil {
			current, err := os.Lstat(p.PostgresDockerSocket)
			switch {
			case errors.Is(err, os.ErrNotExist):
			case err != nil:
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("inspect postgres endpoint socket during cleanup: %w", err))
			case !os.SameFile(ownedSocket, current):
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("postgres endpoint socket ownership changed before cleanup"))
			default:
				if err := os.Remove(p.PostgresDockerSocket); err != nil {
					cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove postgres endpoint socket: %w", err))
				}
			}
		}
		return cleanupErr
	}
	readyDeadline := time.Now().Add(time.Second)
	var lastPingError error
	for time.Now().Before(readyDeadline) {
		info, err := os.Lstat(p.PostgresDockerSocket)
		if err == nil {
			if info.Mode()&os.ModeSocket == 0 {
				cleanupErr := cleanup()
				return nil, errors.Join(fmt.Errorf("postgres endpoint proxy published a non-socket path"), cleanupErr)
			}

			if ownedSocket == nil {
				ownedSocket = info
			} else if !os.SameFile(ownedSocket, info) {
				return nil, errors.Join(fmt.Errorf("postgres endpoint socket changed during readiness"), cleanup())
			}
			lastPingError = postgresEndpointPing(p.PostgresDockerSocket)
			if lastPingError == nil {
				current, statErr := os.Lstat(p.PostgresDockerSocket)
				if statErr == nil && os.SameFile(ownedSocket, current) {
					return cleanup, nil
				}
				return nil, errors.Join(fmt.Errorf("postgres endpoint socket changed during admission"), cleanup())
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			cleanupErr := cleanup()
			return nil, errors.Join(fmt.Errorf("inspect postgres endpoint socket: %w", err), cleanupErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cleanupErr := cleanup()
	return nil, errors.Join(fmt.Errorf("postgres endpoint proxy did not publish a ready per-agent socket: %v", lastPingError), cleanupErr)
}

func postgresEndpointPing(socket string) error {
	conn, err := net.DialTimeout("unix", socket, postgresEndpointPingTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	// A listener can accept without responding. Bound the complete typed ping
	// exchange, not just the initial Unix-socket dial, before admitting it.
	if err := conn.SetDeadline(time.Now().Add(postgresEndpointPingTimeout)); err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodGet, "http://docker/_ping", nil)
	if err != nil {
		return err
	}
	if err := request.Write(conn); err != nil {
		return err
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 33))
	if err != nil {
		return err
	}
	if len(body) > 32 || response.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "OK" {
		return fmt.Errorf("postgres endpoint ping = %s %q", response.Status, body)
	}
	return nil
}

var (
	terraformProviderCredentialHydrator = "/run/current-system/sw/bin/terraform-ouro-provider-credential-hydrate"
	terraformProviderCredentialRoot     = "/run/terraform-ouro-provider-credentials"
)

func Prepare(p nativeplan.Plan) error {
	return prepare(p)
}

// ValidateNativeExecWorktree is read-only admission before endpoint allocation.
// Full preparation repeats it before provisioning the selected runtime.
func ValidateNativeExecWorktree(p nativeplan.Plan) error {
	if strings.TrimSpace(p.Agent.HostWorktree) == "" {
		return fmt.Errorf("host worktree is empty")
	}
	if st, err := os.Stat(p.Agent.HostWorktree); err != nil {
		return fmt.Errorf("host worktree %s: %w", p.Agent.HostWorktree, err)
	} else if !st.IsDir() {
		return fmt.Errorf("host worktree %s is not a directory", p.Agent.HostWorktree)
	}
	return requireGitWorktree(p.Agent.HostWorktree)
}

// PrepareFresh uses ordinary package-owned config/auth/SSH provisioning, but
// never imports predecessor history or a legacy runtime context namespace.
func PrepareFresh(p nativeplan.Plan) error {
	if p.GUITargetConfig != nil && p.GUITargetConfig.RetainedHistoryManifest != "" {
		return fmt.Errorf("fresh native preparation refuses a retained history projection")
	}
	return prepareWithLegacyState(p, false)
}

func prepare(p nativeplan.Plan) error {
	return prepareWithLegacyState(p, true)
}

func prepareWithLegacyState(p nativeplan.Plan, allowLegacyState bool) error {
	if err := nativeplan.ValidateControllerProjection(p); err != nil {
		return err
	}
	if err := ValidateNativePersistentHome(p); err != nil {
		return err
	}
	if p.Agent.ID.Project == "dev-all" && !agent.IsWorkspaceRootRepo(p.Agent.ID.Repo) {
		allowLegacyState = false
	}
	if err := nativeplan.ValidateGitBacklinkProjection(p); err != nil {
		return err
	}
	if err := verifyGitBacklink(p, true); err != nil {
		return err
	}
	sshAuthority, err := resolvePackageSSHAuthority()
	if err != nil {
		return err
	}
	if err := ValidateNativeExecWorktree(p); err != nil {
		return err
	}
	// A selected GUI config is the first mutation beneath HostHome. Its
	// handle-based projection rejects symlinked or foreign-owned destination
	// components before the ordinary runtime directory preparation can follow
	// or populate them.
	if err := SeedCodexConfig(p); err != nil {
		return err
	}
	for _, dir := range []string{p.Agent.HostHome, p.Agent.StateRoot} {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	for _, rel := range []string{
		filepath.Join(".codex", "rollouts"),
		".cache",
		".config",
		".local",
		".sbt",
	} {
		dir := filepath.Join(p.Agent.HostHome, rel)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	if err := prepareGitBacklink(p); err != nil {
		return err
	}
	if selected := p.GUITargetConfig; selected != nil && selected.RetainedHistoryManifest != "" {
		options := codexhistory.SnapshotOptions{Project: p.Agent.ID.Project, ResetKind: "selected-slot-reset", AgentIndex: p.Agent.ID.Index, HostWorktree: p.Agent.HostWorktree, HostHome: p.Agent.HostHome, SandboxHome: p.Agent.SandboxHome, StateRoot: p.Agent.StateRoot, WorkspaceRoot: p.HostWorkspaceRoot}
		binding := codexhistory.PreparationProjection{ManifestPath: selected.RetainedHistoryManifest, ManifestSHA256: selected.RetainedHistoryManifestSHA256, BundleSHA256: selected.RetainedHistoryBundleSHA256, ThreadID: selected.RetainedHistoryThreadID}
		if err := codexhistory.ProjectPreparationHistory(options, binding); err != nil {
			return fmt.Errorf("retained native preparation history: %w", err)
		}
	}
	if allowLegacyState {
		if err := migrateMissingCodexState(p.Agent.HostHome, filepath.Join(p.Agent.StateRoot, "home")); err != nil {
			return err
		}
	}
	if err := ensureCodexShellHook(p); err != nil {
		return err
	}
	if err := ensureScalaCacheDirs(p); err != nil {
		return err
	}
	if err := capCodexTUILog(p.Agent.HostHome); err != nil {
		return err
	}
	if err := SeedCodexAuth(p.Agent.HostHome, false); err != nil {
		return err
	}
	if err := SeedSSH(p.Agent.HostHome, false); err != nil {
		return err
	}
	if err := SeedAWS(p.Agent.HostHome, false); err != nil {
		return err
	}
	if err := SeedTerraformProviderCredentials(p); err != nil {
		return err
	}
	if err := ensureGitSSHConfig(p, sshAuthority); err != nil {
		return err
	}
	if err := ensureResolvConf(p.DNS.ResolvConf); err != nil {
		return err
	}
	for _, bind := range p.Binds {
		if !bind.Required {
			continue
		}
		if _, err := os.Stat(bind.Source); err != nil {
			return fmt.Errorf("required bind source %s: %w", bind.Source, err)
		}
	}
	return nil
}

// validateTerraformRuntimeSupportPlan reuses the native planner's exact TF1
// isolated-lane validator before projecting public support. The normal tools
// entrypoint retains its additional immutable target/config/host admission.
func validateTerraformRuntimeSupportPlan(p nativeplan.Plan) error {
	if !filepath.IsAbs(p.HostRuntimeSupportRoot) || filepath.Clean(p.HostRuntimeSupportRoot) != p.HostRuntimeSupportRoot || filepath.Base(p.HostRuntimeSupportRoot) != ".devkit" {
		return fmt.Errorf("Terraform runtime support requires the declared central support root")
	}
	devRoot := filepath.Dir(p.HostRuntimeSupportRoot)
	expected, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths:    devkitpaths.Paths{Root: p.DevkitHostRoot, RuntimeAuthorityRoot: p.RuntimeAuthorityRoot},
		HostRoot: devRoot, Project: "ouroboros-terraform", Repo: "ouroboros-terraform", Index: 1,
		WorktreeRoot:  filepath.Join(devRoot, "control-plane-worktrees"),
		StateRoot:     filepath.Join(devRoot, ".devkit", "native-agents"),
		WorkspaceRoot: p.HostWorkspaceRoot, IsolationProfile: p.IsolationProfile,
		EgressAllowlist: p.Proxy.AllowlistPath,
	})
	if err != nil {
		return fmt.Errorf("Terraform runtime support plan: %w", err)
	}
	if p.IsolationProfile != nativeplan.IsolationProfileWorkspaceEgress || p.Agent != expected.Agent ||
		p.HostWorktreeRoot != expected.HostWorktreeRoot || p.HostStateRoot != expected.HostStateRoot ||
		p.SandboxWorkspaceRoot != expected.SandboxWorkspaceRoot || p.HostRuntimeSupportRoot != expected.HostRuntimeSupportRoot {
		return fmt.Errorf("Terraform runtime support requires exact declared TF1 projection")
	}
	for _, wanted := range expected.Binds {
		switch wanted.Target {
		case "/workspaces/dev/.devkit/ouro8-governance-env.sh", "/workspaces/dev/.devkit/ouro8-governance-repo-env.json", "/workspaces/dev/.devkit/governance-control-plane":
			matches := 0
			for _, actual := range p.Binds {
				if actual.Target == wanted.Target {
					matches++
					if actual != wanted {
						return fmt.Errorf("Terraform runtime support requires exact required bind %s", wanted.Target)
					}
				}
			}
			if matches != 1 {
				return fmt.Errorf("Terraform runtime support requires one declared bind %s", wanted.Target)
			}
		}
	}
	return nil
}

func copyIsolatedProductRuntimeSupportFile(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("inspect source-derived Product runtime support %s: %w", source, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("source-derived Product runtime support must be a regular non-symlink file: %s", source)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read source-derived Product runtime support %s: %w", source, err)
	}
	if targetInfo, err := os.Lstat(target); err == nil {
		if targetInfo.Mode()&os.ModeSymlink != 0 || !targetInfo.Mode().IsRegular() {
			return fmt.Errorf("isolated Product runtime support target must be a regular non-symlink file: %s", target)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect isolated Product runtime support target %s: %w", target, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".product-runtime-support-")
	if err != nil {
		return fmt.Errorf("create isolated Product runtime support staging file: %w", err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod isolated Product runtime support staging file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write isolated Product runtime support staging file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync isolated Product runtime support staging file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close isolated Product runtime support staging file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return fmt.Errorf("chmod isolated Product runtime support file: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("install isolated Product runtime support target: %w", err)
	}
	committed = true
	return nil
}

// SeedTerraformProviderCredentials projects only the two provider credential
// files owned by the source-derived WSL hydrator into the sole Terraform
// acceptance consumer. The helper has no caller-supplied source, target, or
// secret identifier; Devkit never reads a credential from an ambient path.
func SeedTerraformProviderCredentials(p nativeplan.Plan) error {
	if p.Agent.ID.Project != "ouroboros-terraform" ||
		p.Agent.ID.Repo != "ouroboros-terraform" ||
		p.Agent.ID.Index != 1 {
		return nil
	}
	hydrator := filepath.Clean(strings.TrimSpace(terraformProviderCredentialHydrator))
	root := filepath.Clean(strings.TrimSpace(terraformProviderCredentialRoot))
	if !filepath.IsAbs(hydrator) || !filepath.IsAbs(root) {
		return fmt.Errorf("Terraform provider credential authority is not absolute")
	}
	cmd := exec.Command(hydrator)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Only the finite canonical helper record crosses this boundary. Raw
		// stderr remains private, including on malformed or unknown output.
		if diagnostic := parseTerraformProviderAWSDiagnostic(stderr.Bytes()); diagnostic != nil {
			reason := terraformProviderCredentialHydrationFailureReason(stderr.String())
			message := "source-derived Terraform provider credential hydration failed"
			if reason != "" {
				message += ": " + reason
			}
			return fmt.Errorf("%s\n%s", message, diagnostic.wireLine())
		}
		if reason := terraformProviderCredentialHydrationFailureReason(stderr.String()); reason != "" {
			return fmt.Errorf("source-derived Terraform provider credential hydration failed: %s", reason)
		}
		return fmt.Errorf("source-derived Terraform provider credential hydration failed")
	}
	files := []struct {
		source string
		target string
	}{
		{
			source: filepath.Join(root, "google-adc.json"),
			target: filepath.Join(p.Agent.HostHome, ".config", "gcloud", "application_default_credentials.d", "ouroboros-ai-498921-codex-terraform.json"),
		},
		{
			source: filepath.Join(root, "dnsimple-provider.env"),
			target: filepath.Join(p.Agent.HostHome, ".config", "ouroboros", "dnsimple-provider.env"),
		},
	}
	for _, file := range files {
		if err := copyProtectedProviderCredential(file.source, file.target); err != nil {
			return err
		}
	}
	return nil
}

func terraformProviderCredentialHydrationFailureReason(stderr string) string {
	const prefix = "Terraform provider credential hydration failed: "
	allowed := map[string]struct{}{
		"protected credential source is not a regular non-symlink file": {},
		"protected credential source mode is not private":               {},
		"Google ADC source does not have an accepted credential schema": {},
		"approved DNSimple token source is unavailable":                 {},
		"approved DNSimple account source is unavailable":               {},
		"approved DNSimple credential source returned an empty value":   {},
		"approved DNSimple credential must be one line":                 {},
	}
	for _, line := range strings.Split(stderr, "\n") {
		reason := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		if _, ok := allowed[reason]; ok {
			return reason
		}
	}
	return ""
}

func copyProtectedProviderCredential(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("inspect protected provider credential source: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("protected provider credential source must be a regular non-symlink file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("protected provider credential source has unsafe mode %04o", info.Mode().Perm())
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read protected provider credential source: %w", err)
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create protected provider credential target directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("chmod protected provider credential target directory: %w", err)
	}
	if targetInfo, err := os.Lstat(target); err == nil {
		if targetInfo.Mode()&os.ModeSymlink != 0 || !targetInfo.Mode().IsRegular() {
			return fmt.Errorf("protected provider credential target must be a regular non-symlink file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect protected provider credential target: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".provider-credential-")
	if err != nil {
		return fmt.Errorf("create protected provider credential staging file: %w", err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod protected provider credential staging file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write protected provider credential staging file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync protected provider credential staging file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close protected provider credential staging file: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("install protected provider credential target: %w", err)
	}
	committed = true
	return nil
}

func requireGitWorktree(worktree string) error {
	cmd := exec.Command(gitauthority.Executable(), "-C", worktree, "rev-parse", "--show-toplevel")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("host worktree %s is not a Git worktree: %w: %s", worktree, err, strings.TrimSpace(string(out)))
	}
	top := filepath.Clean(strings.TrimSpace(string(out)))
	expected := filepath.Clean(worktree)
	if resolved, err := filepath.EvalSymlinks(top); err == nil {
		top = resolved
	}
	if resolved, err := filepath.EvalSymlinks(expected); err == nil {
		expected = resolved
	}
	if top != expected {
		return fmt.Errorf("host worktree %s resolves to foreign Git root %s", worktree, top)
	}
	return nil
}

const (
	gitSSHManagedBegin = "# BEGIN DEVKIT NATIVE GIT SSH"
	gitSSHManagedEnd   = "# END DEVKIT NATIVE GIT SSH"
)

var resolvePackageSSHAuthority = sshauthority.Package

func ensureGitSSHConfig(p nativeplan.Plan, sshAuthority sshauthority.Authority) error {
	hostHome := strings.TrimSpace(p.Agent.HostHome)
	sandboxHome := strings.TrimSpace(p.Agent.SandboxHome)
	if hostHome == "" || sandboxHome == "" {
		return nil
	}
	sshDir := filepath.Join(hostHome, ".ssh")
	identityNames := existingSSHIdentities(sshDir)
	if len(identityNames) == 0 {
		return nil
	}
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", sshDir, err)
	}
	if err := sshAuthority.InstallKnownHosts(filepath.Join(sshDir, "known_hosts")); err != nil {
		return err
	}
	sshCommand, err := sshAuthority.Command(filepath.Join(sandboxHome, ".ssh", "config"))
	if err != nil {
		return err
	}
	if err := WriteGitSSHConfigForPlan(p, identityNames); err != nil {
		return err
	}
	if err := runGitConfigFile(filepath.Join(hostHome, ".gitconfig"), "core.sshCommand", sshCommand); err != nil {
		return err
	}
	if !isDevWorkspacePlan(p) {
		if err := configureWorktreeGitSSH(p.Agent.HostWorktree, sshCommand); err != nil {
			return err
		}
	}
	return nil
}

// GitBootstrapSSHCommand returns the package-owned host command used by
// native prepare before a linked worktree exists. The bootstrap command must
// use the per-consumer home; the normal runtime preparation later rewrites the
// same managed config block to sandbox-visible identity paths.
func GitBootstrapSSHCommand(p nativeplan.Plan) (string, error) {
	hostHome := strings.TrimSpace(p.Agent.HostHome)
	if hostHome == "" {
		return "", fmt.Errorf("native Git bootstrap requires an agent host home")
	}
	sshAuthority, err := resolvePackageSSHAuthority()
	if err != nil {
		return "", err
	}
	return sshAuthority.Command(filepath.Join(hostHome, ".ssh", "config"))
}

// PrepareGitBootstrap seeds and configures only the package-owned SSH identity
// needed for the first remote fetch. It deliberately does not require or
// configure a Git worktree because native prepare calls it before materializing
// the linked worktree.
func PrepareGitBootstrap(p nativeplan.Plan) (string, error) {
	if err := ValidateNativePersistentHome(p); err != nil {
		return "", err
	}
	sshAuthority, err := resolvePackageSSHAuthority()
	if err != nil {
		return "", err
	}
	hostHome := strings.TrimSpace(p.Agent.HostHome)
	sshCommand, err := sshAuthority.Command(filepath.Join(hostHome, ".ssh", "config"))
	if err != nil {
		return "", err
	}
	if err := SeedSSH(hostHome, false); err != nil {
		return "", err
	}
	if err := sshAuthority.InstallKnownHosts(filepath.Join(hostHome, ".ssh", "known_hosts")); err != nil {
		return "", err
	}
	identityNames := existingSSHIdentities(filepath.Join(hostHome, ".ssh"))
	if len(identityNames) == 0 {
		return "", fmt.Errorf("native Git bootstrap requires a seeded SSH identity in %s", filepath.Join(hostHome, ".ssh"))
	}
	proxyCommand := ""
	if p.IsolationProfile == nativeplan.IsolationProfileWorkspaceEgress {
		proxyCommand, err = gitManagedProxyCommand(p)
		if err != nil {
			return "", err
		}
	}
	if err := writeGitSSHConfigWithProxyCommand(hostHome, hostHome, identityNames, proxyCommand); err != nil {
		return "", err
	}
	if !isDevWorkspacePlan(p) {
		if err := configureWorktreeGitSSH(p.Agent.HostWorktree, sshCommand); err != nil {
			return "", err
		}
	}
	return sshCommand, nil
}

func gitManagedProxyCommand(p nativeplan.Plan) (string, error) {
	runtimeRoot := strings.TrimSpace(p.RuntimeAuthorityRoot)
	if runtimeRoot == "" {
		return "", fmt.Errorf("native Git bootstrap requires a source-derived runtime authority root")
	}
	project := strings.TrimSpace(p.Agent.ID.Project)
	if project == "" {
		return "", fmt.Errorf("native Git bootstrap requires a project identity")
	}
	socketPath := strings.TrimSpace(p.Proxy.UnixSocket)
	if socketPath == "" {
		return "", fmt.Errorf("native Git bootstrap requires a managed egress proxy socket")
	}
	devctlPath := filepath.Join(filepath.Clean(runtimeRoot), "kit", "bin", "devctl")
	if !isExecutable(devctlPath) {
		return "", fmt.Errorf("native Git bootstrap requires package-owned proxy helper %s", devctlPath)
	}
	return strings.Join([]string{
		shellQuote(devctlPath),
		"-p", shellQuote(project),
		"native", "proxy-connect",
		"--socket", shellQuote(socketPath),
		"--target", "%h:%p",
	}, " "), nil
}

func existingSSHIdentities(sshDir string) []string {
	var names []string
	for _, name := range []string{"id_ed25519", "id_rsa"} {
		if st, err := os.Stat(filepath.Join(sshDir, name)); err == nil && st.Mode().IsRegular() {
			names = append(names, name)
		}
	}
	return names
}

// WriteGitSSHConfigForPlan renders the package-owned Git SSH configuration
// using the plan's host write location, sandbox-visible identity paths, and
// managed workspace-egress connector.
func WriteGitSSHConfigForPlan(p nativeplan.Plan, identityNames []string) error {
	hostHome := strings.TrimSpace(p.Agent.HostHome)
	configHome := strings.TrimSpace(p.Agent.SandboxHome)
	if hostHome == "" || configHome == "" {
		return nil
	}
	proxyCommand := ""
	if p.IsolationProfile == nativeplan.IsolationProfileWorkspaceEgress {
		var err error
		proxyCommand, err = gitManagedProxyCommand(p)
		if err != nil {
			return err
		}
	}
	return writeGitSSHConfigWithProxyCommand(hostHome, configHome, identityNames, proxyCommand)
}

func writeGitSSHConfigWithProxyCommand(hostHome, configHome string, identityNames []string, proxyCommand string) error {
	hostHome = strings.TrimSpace(hostHome)
	configHome = strings.TrimSpace(configHome)
	if hostHome == "" || configHome == "" {
		return nil
	}
	sshDir := filepath.Join(hostHome, ".ssh")
	if len(identityNames) == 0 {
		identityNames = existingSSHIdentities(sshDir)
	}
	if len(identityNames) == 0 {
		return nil
	}
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", sshDir, err)
	}
	cfgPath := filepath.Join(sshDir, "config")
	cfg := buildGitSSHConfigWithProxyCommand(configHome, identityNames, proxyCommand)
	if err := writeManagedBlock(cfgPath, cfg, 0o600); err != nil {
		return err
	}
	return nil
}

func buildGitSSHConfigWithProxyCommand(configHome string, identityNames []string, proxyCommand string) string {
	var b strings.Builder
	b.WriteString(gitSSHManagedBegin + "\n")
	b.WriteString("Host github.com ssh.github.com\n")
	if proxyCommand = strings.TrimSpace(proxyCommand); proxyCommand != "" {
		b.WriteString("  HostName ssh.github.com\n")
		b.WriteString("  Port 443\n")
		b.WriteString("  ProxyCommand " + proxyCommand + "\n")
	}
	b.WriteString("  User git\n")
	for _, name := range identityNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		b.WriteString("  IdentityFile " + filepath.Join(configHome, ".ssh", name) + "\n")
	}
	b.WriteString("  IdentitiesOnly yes\n")
	b.WriteString("  BatchMode yes\n")
	b.WriteString("  StrictHostKeyChecking yes\n")
	b.WriteString("  UserKnownHostsFile " + filepath.Join(configHome, ".ssh", "known_hosts") + "\n")
	b.WriteString(gitSSHManagedEnd + "\n")
	return b.String()
}

func writeManagedBlock(path string, block string, mode os.FileMode) error {
	original := ""
	if data, err := os.ReadFile(path); err == nil {
		original = string(data)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	rest := removeManagedGitSSHBlocks(original)
	// Native agent Git points directly at this SSH config. Keep github.com
	// authoritative so stale devkit-era Host blocks cannot be merged by ssh(1).
	rest = removeGitHubHostBlocks(rest)
	next := strings.TrimRight(block, "\r\n") + "\n"
	if strings.TrimSpace(rest) != "" {
		next += "\n" + strings.TrimLeft(rest, "\r\n")
	}
	if err := os.WriteFile(path, []byte(next), mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func removeManagedGitSSHBlocks(config string) string {
	for {
		start := strings.Index(config, gitSSHManagedBegin)
		if start < 0 {
			return config
		}
		endRel := strings.Index(config[start:], gitSSHManagedEnd)
		if endRel < 0 {
			return config[:start]
		}
		end := start + endRel + len(gitSSHManagedEnd)
		config = strings.TrimRight(config[:start], "\r\n") + "\n" + strings.TrimLeft(config[end:], "\r\n")
	}
}

func removeGitHubHostBlocks(config string) string {
	lines := strings.SplitAfter(config, "\n")
	var kept []string
	for i := 0; i < len(lines); {
		if isGitHubHostLine(lines[i]) {
			i++
			for i < len(lines) && !startsSSHHostOrMatchBlock(lines[i]) {
				i++
			}
			continue
		}
		kept = append(kept, lines[i])
		i++
	}
	return strings.Join(kept, "")
}

func isGitHubHostLine(line string) bool {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 2 || !strings.EqualFold(fields[0], "Host") {
		return false
	}
	for _, pattern := range fields[1:] {
		if pattern == "github.com" {
			return true
		}
	}
	return false
}

func startsSSHHostOrMatchBlock(line string) bool {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return false
	}
	return strings.EqualFold(fields[0], "Host") || strings.EqualFold(fields[0], "Match")
}

func runGitConfigFile(configPath, key, value string) error {
	if strings.TrimSpace(configPath) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(configPath), err)
	}
	if out, err := runGitConfigWithLockRetry("config", "--file", configPath, key, value); err != nil {
		return fmt.Errorf("git config --file %s %s: %w: %s", configPath, key, err, strings.TrimSpace(string(out)))
	}
	if err := os.Chmod(configPath, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", configPath, err)
	}
	return nil
}

func configureWorktreeGitSSH(worktree string, sshCommand string) error {
	worktree = strings.TrimSpace(worktree)
	if worktree == "" {
		return nil
	}
	gitMarker := filepath.Join(worktree, ".git")
	if _, err := os.Lstat(gitMarker); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect Git metadata marker %s: %w", gitMarker, err)
	}
	out, err := exec.Command(gitauthority.Executable(), "-C", worktree, "rev-parse", "--git-dir").CombinedOutput()
	if err != nil {
		return fmt.Errorf("resolve Git metadata for %s: %w: %s", worktree, err, strings.TrimSpace(string(out)))
	}
	gitDir := filepath.Clean(strings.TrimSpace(string(out)))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktree, gitDir)
	}
	if out, err := runGitConfigWithLockRetry("--git-dir", gitDir, "config", "extensions.worktreeConfig", "true"); err != nil {
		return fmt.Errorf("git config extensions.worktreeConfig in %s: %w: %s", worktree, err, strings.TrimSpace(string(out)))
	}
	// Package-owned native worktrees are linked from a bare common
	// repository. Once worktreeConfig is enabled, preserve the selected
	// worktree's non-bare identity explicitly; otherwise the shared
	// core.bare=true makes subsequent Git and libgit2 opens lose the worktree.
	if out, err := runGitConfigWithLockRetry("--git-dir", gitDir, "config", "--worktree", "core.bare", "false"); err != nil {
		return fmt.Errorf("git config --worktree core.bare in %s: %w: %s", worktree, err, strings.TrimSpace(string(out)))
	}
	if out, err := runGitConfigWithLockRetry("--git-dir", gitDir, "config", "--worktree", "core.sshCommand", sshCommand); err != nil {
		return fmt.Errorf("git config --worktree core.sshCommand in %s: %w: %s", worktree, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runGitConfigWithLockRetry(args ...string) ([]byte, error) {
	var out []byte
	var err error
	for attempt := 0; attempt < 6; attempt++ {
		out, err = exec.Command(gitauthority.Executable(), args...).CombinedOutput()
		if err == nil || !isGitConfigLockError(out) {
			return out, err
		}
		time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
	}
	return out, err
}

func isGitConfigLockError(out []byte) bool {
	text := strings.ToLower(string(out))
	return strings.Contains(text, "could not lock config file") || strings.Contains(text, "file exists")
}

func ensureScalaCacheDirs(p nativeplan.Plan) error {
	for _, key := range []string{"COURSIER_CACHE", "SBT_IVY_HOME", "SBT_GLOBAL_BASE", "SBT_BOOT_DIR"} {
		sandboxPath := strings.TrimSpace(p.Env[key])
		if sandboxPath == "" {
			continue
		}
		hostPath, ok := sandboxPathToHost(p, sandboxPath)
		if !ok {
			continue
		}
		if err := os.MkdirAll(hostPath, 0o755); err != nil {
			return fmt.Errorf("mkdir %s for %s: %w", hostPath, key, err)
		}
	}
	return nil
}

func sandboxPathToHost(p nativeplan.Plan, sandboxPath string) (string, bool) {
	sandboxPath = filepath.Clean(sandboxPath)
	if strings.TrimSpace(p.Agent.SandboxHome) != "" {
		if rel, err := filepath.Rel(filepath.Clean(p.Agent.SandboxHome), sandboxPath); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.Join(p.Agent.HostHome, rel), true
		}
	}
	const workspaceRoot = "/workspaces/dev"
	if rel, err := filepath.Rel(workspaceRoot, sandboxPath); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		if strings.TrimSpace(p.HostWorkspaceRoot) != "" {
			return filepath.Join(p.HostWorkspaceRoot, rel), true
		}
		return filepath.Join(filepath.Dir(p.DevkitHostRoot), rel), true
	}
	return "", false
}

const defaultCodexTUILogMaxBytes int64 = 256 * 1024 * 1024

func codexTUILogMaxBytes() (int64, error) {
	raw := strings.TrimSpace(os.Getenv("DEVKIT_CODEX_TUI_LOG_MAX_BYTES"))
	if raw == "" {
		return defaultCodexTUILogMaxBytes, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid DEVKIT_CODEX_TUI_LOG_MAX_BYTES=%q: %w", raw, err)
	}
	return value, nil
}

func capCodexTUILog(hostHome string) error {
	maxBytes, err := codexTUILogMaxBytes()
	if err != nil {
		return err
	}
	if maxBytes <= 0 || strings.TrimSpace(hostHome) == "" {
		return nil
	}
	path := filepath.Join(hostHome, ".codex", "log", "codex-tui.log")
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Size() <= maxBytes {
		return nil
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	if _, err := file.Seek(info.Size()-maxBytes, io.SeekStart); err != nil {
		return fmt.Errorf("seek %s: %w", path, err)
	}
	tail, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read tail from %s: %w", path, err)
	}
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("truncate %s: %w", path, err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind %s: %w", path, err)
	}
	if _, err := file.Write(tail); err != nil {
		return fmt.Errorf("write capped tail to %s: %w", path, err)
	}
	return nil
}

func ensureCodexShellHook(p nativeplan.Plan) error {
	return repairRetiredCodexShellHook(p.Agent.HostHome)
}

func isDevWorkspacePlan(p nativeplan.Plan) bool {
	return strings.TrimSpace(p.Agent.ID.Project) == "dev-workspace"
}

func repairRetiredCodexShellHook(hostHome string) error {
	hostHome = strings.TrimSpace(hostHome)
	if hostHome == "" {
		return nil
	}
	zshrc := filepath.Join(hostHome, ".zshrc")
	data, err := os.ReadFile(zshrc)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", zshrc, err)
	}
	original := string(data)
	repaired := original
	retired := "/usr/local/bin/" + "codex"
	if strings.Contains(repaired, retired) {
		repaired = strings.ReplaceAll(repaired, retired, "command codex")
	}
	if strings.Contains(repaired, "codex() {") && !strings.Contains(repaired, "devkit_codex_tui_log_guard()") {
		repaired = strings.Replace(repaired, "codex() {\n", codexTUILogGuardZsh+"\ncodex() {\n", 1)
	}
	oldCodexCommand := `  HOME="$HOME" CODEX_HOME="$HOME/.codex" CODEX_ROLLOUT_DIR="$HOME/.codex/rollouts" command codex "${` + `extra[@]` + `}" "$@"`
	const codexCommand = `  HOME="$HOME" CODEX_HOME="$HOME/.codex" CODEX_ROLLOUT_DIR="$HOME/.codex/rollouts" command codex "$@"`
	if strings.Contains(repaired, oldCodexCommand) {
		repaired = strings.Replace(repaired, oldCodexCommand, "  devkit_codex_tui_log_guard\n"+codexCommand, 1)
	}
	repaired = removeLegacyCodexExtraBlock(repaired)
	if repaired == original {
		return nil
	}
	if err := os.WriteFile(zshrc, []byte(repaired), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", zshrc, err)
	}
	return nil
}

func removeLegacyCodexExtraBlock(zshrc string) string {
	for {
		start := strings.Index(zshrc, "\n  local -a extra\n  extra=(\n")
		if start < 0 {
			return zshrc
		}
		blockStart := start + 1
		endRel := strings.Index(zshrc[blockStart:], "\n  )\n")
		if endRel < 0 {
			return zshrc
		}
		blockEnd := blockStart + endRel + len("\n  )\n")
		zshrc = zshrc[:blockStart] + zshrc[blockEnd:]
	}
}

const codexTUILogGuardZsh = `devkit_codex_tui_log_guard() {
  local log="$HOME/.codex/log/codex-tui.log"
  local max="${DEVKIT_CODEX_TUI_LOG_MAX_BYTES:-268435456}"
  [[ "$max" == <-> ]] || return 0
  (( max > 0 )) || return 0
  [[ -f "$log" ]] || return 0
  local size tmp
  size=$(wc -c < "$log" 2>/dev/null) || return 0
  (( size > max )) || return 0
  tmp="${log}.tmp.$$"
  tail -c "$max" "$log" > "$tmp" 2>/dev/null && cat "$tmp" > "$log"
  rm -f "$tmp"
}`

func migrateMissingCodexState(dstHome, srcHome string) error {
	dstHome = strings.TrimSpace(dstHome)
	srcHome = strings.TrimSpace(srcHome)
	if dstHome == "" || srcHome == "" || sameFilesystemPath(dstHome, srcHome) {
		return nil
	}
	srcCodex := filepath.Join(srcHome, ".codex")
	dstCodex := filepath.Join(dstHome, ".codex")
	if st, err := os.Stat(srcCodex); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat legacy Codex home %s: %w", srcCodex, err)
	} else if !st.IsDir() {
		return nil
	}
	if err := os.MkdirAll(dstCodex, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dstCodex, err)
	}
	for _, rel := range []string{"sessions", "rollouts", "shell_snapshots", "log"} {
		if err := copyTreeMissing(filepath.Join(srcCodex, rel), filepath.Join(dstCodex, rel)); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(srcCodex)
	if err != nil {
		return fmt.Errorf("read legacy Codex home %s: %w", srcCodex, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !shouldCopyCodexRootFile(entry.Name()) {
			continue
		}
		if err := copyFileMissing(filepath.Join(srcCodex, entry.Name()), filepath.Join(dstCodex, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func sameFilesystemPath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	if resolvedA, err := filepath.EvalSymlinks(a); err == nil {
		a = filepath.Clean(resolvedA)
	}
	if resolvedB, err := filepath.EvalSymlinks(b); err == nil {
		b = filepath.Clean(resolvedB)
	}
	return a == b
}

func shouldCopyCodexRootFile(name string) bool {
	switch name {
	case "history.jsonl", "installation_id", "models_cache.json", "version.json", ".personality_migration":
		return true
	}
	return strings.HasPrefix(name, "state_") || strings.HasPrefix(name, "logs_")
}

func copyTreeMissing(srcRoot, dstRoot string) error {
	if st, err := os.Stat(srcRoot); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", srcRoot, err)
	} else if !st.IsDir() {
		return nil
	}
	return filepath.WalkDir(srcRoot, func(src string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcRoot, src)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstRoot, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(dst, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFileMissing(src, dst)
	})
}

func copyFileMissing(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if _, err := os.Stat(dst); err == nil {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", dst, err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	if err := os.Chtimes(dst, info.ModTime(), info.ModTime()); err != nil {
		return fmt.Errorf("preserve mtime for %s: %w", dst, err)
	}
	return nil
}

func SeedSSH(hostHome string, force bool) error {
	hostHome = strings.TrimSpace(hostHome)
	if hostHome == "" {
		return nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(userHome) == "" {
		return nil
	}
	srcDir := filepath.Join(userHome, ".ssh")
	if st, err := os.Stat(srcDir); err != nil || !st.IsDir() {
		return nil
	}
	targetDir := filepath.Join(hostHome, ".ssh")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", targetDir, err)
	}
	for _, file := range []string{
		"id_ed25519",
		"id_ed25519.pub",
		"id_rsa",
		"id_rsa.pub",
	} {
		src := filepath.Join(srcDir, file)
		data, err := os.ReadFile(src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read SSH seed %s: %w", src, err)
		}
		target := filepath.Join(targetDir, file)
		if !force {
			if _, err := os.Stat(target); err == nil {
				continue
			} else if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("stat SSH seed target %s: %w", target, err)
			}
		}
		mode := os.FileMode(0o600)
		if strings.HasSuffix(file, ".pub") {
			mode = 0o644
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return fmt.Errorf("write SSH seed %s: %w", target, err)
		}
		if err := os.Chmod(target, mode); err != nil {
			return fmt.Errorf("chmod SSH seed %s: %w", target, err)
		}
	}
	return nil
}

func SeedCodexAuth(hostHome string, force bool) error {
	hostHome = strings.TrimSpace(hostHome)
	if hostHome == "" {
		return nil
	}
	src := strings.TrimSpace(os.Getenv("CODEX_AUTH_JSON"))
	if src == "" {
		codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
		if codexHome == "" {
			if home, err := os.UserHomeDir(); err == nil {
				codexHome = filepath.Join(home, ".codex")
			}
		}
		if codexHome != "" {
			src = filepath.Join(codexHome, "auth.json")
		}
	}
	if src == "" {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read Codex auth %s: %w", src, err)
	}
	targetDir := filepath.Join(hostHome, ".codex")
	target := filepath.Join(targetDir, "auth.json")
	if !force {
		if _, err := os.Stat(target); err == nil {
			return nil
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("stat Codex auth %s: %w", target, err)
		}
	}
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", targetDir, err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return fmt.Errorf("write Codex auth %s: %w", target, err)
	}
	return nil
}

// SeedCodexConfig materializes only the immutable GUI-target config selected
// while the plan was built. Ordinary native plans have no selection and do
// not seed config, regardless of hostile legacy ambient environment.
func SeedCodexConfig(p nativeplan.Plan) error {
	if p.GUITargetConfig == nil {
		return nil
	}
	data, err := nativeplan.ReadValidatedGUITargetConfigSource(p)
	if err != nil {
		return err
	}
	return seedCodexConfigData(p.Agent.HostHome, data)
}

func seedCodexConfigData(hostHome string, data []byte) error {
	rawHostHome := hostHome
	hostHome = strings.TrimSpace(hostHome)
	if hostHome == "" || hostHome != rawHostHome || !filepath.IsAbs(hostHome) || filepath.Clean(hostHome) != hostHome || hostHome == string(filepath.Separator) {
		return fmt.Errorf("GUI target host home must be an absolute canonical path: %s", hostHome)
	}
	if len(data) == 0 {
		return fmt.Errorf("Codex config source data is empty")
	}
	return seedCodexConfigDataForUID(hostHome, data, os.Geteuid())
}

type projectedConfigFileIdentity struct {
	device uint64
	inode  uint64
}

func seedCodexConfigDataForUID(hostHome string, data []byte, expectedUID int) error {
	home, err := openOrCreateCanonicalOwnedDirectory(hostHome, expectedUID, 0o700)
	if err != nil {
		return fmt.Errorf("prepare GUI target host home %s: %w", hostHome, err)
	}
	defer home.Close()
	codexDirPath := filepath.Join(hostHome, ".codex")
	codexDir, err := openOrCreateOwnedDirectoryAt(home, ".codex", codexDirPath, expectedUID, 0o700)
	if err != nil {
		return err
	}
	defer codexDir.Close()

	const targetName = "config.toml"
	target := filepath.Join(codexDirPath, targetName)
	current, err := openRegularFileAtNoFollow(codexDir, targetName, target)
	if err == nil {
		identity, uid, statErr := openedFileIdentityAndUID(current)
		if statErr != nil {
			_ = current.Close()
			return fmt.Errorf("inspect Codex config target %s: %w", target, statErr)
		}
		if uid != expectedUID {
			_ = current.Close()
			return fmt.Errorf("Codex config target %s is owned by uid %d, want %d", target, uid, expectedUID)
		}
		existing, readErr := io.ReadAll(current)
		if readErr != nil {
			_ = current.Close()
			return fmt.Errorf("read Codex config target %s: %w", target, readErr)
		}
		if bytes.Equal(existing, data) {
			if err := syscall.Fchmod(int(current.Fd()), 0o600); err != nil {
				_ = current.Close()
				return fmt.Errorf("chmod Codex config target %s: %w", target, err)
			}
			if err := current.Sync(); err != nil {
				_ = current.Close()
				return fmt.Errorf("sync Codex config target %s: %w", target, err)
			}
			if err := current.Close(); err != nil {
				return fmt.Errorf("close Codex config target %s: %w", target, err)
			}
			return verifyProjectedCodexConfigAt(codexDir, targetName, target, data, expectedUID, &identity)
		}
		if err := current.Close(); err != nil {
			return fmt.Errorf("close Codex config target %s: %w", target, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	var staging *os.File
	var stagingName string
	for attempt := 0; attempt < 16; attempt++ {
		stagingName = fmt.Sprintf(".config.toml.new-%d-%d-%d", os.Getpid(), time.Now().UnixNano(), attempt)
		fd, openErr := syscall.Openat(
			int(codexDir.Fd()),
			stagingName,
			syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_CLOEXEC|syscall.O_NOFOLLOW,
			0o600,
		)
		if errors.Is(openErr, syscall.EEXIST) {
			continue
		}
		if openErr != nil {
			return fmt.Errorf("create Codex config staging file beside %s: %w", target, openErr)
		}
		staging = os.NewFile(uintptr(fd), stagingName)
		if staging == nil {
			_ = syscall.Close(fd)
			return fmt.Errorf("create Codex config staging file beside %s: invalid file descriptor", target)
		}
		break
	}
	if staging == nil {
		return fmt.Errorf("create unique Codex config staging file beside %s", target)
	}
	committed := false
	defer func() {
		_ = staging.Close()
		if !committed {
			_ = syscall.Unlinkat(int(codexDir.Fd()), stagingName)
		}
	}()
	if err := syscall.Fchmod(int(staging.Fd()), 0o600); err != nil {
		return fmt.Errorf("chmod Codex config staging file %s: %w", stagingName, err)
	}
	stagingIdentity, stagingUID, err := openedFileIdentityAndUID(staging)
	if err != nil {
		return fmt.Errorf("inspect Codex config staging file %s: %w", stagingName, err)
	}
	if stagingUID != expectedUID {
		return fmt.Errorf("Codex config staging file %s is owned by uid %d, want %d", stagingName, stagingUID, expectedUID)
	}
	if _, err := staging.Write(data); err != nil {
		return fmt.Errorf("write Codex config staging file %s: %w", stagingName, err)
	}
	if err := staging.Sync(); err != nil {
		return fmt.Errorf("sync Codex config staging file %s: %w", stagingName, err)
	}
	if err := staging.Close(); err != nil {
		return fmt.Errorf("close Codex config staging file %s: %w", stagingName, err)
	}
	if err := syscall.Renameat(int(codexDir.Fd()), stagingName, int(codexDir.Fd()), targetName); err != nil {
		return fmt.Errorf("install Codex config target %s atomically: %w", target, err)
	}
	committed = true
	if err := codexDir.Sync(); err != nil {
		return fmt.Errorf("sync Codex config directory %s: %w", codexDirPath, err)
	}
	return verifyProjectedCodexConfigAt(codexDir, targetName, target, data, expectedUID, &stagingIdentity)
}

func openOrCreateCanonicalOwnedDirectory(path string, expectedUID int, mode os.FileMode) (*os.File, error) {
	parentPath := filepath.Dir(path)
	base := filepath.Base(path)
	parent, err := openCanonicalDirectoryNoFollow(parentPath)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	directory, err := openDirectoryAtNoFollow(parent, base)
	if errors.Is(err, os.ErrNotExist) {
		_, parentUID, statErr := openedFileIdentityAndUID(parent)
		if statErr != nil {
			return nil, fmt.Errorf("inspect parent directory %s: %w", parentPath, statErr)
		}
		if parentUID != expectedUID {
			return nil, fmt.Errorf("refuse to create %s beneath parent owned by uid %d, want %d", path, parentUID, expectedUID)
		}
		if err := syscall.Mkdirat(int(parent.Fd()), base, uint32(mode.Perm())); err != nil && !errors.Is(err, syscall.EEXIST) {
			return nil, fmt.Errorf("mkdir %s: %w", path, err)
		}
		directory, err = openDirectoryAtNoFollow(parent, base)
	}
	if err != nil {
		return nil, fmt.Errorf("open directory %s without following links: %w", path, err)
	}
	if err := requireOwnedDirectory(directory, path, expectedUID, mode); err != nil {
		_ = directory.Close()
		return nil, err
	}
	return directory, nil
}

func openOrCreateOwnedDirectoryAt(parent *os.File, name, path string, expectedUID int, mode os.FileMode) (*os.File, error) {
	directory, err := openDirectoryAtNoFollow(parent, name)
	if errors.Is(err, os.ErrNotExist) {
		if err := syscall.Mkdirat(int(parent.Fd()), name, uint32(mode.Perm())); err != nil && !errors.Is(err, syscall.EEXIST) {
			return nil, fmt.Errorf("mkdir %s: %w", path, err)
		}
		directory, err = openDirectoryAtNoFollow(parent, name)
	}
	if err != nil {
		return nil, fmt.Errorf("open directory %s without following links: %w", path, err)
	}
	if err := requireOwnedDirectory(directory, path, expectedUID, mode); err != nil {
		_ = directory.Close()
		return nil, err
	}
	return directory, nil
}

func openCanonicalDirectoryNoFollow(path string) (*os.File, error) {
	rawPath := path
	path = strings.TrimSpace(path)
	if path == "" || path != rawPath || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("directory path must be absolute and canonical: %s", path)
	}
	rootFD, err := syscall.Open(string(filepath.Separator), syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open filesystem root: %w", err)
	}
	current := os.NewFile(uintptr(rootFD), string(filepath.Separator))
	if current == nil {
		_ = syscall.Close(rootFD)
		return nil, fmt.Errorf("open filesystem root: invalid file descriptor")
	}
	if path == string(filepath.Separator) {
		return current, nil
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" || component == "." || component == ".." {
			_ = current.Close()
			return nil, fmt.Errorf("directory path has a non-canonical component: %s", path)
		}
		nextFD, openErr := syscall.Openat(int(current.Fd()), component, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		_ = current.Close()
		if openErr != nil {
			return nil, fmt.Errorf("open directory component %q without following links: %w", component, openErr)
		}
		current = os.NewFile(uintptr(nextFD), component)
		if current == nil {
			_ = syscall.Close(nextFD)
			return nil, fmt.Errorf("open directory component %q: invalid file descriptor", component)
		}
	}
	return current, nil
}

func openDirectoryAtNoFollow(parent *os.File, name string) (*os.File, error) {
	fd, err := syscall.Openat(int(parent.Fd()), name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: name, Err: err}
	}
	directory := os.NewFile(uintptr(fd), name)
	if directory == nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("openat %s: invalid file descriptor", name)
	}
	return directory, nil
}

func requireOwnedDirectory(directory *os.File, path string, expectedUID int, mode os.FileMode) error {
	_, uid, err := openedFileIdentityAndUID(directory)
	if err != nil {
		return fmt.Errorf("inspect directory %s: %w", path, err)
	}
	if uid != expectedUID {
		return fmt.Errorf("directory %s is owned by uid %d, want %d", path, uid, expectedUID)
	}
	if err := syscall.Fchmod(int(directory.Fd()), uint32(mode.Perm())); err != nil {
		return fmt.Errorf("chmod directory %s: %w", path, err)
	}
	return nil
}

func openRegularFileAtNoFollow(parent *os.File, name, path string) (*os.File, error) {
	fd, err := syscall.Openat(int(parent.Fd()), name, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("Codex config target %s must be a regular non-symlink file", path)
		}
		return nil, &os.PathError{Op: "openat", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, statErr
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("Codex config target %s must be a regular non-symlink file", path)
	}
	return file, nil
}

func openedFileIdentityAndUID(file *os.File) (projectedConfigFileIdentity, int, error) {
	info, err := file.Stat()
	if err != nil {
		return projectedConfigFileIdentity{}, 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return projectedConfigFileIdentity{}, 0, fmt.Errorf("filesystem stat identity is unavailable")
	}
	return projectedConfigFileIdentity{device: uint64(stat.Dev), inode: stat.Ino}, int(stat.Uid), nil
}

func verifyProjectedCodexConfigAt(parent *os.File, name, path string, want []byte, expectedUID int, expectedIdentity *projectedConfigFileIdentity) error {
	installed, err := openRegularFileAtNoFollow(parent, name, path)
	if err != nil {
		return fmt.Errorf("verify Codex config target %s: %w", path, err)
	}
	defer installed.Close()
	identity, uid, err := openedFileIdentityAndUID(installed)
	if err != nil {
		return fmt.Errorf("verify Codex config target %s identity: %w", path, err)
	}
	if expectedIdentity != nil && identity != *expectedIdentity {
		return fmt.Errorf("Codex config target %s changed identity during installation", path)
	}
	if uid != expectedUID {
		return fmt.Errorf("Codex config target %s is owned by uid %d, want %d", path, uid, expectedUID)
	}
	info, err := installed.Stat()
	if err != nil {
		return fmt.Errorf("verify Codex config target %s mode: %w", path, err)
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("Codex config target %s mode is %04o, want 0600", path, info.Mode().Perm())
	}
	actual, err := io.ReadAll(installed)
	if err != nil {
		return fmt.Errorf("verify Codex config target %s bytes: %w", path, err)
	}
	wantDigest := sha256.Sum256(want)
	actualDigest := sha256.Sum256(actual)
	if !bytes.Equal(actual, want) || actualDigest != wantDigest {
		return fmt.Errorf("Codex config target %s bytes or SHA-256 do not match the immutable source", path)
	}
	return nil
}

func SeedAWS(hostHome string, force bool) error {
	hostHome = strings.TrimSpace(hostHome)
	if hostHome == "" {
		return nil
	}
	srcAWS := strings.TrimSpace(os.Getenv("DEVKIT_AWS_HOME"))
	if srcAWS == "" {
		if configPath := strings.TrimSpace(os.Getenv("AWS_CONFIG_FILE")); configPath != "" {
			srcAWS = filepath.Dir(configPath)
		}
	}
	if srcAWS == "" {
		if home, err := os.UserHomeDir(); err == nil {
			srcAWS = filepath.Join(home, ".aws")
		}
	}
	if srcAWS == "" {
		return nil
	}
	if st, err := os.Stat(srcAWS); err != nil || !st.IsDir() {
		return nil
	}

	targetAWS := filepath.Join(hostHome, ".aws")
	if err := materializeAWSHome(targetAWS); err != nil {
		return err
	}
	if err := os.MkdirAll(targetAWS, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", targetAWS, err)
	}
	if err := os.Chmod(targetAWS, 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", targetAWS, err)
	}
	for _, rel := range []string{"sso", "cli"} {
		if err := ensureAWSDir(filepath.Join(targetAWS, rel)); err != nil {
			return err
		}
	}
	if err := copyAWSFile(filepath.Join(srcAWS, "config"), filepath.Join(targetAWS, "config"), 0o600, force); err != nil {
		return err
	}

	credentialsSource := filepath.Join(srcAWS, "credentials")
	if override := strings.TrimSpace(os.Getenv("AWS_SHARED_CREDENTIALS_FILE")); override != "" {
		credentialsSource = override
	}
	if err := copyAWSFile(credentialsSource, filepath.Join(targetAWS, "credentials"), 0o600, force); err != nil {
		return err
	}
	for _, rel := range []string{
		filepath.Join("sso", "cache"),
		filepath.Join("cli", "cache"),
	} {
		if err := copyAWSDir(filepath.Join(srcAWS, rel), filepath.Join(targetAWS, rel), force); err != nil {
			return err
		}
	}
	return nil
}

func materializeAWSHome(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("lstat AWS state target %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove external AWS state symlink %s: %w", path, err)
		}
		return nil
	}
	if !info.IsDir() {
		return fmt.Errorf("AWS state target %s exists and is not a directory", path)
	}
	return nil
}

func ensureAWSDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func copyAWSFile(src, dst string, mode os.FileMode, force bool) error {
	src = strings.TrimSpace(src)
	dst = strings.TrimSpace(dst)
	if src == "" || dst == "" {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read AWS state %s: %w", src, err)
	}
	if !force {
		if existing, err := os.ReadFile(dst); err == nil && string(existing) == string(data) {
			return nil
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read AWS state target %s: %w", dst, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		return fmt.Errorf("write AWS state %s: %w", dst, err)
	}
	return nil
}

func copyAWSDir(src, dst string, force bool) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read AWS state dir %s: %w", src, err)
	}
	if err := ensureAWSDir(dst); err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyAWSDir(srcPath, dstPath, force); err != nil {
				return err
			}
			continue
		}
		if entry.Type()&os.ModeType != 0 {
			continue
		}
		if err := copyAWSFile(srcPath, dstPath, 0o600, force); err != nil {
			return err
		}
	}
	return nil
}

func BuildBubblewrap(p nativeplan.Plan, command []string) (Command, error) {
	return buildBubblewrap(p, command, true)
}

// BuildManagedAppServerBubblewrap is reserved for the typed, systemd-owned
// GUI lifecycle. Ordinary native exec must retain parent-death coupling.
func BuildManagedAppServerBubblewrap(p nativeplan.Plan, command []string) (Command, error) {
	if p.IsolationProfile != nativeplan.IsolationProfileWorkspaceEgress {
		return Command{}, fmt.Errorf("managed app-server launcher requires workspace-egress isolation")
	}
	return buildBubblewrap(p, command, false)
}

func buildBubblewrap(p nativeplan.Plan, command []string, dieWithParent bool) (Command, error) {
	if err := nativeplan.ValidateControllerProjection(p); err != nil {
		return Command{}, err
	}
	if err := nativeplan.ValidateGitBacklinkProjection(p); err != nil {
		return Command{}, err
	}
	if err := verifyGitBacklink(p, true); err != nil {
		return Command{}, err
	}
	launchEnv := make(map[string]string, len(p.Env)+1)
	for key, value := range p.Env {
		launchEnv[key] = value
	}
	guiTargetID, hasGUITargetID := launchEnv[nativeplan.GUITargetIDEnvironment]
	if p.GUITargetConfig == nil {
		if hasGUITargetID && guiTargetID != "" {
			return Command{}, fmt.Errorf("ordinary sandbox carries undeclared GUI target identity %q", guiTargetID)
		}
		launchEnv[nativeplan.GUITargetIDEnvironment] = ""
	} else if !hasGUITargetID || guiTargetID != p.GUITargetConfig.TargetID {
		return Command{}, fmt.Errorf(
			"GUI target environment identity %q does not match validated projection %q",
			guiTargetID,
			p.GUITargetConfig.TargetID,
		)
	}
	if strings.TrimSpace(p.DevkitSandboxRoot) == "" {
		return Command{}, fmt.Errorf("devkit sandbox root is empty")
	}
	args := []string{"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
	}
	if dieWithParent {
		args = append([]string{"--die-with-parent"}, args...)
	}
	if p.IsolationProfile == nativeplan.IsolationProfileWorkspaceEgress {
		// Bubblewrap starts from the host mount namespace. Mask the WSL
		// automount root before adding the profile's narrow, source-derived
		// binds so no Windows drive remains visible inside workspace-egress.
		args = append(args, "--tmpfs", "/mnt")
	}
	if strings.TrimSpace(p.Proxy.UnixSocket) != "" {
		args = append(args, "--unshare-net")
	} else {
		args = append(args, "--share-net")
	}
	dirSet := map[string]bool{"/tmp": true}
	dirArgs := []string{}
	bindArgs := []string{}
	symlinkArgs := []string{}
	var addDir func(string)
	addDir = func(path string) {
		path = filepath.Clean(path)
		if path == "." || path == "/" || dirSet[path] {
			return
		}
		parent := filepath.Dir(path)
		if parent != "/" && !dirSet[parent] {
			addDir(parent)
		}
		dirArgs = append(dirArgs, "--dir", path)
		dirSet[path] = true
	}
	addBind := func(mode, source, target string, required bool) error {
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if source == "" || target == "" {
			if required {
				return fmt.Errorf("required bind has empty source or target")
			}
			return nil
		}
		if _, err := os.Stat(source); err != nil {
			if required {
				addDir(filepath.Dir(target))
				if mode == "ro" {
					bindArgs = append(bindArgs, "--ro-bind", source, target)
				} else {
					bindArgs = append(bindArgs, "--bind", source, target)
				}
			}
			return nil
		}
		// Optional adapters do not gate entry when absent. If supplied, they
		// retain exactly the same host-boundary validation as required binds.

		addDir(filepath.Dir(target))
		if mode == "ro" {
			bindArgs = append(bindArgs, "--ro-bind", source, target)
		} else {
			bindArgs = append(bindArgs, "--bind", source, target)
		}
		return nil
	}
	addSymlink := func(target, linkPath string) {
		addDir(filepath.Dir(linkPath))
		symlinkArgs = append(symlinkArgs, "--symlink", target, linkPath)
	}

	if err := addBind("ro", "/nix/store", "/nix/store", true); err != nil {
		return Command{}, err
	}
	if err := addBind("ro", "/nix/var/nix", "/nix/var/nix", true); err != nil {
		return Command{}, err
	}
	_ = addBind("ro", "/run/current-system", "/run/current-system", false)
	_ = addBind("ro", "/etc/nix", "/etc/nix", false)
	_ = addBind("ro", "/etc/static", "/etc/static", false)
	_ = addBind("ro", "/etc/ssl", "/etc/ssl", false)
	_ = addBind("ro", "/etc/pki", "/etc/pki", false)
	_ = addBind("ro", "/etc/passwd", "/etc/passwd", false)
	_ = addBind("ro", "/etc/group", "/etc/group", false)

	for _, bind := range p.Binds {
		if bind.Target == "/nix/store" || bind.Target == "/etc/resolv.conf" {
			continue
		}
		if err := addBind(bind.Mode, bind.Source, bind.Target, bind.Required); err != nil {
			return Command{}, err
		}
	}
	if strings.TrimSpace(p.DNS.ResolvConf) != "" {
		if err := addBind("ro", p.DNS.ResolvConf, "/etc/resolv.conf", false); err != nil {
			return Command{}, err
		}
	} else {
		_ = addBind("ro", "/etc/resolv.conf", "/etc/resolv.conf", false)
	}
	addSymlink("/run/current-system/sw/bin/env", "/usr/bin/env")
	addSymlink("/run/current-system/sw/bin/bash", "/usr/bin/bash")
	addSymlink("/run/current-system/sw/bin/bash", "/bin/bash")
	addSymlink("/run/current-system/sw/bin/sh", "/bin/sh")

	args = append(args, dirArgs...)
	args = append(args, bindArgs...)
	args = append(args, symlinkArgs...)

	gitSSHCommand, err := managedRuntimeGitSSHCommand(p)
	if err != nil {
		return Command{}, err
	}
	if gitSSHCommand != "" {
		launchEnv["GIT_SSH_COMMAND"] = gitSSHCommand
		launchEnv["GIT_SSH_VARIANT"] = "ssh"
	}

	keys := make([]string, 0, len(launchEnv))
	for key := range launchEnv {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--setenv", key, launchEnv[key])
	}

	args = append(args, "--chdir", p.DevkitSandboxRoot)
	runtimeLauncher := strings.TrimSpace(p.RuntimeLauncher)
	if runtimeLauncher == "" {
		return Command{}, fmt.Errorf("immutable native runtime launcher is required")
	}
	if !filepath.IsAbs(runtimeLauncher) {
		return Command{}, fmt.Errorf("immutable native runtime launcher must be an absolute path: %s", runtimeLauncher)
	}
	if !isExecutable(runtimeLauncher) {
		return Command{}, fmt.Errorf("immutable native runtime launcher is not executable: %s", runtimeLauncher)
	}
	bubblewrapBinary := strings.TrimSpace(p.BubblewrapBinary)
	if bubblewrapBinary == "" {
		return Command{}, fmt.Errorf("immutable native bubblewrap binary is required")
	}
	if !filepath.IsAbs(bubblewrapBinary) {
		return Command{}, fmt.Errorf("immutable native bubblewrap binary must be an absolute path: %s", bubblewrapBinary)
	}
	if !isExecutable(bubblewrapBinary) {
		return Command{}, fmt.Errorf("immutable native bubblewrap binary is not executable: %s", bubblewrapBinary)
	}
	runtimeArgs := []string{runtimeLauncher}
	runtimeArgs = append(runtimeArgs, shellCommand(p.DevkitSandboxRoot, p.Agent.ID.Project, p.Agent.SandboxWorktree, command, p.Proxy, launchEnv, false)...)
	if strings.TrimSpace(p.Proxy.UnixSocket) != "" {
		args = append(args, "/run/current-system/sw/bin/bash", "-lc", outerProxyRuntimeCommand(p.DevkitSandboxRoot, p.Agent.ID.Project, runtimeArgs, p.Proxy))
	} else {
		args = append(args, runtimeArgs...)
	}
	return Command{Path: bubblewrapBinary, Args: args, Dir: p.DevkitHostRoot}, nil
}

func ShellString(cmd Command) string {
	parts := append([]string{cmd.Path}, cmd.Args...)
	for i, part := range parts {
		parts[i] = shellQuote(part)
	}
	return strings.Join(parts, " ")
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

var nativeResolvConfSource = "/etc/resolv.conf"

func ensureResolvConf(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat resolv.conf %s: %w", path, err)
	}
	data, err := os.ReadFile(nativeResolvConfSource)
	if err != nil {
		return fmt.Errorf("read %s: %w", nativeResolvConfSource, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write resolv.conf %s: %w", path, err)
	}
	return nil
}

func outerProxyRuntimeCommand(devkitRoot string, project string, runtimeArgs []string, proxy nativeplan.ProxyConfig) string {
	script := proxyBridgeScript(devkitRoot, project, proxy)
	script += " && " + shellJoin(runtimeArgs)
	return script
}

func shellCommand(devkitRoot string, project string, workdir string, command []string, proxy nativeplan.ProxyConfig, env map[string]string, bridgeProxy bool, postgresBridge ...string) []string {
	exports := make([]string, 0, len(env))
	for key, value := range env {
		exports = append(exports, "export "+key+"="+shellQuote(value))
	}
	sort.Strings(exports)
	script := strings.Join(exports, "; ")
	if script != "" {
		script += "; "
	}
	script += "cd " + shellQuote(workdir)
	if bridgeProxy {
		script += " && " + proxyBridgeScript(devkitRoot, project, proxy)
	}
	if len(postgresBridge) > 0 && postgresBridge[0] != "" {
		script += " && " + postgresBridge[0]
	}
	if len(command) == 0 {
		if bridgeProxy {
			script += " && ${SHELL:-bash}"
		} else {
			script += " && exec ${SHELL:-bash}"
		}
	} else {
		quoted := make([]string, 0, len(command))
		for _, arg := range command {
			quoted = append(quoted, shellQuote(arg))
		}
		if bridgeProxy {
			script += " && " + strings.Join(quoted, " ")
		} else {
			script += " && exec " + strings.Join(quoted, " ")
		}
	}
	return []string{"bash", "-c", script}
}

func proxyBridgeScript(devkitRoot string, project string, proxy nativeplan.ProxyConfig) string {
	proxyURL := strings.TrimSpace(proxy.HTTPProxy)
	if proxyURL == "" {
		proxyURL = "http://127.0.0.1:18888"
	}
	devctlPath := filepath.Join(devkitRoot, "kit", "bin", "devctl")
	script := "{ " + shellQuote(devctlPath) + " -p " + shellQuote(project) + " native proxy-bridge --listen 127.0.0.1:18888 --socket " + shellQuote(proxy.UnixSocket) + " & devkit_proxy_bridge_pid=$!; }"
	script += " && trap 'kill \"$devkit_proxy_bridge_pid\" >/dev/null 2>&1 || true; wait \"$devkit_proxy_bridge_pid\" >/dev/null 2>&1 || true' EXIT"
	script += " && sleep 0.1"
	script += " && { kill -0 \"$devkit_proxy_bridge_pid\" >/dev/null 2>&1 || { echo 'native proxy bridge failed to start' >&2; exit 1; }; }"
	script += " && export HTTP_PROXY=" + shellQuote(proxyURL)
	script += " HTTPS_PROXY=" + shellQuote(proxyURL)
	script += " http_proxy=" + shellQuote(proxyURL)
	script += " https_proxy=" + shellQuote(proxyURL)
	script += " NO_PROXY=" + shellQuote(proxy.NoProxy)
	script += " no_proxy=" + shellQuote(proxy.NoProxy)
	return script
}

func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

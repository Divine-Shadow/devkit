//go:build devkitintegration

package productadapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ConfigureIntegrationPackageFromManifest binds the integration binary to one
// exact production-shaped authority fixture. It exists only in the integration
// build; production package identity remains linker-bound and has no runtime
// override.
func ConfigureIntegrationPackageFromManifest(manifestPath string) error {
	clean := filepath.Clean(manifestPath)
	if !filepath.IsAbs(clean) || clean != manifestPath {
		return fmt.Errorf("Product integration authority fixture must be an exact absolute path")
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o222 != 0 {
		return fmt.Errorf("Product integration authority fixture must be a read-only regular file")
	}
	payload, err := os.ReadFile(clean)
	if err != nil {
		return err
	}
	var manifest Manifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return err
	}
	adapter := manifest.ProductAdapter
	packageMode = "composed"
	packageAdapterExecutable = adapter.ExecutablePath
	packageGitExecutable = adapter.GitPath
	packageGitSSHExecutable = adapter.GitSSHPath
	packageEnvExecutable = adapter.EnvPath
	packageSSHExecutable = adapter.SSHPath
	packageSSHKeygenExecutable = adapter.SSHKeygenPath
	packageKnownHosts = adapter.KnownHostsPath
	packageRuntimeLauncher = adapter.RuntimeLauncherPath
	packageBubblewrapExecutable = adapter.BubblewrapPath
	packageBrokerExecutable = adapter.BrokerPath
	packageEgressAllowlist = adapter.EgressAllowlistPath
	packageCodexConfig = adapter.CodexConfigPath
	packageGovernanceEnv = adapter.GovernanceEnvPath
	packageGovernanceRepoConfig = adapter.GovernanceRepoConfigPath
	packageGovernanceRules = adapter.GovernanceRulesPath
	packageShellHook = adapter.ShellHookPath
	packageCodexExecutable = adapter.CodexExecutablePath
	packageReadinessExecutable = adapter.ReadinessExecutablePath
	packageMCPRequirement = adapter.MCPRequirementPath
	packageMountPolicyContract = adapter.MountPolicyContractPath
	packageProductProxyExecutable = adapter.ProxyHelperPath
	packageProductSupervisor = adapter.SupervisorExecutablePath
	packageProductSSHSession = adapter.SSHSessionExecutablePath
	packageProductSSHSetup = adapter.SSHSetupExecutablePath
	packageProductCodexAuthSeed1 = adapter.CodexAuthSeed1ExecutablePath
	packageProductCodexAuthSeed2 = adapter.CodexAuthSeed2ExecutablePath
	packageAdapterLaunchExecutable = "/run/wrappers/bin/devkit-product-adapter"
	packageProxyLaunchExecutable = "/run/wrappers/bin/devkit-product-proxy"
	packageSupervisorLaunchExecutable = "/run/wrappers/bin/devkit-product-adapter-supervisor"
	packageSSHSessionLaunchExecutable = "/run/wrappers/bin/devkit-product-ssh-session"
	packageSSHSetupLaunchExecutable = "/run/wrappers/bin/devkit-product-ssh-setup"
	packageCodexAuthSeed1Launch = "/run/wrappers/bin/devkit-product-codex-auth-seed-1"
	packageCodexAuthSeed2Launch = "/run/wrappers/bin/devkit-product-codex-auth-seed-2"
	packageProductSSHSetupContract = adapter.SSHSetupContractPath
	packageProductSessionContract = adapter.SSHSessionContractPath
	testAuthorityLocator = clean
	return nil
}

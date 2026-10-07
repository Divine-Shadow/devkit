package launch

import (
	"fmt"
	"path/filepath"
	"strings"

	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

// Git consumes the current execution's connector through its environment,
// ahead of any saved core.sshCommand. Constructing this command does not open
// or change the existing SSH configuration, identities, or credential homes.
func managedRuntimeGitSSHCommand(p nativeplan.Plan) (string, error) {
	if p.IsolationProfile != nativeplan.IsolationProfileWorkspaceEgress || strings.TrimSpace(p.Proxy.AllowlistPath) == "" {
		return "", nil
	}
	for _, key := range []string{"GIT_SSH_COMMAND", "GIT_SSH_VARIANT"} {
		if _, supplied := p.Env[key]; supplied {
			return "", fmt.Errorf("managed native Git transport refuses a caller %s override", key)
		}
	}
	home := strings.TrimSpace(p.Agent.SandboxHome)
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("managed native Git transport requires an absolute sandbox home")
	}
	proxyCommand, err := gitManagedProxyCommand(p)
	if err != nil {
		return "", err
	}
	authority, err := resolvePackageSSHAuthority()
	if err != nil {
		return "", err
	}
	command, err := authority.Command(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return "", err
	}
	// OpenSSH command-line options take precedence over the configuration's
	// previous execution socket. Every other Host option, including identity
	// selection and strict host-key admission, stays in the existing config.
	command += " -o " + shellQuote("ProxyCommand="+proxyCommand)
	return command, nil
}

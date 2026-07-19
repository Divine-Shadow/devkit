//go:build devkitintegration

package launch

import (
	"os"
	"strings"
)

func init() {
	resolveOuroGovernanceRuntimeIdentityForPlan = func(string) (ouroGovernanceRuntimeIdentity, error) {
		return ouroGovernanceRuntimeIdentity{
			RuntimeBundlePath: "/nix/store/ffffffffffffffffffffffffffffffff-dev-all-runtime-bundle",
		}, nil
	}
	if source := strings.TrimSpace(os.Getenv("DEVKIT_INTEGRATION_RESOLV_SOURCE")); source != "" {
		nativeResolvConfSource = source
	}
}

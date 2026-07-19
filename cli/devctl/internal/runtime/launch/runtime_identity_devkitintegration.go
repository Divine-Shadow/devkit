//go:build devkitintegration

package launch

func init() {
	resolveOuroGovernanceRuntimeIdentityForPlan = func(string) (ouroGovernanceRuntimeIdentity, error) {
		return ouroGovernanceRuntimeIdentity{
			RuntimeBundlePath: "/nix/store/ffffffffffffffffffffffffffffffff-dev-all-runtime-bundle",
		}, nil
	}
}

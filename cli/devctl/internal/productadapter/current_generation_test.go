//go:build !devkitintegration

package productadapter

import "testing"

func TestAttestationIsRoleBound(t *testing.T) {
	attestation := Attestation{role: RoleSupervisor}
	if err := attestation.consume(RoleSupervisor); err != nil {
		t.Fatal(err)
	}
	if err := attestation.consume(RoleAdapter); err == nil {
		t.Fatal("accepted a namespace attestation for another Product role")
	}
	if err := (Attestation{}).consume(RoleSupervisor); err == nil {
		t.Fatal("accepted a missing Product namespace attestation")
	}
}

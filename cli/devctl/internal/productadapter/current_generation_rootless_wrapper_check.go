//go:build devkitrootlesswrappercheck

package productadapter

import (
	"fmt"
	"os"
	"path/filepath"
)

// This file is the single test-only process-identity seam used by the named
// rootless Nix gate. It preserves namespace, selector, manifest, executable,
// and current-generation checks. The real setuid transition remains a QEMU
// promotion requirement.
type Attestation struct {
	role    Role
	realUID int
	realGID int
}

func AttestInitialNamespaces(role Role) (Attestation, error) {
	if !isControllerSeedRole(role) || os.Geteuid() != 0 || os.Getegid() != 0 {
		return Attestation{}, fmt.Errorf("rootless wrapper check requires its isolated uid-0 namespace")
	}
	held := make([]*os.File, 0, 4)
	closeHeld := func() {
		for _, file := range held {
			_ = file.Close()
		}
	}
	for _, namespace := range []string{"user", "mnt"} {
		self, err := os.Open(filepath.Join("/proc/self/ns", namespace))
		if err != nil {
			closeHeld()
			return Attestation{}, fmt.Errorf("open Product %s namespace: %w", namespace, err)
		}
		held = append(held, self)
		initial, err := os.Open(filepath.Join("/proc/1/ns", namespace))
		if err != nil {
			closeHeld()
			return Attestation{}, fmt.Errorf("open initial Product %s namespace: %w", namespace, err)
		}
		held = append(held, initial)
		selfInfo, err := self.Stat()
		if err != nil {
			closeHeld()
			return Attestation{}, fmt.Errorf("stat Product %s namespace: %w", namespace, err)
		}
		initialInfo, err := initial.Stat()
		if err != nil {
			closeHeld()
			return Attestation{}, fmt.Errorf("stat initial Product %s namespace: %w", namespace, err)
		}
		if !os.SameFile(selfInfo, initialInfo) {
			closeHeld()
			return Attestation{}, fmt.Errorf("Product adapter refuses caller-created %s namespace", namespace)
		}
	}
	closeHeld()
	return Attestation{role: role, realUID: 1000, realGID: 1000}, nil
}

func (attestation Attestation) validateController(ownerUID int) error {
	if !isControllerSeedRole(attestation.role) || attestation.realUID != ownerUID {
		return fmt.Errorf("offline Product seed caller does not match controller authority")
	}
	return nil
}

func validateControllerEffectIdentity() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("offline Product SSH setup requires uid 0")
	}
	return nil
}

func (attestation Attestation) consume(role Role) error {
	if attestation.role == "" || attestation.role != role {
		return fmt.Errorf("Product authority requires matching initial-namespace attestation")
	}
	return nil
}

func validateConsumerProcessIdentity(uid, gid int) error {
	return fmt.Errorf("rootless wrapper check admits controller seed roles only")
}

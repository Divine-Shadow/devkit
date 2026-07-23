//go:build devkitintegration

package productadapter

import (
	"fmt"
	"os"
)

type Attestation struct {
	role    Role
	realUID int
	realGID int
}

func AttestInitialNamespaces(role Role) (Attestation, error) {
	return Attestation{role: role, realUID: os.Getuid(), realGID: os.Getgid()}, nil
}

func (attestation Attestation) validateController(ownerUID int) error {
	if !isControllerSeedRole(attestation.role) || attestation.realUID != ownerUID {
		return fmt.Errorf("offline Product seed caller does not match controller authority")
	}
	return nil
}

func validateControllerEffectIdentity() error {
	if os.Geteuid() != os.Getuid() {
		return fmt.Errorf("Product integration controller identity changed unexpectedly")
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
	if os.Geteuid() != uid || os.Getegid() != gid {
		return fmt.Errorf("Product consumer process identity does not match manifest uid/gid")
	}
	return nil
}

func holdCurrentGeneration(
	role Role,
	adapter AdapterManifest,
	attestation Attestation,
) ([]*os.File, error) {
	if err := attestation.consume(role); err != nil {
		return nil, err
	}
	expected := adapter.ExecutablePath
	if role == RoleProxy {
		expected = adapter.ProxyHelperPath
	} else if role == RoleSupervisor {
		expected = adapter.SupervisorExecutablePath
	} else if role == RoleSSHSession {
		expected = adapter.SSHSessionExecutablePath
	} else if role == RoleSSHSetup {
		expected = adapter.SSHSetupExecutablePath
	} else if role == RoleCodexAuthSeed1 {
		expected = adapter.CodexAuthSeed1ExecutablePath
	} else if role == RoleCodexAuthSeed2 {
		expected = adapter.CodexAuthSeed2ExecutablePath
	}
	running, err := os.Open("/proc/self/exe")
	if err != nil {
		return nil, err
	}
	expectedFile, err := os.Open(expected)
	if err != nil {
		_ = running.Close()
		return nil, err
	}
	runningInfo, err := running.Stat()
	if err != nil {
		_ = running.Close()
		_ = expectedFile.Close()
		return nil, err
	}
	expectedInfo, err := expectedFile.Stat()
	if err != nil {
		_ = running.Close()
		_ = expectedFile.Close()
		return nil, err
	}
	if !os.SameFile(runningInfo, expectedInfo) {
		_ = running.Close()
		_ = expectedFile.Close()
		return nil, fmt.Errorf("running Product fixture executable does not match manifest executable")
	}
	return []*os.File{running, expectedFile}, nil
}

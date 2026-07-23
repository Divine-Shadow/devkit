//go:build !devkitintegration

package productadapter

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	linuxCapabilityVersion3 = 0x20080522
	prCapAmbient            = 47
	prCapAmbientClearAll    = 4
)

type linuxCapabilityHeader struct {
	Version uint32
	PID     int32
}

type linuxCapabilityData struct {
	Effective   uint32
	Permitted   uint32
	Inheritable uint32
}

// Attestation is minted only after the package entrypoint has proved that it
// entered through the source-defined setuid wrapper, is in PID 1's user and
// mount namespaces, and (for runtime roles) has discarded every elevated
// credential before authority or caller input is consumed.
type Attestation struct {
	role    Role
	realUID int
	realGID int
}

func AttestInitialNamespaces(role Role) (Attestation, error) {
	realUID, effectiveUID, savedUID, err := getresuid()
	if err != nil {
		return Attestation{}, err
	}
	realGID, effectiveGID, savedGID, err := getresgid()
	if err != nil {
		return Attestation{}, err
	}
	if role == RoleSSHSetup {
		if realUID == 0 || effectiveUID != 0 || savedUID != 0 ||
			realGID == 0 || effectiveGID != realGID || savedGID != realGID {
			return Attestation{}, fmt.Errorf("offline Product SSH setup requires its controller-only privileged entry wrapper")
		}
	} else if realUID == 0 || effectiveUID != 0 || savedUID != 0 ||
		realGID == 0 || effectiveGID != realGID || savedGID != realGID {
		return Attestation{}, fmt.Errorf("Product runtime requires its package-owned privileged entry wrapper")
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
	if role != RoleSSHSetup {
		if err := dropNamespaceAttestationPrivileges(realUID, realGID); err != nil {
			return Attestation{}, err
		}
	}
	return Attestation{role: role, realUID: realUID, realGID: realGID}, nil
}

func (attestation Attestation) validateController(ownerUID int) error {
	if attestation.role != RoleSSHSetup || attestation.realUID != ownerUID {
		return fmt.Errorf("offline Product SSH setup caller does not match controller authority")
	}
	return nil
}

func (attestation Attestation) consume(role Role) error {
	if attestation.role == "" || attestation.role != role {
		return fmt.Errorf("Product authority requires matching initial-namespace attestation")
	}
	return nil
}

func dropNamespaceAttestationPrivileges(realUID, realGID int) error {
	if err := syscall.Setgroups([]int{}); err != nil {
		return fmt.Errorf("clear Product namespace attestation supplementary groups: %w", err)
	}
	if err := syscall.Setresgid(realGID, realGID, realGID); err != nil {
		return fmt.Errorf("drop Product namespace attestation group: %w", err)
	}
	if err := syscall.Setresuid(realUID, realUID, realUID); err != nil {
		return fmt.Errorf("drop Product namespace attestation user: %w", err)
	}
	if _, _, errno := syscall.AllThreadsSyscall6(
		syscall.SYS_PRCTL,
		prCapAmbient,
		prCapAmbientClearAll,
		0,
		0,
		0,
		0,
	); errno != 0 {
		return fmt.Errorf("clear Product namespace attestation ambient capabilities: %w", errno)
	}
	header := linuxCapabilityHeader{Version: linuxCapabilityVersion3}
	data := [2]linuxCapabilityData{}
	if _, _, errno := syscall.AllThreadsSyscall(
		syscall.SYS_CAPSET,
		uintptr(unsafe.Pointer(&header)),
		uintptr(unsafe.Pointer(&data[0])),
		0,
	); errno != 0 {
		return fmt.Errorf("drop Product namespace attestation capabilities: %w", errno)
	}
	ruid, euid, suid, err := getresuid()
	if err != nil {
		return err
	}
	rgid, egid, sgid, err := getresgid()
	if err != nil {
		return err
	}
	if ruid != realUID || euid != realUID || suid != realUID ||
		rgid != realGID || egid != realGID || sgid != realGID {
		return fmt.Errorf("Product namespace attestation did not drop privilege")
	}
	return nil
}

func getresuid() (real, effective, saved int, result error) {
	var ruid, euid, suid uint32
	if _, _, errno := syscall.RawSyscall(
		syscall.SYS_GETRESUID,
		uintptr(unsafe.Pointer(&ruid)),
		uintptr(unsafe.Pointer(&euid)),
		uintptr(unsafe.Pointer(&suid)),
	); errno != 0 {
		return 0, 0, 0, fmt.Errorf("read Product process user identity: %w", errno)
	}
	return int(ruid), int(euid), int(suid), nil
}

func getresgid() (real, effective, saved int, result error) {
	var rgid, egid, sgid uint32
	if _, _, errno := syscall.RawSyscall(
		syscall.SYS_GETRESGID,
		uintptr(unsafe.Pointer(&rgid)),
		uintptr(unsafe.Pointer(&egid)),
		uintptr(unsafe.Pointer(&sgid)),
	); errno != 0 {
		return 0, 0, 0, fmt.Errorf("read Product process group identity: %w", errno)
	}
	return int(rgid), int(egid), int(sgid), nil
}

func validateConsumerProcessIdentity(uid, gid int) error {
	ruid, euid, suid, err := getresuid()
	if err != nil {
		return err
	}
	rgid, egid, sgid, err := getresgid()
	if err != nil {
		return err
	}
	if ruid != uid || euid != uid || suid != uid ||
		rgid != gid || egid != gid || sgid != gid {
		return fmt.Errorf("Product consumer process identity does not match manifest uid/gid")
	}
	groups, err := syscall.Getgroups()
	if err != nil {
		return fmt.Errorf("read Product consumer supplementary groups: %w", err)
	}
	if len(groups) != 0 {
		return fmt.Errorf("Product consumer process retains supplementary groups")
	}
	return nil
}

func holdCurrentGeneration(role Role, adapter AdapterManifest, attestation Attestation) ([]*os.File, error) {
	if err := attestation.consume(role); err != nil {
		return nil, err
	}
	// The manifest has already selected the immutable executable. The
	// current-system handle is only an activated-generation equality check; it
	// never selects, derives, or rewrites Product runtime identity.
	expected := adapter.ExecutablePath
	name := "product-adapter"
	if role == RoleProxy {
		expected = adapter.ProxyHelperPath
		name = "product-proxy"
	} else if role == RoleSupervisor {
		expected = adapter.SupervisorExecutablePath
		name = "product-adapter-supervisor"
	} else if role == RoleSSHSession {
		expected = adapter.SSHSessionExecutablePath
		name = "product-ssh-session"
	} else if role == RoleSSHSetup {
		expected = adapter.SSHSetupExecutablePath
		name = "product-ssh-setup"
	}
	running, err := os.Open("/proc/self/exe")
	if err != nil {
		return nil, fmt.Errorf("open running Product executable: %w", err)
	}
	closeOnError := func(values ...*os.File) {
		for _, value := range values {
			if value != nil {
				_ = value.Close()
			}
		}
	}
	expectedFile, err := os.Open(expected)
	if err != nil {
		closeOnError(running)
		return nil, fmt.Errorf("open manifest Product executable: %w", err)
	}
	runningInfo, err := running.Stat()
	if err != nil {
		closeOnError(running, expectedFile)
		return nil, err
	}
	expectedInfo, err := expectedFile.Stat()
	if err != nil {
		closeOnError(running, expectedFile)
		return nil, err
	}
	if !os.SameFile(runningInfo, expectedInfo) {
		closeOnError(running, expectedFile)
		return nil, fmt.Errorf("running Product executable does not match manifest executable")
	}
	currentPath := filepath.Join("/run/current-system/sw/bin", name)
	currentFile, err := os.Open(currentPath)
	if err != nil {
		closeOnError(running, expectedFile)
		return nil, fmt.Errorf("open current-system Product executable %s: %w", currentPath, err)
	}
	currentInfo, err := currentFile.Stat()
	if err != nil {
		closeOnError(running, expectedFile, currentFile)
		return nil, err
	}
	if !os.SameFile(currentInfo, expectedInfo) {
		closeOnError(running, expectedFile, currentFile)
		return nil, fmt.Errorf("Product authority locator and current-system executable belong to different generations")
	}
	return []*os.File{running, expectedFile, currentFile}, nil
}

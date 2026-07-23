package productseed

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	linuxOTmpfile        = 0x410000
	linuxAtSymlinkFollow = 0x400
	linuxAtFDCWD         = -100
)

func openAnonymousFileAt(dirFD int, mode os.FileMode) (int, error) {
	fd, err := syscall.Openat(
		dirFD,
		".",
		syscall.O_RDWR|syscall.O_CLOEXEC|linuxOTmpfile,
		uint32(mode.Perm()),
	)
	if err != nil {
		return -1, fmt.Errorf(
			"create anonymous fixed-slot generation without named fallback: %w",
			err,
		)
	}
	return fd, nil
}

func linkAnonymousAtNoReplace(sourceFD, targetDirFD int, targetName string) error {
	sourcePath := fmt.Sprintf("/proc/self/fd/%d", sourceFD)
	sourcePointer, err := syscall.BytePtrFromString(sourcePath)
	if err != nil {
		return err
	}
	targetPointer, err := syscall.BytePtrFromString(targetName)
	if err != nil {
		return err
	}
	currentWorkingDirectory := linuxAtFDCWD
	_, _, errno := syscall.Syscall6(
		syscall.SYS_LINKAT,
		uintptr(currentWorkingDirectory),
		uintptr(unsafe.Pointer(sourcePointer)),
		uintptr(targetDirFD),
		uintptr(unsafe.Pointer(targetPointer)),
		linuxAtSymlinkFollow,
		0,
	)
	if errno != 0 {
		return fmt.Errorf("link anonymous fixed-slot generation without replacement: %w", errno)
	}
	return nil
}

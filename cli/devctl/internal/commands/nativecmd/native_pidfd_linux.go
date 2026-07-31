//go:build linux

package nativecmd

import "syscall"

// pidfd_open and pidfd_send_signal use the architecture-independent syscall
// numbers assigned by Linux for the supported native runtime architectures.
const (
	nativeSysPidfdSendSignal = 424
	nativeSysPidfdOpen       = 434
)

func openNativePidfd(pid int) (int, error) {
	fd, _, errno := syscall.Syscall(nativeSysPidfdOpen, uintptr(pid), 0, 0)
	if errno != 0 {
		return -1, errno
	}
	return int(fd), nil
}

func sendNativePidfdSignal(fd int, signal syscall.Signal) error {
	_, _, errno := syscall.Syscall6(
		nativeSysPidfdSendSignal,
		uintptr(fd),
		uintptr(signal),
		0,
		0,
		0,
		0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}

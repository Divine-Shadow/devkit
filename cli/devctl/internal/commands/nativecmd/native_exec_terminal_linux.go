package nativecmd

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

func nativeCommandHasTerminalInput(cmd *exec.Cmd) bool {
	input, ok := cmd.Stdin.(*os.File)
	if !ok {
		return false
	}
	var attributes syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, input.Fd(), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&attributes)))
	return errno == 0
}

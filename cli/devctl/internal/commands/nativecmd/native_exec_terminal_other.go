//go:build !linux

package nativecmd

import (
	"os"
	"os/exec"
)

func nativeCommandHasTerminalInput(cmd *exec.Cmd) bool {
	input, ok := cmd.Stdin.(*os.File)
	if !ok {
		return false
	}
	info, err := input.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

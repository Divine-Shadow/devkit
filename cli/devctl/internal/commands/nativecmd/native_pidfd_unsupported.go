//go:build !linux

package nativecmd

import (
	"fmt"
	"syscall"
)

func openNativePidfd(int) (int, error) {
	return -1, fmt.Errorf("native slot reset process disposal requires Linux pidfds")
}

func sendNativePidfdSignal(int, syscall.Signal) error {
	return fmt.Errorf("native slot reset process disposal requires Linux pidfds")
}

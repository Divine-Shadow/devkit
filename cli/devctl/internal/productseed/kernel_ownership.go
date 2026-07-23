//go:build !devkitrootlesswrappercheck

package productseed

import "syscall"

func kernelUID(logical int) int {
	return logical
}

func kernelGID(logical int) int {
	return logical
}

func kernelFchown(fd, logicalUID, logicalGID int) error {
	return syscall.Fchown(fd, logicalUID, logicalGID)
}

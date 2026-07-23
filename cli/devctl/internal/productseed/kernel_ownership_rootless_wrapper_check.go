//go:build devkitrootlesswrappercheck

package productseed

import (
	"os"
	"syscall"
)

// The Nix builder cannot exercise host setuid or subordinate UID ownership.
// This is the sole test-only ownership seam: manifest identities remain
// distinct and authoritative, while kernel ownership is projected to the
// isolated namespace identity. No path, slot, selector, mode, or bytes change.
func kernelUID(logical int) int {
	return os.Geteuid()
}

func kernelGID(logical int) int {
	return os.Getegid()
}

func kernelFchown(fd, logicalUID, logicalGID int) error {
	return syscall.Fchown(fd, os.Geteuid(), os.Getegid())
}

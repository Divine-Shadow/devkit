// product-runtime-exec is the package-owned, authority-free exec boundary used
// only after product-adapter has constructed the exact sandbox and projected a
// one-shot capability. It performs no source, identity, environment, or path
// discovery.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func main() {
	if len(os.Args) < 2 || !filepath.IsAbs(os.Args[1]) || filepath.Clean(os.Args[1]) != os.Args[1] {
		_, _ = fmt.Fprintln(os.Stderr, "product-runtime-exec: exact absolute executable is required")
		os.Exit(64)
	}
	if err := syscall.Exec(os.Args[1], os.Args[1:], os.Environ()); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "product-runtime-exec:", err)
		os.Exit(126)
	}
}

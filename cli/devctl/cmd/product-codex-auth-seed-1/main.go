package main

import (
	"fmt"
	"os"

	"devkit/cli/devctl/internal/productadapter"
	"devkit/cli/devctl/internal/productauthseedcmd"
)

func main() {
	if err := productauthseedcmd.Run(
		productadapter.RoleCodexAuthSeed1,
		1,
		os.Args[1:],
		os.Stdin,
		os.Stdout,
	); err != nil {
		if !productauthseedcmd.WriteTypedFailure(os.Stderr, err) {
			_, _ = fmt.Fprintln(os.Stderr, "product-codex-auth-seed-1:", err)
		}
		os.Exit(2)
	}
}

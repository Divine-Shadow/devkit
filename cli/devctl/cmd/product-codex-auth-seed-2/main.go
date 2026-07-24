package main

import (
	"fmt"
	"os"

	"devkit/cli/devctl/internal/productadapter"
	"devkit/cli/devctl/internal/productauthseedcmd"
)

func main() {
	if err := productauthseedcmd.Run(
		productadapter.RoleCodexAuthSeed2,
		2,
		os.Args[1:],
		os.Stdin,
		os.Stdout,
	); err != nil {
		if !productauthseedcmd.WriteTypedFailure(os.Stderr, 2, err) {
			_, _ = fmt.Fprintln(os.Stderr, "product-codex-auth-seed-2: typed failure output failed")
		}
		os.Exit(2)
	}
}

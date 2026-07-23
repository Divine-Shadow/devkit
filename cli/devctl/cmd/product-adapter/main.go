package main

import (
	"errors"
	"fmt"
	"os"

	"devkit/cli/devctl/internal/productadapter"
	"devkit/cli/devctl/internal/productruntime"
)

func main() {
	attestation, err := productadapter.AttestInitialNamespaces(productadapter.RoleAdapter)
	if err != nil {
		fail(err)
	}
	command, err := productadapter.Parse(os.Args[1:])
	if err != nil {
		fail(err)
	}
	if err := productruntime.Run(*command, attestation); err != nil {
		fail(err)
	}
}

func fail(err error) {
	var exitCoder interface{ ExitCode() int }
	if errors.As(err, &exitCoder) && exitCoder.ExitCode() >= 0 {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(exitCoder.ExitCode())
	}
	_, _ = fmt.Fprintln(os.Stderr, "product-adapter:", err)
	os.Exit(2)
}

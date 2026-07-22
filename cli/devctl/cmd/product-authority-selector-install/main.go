package main

import (
	"flag"
	"fmt"
	"os"

	"devkit/cli/devctl/internal/productadapter"
)

func main() {
	flags := flag.NewFlagSet("product-authority-selector-install", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	manifest := flags.String("manifest", "", "exact immutable fleet runtime authority manifest")
	digest := flags.String("manifest-sha256", "", "exact SHA-256 of the immutable manifest bytes")
	if err := flags.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if flags.NArg() != 0 || *manifest == "" || *digest == "" {
		fmt.Fprintln(os.Stderr, "usage: product-authority-selector-install --manifest STORE_PATH --manifest-sha256 SHA256")
		os.Exit(2)
	}
	if err := productadapter.InstallAuthoritySelector(*manifest, *digest); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"devkit/cli/devctl/internal/productadapter"
	"devkit/cli/devctl/internal/productseed"
)

func main() {
	attestation, err := productadapter.AttestInitialNamespaces(productadapter.RoleSSHSetup)
	if err == nil {
		err = run(os.Args[1:], attestation)
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "product-ssh-setup:", err)
		os.Exit(2)
	}
}

func run(args []string, attestation productadapter.Attestation) error {
	if len(args) != 7 || args[0] != "seed-git" || args[1] != "--count" ||
		args[3] != "--index" || args[5] != "--root-projection" {
		return fmt.Errorf("requires exactly: product-ssh-setup seed-git --count N --index N --root-projection NAME")
	}
	count, index, err := productadapter.ParseCanonicalIdentity(args[2], args[4])
	if err != nil {
		return err
	}
	authority, err := productadapter.Load(productadapter.RoleSSHSetup, index, attestation)
	if err != nil {
		return err
	}
	defer authority.Close()
	consumer, err := authority.Consumer(index)
	if err != nil {
		return err
	}
	result, err := productseed.Seed(authority, consumer, count, index, args[6])
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

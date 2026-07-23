package productauthseedcmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"devkit/cli/devctl/internal/productadapter"
	"devkit/cli/devctl/internal/productseed"
)

// Run executes one package-compiled fixed-slot Codex authorization seed. The
// slot is a source constant in the entrypoint; callers can supply no path,
// identity, ownership, mode, manifest, or slot arguments.
func Run(
	role productadapter.Role,
	fixedIndex int,
	args []string,
	input io.Reader,
	output io.Writer,
) error {
	expectedIndex, ok := productadapter.FixedConsumerIndex(role)
	if !ok || expectedIndex != fixedIndex {
		return fmt.Errorf("Codex auth seed entrypoint has no valid fixed-slot identity")
	}
	if len(args) != 0 {
		return fmt.Errorf("fixed-slot Codex auth seed accepts no arguments")
	}
	attestation, err := productadapter.AttestInitialNamespaces(role)
	if err != nil {
		return err
	}
	authority, err := productadapter.Load(role, fixedIndex, attestation)
	if err != nil {
		return err
	}
	defer authority.Close()
	consumer, err := authority.Consumer(fixedIndex)
	if err != nil {
		return err
	}
	result, err := productseed.SeedCodexAuth(authority, consumer, fixedIndex, input)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}

// WriteTypedFailure preserves post-effect certainty across the executable
// boundary without exposing authorization bytes. It returns false for errors
// that occurred before the fixed-slot install path began.
func WriteTypedFailure(output io.Writer, err error) bool {
	var failure *productseed.CodexAuthInstallFailure
	if !errors.As(err, &failure) {
		return false
	}
	return json.NewEncoder(output).Encode(failure) == nil
}

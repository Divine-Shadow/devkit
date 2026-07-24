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
		return commandFailure(
			fixedIndex,
			"entrypoint_identity_invalid",
			"entrypoint-identity",
			fmt.Errorf("Codex auth seed entrypoint has no valid fixed-slot identity"),
		)
	}
	if len(args) != 0 {
		return commandFailure(
			fixedIndex,
			"arguments_forbidden",
			"arguments",
			fmt.Errorf("fixed-slot Codex auth seed accepts no arguments"),
		)
	}
	attestation, err := productadapter.AttestInitialNamespaces(role)
	if err != nil {
		return commandFailure(fixedIndex, "namespace_attestation_failed", "namespace-attestation", err)
	}
	authority, err := productadapter.Load(role, fixedIndex, attestation)
	if err != nil {
		code := productadapter.AuthorityFailureCode(err)
		if code == "" {
			code = "authority_load_failed"
		}
		return commandFailure(fixedIndex, code, "authority-load", err)
	}
	defer authority.Close()
	consumer, err := authority.Consumer(fixedIndex)
	if err != nil {
		return commandFailure(fixedIndex, "consumer_resolution_failed", "consumer-resolution", err)
	}
	result, err := productseed.SeedCodexAuth(authority, consumer, fixedIndex, input)
	if err != nil {
		var installFailure *productseed.CodexAuthInstallFailure
		if errors.As(err, &installFailure) {
			return err
		}
		return commandFailure(fixedIndex, "seed_precondition_failed", "seed-precondition", err)
	}
	return json.NewEncoder(output).Encode(result)
}

type FailureResult struct {
	SchemaVersion  string `json:"schema_version"`
	Status         string `json:"status"`
	ConsumerIndex  int    `json:"consumer_index"`
	Code           string `json:"code"`
	Outcome        string `json:"outcome"`
	Phase          string `json:"phase"`
	Reconciliation string `json:"reconciliation"`
	Message        string `json:"message,omitempty"`
}

type preInstallFailure struct {
	result FailureResult
	cause  error
}

func (failure *preInstallFailure) Error() string { return failure.cause.Error() }
func (failure *preInstallFailure) Unwrap() error { return failure.cause }

func commandFailure(index int, code, phase string, cause error) error {
	return &preInstallFailure{
		result: FailureResult{
			SchemaVersion:  productseed.CodexAuthSeedFailureSchema,
			Status:         "failed",
			ConsumerIndex:  index,
			Code:           code,
			Outcome:        "not-attempted",
			Phase:          phase,
			Reconciliation: "target-not-created",
		},
		cause: cause,
	}
}

// WriteTypedFailure emits one stable, redaction-safe result for every failed
// executable invocation. Unknown failures are conservatively ambiguous; raw
// errors never cross the process boundary.
func WriteTypedFailure(output io.Writer, fixedIndex int, err error) bool {
	result := FailureResult{
		SchemaVersion:  productseed.CodexAuthSeedFailureSchema,
		Status:         "failed",
		ConsumerIndex:  fixedIndex,
		Code:           "internal_failure",
		Outcome:        string(productseed.CodexAuthInstallAmbiguous),
		Phase:          "internal",
		Reconciliation: "consumer-disposal-required",
	}
	var preInstall *preInstallFailure
	var install *productseed.CodexAuthInstallFailure
	switch {
	case errors.As(err, &preInstall):
		result = preInstall.result
	case errors.As(err, &install):
		result.Code = "install_" + string(install.Outcome)
		result.ConsumerIndex = install.ConsumerIndex
		result.Outcome = string(install.Outcome)
		result.Phase = install.Phase
		result.Reconciliation = install.Reconciliation
		result.Message = install.Message
	}
	return json.NewEncoder(output).Encode(result) == nil
}

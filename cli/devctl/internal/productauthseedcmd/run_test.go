package productauthseedcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/productadapter"
	"devkit/cli/devctl/internal/productseed"
)

func TestRunRejectsCallerAuthorityBeforePackageLoad(t *testing.T) {
	for _, test := range []struct {
		name       string
		role       productadapter.Role
		fixedIndex int
		args       []string
	}{
		{
			name:       "wrong-fixed-index",
			role:       productadapter.RoleCodexAuthSeed1,
			fixedIndex: 2,
		},
		{
			name:       "caller-argument",
			role:       productadapter.RoleCodexAuthSeed1,
			fixedIndex: 1,
			args:       []string{"--index", "1"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := Run(
				test.role,
				test.fixedIndex,
				test.args,
				bytes.NewReader([]byte("{}\n")),
				&bytes.Buffer{},
			)
			if err == nil {
				t.Fatal("caller authority was accepted")
			}
			var failure *preInstallFailure
			if !errors.As(err, &failure) || failure.result.Outcome != "not-attempted" {
				t.Fatalf("caller refusal was not typed: %v", err)
			}
		})
	}
}

func TestWriteTypedFailurePreservesEffectClassification(t *testing.T) {
	failure := &productseed.CodexAuthInstallFailure{
		SchemaVersion:  productseed.CodexAuthSeedFailureSchema,
		Status:         "failed",
		ConsumerIndex:  2,
		Outcome:        productseed.CodexAuthInstallEffect,
		Phase:          "sync-parent",
		Reconciliation: "exact-target-installed",
		Message:        "injected",
	}
	var output bytes.Buffer
	if !WriteTypedFailure(&output, 2, fmt.Errorf("outer: %w", failure)) {
		t.Fatal("typed install failure was not encoded")
	}
	var decoded FailureResult
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != productseed.CodexAuthSeedFailureSchema ||
		decoded.Status != "failed" ||
		decoded.ConsumerIndex != 2 ||
		decoded.Code != "install_effect" ||
		decoded.Outcome != string(productseed.CodexAuthInstallEffect) ||
		decoded.Phase != "sync-parent" ||
		decoded.Reconciliation != "exact-target-installed" ||
		decoded.Message != "injected" {
		t.Fatalf("typed process failure changed: %+v", decoded)
	}
	output.Reset()
	if !WriteTypedFailure(&output, 1, fmt.Errorf("ordinary failure")) {
		t.Fatal("ordinary error was not encoded")
	}
	decoded = FailureResult{}
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Code != "internal_failure" ||
		decoded.Outcome != string(productseed.CodexAuthInstallAmbiguous) ||
		decoded.Message != "" {
		t.Fatalf("unexpected internal failure result: %+v", decoded)
	}
}

func TestWriteTypedFailureUsesStablePreInstallCodeWithoutCauseText(t *testing.T) {
	err := commandFailure(1, "authority_load_failed", "authority-load", fmt.Errorf("secret-shaped cause"))
	var output bytes.Buffer
	if !WriteTypedFailure(&output, 1, err) {
		t.Fatal("pre-install failure was not encoded")
	}
	var decoded FailureResult
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Code != "authority_load_failed" || decoded.Outcome != "not-attempted" ||
		decoded.Phase != "authority-load" || decoded.Reconciliation != "target-not-created" ||
		decoded.Message != "" || strings.Contains(output.String(), "secret-shaped") {
		t.Fatalf("pre-install result is unstable or leaked cause text: %s", output.String())
	}
}

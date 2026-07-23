package productauthseedcmd

import (
	"bytes"
	"encoding/json"
	"fmt"
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
			if err := Run(
				test.role,
				test.fixedIndex,
				test.args,
				bytes.NewReader([]byte("{}\n")),
				&bytes.Buffer{},
			); err == nil {
				t.Fatal("caller authority was accepted")
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
	if !WriteTypedFailure(&output, fmt.Errorf("outer: %w", failure)) {
		t.Fatal("typed install failure was not encoded")
	}
	var decoded productseed.CodexAuthInstallFailure
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != productseed.CodexAuthSeedFailureSchema ||
		decoded.Status != "failed" ||
		decoded.ConsumerIndex != 2 ||
		decoded.Outcome != productseed.CodexAuthInstallEffect ||
		decoded.Phase != "sync-parent" ||
		decoded.Reconciliation != "exact-target-installed" ||
		decoded.Message != "injected" {
		t.Fatalf("typed process failure changed: %+v", decoded)
	}
	if WriteTypedFailure(&bytes.Buffer{}, fmt.Errorf("ordinary failure")) {
		t.Fatal("ordinary error was promoted into a typed install effect")
	}
}

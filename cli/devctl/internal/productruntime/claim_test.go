package productruntime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devkit/cli/devctl/internal/productadapter"
)

func TestClaimCandidateDoesNotMaterializeMissingOfflineBoundary(t *testing.T) {
	parent := t.TempDir()
	candidate := filepath.Join(parent, "slot")
	claim, _, err := claimCandidate(
		productadapter.Authority{},
		productadapter.ConsumerManifest{CandidateRoot: candidate},
	)
	if claim != nil {
		claim.Close()
		t.Fatal("missing candidate produced a claim")
	}
	if err == nil {
		t.Fatal("missing offline candidate was accepted")
	}
	if _, statErr := os.Lstat(candidate); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed claim left candidate residue: %v", statErr)
	}
}

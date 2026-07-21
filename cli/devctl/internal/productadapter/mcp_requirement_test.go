package productadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPRequirementValidatesExactSortedInventory(t *testing.T) {
	requirement := MCPRequirement{
		SchemaVersion: MCPRequirementSchema,
		Servers: []MCPServerRequirement{{
			Name: "governance", Tools: []string{"get_run_status", "run"},
		}},
	}
	if err := requirement.Validate(); err != nil {
		t.Fatal(err)
	}
	digest, err := requirement.SemanticDigest()
	if err != nil || len(digest) != 64 {
		t.Fatalf("semantic digest = %q, %v", digest, err)
	}
	servers, tools := requirement.Counts()
	if servers != 1 || tools != 2 {
		t.Fatalf("counts = %d/%d", servers, tools)
	}
}

func TestMCPRequirementRejectsEmptyDuplicateUnsortedAndTruncated(t *testing.T) {
	for _, requirement := range []MCPRequirement{
		{SchemaVersion: MCPRequirementSchema},
		{SchemaVersion: MCPRequirementSchema, Servers: []MCPServerRequirement{{Name: "governance"}}},
		{SchemaVersion: MCPRequirementSchema, Servers: []MCPServerRequirement{
			{Name: "z", Tools: []string{"run"}}, {Name: "a", Tools: []string{"run"}},
		}},
		{SchemaVersion: MCPRequirementSchema, Servers: []MCPServerRequirement{{
			Name: "governance", Tools: []string{"run", "run"},
		}}},
	} {
		if err := requirement.Validate(); err == nil {
			t.Fatalf("invalid requirement accepted: %#v", requirement)
		}
	}

	path := filepath.Join(t.TempDir(), "requirement.json")
	payload := []byte(`{"schemaVersion":"devkit/product-mcp-requirement/v1","servers":[`)
	if err := os.WriteFile(path, payload, 0o444); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	if _, _, err := LoadMCPRequirement(path, hex.EncodeToString(sum[:])); err == nil ||
		!strings.Contains(err.Error(), "decode Product MCP requirement") {
		t.Fatalf("truncated requirement error = %v", err)
	}
}

func TestObservedMCPRequirementNormalizesButDoesNotInventNames(t *testing.T) {
	observed, err := NewObservedMCPRequirement(map[string][]string{
		"governance": {"run", "get_run_status"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if observed.Servers[0].Tools[0] != "get_run_status" || observed.Servers[0].Tools[1] != "run" {
		t.Fatalf("observed inventory = %#v", observed)
	}
	if _, err := NewObservedMCPRequirement(map[string][]string{"governance": {"run", "run"}}); err == nil {
		t.Fatal("duplicate observed tool was accepted")
	}
}

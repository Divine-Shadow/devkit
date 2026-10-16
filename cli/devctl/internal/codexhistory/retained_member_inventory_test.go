package codexhistory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func retainedFixture(t *testing.T) (captureFixture, Result, string) {
	t.Helper()
	f := newCaptureFixture(t)
	member := "sessions/2026/09/14/rollout-original.jsonl"
	createStateDatabase(t, f, filepath.Join(f.options.SandboxHome, ".codex", filepath.FromSlash(member)))
	writeTestFile(t, filepath.Join(f.codexRoot, filepath.FromSlash(member)),
		"{\"type\":\"session_meta\",\"payload\":{\"id\":\"gui-thread\"}}\n"+
			"{\"type\":\"response_item\",\"payload\":{\"text\":\"excluded-private-session-content\"}}\n")
	captured, err := Capture(f.options)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.codexRoot, filepath.FromSlash(member))); err != nil {
		t.Fatal(err)
	}
	return f, captured, member
}

func TestMissingRolloutRefusalInventoriesExactRetainedMemberWithoutRestoring(t *testing.T) {
	f, captured, member := retainedFixture(t)
	manifestBefore, err := os.ReadFile(captured.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Capture(f.options)
	observation := requireMissingRolloutObservation(t, err)
	got := observation.RetainedCustody
	if got == nil || got.Status != "scan-complete" || got.GenerationsChecked != 1 || got.Truncated || len(got.Matches) != 1 {
		t.Fatalf("missing exact bounded custody evidence: %+v", got)
	}
	match := got.Matches[0]
	if match.Generation != captured.GenerationID || match.ManifestSHA256 != projectionDigest(manifestBefore) ||
		match.Member != member || match.Bytes == 0 || !projectionValidHash(match.SHA256) ||
		match.Validation != "member-hash-and-session-identity-verified" ||
		match.BundleVerification != "not-performed-inventory-only" || match.ResumeEligible {
		t.Fatalf("member inventory claimed wrong evidence/acceptance: %+v", match)
	}
	after, err := os.ReadFile(captured.ManifestPath)
	if err != nil || string(after) != string(manifestBefore) {
		t.Fatal("retained manifest changed")
	}
	if _, err := os.Stat(filepath.Join(f.codexRoot, filepath.FromSlash(member))); !os.IsNotExist(err) {
		t.Fatal("observation restored missing history")
	}
	root, _ := WorkspaceCustodyRoot(f.options.WorkspaceRoot, f.options.Project)
	entries, err := os.ReadDir(filepath.Join(root, "agent1"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("refusal published another generation: %v %v", entries, err)
	}
}

func TestRetainedMemberInventoryRejectsForeignTamperedAndWrongIdentity(t *testing.T) {
	for _, kind := range []string{"foreign-home", "tampered-member", "wrong-identity", "generation-symlink", "member-symlink"} {
		t.Run(kind, func(t *testing.T) {
			f, captured, member := retainedFixture(t)
			generation := filepath.Dir(captured.ManifestPath)
			bodyPath := filepath.Join(generation, "payload", filepath.FromSlash(member))
			var manifest snapshotManifest
			raw, _ := os.ReadFile(captured.ManifestPath)
			if err := json.Unmarshal(raw, &manifest); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "foreign-home":
				manifest.SourceHostHome = filepath.Join(t.TempDir(), "foreign-home")
			case "tampered-member":
				writeTestFile(t, bodyPath, "excluded-private-session-content")
			case "wrong-identity":
				body := "{\"type\":\"session_meta\",\"payload\":{\"id\":\"foreign-thread\"}}\n"
				writeTestFile(t, bodyPath, body)
				for i := range manifest.Files {
					if manifest.Files[i].Path == member {
						manifest.Files[i] = projectionRecord(member, []byte(body))
					}
				}
				manifest.BundleSHA256 = aggregateHash(manifest.Files)
			case "generation-symlink":
				other := filepath.Join(t.TempDir(), "held-generation")
				if err := os.Rename(generation, other); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, generation); err != nil {
					t.Fatal(err)
				}
			case "member-symlink":
				other := filepath.Join(t.TempDir(), "private.jsonl")
				if err := os.Rename(bodyPath, other); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, bodyPath); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "foreign-home" || kind == "wrong-identity" {
				updated, _ := json.Marshal(manifest)
				if err := os.WriteFile(captured.ManifestPath, updated, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got := inventoryRetainedMember(f.options, "gui-thread", member)
			if len(got.Matches) != 0 || got.Unclassified != 1 || got.Status != "partial-scan" {
				t.Fatalf("unsafe retained source admitted: %+v", got)
			}
			encoded, _ := json.Marshal(got)
			if strings.Contains(string(encoded), "excluded-private-session-content") || strings.Contains(string(encoded), "foreign-thread") {
				t.Fatal("private body/foreign identity exported")
			}
		})
	}
}

func TestRetainedInventoryCoverageNeverMeansBackupAbsence(t *testing.T) {
	f := newCaptureFixture(t)
	member := "sessions/missing.jsonl"
	got := inventoryRetainedMember(f.options, "gui-thread", member)
	if got.Status != "declared-lane-custody-absent" || got.Scope != "selected-lane-canonical-custody-only" {
		t.Fatalf("missing lane became global absence: %+v", got)
	}
	root, _ := WorkspaceCustodyRoot(f.options.WorkspaceRoot, f.options.Project)
	lane := filepath.Join(root, "agent1")
	for i := 0; i < retainedInventoryEntryLimit+1; i++ {
		if err := os.MkdirAll(filepath.Join(lane, strings.Repeat("x", i+1)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	got = inventoryRetainedMember(f.options, "gui-thread", member)
	if !got.Truncated || got.Status != "partial-scan" || len(got.Matches) != 0 || got.EntriesObserved != retainedInventoryEntryLimit+1 {
		t.Fatalf("bounded scan claimed exhaustive absence: %+v", got)
	}
	if got := inventoryRetainedMember(f.options, "gui-thread", "sessions/../auth.jsonl"); got.Status != "unavailable" || got.EntriesObserved != 0 {
		t.Fatal("noncanonical history member caused custody traversal")
	}
}

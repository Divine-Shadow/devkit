package codexhistory

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflightUsesPrivateSQLiteAndDoesNotRestoreAnUnavailableReference(t *testing.T) {
	f, _, member := retainedFixture(t)
	database := filepath.Join(f.codexRoot, "state_5.sqlite")
	before, err := os.ReadFile(database)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Preflight(f.options)
	if err != nil || result.ColdCapture || result.Status != "metadata-checked" || result.GUIReferences != 1 {
		t.Fatalf("preflight broadened its proof: %+v %v", result, err)
	}
	after, err := os.ReadFile(database)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("preflight changed original SQLite")
	}
	if _, err := os.Stat(filepath.Join(f.codexRoot, filepath.FromSlash(member))); !os.IsNotExist(err) {
		t.Fatal("preflight restored or fabricated history")
	}
}

func TestPreflightAdjudicatedAvailableEvidenceNeverClaimsColdCapture(t *testing.T) {
	for _, archived := range []string{"0", "1", "NULL"} {
		t.Run(archived, func(t *testing.T) {
			f := newCaptureFixture(t)
			database := createStateDatabase(t, f, filepath.Join(f.options.SandboxHome, ".codex/sessions/missing.jsonl"))
			runSQLite(t, f.sqlite, database, "ALTER TABLE threads ADD COLUMN archived; UPDATE threads SET archived = "+archived+";")
			result, err := Preflight(f.options)
			if err != nil || result.ColdCapture || result.Status != "metadata-checked" || result.GUIReferences != 1 {
				t.Fatalf("available-evidence preflight scope changed: %+v %v", result, err)
			}
			root, _ := WorkspaceCustodyRoot(f.options.WorkspaceRoot, f.options.Project)
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("preflight created custody generation/root")
			}
		})
	}
}

func persistentPreflightFixture(t *testing.T) captureFixture {
	t.Helper()
	f := newCaptureFixture(t)
	f.options.HostHome = filepath.Join(f.options.StateRoot, ".homes", "dev-all-agent1", "ouroboros-ide")
	f.options.SandboxHome = "/workspaces/dev/ouroboros-ide/.devhome-agent1"
	f.options.WorkspaceProjectionRoot = filepath.Dir(f.options.HostWorktree)
	f.codexRoot = filepath.Join(f.options.HostHome, ".codex")
	if err := os.MkdirAll(f.codexRoot, 0700); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPersistentHomePreflightUsesOnlyExplicitSourceAliases(t *testing.T) {
	for _, referenceClass := range []string{"host", "sandbox", "prefix-sandbox"} {
		t.Run(referenceClass, func(t *testing.T) {
			f := persistentPreflightFixture(t)
			root := f.options.SandboxHome
			if referenceClass == "host" {
				root = f.options.HostHome
			}
			if referenceClass == "prefix-sandbox" {
				f.options.SandboxHome = "/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1"
				root = f.options.SandboxHome
			}
			rollout := filepath.Join(root, ".codex", "sessions", "retained.jsonl")
			db := createStateDatabase(t, f, rollout)
			writeTestFile(t, filepath.Join(f.codexRoot, "sessions", "retained.jsonl"), "retained payload\n")
			before, _ := os.ReadFile(db)
			info, _ := os.Stat(db)
			result, err := Preflight(f.options)
			if err != nil || result.GUIReferences != 1 || result.ColdCapture {
				t.Fatalf("explicit alias preflight: %+v %v", result, err)
			}
			after, _ := os.ReadFile(db)
			afterInfo, _ := os.Stat(db)
			if !bytes.Equal(before, after) || !os.SameFile(info, afterInfo) {
				t.Fatal("preflight changed original database")
			}
			alias, err := workspaceProjectedCodexRoot(f.options)
			if err != nil || alias != "" {
				t.Fatalf("invented workspace-relative persistent alias: %q %v", alias, err)
			}
			for name, change := range map[string]func(*SnapshotOptions){
				"state-root":                func(o *SnapshotOptions) { o.StateRoot += "/foreign" },
				"foreign-same-index-prefix": func(o *SnapshotOptions) { o.SandboxHome = "/foreign/ouroboros-ide/.devhome-agent1" },
				"foreign-root-alias":        func(o *SnapshotOptions) { o.SandboxHome = "/workspaces/dev/foreign/ouroboros-ide/.devhome-agent1" },
				"sibling-home": func(o *SnapshotOptions) {
					o.HostHome = filepath.Join(o.StateRoot, ".homes", "dev-all-agent2", "ouroboros-ide")
				},
				"sibling-sandbox": func(o *SnapshotOptions) { o.SandboxHome = "/workspaces/dev/.devhome-agent2" },
			} {
				t.Run(name, func(t *testing.T) {
					o := f.options
					change(&o)
					if _, err := Preflight(o); err == nil {
						t.Fatal("unbound external-home pair admitted")
					}
				})
			}
		})
	}
}

func TestPersistentHomePreflightRefusesExternalPayloadAndSymlink(t *testing.T) {
	for _, kind := range []string{"external-reference", "symlink-history"} {
		t.Run(kind, func(t *testing.T) {
			f := persistentPreflightFixture(t)
			external := filepath.Join(f.options.HostWorktree, "external-history", "rollout.jsonl")
			writeTestFile(t, external, "external protected payload\n")
			reference := external
			if kind == "symlink-history" {
				reference = filepath.Join(f.options.SandboxHome, ".codex", "sessions", "rollout.jsonl")
				if err := os.Symlink(filepath.Dir(external), filepath.Join(f.codexRoot, "sessions")); err != nil {
					t.Fatal(err)
				}
			}
			db := createStateDatabase(t, f, reference)
			before, _ := os.ReadFile(db)
			payloadInfo, _ := os.Stat(external)
			_, err := Preflight(f.options)
			if err == nil || (!strings.Contains(err.Error(), "escapes") && !strings.Contains(err.Error(), "not a real directory")) {
				t.Fatalf("external protected history was not refused: %v", err)
			}
			after, _ := os.ReadFile(db)
			afterInfo, _ := os.Stat(external)
			payload, _ := os.ReadFile(external)
			if !bytes.Equal(before, after) || !os.SameFile(payloadInfo, afterInfo) || string(payload) != "external protected payload\n" {
				t.Fatal("refusal changed protected database/payload")
			}
		})
	}
}

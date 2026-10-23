package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
	"devkit/cli/devctl/internal/runtime/agent"
	"devkit/cli/devctl/internal/worktrees"
)

func decodeGitBindDiagnostic(t *testing.T, err error) gitCommonBindDiagnostic {
	t.Helper()
	if err == nil {
		t.Fatal("missing refusal")
	}
	_, body, ok := strings.Cut(err.Error(), ": ")
	if !ok {
		t.Fatalf("missing diagnostic JSON: %v", err)
	}
	var d gitCommonBindDiagnostic
	if e := json.Unmarshal([]byte(body), &d); e != nil {
		t.Fatalf("invalid diagnostic: %v: %s", e, body)
	}
	return d
}

func TestGitCommonBindDiagnosticCountsAndConstructionTrace(t *testing.T) {
	for _, name := range []string{"missing", "duplicate", "conflict", "unavailable", "stale"} {
		t.Run(name, func(t *testing.T) {
			p, wt, sibling := realProductGitPlan(t)
			metadata, err := worktrees.InspectNativeLaneGitMetadata(wt, p.HostWorktreeRoot, "ouroboros-ide", 1)
			if err != nil {
				t.Fatal(err)
			}
			selectedBefore, siblingBefore := snapshotGitFiles(t, metadata.CommonDir), snapshotGitFiles(t, sibling)
			expected := Bind{Source: metadata.CommonDir, Target: "/workspaces/.devkit/git/agent1/ouroboros-ide.git", Mode: "rw", Required: true}
			if p.gitMetadataTrace == nil || p.gitMetadataTrace.Discovery != "relative-forward" {
				t.Fatal("missing real construction trace")
			}
			switch name {
			case "duplicate":
				p.Binds = append(p.Binds, expected)
			case "conflict":
				b := expected
				b.Mode = "ro"
				p.Binds = append(p.Binds, b)
			default:
				filtered := make([]Bind, 0, len(p.Binds))
				for _, b := range p.Binds {
					if b != expected {
						filtered = append(filtered, b)
					}
				}
				p.Binds = filtered
				if name == "unavailable" {
					p.gitMetadataTrace = nil
				}
				if name == "stale" {
					p.gitMetadataTrace.Expected.Source += "-other"
				}
			}
			d := decodeGitBindDiagnostic(t, ValidateGitBacklinkProjection(p))
			if d.Expected != diagnosticGitBind(expected) {
				t.Fatalf("wrong expected: %+v", d)
			}
			if name == "duplicate" {
				if d.ExactMatchCount != 2 || d.SameTargetCount != 2 {
					t.Fatalf("duplicate not observed: %+v", d)
				}
			} else if name == "conflict" {
				if d.ExactMatchCount != 1 || d.SameTargetCount != 2 {
					t.Fatalf("conflict not observed: %+v", d)
				}
			} else if d.ExactMatchCount != 0 || d.SameTargetCount != 0 {
				t.Fatalf("missing not observed: %+v", d)
			}
			status := "construction-time"
			if name == "unavailable" {
				status = "unavailable"
			}
			if name == "stale" {
				status = "stale-expectation"
			}
			if d.TraceStatus != status {
				t.Fatalf("wrong trace status: %+v", d)
			}
			if status == "construction-time" {
				if d.CandidateCount != 1 || d.Candidates[0].Disposition != "package-owned-retained" || d.CommonRead != "parsed" {
					t.Fatalf("wrong actual filter: %+v", d)
				}
			} else if d.CandidateCount != 0 || d.Discovery != "" {
				t.Fatal("unavailable trace manufactured history")
			}
			serialized, e := json.Marshal(p)
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(string(serialized), "construction-time") || strings.Contains(string(serialized), "package-owned-retained") {
				t.Fatal("private trace serialized")
			}
			if !reflect.DeepEqual(selectedBefore, snapshotGitFiles(t, metadata.CommonDir)) || !reflect.DeepEqual(siblingBefore, snapshotGitFiles(t, sibling)) {
				t.Fatal("diagnosis mutated Git custody")
			}
			t.Logf("case=%s exact=%d same_target=%d trace=%s refusal=true", name, d.ExactMatchCount, d.SameTargetCount, d.TraceStatus)
		})
	}
}

func TestGitCommonBindDiagnosticBoundsDoNotCapCounts(t *testing.T) {
	expected := Bind{Source: strings.Repeat("s", 4096), Target: strings.Repeat("t", 4096), Mode: "rw", Required: true}
	p := Plan{}
	for i := 0; i < 25; i++ {
		p.Binds = append(p.Binds, expected)
	}
	err := gitCommonBindError(p, expected, false)
	d := decodeGitBindDiagnostic(t, err)
	if d.ExactMatchCount != 25 || d.SameTargetCount != 25 || d.RelevantBindCount != 25 || len(d.Retained) != 4 || !d.RetainedTruncated || !d.Expected.Truncated {
		t.Fatalf("counts capped or truncation hidden: %+v", d)
	}
	if len(err.Error()) > 8000 {
		t.Fatalf("unbounded error: %d", len(err.Error()))
	}
}

func TestGitMetadataDiagnosticDiscriminatesActualPointerFilter(t *testing.T) {
	p, wt, sibling := realProductGitPlan(t)
	before := snapshotGitFiles(t, sibling)
	metadata, err := worktrees.InspectNativeLaneGitMetadata(wt, p.HostWorktreeRoot, "ouroboros-ide", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+metadata.GitDir+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	trace := &gitMetadataBindTrace{}
	binds := gitMetadataBindsObserved(wt, p.Agent.SandboxWorktree, trace)
	if trace.Discovery != "absolute-forward" || len(binds) != 2 || binds[1].Target != metadata.CommonDir {
		t.Fatalf("absolute branch not observed: %+v %+v", trace, binds)
	}
	// Real ordinary Build refuses the absolute common host alias at the EARLIER
	// outside-lane filter, not the later exact-common-count predicate.
	_, err = Build(BuildOptions{Paths: devkitpaths.Paths{Root: p.DevkitHostRoot}, Project: "dev-all", Repo: "ouroboros-ide", Index: 1, WorktreeRoot: p.HostWorktreeRoot, WorkspaceRoot: filepath.Dir(wt), IsolationProfile: IsolationProfileWorkspaceEgress, EgressAllowlist: p.Proxy.AllowlistPath})
	if err == nil || !strings.Contains(err.Error(), "rejects Git metadata outside the lane root") {
		t.Fatalf("wrong absolute branch: %v", err)
	}
	if !reflect.DeepEqual(before, snapshotGitFiles(t, sibling)) {
		t.Fatal("sibling changed")
	}
	t.Logf("absolute_common_alias=true actual_earlier_filter_refusal=true not_incident_cause_proof=true")
}

func TestGitMetadataDiagnosticUnknownReadsAndMalformedPointers(t *testing.T) {
	for _, name := range []string{"missing", "directory", "malformed", "commondir-unavailable", "commondir-empty"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			wt := filepath.Join(root, "worktree")
			if err := os.Mkdir(wt, 0700); err != nil {
				t.Fatal(err)
			}
			expected := "stat-unavailable"
			switch name {
			case "directory":
				if err := os.Mkdir(filepath.Join(wt, ".git"), 0700); err != nil {
					t.Fatal(err)
				}
				expected = "standalone-directory"
			case "malformed":
				if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("malformed\n"), 0600); err != nil {
					t.Fatal(err)
				}
				expected = "gitdir-unparsed"
			case "commondir-unavailable", "commondir-empty":
				if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: ../metadata\n"), 0600); err != nil {
					t.Fatal(err)
				}
				expected = "relative-forward"
				if name == "commondir-empty" {
					if err := os.Mkdir(filepath.Join(root, "metadata"), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(root, "metadata", "commondir"), nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			trace := &gitMetadataBindTrace{}
			gitMetadataBindsObserved(wt, "/workspaces/dev/ouroboros-ide", trace)
			if trace.Discovery != expected {
				t.Fatalf("unknown classified as known: %+v", trace)
			}
			if name == "commondir-unavailable" && trace.CommonRead != "unavailable" {
				t.Fatalf("unavailable common read hidden: %+v", trace)
			}
			if name == "commondir-empty" && trace.CommonRead != "empty-unparsed" {
				t.Fatalf("empty common read hidden: %+v", trace)
			}
		})
	}
}

func TestGitMetadataDiagnosticRecordsInLaneCoveredCandidate(t *testing.T) {
	root := t.TempDir()
	wt := filepath.Join(root, "lane", "repo")
	common := filepath.Join(root, "lane", "metadata")
	if err := os.MkdirAll(wt, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(common, "worktrees", "selected"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: ../metadata/worktrees/selected\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(common, "worktrees", "selected", "commondir"), []byte("../..\n"), 0600); err != nil {
		t.Fatal(err)
	}
	paths := agent.Paths{DevRoot: root, HostWorktreeRoot: root, HostWorktree: wt, HostHome: filepath.Join(root, "home"), SandboxHome: "/home/fixture", SandboxWorktree: "/workspaces/dev/repo"}
	trace := &gitMetadataBindTrace{}
	binds, err := workspaceEgressBindsObserved(paths, "dev-workspace", 1, "repo", filepath.Join(root, "lane"), filepath.Join(root, "devkit"), "", "", "/fixture/broker", "/fixture/resolv", nil, trace)
	if err != nil {
		t.Fatal(err)
	}
	if len(trace.Candidates) != 1 || trace.Candidates[0].Disposition != "in-lane-covered-skipped" {
		t.Fatalf("wrong candidate trace: %+v", trace)
	}
	for _, bind := range binds {
		if bind.Source == common {
			t.Fatal("diagnostic changed skip selection")
		}
	}
}

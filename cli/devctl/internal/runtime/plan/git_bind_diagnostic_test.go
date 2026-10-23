package plan

import (
	"encoding/json"
	"fmt"
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

func TestGitCommonBindDiagnosticRejectsOtherLaneTraceWithSameLegacyCommon(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	gitFixtureCommand(t, "init", "--initial-branch=main", seed)
	gitFixtureCommand(t, "-C", seed, "commit", "--allow-empty", "-m", "seed")
	worktreeRoot := filepath.Join(root, "leased-roots")
	common := filepath.Join(worktreeRoot, ".devkit", "git", "ouroboros-ide.git")
	gitFixtureCommand(t, "clone", "--bare", seed, common)
	marker := "schema=devkit/native-owned-common-repository/v1\nrepository=ouroboros-ide\norigin=" + seed + "\n"
	if err := os.WriteFile(filepath.Join(common, "devkit-owned-common"), []byte(marker), 0600); err != nil {
		t.Fatal(err)
	}
	devkit := filepath.Join(root, "devkit")
	if err := os.Mkdir(devkit, 0700); err != nil {
		t.Fatal(err)
	}
	var plans []Plan
	for index := 1; index <= 2; index++ {
		wt := filepath.Join(worktreeRoot, fmt.Sprintf("agent%d", index), "ouroboros-ide")
		gitFixtureCommand(t, "--git-dir", common, "worktree", "add", "-b", fmt.Sprintf("lane%d", index), wt, "main")
		if err := worktrees.EnsurePortableNativeGitdir(wt, worktreeRoot, "ouroboros-ide", index); err != nil {
			t.Fatal(err)
		}
		p, err := Build(BuildOptions{Paths: devkitpaths.Paths{Root: devkit}, Project: "dev-all", Repo: "ouroboros-ide", Index: index, WorktreeRoot: worktreeRoot, WorkspaceRoot: filepath.Dir(wt), IsolationProfile: IsolationProfileWorkspaceEgress, EgressAllowlist: filepath.Join(devkit, "allowlist")})
		if err != nil {
			t.Fatal(err)
		}
		plans = append(plans, p)
	}
	before := snapshotGitFiles(t, worktreeRoot)
	if plans[0].gitMetadataTrace.Expected != plans[1].gitMetadataTrace.Expected {
		t.Fatal("fixture does not share exact legacy common tuple")
	}
	selected := plans[1]
	expected := selected.gitMetadataTrace.Expected
	kept := make([]Bind, 0, len(selected.Binds))
	for _, bind := range selected.Binds {
		if bind != expected {
			kept = append(kept, bind)
		}
	}
	selected.Binds = kept
	own := decodeGitBindDiagnostic(t, ValidateGitBacklinkProjection(selected))
	if own.TraceStatus != "construction-time" {
		t.Fatalf("unchanged own lane lost trace: %+v", own)
	}
	selected.gitMetadataTrace = plans[0].gitMetadataTrace
	d := decodeGitBindDiagnostic(t, ValidateGitBacklinkProjection(selected))
	if d.TraceStatus != "stale-geometry" || d.Discovery != "" || d.CandidateCount != 0 || len(d.Candidates) != 0 || d.ExactMatchCount != 0 {
		t.Fatalf("other lane trace attributed to current lane: %+v", d)
	}
	if !reflect.DeepEqual(before, snapshotGitFiles(t, worktreeRoot)) {
		t.Fatal("validation changed shared/selected/sibling Git bytes")
	}
	t.Log("same_legacy_expected_tuple=true own_trace=construction-time cross_lane_trace=stale-geometry refusal_preserved=true all_git_bytes_unchanged=true")
}

func TestGitMetadataDiagnosticTraceDoesNotChangeExecutionIdentity(t *testing.T) {
	p, _, _ := realProductGitPlan(t)
	without := p.WithoutGitMetadataDiagnosticTrace()
	if p.gitMetadataTrace == nil || without.gitMetadataTrace != nil {
		t.Fatal("value copy changed original or retained diagnostictrace")
	}
	if reflect.DeepEqual(p, without) {
		t.Fatal("fixture did not exercise private diagnostic difference")
	}
	if !reflect.DeepEqual(p.WithoutGitMetadataDiagnosticTrace(), without) {
		t.Fatal("trace participates in execution identity")
	}
	for _, name := range []string{"index", "host-worktree", "host-root", "state-root", "proxy", "gui-target", "environment", "bind"} {
		t.Run(name, func(t *testing.T) {
			changed := p
			switch name {
			case "index":
				changed.Agent.ID.Index++
			case "host-worktree":
				changed.Agent.HostWorktree += "/foreign"
			case "host-root":
				changed.HostWorktreeRoot += "/foreign"
			case "state-root":
				changed.Agent.StateRoot += "/foreign"
			case "proxy":
				changed.Proxy.UnixSocket += "-foreign"
			case "gui-target":
				changed.GUITargetConfig = &GUITargetConfigProjection{TargetID: "foreign"}
			case "environment":
				changed.Env = map[string]string{"DIFFERENT_EXECUTION": "true"}
			case "bind":
				changed.Binds = append(append([]Bind{}, p.Binds...), Bind{Source: "/foreign", Target: "/foreign", Mode: "rw", Required: true})
			}
			if reflect.DeepEqual(changed.WithoutGitMetadataDiagnosticTrace(), without) {
				t.Fatalf("execution identity guard lost %s", name)
			}
		})
	}
	if err := ValidateGitBacklinkProjection(p); err != nil {
		t.Fatal(err)
	}
	t.Log("error_trace_only_excluded=true original_plan_unchanged=true execution_identity_changes_retained=true original_guard_valid=true")
}

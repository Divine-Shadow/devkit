package plan

import (
	"encoding/json"
	"fmt"
)

// This trace describes construction, never current filesystem state. It is
// deliberately private: a serialized/hand-built Plan cannot assert this history.
type gitMetadataBindTrace struct {
	Discovery     string
	CommonRead    string
	Candidates    []gitMetadataBindCandidate
	Expected      Bind
	ExpectedBound bool
}

type gitMetadataBindCandidate struct {
	Bind        Bind
	Disposition string
}

func setGitMetadataDiscovery(trace *gitMetadataBindTrace, value string) {
	if trace != nil {
		trace.Discovery = value
	}
}

func recordGitMetadataDisposition(trace *gitMetadataBindTrace, bind Bind, value string) {
	if trace != nil {
		trace.Candidates = append(trace.Candidates, gitMetadataBindCandidate{Bind: bind, Disposition: value})
	}
}

const gitBindDiagnosticPathLimit = 256
const gitBindDiagnosticListLimit = 4

type gitDiagnosticBind struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	Mode      string `json:"mode"`
	Required  bool   `json:"required"`
	Truncated bool   `json:"truncated,omitempty"`
}

func diagnosticGitBind(bind Bind) gitDiagnosticBind {
	short := func(s string, n int) string {
		if len(s) > n {
			return s[:n]
		}
		return s
	}
	return gitDiagnosticBind{
		Source: short(bind.Source, gitBindDiagnosticPathLimit),
		Target: short(bind.Target, gitBindDiagnosticPathLimit),
		Mode:   short(bind.Mode, 16), Required: bind.Required,
		Truncated: len(bind.Source) > gitBindDiagnosticPathLimit || len(bind.Target) > gitBindDiagnosticPathLimit || len(bind.Mode) > 16,
	}
}

type gitDiagnosticCandidate struct {
	Bind        gitDiagnosticBind `json:"bind"`
	Disposition string            `json:"disposition"`
}

type gitCommonBindDiagnostic struct {
	Stage               string                   `json:"stage"`
	Expected            gitDiagnosticBind        `json:"expected"`
	SameTargetCount     int                      `json:"same_target_count"`
	ExactMatchCount     int                      `json:"exact_match_count"`
	RelevantBindCount   int                      `json:"relevant_bind_count"`
	Retained            []gitDiagnosticBind      `json:"retained"`
	RetainedTruncated   bool                     `json:"retained_truncated"`
	TraceStatus         string                   `json:"trace_status"`
	Discovery           string                   `json:"discovery,omitempty"`
	CommonRead          string                   `json:"commondir_read,omitempty"`
	CandidateCount      int                      `json:"candidate_count"`
	Candidates          []gitDiagnosticCandidate `json:"construction_candidates,omitempty"`
	CandidatesTruncated bool                     `json:"candidates_truncated"`
}

// Rendering is bounded; the refusal and its counts always inspect every bind.
// No raw .git contents, environment, or unrelated mount is included.
func gitCommonBindError(p Plan, expected Bind, conflict bool) error {
	d := gitCommonBindDiagnostic{Stage: "exact-common-bind", Expected: diagnosticGitBind(expected), TraceStatus: "unavailable", Retained: []gitDiagnosticBind{}}
	for _, bind := range p.Binds {
		if bind.Target == expected.Target {
			d.SameTargetCount++
		}
		if bind == expected {
			d.ExactMatchCount++
		}
		if bind.Target == expected.Target || bind.Source == expected.Source {
			d.RelevantBindCount++
			if len(d.Retained) < gitBindDiagnosticListLimit {
				d.Retained = append(d.Retained, diagnosticGitBind(bind))
			}
		}
	}
	d.RetainedTruncated = d.RelevantBindCount > len(d.Retained)
	if trace := p.gitMetadataTrace; trace != nil {
		d.TraceStatus = "unavailable-unbound"
		if trace.ExpectedBound {
			d.TraceStatus = "stale-expectation"
			if trace.Expected == expected {
				d.TraceStatus = "construction-time"
				d.Discovery = trace.Discovery
				d.CommonRead = trace.CommonRead
				d.CandidateCount = len(trace.Candidates)
				for _, candidate := range trace.Candidates {
					if len(d.Candidates) == gitBindDiagnosticListLimit {
						break
					}
					d.Candidates = append(d.Candidates, gitDiagnosticCandidate{Bind: diagnosticGitBind(candidate.Bind), Disposition: candidate.Disposition})
				}
				d.CandidatesTruncated = len(trace.Candidates) > len(d.Candidates)
			}
		}
	}
	// All values are strings/primitives; JSON encoding cannot fail.
	payload, _ := json.Marshal(d)
	if conflict {
		return fmt.Errorf("native Git projection has conflicting common repository bind: %s", payload)
	}
	return fmt.Errorf("native Git projection requires one exact common repository bind: %s", payload)
}

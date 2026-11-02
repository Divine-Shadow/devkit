package nativecmd

import "devkit/cli/devctl/internal/runtime/readiness"

type nativeReadinessEvidence struct {
	ReadinessMode     string            `json:"readiness_mode"`
	Status            string            `json:"status"`
	RuntimeReady      bool              `json:"runtime_ready"`
	RepoReady         bool              `json:"repo_ready"`
	CapacityAvailable bool              `json:"capacity_available"`
	Action            string            `json:"action,omitempty"`
	FailedChecks      []readiness.Check `json:"failed_checks,omitempty"`
	Checks            []readiness.Check `json:"checks"`
}

func nativeReadinessEvidenceFor(mode string, report readiness.Report) nativeReadinessEvidence {
	return nativeReadinessEvidence{ReadinessMode: mode, Status: report.Status(), RuntimeReady: report.RuntimeReady(), RepoReady: report.RepoReady(), CapacityAvailable: report.CapacityAvailable(), Action: report.Action(), FailedChecks: report.FailedChecks(), Checks: report.Checks}
}

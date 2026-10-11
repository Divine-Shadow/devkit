package broker

import (
	"context"
	"errors"
	"path/filepath"
)

// RecoveryAssessment is a read-only snapshot of existing station-broker
// recovery predicates. It never admits Stop or Start, which recheck custody and
// bindings under their own lifecycle guard.
type RecoveryAssessment struct {
	SchemaVersion  string `json:"schemaVersion"`
	Classification string `json:"classification"`
	Reason         string `json:"reason"`
	Binding        string `json:"binding"`
	SnapshotOnly   bool   `json:"snapshotOnly"`
}

func newRecoveryAssessment(classification, reason, binding string) RecoveryAssessment {
	return RecoveryAssessment{
		SchemaVersion:  "devkit-native-broker-recovery-assessment/v1",
		Classification: classification,
		Reason:         reason,
		Binding:        binding,
		SnapshotOnly:   true,
	}
}

// AssessRecovery selects the immutable station owner and reads only its fixed
// metadata under the ordinary non-creating lifecycle guard. The result carries
// no PID, path, policy, witness, or credential material.
func AssessRecovery(c Config) (RecoveryAssessment, error) {
	c, err := selectedStationOwner(c)
	if err != nil || c.Owner == nil {
		return RecoveryAssessment{}, errors.New("source-selected station broker owner is required")
	}
	g, err := lockLifecycle(context.Background(), c, false)
	if err != nil {
		return newRecoveryAssessment("unknown", "lifecycle-custody-unavailable", "unknown"), nil
	}
	if g == nil {
		return newRecoveryAssessment("unknown", "state-root-absent", "unknown"), nil
	}
	defer g.Close()
	status, err := inspectLocked(c, g)
	if err != nil {
		return newRecoveryAssessment("unknown", "inspection-failed", "unknown"), nil
	}
	intent, err := readStopIntent(c, g)
	if err != nil {
		return newRecoveryAssessment("unknown", "stop-intent-invalid", "unknown"), nil
	}
	return classifyRecovery(c, status, intent, g.files[PIDFile(c)] != nil), nil
}

func classifyRecovery(c Config, status Status, intent *stopIntent, pidFilePresent bool) RecoveryAssessment {
	if status.State == nil {
		return newRecoveryAssessment("unknown", "retained-state-absent", "unknown")
	}
	if status.PID != status.State.PID {
		if status.PID == 0 && !pidFilePresent &&
			status.BindingError == "broker PID and state bindings disagree" &&
			intent != nil && intent.InProgress &&
			intent.RecordedPID == status.State.PID &&
			intent.StateSHA256 == status.StateDigest &&
			intent.PIDSHA256 != "" {
			return newRecoveryAssessment("interrupted-stop-candidate", "retained-intent-prefix-matches; stop-revalidates-process-and-unit", "pid-state-disagree")
		}
		return newRecoveryAssessment("refused", "pid-state-disagree-without-matching-intent", "pid-state-disagree")
	}
	if status.BindingError != "" {
		if status.BindingError == "broker pid belongs to a different process" && status.SocketExists &&
			exactLegacyWitnessPrefix(c, status) {
			return newRecoveryAssessment("legacy-witness-candidate", "witness-prefix-matches; stop-revalidates-unit-socket-and-process", "legacy-owner-unverified")
		}
		return newRecoveryAssessment("refused", "other-owner-binding-failed", "other-binding-error")
	}
	if status.PID < 1 {
		return newRecoveryAssessment("unknown", "owner-pid-absent", "unknown")
	}
	if !status.Running || status.StaleState || !status.SocketExists {
		return newRecoveryAssessment("refused", "selected-owner-not-running-or-stale", "not-ready")
	}
	return newRecoveryAssessment("ordinary-binding", "selected-owner-metadata-matches", "matching")
}

// This mirrors only the prefix that selects the existing legacy Stop branch.
// Unit, socket and process checks stay in Stop and cannot be satisfied here.
func exactLegacyWitnessPrefix(c Config, status Status) bool {
	if c.Owner == nil || c.Owner.LegacyWitness == nil || status.State == nil {
		return false
	}
	w, s := c.Owner.LegacyWitness, status.State
	if status.StateDigest != w.StateSHA256 || status.PIDDigest != w.PIDSHA256 ||
		s.Binary != w.Binary || s.StartTicks != 0 || s.SocketDevice != 0 ||
		s.SocketInode != 0 || s.OwnerService != "" ||
		status.PID < 1 || status.PID != s.PID ||
		w.SocketDevice == 0 || w.SocketInode == 0 ||
		!filepath.IsAbs(w.Binary) || filepath.Clean(w.Binary) != w.Binary {
		return false
	}
	selected := *s
	selected.Binary = c.Binary
	if stateBinding(c, &selected) != "" {
		return false
	}
	info, err := socketInfo(c.Socket)
	return err == nil && legacySocketMatches(info, w)
}

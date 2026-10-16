package codexhistory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// This observation accompanies an already-required refusal. None of its values
// changes capture admission, archive eligibility or the preservation warrant.
type missingRolloutObservation struct {
	SchemaVersion         string                   `json:"schema_version"`
	Invariant             string                   `json:"invariant"`
	ObservedAt            string                   `json:"observed_at"`
	SourceDatabase        string                   `json:"source_database"`
	ThreadID              string                   `json:"thread_id"`
	RolloutReference      string                   `json:"rollout_reference"`
	MappedHistoryMember   string                   `json:"mapped_history_member"`
	ReferenceMapping      string                   `json:"reference_mapping"`
	CapturedMemberPresent bool                     `json:"captured_member_present"`
	DeclaredSourceRoot    string                   `json:"declared_source_root"`
	DeclaredSourceMember  string                   `json:"declared_source_member"`
	RowArchived           string                   `json:"row_archived"`
	ArchiveObservation    string                   `json:"archive_observation"`
	Resumability          string                   `json:"resumability"`
	RetainedCustody       *retainedMemberInventory `json:"retained_custody,omitempty"`
}

type missingRolloutError struct {
	observation missingRolloutObservation
}

func (e *missingRolloutError) Error() string {
	// All fields are primitive values; retain the original refusal even if a
	// future field makes serialization unavailable.
	message := fmt.Sprintf("GUI rollout reference is missing from resumable history: %s", e.observation.RolloutReference)
	encoded, err := json.Marshal(e.observation)
	if err != nil {
		return message
	}
	return message + "\ncodex_gui_history_refusal=" + string(encoded)
}

func missingGUIRolloutRefusal(executable, database, databaseRel string, rollout sqliteRollout, historyRel string, options SnapshotOptions) error {
	observation := missingRolloutObservation{
		SchemaVersion:        "devkit/codex-gui-history-refusal/v1",
		Invariant:            "captured-gui-rollout-member-absent",
		ObservedAt:           time.Now().UTC().Format(time.RFC3339Nano),
		SourceDatabase:       databaseRel,
		ThreadID:             rollout.ThreadID,
		RolloutReference:     rollout.RolloutPath,
		MappedHistoryMember:  historyRel,
		ReferenceMapping:     "unknown",
		DeclaredSourceRoot:   filepath.Join(options.HostHome, ".codex"),
		DeclaredSourceMember: "unknown",
		Resumability:         "unclassified",
	}
	// Reuse the same declared mapping, never search an alternate history root.
	if mapped, class, err := rolloutReferenceMapping(rollout.RolloutPath, options); err == nil && mapped == historyRel {
		observation.ReferenceMapping = class
		observation.DeclaredSourceMember = observeDeclaredSourceMember(observation.DeclaredSourceRoot, mapped)
	}
	observation.RowArchived, observation.ArchiveObservation = observeGUIArchiveState(executable, database, rollout.ThreadID)
	observation.RetainedCustody = inventoryRetainedMember(options, rollout.ThreadID, historyRel)
	return &missingRolloutError{observation: observation}
}

func observeDeclaredSourceMember(root, rel string) string {
	// A hostile or stale database reference must not turn this diagnostic into
	// an existence oracle for credentials or other non-history members.
	if !isGUIRolloutMember(rel) {
		return "unknown"
	}
	// Reuse the provider's held-directory/no-follow traversal. Observe only the
	// final member's metadata; never follow an alias or open its body.
	dir, err := openProjectionDir(root)
	if err != nil {
		return sourceMemberObservation(nil, err)
	}
	defer dir.f.Close()
	parent, name, err := dir.parent(rel, false)
	if err != nil {
		return sourceMemberObservation(nil, err)
	}
	defer parent.f.Close()
	info, err := os.Lstat(parent.path(name))
	return sourceMemberObservation(info, err)
}

func sourceMemberObservation(info fs.FileInfo, err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "absent"
	case err != nil || info == nil:
		return "unknown"
	case info.Mode().IsRegular():
		return "present-regular"
	case info.Mode()&os.ModeSymlink != 0:
		return "present-symlink"
	default:
		return "present-nonregular"
	}
}

func observeGUIArchiveState(executable, database, threadID string) (string, string) {
	// Read the exact captured SQLite row. Diagnostic callers retain unknown;
	// unavailable-archive classification admits only the strict integer-1 result.
	// Legacy columns, errors and timeouts never establish archived state.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	query := func(sql string) (string, error) {
		command := exec.CommandContext(ctx, executable, "-readonly", "-batch", "-noheader", database, "PRAGMA query_only=ON; "+sql)
		output, err := command.Output()
		return strings.TrimSpace(string(output)), err
	}
	column, err := query("SELECT name FROM pragma_table_info('threads') WHERE name = 'archived';")
	if err != nil {
		return "unknown", "query-unavailable"
	}
	if column != "archived" {
		return "unknown", "archived-column-unavailable"
	}
	quotedID := "'" + strings.ReplaceAll(threadID, "'", "''") + "'"
	value, err := query("SELECT CASE WHEN typeof(archived) = 'integer' AND archived = 1 THEN 'archived' WHEN typeof(archived) = 'integer' AND archived = 0 THEN 'unarchived' ELSE 'unknown' END FROM threads WHERE source = 'vscode' AND id = " + quotedID + ";")
	if err != nil {
		return "unknown", "query-unavailable"
	}
	if value != "archived" && value != "unarchived" {
		return "unknown", "row-value-unavailable"
	}
	return value, "captured-row-metadata"
}

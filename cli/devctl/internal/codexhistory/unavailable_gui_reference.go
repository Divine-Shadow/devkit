package codexhistory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const GUIAvailableEvidenceSchema = "devkit/codex-gui-history-snapshot/v4"
const GUIAvailableEvidenceStatus = "available-evidence-with-unavailable-gui-references"

type referenceEvidenceMode uint8

const (
	requireAllReferenceBodies referenceEvidenceMode = iota
	allowArchivedReferenceEvidence
	allowGUIReferenceEvidence
	captureAvailableReferenceEvidence
)

// This record preserves a pre-existing hole, not a body or a resumability
// judgment. The strict reader rederives every field from retained evidence.
// Archive metadata is an observation and grants no capture eligibility.
type unavailableGUIReference struct {
	CustodyClass          string `json:"custody_class"`
	SourceDatabase        string `json:"source_database"`
	ThreadID              string `json:"thread_id"`
	RolloutReference      string `json:"rollout_reference"`
	MappedHistoryMember   string `json:"mapped_history_member"`
	ReferenceMapping      string `json:"reference_mapping"`
	ArchiveObservation    string `json:"archive_observation"`
	CapturedMemberPresent bool   `json:"captured_member_present"`
	DeclaredSourceMember  string `json:"declared_source_member"`
	Resumability          string `json:"resumability"`
	ResumeEligible        bool   `json:"resume_eligible"`
}

func (record *unavailableGUIReference) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("unavailable GUI reference must be one exact object")
	}
	keys := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || keys[key] {
			return fmt.Errorf("duplicate unavailable GUI reference field")
		}
		keys[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("null unavailable GUI reference field")
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("extra unavailable GUI reference object")
	}
	want := []string{"custody_class", "source_database", "thread_id", "rollout_reference", "mapped_history_member", "reference_mapping", "archive_observation", "captured_member_present", "declared_source_member", "resumability", "resume_eligible"}
	if len(keys) != len(want) {
		return fmt.Errorf("incomplete unavailable GUI reference fields")
	}
	for _, key := range want {
		if !keys[key] {
			return fmt.Errorf("missing unavailable GUI reference field")
		}
	}
	type plain unavailableGUIReference
	strict := json.NewDecoder(bytes.NewReader(data))
	strict.DisallowUnknownFields()
	return strict.Decode((*plain)(record))
}

func classifyUnavailableGUIReference(executable, database, databaseRel string, rollout sqliteRollout, historyRel string, options SnapshotOptions, checkSource bool) (unavailableGUIReference, error) {
	var empty unavailableGUIReference
	if strings.TrimSpace(rollout.ThreadID) == "" || !isGUIRolloutMember(historyRel) || !filepath.IsAbs(rollout.RolloutPath) || filepath.Clean(rollout.RolloutPath) != rollout.RolloutPath || strings.TrimSpace(rollout.RolloutPath) != rollout.RolloutPath {
		return empty, fmt.Errorf("unavailable GUI reference has no exact identity/member")
	}
	mapped, mapping, err := rolloutReferenceMapping(rollout.RolloutPath, options)
	if err != nil || mapped != historyRel {
		return empty, fmt.Errorf("unavailable GUI reference has no canonical declared mapping")
	}
	if checkSource && observeDeclaredSourceMember(filepath.Join(options.HostHome, ".codex"), historyRel) != "absent" {
		return empty, fmt.Errorf("unavailable GUI reference lacks exact declared-source absence")
	}
	observation, err := capturedArchiveObservation(executable, database, rollout.ThreadID)
	if err != nil {
		return empty, err
	}
	return unavailableGUIReference{
		CustodyClass: "unavailable-gui-reference", SourceDatabase: databaseRel,
		ThreadID: rollout.ThreadID, RolloutReference: rollout.RolloutPath,
		MappedHistoryMember: historyRel, ReferenceMapping: mapping,
		ArchiveObservation: observation, CapturedMemberPresent: false,
		DeclaredSourceMember: "absent", Resumability: "unclassified", ResumeEligible: false,
	}, nil
}

// Only bounded, deterministic classifications leave the captured SQLite. A
// missing legacy column is observable; a query/integrity failure is an error.
func capturedArchiveObservation(executable, database, threadID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	query := func(sql string) (string, error) {
		command := exec.CommandContext(ctx, executable, "-readonly", "-batch", "-noheader", database, "PRAGMA query_only=ON; "+sql)
		output, err := command.Output()
		if err != nil {
			return "", fmt.Errorf("unavailable GUI archive observation query failed")
		}
		return strings.TrimSpace(string(output)), nil
	}
	identity := "source='vscode' AND id='" + strings.ReplaceAll(threadID, "'", "''") + "'"
	count, err := query("SELECT count(*) FROM threads WHERE " + identity + ";")
	if err != nil || count != "1" {
		return "", fmt.Errorf("unavailable GUI reference lacks unique captured row")
	}
	column, err := query("SELECT count(*) FROM pragma_table_info('threads') WHERE name='archived';")
	if err != nil {
		return "", err
	}
	if column == "0" {
		return "column-absent", nil
	}
	if column != "1" {
		return "", fmt.Errorf("unavailable GUI archive column is ambiguous")
	}
	state, err := query("SELECT CASE WHEN typeof(archived)='integer' AND archived=1 THEN 'integer-1' WHEN typeof(archived)='integer' AND archived=0 THEN 'integer-0' ELSE 'non-boolean' END FROM threads WHERE " + identity + ";")
	if err != nil {
		return "", err
	}
	if state != "integer-1" && state != "integer-0" && state != "non-boolean" {
		return "", fmt.Errorf("unavailable GUI archive observation is invalid")
	}
	return state, nil
}

func guiReferenceFromArchived(record unavailableArchivedReference) unavailableGUIReference {
	return unavailableGUIReference{CustodyClass: "unavailable-gui-reference", SourceDatabase: record.SourceDatabase,
		ThreadID: record.ThreadID, RolloutReference: record.RolloutReference, MappedHistoryMember: record.MappedHistoryMember,
		ReferenceMapping: record.ReferenceMapping, ArchiveObservation: "integer-1", CapturedMemberPresent: false,
		DeclaredSourceMember: "absent", Resumability: "unclassified", ResumeEligible: false}
}

func sortUnavailableGUIReferences(records []unavailableGUIReference) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].SourceDatabase != records[j].SourceDatabase {
			return records[i].SourceDatabase < records[j].SourceDatabase
		}
		if records[i].ThreadID != records[j].ThreadID {
			return records[i].ThreadID < records[j].ThreadID
		}
		return records[i].RolloutReference < records[j].RolloutReference
	})
}

func verifyUnavailableGUISourceAbsence(root string, records []unavailableGUIReference) error {
	for _, record := range records {
		if observeDeclaredSourceMember(root, record.MappedHistoryMember) != "absent" {
			return fmt.Errorf("unavailable GUI source member appeared or became unclassifiable")
		}
	}
	return nil
}

func archivedReferenceFromGUI(record unavailableGUIReference) unavailableArchivedReference {
	return unavailableArchivedReference{CustodyClass: "unavailable-archived-reference", SourceDatabase: record.SourceDatabase,
		ThreadID: record.ThreadID, RolloutReference: record.RolloutReference, MappedHistoryMember: record.MappedHistoryMember,
		ReferenceMapping: record.ReferenceMapping, RowArchived: "integer-1", CapturedMemberPresent: false,
		DeclaredSourceMember: "absent", ResumeEligible: false}
}

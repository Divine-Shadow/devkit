package codexhistory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"devkit/cli/devctl/internal/sqliteauthority"
)

// This is a pre-existing hole, not a captured file or a claim of resumability.
// The complete record is reproduced from the retained SQLite and payload by
// VerifyCapturedBundle; a freely edited manifest cannot grant an exception.
type unavailableArchivedReference struct {
	CustodyClass          string `json:"custody_class"`
	SourceDatabase        string `json:"source_database"`
	ThreadID              string `json:"thread_id"`
	RolloutReference      string `json:"rollout_reference"`
	MappedHistoryMember   string `json:"mapped_history_member"`
	ReferenceMapping      string `json:"reference_mapping"`
	RowArchived           string `json:"row_archived"`
	CapturedMemberPresent bool   `json:"captured_member_present"`
	DeclaredSourceMember  string `json:"declared_source_member"`
	ResumeEligible        bool   `json:"resume_eligible"`
}

// All record fields are required, including explicit false/absent values.
// Duplicate or additional keys must not manufacture an eligibility exception.
func (record *unavailableArchivedReference) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("unavailable reference must be one exact object")
	}
	keys := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || keys[key] {
			return fmt.Errorf("duplicate unavailable reference field")
		}
		keys[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("null unavailable reference field")
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("extra unavailable reference object")
	}
	if len(keys) != 10 {
		return fmt.Errorf("incomplete unavailable reference fields")
	}
	for _, key := range []string{"custody_class", "source_database", "thread_id", "rollout_reference", "mapped_history_member", "reference_mapping", "row_archived", "captured_member_present", "declared_source_member", "resume_eligible"} {
		if !keys[key] {
			return fmt.Errorf("missing unavailable reference field")
		}
	}
	type plain unavailableArchivedReference
	strict := json.NewDecoder(bytes.NewReader(data))
	strict.DisallowUnknownFields()
	return strict.Decode((*plain)(record))
}

func classifyUnavailableArchivedReference(executable, database, databaseRel string, rollout sqliteRollout, historyRel string, options SnapshotOptions, checkSource bool) (unavailableArchivedReference, error) {
	var empty unavailableArchivedReference
	if rollout.ThreadID == "" || !isGUIRolloutMember(historyRel) || !filepath.IsAbs(rollout.RolloutPath) || filepath.Clean(rollout.RolloutPath) != rollout.RolloutPath || strings.TrimSpace(rollout.RolloutPath) != rollout.RolloutPath {
		return empty, fmt.Errorf("unavailable archive has no exact GUI identity/member")
	}
	mapped, mapping, err := rolloutReferenceMapping(rollout.RolloutPath, options)
	if err != nil || mapped != historyRel {
		return empty, fmt.Errorf("unavailable archive has no canonical declared mapping")
	}
	state, observation := observeGUIArchiveState(executable, database, rollout.ThreadID)
	if state != "archived" || observation != "captured-row-metadata" {
		return empty, fmt.Errorf("unavailable reference lacks captured integer archived=1")
	}
	root := filepath.Join(options.HostHome, ".codex")
	if checkSource && observeDeclaredSourceMember(root, historyRel) != "absent" {
		return empty, fmt.Errorf("unavailable archive lacks exact declared-source absence")
	}
	return unavailableArchivedReference{
		CustodyClass: "unavailable-archived-reference", SourceDatabase: databaseRel,
		ThreadID: rollout.ThreadID, RolloutReference: rollout.RolloutPath,
		MappedHistoryMember: historyRel, ReferenceMapping: mapping,
		RowArchived: "integer-1", CapturedMemberPresent: false,
		DeclaredSourceMember: "absent", ResumeEligible: false,
	}, nil
}

func sortUnavailableReferences(records []unavailableArchivedReference) {
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

func validSnapshotClass(manifest snapshotManifest) bool {
	switch manifest.SchemaVersion {
	case SnapshotSchema:
		return manifest.Status == "complete" && manifest.UnavailableArchivedReferenceCount == 0 && len(manifest.UnavailableArchivedReferences) == 0 && manifest.UnavailableGUIReferenceCount == 0 && len(manifest.UnavailableGUIReferences) == 0
	case AvailableEvidenceSchema:
		return manifest.Status == AvailableEvidenceStatus && manifest.UnavailableArchivedReferenceCount > 0 && manifest.UnavailableArchivedReferenceCount == len(manifest.UnavailableArchivedReferences) && manifest.UnavailableGUIReferenceCount == 0 && len(manifest.UnavailableGUIReferences) == 0
	case GUIAvailableEvidenceSchema:
		return manifest.Status == GUIAvailableEvidenceStatus && manifest.UnavailableGUIReferenceCount > 0 && manifest.UnavailableGUIReferenceCount == len(manifest.UnavailableGUIReferences) && manifest.UnavailableArchivedReferenceCount == 0 && len(manifest.UnavailableArchivedReferences) == 0
	default:
		return false
	}
}

func verifyUnavailableSourceAbsence(root string, records []unavailableArchivedReference) error {
	for _, record := range records {
		if observeDeclaredSourceMember(root, record.MappedHistoryMember) != "absent" {
			return fmt.Errorf("unavailable archived source member appeared or became unclassifiable")
		}
	}
	return nil
}

func verifyCapturedReferenceEvidence(options SnapshotOptions, manifest snapshotManifest, payload string, files map[string]manifestFile) error {
	executable, err := sqliteauthority.Package()
	if err != nil {
		return err
	}
	// Bundle verification is source-free: the historical source's absence was
	// checked at capture. Current-source verification is a separate entrypoint.
	mode := requireAllReferenceBodies
	if manifest.SchemaVersion == AvailableEvidenceSchema {
		mode = allowArchivedReferenceEvidence
	}
	if manifest.SchemaVersion == GUIAvailableEvidenceSchema {
		mode = allowGUIReferenceEvidence
	}
	count, quarantine, unavailable, unavailableGUI, err := validateSQLiteAndRolloutsEvidenceForClass(executable, payload, files, options, mode, false)
	if err != nil {
		return err
	}
	var total int64
	for _, file := range files {
		total += file.Bytes
	}
	if count != manifest.GUIRolloutCount || len(quarantine) != manifest.QuarantinedGUIRolloutCount || !reflect.DeepEqual(quarantine, manifest.QuarantinedGUIRollouts) || !reflect.DeepEqual(unavailable, manifest.UnavailableArchivedReferences) || !reflect.DeepEqual(unavailableGUI, manifest.UnavailableGUIReferences) || total != manifest.TotalBytes {
		return fmt.Errorf("unavailable archived custody does not match captured SQLite/payload evidence")
	}
	return nil
}

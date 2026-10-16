package codexhistory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devkit/cli/devctl/internal/sqliteauthority"
)

type PreflightResult struct {
	SchemaVersion string `json:"schema_version"`
	Status        string `json:"status"`
	Project       string `json:"project"`
	AgentIndex    int    `json:"agent_index"`
	HostHome      string `json:"host_home"`
	GUIReferences int    `json:"gui_references"`
	ColdCapture   bool   `json:"cold_capture"`
}

// Preflight detects known metadata/presence failures before a lifecycle stops
// its consumer. It is not a live snapshot or a replacement for post-stop cold
// capture. Only private temporary SQLite validation copies are written.
func Preflight(options SnapshotOptions) (PreflightResult, error) {
	result := PreflightResult{SchemaVersion: "devkit/codex-gui-history-preflight/v1", Status: "metadata-checked",
		Project: options.Project, AgentIndex: options.AgentIndex, HostHome: options.HostHome, ColdCapture: false}
	if err := validateOptions(options); err != nil {
		return result, err
	}
	root := filepath.Join(options.HostHome, ".codex")
	files, _, _, err := enumerateHistory(root)
	if err != nil {
		return result, err
	}
	var sqliteFiles []sourceFile
	for _, file := range files {
		if !strings.Contains(file.relPath, "/") && sqliteFamilyPattern.MatchString(file.relPath) {
			sqliteFiles = append(sqliteFiles, file)
		}
	}
	if len(sqliteFiles) == 0 {
		return result, nil
	}
	executable, err := sqliteauthority.Package()
	if err != nil {
		return result, err
	}
	validation, err := os.MkdirTemp("", "devkit-history-preflight-")
	if err != nil {
		return result, fmt.Errorf("create private history preflight: %w", err)
	}
	defer os.RemoveAll(validation)
	for _, file := range sqliteFiles {
		if _, err := copyStableFile(file, filepath.Join(validation, file.relPath)); err != nil {
			return result, fmt.Errorf("history metadata changed/unavailable before stop: %s", file.relPath)
		}
	}
	type reference struct {
		database, databaseRel, member string
		rollout                       sqliteRollout
	}
	var references []reference
	identities := make(map[string]int)
	members := make(map[string]int)
	for _, file := range sqliteFiles {
		if !strings.HasSuffix(file.relPath, ".sqlite") {
			continue
		}
		database := filepath.Join(validation, file.relPath)
		if err := sqliteQuickCheck(executable, database); err != nil {
			return result, fmt.Errorf("history metadata preflight integrity unavailable: %s", file.relPath)
		}
		if !strings.HasPrefix(file.relPath, "state_") {
			continue
		}
		rollouts, err := sqliteGUIRollouts(executable, database)
		if err != nil {
			return result, fmt.Errorf("history metadata preflight GUI index unavailable: %s", file.relPath)
		}
		for _, rollout := range rollouts {
			result.GUIReferences++
			member, err := rolloutRelativePath(rollout.RolloutPath, options)
			if err != nil {
				return result, err
			}
			identities[rollout.ThreadID]++
			members[member]++
			references = append(references, reference{database: database, databaseRel: file.relPath, member: member, rollout: rollout})
		}
	}
	for _, ref := range references {
		switch observeDeclaredSourceMember(root, ref.member) {
		case "present-regular":
			// JSONL content and stable capture remain the mandatory cold gate.
			continue
		case "absent":
			if identities[ref.rollout.ThreadID] != 1 || members[ref.member] != 1 {
				return result, fmt.Errorf("history preflight GUI reference has ambiguous captured identity")
			}
			if _, err := classifyUnavailableGUIReference(executable, ref.database, ref.databaseRel, ref.rollout, ref.member, options, true); err != nil {
				return result, err
			}
		default:
			return result, fmt.Errorf("history metadata preflight member is unavailable or nonregular: %s", ref.member)
		}
	}
	return result, nil
}

package codexhistory

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

const retainedInventoryEntryLimit = 128
const retainedInventoryMatchLimit = 4
const retainedInventoryMemberLimit = 64 << 20

var retainedGenerationName = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}\.[0-9]{9}Z-[0-9a-f]{16}$`)

// Only the existing capture provider calls this observation. It neither selects
// a restore source nor declares a bundle complete, resumable or admitted.
type retainedMemberInventory struct {
	Scope              string                `json:"scope"`
	Status             string                `json:"status"`
	EntriesObserved    int                   `json:"entries_observed"`
	GenerationsChecked int                   `json:"generations_checked"`
	Unclassified       int                   `json:"unclassified_generations"`
	Truncated          bool                  `json:"truncated"`
	Matches            []retainedMemberMatch `json:"matches"`
}

type retainedMemberMatch struct {
	Generation           string `json:"generation"`
	ManifestSHA256       string `json:"manifest_sha256"`
	DeclaredBundleSHA256 string `json:"declared_bundle_sha256"`
	Member               string `json:"member"`
	Bytes                int64  `json:"bytes"`
	SHA256               string `json:"sha256"`
	Validation           string `json:"validation"`
	BundleVerification   string `json:"bundle_verification"`
	ResumeEligible       bool   `json:"resume_eligible"`
}

func inventoryRetainedMember(options SnapshotOptions, threadID, member string) *retainedMemberInventory {
	result := &retainedMemberInventory{Scope: "selected-lane-canonical-custody-only", Status: "unavailable", Matches: []retainedMemberMatch{}}
	// No alternate home, global backup search, caller-selected path or identity.
	if validateOptions(options) != nil || options.WorkspaceRoot == "" || threadID == "" || !isGUIRolloutMember(member) ||
		!filepath.IsLocal(member) || filepath.ToSlash(filepath.Clean(member)) != member {
		return result
	}
	root, err := WorkspaceCustodyRoot(options.WorkspaceRoot, options.Project)
	if err != nil {
		return result
	}
	lane, err := openProjectionDir(filepath.Join(root, "agent"+strconv.Itoa(options.AgentIndex)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.Status = "declared-lane-custody-absent"
		}
		return result
	}
	defer lane.f.Close()
	names, err := lane.f.Readdirnames(retainedInventoryEntryLimit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return result
	}
	result.EntriesObserved = len(names)
	if len(names) > retainedInventoryEntryLimit {
		result.Truncated = true
		names = names[:retainedInventoryEntryLimit]
	}
	sort.Strings(names)
	result.Status = "scan-complete"
	for _, name := range names {
		if !retainedGenerationName.MatchString(name) {
			// Pending/foreign names are not committed canonical generations.
			continue
		}
		result.GenerationsChecked++
		match, classified := inspectRetainedMember(lane, name, options, threadID, member)
		if !classified {
			result.Unclassified++
		}
		if match != nil {
			result.Matches = append(result.Matches, *match)
			if len(result.Matches) == retainedInventoryMatchLimit {
				result.Truncated = true
				break
			}
		}
	}
	if result.Truncated || result.Unclassified != 0 {
		result.Status = "partial-scan"
	}
	return result
}

func inspectRetainedMember(lane *projectionDir, name string, options SnapshotOptions, threadID, member string) (*retainedMemberMatch, bool) {
	generation, err := lane.child(name, false)
	if err != nil {
		return nil, false
	}
	defer generation.f.Close()
	raw, err := generation.read("manifest.json", 4<<20)
	if err != nil {
		return nil, false
	}
	var manifest snapshotManifest
	if projectionStrictJSON(raw, &manifest) != nil || !validSnapshotClass(manifest) ||
		manifest.Project != options.Project || manifest.AgentIndex != options.AgentIndex ||
		manifest.SourceHostHome != options.HostHome || manifest.SourceSandboxHome != options.SandboxHome ||
		manifest.ResetKind != "selected-slot-reset" || manifest.FileCount != len(manifest.Files) ||
		!projectionValidHash(manifest.BundleSHA256) || aggregateHash(manifest.Files) != manifest.BundleSHA256 {
		return nil, false
	}
	var selected *manifestFile
	previous := ""
	for i := range manifest.Files {
		record := &manifest.Files[i]
		if record.Path <= previous || record.Bytes < 0 || !projectionValidHash(record.SHA256) {
			return nil, false
		}
		previous = record.Path
		if record.Path == member {
			selected = record
		}
	}
	if selected == nil {
		return nil, true
	}
	if selected.Bytes > retainedInventoryMemberLimit {
		return nil, false
	}
	body, err := generation.read("payload/"+member, retainedInventoryMemberLimit)
	if err != nil || int64(len(body)) != selected.Bytes || projectionDigest(body) != selected.SHA256 {
		return nil, false
	}
	validation, err := validateGUIRolloutReader(bytes.NewReader(body), member, threadID)
	if err != nil {
		return nil, false
	}
	class := "member-hash-and-session-identity-verified"
	if validation.InvalidRecordCount != 0 {
		class = "matching-identity-with-invalid-records-not-resumable"
	}
	// Re-read the manifest through the held directory after member validation.
	after, err := generation.read("manifest.json", 4<<20)
	if err != nil || !bytes.Equal(raw, after) {
		return nil, false
	}
	return &retainedMemberMatch{Generation: name, ManifestSHA256: projectionDigest(raw),
		DeclaredBundleSHA256: manifest.BundleSHA256, Member: member, Bytes: selected.Bytes,
		SHA256: selected.SHA256, Validation: class, BundleVerification: "not-performed-inventory-only",
		ResumeEligible: false}, true
}

package codexhistory

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"devkit/cli/devctl/internal/sqliteauthority"
)

const SnapshotSchema = "devkit/codex-gui-history-snapshot/v1"

var sqliteFamilyPattern = regexp.MustCompile(`^(state|goals)_[0-9]+\.sqlite(?:-(?:wal|shm))?$`)

var historyDirectories = map[string]struct{}{
	"sessions":          {},
	"archived_sessions": {},
	"rollouts":          {},
	"shell_snapshots":   {},
}

type SnapshotOptions struct {
	Project       string
	ResetKind     string
	AgentIndex    int
	HostWorktree  string
	HostHome      string
	SandboxHome   string
	StateRoot     string
	WorkspaceRoot string
	DryRun        bool
}

type Result struct {
	Status       string
	GenerationID string
	ManifestPath string
	FileCount    int
	TotalBytes   int64
	GUIRollouts  int
	BundleSHA256 string
}

type manifestFile struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type snapshotManifest struct {
	SchemaVersion     string         `json:"schema_version"`
	Status            string         `json:"status"`
	Project           string         `json:"project"`
	ResetKind         string         `json:"reset_kind"`
	AgentIndex        int            `json:"agent_index"`
	CreatedAt         string         `json:"created_at"`
	SourceHostHome    string         `json:"source_host_home"`
	SourceSandboxHome string         `json:"source_sandbox_home"`
	FileCount         int            `json:"file_count"`
	TotalBytes        int64          `json:"total_bytes"`
	GUIRolloutCount   int            `json:"gui_rollout_count"`
	BundleSHA256      string         `json:"bundle_sha256"`
	Files             []manifestFile `json:"files"`
}

type sourceFile struct {
	relPath     string
	path        string
	size        int64
	mode        os.FileMode
	modTimeNano int64
	info        os.FileInfo
}

type sqliteRollout struct {
	ThreadID    string `json:"thread_id"`
	RolloutPath string `json:"rollout_path"`
}

var availableSpaceCheck = requireAvailableSpace

func CustodyRoot(stateRoot, project string) (string, error) {
	stateRoot = strings.TrimSpace(stateRoot)
	project = strings.TrimSpace(project)
	if stateRoot == "" || !filepath.IsAbs(stateRoot) {
		return "", fmt.Errorf("Codex GUI history custody requires an absolute native state root")
	}
	if project == "" || project != filepath.Base(project) {
		return "", fmt.Errorf("Codex GUI history custody requires one exact project name")
	}
	return filepath.Join(filepath.Dir(filepath.Clean(stateRoot)), "codex-gui-history", project), nil
}

func WorkspaceCustodyRoot(workspaceRoot, project string) (string, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	project = strings.TrimSpace(project)
	if workspaceRoot == "" || !filepath.IsAbs(workspaceRoot) || filepath.Clean(workspaceRoot) != workspaceRoot {
		return "", fmt.Errorf("Codex GUI history custody requires an absolute canonical workspace root")
	}
	if project == "" || project != filepath.Base(project) {
		return "", fmt.Errorf("Codex GUI history custody requires one exact project name")
	}
	return filepath.Join(workspaceRoot, ".devkit", "codex-gui-history", project), nil
}

func Capture(options SnapshotOptions) (result Result, retErr error) {
	if err := validateOptions(options); err != nil {
		return result, err
	}
	var custodyProjectRoot string
	var err error
	if strings.TrimSpace(options.WorkspaceRoot) != "" {
		custodyProjectRoot, err = WorkspaceCustodyRoot(options.WorkspaceRoot, options.Project)
	} else {
		custodyProjectRoot, err = CustodyRoot(options.StateRoot, options.Project)
	}
	if err != nil {
		return result, err
	}
	hostWorktree := filepath.Clean(options.HostWorktree)
	hostHome := filepath.Clean(options.HostHome)
	hostCodex := filepath.Join(hostHome, ".codex")
	for name, ownedRoot := range map[string]string{
		"worktree": hostWorktree,
		"home":     hostHome,
	} {
		if pathsOverlap(custodyProjectRoot, ownedRoot) {
			return result, fmt.Errorf("Codex GUI history custody root overlaps the reset-owned %s", name)
		}
	}

	files, directories, exists, err := enumerateHistory(hostCodex)
	if err != nil {
		return result, err
	}
	if options.DryRun {
		var total int64
		for _, file := range files {
			total += file.size
		}
		return Result{Status: "planned", FileCount: len(files), TotalBytes: total}, nil
	}

	sqliteExecutable, err := sqliteauthority.Package()
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(options.WorkspaceRoot) != "" {
		if err := ensurePrivateDirectory(filepath.Join(filepath.Clean(options.WorkspaceRoot), ".devkit")); err != nil {
			return result, err
		}
	}
	if err := ensurePrivateDirectory(filepath.Dir(custodyProjectRoot)); err != nil {
		return result, err
	}
	if err := ensurePrivateDirectory(custodyProjectRoot); err != nil {
		return result, err
	}
	agentRoot := filepath.Join(custodyProjectRoot, "agent"+strconv.Itoa(options.AgentIndex))
	if err := ensurePrivateDirectory(agentRoot); err != nil {
		return result, err
	}
	requiredBytes, err := captureRequiredBytes(files)
	if err != nil {
		return result, err
	}
	if err := availableSpaceCheck(agentRoot, requiredBytes); err != nil {
		return result, err
	}

	generationID, err := newGenerationID()
	if err != nil {
		return result, err
	}
	stagingRoot := filepath.Join(agentRoot, ".staging-"+generationID)
	finalRoot := filepath.Join(agentRoot, generationID)
	if err := os.Mkdir(stagingRoot, 0o700); err != nil {
		return result, fmt.Errorf("create Codex GUI history staging generation: %w", err)
	}
	removeStaging := true
	defer func() {
		if removeStaging {
			retErr = errors.Join(retErr, removeExactStaging(stagingRoot, agentRoot))
		}
	}()
	payloadRoot := filepath.Join(stagingRoot, "payload")
	if err := os.Mkdir(payloadRoot, 0o700); err != nil {
		return result, fmt.Errorf("create Codex GUI history payload: %w", err)
	}
	for _, directory := range directories {
		if err := os.MkdirAll(filepath.Join(payloadRoot, filepath.FromSlash(directory)), 0o700); err != nil {
			return result, fmt.Errorf("create Codex GUI history payload directory %s: %w", directory, err)
		}
	}

	manifestFiles := make([]manifestFile, 0, len(files))
	fileSet := make(map[string]manifestFile, len(files))
	var totalBytes int64
	for _, file := range files {
		destination := filepath.Join(payloadRoot, filepath.FromSlash(file.relPath))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return result, fmt.Errorf("create Codex GUI history parent for %s: %w", file.relPath, err)
		}
		hash, err := copyStableFile(file, destination)
		if err != nil {
			return result, err
		}
		record := manifestFile{Path: file.relPath, Bytes: file.size, SHA256: hash}
		manifestFiles = append(manifestFiles, record)
		fileSet[file.relPath] = record
		totalBytes += file.size
	}
	if err := verifySourceSetStable(hostCodex, files, directories, exists, fileSet); err != nil {
		return result, err
	}

	guiRollouts, err := validateSQLiteAndRollouts(sqliteExecutable, payloadRoot, fileSet, options)
	if err != nil {
		return result, err
	}
	sort.Slice(manifestFiles, func(i, j int) bool { return manifestFiles[i].Path < manifestFiles[j].Path })
	bundleHash := aggregateHash(manifestFiles)
	manifest := snapshotManifest{
		SchemaVersion:     SnapshotSchema,
		Status:            "complete",
		Project:           options.Project,
		ResetKind:         options.ResetKind,
		AgentIndex:        options.AgentIndex,
		CreatedAt:         time.Now().UTC().Format(time.RFC3339Nano),
		SourceHostHome:    hostHome,
		SourceSandboxHome: filepath.Clean(options.SandboxHome),
		FileCount:         len(manifestFiles),
		TotalBytes:        totalBytes,
		GUIRolloutCount:   guiRollouts,
		BundleSHA256:      bundleHash,
		Files:             manifestFiles,
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return result, fmt.Errorf("encode Codex GUI history manifest: %w", err)
	}
	manifestData = append(manifestData, '\n')
	if err := syncPayloadTree(payloadRoot, directories); err != nil {
		return result, err
	}
	manifestPath := filepath.Join(stagingRoot, "manifest.json")
	if err := writeSyncedExclusive(manifestPath, manifestData, 0o600); err != nil {
		return result, err
	}
	if err := syncDirectory(stagingRoot); err != nil {
		return result, err
	}
	// Close the full staging/validation/manifest window against direct source
	// mutation before the generation becomes visible to the reset boundary.
	if err := verifySourceSetStable(hostCodex, files, directories, exists, fileSet); err != nil {
		return result, err
	}
	if err := os.Rename(stagingRoot, finalRoot); err != nil {
		return result, fmt.Errorf("commit Codex GUI history generation: %w", err)
	}
	removeStaging = false
	if err := syncDirectory(agentRoot); err != nil {
		return result, err
	}
	status := "captured"
	if !exists || len(manifestFiles) == 0 {
		status = "empty"
	}
	return Result{
		Status:       status,
		GenerationID: generationID,
		ManifestPath: filepath.Join(finalRoot, "manifest.json"),
		FileCount:    len(manifestFiles),
		TotalBytes:   totalBytes,
		GUIRollouts:  guiRollouts,
		BundleSHA256: bundleHash,
	}, nil
}

func validateOptions(options SnapshotOptions) error {
	if strings.TrimSpace(options.Project) != "dev-all" {
		return fmt.Errorf("Codex GUI history custody is restricted to the exact dev-all project")
	}
	if options.ResetKind != "selected-slot-reset" && options.ResetKind != "whole-prefix-reset" {
		return fmt.Errorf("Codex GUI history custody reset kind is not source-declared")
	}
	if options.AgentIndex < 1 {
		return fmt.Errorf("Codex GUI history custody requires a positive agent index")
	}
	for name, value := range map[string]string{
		"host worktree": options.HostWorktree,
		"host home":     options.HostHome, "sandbox home": options.SandboxHome, "state root": options.StateRoot,
	} {
		if strings.TrimSpace(value) == "" || !filepath.IsAbs(value) {
			return fmt.Errorf("Codex GUI history custody %s must be absolute", name)
		}
	}
	if workspaceRoot := strings.TrimSpace(options.WorkspaceRoot); workspaceRoot != "" {
		if !filepath.IsAbs(workspaceRoot) || filepath.Clean(workspaceRoot) != workspaceRoot {
			return fmt.Errorf("Codex GUI history custody workspace root must be absolute and canonical")
		}
		if filepath.Dir(filepath.Clean(options.HostWorktree)) != workspaceRoot {
			return fmt.Errorf("Codex GUI history custody workspace root must own the selected worktree parent")
		}
	}
	return nil
}

func enumerateHistory(codexRoot string) ([]sourceFile, []string, bool, error) {
	info, err := os.Lstat(codexRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, fmt.Errorf("inspect Codex GUI history source %s: %w", codexRoot, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, false, fmt.Errorf("Codex GUI history source is not a real directory: %s", codexRoot)
	}
	resolved, err := filepath.EvalSymlinks(codexRoot)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(codexRoot) {
		return nil, nil, false, fmt.Errorf("Codex GUI history source resolves through a symlink: %s", codexRoot)
	}

	entries, err := os.ReadDir(codexRoot)
	if err != nil {
		return nil, nil, false, fmt.Errorf("read Codex GUI history source %s: %w", codexRoot, err)
	}
	var files []sourceFile
	var directories []string
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(codexRoot, name)
		if sqliteFamilyPattern.MatchString(name) {
			file, err := inspectSourceFile(path, name)
			if err != nil {
				return nil, nil, false, err
			}
			files = append(files, file)
			continue
		}
		if _, ok := historyDirectories[name]; !ok {
			continue
		}
		entryInfo, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, false, fmt.Errorf("inspect Codex GUI history directory %s: %w", path, err)
		}
		if !entryInfo.IsDir() || entryInfo.Mode()&os.ModeSymlink != 0 {
			return nil, nil, false, fmt.Errorf("Codex GUI history member is not a real directory: %s", path)
		}
		err = filepath.WalkDir(path, func(candidate string, dirEntry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(codexRoot, candidate)
			if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("Codex GUI history member escapes its source root: %s", candidate)
			}
			rel = filepath.ToSlash(rel)
			memberInfo, err := os.Lstat(candidate)
			if err != nil {
				return err
			}
			if memberInfo.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("Codex GUI history refuses symlink member: %s", candidate)
			}
			if memberInfo.IsDir() {
				directories = append(directories, rel)
				return nil
			}
			if !memberInfo.Mode().IsRegular() {
				return fmt.Errorf("Codex GUI history refuses unsupported member: %s", candidate)
			}
			file, err := inspectSourceFile(candidate, rel)
			if err != nil {
				return err
			}
			files = append(files, file)
			return nil
		})
		if err != nil {
			return nil, nil, false, fmt.Errorf("walk Codex GUI history directory %s: %w", path, err)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].relPath < files[j].relPath })
	sort.Strings(directories)
	if err := validateSQLiteFamilies(files); err != nil {
		return nil, nil, false, err
	}
	return files, directories, true, nil
}

func inspectSourceFile(path, rel string) (sourceFile, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return sourceFile{}, fmt.Errorf("inspect Codex GUI history file %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return sourceFile{}, fmt.Errorf("Codex GUI history member is not a regular file: %s", path)
	}
	return sourceFile{
		relPath: filepath.ToSlash(rel), path: path, size: info.Size(), mode: info.Mode(),
		modTimeNano: info.ModTime().UnixNano(), info: info,
	}, nil
}

func validateSQLiteFamilies(files []sourceFile) error {
	set := make(map[string]struct{}, len(files))
	for _, file := range files {
		set[file.relPath] = struct{}{}
	}
	for _, file := range files {
		if strings.HasSuffix(file.relPath, ".sqlite-wal") || strings.HasSuffix(file.relPath, ".sqlite-shm") {
			main := strings.TrimSuffix(strings.TrimSuffix(file.relPath, "-wal"), "-shm")
			if _, ok := set[main]; !ok {
				return fmt.Errorf("Codex GUI history SQLite sidecar has no main database: %s", file.relPath)
			}
		}
	}
	return nil
}

func copyStableFile(source sourceFile, destination string) (string, error) {
	in, err := os.Open(source.path)
	if err != nil {
		return "", fmt.Errorf("open Codex GUI history source %s: %w", source.relPath, err)
	}
	defer in.Close()
	openedInfo, err := in.Stat()
	if err != nil || !os.SameFile(source.info, openedInfo) || openedInfo.Size() != source.size || openedInfo.ModTime().UnixNano() != source.modTimeNano {
		return "", fmt.Errorf("Codex GUI history source changed before capture: %s", source.relPath)
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create Codex GUI history destination %s: %w", source.relPath, err)
	}
	removeDestination := true
	defer func() {
		_ = out.Close()
		if removeDestination {
			_ = os.Remove(destination)
		}
	}()
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(out, hasher), in)
	if err != nil {
		return "", fmt.Errorf("copy Codex GUI history file %s: %w", source.relPath, err)
	}
	if written != source.size {
		return "", fmt.Errorf("Codex GUI history source size changed during capture: %s", source.relPath)
	}
	if err := out.Sync(); err != nil {
		return "", fmt.Errorf("sync Codex GUI history file %s: %w", source.relPath, err)
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("close Codex GUI history file %s: %w", source.relPath, err)
	}
	removeDestination = false
	expected := hex.EncodeToString(hasher.Sum(nil))
	destinationHash, err := hashFile(destination)
	if err != nil {
		return "", err
	}
	if destinationHash != expected {
		return "", fmt.Errorf("Codex GUI history destination hash mismatch: %s", source.relPath)
	}
	return expected, nil
}

func verifySourceSetStable(codexRoot string, before []sourceFile, beforeDirectories []string, existed bool, captured map[string]manifestFile) error {
	after, afterDirectories, afterExisted, err := enumerateHistory(codexRoot)
	if err != nil {
		return err
	}
	if existed != afterExisted || len(before) != len(after) || !equalStrings(beforeDirectories, afterDirectories) {
		return fmt.Errorf("Codex GUI history source file set changed during capture")
	}
	for i := range before {
		if before[i].relPath != after[i].relPath || before[i].size != after[i].size || before[i].mode != after[i].mode || before[i].modTimeNano != after[i].modTimeNano {
			return fmt.Errorf("Codex GUI history source metadata changed during capture: %s", before[i].relPath)
		}
		hash, err := hashFile(after[i].path)
		if err != nil {
			return err
		}
		if captured[after[i].relPath].SHA256 != hash {
			return fmt.Errorf("Codex GUI history source content changed during capture: %s", before[i].relPath)
		}
	}
	return nil
}

func validateSQLiteAndRollouts(sqliteExecutable, payloadRoot string, files map[string]manifestFile, options SnapshotOptions) (int, error) {
	validationRoot := filepath.Join(filepath.Dir(payloadRoot), ".sqlite-validation")
	if err := os.Mkdir(validationRoot, 0o700); err != nil {
		return 0, fmt.Errorf("create private SQLite validation directory: %w", err)
	}
	removeValidation := func() error {
		if err := os.RemoveAll(validationRoot); err != nil {
			return fmt.Errorf("remove private SQLite validation directory: %w", err)
		}
		return nil
	}
	validationCleaned := false
	defer func() {
		if !validationCleaned {
			_ = removeValidation()
		}
	}()
	for rel := range files {
		if !sqliteFamilyPattern.MatchString(filepath.Base(rel)) || strings.Contains(rel, "/") {
			continue
		}
		sourcePath := filepath.Join(payloadRoot, filepath.FromSlash(rel))
		source, err := inspectSourceFile(sourcePath, rel)
		if err != nil {
			return 0, err
		}
		if _, err := copyStableFile(source, filepath.Join(validationRoot, filepath.Base(rel))); err != nil {
			return 0, fmt.Errorf("copy private SQLite validation file %s: %w", rel, err)
		}
	}
	var stateDatabases []string
	for rel := range files {
		if strings.HasPrefix(rel, "state_") && strings.HasSuffix(rel, ".sqlite") {
			stateDatabases = append(stateDatabases, rel)
		}
		if (strings.HasPrefix(rel, "state_") || strings.HasPrefix(rel, "goals_")) && strings.HasSuffix(rel, ".sqlite") {
			if err := sqliteQuickCheck(sqliteExecutable, filepath.Join(validationRoot, filepath.Base(rel))); err != nil {
				return 0, fmt.Errorf("validate captured SQLite database %s: %w", rel, err)
			}
		}
	}
	sort.Strings(stateDatabases)
	guiRollouts := 0
	for _, rel := range stateDatabases {
		rollouts, err := sqliteGUIRollouts(sqliteExecutable, filepath.Join(validationRoot, filepath.Base(rel)))
		if err != nil {
			return 0, fmt.Errorf("enumerate GUI rollout references from %s: %w", rel, err)
		}
		for _, rollout := range rollouts {
			historyRel, err := rolloutRelativePath(rollout.RolloutPath, options)
			if err != nil {
				return 0, err
			}
			if _, ok := files[historyRel]; !ok {
				return 0, fmt.Errorf("GUI rollout reference is missing from resumable history: %s", rollout.RolloutPath)
			}
			if err := validateGUIRolloutJSONL(payloadRoot, historyRel, rollout.ThreadID); err != nil {
				return 0, err
			}
			guiRollouts++
		}
	}
	if err := removeValidation(); err != nil {
		return 0, err
	}
	validationCleaned = true
	return guiRollouts, nil
}

func captureRequiredBytes(files []sourceFile) (int64, error) {
	// The snapshot payload and a private SQLite validation copy coexist until
	// validation completes. Reserve one MiB for directories and the manifest.
	required := int64(1 << 20)
	for _, file := range files {
		copies := int64(1)
		if sqliteFamilyPattern.MatchString(filepath.Base(file.relPath)) && !strings.Contains(file.relPath, "/") {
			copies = 2
		}
		if file.size < 0 || file.size > (int64(^uint64(0)>>1)-required)/copies {
			return 0, fmt.Errorf("Codex GUI history capture size overflow")
		}
		required += file.size * copies
	}
	return required, nil
}

func requireAvailableSpace(path string, required int64) error {
	var stats syscall.Statfs_t
	if err := syscall.Statfs(path, &stats); err != nil {
		return fmt.Errorf("inspect Codex GUI history storage capacity: %w", err)
	}
	available := uint64(stats.Bavail) * uint64(stats.Bsize)
	if required < 0 || uint64(required) > available {
		return fmt.Errorf("insufficient Codex GUI history storage: require %d bytes, have %d", required, available)
	}
	return nil
}

// SetAvailableSpaceCheckForTesting is unavailable to production binaries. In a
// Go test binary it replaces the preflight storage check and returns a function
// that restores the prior check.
func SetAvailableSpaceCheckForTesting(check func(string, int64) error) func() {
	if !strings.HasSuffix(filepath.Base(os.Args[0]), ".test") {
		panic("Codex GUI history capacity override is available only to Go test binaries")
	}
	if check == nil {
		panic("nil Codex GUI history available-space check")
	}
	previous := availableSpaceCheck
	availableSpaceCheck = check
	return func() {
		availableSpaceCheck = previous
	}
}

func sqliteQuickCheck(executable, database string) error {
	command := exec.Command(executable, "-readonly", "-batch", "-noheader", database, "PRAGMA query_only=ON; PRAGMA quick_check;")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("sqlite3 quick_check failed: %w: %s", err, boundedText(stderr.String()))
	}
	if strings.TrimSpace(string(output)) != "ok" {
		return fmt.Errorf("sqlite3 quick_check returned %q", boundedText(string(output)))
	}
	return nil
}

func validateGUIRolloutJSONL(payloadRoot, rel, threadID string) error {
	parts := strings.Split(rel, "/")
	if len(parts) < 2 || (parts[0] != "sessions" && parts[0] != "archived_sessions" && parts[0] != "rollouts") || filepath.Ext(rel) != ".jsonl" {
		return fmt.Errorf("GUI rollout reference is not a resumable JSONL history file: %s", rel)
	}
	if strings.TrimSpace(threadID) == "" {
		return fmt.Errorf("GUI rollout reference has an empty thread identity: %s", rel)
	}
	file, err := os.Open(filepath.Join(payloadRoot, filepath.FromSlash(rel)))
	if err != nil {
		return fmt.Errorf("open captured GUI rollout %s: %w", rel, err)
	}
	defer file.Close()

	type rolloutEnvelope struct {
		Type    string `json:"type"`
		Payload struct {
			ID string `json:"id"`
		} `json:"payload"`
	}
	reader := bufio.NewReader(file)
	records := 0
	lineNumber := 0
	matchingSessionMeta := false
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineNumber++
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) == 0 {
				return fmt.Errorf("captured GUI rollout contains an empty JSONL record at %s:%d", rel, lineNumber)
			}
			var envelope rolloutEnvelope
			if err := json.Unmarshal(trimmed, &envelope); err != nil {
				return fmt.Errorf("captured GUI rollout contains invalid JSON at %s:%d: %w", rel, lineNumber, err)
			}
			records++
			if envelope.Type == "session_meta" {
				if envelope.Payload.ID != threadID {
					return fmt.Errorf("captured GUI rollout session identity mismatch at %s:%d", rel, lineNumber)
				}
				matchingSessionMeta = true
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read captured GUI rollout %s: %w", rel, readErr)
		}
	}
	if records == 0 {
		return fmt.Errorf("captured GUI rollout is empty: %s", rel)
	}
	if !matchingSessionMeta {
		return fmt.Errorf("captured GUI rollout has no matching session_meta identity: %s", rel)
	}
	return nil
}

func sqliteGUIRollouts(executable, database string) ([]sqliteRollout, error) {
	query := "PRAGMA query_only=ON; SELECT id AS thread_id, rollout_path FROM threads WHERE source = 'vscode' ORDER BY id;"
	command := exec.Command(executable, "-readonly", "-batch", "-json", database, query)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("sqlite3 GUI rollout query failed: %w: %s", err, boundedText(stderr.String()))
	}
	if strings.TrimSpace(string(output)) == "" {
		return nil, nil
	}
	var rollouts []sqliteRollout
	if err := json.Unmarshal(output, &rollouts); err != nil {
		return nil, fmt.Errorf("decode sqlite3 GUI rollout query: %w", err)
	}
	return rollouts, nil
}

func rolloutRelativePath(raw string, options SnapshotOptions) (string, error) {
	path := filepath.Clean(strings.TrimSpace(raw))
	if path == "." || !filepath.IsAbs(path) {
		return "", fmt.Errorf("GUI rollout reference is not absolute: %s", raw)
	}
	for _, root := range []string{filepath.Join(options.SandboxHome, ".codex"), filepath.Join(options.HostHome, ".codex")} {
		root = filepath.Clean(root)
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel), nil
		}
	}
	return "", fmt.Errorf("GUI rollout reference escapes the source-derived Codex home: %s", raw)
}

func ensurePrivateDirectory(path string) error {
	path = filepath.Clean(path)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		parent := filepath.Dir(path)
		resolvedParent, resolveErr := filepath.EvalSymlinks(parent)
		if resolveErr != nil || filepath.Clean(resolvedParent) != filepath.Clean(parent) {
			return fmt.Errorf("Codex GUI history directory parent resolves through a symlink: %s", parent)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			return fmt.Errorf("create private Codex GUI history directory %s: %w", path, err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return fmt.Errorf("inspect Codex GUI history directory %s: %w", path, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("Codex GUI history directory is not a private real directory: %s", path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || filepath.Clean(resolved) != path {
		return fmt.Errorf("Codex GUI history directory resolves through a symlink: %s", path)
	}
	return nil
}

func newGenerationID() (string, error) {
	var nonce [8]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return "", fmt.Errorf("generate Codex GUI history generation id: %w", err)
	}
	return time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(nonce[:]), nil
}

func writeSyncedExclusive(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create Codex GUI history manifest: %w", err)
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write Codex GUI history manifest: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync Codex GUI history manifest: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close Codex GUI history manifest: %w", err)
	}
	remove = false
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open Codex GUI history directory for sync %s: %w", path, err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync Codex GUI history directory %s: %w", path, err)
	}
	return nil
}

func syncPayloadTree(payloadRoot string, directories []string) error {
	ordered := append([]string(nil), directories...)
	sort.Slice(ordered, func(i, j int) bool {
		return strings.Count(ordered[i], "/") > strings.Count(ordered[j], "/")
	})
	for _, directory := range ordered {
		if err := syncDirectory(filepath.Join(payloadRoot, filepath.FromSlash(directory))); err != nil {
			return err
		}
	}
	return syncDirectory(payloadRoot)
}

func removeExactStaging(stagingRoot, agentRoot string) error {
	rel, err := filepath.Rel(agentRoot, stagingRoot)
	if err != nil || rel == "." || filepath.Dir(rel) != "." || !strings.HasPrefix(filepath.Base(rel), ".staging-") {
		return fmt.Errorf("refuse cleanup outside exact Codex GUI history staging root")
	}
	if err := os.RemoveAll(stagingRoot); err != nil {
		return fmt.Errorf("cleanup Codex GUI history staging root: %w", err)
	}
	return nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open Codex GUI history file for hashing %s: %w", path, err)
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("hash Codex GUI history file %s: %w", path, err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func aggregateHash(files []manifestFile) string {
	hasher := sha256.New()
	for _, file := range files {
		_, _ = fmt.Fprintf(hasher, "%s\x00%d\x00%s\n", file.Path, file.Bytes, file.SHA256)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func pathsOverlap(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	return pathWithin(a, b) || pathWithin(b, a)
}

func pathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func boundedText(value string) string {
	value = strings.TrimSpace(strings.ToValidUTF8(value, ""))
	if len(value) > 512 {
		return value[:512] + "..."
	}
	return value
}

package codexhistory

// Normal preparation may repair an explicitly source-bound missing native
// history projection. Reset capture/retirement never calls this code.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"devkit/cli/devctl/internal/sqliteauthority"
)

// Package tests can interrupt actual durable transitions. No production
// command, configuration or environment exposes this hook.
var preparationProjectionCheckpoint func(string) error

func projectionCheckpoint(point string) error {
	if preparationProjectionCheckpoint != nil {
		return preparationProjectionCheckpoint(point)
	}
	return nil
}

type PreparationProjection struct {
	ManifestPath   string
	ManifestSHA256 string
	BundleSHA256   string
	ThreadID       string
}

type projectionJournal struct {
	Schema         string        `json:"schema"`
	Status         string        `json:"status"`
	ManifestSHA256 string        `json:"manifest_sha256"`
	BundleSHA256   string        `json:"bundle_sha256"`
	ThreadID       string        `json:"thread_id"`
	Before         *manifestFile `json:"before_goals,omitempty"`
	Goal           manifestFile  `json:"goal"`
	Rollout        manifestFile  `json:"rollout"`
}

type projectionDir struct{ f *os.File }

func openProjectionDir(path string) (*projectionDir, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("history projection directory is not canonical")
	}
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	d := &projectionDir{os.NewFile(uintptr(fd), "/")}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" {
			continue
		}
		next, err := d.child(part, false)
		d.f.Close()
		if err != nil {
			return nil, err
		}
		d = next
	}
	return d, nil
}

func (d *projectionDir) path(name string) string {
	return "/proc/self/fd/" + strconv.Itoa(int(d.f.Fd())) + "/" + name
}
func (d *projectionDir) externalPath(name string) string {
	return "/proc/" + strconv.Itoa(os.Getpid()) + "/fd/" + strconv.Itoa(int(d.f.Fd())) + "/" + name
}

func (d *projectionDir) child(name string, create bool) (*projectionDir, error) {
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
		return nil, errors.New("unsupported history directory component")
	}
	if create {
		err := syscall.Mkdirat(int(d.f.Fd()), name, 0o700)
		if err != nil && !errors.Is(err, syscall.EEXIST) {
			return nil, err
		}
		if err == nil {
			if err := d.f.Sync(); err != nil {
				return nil, err
			}
		}
	}
	fd, err := syscall.Openat(int(d.f.Fd()), name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	if create {
		if err := projectionOwned(f); err != nil {
			f.Close()
			return nil, err
		}
	}
	return &projectionDir{f}, nil
}

func projectionOwned(f *os.File) error {
	i, err := f.Stat()
	if err != nil {
		return err
	}
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok || s.Uid != uint32(os.Geteuid()) {
		return errors.New("history projection member has foreign ownership")
	}
	return nil
}

func (d *projectionDir) parent(rel string, create bool) (*projectionDir, string, error) {
	if rel == "" || filepath.IsAbs(rel) || filepath.ToSlash(filepath.Clean(rel)) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, "", errors.New("unsupported history projection path")
	}
	fd, err := syscall.Dup(int(d.f.Fd()))
	if err != nil {
		return nil, "", err
	}
	current := &projectionDir{os.NewFile(uintptr(fd), "projection")}
	parts := strings.Split(rel, "/")
	for _, part := range parts[:len(parts)-1] {
		next, err := current.child(part, create)
		current.f.Close()
		if err != nil {
			return nil, "", err
		}
		current = next
		if err := projectionOwned(current.f); err != nil {
			current.f.Close()
			return nil, "", err
		}
	}
	return current, parts[len(parts)-1], nil
}

func (d *projectionDir) read(rel string, limit int64) ([]byte, error) {
	p, name, err := d.parent(rel, false)
	if err != nil {
		return nil, err
	}
	defer p.f.Close()
	fd, err := syscall.Openat(int(p.f.Fd()), name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	i, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !i.Mode().IsRegular() || i.Size() < 0 || i.Size() > limit {
		return nil, errors.New("unsupported history projection file")
	}
	if err := projectionOwned(f); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	a, err := f.Stat()
	if err != nil || !os.SameFile(i, a) || i.Size() != a.Size() || i.ModTime() != a.ModTime() || int64(len(b)) != i.Size() {
		return nil, errors.New("history projection file changed during read")
	}
	// Bind the bytes to the still-named inode, not just an unlinked handle.
	named, err := os.Lstat(p.path(name))
	if err != nil || !os.SameFile(i, named) {
		return nil, errors.New("history projection pathname changed during read")
	}
	return b, nil
}

func (d *projectionDir) exclusive(rel string, data []byte) error {
	p, name, err := d.parent(rel, true)
	if err != nil {
		return err
	}
	defer p.f.Close()
	fd, err := syscall.Openat(int(p.f.Fd()), name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), name)
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	return p.f.Sync()
}

func projectionDigest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }

func projectionRecord(path string, data []byte) manifestFile {
	return manifestFile{Path: path, Bytes: int64(len(data)), SHA256: projectionDigest(data)}
}

func projectionStrictJSON(data []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("history projection JSON must contain one object")
	}
	return nil
}

func projectionValidHash(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size && s == strings.ToLower(s)
}

func projectionSQLite(executable, path, query string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, executable, "-readonly", "-batch", "-json", "file:"+path+"?immutable=1", "PRAGMA query_only=ON; PRAGMA trusted_schema=OFF; "+query)
	b, err := c.Output()
	if err != nil {
		return nil, errors.New("history projection SQLite validation failed")
	}
	return b, nil
}

func projectionQuickCheck(executable, path string) error {
	b, err := projectionSQLite(executable, path, "PRAGMA quick_check;")
	if err != nil {
		return err
	}
	var rows []map[string]string
	if err := json.Unmarshal(b, &rows); err != nil {
		return err
	}
	if len(rows) != 1 || rows[0]["quick_check"] != "ok" {
		return errors.New("history projection database integrity failed")
	}
	return nil
}

func (d *projectionDir) members(prefix string, seen map[string]bool) error {
	names, err := d.f.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		rel := name
		if prefix != "" {
			rel = prefix + "/" + name
		}
		i, err := os.Lstat(d.path(name))
		if err != nil {
			return err
		}
		if i.IsDir() {
			if _, ok := historyDirectories[strings.Split(rel, "/")[0]]; !ok {
				return errors.New("retained payload has an unsupported directory")
			}
			child, err := d.child(name, false)
			if err != nil {
				return err
			}
			err = child.members(rel, seen)
			child.f.Close()
			if err != nil {
				return err
			}
		} else {
			if !i.Mode().IsRegular() || !seen[rel] {
				return errors.New("retained payload has an extra or unsupported member")
			}
			delete(seen, rel)
		}
	}
	return nil
}

func projectionEmptyGoals(executable, path string) error {
	var tables []struct {
		Name string `json:"name"`
	}
	b, err := projectionSQLite(executable, path, "SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name;")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &tables); err != nil {
		return err
	}
	seenGoals := false
	for _, table := range tables {
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(table.Name) {
			return errors.New("unsupported goals table name")
		}
		if table.Name == "_sqlx_migrations" {
			continue
		}
		if table.Name == "thread_goals" {
			seenGoals = true
		}
		var counts []struct {
			Count int64 `json:"count"`
		}
		b, err := projectionSQLite(executable, path, "SELECT count(*) AS count FROM \""+table.Name+"\";")
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &counts); err != nil {
			return err
		}
		if len(counts) != 1 || counts[0].Count != 0 {
			return errors.New("history projection refuses nonempty current goals business state")
		}
	}
	if !seenGoals {
		return errors.New("current goals schema is not native")
	}
	return nil
}

// ProjectPreparationHistory requires an existing owning lifecycle lease and an
// absent selected native consumer before installation. Committed replay only
// verifies custody and permits the healthy current owner. It never touches state,
// credentials or reset paths. Only an immutable selected projection can call it.
func ProjectPreparationHistory(options SnapshotOptions, binding PreparationProjection) error {
	if binding.ManifestPath == "" && binding.ManifestSHA256 == "" && binding.BundleSHA256 == "" && binding.ThreadID == "" {
		return nil
	}
	if !projectionValidHash(binding.ManifestSHA256) || !projectionValidHash(binding.BundleSHA256) || !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(binding.ThreadID) {
		return errors.New("invalid source-bound retained history identity")
	}
	if err := validateOptions(options); err != nil {
		return err
	}
	if options.Project != "dev-all" || options.ResetKind != "selected-slot-reset" || options.WorkspaceRoot == "" {
		return errors.New("retained preparation history lacks selected native geometry")
	}
	custody, err := WorkspaceCustodyRoot(options.WorkspaceRoot, options.Project)
	if err != nil {
		return err
	}
	agentRoot := filepath.Join(custody, "agent"+strconv.Itoa(options.AgentIndex))
	if filepath.Clean(binding.ManifestPath) != binding.ManifestPath || filepath.Base(binding.ManifestPath) != "manifest.json" || filepath.Dir(filepath.Dir(binding.ManifestPath)) != agentRoot {
		return errors.New("retained history binding escapes selected lane custody")
	}
	source, err := openProjectionDir(filepath.Dir(binding.ManifestPath))
	if err != nil {
		return err
	}
	defer source.f.Close()
	mb, err := source.read("manifest.json", 4<<20)
	if err != nil {
		return err
	}
	if projectionDigest(mb) != binding.ManifestSHA256 {
		return errors.New("retained history manifest digest changed")
	}
	var manifest snapshotManifest
	if err := projectionStrictJSON(mb, &manifest); err != nil {
		return err
	}
	if manifest.SchemaVersion != SnapshotSchema || manifest.Status != "complete" || manifest.Project != options.Project || manifest.ResetKind != options.ResetKind || manifest.AgentIndex != options.AgentIndex || manifest.SourceHostHome != options.HostHome || manifest.QuarantinedGUIRolloutCount != 0 || len(manifest.QuarantinedGUIRollouts) != 0 || manifest.BundleSHA256 != binding.BundleSHA256 || len(manifest.Files) != manifest.FileCount || manifest.FileCount != 3 || aggregateHash(manifest.Files) != binding.BundleSHA256 {
		return errors.New("retained history manifest does not bind one complete nonquarantined native thread")
	}
	payload, err := source.child("payload", false)
	if err != nil {
		return err
	}
	defer payload.f.Close()
	data := map[string][]byte{}
	total := int64(0)
	previous := ""
	for _, record := range manifest.Files {
		if record.Path <= previous || record.Bytes < 0 || !projectionValidHash(record.SHA256) {
			return errors.New("invalid retained history member identity")
		}
		previous = record.Path
		b, err := payload.read(record.Path, 128<<20)
		if err != nil {
			return err
		}
		if int64(len(b)) != record.Bytes || projectionDigest(b) != record.SHA256 {
			return errors.New("retained history member digest changed")
		}
		data[record.Path] = b
		total += record.Bytes
	}
	if total != manifest.TotalBytes {
		return errors.New("retained history aggregate byte count changed")
	}
	seen := map[string]bool{}
	for rel := range data {
		seen[rel] = true
	}
	if err := payload.members("", seen); err != nil {
		return err
	}
	if len(seen) != 0 {
		return errors.New("retained payload has missing members")
	}
	// This maintenance projection is deliberately bounded to one source-bound
	// goals family, state binding witness and resumable rollout, without sidecars.
	goalData, goalOK := data["goals_1.sqlite"]
	_, stateOK := data["state_5.sqlite"]
	rolloutRel := ""
	for rel := range data {
		if strings.HasPrefix(rel, "sessions/") && strings.HasSuffix(rel, "-"+binding.ThreadID+".jsonl") {
			if rolloutRel != "" {
				return errors.New("ambiguous retained rollout")
			}
			rolloutRel = rel
		}
	}
	if !goalOK || !stateOK || rolloutRel == "" {
		return errors.New("unsupported retained preparation history family")
	}
	validation, err := validateGUIRolloutJSONL(payload.path("."), rolloutRel, binding.ThreadID)
	if err != nil || validation.InvalidRecordCount != 0 {
		return errors.New("retained rollout is not eligible for native resume")
	}
	target, err := openProjectionDir(filepath.Join(options.HostHome, ".codex"))
	if err != nil {
		return err
	}
	defer target.f.Close()
	if err := projectionOwned(target.f); err != nil {
		return err
	}
	journalParent, err := target.child(".retained-history-projection", true)
	if err != nil {
		return err
	}
	defer journalParent.f.Close()
	jdir, err := journalParent.child(binding.ManifestSHA256, true)
	if err != nil {
		return err
	}
	defer jdir.f.Close()
	goal := projectionRecord("goals_1.sqlite", goalData)
	rollout := projectionRecord(rolloutRel, data[rolloutRel])
	journal := projectionJournal{Schema: "devkit/normal-retained-history-projection/v1", Status: "prepared", ManifestSHA256: binding.ManifestSHA256, BundleSHA256: binding.BundleSHA256, ThreadID: binding.ThreadID, Goal: goal, Rollout: rollout}
	jb, err := jdir.read("journal.json", 4<<20)
	if err == nil {
		var existing projectionJournal
		if err := projectionStrictJSON(jb, &existing); err != nil {
			return err
		}
		if existing.Schema != journal.Schema || existing.ManifestSHA256 != journal.ManifestSHA256 || existing.BundleSHA256 != journal.BundleSHA256 || existing.ThreadID != journal.ThreadID || existing.Goal != goal || existing.Rollout != rollout || (existing.Status != "prepared" && existing.Status != "committed") {
			return errors.New("foreign or invalid history projection journal")
		}
		journal = existing
		if journal.Status == "committed" {
			if _, err := target.read(goal.Path, 128<<20); err != nil {
				return errors.New("committed native goals family is missing or unsupported")
			}
			b, err := target.read(rollout.Path, 128<<20)
			if err != nil || !bytes.HasPrefix(b, data[rolloutRel]) {
				return errors.New("projected native history lost its original prefix")
			}
			if journal.Before != nil {
				b, err := jdir.read("before-goals.sqlite", 128<<20)
				if err != nil || projectionRecord(journal.Before.Path, b) != *journal.Before {
					return errors.New("preserved original goals custody changed")
				}
			}
			return nil
		}
	} else if errors.Is(err, syscall.ENOENT) {
		// Existing owning lifecycle removes this marker only after exact native
		// owner retirement. A live or stale marker is an owning-boundary refusal.
		if _, err := os.Lstat(target.path("a" + strconv.Itoa(options.AgentIndex) + "-app.sock")); !errors.Is(err, os.ErrNotExist) {
			return errors.New("retained history projection requires the selected native socket to be absent")
		}
		// Refuse any staging residue without its durable admitted journal.
		entries, err := jdir.f.ReadDir(-1)
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return errors.New("unjournaled history projection staging exists")
		}
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			if _, err := target.read("goals_1.sqlite"+suffix, 128<<20); !errors.Is(err, syscall.ENOENT) {
				return errors.New("current goals family has a sidecar or unsupported member")
			}
		}
		if _, err := target.read(rollout.Path, 128<<20); !errors.Is(err, syscall.ENOENT) {
			return errors.New("history projection refuses an existing rollout collision")
		}
		executable, err := sqliteauthority.Package()
		if err != nil {
			return err
		}
		// Validation copies are private computation; the cold source is read-only.
		if err := jdir.exclusive("cold-state.sqlite", data["state_5.sqlite"]); err != nil {
			return err
		}
		if err := jdir.exclusive("pending-goals.sqlite", goalData); err != nil {
			return err
		}
		if err := projectionQuickCheck(executable, jdir.externalPath("cold-state.sqlite")); err != nil {
			return err
		}
		if err := projectionQuickCheck(executable, jdir.externalPath("pending-goals.sqlite")); err != nil {
			return err
		}
		var rows []struct {
			ThreadID    string `json:"thread_id"`
			RolloutPath string `json:"rollout_path"`
		}
		b, err := projectionSQLite(executable, jdir.externalPath("cold-state.sqlite"), "SELECT id AS thread_id,rollout_path FROM threads WHERE source='vscode';")
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &rows); err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ThreadID != binding.ThreadID {
			return errors.New("cold state does not bind the retained native thread")
		}
		coldOptions := options
		coldOptions.SandboxHome = manifest.SourceSandboxHome
		rel, err := rolloutRelativePath(rows[0].RolloutPath, coldOptions)
		if err != nil || rel != rollout.Path {
			return errors.New("cold state rollout binding differs")
		}
		var goals []struct {
			ThreadID string `json:"thread_id"`
		}
		b, err = projectionSQLite(executable, jdir.externalPath("pending-goals.sqlite"), "SELECT thread_id FROM thread_goals;")
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &goals); err != nil {
			return err
		}
		if len(goals) != 1 || goals[0].ThreadID != binding.ThreadID {
			return errors.New("cold goals do not bind only the retained thread")
		}
		before, err := target.read(goal.Path, 128<<20)
		if err == nil {
			record := projectionRecord(goal.Path, before)
			journal.Before = &record
			if err := jdir.exclusive("before-validation.sqlite", before); err != nil {
				return err
			}
			if err := projectionEmptyGoals(executable, jdir.externalPath("before-validation.sqlite")); err != nil {
				return err
			}
		} else if !errors.Is(err, syscall.ENOENT) {
			return err
		}
		if err := jdir.exclusive("pending-rollout.jsonl", data[rolloutRel]); err != nil {
			return err
		}
		jb, _ := json.Marshal(journal)
		if err := jdir.exclusive("journal.json", append(jb, '\n')); err != nil {
			return err
		}
	} else {
		return err
	}
	// Existing owning lifecycle removes this marker only after exact native
	// owner retirement. A live or stale marker is an owning-boundary refusal.
	if _, err := os.Lstat(target.path("a" + strconv.Itoa(options.AgentIndex) + "-app.sock")); !errors.Is(err, os.ErrNotExist) {
		return errors.New("retained history projection requires the selected native socket to be absent")
	}
	if journal.Before != nil {
		if journal.Before.Path != goal.Path || !projectionValidHash(journal.Before.SHA256) || journal.Before.Bytes < 0 {
			return errors.New("invalid preserved goals custody identity")
		}
		before, err := jdir.read("before-validation.sqlite", 128<<20)
		if err != nil || projectionRecord(goal.Path, before) != *journal.Before {
			return errors.New("original goals validation copy changed")
		}
		executable, err := sqliteauthority.Package()
		if err != nil {
			return err
		}
		if err := projectionEmptyGoals(executable, jdir.externalPath("before-validation.sqlite")); err != nil {
			return err
		}
	}
	if err := continuePreparationProjection(target, jdir, journal, data); err != nil {
		return err
	}
	// Recheck source identity and its complete member set before native admission.
	manifestAgain, err := source.read("manifest.json", 4<<20)
	if err != nil || !bytes.Equal(manifestAgain, mb) {
		return errors.New("cold manifest changed during projection")
	}
	freshPayload, err := source.child("payload", false)
	if err != nil {
		return err
	}
	defer freshPayload.f.Close()
	originalInfo, err := payload.f.Stat()
	if err != nil {
		return err
	}
	freshInfo, err := freshPayload.f.Stat()
	if err != nil || !os.SameFile(originalInfo, freshInfo) {
		return errors.New("cold payload directory changed during projection")
	}
	seenAgain := map[string]bool{}
	for rel := range data {
		seenAgain[rel] = true
	}
	if err := freshPayload.members("", seenAgain); err != nil {
		return err
	}
	if len(seenAgain) != 0 {
		return errors.New("cold payload lost a member during projection")
	}
	// Recheck every cold member before granting the native launcher its effect.
	for _, record := range manifest.Files {
		b, err := payload.read(record.Path, 128<<20)
		if err != nil || projectionRecord(record.Path, b) != record {
			return errors.New("cold history changed during projection")
		}
	}
	for _, record := range []manifestFile{goal, rollout} {
		b, err := target.read(record.Path, 128<<20)
		if err != nil || projectionRecord(record.Path, b) != record {
			return errors.New("projected history changed before native admission")
		}
	}
	if err := projectionNoGoalSidecars(target); err != nil {
		return err
	}
	if err := projectionCheckpoint("before-commit"); err != nil {
		return err
	}
	journal.Status = "committed"
	jb, _ = json.Marshal(journal)
	committedBytes := append(jb, '\n')
	if err := jdir.exclusive("journal.committed", committedBytes); err != nil {
		if !errors.Is(err, syscall.EEXIST) {
			return err
		}
		pending, readErr := jdir.read("journal.committed", 4<<20)
		if readErr != nil || !bytes.Equal(pending, committedBytes) {
			return errors.New("foreign history commit artifact")
		}
	}
	if err := projectionCheckpoint("commit-artifact-written"); err != nil {
		return err
	}
	if err := os.Rename(jdir.path("journal.committed"), jdir.path("journal.json")); err != nil {
		return err
	}
	return jdir.f.Sync()
}

func projectionNoGoalSidecars(target *projectionDir) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(target.path("goals_1.sqlite" + suffix)); !errors.Is(err, os.ErrNotExist) {
			return errors.New("current goals family has a sidecar or unsupported member")
		}
	}
	return nil
}

func continuePreparationProjection(target, jdir *projectionDir, j projectionJournal, data map[string][]byte) error {
	if err := projectionNoGoalSidecars(target); err != nil {
		return err
	}
	if j.Before != nil {
		before, err := jdir.read("before-goals.sqlite", 128<<20)
		if errors.Is(err, syscall.ENOENT) {
			current, err := target.read(j.Goal.Path, 128<<20)
			if err != nil || projectionRecord(j.Goal.Path, current) != *j.Before {
				return errors.New("current goals changed before preserved retirement")
			}
			if err := projectionNoGoalSidecars(target); err != nil {
				return err
			}
			if err := projectionCheckpoint("before-retirement"); err != nil {
				return err
			}
			if err := os.Rename(target.path(j.Goal.Path), jdir.path("before-goals.sqlite")); err != nil {
				return err
			}
			if err := target.f.Sync(); err != nil {
				return err
			}
			if err := jdir.f.Sync(); err != nil {
				return err
			}
			if err := projectionCheckpoint("after-retirement"); err != nil {
				return err
			}
		} else if err != nil || projectionRecord(j.Before.Path, before) != *j.Before {
			return errors.New("preserved empty goals database changed")
		}
	}
	for _, step := range []struct {
		record  manifestFile
		pending string
	}{{j.Goal, "pending-goals.sqlite"}, {j.Rollout, "pending-rollout.jsonl"}} {
		b, err := target.read(step.record.Path, 128<<20)
		if err == nil {
			if projectionRecord(step.record.Path, b) != step.record {
				return errors.New("history projection collision during recovery")
			}
			pending, readErr := jdir.read(step.pending, 128<<20)
			if readErr == nil {
				if projectionRecord(step.record.Path, pending) != step.record {
					return errors.New("leftover projection staging changed")
				}
				if err := os.Remove(jdir.path(step.pending)); err != nil {
					return err
				}
				if err := jdir.f.Sync(); err != nil {
					return err
				}
			} else if !errors.Is(readErr, syscall.ENOENT) {
				return readErr
			}
			continue
		}
		if !errors.Is(err, syscall.ENOENT) {
			return err
		}
		b, err = jdir.read(step.pending, 128<<20)
		if err != nil || projectionRecord(step.record.Path, b) != step.record {
			return errors.New("pending history projection member changed")
		}
		parent, name, err := target.parent(step.record.Path, true)
		if err != nil {
			return err
		}
		// Atomic no-overwrite publication of a private COPY, never a cold inode.
		if err := projectionNoGoalSidecars(target); err != nil {
			parent.f.Close()
			return err
		}
		err = os.Link(jdir.path(step.pending), parent.path(name))
		if err == nil {
			err = parent.f.Sync()
		}
		parent.f.Close()
		if err != nil {
			return err
		}
		if err := projectionCheckpoint("installed-" + step.record.Path); err != nil {
			return err
		}
		if err := os.Remove(jdir.path(step.pending)); err != nil {
			return err
		}
		if err := jdir.f.Sync(); err != nil {
			return err
		}
	}
	return nil
}

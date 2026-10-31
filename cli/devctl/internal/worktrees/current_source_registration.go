package worktrees

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
	"unicode/utf8"
)

const SourceRegistrationLookupLimit = 20 * time.Second

// SourceRegistrationBinding is the owning runtime's current registration
// projection. The CLI obtains it only from its package-bound lookup executable;
// neither a caller root nor a caller-selected registration file is an input.
type SourceRegistrationBinding struct {
	SchemaVersion      string `json:"schemaVersion"`
	WorkspaceID        string `json:"workspaceId"`
	Worktree           string `json:"worktree"`
	BaseWorktree       string `json:"baseWorktree"`
	Home               string `json:"home"`
	TargetID           string `json:"targetId"`
	AgentIndex         int    `json:"agentIndex"`
	CodexHome          string `json:"codexHome"`
	GitDirectory       string `json:"gitDirectory"`
	CommonDirectory    string `json:"commonDirectory"`
	RegistrationSHA256 string `json:"registrationSha256"`
}

// OwnedSourceRegistration retains the actual lookup and directory identities.
// Rechecking it under Git's ref lock detects a changed registration or replaced
// root before the existing transaction can commit.
type OwnedSourceRegistration struct {
	binding SourceRegistrationBinding
	lookup  func(context.Context) (SourceRegistrationBinding, error)
	paths   map[string]os.FileInfo
}

var sourceRegistrationID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var sourceRegistrationDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func DecodeSourceRegistration(reader io.Reader) (SourceRegistrationBinding, error) {
	var binding SourceRegistrationBinding
	data, err := io.ReadAll(io.LimitReader(reader, 32769))
	if err != nil || len(data) > 32768 || !utf8.Valid(data) {
		return binding, fmt.Errorf("registered-source lookup response exceeds its bounded contract")
	}
	// The ordinary JSON decoder accepts repeated keys. A conflicting binding
	// must not acquire meaning from the order of duplicate producer fields.
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return binding, fmt.Errorf("registered-source lookup requires one JSON object")
	}
	seen := map[string]bool{}
	required := map[string]bool{
		"schemaVersion": true, "workspaceId": true, "worktree": true,
		"baseWorktree": true, "home": true, "targetId": true,
		"agentIndex": true, "codexHome": true, "gitDirectory": true,
		"commonDirectory": true, "registrationSha256": true,
	}
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] || !required[name] {
			return binding, fmt.Errorf("registered-source lookup has a duplicate or invalid field")
		}
		seen[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return binding, fmt.Errorf("registered-source lookup is malformed: %w", err)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return binding, fmt.Errorf("registered-source lookup requires every typed field")
		}
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') || len(seen) != len(required) {
		return binding, fmt.Errorf("registered-source lookup is incomplete")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return binding, fmt.Errorf("registered-source lookup has trailing data")
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&binding); err != nil {
		return binding, fmt.Errorf("registered-source lookup does not match its schema: %w", err)
	}
	return binding, nil
}

func validateSourceRegistration(s CurrentSource, binding SourceRegistrationBinding) error {
	if binding.SchemaVersion != "devkit/current-source-registration/v1" ||
		!sourceRegistrationID.MatchString(binding.WorkspaceID) ||
		!sourceRegistrationDigest.MatchString(binding.RegistrationSHA256) {
		return fmt.Errorf("registered-source lookup lacks a current typed registration")
	}
	if s.TargetID == "" || binding.TargetID != s.TargetID || binding.AgentIndex != s.Index ||
		binding.Worktree != s.Worktree || binding.BaseWorktree != s.BaseWorktree ||
		binding.Home != s.Home || binding.CodexHome != filepath.Join(s.Home, ".codex") {
		return fmt.Errorf("registered-source binding differs from the actual consumer or current root")
	}
	// This variant admits an actually registered sibling Source, not every
	// directory within the mounted workspace or an arbitrary caller-selected root.
	if s.Worktree == s.BaseWorktree || filepath.Dir(s.Worktree) != filepath.Dir(s.BaseWorktree) {
		return fmt.Errorf("registered-source preparation requires the consumer's exact sibling-root geometry")
	}
	for _, path := range []string{binding.Worktree, binding.BaseWorktree, binding.Home, binding.GitDirectory, binding.CommonDirectory} {
		actual, err := filepath.EvalSymlinks(path)
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || err != nil || actual != path {
			return fmt.Errorf("registered-source binding contains redirected or noncanonical geometry")
		}
	}
	return nil
}

func BindCurrentSourceRegistration(s CurrentSource, lookup func(context.Context) (SourceRegistrationBinding, error)) (*OwnedSourceRegistration, error) {
	if lookup == nil {
		return nil, fmt.Errorf("owning registered-source lookup is unsupplied")
	}
	ctx, cancel := context.WithTimeout(context.Background(), SourceRegistrationLookupLimit)
	defer cancel()
	binding, err := lookup(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateSourceRegistration(s, binding); err != nil {
		return nil, err
	}
	owned := &OwnedSourceRegistration{binding: binding, lookup: lookup, paths: map[string]os.FileInfo{}}
	for _, path := range []string{binding.Worktree, binding.GitDirectory, binding.CommonDirectory} {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("registered-source directory identity is unavailable")
		}
		owned.paths[path] = info
	}
	return owned, nil
}

func (registration *OwnedSourceRegistration) validate(parent context.Context, s CurrentSource, common, gitDirectory string) error {
	if err := validateSourceRegistration(s, registration.binding); err != nil {
		return err
	}
	if common != registration.binding.CommonDirectory || gitDirectory != registration.binding.GitDirectory {
		return fmt.Errorf("registered-source Git metadata differs from its owning binding")
	}
	for path, previous := range registration.paths {
		current, err := os.Stat(path)
		if err != nil || !os.SameFile(previous, current) {
			return fmt.Errorf("registered-source root or metadata identity changed")
		}
	}
	ctx, cancel := context.WithTimeout(parent, SourceRegistrationLookupLimit)
	defer cancel()
	current, err := registration.lookup(ctx)
	if err != nil {
		return err
	}
	if current != registration.binding {
		return fmt.Errorf("registered-source registration changed; preparation must be reevaluated")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("registered-source lookup exceeded the enclosing operation: %w", err)
	}
	return nil
}

func (registration *OwnedSourceRegistration) Receipt() map[string]string {
	return map[string]string{"workspaceId": registration.binding.WorkspaceID, "registrationSha256": registration.binding.RegistrationSHA256}
}

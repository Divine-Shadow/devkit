package managementinspect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/gitauthority"
)

const (
	manifestSchema  = "devkit.management-inspection.manifest.v1"
	identitySchema  = "devkit.management-inspection.identity.v1"
	defaultProfile  = "product"
	workspaceSource = "/workspace/source"
	workspaceState  = "/workspace/state"
	identityRoot    = "/inspection"
)

var profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Identity is the immutable, Nix-store-backed source identity exposed inside
// the inspection profile.
type Identity struct {
	Schema            string `json:"schema"`
	Profile           string `json:"profile"`
	RepositoryPath    string `json:"repositoryPath"`
	RequestedRevision string `json:"requestedRevision"`
	ResolvedRevision  string `json:"resolvedRevision"`
	SourceStorePath   string `json:"sourceStorePath"`
	SnapshotMethod    string `json:"snapshotMethod"`
}

// Manifest is the writable control-plane pointer to one immutable source
// generation and its explicitly separate runtime state.
type Manifest struct {
	Schema            string   `json:"schema"`
	Identity          Identity `json:"identity"`
	IdentityStorePath string   `json:"identityStorePath"`
	RuntimeStatePath  string   `json:"runtimeStatePath"`
	StateRoot         string   `json:"stateRoot"`
	RefreshedAt       string   `json:"refreshedAt"`
}

// Readback is the stable operator-facing identity and isolation report.
type Readback struct {
	Schema               string `json:"schema"`
	Profile              string `json:"profile"`
	RepositoryPath       string `json:"repositoryPath"`
	RequestedRevision    string `json:"requestedRevision"`
	ResolvedRevision     string `json:"resolvedRevision"`
	SourceStorePath      string `json:"sourceStorePath"`
	IdentityPath         string `json:"identityPath"`
	RuntimeStatePath     string `json:"runtimeStatePath"`
	StateRoot            string `json:"stateRoot"`
	SnapshotMethod       string `json:"snapshotMethod"`
	SourceReadOnly       bool   `json:"sourceReadOnly"`
	RuntimeStateWritable bool   `json:"runtimeStateWritable"`
	CodexHomeCopied      bool   `json:"codexHomeCopied"`
	RefreshedAt          string `json:"refreshedAt"`
}

type commonOptions struct {
	Name      string
	StateRoot string
	Format    string
}

type refreshOptions struct {
	commonOptions
	Repository string
	Revision   string
}

// Register adds management-inspect to the compiled command registry.
func Register(r *cmdregistry.Registry) {
	r.Register("management-inspect", handle)
}

func handle(ctx *cmdregistry.Context) error {
	if len(ctx.Args) == 0 {
		return usageError()
	}
	action := ctx.Args[0]
	args := ctx.Args[1:]
	switch action {
	case "refresh":
		opts, err := parseRefreshOptions(args)
		if err != nil {
			return err
		}
		manifest, err := Refresh(context.Background(), opts)
		if err != nil {
			return err
		}
		return printReadback(os.Stdout, BuildReadback(manifest), opts.Format)
	case "status", "readback":
		opts, rest, err := parseCommonOptions(args)
		if err != nil {
			return err
		}
		if len(rest) != 0 {
			return fmt.Errorf("unknown management-inspect %s argument %q", action, rest[0])
		}
		manifest, err := LoadManifest(opts.StateRoot, opts.Name)
		if err != nil {
			return err
		}
		if err := ValidateManifest(manifest); err != nil {
			return err
		}
		return printReadback(os.Stdout, BuildReadback(manifest), opts.Format)
	case "exec", "shell":
		opts, command, err := parseCommonOptions(args)
		if err != nil {
			return err
		}
		if action == "shell" && len(command) != 0 {
			return fmt.Errorf("management-inspect shell does not accept a command; use exec -- COMMAND")
		}
		if len(command) > 0 && command[0] == "--" {
			command = command[1:]
		}
		if len(command) == 0 {
			command = []string{"bash"}
		}
		manifest, err := LoadManifest(opts.StateRoot, opts.Name)
		if err != nil {
			return err
		}
		if err := ValidateManifest(manifest); err != nil {
			return err
		}
		readback := BuildReadback(manifest)
		fmt.Fprintf(os.Stderr, "management-inspect: profile=%s revision=%s source=%s state=%s\n",
			readback.Profile, readback.ResolvedRevision, readback.SourceStorePath, readback.RuntimeStatePath)
		return Execute(context.Background(), manifest, command, os.Stdin, os.Stdout, os.Stderr)
	default:
		return fmt.Errorf("unknown management-inspect action %q; expected refresh, status, readback, shell, or exec", action)
	}
}

func usageError() error {
	return errors.New("usage: management-inspect refresh --repo PATH --revision REF [--name NAME] [--state-root PATH] [--format text|json]\n" +
		"       management-inspect status [--name NAME] [--state-root PATH] [--format text|json]\n" +
		"       management-inspect shell [--name NAME] [--state-root PATH]\n" +
		"       management-inspect exec [--name NAME] [--state-root PATH] -- COMMAND...")
}

func parseRefreshOptions(args []string) (refreshOptions, error) {
	common, rest, err := parseCommonOptions(args)
	if err != nil {
		return refreshOptions{}, err
	}
	opts := refreshOptions{commonOptions: common}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--repo", "--repository":
			value, err := nextArg(rest, &i, rest[i])
			if err != nil {
				return refreshOptions{}, err
			}
			opts.Repository = value
		case "--revision", "--rev":
			value, err := nextArg(rest, &i, rest[i])
			if err != nil {
				return refreshOptions{}, err
			}
			opts.Revision = value
		default:
			return refreshOptions{}, fmt.Errorf("unknown management-inspect refresh argument %q", rest[i])
		}
	}
	if strings.TrimSpace(opts.Repository) == "" {
		return refreshOptions{}, errors.New("management-inspect refresh requires --repo PATH")
	}
	if strings.TrimSpace(opts.Revision) == "" {
		return refreshOptions{}, errors.New("management-inspect refresh requires --revision REF; branch tips are never followed implicitly")
	}
	return opts, nil
}

func parseCommonOptions(args []string) (commonOptions, []string, error) {
	opts := commonOptions{Name: defaultProfile, StateRoot: defaultStateRoot(), Format: "text"}
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--":
			rest = append(rest, args[i:]...)
			i = len(args)
		case "--name":
			value, err := nextArg(args, &i, "--name")
			if err != nil {
				return commonOptions{}, nil, err
			}
			opts.Name = value
		case "--state-root":
			value, err := nextArg(args, &i, "--state-root")
			if err != nil {
				return commonOptions{}, nil, err
			}
			opts.StateRoot = value
		case "--format":
			value, err := nextArg(args, &i, "--format")
			if err != nil {
				return commonOptions{}, nil, err
			}
			opts.Format = value
		default:
			rest = append(rest, args[i])
		}
	}
	opts.Name = strings.TrimSpace(opts.Name)
	if !profileNamePattern.MatchString(opts.Name) {
		return commonOptions{}, nil, fmt.Errorf("invalid management inspection profile name %q", opts.Name)
	}
	absState, err := filepath.Abs(expandHome(strings.TrimSpace(opts.StateRoot)))
	if err != nil {
		return commonOptions{}, nil, fmt.Errorf("resolve management inspection state root: %w", err)
	}
	opts.StateRoot = filepath.Clean(absState)
	if opts.Format != "text" && opts.Format != "json" {
		return commonOptions{}, nil, fmt.Errorf("invalid --format %q; expected text or json", opts.Format)
	}
	return opts, rest, nil
}

func nextArg(args []string, index *int, flag string) (string, error) {
	if *index+1 >= len(args) {
		return "", fmt.Errorf("%s requires a value", flag)
	}
	(*index)++
	value := strings.TrimSpace(args[*index])
	if value == "" {
		return "", fmt.Errorf("%s requires a non-empty value", flag)
	}
	return value, nil
}

func defaultStateRoot() string {
	if value := strings.TrimSpace(os.Getenv("DEVKIT_MANAGEMENT_INSPECTION_STATE_ROOT")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); value != "" {
		return filepath.Join(value, "devkit", "management-inspection")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".", ".devkit-management-inspection-state")
	}
	return filepath.Join(home, ".local", "state", "devkit", "management-inspection")
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

// Refresh resolves an explicit revision, exports only the committed tree into
// the Nix store, creates a new per-revision runtime generation, and atomically
// advances the named profile manifest. It never reads source files from the
// repository worktree.
func Refresh(ctx context.Context, opts refreshOptions) (Manifest, error) {
	repo, err := canonicalRepository(ctx, opts.Repository)
	if err != nil {
		return Manifest{}, err
	}
	resolved, err := gitOutput(ctx, repo, "rev-parse", "--verify", opts.Revision+"^{commit}")
	if err != nil {
		return Manifest{}, fmt.Errorf("resolve requested revision %q: %w", opts.Revision, err)
	}
	resolved = strings.TrimSpace(resolved)
	if len(resolved) != 40 {
		return Manifest{}, fmt.Errorf("resolved revision %q is not a full commit identity", resolved)
	}

	if err := os.MkdirAll(opts.StateRoot, 0o700); err != nil {
		return Manifest{}, fmt.Errorf("create state root %s: %w", opts.StateRoot, err)
	}
	canonicalStateRoot, err := filepath.EvalSymlinks(opts.StateRoot)
	if err != nil {
		return Manifest{}, fmt.Errorf("canonicalize state root %s: %w", opts.StateRoot, err)
	}
	opts.StateRoot = filepath.Clean(canonicalStateRoot)
	tmp, err := os.MkdirTemp(opts.StateRoot, ".refresh-")
	if err != nil {
		return Manifest{}, fmt.Errorf("create refresh workspace: %w", err)
	}
	defer os.RemoveAll(tmp)

	snapshot := filepath.Join(tmp, "source")
	if err := os.Mkdir(snapshot, 0o700); err != nil {
		return Manifest{}, err
	}
	if err := archiveCommit(ctx, repo, resolved, snapshot); err != nil {
		return Manifest{}, err
	}
	if _, err := os.Lstat(filepath.Join(snapshot, ".git")); !os.IsNotExist(err) {
		return Manifest{}, errors.New("source snapshot unexpectedly contains Git metadata")
	}
	sourceStorePath, err := addPathToNixStore(ctx, snapshot)
	if err != nil {
		return Manifest{}, err
	}

	identity := Identity{
		Schema:            identitySchema,
		Profile:           opts.Name,
		RepositoryPath:    repo,
		RequestedRevision: opts.Revision,
		ResolvedRevision:  resolved,
		SourceStorePath:   sourceStorePath,
		SnapshotMethod:    "git-archive+nix-store",
	}
	identityDir := filepath.Join(tmp, "identity")
	if err := os.Mkdir(identityDir, 0o700); err != nil {
		return Manifest{}, err
	}
	identityBytes, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	identityBytes = append(identityBytes, '\n')
	if err := os.WriteFile(filepath.Join(identityDir, "identity.json"), identityBytes, 0o600); err != nil {
		return Manifest{}, fmt.Errorf("write immutable identity: %w", err)
	}
	identityStorePath, err := addPathToNixStore(ctx, identityDir)
	if err != nil {
		return Manifest{}, err
	}

	profileDir := filepath.Join(opts.StateRoot, "profiles", opts.Name)
	runtimeStatePath := filepath.Join(profileDir, "runtime", resolved)
	for _, dir := range []string{
		runtimeStatePath,
		filepath.Join(runtimeStatePath, "home"),
		filepath.Join(runtimeStatePath, "cache"),
		filepath.Join(runtimeStatePath, "config"),
		filepath.Join(runtimeStatePath, "data"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Manifest{}, fmt.Errorf("create runtime state %s: %w", dir, err)
		}
	}
	gcRoots := filepath.Join(profileDir, "gcroots")
	if err := os.MkdirAll(gcRoots, 0o700); err != nil {
		return Manifest{}, err
	}
	if err := ensureGCRoot(ctx, filepath.Join(gcRoots, "source-"+resolved), sourceStorePath); err != nil {
		return Manifest{}, err
	}
	if err := ensureGCRoot(ctx, filepath.Join(gcRoots, "identity-"+resolved), identityStorePath); err != nil {
		return Manifest{}, err
	}

	manifest := Manifest{
		Schema:            manifestSchema,
		Identity:          identity,
		IdentityStorePath: identityStorePath,
		RuntimeStatePath:  runtimeStatePath,
		StateRoot:         opts.StateRoot,
		RefreshedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	if err := writeManifestAtomic(profileDir, manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func canonicalRepository(ctx context.Context, requested string) (string, error) {
	abs, err := filepath.Abs(expandHome(strings.TrimSpace(requested)))
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	root, err := gitOutput(ctx, abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not a readable Git repository: %w", abs, err)
	}
	root = strings.TrimSpace(root)
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("canonicalize repository root %s: %w", root, err)
	}
	return filepath.Clean(canonical), nil
}

func archiveCommit(ctx context.Context, repo, revision, destination string) error {
	archive := exec.CommandContext(ctx, gitauthority.Executable(), "-C", repo, "archive", "--format=tar", revision)
	tar := exec.CommandContext(ctx, "tar", "-x", "-C", destination, "--no-same-owner", "--no-same-permissions")
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	tar.Stdin = pipe
	var archiveErr bytes.Buffer
	var tarErr bytes.Buffer
	archive.Stderr = &archiveErr
	tar.Stderr = &tarErr
	if err := archive.Start(); err != nil {
		return fmt.Errorf("start git archive: %w", err)
	}
	if err := tar.Run(); err != nil {
		_ = archive.Process.Kill()
		_ = archive.Wait()
		return fmt.Errorf("extract committed source archive: %w: %s", err, strings.TrimSpace(tarErr.String()))
	}
	if err := archive.Wait(); err != nil {
		return fmt.Errorf("export committed source revision: %w: %s", err, strings.TrimSpace(archiveErr.String()))
	}
	return nil
}

func addPathToNixStore(ctx context.Context, path string) (string, error) {
	out, err := commandOutput(ctx, "nix-store", "--add", path)
	if err != nil {
		return "", fmt.Errorf("add source-derived path to Nix store: %w", err)
	}
	storePath := filepath.Clean(strings.TrimSpace(out))
	if !isNixStorePath(storePath) {
		return "", fmt.Errorf("nix-store returned non-store path %q", storePath)
	}
	info, err := os.Stat(storePath)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("Nix store path is not a readable directory: %s", storePath)
	}
	return storePath, nil
}

func ensureGCRoot(ctx context.Context, root, storePath string) error {
	if target, err := os.Readlink(root); err == nil {
		if filepath.Clean(target) == filepath.Clean(storePath) {
			return nil
		}
		return fmt.Errorf("managed GC root collision at %s: points to %s, expected %s", root, target, storePath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect managed GC root %s: %w", root, err)
	}
	if _, err := commandOutput(ctx, "nix-store", "--add-root", root, "-r", storePath); err != nil {
		return fmt.Errorf("root immutable inspection generation %s: %w", storePath, err)
	}
	return nil
}

func gitOutput(ctx context.Context, repo string, args ...string) (string, error) {
	all := append([]string{"-C", repo}, args...)
	return commandOutput(ctx, gitauthority.Executable(), all...)
}

func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", errors.New(message)
	}
	return stdout.String(), nil
}

func writeManifestAtomic(profileDir string, manifest Manifest) error {
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return fmt.Errorf("create profile directory: %w", err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(profileDir, ".current-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(profileDir, "current.json")); err != nil {
		return fmt.Errorf("publish profile manifest: %w", err)
	}
	return nil
}

// LoadManifest reads the current explicit profile generation.
func LoadManifest(stateRoot, name string) (Manifest, error) {
	path := filepath.Join(stateRoot, "profiles", name, "current.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{}, fmt.Errorf("management inspection profile %q is not initialized; run refresh explicitly", name)
		}
		return Manifest{}, fmt.Errorf("read management inspection manifest %s: %w", path, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode management inspection manifest %s: %w", path, err)
	}
	return manifest, nil
}

// ValidateManifest checks source/store identity and state separation before a
// profile is entered.
func ValidateManifest(manifest Manifest) error {
	if manifest.Schema != manifestSchema {
		return fmt.Errorf("unsupported management inspection manifest schema %q", manifest.Schema)
	}
	if manifest.Identity.Schema != identitySchema {
		return fmt.Errorf("unsupported management inspection identity schema %q", manifest.Identity.Schema)
	}
	if !profileNamePattern.MatchString(manifest.Identity.Profile) {
		return fmt.Errorf("invalid manifest profile %q", manifest.Identity.Profile)
	}
	if len(manifest.Identity.ResolvedRevision) != 40 {
		return fmt.Errorf("manifest revision %q is not a full commit identity", manifest.Identity.ResolvedRevision)
	}
	if !isNixStorePath(manifest.Identity.SourceStorePath) {
		return fmt.Errorf("inspection source is not Nix-store-backed: %s", manifest.Identity.SourceStorePath)
	}
	if !isNixStorePath(manifest.IdentityStorePath) {
		return fmt.Errorf("inspection identity is not Nix-store-backed: %s", manifest.IdentityStorePath)
	}
	for _, path := range []string{manifest.Identity.SourceStorePath, manifest.IdentityStorePath, manifest.RuntimeStatePath, manifest.StateRoot} {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("inspection path is not absolute: %s", path)
		}
	}
	if pathWithin(manifest.Identity.SourceStorePath, manifest.StateRoot) || pathWithin(manifest.RuntimeStatePath, manifest.Identity.SourceStorePath) {
		return errors.New("writable runtime state and immutable inspection source are not separate")
	}
	if err := validateStateLayout(manifest); err != nil {
		return err
	}
	for label, path := range map[string]string{
		"source":   manifest.Identity.SourceStorePath,
		"identity": manifest.IdentityStorePath,
		"state":    manifest.RuntimeStatePath,
	} {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("inspection %s path is not a readable directory: %s", label, path)
		}
	}
	identityBytes, err := os.ReadFile(filepath.Join(manifest.IdentityStorePath, "identity.json"))
	if err != nil {
		return fmt.Errorf("read immutable inspection identity: %w", err)
	}
	var immutable Identity
	if err := json.Unmarshal(identityBytes, &immutable); err != nil {
		return fmt.Errorf("decode immutable inspection identity: %w", err)
	}
	if immutable != manifest.Identity {
		return errors.New("writable manifest identity does not match immutable Nix-store identity")
	}
	return nil
}

func validateStateLayout(manifest Manifest) error {
	expected := filepath.Join(
		filepath.Clean(manifest.StateRoot),
		"profiles",
		manifest.Identity.Profile,
		"runtime",
		manifest.Identity.ResolvedRevision,
	)
	if filepath.Clean(manifest.RuntimeStatePath) != expected {
		return fmt.Errorf("runtime state %s does not match revision-specific generation %s", manifest.RuntimeStatePath, expected)
	}
	canonicalRoot, err := filepath.EvalSymlinks(manifest.StateRoot)
	if err != nil {
		return fmt.Errorf("canonicalize explicit state root %s: %w", manifest.StateRoot, err)
	}
	canonicalRuntime, err := filepath.EvalSymlinks(manifest.RuntimeStatePath)
	if err != nil {
		return fmt.Errorf("canonicalize runtime state %s: %w", manifest.RuntimeStatePath, err)
	}
	if !pathWithin(canonicalRuntime, canonicalRoot) {
		return fmt.Errorf("runtime state %s escapes explicit state root %s through a symlink", manifest.RuntimeStatePath, manifest.StateRoot)
	}
	return nil
}

func isNixStorePath(path string) bool {
	clean := filepath.Clean(path)
	return filepath.IsAbs(clean) && strings.HasPrefix(clean, "/nix/store/")
}

func pathWithin(path, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// BuildReadback renders the invariant-bearing identity shown by refresh and
// status.
func BuildReadback(manifest Manifest) Readback {
	return Readback{
		Schema:               "devkit.management-inspection.readback.v1",
		Profile:              manifest.Identity.Profile,
		RepositoryPath:       manifest.Identity.RepositoryPath,
		RequestedRevision:    manifest.Identity.RequestedRevision,
		ResolvedRevision:     manifest.Identity.ResolvedRevision,
		SourceStorePath:      manifest.Identity.SourceStorePath,
		IdentityPath:         filepath.Join(manifest.IdentityStorePath, "identity.json"),
		RuntimeStatePath:     manifest.RuntimeStatePath,
		StateRoot:            manifest.StateRoot,
		SnapshotMethod:       manifest.Identity.SnapshotMethod,
		SourceReadOnly:       true,
		RuntimeStateWritable: true,
		CodexHomeCopied:      false,
		RefreshedAt:          manifest.RefreshedAt,
	}
}

func printReadback(w io.Writer, readback Readback, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(readback)
	}
	fmt.Fprintf(w, "profile: %s\n", readback.Profile)
	fmt.Fprintf(w, "repository: %s\n", readback.RepositoryPath)
	fmt.Fprintf(w, "requested revision: %s\n", readback.RequestedRevision)
	fmt.Fprintf(w, "resolved revision: %s\n", readback.ResolvedRevision)
	fmt.Fprintf(w, "source: %s (read-only)\n", readback.SourceStorePath)
	fmt.Fprintf(w, "identity: %s\n", readback.IdentityPath)
	fmt.Fprintf(w, "runtime state: %s (writable, revision-specific)\n", readback.RuntimeStatePath)
	fmt.Fprintf(w, "snapshot method: %s\n", readback.SnapshotMethod)
	fmt.Fprintf(w, "Codex home copied: %t\n", readback.CodexHomeCopied)
	return nil
}

// Execute enters the constrained inspection filesystem. Only the Nix store,
// immutable source/identity, proc/dev/tmp, and this revision's explicit state
// generation are visible.
func Execute(ctx context.Context, manifest Manifest, command []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(command) == 0 {
		return errors.New("inspection command is required")
	}
	resolvedCommand, err := exec.LookPath(command[0])
	if err != nil {
		return fmt.Errorf("inspection command %q not found in the Nix profile", command[0])
	}
	resolvedCommand, err = filepath.EvalSymlinks(resolvedCommand)
	if err != nil {
		return fmt.Errorf("resolve inspection command %q: %w", command[0], err)
	}
	if !isNixStorePath(resolvedCommand) {
		return fmt.Errorf("inspection command is not source-derived from the Nix profile: %s", resolvedCommand)
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return errors.New("bubblewrap is required for the Management read-only inspection profile")
	}
	args := SandboxArgs(manifest, resolvedCommand, command[1:], os.Getenv("PATH"))
	cmd := exec.CommandContext(ctx, bwrap, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("management inspection command failed: %w", err)
	}
	return nil
}

// SandboxArgs returns the filesystem and environment contract used by Execute.
func SandboxArgs(manifest Manifest, command string, commandArgs []string, hostPath string) []string {
	paths := nixStorePathEntries(hostPath)
	args := []string{
		"--die-with-parent",
		"--new-session",
		"--unshare-all",
		"--clearenv",
		"--ro-bind", "/nix/store", "/nix/store",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--dir", "/workspace",
		"--ro-bind", manifest.Identity.SourceStorePath, workspaceSource,
		"--bind", manifest.RuntimeStatePath, workspaceState,
		"--ro-bind", manifest.IdentityStorePath, identityRoot,
		"--chdir", workspaceSource,
		"--setenv", "HOME", workspaceState + "/home",
		"--setenv", "XDG_CACHE_HOME", workspaceState + "/cache",
		"--setenv", "XDG_CONFIG_HOME", workspaceState + "/config",
		"--setenv", "XDG_DATA_HOME", workspaceState + "/data",
		"--setenv", "PATH", strings.Join(paths, string(os.PathListSeparator)),
		"--setenv", "LANG", "C.UTF-8",
		"--setenv", "TERM", "xterm-256color",
		"--setenv", "USER", "management-inspection",
		"--setenv", "DEVKIT_INSPECTION_SOURCE", workspaceSource,
		"--setenv", "DEVKIT_INSPECTION_STATE", workspaceState,
		"--setenv", "DEVKIT_INSPECTION_IDENTITY", identityRoot + "/identity.json",
		"--setenv", "DEVKIT_INSPECTION_REVISION", manifest.Identity.ResolvedRevision,
		"--unsetenv", "CODEX_HOME",
		"--unsetenv", "CODEX_ROLLOUT_DIR",
		command,
	}
	return append(args, commandArgs...)
}

func nixStorePathEntries(path string) []string {
	seen := map[string]bool{}
	var entries []string
	for _, entry := range filepath.SplitList(path) {
		resolved, err := filepath.EvalSymlinks(entry)
		if err != nil || !isNixStorePath(resolved) || seen[resolved] {
			continue
		}
		seen[resolved] = true
		entries = append(entries, resolved)
	}
	return entries
}

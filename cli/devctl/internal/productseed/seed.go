package productseed

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"devkit/cli/devctl/internal/productadapter"
)

const (
	CodexAuthSeedSchema        = "devkit/product-codex-auth-seed/v1"
	CodexAuthSeedFailureSchema = "devkit/product-codex-auth-seed-failure/v1"
	maxCodexAuthBytes          = 1 << 20
)

type Result struct {
	SchemaVersion      string `json:"schema_version"`
	Status             string `json:"status"`
	ConsumerIndex      int    `json:"consumer_index"`
	RelativeProjection string `json:"relative_projection"`
}

type CodexAuthResult struct {
	SchemaVersion string `json:"schema_version"`
	Status        string `json:"status"`
	ConsumerIndex int    `json:"consumer_index"`
}

type CodexAuthInstallOutcome string

const (
	CodexAuthInstallAttempted CodexAuthInstallOutcome = "attempted"
	CodexAuthInstallEffect    CodexAuthInstallOutcome = "effect"
	CodexAuthInstallAmbiguous CodexAuthInstallOutcome = "ambiguous"
)

// CodexAuthInstallFailure is the typed process-boundary result for a failed
// install after the fixed-slot effect path begins. "attempted" means no target
// from this invocation remains, "effect" means the exact intended target is
// present, and "ambiguous" means the consumer must be discarded rather than
// repaired in place.
type CodexAuthInstallFailure struct {
	SchemaVersion  string                  `json:"schema_version"`
	Status         string                  `json:"status"`
	ConsumerIndex  int                     `json:"consumer_index"`
	Outcome        CodexAuthInstallOutcome `json:"outcome"`
	Phase          string                  `json:"phase"`
	Reconciliation string                  `json:"reconciliation"`
	Message        string                  `json:"message"`
	cause          error
}

func (failure *CodexAuthInstallFailure) Error() string {
	if failure == nil {
		return "Codex auth install failure"
	}
	return fmt.Sprintf(
		"Codex auth install %s during %s (%s): %s",
		failure.Outcome,
		failure.Phase,
		failure.Reconciliation,
		failure.Message,
	)
}

func (failure *CodexAuthInstallFailure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func Seed(authority productadapter.Authority, consumer productadapter.ConsumerManifest, count, index int, projection string) (Result, error) {
	if count != authority.Adapter.Count || index != consumer.Index {
		return Result{}, fmt.Errorf("offline Product seed identity does not match authority")
	}
	if !safeRelativeProjection(projection) {
		return Result{}, fmt.Errorf("offline Product seed requires one safe relative projection component")
	}
	projectionRoot, err := openProjectionRoot(projection)
	if err != nil {
		return Result{}, err
	}
	defer projectionRoot.Close()
	identityPath := os.Getenv(productadapter.SourceIdentityEnvironment)
	identityFile, identity, err := readProtectedIdentity(identityPath, authority.Adapter.ControllerCredentialOwnerUID)
	if err != nil {
		return Result{}, err
	}
	defer identityFile.Close()
	public, err := derivePublicKey(authority.Adapter.SSHKeygenPath, identityFile)
	if err != nil {
		return Result{}, err
	}
	authorizedKeys, err := productadapter.ReadImmutableAuthorizedKeys(
		consumer.AuthorizedKeysPath,
		authority.Adapter.SSHKeygenPath,
	)
	if err != nil {
		return Result{}, err
	}
	guiAuthorizedKey := bytes.TrimPrefix(bytes.TrimSpace(authorizedKeys), []byte("restrict "))
	if bytes.Equal(bytes.TrimSpace(public), guiAuthorizedKey) {
		return Result{}, fmt.Errorf("Product Git and GUI SSH credentials must be distinct")
	}
	knownHosts, err := os.ReadFile(authority.Adapter.KnownHostsPath)
	if err != nil {
		return Result{}, fmt.Errorf("read package known-hosts: %w", err)
	}
	sshConfig, err := productadapter.ProductSSHConfig(authority, consumer, count, index)
	if err != nil {
		return Result{}, err
	}
	targets := []struct {
		guest string
		mode  os.FileMode
		data  []byte
	}{
		{consumer.SSHIdentityPath, 0o600, identity},
		{consumer.SSHPublicKeyPath, 0o600, public},
		{filepath.Join(consumer.HomePath, ".ssh", "known_hosts"), 0o600, knownHosts},
		{filepath.Join(consumer.HomePath, ".ssh", "config"), 0o600, sshConfig},
	}
	for _, target := range targets {
		projected, err := projectGuestRelativePath(consumer.CandidateRoot, target.guest)
		if err != nil {
			return Result{}, err
		}
		if err := writeOwnedAt(projectionRoot, projected, target.data, target.mode, consumer.UID, consumer.GID); err != nil {
			return Result{}, err
		}
	}
	markerPayload, err := json.MarshalIndent(productadapter.OfflineSeedMarker{
		SchemaVersion:        productadapter.OfflineSeedSchema,
		ConsumerIndex:        index,
		CandidateRoot:        consumer.CandidateRoot,
		AuthorizedKeysSHA256: fmt.Sprintf("%x", sha256.Sum256(authorizedKeys)),
	}, "", "  ")
	if err != nil {
		return Result{}, err
	}
	markerPayload = append(markerPayload, '\n')
	markerPath, err := projectGuestRelativePath(
		consumer.CandidateRoot,
		productadapter.OfflineSeedMarkerPath(consumer),
	)
	if err != nil {
		return Result{}, err
	}
	if err := writeOwnedAt(projectionRoot, markerPath, markerPayload, 0o600, consumer.UID, consumer.GID); err != nil {
		return Result{}, err
	}
	codexAuthPath, err := projectGuestRelativePath(
		consumer.CandidateRoot,
		consumer.CodexAuthPath,
	)
	if err != nil {
		return Result{}, err
	}
	codexParentFD, _, err := openRelativeParentAt(
		projectionRoot,
		codexAuthPath,
		consumer.UID,
		consumer.GID,
		true,
	)
	if err != nil {
		return Result{}, fmt.Errorf("create fixed-slot Codex home: %w", err)
	}
	if err := syscall.Close(codexParentFD); err != nil {
		return Result{}, fmt.Errorf("close fixed-slot Codex home: %w", err)
	}
	if err := kernelFchown(int(projectionRoot.Fd()), consumer.UID, consumer.GID); err != nil {
		return Result{}, fmt.Errorf("assign offline Product projection root: %w", err)
	}
	if err := syscall.Fchmod(int(projectionRoot.Fd()), 0o700); err != nil {
		return Result{}, err
	}
	return Result{
		SchemaVersion:      productadapter.OfflineSeedSchema,
		Status:             "seeded",
		ConsumerIndex:      index,
		RelativeProjection: projection,
	}, nil
}

// SeedCodexAuth installs one opaque Codex authorization document into the
// manifest-selected fixed consumer slot. The caller supplies only bytes on
// stdin; path, ownership, mode, manifest, and slot identity all come from the
// already-validated authority.
func SeedCodexAuth(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
	fixedIndex int,
	input io.Reader,
) (CodexAuthResult, error) {
	if fixedIndex != consumer.Index || fixedIndex < 1 ||
		fixedIndex > authority.Adapter.Count {
		return CodexAuthResult{}, fmt.Errorf("Codex auth seed fixed slot does not match authority")
	}
	candidateRoot, err := openExactOwnedDirectory(
		consumer.CandidateRoot,
		consumer.UID,
		consumer.GID,
		0o700,
	)
	if err != nil {
		return CodexAuthResult{}, fmt.Errorf("open fixed Product consumer root: %w", err)
	}
	defer candidateRoot.Close()
	if err := validateOfflineSeedAt(candidateRoot, authority, consumer); err != nil {
		return CodexAuthResult{}, err
	}
	payload, err := readCodexAuth(input)
	if err != nil {
		return CodexAuthResult{}, err
	}
	defer wipe(payload)
	target, err := projectGuestRelativePath(consumer.CandidateRoot, consumer.CodexAuthPath)
	if err != nil {
		return CodexAuthResult{}, err
	}
	if err := writeOwnedAtomicAt(
		candidateRoot,
		target,
		payload,
		0o600,
		consumer.UID,
		consumer.GID,
	); err != nil {
		var failure *CodexAuthInstallFailure
		if errors.As(err, &failure) {
			failure.ConsumerIndex = fixedIndex
			return CodexAuthResult{}, failure
		}
		return CodexAuthResult{}, fmt.Errorf("install fixed-slot Codex auth: %w", err)
	}
	return CodexAuthResult{
		SchemaVersion: CodexAuthSeedSchema,
		Status:        "seeded",
		ConsumerIndex: fixedIndex,
	}, nil
}

func readCodexAuth(input io.Reader) ([]byte, error) {
	if input == nil {
		return nil, fmt.Errorf("Codex auth seed requires stdin")
	}
	payload, err := io.ReadAll(io.LimitReader(input, maxCodexAuthBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Codex auth seed stdin: %w", err)
	}
	if len(payload) == 0 || len(payload) > maxCodexAuthBytes ||
		len(bytes.TrimSpace(payload)) == 0 || !json.Valid(payload) {
		wipe(payload)
		return nil, fmt.Errorf("Codex auth seed stdin is not one bounded JSON document")
	}
	return payload, nil
}

func wipe(payload []byte) {
	for index := range payload {
		payload[index] = 0
	}
}

func openExactOwnedDirectory(path string, uid, gid int, mode os.FileMode) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, fmt.Errorf("directory path is not exact and absolute")
	}
	rootFD, err := syscall.Open(
		"/",
		syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,
		0,
	)
	if err != nil {
		return nil, err
	}
	currentFD := rootFD
	for _, component := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		nextFD, openErr := syscall.Openat(
			currentFD,
			component,
			syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,
			0,
		)
		_ = syscall.Close(currentFD)
		if openErr != nil {
			return nil, openErr
		}
		currentFD = nextFD
	}
	if err := validateOwnedDirectoryFD(currentFD, uid, gid, mode); err != nil {
		_ = syscall.Close(currentFD)
		return nil, err
	}
	file := os.NewFile(uintptr(currentFD), path)
	if file == nil {
		_ = syscall.Close(currentFD)
		return nil, fmt.Errorf("hold exact directory")
	}
	return file, nil
}

func validateOfflineSeedAt(
	root *os.File,
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
) error {
	markerPath, err := projectGuestRelativePath(
		consumer.CandidateRoot,
		productadapter.OfflineSeedMarkerPath(consumer),
	)
	if err != nil {
		return err
	}
	markerPayload, err := readOwnedPlainAt(root, markerPath, 0o600, consumer.UID, consumer.GID, 1<<16)
	if err != nil {
		return fmt.Errorf("validate fixed-slot offline seed marker: %w", err)
	}
	var marker productadapter.OfflineSeedMarker
	decoder := json.NewDecoder(bytes.NewReader(markerPayload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil {
		return fmt.Errorf("decode fixed-slot offline seed marker: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("fixed-slot offline seed marker has trailing content")
	}
	authorizedKeys, err := productadapter.ReadImmutableAuthorizedKeys(
		consumer.AuthorizedKeysPath,
		authority.Adapter.SSHKeygenPath,
	)
	if err != nil {
		return err
	}
	if marker.SchemaVersion != productadapter.OfflineSeedSchema ||
		marker.ConsumerIndex != consumer.Index ||
		marker.CandidateRoot != consumer.CandidateRoot ||
		marker.AuthorizedKeysSHA256 != fmt.Sprintf("%x", sha256.Sum256(authorizedKeys)) {
		return fmt.Errorf("fixed-slot offline seed marker does not match authority")
	}
	for label, path := range map[string]string{
		"SSH private identity": consumer.SSHIdentityPath,
		"SSH public identity":  consumer.SSHPublicKeyPath,
		"SSH known-hosts":      filepath.Join(consumer.HomePath, ".ssh", "known_hosts"),
		"SSH config":           filepath.Join(consumer.HomePath, ".ssh", "config"),
	} {
		relative, err := projectGuestRelativePath(consumer.CandidateRoot, path)
		if err != nil {
			return err
		}
		if err := validateOwnedPlainAt(root, relative, 0o600, consumer.UID, consumer.GID); err != nil {
			return fmt.Errorf("validate fixed-slot %s: %w", label, err)
		}
	}
	return nil
}

func readOwnedPlainAt(
	root *os.File,
	relative string,
	mode os.FileMode,
	uid, gid int,
	limit int64,
) ([]byte, error) {
	file, err := openOwnedPlainAt(root, relative, mode, uid, gid)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("fixed-slot file exceeds bound")
	}
	return payload, nil
}

func validateOwnedPlainAt(root *os.File, relative string, mode os.FileMode, uid, gid int) error {
	file, err := openOwnedPlainAt(root, relative, mode, uid, gid)
	if err != nil {
		return err
	}
	return file.Close()
}

func openOwnedPlainAt(
	root *os.File,
	relative string,
	mode os.FileMode,
	uid, gid int,
) (*os.File, error) {
	parentFD, leaf, err := openRelativeParentAt(root, relative, uid, gid, false)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(parentFD)
	fd, err := syscall.Openat(
		parentFD,
		leaf,
		syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,
		0,
	)
	if err != nil {
		return nil, err
	}
	if err := validateOwnedPlainFD(fd, uid, gid, mode); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	file := os.NewFile(uintptr(fd), leaf)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("hold fixed-slot file")
	}
	return file, nil
}

func safeRelativeProjection(value string) bool {
	if value == "" || value == "." || value == ".." || filepath.Clean(value) != value || filepath.IsAbs(value) {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func openProjectionRoot(component string) (*os.File, error) {
	cwdFD, err := syscall.Open(".", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open offline Product projection parent: %w", err)
	}
	defer syscall.Close(cwdFD)
	fd, err := syscall.Openat(
		cwdFD,
		component,
		syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open offline Product projection: %w", err)
	}
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("inspect held offline Product projection: %w", err)
	}
	if stat.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		syscall.Close(fd)
		return nil, fmt.Errorf("offline Product projection is not a plain directory")
	}
	return os.NewFile(uintptr(fd), component), nil
}

func projectGuestRelativePath(candidateRoot, guestPath string) (string, error) {
	relative, err := filepath.Rel(candidateRoot, guestPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("offline Product target escapes the consumer root")
	}
	if filepath.IsAbs(relative) || filepath.Clean(relative) != relative {
		return "", fmt.Errorf("offline Product target is not one exact relative path")
	}
	return relative, nil
}

func readProtectedIdentity(path string, expectedOwnerUID int) (*os.File, []byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, nil, fmt.Errorf("source transport identity handle is not exact and absolute")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open source transport identity: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, nil, fmt.Errorf("hold source transport identity")
	}
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("inspect held source transport identity: %w", err)
	}
	if err := validateProtectedIdentityStat(stat, expectedOwnerUID); err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if len(data) == 0 || len(data) > 1<<20 {
		_ = file.Close()
		return nil, nil, fmt.Errorf("source transport identity payload is invalid")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("rewind held source transport identity: %w", err)
	}
	return file, data, nil
}

func validateProtectedIdentityStat(stat syscall.Stat_t, expectedOwnerUID int) error {
	if expectedOwnerUID < 1 || stat.Mode&syscall.S_IFMT != syscall.S_IFREG ||
		stat.Mode&0o777 != 0o600 || int(stat.Uid) != kernelUID(expectedOwnerUID) {
		return fmt.Errorf("source transport identity handle is not a protected manifest-owned file")
	}
	return nil
}

func derivePublicKey(executable string, identity *os.File) ([]byte, error) {
	if _, err := identity.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind Product private key: %w", err)
	}
	command := exec.Command(executable, "-y", "-f", "/proc/self/fd/3")
	command.Env = []string{}
	command.ExtraFiles = []*os.File{identity}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("derive Product public key: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	output = bytes.TrimSpace(output)
	if !bytes.HasPrefix(output, []byte("ssh-ed25519 ")) || bytes.ContainsAny(output, "\r\n") {
		return nil, fmt.Errorf("derived Product public key is not one exact Ed25519 key")
	}
	return append(output, '\n'), nil
}

func writeOwnedAt(root *os.File, relative string, data []byte, mode os.FileMode, uid, gid int) error {
	if root == nil || filepath.IsAbs(relative) || filepath.Clean(relative) != relative {
		return fmt.Errorf("offline Product seed target is not exact and relative")
	}
	components := strings.Split(relative, string(filepath.Separator))
	if len(components) < 1 {
		return fmt.Errorf("offline Product seed target is empty")
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return fmt.Errorf("offline Product seed target contains an unsafe component")
		}
	}
	currentFD, err := syscall.Dup(int(root.Fd()))
	if err != nil {
		return fmt.Errorf("hold offline Product projection generation: %w", err)
	}
	syscall.CloseOnExec(currentFD)
	defer func() { _ = syscall.Close(currentFD) }()
	for _, component := range components[:len(components)-1] {
		nextFD, err := openOrCreateDirectoryAt(currentFD, component, uid, gid)
		if err != nil {
			return err
		}
		_ = syscall.Close(currentFD)
		currentFD = nextFD
	}
	leaf := components[len(components)-1]
	fd, err := syscall.Openat(
		currentFD,
		leaf,
		syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,
		uint32(mode.Perm()),
	)
	if err != nil {
		return fmt.Errorf("create offline Product seed target: %w", err)
	}
	file := os.NewFile(uintptr(fd), leaf)
	if file == nil {
		_ = syscall.Close(fd)
		return fmt.Errorf("hold offline Product seed target")
	}
	var stat syscall.Stat_t
	statErr := syscall.Fstat(fd, &stat)
	writeErr := error(nil)
	if statErr == nil && stat.Mode&syscall.S_IFMT != syscall.S_IFREG {
		statErr = fmt.Errorf("offline Product seed target is not a regular file")
	}
	if statErr == nil {
		_, writeErr = file.Write(data)
	}
	syncErr := file.Sync()
	chownErr := kernelFchown(fd, uid, gid)
	chmodErr := syscall.Fchmod(fd, uint32(mode.Perm()))
	closeErr := file.Close()
	if err := errorsJoin(statErr, writeErr, syncErr, chownErr, chmodErr, closeErr); err != nil {
		return err
	}
	return nil
}

func writeOwnedAtomicAt(
	root *os.File,
	relative string,
	data []byte,
	mode os.FileMode,
	uid, gid int,
) error {
	return writeOwnedAtomicAtWithHooks(
		root,
		relative,
		data,
		mode,
		uid,
		gid,
		atomicWriteHooks{},
	)
}

type atomicWriteHooks struct {
	beforeLink       func(parentFD, anonymousFD int, leaf string) error
	afterLink        func(parentFD, anonymousFD int, leaf string) error
	beforeParentSync func(parentFD, anonymousFD int, leaf string) error
	afterParentSync  func(parentFD, anonymousFD int, leaf string) error
}

func writeOwnedAtomicAtWithHooks(
	root *os.File,
	relative string,
	data []byte,
	mode os.FileMode,
	uid, gid int,
	hooks atomicWriteHooks,
) error {
	parentFD, leaf, err := openRelativeParentAt(root, relative, uid, gid, false)
	if err != nil {
		return attemptedInstallFailure("open-parent", "target-not-created", err)
	}
	defer syscall.Close(parentFD)
	fd, err := openAnonymousFileAt(parentFD, mode)
	if err != nil {
		return attemptedInstallFailure("create-generation", "target-not-created", err)
	}
	file := os.NewFile(uintptr(fd), "anonymous fixed-slot Codex auth generation")
	if file == nil {
		_ = syscall.Close(fd)
		return attemptedInstallFailure(
			"create-generation",
			"target-not-created",
			fmt.Errorf("hold anonymous fixed-slot generation"),
		)
	}
	defer file.Close()
	if err := validatePlainFD(fd); err != nil {
		return attemptedInstallFailure("validate-generation", "target-not-created", err)
	}
	if err := writeAll(file, data); err != nil {
		return attemptedInstallFailure("write-generation", "target-not-created", err)
	}
	if err := file.Sync(); err != nil {
		return attemptedInstallFailure("sync-generation", "target-not-created", err)
	}
	if err := kernelFchown(fd, uid, gid); err != nil {
		return attemptedInstallFailure("assign-generation-owner", "target-not-created", err)
	}
	if err := syscall.Fchmod(fd, uint32(mode.Perm())); err != nil {
		return attemptedInstallFailure("assign-generation-mode", "target-not-created", err)
	}
	if err := file.Sync(); err != nil {
		return attemptedInstallFailure("sync-generation-metadata", "target-not-created", err)
	}
	if err := validateOwnedPlainFD(fd, uid, gid, mode); err != nil {
		return attemptedInstallFailure("verify-generation-metadata", "target-not-created", err)
	}
	if err := validateExactPayloadFD(fd, data); err != nil {
		return attemptedInstallFailure("verify-generation", "target-not-created", err)
	}
	if hooks.beforeLink != nil {
		if err := hooks.beforeLink(parentFD, fd, leaf); err != nil {
			return attemptedInstallFailure("before-link", "target-not-created", err)
		}
	}
	if err := linkAnonymousAtNoReplace(fd, parentFD, leaf); err != nil {
		return attemptedInstallFailure("link-generation", "target-not-created", err)
	}
	postLinkFailure := func(phase string, cause error) error {
		return reconcileInstalledGeneration(
			root,
			parentFD,
			fd,
			relative,
			leaf,
			data,
			mode,
			uid,
			gid,
			phase,
			cause,
		)
	}
	if hooks.afterLink != nil {
		if err := hooks.afterLink(parentFD, fd, leaf); err != nil {
			return postLinkFailure("after-link", err)
		}
	}
	if err := validateInstalledGeneration(
		root,
		fd,
		relative,
		data,
		mode,
		uid,
		gid,
	); err != nil {
		return postLinkFailure("verify-installed-generation", err)
	}
	if hooks.beforeParentSync != nil {
		if err := hooks.beforeParentSync(parentFD, fd, leaf); err != nil {
			return postLinkFailure("before-parent-sync", err)
		}
	}
	if err := syscall.Fsync(parentFD); err != nil {
		return postLinkFailure("sync-parent", err)
	}
	if hooks.afterParentSync != nil {
		if err := hooks.afterParentSync(parentFD, fd, leaf); err != nil {
			return postLinkFailure("after-parent-sync", err)
		}
	}
	if err := validateInstalledGeneration(
		root,
		fd,
		relative,
		data,
		mode,
		uid,
		gid,
	); err != nil {
		return postLinkFailure("verify-durable-generation", err)
	}
	return nil
}

func attemptedInstallFailure(
	phase string,
	reconciliation string,
	cause error,
) *CodexAuthInstallFailure {
	return &CodexAuthInstallFailure{
		SchemaVersion:  CodexAuthSeedFailureSchema,
		Status:         "failed",
		Outcome:        CodexAuthInstallAttempted,
		Phase:          phase,
		Reconciliation: reconciliation,
		Message:        cause.Error(),
		cause:          cause,
	}
}

func reconcileInstalledGeneration(
	root *os.File,
	parentFD int,
	anonymousFD int,
	relative string,
	leaf string,
	data []byte,
	mode os.FileMode,
	uid, gid int,
	phase string,
	cause error,
) *CodexAuthInstallFailure {
	installedFD, err := syscall.Openat(
		parentFD,
		leaf,
		syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,
		0,
	)
	if errors.Is(err, syscall.ENOENT) {
		return &CodexAuthInstallFailure{
			SchemaVersion:  CodexAuthSeedFailureSchema,
			Status:         "failed",
			Outcome:        CodexAuthInstallAttempted,
			Phase:          phase,
			Reconciliation: "target-absent",
			Message:        cause.Error(),
			cause:          cause,
		}
	}
	if err != nil {
		return &CodexAuthInstallFailure{
			SchemaVersion:  CodexAuthSeedFailureSchema,
			Status:         "failed",
			Outcome:        CodexAuthInstallAmbiguous,
			Phase:          phase,
			Reconciliation: "target-unreadable",
			Message:        cause.Error(),
			cause:          cause,
		}
	}
	installed := os.NewFile(uintptr(installedFD), leaf)
	if installed == nil {
		_ = syscall.Close(installedFD)
		return &CodexAuthInstallFailure{
			SchemaVersion:  CodexAuthSeedFailureSchema,
			Status:         "failed",
			Outcome:        CodexAuthInstallAmbiguous,
			Phase:          phase,
			Reconciliation: "target-uninspectable",
			Message:        cause.Error(),
			cause:          cause,
		}
	}
	defer installed.Close()
	same, sameErr := sameFileFD(anonymousFD, installedFD)
	if sameErr != nil || !same {
		return &CodexAuthInstallFailure{
			SchemaVersion:  CodexAuthSeedFailureSchema,
			Status:         "failed",
			Outcome:        CodexAuthInstallAmbiguous,
			Phase:          phase,
			Reconciliation: "target-generation-changed",
			Message:        cause.Error(),
			cause:          cause,
		}
	}
	if err := validateOwnedPlainFD(installedFD, uid, gid, mode); err != nil {
		return &CodexAuthInstallFailure{
			SchemaVersion:  CodexAuthSeedFailureSchema,
			Status:         "failed",
			Outcome:        CodexAuthInstallAmbiguous,
			Phase:          phase,
			Reconciliation: "installed-metadata-changed",
			Message:        cause.Error(),
			cause:          cause,
		}
	}
	if err := validateExactPayloadFD(installedFD, data); err != nil {
		return &CodexAuthInstallFailure{
			SchemaVersion:  CodexAuthSeedFailureSchema,
			Status:         "failed",
			Outcome:        CodexAuthInstallAmbiguous,
			Phase:          phase,
			Reconciliation: "installed-bytes-changed",
			Message:        cause.Error(),
			cause:          cause,
		}
	}
	if err := validateInstalledGeneration(root, anonymousFD, relative, data, mode, uid, gid); err != nil {
		return &CodexAuthInstallFailure{
			SchemaVersion:  CodexAuthSeedFailureSchema,
			Status:         "failed",
			Outcome:        CodexAuthInstallAmbiguous,
			Phase:          phase,
			Reconciliation: "installed-path-changed",
			Message:        cause.Error(),
			cause:          cause,
		}
	}
	return &CodexAuthInstallFailure{
		SchemaVersion:  CodexAuthSeedFailureSchema,
		Status:         "failed",
		Outcome:        CodexAuthInstallEffect,
		Phase:          phase,
		Reconciliation: "exact-target-installed",
		Message:        cause.Error(),
		cause:          cause,
	}
}

func validateInstalledGeneration(
	root *os.File,
	anonymousFD int,
	relative string,
	data []byte,
	mode os.FileMode,
	uid, gid int,
) error {
	installed, err := openOwnedPlainAt(root, relative, mode, uid, gid)
	if err != nil {
		return fmt.Errorf("open installed fixed-slot generation: %w", err)
	}
	defer installed.Close()
	same, err := sameFileFD(anonymousFD, int(installed.Fd()))
	if err != nil {
		return err
	}
	if !same {
		return fmt.Errorf("fixed-slot target generation changed during installation")
	}
	if err := validateExactPayloadFD(int(installed.Fd()), data); err != nil {
		return fmt.Errorf("validate installed fixed-slot bytes: %w", err)
	}
	return nil
}

func sameFileFD(leftFD, rightFD int) (bool, error) {
	var left syscall.Stat_t
	if err := syscall.Fstat(leftFD, &left); err != nil {
		return false, err
	}
	var right syscall.Stat_t
	if err := syscall.Fstat(rightFD, &right); err != nil {
		return false, err
	}
	return left.Dev == right.Dev && left.Ino == right.Ino, nil
}

func validateExactPayloadFD(fd int, expected []byte) error {
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Size != int64(len(expected)) {
		return fmt.Errorf("fixed-slot generation byte length changed")
	}
	actual := make([]byte, len(expected)+1)
	read, err := syscall.Pread(fd, actual, 0)
	if err != nil {
		return err
	}
	if read != len(expected) || !bytes.Equal(actual[:read], expected) {
		return fmt.Errorf("fixed-slot generation bytes changed")
	}
	return nil
}

func openRelativeParentAt(
	root *os.File,
	relative string,
	uid, gid int,
	create bool,
) (int, string, error) {
	if root == nil || filepath.IsAbs(relative) || filepath.Clean(relative) != relative {
		return -1, "", fmt.Errorf("fixed-slot target is not exact and relative")
	}
	components := strings.Split(relative, string(filepath.Separator))
	if len(components) < 1 {
		return -1, "", fmt.Errorf("fixed-slot target is empty")
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return -1, "", fmt.Errorf("fixed-slot target contains an unsafe component")
		}
	}
	currentFD, err := syscall.Dup(int(root.Fd()))
	if err != nil {
		return -1, "", err
	}
	syscall.CloseOnExec(currentFD)
	for _, component := range components[:len(components)-1] {
		nextFD, openErr := openOwnedDirectoryAt(currentFD, component, uid, gid, create)
		_ = syscall.Close(currentFD)
		if openErr != nil {
			return -1, "", openErr
		}
		currentFD = nextFD
	}
	return currentFD, components[len(components)-1], nil
}

func openOwnedDirectoryAt(parentFD int, component string, uid, gid int, create bool) (int, error) {
	flags := syscall.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	fd, err := syscall.Openat(parentFD, component, flags, 0)
	created := false
	if create && errors.Is(err, syscall.ENOENT) {
		if err := syscall.Mkdirat(parentFD, component, 0o700); err != nil {
			return -1, fmt.Errorf("create fixed-slot directory: %w", err)
		}
		created = true
		fd, err = syscall.Openat(parentFD, component, flags, 0)
	}
	if err != nil {
		return -1, fmt.Errorf("open fixed-slot directory without following links: %w", err)
	}
	if created {
		if err := kernelFchown(fd, uid, gid); err != nil {
			_ = syscall.Close(fd)
			return -1, err
		}
		if err := syscall.Fchmod(fd, 0o700); err != nil {
			_ = syscall.Close(fd)
			return -1, err
		}
	}
	if err := validateOwnedDirectoryFD(fd, uid, gid, 0o700); err != nil {
		_ = syscall.Close(fd)
		return -1, err
	}
	return fd, nil
}

func validateOwnedDirectoryFD(fd, uid, gid int, mode os.FileMode) error {
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&syscall.S_IFMT != syscall.S_IFDIR ||
		stat.Mode&0o777 != uint32(mode.Perm()) ||
		int(stat.Uid) != kernelUID(uid) || int(stat.Gid) != kernelGID(gid) {
		return fmt.Errorf("fixed-slot directory ownership or mode does not match authority")
	}
	return nil
}

func validatePlainFD(fd int) error {
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return fmt.Errorf("fixed-slot target is not a regular file")
	}
	return nil
}

func validateOwnedPlainFD(fd, uid, gid int, mode os.FileMode) error {
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&syscall.S_IFMT != syscall.S_IFREG ||
		stat.Mode&0o777 != uint32(mode.Perm()) ||
		int(stat.Uid) != kernelUID(uid) || int(stat.Gid) != kernelGID(gid) {
		return fmt.Errorf("fixed-slot file ownership or mode does not match authority")
	}
	return nil
}

func writeAll(file *os.File, data []byte) error {
	for len(data) > 0 {
		written, err := file.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func openOrCreateDirectoryAt(parentFD int, component string, uid, gid int) (int, error) {
	flags := syscall.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	fd, err := syscall.Openat(parentFD, component, flags, 0)
	if errors.Is(err, syscall.ENOENT) {
		if err := syscall.Mkdirat(parentFD, component, 0o700); err != nil && !errors.Is(err, syscall.EEXIST) {
			return -1, fmt.Errorf("create offline Product seed directory: %w", err)
		}
		fd, err = syscall.Openat(parentFD, component, flags, 0)
	}
	if err != nil {
		return -1, fmt.Errorf("open offline Product seed directory without following links: %w", err)
	}
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		_ = syscall.Close(fd)
		return -1, err
	}
	if stat.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		_ = syscall.Close(fd)
		return -1, fmt.Errorf("offline Product seed ancestor is not a directory")
	}
	if err := kernelFchown(fd, uid, gid); err != nil {
		_ = syscall.Close(fd)
		return -1, err
	}
	if err := syscall.Fchmod(fd, 0o700); err != nil {
		_ = syscall.Close(fd)
		return -1, err
	}
	return fd, nil
}

func errorsJoin(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

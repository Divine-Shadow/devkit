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

type Result struct {
	SchemaVersion      string `json:"schema_version"`
	Status             string `json:"status"`
	ConsumerIndex      int    `json:"consumer_index"`
	RelativeProjection string `json:"relative_projection"`
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
	if err := syscall.Fchown(int(projectionRoot.Fd()), consumer.UID, consumer.GID); err != nil {
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
	if expectedOwnerUID < 1 || stat.Mode&syscall.S_IFMT != syscall.S_IFREG || stat.Mode&0o777 != 0o600 || int(stat.Uid) != expectedOwnerUID {
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
	chownErr := syscall.Fchown(fd, uid, gid)
	chmodErr := syscall.Fchmod(fd, uint32(mode.Perm()))
	closeErr := file.Close()
	if err := errorsJoin(statErr, writeErr, syncErr, chownErr, chmodErr, closeErr); err != nil {
		return err
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
	if err := syscall.Fchown(fd, uid, gid); err != nil {
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

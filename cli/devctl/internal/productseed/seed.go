package productseed

import (
	"bytes"
	"encoding/json"
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

type marker struct {
	SchemaVersion string `json:"schema_version"`
	ConsumerIndex int    `json:"consumer_index"`
	CandidateRoot string `json:"candidate_root"`
}

func Seed(authority productadapter.Authority, consumer productadapter.ConsumerManifest, count, index int, projection string) (Result, error) {
	if count != authority.Adapter.Count || index != consumer.Index {
		return Result{}, fmt.Errorf("offline Product seed identity does not match authority")
	}
	if !safeRelativeProjection(projection) {
		return Result{}, fmt.Errorf("offline Product seed requires one safe relative projection component")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return Result{}, err
	}
	projectionRoot := filepath.Join(cwd, projection)
	if err := requireProjectionRoot(projectionRoot); err != nil {
		return Result{}, err
	}
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
		{consumer.AuthorizedKeysPath, 0o600, append([]byte("restrict "), public...)},
	}
	for _, target := range targets {
		projected, err := projectGuestPath(consumer.CandidateRoot, projectionRoot, target.guest)
		if err != nil {
			return Result{}, err
		}
		if err := writeOwned(projected, target.data, target.mode, consumer.UID, consumer.GID); err != nil {
			return Result{}, err
		}
	}
	markerPayload, err := json.MarshalIndent(marker{
		SchemaVersion: productadapter.OfflineSeedSchema,
		ConsumerIndex: index,
		CandidateRoot: consumer.CandidateRoot,
	}, "", "  ")
	if err != nil {
		return Result{}, err
	}
	markerPayload = append(markerPayload, '\n')
	markerPath, err := projectGuestPath(
		consumer.CandidateRoot,
		projectionRoot,
		productadapter.OfflineSeedMarkerPath(consumer),
	)
	if err != nil {
		return Result{}, err
	}
	if err := writeOwned(markerPath, markerPayload, 0o600, consumer.UID, consumer.GID); err != nil {
		return Result{}, err
	}
	if err := os.Chown(projectionRoot, consumer.UID, consumer.GID); err != nil {
		return Result{}, fmt.Errorf("assign offline Product projection root: %w", err)
	}
	if err := os.Chmod(projectionRoot, 0o700); err != nil {
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

func requireProjectionRoot(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect offline Product projection: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("offline Product projection is not a plain directory")
	}
	return nil
}

func projectGuestPath(candidateRoot, projectionRoot, guestPath string) (string, error) {
	relative, err := filepath.Rel(candidateRoot, guestPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("offline Product target escapes the consumer root")
	}
	return filepath.Join(projectionRoot, relative), nil
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

func writeOwned(path string, data []byte, mode os.FileMode, uid, gid int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := chownAncestors(path, uid, gid); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create offline Product seed target: %w", err)
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	chownErr := file.Chown(uid, gid)
	closeErr := file.Close()
	if err := errorsJoin(writeErr, syncErr, chownErr, closeErr); err != nil {
		return err
	}
	return nil
}

func chownAncestors(path string, uid, gid int) error {
	current := filepath.Dir(path)
	for {
		if err := os.Chown(current, uid, gid); err != nil {
			return err
		}
		if err := os.Chmod(current, 0o700); err != nil {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		// Stop at the caller-owned projection directory; its parent is not part
		// of the guest consumer geometry.
		if filepath.Base(current) == ".ssh" || filepath.Base(current) == ".codex" {
			current = parent
			continue
		}
		return nil
	}
}

func errorsJoin(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

package launch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

type emdrOwnerPreparationIdentity struct {
	SchemaVersion    string                             `json:"schemaVersion"`
	State            string                             `json:"state"`
	ProfileIdentity  string                             `json:"profileIdentity"`
	Manifest         controllerOperationFileIdentity    `json:"manifest"`
	Socket           emdrOwnerPreparationSocketIdentity `json:"socket"`
	ClientExecutable string                             `json:"clientExecutable"`
	ServerExecutable string                             `json:"serverExecutable"`
	Operations       []string                           `json:"operations"`
	ServerPID        int                                `json:"serverPid"`
	StartedAt        string                             `json:"startedAt"`
}

type emdrOwnerPreparationSocketIdentity struct {
	Path   string `json:"path"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	UID    uint32 `json:"uid"`
	GID    uint32 `json:"gid"`
	Mode   uint32 `json:"mode"`
}

func validateEMDROwnerPreparationIdentity(capability nativeplan.EMDROwnerPreparationCapability) error {
	socketInfo, err := os.Lstat(nativeplan.EMDROwnerPreparationSocketPath)
	if err != nil {
		return fmt.Errorf("inspect EMDR owner preparation socket: %w", err)
	}
	if socketInfo.Mode()&os.ModeSymlink != 0 || socketInfo.Mode()&os.ModeSocket == 0 || socketInfo.Mode().Perm() != 0o600 {
		return fmt.Errorf("EMDR owner preparation socket must be a non-symlink mode 0600 Unix socket")
	}
	socketStat, ok := socketInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("EMDR owner preparation socket lacks stat identity")
	}
	identityInfo, err := os.Lstat(nativeplan.EMDROwnerPreparationIdentityPath)
	if err != nil {
		return fmt.Errorf("inspect EMDR owner preparation identity: %w", err)
	}
	if identityInfo.Mode()&os.ModeSymlink != 0 || !identityInfo.Mode().IsRegular() || identityInfo.Mode().Perm() != 0o400 {
		return fmt.Errorf("EMDR owner preparation identity must be a regular non-symlink mode 0400 file")
	}
	current, err := user.Current()
	if err != nil {
		return fmt.Errorf("resolve launching user: %w", err)
	}
	expectedGroup, err := user.LookupGroup(capability.Group)
	if err != nil {
		return fmt.Errorf("resolve EMDR owner preparation group %q: %w", capability.Group, err)
	}
	expectedUID, err := strconv.ParseUint(current.Uid, 10, 32)
	if err != nil {
		return fmt.Errorf("parse launching UID: %w", err)
	}
	expectedGID, err := strconv.ParseUint(expectedGroup.Gid, 10, 32)
	if err != nil {
		return fmt.Errorf("parse EMDR owner preparation GID: %w", err)
	}
	identityStat, ok := identityInfo.Sys().(*syscall.Stat_t)
	if current.Username != capability.Owner || uint64(socketStat.Uid) != expectedUID ||
		uint64(socketStat.Gid) != expectedGID || !ok || uint64(identityStat.Uid) != expectedUID ||
		uint64(identityStat.Gid) != expectedGID {
		return fmt.Errorf("EMDR owner preparation runtime ownership does not match the capability")
	}

	data, err := os.ReadFile(nativeplan.EMDROwnerPreparationIdentityPath)
	if err != nil {
		return fmt.Errorf("read EMDR owner preparation identity: %w", err)
	}
	var identity emdrOwnerPreparationIdentity
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&identity); err != nil {
		return fmt.Errorf("decode EMDR owner preparation identity: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode EMDR owner preparation identity: trailing JSON value")
		}
		return fmt.Errorf("decode EMDR owner preparation identity: trailing JSON content: %w", err)
	}
	manifestSHA, err := hashRegularNoFollowFile(nativeplan.EMDROwnerPreparationManifestPath, true)
	if err != nil {
		return fmt.Errorf("validate EMDR owner preparation manifest: %w", err)
	}
	expectedOperations := append([]string(nil), capability.Operations...)
	actualOperations := append([]string(nil), identity.Operations...)
	sort.Strings(expectedOperations)
	sort.Strings(actualOperations)
	if identity.SchemaVersion != capability.IdentitySchema || identity.State != "ready" ||
		identity.ProfileIdentity != capability.ProfileIdentity ||
		identity.Manifest != (controllerOperationFileIdentity{Path: nativeplan.EMDROwnerPreparationManifestPath, SHA256: manifestSHA}) ||
		identity.Socket != (emdrOwnerPreparationSocketIdentity{
			Path: nativeplan.EMDROwnerPreparationSocketPath, Device: uint64(socketStat.Dev), Inode: socketStat.Ino,
			UID: uint32(socketStat.Uid), GID: uint32(socketStat.Gid), Mode: uint32(socketInfo.Mode().Perm()),
		}) || identity.ClientExecutable != capability.ClientExecutable ||
		identity.ServerExecutable != capability.ServerExecutable ||
		strings.Join(actualOperations, "\x00") != strings.Join(expectedOperations, "\x00") || identity.ServerPID <= 0 {
		return fmt.Errorf("EMDR owner preparation identity does not match the live capability generation")
	}
	if _, err := time.Parse(time.RFC3339, identity.StartedAt); err != nil {
		return fmt.Errorf("EMDR owner preparation identity startedAt is invalid: %w", err)
	}
	return nil
}

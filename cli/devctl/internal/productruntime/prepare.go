package productruntime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"devkit/cli/devctl/internal/productadapter"
)

type preparedFile struct {
	name      string
	source    string
	target    string
	mode      os.FileMode
	secret    bool
	generated []byte
}

func prepareConsumerFiles(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
	command productadapter.Command,
) error {
	for _, directory := range []string{
		consumer.AgentRoot,
		consumer.StateRoot,
		consumer.HomePath,
		filepath.Join(consumer.HomePath, ".ssh"),
		filepath.Join(consumer.HomePath, ".codex"),
		filepath.Join(consumer.HomePath, ".codex", "rollouts"),
		filepath.Join(consumer.HomePath, ".codex", "rules"),
		filepath.Join(consumer.HomePath, ".cache"),
		filepath.Join(consumer.HomePath, ".config"),
		filepath.Join(consumer.HomePath, ".local"),
		consumer.GovernanceStateRoot,
	} {
		if !pathInside(consumer.CandidateRoot, directory) {
			return fmt.Errorf("Product prepared directory escapes candidate: %s", directory)
		}
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create Product prepared directory %s: %w", directory, err)
		}
	}
	for _, file := range preparedFiles(authority, consumer, command) {
		if err := writePreparedFile(file); err != nil {
			return err
		}
	}
	return nil
}

func preparedFiles(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
	command productadapter.Command,
) []preparedFile {
	return []preparedFile{
		{
			name: "codex_config", source: authority.Adapter.CodexConfigPath,
			target: filepath.Join(consumer.HomePath, ".codex", "config.toml"), mode: 0o600,
		},
		{
			name: "governance_rules", source: authority.Adapter.GovernanceRulesPath,
			target: filepath.Join(consumer.HomePath, ".codex", "rules", "governed-search-policy.rules"), mode: 0o600,
		},
		{
			name: "shell_hook", source: authority.Adapter.ShellHookPath,
			target: filepath.Join(consumer.HomePath, ".bashrc"), mode: 0o600,
		},
		{
			name: "governance_env", source: authority.Adapter.GovernanceEnvPath,
			target: consumer.GovernanceEnvTarget, mode: 0o600,
		},
		{
			name: "governance_repo", source: authority.Adapter.GovernanceRepoConfigPath,
			target: consumer.GovernanceRepoConfigTarget, mode: 0o600,
		},
	}
}

func validateOfflineSeededCandidate(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
) error {
	if err := requireOwnedPlainFile(consumer.CodexAuthPath, 0o600, consumer.UID); err != nil {
		return fmt.Errorf("validate Fleet-prehydrated Codex auth handle: %w", err)
	}
	for _, path := range []string{consumer.AgentRoot, consumer.StateRoot} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			if err == nil {
				return fmt.Errorf("offline Product seed contains runtime construction state: %s", path)
			}
			return err
		}
	}
	markerPath := productadapter.OfflineSeedMarkerPath(consumer)
	if err := requireOwnedPlainFile(markerPath, 0o600, consumer.UID); err != nil {
		return fmt.Errorf("validate offline Product seed marker: %w", err)
	}
	markerPayload, err := os.ReadFile(markerPath)
	if err != nil {
		return err
	}
	var marker struct {
		SchemaVersion string `json:"schema_version"`
		ConsumerIndex int    `json:"consumer_index"`
		CandidateRoot string `json:"candidate_root"`
	}
	decoder := json.NewDecoder(bytes.NewReader(markerPayload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil || marker.SchemaVersion != productadapter.OfflineSeedSchema ||
		marker.ConsumerIndex != consumer.Index || marker.CandidateRoot != consumer.CandidateRoot {
		return fmt.Errorf("offline Product seed marker does not match authority")
	}
	sshConfig, err := productadapter.ProductSSHConfig(authority, consumer, authority.Adapter.Count, consumer.Index)
	if err != nil {
		return err
	}
	knownHosts, err := os.ReadFile(authority.Adapter.KnownHostsPath)
	if err != nil {
		return fmt.Errorf("read immutable Product known-hosts: %w", err)
	}
	for name, expected := range map[string][]byte{
		"known-hosts": knownHosts,
		"SSH config":  sshConfig,
	} {
		path := filepath.Join(consumer.HomePath, ".ssh", map[string]string{
			"known-hosts": "known_hosts", "SSH config": "config",
		}[name])
		if err := requireOwnedPlainFile(path, 0o600, consumer.UID); err != nil {
			return fmt.Errorf("validate prehydrated %s: %w", name, err)
		}
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			return fmt.Errorf("prehydrated %s does not match immutable package authority", name)
		}
	}
	privatePublic := exec.Command(authority.Adapter.SSHKeygenPath, "-y", "-f", consumer.SSHIdentityPath)
	privatePublic.Env = []string{}
	derived, err := privatePublic.Output()
	if err != nil {
		return fmt.Errorf("validate prehydrated Product SSH identity: %w", err)
	}
	public, err := os.ReadFile(consumer.SSHPublicKeyPath)
	if err != nil || !bytes.Equal(bytes.TrimSpace(derived), bytes.TrimSpace(public)) {
		return fmt.Errorf("prehydrated Product SSH public key does not match private identity")
	}
	authorized, err := os.ReadFile(consumer.AuthorizedKeysPath)
	if err != nil || !bytes.Equal(authorized, append([]byte("restrict "), public...)) {
		return fmt.Errorf("prehydrated Product authorized_keys does not match the SSH identity")
	}
	return nil
}

func preparedPathSet(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
	command productadapter.Command,
) map[string]string {
	result := map[string]string{
		"candidate_root":   consumer.CandidateRoot,
		"agent_root":       consumer.AgentRoot,
		"worktree":         consumer.WorktreePath,
		"common_dir":       consumer.CommonDirPath,
		"home":             consumer.HomePath,
		"state":            consumer.StateRoot,
		"governance_state": consumer.GovernanceStateRoot,
	}
	for _, file := range preparedFiles(authority, consumer, command) {
		result[file.name] = file.target
	}
	return result
}

func validatePreparedFiles(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
	command productadapter.Command,
) error {
	for _, file := range preparedFiles(authority, consumer, command) {
		if err := validatePreparedFile(file); err != nil {
			return err
		}
	}
	for _, directory := range []string{
		consumer.CandidateRoot,
		consumer.AgentRoot,
		consumer.WorktreePath,
		consumer.CommonDirPath,
		consumer.HomePath,
		consumer.StateRoot,
		consumer.GovernanceStateRoot,
	} {
		if err := requirePlainDirectory(directory); err != nil {
			return fmt.Errorf("validate Product prepared directory %s: %w", directory, err)
		}
	}
	return nil
}

func writePreparedFile(file preparedFile) error {
	if err := os.MkdirAll(filepath.Dir(file.target), 0o700); err != nil {
		return err
	}
	if _, err := os.Lstat(file.target); !os.IsNotExist(err) {
		if err == nil {
			return fmt.Errorf("Product prepared file already exists: %s", file.target)
		}
		return err
	}
	data := file.generated
	if data == nil {
		var err error
		data, err = os.ReadFile(file.source)
		if err != nil {
			return fmt.Errorf("read Product %s source: %w", file.name, err)
		}
	}
	fileHandle, err := os.OpenFile(file.target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, file.mode)
	if err != nil {
		return fmt.Errorf("create Product %s target: %w", file.name, err)
	}
	_, writeErr := fileHandle.Write(data)
	syncErr := fileHandle.Sync()
	closeErr := fileHandle.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return fmt.Errorf("persist Product %s target: %v %v %v", file.name, writeErr, syncErr, closeErr)
	}
	return nil
}

func validatePreparedFile(file preparedFile) error {
	if err := requirePlainFile(file.target, file.mode); err != nil {
		return fmt.Errorf("validate Product %s target: %w", file.name, err)
	}
	expected := file.generated
	if expected == nil {
		var err error
		expected, err = os.ReadFile(file.source)
		if err != nil {
			return err
		}
	}
	actual, err := os.ReadFile(file.target)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, expected) {
		return fmt.Errorf("Product prepared file %s changed in place", file.name)
	}
	return nil
}

func requirePlainFile(path string, mode os.FileMode) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("path is not exact and absolute")
	}
	current := string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(path, current), string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path traverses symlink %s", current)
		}
		if index < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("path ancestor is not a directory: %s", current)
		}
		if index == len(parts)-1 &&
			(!info.Mode().IsRegular() || info.Mode().Perm() != mode.Perm()) {
			return fmt.Errorf("path is not a plain %04o file", mode.Perm())
		}
	}
	return nil
}

func requireOwnedPlainFile(path string, mode os.FileMode, uid int) error {
	if err := requirePlainFile(path, mode); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != uid {
		return fmt.Errorf("path is not owned by uid %d", uid)
	}
	return nil
}

func pathInside(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validateCandidateBoundary(consumer productadapter.ConsumerManifest) error {
	for _, path := range []string{
		consumer.CandidateRoot,
		consumer.AgentRoot,
		consumer.WorktreePath,
		consumer.CommonDirPath,
		consumer.HomePath,
		consumer.StateRoot,
		consumer.GovernanceStateRoot,
	} {
		if !pathInside(consumer.CandidateRoot, path) {
			return fmt.Errorf("Product prepared path escapes candidate: %s", path)
		}
		if err := requirePlainDirectory(path); err != nil {
			return err
		}
	}
	return nil
}

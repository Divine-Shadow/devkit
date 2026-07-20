package productruntime

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	sshDir := filepath.Join(consumer.HomePath, ".ssh")
	return []preparedFile{
		{
			name: "ssh_private", source: consumer.SSHIdentityPath,
			target: filepath.Join(sshDir, "id_ed25519"), mode: 0o600, secret: true,
		},
		{
			name: "ssh_public", source: consumer.SSHPublicKeyPath,
			target: filepath.Join(sshDir, "id_ed25519.pub"), mode: 0o600,
		},
		{
			name: "ssh_known_hosts", source: authority.Adapter.KnownHostsPath,
			target: filepath.Join(sshDir, "known_hosts"), mode: 0o600,
		},
		{
			name: "ssh_config", generated: productSSHConfig(authority, consumer, command),
			target: filepath.Join(sshDir, "config"), mode: 0o600,
		},
		{
			name: "codex_auth", source: consumer.CodexAuthPath,
			target: filepath.Join(consumer.HomePath, ".codex", "auth.json"), mode: 0o600, secret: true,
		},
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

func productSSHConfig(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
	command productadapter.Command,
) []byte {
	userName := "git"
	if parsed, err := url.Parse(authority.Adapter.ProductOrigin); err == nil && parsed.User != nil {
		if value := parsed.User.Username(); value != "" {
			userName = value
		}
	}
	return []byte(strings.Join([]string{
		"Host github.com ssh.github.com",
		"  HostName ssh.github.com",
		"  Port 443",
		"  User " + userName,
		"  IdentitiesOnly yes",
		"  IdentityFile " + filepath.Join(consumer.HomePath, ".ssh", "id_ed25519"),
		"  UserKnownHostsFile " + filepath.Join(consumer.HomePath, ".ssh", "known_hosts"),
		"  StrictHostKeyChecking yes",
		"  ProxyCommand " + authority.Adapter.ProxyHelperPath +
			" stdio --count " + strconv.Itoa(command.Count) +
			" --index " + strconv.Itoa(command.Index),
		"",
	}, "\n"))
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

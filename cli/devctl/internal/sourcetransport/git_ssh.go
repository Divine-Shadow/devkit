package sourcetransport

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	GitSSHSchema        = "devkit/source-transport-git-ssh/v1"
	IdentityEnvironment = "DEVKIT_SOURCE_TRANSPORT_IDENTITY"
	SocketEnvironment   = "DEVKIT_SOURCE_TRANSPORT_SOCKET"
)

var (
	packageOpenSSHExecutable string
	packageSSHConfig         string
	packageTransport         string
)

func RunGitSSH(args []string) error {
	if err := ValidateGitSSHArgs(args); err != nil {
		return err
	}
	identity, err := protectedHandle(os.Getenv(IdentityEnvironment), false)
	if err != nil {
		return fmt.Errorf("identity handle: %w", err)
	}
	socket, err := protectedHandle(os.Getenv(SocketEnvironment), true)
	if err != nil {
		return fmt.Errorf("transport socket: %w", err)
	}
	for label, path := range map[string]string{
		"package OpenSSH":    packageOpenSSHExecutable,
		"package SSH config": packageSSHConfig,
		"package transport":  packageTransport,
	} {
		if !immutablePackagePath(path) {
			return fmt.Errorf("%s is not an immutable package path", label)
		}
	}
	proxyCommand := packageTransport + " connect --socket " + socket + " --target %h:%p"
	sshArgs := []string{
		"-F", packageSSHConfig,
		"-i", identity,
		"-o", "ProxyCommand=" + proxyCommand,
	}
	sshArgs = append(sshArgs, args...)
	command := exec.Command(packageOpenSSHExecutable, sshArgs...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = []string{}
	if protocol := os.Getenv("GIT_PROTOCOL"); protocol != "" {
		if protocol != "version=2" {
			return fmt.Errorf("unsupported GIT_PROTOCOL value")
		}
		command.Env = []string{"GIT_PROTOCOL=version=2"}
	}
	if err := command.Run(); err != nil {
		return err
	}
	return nil
}

func ValidateGitSSHArgs(args []string) error {
	if len(args) < 1 || len(args) > 7 {
		return fmt.Errorf("Git SSH argv is outside the bounded grammar")
	}
	query := false
	index := 0
	for index < len(args) && strings.HasPrefix(args[index], "-") {
		switch args[index] {
		case "-G":
			if query {
				return fmt.Errorf("duplicate Git SSH query option")
			}
			query = true
			index++
		case "-o":
			if index+1 >= len(args) || args[index+1] != "SendEnv=GIT_PROTOCOL" {
				return fmt.Errorf("Git SSH permits only SendEnv=GIT_PROTOCOL")
			}
			index += 2
		case "-p":
			if index+1 >= len(args) || args[index+1] != "443" {
				return fmt.Errorf("Git SSH permits only package-selected port 443")
			}
			index += 2
		default:
			return fmt.Errorf("Git SSH option %q is not permitted", args[index])
		}
	}
	if index >= len(args) || !allowedGitHubHost(args[index]) {
		return fmt.Errorf("Git SSH host is not the package-owned GitHub endpoint")
	}
	index++
	if query {
		if index != len(args) {
			return fmt.Errorf("Git SSH configuration query accepts no remote command")
		}
		return nil
	}
	if index+1 != len(args) || !validUploadPack(args[index]) {
		return fmt.Errorf("Git SSH requires one exact git-upload-pack command")
	}
	return nil
}

func allowedGitHubHost(value string) bool {
	switch value {
	case "github.com", "git@github.com", "ssh.github.com", "git@ssh.github.com":
		return true
	default:
		return false
	}
}

func validUploadPack(command string) bool {
	const prefix = "git-upload-pack '"
	if !strings.HasPrefix(command, prefix) || !strings.HasSuffix(command, "'") {
		return false
	}
	path := strings.TrimSuffix(strings.TrimPrefix(command, prefix), "'")
	if path == "" || len(path) > 512 || !strings.HasSuffix(path, ".git") || strings.Contains(path, "..") || strings.Contains(path, "//") {
		return false
	}
	for _, value := range []byte(path) {
		if (value < 'a' || value > 'z') && (value < 'A' || value > 'Z') &&
			(value < '0' || value > '9') && value != '/' && value != '-' && value != '_' && value != '.' {
			return false
		}
	}
	return true
}

func protectedHandle(raw string, socket bool) (string, error) {
	if raw == "" || !filepath.IsAbs(raw) || filepath.Clean(raw) != raw || strings.ContainsAny(raw, " \t\r\n'\"`$;&|<>\\") {
		return "", fmt.Errorf("path is not an exact safe absolute handle")
	}
	if err := noSymlinkTraversal(raw); err != nil {
		return "", err
	}
	info, err := os.Lstat(raw)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("handle has unsafe ownership or type")
	}
	if socket {
		if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 {
			return "", fmt.Errorf("handle is not an exact owner-only Unix socket")
		}
	} else if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return "", fmt.Errorf("handle is not an exact owner-only regular file")
	}
	return raw, nil
}

func noSymlinkTraversal(path string) error {
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(path, current), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("handle traverses symlink %s", current)
		}
	}
	return nil
}

func immutablePackagePath(path string) bool {
	return strings.HasPrefix(path, "/nix/store/") && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, " \t\r\n'\"`$;&|<>\\")
}

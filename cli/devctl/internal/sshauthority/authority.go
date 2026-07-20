package sshauthority

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// packageExecutable is injected by the immutable Devkit package build. It has
// no source default: an unpackaged binary must fail closed instead of
// consulting PATH or a host system location.
var packageExecutable string

// packageKnownHosts is injected by the same immutable Devkit package build.
// It is deliberately not discovered from a caller home or the network.
var packageKnownHosts string

// Authority is the single source-controlled OpenSSH executable and host-key
// authority.
// Production code obtains it only through Package. New exists so tests can
// inject immutable fixture inputs without adding a runtime flag or environment
// override.
type Authority struct {
	executable     string
	knownHostsFile string
}

// Package resolves the executable and pinned host keys selected by the Devkit
// package build.
func Package() (Authority, error) {
	return New(packageExecutable, packageKnownHosts)
}

// New validates explicitly injected executable and host-key authorities.
func New(executable, knownHostsFile string) (Authority, error) {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return Authority{}, fmt.Errorf("package-owned OpenSSH executable is not bound")
	}
	if !filepath.IsAbs(executable) {
		return Authority{}, fmt.Errorf("package-owned OpenSSH executable must be absolute: %s", executable)
	}
	executable = filepath.Clean(executable)
	info, err := os.Stat(executable)
	if err != nil {
		return Authority{}, fmt.Errorf("package-owned OpenSSH executable %s: %w", executable, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return Authority{}, fmt.Errorf("package-owned OpenSSH executable is not executable: %s", executable)
	}
	knownHostsFile = strings.TrimSpace(knownHostsFile)
	if knownHostsFile == "" {
		return Authority{}, fmt.Errorf("package-owned SSH known-hosts authority is not bound")
	}
	if !filepath.IsAbs(knownHostsFile) {
		return Authority{}, fmt.Errorf("package-owned SSH known-hosts path must be absolute: %s", knownHostsFile)
	}
	knownHostsFile = filepath.Clean(knownHostsFile)
	knownHosts, err := os.ReadFile(knownHostsFile)
	if err != nil {
		return Authority{}, fmt.Errorf("package-owned SSH known-hosts %s: %w", knownHostsFile, err)
	}
	if strings.TrimSpace(string(knownHosts)) == "" {
		return Authority{}, fmt.Errorf("package-owned SSH known-hosts is empty: %s", knownHostsFile)
	}
	return Authority{executable: executable, knownHostsFile: knownHostsFile}, nil
}

// Executable returns the validated absolute executable path.
func (a Authority) Executable() string {
	return a.executable
}

// KnownHostsFile returns the immutable source-selected host-key material.
func (a Authority) KnownHostsFile() string {
	return a.knownHostsFile
}

// InstallKnownHosts atomically replaces one consumer's host-key file with the
// exact immutable package material. It never reads, merges, or accepts caller
// known-host state.
func (a Authority) InstallKnownHosts(destination string) error {
	if strings.TrimSpace(a.knownHostsFile) == "" {
		return fmt.Errorf("package-owned SSH known-hosts authority is empty")
	}
	destination = strings.TrimSpace(destination)
	if destination == "" || !filepath.IsAbs(destination) {
		return fmt.Errorf("consumer SSH known-hosts destination must be absolute: %s", destination)
	}
	content, err := os.ReadFile(a.knownHostsFile)
	if err != nil {
		return fmt.Errorf("read package-owned SSH known-hosts %s: %w", a.knownHostsFile, err)
	}
	destination = filepath.Clean(destination)
	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create consumer SSH directory %s: %w", directory, err)
	}
	temporary, err := os.CreateTemp(directory, ".known-hosts-*")
	if err != nil {
		return fmt.Errorf("create consumer SSH known-hosts temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(0o644); err != nil {
		cleanup()
		return fmt.Errorf("chmod consumer SSH known-hosts temporary file: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		cleanup()
		return fmt.Errorf("write consumer SSH known-hosts temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("close consumer SSH known-hosts temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("install consumer SSH known-hosts %s: %w", destination, err)
	}
	return nil
}

// Command returns the exact Git-compatible SSH command for one source-derived
// configuration path.
func (a Authority) Command(configPath string) (string, error) {
	if strings.TrimSpace(a.executable) == "" {
		return "", fmt.Errorf("package-owned OpenSSH authority is empty")
	}
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return "", fmt.Errorf("source-derived SSH config path is empty")
	}
	if !filepath.IsAbs(configPath) {
		return "", fmt.Errorf("source-derived SSH config path must be absolute: %s", configPath)
	}
	return shellQuote(a.executable) + " -F " + shellQuote(filepath.Clean(configPath)), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

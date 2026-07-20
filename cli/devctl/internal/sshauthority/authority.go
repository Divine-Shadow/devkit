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

// Authority is the single source-controlled OpenSSH executable authority.
// Production code obtains it only through Package. New exists so tests can
// inject an immutable fixture executable without adding a runtime flag or
// environment override.
type Authority struct {
	executable string
}

// Package resolves the executable selected by the Devkit package build.
func Package() (Authority, error) {
	return New(packageExecutable)
}

// New validates an explicitly injected executable authority.
func New(executable string) (Authority, error) {
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
	return Authority{executable: executable}, nil
}

// Executable returns the validated absolute executable path.
func (a Authority) Executable() string {
	return a.executable
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

package fleetmode

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
	"strconv"
	"strings"
	"time"
)

type SSHTransport struct{ Executable string }

// The immutable package injects its selected OpenSSH path. Source execution
// must not acquire a different executable from ambient PATH.
var packageSSHExecutable string

func SSHArguments(s Station, mode string) ([]string, error) {
	if mode != "vr" && mode != "status" {
		return nil, errors.New("unsupported remote verb")
	}
	// The controller has already validated the four distinct stations. This
	// one connection validates only its own fields, without invented peer rows.
	if err := validateStation(s); err != nil {
		return nil, err
	}
	// These enrolled Windows endpoints use PowerShell as OpenSSH's default
	// shell. Its call operator invokes the quoted native path; explicit exit
	// preserves the helper's structured domain-failure status over SSH.
	command := `& '` + RemoteAgentPath + `' agent ` + mode + ` --config 'C:\ProgramData\FleetMode\agent.json'; exit $LASTEXITCODE`
	return []string{"-F", "/dev/null", "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "-o", "StrictHostKeyChecking=yes", "-o", "GlobalKnownHostsFile=/dev/null", "-o", "UpdateHostKeys=no", "-o", "VerifyHostKeyDNS=no", "-o", "ConnectTimeout=8", "-o", "ConnectionAttempts=1", "-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=2", "-o", "ClearAllForwardings=yes", "-o", "UserKnownHostsFile=" + s.KnownHostsFile, "-i", s.IdentityFile, "-p", strconv.Itoa(s.Port), "-l", s.User, "--", s.Host, command}, nil
}

type cappedOutput struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *cappedOutput) Write(p []byte) (int, error) {
	n := len(p)
	room := b.limit - b.Len()
	if len(p) > room {
		b.overflow = true
		p = p[:room]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

func (t SSHTransport) Call(ctx context.Context, station Station, mode string) (StationResult, error) {
	var result StationResult
	args, err := SSHArguments(station, mode)
	if err != nil {
		return result, err
	}
	for _, path := range []string{station.IdentityFile, station.KnownHostsFile} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return result, errors.New("configured SSH identity or pinned known-hosts file unavailable")
		}
	}
	executable := t.Executable
	if executable == "" {
		executable = packageSSHExecutable
	}
	if !filepath.IsAbs(executable) {
		return result, errors.New("package-owned SSH executable is not bound to an absolute path")
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return result, errors.New("package-owned SSH executable is unavailable or not executable")
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdin = nil
	cmd.WaitDelay = 2 * time.Second
	stdout := &cappedOutput{limit: 1 << 20}
	stderr := &cappedOutput{limit: 16 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	exitCode := 0
	if err := cmd.Run(); err != nil {
		// Remote stderr is not an authority/result and may contain unrelated
		// connection details. Return only the fixed local failure category.
		if ctx.Err() != nil {
			return result, fmt.Errorf("SSH request ended without verified result: %w", ctx.Err())
		}
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
			return result, fmt.Errorf("SSH request failed without verified result: %w", err)
		}
		// Exit 1 can be the helper's structured domain failure. It is never
		// accepted without the complete host-bound JSON and matching semantics.
		exitCode = 1
	}
	if stdout.overflow {
		return result, errors.New("remote JSON exceeded result limit")
	}
	d := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	d.DisallowUnknownFields()
	if err := d.Decode(&result); err != nil {
		return StationResult{}, errors.New("remote result was not valid station JSON")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return StationResult{}, errors.New("remote result contained trailing data")
	}
	if !strings.EqualFold(result.Hostname, station.ExpectedHostname) {
		return StationResult{}, errors.New("remote hostname did not match configured station")
	}
	if agentResultExit(mode, result) != exitCode || (exitCode == 1 && result.Error == "") {
		return StationResult{}, errors.New("remote exit code and station result disagree")
	}
	return result, nil
}

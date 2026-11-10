package fleetmode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeSSH(t *testing.T, body string) (SSHTransport, Station) {
	t.Helper()
	root := t.TempDir()
	s := testConfig().Stations[0]
	s.IdentityFile = filepath.Join(root, "identity")
	s.KnownHostsFile = filepath.Join(root, "known-hosts")
	for _, p := range []string{s.IdentityFile, s.KnownHostsFile} {
		if err := os.WriteFile(p, []byte("nonsecret-fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "ssh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	return SSHTransport{Executable: path}, s
}

func TestSSHStrictArgumentsAndConstantRemoteCommand(t *testing.T) {
	s := testConfig().Stations[0]
	s.User = "Shadow's Throne"
	args, err := SSHArguments(s, "vr")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\n")
	for _, required := range []string{"BatchMode=yes", "IdentitiesOnly=yes", "StrictHostKeyChecking=yes", "GlobalKnownHostsFile=/dev/null", "UpdateHostKeys=no", "VerifyHostKeyDNS=no", "ConnectTimeout=8", "UserKnownHostsFile=" + s.KnownHostsFile} {
		if !strings.Contains(joined, required) {
			t.Fatal("missing strict setting", required)
		}
	}
	if got := args[len(args)-1]; got != `& 'C:\ProgramData\FleetMode\fleet-mode.exe' agent vr --config 'C:\ProgramData\FleetMode\agent.json'; exit $LASTEXITCODE` {
		t.Fatalf("nonfixed remote command: %q", got)
	}
	if _, err := SSHArguments(s, "vr;shutdown"); err == nil {
		t.Fatal("remote verb injection accepted")
	}
	foundUser := false
	for i, arg := range args {
		if arg == "-l" {
			foundUser = true
			if args[i+1] != s.User {
				t.Fatal("Windows user was reinterpreted")
			}
		}
	}
	if !foundUser {
		t.Fatal("explicit Windows login argv missing")
	}
}

func TestSSHRequiresRealJSONAndMatchingHostname(t *testing.T) {
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`printf '%s' '{"name":"Derp","hostname":"Derp-PC","mode":"vr","restartInhibited":true,"wslOff":true,"modeRecorded":true,"elapsedMillis":1,"stages":[]}'`, true},
		{`printf '%s' '{"hostname":"wrong","wslOff":true,"restartInhibited":true}'`, false},
		{`printf '%s' 'connection closed'`, false},
		{`printf '%s' '{"hostname":"Derp-PC"} forged'`, false},
		{`exit 255`, false},
	} {
		tr, s := fakeSSH(t, tc.body)
		r, err := tr.Call(context.Background(), s, "vr")
		if (err == nil) != tc.ok {
			t.Fatalf("result=%+v err=%v", r, err)
		}
	}
}

func TestSSHCancellationReapsOwnedClientAndMissingKeyIsImmediate(t *testing.T) {
	tr, s := fakeSSH(t, "exec sleep 20\n")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	began := time.Now()
	if _, err := tr.Call(ctx, s, "vr"); err == nil {
		t.Fatal("timeout accepted")
	}
	if time.Since(began) > time.Second {
		t.Fatal("owned client not bounded/reaped")
	}
	if err := os.Remove(s.IdentityFile); err != nil {
		t.Fatal(err)
	}
	began = time.Now()
	if _, err := tr.Call(context.Background(), s, "vr"); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatal("missing key did not refuse", err)
	}
	if time.Since(began) > time.Second {
		t.Fatal("missing key attempted remote call")
	}
}

func TestSSHUnpackagedExecutableFailsClosed(t *testing.T) {
	_, s := fakeSSH(t, "exit 99\n")
	previous := packageSSHExecutable
	packageSSHExecutable = ""
	defer func() { packageSSHExecutable = previous }()
	if _, err := (SSHTransport{}).Call(context.Background(), s, "vr"); err == nil || !strings.Contains(err.Error(), "package-owned SSH executable") {
		t.Fatal("ambient PATH supplied executable", err)
	}
}

func TestSSHStructuredDomainExitPreservesPartialFacts(t *testing.T) {
	body := `printf '%s' '{"hostname":"Derp-PC","mode":"vr","wslOff":true,"restartInhibited":false,"error":"restart inhibition not confirmed"}'; exit 1`
	tr, s := fakeSSH(t, body)
	result, err := tr.Call(context.Background(), s, "vr")
	if err != nil || !result.WSLOff || result.RestartInhibited || result.Error == "" {
		t.Fatalf("structured partial failure discarded: %+v %v", result, err)
	}
	for _, body := range []string{
		`printf '%s' '{"hostname":"Derp-PC","mode":"vr","wslOff":true,"restartInhibited":true}'; exit 1`,
		`printf '%s' '{"hostname":"Derp-PC","mode":"vr","wslOff":true,"restartInhibited":false}'; exit 1`,
		`printf '%s' '{"hostname":"Derp-PC","mode":"vr","wslOff":true,"restartInhibited":false,"error":"failure"}'; exit 255`,
		`printf '%s' '{"hostname":"Derp-PC","mode":"vr","wslOff":true,"restartInhibited":false,"error":"failure"}'; exit 0`,
	} {
		tr, s := fakeSSH(t, body)
		if _, err := tr.Call(context.Background(), s, "vr"); err == nil {
			t.Fatal("inconsistent/transport exit accepted")
		}
	}
}

func TestSSHRunningStatusJSONIsAValidObservation(t *testing.T) {
	tr, s := fakeSSH(t, `printf '%s' '{"hostname":"Derp-PC","mode":"status","wslOff":false,"restartInhibited":false}'`)
	r, err := tr.Call(context.Background(), s, "status")
	if err != nil || r.WSLOff || r.RestartInhibited {
		t.Fatalf("running status lost: %+v %v", r, err)
	}
}

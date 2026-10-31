package nativecmd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devkit/cli/devctl/internal/worktrees"
)

func TestCurrentSourceRejectsCallerSelectors(t *testing.T) {
	base := []string{"--current-worktree", "--source-only", "--repo", "ouroboros-ide"}
	if err := parseCurrentSourceArgs(base); err != nil {
		t.Fatal(err)
	}
	for _, extra := range [][]string{{"--index", "2"}, {"--workspace-root", "/foreign"}, {"--registration", "/caller.json"}, {"--registration-executable", "/caller"}, {"--origin", "foreign"}, {"--base-branch", "other"}, {"--repo-check", "git fetch"}, {"--", "bash"}} {
		if err := parseCurrentSourceArgs(append(append([]string{}, base...), extra...)); err == nil {
			t.Fatalf("accepted %v", extra)
		}
	}
}

func registrationTestExecutable(t *testing.T, body string) string {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "lookup")
	if err := os.WriteFile(path, []byte("#!"+sh+"\nset -eu\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSourceRegistrationProjectionUsesActualCwdAndNoArguments(t *testing.T) {
	cwd := t.TempDir()
	binding := worktrees.SourceRegistrationBinding{SchemaVersion: "devkit/current-source-registration/v1", WorkspaceID: "registered", Worktree: cwd, BaseWorktree: "/base", Home: "/home", TargetID: "darksteel-1", AgentIndex: 1, CodexHome: "/home/.codex", GitDirectory: cwd + "/.git", CommonDirectory: cwd + "/.git", RegistrationSHA256: strings.Repeat("a", 64)}
	payload, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	program := registrationTestExecutable(t, "[ \"$#\" -eq 0 ]\n[ \"$PWD\" = "+quote(cwd)+" ]\nprintf '%s\\n' "+quote(string(payload)))
	got, err := readSourceRegistrationProjection(context.Background(), program, cwd)
	if err != nil || got != binding {
		t.Fatalf("projection %#v error %v", got, err)
	}
}

func TestSourceRegistrationProjectionRefusesFailureAndBoundedMalformedOutput(t *testing.T) {
	for name, body := range map[string]string{
		"nonzero":   "printf '{}'; exit 7",
		"malformed": "printf '{}{}'",
		"oversized": "i=0; while [ \"$i\" -lt 4000 ]; do printf '0123456789'; i=$((i+1)); done",
	} {
		t.Run(name, func(t *testing.T) {
			program := registrationTestExecutable(t, body)
			if got, err := readSourceRegistrationProjection(context.Background(), program, t.TempDir()); err == nil || got != (worktrees.SourceRegistrationBinding{}) {
				t.Fatalf("failed response admitted: %#v %v", got, err)
			}
		})
	}
}

func TestSourceRegistrationProjectionCancellationSettlesProcess(t *testing.T) {
	program := registrationTestExecutable(t, "while :; do :; done")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := captureSourceRegistration(ctx, program, t.TempDir()); err == nil {
		t.Fatal("cancelled lookup admitted")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("lookup cancellation exceeded managed settlement bound")
	}
}

func TestSourceRegistrationLookupCannotBeReplacedByCaller(t *testing.T) {
	old := currentSourceRegistrationExecutable
	t.Cleanup(func() { currentSourceRegistrationExecutable = old })
	program := registrationTestExecutable(t, "exit 77")
	t.Setenv("DEVKIT_CURRENT_SOURCE_REGISTRATION_EXECUTABLE", program)
	for _, path := range []string{"", program, "/nix/store/../tmp/caller", "/nix/store/missing-source-registration/bin/lookup"} {
		currentSourceRegistrationExecutable = path
		if _, err := lookupCurrentSourceRegistration(context.Background(), t.TempDir()); err == nil {
			t.Fatalf("caller lookup accepted: %s", path)
		}
	}
}

func TestCurrentSourceRequiresExactExistingManagedTransport(t *testing.T) {
	q := func(s string) string {
		out := "'"
		for _, c := range s {
			if c == '\'' {
				out += "'\"'\"'"
			} else {
				out += string(c)
			}
		}
		return out + "'"
	}
	prefix := "package-managed-ssh"
	authority := "/nix/store/package"
	proxy := q(authority+"/kit/bin/devctl") + " -p 'dev-all' native proxy-connect --socket '/home/bayesartre/dev/.devkit/native-egress/.managed-egress-123.sock' --target %h:%p"
	command := prefix + " -o " + q("ProxyCommand="+proxy)
	if err := validateCurrentSourceTransport(prefix, authority+"/kit/bin/devctl", "/home/bayesartre/dev", command); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{command + "; evil", command + " -o ProxyCommand=evil", prefix + " -o 'ProxyCommand=evil'"} {
		if err := validateCurrentSourceTransport(prefix, authority+"/kit/bin/devctl", "/home/bayesartre/dev", bad); err == nil {
			t.Fatal("accepted replacement transport")
		}
	}
	if err := validateCurrentSourceTransport(prefix, "/nix/store/foreign/kit/bin/devctl", "/home/bayesartre/dev", command); err == nil {
		t.Fatal("accepted helper outside the current consumer profile")
	}
}

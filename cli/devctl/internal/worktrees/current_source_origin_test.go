package worktrees

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sourceCanonicalOrigin = "ssh://git@ssh.github.com:443/Divine-Shadow/ouroboros-ide.git"
const sourceHistoricalOrigin = "git@github.com:Divine-Shadow/ouroboros-ide.git"

func TestCurrentSourceOriginRepresentationContract(t *testing.T) {
	for _, test := range []struct {
		name, observed, declared string
		want                     bool
	}{
		{"exact", sourceCanonicalOrigin, sourceCanonicalOrigin, true},
		{"historical-scp", sourceHistoricalOrigin, sourceCanonicalOrigin, true},
		{"owner-derived", "git@github.com:Another-Owner/ouroboros-ide.git", "ssh://git@ssh.github.com:443/Another-Owner/ouroboros-ide.git", true},
		{"existing-exact-origin", "/fixture/origin.git", "/fixture/origin.git", true},
		{"reverse-relation", sourceCanonicalOrigin, sourceHistoricalOrigin, false},
		{"wrong-owner", "git@github.com:Other/ouroboros-ide.git", sourceCanonicalOrigin, false},
		{"owner-case", "git@github.com:divine-shadow/ouroboros-ide.git", sourceCanonicalOrigin, false},
		{"repository-case", "git@github.com:Divine-Shadow/Ouroboros-ide.git", sourceCanonicalOrigin, false},
		{"wrong-repository", "git@github.com:Divine-Shadow/other.git", sourceCanonicalOrigin, false},
		{"other-repository-declaration", "git@github.com:Divine-Shadow/other.git", "ssh://git@ssh.github.com:443/Divine-Shadow/other.git", false},
		{"wrong-user", "root@github.com:Divine-Shadow/ouroboros-ide.git", sourceCanonicalOrigin, false},
		{"wrong-host", "git@other.invalid:Divine-Shadow/ouroboros-ide.git", sourceCanonicalOrigin, false},
		{"host-case", "git@GitHub.com:Divine-Shadow/ouroboros-ide.git", sourceCanonicalOrigin, false},
		{"scp-port", "git@github.com:443/Divine-Shadow/ouroboros-ide.git", sourceCanonicalOrigin, false},
		{"scp-ssh-host", "git@ssh.github.com:Divine-Shadow/ouroboros-ide.git", sourceCanonicalOrigin, false},
		{"github-uri", "ssh://git@github.com/Divine-Shadow/ouroboros-ide.git", sourceCanonicalOrigin, false},
		{"port-22", "ssh://git@github.com:22/Divine-Shadow/ouroboros-ide.git", sourceCanonicalOrigin, false},
		{"https", "https://github.com/Divine-Shadow/ouroboros-ide.git", sourceCanonicalOrigin, false},
		{"missing-suffix", "git@github.com:Divine-Shadow/ouroboros-ide", sourceCanonicalOrigin, false},
		{"encoded", "git@github.com:Divine-Shadow/ouroboros%2dide.git", sourceCanonicalOrigin, false},
		{"extra-component", "git@github.com:Divine-Shadow/team/ouroboros-ide.git", sourceCanonicalOrigin, false},
		{"dot-component", "git@github.com:Divine-Shadow/./ouroboros-ide.git", sourceCanonicalOrigin, false},
		{"query", sourceHistoricalOrigin + "?x=1", sourceCanonicalOrigin, false},
		{"fragment", sourceHistoricalOrigin + "#x", sourceCanonicalOrigin, false},
		{"trailing-space", sourceHistoricalOrigin + " ", sourceCanonicalOrigin, false},
		{"leading-space", " " + sourceHistoricalOrigin, sourceCanonicalOrigin, false},
		{"newline", sourceHistoricalOrigin + "\n", sourceCanonicalOrigin, false},
		{"nul", sourceHistoricalOrigin + "\x00", sourceCanonicalOrigin, false},
		{"declared-port", sourceHistoricalOrigin, "ssh://git@ssh.github.com:22/Divine-Shadow/ouroboros-ide.git", false},
		{"declared-user", sourceHistoricalOrigin, "ssh://root@ssh.github.com:443/Divine-Shadow/ouroboros-ide.git", false},
		{"declared-host", sourceHistoricalOrigin, "ssh://git@github.com:443/Divine-Shadow/ouroboros-ide.git", false},
		{"declared-encoding", sourceHistoricalOrigin, "ssh://git@ssh.github.com:443/Divine%2dShadow/ouroboros-ide.git", false},
		{"declared-query", sourceHistoricalOrigin, sourceCanonicalOrigin + "?x=1", false},
		{"declared-space", sourceHistoricalOrigin, sourceCanonicalOrigin + " ", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := currentSourceOriginMatches(test.observed, test.declared); got != test.want {
				t.Fatalf("origin relationship (%q, %q) = %t, want %t", test.observed, test.declared, got, test.want)
			}
		})
	}
}

// Real Git must select the immutable host, port and repository for both remote
// operations. The test SSH endpoint rejects every other invocation and serves
// only the local bare fixture; no network or repository URL rewrite is used.
func sourceFixtureHistoricalOrigin(t *testing.T, s CurrentSource, remote string) (CurrentSource, string, string) {
	t.Helper()
	s.Origin = sourceCanonicalOrigin
	sourceTestGit(t, s.Worktree, "remote", "set-url", "origin", sourceHistoricalOrigin)
	common := sourceTestGit(t, s.Worktree, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if common != filepath.Join(s.Worktree, ".git") {
		marker, err := nativeOwnedCommonRepositoryMarker("ouroboros-ide", s.Origin, s.Index)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(common, "devkit-owned-common"), []byte(marker), 0600); err != nil {
			t.Fatal(err)
		}
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	ssh, log := filepath.Join(root, "ssh"), filepath.Join(root, "remote-calls")
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\n" +
		"if [ \"${1:-}\" = -G ]; then exit 0; fi\n" +
		"host=0\nport=0\nupload=0\n" +
		"while [ \"$#\" -gt 0 ]; do\n" +
		" case \"$1\" in\n" +
		" -o) [ \"$#\" -ge 2 ] && [ \"$2\" = SendEnv=GIT_PROTOCOL ] || exit 91; shift 2 ;;\n" +
		" -p) [ \"$#\" -ge 2 ] && [ \"$2\" = 443 ] || exit 92; port=1; shift 2 ;;\n" +
		" git@ssh.github.com) host=1; shift ;;\n" +
		" \"git-upload-pack '/Divine-Shadow/ouroboros-ide.git'\") upload=1; shift ;;\n" +
		" *) exit 93 ;;\n" +
		" esac\n" +
		"done\n" +
		"[ \"$host$port$upload\" = 111 ] || exit 94\n" +
		"printf '%s\\n' canonical >> " + q(log) + "\n" +
		"exec " + q(realGit) + " upload-pack " + q(remote) + "\n"
	if err := os.WriteFile(ssh, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return s, q(ssh), log
}

func TestCurrentSourceHistoricalOriginRefusalsDoNotAcquireOrChangeConfiguration(t *testing.T) {
	for _, kind := range []string{"foreign-owner", "foreign-repository", "leading-space", "trailing-space", "newline", "duplicate-origin", "empty-origin", "effective-rewrite", "unrelated-rewrite", "push-rewrite", "scp-lane-marker"} {
		t.Run(kind, func(t *testing.T) {
			s, _, remote := sourceFixture(t, kind == "scp-lane-marker")
			s, ssh, log := sourceFixtureHistoricalOrigin(t, s, remote)
			common := sourceTestGit(t, s.Worktree, "rev-parse", "--path-format=absolute", "--git-common-dir")
			switch kind {
			case "foreign-owner":
				sourceTestGit(t, s.Worktree, "remote", "set-url", "origin", "git@github.com:Other/ouroboros-ide.git")
			case "foreign-repository":
				sourceTestGit(t, s.Worktree, "remote", "set-url", "origin", "git@github.com:Divine-Shadow/other.git")
			case "leading-space":
				sourceTestGit(t, s.Worktree, "remote", "set-url", "origin", " "+sourceHistoricalOrigin)
			case "trailing-space":
				sourceTestGit(t, s.Worktree, "remote", "set-url", "origin", sourceHistoricalOrigin+" ")
			case "newline":
				sourceTestGit(t, s.Worktree, "remote", "set-url", "origin", sourceHistoricalOrigin+"\n")
			case "duplicate-origin":
				sourceTestGit(t, s.Worktree, "config", "--add", "remote.origin.url", sourceHistoricalOrigin)
			case "empty-origin":
				sourceTestGit(t, s.Worktree, "config", "remote.origin.url", "")
			case "effective-rewrite":
				sourceTestGit(t, s.Worktree, "config", "url."+sourceCanonicalOrigin+".insteadOf", sourceHistoricalOrigin)
			case "unrelated-rewrite":
				sourceTestGit(t, s.Worktree, "config", "url.https://foreign.invalid/.insteadOf", "https://unused.invalid/")
			case "push-rewrite":
				sourceTestGit(t, s.Worktree, "config", "url.https://foreign.invalid/.pushInsteadOf", sourceHistoricalOrigin)
			case "scp-lane-marker":
				marker, err := nativeOwnedCommonRepositoryMarker("ouroboros-ide", sourceHistoricalOrigin, s.Index)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(common, "devkit-owned-common"), []byte(marker), 0600); err != nil {
					t.Fatal(err)
				}
			}
			configPath := filepath.Join(common, "config")
			config, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			beforeRefs := sourceTestGit(t, s.Worktree, "for-each-ref", "--format=%(refname)|%(symref)|%(objectname)")
			result, err := PrepareCurrentSource(s, false, ssh, false)
			if err == nil || result.TrackingUpdate != "not-started" || result.Acquired != "" || result.Previous != "" || result.CommonDirectory != "" {
				t.Fatalf("origin refusal did not precede acquisition: %#v %v", result, err)
			}
			if _, err := os.Lstat(log); !os.IsNotExist(err) {
				t.Fatal("refused source contacted the remote")
			}
			after, err := os.ReadFile(configPath)
			if err != nil || string(after) != string(config) {
				t.Fatal("refusal changed configuration")
			}
			if afterRefs := sourceTestGit(t, s.Worktree, "for-each-ref", "--format=%(refname)|%(symref)|%(objectname)"); afterRefs != beforeRefs {
				t.Fatal("refusal changed refs")
			}
		})
	}
}

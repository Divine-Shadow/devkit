package worktrees

import (
	"reflect"
	"testing"
)

func TestPackageGitCommandBindsCoreutilsEnvApplet(t *testing.T) {
	t.Parallel()
	got := packageGitCommand(
		"/nix/store/fixture-coreutils/bin/coreutils",
		"/nix/store/fixture-git/bin/git",
		"",
		"status",
	)
	wantPrefix := []string{
		"--coreutils-prog=env",
		"-i",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"/nix/store/fixture-git/bin/git",
	}
	if len(got) < len(wantPrefix) || !reflect.DeepEqual(got[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("package Git argv prefix = %#v, want %#v", got, wantPrefix)
	}
}

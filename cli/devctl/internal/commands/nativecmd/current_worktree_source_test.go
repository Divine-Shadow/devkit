package nativecmd

import "testing"

func TestCurrentSourceRejectsCallerSelectors(t *testing.T) {
	base := []string{"--current-worktree", "--source-only", "--repo", "ouroboros-ide"}
	if err := parseCurrentSourceArgs(base); err != nil {
		t.Fatal(err)
	}
	for _, extra := range [][]string{{"--index", "2"}, {"--workspace-root", "/foreign"}, {"--origin", "foreign"}, {"--base-branch", "other"}, {"--repo-check", "git fetch"}, {"--", "bash"}} {
		if err := parseCurrentSourceArgs(append(append([]string{}, base...), extra...)); err == nil {
			t.Fatalf("accepted %v", extra)
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

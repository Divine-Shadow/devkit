package main

import "testing"

func TestParseLoginShellInvocationAcceptsOnlyExactSSHDShape(t *testing.T) {
	command := "/nix/store/session/bin/product-ssh-session force-command --count 2 --index 1"
	count, index, observed, err := parseLoginShellInvocation([]string{"-c", command})
	if err != nil || count != 2 || index != 1 || observed != command {
		t.Fatalf("exact sshd shell invocation rejected: %d/%d %q %v", count, index, observed, err)
	}
	invalid := [][]string{
		nil,
		{"force-command", "--count", "2", "--index", "1"},
		{"-c", command, "extra"},
		{"-c", command + "; id"},
		{"-c", "/nix/store/session/bin/product-ssh-session force-command --count 02 --index 1"},
		{"-c", "/nix/store/session/bin/product-ssh-session force-command --index 1 --count 2"},
	}
	for _, args := range invalid {
		if _, _, _, err := parseLoginShellInvocation(args); err == nil {
			t.Fatalf("accepted invalid account-shell argv %#v", args)
		}
	}
}

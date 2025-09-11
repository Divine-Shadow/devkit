package sshcfg

// BuildGitHubConfig returns an SSH config that routes github.com via proxy
// and uses a per-agent HOME for keys/known_hosts. The `home` should be the
// per-agent HOME (e.g., /workspace/.devhome-agentN).
func BuildGitHubConfig(home string) string {
	return "Host github.com\n" +
		"  HostName ssh.github.com\n" +
		"  Port 443\n" +
		"  User git\n" +
		"  ProxyCommand nc -X connect -x tinyproxy:8888 %h %p\n" +
		"  IdentityFile '" + home + "'/.ssh/id_ed25519\n" +
		"  IdentitiesOnly yes\n" +
		"  StrictHostKeyChecking accept-new\n" +
		"  UserKnownHostsFile '" + home + "'/.ssh/known_hosts\n"
}

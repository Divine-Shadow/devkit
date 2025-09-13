package sshcfg

// BuildGitHubConfig returns an SSH config that routes github.com via proxy
// and uses a per-agent HOME for keys/known_hosts. The `home` should be the
// per-agent HOME (e.g., /workspace/.devhome-agentN).
func BuildGitHubConfig(home string) string { return BuildGitHubConfigFor(home, true, false) }

// BuildGitHubConfigFor builds a proxy-aware SSH config for GitHub with explicit IdentityFile lines
// for any keys that were actually copied into the agent (ed25519 and/or rsa).
func BuildGitHubConfigFor(home string, ed25519, rsa bool) string {
    cfg := "Host github.com\n" +
        "  HostName ssh.github.com\n" +
        "  Port 443\n" +
        "  User git\n" +
        "  ProxyCommand nc -X connect -x tinyproxy:8888 %h %p\n"
    if ed25519 {
        cfg += "  IdentityFile " + home + "/.ssh/id_ed25519\n"
    }
    if rsa {
        cfg += "  IdentityFile " + home + "/.ssh/id_rsa\n"
    }
    cfg += "  IdentitiesOnly yes\n" +
        "  StrictHostKeyChecking accept-new\n" +
        "  UserKnownHostsFile " + home + "/.ssh/known_hosts\n"
    return cfg
}

// BuildGitHubConfigMany builds a config with multiple IdentityFile entries.
func BuildGitHubConfigMany(home string, files []string) string {
    cfg := "Host github.com\n" +
        "  HostName ssh.github.com\n" +
        "  Port 443\n" +
        "  User git\n" +
        "  ProxyCommand nc -X connect -x tinyproxy:8888 %h %p\n"
    for _, f := range files {
        if f == "" { continue }
        cfg += "  IdentityFile " + home + "/.ssh/" + f + "\n"
    }
    cfg += "  IdentitiesOnly yes\n" +
        "  StrictHostKeyChecking accept-new\n" +
        "  UserKnownHostsFile " + home + "/.ssh/known_hosts\n"
    return cfg
}

// BuildGitHubConfigTilde returns an SSH config using tilde paths so it follows $HOME,
// avoiding absolute, index-specific paths.
func BuildGitHubConfigTilde() string {
    return "Host github.com\n" +
        "  HostName ssh.github.com\n" +
        "  Port 443\n" +
        "  User git\n" +
        "  ProxyCommand nc -X connect -x tinyproxy:8888 %h %p\n" +
        "  IdentityFile ~/.ssh/id_ed25519\n" +
        "  IdentitiesOnly yes\n" +
        "  StrictHostKeyChecking accept-new\n" +
        "  UserKnownHostsFile ~/.ssh/known_hosts\n"
}

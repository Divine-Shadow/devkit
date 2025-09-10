package sshsteps

// Small helpers that return minimal bash snippets to avoid large inline scripts.

// MkdirSSH returns a command to create ~/.ssh with safe perms.
func MkdirSSH(home string) string {
    return "mkdir -p '" + home + "'/.ssh && chmod 700 '" + home + "'/.ssh"
}

// WaitConfigNonEmpty waits until ~/.ssh/config exists and is non-empty.
func WaitConfigNonEmpty(home string) string {
    return "for i in $(seq 1 20); do [ -s '" + home + "'/.ssh/config ] && break || sleep 0.25; done"
}

// GitSetGlobalSSH sets git global core.sshCommand to use the per-agent config.
func GitSetGlobalSSH(home string) string {
    return "export HOME='" + home + "' && git config --global core.sshCommand 'ssh -F '" + home + "'/.ssh/config'"
}

// GitSetRepoSSH sets repo-level core.sshCommand in the given repo path.
func GitSetRepoSSH(repoPath, home string) string {
    return "cd '" + repoPath + "' && git config core.sshCommand 'ssh -F '" + home + "'/.ssh/config'"
}

// GitPullWithSSH runs git pull --ff-only with the per-agent SSH config.
func GitPullWithSSH(repoPath, home string) string {
    return "set -e; cd '" + repoPath + "'; GIT_SSH_COMMAND=\"ssh -F '" + home + "'/.ssh/config\" git pull --ff-only || true"
}

// Paths to common SSH files under the agent home
func PrivateKeyPath(home string) string { return home + "/.ssh/id_ed25519" }
func PublicKeyPath(home string) string  { return home + "/.ssh/id_ed25519.pub" }
func KnownHostsPath(home string) string { return home + "/.ssh/known_hosts" }
func ConfigPath(home string) string     { return home + "/.ssh/config" }

// WriteCmd returns a tiny shell snippet to write to 'path' via stdin then chmod to mode.
func WriteCmd(path string, mode string) string {
    return "cat > '" + path + "' && chmod " + mode + " '" + path + "'"
}

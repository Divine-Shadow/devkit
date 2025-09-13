package ssh

// WriteStep represents a content write into a file accompanied by a chmod.
// The Script should be a tiny bash -lc snippet that reads from stdin and writes the file.
type WriteStep struct {
	Content []byte
	Script  string
}

// BuildWriteSteps constructs WriteSteps to write SSH private/public keys, known_hosts, and config
// into the agent's HOME, with correct permissions. Nil/empty contents are skipped.
func BuildWriteSteps(home string, key, pub, known []byte, cfg string) []WriteStep {
	steps := make([]WriteStep, 0, 4)
	if len(key) > 0 {
		steps = append(steps, WriteStep{Content: key, Script: "cat > '" + home + "'/.ssh/id_ed25519 && chmod 600 '" + home + "'/.ssh/id_ed25519"})
	}
	if len(pub) > 0 {
		steps = append(steps, WriteStep{Content: pub, Script: "cat > '" + home + "'/.ssh/id_ed25519.pub && chmod 644 '" + home + "'/.ssh/id_ed25519.pub"})
	}
	if len(known) > 0 {
		steps = append(steps, WriteStep{Content: known, Script: "cat > '" + home + "'/.ssh/known_hosts && chmod 644 '" + home + "'/.ssh/known_hosts"})
	}
	if cfg != "" {
		steps = append(steps, WriteStep{Content: []byte(cfg), Script: "cat > '" + home + "'/.ssh/config && chmod 600 '" + home + "'/.ssh/config"})
	}
	return steps
}

// BuildConfigureScripts returns small bash -lc scripts to configure git
// for the given agent HOME and repository path inside the container.
// Includes: wait for ~/.ssh/config to be non-empty, set global core.sshCommand,
// set repo-level core.sshCommand, and git pull --ff-only using the config.
func BuildConfigureScripts(home string, repoPath string) []string {
    // Use tilde-based ssh config anchored at the provided home, set global core.sshCommand,
    // scrub any repo-local override, then validate pull via explicit GIT_SSH_COMMAND.
    return []string{
        "home=\"" + home + "\"; for i in $(seq 1 20); do [ -s \"$home/.ssh/config\" ] && break || sleep 0.25; done",
        "home=\"" + home + "\"; HOME=\"$home\" git config --global core.sshCommand \"ssh -F ~/.ssh/config\"",
        "cd '" + repoPath + "' && git config --unset core.sshCommand || true",
        "home=\"" + home + "\"; set -e; cd '" + repoPath + "'; HOME=\"$home\" GIT_SSH_COMMAND=\"ssh -F ~/.ssh/config\" git pull --ff-only || true",
    }
}

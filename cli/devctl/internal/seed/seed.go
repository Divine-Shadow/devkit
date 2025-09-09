package seed

// WaitForHostMountsScript returns a script that waits (up to ~10s) for
// /var/host-codex or /var/auth.json to become available.
func WaitForHostMountsScript() string {
    return `for i in $(seq 1 20); do { [ -d /var/host-codex ] || [ -f /var/auth.json ]; } && break || sleep 0.5; done`
}

// ResetAndCreateDirsScript resets $HOME/.codex and ensures auxiliary dirs exist.
func ResetAndCreateDirsScript(home string) string {
    h := home
    return `rm -rf '` + h + `/.codex' && mkdir -p '` + h + `/.codex' '` + h + `/.codex/rollouts' '` + h + `/.cache' '` + h + `/.config' '` + h + `/.local'`
}

// CloneHostCodexScript clones the entire /var/host-codex into $HOME/.codex (if present).
func CloneHostCodexScript(home string) string {
    h := home
    return `if [ -d /var/host-codex ]; then cp -a /var/host-codex/. '` + h + `/.codex/'; fi`
}

// FallbackCopyAuthScript copies /var/auth.json into $HOME/.codex/auth.json if still missing.
func FallbackCopyAuthScript(home string) string {
    h := home
    return `if [ ! -f '` + h + `/.codex/auth.json' ] && [ -r /var/auth.json ]; then cp -f /var/auth.json '` + h + `/.codex/auth.json'; fi`
}

// TightenPermsScript chmods 600 on $HOME/.codex/auth.json if present.
func TightenPermsScript(home string) string {
    h := home
    return `if [ -f '` + h + `/.codex/auth.json' ]; then chmod 600 '` + h + `/.codex/auth.json'; fi`
}

// BuildSeedScripts returns a sequence of small bash scripts that, when run
// inside the agent container (via `bash -lc`), refresh the per‑agent Codex HOME
// from host mounts.
func BuildSeedScripts(home string) []string {
    return []string{
        WaitForHostMountsScript(),
        ResetAndCreateDirsScript(home),
        CloneHostCodexScript(home),
        FallbackCopyAuthScript(home),
        TightenPermsScript(home),
    }
}

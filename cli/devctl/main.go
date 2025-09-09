package main

import (
    "fmt"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "time"

    "devkit/cli/devctl/internal/compose"
    "devkit/cli/devctl/internal/config"
    "devkit/cli/devctl/internal/netutil"
    fz "devkit/cli/devctl/internal/files"
    "devkit/cli/devctl/internal/execx"
)

func usage() {
    fmt.Fprintf(os.Stderr, `devctl (Go) — experimental
Usage: devctl -p <project> [--profile <profiles>] <command> [args]

Commands:
  up, down, restart, status, logs
  scale N, exec <n> <cmd...>, attach <n>
  allow <domain>, warm, maintain, check-net
  proxy {tinyproxy|envoy}
  tmux-shells [N], open [N], fresh-open [N]
  exec-cd <index> <subpath> [cmd...], attach-cd <index> <subpath>
  ssh-setup [--key path] [--index N], ssh-test [N]
  repo-config-ssh <repo> [--index N], repo-push-ssh <repo> [--index N]
  repo-config-https <repo> [--index N], repo-push-https <repo> [--index N]
  worktrees-init <repo> <count> [--base agent] [--branch main]
  worktrees-branch <repo> <index> <branch>   (dev-all)
  worktrees-status <repo> [--all|--index N]  (dev-all)
  worktrees-sync <repo> (--pull|--push) [--all|--index N]  (dev-all)
  worktrees-tmux <repo> <count>              (dev-all)
  bootstrap <repo> <count>                   (dev-all)

Flags:
  -p, --project   overlay project name (required for most)
  --profile       comma-separated: hardened,dns,envoy (default: dns)

Environment:
  DEVKIT_DEBUG=1  print executed commands
`)
}

func main() {
    var project string
    var profile string
    var dryRun bool

    // rudimentary -p/--project and --profile parsing before subcmd
    args := os.Args[1:]
    out := make([]string, 0, len(args))
    for i := 0; i < len(args); i++ {
        a := args[i]
        switch a {
        case "-p", "--project":
            if i+1 >= len(args) { fmt.Fprintln(os.Stderr, "-p requires value"); os.Exit(2) }
            project = args[i+1]
            i++
        case "--profile":
            if i+1 >= len(args) { fmt.Fprintln(os.Stderr, "--profile requires value"); os.Exit(2) }
            profile = args[i+1]
            i++
        case "--dry-run":
            dryRun = true
        case "-h", "--help", "help":
            usage(); return
        default:
            out = append(out, a)
        }
    }
    args = out
    if len(args) == 0 {
        usage(); os.Exit(2)
    }

    exe, _ := os.Executable()
    paths, _ := compose.DetectPathsFromExe(exe)
    files, err := compose.Files(paths, project, profile)
    if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(2) }

    // Preflight: choose a non-overlapping internal subnet and DNS IP if not explicitly set
    cidr, dns := netutil.PickInternalSubnet()
    // Export so docker compose can substitute in compose.dns.yml
    _ = os.Setenv("DEVKIT_INTERNAL_SUBNET", cidr)
    _ = os.Setenv("DEVKIT_DNS_IP", dns)
    if os.Getenv("DEVKIT_DEBUG") == "1" {
        fmt.Fprintf(os.Stderr, "[devctl] internal subnet=%s dns_ip=%s\n", cidr, dns)
    }

    cmd := args[0]
    sub := args[1:]
    switch cmd {
    case "up":
        mustProject(project)
        runCompose(dryRun, files, "up", "-d")
    case "down":
        mustProject(project)
        runCompose(dryRun, files, "down")
    case "restart":
        mustProject(project)
        runCompose(dryRun, files, "restart")
    case "status":
        mustProject(project)
        runCompose(dryRun, files, "ps")
    case "logs":
        mustProject(project)
        runCompose(dryRun, files, append([]string{"logs"}, sub...)...)
    case "scale":
        mustProject(project)
        n := "1"
        if len(sub) > 0 { n = sub[0] }
        runCompose(dryRun, files, "up", "-d", "--scale", "dev-agent="+n)
    case "exec":
        mustProject(project)
        if len(sub) == 0 { die("exec requires <index> and <cmd>") }
        idx := sub[0]
        rest := []string{}
        if len(sub) > 1 { rest = sub[1:] }
        // Provide per-agent HOME and CODEX_HOME so codex reads/writes to a writable, isolated path
        home := "/workspace/.devhome-agent" + idx
        runCompose(dryRun, files, append([]string{
            "exec", "--index", idx,
            "-e", "HOME=" + home,
            "-e", "CODEX_HOME=" + home + "/.codex",
            "-e", "CODEX_ROLLOUT_DIR=" + home + "/.codex/rollouts",
            "-e", "XDG_CACHE_HOME=" + home + "/.cache",
            "-e", "XDG_CONFIG_HOME=" + home + "/.config",
            "dev-agent"}, rest...)...)
    case "attach":
        mustProject(project)
        idx := "1"
        if len(sub) > 0 { idx = sub[0] }
        runCompose(dryRun, files, "attach", "--index", idx, "dev-agent")
    case "allow":
        mustProject(project)
        if len(sub) == 0 { die("allow requires <domain>") }
        domain := strings.TrimSpace(sub[0])
        // proxy allowlist
        added1, err1 := fz.AppendLineIfMissing(filepath.Join(paths.Kit, "proxy", "allowlist.txt"), domain)
        // dns allowlist
        dnsRule := fmt.Sprintf("server=/%s/1.1.1.1", domain)
        added2, err2 := fz.AppendLineIfMissing(filepath.Join(paths.Kit, "dns", "dnsmasq.conf"), dnsRule)
        if err1 != nil || err2 != nil {
            if err1 != nil { fmt.Fprintln(os.Stderr, "allowlist:", err1) }
            if err2 != nil { fmt.Fprintln(os.Stderr, "dnsmasq:", err2) }
            os.Exit(1)
        }
        if added1 { fmt.Println("Added to proxy allowlist:", domain) } else { fmt.Println("Already in proxy allowlist:", domain) }
        if added2 { fmt.Println("Added to DNS allowlist:", domain) } else { fmt.Println("Already in DNS allowlist:", domain) }
        fmt.Printf("Note: restart dns and proxy to apply (devctl -p %s restart)\n", project)
    case "proxy":
        mustProject(project)
        which := "tinyproxy"
        if len(sub) > 0 && strings.TrimSpace(sub[0]) != "" {
            which = sub[0]
        }
        switch which {
        case "tinyproxy":
            fmt.Println("Switching agent env to tinyproxy... (ensure overlay uses HTTP(S)_PROXY=http://tinyproxy:8888)")
        case "envoy":
            fmt.Println("Enable envoy profile: add --profile envoy to up/restart commands")
        default:
            die("unknown proxy: " + which)
        }
    case "warm":
        mustProject(project)
        hooks, _ := config.ReadHooks(paths.Root, project)
        if strings.TrimSpace(hooks.Warm) == "" {
            fmt.Println("No warm hook defined")
            return
        }
        runCompose(dryRun, files, "exec", "dev-agent", "bash", "-lc", hooks.Warm)
    case "maintain":
        mustProject(project)
        hooks, _ := config.ReadHooks(paths.Root, project)
        if strings.TrimSpace(hooks.Maintain) == "" {
            fmt.Println("No maintain hook defined")
            return
        }
        runCompose(dryRun, files, "exec", "dev-agent", "bash", "-lc", hooks.Maintain)
    case "check-net":
        mustProject(project)
        script := "set -x; env | grep -E 'HTTP(S)?_PROXY|NO_PROXY'; curl -Is https://github.com | head -n1; (curl -Is https://example.com | head -n1 || true)"
        runCompose(dryRun, files, "exec", "dev-agent", "bash", "-lc", script)
    case "check-codex":
        mustProject(project)
        fmt.Println("== Env vars ==")
        runCompose(dryRun, files, "exec", "dev-agent", "bash", "-lc", "env | grep -E '^HTTPS?_PROXY=|^NO_PROXY=' || true")
        fmt.Println("== Curl checks (through proxy) ==")
        runCompose(dryRun, files, "exec", "dev-agent", "bash", "-lc", "set -e; echo -n 'chatgpt.com          : '; curl -sSvo /dev/null -w '%{http_code}\\n' https://chatgpt.com || true")
        runCompose(dryRun, files, "exec", "dev-agent", "bash", "-lc", "set -e; echo -n 'chatgpt.com/backend..: '; curl -sSvo /dev/null -w '%{http_code}\\n' https://chatgpt.com/backend-api/codex/responses || true")
        // attempt to run codex binary if present
        runCompose(dryRun, files, "exec", "dev-agent", "bash", "-lc", "mkdir -p /workspace/.devhome; HOME=/workspace/.devhome CODEX_HOME=/workspace/.devhome/.codex timeout 15s codex exec 'Reply with: ok' || true")
    case "codex-debug":
        mustProject(project)
        idx := "1"; if len(sub) > 0 { idx = sub[0] }
        script := `set -e
echo "HOME=$HOME"; echo "CODEX_HOME=$CODEX_HOME"
echo "-- locations --"
for p in "$HOME/.codex/auth.json" "$CODEX_HOME/auth.json" "/var/auth.json" "/var/host-codex/auth.json"; do
  [ -n "$p" ] || continue; echo -n "$p : "; [ -f "$p" ] && { stat -c '%s bytes' "$p" 2>/dev/null || wc -c < "$p"; } || echo "(missing)"; done
exit 0`
        runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", script)
    case "check-claude":
        mustProject(project)
        idx := "1"
        if len(sub) > 0 { idx = sub[0] }
        fmt.Println("== Env vars ==")
        runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "env | grep -E '^HTTPS?_PROXY=|^NO_PROXY=' || true")
        fmt.Println("== Curl checks (through proxy) ==")
        runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "set -e; echo -n 'claude.ai            : '; curl -sSvo /dev/null -w '%{http_code}\\n' https://claude.ai || true")
        runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "set -e; echo -n 'anthropic.com        : '; curl -sSvo /dev/null -w '%{http_code}\\n' https://www.anthropic.com || true")
        home := fmt.Sprintf("/workspace/.devhome-agent%s", idx)
        runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "mkdir -p '"+home+"'; HOME='"+home+"' timeout 15s claude --version || claude --help || true")
    case "check-sts":
        mustProject(project)
        which := "envoy"
        if len(sub) > 0 { which = strings.TrimSpace(sub[0]) }
        var px string
        switch which {
        case "envoy": px = "http://envoy:3128"
        case "tinyproxy": px = "http://tinyproxy:8888"
        default: die("Usage: check-sts [envoy|tinyproxy]")
        }
        runCompose(dryRun, files, "exec", "dev-agent", "bash", "-lc", "HTTPS_PROXY='"+px+"' HTTP_PROXY='"+px+"' curl -sSvo /dev/null -w '%{http_code}\\n' https://sts.amazonaws.com || true")
        runCompose(dryRun, files, "exec", "dev-agent", "bash", "-lc", "HTTPS_PROXY='"+px+"' HTTP_PROXY='"+px+"' aws sts get-caller-identity || true")
        runCompose(dryRun, files, "exec", "dev-agent", "bash", "-lc", "curl -sSvo /dev/null -w '%{http_code}\\n' https://sts.amazonaws.com || true")
        runCompose(dryRun, files, "exec", "dev-agent", "bash", "-lc", "aws sts get-caller-identity || true")
    case "exec-cd":
        mustProject(project)
        if len(sub) < 2 { die("Usage: exec-cd <index> <subpath> [cmd...]") }
        idx := sub[0]; subpath := sub[1]
        dest := subpath
        if !strings.HasPrefix(subpath, "/") {
            dest = filepath.Join("/workspaces/dev", subpath)
        }
        cmdstr := "bash"
        if len(sub) > 2 { cmdstr = strings.Join(sub[2:], " ") }
        runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "cd '"+dest+"' && exec "+cmdstr)
    case "attach-cd":
        mustProject(project)
        if len(sub) < 2 { die("Usage: attach-cd <index> <subpath>") }
        idx := sub[0]; subpath := sub[1]
        dest := subpath
        if !strings.HasPrefix(subpath, "/") { dest = filepath.Join("/workspaces/dev", subpath) }
        runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "cd '"+dest+"' && exec bash")
    case "tmux-shells":
        mustProject(project)
        n := 2; if len(sub) > 0 { n = mustAtoi(sub[0]) }
        runCompose(dryRun, files, "up", "-d", "--scale", fmt.Sprintf("dev-agent=%d", n))
        // best-effort ssh-setup per agent
        if !dryRun {
            // loop via invoking self for simplicity is skipped; rely on user to run ssh-setup if needed
        }
        sess := "devkit-shells"
        // window 1
        home1 := "/workspace/.devhome-agent1"
        if !skipTmux() {
            runHost(dryRun, "tmux", "new-session", "-d", "-s", sess, "docker compose "+strings.Join(files, " ")+" exec --index 1 dev-agent bash -lc 'mkdir -p \""+home1+"/.codex/rollouts\" \""+home1+"/.cache\" \""+home1+"/.config\" \""+home1+"/.local\"; export HOME=\""+home1+"\"; export CODEX_HOME=\""+home1+"/.codex\"; export CODEX_ROLLOUT_DIR=\""+home1+"/.codex/rollouts\"; export XDG_CACHE_HOME=\""+home1+"/.cache\"; export XDG_CONFIG_HOME=\""+home1+"/.config\"; cd /workspace; exec bash'")
            runHost(dryRun, "tmux", "rename-window", "-t", sess+":0", "agent-1")
            for i := 2; i <= n; i++ {
                homei := fmt.Sprintf("/workspace/.devhome-agent%d", i)
                runHost(dryRun, "tmux", "new-window", "-t", sess, "-n", fmt.Sprintf("agent-%d", i), "docker compose "+strings.Join(files, " ")+fmt.Sprintf(" exec --index %d dev-agent bash -lc 'mkdir -p \"%s/.codex/rollouts\" \"%s/.cache\" \"%s/.config\" \"%s/.local\"; export HOME=\"%s\"; export CODEX_HOME=\"%s/.codex\"; export CODEX_ROLLOUT_DIR=\"%s/.codex/rollouts\"; export XDG_CACHE_HOME=\"%s/.cache\"; export XDG_CONFIG_HOME=\"%s/.config\"; cd /workspace; exec bash'", i, homei, homei, homei, homei, homei, homei, homei, homei, homei))
            }
            runHost(dryRun, "tmux", "attach", "-t", sess)
        }
    case "open":
        mustProject(project)
        n := 2; if len(sub) > 0 { n = mustAtoi(sub[0]) }
        runCompose(dryRun, files, "up", "-d", "--scale", fmt.Sprintf("dev-agent=%d", n))
        sess := "devkit-open"
        home1 := "/workspace/.devhome-agent1"
        if !skipTmux() {
            runHost(dryRun, "tmux", "new-session", "-d", "-s", sess, "docker compose "+strings.Join(files, " ")+" exec --index 1 dev-agent bash -lc 'mkdir -p \""+home1+"/.codex/rollouts\" \""+home1+"/.cache\" \""+home1+"/.config\" \""+home1+"/.local\"; export HOME=\""+home1+"\"; export CODEX_HOME=\""+home1+"/.codex\"; export CODEX_ROLLOUT_DIR=\""+home1+"/.codex/rollouts\"; export XDG_CACHE_HOME=\""+home1+"/.cache\"; export XDG_CONFIG_HOME=\""+home1+"/.config\"; cd /workspace; exec bash'")
            runHost(dryRun, "tmux", "rename-window", "-t", sess+":0", "agent-1")
            for i := 2; i <= n; i++ {
                homei := fmt.Sprintf("/workspace/.devhome-agent%d", i)
                runHost(dryRun, "tmux", "new-window", "-t", sess, "-n", fmt.Sprintf("agent-%d", i), "docker compose "+strings.Join(files, " ")+fmt.Sprintf(" exec --index %d dev-agent bash -lc 'mkdir -p \"%s/.codex/rollouts\" \"%s/.cache\" \"%s/.config\" \"%s/.local\"; export HOME=\"%s\"; export CODEX_HOME=\"%s/.codex\"; export CODEX_ROLLOUT_DIR=\"%s/.codex/rollouts\"; export XDG_CACHE_HOME=\"%s/.cache\"; export XDG_CONFIG_HOME=\"%s/.config\"; cd /workspace; exec bash'", i, homei, homei, homei, homei, homei, homei, homei, homei, homei))
            }
            runHost(dryRun, "tmux", "attach", "-t", sess)
        }
    case "fresh-open":
        mustProject(project)
        n := 3; if len(sub) > 0 { n = mustAtoi(sub[0]) }
        all := compose.AllProfilesFiles(paths, project)
        // bring everything down and cleanup
        runCompose(dryRun, all, "down")
        if !skipTmux() { runHostBestEffort(dryRun, "tmux", "kill-session", "-t", "devkit-open") }
        if !skipTmux() { runHostBestEffort(dryRun, "tmux", "kill-session", "-t", "devkit-shells") }
        if !skipTmux() { runHostBestEffort(dryRun, "tmux", "kill-session", "-t", "devkit-worktrees") }
        runHostBestEffort(dryRun, "docker", "rm", "-f", "devkit_envoy", "devkit_envoy_sni", "devkit_dns", "devkit_tinyproxy")
        runHostBestEffort(dryRun, "docker", "network", "rm", "devkit_dev-internal", "devkit_dev-egress")
        // start up with all profiles
        runCompose(dryRun, all, "up", "-d", "--scale", fmt.Sprintf("dev-agent=%d", n))
        // best-effort: seed per-agent Codex auth from /var/auth.json or /var/host-codex/auth.json if present
        for j := 1; j <= n; j++ {
            homej := fmt.Sprintf("/workspace/.devhome-agent%d", j)
            seed := strings.Join([]string{
                // Ensure base dirs
                fmt.Sprintf("mkdir -p '%s'/.codex '{0}/.codex/rollouts' '{0}/.cache' '{0}/.config' '{0}/.local'", homej),
                // Seed auth.json from either /var/auth.json or /var/host-codex/auth.json
                "SRC=; if [ -r /var/auth.json ]; then SRC=/var/auth.json; elif [ -r /var/host-codex/auth.json ]; then SRC=/var/host-codex/auth.json; fi",
                fmt.Sprintf("if [ -n \"$SRC\" ] && [ ! -f '%s'/.codex/auth.json ]; then cp -f \"$SRC\" '%s'/.codex/auth.json; fi", homej, homej),
                // Seed sessions directory if present on host and missing in agent
                "if [ -d /var/host-codex/sessions ]; then ",
                fmt.Sprintf("  mkdir -p '%s'/.codex/sessions; ", homej),
                fmt.Sprintf("  if [ -z \"$(ls -A '%s'/.codex/sessions 2>/dev/null)\" ]; then cp -a /var/host-codex/sessions/. '%s'/.codex/sessions/; fi; ", homej, homej),
                "fi",
                // Seed config.toml if present
                fmt.Sprintf("if [ -r /var/host-codex/config.toml ] && [ ! -f '%s'/.codex/config.toml ]; then cp -f /var/host-codex/config.toml '%s'/.codex/config.toml; fi", homej, homej),
                // If entire host codex dir exists and agent CODEX_HOME is effectively empty (no files), clone it wholesale
                fmt.Sprintf("if [ -d /var/host-codex ] && [ -z \"$(find '%s'/.codex -type f -maxdepth 1 2>/dev/null)\" ]; then cp -a /var/host-codex/. '%s'/.codex/; fi", homej, homej),
            }, " && ")
            runCompose(dryRun, all, "exec", "-T", "--index", fmt.Sprintf("%d", j), "dev-agent", "bash", "-lc", seed)
        }
        // tmux session
        if !skipTmux() {
            sess := "devkit-open"
            home1 := "/workspace/.devhome-agent1"
            runHost(dryRun, "tmux", "new-session", "-d", "-s", sess, "docker compose "+strings.Join(all, " ")+" exec --index 1 dev-agent bash -lc 'mkdir -p \""+home1+"/.codex/rollouts\" \""+home1+"/.cache\" \""+home1+"/.config\" \""+home1+"/.local\"; export HOME=\""+home1+"\"; export CODEX_HOME=\""+home1+"/.codex\"; export CODEX_ROLLOUT_DIR=\""+home1+"/.codex/rollouts\"; export XDG_CACHE_HOME=\""+home1+"/.cache\"; export XDG_CONFIG_HOME=\""+home1+"/.config\"; cd /workspace; exec bash'")
            runHost(dryRun, "tmux", "rename-window", "-t", sess+":0", "agent-1")
            for i := 2; i <= n; i++ {
                homei := fmt.Sprintf("/workspace/.devhome-agent%d", i)
                runHost(dryRun, "tmux", "new-window", "-t", sess, "-n", fmt.Sprintf("agent-%d", i), "docker compose "+strings.Join(all, " ")+fmt.Sprintf(" exec --index %d dev-agent bash -lc 'mkdir -p \"%s/.codex/rollouts\" \"%s/.cache\" \"%s/.config\" \"%s/.local\"; export HOME=\"%s\"; export CODEX_HOME=\"%s/.codex\"; export CODEX_ROLLOUT_DIR=\"%s/.codex/rollouts\"; export XDG_CACHE_HOME=\"%s/.cache\"; export XDG_CONFIG_HOME=\"%s/.config\"; cd /workspace; exec bash'", i, homei, homei, homei, homei, homei, homei, homei, homei, homei))
            }
            runHost(dryRun, "tmux", "attach", "-t", sess)
        }
    case "ssh-setup":
        mustProject(project)
        // Parse flags: [--key path] [--index N]
        idx := "1"
        keyfile := ""
        for i := 0; i < len(sub); i++ {
            switch sub[i] {
            case "--key":
                if i+1 < len(sub) { keyfile = sub[i+1]; i++ }
            case "--index":
                if i+1 < len(sub) { idx = sub[i+1]; i++ }
            default:
                if keyfile == "" { keyfile = sub[i] }
            }
        }
        hostKey := keyfile
        if strings.TrimSpace(hostKey) == "" { hostKey = filepath.Join(os.Getenv("HOME"), ".ssh", "id_ed25519") }
        if _, err := os.Stat(hostKey); err != nil {
            // fallback to rsa
            hostKey = filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa")
        }
        if _, err := os.Stat(hostKey); err != nil {
            die("Host key not found: " + hostKey)
        }
        pubPath := hostKey + ".pub"
        pubData, err := os.ReadFile(pubPath)
        if err != nil || len(pubData) == 0 {
            die("Public key not found: " + pubPath)
        }
        // allowlist + restart proxy/dns
        _, _ = fz.AppendLineIfMissing(filepath.Join(paths.Kit, "proxy", "allowlist.txt"), "ssh.github.com")
        _, _ = fz.AppendLineIfMissing(filepath.Join(paths.Kit, "dns", "dnsmasq.conf"), "server=/ssh.github.com/1.1.1.1")
        runCompose(dryRun, files, "restart", "tinyproxy", "dns")
        home := "/workspace/.devhome-agent" + idx
        // mkdir .ssh
        runCompose(dryRun, files, "exec", "-T", "--index", idx, "dev-agent", "bash", "-lc", "mkdir -p '"+home+"'/.ssh && chmod 700 '"+home+"'/.ssh")
        // copy keys and known_hosts
        keyBytes, _ := os.ReadFile(hostKey)
        runComposeInput(dryRun, files, keyBytes, "exec", "-T", "--index", idx, "dev-agent", "bash", "-lc", "cat > '"+home+"'/.ssh/id_ed25519 && chmod 600 '"+home+"'/.ssh/id_ed25519")
        runComposeInput(dryRun, files, pubData, "exec", "-T", "--index", idx, "dev-agent", "bash", "-lc", "cat > '"+home+"'/.ssh/id_ed25519.pub && chmod 644 '"+home+"'/.ssh/id_ed25519.pub")
        known := filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts")
        if b, err := os.ReadFile(known); err == nil {
            runComposeInput(dryRun, files, b, "exec", "-T", "--index", idx, "dev-agent", "bash", "-lc", "cat > '"+home+"'/.ssh/known_hosts && chmod 644 '"+home+"'/.ssh/known_hosts")
        }
        // write SSH config
        cfg := "Host github.com\n  HostName ssh.github.com\n  Port 443\n  User git\n  ProxyCommand nc -X connect -x tinyproxy:8888 %h %p\n  IdentityFile '"+home+"'/.ssh/id_ed25519\n  IdentitiesOnly yes\n  StrictHostKeyChecking accept-new\n  UserKnownHostsFile '"+home+"'/.ssh/known_hosts\n"
        runComposeInput(dryRun, files, []byte(cfg), "exec", "-T", "--index", idx, "dev-agent", "bash", "-lc", "cat > '"+home+"'/.ssh/config && chmod 600 '"+home+"'/.ssh/config")
        // git config global sshCommand
        runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "export HOME='"+home+"' && git config --global core.sshCommand 'ssh -F '"+home+"'/.ssh/config'")
    case "ssh-test":
        mustProject(project)
        idx := "1"; if len(sub) > 0 { idx = sub[0] }
        home := "/workspace/.devhome-agent" + idx
        runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "export HOME='"+home+"'; ssh -F '"+home+"'/.ssh/config -T github.com -o BatchMode=yes || true")
    case "repo-config-ssh":
        mustProject(project)
        if len(sub) < 1 { die("Usage: repo-config-ssh <repo-path> [--index N]") }
        repo := sub[0]
        idx := "1"; if len(sub) >= 3 && sub[1] == "--index" { idx = sub[2] }
        base := "/workspace"; if project == "dev-all" { base = "/workspaces/dev" }
        dest := base + "/" + repo
        if repo == "." || repo == "" { dest = base }
        home := "/workspace/.devhome-agent" + idx
        cmd := "set -euo pipefail; export HOME='"+home+"'; cd '"+dest+"'; url=$(git remote get-url origin 2>/dev/null || true); if [ -z \"$url\" ]; then echo 'No origin remote configured' >&2; exit 1; fi; if [[ \"$url\" =~ ^https://github.com/([^/]+)/([^/.]+)(\\.git)?$ ]]; then newurl=git@github.com:${BASH_REMATCH[1]}/${BASH_REMATCH[2]}.git; echo Setting SSH origin to \"$newurl\"; git remote set-url origin \"$newurl\"; else echo \"Origin already SSH: $url\"; fi"
        runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", cmd)
    case "repo-config-https":
        mustProject(project)
        if len(sub) < 1 { die("Usage: repo-config-https <repo-path> [--index N]") }
        repo := sub[0]
        idx := "1"; if len(sub) >= 3 && sub[1] == "--index" { idx = sub[2] }
        base := "/workspace"; if project == "dev-all" { base = "/workspaces/dev" }
        dest := base + "/" + repo
        if repo == "." || repo == "" { dest = base }
        cmd := "set -euo pipefail; cd '"+dest+"'; url=$(git remote get-url origin 2>/dev/null || true); if [ -z \"$url\" ]; then echo 'No origin remote configured' >&2; exit 1; fi; if [[ \"$url\" =~ ^git@github.com:([^/]+)/([^/.]+)(\\.git)?$ ]]; then newurl=https://github.com/${BASH_REMATCH[1]}/${BASH_REMATCH[2]}.git; echo Setting HTTPS origin to \"$newurl\"; git remote set-url origin \"$newurl\"; else echo \"Origin already HTTPS: $url\"; fi"
        runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", cmd)
    case "repo-push-ssh":
        mustProject(project)
        if len(sub) < 1 { die("Usage: repo-push-ssh <repo-path> [--index N]") }
        repo := sub[0]
        idx := "1"; for i := 1; i+1 < len(sub); i++ { if sub[i] == "--index" { idx = sub[i+1] } }
        // best-effort ensure ssh
        // assemble dest and push
        base := "/workspace"; if project == "dev-all" { base = "/workspaces/dev" }
        dest := base + "/" + repo; if repo == "." || repo == "" { dest = base }
        home := "/workspace/.devhome-agent" + idx
        cmd := "set -euo pipefail; export HOME='"+home+"'; cd '"+dest+"'; cur=$(git rev-parse --abbrev-ref HEAD); url=$(git remote get-url origin 2>/dev/null || true); if [ -z \"$url\" ]; then echo 'No origin remote configured' >&2; exit 1; fi; if [[ \"$url\" =~ ^https://github.com/([^/]+)/([^/.]+)(\\.git)?$ ]]; then newurl=git@github.com:${BASH_REMATCH[1]}/${BASH_REMATCH[2]}.git; echo Setting SSH origin to \"$newurl\"; git remote set-url origin \"$newurl\"; fi; echo Pushing branch \"$cur\" to origin...; GIT_SSH_COMMAND=\"ssh -F '"+home+"'/.ssh/config\" git push -u origin HEAD"
        runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", cmd)
    case "repo-push-https":
        mustProject(project)
        if len(sub) < 1 { die("Usage: repo-push-https <repo-path> [--index N]") }
        repo := sub[0]
        idx := "1"; if len(sub) >= 3 && sub[1] == "--index" { idx = sub[2] }
        // ensure HTTPS config then push
        base := "/workspace"; if project == "dev-all" { base = "/workspaces/dev" }
        dest := base + "/" + repo; if repo == "." || repo == "" { dest = base }
        cmd := "set -euo pipefail; cd '"+dest+"'; echo Pushing branch $(git rev-parse --abbrev-ref HEAD) to origin...; git push -u origin HEAD"
        // call repo-config-https first? skipped for simplicity
        runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", cmd)
    case "worktrees-init":
        mustProject(project)
        if len(sub) < 2 { die("Usage: worktrees-init <repo> <count> [--base agent] [--branch main]") }
        repo := sub[0]; count := sub[1]
        base := "agent"; branch := "main"
        for i := 2; i+1 < len(sub); i++ { if sub[i] == "--base" { base = sub[i+1] } else if sub[i] == "--branch" { branch = sub[i+1] } }
        // create worktrees on host filesystem
        // primary at /workspaces/dev/<repo>, others at /workspaces/dev/agentN/<repo>
        // Here we just print guidance; actual creation may be outside scope.
        fmt.Printf("Initialize worktrees for %s: base=%s branch=%s (1..%s) on host (manual)\n", repo, base, branch, count)
    case "worktrees-branch":
        mustProject(project)
        if project != "dev-all" { die("Use -p dev-all for worktrees-branch") }
        if len(sub) < 3 { die("Usage: -p dev-all worktrees-branch <repo> <index> <branch>") }
        repo := sub[0]; idx := sub[1]; branch := sub[2]
        base := "/workspaces/dev"
        var path string
        if idx == "1" { path = base+"/"+repo } else { path = base+"/agent"+idx+"/"+repo }
        runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "set -e; cd '"+path+"'; git checkout -b '"+branch+"'")
    case "worktrees-status":
        mustProject(project)
        if project != "dev-all" { die("Use -p dev-all for worktrees-status") }
        if len(sub) < 1 { die("Usage: -p dev-all worktrees-status <repo> [--all|--index N]") }
        repo := sub[0]
        idx := ""; if len(sub) >= 3 && sub[1] == "--index" { idx = sub[2] }
        base := "/workspaces/dev"
        if idx != "" {
            path := base+"/"+repo
            if idx != "1" { path = base+"/agent"+idx+"/"+repo }
            runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "set -e; cd '"+path+"'; git status -sb")
        } else {
            // sample for first two agents
            for _, i := range []string{"1","2"} {
                path := base+"/"+repo
                if i != "1" { path = base+"/agent"+i+"/"+repo }
                runCompose(dryRun, files, "exec", "--index", i, "dev-agent", "bash", "-lc", "cd '"+path+"' 2>/dev/null && git status -sb || true")
            }
        }
    case "worktrees-sync":
        mustProject(project)
        if project != "dev-all" { die("Use -p dev-all for worktrees-sync") }
        if len(sub) < 2 { die("Usage: worktrees-sync <repo> (--pull|--push) [--all|--index N]") }
        repo := sub[0]
        op := sub[1]
        idx := ""; if len(sub) >= 4 && sub[2] == "--index" { idx = sub[3] }
        base := "/workspaces/dev"
        gitcmd := "git pull --ff-only"; if op == "--push" { gitcmd = "git push" }
        if idx != "" {
            path := base+"/"+repo
            if idx != "1" { path = base+"/agent"+idx+"/"+repo }
            runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "set -e; cd '"+path+"'; "+gitcmd)
        } else {
            for _, i := range []string{"1","2","3","4","5","6"} {
                path := base+"/"+repo
                if i != "1" { path = base+"/agent"+i+"/"+repo }
                runCompose(dryRun, files, "exec", "--index", i, "dev-agent", "bash", "-lc", "cd '"+path+"' 2>/dev/null && (set -e; cd '"+path+"'; "+gitcmd+") || true")
            }
        }
    case "worktrees-tmux":
        mustProject(project)
        if project != "dev-all" { die("Use -p dev-all for worktrees-tmux") }
        if len(sub) < 2 { die("Usage: -p dev-all worktrees-tmux <repo> <count>") }
        repo := sub[0]; n := mustAtoi(sub[1])
        // Bring up and open tmux windows for N agents
        runCompose(dryRun, files, "up", "-d", "--scale", fmt.Sprintf("dev-agent=%d", n))
        base := "/workspaces/dev"
        sess := "devkit-worktrees"
        home1 := base+"/"+repo+"/.devhome-agent1"
        if !skipTmux() {
        runHost(dryRun, "tmux", "new-session", "-d", "-s", sess, "docker compose "+strings.Join(files, " ")+" exec --index 1 dev-agent bash -lc 'mkdir -p \""+home1+"/.codex/rollouts\" \""+home1+"/.cache\" \""+home1+"/.config\" \""+home1+"/.local\"; export HOME=\""+home1+"\"; export CODEX_HOME=\""+home1+"/.codex\"; export CODEX_ROLLOUT_DIR=\""+home1+"/.codex/rollouts\"; export XDG_CACHE_HOME=\""+home1+"/.cache\"; export XDG_CONFIG_HOME=\""+home1+"/.config\"; cd \""+base+"/"+repo+"\"; exec bash'")
            runHost(dryRun, "tmux", "rename-window", "-t", sess+":0", "agent-1")
            for i := 2; i <= n; i++ {
                whome := fmt.Sprintf("%s/agent%d/.devhome-agent%d", base, i, i)
                wpath := fmt.Sprintf("%s/agent%d/%s", base, i, repo)
            runHost(dryRun, "tmux", "new-window", "-t", sess, "-n", fmt.Sprintf("agent-%d", i), "docker compose "+strings.Join(files, " ")+fmt.Sprintf(" exec --index %d dev-agent bash -lc 'mkdir -p \"%s/.codex/rollouts\" \"%s/.cache\" \"%s/.config\" \"%s/.local\"; export HOME=\"%s\"; export CODEX_HOME=\"%s/.codex\"; export CODEX_ROLLOUT_DIR=\"%s/.codex/rollouts\"; export XDG_CACHE_HOME=\"%s/.cache\"; export XDG_CONFIG_HOME=\"%s/.config\"; cd \"%s\"; exec bash'", i, whome, whome, whome, whome, whome, whome, whome, whome, whome, wpath))
            }
            runHost(dryRun, "tmux", "attach", "-t", sess)
        }
    case "bootstrap":
        mustProject(project)
        if project != "dev-all" { die("Use -p dev-all for bootstrap") }
        if len(sub) < 2 { die("Usage: -p dev-all bootstrap <repo> <count>") }
        repo := sub[0]; n := sub[1]
        // suggest init then open tmux
        fmt.Println("Bootstrapping worktrees and tmux for repo:", repo, "count:", n)
        // call worktrees-init and worktrees-tmux can be done manually
    default:
        usage(); os.Exit(2)
    }
}

func die(msg string) { fmt.Fprintln(os.Stderr, msg); os.Exit(2) }
func mustProject(p string) { if strings.TrimSpace(p) == "" { die("-p <project> is required") } }

func runCompose(dry bool, fileArgs []string, args ...string) {
    // add a default timeout for safety
    ctx, cancel := execx.WithTimeout(10 * time.Minute)
    defer cancel()
    all := append([]string{"compose"}, append(fileArgs, args...)...)
    if dry {
        // echo the command and return success
        fmt.Fprintln(os.Stderr, "+ docker "+strings.Join(all, " "))
        return
    }
    res := execx.RunCtx(ctx, "docker", all...)
    if res.Code != 0 { os.Exit(res.Code) }
}

func runComposeInput(dry bool, fileArgs []string, input []byte, args ...string) {
    ctx, cancel := execx.WithTimeout(10 * time.Minute)
    defer cancel()
    all := append([]string{"compose"}, append(fileArgs, args...)...)
    if dry {
        fmt.Fprintln(os.Stderr, "+ docker "+strings.Join(all, " "))
        return
    }
    res := execx.RunWithInput(ctx, input, "docker", all...)
    if res.Code != 0 { os.Exit(res.Code) }
}

func runHost(dry bool, name string, args ...string) {
    ctx, cancel := execx.WithTimeout(10 * time.Minute)
    defer cancel()
    if dry {
        fmt.Fprintln(os.Stderr, "+ "+name+" "+strings.Join(args, " "))
        return
    }
    res := execx.RunCtx(ctx, name, args...)
    if res.Code != 0 { os.Exit(res.Code) }
}

func runHostBestEffort(dry bool, name string, args ...string) {
    ctx, cancel := execx.WithTimeout(2 * time.Minute)
    defer cancel()
    if dry {
        fmt.Fprintln(os.Stderr, "+ "+name+" "+strings.Join(args, " "))
        return
    }
    _ = execx.RunCtx(ctx, name, args...)
}

func skipTmux() bool { return os.Getenv("DEVKIT_NO_TMUX") == "1" }
func mustAtoi(s string) int {
    n, err := strconv.Atoi(s)
    if err != nil || n < 1 { die("count must be a positive integer") }
    return n
}

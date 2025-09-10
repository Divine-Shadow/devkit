package main

import (
    "fmt"
    "io/fs"
    "context"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "time"

    "devkit/cli/devctl/internal/compose"
    "devkit/cli/devctl/internal/netutil"
    fz "devkit/cli/devctl/internal/files"
    seed "devkit/cli/devctl/internal/seed"
    "devkit/cli/devctl/internal/tmuxutil"
    sshcfg "devkit/cli/devctl/internal/sshcfg"
    pth "devkit/cli/devctl/internal/paths"
    "devkit/cli/devctl/internal/execx"
    "devkit/cli/devctl/internal/config"
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
  worktrees-setup <repo> <count> [--base agent] [--branch main]  (dev-all)
  worktrees-branch <repo> <index> <branch>   (dev-all)
  worktrees-status <repo> [--all|--index N]  (dev-all)
  worktrees-sync <repo> (--pull|--push) [--all|--index N]  (dev-all)
  worktrees-tmux <repo> <count>              (dev-all)
  reset [N]                                  (alias: fresh-open)
  bootstrap <repo> <count>                   (dev-all)
  verify                                     (ssh + codex + worktrees)

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
    // Ensure codex overlay mounts the intended repo path via WORKSPACE_DIR
    if project == "codex" {
        devRoot := filepath.Clean(filepath.Join(paths.Root, ".."))
        _ = os.Setenv("WORKSPACE_DIR", filepath.Join(devRoot, "ouroboros-ide"))
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
        repoName := "ouroboros-ide"
        if project == "dev-all" {
            if cfg, err := config.ReadAll(paths.Root, project); err == nil && strings.TrimSpace(cfg.Defaults.Repo) != "" {
                repoName = cfg.Defaults.Repo
            }
        }
        home := pth.AgentHomePath(project, idx, repoName)
        // Interactive exec: do not impose a timeout
        runComposeInteractive(dryRun, files, append([]string{
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
        // Long-lived attach: no timeout
        runComposeInteractive(dryRun, files, "attach", "--index", idx, "dev-agent")
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
    case "codex-test":
        mustProject(project)
        // Parse optional args: [index] [repo]
        idx := "1"; var repo string
        if len(sub) > 0 {
            if _, err := strconv.Atoi(sub[0]); err == nil { idx = sub[0]; if len(sub) > 1 { repo = sub[1] } }
            if _, err := strconv.Atoi(sub[0]); err != nil { repo = sub[0]; if len(sub) > 1 { idx = sub[1] } }
        }
        if project == "dev-all" && strings.TrimSpace(repo) == "" {
            if cfg, err := config.ReadAll(paths.Root, project); err == nil { repo = cfg.Defaults.Repo }
        }
        if strings.TrimSpace(repo) == "" { repo = "ouroboros-ide" }
        // Determine working directory/home inside container using helpers
        wd := pth.AgentRepoPath(project, idx, repo)
        home := pth.AgentHomePath(project, idx, repo)
        // Build a script that ensures HOME dirs and runs codex inside a repo dir
        script := fmt.Sprintf("set -euo pipefail; mkdir -p '%[1]s'/.codex/rollouts '%[1]s'/.cache '%[1]s'/.config '%[1]s'/.local; cd '%[2]s' 2>/dev/null || true; export HOME='%[1]s' CODEX_HOME='%[1]s'/.codex CODEX_ROLLOUT_DIR='%[1]s'/.codex/rollouts XDG_CACHE_HOME='%[1]s'/.cache XDG_CONFIG_HOME='%[1]s'/.config; if codex exec 'reply with: ok' 2>&1 | tr -d '\r' | grep -m1 -x ok >/dev/null; then echo ok; else echo 'codex-test failed'; exit 1; fi", home, wd)
        runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", script)
    case "verify":
        // Verify SSH to GitHub, Codex basic exec, and worktrees status (when applicable)
        mustProject(project)
        // 1) SSH test on agent 1
        {
            home := "/workspace/.devhome-agent1"
            runCompose(dryRun, files, "exec", "--index", "1", "dev-agent", "bash", "-lc", "export HOME='"+home+"'; ssh -F '"+home+"'/.ssh/config -T github.com -o BatchMode=yes || true")
        }
        // 2) Codex basic check in-place
        {
            if project == "dev-all" {
                // Use defaults to pick a repo
                cfg, _ := config.ReadAll(paths.Root, project)
                repo := cfg.Defaults.Repo; if strings.TrimSpace(repo) == "" { repo = "ouroboros-ide" }
                n := cfg.Defaults.Agents; if n < 1 { n = 2 }
                // ensure desired scale is up
                runCompose(dryRun, files, "up", "-d", "--scale", fmt.Sprintf("dev-agent=%d", n))
                base := "/workspaces/dev"; wd := filepath.Join(base, repo)
                home := filepath.Join(base, repo, ".devhome-agent1")
                script := fmt.Sprintf("set -e; cd '%s' 2>/dev/null || true; HOME='%s' CODEX_HOME='%s/.codex' CODEX_ROLLOUT_DIR='%s/.codex/rollouts' XDG_CACHE_HOME='%s/.cache' XDG_CONFIG_HOME='%s/.config' codex exec 'reply with: ok' || true", wd, home, home, home, home, home)
                runCompose(dryRun, files, "exec", "--index", "1", "dev-agent", "bash", "-lc", script)
                // quick worktrees status across up to 3 agents
                for i := 1; i <= n && i <= 3; i++ {
                    is := fmt.Sprintf("%d", i)
                    path := wd
                    if i != 1 { path = filepath.Join(base, "agent"+is, repo) }
                    runCompose(dryRun, files, "exec", "--index", is, "dev-agent", "bash", "-lc", "cd '"+path+"' 2>/dev/null && git status -sb && git rev-parse --abbrev-ref --symbolic-full-name @{u} && git config --get push.default || true")
                }
            } else {
                // codex overlay: run from /workspace
                script := "set -e; cd /workspace 2>/dev/null || true; HOME=/workspace/.devhome-agent1 CODEX_HOME=/workspace/.devhome-agent1/.codex CODEX_ROLLOUT_DIR=/workspace/.devhome-agent1/.codex/rollouts XDG_CACHE_HOME=/workspace/.devhome-agent1/.cache XDG_CONFIG_HOME=/workspace/.devhome-agent1/.config codex exec 'reply with: ok' || true"
                runCompose(dryRun, files, "exec", "--index", "1", "dev-agent", "bash", "-lc", script)
            }
        }
        fmt.Println("verify completed")
    case "codex-debug":
        mustProject(project)
        idx := "1"; if len(sub) > 0 { idx = sub[0] }
        home := "/workspace/.devhome-agent" + idx
        script := fmt.Sprintf(`set -e
export HOME='%s'
export CODEX_HOME='%s/.codex'
export CODEX_ROLLOUT_DIR='%s/.codex/rollouts'
export XDG_CACHE_HOME='%s/.cache'
export XDG_CONFIG_HOME='%s/.config'
echo "HOME=$HOME"; echo "CODEX_HOME=$CODEX_HOME"
echo "-- locations --"
for p in "$HOME/.codex/auth.json" "$CODEX_HOME/auth.json" "/var/auth.json" "/var/host-codex/auth.json"; do
  [ -n "$p" ] || continue; echo -n "$p : "; [ -f "$p" ] && wc -c < "$p" || echo "(missing)"; done
exit 0`, home, home, home, home, home)
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
        if !strings.HasPrefix(subpath, "/") { dest = filepath.Join("/workspaces/dev", subpath) }
        // Compute a sensible per-agent HOME for dev-all based on the destination path
        repo := "ouroboros-ide"
        if project == "dev-all" {
            rel := strings.TrimPrefix(dest, "/workspaces/dev/")
            parts := strings.Split(rel, "/")
            if len(parts) > 0 {
                if strings.HasPrefix(parts[0], "agent") && len(parts) > 1 { repo = parts[1] } else { repo = parts[0] }
            }
            if strings.TrimSpace(repo) == "" { repo = "ouroboros-ide" }
        }
        home := pth.AgentHomePath(project, idx, repo)
        cmdstr := "bash"
        if len(sub) > 2 { cmdstr = strings.Join(sub[2:], " ") }
        // Interactive shell: no timeout; export HOME/XDG so codex uses the seeded per-agent home
        runComposeInteractive(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "export HOME='"+home+"' CODEX_HOME='"+home+"/.codex' CODEX_ROLLOUT_DIR='"+home+"/.codex/rollouts' XDG_CACHE_HOME='"+home+"/.cache' XDG_CONFIG_HOME='"+home+"/.config'; cd '"+dest+"' && exec "+cmdstr)
    case "attach-cd":
        mustProject(project)
        if len(sub) < 2 { die("Usage: attach-cd <index> <subpath>") }
        idx := sub[0]; subpath := sub[1]
        dest := subpath
        if !strings.HasPrefix(subpath, "/") { dest = filepath.Join("/workspaces/dev", subpath) }
        repo := "ouroboros-ide"
        if project == "dev-all" {
            rel := strings.TrimPrefix(dest, "/workspaces/dev/")
            parts := strings.Split(rel, "/")
            if len(parts) > 0 {
                if strings.HasPrefix(parts[0], "agent") && len(parts) > 1 { repo = parts[1] } else { repo = parts[0] }
            }
            if strings.TrimSpace(repo) == "" { repo = "ouroboros-ide" }
        }
        home := pth.AgentHomePath(project, idx, repo)
        // Interactive shell: no timeout
        runComposeInteractive(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "export HOME='"+home+"' CODEX_HOME='"+home+"/.codex' CODEX_ROLLOUT_DIR='"+home+"/.codex/rollouts' XDG_CACHE_HOME='"+home+"/.cache' XDG_CONFIG_HOME='"+home+"/.config'; cd '"+dest+"' && exec bash")
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
            cmd := "docker compose "+strings.Join(files, " ")+" exec --index 1 dev-agent bash -lc 'mkdir -p \""+home1+"/.codex/rollouts\" \""+home1+"/.cache\" \""+home1+"/.config\" \""+home1+"/.local\"; export HOME=\""+home1+"\"; export CODEX_HOME=\""+home1+"/.codex\"; export CODEX_ROLLOUT_DIR=\""+home1+"/.codex/rollouts\"; export XDG_CACHE_HOME=\""+home1+"/.cache\"; export XDG_CONFIG_HOME=\""+home1+"/.config\"; cd /workspace; exec bash'"
            runHost(dryRun, "tmux", tmuxutil.NewSession(sess, cmd)...)
            runHost(dryRun, "tmux", tmuxutil.RenameWindow(sess+":0", "agent-1")...)
            for i := 2; i <= n; i++ {
                homei := fmt.Sprintf("/workspace/.devhome-agent%d", i)
                wcmd := "docker compose "+strings.Join(files, " ")+fmt.Sprintf(" exec --index %d dev-agent bash -lc 'mkdir -p \"%s/.codex/rollouts\" \"%s/.cache\" \"%s/.config\" \"%s/.local\"; export HOME=\"%s\"; export CODEX_HOME=\"%s/.codex\"; export CODEX_ROLLOUT_DIR=\"%s/.codex/rollouts\"; export XDG_CACHE_HOME=\"%s/.cache\"; export XDG_CONFIG_HOME=\"%s/.config\"; cd /workspace; exec bash'", i, homei, homei, homei, homei, homei, homei, homei, homei, homei)
                runHost(dryRun, "tmux", tmuxutil.NewWindow(sess, fmt.Sprintf("agent-%d", i), wcmd)...)
            }
            // tmux attach is long-lived: no timeout
            runHostInteractive(dryRun, "tmux", tmuxutil.Attach(sess)...)
        }
    case "open":
        mustProject(project)
        n := 2; if len(sub) > 0 { n = mustAtoi(sub[0]) }
        runCompose(dryRun, files, "up", "-d", "--scale", fmt.Sprintf("dev-agent=%d", n))
        sess := "devkit-open"
        home1 := "/workspace/.devhome-agent1"
        if !skipTmux() {
            cmd := "docker compose "+strings.Join(files, " ")+" exec --index 1 dev-agent bash -lc 'mkdir -p \""+home1+"/.codex/rollouts\" \""+home1+"/.cache\" \""+home1+"/.config\" \""+home1+"/.local\"; export HOME=\""+home1+"\"; export CODEX_HOME=\""+home1+"/.codex\"; export CODEX_ROLLOUT_DIR=\""+home1+"/.codex/rollouts\"; export XDG_CACHE_HOME=\""+home1+"/.cache\"; export XDG_CONFIG_HOME=\""+home1+"/.config\"; cd /workspace; exec bash'"
            runHost(dryRun, "tmux", tmuxutil.NewSession(sess, cmd)...)
            runHost(dryRun, "tmux", tmuxutil.RenameWindow(sess+":0", "agent-1")...)
            for i := 2; i <= n; i++ {
                homei := fmt.Sprintf("/workspace/.devhome-agent%d", i)
                wcmd := "docker compose "+strings.Join(files, " ")+fmt.Sprintf(" exec --index %d dev-agent bash -lc 'mkdir -p \"%s/.codex/rollouts\" \"%s/.cache\" \"%s/.config\" \"%s/.local\"; export HOME=\"%s\"; export CODEX_HOME=\"%s/.codex\"; export CODEX_ROLLOUT_DIR=\"%s/.codex/rollouts\"; export XDG_CACHE_HOME=\"%s/.cache\"; export XDG_CONFIG_HOME=\"%s/.config\"; cd /workspace; exec bash'", i, homei, homei, homei, homei, homei, homei, homei, homei, homei)
                runHost(dryRun, "tmux", tmuxutil.NewWindow(sess, fmt.Sprintf("agent-%d", i), wcmd)...)
            }
            // tmux attach is long-lived: no timeout
            runHostInteractive(dryRun, "tmux", tmuxutil.Attach(sess)...)
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
        // Seed per-agent Codex HOME from host mounts using small, robust scripts
        for j := 1; j <= n; j++ {
            homej := fmt.Sprintf("/workspace/.devhome-agent%d", j)
            for _, script := range seed.BuildSeedScripts(homej) {
                runCompose(dryRun, all, "exec", "-T", "--index", fmt.Sprintf("%d", j), "dev-agent", "bash", "-lc", script)
            }
        }

        // tmux session
        if !skipTmux() {
            sess := "devkit-open"
            home1 := "/workspace/.devhome-agent1"
            cmd := "docker compose "+strings.Join(all, " ")+" exec --index 1 dev-agent bash -lc 'mkdir -p \""+home1+"/.codex/rollouts\" \""+home1+"/.cache\" \""+home1+"/.config\" \""+home1+"/.local\"; export HOME=\""+home1+"\"; export CODEX_HOME=\""+home1+"/.codex\"; export CODEX_ROLLOUT_DIR=\""+home1+"/.codex/rollouts\"; export XDG_CACHE_HOME=\""+home1+"/.cache\"; export XDG_CONFIG_HOME=\""+home1+"/.config\"; cd /workspace; exec bash'"
            runHost(dryRun, "tmux", tmuxutil.NewSession(sess, cmd)...)
            runHost(dryRun, "tmux", tmuxutil.RenameWindow(sess+":0", "agent-1")...)
            for i := 2; i <= n; i++ {
                homei := fmt.Sprintf("/workspace/.devhome-agent%d", i)
                wcmd := "docker compose "+strings.Join(all, " ")+fmt.Sprintf(" exec --index %d dev-agent bash -lc 'mkdir -p \"%s/.codex/rollouts\" \"%s/.cache\" \"%s/.config\" \"%s/.local\"; export HOME=\"%s\"; export CODEX_HOME=\"%s/.codex\"; export CODEX_ROLLOUT_DIR=\"%s/.codex/rollouts\"; export XDG_CACHE_HOME=\"%s/.cache\"; export XDG_CONFIG_HOME=\"%s/.config\"; cd /workspace; exec bash'", i, homei, homei, homei, homei, homei, homei, homei, homei, homei)
                runHost(dryRun, "tmux", tmuxutil.NewWindow(sess, fmt.Sprintf("agent-%d", i), wcmd)...)
            }
            // tmux attach is long-lived: no timeout
            runHostInteractive(dryRun, "tmux", tmuxutil.Attach(sess)...)
        }
    case "reset":
        // Alias to fresh-open with identical behavior
        mustProject(project)
        n := 3; if len(sub) > 0 { n = mustAtoi(sub[0]) }
        all := compose.AllProfilesFiles(paths, project)
        runCompose(dryRun, all, "down")
        if !skipTmux() { runHostBestEffort(dryRun, "tmux", "kill-session", "-t", "devkit-open") }
        if !skipTmux() { runHostBestEffort(dryRun, "tmux", "kill-session", "-t", "devkit-shells") }
        if !skipTmux() { runHostBestEffort(dryRun, "tmux", "kill-session", "-t", "devkit-worktrees") }
        runHostBestEffort(dryRun, "docker", "rm", "-f", "devkit_envoy", "devkit_envoy_sni", "devkit_dns", "devkit_tinyproxy")
        runHostBestEffort(dryRun, "docker", "network", "rm", "devkit_dev-internal", "devkit_dev-egress")
        runCompose(dryRun, all, "up", "-d", "--scale", fmt.Sprintf("dev-agent=%d", n))
        for j := 1; j <= n; j++ {
            homej := fmt.Sprintf("/workspace/.devhome-agent%d", j)
            for _, script := range seed.BuildSeedScripts(homej) {
                runCompose(dryRun, all, "exec", "-T", "--index", fmt.Sprintf("%d", j), "dev-agent", "bash", "-lc", script)
            }
        }
        if !skipTmux() {
            sess := "devkit-open"
            home1 := "/workspace/.devhome-agent1"
            cmd := "docker compose "+strings.Join(all, " ")+" exec --index 1 dev-agent bash -lc 'mkdir -p \""+home1+"/.codex/rollouts\" \""+home1+"/.cache\" \""+home1+"/.config\" \""+home1+"/.local\"; export HOME=\""+home1+"\"; export CODEX_HOME=\""+home1+"/.codex\"; export CODEX_ROLLOUT_DIR=\""+home1+"/.codex/rollouts\"; export XDG_CACHE_HOME=\""+home1+"/.cache\"; export XDG_CONFIG_HOME=\""+home1+"/.config\"; cd /workspace; exec bash'"
            runHost(dryRun, "tmux", tmuxutil.NewSession(sess, cmd)...)
            runHost(dryRun, "tmux", tmuxutil.RenameWindow(sess+":0", "agent-1")...)
            for i := 2; i <= n; i++ {
                homei := fmt.Sprintf("/workspace/.devhome-agent%d", i)
                wcmd := "docker compose "+strings.Join(all, " ")+fmt.Sprintf(" exec --index %d dev-agent bash -lc 'mkdir -p \"%s/.codex/rollouts\" \"%s/.cache\" \"%s/.config\" \"%s/.local\"; export HOME=\"%s\"; export CODEX_HOME=\"%s/.codex\"; export CODEX_ROLLOUT_DIR=\"%s/.codex/rollouts\"; export XDG_CACHE_HOME=\"%s/.cache\"; export XDG_CONFIG_HOME=\"%s/.config\"; cd /workspace; exec bash'", i, homei, homei, homei, homei, homei, homei, homei, homei, homei)
                runHost(dryRun, "tmux", tmuxutil.NewWindow(sess, fmt.Sprintf("agent-%d", i), wcmd)...)
            }
            runHostInteractive(dryRun, "tmux", tmuxutil.Attach(sess)...)
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
        // Compute per-agent HOME depending on overlay
        repoName := "ouroboros-ide"
        if project == "dev-all" {
            if cfg, err := config.ReadAll(paths.Root, project); err == nil && strings.TrimSpace(cfg.Defaults.Repo) != "" {
                repoName = cfg.Defaults.Repo
            }
        }
        home := pth.AgentHomePath(project, idx, repoName)
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
        cfg := sshcfg.BuildGitHubConfig(home)
        /* cfg template moved to sshcfg */
        // old: cfg := "Host github.com\n  HostName ssh.github.com\n  Port 443\n  User git\n  ProxyCommand nc -X connect -x tinyproxy:8888 %h %p\n  IdentityFile '"+home+"'/.ssh/id_ed25519\n  IdentitiesOnly yes\n  StrictHostKeyChecking accept-new\n  UserKnownHostsFile '"+home+"'/.ssh/known_hosts\n"
        runComposeInput(dryRun, files, []byte(cfg), "exec", "-T", "--index", idx, "dev-agent", "bash", "-lc", "cat > '"+home+"'/.ssh/config && chmod 600 '"+home+"'/.ssh/config")
        // git config global sshCommand
        runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "export HOME='"+home+"' && git config --global core.sshCommand 'ssh -F '"+home+"'/.ssh/config'")
    case "ssh-test":
        mustProject(project)
        idx := "1"; if len(sub) > 0 { idx = sub[0] }
        repoName := "ouroboros-ide"
        if project == "dev-all" {
            if cfg, err := config.ReadAll(paths.Root, project); err == nil && strings.TrimSpace(cfg.Defaults.Repo) != "" {
                repoName = cfg.Defaults.Repo
            }
        }
        home := pth.AgentHomePath(project, idx, repoName)
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
    case "worktrees-setup":
        // Create per-agent branches and worktrees rooted in the dev root (dev-all overlay pattern)
        mustProject(project)
        if project != "dev-all" { die("Use -p dev-all for worktrees-setup") }
        if len(sub) < 2 { die("Usage: worktrees-setup <repo> <count> [--base agent] [--branch main]") }
        repo := sub[0]; n := mustAtoi(sub[1])
        base := "agent"; baseBranch := "main"
        for i := 2; i+1 < len(sub); i++ {
            if sub[i] == "--base" { base = sub[i+1] } else if sub[i] == "--branch" { baseBranch = sub[i+1] }
        }
        // dev root is one level up from devkit root (../) matching dev-all volume ../../ from overlay dir
        devRoot := filepath.Clean(filepath.Join(paths.Root, ".."))
        repoPath := filepath.Join(devRoot, repo)
        // sanity: fetch on the primary repo
        runHost(dryRun, "git", "-C", repoPath, "fetch", "--all", "--prune")
        // ensure pushing from local agent branches updates the upstream branch (origin/<baseBranch>)
        runHost(dryRun, "git", "-C", repoPath, "config", "push.default", "upstream")
        // ensure relative paths for worktrees so mounts work inside containers
        runHost(dryRun, "git", "-C", repoPath, "config", "worktree.useRelativePaths", "true")
        // agent 1: reuse primary repo path; create/switch to branch without resetting files (preserve local work)
        branch1 := fmt.Sprintf("%s%d", base, 1)
        runHost(dryRun, "git", "-C", repoPath, "checkout", "-B", branch1)
        runHost(dryRun, "git", "-C", repoPath, "branch", "--set-upstream-to=origin/"+baseBranch, branch1)
        for i := 2; i <= n; i++ {
            // ensure parent directory like <devRoot>/agent2
            parent := filepath.Join(devRoot, fmt.Sprintf("%s%d", base, i))
            // create parent directory on host
            if !dryRun { _ = os.MkdirAll(parent, 0o755) }
            wt := filepath.Join(parent, repo)
            br := fmt.Sprintf("%s%d", base, i)
            // idempotency: prune dead worktrees and remove existing path if it is already a worktree
            runHostBestEffort(dryRun, "git", "-C", repoPath, "worktree", "prune")
            runHostBestEffort(dryRun, "git", "-C", repoPath, "worktree", "remove", "-f", wt)
            runHost(dryRun, "git", "-C", repoPath, "worktree", "add", wt, "-B", br, "origin/"+baseBranch)
            if !dryRun { rewriteWorktreeGitdir(wt) }
            runHost(dryRun, "git", "-C", wt, "branch", "--set-upstream-to=origin/"+baseBranch, br)
        }
        fmt.Printf("Worktrees ready under %s (branches %s1..%s%d tracking origin/%s)\n", devRoot, base, base, n, baseBranch)
    case "run":
        // Idempotent end-to-end launcher: ensures worktrees, scales up, and opens tmux across N agents
        mustProject(project)
        if project != "dev-all" { die("Use -p dev-all for run") }
        if len(sub) < 2 { die("Usage: run <repo> <count>") }
        repo := sub[0]; n := mustAtoi(sub[1])
        // Ensure worktrees are present and configured (idempotent)
        // Reuse handler behavior inline
        {
            // dev root and repo path
            devRoot := filepath.Clean(filepath.Join(paths.Root, ".."))
            repoPath := filepath.Join(devRoot, repo)
            // Ensure host-side git does not inherit any container GIT_SSH_COMMAND
            runHost(dryRun, "env", "-u", "GIT_SSH_COMMAND", "git", "-c", "core.sshCommand=ssh", "-C", repoPath, "fetch", "--all", "--prune")
            runHost(dryRun, "env", "-u", "GIT_SSH_COMMAND", "git", "-c", "core.sshCommand=ssh", "-C", repoPath, "config", "push.default", "upstream")
            runHost(dryRun, "env", "-u", "GIT_SSH_COMMAND", "git", "-c", "core.sshCommand=ssh", "-C", repoPath, "config", "worktree.useRelativePaths", "true")
            runHost(dryRun, "env", "-u", "GIT_SSH_COMMAND", "git", "-c", "core.sshCommand=ssh", "-C", repoPath, "checkout", "-B", "agent1")
            runHost(dryRun, "env", "-u", "GIT_SSH_COMMAND", "git", "-c", "core.sshCommand=ssh", "-C", repoPath, "branch", "--set-upstream-to=origin/main", "agent1")
            for i := 2; i <= n; i++ {
                parent := filepath.Join(devRoot, fmt.Sprintf("agent%d", i))
                if !dryRun { _ = os.MkdirAll(parent, 0o755) }
                wt := filepath.Join(parent, repo)
                br := fmt.Sprintf("agent%d", i)
                runHostBestEffort(dryRun, "env", "-u", "GIT_SSH_COMMAND", "git", "-c", "core.sshCommand=ssh", "-C", repoPath, "worktree", "prune")
                runHostBestEffort(dryRun, "env", "-u", "GIT_SSH_COMMAND", "git", "-c", "core.sshCommand=ssh", "-C", repoPath, "worktree", "remove", "-f", wt)
                runHost(dryRun, "env", "-u", "GIT_SSH_COMMAND", "git", "-c", "core.sshCommand=ssh", "-C", repoPath, "worktree", "add", wt, "-B", br, "origin/main")
                if !dryRun { rewriteWorktreeGitdir(wt) }
                runHost(dryRun, "env", "-u", "GIT_SSH_COMMAND", "git", "-c", "core.sshCommand=ssh", "-C", wt, "branch", "--set-upstream-to=origin/main", br)
            }
        }
        // Bring up and open tmux windows for N agents
        // Compose up with scale (remove orphans for idempotency)
        runCompose(dryRun, files, "up", "-d", "--remove-orphans", "--scale", fmt.Sprintf("dev-agent=%d", n))
        // Seed per-agent Codex HOME from host mounts so codex can run non-interactively
        {
            // agent 1 home under primary repo path
            home1 := pth.AgentHomePath(project, "1", repo)
            for _, script := range seed.BuildSeedScripts(home1) {
                runCompose(dryRun, files, "exec", "-T", "--index", "1", "dev-agent", "bash", "-lc", script)
            }
            // agents 2..n: home under agentN/<repo>
            for j := 2; j <= n; j++ {
                idx := fmt.Sprintf("%d", j)
                homej := pth.AgentHomePath(project, idx, repo)
                for _, script := range seed.BuildSeedScripts(homej) {
                    runCompose(dryRun, files, "exec", "-T", "--index", idx, "dev-agent", "bash", "-lc", script)
                }
            }
        }
        // Ensure SSH config per agent with correct HOME under repo paths, then validate git pull
        {
            // Make sure ssh.github.com is allowlisted and proxies are active before any git/ssh calls
            _, _ = fz.AppendLineIfMissing(filepath.Join(paths.Kit, "proxy", "allowlist.txt"), "ssh.github.com")
            _, _ = fz.AppendLineIfMissing(filepath.Join(paths.Kit, "dns", "dnsmasq.conf"), "server=/ssh.github.com/1.1.1.1")
            runCompose(dryRun, files, "restart", "tinyproxy", "dns")
            hostKey := filepath.Join(os.Getenv("HOME"), ".ssh", "id_ed25519")
            if _, err := os.Stat(hostKey); err != nil { hostKey = filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa") }
            keyBytes, _ := os.ReadFile(hostKey)
            pubBytes, _ := os.ReadFile(hostKey+".pub")
            known := filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts")
            knownBytes, _ := os.ReadFile(known)
            // agent 1
            home1 := pth.AgentHomePath(project, "1", repo)
            cfg1 := sshcfg.BuildGitHubConfig(home1)
            runCompose(dryRun, files, "exec", "-T", "--index", "1", "dev-agent", "bash", "-lc", "mkdir -p '"+home1+"'/.ssh && chmod 700 '"+home1+"'/.ssh")
            if len(keyBytes) > 0 { runComposeInput(dryRun, files, keyBytes, "exec", "-T", "--index", "1", "dev-agent", "bash", "-lc", "cat > '"+home1+"'/.ssh/id_ed25519 && chmod 600 '"+home1+"'/.ssh/id_ed25519") }
            if len(pubBytes) > 0 { runComposeInput(dryRun, files, pubBytes, "exec", "-T", "--index", "1", "dev-agent", "bash", "-lc", "cat > '"+home1+"'/.ssh/id_ed25519.pub && chmod 644 '"+home1+"'/.ssh/id_ed25519.pub") }
            if len(knownBytes) > 0 { runComposeInput(dryRun, files, knownBytes, "exec", "-T", "--index", "1", "dev-agent", "bash", "-lc", "cat > '"+home1+"'/.ssh/known_hosts && chmod 644 '"+home1+"'/.ssh/known_hosts") }
            runComposeInput(dryRun, files, []byte(cfg1), "exec", "-T", "--index", "1", "dev-agent", "bash", "-lc", "cat > '"+home1+"'/.ssh/config && chmod 600 '"+home1+"'/.ssh/config")
            // wait for config to be visible and non-empty before git commands
            runCompose(dryRun, files, "exec", "--index", "1", "dev-agent", "bash", "-lc", "for i in $(seq 1 20); do [ -s '"+home1+"'/.ssh/config ] && break || sleep 0.25; done")
            runCompose(dryRun, files, "exec", "--index", "1", "dev-agent", "bash", "-lc", "export HOME='"+home1+"' && git config --global core.sshCommand 'ssh -F '"+home1+"'/.ssh/config'")
            // also persist in repo config to avoid relying on HOME
            runCompose(dryRun, files, "exec", "--index", "1", "dev-agent", "bash", "-lc", "cd '"+pth.AgentRepoPath(project, "1", repo)+"' && git config core.sshCommand 'ssh -F '"+home1+"'/.ssh/config'")
            runCompose(dryRun, files, "exec", "--index", "1", "dev-agent", "bash", "-lc", "set -e; cd '"+pth.AgentRepoPath(project, "1", repo)+"'; GIT_SSH_COMMAND=\"ssh -F '"+home1+"'/.ssh/config\" git pull --ff-only || true")
            // agents 2..n
            for i := 2; i <= n; i++ {
                idx := fmt.Sprintf("%d", i)
                whome := pth.AgentHomePath(project, idx, repo)
                wpath := pth.AgentRepoPath(project, idx, repo)
                cfg := sshcfg.BuildGitHubConfig(whome)
                runCompose(dryRun, files, "exec", "-T", "--index", idx, "dev-agent", "bash", "-lc", "mkdir -p '"+whome+"'/.ssh && chmod 700 '"+whome+"'/.ssh")
                if len(keyBytes) > 0 { runComposeInput(dryRun, files, keyBytes, "exec", "-T", "--index", idx, "dev-agent", "bash", "-lc", "cat > '"+whome+"'/.ssh/id_ed25519 && chmod 600 '"+whome+"'/.ssh/id_ed25519") }
                if len(pubBytes) > 0 { runComposeInput(dryRun, files, pubBytes, "exec", "-T", "--index", idx, "dev-agent", "bash", "-lc", "cat > '"+whome+"'/.ssh/id_ed25519.pub && chmod 644 '"+whome+"'/.ssh/id_ed25519.pub") }
                if len(knownBytes) > 0 { runComposeInput(dryRun, files, knownBytes, "exec", "-T", "--index", idx, "dev-agent", "bash", "-lc", "cat > '"+whome+"'/.ssh/known_hosts && chmod 644 '"+whome+"'/.ssh/known_hosts") }
                runComposeInput(dryRun, files, []byte(cfg), "exec", "-T", "--index", idx, "dev-agent", "bash", "-lc", "cat > '"+whome+"'/.ssh/config && chmod 600 '"+whome+"'/.ssh/config")
                // wait for config to be visible and non-empty before git commands
                runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "for i in $(seq 1 20); do [ -s '"+whome+"'/.ssh/config ] && break || sleep 0.25; done")
                runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "export HOME='"+whome+"' && git config --global core.sshCommand 'ssh -F '"+whome+"'/.ssh/config'")
                runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "cd '"+wpath+"' && git config core.sshCommand 'ssh -F '"+whome+"'/.ssh/config'")
                runCompose(dryRun, files, "exec", "--index", idx, "dev-agent", "bash", "-lc", "cd '"+wpath+"' 2>/dev/null && GIT_SSH_COMMAND=\"ssh -F '"+whome+"'/.ssh/config\" git pull --ff-only || true")
            }
        }
        // Reuse tmux workflow
        sess := "devkit-worktrees"
        home1 := pth.AgentHomePath(project, "1", repo)
        if !skipTmux() {
            // Idempotency: kill existing session if present
            runHostBestEffort(dryRun, "tmux", "kill-session", "-t", sess)
            cmd := "docker compose "+strings.Join(files, " ")+" exec --index 1 dev-agent bash -lc 'mkdir -p \""+home1+"/.codex/rollouts\" \""+home1+"/.cache\" \""+home1+"/.config\" \""+home1+"/.local\"; export HOME=\""+home1+"\"; export CODEX_HOME=\""+home1+"/.codex\"; export CODEX_ROLLOUT_DIR=\""+home1+"/.codex/rollouts\"; export XDG_CACHE_HOME=\""+home1+"/.cache\"; export XDG_CONFIG_HOME=\""+home1+"/.config\"; cd \""+pth.AgentRepoPath(project, "1", repo)+"\"; exec bash'"
            runHost(dryRun, "tmux", tmuxutil.NewSession(sess, cmd)...)
            runHost(dryRun, "tmux", tmuxutil.RenameWindow(sess+":0", "agent-1")...)
            for i := 2; i <= n; i++ {
                whome := pth.AgentHomePath(project, fmt.Sprintf("%d", i), repo)
                wpath := pth.AgentRepoPath(project, fmt.Sprintf("%d", i), repo)
                wcmd := "docker compose "+strings.Join(files, " ")+fmt.Sprintf(" exec --index %d dev-agent bash -lc 'mkdir -p \"%s/.codex/rollouts\" \"%s/.cache\" \"%s/.config\" \"%s/.local\"; export HOME=\"%s\"; export CODEX_HOME=\"%s/.codex\"; export CODEX_ROLLOUT_DIR=\"%s/.codex/rollouts\"; export XDG_CACHE_HOME=\"%s/.cache\"; export XDG_CONFIG_HOME=\"%s/.config\"; cd \"%s\"; exec bash'", i, whome, whome, whome, whome, whome, whome, whome, whome, whome, wpath)
                runHost(dryRun, "tmux", tmuxutil.NewWindow(sess, fmt.Sprintf("agent-%d", i), wcmd)...)
            }
            runHostInteractive(dryRun, "tmux", tmuxutil.Attach(sess)...)
        }
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
        gitcmd := "git pull --ff-only"; if op == "--push" { gitcmd = "git push origin HEAD:main" }
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
        sess := "devkit-worktrees"
        home1 := pth.AgentHomePath(project, "1", repo)
        if !skipTmux() {
        cmd := "docker compose "+strings.Join(files, " ")+" exec --index 1 dev-agent bash -lc 'mkdir -p \""+home1+"/.codex/rollouts\" \""+home1+"/.cache\" \""+home1+"/.config\" \""+home1+"/.local\"; export HOME=\""+home1+"\"; export CODEX_HOME=\""+home1+"/.codex\"; export CODEX_ROLLOUT_DIR=\""+home1+"/.codex/rollouts\"; export XDG_CACHE_HOME=\""+home1+"/.cache\"; export XDG_CONFIG_HOME=\""+home1+"/.config\"; cd \""+pth.AgentRepoPath(project, "1", repo)+"\"; exec bash'"
            runHost(dryRun, "tmux", tmuxutil.NewSession(sess, cmd)...)
            runHost(dryRun, "tmux", tmuxutil.RenameWindow(sess+":0", "agent-1")...)
            for i := 2; i <= n; i++ {
                whome := pth.AgentHomePath(project, fmt.Sprintf("%d", i), repo)
                wpath := pth.AgentRepoPath(project, fmt.Sprintf("%d", i), repo)
            wcmd := "docker compose "+strings.Join(files, " ")+fmt.Sprintf(" exec --index %d dev-agent bash -lc 'mkdir -p \"%s/.codex/rollouts\" \"%s/.cache\" \"%s/.config\" \"%s/.local\"; export HOME=\"%s\"; export CODEX_HOME=\"%s/.codex\"; export CODEX_ROLLOUT_DIR=\"%s/.codex/rollouts\"; export XDG_CACHE_HOME=\"%s/.cache\"; export XDG_CONFIG_HOME=\"%s/.config\"; cd \"%s\"; exec bash'", i, whome, whome, whome, whome, whome, whome, whome, whome, whome, wpath)
                runHost(dryRun, "tmux", tmuxutil.NewWindow(sess, fmt.Sprintf("agent-%d", i), wcmd)...)
            }
            // tmux attach is long-lived: no timeout
            runHostInteractive(dryRun, "tmux", tmuxutil.Attach(sess)...)
        }
    case "bootstrap":
        // Opinionated: set up worktrees and open tmux with defaults if args omitted
        mustProject(project)
        if project != "dev-all" { die("Use -p dev-all for bootstrap") }
        var repo string
        var n int
        if len(sub) >= 2 {
            repo = sub[0]
            n = mustAtoi(sub[1])
        } else {
            // Try overlay defaults
            cfg, _ := config.ReadAll(paths.Root, project)
            if strings.TrimSpace(cfg.Defaults.Repo) == "" || cfg.Defaults.Agents < 1 {
                die("Usage: -p dev-all bootstrap <repo> <count> (or set defaults in overlays/dev-all/devkit.yaml)")
            }
            repo = cfg.Defaults.Repo
            n = cfg.Defaults.Agents
        }
        // Create worktrees and open tmux windows
        // Reuse this process: invoke internal handlers directly
        // Setup worktrees
        {
            // dev root path (parent of devkit)
            devRoot := filepath.Clean(filepath.Join(paths.Root, ".."))
            repoPath := filepath.Join(devRoot, repo)
            runHost(dryRun, "git", "-C", repoPath, "fetch", "--all", "--prune")
            runHost(dryRun, "git", "-C", repoPath, "config", "push.default", "upstream")
            runHost(dryRun, "git", "-C", repoPath, "config", "worktree.useRelativePaths", "true")
            base := "agent"; baseBranch := "main"
            cfg, _ := config.ReadAll(paths.Root, project)
            if strings.TrimSpace(cfg.Defaults.BranchPrefix) != "" { base = cfg.Defaults.BranchPrefix }
            if strings.TrimSpace(cfg.Defaults.BaseBranch) != "" { baseBranch = cfg.Defaults.BaseBranch }
            br1 := fmt.Sprintf("%s1", base)
            // preserve local work for agent1 by not resetting to origin
            runHost(dryRun, "git", "-C", repoPath, "checkout", "-B", br1)
            runHost(dryRun, "git", "-C", repoPath, "branch", "--set-upstream-to=origin/"+baseBranch, br1)
            for i := 2; i <= n; i++ {
                parent := filepath.Join(devRoot, fmt.Sprintf("%s%d", base, i))
                if !dryRun { _ = os.MkdirAll(parent, 0o755) }
                wt := filepath.Join(parent, repo)
                bri := fmt.Sprintf("%s%d", base, i)
                runHost(dryRun, "git", "-C", repoPath, "worktree", "add", wt, "-B", bri, "origin/"+baseBranch)
                runHost(dryRun, "git", "-C", wt, "branch", "--set-upstream-to=origin/"+baseBranch, bri)
            }
        }
        // Bring up N agents and open tmux using existing worktrees-tmux behavior
        runCompose(dryRun, compose.AllProfilesFiles(paths, project), "up", "-d", "--scale", fmt.Sprintf("dev-agent=%d", n))
        // finally open tmux
        // call existing worktrees-tmux handler logic inline
        {
            base := "/workspaces/dev"
            sess := "devkit-worktrees"
            home1 := base+"/"+repo+"/.devhome-agent1"
            if !skipTmux() {
                cmd := "docker compose "+strings.Join(compose.AllProfilesFiles(paths, project), " ")+" exec --index 1 dev-agent bash -lc 'mkdir -p \""+home1+"/.codex/rollouts\" \""+home1+"/.cache\" \""+home1+"/.config\" \""+home1+"/.local\"; export HOME=\""+home1+"\"; export CODEX_HOME=\""+home1+"/.codex\"; export CODEX_ROLLOUT_DIR=\""+home1+"/.codex/rollouts\"; export XDG_CACHE_HOME=\""+home1+"/.cache\"; export XDG_CONFIG_HOME=\""+home1+"/.config\"; cd \""+base+"/"+repo+"\"; exec bash'"
                runHost(dryRun, "tmux", tmuxutil.NewSession(sess, cmd)...)
                runHost(dryRun, "tmux", tmuxutil.RenameWindow(sess+":0", "agent-1")...)
                for i := 2; i <= n; i++ {
                    whome := fmt.Sprintf("%s/agent%d/.devhome-agent%d", base, i, i)
                    wpath := fmt.Sprintf("%s/agent%d/%s", base, i, repo)
                    wcmd := "docker compose "+strings.Join(compose.AllProfilesFiles(paths, project), " ")+fmt.Sprintf(" exec --index %d dev-agent bash -lc 'mkdir -p \"%s/.codex/rollouts\" \"%s/.cache\" \"%s/.config\" \"%s/.local\"; export HOME=\"%s\"; export CODEX_HOME=\"%s/.codex\"; export CODEX_ROLLOUT_DIR=\"%s/.codex/rollouts\"; export XDG_CACHE_HOME=\"%s/.cache\"; export XDG_CONFIG_HOME=\"%s/.config\"; cd \"%s\"; exec bash'", i, whome, whome, whome, whome, whome, whome, whome, whome, whome, wpath)
                    runHost(dryRun, "tmux", tmuxutil.NewWindow(sess, fmt.Sprintf("agent-%d", i), wcmd)...)
                }
                runHostInteractive(dryRun, "tmux", tmuxutil.Attach(sess)...)
            }
        }
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

// runComposeInteractive executes docker compose without a timeout for long-lived, interactive sessions.
func runComposeInteractive(dry bool, fileArgs []string, args ...string) {
    all := append([]string{"compose"}, append(fileArgs, args...)...)
    if dry {
        fmt.Fprintln(os.Stderr, "+ docker "+strings.Join(all, " "))
        return
    }
    res := execx.Run("docker", all...)
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

// runHostInteractive runs host commands without a timeout for long-lived interactive attachments (e.g., tmux attach).
func runHostInteractive(dry bool, name string, args ...string) {
    if dry {
        fmt.Fprintln(os.Stderr, "+ "+name+" "+strings.Join(args, " "))
        return
    }
    res := execx.Run(name, args...)
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

// rewriteWorktreeGitdir makes the .git file inside a worktree point to a relative gitdir
// so that it resolves correctly inside containers where the host absolute path differs.
func rewriteWorktreeGitdir(wt string) {
    // Resolve current gitdir
    out, res := execx.Capture(context.Background(), "git", "-C", wt, "rev-parse", "--git-dir")
    if res.Code != 0 { return }
    gitdir := strings.TrimSpace(out)
    if gitdir == "" { return }
    // Compute relative path from worktree dir to gitdir
    rel, err := filepath.Rel(wt, gitdir)
    if err != nil { return }
    // Write .git file with strict perms
    data := []byte("gitdir: "+rel+"\n")
    _ = os.WriteFile(filepath.Join(wt, ".git"), data, fs.FileMode(0644))
}

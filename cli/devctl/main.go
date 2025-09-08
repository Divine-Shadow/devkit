package main

import (
    "errors"
    "flag"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "time"

    "devkit/cli/devctl/internal/compose"
    "devkit/cli/devctl/internal/config"
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

    cmd := args[0]
    sub := args[1:]
    switch cmd {
    case "up":
        mustProject(project)
        runCompose(files, "up", "-d")
    case "down":
        mustProject(project)
        runCompose(files, "down")
    case "restart":
        mustProject(project)
        runCompose(files, "restart")
    case "status":
        mustProject(project)
        runCompose(files, "ps")
    case "logs":
        mustProject(project)
        runCompose(files, append([]string{"logs"}, sub...)...)
    case "scale":
        mustProject(project)
        n := "1"
        if len(sub) > 0 { n = sub[0] }
        runCompose(files, "up", "-d", "--scale", "dev-agent="+n)
    case "exec":
        mustProject(project)
        if len(sub) == 0 { die("exec requires <index> and <cmd>") }
        idx := sub[0]
        rest := []string{}
        if len(sub) > 1 { rest = sub[1:] }
        runCompose(files, append([]string{"exec", "--index", idx, "dev-agent"}, rest...)...)
    case "attach":
        mustProject(project)
        idx := "1"
        if len(sub) > 0 { idx = sub[0] }
        runCompose(files, "attach", "--index", idx, "dev-agent")
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
    case "warm":
        mustProject(project)
        hooks, _ := config.ReadHooks(paths.Root, project)
        if strings.TrimSpace(hooks.Warm) == "" {
            fmt.Println("No warm hook defined")
            return
        }
        runCompose(files, "exec", "dev-agent", "bash", "-lc", hooks.Warm)
    case "maintain":
        mustProject(project)
        hooks, _ := config.ReadHooks(paths.Root, project)
        if strings.TrimSpace(hooks.Maintain) == "" {
            fmt.Println("No maintain hook defined")
            return
        }
        runCompose(files, "exec", "dev-agent", "bash", "-lc", hooks.Maintain)
    case "check-net":
        mustProject(project)
        script := "set -x; env | grep -E 'HTTP(S)?_PROXY|NO_PROXY'; curl -Is https://github.com | head -n1; (curl -Is https://example.com | head -n1 || true)"
        runCompose(files, "exec", "dev-agent", "bash", "-lc", script)
    default:
        usage(); os.Exit(2)
    }
}

func die(msg string) { fmt.Fprintln(os.Stderr, msg); os.Exit(2) }
func mustProject(p string) { if strings.TrimSpace(p) == "" { die("-p <project> is required") } }

func runCompose(fileArgs []string, args ...string) {
    // add a default timeout for safety
    ctx, cancel := execx.WithTimeout(10 * time.Minute)
    defer cancel()
    all := append([]string{"compose"}, append(fileArgs, args...)...)
    res := execx.RunCtx(ctx, "docker", all...)
    if res.Code != 0 { os.Exit(res.Code) }
}


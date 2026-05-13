package network

import (
	"fmt"
	"strings"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	runner "devkit/cli/devctl/internal/runner"
)

// Register adds proxy/check commands to the registry.
func Register(r *cmdregistry.Registry) {
	r.Register("proxy", handleProxy)
	r.Register("check-net", handleCheckNet)
	r.Register("check-codex", handleCheckCodex)
}

func handleProxy(ctx *cmdregistry.Context) error {
	project := strings.TrimSpace(ctx.Project)
	if project == "" {
		return fmt.Errorf("-p <project> is required")
	}
	which := "tinyproxy"
	if len(ctx.Args) > 0 && strings.TrimSpace(ctx.Args[0]) != "" {
		which = strings.TrimSpace(ctx.Args[0])
	}
	switch which {
	case "tinyproxy":
		fmt.Println("Switching agent env to tinyproxy... (ensure overlay uses HTTP(S)_PROXY=http://tinyproxy:8888)")
	case "envoy":
		fmt.Println("Enable envoy profile: add --profile envoy to up/restart commands")
	default:
		return fmt.Errorf("unknown proxy: %s", which)
	}
	return nil
}

func handleCheckNet(ctx *cmdregistry.Context) error {
	project := strings.TrimSpace(ctx.Project)
	if project == "" {
		return fmt.Errorf("-p <project> is required")
	}
	script := "set -x; env | grep -E 'HTTP(S)?_PROXY|NO_PROXY'; curl -Is https://github.com | head -n1; (curl -Is https://example.com | head -n1 || true)"
	if !nativeRuntimeConfigured(ctx) {
		return fmt.Errorf("check-net requires an overlay with runtime.flake")
	}
	runNativeScript(ctx, script)
	return nil
}

func handleCheckCodex(ctx *cmdregistry.Context) error {
	project := strings.TrimSpace(ctx.Project)
	if project == "" {
		return fmt.Errorf("-p <project> is required")
	}
	if !nativeRuntimeConfigured(ctx) {
		return fmt.Errorf("check-codex requires an overlay with runtime.flake")
	}
	script := strings.Join([]string{
		`echo "== Env vars =="`,
		`env | grep -E '^HTTPS?_PROXY=|^NO_PROXY=' || true`,
		`echo "== Curl checks (through proxy) =="`,
		`set -e; echo -n 'chatgpt.com          : '; curl -sSvo /dev/null -w '%{http_code}\n' https://chatgpt.com || true`,
		`set -e; echo -n 'chatgpt.com/backend..: '; curl -sSvo /dev/null -w '%{http_code}\n' https://chatgpt.com/backend-api/codex/responses || true`,
		`timeout 15s codex exec 'Reply with: ok' || true`,
	}, "\n")
	runNativeScript(ctx, script)
	return nil
}

func runNativeScript(ctx *cmdregistry.Context, script string) {
	exe := strings.TrimSpace(ctx.Exe)
	if exe == "" {
		exe = "devkit"
	}
	runner.Host(ctx.DryRun, exe, "-p", strings.TrimSpace(ctx.Project), "exec", "1", "--repo", nativeRepo(ctx), "--", "bash", "-lc", script)
}

func nativeRepo(ctx *cmdregistry.Context) string {
	project := strings.TrimSpace(ctx.Project)
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, project)
	if err == nil {
		if repo := strings.TrimSpace(cfg.Defaults.Repo); repo != "" {
			return repo
		}
	}
	if project != "" && project != "dev-all" {
		return project
	}
	return "ouroboros-ide"
}

func nativeRuntimeConfigured(ctx *cmdregistry.Context) bool {
	project := strings.TrimSpace(ctx.Project)
	if project == "" {
		return false
	}
	if project == "dev-all" {
		return true
	}
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, project)
	return err == nil && config.HasRuntimeFlake(cfg)
}

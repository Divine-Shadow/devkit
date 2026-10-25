package launch_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/commands/nativecmd"
	"devkit/cli/devctl/internal/devkitpaths"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"gopkg.in/yaml.v3"
)

// The supported dry-run renderer supplies the actual command that is executed.
// This bounded accommodation avoids requiring the unrelated Docker station
// broker for a Java-only workload. It qualifies CLI configuration/rendering and
// real sandbox entry, not station-owner acquisition or Desktop/app-server startup.
func TestManagementCLIEntryWithoutIssuer(t *testing.T) {
	if os.Getenv("DEVKIT_TEST_RECEIVING_DEVCTL") == "" {
		t.Skip("requires immutable candidate Devctl, runtime shell and Bubblewrap")
	}
	launch.RunManagementEntryWithoutIssuerFixture(t, func(t *testing.T, p nativeplan.Plan, root string) {
		t.Setenv("DEVKIT_RUNTIME_SHELL_LAUNCHER", p.RuntimeLauncher)
		t.Setenv("DEVKIT_RUNTIME_BWRAP_BINARY", p.BubblewrapBinary)
		overlays := filepath.Join(root, "cli-overlays")
		overlay := map[string]any{
			"runtime": map[string]any{"flake": p.Flake},
			"broker":  map[string]any{"socket": filepath.Join(root, "unused-broker.sock"), "upstream": "unix://" + filepath.Join(root, "unused-docker.sock")},
			"native": map[string]any{
				"host_root":                  filepath.Dir(p.DevkitHostRoot),
				"worktree_root":              p.HostWorktreeRoot,
				"state_root":                 p.HostStateRoot,
				"worktree_container_root":    p.SandboxWorktreeRoot,
				"state_container_root":       p.SandboxStateRoot,
				"required_isolation_profile": "workspace-egress",
				"isolation_profiles": map[string]any{"workspace-egress": map[string]any{
					"filesystem": "workspace-only", "egress_allowlist": p.Proxy.AllowlistPath,
					"proxy_socket": p.Proxy.UnixSocket,
				}},
			},
		}
		data, err := yaml.Marshal(overlay)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(overlays, "dev-workspace", "devkit.yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		registry := cmdregistry.New()
		nativecmd.Register(registry)
		handler, ok := registry.Lookup("exec")
		if !ok {
			t.Fatal("public exec command is not registered")
		}
		entry := cmdregistry.Context{
			DryRun: true, Project: "dev-workspace",
			Exe:   os.Getenv("DEVKIT_TEST_RECEIVING_DEVCTL"),
			Paths: devkitpaths.Paths{Root: p.DevkitHostRoot, Kit: filepath.Join(p.DevkitHostRoot, "kit"), RuntimeAuthorityRoot: p.RuntimeAuthorityRoot, OverlayPaths: []string{overlays}},
		}
		cliPlan, err := nativecmd.BuildAgentPlan(&entry, p.Agent.ID.Repo, p.Agent.ID.Index)
		if err != nil {
			t.Fatal(err)
		}
		if cliPlan.Agent.HostWorktree != p.Agent.HostWorktree || cliPlan.Agent.HostHome != p.Agent.HostHome || cliPlan.ControllerProfile != p.ControllerProfile || cliPlan.Proxy != p.Proxy {
			t.Fatal("public CLI selected different fixture geometry, profile or proxy")
		}
		if err := launch.Prepare(cliPlan); err != nil {
			t.Fatal(err)
		}
		p = cliPlan
		run := func(command []string) string {
			t.Helper()
			capture, err := os.CreateTemp(root, "cli-command-")
			if err != nil {
				t.Fatal(err)
			}
			previous := os.Stdout
			func() {
				os.Stdout = capture
				defer func() { os.Stdout = previous }()
				invocation := entry
				invocation.Args = append([]string{strconv.Itoa(p.Agent.ID.Index), "--repo", p.Agent.ID.Repo, "--proxy-socket", p.Proxy.UnixSocket, "--"}, command...)
				err = handler(&invocation)
			}()
			capture.Close()
			if err != nil {
				t.Fatalf("public CLI rendering: %v", err)
			}
			rendered, err := os.ReadFile(capture.Name())
			if err != nil {
				t.Fatal(err)
			}
			diagnostic := "native egress proxy socket=" + p.Proxy.UnixSocket + " allowlist=" + p.Proxy.AllowlistPath + "\n"
			if !strings.HasPrefix(string(rendered), diagnostic) {
				t.Fatalf("missing expected CLI proxy diagnostic: %s", rendered)
			}
			rendered = rendered[len(diagnostic):]
			if !strings.Contains(string(rendered), "--unshare-net") || strings.Contains(string(rendered), "'--ro-bind' '/run/fleet-controller-operation/identity.json'") {
				t.Fatalf("unexpected public CLI rendering: %s", rendered)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash", "-c", string(rendered))
			cmd.Dir = p.DevkitHostRoot
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("candidate CLI-rendered entry: %v\n%s", err, output)
			}
			return string(output)
		}
		launch.RunControllerCandidateReceiving(t, p, root, run)
	})
}

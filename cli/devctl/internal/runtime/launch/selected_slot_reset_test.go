package launch

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

// Link the existing package-owned manifest location into a disposable test
// binary, like the native CLI integration fixture. Production gains no hook,
// flag or environment override. No launcher, SSH or reset effect is executed.
func TestSelectedSlotResetProducerFeedsManagedGitConsumer(t *testing.T) {
	if os.Getenv("DEVKIT_TEST_SELECTED_RESET_CHILD") != "1" {
		root := t.TempDir()
		var records []map[string]any
		for index := 1; index <= 3; index++ {
			workspace := fmt.Sprintf("/home/bayesartre/dev/agent-worktrees/agent%d", index)
			homeRoot := workspace
			if index == 1 {
				homeRoot += "/ouroboros-ide"
			}
			kind := "local-wsl-devkit-agent"
			if index == 3 {
				kind = "devkit-agent"
			}
			records = append(records, map[string]any{"targetId": fmt.Sprintf("fixture-%d", index), "configProfile": "product-governance", "project": "dev-all", "repo": "ouroboros-ide", "agentIndex": index, "workspaceRoot": workspace, "hostWorktree": workspace + "/ouroboros-ide", "hostHome": fmt.Sprintf("%s/.devhome-agent%d", homeRoot, index), "kind": kind, "expectedExecutionHost": "fixture-host", "source": "/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-config.toml", "sourceSha256": strings.Repeat("a", 64)})
		}
		data, err := json.Marshal(map[string]any{"schemaVersion": "devkit/gui-codex-config-projections/v1", "projections": records})
		if err != nil {
			t.Fatal(err)
		}
		manifest := filepath.Join(root, "projections.json")
		if err := os.WriteFile(manifest, data, 0600); err != nil {
			t.Fatal(err)
		}
		binary := filepath.Join(root, "launch.test")
		build := exec.Command("go", "test", "-c", "-ldflags", "-X=devkit/cli/devctl/internal/runtime/plan.guiCodexConfigProjectionManifestPath="+manifest, "-o", binary, ".")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("compile source-bound test consumer: %v\n%s", err, out)
		}
		child := exec.Command(binary, "-test.run=^TestSelectedSlotResetProducerFeedsManagedGitConsumer$", "-test.v")
		child.Env = append(os.Environ(), "DEVKIT_TEST_SELECTED_RESET_CHILD=1")
		out, err := child.CombinedOutput()
		t.Logf("source-bound child:\n%s", out)
		if err != nil {
			t.Fatalf("producer-consumer regression: %v", err)
		}
		return
	}
	fixture := managedGitFixturePlan(t, "/unused", filepath.Join(t.TempDir(), "current.sock"))
	for index := 1; index <= 3; index++ {
		opts := nativeplan.BuildOptions{Paths: devkitpaths.Paths{Root: "/repo/devkit", RuntimeAuthorityRoot: fixture.RuntimeAuthorityRoot}, HostRoot: "/home/bayesartre/dev", Project: "dev-all", Repo: "ouroboros-ide", Index: index, GUITargetID: fmt.Sprintf("fixture-%d", index), IsolationProfile: nativeplan.IsolationProfileWorkspaceEgress, EgressAllowlist: "/fixture/allowlist", ProxySocket: fixture.Proxy.UnixSocket}
		prefix, err := nativeplan.BuildDevAll(opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := managedRuntimeGitSSHCommand(prefix); err == nil || !strings.Contains(err.Error(), "standard native geometry") {
			t.Fatalf("unfixed prefix case %d did not fail strict consumer: %v", index, err)
		}
		p, err := nativeplan.BuildSelectedSlotReset(opts)
		if err != nil {
			t.Fatal(err)
		}
		command, err := managedRuntimeGitSSHCommand(p)
		if err != nil || !strings.Contains(command, "DEVKIT_MANAGED_GIT_HOME="+shellQuote(p.Agent.SandboxHome)) || !strings.Contains(command, fixture.Proxy.UnixSocket) {
			t.Fatalf("selected producer case %d refused or lost current connector: %v %s", index, err, command)
		}
		for _, key := range []string{"HOME", "GIT_SSH_COMMAND", "GIT_SSH_VARIANT", "DEVKIT_MANAGED_GIT_HOME", "DEVKIT_MANAGED_GIT_KNOWN_HOSTS"} {
			p.Env[key] = "hostile"
			if _, err := managedRuntimeGitSSHCommand(p); err == nil {
				t.Fatalf("selected case %d admitted %s override", index, key)
			}
			delete(p.Env, key)
		}
		p.Agent.SandboxHome += "/foreign"
		if _, err := managedRuntimeGitSSHCommand(p); err == nil {
			t.Fatal("foreign sandbox home admitted")
		}
		t.Logf("slot=%d source_constructor=true strict_consumer=true current_connector=true override_refusals=true", index)
	}
}

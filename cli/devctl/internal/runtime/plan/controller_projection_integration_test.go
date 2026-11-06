package plan_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/runtime/launch"
	"devkit/cli/devctl/internal/runtime/plan"
)

func TestControllerProjectionPlanArgumentsAndLaunchRevalidation(t *testing.T) {
	for _, index := range []int{1, 2} {
		t.Run(string(rune('0'+index)), func(t *testing.T) {
			f := plan.NewControllerFixture(t, index)
			p, err := plan.Build(f.Options)
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(p.LauncherArgs, "\x00")
			for _, args := range [][]string{{"--ro-bind", p.Controller.ManifestSource, "/etc/fleet/controller-operation-authority.json"}, {"--bind", f.Socket, f.Socket}, {"--bind", filepath.Join(f.Workspace, "wsl-nix"), "/workspaces/dev/wsl-nix"}, {"--setenv", "FLEET_CONTROLLER_OPERATION_HANDLE", "required"}} {
				if !strings.Contains(joined, strings.Join(args, "\x00")) {
					t.Fatalf("missing fixed argument tuple %q", args)
				}
			}
			for _, bind := range p.Binds {
				if strings.Contains(bind.Source, "/credentials/") || strings.Contains(bind.Target, "/credentials/") {
					t.Fatal("credential binding exposed")
				}
			}
			before, err := os.ReadDir(p.Agent.HostHome)
			if err != nil {
				t.Fatal(err)
			}
			f.ReplaceSocket(t)
			if err := launch.Prepare(p); err == nil || !strings.Contains(err.Error(), "controller capability projection refused") {
				t.Fatal("preparation did not refuse replaced projection first")
			}
			after, err := os.ReadDir(p.Agent.HostHome)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("refused preparation mutated home")
			}
			if _, err := launch.BuildBubblewrap(p, []string{"/bin/true"}); err == nil {
				t.Fatal("launch accepted changed socket")
			}
		})
	}
}

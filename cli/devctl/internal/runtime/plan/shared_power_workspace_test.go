package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
)

func sharedPowerWorkspaceOptions(t *testing.T, index int) BuildOptions {
	t.Helper()
	root := t.TempDir()
	worktreeRoot := filepath.Join(root, "project-worktrees")
	return BuildOptions{
		Paths:   devkitpaths.Paths{Root: filepath.Join(root, "devkit")},
		Project: "pokeemerald-expansion-shared-power", Repo: "pokeemerald-expansion-shared-power",
		Index: index, AgentStatePrefix: "pokeemerald", WorktreeRoot: worktreeRoot,
		WorkspaceRoot:    filepath.Join(worktreeRoot, fmt.Sprintf("pokeemerald-agent%d", index)),
		IsolationProfile: IsolationProfileWorkspaceEgress, EgressAllowlist: filepath.Join(root, "allowlist.txt"),
	}
}

func TestSharedPowerWorkspaceRootUsesSelectedGeometryAndHistoricalHome(t *testing.T) {
	for _, index := range []int{1, 2} {
		t.Run(fmt.Sprintf("agent%d", index), func(t *testing.T) {
			opts := sharedPowerWorkspaceOptions(t, index)
			p, err := Build(opts)
			if err != nil {
				t.Fatal(err)
			}
			if p.Agent.HostWorktree != filepath.Join(opts.WorkspaceRoot, opts.Repo) ||
				p.Agent.SandboxWorktree != "/workspaces/dev/"+opts.Repo {
				t.Fatalf("wrong selected worktree: %#v", p.Agent)
			}
			wantHome := filepath.Join(filepath.Dir(opts.Paths.Root), ".devkit", "native-agents", fmt.Sprintf("pokeemerald-agent%d", index), "home")
			if p.Agent.HostHome != wantHome || p.Agent.SandboxHome != fmt.Sprintf("/agent-state/pokeemerald-agent%d/home", index) {
				t.Fatalf("historical home moved: %#v", p.Agent)
			}
			if !hasExactBind(p.Binds, Bind{Source: opts.WorkspaceRoot, Target: "/workspaces/dev", Mode: "rw", Required: true}) || p.WindowsMountsVisible {
				t.Fatalf("missing lane isolation: %#v", p.Binds)
			}
			for _, bind := range p.Binds {
				if bind.Source == opts.WorktreeRoot || bind.Source == filepath.Dir(opts.Paths.Root) ||
					strings.Contains(bind.Source, fmt.Sprintf("pokeemerald-agent%d", 3-index)) || isMntFilesystemPath(bind.Source) {
					t.Fatalf("broad, sibling or Windows bind: %#v", bind)
				}
			}
			opts.WorkspaceRoot = ""
			legacy, err := Build(opts)
			if err != nil {
				t.Fatal(err)
			}
			if legacy.Agent.HostWorktree != filepath.Join(opts.WorktreeRoot, fmt.Sprintf("agent%d", index), opts.Repo) {
				t.Fatalf("legacy prefix geometry changed: %#v", legacy.Agent)
			}
		})
	}
}

func TestSharedPowerWorkspaceRootRejectsWrongLaneAndExternalGitMetadata(t *testing.T) {
	base := sharedPowerWorkspaceOptions(t, 2)
	for name, mutate := range map[string]func(*BuildOptions){
		"other lane":    func(o *BuildOptions) { o.WorkspaceRoot = filepath.Join(o.WorktreeRoot, "pokeemerald-agent1") },
		"legacy lane":   func(o *BuildOptions) { o.WorkspaceRoot = filepath.Join(o.WorktreeRoot, "agent2") },
		"other project": func(o *BuildOptions) { o.Project = "pokeemerald" },
		"other repo":    func(o *BuildOptions) { o.Repo = "pokeemerald" },
		"other index": func(o *BuildOptions) {
			o.Index = 3
			o.WorkspaceRoot = filepath.Join(o.WorktreeRoot, "pokeemerald-agent3")
		},
		"noncanonical":      func(o *BuildOptions) { o.WorkspaceRoot += "/../pokeemerald-agent2" },
		"without isolation": func(o *BuildOptions) { o.IsolationProfile = ""; o.EgressAllowlist = "" },
	} {
		t.Run(name, func(t *testing.T) {
			opts := base
			mutate(&opts)
			if _, err := Build(opts); err == nil {
				t.Fatalf("accepted %s", name)
			}
		})
	}
	worktree := filepath.Join(base.WorkspaceRoot, base.Repo)
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+external+"\\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(base); err == nil || !strings.Contains(err.Error(), "Git metadata outside the lane root") {
		t.Fatalf("external metadata accepted: %v", err)
	}
}

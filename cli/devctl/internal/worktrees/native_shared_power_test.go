package worktrees

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupNativeSelectedSharedPowerMaterializesPortableIsolatedCheckout(t *testing.T) {
	const repo = "pokeemerald-expansion-shared-power"
	for _, index := range []int{1, 2} {
		t.Run(fmt.Sprintf("agent%d", index), func(t *testing.T) {
			root := t.TempDir()
			devRoot := filepath.Join(root, "dev")
			makeRepoWithBare(t, root, devRoot, repo)
			worktreeRoot := filepath.Join(devRoot, "project-worktrees")
			workspaceRoot := filepath.Join(worktreeRoot, fmt.Sprintf("pokeemerald-agent%d", index))
			selected := filepath.Join(workspaceRoot, repo)
			// An existing sibling, old checkout, old home/history and shared
			// manifest must remain byte-for-byte unchanged during fresh setup.
			protected := []string{
				filepath.Join(worktreeRoot, fmt.Sprintf("pokeemerald-agent%d", 3-index), repo, "active"),
				filepath.Join(devRoot, "agent-worktrees", fmt.Sprintf("agent%d", index), repo, "old"),
				filepath.Join(devRoot, ".devkit", "native-agents", fmt.Sprintf("pokeemerald-agent%d", index), "home", ".codex", "history"),
				filepath.Join(devRoot, ".devkit", "native-agents", "shared-power-manifest.json"),
			}
			for _, file := range protected {
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte("preserve me"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			opts := NativeOptions{
				DevkitRoot: filepath.Join(devRoot, "devkit"), Repo: repo,
				Origin: filepath.Join(root, "remotes", repo+".git"),
				Index:  index, Count: 2, WorktreeRoot: worktreeRoot,
				WorkspaceRoot: workspaceRoot, BaseBranch: "main", BranchPrefix: "agent",
			}
			if err := SetupNative(opts); err != nil {
				t.Fatal(err)
			}
			if got := readTrim(t, "git", "-C", selected, "status", "--porcelain"); got != "" {
				t.Fatalf("fresh checkout is dirty: %s", got)
			}
			if got := readTrim(t, "git", "-C", selected, "rev-parse", "HEAD"); got != readTrim(t, "git", "-C", filepath.Join(devRoot, repo), "rev-parse", "HEAD") {
				t.Fatal("fresh checkout is not fetched current main")
			}
			common, err := nativeWorktreeCommonDirectory(selected)
			if err != nil {
				t.Fatal(err)
			}
			wantCommon := filepath.Join(workspaceRoot, ".devkit", "git", fmt.Sprintf("agent%d", index), repo+".git")
			if common != wantCommon {
				t.Fatalf("common repository = %s, want %s", common, wantCommon)
			}
			gitdir, err := readGitdirPointer(filepath.Join(selected, ".git"))
			if err != nil || filepath.IsAbs(gitdir) {
				t.Fatalf("nonportable Git metadata: %q, %v", gitdir, err)
			}
			// Repeated preparation preserves source edits and never rebuilds
			// an active sibling or the historical prefix.
			if err := os.WriteFile(filepath.Join(selected, "owner-change"), []byte("in progress"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := SetupNative(opts); err != nil {
				t.Fatal(err)
			}
			for _, file := range protected {
				data, err := os.ReadFile(file)
				if err != nil || string(data) != "preserve me" {
					t.Fatalf("changed protected path %s: %q, %v", file, data, err)
				}
			}
			if _, err := os.Stat(filepath.Join(selected, "owner-change")); err != nil {
				t.Fatal(err)
			}
			for _, wrong := range []string{
				filepath.Join(worktreeRoot, fmt.Sprintf("agent%d", index)),
				filepath.Join(workspaceRoot, fmt.Sprintf("agent%d", index)),
			} {
				if _, err := os.Stat(wrong); !os.IsNotExist(err) {
					t.Fatalf("materialized wrong geometry %s: %v", wrong, err)
				}
			}
			// A lane-root projection retains all relative Git relationships.
			projected := filepath.Join(root, "projected")
			if err := os.Rename(workspaceRoot, projected); err != nil {
				t.Fatal(err)
			}
			if got := readTrim(t, "git", "-C", filepath.Join(projected, repo), "rev-parse", "--show-toplevel"); got != filepath.Join(projected, repo) {
				t.Fatalf("projected checkout is not usable: %s", got)
			}
		})
	}
}

func TestSetupNativeSelectedSharedPowerRejectsForeignGeometryBeforeEffects(t *testing.T) {
	root := t.TempDir()
	base := NativeOptions{DevkitRoot: filepath.Join(root, "devkit"), Repo: "pokeemerald-expansion-shared-power",
		Index: 2, Count: 2, WorktreeRoot: filepath.Join(root, "project-worktrees"),
		WorkspaceRoot: filepath.Join(root, "project-worktrees", "pokeemerald-agent2")}
	for name, mutate := range map[string]func(*NativeOptions){
		"other repo":       func(o *NativeOptions) { o.Repo = "other" },
		"unselected":       func(o *NativeOptions) { o.Index = 0 },
		"other index":      func(o *NativeOptions) { o.Index = 1 },
		"outside capacity": func(o *NativeOptions) { o.Count = 1 },
		"other root":       func(o *NativeOptions) { o.WorkspaceRoot = filepath.Join(root, "escape") },
		"legacy root":      func(o *NativeOptions) { o.WorkspaceRoot = filepath.Join(o.WorktreeRoot, "agent2") },
		"noncanonical":     func(o *NativeOptions) { o.WorkspaceRoot += "/../pokeemerald-agent2" },
		"reconstruction":   func(o *NativeOptions) { o.ReconstructSelected = true },
		"mnt": func(o *NativeOptions) {
			o.WorktreeRoot = "/mnt/c/projects"
			o.WorkspaceRoot = "/mnt/c/projects/pokeemerald-agent2"
		},
	} {
		t.Run(name, func(t *testing.T) {
			opts := base
			mutate(&opts)
			if err := PreflightNative(opts); err == nil {
				t.Fatalf("accepted %s", name)
			}
		})
	}
	external := t.TempDir()
	if err := os.Symlink(external, base.WorktreeRoot); err != nil {
		t.Fatal(err)
	}
	if err := PreflightNative(base); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("accepted symlinked parent for absent checkout: %v", err)
	}
	entries, err := os.ReadDir(external)
	if err != nil || len(entries) != 0 {
		t.Fatalf("changed external root: %v, %v", entries, err)
	}
}

func TestSelectedSharedPowerRejectsSymlinkedGitParentBeforeBootstrap(t *testing.T) {
	root := t.TempDir()
	opts := NativeOptions{DevkitRoot: filepath.Join(root, "devkit"), Repo: "pokeemerald-expansion-shared-power",
		Index: 1, Count: 2, WorktreeRoot: filepath.Join(root, "project-worktrees"),
		WorkspaceRoot: filepath.Join(root, "project-worktrees", "pokeemerald-agent1")}
	if err := os.MkdirAll(opts.WorkspaceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(opts.WorkspaceRoot, ".devkit")); err != nil {
		t.Fatal(err)
	}
	if err := PreflightNative(opts); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("accepted symlinked Git parent: %v", err)
	}
	entries, err := os.ReadDir(external)
	if err != nil || len(entries) != 0 {
		t.Fatalf("changed external metadata root: %v, %v", entries, err)
	}
}

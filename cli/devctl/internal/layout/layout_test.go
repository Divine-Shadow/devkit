package layout

import (
    "os"
    "path/filepath"
    "testing"
)

func TestReadAndCleanPath(t *testing.T) {
    tmp := t.TempDir()
    y := `session: devkit:mixed
windows:
  - index: 1
    path: ouroboros-ide
    name: ouro-1
    service: dev-agent
  - index: 2
    path: /abs/path
    name: abs
`
    p := filepath.Join(tmp, "layout.yaml")
    if err := os.WriteFile(p, []byte(y), 0644); err != nil { t.Fatal(err) }
    f, err := Read(p)
    if err != nil { t.Fatalf("read failed: %v", err) }
    if f.Session != "devkit:mixed" || len(f.Windows) != 2 { t.Fatalf("unexpected: %+v", f) }
    if CleanPath("dev-all", f.Windows[0].Path) != "/workspaces/dev/ouroboros-ide" {
        t.Fatalf("clean dev-all rel mismatch")
    }
    if CleanPath("codex", f.Windows[0].Path) != "/workspace/ouroboros-ide" {
        t.Fatalf("clean codex rel mismatch")
    }
    if CleanPath("dev-all", f.Windows[1].Path) != "/abs/path" {
        t.Fatalf("clean abs mismatch")
    }
}

func TestReadOverlayWorktrees(t *testing.T) {
    tmp := t.TempDir()
    y := `session: devkit:mixed
overlays:
  - project: dev-all
    service: dev-agent
    count: 2
    profiles: dns
    worktrees:
      repo: dumb-onion-hax
      count: 2
      base_branch: main
      branch_prefix: agent
windows: []
`
    p := filepath.Join(tmp, "layout.yaml")
    if err := os.WriteFile(p, []byte(y), 0644); err != nil { t.Fatal(err) }
    f, err := Read(p)
    if err != nil { t.Fatalf("read failed: %v", err) }
    if len(f.Overlays) != 1 { t.Fatalf("expected 1 overlay, got %d", len(f.Overlays)) }
    ov := f.Overlays[0]
    if ov.Project != "dev-all" || ov.Worktrees == nil {
        t.Fatalf("overlay/worktrees not parsed: %+v", ov)
    }
    if ov.Worktrees.Repo != "dumb-onion-hax" || ov.Worktrees.Count != 2 {
        t.Fatalf("worktrees values wrong: %+v", ov.Worktrees)
    }
    if ov.Worktrees.BaseBranch != "main" || ov.Worktrees.BranchPrefix != "agent" {
        t.Fatalf("branch settings wrong: %+v", ov.Worktrees)
    }
}

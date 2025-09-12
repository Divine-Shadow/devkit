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


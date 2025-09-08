package compose

import (
    "path/filepath"
    "testing"
)

func TestFiles_DefaultDns(t *testing.T) {
    p := Paths{Root: "/repo/devkit", Kit: "/repo/devkit/kit", Overlays: "/repo/devkit/overlays"}
    got, err := Files(p, "codex", "")
    if err != nil { t.Fatal(err) }
    wantFirst := filepath.Join(p.Kit, "compose.yml")
    if len(got) < 2 || got[0] != "-f" || got[1] != wantFirst {
        t.Fatalf("unexpected base files: %v", got)
    }
}


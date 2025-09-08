package files

import (
    "os"
    "path/filepath"
    "testing"
)

func TestAppendLineIfMissing(t *testing.T) {
    dir := t.TempDir()
    p := filepath.Join(dir, "list.txt")
    added, err := AppendLineIfMissing(p, "example.com")
    if err != nil || !added { t.Fatalf("first add: added=%v err=%v", added, err) }
    added, err = AppendLineIfMissing(p, "example.com")
    if err != nil || added { t.Fatalf("duplicate not detected: added=%v err=%v", added, err) }
    data, _ := os.ReadFile(p)
    if string(data) != "example.com" { t.Fatalf("content=%q", string(data)) }
}


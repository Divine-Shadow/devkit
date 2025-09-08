package config

import (
    "bufio"
    "os"
    "path/filepath"
    "strings"
)

type Hooks struct {
    Warm     string
    Maintain string
}

// ReadHooks attempts a minimal parse of overlays/<project>/devkit.yaml to extract warm/maintain.
// Intentionally dependency-free; replace with YAML parsing in a later phase.
func ReadHooks(root, project string) (Hooks, error) {
    var h Hooks
    if project == "" { return h, nil }
    path := filepath.Join(root, "overlays", project, "devkit.yaml")
    f, err := os.Open(path)
    if err != nil { return h, nil } // missing is fine
    defer f.Close()
    s := bufio.NewScanner(f)
    for s.Scan() {
        line := strings.TrimSpace(s.Text())
        if strings.HasPrefix(line, "warm:") && h.Warm == "" {
            h.Warm = strings.TrimSpace(strings.TrimPrefix(line, "warm:"))
        } else if strings.HasPrefix(line, "maintain:") && h.Maintain == "" {
            h.Maintain = strings.TrimSpace(strings.TrimPrefix(line, "maintain:"))
        }
    }
    return h, nil
}


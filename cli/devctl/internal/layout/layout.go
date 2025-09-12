package layout

import (
    "os"
    "path/filepath"
    "strings"

    yaml "gopkg.in/yaml.v3"
)

type Window struct {
    Index   int    `yaml:"index"`
    Path    string `yaml:"path"`
    Name    string `yaml:"name"`
    Service string `yaml:"service"`
    Project string `yaml:"project"`
}

type File struct {
    Session string   `yaml:"session"`
    Windows []Window `yaml:"windows"`
}

func Read(p string) (File, error) {
    var f File
    b, err := os.ReadFile(p)
    if err != nil { return f, err }
    if err := yaml.Unmarshal(b, &f); err != nil { return f, err }
    return f, nil
}

// CleanPath normalizes a subpath into a container path based on overlay project.
// If subpath is absolute, it is returned as-is.
// For dev-all, relative paths resolve under /workspaces/dev.
// For codex, relative paths resolve under /workspace.
func CleanPath(project, subpath string) string {
    if strings.HasPrefix(subpath, "/") { return filepath.Clean(subpath) }
    switch project {
    case "dev-all":
        return filepath.Join("/workspaces/dev", subpath)
    default:
        return filepath.Join("/workspace", subpath)
    }
}


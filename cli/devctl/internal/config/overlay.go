package config

import (
    "os"
    "path/filepath"

    "gopkg.in/yaml.v3"
)

type Hooks struct {
    Warm     string
    Maintain string
}

type overlayYAML struct {
    Hooks struct {
        Warm     string `yaml:"warm"`
        Maintain string `yaml:"maintain"`
    } `yaml:"hooks"`
}

// ReadHooks parses overlays/<project>/devkit.yaml using YAML and returns warm/maintain hooks.
func ReadHooks(root, project string) (Hooks, error) {
    var h Hooks
    if project == "" { return h, nil }
    path := filepath.Join(root, "overlays", project, "devkit.yaml")
    data, err := os.ReadFile(path)
    if err != nil { return h, nil }
    var oy overlayYAML
    if err := yaml.Unmarshal(data, &oy); err != nil {
        return h, nil
    }
    h.Warm = oy.Hooks.Warm
    h.Maintain = oy.Hooks.Maintain
    return h, nil
}

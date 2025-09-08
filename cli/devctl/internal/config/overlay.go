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

// ReadHooks parses overlays/<project>/devkit.yaml to extract hooks.warm and hooks.maintain.
// Minimal YAML subset: supports top-level mappings and nested `hooks:` mapping with scalar values.
// This is dependency-free to avoid network fetch; swap for a YAML lib later.
func ReadHooks(root, project string) (Hooks, error) {
    var h Hooks
    if project == "" {
        return h, nil
    }
    path := filepath.Join(root, "overlays", project, "devkit.yaml")
    f, err := os.Open(path)
    if err != nil {
        return h, nil // missing is fine
    }
    defer f.Close()

    s := bufio.NewScanner(f)
    inHooks := false
    indentHooks := 0
    for s.Scan() {
        raw := s.Text()
        line := strings.TrimRight(raw, "\r\n")
        // strip comments
        if i := strings.Index(line, "#"); i >= 0 {
            line = line[:i]
        }
        if strings.TrimSpace(line) == "" {
            continue
        }
        // measure indent (spaces only)
        indent := len(line) - len(strings.TrimLeft(line, " "))
        trimmed := strings.TrimSpace(line)

        if !inHooks {
            // detect hooks:
            if trimmed == "hooks:" {
                inHooks = true
                indentHooks = indent
                continue
            }
            continue
        }
        // we are in hooks block; if indentation less or equal, we left the block
        if indent <= indentHooks {
            inHooks = false
            indentHooks = 0
            // continue parsing in case hooks repeats later
            if trimmed == "hooks:" {
                inHooks = true
                indentHooks = indent
            }
            continue
        }
        // parse key: value
        kv := strings.SplitN(strings.TrimSpace(line), ":", 2)
        if len(kv) != 2 {
            continue
        }
        key := strings.TrimSpace(kv[0])
        val := strings.TrimSpace(kv[1])
        val = trimQuotes(val)
        switch key {
        case "warm":
            if h.Warm == "" { h.Warm = val }
        case "maintain":
            if h.Maintain == "" { h.Maintain = val }
        }
    }
    return h, nil
}

func trimQuotes(s string) string {
    s = strings.TrimSpace(s)
    if len(s) >= 2 {
        if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
            return s[1:len(s)-1]
        }
    }
    return s
}

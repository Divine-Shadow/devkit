package launch

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

type Command struct {
	Path string
	Args []string
	Dir  string
}

func Prepare(p nativeplan.Plan) error {
	if strings.TrimSpace(p.Agent.HostWorktree) == "" {
		return fmt.Errorf("host worktree is empty")
	}
	if st, err := os.Stat(p.Agent.HostWorktree); err != nil {
		return fmt.Errorf("host worktree %s: %w", p.Agent.HostWorktree, err)
	} else if !st.IsDir() {
		return fmt.Errorf("host worktree %s is not a directory", p.Agent.HostWorktree)
	}
	for _, dir := range []string{p.Agent.HostHome, p.Agent.StateRoot} {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	for _, rel := range []string{
		filepath.Join(".codex", "rollouts"),
		".cache",
		".config",
		".local",
		".sbt",
	} {
		dir := filepath.Join(p.Agent.HostHome, rel)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	if err := ensureResolvConf(p.DNS.ResolvConf); err != nil {
		return err
	}
	for _, bind := range p.Binds {
		if !bind.Required {
			continue
		}
		if _, err := os.Stat(bind.Source); err != nil {
			return fmt.Errorf("required bind source %s: %w", bind.Source, err)
		}
	}
	return nil
}

func BuildBubblewrap(p nativeplan.Plan, command []string) (Command, error) {
	if strings.TrimSpace(p.DevkitSandboxRoot) == "" {
		return Command{}, fmt.Errorf("devkit sandbox root is empty")
	}
	args := []string{
		"--die-with-parent",
		"--share-net",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
	}
	dirSet := map[string]bool{}
	dirArgs := []string{}
	bindArgs := []string{}
	var addDir func(string)
	addDir = func(path string) {
		path = filepath.Clean(path)
		if path == "." || path == "/" || dirSet[path] {
			return
		}
		parent := filepath.Dir(path)
		if parent != "/" && !dirSet[parent] {
			addDir(parent)
		}
		dirArgs = append(dirArgs, "--dir", path)
		dirSet[path] = true
	}
	addBind := func(mode, source, target string, required bool) error {
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if source == "" || target == "" {
			if required {
				return fmt.Errorf("required bind has empty source or target")
			}
			return nil
		}
		if _, err := os.Stat(source); err != nil {
			if required {
				addDir(filepath.Dir(target))
				if mode == "ro" {
					bindArgs = append(bindArgs, "--ro-bind", source, target)
				} else {
					bindArgs = append(bindArgs, "--bind", source, target)
				}
			}
			return nil
		}
		addDir(filepath.Dir(target))
		if mode == "ro" {
			bindArgs = append(bindArgs, "--ro-bind", source, target)
		} else {
			bindArgs = append(bindArgs, "--bind", source, target)
		}
		return nil
	}

	if err := addBind("ro", "/nix/store", "/nix/store", true); err != nil {
		return Command{}, err
	}
	if err := addBind("ro", "/nix/var/nix", "/nix/var/nix", true); err != nil {
		return Command{}, err
	}
	_ = addBind("ro", "/run/current-system", "/run/current-system", false)
	_ = addBind("ro", "/etc/nix", "/etc/nix", false)
	_ = addBind("ro", "/etc/passwd", "/etc/passwd", false)
	_ = addBind("ro", "/etc/group", "/etc/group", false)

	for _, bind := range p.Binds {
		if bind.Target == "/nix/store" || bind.Target == "/etc/resolv.conf" {
			continue
		}
		if err := addBind(bind.Mode, bind.Source, bind.Target, bind.Required); err != nil {
			return Command{}, err
		}
	}
	if strings.TrimSpace(p.DNS.ResolvConf) != "" {
		if err := addBind("ro", p.DNS.ResolvConf, "/etc/resolv.conf", false); err != nil {
			return Command{}, err
		}
	} else {
		_ = addBind("ro", "/etc/resolv.conf", "/etc/resolv.conf", false)
	}

	args = append(args, dirArgs...)
	args = append(args, bindArgs...)

	keys := make([]string, 0, len(p.Env))
	for key := range p.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--setenv", key, p.Env[key])
	}

	args = append(args, "--chdir", p.DevkitSandboxRoot)
	args = append(args, "/run/current-system/sw/bin/nix", "--extra-experimental-features", "nix-command flakes", "develop", p.Flake, "--command")
	args = append(args, shellCommand(p.Agent.SandboxWorktree, command)...)
	return Command{Path: "bwrap", Args: args, Dir: p.DevkitHostRoot}, nil
}

func ShellString(cmd Command) string {
	parts := append([]string{cmd.Path}, cmd.Args...)
	for i, part := range parts {
		parts[i] = shellQuote(part)
	}
	return strings.Join(parts, " ")
}

func ensureResolvConf(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat resolv.conf %s: %w", path, err)
	}
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return fmt.Errorf("read /etc/resolv.conf: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write resolv.conf %s: %w", path, err)
	}
	return nil
}

func shellCommand(workdir string, command []string) []string {
	script := "cd " + shellQuote(workdir)
	if len(command) == 0 {
		script += " && exec ${SHELL:-bash}"
	} else {
		quoted := make([]string, 0, len(command))
		for _, arg := range command {
			quoted = append(quoted, shellQuote(arg))
		}
		script += " && exec " + strings.Join(quoted, " ")
	}
	return []string{"bash", "-lc", script}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

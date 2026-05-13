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
	if err := SeedCodexAuth(p.Agent.HostHome, false); err != nil {
		return err
	}
	if err := SeedSSH(p.Agent.HostHome, false); err != nil {
		return err
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

func SeedSSH(hostHome string, force bool) error {
	hostHome = strings.TrimSpace(hostHome)
	if hostHome == "" {
		return nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(userHome) == "" {
		return nil
	}
	srcDir := filepath.Join(userHome, ".ssh")
	if st, err := os.Stat(srcDir); err != nil || !st.IsDir() {
		return nil
	}
	targetDir := filepath.Join(hostHome, ".ssh")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", targetDir, err)
	}
	for _, file := range []string{
		"id_ed25519",
		"id_ed25519.pub",
		"id_rsa",
		"id_rsa.pub",
		"known_hosts",
	} {
		src := filepath.Join(srcDir, file)
		data, err := os.ReadFile(src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read SSH seed %s: %w", src, err)
		}
		target := filepath.Join(targetDir, file)
		if !force {
			if _, err := os.Stat(target); err == nil {
				continue
			} else if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("stat SSH seed target %s: %w", target, err)
			}
		}
		mode := os.FileMode(0o600)
		if strings.HasSuffix(file, ".pub") || file == "known_hosts" {
			mode = 0o644
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return fmt.Errorf("write SSH seed %s: %w", target, err)
		}
	}
	return nil
}

func SeedCodexAuth(hostHome string, force bool) error {
	hostHome = strings.TrimSpace(hostHome)
	if hostHome == "" {
		return nil
	}
	src := strings.TrimSpace(os.Getenv("CODEX_AUTH_JSON"))
	if src == "" {
		codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
		if codexHome == "" {
			if home, err := os.UserHomeDir(); err == nil {
				codexHome = filepath.Join(home, ".codex")
			}
		}
		if codexHome != "" {
			src = filepath.Join(codexHome, "auth.json")
		}
	}
	if src == "" {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read Codex auth %s: %w", src, err)
	}
	targetDir := filepath.Join(hostHome, ".codex")
	target := filepath.Join(targetDir, "auth.json")
	if !force {
		if _, err := os.Stat(target); err == nil {
			return nil
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("stat Codex auth %s: %w", target, err)
		}
	}
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", targetDir, err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return fmt.Errorf("write Codex auth %s: %w", target, err)
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
	dirSet := map[string]bool{"/tmp": true}
	dirArgs := []string{}
	bindArgs := []string{}
	symlinkArgs := []string{}
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
	addSymlink := func(target, linkPath string) {
		addDir(filepath.Dir(linkPath))
		symlinkArgs = append(symlinkArgs, "--symlink", target, linkPath)
	}

	if err := addBind("ro", "/nix/store", "/nix/store", true); err != nil {
		return Command{}, err
	}
	if err := addBind("ro", "/nix/var/nix", "/nix/var/nix", true); err != nil {
		return Command{}, err
	}
	_ = addBind("ro", "/run/current-system", "/run/current-system", false)
	_ = addBind("ro", "/etc/nix", "/etc/nix", false)
	_ = addBind("ro", "/etc/static", "/etc/static", false)
	_ = addBind("ro", "/etc/ssl", "/etc/ssl", false)
	_ = addBind("ro", "/etc/pki", "/etc/pki", false)
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
	addSymlink("/run/current-system/sw/bin/env", "/usr/bin/env")
	addSymlink("/run/current-system/sw/bin/bash", "/bin/bash")
	addSymlink("/run/current-system/sw/bin/sh", "/bin/sh")

	args = append(args, dirArgs...)
	args = append(args, bindArgs...)
	args = append(args, symlinkArgs...)

	keys := make([]string, 0, len(p.Env))
	for key := range p.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--setenv", key, p.Env[key])
	}

	args = append(args, "--chdir", p.DevkitSandboxRoot)
	args = append(args, "/run/current-system/sw/bin/nix", "--extra-experimental-features", "nix-command flakes", "develop", p.Flake, "--no-write-lock-file", "--command")
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

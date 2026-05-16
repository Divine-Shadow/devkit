package launch

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	if err := migrateMissingCodexState(p.Agent.HostHome, filepath.Join(p.Agent.StateRoot, "home")); err != nil {
		return err
	}
	if err := repairRetiredCodexWrapper(p.Agent.HostHome); err != nil {
		return err
	}
	if err := installProjectCodexRules(p); err != nil {
		return err
	}
	if err := ensureScalaCacheDirs(p); err != nil {
		return err
	}
	if err := capCodexTUILog(p.Agent.HostHome); err != nil {
		return err
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

func ensureScalaCacheDirs(p nativeplan.Plan) error {
	for _, key := range []string{"COURSIER_CACHE", "SBT_IVY_HOME", "SBT_GLOBAL_BASE", "SBT_BOOT_DIR"} {
		sandboxPath := strings.TrimSpace(p.Env[key])
		if sandboxPath == "" {
			continue
		}
		hostPath, ok := sandboxPathToHost(p, sandboxPath)
		if !ok {
			continue
		}
		if err := os.MkdirAll(hostPath, 0o755); err != nil {
			return fmt.Errorf("mkdir %s for %s: %w", hostPath, key, err)
		}
	}
	return nil
}

func sandboxPathToHost(p nativeplan.Plan, sandboxPath string) (string, bool) {
	sandboxPath = filepath.Clean(sandboxPath)
	if strings.TrimSpace(p.Agent.SandboxHome) != "" {
		if rel, err := filepath.Rel(filepath.Clean(p.Agent.SandboxHome), sandboxPath); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.Join(p.Agent.HostHome, rel), true
		}
	}
	const workspaceRoot = "/workspaces/dev"
	if rel, err := filepath.Rel(workspaceRoot, sandboxPath); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.Join(filepath.Dir(p.DevkitHostRoot), rel), true
	}
	return "", false
}

const defaultCodexTUILogMaxBytes int64 = 256 * 1024 * 1024

func codexTUILogMaxBytes() (int64, error) {
	raw := strings.TrimSpace(os.Getenv("DEVKIT_CODEX_TUI_LOG_MAX_BYTES"))
	if raw == "" {
		return defaultCodexTUILogMaxBytes, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid DEVKIT_CODEX_TUI_LOG_MAX_BYTES=%q: %w", raw, err)
	}
	return value, nil
}

func capCodexTUILog(hostHome string) error {
	maxBytes, err := codexTUILogMaxBytes()
	if err != nil {
		return err
	}
	if maxBytes <= 0 || strings.TrimSpace(hostHome) == "" {
		return nil
	}
	path := filepath.Join(hostHome, ".codex", "log", "codex-tui.log")
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Size() <= maxBytes {
		return nil
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	if _, err := file.Seek(info.Size()-maxBytes, io.SeekStart); err != nil {
		return fmt.Errorf("seek %s: %w", path, err)
	}
	tail, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read tail from %s: %w", path, err)
	}
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("truncate %s: %w", path, err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind %s: %w", path, err)
	}
	if _, err := file.Write(tail); err != nil {
		return fmt.Errorf("write capped tail to %s: %w", path, err)
	}
	return nil
}

func installProjectCodexRules(p nativeplan.Plan) error {
	if strings.TrimSpace(p.Agent.ID.Project) != "dev-all" || strings.TrimSpace(p.Agent.ID.Repo) != "ouroboros-ide" {
		return nil
	}
	if strings.TrimSpace(p.Agent.HostHome) == "" || strings.TrimSpace(p.DevkitHostRoot) == "" {
		return nil
	}
	source := filepath.Join(p.DevkitHostRoot, "overlays", "dev-all", "codex-governed-search-policy.rules")
	data, err := os.ReadFile(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read governed search policy %s: %w", source, err)
	}
	target := filepath.Join(p.Agent.HostHome, ".codex", "rules", "governed-search-policy.rules")
	if existing, err := os.ReadFile(target); err == nil && string(existing) == string(data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return fmt.Errorf("write governed search policy %s: %w", target, err)
	}
	return nil
}

func repairRetiredCodexWrapper(hostHome string) error {
	hostHome = strings.TrimSpace(hostHome)
	if hostHome == "" {
		return nil
	}
	zshrc := filepath.Join(hostHome, ".zshrc")
	data, err := os.ReadFile(zshrc)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", zshrc, err)
	}
	original := string(data)
	repaired := original
	const retired = "/usr/local/bin/codex"
	if strings.Contains(repaired, retired) {
		repaired = strings.ReplaceAll(repaired, retired, "command codex")
	}
	if strings.Contains(repaired, "codex() {") && !strings.Contains(repaired, "devkit_codex_tui_log_guard()") {
		repaired = strings.Replace(repaired, "codex() {\n", codexTUILogGuardZsh+"\ncodex() {\n", 1)
	}
	const codexCommand = `  HOME="$HOME" CODEX_HOME="$HOME/.codex" CODEX_ROLLOUT_DIR="$HOME/.codex/rollouts" command codex "${extra[@]}" "$@"`
	if strings.Contains(repaired, codexCommand) && !strings.Contains(repaired, "  devkit_codex_tui_log_guard\n"+codexCommand) {
		repaired = strings.Replace(repaired, codexCommand, "  devkit_codex_tui_log_guard\n"+codexCommand, 1)
	}
	if repaired == original {
		return nil
	}
	if err := os.WriteFile(zshrc, []byte(repaired), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", zshrc, err)
	}
	return nil
}

const codexTUILogGuardZsh = `devkit_codex_tui_log_guard() {
  local log="$HOME/.codex/log/codex-tui.log"
  local max="${DEVKIT_CODEX_TUI_LOG_MAX_BYTES:-268435456}"
  [[ "$max" == <-> ]] || return 0
  (( max > 0 )) || return 0
  [[ -f "$log" ]] || return 0
  local size tmp
  size=$(wc -c < "$log" 2>/dev/null) || return 0
  (( size > max )) || return 0
  tmp="${log}.tmp.$$"
  tail -c "$max" "$log" > "$tmp" 2>/dev/null && cat "$tmp" > "$log"
  rm -f "$tmp"
}`

func migrateMissingCodexState(dstHome, srcHome string) error {
	dstHome = strings.TrimSpace(dstHome)
	srcHome = strings.TrimSpace(srcHome)
	if dstHome == "" || srcHome == "" || sameFilesystemPath(dstHome, srcHome) {
		return nil
	}
	srcCodex := filepath.Join(srcHome, ".codex")
	dstCodex := filepath.Join(dstHome, ".codex")
	if st, err := os.Stat(srcCodex); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat legacy Codex home %s: %w", srcCodex, err)
	} else if !st.IsDir() {
		return nil
	}
	if err := os.MkdirAll(dstCodex, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dstCodex, err)
	}
	for _, rel := range []string{"sessions", "rollouts", "shell_snapshots", "log"} {
		if err := copyTreeMissing(filepath.Join(srcCodex, rel), filepath.Join(dstCodex, rel)); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(srcCodex)
	if err != nil {
		return fmt.Errorf("read legacy Codex home %s: %w", srcCodex, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !shouldCopyCodexRootFile(entry.Name()) {
			continue
		}
		if err := copyFileMissing(filepath.Join(srcCodex, entry.Name()), filepath.Join(dstCodex, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func sameFilesystemPath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	if resolvedA, err := filepath.EvalSymlinks(a); err == nil {
		a = filepath.Clean(resolvedA)
	}
	if resolvedB, err := filepath.EvalSymlinks(b); err == nil {
		b = filepath.Clean(resolvedB)
	}
	return a == b
}

func shouldCopyCodexRootFile(name string) bool {
	switch name {
	case "history.jsonl", "installation_id", "models_cache.json", "version.json", ".personality_migration":
		return true
	}
	return strings.HasPrefix(name, "state_") || strings.HasPrefix(name, "logs_")
}

func copyTreeMissing(srcRoot, dstRoot string) error {
	if st, err := os.Stat(srcRoot); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", srcRoot, err)
	} else if !st.IsDir() {
		return nil
	}
	return filepath.WalkDir(srcRoot, func(src string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcRoot, src)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstRoot, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(dst, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFileMissing(src, dst)
	})
}

func copyFileMissing(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if _, err := os.Stat(dst); err == nil {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", dst, err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	if err := os.Chtimes(dst, info.ModTime(), info.ModTime()); err != nil {
		return fmt.Errorf("preserve mtime for %s: %w", dst, err)
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
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
	}
	if strings.TrimSpace(p.Proxy.UnixSocket) != "" {
		args = append(args, "--unshare-net")
	} else {
		args = append(args, "--share-net")
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
		value := p.Env[key]
		if key == "XDG_CACHE_HOME" {
			value = filepath.Join("/tmp", "devkit-nix-cache", p.Agent.ID.Name())
		}
		args = append(args, "--setenv", key, value)
	}

	args = append(args, "--chdir", p.DevkitSandboxRoot)
	args = append(args, "/run/current-system/sw/bin/nix", "--extra-experimental-features", "nix-command flakes", "develop", p.Flake, "--output-lock-file", "/dev/null", "--command")
	args = append(args, shellCommand(p.DevkitSandboxRoot, p.Agent.ID.Project, p.Agent.SandboxWorktree, command, p.Proxy, p.Env)...)
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

func shellCommand(devkitRoot string, project string, workdir string, command []string, proxy nativeplan.ProxyConfig, env map[string]string) []string {
	exports := make([]string, 0, len(env))
	for key, value := range env {
		exports = append(exports, "export "+key+"="+shellQuote(value))
	}
	sort.Strings(exports)
	script := strings.Join(exports, "; ")
	if script != "" {
		script += "; "
	}
	script += "cd " + shellQuote(workdir)
	bridgeProxy := strings.TrimSpace(proxy.UnixSocket) != ""
	if bridgeProxy {
		proxyURL := strings.TrimSpace(proxy.HTTPProxy)
		if proxyURL == "" {
			proxyURL = "http://127.0.0.1:18888"
		}
		devctlPath := filepath.Join(devkitRoot, "kit", "bin", "devctl")
		script += " && { " + shellQuote(devctlPath) + " -p " + shellQuote(project) + " native proxy-bridge --listen 127.0.0.1:18888 --socket " + shellQuote(proxy.UnixSocket) + " & devkit_proxy_bridge_pid=$!; }"
		script += " && trap 'kill \"$devkit_proxy_bridge_pid\" >/dev/null 2>&1 || true; wait \"$devkit_proxy_bridge_pid\" >/dev/null 2>&1 || true' EXIT"
		script += " && sleep 0.1"
		script += " && { kill -0 \"$devkit_proxy_bridge_pid\" >/dev/null 2>&1 || { echo 'native proxy bridge failed to start' >&2; exit 1; }; }"
		script += " && export HTTP_PROXY=" + shellQuote(proxyURL)
		script += " HTTPS_PROXY=" + shellQuote(proxyURL)
		script += " http_proxy=" + shellQuote(proxyURL)
		script += " https_proxy=" + shellQuote(proxyURL)
		script += " NO_PROXY=" + shellQuote(proxy.NoProxy)
		script += " no_proxy=" + shellQuote(proxy.NoProxy)
	}
	if len(command) == 0 {
		if bridgeProxy {
			script += " && ${SHELL:-bash}"
		} else {
			script += " && exec ${SHELL:-bash}"
		}
	} else {
		quoted := make([]string, 0, len(command))
		for _, arg := range command {
			quoted = append(quoted, shellQuote(arg))
		}
		if bridgeProxy {
			script += " && " + strings.Join(quoted, " ")
		} else {
			script += " && exec " + strings.Join(quoted, " ")
		}
	}
	return []string{"bash", "-lc", script}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	DefaultSocket   = "/run/devkit/test-container-broker.sock"
	DefaultUpstream = "unix:///var/run/docker.sock"
)

type Config struct {
	DevkitRoot        string
	StateRoot         string
	Socket            string
	Upstream          string
	AllowedImages     []string
	SocketBindAliases []string
	AllowPulls        bool
	LogLevel          string
	Binary            string
	StartTimeout      time.Duration
}

type State struct {
	PID               int       `json:"pid"`
	Socket            string    `json:"socket"`
	Upstream          string    `json:"upstream"`
	AllowedImages     []string  `json:"allowed_images"`
	SocketBindAliases []string  `json:"socket_bind_aliases,omitempty"`
	AllowPulls        bool      `json:"allow_pulls"`
	LogLevel          string    `json:"log_level"`
	Binary            string    `json:"binary"`
	LogPath           string    `json:"log_path"`
	StartedAt         time.Time `json:"started_at"`
}

type Status struct {
	Running      bool     `json:"running"`
	PID          int      `json:"pid,omitempty"`
	Socket       string   `json:"socket"`
	SocketExists bool     `json:"socket_exists"`
	StateRoot    string   `json:"state_root"`
	PIDFile      string   `json:"pid_file"`
	StateFile    string   `json:"state_file"`
	LogPath      string   `json:"log_path"`
	StaleState   bool     `json:"stale_state"`
	Message      string   `json:"message,omitempty"`
	State        *State   `json:"state,omitempty"`
	Command      []string `json:"command,omitempty"`
}

func DefaultStateRoot(devkitRoot string) string {
	root := filepath.Clean(devkitRoot)
	if root == "." || root == string(filepath.Separator) {
		return filepath.Join(root, ".devkit", "native-broker")
	}
	return filepath.Join(filepath.Dir(root), ".devkit", "native-broker")
}

func Normalize(c Config) Config {
	c.DevkitRoot = filepath.Clean(strings.TrimSpace(c.DevkitRoot))
	if c.DevkitRoot == "." {
		c.DevkitRoot = ""
	}
	if strings.TrimSpace(c.StateRoot) == "" {
		c.StateRoot = DefaultStateRoot(c.DevkitRoot)
	}
	c.StateRoot = filepath.Clean(c.StateRoot)
	if strings.TrimSpace(c.Socket) == "" {
		c.Socket = DefaultSocket
	}
	if strings.TrimSpace(c.Upstream) == "" {
		c.Upstream = DefaultUpstream
	}
	if len(c.AllowedImages) == 0 {
		c.AllowedImages = []string{"postgres:latest"}
	}
	c.SocketBindAliases = normalizeSocketBindAliases(c)
	if strings.TrimSpace(c.LogLevel) == "" {
		c.LogLevel = "info"
	}
	if c.StartTimeout <= 0 {
		c.StartTimeout = 5 * time.Second
	}
	return c
}

func normalizeSocketBindAliases(c Config) []string {
	aliases := append([]string{}, c.SocketBindAliases...)
	if alias := sandboxSocketAlias(c.DevkitRoot, c.Socket); alias != "" {
		aliases = append(aliases, alias)
	}
	return uniqueCleanPaths(aliases)
}

func sandboxSocketAlias(devkitRoot, socket string) string {
	devkitRoot = filepath.Clean(strings.TrimSpace(devkitRoot))
	socket = filepath.Clean(strings.TrimSpace(socket))
	if devkitRoot == "" || devkitRoot == "." || socket == "" || socket == "." {
		return ""
	}
	devRoot := filepath.Dir(devkitRoot)
	rel, err := filepath.Rel(devRoot, socket)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return ""
	}
	return filepath.ToSlash(filepath.Join("/workspaces/dev", rel))
}

func uniqueCleanPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		cleaned := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
		if cleaned == "" || cleaned == "." {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

func PIDFile(c Config) string {
	c = Normalize(c)
	return filepath.Join(c.StateRoot, "broker.pid")
}

func StateFile(c Config) string {
	c = Normalize(c)
	return filepath.Join(c.StateRoot, "broker.json")
}

func LogPath(c Config) string {
	c = Normalize(c)
	return filepath.Join(c.StateRoot, "broker.log")
}

func Env(c Config) []string {
	c = Normalize(c)
	env := append([]string{}, os.Environ()...)
	env = append(env,
		"BROKER_LISTEN=unix://"+c.Socket,
		"BROKER_UPSTREAM="+c.Upstream,
		"BROKER_ALLOWED_IMAGES="+strings.Join(c.AllowedImages, ","),
		"BROKER_ALLOW_PULLS="+strconv.FormatBool(c.AllowPulls),
		"BROKER_LOG_LEVEL="+c.LogLevel,
	)
	if len(c.SocketBindAliases) > 0 {
		env = append(env, "BROKER_SOCKET_BIND_ALIASES="+strings.Join(c.SocketBindAliases, ","))
	}
	return env
}

func ResolveBinary(ctx context.Context, c Config) (string, error) {
	_ = ctx
	c = Normalize(c)
	binary := strings.TrimSpace(c.Binary)
	if binary == "" {
		return "", fmt.Errorf("immutable postgres-broker binary is required; use the authoritative runtime package")
	}
	if !filepath.IsAbs(binary) {
		return "", fmt.Errorf("immutable postgres-broker binary must be an absolute path: %s", binary)
	}
	info, err := os.Stat(binary)
	if err != nil {
		return "", fmt.Errorf("stat immutable postgres-broker binary %s: %w", binary, err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("immutable postgres-broker binary is not executable: %s", binary)
	}
	return filepath.Clean(binary), nil
}

func Inspect(c Config) (Status, error) {
	c = Normalize(c)
	status := Status{
		Socket:    c.Socket,
		StateRoot: c.StateRoot,
		PIDFile:   PIDFile(c),
		StateFile: StateFile(c),
		LogPath:   LogPath(c),
	}
	data, err := os.ReadFile(status.StateFile)
	if err == nil {
		var state State
		if json.Unmarshal(data, &state) == nil {
			status.State = &state
			if state.Socket != "" {
				status.Socket = state.Socket
			}
			if state.LogPath != "" {
				status.LogPath = state.LogPath
			}
		}
	}
	if info, err := os.Stat(status.Socket); err == nil {
		status.SocketExists = info.Mode()&os.ModeSocket != 0
	} else if !errors.Is(err, os.ErrNotExist) {
		return status, err
	}

	pid, err := readPID(status.PIDFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			status.Message = "broker is not started"
			return status, nil
		}
		status.StaleState = true
		status.Message = err.Error()
		return status, nil
	}
	status.PID = pid
	if !processRunning(pid) {
		status.StaleState = true
		status.Message = "broker pid file is stale"
		return status, nil
	}
	if !managedProcess(status) {
		status.StaleState = true
		status.Message = "broker pid belongs to a different process"
		return status, nil
	}
	if !socketAcceptsConnections(status.Socket) {
		status.StaleState = true
		status.Message = "broker process is running but its socket is not accepting connections"
		return status, nil
	}
	status.Running = true
	status.Message = "broker is running"
	return status, nil
}

func Start(ctx context.Context, c Config, dryRun bool) (Status, error) {
	c = Normalize(c)
	status, err := Inspect(c)
	if err != nil {
		return status, err
	}
	if status.Running {
		return status, nil
	}

	if dryRun {
		binary, err := ResolveBinary(ctx, c)
		if err != nil {
			return status, err
		}
		status.Command = []string{binary}
		status.Message = "dry run: broker would be started"
		return status, nil
	}

	binary, err := ResolveBinary(ctx, c)
	if err != nil {
		return status, err
	}
	status.Command = []string{binary}
	if processRunning(status.PID) && managedProcess(status) {
		if err := terminateManagedProcess(status); err != nil {
			return status, fmt.Errorf("stop unresponsive managed postgres-broker: %w", err)
		}
	}

	if err := os.MkdirAll(c.StateRoot, 0o700); err != nil {
		return status, fmt.Errorf("mkdir broker state root %s: %w", c.StateRoot, err)
	}
	if err := os.MkdirAll(filepath.Dir(c.Socket), 0o755); err != nil {
		return status, fmt.Errorf("mkdir broker socket dir %s: %w", filepath.Dir(c.Socket), err)
	}
	if err := removeStaleSocket(c.Socket); err != nil {
		return status, err
	}

	logFile, err := os.OpenFile(LogPath(c), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return status, fmt.Errorf("open broker log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command(binary)
	cmd.Env = Env(c)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return status, fmt.Errorf("start postgres-broker: %w", err)
	}

	state := State{
		PID:               cmd.Process.Pid,
		Socket:            c.Socket,
		Upstream:          c.Upstream,
		AllowedImages:     append([]string{}, c.AllowedImages...),
		SocketBindAliases: append([]string{}, c.SocketBindAliases...),
		AllowPulls:        c.AllowPulls,
		LogLevel:          c.LogLevel,
		Binary:            binary,
		LogPath:           LogPath(c),
		StartedAt:         time.Now(),
	}
	if err := writeState(c, state); err != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		return status, err
	}
	if err := waitForSocket(c.Socket, c.StartTimeout); err != nil {
		if !processRunning(cmd.Process.Pid) {
			return status, fmt.Errorf("postgres-broker exited before socket became ready; see %s", LogPath(c))
		}
		return status, err
	}
	return Inspect(c)
}

func Stop(c Config, dryRun bool) (Status, error) {
	c = Normalize(c)
	status, err := Inspect(c)
	if err != nil {
		return status, err
	}
	if !status.Running {
		if dryRun {
			status.Message = "dry run: broker is not running"
			return status, nil
		}
		if processRunning(status.PID) {
			if !managedProcess(status) {
				return status, fmt.Errorf("refusing to stop unmanaged process %d; remove stale state under %s after inspection", status.PID, c.StateRoot)
			}
			if err := terminateManagedProcess(status); err != nil {
				return status, err
			}
		}
		if socketAcceptsConnections(status.Socket) {
			return status, fmt.Errorf("refusing to remove active unmanaged broker socket %s", status.Socket)
		}
		_ = cleanupState(c)
		status.Message = "broker is not running"
		return status, nil
	}
	if dryRun {
		status.Message = "dry run: broker would be stopped"
		return status, nil
	}
	if !managedProcess(status) {
		return status, fmt.Errorf("refusing to stop unmanaged process %d; remove stale state under %s after inspection", status.PID, c.StateRoot)
	}
	if err := syscall.Kill(status.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return status, fmt.Errorf("stop broker pid %d: %w", status.PID, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for processRunning(status.PID) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if processRunning(status.PID) {
		_ = syscall.Kill(status.PID, syscall.SIGKILL)
	}
	if err := cleanupState(c); err != nil {
		return status, err
	}
	return Inspect(c)
}

func writeState(c Config, state State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(PIDFile(c), []byte(strconv.Itoa(state.PID)+"\n"), 0o600); err != nil {
		return fmt.Errorf("write broker pid: %w", err)
	}
	if err := os.WriteFile(StateFile(c), data, 0o600); err != nil {
		return fmt.Errorf("write broker state: %w", err)
	}
	return nil
}

func cleanupState(c Config) error {
	for _, path := range []string{PIDFile(c), StateFile(c)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if info, err := os.Stat(c.Socket); err == nil && info.Mode()&os.ModeSocket != 0 {
		_ = os.Remove(c.Socket)
	}
	return nil
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid < 1 {
		return 0, fmt.Errorf("invalid pid file %s", path)
	}
	return pid, nil
}

func processRunning(pid int) bool {
	if pid < 1 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func terminateManagedProcess(status Status) error {
	if !managedProcess(status) {
		return fmt.Errorf("refusing to stop unmanaged process %d", status.PID)
	}
	if err := syscall.Kill(status.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("stop broker pid %d: %w", status.PID, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for processRunning(status.PID) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !processRunning(status.PID) {
		return nil
	}
	if err := syscall.Kill(status.PID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill unresponsive broker pid %d: %w", status.PID, err)
	}
	deadline = time.Now().Add(1 * time.Second)
	for processRunning(status.PID) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if processRunning(status.PID) {
		return fmt.Errorf("broker pid %d remained alive after SIGKILL", status.PID)
	}
	return nil
}

func managedProcess(status Status) bool {
	if status.PID < 1 || status.State == nil || status.State.PID != status.PID ||
		strings.TrimSpace(status.State.Binary) == "" || strings.TrimSpace(status.State.Socket) == "" {
		return false
	}
	exe, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(status.PID), "exe"))
	if err != nil || filepath.Clean(exe) != filepath.Clean(status.State.Binary) {
		return false
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(status.PID), "environ"))
	if err != nil {
		return false
	}
	want := "BROKER_LISTEN=unix://" + status.State.Socket
	for _, entry := range strings.Split(string(data), "\x00") {
		if entry == want {
			return true
		}
	}
	return false
}

func removeStaleSocket(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("broker socket path exists and is not a socket: %s", path)
	}
	conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("broker socket %s already accepts connections; refusing to replace it", path)
	}
	return os.Remove(path)
}

func socketAcceptsConnections(path string) bool {
	conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("broker socket %s did not become ready within %s: %w", path, timeout, lastErr)
}

package broker

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	Owner             *StationOwner
}

type State struct {
	PID               int       `json:"pid"`
	StartTicks        uint64    `json:"startTicks,omitempty"`
	SocketDevice      uint64    `json:"socketDevice,omitempty"`
	SocketInode       uint64    `json:"socketInode,omitempty"`
	OwnerService      string    `json:"ownerService,omitempty"`
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
	StateDigest  string   `json:"-"`
	PIDDigest    string   `json:"-"`
	BindingError string   `json:"bindingError,omitempty"`
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
	if c.Owner != nil {
		c.SocketBindAliases = uniqueCleanPaths(c.Owner.SocketAliases)
	} else {
		c.SocketBindAliases = normalizeSocketBindAliases(c)
	}
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
	env := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "BROKER_") {
			env = append(env, entry)
		}
	}
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

func newStatus(c Config) Status {
	return Status{Socket: c.Socket, StateRoot: c.StateRoot, PIDFile: PIDFile(c), StateFile: StateFile(c), LogPath: LogPath(c)}
}
func Inspect(c Config) (Status, error) {
	var err error
	c, err = selectedStationOwner(c)
	if err != nil {
		return newStatus(c), err
	}
	g, err := lockLifecycle(context.Background(), c, false)
	if err != nil {
		return newStatus(c), err
	}
	if g == nil {
		return newStatus(c), nil
	}
	defer g.Close()
	return inspectLocked(c, g)
}
func inspectLocked(c Config, g *lifecycleGuard) (Status, error) {
	status := newStatus(c)
	data, err := g.read(StateFile(c))
	if err == nil {
		digest := sha256.Sum256(data)
		status.StateDigest = hex.EncodeToString(digest[:])
		var state State
		if err = json.Unmarshal(data, &state); err != nil {
			return status, fmt.Errorf("broker state is malformed: %w", err)
		}
		status.State = &state
	} else if !errors.Is(err, os.ErrNotExist) {
		return status, err
	}
	data, err = g.read(PIDFile(c))
	if err == nil {
		digest := sha256.Sum256(data)
		status.PIDDigest = hex.EncodeToString(digest[:])
		status.PID, err = strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || status.PID < 1 {
			return status, fmt.Errorf("broker PID metadata is malformed")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return status, err
	}
	info, err := socketInfo(c.Socket)
	if err == nil {
		status.SocketExists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return status, err
	}
	if status.State != nil {
		if status.PID != status.State.PID {
			status.BindingError = "broker PID and state bindings disagree"
		}

		if reason := stateBinding(c, status.State); reason != "" {
			legacy := c.Owner != nil && c.Owner.LegacyWitness != nil &&
				status.StateDigest == c.Owner.LegacyWitness.StateSHA256 &&
				status.PIDDigest == c.Owner.LegacyWitness.PIDSHA256 &&
				status.State.Binary == c.Owner.LegacyWitness.Binary
			if legacy {
				selected := *status.State
				selected.Binary = c.Binary
				reason = stateBinding(c, &selected)
			}
			status.BindingError = reason
		}

	}
	if info != nil && status.State == nil {
		status.BindingError = "broker socket has no retained owner state"
	}
	if info != nil && status.State != nil && status.State.SocketInode != 0 && !socketMatchesState(info, status.State) {
		status.BindingError = "broker socket identity differs from recorded owner"
	}
	if status.PID == 0 {
		status.Message = "broker is not started"
		if status.State != nil {
			status.StaleState = true
		}
		return status, nil
	}
	if !processRunning(status.PID) {
		status.StaleState = true
		status.Message = "broker pid file is stale"
		return status, nil
	}
	if status.State == nil || !managedProcess(c, status.State) {
		status.StaleState = true
		status.BindingError = "broker pid belongs to a different process"
		status.Message = status.BindingError
		return status, nil
	}
	if status.BindingError != "" {
		status.StaleState = true
		status.Message = status.BindingError
		return status, nil
	}
	if info == nil || !socketMatchesState(info, status.State) {
		status.StaleState = true
		status.BindingError = "broker socket identity differs from recorded owner"
		status.Message = status.BindingError
		return status, nil
	}
	if err := brokerPing(c.Socket, status.State); err != nil {
		status.StaleState = true
		status.Message = "matching broker failed functional readiness: " + err.Error()
		return status, nil
	}
	status.Running = true
	status.Message = "broker is running"
	return status, nil
}
func stateBinding(c Config, s *State) string {
	if filepath.Clean(s.Socket) != c.Socket || s.Upstream != c.Upstream || s.Binary != c.Binary ||
		s.AllowPulls != c.AllowPulls || s.LogLevel != c.LogLevel ||
		!sameStrings(s.AllowedImages, c.AllowedImages) || !sameStrings(s.SocketBindAliases, c.SocketBindAliases) {
		return "broker state differs from source-selected configuration"
	}
	if c.Owner != nil && s.OwnerService != "" && s.OwnerService != StationOwnerService {
		return "broker owner service identity differs"
	}
	return ""
}
func sameStrings(a, b []string) bool {
	a = append([]string{}, a...)
	b = append([]string{}, b...)
	sort.Strings(a)
	sort.Strings(b)
	return strings.Join(a, "\x00") == strings.Join(b, "\x00")
}
func socketInfo(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeSocket == 0 || stat.Uid != uint32(os.Getuid()) {
		return nil, fmt.Errorf("broker socket has foreign or unsafe identity: %s", path)
	}
	return info, nil
}
func socketMatchesState(info os.FileInfo, s *State) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.SocketDevice != 0 && s.SocketInode != 0 &&
		uint64(stat.Dev) == s.SocketDevice && stat.Ino == s.SocketInode
}
func socketAcceptsConnections(path string) bool {
	conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
func brokerPing(path string, state *State) error {
	conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		return err
	}
	if state != nil {
		unix, ok := conn.(*net.UnixConn)
		if !ok {
			return fmt.Errorf("broker connection is not Unix")
		}
		raw, err := unix.SyscallConn()
		if err != nil {
			return err
		}
		var peer *syscall.Ucred
		var peerErr error
		if err = raw.Control(func(fd uintptr) {
			peer, peerErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		}); err != nil {
			return err
		}
		if peerErr != nil {
			return peerErr
		}
		if peer == nil || int(peer.Pid) != state.PID || peer.Uid != uint32(os.Getuid()) {
			return fmt.Errorf("broker listener peer differs from retained owner")
		}
	}
	request, err := http.NewRequest(http.MethodGet, "http://docker/_ping", nil)
	if err != nil {
		return err
	}
	if err = request.Write(conn); err != nil {
		return err
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 33))
	if err != nil {
		return err
	}
	if len(body) > 32 || response.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "OK" {
		return fmt.Errorf("broker /_ping returned an invalid bounded response")
	}
	return nil
}
func Start(ctx context.Context, c Config, dryRun bool) (Status, error) {
	return startSelected(ctx, c, dryRun, true)
}
func EnsureReady(ctx context.Context, c Config, dryRun bool) (Status, error) {
	return startSelected(ctx, c, dryRun, false)
}
func startSelected(ctx context.Context, c Config, dryRun, explicit bool) (Status, error) {
	var err error
	c, err = selectedStationOwner(c)
	if err != nil {
		return newStatus(c), err
	}
	if c.Owner != nil && !inStationOwner() {
		return requestStationStart(ctx, c, dryRun, explicit)
	}
	return startLocal(ctx, c, dryRun, explicit && c.Owner == nil)
}

type stopIntent struct {
	Generation  int64  `json:"generation"`
	InProgress  bool   `json:"inProgress"`
	RecordedPID int    `json:"recordedPID,omitempty"`
	StateSHA256 string `json:"stateSHA256,omitempty"`
	PIDSHA256   string `json:"pidSHA256,omitempty"`
}

func readStopIntent(c Config, g *lifecycleGuard) (*stopIntent, error) {
	data, err := g.read(stopIntentFile(c))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var intent stopIntent
	if err = json.Unmarshal(data, &intent); err != nil || intent.Generation <= 0 {
		return nil, fmt.Errorf("broker stop intent is malformed")
	}
	return &intent, nil
}
func writeStopIntent(c Config, g *lifecycleGuard, intent stopIntent) error {
	data, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	return g.write(stopIntentFile(c), data)
}
func startLocal(ctx context.Context, c Config, dryRun, explicit bool) (Status, error) {
	g, err := lockLifecycle(ctx, c, !dryRun)
	if err != nil {
		return newStatus(c), err
	}
	if g == nil {
		return newStatus(c), nil
	}
	defer g.Close()
	status, err := inspectLocked(c, g)
	if err != nil {
		return status, err
	}
	intent, err := readStopIntent(c, g)
	if err != nil {
		return status, err
	}
	if intent != nil && (!explicit || intent.InProgress) {
		return status, fmt.Errorf("shared broker owner is deliberately stopped; automatic acquisition is inhibited")
	}
	if status.BindingError != "" {
		return status, fmt.Errorf("refusing foreign broker binding: %s", status.BindingError)
	}
	if status.Running {
		if !dryRun && intent != nil {
			if err = g.remove(stopIntentFile(c)); err != nil {
				return status, err
			}
		}
		return status, nil
	}
	if processRunning(status.PID) {
		return status, fmt.Errorf("matching broker owner is alive but not functionally ready: %s", status.Message)
	}
	binary, err := ResolveBinary(ctx, c)
	if err != nil {
		return status, err
	}
	status.Command = []string{binary}
	if dryRun {
		status.Message = "dry run: fixed broker owner would be acquired"
		return status, nil
	}
	if err = retireStaleSocket(c, status); err != nil {
		return status, err
	}
	if intent != nil {
		if err = g.remove(stopIntentFile(c)); err != nil {
			return status, err
		}
	}
	if err = g.checkRoot(); err != nil {
		return status, err
	}
	logFile, err := openBrokerLog(c, g)
	if err != nil {
		return status, err
	}
	defer logFile.Close()
	cmd := exec.Command(binary)
	cmd.Env = Env(c)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err = cmd.Start(); err != nil {
		return status, fmt.Errorf("start source-selected broker: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	ticks, err := processStart(cmd.Process.Pid)
	if err != nil {
		// The retained os.Process still names this unadmitted child.
		_ = cmd.Process.Kill()
		return status, fmt.Errorf("retain newly started broker identity: %w", err)
	}
	state := State{PID: cmd.Process.Pid, StartTicks: ticks, Socket: c.Socket, Upstream: c.Upstream,
		AllowedImages: append([]string{}, c.AllowedImages...), SocketBindAliases: append([]string{}, c.SocketBindAliases...),
		AllowPulls: c.AllowPulls, LogLevel: c.LogLevel, Binary: binary, LogPath: LogPath(c), StartedAt: time.Now()}
	if c.Owner != nil {
		state.OwnerService = StationOwnerService
	}
	if err = writeStateLocked(c, g, state); err != nil {
		return status, errors.Join(err, terminateManagedProcess(c, &state), cleanupStateLocked(c, g, &state))
	}
	deadline := time.Now().Add(c.StartTimeout)
	var last error
	var ownedSocket os.FileInfo
	for time.Now().Before(deadline) {
		info, probeErr := socketInfo(c.Socket)
		if probeErr == nil && managedProcess(c, &state) {
			if ownedSocket == nil {
				ownedSocket = info
			} else if !os.SameFile(ownedSocket, info) {
				last = fmt.Errorf("new broker socket was replaced during admission")
				break
			}
			st := info.Sys().(*syscall.Stat_t)
			state.SocketDevice = uint64(st.Dev)
			state.SocketInode = st.Ino
			probeErr = brokerPing(c.Socket, &state)
			if probeErr == nil {
				current, identityErr := socketInfo(c.Socket)
				if identityErr == nil && os.SameFile(info, current) {
					if err = writeStateLocked(c, g, state); err != nil {
						return status, errors.Join(err, terminateManagedProcess(c, &state), cleanupStateLocked(c, g, &state))
					}
					return inspectLocked(c, g)
				}
				probeErr = fmt.Errorf("broker socket changed during readiness")
			}
		}
		last = probeErr
		if !processRunning(state.PID) {
			break
		}
		select {
		case <-ctx.Done():
			last = ctx.Err()
			deadline = time.Now()
		case <-time.After(10 * time.Millisecond):
		}
	}
	// This transaction has never admitted its new owner to a sibling. Cleanup
	// retains that exact process and socket; an existing owner is never retired
	// merely because upstream readiness failed.
	stopErr := terminateManagedProcess(c, &state)
	cleanupErr := cleanupStateLocked(c, g, &state)
	return status, errors.Join(fmt.Errorf("new broker failed bounded functional readiness: %v", last), stopErr, cleanupErr)
}
func openBrokerLog(c Config, g *lifecycleGuard) (*os.File, error) {
	info, err := os.Lstat(g.heldPath(LogPath(c)))
	if err == nil {
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || st.Uid != uint32(os.Getuid()) || st.Nlink != 1 || info.Mode().Perm()&0077 != 0 {
			return nil, fmt.Errorf("broker log has foreign or unsafe identity")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	fd, err := syscall.Openat(int(g.directory.Fd()), filepath.Base(LogPath(c)), syscall.O_CREAT|syscall.O_WRONLY|syscall.O_APPEND|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), LogPath(c))
	opened, err := file.Stat()
	if err != nil || (info != nil && !os.SameFile(info, opened)) {
		file.Close()
		return nil, fmt.Errorf("broker log identity changed while opening")
	}
	return file, nil
}
func staleSocketOwner(c Config, status Status) (os.FileInfo, error) {
	info, err := socketInfo(c.Socket)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if status.State == nil || status.BindingError != "" || processRunning(status.PID) || socketAcceptsConnections(c.Socket) {
		return nil, fmt.Errorf("refusing to replace an unowned or active broker socket")
	}
	owned := socketMatchesState(info, status.State)

	// Legacy migration is admitted only by this host composition's fixed
	// pre-change witness. Timestamp similarity alone never grants ownership.
	if !owned && status.State.SocketDevice == 0 && status.State.SocketInode == 0 &&
		c.Owner != nil && c.Owner.LegacyWitness != nil {
		witness := c.Owner.LegacyWitness
		st := info.Sys().(*syscall.Stat_t)
		owned = status.StateDigest == witness.StateSHA256 && status.PIDDigest == witness.PIDSHA256 &&
			uint64(st.Dev) == witness.SocketDevice && st.Ino == witness.SocketInode &&
			st.Ctim.Sec == witness.SocketCTimeSec && st.Ctim.Nsec == witness.SocketCTimeNsec
	}
	if !owned {
		return nil, fmt.Errorf("refusing to retire a replaced or unwitnessed stale broker socket")
	}
	return info, nil
}
func retireStaleSocket(c Config, status Status) error {
	info, err := staleSocketOwner(c, status)
	if err != nil || info == nil {
		return err
	}
	now, err := socketInfo(c.Socket)
	if err != nil || !os.SameFile(info, now) {
		return fmt.Errorf("broker socket identity changed before stale retirement")
	}
	return removeRetainedSocket(c.Socket, info)
}
func Stop(c Config, dryRun bool) (Status, error) {
	var err error
	c, err = selectedStationOwner(c)
	if err != nil {
		return newStatus(c), err
	}
	if c.Owner != nil && !inStationOwner() {
		return requestStationStop(c, dryRun)
	}
	return stopLocal(c, dryRun)
}
func stopLocal(c Config, dryRun bool) (Status, error) {
	g, err := lockLifecycle(context.Background(), c, !dryRun)
	if err != nil {
		return newStatus(c), err
	}
	if g == nil {
		return newStatus(c), nil
	}
	defer g.Close()
	status, err := inspectLocked(c, g)
	if err != nil {
		return status, err
	}
	if status.BindingError != "" {
		return status, fmt.Errorf("refusing to stop unmanaged process or foreign binding: %s", status.BindingError)
	}
	if !processRunning(status.PID) && status.SocketExists {
		if _, err = staleSocketOwner(c, status); err != nil {
			return status, err
		}
	}
	if dryRun {
		status.Message = "dry run: fixed broker owner would be stopped"
		return status, nil
	}
	intent, err := readStopIntent(c, g)
	if err != nil {
		return status, err
	}
	outerStop := intent != nil && intent.InProgress
	if intent == nil {
		intent = &stopIntent{Generation: time.Now().UnixNano(), InProgress: true}
	}
	if err = writeStopIntent(c, g, *intent); err != nil {
		return status, err
	}
	if processRunning(status.PID) {
		if !managedProcess(c, status.State) {
			return status, fmt.Errorf("refusing to stop unmanaged process %d", status.PID)
		}
		if err = terminateManagedProcess(c, status.State); err != nil {
			return status, err
		}
	} else if err = retireStaleSocket(c, status); err != nil {
		return status, err
	}
	if err = cleanupStateLocked(c, g, status.State); err != nil {
		return status, err
	}
	if !outerStop {
		intent.InProgress = false
		if err = writeStopIntent(c, g, *intent); err != nil {
			return status, err
		}
	}
	result := newStatus(c)
	result.Message = "broker is deliberately stopped"
	return result, nil
}
func terminateManagedProcess(c Config, state *State) error {
	if !processRunning(state.PID) {
		return nil
	}
	if err := signalManaged(c, state, syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for processRunning(state.PID) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !processRunning(state.PID) {
		return nil
	}
	if err := signalManaged(c, state, syscall.SIGKILL); err != nil {
		return err
	}
	deadline = time.Now().Add(time.Second)
	for processRunning(state.PID) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if processRunning(state.PID) {
		return fmt.Errorf("retained broker did not retire within bounded stop")
	}
	return nil
}
func writeStateLocked(c Config, g *lifecycleGuard, state State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err = g.write(StateFile(c), data); err != nil {
		return fmt.Errorf("write broker state: %w", err)
	}
	return g.write(PIDFile(c), []byte(strconv.Itoa(state.PID)+"\n"))
}
func writeState(c Config, state State) error {
	c = Normalize(c)
	g, err := lockLifecycle(context.Background(), c, true)
	if err != nil {
		return err
	}
	defer g.Close()
	return writeStateLocked(c, g, state)
}
func cleanupStateLocked(c Config, g *lifecycleGuard, state *State) error {
	if err := g.checkRoot(); err != nil {
		return err
	}
	info, err := socketInfo(c.Socket)
	if err == nil {
		if state == nil || !socketMatchesState(info, state) {
			return fmt.Errorf("refusing to remove a replaced broker socket")
		}
		now, err := socketInfo(c.Socket)
		if err != nil || !os.SameFile(info, now) {
			return fmt.Errorf("broker socket identity changed during cleanup")
		}
		if err = removeRetainedSocket(c.Socket, info); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = g.remove(PIDFile(c)); err != nil {
		return err
	}
	return g.remove(StateFile(c))
}
func processRunning(pid int) bool {
	if pid < 1 {
		return false
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return false
	}
	fields := strings.Fields(string(data[end+1:]))
	return len(fields) > 0 && fields[0] != "Z" && fields[0] != "X"
}

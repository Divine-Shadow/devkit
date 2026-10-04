//go:build linux

package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Linked only into the separate, composition-selected host observation helper.
// The helper accepts a retained PID/start tuple, never a policy or path.
var packageObservationManifest string

const stationObservationSchema = "devkit-native-broker-observation/v1"
const stationObservationLimit = 4096
const stationObservationDeadline = 5 * time.Second

type stationObservationPolicy struct {
	SchemaVersion string `json:"schemaVersion"`
	Service       string `json:"service"`
	Binary        string `json:"binary"`
}

type stationObservationResult struct {
	SchemaVersion string `json:"schemaVersion"`
	PID           int    `json:"pid"`
	StartTicks    uint64 `json:"startTicks"`
	Binary        string `json:"binary"`
}

type stationObservationBuffer struct{ buffer bytes.Buffer }

func (b *stationObservationBuffer) Len() int      { return b.buffer.Len() }
func (b *stationObservationBuffer) Bytes() []byte { return b.buffer.Bytes() }

func (b *stationObservationBuffer) Write(p []byte) (int, error) {
	if len(p) > stationObservationLimit-b.Len() {
		return 0, fmt.Errorf("fixed station observation exceeds its byte bound")
	}
	return b.buffer.Write(p)
}

func decodeStationObservation(data []byte, value any) error {
	if len(data) == 0 || len(data) > stationObservationLimit {
		return fmt.Errorf("fixed station observation has an invalid size")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := d.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("fixed station observation has trailing data")
	}
	return nil
}

func stationObservationCommand(ctx context.Context, owner *StationOwner, state *State) *exec.Cmd {
	command := exec.CommandContext(ctx, owner.SystemdRun,
		"--user", "--quiet", "--wait", "--pipe", "--collect", "--service-type=exec",
		"--property=NoNewPrivileges=yes", "--property=RuntimeMaxSec=5s",
		"--property=KillMode=control-group", "--property=TimeoutStopSec=1s", "--property=SendSIGKILL=yes", "--",
		owner.ObservationExecutable, strconv.Itoa(state.PID), strconv.FormatUint(state.StartTicks, 10))
	command.WaitDelay = time.Second
	uid := strconv.Itoa(os.Getuid())
	command.Env = []string{"PATH=/no-ambient-path", "LC_ALL=C", "XDG_RUNTIME_DIR=/run/user/" + uid, "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/" + uid + "/bus"}
	return command
}

func observedStationProcess(c Config, state *State) bool {
	if c.Owner == nil || c.Owner.Service != StationOwnerService ||
		c.Owner.SystemdRun == "" || c.Owner.ObservationExecutable == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), stationObservationDeadline)
	defer cancel()
	command := stationObservationCommand(ctx, c.Owner, state)
	var stdout, stderr stationObservationBuffer
	command.Stdout, command.Stderr = &stdout, &stderr
	// A successful observation requires actual helper completion. Timeout,
	// manager/helper failure, malformed output and truncation never fall back.
	if err := command.Run(); err != nil || ctx.Err() != nil {
		return false
	}
	var result stationObservationResult
	if decodeStationObservation(stdout.Bytes(), &result) != nil {
		return false
	}
	wanted, err := filepath.EvalSymlinks(c.Binary)
	return err == nil && result.SchemaVersion == stationObservationSchema &&
		result.PID == state.PID && result.StartTicks == state.StartTicks &&
		result.Binary == filepath.Clean(wanted)
}

// ObserveSelectedStationProcess runs in the user manager's host namespace.
// It reads only the selected broker's start/executable/cgroup identity and
// neither acquires the caller's lifecycle lock nor controls the broker.
func ObserveSelectedStationProcess(args []string, stdout io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("fixed station observation requires a retained PID/start tuple")
	}
	pid, err := strconv.Atoi(args[0])
	if err != nil || pid < 1 || strconv.Itoa(pid) != args[0] {
		return fmt.Errorf("fixed station observation PID is invalid")
	}
	ticks, err := strconv.ParseUint(args[1], 10, 64)
	if err != nil || ticks == 0 || strconv.FormatUint(ticks, 10) != args[1] {
		return fmt.Errorf("fixed station observation start identity is invalid")
	}
	if !filepath.IsAbs(packageObservationManifest) || filepath.Clean(packageObservationManifest) != packageObservationManifest ||
		!strings.HasPrefix(packageObservationManifest, "/nix/store/") {
		return fmt.Errorf("fixed station observation policy is not source selected")
	}
	f, err := os.Open(packageObservationManifest)
	if err != nil {
		return fmt.Errorf("read fixed station observation policy: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, stationObservationLimit+1))
	if err != nil {
		return err
	}
	var policy stationObservationPolicy
	if err := decodeStationObservation(data, &policy); err != nil {
		return fmt.Errorf("decode fixed station observation policy: %w", err)
	}
	if policy.SchemaVersion != stationObservationSchema || policy.Service != StationOwnerService ||
		!filepath.IsAbs(policy.Binary) || filepath.Clean(policy.Binary) != policy.Binary || !strings.HasPrefix(policy.Binary, "/nix/store/") {
		return fmt.Errorf("fixed station observation policy is unsupported")
	}
	state := &State{PID: pid, StartTicks: ticks}
	c := Config{Binary: policy.Binary, Owner: &StationOwner{Service: policy.Service}}
	if !directManagedProcess(c, state) {
		return fmt.Errorf("fixed station broker process identity does not match")
	}
	binary, err := filepath.EvalSymlinks(policy.Binary)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(stationObservationResult{SchemaVersion: stationObservationSchema, PID: pid, StartTicks: ticks, Binary: filepath.Clean(binary)})
}

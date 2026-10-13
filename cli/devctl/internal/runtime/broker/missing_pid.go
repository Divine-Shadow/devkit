package broker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

// Reconcile only the current station owner's fully recorded, retired lifetime.
// Inspection remains strict. No PID file is synthesized, no process is signaled,
// and a deliberate stop remains owned by the existing stop/retry protocol.
func reconcileMissingPID(c Config, g *lifecycleGuard, status Status, dryRun bool) (Status, error) {
	if status.BindingError != "broker PID and state bindings disagree" || status.PID != 0 || g.files[PIDFile(c)] != nil {
		return status, nil
	}
	s := status.State
	if c.Owner == nil || c.Owner.Service != StationOwnerService || s == nil ||
		s.OwnerService != StationOwnerService || s.PID < 1 || s.StartTicks == 0 ||
		s.SocketDevice == 0 || s.SocketInode == 0 || stateBinding(c, s) != "" {
		return status, fmt.Errorf("missing broker PID has no complete source-selected station lifetime")
	}
	intent, err := readStopIntent(c, g)
	if err != nil || intent != nil {
		return status, errors.Join(fmt.Errorf("missing broker PID reconciliation cannot cross a stop intent"), err)
	}
	proof := status
	proof.PID, proof.BindingError = s.PID, ""
	retired, err := recordedProcessRetired(proof)
	if err != nil || !retired {
		return status, errors.Join(fmt.Errorf("missing broker PID does not prove process retirement"), err)
	}
	stateFile, err := g.retainFile(StateFile(c))
	if err != nil {
		return status, err
	}
	defer stateFile.Close()
	socket, identity, err := g.retainSocket(c.Socket)
	if err != nil {
		return status, err
	}
	if socket == nil {
		return status, fmt.Errorf("missing broker PID has no retained socket lifetime")
	}
	defer socket.Close()
	if !socketMatchesState(identity, s) {
		return status, fmt.Errorf("missing broker PID socket differs from recorded owner")
	}
	// The unit's own forking ExecStart runs while MainPID is still zero and
	// ActiveState is activating. No outside consumer may admit that phase.
	pid, phase, err := fixedStationIdentity(c)
	if err != nil {
		return status, err
	}
	if pid != 0 || (phase != "inactive" && phase != "failed" && !(phase == "activating" && inStationOwner())) {
		return status, fmt.Errorf("missing broker PID has an active or transitioning station owner")
	}
	// Revalidate bytes, pathname absence, stop generation, process and socket
	// after the external unit observation, with descriptors preventing reuse.
	raw, err := g.read(StateFile(c))
	sum := sha256.Sum256(raw)
	if err != nil || hex.EncodeToString(sum[:]) != status.StateDigest {
		return status, errors.Join(fmt.Errorf("broker state changed during missing PID reconciliation"), err)
	}
	if _, err = g.read(PIDFile(c)); !errors.Is(err, os.ErrNotExist) {
		return status, fmt.Errorf("broker PID pathname changed during reconciliation")
	}
	intent, err = readStopIntent(c, g)
	if err != nil || intent != nil {
		return status, errors.Join(fmt.Errorf("broker stop intent changed during reconciliation"), err)
	}
	retired, err = recordedProcessRetired(proof)
	if err != nil || !retired {
		return status, errors.Join(fmt.Errorf("broker lifetime changed during missing PID reconciliation"), err)
	}
	conn, err := net.DialTimeout("unix", c.Socket, 100*time.Millisecond)
	if err == nil {
		conn.Close()
		return status, fmt.Errorf("missing broker PID socket still accepts connections")
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return status, fmt.Errorf("missing broker PID socket retirement is unknown: %w", err)
	}
	current, err := socketInfo(c.Socket)
	if err != nil || !sameRetainedFile(identity, current) {
		return status, fmt.Errorf("broker socket changed during missing PID reconciliation")
	}
	if dryRun {
		return proof, nil
	}
	if _, err := ResolveBinary(context.Background(), c); err != nil {
		return status, err
	}
	if err = g.checkFile(StateFile(c)); err != nil {
		return status, err
	}
	if err = removeRetainedSocket(c.Socket, identity); err != nil {
		return status, err
	}
	if err = g.remove(StateFile(c)); err != nil {
		return status, err
	}
	return newStatus(c), nil
}

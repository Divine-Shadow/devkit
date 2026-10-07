//go:build linux

package broker

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// This compatibility transition belongs only to explicit Stop. Readiness and
// ordinary startup never acquire or retire a legacy producer.
func stopWitnessedLegacy(c Config, g *lifecycleGuard, status Status, dryRun bool) (bool, Status, error) {
	if c.Owner == nil || c.Owner.LegacyWitness == nil || status.State == nil {
		return false, status, nil
	}
	w := c.Owner.LegacyWitness
	s := status.State
	if status.StateDigest != w.StateSHA256 || status.PIDDigest != w.PIDSHA256 ||
		s.Binary != w.Binary || s.StartTicks != 0 || s.SocketDevice != 0 ||
		s.SocketInode != 0 || s.OwnerService != "" {
		return false, status, nil
	}
	fail := func(err error) (bool, Status, error) { return true, status, err }
	if status.PID < 1 || status.PID != s.PID || w.SocketDevice == 0 || w.SocketInode == 0 ||
		!filepath.IsAbs(w.Binary) || filepath.Clean(w.Binary) != w.Binary {
		return fail(fmt.Errorf("legacy broker stop witness is incomplete"))
	}
	selected := *s
	selected.Binary = c.Binary
	if reason := stateBinding(c, &selected); reason != "" {
		return fail(fmt.Errorf("legacy broker stop policy differs: %s", reason))
	}
	// The selected service cannot already own another producer.
	if err := fixedStationStopBinding(c, status); err != nil {
		return fail(err)
	}
	socket, identity, err := g.retainSocket(c.Socket)
	if err != nil {
		return fail(err)
	}
	if socket != nil {
		defer socket.Close()
	}
	if identity != nil && !legacySocketMatches(identity, w) {
		return fail(fmt.Errorf("legacy broker socket differs from source witness"))
	}
	retained, err := g.retainFile(StateFile(c))
	if err != nil {
		return fail(err)
	}
	if retained != nil {
		defer retained.Close()
	}
	pidRecord, err := g.retainFile(PIDFile(c))
	if err != nil {
		return fail(err)
	}
	if pidRecord != nil {
		defer pidRecord.Close()
	}
	metadata := func() error {
		if err := g.checkRoot(); err != nil {
			return err
		}
		for path, digest := range map[string]string{StateFile(c): w.StateSHA256, PIDFile(c): w.PIDSHA256} {
			raw, err := g.read(path)
			if path == PIDFile(c) && errors.Is(err, os.ErrNotExist) {
				intent, intentErr := readStopIntent(c, g)
				if intentErr == nil && intent != nil && intent.InProgress &&
					intent.RecordedPID == status.PID && intent.StateSHA256 == w.StateSHA256 && intent.PIDSHA256 == w.PIDSHA256 {
					continue
				}
			}
			if err != nil {
				return err
			}
			sum := sha256.Sum256(raw)
			if hex.EncodeToString(sum[:]) != digest {
				return fmt.Errorf("legacy broker metadata bytes changed")
			}
		}
		return nil
	}
	if err = metadata(); err != nil {
		return fail(err)
	}
	// Already absent producers retain the existing stale-witness stop path.
	// A disappearance during live admission is an effect-free refusal.
	if _, err := os.Stat(filepath.Join("/proc", fmt.Sprint(status.PID))); errors.Is(err, os.ErrNotExist) {
		return false, status, nil
	}
	fd, _, errno := syscall.Syscall(pidfdOpenSyscall, uintptr(status.PID), 0, 0)
	if errno == syscall.ESRCH {
		return fail(fmt.Errorf("legacy broker retired during admission; explicit stop may re-evaluate the retired witness"))
	}
	if errno != 0 {
		return fail(fmt.Errorf("retain legacy broker process: %w", errno))
	}
	defer syscall.Close(int(fd))
	previous, err := readStopIntent(c, g)
	if err != nil {
		return fail(err)
	}
	if previous != nil && previous.InProgress && (previous.RecordedPID != status.PID || previous.StateSHA256 != w.StateSHA256 || previous.PIDSHA256 != w.PIDSHA256 || previous.LegacyStartTicks == 0) {
		return fail(fmt.Errorf("legacy broker interrupted stop has no matching retained lifetime"))
	}
	retry := previous != nil && previous.InProgress
	var ticks uint64
	retired := false
	if !retired {
		ticks, err = processStart(status.PID)
		if err != nil || ticks == 0 {
			return fail(fmt.Errorf("legacy broker start tuple is unavailable"))
		}
		if previous != nil && previous.InProgress && ticks != previous.LegacyStartTicks {
			return fail(fmt.Errorf("legacy broker interrupted stop lifetime changed"))
		}
		// A retained pidfd must be pollable before any signal is possible.
		retired, err = legacyPIDFDExited(int(fd), 0)
		if err != nil {
			return fail(err)
		}
		if !retired {
			proof := *s
			proof.StartTicks = ticks
			old := c
			old.Owner, old.Binary = nil, w.Binary
			if !directManagedProcess(old, &proof) {
				return fail(fmt.Errorf("legacy broker process does not match its retained executable"))
			}
			// A matching persisted lifetime may be closing after interruption.
			// This does not assert that an earlier TERM was delivered.
			if err = legacySignalSocketBinding(c.Socket, identity, w, status.PID, retry); err != nil {
				return fail(err)
			}
			if !directManagedProcess(old, &proof) {
				return fail(fmt.Errorf("legacy broker lifetime changed during listener admission"))
			}
		}
	}
	if dryRun {
		status.Message = "dry run: exact witnessed legacy broker would be deliberately stopped"
		return true, status, nil
	}
	intent := stopIntent{Generation: time.Now().UnixNano(), InProgress: true, RecordedPID: status.PID, StateSHA256: w.StateSHA256, PIDSHA256: w.PIDSHA256, LegacyStartTicks: ticks}
	if previous != nil && previous.InProgress {
		intent = *previous
	}
	if err = writeStopIntent(c, g, intent); err != nil {
		return fail(err)
	}
	if !retired {
		proof := *s
		proof.StartTicks = ticks
		old := c
		old.Owner, old.Binary = nil, w.Binary
		termSent := retry
		signal := func(sig syscall.Signal) error {
			if err := metadata(); err != nil {
				return err
			}
			currentIntent, err := readStopIntent(c, g)
			if err != nil || currentIntent == nil || *currentIntent != intent {
				return fmt.Errorf("legacy broker stop intent changed before retained signal")
			}
			if !directManagedProcess(old, &proof) {
				return fmt.Errorf("legacy broker lifetime changed before retained signal")
			}
			if err := legacySignalSocketBinding(c.Socket, identity, w, status.PID, termSent); err != nil {
				return err
			}
			_, _, e := syscall.Syscall6(pidfdSendSignalSyscall, fd, uintptr(sig), 0, 0, 0, 0)
			if e != 0 && e != syscall.ESRCH {
				return fmt.Errorf("signal retained legacy broker: %w", e)
			}
			return nil
		}
		if err = signal(syscall.SIGTERM); err != nil {
			return fail(err)
		}
		termSent = true
		retired, err = legacyPIDFDExited(int(fd), 5*time.Second)
		if err != nil {
			return fail(err)
		}
		if !retired {
			if err = signal(syscall.SIGKILL); err != nil {
				return fail(err)
			}
			retired, err = legacyPIDFDExited(int(fd), time.Second)
			if err != nil {
				return fail(err)
			}
		}
		if !retired {
			return fail(fmt.Errorf("retained legacy broker did not retire within bounded stop"))
		}
	}
	if err = metadata(); err != nil {
		return fail(err)
	}
	currentIntent, err := readStopIntent(c, g)
	if err != nil || currentIntent == nil || *currentIntent != intent {
		return fail(fmt.Errorf("legacy broker stop intent changed before cleanup"))
	}
	now, err := socketInfo(c.Socket)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail(err)
	}
	if now != nil {
		if identity == nil || !sameRetainedFile(identity, now) || !legacySocketMatches(now, w) || socketAcceptsConnections(c.Socket) {
			return fail(fmt.Errorf("legacy broker socket changed or remains active after retirement"))
		}
		if err = removeRetainedSocket(c.Socket, identity); err != nil {
			return fail(err)
		}
	}
	if err = cleanupStateLocked(c, g, nil); err != nil {
		return fail(err)
	}
	intent.InProgress = false
	if err = writeStopIntent(c, g, intent); err != nil {
		return fail(err)
	}
	result := newStatus(c)
	result.Message = "exact witnessed legacy broker is deliberately stopped"
	return true, result, nil
}

func legacySocketMatches(info os.FileInfo, w *LegacySocketWitness) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && uint64(st.Dev) == w.SocketDevice && st.Ino == w.SocketInode && st.Ctim.Sec == w.SocketCTimeSec && st.Ctim.Nsec == w.SocketCTimeNsec
}

func legacySocketPeer(path string, pid int) error {
	conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err != nil {
		return errLegacyListenerUnavailable
	}
	defer conn.Close()
	unix, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("legacy broker listener is not Unix")
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
	if peerErr != nil || peer == nil || int(peer.Pid) != pid || peer.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("legacy broker socket peer does not own the recorded process")
	}
	return nil
}

func legacyPIDFDExited(fd int, wait time.Duration) (bool, error) {
	var ready syscall.FdSet
	if fd < 0 || fd >= len(ready.Bits)*64 {
		return false, fmt.Errorf("legacy broker retained descriptor exceeds poll boundary")
	}
	ready.Bits[fd/64] |= int64(1) << uint(fd%64)
	deadline := time.Now().Add(wait)
	for {
		ready.Bits[fd/64] = int64(1) << uint(fd%64)
		remaining := time.Until(deadline)
		if remaining < 0 {
			remaining = 0
		}
		timeout := syscall.NsecToTimeval(remaining.Nanoseconds())
		n, err := syscall.Select(fd+1, &ready, nil, nil, &timeout)
		if err == syscall.EINTR {
			if time.Now().Before(deadline) {
				continue
			}
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("poll retained legacy broker retirement: %w", err)
		}
		return n > 0, nil
	}
}

var errLegacyListenerUnavailable = errors.New("legacy broker listener is unavailable")

func legacySignalSocketBinding(path string, identity os.FileInfo, w *LegacySocketWitness, pid int, afterTerm bool) error {
	current, err := socketInfo(path)
	if afterTerm && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || identity == nil || !sameRetainedFile(identity, current) || !legacySocketMatches(current, w) {
		return fmt.Errorf("legacy broker socket changed before retained signal")
	}
	err = legacySocketPeer(path, pid)
	if err != nil && !(afterTerm && errors.Is(err, errLegacyListenerUnavailable) && !socketAcceptsConnections(path)) {
		return err
	}
	current, err = socketInfo(path)
	if afterTerm && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !sameRetainedFile(identity, current) || !legacySocketMatches(current, w) {
		return fmt.Errorf("legacy broker socket changed during signal peer proof")
	}
	return nil
}

package execx

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestRunManagedPreparedCommandPreservesSelectedInputsAndStreams(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("sh", "-c", `read value; printf '%s|%s|%s' "$PWD" "$OWNED_VALUE" "$value"; printf diagnostic >&2; exit 7`)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "OWNED_VALUE=selected")
	cmd.Stdin = strings.NewReader("payload\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	result := RunManagedCommand(context.Background(), ManagedPolicy{}, cmd)
	if result.Code != 7 || IsManagedCleanupError(result.Err) {
		t.Fatalf("result=%+v", result)
	}
	if stdout.String() != dir+"|selected|payload" || stderr.String() != "diagnostic" {
		t.Fatalf("streams stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunManagedPreparedCommandCannotJoinForeignProcessGroup(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: syscall.Getpgrp()}
	result := RunManagedCommand(context.Background(), ManagedPolicy{}, cmd)
	if result.Err == nil || cmd.Process != nil {
		t.Fatalf("foreign group admission=%+v", result)
	}
}

type overlapRejectingWriter struct {
	buffer bytes.Buffer
	active int32
}

func (w *overlapRejectingWriter) Write(payload []byte) (int, error) {
	if !atomic.CompareAndSwapInt32(&w.active, 0, 1) {
		return 0, errors.New("concurrent calls to a shared command writer")
	}
	defer atomic.StoreInt32(&w.active, 0)
	// Keep a copy active long enough for independently drained streams to
	// overlap. Like bytes.Buffer, this destination requires serialized writes.
	time.Sleep(time.Millisecond)
	return w.buffer.Write(payload)
}

func TestRunManagedPreparedCommandSerializesSharedOutputWriter(t *testing.T) {
	cmd := exec.Command("sh", "-c", `
emit() { i=0; while [ "$i" -lt 128 ]; do printf '%01024d' 0; i=$((i + 1)); done; }
emit &
emit >&2 &
wait
`)
	var output overlapRejectingWriter
	cmd.Stdout, cmd.Stderr = &output, &output
	result := RunManagedCommand(context.Background(), ManagedPolicy{}, cmd)
	if result.Code != 0 || result.Err != nil {
		t.Fatalf("shared command writer: %+v", result)
	}
	if output.buffer.Len() != 2*128*1024 {
		t.Fatalf("shared output length=%d, want %d", output.buffer.Len(), 2*128*1024)
	}
}

//go:build windows

package fleetmode

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var serviceKernel = syscall.NewLazyDLL("kernel32.dll")
var serviceProcessID = serviceKernel.NewProc("GetProcessId")
var serviceProcessImage = serviceKernel.NewProc("QueryFullProcessImageNameW")

type windowsServiceProcess struct{ handle syscall.Handle }

func (o *windowsOps) ReadWSLService(ctx context.Context) (wslServiceSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return wslServiceSnapshot{}, err
	}
	var snapshot wslServiceSnapshot
	err := withWSLService(1|4, func(h uintptr) error {
		var err error
		snapshot, err = readWSLServiceSnapshot(h)
		return err
	})
	if err == nil {
		err = ctx.Err()
	}
	return snapshot, err
}

func (o *windowsOps) OpenWSLServiceProcess(ctx context.Context, pid uint32) (heldServiceProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// PROCESS_QUERY_LIMITED_INFORMATION | SYNCHRONIZE | PROCESS_TERMINATE.
	// This handle is retained; no later operation opens a new process by PID.
	handle, err := syscall.OpenProcess(0x1000|0x100000|0x0001, false, pid)
	if err != nil {
		return nil, fmt.Errorf("retain WSLService process: %w", err)
	}
	return &windowsServiceProcess{handle: handle}, nil
}

func (p *windowsServiceProcess) Close() error { return syscall.CloseHandle(p.handle) }

func (p *windowsServiceProcess) Identity(ctx context.Context) (serviceProcessIdentity, error) {
	if err := ctx.Err(); err != nil {
		return serviceProcessIdentity{}, err
	}
	pid, _, err := serviceProcessID.Call(uintptr(p.handle))
	if pid == 0 {
		return serviceProcessIdentity{}, fmt.Errorf("read retained service PID: %w", err)
	}
	var created, exited, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(p.handle, &created, &exited, &kernel, &user); err != nil {
		return serviceProcessIdentity{}, fmt.Errorf("read retained service creation time: %w", err)
	}
	buffer := make([]uint16, 32768)
	length := uint32(len(buffer))
	ok, _, err := serviceProcessImage.Call(uintptr(p.handle), 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&length)))
	if ok == 0 {
		return serviceProcessIdentity{}, fmt.Errorf("read retained service image: %w", err)
	}
	if length == 0 || length > uint32(len(buffer)) {
		return serviceProcessIdentity{}, errors.New("retained service image length is invalid")
	}
	state, waitErr := syscall.WaitForSingleObject(p.handle, 0)
	if waitErr != nil || (state != syscall.WAIT_OBJECT_0 && state != syscall.WAIT_TIMEOUT) {
		return serviceProcessIdentity{}, errors.New("retained service liveness is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return serviceProcessIdentity{}, err
	}
	return serviceProcessIdentity{
		PID: uint32(pid), Created: uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime),
		Image: syscall.UTF16ToString(buffer[:length]), Exited: state == syscall.WAIT_OBJECT_0,
	}, nil
}

func (p *windowsServiceProcess) Terminate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := syscall.TerminateProcess(p.handle, 1)
	if err != nil {
		// The same retained process may have completed its normal stop meanwhile.
		// Its signaled handle is absence evidence; a replacement PID is not used.
		state, waitErr := syscall.WaitForSingleObject(p.handle, 0)
		if waitErr == nil && state == syscall.WAIT_OBJECT_0 {
			return nil
		}
	}
	return err
}

func (p *windowsServiceProcess) Wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		wait := 50 * time.Millisecond
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < wait {
			wait = time.Until(deadline)
		}
		if wait <= 0 {
			return context.DeadlineExceeded
		}
		state, err := syscall.WaitForSingleObject(p.handle, uint32((wait+time.Millisecond-1)/time.Millisecond))
		if err != nil {
			return err
		}
		switch state {
		case syscall.WAIT_OBJECT_0:
			return nil
		case syscall.WAIT_TIMEOUT:
		default:
			return errors.New("unexpected retained WSLService process wait result")
		}
	}
}

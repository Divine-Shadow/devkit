//go:build windows

package fleetmode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Microsoft WSL src/windows/common/wslutil.h defines c_vmOwner = L"WSL";
// WslCoreVm.cpp GenerateBackendRequest assigns it to request.Owner.
// https://github.com/microsoft/WSL/blob/2183822c4e547a9de392204eeecd3516c8944b12/src/windows/common/wslutil.h#L53
// https://github.com/microsoft/WSL/blob/2183822c4e547a9de392204eeecd3516c8944b12/src/windows/service/exe/WslCoreVm.cpp#L1384
// https://learn.microsoft.com/en-us/virtualization/api/hcs/reference/hcswaitforoperationresult
// HCS result documents belong to LocalFree, not CoTaskMemFree.
const hcsWSLOwner = "WSL"

var computeCore = syscall.NewLazyDLL("computecore.dll")
var hcsCreateOperationProc = computeCore.NewProc("HcsCreateOperation")
var hcsCloseOperationProc = computeCore.NewProc("HcsCloseOperation")
var hcsWaitProc = computeCore.NewProc("HcsWaitForOperationResult")
var hcsEnumerateProc = computeCore.NewProc("HcsEnumerateComputeSystems")
var hcsOpenProc = computeCore.NewProc("HcsOpenComputeSystem")
var hcsCloseSystemProc = computeCore.NewProc("HcsCloseComputeSystem")
var hcsPropertiesProc = computeCore.NewProc("HcsGetComputeSystemProperties")
var hcsTerminateProc = computeCore.NewProc("HcsTerminateComputeSystem")
var hcsLocalFreeProc = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")

type hcsSystemRecord struct {
	ID    string `json:"Id"`
	Owner string `json:"Owner"`
}

func hcsHRESULT(name string, value uintptr) error {
	if int32(uint32(value)) < 0 {
		return fmt.Errorf("%s HRESULT 0x%08x", name, uint32(value))
	}
	return nil
}

// Closing an operation handle never asserts cancellation or completion.
func hcsOperation(ctx context.Context, start func(uintptr) error) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, proc := range []*syscall.LazyProc{hcsCreateOperationProc, hcsCloseOperationProc, hcsWaitProc, hcsLocalFreeProc} {
		if err := proc.Find(); err != nil {
			return nil, err
		}
	}
	op, _, callErr := hcsCreateOperationProc.Call(0, 0)
	if op == 0 {
		return nil, fmt.Errorf("HcsCreateOperation: %w", callErr)
	}
	defer hcsCloseOperationProc.Call(op)
	if err := start(op); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		wait := 200 * time.Millisecond
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < wait {
			wait = time.Until(deadline)
		}
		if wait <= 0 {
			return nil, context.DeadlineExceeded
		}
		var result *uint16
		hr, _, _ := hcsWaitProc.Call(op, uintptr((wait+time.Millisecond-1)/time.Millisecond), uintptr(unsafe.Pointer(&result)))
		var data []byte
		var decodeErr error
		if result != nil {
			data, decodeErr = hcsResultBytes(result)
			hcsLocalFreeProc.Call(uintptr(unsafe.Pointer(result)))
		}
		if uint32(hr) == 0x80370118 {
			continue
		} // HCS_E_OPERATION_TIMEOUT.
		if err := hcsHRESULT("HcsWaitForOperationResult", hr); err != nil {
			return nil, err
		}
		if decodeErr != nil {
			return nil, decodeErr
		}
		return data, nil
	}
}

func hcsResultBytes(p *uint16) ([]byte, error) {
	units := make([]uint16, 0, 4096)
	for i := uintptr(0); i < 8*1024*1024; i++ {
		c := *(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(p)) + i*2))
		if c == 0 {
			return []byte(syscall.UTF16ToString(units)), nil
		}
		units = append(units, c)
	}
	return nil, errors.New("HCS result exceeds 16 MiB")
}

func hcsWSLSystems(ctx context.Context) ([]string, error) {
	if err := hcsEnumerateProc.Find(); err != nil {
		return nil, err
	}
	query, _ := syscall.UTF16PtrFromString(`{"Owners":["WSL"]}`)
	data, err := hcsOperation(ctx, func(op uintptr) error {
		hr, _, _ := hcsEnumerateProc.Call(uintptr(unsafe.Pointer(query)), op)
		return hcsHRESULT("HcsEnumerateComputeSystems", hr)
	})
	if err != nil {
		return nil, err
	}
	var systems []hcsSystemRecord
	if err := json.Unmarshal(data, &systems); err != nil {
		return nil, fmt.Errorf("decode HCS enumeration: %w", err)
	}
	if strings.TrimSpace(string(data)) == "null" {
		return nil, errors.New("HCS enumeration returned null")
	}
	ids := make([]string, 0, len(systems))
	seen := make(map[string]bool)
	for _, system := range systems {
		if system.Owner != hcsWSLOwner || system.ID == "" || seen[system.ID] {
			return nil, errors.New("HCS enumeration has invalid WSL owner/id")
		}
		seen[system.ID] = true
		ids = append(ids, system.ID)
	}
	return ids, nil
}

func hcsTerminate(ctx context.Context, id string) error {
	for _, proc := range []*syscall.LazyProc{hcsOpenProc, hcsCloseSystemProc, hcsPropertiesProc, hcsTerminateProc} {
		if err := proc.Find(); err != nil {
			return err
		}
	}
	ids, err := hcsWSLSystems(ctx)
	if err != nil {
		return err
	}
	selected := false
	for _, current := range ids {
		if current == id {
			selected = true
		}
	}
	if !selected {
		return errors.New("requested compute system is not an enumerated WSL system")
	}
	name, err := syscall.UTF16PtrFromString(id)
	if err != nil {
		return err
	}
	var system uintptr
	hr, _, _ := hcsOpenProc.Call(uintptr(unsafe.Pointer(name)), 0x10000000, uintptr(unsafe.Pointer(&system))) // GENERIC_ALL required by HCS.
	if err := hcsHRESULT("HcsOpenComputeSystem", hr); err != nil {
		return err
	}
	if system == 0 {
		return errors.New("HcsOpenComputeSystem returned a null handle")
	}
	defer hcsCloseSystemProc.Call(system)
	query, _ := syscall.UTF16PtrFromString(`{}`)
	data, err := hcsOperation(ctx, func(op uintptr) error {
		hr, _, _ := hcsPropertiesProc.Call(system, op, uintptr(unsafe.Pointer(query)))
		return hcsHRESULT("HcsGetComputeSystemProperties", hr)
	})
	if err != nil {
		return err
	}
	var properties hcsSystemRecord
	if err := json.Unmarshal(data, &properties); err != nil {
		return fmt.Errorf("decode HCS properties: %w", err)
	}
	// Reobserve the actual opened handle, not merely the preceding query filter.
	if properties.Owner != hcsWSLOwner {
		return errors.New("opened compute system owner is not WSL")
	}
	if properties.ID != "" && properties.ID != id {
		return errors.New("opened compute system identity changed")
	}
	_, err = hcsOperation(ctx, func(op uintptr) error {
		hr, _, _ := hcsTerminateProc.Call(system, op, 0)
		return hcsHRESULT("HcsTerminateComputeSystem", hr)
	})
	return err
}

func nativeVmmemWSLPresent() (bool, error) {
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false, err
	}
	defer syscall.CloseHandle(snapshot)
	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	err = syscall.Process32First(snapshot, &entry)
	for err == nil {
		name := syscall.UTF16ToString(entry.ExeFile[:])
		if strings.EqualFold(name, "vmmemWSL") || strings.EqualFold(name, "vmmemWSL.exe") {
			return true, nil
		}
		err = syscall.Process32Next(snapshot, &entry)
	}
	if err != syscall.ERROR_NO_MORE_FILES {
		return false, err
	}
	return false, nil
}

package nativecmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Evidence only. These observations never select, stop, exempt or acquire a
// process. Retain them during the same snapshot as the refusal, rather than
// rereading an exited/reused PID when an error is later formatted.
type nativeRefusalExecutable struct {
	status string
	start  string
	object string
	pin    *nativeCustodyWitness
}

var nativeRefusalBeforeRevalidate = func(string) {}
var nativeRefusalBeforeJointRevalidate = func(string, string) {}

func (w nativeRefusalExecutable) close() {
	if w.pin != nil {
		w.pin.close()
	}
}

func nativeDiagnosticStart(start string) string {
	if value, err := strconv.ParseUint(start, 10, 64); err == nil {
		return strconv.FormatUint(value, 10)
	}
	return "unavailable"
}

func observeNativeRefusalExecutable(path string, expectedParent int, expectedStart string) nativeRefusalExecutable {
	result := nativeRefusalExecutable{status: "unavailable", start: "unavailable", object: "-:-"}
	parent, start := readNativeProcIdentity(path)
	if nativeDiagnosticStart(start) == "unavailable" || start != expectedStart || parent != expectedParent {
		return result
	}
	result.start = nativeDiagnosticStart(start)
	name := filepath.Join(path, "exe")
	info, err := os.Stat(name)
	if err != nil || custodyObject(info) == nil {
		return result
	}
	pin := &nativeCustodyWitness{}
	pin.capture(name, info)
	nativeRefusalBeforeRevalidate(path)
	finalParent, finalStart := readNativeProcIdentity(path)
	if finalParent != parent || finalStart != start || !pin.revalidate() {
		pin.close()
		result.status = "unstable"
		result.start = "unavailable"
		return result
	}
	object := custodyObject(info)
	result.status = "stable"
	result.object = fmt.Sprintf("%x:%x", object.Device, object.Inode)
	result.pin = pin
	return result
}

func observeNativeUnownedProcessWitness(path string, parent int, start, touchKind string) string {
	child := observeNativeRefusalExecutable(path, parent, start)
	defer child.close()
	parentPath := filepath.Join(nativeSlotProcRoot, strconv.Itoa(parent))
	grandparent, parentStart := readNativeProcIdentity(parentPath)
	parentWitness := observeNativeRefusalExecutable(parentPath, grandparent, parentStart)
	defer parentWitness.close()
	nativeRefusalBeforeJointRevalidate(path, parentPath)
	// The parent must still be this child's parent after its own observation.
	finalParent, finalStart := readNativeProcIdentity(path)
	if finalParent != parent || finalStart != start || !nativeRefusalExecutableStillMatches(path, parent, start, child) {
		child = nativeRefusalExecutable{status: "unstable", start: "unavailable", object: "-:-"}
		parentWitness = child
	} else if !nativeRefusalExecutableStillMatches(parentPath, grandparent, parentStart, parentWitness) {
		parentWitness = nativeRefusalExecutable{status: "unstable", start: "unavailable", object: "-:-"}
	}
	observerPID := os.Getpid()
	_, observerStart := readNativeProcIdentity(filepath.Join(nativeSlotProcRoot, strconv.Itoa(observerPID)))
	return formatNativeUnownedProcessWitness(touchKind, child, parentWitness, observerPID, observerStart)
}

func formatNativeUnownedProcessWitness(touchKind string, child, parentWitness nativeRefusalExecutable, observerPID int, observerStart string) string {
	// Clamp even internal tokens, so no future caller can export a proc path.
	switch touchKind {
	case "cwd", "root", "exe", "fd":
	default:
		touchKind = "unavailable"
	}
	// w1 has four slash-separated fields: touch kind, child status/object,
	// parent status/start/object, observer PID/start. All numbers are hex;
	// S/U/X mean stable/unavailable/unstable, and '-' means unavailable.
	// Seven uint64 values plus fixed tokens fit the existing 512-byte refusal
	// envelope even with maximum-width original PID/index/start fields.
	return fmt.Sprintf("w1=%s/%s:%s/%s:%s:%s/%x:%s", touchKind, nativeRefusalStatus(child.status), child.object, nativeRefusalStatus(parentWitness.status), nativeRefusalHexStart(parentWitness.start), parentWitness.object, observerPID, nativeRefusalHexStart(observerStart))
}

func nativeRefusalExecutableStillMatches(path string, parent int, start string, witness nativeRefusalExecutable) bool {
	if witness.status != "stable" {
		// Unavailable observations make no claim that needs promotion.
		return true
	}
	finalParent, finalStart := readNativeProcIdentity(path)
	stableObject := witness.pin != nil && witness.pin.revalidate()
	afterParent, afterStart := readNativeProcIdentity(path)
	return stableObject && finalParent == parent && afterParent == parent && finalStart == start && afterStart == start
}

func nativeRefusalHexStart(start string) string {
	if value, err := strconv.ParseUint(start, 10, 64); err == nil {
		return strconv.FormatUint(value, 16)
	}
	return "-"
}

func nativeRefusalStatus(status string) string {
	switch status {
	case "stable":
		return "S"
	case "unstable":
		return "X"
	default:
		return "U"
	}
}

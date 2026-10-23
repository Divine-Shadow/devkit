package nativecmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

// This sink records the first matched ancestor from the existing pinned walk.
// Capturing metadata cannot change its physical-touch decision.
type nativePhysicalContact struct {
	matched  os.FileInfo
	pin      *os.File
	linkInfo os.FileInfo
	link     string
}

func (p *nativePhysicalContact) capture(current *os.File, info os.FileInfo) {
	fd, err := syscall.Dup(int(current.Fd()))
	if err != nil {
		return
	}
	syscall.CloseOnExec(fd)
	p.pin = os.NewFile(uintptr(fd), "native-contact-ancestor")
	p.matched = info
}
func (p *nativePhysicalContact) capturePath(path string, info os.FileInfo) {
	fd, err := syscall.Open(path, 0x200000|syscall.O_CLOEXEC, 0)
	if err != nil {
		return
	}
	p.pin = os.NewFile(uintptr(fd), "native-contact-ancestor")
	p.matched = info
}
func (p *nativePhysicalContact) close() {
	if p.pin != nil {
		_ = p.pin.Close()
		p.pin = nil
	}
}
func (p *nativePhysicalContact) stable() bool {
	if p.pin == nil || p.matched == nil {
		return false
	}
	held, err := p.pin.Stat()
	return err == nil && os.SameFile(held, p.matched)
}

type nativeContactExecutable struct {
	Status        string               `json:"status"`
	Object        *nativeCustodyObject `json:"object,omitempty"`
	StoreIdentity string               `json:"storeIdentity"`
}
type nativeProcessContact struct {
	Schema              string                  `json:"schemaVersion"`
	PID                 int                     `json:"pid"`
	Start               string                  `json:"startTicks"`
	Parent              int                     `json:"parent"`
	ParentStart         string                  `json:"parentStartTicks"`
	ParentStatus        string                  `json:"parentStatus"`
	Status              string                  `json:"status"`
	TouchKind           string                  `json:"touchKind"`
	RootClasses         []string                `json:"rootClasses"`
	RootClassMeaning    string                  `json:"rootClassMeaning"`
	MatchedRoot         *nativeCustodyObject    `json:"matchedRoot,omitempty"`
	Executable          nativeContactExecutable `json:"executable"`
	Namespaces          map[string]string       `json:"namespaces,omitempty"`
	ObserverNamespaces  map[string]string       `json:"observerNamespaces,omitempty"`
	NamespaceComparison map[string]string       `json:"namespaceComparison"`
}

// A store executable is public immutable identity, not an arbitrary path. Do
// not export user paths, deleted suffixes, traversal, whitespace or shell text.
var nativeStoreExecutableGrammar = regexp.MustCompile(`^/nix/store/[0123456789abcdfghijklmnpqrsvwxyz]{32}-[A-Za-z0-9+._=-]+/(bin|libexec)/[A-Za-z0-9+._=-]+(/[A-Za-z0-9+._=-]+)*$`)

func nativeSafeStoreExecutable(path string) bool {
	if len(path) > 512 || filepath.Clean(path) != path || !nativeStoreExecutableGrammar.MatchString(path) {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

var nativeContactBeforeRevalidate = func(string) {}

func observeNativeProcessContact(path string, parent int, start, kind string, identity nativeSlotProcessIdentity, targets []os.FileInfo, physical *nativePhysicalContact) nativeProcessContact {
	pid, _ := strconv.Atoi(filepath.Base(path))
	c := nativeProcessContact{Schema: "devkit/native-process-contact/v1", PID: pid, Start: nativeDiagnosticStart(start), Parent: parent, ParentStart: "unavailable", ParentStatus: "unavailable", Status: "unavailable", TouchKind: kind, RootClasses: []string{}, RootClassMeaning: "first-matched-physical-ancestor", Executable: nativeContactExecutable{Status: "unavailable", StoreIdentity: "unknown"}, NamespaceComparison: map[string]string{}}
	switch kind {
	case "cwd", "root", "exe", "fd":
	default:
		c.TouchKind = "unavailable"
	}
	pin := &nativeCustodyWitness{}
	defer pin.close()
	if physical != nil && physical.stable() {
		c.MatchedRoot = custodyObject(physical.matched)
		// Multiple declared roots may be the same physical object. Report every
		// matching class at this ancestor; do not infer outer ancestors from names.
		for _, root := range []struct{ kind, path string }{{"worktree", identity.hostWorktree}, {"home", identity.hostHome}, {"state", identity.stateRoot}} {
			info, err := os.Stat(root.path)
			if err != nil || !os.SameFile(info, physical.matched) {
				continue
			}
			for _, target := range targets {
				if os.SameFile(info, target) {
					c.RootClasses = append(c.RootClasses, root.kind)
					pin.capture(root.path, info)
					break
				}
			}
		}
		if physical.linkInfo != nil && physical.link != "" {
			pin.capture(filepath.Join(path, physical.link), physical.linkInfo)
		} else {
			pin.pinFailed = true
		}
	}
	ns := custodyNamespaces(path)
	observerPath := filepath.Join(nativeSlotProcRoot, "self")
	observerNS := custodyNamespaces(observerPath)
	c.Namespaces = ns
	c.ObserverNamespaces = observerNS
	for _, name := range []string{"pid", "mnt", "user"} {
		c.NamespaceComparison[name] = "unavailable"
		if ns != nil && observerNS != nil {
			c.NamespaceComparison[name] = "different"
			if ns[name] == observerNS[name] {
				c.NamespaceComparison[name] = "same"
			}
		}
		for _, base := range []string{path, observerPath} {
			link := filepath.Join(base, "ns", name)
			info, err := os.Stat(link)
			if err != nil {
				pin.pinFailed = true
			} else {
				pin.capture(link, info)
			}
		}
	}
	exe := observeNativeRefusalExecutable(path, parent, start)
	defer exe.close()
	c.Executable.Status = exe.status
	if exe.status == "stable" {
		info := exe.pin.infos[filepath.Join(path, "exe")]
		c.Executable.Object = custodyObject(info)
		visible, err := os.Readlink(filepath.Join(path, "exe"))
		if err == nil && nativeSafeStoreExecutable(visible) {
			host, hostErr := os.Stat(visible)
			if hostErr == nil && info != nil && os.SameFile(info, host) {
				pin.capture(visible, host)
				c.Executable.StoreIdentity = visible
			}
		}
	}
	parentPath := filepath.Join(nativeSlotProcRoot, strconv.Itoa(parent))
	grandparent, parentStart := readNativeProcIdentity(parentPath)
	if nativeDiagnosticStart(parentStart) != "unavailable" {
		c.ParentStart = nativeDiagnosticStart(parentStart)
		c.ParentStatus = "stable"
	}
	metadataAvailable := physical != nil && physical.stable() && len(c.RootClasses) > 0 && ns != nil && observerNS != nil && !pin.pinFailed
	beforeParent, beforeStart := readNativeProcIdentity(path)
	nativeContactBeforeRevalidate(path)
	finalParent, finalStart := readNativeProcIdentity(path)
	afterGrandparent, afterParentStart := readNativeProcIdentity(parentPath)
	if c.ParentStatus == "stable" && (afterGrandparent != grandparent || afterParentStart != parentStart) {
		c.ParentStatus = "unstable"
	}
	if start == "" || beforeStart != start || beforeParent != parent || finalStart != start || finalParent != parent {
		c.Status = "unstable"
	} else if metadataAvailable {
		c.Status = "unstable"
		if physical.stable() && reflect.DeepEqual(ns, custodyNamespaces(path)) && reflect.DeepEqual(observerNS, custodyNamespaces(observerPath)) && pin.revalidate() && nativeRefusalExecutableStillMatches(path, parent, start, exe) {
			c.Status = "stable"
		}
	}
	if c.Status != "stable" {
		c.RootClasses = []string{}
		c.MatchedRoot = nil
		c.Executable.StoreIdentity = "unknown"
		if c.Status == "unstable" {
			c.Executable.Status = "unstable"
		}
	}
	return c
}

// Keep the established 512-byte refusal unchanged. One additional bounded
// stderr record retains evidence even when the foreign process has exited by
// the time a receiver reads the ordinary durable effect output.
func emitNativeProcessContact(contact *nativeProcessContact) {
	if contact == nil {
		return
	}
	b, err := json.Marshal(contact)
	if err == nil && len(b) <= 4096 {
		_, _ = os.Stderr.Write(append(append([]byte("codex_native_process_contact="), b...), '\n'))
	}
}

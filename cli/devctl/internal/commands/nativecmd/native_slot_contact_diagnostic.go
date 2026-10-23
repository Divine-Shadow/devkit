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
	Comm                string                  `json:"comm"`
	Parent              int                     `json:"parent"`
	ParentStart         string                  `json:"parentStartTicks"`
	ParentStatus        string                  `json:"parentStatus"`
	ParentExecutable    nativeContactExecutable `json:"parentExecutable"`
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

var nativeContactCommGrammar = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// readNativeProcContactIdentity retains only the kernel comm token already
// present in stat. It does not read argv, environment, or another proc path.
func readNativeProcContactIdentity(path string) (int, string, string) {
	data, err := os.ReadFile(filepath.Join(path, "stat"))
	if err != nil {
		return 0, "", ""
	}
	text := string(data)
	open, close := strings.Index(text, "("), strings.LastIndex(text, ")")
	if open < 0 || close <= open {
		return 0, "", ""
	}
	fields := strings.Fields(text[close+1:])
	if len(fields) < 20 {
		return 0, "", ""
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, "", ""
	}
	comm := text[open+1 : close]
	if !nativeContactCommGrammar.MatchString(comm) {
		comm = ""
	}
	return parent, fields[19], comm
}

func observeNativeProcessContact(path string, parent int, start, kind string, identity nativeSlotProcessIdentity, targets []os.FileInfo, physical *nativePhysicalContact) nativeProcessContact {
	pid, _ := strconv.Atoi(filepath.Base(path))
	c := nativeProcessContact{Schema: "devkit/native-process-contact/v1", PID: pid, Start: nativeDiagnosticStart(start), Comm: "unavailable", Parent: parent, ParentStart: "unavailable", ParentStatus: "unavailable", ParentExecutable: nativeContactExecutable{Status: "unavailable", StoreIdentity: "unknown"}, Status: "unavailable", TouchKind: kind, RootClasses: []string{}, RootClassMeaning: "first-matched-physical-ancestor", Executable: nativeContactExecutable{Status: "unavailable", StoreIdentity: "unknown"}, NamespaceComparison: map[string]string{}}
	initialParent, initialStart, initialComm := readNativeProcContactIdentity(path)
	if initialParent == parent && initialStart == start && initialComm != "" {
		c.Comm = initialComm
	}
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
	parentExe := observeNativeRefusalExecutable(parentPath, grandparent, parentStart)
	defer parentExe.close()
	if parentExe.status == "stable" {
		info := parentExe.pin.infos[filepath.Join(parentPath, "exe")]
		c.ParentExecutable.Status = "stable"
		c.ParentExecutable.Object = custodyObject(info)
		visible, err := os.Readlink(filepath.Join(parentPath, "exe"))
		if err == nil && nativeSafeStoreExecutable(visible) {
			host, hostErr := os.Stat(visible)
			if hostErr == nil && info != nil && os.SameFile(info, host) {
				pin.capture(visible, host)
				c.ParentExecutable.StoreIdentity = visible
			}
		}
	}
	metadataAvailable := physical != nil && physical.stable() && len(c.RootClasses) > 0 && ns != nil && observerNS != nil && !pin.pinFailed
	beforeParent, beforeStart := readNativeProcIdentity(path)
	nativeContactBeforeRevalidate(path)
	finalParent, finalStart, finalComm := readNativeProcContactIdentity(path)
	afterGrandparent, afterParentStart := readNativeProcIdentity(parentPath)
	if c.ParentStatus == "stable" && (afterGrandparent != grandparent || afterParentStart != parentStart) {
		c.ParentStatus = "unstable"
	}
	// The parent executable is evidence about the same parent identity, not a
	// reusable fact about a PID. Revalidate its pinned object after the child's
	// final stat boundary before retaining the immutable store identity.
	if c.ParentStatus != "stable" || !nativeRefusalExecutableStillMatches(parentPath, grandparent, parentStart, parentExe) {
		if c.ParentStatus == "stable" {
			c.ParentStatus = "unstable"
		}
		c.ParentExecutable = nativeContactExecutable{Status: "unavailable", StoreIdentity: "unknown"}
	}
	if start == "" || beforeStart != start || beforeParent != parent || finalStart != start || finalParent != parent || finalComm == "" || finalComm != c.Comm {
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
		c.Comm = "unavailable"
		c.ParentExecutable = nativeContactExecutable{Status: "unavailable", StoreIdentity: "unknown"}
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

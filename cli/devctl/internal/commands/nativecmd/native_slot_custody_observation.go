package nativecmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"

	nativeagent "devkit/cli/devctl/internal/runtime/agent"
)

// Metadata only. It grants neither process ownership nor reset eligibility.
type nativeCustodyObject struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	Mode   uint32 `json:"mode"`
	UID    uint32 `json:"uid"`
	GID    uint32 `json:"gid"`
}
type nativeCustodyAlias struct {
	Kind     string               `json:"kind"`
	Status   string               `json:"status"`
	Expected *nativeCustodyObject `json:"expected,omitempty"`
	Observed *nativeCustodyObject `json:"observed,omitempty"`
	Same     bool                 `json:"same"`
}
type nativeCustodyWitness struct {
	PID                int                  `json:"pid"`
	Parent             int                  `json:"parent"`
	Start              string               `json:"startTicks"`
	Boot               string               `json:"bootId"`
	UIDs               string               `json:"uids"`
	Namespaces         map[string]string    `json:"namespaces"`
	ObserverNamespaces map[string]string    `json:"observerNamespaces"`
	Status             string               `json:"status"`
	HomeMatches        bool                 `json:"homeMatches"`
	WorktreeMatches    bool                 `json:"worktreeMatches"`
	Predicate          string               `json:"predicate"`
	Aliases            []nativeCustodyAlias `json:"aliases"`
	pins               map[string]*os.File
	infos              map[string]os.FileInfo
	absent             []string
	pinFailed          bool
}
type nativeCustodyReport struct {
	Schema       string                 `json:"schemaVersion"`
	Target       string                 `json:"target"`
	Index        int                    `json:"index"`
	Status       string                 `json:"status"`
	Unavailable  int                    `json:"unavailable"`
	Observations []nativeCustodyWitness `json:"observations"`
}

func custodyObject(info os.FileInfo) *nativeCustodyObject {
	if info == nil {
		return nil
	}
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	return &nativeCustodyObject{uint64(s.Dev), s.Ino, s.Mode, s.Uid, s.Gid}
}
func (w *nativeCustodyWitness) capture(path string, info os.FileInfo) {
	if info == nil {
		return
	}
	if w.pins == nil {
		w.pins = map[string]*os.File{}
		w.infos = map[string]os.FileInfo{}
	}
	if _, ok := w.pins[path]; ok {
		return
	}
	// O_PATH pins the observed object without reading it, following normal stat semantics.
	fd, err := syscall.Open(path, 0x200000|syscall.O_CLOEXEC, 0)
	if err != nil {
		w.pinFailed = true
		return
	}
	f := os.NewFile(uintptr(fd), "custody-object")
	held, err := f.Stat()
	if err != nil || !os.SameFile(info, held) {
		w.pinFailed = true
		_ = f.Close()
		return
	}
	w.pins[path] = f
	w.infos[path] = info
}
func (w *nativeCustodyWitness) state(kind, path, hostPath, observedPath string, host, observed os.FileInfo, err error) {
	status := "observed"
	if os.IsNotExist(err) {
		status = "absent"
		w.absent = append(w.absent, observedPath)
	} else if err != nil {
		status = "unavailable"
	}
	w.Aliases = append(w.Aliases, nativeCustodyAlias{Kind: kind, Status: status, Expected: custodyObject(host), Observed: custodyObject(observed), Same: err == nil && os.SameFile(host, observed)})
	w.capture(hostPath, host)
	w.capture(observedPath, observed)
}
func (w *nativeCustodyWitness) revalidate() bool {
	if w.pinFailed {
		return false
	}
	for _, path := range w.absent {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			return false
		}
	}
	for path, f := range w.pins {
		held, e := f.Stat()
		named, n := os.Stat(path)
		if e != nil || n != nil || !os.SameFile(w.infos[path], held) || !os.SameFile(held, named) || !reflect.DeepEqual(custodyObject(w.infos[path]), custodyObject(named)) {
			return false
		}
	}
	return true
}
func (w *nativeCustodyWitness) close() {
	for _, f := range w.pins {
		_ = f.Close()
	}
	w.pins = nil
	w.infos = nil
}

var nativeCustodyBeforeRevalidate = func() {}

var nativeCustodyBoot = func() string {
	b, e := custodyReadBounded("/proc/sys/kernel/random/boot_id", 128)
	if e != nil {
		return ""
	}
	v := strings.TrimSpace(string(b))
	if len(v) != 36 || strings.Count(v, "-") != 4 {
		return ""
	}
	return v
}

func custodyReadBounded(path string, limit int64) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, syscall.EOVERFLOW
	}
	return b, e
}
func custodyNamespaces(path string) map[string]string {
	out := map[string]string{}
	for _, n := range []string{"pid", "mnt", "user"} {
		v, e := os.Readlink(filepath.Join(path, "ns", n))
		if e != nil || !strings.HasPrefix(v, n+":[") || !strings.HasSuffix(v, "]") {
			return nil
		}
		if _, e := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(v, n+":["), "]"), 10, 64); e != nil {
			return nil
		}
		out[n] = v
	}
	return out
}
func custodyUIDs(path string) string {
	b, e := custodyReadBounded(filepath.Join(path, "status"), 65536)
	if e != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "Uid:") {
			v := strings.Fields(strings.TrimPrefix(l, "Uid:"))
			if len(v) != 4 {
				return ""
			}
			for _, x := range v {
				if _, e := strconv.ParseUint(x, 10, 32); e != nil {
					return ""
				}
			}
			return strings.Join(v, ":")
		}
	}
	return ""
}
func observeNativeSlotCustody(spec nativeagent.Spec) nativeCustodyReport {
	r := nativeCustodyReport{Schema: "devkit/selected-slot-custody-observation/v1", Index: spec.ID.Index, Status: "observed", Observations: []nativeCustodyWitness{}}
	identity := nativeSlotProcessIdentity{index: spec.ID.Index, hostHome: spec.HostHome, hostWorktree: spec.HostWorktree, sandboxHome: spec.SandboxHome, sandboxWorktree: spec.SandboxWorktree, stateRoot: spec.StateRoot, sandboxState: spec.SandboxStateRoot}
	entries, e := os.ReadDir(nativeSlotProcRoot)
	if e != nil {
		r.Status = "unavailable"
		return r
	}
	if len(entries) > 65536 {
		r.Status = "truncated"
		return r
	}
	budget := 16 * 1024 * 1024
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil || pid < 1 || pid == os.Getpid() {
			continue
		}
		path := filepath.Join(nativeSlotProcRoot, entry.Name())
		parent, start := readNativeProcIdentity(path)
		boot := nativeCustodyBoot()
		envData, e := custodyReadBounded(filepath.Join(path, "environ"), 1024*1024)
		if e != nil {
			r.Unavailable++
			continue
		}
		budget -= len(envData)
		if budget < 0 {
			r.Status = "truncated"
			break
		}
		env := parseNativeProcEnviron(envData)
		if env["DEVKIT_NATIVE_AGENT"] != strconv.Itoa(identity.index) {
			continue
		}
		w := nativeCustodyWitness{PID: pid, Parent: parent, Start: start, Boot: boot, UIDs: custodyUIDs(path), Namespaces: custodyNamespaces(path), ObserverNamespaces: custodyNamespaces(filepath.Join(nativeSlotProcRoot, "self")), Status: "unstable", Aliases: []nativeCustodyAlias{}}
		rootInfo, rootErr := os.Stat(filepath.Join(path, "root"))
		if rootErr != nil {
			w.pinFailed = true
		}
		w.capture(filepath.Join(path, "root"), rootInfo)
		for _, ns := range []string{"pid", "mnt", "user"} {
			for _, base := range []string{path, filepath.Join(nativeSlotProcRoot, "self")} {
				name := filepath.Join(base, "ns", ns)
				info, err := os.Stat(name)
				if err != nil {
					w.pinFailed = true
				}
				w.capture(name, info)
			}
		}
		matches, err := nativeProcHomeMatchesSlotObserved(path, env["HOME"], identity, &w)
		if !w.HomeMatches && !w.WorktreeMatches {
			w.close()
			continue
		}
		w.Predicate = "different"
		if matches {
			w.Predicate = "match"
		} else if err != nil {
			w.Predicate = "conflict-or-unavailable"
		}
		nativeCustodyBeforeRevalidate()
		finalParent, finalStart := readNativeProcIdentity(path)
		finalEnv, envErr := custodyReadBounded(filepath.Join(path, "environ"), 1024*1024)
		if start != "" && start == finalStart && parent == finalParent && boot != "" && boot == nativeCustodyBoot() && w.UIDs != "" && w.UIDs == custodyUIDs(path) && w.Namespaces != nil && w.ObserverNamespaces != nil && reflect.DeepEqual(w.Namespaces, custodyNamespaces(path)) && reflect.DeepEqual(w.ObserverNamespaces, custodyNamespaces(filepath.Join(nativeSlotProcRoot, "self"))) && envErr == nil && bytes.Equal(envData, finalEnv) && w.revalidate() {
			w.Status = "stable"
		}
		w.close()
		r.Observations = append(r.Observations, w)
		if len(r.Observations) == 32 {
			r.Status = "truncated"
			break
		}
	}
	// Empty/unavailable observations are never proof of process absence.
	return r
}

func validateCustodyObservationPlanArgs(args []string) error {
	active := false
	for _, arg := range args {
		if arg == "--process-custody-observation" {
			active = true
		}
	}
	if !active {
		return nil
	}
	if len(args) == 0 || args[0] != "plan" {
		return fmt.Errorf("custody observation requires native plan")
	}
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if seen[arg] {
			return fmt.Errorf("duplicate custody observation argument")
		}
		seen[arg] = true
		switch arg {
		case "--process-custody-observation":
		case "--repo", "--index", "--gui-target-id", "--format":
			if i+1 >= len(args) {
				return fmt.Errorf("missing custody observation value")
			}
			i++
			if arg == "--repo" && args[i] != "ouroboros-ide" || arg == "--format" && args[i] != "json" {
				return fmt.Errorf("invalid custody observation value")
			}
		default:
			return fmt.Errorf("custody observation rejects caller geometry/runtime options")
		}
	}
	for _, arg := range []string{"--repo", "--index", "--gui-target-id", "--format"} {
		if !seen[arg] {
			return fmt.Errorf("custody observation requires exact source-selected GUI target/index and JSON")
		}
	}
	return nil
}

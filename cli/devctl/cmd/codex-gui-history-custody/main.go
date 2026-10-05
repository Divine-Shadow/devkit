package main

// This executable only binds the existing cold-history validator to immutable
// constructor-selected fixed-slot geometry. It owns no reset, import or prune.
import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"devkit/cli/devctl/internal/codexhistory"
)

var packageBindings string

type slotBinding struct {
	HostHome         string `json:"hostHome"`
	MountedGuestHome string `json:"mountedGuestHome"`
	GuestHome        string `json:"guestHome"`
	HostWorktree     string `json:"hostWorktree"`
	GuestWorktree    string `json:"guestWorktree"`
}
type bindings struct {
	SchemaVersion string                 `json:"schemaVersion"`
	StateRoot     string                 `json:"stateRoot"`
	Slots         map[string]slotBinding `json:"slots"`
}

func readBindings(path string) (bindings, error) {
	var value bindings
	file, err := os.Open(path)
	if err != nil {
		return value, fmt.Errorf("open package-owned history bindings: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 65537))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("decode package-owned history bindings: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return value, fmt.Errorf("history bindings must contain one object")
	}
	if value.SchemaVersion != "product-agent-history-bindings/v1" || len(value.Slots) != 2 {
		return value, fmt.Errorf("unsupported fixed-slot history bindings")
	}
	for _, id := range []string{"1", "2"} {
		slot, ok := value.Slots[id]
		if !ok {
			return value, fmt.Errorf("missing fixed slot %s", id)
		}
		for _, path := range []string{value.StateRoot, slot.HostHome, slot.MountedGuestHome, slot.GuestHome, slot.HostWorktree, slot.GuestWorktree} {
			if !filepath.IsAbs(path) || filepath.Clean(path) != path {
				return value, fmt.Errorf("history bindings require canonical absolute paths")
			}
		}
	}
	return value, nil
}

func optionsFor(value bindings, args []string) (codexhistory.SnapshotOptions, error) {
	if len(args) != 2 || (args[0] != "1" && args[0] != "2") || (args[1] != "host" && args[1] != "guest") {
		return codexhistory.SnapshotOptions{}, fmt.Errorf("usage: codex-gui-history-custody {1|2} {host|guest}")
	}
	slot := value.Slots[args[0]]
	index := 1
	if args[0] == "2" {
		index = 2
	}
	options := codexhistory.SnapshotOptions{
		Project: "product-agent", ResetKind: "fixed-product-slot-retirement", AgentIndex: index,
		HostHome: slot.HostHome, SandboxHome: slot.HostHome, HostWorktree: slot.HostWorktree,
		StateRoot: value.StateRoot,
	}
	if args[1] == "guest" {
		options.HostHome = slot.MountedGuestHome
		options.SandboxHome = slot.GuestHome
		options.HostWorktree = slot.GuestWorktree
	}
	return options, nil
}

func main() {
	// No caller-supplied binding or environment override exists in production.
	if !strings.HasPrefix(packageBindings, "/nix/store/") || filepath.Clean(packageBindings) != packageBindings {
		fmt.Fprintln(os.Stderr, "cold-history custody has no package-owned fixed-slot bindings")
		os.Exit(1)
	}
	value, err := readBindings(packageBindings)
	if err == nil {
		args := os.Args[1:]
		verify := len(args) == 4 && (args[0] == "verify" || args[0] == "verify-bundle")
		selection := args
		if verify {
			selection = args[1:3]
		}
		var options codexhistory.SnapshotOptions
		options, err = optionsFor(value, selection)
		if err == nil && verify {
			if args[0] == "verify-bundle" {
				err = codexhistory.VerifyCapturedBundle(options, args[3])
			} else {
				err = codexhistory.VerifyCapturedSource(options, args[3])
			}
			if err == nil {
				err = json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "verified", "manifestPath": args[3]})
			}
		} else if err == nil {
			var result codexhistory.Result
			result, err = codexhistory.Capture(options)
			if err == nil {
				err = json.NewEncoder(os.Stdout).Encode(result)
			}
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

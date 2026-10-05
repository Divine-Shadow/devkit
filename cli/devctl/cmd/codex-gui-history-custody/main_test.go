package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFixedBindingSelectionRejectsOtherSlotsAndCallerGeometry(t *testing.T) {
	value := bindings{StateRoot: "/var/lib/product-agent/history-custody/state", Slots: map[string]slotBinding{
		"1": {HostHome: "/slots/1/gui", MountedGuestHome: "/mount/1/home", GuestHome: "/var/lib/product-agent/home", HostWorktree: "/mount/1/worktree", GuestWorktree: "/mount/1/worktree"},
		"2": {HostHome: "/slots/2/gui", MountedGuestHome: "/mount/2/home", GuestHome: "/var/lib/product-agent/home", HostWorktree: "/mount/2/worktree", GuestWorktree: "/mount/2/worktree"},
	}}
	for _, args := range [][]string{{}, {"1"}, {"3", "host"}, {"1", "other"}, {"1", "host", "/caller/home"}, {"../1", "guest"}} {
		if _, err := optionsFor(value, args); err == nil {
			t.Fatalf("admitted caller geometry: %v", args)
		}
	}
	options, err := optionsFor(value, []string{"2", "guest"})
	if err != nil || options.HostHome != "/mount/2/home" || options.AgentIndex != 2 || options.Project != "product-agent" || options.ResetKind != "fixed-product-slot-retirement" || options.WorkspaceRoot != "" {
		t.Fatalf("wrong selected binding: %+v %v", options, err)
	}
}

func TestPackageBindingRejectsMalformedExtraOrRelativeSource(t *testing.T) {
	valid := `{"schemaVersion":"product-agent-history-bindings/v1","stateRoot":"/state","slots":{"1":{"hostHome":"/h1","mountedGuestHome":"/m1","guestHome":"/g","hostWorktree":"/w1","guestWorktree":"/mw1"},"2":{"hostHome":"/h2","mountedGuestHome":"/m2","guestHome":"/g","hostWorktree":"/w2","guestWorktree":"/mw2"}}}`
	path := filepath.Join(t.TempDir(), "bindings.json")
	for _, content := range []string{`{}`, valid + `{}`, `{"schemaVersion":"product-agent-history-bindings/v1","unknown":"refuse"}`, valid[:len(valid)-1]} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readBindings(path); err == nil {
			t.Fatalf("accepted unsupported bindings: %s", content)
		}
	}
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBindings(path); err != nil {
		t.Fatal(err)
	}
}

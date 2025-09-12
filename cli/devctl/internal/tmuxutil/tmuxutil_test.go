package tmuxutil

import (
    "reflect"
    "testing"
)

func TestNewSession(t *testing.T) {
	got := NewSession("sess", "echo hi")
	want := []string{"new-session", "-d", "-s", "sess", "echo hi"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NewSession mismatch: got %#v want %#v", got, want)
	}
}

func TestRenameWindow(t *testing.T) {
	got := RenameWindow("sess:0", "agent-1")
	want := []string{"rename-window", "-t", "sess:0", "agent-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RenameWindow mismatch: got %#v want %#v", got, want)
	}
}

func TestNewWindow(t *testing.T) {
	got := NewWindow("sess", "agent-2", "echo test")
	want := []string{"new-window", "-t", "sess", "-n", "agent-2", "echo test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NewWindow mismatch: got %#v want %#v", got, want)
	}
}

func TestAttach(t *testing.T) {
    got := Attach("sess")
    want := []string{"attach", "-t", "sess"}
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("Attach mismatch: got %#v want %#v", got, want)
    }
}

func TestHasSessionAndListWindows(t *testing.T) {
    if got := HasSession("s"); len(got) != 3 || got[0] != "has-session" || got[2] != "s" {
        t.Fatalf("HasSession args unexpected: %v", got)
    }
    if got := ListWindows("s"); len(got) != 5 || got[0] != "list-windows" || got[2] != "s" || got[3] != "-F" {
        t.Fatalf("ListWindows args unexpected: %v", got)
    }
}

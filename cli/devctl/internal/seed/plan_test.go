package seed

import "testing"

func TestBuildResetPlan(t *testing.T) {
	p := BuildResetPlan("/home/x")
	if len(p.Steps) != 2 {
		t.Fatalf("want 2 steps, got %d", len(p.Steps))
	}
	if p.Steps[0].Cmd[0] != "rm" || p.Steps[0].Cmd[1] != "-rf" {
		t.Fatalf("rm step malformed: %#v", p.Steps[0].Cmd)
	}
}

func TestBuildCopyFrom(t *testing.T) {
	p := BuildCopyFrom("/pool/slot1", "/home/x")
	if len(p.Steps) != 2 {
		t.Fatalf("want 2 steps, got %d", len(p.Steps))
	}
	if p.Steps[0].Cmd[0] != "cp" || p.Steps[0].Cmd[1] != "-a" {
		t.Fatalf("cp step malformed: %#v", p.Steps[0].Cmd)
	}
}

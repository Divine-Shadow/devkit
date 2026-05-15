package seed

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildResetPlan(t *testing.T) {
	p := BuildResetPlan("/home/x")
	if len(p.Steps) != 1 {
		t.Fatalf("want 1 step, got %d", len(p.Steps))
	}
	if p.Steps[0].Cmd[0] != "mkdir" || p.Steps[0].Cmd[1] != "-p" {
		t.Fatalf("mkdir step malformed: %#v", p.Steps[0].Cmd)
	}
	for _, arg := range p.Steps[0].Cmd {
		if arg == "/home/x/.codex/rollouts" {
			return
		}
	}
	t.Fatalf("reset plan missing Codex rollout dir: %#v", p.Steps[0].Cmd)
}

func TestBuildResetPlanPreservesCodexState(t *testing.T) {
	home := t.TempDir()
	writeCodexSentinels(t, home)

	p := BuildResetPlan(home)
	for _, step := range p.Steps {
		joined := strings.Join(step.Cmd, " ")
		if step.Cmd[0] == "rm" && strings.Contains(joined, ".codex") {
			t.Fatalf("reset plan must not remove Codex state: %#v", step.Cmd)
		}
		cmd := exec.Command(step.Cmd[0], step.Cmd[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("run reset step %#v: %v\n%s", step.Cmd, err, out)
		}
	}

	assertCodexSentinels(t, home)
	for _, dir := range []string{".codex", ".codex/rollouts", ".cache", ".config", ".local"} {
		if st, err := os.Stat(filepath.Join(home, dir)); err != nil || !st.IsDir() {
			t.Fatalf("required dir %s missing after reset plan: stat=%v err=%v", dir, st, err)
		}
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

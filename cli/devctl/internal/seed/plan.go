package seed

// SeedStep is a single container command (argv) to execute during seeding.
type SeedStep struct{ Cmd []string }

// Plan is an ordered list of steps.
type Plan struct{ Steps []SeedStep }

// BuildResetPlan removes the Codex dir and recreates the needed directories under home.
// This mirrors the behavior of ResetAndCreateDirsScript but returns argv steps to avoid heredocs.
func BuildResetPlan(home string) Plan {
	steps := []SeedStep{
		{Cmd: []string{"rm", "-rf", home + "/.codex"}},
		{Cmd: []string{"mkdir", "-p", home + "/.codex/rollouts", home + "/.cache", home + "/.config", home + "/.local"}},
	}
	return Plan{Steps: steps}
}

// BuildCopyFrom copies a prepared Codex home from src into $home/.codex and tightens permissions.
// src is a directory containing {auth.json, sessions/, rollouts/, ...}.
func BuildCopyFrom(src, home string) Plan {
	dst := home + "/.codex"
	steps := []SeedStep{
		// Copy contents: src/. to dst/
		{Cmd: []string{"cp", "-a", src + "/.", dst}},
		// Tighten permissions if auth.json exists
		{Cmd: []string{"bash", "-lc", "if [ -f '" + dst + "/auth.json' ]; then chmod 600 '" + dst + "/auth.json'; fi"}},
	}
	return Plan{Steps: steps}
}

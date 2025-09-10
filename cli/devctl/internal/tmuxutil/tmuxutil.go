package tmuxutil

// NewSession builds args for: tmux new-session -d -s <session> <cmd>
func NewSession(session, cmd string) []string {
	return []string{"new-session", "-d", "-s", session, cmd}
}

// RenameWindow builds args for: tmux rename-window -t <target> <newName>
// target can be session:idx or a window name.
func RenameWindow(target, newName string) []string {
	return []string{"rename-window", "-t", target, newName}
}

// NewWindow builds args for: tmux new-window -t <session> -n <name> <cmd>
func NewWindow(session, name, cmd string) []string {
	return []string{"new-window", "-t", session, "-n", name, cmd}
}

// Attach builds args for: tmux attach -t <session>
func Attach(session string) []string {
	return []string{"attach", "-t", session}
}

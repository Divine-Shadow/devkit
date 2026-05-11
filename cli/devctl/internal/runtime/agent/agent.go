package agent

import "strings"

// ID identifies a native devkit agent without relying on Docker or Compose
// identity such as service names, container indexes, or labels.
type ID struct {
	Project string `json:"project"`
	Index   int    `json:"index"`
	Repo    string `json:"repo"`
}

// Name returns a stable human-readable agent name.
func (id ID) Name() string {
	project := strings.TrimSpace(id.Project)
	if project == "" {
		project = "default"
	}
	if id.Index < 1 {
		id.Index = 1
	}
	return project + "-agent" + itoa(id.Index)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// Spec contains the filesystem anchors that make an agent durable across
// launcher implementations.
type Spec struct {
	ID              ID     `json:"id"`
	HostWorktree    string `json:"host_worktree"`
	SandboxWorktree string `json:"sandbox_worktree"`
	HostHome        string `json:"host_home"`
	SandboxHome     string `json:"sandbox_home"`
	StateRoot       string `json:"state_root"`
}

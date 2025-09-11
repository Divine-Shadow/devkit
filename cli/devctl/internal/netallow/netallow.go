package netallow

import (
	fz "devkit/cli/devctl/internal/files"
	"path/filepath"
)

// EnsureSSHGitHub adds ssh.github.com to proxy and DNS allowlists.
// Returns booleans indicating if files were modified.
func EnsureSSHGitHub(kitPath string) (proxyChanged, dnsChanged bool, err error) {
	p := filepath.Join(kitPath, "proxy", "allowlist.txt")
	d := filepath.Join(kitPath, "dns", "dnsmasq.conf")
	var e1, e2 error
	proxyChanged, e1 = fz.AppendLineIfMissing(p, "ssh.github.com")
	dnsChanged, e2 = fz.AppendLineIfMissing(d, "server=/ssh.github.com/1.1.1.1")
	if e1 != nil {
		err = e1
	}
	if e2 != nil && err == nil {
		err = e2
	}
	return
}

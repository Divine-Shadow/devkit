package plan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func RenderJSON(p Plan) ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

func RenderText(p Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Native agent plan\n")
	fmt.Fprintf(&b, "project: %s\n", p.Agent.ID.Project)
	fmt.Fprintf(&b, "index: %d\n", p.Agent.ID.Index)
	fmt.Fprintf(&b, "repo: %s\n", p.Agent.ID.Repo)
	fmt.Fprintf(&b, "devkit_host_root: %s\n", p.DevkitHostRoot)
	fmt.Fprintf(&b, "devkit_sandbox_root: %s\n", p.DevkitSandboxRoot)
	fmt.Fprintf(&b, "flake: %s\n", p.Flake)
	fmt.Fprintf(&b, "launcher: %s\n", p.Launcher)
	fmt.Fprintf(&b, "host_worktree: %s\n", p.Agent.HostWorktree)
	fmt.Fprintf(&b, "sandbox_worktree: %s\n", p.Agent.SandboxWorktree)
	fmt.Fprintf(&b, "host_home: %s\n", p.Agent.HostHome)
	fmt.Fprintf(&b, "sandbox_home: %s\n", p.Agent.SandboxHome)
	fmt.Fprintf(&b, "state_root: %s\n", p.Agent.StateRoot)
	fmt.Fprintf(&b, "broker_endpoint: %s\n", p.BrokerEndpoint)
	fmt.Fprintf(&b, "direct_docker_socket: %t\n", p.DirectDockerSocket)
	fmt.Fprintf(&b, "proxy: %s\n", p.Proxy.HTTPProxy)
	fmt.Fprintf(&b, "resolv_conf: %s\n", p.DNS.ResolvConf)
	fmt.Fprintf(&b, "binds:\n")
	for _, bind := range p.Binds {
		fmt.Fprintf(&b, "  - %s -> %s (%s, required=%t)\n", bind.Source, bind.Target, bind.Mode, bind.Required)
	}
	fmt.Fprintf(&b, "env:\n")
	keys := make([]string, 0, len(p.Env))
	for key := range p.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&b, "  %s=%s\n", key, p.Env[key])
	}
	return b.String()
}

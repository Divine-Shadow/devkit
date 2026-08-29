package plan

import (
	"devkit/cli/devctl/internal/runtime/agent"
)

func BuildManifest(opts BuildOptions, count int) (agent.Manifest, error) {
	if count < 1 {
		count = 1
	}
	agents := make([]agent.Spec, 0, count)
	var roots agent.Paths
	brokerEndpoint := opts.BrokerEndpoint
	for i := 1; i <= count; i++ {
		next := opts
		next.Index = i
		// A GUI target projection belongs only to the selected execution plan.
		// The shared all-slot manifest records lifecycle geometry, so replaying
		// one target's projection while enumerating sibling indexes is invalid.
		next.GUITargetID = ""
		p, err := BuildDevAll(next)
		if err != nil {
			return agent.Manifest{}, err
		}
		agents = append(agents, p.Agent)
		if i == 1 {
			brokerEndpoint = p.BrokerEndpoint
			roots = agent.Paths{
				HostWorktreeRoot:    p.HostWorktreeRoot,
				HostStateRoot:       p.HostStateRoot,
				SandboxWorktreeRoot: p.SandboxWorktreeRoot,
				SandboxStateRoot:    p.SandboxStateRoot,
			}
		}
	}
	first := agents[0]
	return agent.NewManifest(
		first.ID.Project,
		first.ID.Repo,
		agents,
		roots,
		opts.BaseBranch,
		opts.BranchPrefix,
		brokerEndpoint,
	), nil
}

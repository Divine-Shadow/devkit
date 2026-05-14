package capacity

import (
	"sort"

	"devkit/cli/devctl/internal/runtime/readiness"
)

type Agent struct {
	Index             int               `json:"index"`
	Status            string            `json:"status"`
	RuntimeReady      bool              `json:"runtime_ready"`
	RepoReady         bool              `json:"repo_ready"`
	CapacityAvailable bool              `json:"capacity_available"`
	WorktreeState     string            `json:"worktree_state"`
	BrokerState       string            `json:"broker_state"`
	SandboxState      string            `json:"sandbox_state"`
	ToolingState      string            `json:"tooling_state"`
	RepoState         string            `json:"repo_state"`
	Action            string            `json:"action,omitempty"`
	FailedChecks      []readiness.Check `json:"failed_checks,omitempty"`
	Checks            []readiness.Check `json:"checks"`
}

type Summary struct {
	Total             int     `json:"total"`
	Status            string  `json:"status"`
	UsableCapacity    int     `json:"usable_capacity"`
	RuntimeReady      int     `json:"runtime_ready"`
	RepoReady         int     `json:"repo_ready"`
	CapacityAvailable int     `json:"capacity_available"`
	BlockedAgents     []int   `json:"blocked_agents,omitempty"`
	DegradedAgents    []int   `json:"degraded_agents,omitempty"`
	Action            string  `json:"action,omitempty"`
	Agents            []Agent `json:"agents"`
}

func Build(reports map[int]readiness.Report) Summary {
	summary := Summary{Total: len(reports), Agents: make([]Agent, 0, len(reports))}
	indexes := make([]int, 0, len(reports))
	for index := range reports {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		report := reports[index]
		agent := Agent{
			Index:             index,
			Status:            report.Status(),
			RuntimeReady:      report.RuntimeReady(),
			RepoReady:         report.RepoReady(),
			CapacityAvailable: report.CapacityAvailable(),
			WorktreeState:     report.ComponentState("worktree"),
			BrokerState:       report.ComponentState("broker"),
			SandboxState:      report.ComponentState("sandbox"),
			ToolingState:      report.ComponentState("tooling"),
			RepoState:         report.ComponentState("repo"),
			Action:            report.Action(),
			FailedChecks:      report.FailedChecks(),
			Checks:            report.Checks,
		}
		if agent.RuntimeReady {
			summary.RuntimeReady++
		}
		if agent.RepoReady {
			summary.RepoReady++
		}
		if agent.CapacityAvailable {
			summary.CapacityAvailable++
		} else {
			summary.BlockedAgents = append(summary.BlockedAgents, agent.Index)
		}
		if agent.CapacityAvailable && !agent.RepoReady {
			summary.DegradedAgents = append(summary.DegradedAgents, agent.Index)
		}
		if summary.Action == "" && agent.Action != "" {
			summary.Action = agent.Action
		}
		summary.Agents = append(summary.Agents, agent)
	}
	summary.UsableCapacity = summary.CapacityAvailable
	summary.Status = summaryStatus(summary)
	return summary
}

func summaryStatus(summary Summary) string {
	if summary.Total == 0 {
		return "empty"
	}
	if summary.CapacityAvailable == 0 {
		return "blocked"
	}
	if summary.CapacityAvailable < summary.Total || summary.RepoReady < summary.Total {
		return "degraded"
	}
	return "ready"
}

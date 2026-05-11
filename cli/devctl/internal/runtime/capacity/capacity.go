package capacity

import (
	"sort"

	"devkit/cli/devctl/internal/runtime/readiness"
)

type Agent struct {
	Index             int               `json:"index"`
	RuntimeReady      bool              `json:"runtime_ready"`
	RepoReady         bool              `json:"repo_ready"`
	CapacityAvailable bool              `json:"capacity_available"`
	Checks            []readiness.Check `json:"checks"`
}

type Summary struct {
	Total             int     `json:"total"`
	RuntimeReady      int     `json:"runtime_ready"`
	RepoReady         int     `json:"repo_ready"`
	CapacityAvailable int     `json:"capacity_available"`
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
			RuntimeReady:      report.RuntimeReady(),
			RepoReady:         report.RepoReady(),
			CapacityAvailable: report.CapacityAvailable(),
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
		}
		summary.Agents = append(summary.Agents, agent)
	}
	return summary
}

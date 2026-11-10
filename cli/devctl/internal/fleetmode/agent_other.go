//go:build !windows

package fleetmode

import "errors"

func NewAgentOps(AgentConfig) (AgentOps, error) {
	return nil, errors.New("the station agent must run on Windows; use the fleet controller from this host")
}

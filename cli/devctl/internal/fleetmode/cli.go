package fleetmode

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
)

// Main is shared by the operator's controller and fixed Windows agent.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	agent := len(args) > 0 && args[0] == "agent"
	if agent {
		args = args[1:]
	}
	if len(args) == 0 || (args[0] != "vr" && args[0] != "status") {
		fmt.Fprintln(stderr, "usage: fleet-mode [agent] vr|status --config FILE; no automatic compute restore")
		return 2
	}
	mode := args[0]
	flags := flag.NewFlagSet("fleet-mode "+mode, flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "static station configuration")
	if flags.Parse(args[1:]) != nil || *path == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "exactly --config FILE is required")
		return 2
	}
	if agent {
		cfg, err := LoadAgentConfig(*path)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		result := RunAgent(ctx, mode, cfg)
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			return 1
		}
		return agentResultExit(mode, result)
	}
	cfg, err := LoadConfig(*path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	result := RunController(ctx, mode, cfg, SSHTransport{}, FileState{Path: cfg.StatePath})
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return 1
	}
	return controllerResultExit(mode, result)
}

func agentResultExit(mode string, result StationResult) int {
	if mode == "status" {
		if result.Error != "" {
			return 1
		}
		return 0
	}
	if !verifiedVRResult(result) {
		return 1
	}
	return 0
}

func controllerResultExit(mode string, result Result) int {
	if mode == "status" {
		if result.Error != "" || len(result.Stations) != 4 {
			return 1
		}
		for _, station := range result.Stations {
			if station.Error != "" {
				return 1
			}
		}
		return 0
	}
	if !result.ShutdownSucceeded {
		return 1
	}
	return 0
}

func verifiedVRResult(result StationResult) bool {
	return result.Mode == "vr" && result.WSLOff && result.RestartInhibited
}

// Package fleetmode implements the Arcade fleet's bounded Windows-side mode switch.
package fleetmode

import (
	"context"
	"time"
)

const (
	ShutdownDeadline = 180 * time.Second
	DispatchBudget   = 170 * time.Second
	AgentBudget      = 160 * time.Second
	RemoteAgentPath  = `C:\ProgramData\FleetMode\fleet-mode.exe`
)

// Station is ordinary connection configuration for an already enrolled host.
// It grants no credential, host trust, or effect authority.
type Station struct {
	Name             string `json:"name"`
	Host             string `json:"host"`
	User             string `json:"user"`
	Port             int    `json:"port"`
	ExpectedHostname string `json:"expectedHostname"`
	IdentityFile     string `json:"identityFile"`
	KnownHostsFile   string `json:"knownHostsFile"`
}

type Config struct {
	SchemaVersion int       `json:"schemaVersion"`
	Stations      []Station `json:"stations"`
	StatePath     string    `json:"statePath"`
}

type Stage struct {
	Name          string `json:"name"`
	ExitCode      int    `json:"exitCode"`
	ElapsedMillis int64  `json:"elapsedMillis"`
	Error         string `json:"error,omitempty"`
}

type StationResult struct {
	Name             string  `json:"name"`
	Hostname         string  `json:"hostname"`
	Mode             string  `json:"mode"`
	RestartInhibited bool    `json:"restartInhibited"`
	WSLOff           bool    `json:"wslOff"`
	ModeRecorded     bool    `json:"modeRecorded"`
	ElapsedMillis    int64   `json:"elapsedMillis"`
	Stages           []Stage `json:"stages"`
	Error            string  `json:"error,omitempty"`
}

type Result struct {
	Mode              string          `json:"mode"`
	StartedUTC        string          `json:"startedUtc"`
	ElapsedMillis     int64           `json:"elapsedMillis"`
	DeadlineMillis    int64           `json:"deadlineMillis"`
	ShutdownSucceeded bool            `json:"shutdownSucceeded"`
	ModeRecorded      bool            `json:"modeRecorded"`
	Stations          []StationResult `json:"stations"`
	Error             string          `json:"error,omitempty"`
}

type Transport interface {
	Call(context.Context, Station, string) (StationResult, error)
}

// State stores operator mode intent. It is not an authentication or admission system.
type State interface {
	Record(context.Context, string, []string) error
}

// Task names are resolved once during installation, not rediscovered while a
// customer is waiting. Service inhibition prevents other launchers restarting
// WSL without terminating unrelated PowerShell processes.
type AgentConfig struct {
	SchemaVersion    int      `json:"schemaVersion"`
	Name             string   `json:"name"`
	ExpectedHostname string   `json:"expectedHostname"`
	StatePath        string   `json:"statePath"`
	KeepAliveTasks   []string `json:"keepAliveTasks"`
}

type AgentOps interface {
	Hostname() (string, error)
	RecordMode(context.Context, string) error
	InhibitRestart(context.Context, AgentConfig) ([]Stage, bool)
	ObserveRestartInhibition(context.Context, AgentConfig) (bool, error)
	ForceWSLOff(context.Context) ([]Stage, bool)
	ObserveOff(context.Context) (bool, error)
}

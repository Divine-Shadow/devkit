package productadapter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const SupervisorCapabilitySchema = "devkit/product-proxy-supervisor-capability/v1"
const SupervisorCapabilityPath = "/run/devkit/product-adapter-supervisor.json"
const SandboxProxySocketPath = "/agent-state/product/product-egress.sock"

// SupervisorCapability is projected by bubblewrap from a one-shot inherited
// pipe into the sandbox at SupervisorCapabilityPath. It is a private
// invocation transport, not a same-UID security boundary. The subordinate has
// no caller-selectable target or socket surface and validates every package
// artifact that can be fixed independently of the requested exec argv.
type SupervisorCapability struct {
	SchemaVersion        string   `json:"schema_version"`
	Operation            string   `json:"operation"`
	AdapterExecutable    string   `json:"adapter_executable"`
	BubblewrapExecutable string   `json:"bubblewrap_executable"`
	ManifestDigest       string   `json:"manifest_digest"`
	Count                int      `json:"count"`
	Index                int      `json:"index"`
	ProxySocketPath      string   `json:"proxy_socket_path"`
	RuntimeArgs          []string `json:"runtime_args"`
}

// ValidateSupervisorCapability binds the one-shot sandbox transport to this
// exact composed package. Product authority remains the root-owned manifest
// consumed by product-adapter; this transport cannot introduce another target,
// socket, launcher, or package identity.
func ValidateSupervisorCapability(capability SupervisorCapability) error {
	if !Enabled() {
		return fmt.Errorf("uncomposed Product proxy has no supervisor authority")
	}
	if capability.SchemaVersion != SupervisorCapabilitySchema ||
		(capability.Operation != string(CommandPrepare) &&
			capability.Operation != string(CommandExec)) ||
		capability.Count < 1 ||
		capability.Index < 1 ||
		capability.Index > capability.Count {
		return fmt.Errorf("adapter capability is incomplete")
	}
	if capability.AdapterExecutable != packageAdapterExecutable ||
		capability.BubblewrapExecutable != packageBubblewrapExecutable {
		return fmt.Errorf("adapter capability does not match the composed package")
	}
	running, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve Product proxy executable: %w", err)
	}
	running, err = filepath.EvalSymlinks(running)
	if err != nil {
		return fmt.Errorf("canonicalize Product proxy executable: %w", err)
	}
	expected, err := filepath.EvalSymlinks(packageProductProxyExecutable)
	if err != nil {
		return fmt.Errorf("canonicalize composed Product proxy executable: %w", err)
	}
	if running != expected {
		return fmt.Errorf("running Product proxy does not match the composed package")
	}
	if len(capability.ManifestDigest) != 64 ||
		strings.ToLower(capability.ManifestDigest) != capability.ManifestDigest {
		return fmt.Errorf("adapter capability manifest digest is invalid")
	}
	for _, value := range capability.ManifestDigest {
		if (value < '0' || value > '9') && (value < 'a' || value > 'f') {
			return fmt.Errorf("adapter capability manifest digest is invalid")
		}
	}
	if capability.ProxySocketPath != SandboxProxySocketPath {
		return fmt.Errorf("adapter capability proxy socket is not package-fixed")
	}
	if len(capability.RuntimeArgs) < 2 ||
		capability.RuntimeArgs[0] != packageRuntimeLauncher {
		return fmt.Errorf("adapter capability runtime launcher is not package-fixed")
	}
	for _, argument := range capability.RuntimeArgs {
		if strings.IndexByte(argument, 0) >= 0 {
			return fmt.Errorf("adapter capability runtime argv is invalid")
		}
	}
	if capability.Operation == string(CommandPrepare) {
		expectedArgs := []string{
			packageRuntimeLauncher,
			packageReadinessExecutable,
			"probe",
			"--codex", packageCodexExecutable,
			"--mcp-requirement", packageMCPRequirement,
			"--mcp-requirement-digest", capability.RuntimeArgs[len(capability.RuntimeArgs)-1],
		}
		if len(capability.RuntimeArgs) != 9 || len(capability.RuntimeArgs[len(capability.RuntimeArgs)-1]) != 64 {
			return fmt.Errorf("Product prepare capability runtime argv is not package-fixed")
		}
		if len(capability.RuntimeArgs) != len(expectedArgs) {
			return fmt.Errorf("Product prepare capability runtime argv is not package-fixed")
		}
		for index := range expectedArgs {
			if capability.RuntimeArgs[index] != expectedArgs[index] {
				return fmt.Errorf("Product prepare capability runtime argv is not package-fixed")
			}
		}
	}
	return nil
}

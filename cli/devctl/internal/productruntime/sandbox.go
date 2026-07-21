package productruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"devkit/cli/devctl/internal/productadapter"
)

func runSandboxDiagnostic(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
	command productadapter.Command,
) ([]productadapter.ReadinessEvidence, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runSandboxProcess(
		authority,
		consumer,
		command,
		[]string{
			authority.Adapter.ReadinessExecutablePath,
			"probe",
			"--codex", authority.Adapter.CodexExecutablePath,
			"--mcp-requirement", authority.Adapter.MCPRequirementPath,
			"--mcp-requirement-digest", authority.Adapter.ArtifactDigests["mcp_requirement"],
		},
		&stdout,
		&stderr,
	)
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		return []productadapter.ReadinessEvidence{{
			Name: "codex-app-server-stdio-thread-mcp-readiness", Status: "blocked",
			Detail: detail,
		}}, fmt.Errorf("Product typed readiness probe: %w: %s", err, detail)
	}
	result, err := decodeProductReadinessResult(authority, stdout.Bytes())
	if err != nil {
		return nil, err
	}
	return []productadapter.ReadinessEvidence{{
		Name:   "codex-app-server-stdio-thread-mcp-readiness",
		Status: "ready",
		Result: &result,
	}}, nil
}

func decodeProductReadinessResult(
	authority productadapter.Authority,
	payload []byte,
) (productadapter.ProductReadinessResult, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var result productadapter.ProductReadinessResult
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("decode Product typed readiness result: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return result, fmt.Errorf("Product typed readiness emitted trailing output")
	}
	if err := validateProductReadinessResult(authority, result); err != nil {
		return result, err
	}
	return result, nil
}

func validateProductReadinessResult(
	authority productadapter.Authority,
	result productadapter.ProductReadinessResult,
) error {
	requirement, requirementInventoryDigest, err := productadapter.LoadMCPRequirement(
		authority.Adapter.MCPRequirementPath,
		authority.Adapter.ArtifactDigests["mcp_requirement"],
	)
	if err != nil {
		return err
	}
	requirementServers, requirementTools := requirement.Counts()
	if result.SchemaVersion != productadapter.ReadinessSchema ||
		!result.AppServerAlive ||
		!result.InitializeReady ||
		!result.MCPStatusRead ||
		result.MCPRequirementPath != authority.Adapter.MCPRequirementPath ||
		result.MCPRequirementDigest != authority.Adapter.ArtifactDigests["mcp_requirement"] ||
		result.MCPInventoryDigest != requirementInventoryDigest ||
		result.MCPServerCount != requirementServers ||
		result.MCPToolCount != requirementTools ||
		!result.EphemeralThreadCreated ||
		!result.EphemeralThreadReadBack ||
		result.ApprovalPolicy != "never" ||
		result.SandboxPolicy != "dangerFullAccess" {
		return fmt.Errorf("Product typed readiness result is incomplete or mismatched")
	}
	return nil
}

func runSandboxCommand(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
	command productadapter.Command,
	child []string,
) error {
	return runSandboxProcess(authority, consumer, command, child, os.Stdout, os.Stderr)
}

func runSandboxProcess(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
	command productadapter.Command,
	child []string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	if len(child) == 0 {
		return fmt.Errorf("Product sandbox requires an exact child argv")
	}
	parentFD, childFD, err := supervisorCapabilityPipe()
	if err != nil {
		return err
	}
	defer parentFD.Close()
	defer childFD.Close()
	args, err := buildBubblewrapArgs(authority, consumer, child)
	if err != nil {
		return err
	}
	process := exec.Command(authority.Adapter.BubblewrapPath, args...)
	process.Dir = consumer.CandidateRoot
	process.Stdin = os.Stdin
	process.Stdout = stdout
	process.Stderr = stderr
	process.ExtraFiles = []*os.File{childFD}
	process.Env = []string{}
	if err := process.Start(); err != nil {
		return err
	}
	_ = childFD.Close()
	capability := productadapter.SupervisorCapability{
		SchemaVersion:        productadapter.SupervisorCapabilitySchema,
		Operation:            string(command.Kind),
		AdapterExecutable:    authority.Adapter.ExecutablePath,
		BubblewrapExecutable: authority.Adapter.BubblewrapPath,
		ManifestDigest:       authority.ManifestDigest,
		Count:                command.Count,
		Index:                command.Index,
		ProxySocketPath:      consumer.SandboxProxySocketPath,
		RuntimeArgs:          append([]string{authority.Adapter.RuntimeLauncherPath}, child...),
	}
	encodeErr := json.NewEncoder(parentFD).Encode(capability)
	closeErr := parentFD.Close()
	waitErr := process.Wait()
	return errors.Join(encodeErr, closeErr, waitErr)
}

func buildBubblewrapArgs(
	authority productadapter.Authority,
	consumer productadapter.ConsumerManifest,
	child []string,
) ([]string, error) {
	args := []string{
		"--die-with-parent",
		"--clearenv",
		"--unshare-net",
		"--unshare-uts",
		"--unshare-ipc",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
	}
	directories := map[string]bool{"/": true, "/tmp": true}
	var directoryArgs []string
	addDirectory := func(path string) {}
	addDirectory = func(path string) {
		path = filepath.Clean(path)
		if path == "/" || path == "." || directories[path] {
			return
		}
		addDirectory(filepath.Dir(path))
		directories[path] = true
		directoryArgs = append(directoryArgs, "--dir", path)
	}
	var bindArgs []string
	for _, bind := range consumer.Binds {
		if err := validateBindSource(bind); err != nil {
			return nil, err
		}
		addDirectory(filepath.Dir(bind.Target))
		switch bind.Mode {
		case "ro":
			bindArgs = append(bindArgs, "--ro-bind", bind.Source, bind.Target)
		case "rw":
			bindArgs = append(bindArgs, "--bind", bind.Source, bind.Target)
		default:
			return nil, fmt.Errorf("Product bind mode %q is invalid", bind.Mode)
		}
	}
	addDirectory(filepath.Dir(productadapter.SupervisorCapabilityPath))
	args = append(args, directoryArgs...)
	args = append(args, bindArgs...)
	args = append(
		args,
		"--ro-bind",
		authority.Adapter.CodexConfigPath,
		filepath.Join(consumer.SandboxHomePath, ".codex", "config.toml"),
	)
	args = append(
		args,
		"--perms", "0600",
		"--file", "3", productadapter.SupervisorCapabilityPath,
	)
	environment := map[string]string{
		"CODEX_HOME":                           filepath.Join(consumer.SandboxHomePath, ".codex"),
		"CODEX_ROLLOUT_DIR":                    filepath.Join(consumer.SandboxHomePath, ".codex", "rollouts"),
		"DEVKIT_NATIVE_AGENT":                  strconv.Itoa(consumer.Index),
		"DEVKIT_NATIVE_MOUNT_POLICY_IDENTITY":  authority.Adapter.MountPolicyIdentity,
		"DEVKIT_NATIVE_WINDOWS_MOUNTS_VISIBLE": "false",
		"DOCKER_HOST":                          "unix://" + consumer.SandboxBrokerSocketPath,
		"HOME":                                 consumer.SandboxHomePath,
		"HTTP_PROXY":                           "http://127.0.0.1:18888",
		"HTTPS_PROXY":                          "http://127.0.0.1:18888",
		"NO_PROXY":                             "localhost,127.0.0.1,::1",
		"http_proxy":                           "http://127.0.0.1:18888",
		"https_proxy":                          "http://127.0.0.1:18888",
		"no_proxy":                             "localhost,127.0.0.1,::1",
	}
	for key, value := range authority.Adapter.RuntimeEnvironment {
		environment[key] = value
	}
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--setenv", key, environment[key])
	}
	args = append(
		args,
		"--chdir", consumer.SandboxWorktreePath,
		authority.Adapter.ProxyHelperPath,
		"supervise",
	)
	_ = child
	return args, nil
}

func validateBindSource(bind productadapter.BindManifest) error {
	if !filepath.IsAbs(bind.Source) || filepath.Clean(bind.Source) != bind.Source ||
		!filepath.IsAbs(bind.Target) || filepath.Clean(bind.Target) != bind.Target {
		return fmt.Errorf("Product bind tuple is not exact")
	}
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(bind.Source, current), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if !bind.Required && errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Product bind source traverses symlink %s", current)
		}
	}
	return nil
}

func supervisorCapabilityPipe() (*os.File, *os.File, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	return writer, reader, nil
}

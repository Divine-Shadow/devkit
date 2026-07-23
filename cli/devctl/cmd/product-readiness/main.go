package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"devkit/cli/devctl/internal/productadapter"
)

const readinessSchema = "devkit/product-codex-app-server-stdio-thread-mcp-readiness/v1"

type request struct {
	ID     int    `json:"id,omitempty"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type response struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type mcpStatusPage struct {
	Data       []mcpServerStatus `json:"data"`
	NextCursor *string           `json:"nextCursor"`
}

type mcpServerStatus struct {
	AuthStatus        string                   `json:"authStatus"`
	Name              string                   `json:"name"`
	ResourceTemplates []json.RawMessage        `json:"resourceTemplates"`
	Resources         []json.RawMessage        `json:"resources"`
	ServerInfo        *mcpServerInfo           `json:"serverInfo"`
	Tools             map[string]mcpToolStatus `json:"tools"`
}

type mcpServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type mcpToolStatus struct {
	Name        string          `json:"name"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type readinessResult struct {
	SchemaVersion           string `json:"schema_version"`
	AppServerAlive          bool   `json:"app_server_alive"`
	InitializeReady         bool   `json:"initialize_ready"`
	MCPStatusRead           bool   `json:"mcp_status_read"`
	MCPRequirementPath      string `json:"mcp_requirement_path"`
	MCPRequirementDigest    string `json:"mcp_requirement_digest"`
	MCPInventoryDigest      string `json:"mcp_inventory_digest"`
	MCPServerCount          int    `json:"mcp_server_count"`
	MCPToolCount            int    `json:"mcp_tool_count"`
	EphemeralThreadCreated  bool   `json:"ephemeral_thread_created"`
	EphemeralThreadReadBack bool   `json:"ephemeral_thread_read_back"`
	ApprovalPolicy          string `json:"approval_policy"`
	SandboxPolicy           string `json:"sandbox_policy"`
}

func main() {
	if len(os.Args) != 10 ||
		os.Args[1] != "probe" ||
		os.Args[2] != "--codex" ||
		os.Args[4] != "--mcp-requirement" ||
		os.Args[6] != "--mcp-requirement-digest" ||
		os.Args[8] != "--consumer-index" {
		fail("readiness probe accepts only: probe --codex PATH --mcp-requirement PATH --mcp-requirement-digest SHA256 --consumer-index N")
	}
	consumerIndex, err := strconv.Atoi(os.Args[9])
	if err != nil || consumerIndex < 1 || consumerIndex > 2 {
		fail("readiness probe consumer index must be 1 or 2")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	result, err := probe(ctx, os.Args[3], os.Args[5], os.Args[7], consumerIndex)
	if err != nil {
		fail("%v", err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fail("encode readiness result: %v", err)
	}
}

func probe(
	ctx context.Context,
	codexPath, requirementPath, requirementDigest string,
	consumerIndex int,
) (result readinessResult, retErr error) {
	if consumerIndex < 1 || consumerIndex > 2 {
		return result, fmt.Errorf("Product readiness consumer index must be 1 or 2")
	}
	requirement, requirementInventoryDigest, err := productadapter.LoadMCPRequirement(
		requirementPath,
		requirementDigest,
	)
	if err != nil {
		return result, err
	}
	requirementServers, requirementTools := requirement.Counts()
	workingDirectory, err := os.Getwd()
	if err != nil {
		return result, err
	}
	command := exec.Command(codexPath, "app-server", "--stdio")
	command.Env = os.Environ()
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := command.StdinPipe()
	if err != nil {
		return result, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return result, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return result, err
	}
	if err := command.Start(); err != nil {
		return result, err
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	var stderrMu sync.Mutex
	var stderrBytes []byte
	go func() {
		data, _ := io.ReadAll(io.LimitReader(stderr, 1<<20))
		stderrMu.Lock()
		stderrBytes = data
		stderrMu.Unlock()
	}()
	defer func() {
		stopErr := stopProcessGroup(command.Process.Pid, wait)
		if retErr == nil && stopErr != nil {
			retErr = stopErr
		}
	}()

	responses := make(chan response, 16)
	scanErrors := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 4<<20)
		for scanner.Scan() {
			var message response
			if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
				scanErrors <- fmt.Errorf("decode app-server response: %w", err)
				return
			}
			if message.ID != 0 {
				responses <- message
			}
		}
		scanErrors <- scanner.Err()
	}()
	send := func(value request) error {
		return json.NewEncoder(stdin).Encode(value)
	}
	await := func(id int) (json.RawMessage, error) {
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case err := <-wait:
				stderrMu.Lock()
				detail := string(stderrBytes)
				stderrMu.Unlock()
				return nil, fmt.Errorf("app-server exited before response %d: %v: %s", id, err, detail)
			case err := <-scanErrors:
				return nil, fmt.Errorf("app-server response stream ended before %d: %v", id, err)
			case <-timer.C:
				return nil, fmt.Errorf("app-server response %d timed out", id)
			case message := <-responses:
				if message.ID != id {
					return nil, fmt.Errorf("app-server returned unexpected response id %d before %d", message.ID, id)
				}
				if len(message.Error) != 0 && string(message.Error) != "null" {
					return nil, fmt.Errorf("app-server request %d failed: %s", id, message.Error)
				}
				return message.Result, nil
			}
		}
	}

	if err := send(request{ID: 1, Method: "initialize", Params: map[string]any{
		"clientInfo": map[string]string{"name": "devkit-product-readiness", "version": "1"},
	}}); err != nil {
		return result, err
	}
	initialize, err := await(1)
	if err != nil {
		return result, err
	}
	var initializeResult struct {
		CodexHome string `json:"codexHome"`
	}
	if err := json.Unmarshal(initialize, &initializeResult); err != nil || initializeResult.CodexHome != os.Getenv("CODEX_HOME") {
		return result, fmt.Errorf("app-server initialize selected an unexpected CODEX_HOME")
	}
	if err := send(request{Method: "initialized", Params: map[string]any{}}); err != nil {
		return result, err
	}
	if err := send(request{ID: 2, Method: "thread/start", Params: map[string]any{
		"cwd": workingDirectory, "approvalPolicy": "never",
		"sandbox": "danger-full-access", "ephemeral": true,
	}}); err != nil {
		return result, err
	}
	threadPayload, err := await(2)
	if err != nil {
		return result, err
	}
	var started struct {
		Cwd            string `json:"cwd"`
		ApprovalPolicy string `json:"approvalPolicy"`
		Sandbox        struct {
			Type string `json:"type"`
		} `json:"sandbox"`
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(threadPayload, &started); err != nil ||
		started.Cwd != workingDirectory ||
		started.ApprovalPolicy != "never" ||
		started.Sandbox.Type != "dangerFullAccess" ||
		started.Thread.ID == "" {
		return result, fmt.Errorf("thread/start did not preserve the exact ephemeral thread identity")
	}
	if err := send(request{ID: 3, Method: "thread/read", Params: map[string]any{
		"threadId": started.Thread.ID, "includeTurns": false,
	}}); err != nil {
		return result, err
	}
	threadReadPayload, err := await(3)
	if err != nil {
		return result, err
	}
	var threadRead struct {
		Thread struct {
			ID  string `json:"id"`
			Cwd string `json:"cwd"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(threadReadPayload, &threadRead); err != nil ||
		threadRead.Thread.ID != started.Thread.ID ||
		threadRead.Thread.Cwd != workingDirectory {
		return result, fmt.Errorf("thread/read did not return the exact ephemeral thread")
	}
	observedServers := make(map[string][]string)
	seenCursors := make(map[string]bool)
	nextCursor := ""
	nextRequestID := 4
	for pageIndex := 0; ; pageIndex++ {
		if pageIndex >= 16 {
			return result, fmt.Errorf("app-server MCP status exceeded 16 bounded pages")
		}
		params := map[string]any{
			"threadId": started.Thread.ID,
			"detail":   "toolsAndAuthOnly",
			"limit":    128,
		}
		if nextCursor != "" {
			params["cursor"] = nextCursor
		}
		requestID := nextRequestID
		nextRequestID++
		if err := send(request{ID: requestID, Method: "mcpServerStatus/list", Params: params}); err != nil {
			return result, err
		}
		mcpPayload, err := await(requestID)
		if err != nil {
			return result, err
		}
		var page mcpStatusPage
		if err := json.Unmarshal(mcpPayload, &page); err != nil || page.Data == nil {
			return result, fmt.Errorf("app-server MCP status was not typed")
		}
		for _, server := range page.Data {
			if _, exists := observedServers[server.Name]; exists {
				return result, fmt.Errorf("app-server MCP status repeated server %q", server.Name)
			}
			if server.Name == "" || server.ServerInfo == nil ||
				server.ServerInfo.Name == "" || server.ServerInfo.Version == "" ||
				server.ResourceTemplates == nil || server.Resources == nil || server.Tools == nil {
				return result, fmt.Errorf("app-server MCP server %q is uninitialized", server.Name)
			}
			switch server.AuthStatus {
			case "unsupported", "notLoggedIn", "bearerToken", "oAuth":
			default:
				return result, fmt.Errorf("app-server MCP server %q has invalid auth status", server.Name)
			}
			tools := make([]string, 0, len(server.Tools))
			for name, tool := range server.Tools {
				if name == "" || tool.Name != name || len(tool.InputSchema) == 0 || string(tool.InputSchema) == "null" {
					return result, fmt.Errorf("app-server MCP server %q has an invalid typed tool", server.Name)
				}
				tools = append(tools, name)
			}
			observedServers[server.Name] = tools
		}
		if page.NextCursor == nil {
			break
		}
		next := *page.NextCursor
		if next == "" || seenCursors[next] {
			return result, fmt.Errorf("app-server MCP status returned an invalid pagination cursor")
		}
		seenCursors[next] = true
		nextCursor = next
	}
	observedRequirement, err := productadapter.NewObservedMCPRequirement(observedServers)
	if err != nil {
		return result, fmt.Errorf("app-server MCP inventory is invalid: %w", err)
	}
	observedInventoryDigest, err := observedRequirement.SemanticDigest()
	if err != nil {
		return result, err
	}
	if observedInventoryDigest != requirementInventoryDigest {
		return result, fmt.Errorf("app-server MCP inventory does not exactly match the immutable requirement")
	}
	return readinessResult{
		SchemaVersion:           readinessSchema,
		AppServerAlive:          true,
		InitializeReady:         true,
		MCPStatusRead:           true,
		MCPRequirementPath:      requirementPath,
		MCPRequirementDigest:    requirementDigest,
		MCPInventoryDigest:      observedInventoryDigest,
		MCPServerCount:          requirementServers,
		MCPToolCount:            requirementTools,
		EphemeralThreadCreated:  true,
		EphemeralThreadReadBack: true,
		ApprovalPolicy:          "never",
		SandboxPolicy:           "dangerFullAccess",
	}, nil
}

func stopProcessGroup(pid int, wait <-chan error) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("terminate app-server process group: %w", err)
	}
	var waitErr error
	parentExited := false
	select {
	case err := <-wait:
		waitErr = err
		parentExited = true
	case <-time.After(3 * time.Second):
	}
	// The group leader may exit before a descendant handles SIGTERM. Always
	// sweep the process group after waiting for the leader so a successful
	// command.Wait cannot be mistaken for complete lifecycle cleanup.
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	if !parentExited {
		select {
		case waitErr = <-wait:
			parentExited = true
		case <-time.After(3 * time.Second):
			return fmt.Errorf("app-server process group leader did not terminate")
		}
	}
	if waitErr == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return nil
		}
	}
	return waitErr
}

func fail(format string, values ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "product-readiness: "+format+"\n", values...)
	os.Exit(2)
}

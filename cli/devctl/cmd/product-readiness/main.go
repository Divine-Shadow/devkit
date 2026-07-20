package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const readinessSchema = "devkit/product-codex-app-server-thread-readiness/v1"

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

type readinessResult struct {
	SchemaVersion             string `json:"schema_version"`
	AppServerAlive            bool   `json:"app_server_alive"`
	InitializeReady           bool   `json:"initialize_ready"`
	MCPStatusRead             bool   `json:"mcp_status_read"`
	EphemeralThreadCreated    bool   `json:"ephemeral_thread_created"`
	EphemeralThreadReadBack   bool   `json:"ephemeral_thread_read_back"`
	ApprovalPolicy            string `json:"approval_policy"`
	SandboxPolicy             string `json:"sandbox_policy"`
	StandaloneExecutable      string `json:"standalone_executable"`
	StandaloneExecutableExit  int    `json:"standalone_executable_exit"`
	StandaloneExecutableProof string `json:"standalone_executable_stdout_sha256"`
}

func main() {
	if len(os.Args) != 6 ||
		os.Args[1] != "probe" ||
		os.Args[2] != "--codex" ||
		os.Args[4] != "--first-executable" {
		fail("readiness probe accepts only: probe --codex PATH --first-executable PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	result, err := probe(ctx, os.Args[3], os.Args[5])
	if err != nil {
		fail("%v", err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fail("encode readiness result: %v", err)
	}
}

func probe(ctx context.Context, codexPath, executablePath string) (result readinessResult, retErr error) {
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
	if err := send(request{ID: 4, Method: "mcpServerStatus/list", Params: map[string]any{
		"threadId": started.Thread.ID, "detail": "toolsAndAuthOnly",
	}}); err != nil {
		return result, err
	}
	mcpPayload, err := await(4)
	if err != nil {
		return result, err
	}
	var mcpStatus struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(mcpPayload, &mcpStatus); err != nil || mcpStatus.Data == nil {
		return result, fmt.Errorf("app-server MCP status was not typed")
	}
	const marker = "product-app-server-standalone-command-boundary"
	if err := send(request{ID: 5, Method: "command/exec", Params: map[string]any{
		"command": []string{executablePath, marker},
		"cwd":     workingDirectory,
		"sandboxPolicy": map[string]string{
			"type": "dangerFullAccess",
		},
		"timeoutMs": 5000,
	}}); err != nil {
		return result, err
	}
	execPayload, err := await(5)
	if err != nil {
		return result, err
	}
	var executed struct {
		ExitCode int    `json:"exitCode"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
	}
	if err := json.Unmarshal(execPayload, &executed); err != nil ||
		executed.ExitCode != 0 || executed.Stdout != marker || executed.Stderr != "" {
		return result, fmt.Errorf("app-server command/exec did not cross the first executable boundary")
	}
	sum := sha256.Sum256([]byte(executed.Stdout))
	return readinessResult{
		SchemaVersion:             readinessSchema,
		AppServerAlive:            true,
		InitializeReady:           true,
		MCPStatusRead:             true,
		EphemeralThreadCreated:    true,
		EphemeralThreadReadBack:   true,
		ApprovalPolicy:            "never",
		SandboxPolicy:             "dangerFullAccess",
		StandaloneExecutable:      executablePath,
		StandaloneExecutableExit:  0,
		StandaloneExecutableProof: hex.EncodeToString(sum[:]),
	}, nil
}

func stopProcessGroup(pid int, wait <-chan error) error {
	if pid <= 0 {
		return nil
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case err := <-wait:
		if err == nil {
			return nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				return nil
			}
		}
		return err
	case <-time.After(3 * time.Second):
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	select {
	case <-wait:
		return nil
	case <-time.After(3 * time.Second):
		return fmt.Errorf("app-server process group did not terminate")
	}
}

func fail(format string, values ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "product-readiness: "+format+"\n", values...)
	os.Exit(2)
}

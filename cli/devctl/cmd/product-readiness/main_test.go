package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

const readinessHelperMode = "DEVKIT_PRODUCT_READINESS_HELPER"

func TestMain(m *testing.M) {
	switch os.Getenv(readinessHelperMode) {
	case "success":
		runReadinessSuccessHelper()
		os.Exit(0)
	case "hang":
		child := exec.Command(os.Args[0])
		child.Env = replaceEnvironment(os.Environ(), readinessHelperMode, "descendant")
		if err := child.Start(); err != nil {
			os.Exit(93)
		}
		if err := os.WriteFile(os.Getenv("DEVKIT_PRODUCT_READINESS_CHILD_PID"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			os.Exit(94)
		}
		for {
			time.Sleep(time.Hour)
		}
	case "descendant":
		for {
			time.Sleep(time.Hour)
		}
	}
	os.Exit(m.Run())
}

func TestProbeUsesTypedThreadReadAndStandaloneCommandExec(t *testing.T) {
	t.Setenv(readinessHelperMode, "success")
	t.Setenv("CODEX_HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := probe(ctx, os.Args[0], "/nix/store/fixture-first-executable")
	if err != nil {
		t.Fatal(err)
	}
	if !result.AppServerAlive ||
		!result.InitializeReady ||
		!result.MCPStatusRead ||
		!result.EphemeralThreadCreated ||
		!result.EphemeralThreadReadBack ||
		result.StandaloneExecutableExit != 0 {
		t.Fatalf("incomplete readiness result: %#v", result)
	}
}

func TestProbeDeadlineTerminatesAppServerProcessGroup(t *testing.T) {
	pidFile := t.TempDir() + "/descendant.pid"
	t.Setenv(readinessHelperMode, "hang")
	t.Setenv("DEVKIT_PRODUCT_READINESS_CHILD_PID", pidFile)
	t.Setenv("CODEX_HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := probe(ctx, os.Args[0], "/nix/store/fixture-first-executable"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hung app-server error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed < 200*time.Millisecond || elapsed > 4*time.Second {
		t.Fatalf("bounded app-server timeout elapsed = %s", elapsed)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read helper descendant pid: %v", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for processIsAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processIsAlive(pid) {
		t.Fatalf("app-server descendant %d survived bounded cleanup", pid)
	}
}

func replaceEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

func processIsAlive(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		return true
	}
	fields := strings.Fields(string(data))
	return len(fields) < 3 || fields[2] != "Z"
}

func runReadinessSuccessHelper() {
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	threadID := "019f-readiness-ephemeral"
	for scanner.Scan() {
		var value struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &value) != nil || value.ID == 0 {
			continue
		}
		var result any
		switch value.Method {
		case "initialize":
			result = map[string]any{"codexHome": os.Getenv("CODEX_HOME")}
		case "thread/start":
			var params struct {
				Cwd string `json:"cwd"`
			}
			_ = json.Unmarshal(value.Params, &params)
			result = map[string]any{
				"cwd": params.Cwd, "approvalPolicy": "never",
				"sandbox": map[string]string{"type": "dangerFullAccess"},
				"thread":  map[string]string{"id": threadID},
			}
		case "thread/read":
			var params struct {
				ThreadID string `json:"threadId"`
			}
			_ = json.Unmarshal(value.Params, &params)
			if params.ThreadID != threadID {
				os.Exit(95)
			}
			cwd, _ := os.Getwd()
			result = map[string]any{"thread": map[string]string{"id": threadID, "cwd": cwd}}
		case "mcpServerStatus/list":
			result = map[string]any{"data": []any{}}
		case "command/exec":
			var params struct {
				Command   []string `json:"command"`
				TimeoutMS int      `json:"timeoutMs"`
			}
			_ = json.Unmarshal(value.Params, &params)
			if len(params.Command) != 2 ||
				params.Command[1] != "product-app-server-standalone-command-boundary" ||
				params.TimeoutMS != 5000 {
				os.Exit(96)
			}
			result = map[string]any{
				"exitCode": 0,
				"stdout":   "product-app-server-standalone-command-boundary",
				"stderr":   "",
			}
		default:
			os.Exit(97)
		}
		_ = encoder.Encode(map[string]any{"id": value.ID, "result": result})
	}
}

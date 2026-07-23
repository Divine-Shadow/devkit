package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const readinessHelperMode = "DEVKIT_PRODUCT_READINESS_HELPER"
const readinessMCPCase = "DEVKIT_PRODUCT_READINESS_MCP_CASE"

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

func TestProbeUsesTypedThreadReadAndProductDerivedMCPRequirement(t *testing.T) {
	t.Setenv(readinessHelperMode, "success")
	t.Setenv("CODEX_HOME", t.TempDir())
	requirementPath, requirementDigest := writeReadinessRequirement(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := probe(
		ctx,
		os.Args[0],
		requirementPath,
		requirementDigest,
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.AppServerAlive ||
		!result.InitializeReady ||
		!result.MCPStatusRead ||
		result.MCPRequirementPath != requirementPath ||
		result.MCPRequirementDigest != requirementDigest ||
		len(result.MCPInventoryDigest) != 64 ||
		result.MCPServerCount != 1 ||
		result.MCPToolCount != 2 ||
		!result.EphemeralThreadCreated ||
		!result.EphemeralThreadReadBack {
		t.Fatalf("incomplete readiness result: %#v", result)
	}
}

func TestProbeDeadlineTerminatesAppServerProcessGroup(t *testing.T) {
	pidFile := t.TempDir() + "/descendant.pid"
	t.Setenv(readinessHelperMode, "hang")
	t.Setenv("DEVKIT_PRODUCT_READINESS_CHILD_PID", pidFile)
	t.Setenv("CODEX_HOME", t.TempDir())
	requirementPath, requirementDigest := writeReadinessRequirement(t)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := probe(
		ctx,
		os.Args[0],
		requirementPath,
		requirementDigest,
		1,
	); !errors.Is(err, context.DeadlineExceeded) {
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

func TestProbeRejectsMissingUninitializedAndWrongMCPInventory(t *testing.T) {
	for _, test := range []struct {
		name string
		mode string
		want string
	}{
		{name: "empty", mode: "empty", want: "inventory is invalid"},
		{name: "missing", mode: "missing", want: "does not exactly match"},
		{name: "uninitialized", mode: "uninitialized", want: "is uninitialized"},
		{name: "wrong tool", mode: "wrong-tool", want: "does not exactly match"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(readinessHelperMode, "success")
			t.Setenv(readinessMCPCase, test.mode)
			t.Setenv("CODEX_HOME", t.TempDir())
			requirementPath, requirementDigest := writeReadinessRequirement(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := probe(
				ctx,
				os.Args[0],
				requirementPath,
				requirementDigest,
				1,
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("probe error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestProbeConsumesEveryTypedMCPStatusPage(t *testing.T) {
	t.Setenv(readinessHelperMode, "success")
	t.Setenv(readinessMCPCase, "paginated")
	t.Setenv("CODEX_HOME", t.TempDir())
	payload := []byte(`{"schemaVersion":"devkit/product-mcp-requirement/v1","servers":[{"name":"governance","tools":["get_run_status","run"]},{"name":"submit","tools":["submit"]}]}`)
	path := filepath.Join(t.TempDir(), "mcp-requirement.json")
	if err := os.WriteFile(path, payload, 0o444); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	result, err := probe(
		context.Background(),
		os.Args[0],
		path,
		hex.EncodeToString(sum[:]),
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.MCPServerCount != 2 || result.MCPToolCount != 3 {
		t.Fatalf("paginated counts = %d/%d", result.MCPServerCount, result.MCPToolCount)
	}
}

func TestProbeRejectsTruncatedAndDigestMismatchedRequirementBeforeAppServer(t *testing.T) {
	t.Setenv(readinessHelperMode, "success")
	t.Setenv("CODEX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "mcp-requirement.json")
	payload := []byte(`{"schemaVersion":"devkit/product-mcp-requirement/v1","servers":[`)
	if err := os.WriteFile(path, payload, 0o444); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	_, err := probe(
		context.Background(),
		os.Args[0],
		path,
		hex.EncodeToString(sum[:]),
		1,
	)
	if err == nil || !strings.Contains(err.Error(), "decode Product MCP requirement") {
		t.Fatalf("truncated requirement error = %v", err)
	}
	if _, err := probe(
		context.Background(),
		os.Args[0],
		path,
		strings.Repeat("0", 64),
		1,
	); err == nil || !strings.Contains(err.Error(), "digest does not match") {
		t.Fatalf("mismatched requirement digest error = %v", err)
	}
}

func writeReadinessRequirement(t *testing.T) (string, string) {
	t.Helper()
	payload := []byte(`{"schemaVersion":"devkit/product-mcp-requirement/v1","servers":[{"name":"governance","tools":["get_run_status","run"]}]}`)
	path := filepath.Join(t.TempDir(), "mcp-requirement.json")
	if err := os.WriteFile(path, payload, 0o444); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	return path, hex.EncodeToString(sum[:])
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
			var mcpParams struct {
				Cursor string `json:"cursor"`
			}
			_ = json.Unmarshal(value.Params, &mcpParams)
			tools := map[string]any{
				"get_run_status": map[string]any{
					"name": "get_run_status", "inputSchema": map[string]any{"type": "object"},
				},
				"run": map[string]any{
					"name": "run", "inputSchema": map[string]any{"type": "object"},
				},
			}
			serverName := "governance"
			serverInfo := any(map[string]any{"name": "governance-fixture", "version": "1"})
			var nextCursor any
			switch os.Getenv(readinessMCPCase) {
			case "empty":
				result = map[string]any{"data": []any{}}
				break
			case "missing":
				serverName = "not-governance"
			case "uninitialized":
				serverInfo = nil
			case "wrong-tool":
				delete(tools, "run")
				tools["not_run"] = map[string]any{
					"name": "not_run", "inputSchema": map[string]any{"type": "object"},
				}
			case "paginated":
				if mcpParams.Cursor == "" {
					nextCursor = "page-2"
				} else if mcpParams.Cursor == "page-2" {
					serverName = "submit"
					serverInfo = map[string]any{"name": "submit-fixture", "version": "1"}
					tools = map[string]any{
						"submit": map[string]any{
							"name": "submit", "inputSchema": map[string]any{"type": "object"},
						},
					}
				} else {
					os.Exit(98)
				}
			}
			if result == nil {
				result = map[string]any{"data": []any{map[string]any{
					"authStatus":        "unsupported",
					"name":              serverName,
					"resourceTemplates": []any{},
					"resources":         []any{},
					"serverInfo":        serverInfo,
					"tools":             tools,
				}}, "nextCursor": nextCursor}
			}
		default:
			os.Exit(97)
		}
		_ = encoder.Encode(map[string]any{"id": value.ID, "result": result})
	}
}

// product-mcp-fixture is built only by the Devkit lifecycle test. It gives the
// real pinned Codex app-server a deterministic initialized MCP inventory; it
// is not part of the production Product adapter closure or authority.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type fixtureRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "product-mcp-fixture:", err)
		os.Exit(2)
	}
}

func run(reader io.Reader, writer io.Writer) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	encoder := json.NewEncoder(writer)
	for scanner.Scan() {
		var request fixtureRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			return err
		}
		if len(request.ID) == 0 || string(request.ID) == "null" {
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			var params struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			if err := json.Unmarshal(request.Params, &params); err != nil {
				return err
			}
			if params.ProtocolVersion == "" {
				return fmt.Errorf("initialize omitted protocolVersion")
			}
			result = map[string]any{
				"protocolVersion": params.ProtocolVersion,
				"capabilities": map[string]any{
					"tools": map[string]bool{"listChanged": false},
				},
				"serverInfo": map[string]string{
					"name": "governance-fixture", "version": "1",
				},
			}
		case "tools/list":
			result = map[string]any{"tools": []any{
				map[string]any{
					"name":        "get_run_status",
					"inputSchema": map[string]any{"type": "object"},
				},
				map[string]any{
					"name":        "run",
					"inputSchema": map[string]any{"type": "object"},
				},
			}}
		case "ping":
			result = map[string]any{}
		default:
			return fmt.Errorf("unexpected method %q", request.Method)
		}
		if err := encoder.Encode(map[string]any{
			"jsonrpc": "2.0", "id": request.ID, "result": result,
		}); err != nil {
			return err
		}
	}
	return scanner.Err()
}

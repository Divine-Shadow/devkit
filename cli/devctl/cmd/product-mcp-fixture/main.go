// product-mcp-fixture is built only by the Devkit lifecycle test. It gives the
// real pinned Codex app-server a deterministic initialized MCP inventory; it
// is not part of the production Product adapter closure or authority.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const (
	toolCallSchema    = "devkit/product-mcp-tool-call-fixture/v1"
	toolCallObjective = "prove-mcp-tool-call-transport"
)

type fixtureRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type toolCallArguments struct {
	SchemaVersion string `json:"schema_version"`
	ConsumerIndex int    `json:"consumer_index"`
	Objective     string `json:"objective"`
}

type toolCallReceipt struct {
	SchemaVersion string `json:"schema_version"`
	Status        string `json:"status"`
	ConsumerIndex int    `json:"consumer_index"`
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
					"name": "devkit-lifecycle-fixture", "version": "1",
				},
			}
		case "tools/list":
			result = map[string]any{"tools": []any{
				map[string]any{
					"name": "lifecycle_probe",
					"inputSchema": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"required":             []string{"schema_version", "consumer_index", "objective"},
						"properties": map[string]any{
							"schema_version": map[string]any{"type": "string", "const": toolCallSchema},
							"consumer_index": map[string]any{"type": "integer", "minimum": 1, "maximum": 2},
							"objective":      map[string]any{"type": "string", "const": toolCallObjective},
						},
					},
				},
			}}
		case "tools/call":
			var params struct {
				Name      string            `json:"name"`
				Arguments toolCallArguments `json:"arguments"`
				Meta      json.RawMessage   `json:"_meta,omitempty"`
			}
			decoder := json.NewDecoder(bytes.NewReader(request.Params))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&params); err != nil {
				return fmt.Errorf("decode tools/call: %w", err)
			}
			var trailing any
			if err := decoder.Decode(&trailing); err != io.EOF {
				return fmt.Errorf("tools/call has trailing input")
			}
			if params.Name != "lifecycle_probe" ||
				params.Arguments.SchemaVersion != toolCallSchema ||
				params.Arguments.ConsumerIndex < 1 ||
				params.Arguments.ConsumerIndex > 2 ||
				params.Arguments.Objective != toolCallObjective {
				return fmt.Errorf("tools/call did not request the exact lifecycle probe fixture")
			}
			receipt := toolCallReceipt{
				SchemaVersion: toolCallSchema,
				Status:        "observed",
				ConsumerIndex: params.Arguments.ConsumerIndex,
			}
			receiptJSON, err := json.Marshal(receipt)
			if err != nil {
				return err
			}
			result = map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": string(receiptJSON)},
				},
				"structuredContent": receipt,
				"isError":           false,
			}
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

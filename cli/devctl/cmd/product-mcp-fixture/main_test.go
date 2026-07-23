package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestFixtureExposesDeterministicMCPToolCall(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"lifecycle_probe","arguments":{"schema_version":"devkit/product-mcp-tool-call-fixture/v1","consumer_index":2,"objective":"prove-mcp-tool-call-transport"}}}`,
		"",
	}, "\n")
	var output bytes.Buffer
	if err := run(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	var initialize, tools, call map[string]any
	if err := decoder.Decode(&initialize); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&tools); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&call); err != nil {
		t.Fatal(err)
	}
	toolResult := tools["result"].(map[string]any)["tools"].([]any)
	if len(toolResult) != 1 || toolResult[0].(map[string]any)["name"] != "lifecycle_probe" {
		t.Fatalf("tools response = %#v", tools)
	}
	callResult := call["result"].(map[string]any)
	if callResult["isError"] != false {
		t.Fatalf("call response = %#v", call)
	}
	receipt := callResult["structuredContent"].(map[string]any)
	if receipt["schema_version"] != toolCallSchema ||
		receipt["status"] != "observed" ||
		receipt["consumer_index"] != float64(2) ||
		len(receipt) != 3 {
		t.Fatalf("tool-call receipt = %#v", receipt)
	}
	content := callResult["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["type"] != "text" {
		t.Fatalf("fixture content = %#v", content)
	}
	var textReceipt map[string]any
	if err := json.Unmarshal([]byte(content[0].(map[string]any)["text"].(string)), &textReceipt); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mustJSON(t, textReceipt), mustJSON(t, receipt)) {
		t.Fatalf("text receipt %#v != structured receipt %#v", textReceipt, receipt)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestFixtureRejectsInexactMCPToolCall(t *testing.T) {
	for name, request := range map[string]string{
		"wrong tool":  `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"other","arguments":{"schema_version":"devkit/product-mcp-tool-call-fixture/v1","consumer_index":1,"objective":"prove-mcp-tool-call-transport"}}}`,
		"wrong index": `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lifecycle_probe","arguments":{"schema_version":"devkit/product-mcp-tool-call-fixture/v1","consumer_index":3,"objective":"prove-mcp-tool-call-transport"}}}`,
		"extra field": `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lifecycle_probe","arguments":{"schema_version":"devkit/product-mcp-tool-call-fixture/v1","consumer_index":1,"objective":"prove-mcp-tool-call-transport"},"command":"override"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := run(strings.NewReader(request+"\n"), &bytes.Buffer{}); err == nil {
				t.Fatal("inexact tool call was accepted")
			}
		})
	}
}

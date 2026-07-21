package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestFixtureExposesDeterministicGovernanceTools(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		"",
	}, "\n")
	var output bytes.Buffer
	if err := run(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	var initialize, tools map[string]any
	if err := decoder.Decode(&initialize); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&tools); err != nil {
		t.Fatal(err)
	}
	toolResult := tools["result"].(map[string]any)["tools"].([]any)
	if len(toolResult) != 2 || toolResult[0].(map[string]any)["name"] != "get_run_status" ||
		toolResult[1].(map[string]any)["name"] != "run" {
		t.Fatalf("tools response = %#v", tools)
	}
}

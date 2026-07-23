package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestWebSocketHandshakeAuthenticatesUpgrade(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	serverDone := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(server)
		requestLine, err := reader.ReadString('\n')
		if err != nil {
			serverDone <- err
			return
		}
		if requestLine != "GET /rpc HTTP/1.1\r\n" {
			serverDone <- fmt.Errorf("unexpected request line %q", requestLine)
			return
		}
		key := ""
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				serverDone <- err
				return
			}
			if line == "\r\n" {
				break
			}
			name, value, found := strings.Cut(strings.TrimSuffix(line, "\r\n"), ":")
			if found && strings.EqualFold(strings.TrimSpace(name), "Sec-WebSocket-Key") {
				key = strings.TrimSpace(value)
			}
		}
		if key == "" {
			serverDone <- fmt.Errorf("missing Sec-WebSocket-Key")
			return
		}
		digest := sha1.Sum([]byte(key + websocketGUID))
		accept := base64.StdEncoding.EncodeToString(digest[:])
		_, err = io.WriteString(server, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: keep-alive, Upgrade\r\nSec-WebSocket-Accept: "+accept+"\r\n\r\n")
		serverDone <- err
	}()
	if err := websocketHandshake(client, bufio.NewReader(client)); err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestWebSocketHandshakeRejectsUnauthenticatedUpgrade(t *testing.T) {
	for name, response := range map[string]string{
		"status": "HTTP/1.1 200 OK\r\n\r\n",
		"accept": "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: wrong\r\n\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			var request bytes.Buffer
			err := websocketHandshake(&request, bufio.NewReader(strings.NewReader(response)))
			if err == nil {
				t.Fatal("expected strict upgrade failure")
			}
			if !strings.HasPrefix(request.String(), "GET /rpc HTTP/1.1\r\n") {
				t.Fatalf("unexpected request %q", request.String())
			}
		})
	}
}

func TestClientAndServerFrameDirections(t *testing.T) {
	payload := []byte(`{"hello":"world"}`)
	var clientFrame bytes.Buffer
	if err := writeClientFrame(&clientFrame, payload, 0x1); err != nil {
		t.Fatal(err)
	}
	opcode, got, err := readWebSocketFrame(bytes.NewReader(clientFrame.Bytes()), true)
	if err != nil {
		t.Fatal(err)
	}
	if opcode != 0x1 || !bytes.Equal(got, payload) {
		t.Fatalf("unexpected decoded client frame opcode=%#x payload=%q", opcode, got)
	}

	serverFrame := append(websocketHeader(0x1, len(payload), false), payload...)
	opcode, got, err = readWebSocketFrame(bytes.NewReader(serverFrame), false)
	if err != nil {
		t.Fatal(err)
	}
	if opcode != 0x1 || !bytes.Equal(got, payload) {
		t.Fatalf("unexpected decoded server frame opcode=%#x payload=%q", opcode, got)
	}
}

func TestFrameReaderRejectsFragmentMaskAndOversize(t *testing.T) {
	for name, frame := range map[string][]byte{
		"fragment":      {0x01, 0x00},
		"masked-server": {0x81, 0x80, 0, 0, 0, 0},
		"oversize":      {0x81, 0x7f, 0, 0, 0, 0, 0, 0x40, 0, 1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := readWebSocketFrame(bytes.NewReader(frame), false); err == nil {
				t.Fatal("expected strict frame rejection")
			}
		})
	}
}

func TestRequestWebSocketRPCCorrelatesAndReturnsResult(t *testing.T) {
	serverPayload := []byte(`{"id":7,"result":{"ok":true}}`)
	serverFrame := append(websocketHeader(0x1, len(serverPayload), false), serverPayload...)
	var clientFrames bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := requestWebSocketRPC(ctx, bytes.NewReader(serverFrame), &clientFrames, 7, "example", map[string]bool{"enabled": true})
	if err != nil {
		t.Fatal(err)
	}
	var resultShape map[string]bool
	if err := json.Unmarshal(result, &resultShape); err != nil || !resultShape["ok"] {
		t.Fatalf("unexpected result %s: %v", result, err)
	}
	opcode, requestPayload, err := readWebSocketFrame(bytes.NewReader(clientFrames.Bytes()), true)
	if err != nil || opcode != 0x1 {
		t.Fatalf("invalid client request frame: opcode=%#x err=%v", opcode, err)
	}
	var requestShape struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(requestPayload, &requestShape); err != nil {
		t.Fatal(err)
	}
	if requestShape.ID != 7 || requestShape.Method != "example" {
		t.Fatalf("unexpected request %+v", requestShape)
	}
}

func TestRequestWebSocketRPCRejectsMismatchAndError(t *testing.T) {
	for name, payload := range map[string]string{
		"mismatched-id":  `{"id":8,"result":{}}`,
		"rpc-error":      `{"id":7,"error":{"code":-32000,"message":"failed"}}`,
		"missing-result": `{"id":7}`,
	} {
		t.Run(name, func(t *testing.T) {
			serverPayload := []byte(payload)
			serverFrame := append(websocketHeader(0x1, len(serverPayload), false), serverPayload...)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := requestWebSocketRPC(ctx, bytes.NewReader(serverFrame), io.Discard, 7, "example", map[string]any{}); err == nil {
				t.Fatal("expected correlated JSON-RPC failure")
			}
		})
	}
}

func TestRequestWebSocketRPCHandlesPingThenResponse(t *testing.T) {
	ping := []byte("probe")
	responsePayload := []byte(`{"id":3,"result":null}`)
	frames := append(websocketHeader(0x9, len(ping), false), ping...)
	frames = append(frames, websocketHeader(0x1, len(responsePayload), false)...)
	frames = append(frames, responsePayload...)
	var clientFrames bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := requestWebSocketRPC(ctx, bytes.NewReader(frames), &clientFrames, 3, "example", nil); err != nil {
		t.Fatal(err)
	}
	reader := bytes.NewReader(clientFrames.Bytes())
	if opcode, _, err := readWebSocketFrame(reader, true); err != nil || opcode != 0x1 {
		t.Fatalf("missing request frame: opcode=%#x err=%v", opcode, err)
	}
	if opcode, payload, err := readWebSocketFrame(reader, true); err != nil || opcode != 0xA || !bytes.Equal(payload, ping) {
		t.Fatalf("missing correlated pong: opcode=%#x payload=%q err=%v", opcode, payload, err)
	}
}

func TestDecodeMCPFixtureCallRequiresMatchingTypedContent(t *testing.T) {
	receipt := mcpFixtureCallReceipt{
		SchemaVersion: "devkit/product-mcp-tool-call-fixture/v1",
		Status:        "observed",
		ConsumerIndex: 2,
	}
	receiptJSON, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	success, err := json.Marshal(map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": string(receiptJSON)}},
		"structuredContent": receipt,
		"isError":           false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := decodeMCPFixtureCall(success, 2); err != nil || got != receipt {
		t.Fatalf("typed fixture call = %#v, %v", got, err)
	}
	for name, payload := range map[string][]byte{
		"error":          []byte(`{"content":[{"type":"text","text":"{}"}],"structuredContent":{},"isError":true}`),
		"wrong consumer": bytes.ReplaceAll(success, []byte(`"consumer_index":2`), []byte(`"consumer_index":1`)),
		"no content":     []byte(`{"content":[],"structuredContent":{},"isError":false}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeMCPFixtureCall(payload, 2); err == nil {
				t.Fatal("invalid MCP fixture result was accepted")
			}
		})
	}
}

// product-managed-client is a test-only JSON-RPC client for the installed
// Product SSH/session/supervisor/app-server lifecycle. It owns no runtime
// authority; its exact SSH argv enters the same forced-command path as a real
// external consumer and reports only observed typed postconditions.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type response struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

const (
	websocketRPCPath     = "/rpc"
	websocketGUID        = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	maximumFramePayload  = 4 << 20
	maximumHandshakeLine = 4096
	maximumHeaders       = 64
	rpcRequestTimeout    = 15 * time.Second
)

type result struct {
	SchemaVersion           string `json:"schema_version"`
	InitializeReady         bool   `json:"initialize_ready"`
	EphemeralThreadCreated  bool   `json:"ephemeral_thread_created"`
	EphemeralThreadRead     bool   `json:"ephemeral_thread_read"`
	MCPStatusRead           bool   `json:"mcp_status_read"`
	MCPFixtureTools         int    `json:"mcp_fixture_tools"`
	MCPFixtureCallVerified  bool   `json:"mcp_fixture_call_verified"`
	MCPFixtureConsumerIndex int    `json:"mcp_fixture_consumer_index"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "product-managed-client:", err)
		os.Exit(2)
	}
}

func run(args []string) error {
	if len(args) != 15 || (args[0] != "probe" && args[0] != "hold") || args[1] != "--ssh" ||
		args[3] != "--identity" || args[5] != "--known-hosts" ||
		args[7] != "--port" || args[9] != "--user" || args[11] != "--cwd" ||
		args[13] != "--consumer-index" {
		return fmt.Errorf("requires probe|hold --ssh PATH --identity PATH --known-hosts PATH --port N --user NAME --cwd PATH --consumer-index N")
	}
	mode := args[0]
	port, err := strconv.Atoi(args[8])
	consumerIndex, indexErr := strconv.Atoi(args[14])
	if err != nil || port < 1 || port > 65535 || args[10] == "" || args[12] == "" ||
		indexErr != nil || consumerIndex < 1 || consumerIndex > 2 {
		return fmt.Errorf("invalid SSH Product consumer identity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, args[2],
		"-T", "-F", "/dev/null", "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes",
		"-o", "IdentityFile="+args[4], "-o", "UserKnownHostsFile="+args[6],
		"-o", "GlobalKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=yes",
		"-p", strconv.Itoa(port), args[10]+"@127.0.0.1",
		"codex", "-c", "features.code_mode_host=true", "app-server", "proxy",
	)
	command.Env = []string{}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := command.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	processWaited := false
	stderrDone := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(io.LimitReader(stderr, 1<<20))
		stderrDone <- data
	}()
	defer func() {
		_ = stdin.Close()
		if processWaited {
			return
		}
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		select {
		case <-wait:
		case <-time.After(3 * time.Second):
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			<-wait
		}
	}()

	reader := bufio.NewReaderSize(stdout, maximumHandshakeLine)
	if err := websocketHandshake(stdin, reader); err != nil {
		return fmt.Errorf("managed app-server WebSocket handshake: %w", err)
	}
	nextRequestID := 1
	requestRPC := func(method string, params any) (json.RawMessage, error) {
		requestContext, requestCancel := context.WithTimeout(ctx, rpcRequestTimeout)
		defer requestCancel()
		id := nextRequestID
		nextRequestID++
		result, err := requestWebSocketRPC(requestContext, reader, stdin, id, method, params)
		if err == nil {
			return result, nil
		}
		select {
		case processErr := <-wait:
			detail := <-stderrDone
			return nil, fmt.Errorf("managed SSH session exited before response %d: %v: %s: %w", id, processErr, strings.TrimSpace(string(detail)), err)
		default:
			return nil, err
		}
	}
	notify := func(method string, params any) error {
		return notifyWebSocketRPC(stdin, method, params)
	}

	if _, err := requestRPC("initialize", map[string]any{
		"clientInfo": map[string]string{"name": "devkit-product-managed-lifecycle", "version": "1"},
		"capabilities": map[string]any{
			"experimentalApi": true, "requestAttestation": false,
			"optOutNotificationMethods": []string{},
		},
	}); err != nil {
		return err
	}
	if err := notify("initialized", map[string]any{}); err != nil {
		return err
	}
	if mode == "hold" {
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
			"schema_version":   "devkit/product-managed-app-server-hold/v1",
			"initialize_ready": true,
			"status":           "holding",
		}); err != nil {
			return err
		}
		select {
		case <-wait:
			processWaited = true
			<-stderrDone
			return nil
		case <-ctx.Done():
			return fmt.Errorf("managed app-server hold timed out: %w", ctx.Err())
		}
	}
	threadPayload, err := requestRPC("thread/start", map[string]any{
		"cwd": args[12], "approvalPolicy": "never", "sandbox": "danger-full-access", "ephemeral": true,
	})
	if err != nil {
		return err
	}
	var threadStart struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(threadPayload, &threadStart); err != nil || threadStart.Thread.ID == "" {
		return fmt.Errorf("managed app-server did not create an ephemeral thread")
	}
	threadReadPayload, err := requestRPC("thread/read", map[string]any{
		"threadId": threadStart.Thread.ID, "includeTurns": false,
	})
	if err != nil {
		return err
	}
	var threadRead struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(threadReadPayload, &threadRead); err != nil || threadRead.Thread.ID != threadStart.Thread.ID {
		return fmt.Errorf("managed app-server thread readback mismatched")
	}
	mcpPayload, err := requestRPC("mcpServerStatus/list", map[string]any{
		"threadId": threadStart.Thread.ID, "detail": "toolsAndAuthOnly", "limit": 128,
	})
	if err != nil {
		return err
	}
	var page struct {
		Data []struct {
			Name  string `json:"name"`
			Tools map[string]struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"data"`
		NextCursor *string `json:"nextCursor"`
	}
	if err := json.Unmarshal(mcpPayload, &page); err != nil || page.NextCursor != nil {
		return fmt.Errorf("managed app-server MCP status has unexpected shape")
	}
	fixtureTools := 0
	for _, server := range page.Data {
		if server.Name == "devkit_fixture" {
			tool, ok := server.Tools["lifecycle_probe"]
			if !ok || tool.Name != "lifecycle_probe" {
				return fmt.Errorf("managed app-server is missing the lifecycle probe fixture")
			}
			fixtureTools++
		}
	}
	if fixtureTools != 1 {
		return fmt.Errorf("managed app-server did not expose the exact MCP fixture")
	}
	toolCallPayload, err := requestRPC("mcpServer/tool/call", map[string]any{
		"threadId": threadStart.Thread.ID,
		"server":   "devkit_fixture",
		"tool":     "lifecycle_probe",
		"arguments": map[string]any{
			"schema_version": "devkit/product-mcp-tool-call-fixture/v1",
			"consumer_index": consumerIndex,
			"objective":      "prove-mcp-tool-call-transport",
		},
	})
	if err != nil {
		return err
	}
	toolCall, err := decodeMCPFixtureCall(toolCallPayload, consumerIndex)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result{
		SchemaVersion:           "devkit/product-managed-app-server-client/v1",
		InitializeReady:         true,
		EphemeralThreadCreated:  true,
		EphemeralThreadRead:     true,
		MCPStatusRead:           true,
		MCPFixtureTools:         fixtureTools,
		MCPFixtureCallVerified:  true,
		MCPFixtureConsumerIndex: toolCall.ConsumerIndex,
	})
}

type mcpFixtureCallReceipt struct {
	SchemaVersion string `json:"schema_version"`
	Status        string `json:"status"`
	ConsumerIndex int    `json:"consumer_index"`
}

func decodeMCPFixtureCall(
	payload json.RawMessage,
	consumerIndex int,
) (mcpFixtureCallReceipt, error) {
	var call struct {
		Content           []json.RawMessage `json:"content"`
		IsError           *bool             `json:"isError"`
		StructuredContent json.RawMessage   `json:"structuredContent"`
	}
	if err := json.Unmarshal(payload, &call); err != nil ||
		call.IsError == nil || *call.IsError ||
		len(call.Content) != 1 || len(call.StructuredContent) == 0 {
		return mcpFixtureCallReceipt{}, fmt.Errorf("managed MCP fixture did not return a typed non-error result")
	}
	decodeReceipt := func(payload []byte) (mcpFixtureCallReceipt, error) {
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		var receipt mcpFixtureCallReceipt
		if err := decoder.Decode(&receipt); err != nil {
			return receipt, err
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return receipt, fmt.Errorf("trailing input")
		}
		return receipt, nil
	}
	receipt, err := decodeReceipt(call.StructuredContent)
	if err != nil {
		return receipt, fmt.Errorf("decode managed MCP fixture structured result: %w", err)
	}
	var content struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(call.Content[0], &content); err != nil || content.Type != "text" {
		return receipt, fmt.Errorf("managed MCP fixture content is not one text item")
	}
	textReceipt, err := decodeReceipt([]byte(content.Text))
	if err != nil || textReceipt != receipt {
		return receipt, fmt.Errorf("managed MCP fixture text result does not match structured content")
	}
	if receipt.SchemaVersion != "devkit/product-mcp-tool-call-fixture/v1" ||
		receipt.Status != "observed" ||
		receipt.ConsumerIndex != consumerIndex {
		return receipt, fmt.Errorf("managed MCP fixture result mismatched the exact consumer")
	}
	return receipt, nil
}

func websocketHandshake(writer io.Writer, reader *bufio.Reader) error {
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		return err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	request := fmt.Sprintf(
		"GET %s HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		websocketRPCPath,
		key,
	)
	if _, err := io.WriteString(writer, request); err != nil {
		return err
	}
	status, err := readHandshakeLine(reader)
	if err != nil {
		return fmt.Errorf("read status: %w", err)
	}
	if !strings.HasPrefix(status, "HTTP/1.1 101 ") {
		return fmt.Errorf("unexpected status %q", strings.TrimSpace(status))
	}
	headers := make(map[string]string)
	for count := 0; count < maximumHeaders; count++ {
		line, err := readHandshakeLine(reader)
		if err != nil {
			return fmt.Errorf("read header: %w", err)
		}
		if line == "\r\n" {
			break
		}
		name, value, found := strings.Cut(strings.TrimSuffix(line, "\r\n"), ":")
		if !found || strings.TrimSpace(name) == "" {
			return fmt.Errorf("malformed upgrade header")
		}
		keyName := strings.ToLower(strings.TrimSpace(name))
		if _, duplicate := headers[keyName]; duplicate {
			return fmt.Errorf("duplicate upgrade header %q", keyName)
		}
		headers[keyName] = strings.TrimSpace(value)
		if count == maximumHeaders-1 {
			return fmt.Errorf("upgrade headers exceed bounded count")
		}
	}
	acceptDigest := sha1.Sum([]byte(key + websocketGUID))
	expectedAccept := base64.StdEncoding.EncodeToString(acceptDigest[:])
	if !strings.EqualFold(headers["upgrade"], "websocket") ||
		!headerContainsToken(headers["connection"], "upgrade") ||
		headers["sec-websocket-accept"] != expectedAccept {
		return fmt.Errorf("upgrade headers do not authenticate the WebSocket endpoint")
	}
	return nil
}

func requestWebSocketRPC(
	ctx context.Context,
	reader io.Reader,
	writer io.Writer,
	id int,
	method string,
	params any,
) (json.RawMessage, error) {
	payload, err := json.Marshal(map[string]any{
		"id": id, "method": method, "params": params,
	})
	if err != nil {
		return nil, err
	}
	if err := writeClientFrame(writer, payload, 0x1); err != nil {
		return nil, err
	}
	for {
		opcode, frame, err := readFrameWithContext(ctx, reader)
		if err != nil {
			return nil, fmt.Errorf("managed app-server response %d: %w", id, err)
		}
		switch opcode {
		case 0x1:
			var message response
			if err := json.Unmarshal(frame, &message); err != nil {
				return nil, fmt.Errorf("decode managed app-server response: %w", err)
			}
			if message.ID == 0 {
				continue
			}
			if message.ID != id {
				return nil, fmt.Errorf("managed app-server returned response %d before %d", message.ID, id)
			}
			if len(message.Error) != 0 && string(message.Error) != "null" {
				return nil, fmt.Errorf("managed app-server request %d failed: %s", id, message.Error)
			}
			if len(message.Result) == 0 {
				return nil, fmt.Errorf("managed app-server response %d has neither result nor error", id)
			}
			return message.Result, nil
		case 0x8:
			return nil, fmt.Errorf("managed app-server closed WebSocket before response %d", id)
		case 0x9:
			if err := writeClientFrame(writer, frame, 0xA); err != nil {
				return nil, err
			}
		case 0xA:
		default:
			return nil, fmt.Errorf("managed app-server returned unsupported WebSocket opcode %#x", opcode)
		}
	}
}

func notifyWebSocketRPC(writer io.Writer, method string, params any) error {
	payload, err := json.Marshal(map[string]any{
		"method": method, "params": params,
	})
	if err != nil {
		return err
	}
	return writeClientFrame(writer, payload, 0x1)
}

func readHandshakeLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) > maximumHandshakeLine || !strings.HasSuffix(line, "\r\n") {
		return "", fmt.Errorf("handshake line exceeds or violates the bounded CRLF grammar")
	}
	return line, nil
}

func headerContainsToken(value, token string) bool {
	for _, item := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(item), token) {
			return true
		}
	}
	return false
}

type websocketFrameResult struct {
	opcode  byte
	payload []byte
	err     error
}

func readFrameWithContext(ctx context.Context, reader io.Reader) (byte, []byte, error) {
	result := make(chan websocketFrameResult, 1)
	go func() {
		opcode, payload, err := readWebSocketFrame(reader, false)
		result <- websocketFrameResult{opcode: opcode, payload: payload, err: err}
	}()
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case frame := <-result:
		return frame.opcode, frame.payload, frame.err
	}
}

func writeClientFrame(writer io.Writer, payload []byte, opcode byte) error {
	if len(payload) > maximumFramePayload {
		return fmt.Errorf("client WebSocket frame exceeds bounded payload")
	}
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	header := websocketHeader(opcode, len(payload), true)
	masked := make([]byte, len(payload))
	for index, value := range payload {
		masked[index] = value ^ mask[index%4]
	}
	frame := append(append(header, mask...), masked...)
	for len(frame) > 0 {
		count, err := writer.Write(frame)
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
		frame = frame[count:]
	}
	return nil
}

func readWebSocketFrame(reader io.Reader, expectedMasked bool) (byte, []byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return 0, nil, err
	}
	if header[0]&0x70 != 0 || header[0]&0x80 == 0 {
		return 0, nil, fmt.Errorf("fragmented or reserved WebSocket frame is forbidden")
	}
	opcode := header[0] & 0x0F
	masked := header[1]&0x80 != 0
	if masked != expectedMasked {
		return 0, nil, fmt.Errorf("WebSocket frame masking direction is invalid")
	}
	length := uint64(header[1] & 0x7F)
	switch length {
	case 126:
		var extended [2]byte
		if _, err := io.ReadFull(reader, extended[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(extended[:]))
		if length < 126 {
			return 0, nil, fmt.Errorf("WebSocket frame uses noncanonical length encoding")
		}
	case 127:
		var extended [8]byte
		if _, err := io.ReadFull(reader, extended[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(extended[:])
		if length < 65536 || length>>63 != 0 {
			return 0, nil, fmt.Errorf("WebSocket frame uses invalid extended length")
		}
	}
	if length > maximumFramePayload {
		return 0, nil, fmt.Errorf("WebSocket frame exceeds bounded payload")
	}
	if opcode >= 0x8 && length > 125 {
		return 0, nil, fmt.Errorf("WebSocket control frame exceeds bounded payload")
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(reader, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for index := range payload {
			payload[index] ^= mask[index%4]
		}
	}
	return opcode, payload, nil
}

func websocketHeader(opcode byte, payloadLength int, masked bool) []byte {
	header := []byte{0x80 | opcode}
	maskBit := byte(0)
	if masked {
		maskBit = 0x80
	}
	switch {
	case payloadLength < 126:
		header = append(header, maskBit|byte(payloadLength))
	case payloadLength < 65536:
		header = append(header, maskBit|126, byte(payloadLength>>8), byte(payloadLength))
	default:
		var extended [8]byte
		binary.BigEndian.PutUint64(extended[:], uint64(payloadLength))
		header = append(header, maskBit|127)
		header = append(header, extended[:]...)
	}
	return header
}

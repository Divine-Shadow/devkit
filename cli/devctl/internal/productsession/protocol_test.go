package productsession

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

func TestFrameRoundTripAndTrailingTransportBytes(t *testing.T) {
	request := Request{SchemaVersion: RequestSchema, Count: 2, Index: 1, OriginalCommand: "codex app-server proxy"}
	var transport bytes.Buffer
	if err := WriteFrame(&transport, request); err != nil {
		t.Fatal(err)
	}
	transport.WriteString("raw-proxy-data")
	var decoded Request
	if err := ReadFrame(&transport, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != request || transport.String() != "raw-proxy-data" {
		t.Fatalf("frame crossed raw transport boundary: %#v %q", decoded, transport.String())
	}
}

func TestReadFrameRejectsUnknownAndOversized(t *testing.T) {
	var unknown bytes.Buffer
	payload := []byte(`{"schema_version":"devkit/product-supervisor-request/v1","unknown":true}`)
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	unknown.Write(header[:])
	unknown.Write(payload)
	if err := ReadFrame(&unknown, &Request{}); err == nil {
		t.Fatal("unknown request field accepted")
	}

	var oversized bytes.Buffer
	binary.BigEndian.PutUint32(header[:], maximumFrame+1)
	oversized.Write(header[:])
	oversized.WriteString(strings.Repeat("x", 8))
	if err := ReadFrame(&oversized, &Request{}); err == nil {
		t.Fatal("oversized request accepted")
	}
}

func TestFailureForReturnsClosedTypedCodes(t *testing.T) {
	for operation, expected := range map[FailureOperation]string{
		FailureRequest: "invalid-request",
		FailurePrepare: "consumer-preparation-failed",
		FailureListen:  "app-server-start-failed",
		FailureProxy:   "app-server-proxy-failed",
		FailureVersion: "codex-version-failed",
	} {
		failure := FailureFor(operation, fmt.Errorf("diagnostic"))
		if failure == nil || !failure.Valid() || failure.Code != expected {
			t.Fatalf("operation %q produced %#v", operation, failure)
		}
	}
	if (&Failure{
		SchemaVersion: FailureSchema,
		Phase:         "request",
		Code:          "caller-invented",
		Message:       "diagnostic",
	}).Valid() {
		t.Fatal("unknown failure code was accepted")
	}
	if (&Failure{
		SchemaVersion: FailureSchema,
		Phase:         string(FailureRequest),
		Code:          "app-server-start-failed",
		Message:       "diagnostic",
	}).Valid() {
		t.Fatal("mismatched failure phase and code were accepted")
	}
}

func TestFailureOperationForBindsParsedCommandKinds(t *testing.T) {
	for kind, expected := range map[Kind]FailureOperation{
		"":          FailureRequest,
		KindPrepare: FailurePrepare,
		KindListen:  FailureListen,
		KindProxy:   FailureProxy,
		KindVersion: FailureVersion,
	} {
		if actual := FailureOperationFor(Command{Kind: kind}); actual != expected {
			t.Fatalf("kind %q mapped to %q, expected %q", kind, actual, expected)
		}
	}
}

package productsession

import (
	"bytes"
	"encoding/binary"
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

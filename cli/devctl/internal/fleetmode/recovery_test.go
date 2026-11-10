package fleetmode

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

func TestRecoveryRetainsOriginalAcrossRepeat(t *testing.T) {
	start, disabled := uint32(3), uint32(4)
	path := filepath.Join(t.TempDir(), "created", "service.json")
	first := recoverySnapshot{SchemaVersion: 1, Hostname: "ARCADE", Kind: "service", Name: "WSLService", StartType: &start}
	if err := retainRecoverySnapshot(path, first); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	repeated := first
	repeated.Hostname = "arcade"
	repeated.StartType = &disabled
	if err := retainRecoverySnapshot(path, repeated); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("repeat replaced original startup observation")
	}
	var stored recoverySnapshot
	if err := json.Unmarshal(after, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.StartType == nil || *stored.StartType != 3 {
		t.Fatal("original manual startup mode lost")
	}

	enabled, off := true, false
	taskPath := filepath.Join(t.TempDir(), "task.json")
	task := recoverySnapshot{SchemaVersion: 1, Hostname: "ARCADE", Kind: "task", Name: `\Reconnect`, Enabled: &enabled}
	if err := retainRecoverySnapshot(taskPath, task); err != nil {
		t.Fatal(err)
	}
	task.Enabled = &off
	if err := retainRecoverySnapshot(taskPath, task); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Enabled == nil || !*stored.Enabled {
		t.Fatal("original enabled task state lost")
	}
}

func TestRecoveryRefusesInvalidExistingSnapshotWithoutChangingIt(t *testing.T) {
	start := uint32(3)
	want := recoverySnapshot{SchemaVersion: 1, Hostname: "ARCADE", Kind: "service", Name: "WSLService", StartType: &start}
	for name, raw := range map[string]string{
		"malformed":      `{"schemaVersion":`,
		"wrong-host":     `{"schemaVersion":1,"hostname":"OTHER","kind":"service","name":"WSLService","startType":3}`,
		"wrong-service":  `{"schemaVersion":1,"hostname":"ARCADE","kind":"service","name":"vmcompute","startType":3}`,
		"trailing":       `{"schemaVersion":1,"hostname":"ARCADE","kind":"service","name":"WSLService","startType":3} {}`,
		"unknown-field":  `{"schemaVersion":1,"hostname":"ARCADE","kind":"service","name":"WSLService","startType":3,"extra":true}`,
		"invalid-start":  `{"schemaVersion":1,"hostname":"ARCADE","kind":"service","name":"WSLService","startType":0}`,
		"mixed-settings": `{"schemaVersion":1,"hostname":"ARCADE","kind":"service","name":"WSLService","startType":3,"enabled":true}`,
		"oversize":       string(bytes.Repeat([]byte(" "), 65537)),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "service.json")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if err := retainRecoverySnapshot(path, want); err == nil {
				t.Fatal("invalid prior snapshot accepted")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != raw {
				t.Fatal("refused snapshot was changed")
			}
		})
	}
}

func recoveryUTF16(text string, order binary.ByteOrder, bom bool) []byte {
	words := utf16.Encode([]rune(text))
	raw := make([]byte, len(words)*2)
	for i, word := range words {
		order.PutUint16(raw[i*2:], word)
	}
	if bom {
		if order == binary.LittleEndian {
			return append([]byte{0xff, 0xfe}, raw...)
		}
		return append([]byte{0xfe, 0xff}, raw...)
	}
	return raw
}

func TestTaskEnabledTransportEncodings(t *testing.T) {
	for _, value := range []string{"true", "false"} {
		xml := `<?xml version="1.0" encoding="UTF-16"?><Task xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task"><Settings><Enabled>` + value + `</Enabled></Settings></Task>`
		for name, raw := range map[string][]byte{
			"utf8":           []byte(xml),
			"utf16le-bom":    recoveryUTF16(xml, binary.LittleEndian, true),
			"utf16le-no-bom": recoveryUTF16(xml, binary.LittleEndian, false),
			"utf16be-bom":    recoveryUTF16(xml, binary.BigEndian, true),
			"utf16be-no-bom": recoveryUTF16(xml, binary.BigEndian, false),
		} {
			t.Run(value+"/"+name, func(t *testing.T) {
				enabled, err := parseTaskEnabled(raw)
				if err != nil {
					t.Fatal(err)
				}
				if enabled != (value == "true") {
					t.Fatalf("enabled=%v, expected %s", enabled, value)
				}
			})
		}
	}
}

func TestTaskEnabledRefusesUnknownAndTruncated(t *testing.T) {
	valid := `<?xml version="1.0" encoding="UTF-16"?><Task><Settings><Enabled>false</Enabled></Settings></Task>`
	truncated := recoveryUTF16(valid, binary.LittleEndian, true)
	for name, raw := range map[string][]byte{
		"unknown-value":   []byte(`<Task><Settings><Enabled>maybe</Enabled></Settings></Task>`),
		"missing-value":   []byte(`<Task><Settings/></Task>`),
		"wrong-root":      []byte(`<Other><Settings><Enabled>false</Enabled></Settings></Other>`),
		"truncated-xml":   []byte(`<Task><Settings><Enabled>false</Enabled>`),
		"truncated-utf16": truncated[:len(truncated)-1],
		"oversize":        bytes.Repeat([]byte(" "), (1<<20)+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseTaskEnabled(raw); err == nil {
				t.Fatal("invalid task XML accepted")
			}
		})
	}
}

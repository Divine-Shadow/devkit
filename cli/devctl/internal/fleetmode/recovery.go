package fleetmode

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

type recoverySnapshot struct {
	SchemaVersion int     `json:"schemaVersion"`
	Hostname      string  `json:"hostname"`
	Kind          string  `json:"kind"`
	Name          string  `json:"name"`
	StartType     *uint32 `json:"startType,omitempty"`
	Enabled       *bool   `json:"enabled,omitempty"`
}

func (s recoverySnapshot) validate() error {
	if s.SchemaVersion != 1 || s.Hostname == "" || s.Name == "" {
		return errors.New("invalid recovery snapshot identity")
	}
	if s.Kind == "service" && s.Name == "WSLService" && s.StartType != nil && *s.StartType >= 2 && *s.StartType <= 4 && s.Enabled == nil {
		return nil
	}
	if s.Kind == "task" && s.Enabled != nil && s.StartType == nil {
		return nil
	}
	return errors.New("invalid recovery snapshot settings")
}

// Retain the first observation. A partial write is removed by its owner; an
// invalid preexisting record is never treated as a usable recovery snapshot.
func retainRecoverySnapshot(path string, s recoverySnapshot) error {
	if err := s.validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		priorFile, readErr := os.Open(path)
		if readErr != nil {
			return readErr
		}
		raw, readErr := io.ReadAll(io.LimitReader(priorFile, 65537))
		_ = priorFile.Close()
		if readErr != nil {
			return readErr
		}
		if len(raw) > 65536 {
			return errors.New("recovery snapshot exceeds limit")
		}
		var prior recoverySnapshot
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&prior); err != nil {
			return err
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return errors.New("recovery snapshot contains trailing data")
		}
		if err := prior.validate(); err != nil {
			return err
		}
		if prior.Kind != s.Kind || prior.Name != s.Name || !strings.EqualFold(prior.Hostname, s.Hostname) {
			return errors.New("recovery snapshot belongs to a different target")
		}
		return nil
	}
	if err != nil {
		return err
	}
	err = json.NewEncoder(f).Encode(s)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

func parseTaskEnabled(raw []byte) (bool, error) {
	if len(raw) > 1<<20 {
		return false, errors.New("task XML exceeds limit")
	}
	var order binary.ByteOrder
	if bytes.HasPrefix(raw, []byte{0xff, 0xfe}) {
		order = binary.LittleEndian
		raw = raw[2:]
	} else if bytes.HasPrefix(raw, []byte{0xfe, 0xff}) {
		order = binary.BigEndian
		raw = raw[2:]
	} else if len(raw) > 3 && raw[0] == '<' && raw[1] == 0 {
		order = binary.LittleEndian
	} else if len(raw) > 3 && raw[0] == 0 && raw[1] == '<' {
		order = binary.BigEndian
	}
	if order != nil {
		if len(raw)%2 != 0 {
			return false, errors.New("truncated UTF-16 task XML")
		}
		words := make([]uint16, len(raw)/2)
		for i := range words {
			words[i] = order.Uint16(raw[i*2:])
		}
		raw = []byte(string(utf16.Decode(words)))
	}
	// The bytes are normalized above. The declaration may still describe the
	// original UTF-16 transport emitted by schtasks.exe.
	return parseNormalizedTaskXML(raw)
}

func parseNormalizedTaskXML(raw []byte) (bool, error) {
	if end := bytes.Index(raw, []byte("?>")); bytes.HasPrefix(raw, []byte("<?xml")) && end >= 0 {
		raw = raw[end+2:]
	}
	var task struct {
		XMLName  xml.Name `xml:"Task"`
		Settings struct {
			Enabled string `xml:"Enabled"`
		} `xml:"Settings"`
	}
	if err := xml.Unmarshal(raw, &task); err != nil {
		return false, err
	}
	switch strings.TrimSpace(task.Settings.Enabled) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, errors.New("task XML has no explicit Enabled setting")
	}
}

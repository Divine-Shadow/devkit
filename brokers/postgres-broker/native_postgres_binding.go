package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Only the launcher-owned native path needs the IPv4 loopback endpoint. Pin
// its validated unspecified binding before Docker expands it to both families.
// Caller-selected addresses and host-only clients keep their request semantics.
func pinNativePostgresBinding(r *http.Request, body []byte, payload containerCreateRequest) error {
	bindings := payload.HostConfig.PortBindings["5432/tcp"]
	if r.Header.Get(runHeader) == "" || r.Header.Get(credentialHeader) == "" || len(bindings) != 1 || bindings[0].HostIP != "" {
		return nil
	}
	request, err := createJSONObject(body)
	if err != nil {
		return err
	}
	hostKey, err := createJSONField(request, "HostConfig")
	if err != nil {
		return err
	}
	host, err := createJSONObject(request[hostKey])
	if err != nil {
		return err
	}
	portsKey, err := createJSONField(host, "PortBindings")
	if err != nil {
		return err
	}
	ports, err := createJSONObject(host[portsKey])
	if err != nil {
		return err
	}
	var values []json.RawMessage
	if err := json.Unmarshal(ports["5432/tcp"], &values); err != nil || len(values) != 1 {
		return errForbidden
	}
	binding, err := createJSONObject(values[0])
	if err != nil {
		return err
	}
	// HostIp may be omitted. Remove its one case-insensitive spelling before
	// writing the canonical Docker field; ambiguous objects were refused above.
	for key := range binding {
		if strings.EqualFold(key, "HostIp") {
			delete(binding, key)
		}
	}
	binding["HostIp"] = json.RawMessage(`"127.0.0.1"`)
	ports["5432/tcp"], err = json.Marshal([]map[string]json.RawMessage{binding})
	if err != nil {
		return errForbidden
	}
	host[portsKey], err = json.Marshal(ports)
	if err != nil {
		return errForbidden
	}
	request[hostKey], err = json.Marshal(host)
	if err != nil {
		return errForbidden
	}
	rewritten, err := json.Marshal(request)
	if err != nil {
		return errForbidden
	}
	r.Body = io.NopCloser(bytes.NewReader(rewritten))
	r.ContentLength = int64(len(rewritten))
	r.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
	r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(rewritten)), nil }
	return nil
}

// Preserve raw values, including unknown Docker options and large integers.
// Refuse duplicate/case-ambiguous fields rather than change their interpretation.
func createJSONObject(body []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errForbidden
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, errForbidden
		}
		for existing := range fields {
			if strings.EqualFold(existing, key) {
				return nil, errForbidden
			}
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errForbidden
		}
		fields[key] = value
	}
	end, err := decoder.Token()
	var trailing json.RawMessage
	if err != nil || end != json.Delim('}') || decoder.Decode(&trailing) != io.EOF {
		return nil, errForbidden
	}
	return fields, nil
}

func createJSONField(fields map[string]json.RawMessage, name string) (string, error) {
	for key := range fields {
		if strings.EqualFold(key, name) {
			return key, nil
		}
	}
	return "", errForbidden
}

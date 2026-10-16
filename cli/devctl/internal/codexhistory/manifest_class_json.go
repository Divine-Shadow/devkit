package codexhistory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// A recognized field for one schema must not silently widen older schemas.
// Reject duplicates before decoding instead of accepting a last-value binding.
func (manifest *snapshotManifest) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("history manifest must be one exact object")
	}
	type plain snapshotManifest
	canonical := make(map[string]bool)
	t := reflect.TypeOf(plain{})
	for i := 0; i < t.NumField(); i++ {
		canonical[strings.Split(t.Field(i).Tag.Get("json"), ",")[0]] = true
	}
	keys := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || !canonical[key] || keys[key] {
			return fmt.Errorf("duplicate history manifest field")
		}
		keys[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("extra history manifest object")
	}
	var decoded plain
	strict := json.NewDecoder(bytes.NewReader(data))
	strict.DisallowUnknownFields()
	if err := strict.Decode(&decoded); err != nil {
		return err
	}
	if decoded.SchemaVersion != GUIAvailableEvidenceSchema && (keys["unavailable_gui_reference_count"] || keys["unavailable_gui_references"]) {
		return fmt.Errorf("GUI unavailable evidence requires the distinct v4 schema")
	}
	if decoded.SchemaVersion == GUIAvailableEvidenceSchema && (keys["unavailable_archived_reference_count"] || keys["unavailable_archived_references"]) {
		return fmt.Errorf("v4 cannot masquerade as archived-only evidence")
	}
	*manifest = snapshotManifest(decoded)
	return nil
}

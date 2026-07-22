package productadapter

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

const (
	canonicalAuthoritySelector = "/var/lib/product-runtime/authority-selector.json"
	authoritySelectorSchema    = "devkit/product-runtime-authority-selector/v1"
)

type authoritySelector struct {
	SchemaVersion  string `json:"schemaVersion"`
	ManifestPath   string `json:"manifestPath"`
	ManifestSHA256 string `json:"manifestSha256"`
}

func parseAuthoritySelector(payload []byte) (authoritySelector, error) {
	var selector authoritySelector
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&selector); err != nil {
		return authoritySelector{}, fmt.Errorf("decode Product authority selector: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return authoritySelector{}, fmt.Errorf("Product authority selector contains trailing data")
	}
	if selector.SchemaVersion != authoritySelectorSchema ||
		len(selector.ManifestSHA256) != 64 ||
		strings.ToLower(selector.ManifestSHA256) != selector.ManifestSHA256 {
		return authoritySelector{}, fmt.Errorf("Product authority selector is invalid")
	}
	if _, err := hex.DecodeString(selector.ManifestSHA256); err != nil {
		return authoritySelector{}, fmt.Errorf("Product authority selector digest is invalid")
	}
	if filepath.Clean(selector.ManifestPath) != selector.ManifestPath || !strings.HasPrefix(selector.ManifestPath, "/nix/store/") {
		return authoritySelector{}, fmt.Errorf("Product authority selector manifest path is invalid")
	}
	return selector, nil
}

//go:build !devkitintegration

package productadapter

import "testing"

func TestParseAuthoritySelectorRejectsTornUnknownAndTrailingGenerations(t *testing.T) {
	valid := []byte(`{"schemaVersion":"devkit/product-runtime-authority-selector/v1","manifestPath":"/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-authority/identity.json","manifestSha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}` + "\n")
	if _, err := parseAuthoritySelector(valid); err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string][]byte{
		"torn":     valid[:len(valid)/2],
		"unknown":  []byte(`{"schemaVersion":"devkit/product-runtime-authority-selector/v1","manifestPath":"/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-authority/identity.json","manifestSha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","other":true}`),
		"trailing": append(append([]byte{}, valid...), []byte(`{}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseAuthoritySelector(payload); err == nil {
				t.Fatalf("%s selector was accepted", name)
			}
		})
	}
}

//go:build devkitintegration

package productadapter

import (
	"net/url"
	"strings"
)

// The integration artifact is compile-time bound to an immutable fixture
// manifest and strict ephemeral SSH authority. Production accepts only the
// canonical Product SSH origins in product_origin.go.
func authorityPermitsProductOrigin(origin string) bool {
	if isProductSSHOrigin(origin) {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(origin))
	if err != nil ||
		parsed.Scheme != "ssh" ||
		parsed.User == nil ||
		parsed.User.Username() == "" ||
		parsed.Host != "ssh.github.com:443" {
		return false
	}
	return strings.HasSuffix(strings.TrimSuffix(parsed.Path, ".git"), "/"+ProductRepo)
}

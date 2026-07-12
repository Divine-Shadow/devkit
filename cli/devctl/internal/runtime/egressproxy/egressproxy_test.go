package egressproxy

import "testing"

func TestAllowlistAllowsExactAndSubdomains(t *testing.T) {
	allowlist := Allowlist{domains: map[string]bool{"example.com": true}}
	for _, host := range []string{"example.com", "www.example.com", "api.www.example.com:443"} {
		if !allowlist.Allowed(host) {
			t.Fatalf("expected %s to be allowed", host)
		}
	}
}

func TestAllowlistRejectsSiblingSuffixesAndIPs(t *testing.T) {
	allowlist := Allowlist{domains: map[string]bool{"example.com": true}}
	for _, host := range []string{"badexample.com", "example.org", "93.184.216.34", "[::1]:443"} {
		if allowlist.Allowed(host) {
			t.Fatalf("expected %s to be rejected", host)
		}
	}
}

package launch

import (
	"strings"
	"testing"
)

func TestTerraformProviderDiagnosticFiniteWire(t *testing.T) {
	good := terraformProviderAWSDiagnostic{SchemaVersion: "terraform-provider-aws-diagnostic/v1", Stage: "token", ErrorCode: "ExpiredToken", FailureClass: "expired_token"}.wireLine()
	got := parseTerraformProviderAWSDiagnostic([]byte("private unrelated stderr\n" + good + "\n"))
	if got == nil || got.ErrorCode != "ExpiredToken" || got.FailureClass != "expired_token" {
		t.Fatal("finite source evidence lost")
	}
	cases := map[string]string{
		"unknown field":   strings.Replace(good, `"stage":"token"`, `"stage":"token","private":"SENTINEL"`, 1),
		"wrong type":      strings.Replace(good, `"accessKeyId":false`, `"accessKeyId":"SENTINEL"`, 1),
		"missing boolean": strings.Replace(good, `"accessKeyId":false,`, "", 1),
		"duplicate":       strings.Replace(good, `"stage":"token"`, `"stage":"token","stage":"account"`, 1),
		"trailing":        good + ` {}`,
		"noncanonical":    strings.Replace(good, `"stage":"token"`, `"stage": "token"`, 1),
		"class mismatch":  strings.Replace(good, `"failureClass":"expired_token"`, `"failureClass":"unknown"`, 1),
		"unknown code":    strings.Replace(good, `"errorCode":"ExpiredToken"`, `"errorCode":"SENTINEL"`, 1),
		"malformed":       strings.TrimSuffix(good, "}"),
		"oversized":       good + strings.Repeat("x", 1025),
		"ambiguous":       good + "\n" + good,
	}
	for name, wire := range cases {
		t.Run(name, func(t *testing.T) {
			if parseTerraformProviderAWSDiagnostic([]byte(wire)) != nil {
				t.Fatal("unsafe or ambiguous wire admitted")
			}
		})
	}
	for code, class := range map[string]string{"ExpiredTokenException": "expired_token", "RequestExpired": "request_expired", "AccessDeniedException": "access_denied", "ResourceNotFoundException": "secret_not_found", "InvalidClientTokenId": "authentication_failure", "ValidationException": "request_rejected", "ServiceUnavailable": "service_failure", "unknown": "unknown"} {
		d := terraformProviderAWSDiagnostic{SchemaVersion: "terraform-provider-aws-diagnostic/v1", Stage: "account", ErrorCode: code, FailureClass: class}
		if parseTerraformProviderAWSDiagnostic([]byte(d.wireLine())) == nil {
			t.Errorf("allowed %s lost", code)
		}
	}
}

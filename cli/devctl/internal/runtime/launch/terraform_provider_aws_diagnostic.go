package launch

import (
	"bytes"
	"encoding/json"
	"strings"
)

const terraformProviderAWSWirePrefix = "TF_PROVIDER_AWS_DIAGNOSTIC_V1="

type terraformProviderAWSEnvironmentPresence struct {
	AccessKeyID           bool `json:"accessKeyId"`
	SecretAccessKey       bool `json:"secretAccessKey"`
	SessionToken          bool `json:"sessionToken"`
	ConfigFile            bool `json:"configFile"`
	SharedCredentialsFile bool `json:"sharedCredentialsFile"`
}

// terraformProviderAWSDiagnostic contains only finite public evidence from the existing
// source-owned hydrator. Presence means set (even empty), not provider selection.
type terraformProviderAWSDiagnostic struct {
	SchemaVersion      string                                  `json:"schemaVersion"`
	Stage              string                                  `json:"stage"`
	ErrorCode          string                                  `json:"errorCode"`
	FailureClass       string                                  `json:"failureClass"`
	EnvironmentPresent terraformProviderAWSEnvironmentPresence `json:"environmentPresent"`
}

func (d terraformProviderAWSDiagnostic) valid() bool {
	if d.SchemaVersion != "terraform-provider-aws-diagnostic/v1" || (d.Stage != "token" && d.Stage != "account") {
		return false
	}
	classes := map[string]string{
		"ExpiredToken": "expired_token", "ExpiredTokenException": "expired_token",
		"RequestExpired": "request_expired", "AccessDenied": "access_denied", "AccessDeniedException": "access_denied",
		"ResourceNotFoundException": "secret_not_found", "UnrecognizedClientException": "authentication_failure",
		"InvalidClientTokenId": "authentication_failure", "SignatureDoesNotMatch": "authentication_failure",
		"InvalidSignatureException": "authentication_failure", "ValidationException": "request_rejected",
		"ThrottlingException": "service_failure", "InternalServiceError": "service_failure", "ServiceUnavailable": "service_failure",
	}
	if want, ok := classes[d.ErrorCode]; ok {
		return d.FailureClass == want
	}
	if d.ErrorCode != "unknown" {
		return false
	}
	switch d.FailureClass {
	case "unknown", "credential_source_failure", "configuration_failure", "transport_failure":
		return true
	default:
		return false
	}
}

func (d terraformProviderAWSDiagnostic) wireLine() string {
	data, _ := json.Marshal(d)
	return terraformProviderAWSWirePrefix + string(data)
}

// parseTerraformProviderAWSDiagnostic never returns provider text, unknown fields, paths,
// duplicate/missing fields or ambiguous multiple records. Canonical round-trip
// equality is the small internal wire contract shared by the immutable helper.
func parseTerraformProviderAWSDiagnostic(stderr []byte) *terraformProviderAWSDiagnostic {
	var result *terraformProviderAWSDiagnostic
	for _, line := range strings.Split(string(stderr), "\n") {
		if !strings.HasPrefix(line, terraformProviderAWSWirePrefix) {
			continue
		}
		if result != nil {
			return nil
		}
		raw := []byte(strings.TrimPrefix(line, terraformProviderAWSWirePrefix))
		if len(raw) > 1024 {
			return nil
		}
		var diagnostic terraformProviderAWSDiagnostic
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&diagnostic) != nil || !diagnostic.valid() {
			return nil
		}
		canonical, err := json.Marshal(diagnostic)
		if err != nil || !bytes.Equal(raw, canonical) {
			return nil
		}
		result = &diagnostic
	}
	return result
}

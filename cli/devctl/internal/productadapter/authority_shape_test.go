package productadapter

import (
	"encoding/json"
	"strings"
	"testing"
)

func exactManifestEnvelopeFixture(t *testing.T) map[string]any {
	t.Helper()
	sources := make(map[string]any, len(manifestSourceKeys))
	for index, sourceID := range manifestSourceKeys {
		sources[sourceID] = map[string]any{"rev": strings.Repeat(string(rune('0'+index)), 40)}
	}
	return map[string]any{
		"artifactDigests":                        map[string]any{},
		"bundlePath":                             "/nix/store/fixture",
		"codexAuthorization":                     map[string]any{},
		"controllerFleetPath":                    "/nix/store/fixture/bin/fleet",
		"devctlLauncherPath":                     "/nix/store/fixture/bin/devctl",
		"devkitProductAdapter":                   map[string]any{},
		"identityEnvPath":                        "/nix/store/fixture/identity.env",
		"identityJsonPath":                       "/nix/store/fixture/identity.json",
		"launcherPath":                           "/nix/store/fixture/bin/launcher",
		"productRealConvergencePromotionAppPath": "/nix/store/fixture/bin/promote",
		"runtimeIdentity":                        map[string]any{},
		"schemaVersion":                          ManifestSchema,
		"sources":                                sources,
		"sourceEvidence": map[string]any{
			"path": "source-evidence", "schemaVersion": "fixture", "sourceIds": manifestSourceKeys,
			"validationPath": "validation", "wslLockSha256": strings.Repeat("a", 64),
		},
		"nativeControllerStation": map[string]any{
			"guestSystemPath": "system", "interfaceContractPath": "interface", "launcherPath": "launcher",
			"mechanicalContractPath": "mechanical", "prerequisiteContractPath": "prerequisite",
			"readinessPath": "readiness", "runnerPath": "runner", "schemaVersion": "fixture",
		},
	}
}

func marshalManifestEnvelope(t *testing.T, value map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestValidateManifestEnvelopeRequiresSingleExactAuthorityShape(t *testing.T) {
	if err := validateManifestEnvelope(marshalManifestEnvelope(t, exactManifestEnvelopeFixture(t))); err != nil {
		t.Fatal(err)
	}

	for name, mutate := range map[string]func(map[string]any){
		"missing-top-level": func(value map[string]any) { delete(value, "runtimeIdentity") },
		"extra-top-level":   func(value map[string]any) { value["alternateAuthority"] = true },
		"missing-source": func(value map[string]any) {
			delete(value["sources"].(map[string]any), "devkit")
		},
		"extra-source-field": func(value map[string]any) {
			value["sources"].(map[string]any)["devkit"].(map[string]any)["path"] = "forbidden"
		},
		"invalid-source-revision": func(value map[string]any) {
			value["sources"].(map[string]any)["devkit"].(map[string]any)["rev"] = "main"
		},
		"extra-native-authority": func(value map[string]any) {
			value["nativeControllerStation"].(map[string]any)["compiledRevision"] = "forbidden"
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := exactManifestEnvelopeFixture(t)
			mutate(value)
			if err := validateManifestEnvelope(marshalManifestEnvelope(t, value)); err == nil {
				t.Fatal("expected noncanonical authority envelope rejection")
			}
		})
	}
}

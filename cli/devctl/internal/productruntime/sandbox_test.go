package productruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/productadapter"
)

func TestBuildBubblewrapArgsProjectsOnlyTheInheritedSupervisorCapability(t *testing.T) {
	authority := productadapter.Authority{
		Adapter: productadapter.AdapterManifest{
			ProxyHelperPath:    "/nix/store/proxy/bin/product-proxy",
			RuntimeEnvironment: map[string]string{"PATH": "/nix/store/runtime/bin"},
		},
	}
	consumer := productadapter.ConsumerManifest{
		Index:                   1,
		CandidateRoot:           "/candidate",
		SandboxWorktreePath:     "/workspaces/dev",
		SandboxHomePath:         "/home/product",
		SandboxStateRoot:        "/agent-state/product",
		SandboxProxySocketPath:  "/agent-state/product/product-egress.sock",
		SandboxBrokerSocketPath: "/agent-state/product/postgres.sock",
	}
	args, err := buildBubblewrapArgs(authority, consumer, []string{"/nix/store/child"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(args, "--clearenv") {
		t.Fatal("bubblewrap does not clear the caller environment")
	}
	fileIndex := slices.Index(args, "--file")
	if fileIndex < 0 || fileIndex+2 >= len(args) ||
		args[fileIndex+1] != "3" ||
		args[fileIndex+2] != productadapter.SupervisorCapabilityPath {
		t.Fatalf("bubblewrap does not project the exact inherited capability fd: %v", args)
	}
	if fileIndex < 2 ||
		args[fileIndex-2] != "--perms" ||
		args[fileIndex-1] != "0600" {
		t.Fatalf("bubblewrap does not protect the one-shot capability: %v", args)
	}
	if slices.Contains(args, "--preserve-fds") || slices.Contains(args, "--sync-fd") {
		t.Fatal("bubblewrap argv exposes an unsupported inherited-fd surface")
	}
}

func TestDecodeProductReadinessResultRejectsFabricatedOrIncompleteEvidence(t *testing.T) {
	t.Parallel()
	marker := sha256.Sum256([]byte("product-app-server-standalone-command-boundary"))
	valid := productadapter.ProductReadinessResult{
		SchemaVersion:             productadapter.ReadinessSchema,
		AppServerAlive:            true,
		InitializeReady:           true,
		MCPStatusRead:             true,
		EphemeralThreadCreated:    true,
		EphemeralThreadReadBack:   true,
		ApprovalPolicy:            "never",
		SandboxPolicy:             "dangerFullAccess",
		StandaloneExecutable:      "/nix/store/fixture-first-executable",
		StandaloneExecutableExit:  0,
		StandaloneExecutableProof: hex.EncodeToString(marker[:]),
	}
	authority := productadapter.Authority{
		Adapter: productadapter.AdapterManifest{
			FirstExecutablePath: valid.StandaloneExecutable,
		},
	}
	encode := func(value any) []byte {
		t.Helper()
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return append(payload, '\n')
	}
	if _, err := decodeProductReadinessResult(authority, encode(valid)); err != nil {
		t.Fatalf("valid result rejected: %v", err)
	}
	cases := map[string]func(*productadapter.ProductReadinessResult){
		"app-server":       func(value *productadapter.ProductReadinessResult) { value.AppServerAlive = false },
		"initialize":       func(value *productadapter.ProductReadinessResult) { value.InitializeReady = false },
		"mcp":              func(value *productadapter.ProductReadinessResult) { value.MCPStatusRead = false },
		"thread-create":    func(value *productadapter.ProductReadinessResult) { value.EphemeralThreadCreated = false },
		"thread-read-back": func(value *productadapter.ProductReadinessResult) { value.EphemeralThreadReadBack = false },
		"approval":         func(value *productadapter.ProductReadinessResult) { value.ApprovalPolicy = "on-request" },
		"sandbox":          func(value *productadapter.ProductReadinessResult) { value.SandboxPolicy = "workspace-write" },
		"executable":       func(value *productadapter.ProductReadinessResult) { value.StandaloneExecutable += "-other" },
		"exit":             func(value *productadapter.ProductReadinessResult) { value.StandaloneExecutableExit = 1 },
		"proof": func(value *productadapter.ProductReadinessResult) {
			value.StandaloneExecutableProof = strings.Repeat("0", 64)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			value := valid
			mutate(&value)
			if _, err := decodeProductReadinessResult(authority, encode(value)); err == nil {
				t.Fatal("incomplete typed result was accepted")
			}
		})
	}
	var object map[string]any
	if err := json.Unmarshal(encode(valid), &object); err != nil {
		t.Fatal(err)
	}
	object["fabricated_predicate"] = true
	if _, err := decodeProductReadinessResult(authority, encode(object)); err == nil {
		t.Fatal("unknown readiness predicate was accepted")
	}
	if _, err := decodeProductReadinessResult(authority, append(encode(valid), []byte("{}\n")...)); err == nil {
		t.Fatal("trailing readiness output was accepted")
	}
}

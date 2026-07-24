package productadapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func exactConsumerEnvelopeFixture(index int) map[string]any {
	return map[string]any{
		"index": index, "uid": 2000 + index, "gid": 2000 + index,
		"candidateRoot": "/candidate", "agentRoot": "/candidate/agent",
		"worktreePath": "/candidate/worktree", "commonDirPath": "/candidate/common",
		"homePath": "/candidate/home", "stateRoot": "/candidate/state",
		"receiptPath": "/candidate/state/receipt", "proxySocketPath": "/candidate/state/proxy",
		"brokerSocketPath": "/candidate/state/broker", "supervisorSocketPath": "/run/supervisor",
		"appServerSocketPath": "/candidate/state/app", "sandboxWorktreePath": "/worktree",
		"sandboxHomePath": "/home", "sandboxStateRoot": "/state",
		"sandboxProxySocketPath": "/state/proxy", "sandboxBrokerSocketPath": "/state/broker",
		"sandboxAppServerSocketPath": "/state/app", "governanceEnvTarget": "/candidate/state/env",
		"governanceRepoConfigTarget": "/candidate/state/repo", "governanceStateRoot": "/candidate/state/governance",
		"sshIdentityPath": "/candidate/home/.ssh/id", "sshPublicKeyPath": "/candidate/home/.ssh/id.pub",
		"codexAuthPath":      "/candidate/home/.codex/auth.json",
		"authorizedKeysPath": "/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-product-authorized-keys",
		"binds":              []any{map[string]any{"source": "/source", "target": "/target", "mode": "ro", "required": true}},
	}
}

func exactAdapterEnvelopeFixture() map[string]any {
	return map[string]any{
		"schemaVersion": AdapterSchema, "executablePath": "/adapter", "proxyHelperPath": "/proxy",
		"gitPath": "/git", "gitSSHPath": "/git-ssh", "envPath": "/env", "sshPath": "/ssh", "sshKeygenPath": "/ssh-keygen",
		"knownHostsPath": "/known-hosts", "runtimeLauncherPath": "/runtime", "bubblewrapPath": "/bwrap",
		"brokerPath": "/broker", "egressAllowlistPath": "/allowlist",
		"codexConfigPath":   "/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-codex-config.toml",
		"governanceEnvPath": "/governance-env", "governanceRepoConfigPath": "/governance-repo",
		"governanceRulesPath": "/rules", "shellHookPath": "/shell", "codexExecutablePath": "/codex",
		"readinessExecutablePath": "/readiness", "mcpRequirementPath": "/mcp",
		"mountPolicyContractPath": "/mount-policy", "supervisorExecutablePath": "/supervisor",
		"sshSessionExecutablePath": "/ssh-session", "sshSetupExecutablePath": "/ssh-setup",
		"codexAuthSeed1ExecutablePath": "/codex-auth-seed-1",
		"codexAuthSeed2ExecutablePath": "/codex-auth-seed-2",
		"sshSetupContractPath":         "/ssh-setup-contract", "sshSessionContractPath": "/ssh-session-contract",
		"productOrigin": "ssh://git@example/product", "controllerCredentialOwnerUid": 1000,
		"count": 1, "baseBranch": "main", "branchPrefix": "agent", "upstreamProxyUrl": "",
		"resolvConfPath": "/resolv.conf", "nscdSocketPath": "", "mountPolicyIdentity": "devkit/workspace-egress/v3",
		"runtimeEnvironment": map[string]any{}, "consumers": []any{exactConsumerEnvelopeFixture(1)},
		"artifactDigests": map[string]any{"codex_config": strings.Repeat("e", 64)},
	}
}

func exactRuntimeIdentityEnvelopeFixture() map[string]any {
	jar := func(digest string) map[string]any {
		return map[string]any{"packagePath": "/package", "jarPath": "/package.jar", "jarSha256": digest}
	}
	return map[string]any{
		"governance":      jar(strings.Repeat("b", 64)),
		"submitToCi":      jar(strings.Repeat("d", 64)),
		"sbtControlPlane": jar(strings.Repeat("c", 64)),
		"javaHome":        "/java",
		"artifactColumnPlugin": map[string]any{
			"repositoryPath": "/repository", "metadataEnv": "/metadata", "version": "1",
			"ivyPath": "ivy", "jarSha256": strings.Repeat("a", 64), "smokeEvidence": "/smoke",
		},
	}
}

func exactManifestEnvelopeFixture(t *testing.T) map[string]any {
	t.Helper()
	sources := make(map[string]any, len(manifestSourceKeys))
	for index, sourceID := range manifestSourceKeys {
		sources[sourceID] = map[string]any{"rev": strings.Repeat(string(rune('0'+index)), 40)}
	}
	return map[string]any{
		"artifactDigests": map[string]any{
			"artifactColumnPlugin": strings.Repeat("a", 64),
			"governance":           strings.Repeat("b", 64),
			"sbtControlPlane":      strings.Repeat("c", 64),
			"submitToCi":           strings.Repeat("d", 64),
		},
		"bundlePath": "/nix/store/fixture",
		"codexAuthorization": map[string]any{
			"configPath":   "/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-codex-config.toml",
			"configSha256": strings.Repeat("e", 64),
			"systemPath":   "/etc/codex/config.toml",
		},
		"controllerSourceLayer": map[string]any{
			"controllerDevctlPath": "/nix/store/fixture/bin/devctl",
			"controllerFleetPath":  "/nix/store/fixture/bin/controller-fleet",
			"controllerGUIInventory": map[string]any{
				"path": "/nix/store/gui-inventory.json", "sha256": strings.Repeat("3", 64),
			},
			"controllerSourceInventory": map[string]any{
				"path": "/nix/store/source-inventory.json", "sha256": strings.Repeat("4", 64),
			},
			"launcherPath":   "/nix/store/fixture/bin/fleet-source-layer",
			"manifestPath":   "/nix/store/fixture/share/fleet-controller-source-layer/manifest.json",
			"manifestSha256": strings.Repeat("1", 64),
			"packagePath":    "/nix/store/fixture",
			"packageSha256":  strings.Repeat("2", 64),
		},
		"devkitProductAdapter": exactAdapterEnvelopeFixture(),
		"identityEnvPath":      "/nix/store/fixture/identity.env",
		"identityJsonPath":     "/nix/store/fixture/identity.json",
		"launcherPath":         "/nix/store/fixture/bin/launcher",
		"runtimeIdentity":      exactRuntimeIdentityEnvelopeFixture(),
		"schemaVersion":        ManifestSchema,
		"sources":              sources,
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
		"missing-artifact-digest": func(value map[string]any) {
			delete(value["artifactDigests"].(map[string]any), "governance")
		},
		"extra-artifact-digest-unknown-field": func(value map[string]any) {
			value["artifactDigests"].(map[string]any)["alternateAuthority"] = strings.Repeat("1", 64)
		},
		"extra-artifact-digest-compiled-revision": func(value map[string]any) {
			value["artifactDigests"].(map[string]any)["compiledRevision"] = strings.Repeat("1", 40)
		},
		"artifact-digest-object-type": func(value map[string]any) {
			value["artifactDigests"].(map[string]any)["governance"] = map[string]any{
				"compiledRevision": strings.Repeat("1", 40),
			}
		},
		"artifact-digest-array-type": func(value map[string]any) {
			value["artifactDigests"].(map[string]any)["governance"] = []any{
				map[string]any{"unknownAuthority": true},
			}
		},
		"artifact-digest-invalid-hex": func(value map[string]any) {
			value["artifactDigests"].(map[string]any)["governance"] = strings.Repeat("g", 64)
		},
		"artifact-digest-invalid-length": func(value map[string]any) {
			value["artifactDigests"].(map[string]any)["governance"] = strings.Repeat("a", 63)
		},
		"artifact-digest-surrounding-whitespace": func(value map[string]any) {
			value["artifactDigests"].(map[string]any)["governance"] = " " + strings.Repeat("a", 64)
		},
		"artifact-digest-valid-but-wrong-runtime-value": func(value map[string]any) {
			value["artifactDigests"].(map[string]any)["governance"] = strings.Repeat("f", 64)
		},
		"missing-codex-authorization-field": func(value map[string]any) {
			delete(value["codexAuthorization"].(map[string]any), "systemPath")
		},
		"extra-codex-authorization-unknown-field": func(value map[string]any) {
			value["codexAuthorization"].(map[string]any)["alternateAuthority"] = true
		},
		"extra-codex-authorization-compiled-revision": func(value map[string]any) {
			value["codexAuthorization"].(map[string]any)["compiledRevision"] = strings.Repeat("1", 40)
		},
		"codex-authorization-config-object-type": func(value map[string]any) {
			value["codexAuthorization"].(map[string]any)["configPath"] = map[string]any{
				"compiledRevision": strings.Repeat("1", 40),
			}
		},
		"codex-authorization-config-array-type": func(value map[string]any) {
			value["codexAuthorization"].(map[string]any)["configPath"] = []any{
				map[string]any{"unknownAuthority": true},
			}
		},
		"codex-authorization-config-non-store-path": func(value map[string]any) {
			value["codexAuthorization"].(map[string]any)["configPath"] = "/tmp/config.toml"
		},
		"codex-authorization-valid-but-wrong-config-path": func(value map[string]any) {
			value["codexAuthorization"].(map[string]any)["configPath"] =
				"/nix/store/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-other-codex-config.toml"
		},
		"codex-authorization-invalid-digest": func(value map[string]any) {
			value["codexAuthorization"].(map[string]any)["configSha256"] = strings.Repeat("z", 64)
		},
		"codex-authorization-valid-but-wrong-config-digest": func(value map[string]any) {
			value["codexAuthorization"].(map[string]any)["configSha256"] = strings.Repeat("f", 64)
		},
		"codex-authorization-relative-system-path": func(value map[string]any) {
			value["codexAuthorization"].(map[string]any)["systemPath"] = "etc/codex/config.toml"
		},
		"codex-authorization-valid-absolute-but-wrong-system-path": func(value map[string]any) {
			value["codexAuthorization"].(map[string]any)["systemPath"] = "/etc/codex/other.toml"
		},
		"extra-adapter-compiled-revision": func(value map[string]any) {
			value["devkitProductAdapter"].(map[string]any)["compiledRevision"] = strings.Repeat("1", 40)
		},
		"missing-fixed-slot-codex-auth-seeder": func(value map[string]any) {
			delete(value["devkitProductAdapter"].(map[string]any), "codexAuthSeed2ExecutablePath")
		},
		"extra-runtime-identity": func(value map[string]any) {
			value["runtimeIdentity"].(map[string]any)["launcherIdentity"] = "forbidden"
		},
		"extra-nested-runtime-revision": func(value map[string]any) {
			value["runtimeIdentity"].(map[string]any)["governance"].(map[string]any)["compiledRevision"] = strings.Repeat("2", 40)
		},
		"extra-consumer-identity": func(value map[string]any) {
			adapter := value["devkitProductAdapter"].(map[string]any)
			adapter["consumers"].([]any)[0].(map[string]any)["runtimeIdentity"] = "forbidden"
		},
		"extra-bind-authority": func(value map[string]any) {
			adapter := value["devkitProductAdapter"].(map[string]any)
			consumer := adapter["consumers"].([]any)[0].(map[string]any)
			consumer["binds"].([]any)[0].(map[string]any)["authority"] = "forbidden"
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

func TestCodexAuthorizationDigestRemainsTransitivelyBoundToImmutableBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	payload := []byte("approval_policy = \"never\"\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateArtifactDigest("codex_config", path, sha256Hex(payload)); err != nil {
		t.Fatalf("matching Codex authorization bytes were rejected: %v", err)
	}
	if err := validateArtifactDigest("codex_config", path, strings.Repeat("f", 64)); err == nil {
		t.Fatal("valid but wrong Codex authorization digest was not checked against immutable bytes")
	}
}

func consumerGeometryFixture(index int, root, supervisorRoot string) ConsumerManifest {
	agent := filepath.Join(root, "agent"+strconv.Itoa(index))
	home := filepath.Join(root, "home")
	state := filepath.Join(root, "state")
	return ConsumerManifest{
		Index: index, UID: 2000 + index, GID: 2000 + index,
		CandidateRoot: root, AgentRoot: agent, WorktreePath: filepath.Join(agent, ProductRepo),
		CommonDirPath: filepath.Join(agent, ".devkit", "git", ProductRepo+".git"),
		HomePath:      home, StateRoot: state, ReceiptPath: filepath.Join(state, "receipt.json"),
		ProxySocketPath: filepath.Join(state, "proxy.sock"), BrokerSocketPath: filepath.Join(state, "broker.sock"),
		SupervisorSocketPath: filepath.Join(supervisorRoot, "control.sock"),
		AppServerSocketPath:  filepath.Join(state, "app.sock"), GovernanceEnvTarget: filepath.Join(state, "governance.env"),
		GovernanceRepoConfigTarget: filepath.Join(state, "governance.json"), GovernanceStateRoot: filepath.Join(state, "governance"),
		SSHIdentityPath:  filepath.Join(home, ".ssh", "id_ed25519"),
		SSHPublicKeyPath: filepath.Join(home, ".ssh", "id_ed25519.pub"),
		CodexAuthPath:    filepath.Join(home, ".codex", "auth.json"),
		AuthorizedKeysPath: filepath.Join(
			"/nix/store",
			strings.Repeat(string(rune('a'+index-1)), 32)+"-product-authorized-keys",
		),
	}
}

func TestValidateCrossConsumerGeometryRejectsEqualityAndNestingForEveryMutableField(t *testing.T) {
	left := consumerGeometryFixture(1, "/var/lib/product-a", "/run/product-supervisor-a")
	rightBaseline := consumerGeometryFixture(2, "/var/lib/product-b", "/run/product-supervisor-b")
	if err := validateCrossConsumerGeometry([]ConsumerManifest{left, rightBaseline}); err != nil {
		t.Fatalf("independent cross-consumer geometry was rejected: %v", err)
	}
	setters := map[string]func(*ConsumerManifest, string){
		"candidate":         func(value *ConsumerManifest, path string) { value.CandidateRoot = path },
		"agent":             func(value *ConsumerManifest, path string) { value.AgentRoot = path },
		"worktree":          func(value *ConsumerManifest, path string) { value.WorktreePath = path },
		"common":            func(value *ConsumerManifest, path string) { value.CommonDirPath = path },
		"home":              func(value *ConsumerManifest, path string) { value.HomePath = path },
		"state":             func(value *ConsumerManifest, path string) { value.StateRoot = path },
		"receipt":           func(value *ConsumerManifest, path string) { value.ReceiptPath = path },
		"proxy":             func(value *ConsumerManifest, path string) { value.ProxySocketPath = path },
		"broker":            func(value *ConsumerManifest, path string) { value.BrokerSocketPath = path },
		"supervisor":        func(value *ConsumerManifest, path string) { value.SupervisorSocketPath = path },
		"app-server":        func(value *ConsumerManifest, path string) { value.AppServerSocketPath = path },
		"governance-env":    func(value *ConsumerManifest, path string) { value.GovernanceEnvTarget = path },
		"governance-config": func(value *ConsumerManifest, path string) { value.GovernanceRepoConfigTarget = path },
		"governance-state":  func(value *ConsumerManifest, path string) { value.GovernanceStateRoot = path },
		"ssh-private":       func(value *ConsumerManifest, path string) { value.SSHIdentityPath = path },
		"ssh-public":        func(value *ConsumerManifest, path string) { value.SSHPublicKeyPath = path },
		"codex-auth":        func(value *ConsumerManifest, path string) { value.CodexAuthPath = path },
	}
	leftPaths := mutableConsumerGeometry(left)
	for name, set := range setters {
		base, ok := leftPaths[map[string]string{
			"candidate": "candidate root", "agent": "agent root", "worktree": "worktree",
			"common": "common directory", "home": "home", "state": "state root", "receipt": "receipt",
			"proxy": "proxy socket", "broker": "broker socket", "supervisor": "supervisor socket",
			"app-server": "app-server socket", "governance-env": "governance env",
			"governance-config": "governance config", "governance-state": "governance state",
			"ssh-private": "SSH private identity", "ssh-public": "SSH public identity",
			"codex-auth": "Codex auth",
		}[name]]
		if !ok {
			t.Fatalf("missing left path for %s", name)
		}
		for relation, path := range map[string]string{
			"equal":      base,
			"descendant": filepath.Join(base, "nested"),
			"ancestor":   filepath.Dir(base),
		} {
			t.Run(name+"/"+relation, func(t *testing.T) {
				right := consumerGeometryFixture(2, "/var/lib/product-b", "/run/product-supervisor-b")
				set(&right, path)
				if err := validateCrossConsumerGeometry([]ConsumerManifest{left, right}); err == nil {
					t.Fatal("cross-consumer mutable geometry overlap was accepted")
				}
			})
		}
	}
}

func TestValidateCrossConsumerGeometryRejectsMutableOrSharedSSHAdmission(t *testing.T) {
	left := consumerGeometryFixture(1, "/var/lib/product-a", "/run/product-supervisor-a")
	right := consumerGeometryFixture(2, "/var/lib/product-b", "/run/product-supervisor-b")

	right.AuthorizedKeysPath = filepath.Join(left.CandidateRoot, "authorized_keys")
	if err := validateCrossConsumerGeometry([]ConsumerManifest{left, right}); err == nil {
		t.Fatal("immutable SSH admission inside another consumer root was accepted")
	}

	right = consumerGeometryFixture(2, "/var/lib/product-b", "/run/product-supervisor-b")
	right.AuthorizedKeysPath = left.AuthorizedKeysPath
	if err := validateCrossConsumerGeometry([]ConsumerManifest{left, right}); err == nil {
		t.Fatal("shared immutable SSH admission path was accepted")
	}
}

func TestOnlyInertBootstrapRolesMayLoadBeforeCredentialSeeding(t *testing.T) {
	for _, test := range []struct {
		role Role
		want bool
	}{
		{role: RoleAdapter, want: true},
		{role: RoleProxy, want: true},
		{role: RoleSupervisor, want: false},
		{role: RoleSSHSession, want: true},
		{role: RoleSSHSetup, want: false},
		{role: RoleCodexAuthSeed1, want: false},
		{role: RoleCodexAuthSeed2, want: false},
	} {
		if got := roleRequiresSelectedCredentialHandlesAtLoad(test.role); got != test.want {
			t.Fatalf(
				"roleRequiresSelectedCredentialHandlesAtLoad(%q) = %t, want %t",
				test.role,
				got,
				test.want,
			)
		}
	}
}

func TestCodexAuthSeedRolesHaveOneFixedConsumerIdentity(t *testing.T) {
	for _, test := range []struct {
		role Role
		want int
	}{
		{role: RoleCodexAuthSeed1, want: 1},
		{role: RoleCodexAuthSeed2, want: 2},
	} {
		got, ok := FixedConsumerIndex(test.role)
		if !ok || got != test.want {
			t.Fatalf("FixedConsumerIndex(%q) = %d, %t; want %d, true", test.role, got, ok, test.want)
		}
		if !isControllerSeedRole(test.role) {
			t.Fatalf("fixed-slot role %q did not require controller admission", test.role)
		}
	}
	for _, role := range []Role{RoleAdapter, RoleProxy, RoleSupervisor, RoleSSHSession, RoleSSHSetup} {
		if got, ok := FixedConsumerIndex(role); ok || got != 0 {
			t.Fatalf("non-fixed role %q acquired consumer identity %d", role, got)
		}
	}
}

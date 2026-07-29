package launch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
	"devkit/cli/devctl/internal/gitauthority"
	"devkit/cli/devctl/internal/runtime/agent"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/sshauthority"
)

func TestMain(m *testing.M) {
	gitExecutable, err := exec.LookPath("git")
	if err != nil {
		panic(err)
	}
	restoreGit := gitauthority.SetExecutableForTesting(gitExecutable)
	defer restoreGit()
	testExecutable, err := os.Executable()
	if err != nil {
		panic(err)
	}
	knownHosts, err := os.CreateTemp("", "devkit-launch-known-hosts-")
	if err != nil {
		panic(err)
	}
	knownHostsPath := knownHosts.Name()
	if _, err := knownHosts.WriteString("[ssh.github.com]:443 ssh-ed25519 fixture\n"); err != nil {
		panic(err)
	}
	if err := knownHosts.Close(); err != nil {
		panic(err)
	}
	defer os.Remove(knownHostsPath)
	testSSHAuthority, err := sshauthority.New(testExecutable, knownHostsPath)
	if err != nil {
		panic(err)
	}
	resolvePackageSSHAuthority = func() (sshauthority.Authority, error) {
		return testSSHAuthority, nil
	}
	os.Exit(m.Run())
}

func TestConfigureWorktreeGitSSHUsesPackageGitUnderHostilePath(t *testing.T) {
	worktree := filepath.Join(t.TempDir(), "worktree")
	gitExecutable := gitauthority.Executable()
	command := exec.Command(gitExecutable, "init", "--initial-branch=main", worktree)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	originalPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", filepath.Join(t.TempDir(), "hostile-empty-path")); err != nil {
		t.Fatal(err)
	}
	configureErr := configureWorktreeGitSSH(worktree, "/nix/store/example-openssh/bin/ssh -F /tmp/config")
	if err := os.Setenv("PATH", originalPath); err != nil {
		t.Fatal(err)
	}
	if configureErr != nil {
		t.Fatalf("configureWorktreeGitSSH under hostile PATH: %v", configureErr)
	}
	output, err := exec.Command(gitExecutable, "-C", worktree, "config", "--worktree", "--get", "core.sshCommand").CombinedOutput()
	if err != nil {
		t.Fatalf("read worktree SSH command: %v\n%s", err, output)
	}
	if got, want := strings.TrimSpace(string(output)), "/nix/store/example-openssh/bin/ssh -F /tmp/config"; got != want {
		t.Fatalf("core.sshCommand = %q, want %q", got, want)
	}
}

func withProductGovernanceEnvironmentFixture(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	storeRoot := filepath.Join(root, "nix", "store")
	packageRoot := filepath.Join(storeRoot, "aaaaaaaa-native-product-governance")
	storeFile := filepath.Join(packageRoot, "product-governance.env")
	if err := os.MkdirAll(packageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storeFile, []byte("export FIXTURE=1\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "run", "current-system", "etc", "fleet", "product-governance.env")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(storeFile, source); err != nil {
		t.Fatal(err)
	}
	previousSource := nativeplan.WorkspaceProductGovernanceEnvSource
	previousStoreRoot := nativeplan.WorkspaceProductGovernanceStoreRoot
	nativeplan.WorkspaceProductGovernanceEnvSource = source
	nativeplan.WorkspaceProductGovernanceStoreRoot = storeRoot
	t.Cleanup(func() {
		nativeplan.WorkspaceProductGovernanceEnvSource = previousSource
		nativeplan.WorkspaceProductGovernanceStoreRoot = previousStoreRoot
	})
}

func TestWorkspaceControllerCapabilityValidationRejectsSymlinkAndWrongSocketMode(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "devkit-exec-handle-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "control.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := os.Chmod(socket, 0o600); err != nil {
		t.Fatal(err)
	}
	previousExecSocket := nativeplan.WorkspaceControllerExecSocket
	nativeplan.WorkspaceControllerExecSocket = socket
	t.Cleanup(func() { nativeplan.WorkspaceControllerExecSocket = previousExecSocket })
	if err := validateWorkspaceControllerCapability(socket, socket); err != nil {
		t.Fatalf("valid protected exec socket rejected: %v", err)
	}
	if err := os.Chmod(socket, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := validateWorkspaceControllerCapability(socket, socket); err == nil {
		t.Fatal("wrong exec socket mode must fail closed")
	}
	symlink := filepath.Join(root, "control-link.sock")
	if err := os.Symlink(socket, symlink); err != nil {
		t.Fatal(err)
	}
	nativeplan.WorkspaceControllerExecSocket = symlink
	if err := validateWorkspaceControllerCapability(symlink, symlink); err == nil {
		t.Fatal("symlink exec socket must fail closed")
	}
}

func TestWorkspaceControllerCapabilityValidationRequiresRegularInventory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "inventory")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateWorkspaceControllerCapability(dir, "/etc/fleet/source/fleet-inventory.json"); err == nil {
		t.Fatal("directory inventory must fail closed")
	}
}

func TestWorkspaceControllerCapabilityBindingRejectsOverridesAndWritableMode(t *testing.T) {
	root := t.TempDir()
	socket := filepath.Join(root, "control.sock")
	if err := os.WriteFile(socket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	previousExecSocket := nativeplan.WorkspaceControllerExecSocket
	nativeplan.WorkspaceControllerExecSocket = socket
	t.Cleanup(func() { nativeplan.WorkspaceControllerExecSocket = previousExecSocket })
	if err := validateWorkspaceControllerCapabilityBinding(socket, socket, "rw"); err == nil {
		t.Fatal("writable controller capability must fail closed")
	}
	if err := validateWorkspaceControllerCapabilityBinding(filepath.Join(root, "other"), socket, "ro"); err == nil {
		t.Fatal("caller source override must fail closed")
	}
}

func TestProductGovernanceEnvironmentProjectionAcceptsOnlyImmutableCompiledSource(t *testing.T) {
	root := t.TempDir()
	storeRoot := filepath.Join(root, "nix", "store")
	packageRoot := filepath.Join(storeRoot, "aaaaaaaa-native-product-governance")
	storeFile := filepath.Join(packageRoot, "product-governance.env")
	if err := os.MkdirAll(packageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storeFile, []byte("export FIXTURE=1\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "run", "current-system", "etc", "fleet", "product-governance.env")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(storeFile, source); err != nil {
		t.Fatal(err)
	}
	previousSource := nativeplan.WorkspaceProductGovernanceEnvSource
	previousStoreRoot := nativeplan.WorkspaceProductGovernanceStoreRoot
	nativeplan.WorkspaceProductGovernanceEnvSource = source
	nativeplan.WorkspaceProductGovernanceStoreRoot = storeRoot
	t.Cleanup(func() {
		nativeplan.WorkspaceProductGovernanceEnvSource = previousSource
		nativeplan.WorkspaceProductGovernanceStoreRoot = previousStoreRoot
	})
	valid := nativeplan.Plan{
		Agent:            agent.Spec{ID: agent.ID{Project: "dev-all", Repo: "ouroboros-ide"}},
		IsolationProfile: nativeplan.IsolationProfileWorkspaceEgress,
		Binds: []nativeplan.Bind{{
			Source: source, Target: nativeplan.WorkspaceProductGovernanceEnvTarget, Mode: "ro", Required: true,
		}},
	}
	if err := validateProductGovernanceEnvironmentPlan(valid); err != nil {
		t.Fatalf("valid immutable governance projection rejected: %v", err)
	}

	for name, mutate := range map[string]func(nativeplan.Plan) nativeplan.Plan{
		"absent": func(p nativeplan.Plan) nativeplan.Plan {
			p.Binds = nil
			return p
		},
		"source override": func(p nativeplan.Plan) nativeplan.Plan {
			p.Binds = append([]nativeplan.Bind(nil), p.Binds...)
			p.Binds[0].Source = filepath.Join(root, "caller.env")
			return p
		},
		"writable": func(p nativeplan.Plan) nativeplan.Plan {
			p.Binds = append([]nativeplan.Bind(nil), p.Binds...)
			p.Binds[0].Mode = "rw"
			return p
		},
		"optional": func(p nativeplan.Plan) nativeplan.Plan {
			p.Binds = append([]nativeplan.Bind(nil), p.Binds...)
			p.Binds[0].Required = false
			return p
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateProductGovernanceEnvironmentPlan(mutate(valid)); err == nil {
				t.Fatalf("%s projection must fail closed", name)
			}
		})
	}

	if err := os.Chmod(storeFile, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateProductGovernanceEnvironmentPlan(valid); err == nil ||
		!strings.Contains(err.Error(), "immutable regular file") {
		t.Fatalf("writable store governance source must fail closed, got %v", err)
	}
	if err := os.Chmod(storeFile, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(packageRoot, source); err != nil {
		t.Fatal(err)
	}
	if err := validateProductGovernanceEnvironmentPlan(valid); err == nil ||
		!strings.Contains(err.Error(), "immutable regular file") {
		t.Fatalf("directory governance source must fail closed, got %v", err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	mutable := filepath.Join(root, "mutable.env")
	if err := os.WriteFile(mutable, []byte("mutable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(mutable, source); err != nil {
		t.Fatal(err)
	}
	if err := validateProductGovernanceEnvironmentPlan(valid); err == nil ||
		!strings.Contains(err.Error(), "beneath") {
		t.Fatalf("mutable governance source must fail closed, got %v", err)
	}
}

func testPackageSSHCommand(t *testing.T, configPath string) string {
	t.Helper()
	authority, err := resolvePackageSSHAuthority()
	if err != nil {
		t.Fatal(err)
	}
	command, err := authority.Command(configPath)
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func devkitRootFromPackage(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "..")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("resolve devkit root: %v", err)
	}
	return abs
}

func initTestGitWorktree(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir worktree parent: %v", err)
	}
	runTestCommand(t, "", "git", "init", path)
}

type managementSkillsFixture struct {
	PackagePath string
	SkillsRoot  string
	SourceRev   string
	PrimarySHA  string
}

func writeManagementSkillsFixture(t *testing.T, storeRoot, packageName, sourceRev string, files map[string]string) managementSkillsFixture {
	t.Helper()
	packagePath := filepath.Join(storeRoot, packageName)
	skillsRoot := filepath.Join(packagePath, "share", "management-runtime-skills", "skills")
	manifest := make([]managementRuntimeSkillFile, 0, len(files))
	for rel, content := range files {
		writeTestFile(t, filepath.Join(skillsRoot, filepath.FromSlash(rel)), content)
		digest := sha256.Sum256([]byte(content))
		manifest = append(manifest, managementRuntimeSkillFile{Path: rel, SHA256: hex.EncodeToString(digest[:])})
	}
	sort.Slice(manifest, func(i, j int) bool { return manifest[i].Path < manifest[j].Path })
	primaryPath := "fleet-health-hypervisor/SKILL.md"
	primarySHA := ""
	for _, file := range manifest {
		if file.Path == primaryPath {
			primarySHA = file.SHA256
			break
		}
	}
	if primarySHA == "" {
		t.Fatalf("fixture requires %s", primaryPath)
	}
	identity := managementRuntimeSkillsIdentity{
		SchemaVersion:       managementRuntimeSkillsIdentitySchema,
		ManagementSourceRev: sourceRev,
		PackagePath:         packagePath,
		SkillsRoot:          skillsRoot,
		SkillPath:           primaryPath,
		SkillSHA256:         primarySHA,
		Files:               manifest,
	}
	data, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(filepath.Dir(skillsRoot), "identity.json"), string(data)+"\n")
	return managementSkillsFixture{
		PackagePath: packagePath,
		SkillsRoot:  skillsRoot,
		SourceRev:   sourceRev,
		PrimarySHA:  primarySHA,
	}
}

func prepareManagementSkillsFixture(t *testing.T, fixture managementSkillsFixture, devRoot, hostHome string) nativeplan.Plan {
	t.Helper()
	t.Setenv("DEVKIT_MANAGEMENT_SKILLS_ROOT", fixture.SkillsRoot)
	t.Setenv("DEVKIT_MANAGEMENT_SOURCE_REV", fixture.SourceRev)
	t.Setenv("DEVKIT_MANAGEMENT_SKILL_SHA256", fixture.PrimarySHA)
	t.Setenv("DEVKIT_CODEX_CONFIG_SOURCE", "")
	t.Setenv("CODEX_AUTH_JSON", filepath.Join(t.TempDir(), "missing-auth.json"))
	t.Setenv("DEVKIT_AWS_HOME", filepath.Join(t.TempDir(), "missing-aws"))
	t.Setenv("HOME", filepath.Join(t.TempDir(), "empty-home"))
	worktree := filepath.Join(devRoot, "control-plane-worktrees", "agent2", "shadow-throne-management")
	initTestGitWorktree(t, worktree)
	return nativeplan.Plan{
		Agent: agent.Spec{
			ID:              agent.ID{Project: "dev-workspace", Index: 2, Repo: "shadow-throne-management"},
			HostWorktree:    worktree,
			SandboxWorktree: "/workspace",
			HostHome:        hostHome,
			SandboxHome:     "/agent-state/dev-workspace-agent2/home",
			StateRoot:       filepath.Join(devRoot, ".devkit", "native-agents", "dev-workspace-agent2"),
		},
		DevkitHostRoot: filepath.Join(devRoot, "devkit"),
		Binds: []nativeplan.Bind{
			{Source: devRoot, Target: "/workspaces/dev", Mode: "rw"},
		},
		Env: map[string]string{},
	}
}

func withManagementSkillsStoreRoot(t *testing.T) string {
	t.Helper()
	storeRoot := filepath.Join(t.TempDir(), "nix", "store")
	if err := os.MkdirAll(storeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	previous := managementRuntimeSkillsStoreRoot
	managementRuntimeSkillsStoreRoot = storeRoot
	t.Cleanup(func() { managementRuntimeSkillsStoreRoot = previous })
	return storeRoot
}

func TestPrepareLinksImmutableManagementSkillsBeforeProductSkills(t *testing.T) {
	storeRoot := withManagementSkillsStoreRoot(t)
	fixture := writeManagementSkillsFixture(t, storeRoot, "aaaaaaaa-management-skills", strings.Repeat("a", 40), map[string]string{
		"fleet-health-hypervisor/SKILL.md": "immutable management\n",
		"shared-name/SKILL.md":             "immutable shared\n",
	})
	devRoot := filepath.Join(t.TempDir(), "dev")
	writeTestFile(t, filepath.Join(devRoot, ".codex", "skills", "shared-name", "SKILL.md"), "mutable management\n")
	writeTestFile(t, filepath.Join(devRoot, "ouroboros-ide", ".codex", "skills", "shared-name", "SKILL.md"), "mutable Product collision\n")
	writeTestFile(t, filepath.Join(devRoot, "ouroboros-ide", ".codex", "skills", "product-only", "SKILL.md"), "Product only\n")
	hostHome := filepath.Join(t.TempDir(), "agent-home")
	p := prepareManagementSkillsFixture(t, fixture, devRoot, hostHome)

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	for name, want := range map[string]string{
		"fleet-health-hypervisor": filepath.Join(fixture.SkillsRoot, "fleet-health-hypervisor"),
		"shared-name":             filepath.Join(fixture.SkillsRoot, "shared-name"),
		"product-only":            filepath.Join(devRoot, "ouroboros-ide", ".codex", "skills", "product-only"),
	} {
		got, err := os.Readlink(filepath.Join(hostHome, ".codex", "skills", name))
		if err != nil {
			t.Fatalf("read %s link: %v", name, err)
		}
		if got != want {
			t.Fatalf("%s link = %q, want %q", name, got, want)
		}
	}
	var receipt managementRuntimeSkillsReceipt
	if err := json.Unmarshal([]byte(readTestFile(t, filepath.Join(hostHome, ".codex", "management-runtime-skills.json"))), &receipt); err != nil {
		t.Fatalf("decode receipt: %v", err)
	}
	if receipt.SchemaVersion != managementRuntimeSkillsReceiptSchema ||
		receipt.ManagementSourceRev != fixture.SourceRev ||
		receipt.SkillsRoot != fixture.SkillsRoot ||
		strings.TrimSpace(receipt.IdentitySHA256) == "" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
}

func TestPrepareTransitionsImmutableManagementSkillsAndRemovesStaleOwnedLinks(t *testing.T) {
	storeRoot := withManagementSkillsStoreRoot(t)
	first := writeManagementSkillsFixture(t, storeRoot, "aaaaaaaa-management-skills", strings.Repeat("a", 40), map[string]string{
		"fleet-health-hypervisor/SKILL.md": "first\n",
		"removed-skill/SKILL.md":           "removed\n",
	})
	devRoot := filepath.Join(t.TempDir(), "dev")
	hostHome := filepath.Join(t.TempDir(), "agent-home")
	p := prepareManagementSkillsFixture(t, first, devRoot, hostHome)
	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare first package: %v", err)
	}
	legacyReceipt := map[string]string{
		"schemaVersion":       managementRuntimeSkillsReceiptSchema,
		"managementSourceRev": first.SourceRev,
		"skillsRoot":          first.SkillsRoot,
		"skillPath":           "fleet-skill-supply-chain/SKILL.md",
		"skillSha256":         strings.Repeat("a", 64),
	}
	legacyReceiptBytes, err := json.MarshalIndent(legacyReceipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(hostHome, ".codex", "management-runtime-skills.json"), string(legacyReceiptBytes)+"\n")

	second := writeManagementSkillsFixture(t, storeRoot, "bbbbbbbb-management-skills", strings.Repeat("b", 40), map[string]string{
		"fleet-health-hypervisor/SKILL.md": "second\n",
		"new-skill/SKILL.md":               "new\n",
	})
	t.Setenv("DEVKIT_MANAGEMENT_SKILLS_ROOT", second.SkillsRoot)
	t.Setenv("DEVKIT_MANAGEMENT_SOURCE_REV", second.SourceRev)
	t.Setenv("DEVKIT_MANAGEMENT_SKILL_SHA256", second.PrimarySHA)
	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare transitioned package: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(hostHome, ".codex", "skills", "removed-skill")); !os.IsNotExist(err) {
		t.Fatalf("stale owned skill link remains: %v", err)
	}
	for _, name := range []string{"fleet-health-hypervisor", "new-skill"} {
		got, err := os.Readlink(filepath.Join(hostHome, ".codex", "skills", name))
		if err != nil {
			t.Fatalf("read transitioned %s link: %v", name, err)
		}
		if want := filepath.Join(second.SkillsRoot, name); got != want {
			t.Fatalf("transitioned %s link = %q, want %q", name, got, want)
		}
	}
	currentReceipt := readTestFile(t, filepath.Join(hostHome, ".codex", "management-runtime-skills.json"))
	if strings.Contains(currentReceipt, `"skillPath"`) || strings.Contains(currentReceipt, `"skillSha256"`) {
		t.Fatalf("legacy receipt fields survived convergence: %s", currentReceipt)
	}
}

func TestPrepareRejectsMissingOrMismatchedImmutableManagementSkills(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, fixture managementSkillsFixture)
		want   string
	}{
		{
			name: "missing input",
			mutate: func(t *testing.T, fixture managementSkillsFixture) {
				t.Setenv("DEVKIT_MANAGEMENT_SKILLS_ROOT", "")
			},
			want: "requires DEVKIT_MANAGEMENT_SKILLS_ROOT",
		},
		{
			name: "environment primary hash mismatch",
			mutate: func(t *testing.T, fixture managementSkillsFixture) {
				t.Setenv("DEVKIT_MANAGEMENT_SKILL_SHA256", strings.Repeat("0", 64))
			},
			want: "primary skill SHA-256",
		},
		{
			name: "content manifest mismatch",
			mutate: func(t *testing.T, fixture managementSkillsFixture) {
				writeTestFile(t, filepath.Join(fixture.SkillsRoot, "fleet-health-hypervisor", "SKILL.md"), "corrupt\n")
			},
			want: "manifest SHA-256",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storeRoot := withManagementSkillsStoreRoot(t)
			fixture := writeManagementSkillsFixture(t, storeRoot, "aaaaaaaa-management-skills", strings.Repeat("a", 40), map[string]string{
				"fleet-health-hypervisor/SKILL.md": "immutable\n",
			})
			devRoot := filepath.Join(t.TempDir(), "dev")
			p := prepareManagementSkillsFixture(t, fixture, devRoot, filepath.Join(t.TempDir(), "agent-home"))
			test.mutate(t, fixture)
			err := Prepare(p)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Prepare error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestPrepareRefusesForeignManagementSkillPaths(t *testing.T) {
	for _, kind := range []string{"regular file", "directory", "foreign symlink"} {
		t.Run(kind, func(t *testing.T) {
			storeRoot := withManagementSkillsStoreRoot(t)
			fixture := writeManagementSkillsFixture(t, storeRoot, "aaaaaaaa-management-skills", strings.Repeat("a", 40), map[string]string{
				"fleet-health-hypervisor/SKILL.md": "immutable\n",
			})
			devRoot := filepath.Join(t.TempDir(), "dev")
			hostHome := filepath.Join(t.TempDir(), "agent-home")
			p := prepareManagementSkillsFixture(t, fixture, devRoot, hostHome)
			target := filepath.Join(hostHome, ".codex", "skills", "fleet-health-hypervisor")
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "regular file":
				writeTestFile(t, target, "foreign\n")
			case "directory":
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatal(err)
				}
			case "foreign symlink":
				if err := os.Symlink(filepath.Join(t.TempDir(), "foreign"), target); err != nil {
					t.Fatal(err)
				}
			}
			err := Prepare(p)
			if err == nil || !strings.Contains(err.Error(), "foreign") {
				t.Fatalf("Prepare error = %v, want foreign path refusal", err)
			}
		})
	}
}

func TestDevWorkspaceRuntimeExportsNestedCodexConfigSource(t *testing.T) {
	root := devkitRootFromPackage(t)
	runtimeNix := readTestFile(t, filepath.Join(root, "overlays", "dev-workspace", "runtime.nix"))
	for _, want := range []string{
		`pkgs.python3Packages.pyyaml`,
		`[ -n "''${CODEX_HOME:-}" ]`,
		`[ -r "$CODEX_HOME/config.toml" ]`,
		`export DEVKIT_CODEX_CONFIG_SOURCE="$CODEX_HOME/config.toml"`,
	} {
		if !strings.Contains(runtimeNix, want) {
			t.Fatalf("dev-workspace runtime missing %q:\n%s", want, runtimeNix)
		}
	}
}

func runtimeLauncherFixture(t *testing.T) string {
	t.Helper()
	launcher := filepath.Join(t.TempDir(), "dev-all-runtime-shell")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return launcher
}

func TestShellCommandPreservesImmutableRuntimePathAgainstHostileLoginProfile(t *testing.T) {
	tmp := t.TempDir()
	runtimeBin := filepath.Join(tmp, "runtime-bin")
	home := filepath.Join(tmp, "home")
	workdir := filepath.Join(tmp, "work")
	for _, dir := range []string{runtimeBin, home, workdir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bashPath, filepath.Join(runtimeBin, "bash")); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(tmp, "node-ran")
	node := filepath.Join(runtimeBin, "node")
	if err := os.WriteFile(node, []byte("#!/bin/sh\nprintf preserved > \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte("export PATH=/usr/bin:/bin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(tmp, "runtime-launcher")
	launcherBody := "#!/bin/sh\nexport PATH=" + shellQuote(runtimeBin) + ":/usr/bin:/bin\nexec \"$@\"\n"
	if err := os.WriteFile(launcher, []byte(launcherBody), 0o755); err != nil {
		t.Fatal(err)
	}

	args := shellCommand("", "", workdir, []string{"node", output}, nativeplan.ProxyConfig{}, nil, false)
	cmd := exec.Command(launcher, args...)
	cmd.Env = append(os.Environ(), "HOME="+home)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runtime launcher command failed: %v\n%s", err, combined)
	}
	if got, err := os.ReadFile(output); err != nil || string(got) != "preserved" {
		t.Fatalf("immutable runtime node was not selected: content=%q err=%v", got, err)
	}
}

func TestBuildBubblewrapUsesBrokerAndNoHostDockerSocket(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	repoRoot := filepath.Join(devRoot, "ouroboros-ide")
	for _, dir := range []string{devkitRoot, repoRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	brokerSocket := filepath.Join(tmp, "broker.sock")
	if err := os.WriteFile(brokerSocket, []byte("socket placeholder"), 0o600); err != nil {
		t.Fatalf("write broker socket placeholder: %v", err)
	}
	runtimeLauncher := runtimeLauncherFixture(t)
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Repo:             "ouroboros-ide",
		Flake:            ".#runtime-test-agent",
		RuntimeLauncher:  runtimeLauncher,
		BubblewrapBinary: runtimeLauncherFixture(t),
		BrokerEndpoint:   brokerSocket,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	initTestGitWorktree(t, p.Agent.HostWorktree)
	if err := os.MkdirAll(p.Agent.HostHome, 0o700); err != nil {
		t.Fatalf("mkdir host home: %v", err)
	}
	if err := os.MkdirAll(p.Agent.StateRoot, 0o700); err != nil {
		t.Fatalf("mkdir state root: %v", err)
	}
	if err := os.WriteFile(p.DNS.ResolvConf, []byte("nameserver 127.0.0.1\n"), 0o644); err != nil {
		t.Fatalf("write resolv: %v", err)
	}

	cmd, err := BuildBubblewrap(p, []string{"git", "--version"})
	if err != nil {
		t.Fatalf("BuildBubblewrap: %v", err)
	}
	joined := ShellString(cmd)
	for _, want := range []string{
		"'--bind' '" + devRoot + "' '/workspaces/dev'",
		"'--symlink' '/run/current-system/sw/bin/env' '/usr/bin/env'",
		"'--symlink' '/run/current-system/sw/bin/bash' '/usr/bin/bash'",
		"'--symlink' '/run/current-system/sw/bin/sh' '/bin/sh'",
		"'--bind' '" + brokerSocket + "' '" + brokerSocket + "'",
		"'--setenv' 'COURSIER_CACHE' '/workspaces/dev/.cache/shared/coursier'",
		"'--setenv' 'DOCKER_HOST' 'unix://" + brokerSocket + "'",
		"'--setenv' 'OURO_NIX_SANDBOX' '1'",
		"'--setenv' 'SBT_IVY_HOME' '/workspaces/dev/.cache/shared/ivy2'",
		"'--setenv' 'TMPDIR' '/tmp'",
		"'--setenv' 'XDG_CACHE_HOME' '/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1/.cache'",
		"'" + runtimeLauncher + "' 'bash' '-c'",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command missing %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(cmd.Args[len(cmd.Args)-1], "export XDG_CACHE_HOME='/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1/.cache'") {
		t.Fatalf("agent shell does not restore XDG_CACHE_HOME:\n%#v", cmd.Args)
	}
	if !strings.Contains(cmd.Args[len(cmd.Args)-1], "export COURSIER_CACHE='/workspaces/dev/.cache/shared/coursier'") {
		t.Fatalf("agent shell does not export shared coursier cache:\n%#v", cmd.Args)
	}
	if !strings.Contains(cmd.Args[len(cmd.Args)-1], "export OURO_NIX_SANDBOX='1'") {
		t.Fatalf("agent shell does not export Nix sandbox marker:\n%#v", cmd.Args)
	}
	if !strings.Contains(cmd.Args[len(cmd.Args)-1], "export SBT_BOOT_DIR='/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1/.sbt/boot'") {
		t.Fatalf("agent shell does not export per-agent SBT boot dir:\n%#v", cmd.Args)
	}
	serverSystemProperties := p.Env["SBT_CONTROL_PLANE_SERVER_SYSTEM_PROPERTIES"]
	if !strings.Contains(cmd.Args[len(cmd.Args)-1], "export SBT_CONTROL_PLANE_SERVER_SYSTEM_PROPERTIES="+shellQuote(serverSystemProperties)) {
		t.Fatalf("agent shell does not bind the spawned SBT server to prepared slot paths:\n%#v", cmd.Args)
	}
	if strings.Contains(serverSystemProperties, p.Agent.HostHome) || strings.Contains(serverSystemProperties, "/home/bayesartre") {
		t.Fatalf("spawned SBT server properties contain host-home authority: %q", serverSystemProperties)
	}
	for _, optionalHostPath := range []string{"/etc/static", "/etc/ssl", "/etc/pki"} {
		if _, err := os.Stat(optionalHostPath); err != nil {
			continue
		}
		want := "'--ro-bind' '" + optionalHostPath + "' '" + optionalHostPath + "'"
		if !strings.Contains(joined, want) {
			t.Fatalf("command missing optional host bind %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "'--dir' '/tmp'") {
		t.Fatalf("launcher must not create /tmp after mounting it as tmpfs:\n%s", joined)
	}
	if len(cmd.Args) < 1 || !strings.Contains(cmd.Args[len(cmd.Args)-1], "cd '/workspaces/dev/agent-worktrees/agent1/ouroboros-ide' && exec 'git' '--version'") {
		t.Fatalf("launch script did not cd and exec command:\n%#v", cmd.Args)
	}
	if strings.Contains(joined, "/var/run/docker.sock") {
		t.Fatalf("native launcher must not expose /var/run/docker.sock:\n%s", joined)
	}
}

func TestBuildBubblewrapUsesImmutableRuntimeLauncherWithoutConsumerFlakeEvaluation(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	repoRoot := filepath.Join(devRoot, "ouroboros-terraform")
	for _, dir := range []string{devkitRoot, repoRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	runtimeLauncher := runtimeLauncherFixture(t)
	p, err := nativeplan.Build(nativeplan.BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Project:          "ouroboros-terraform",
		Repo:             "ouroboros-terraform",
		Flake:            "./overlays/ouroboros-terraform#default",
		RuntimeLauncher:  runtimeLauncher,
		BubblewrapBinary: runtimeLauncherFixture(t),
		FlakeInputOverrides: map[string]string{
			"ouroboros-terraform": "path:" + repoRoot,
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	initTestGitWorktree(t, p.Agent.HostWorktree)
	if err := os.MkdirAll(p.Agent.HostHome, 0o700); err != nil {
		t.Fatalf("mkdir host home: %v", err)
	}
	if err := os.MkdirAll(p.Agent.StateRoot, 0o700); err != nil {
		t.Fatalf("mkdir state root: %v", err)
	}
	if err := os.WriteFile(p.DNS.ResolvConf, []byte("nameserver 127.0.0.1\n"), 0o644); err != nil {
		t.Fatalf("write resolv: %v", err)
	}
	cmd, err := BuildBubblewrap(p, []string{"true"})
	if err != nil {
		t.Fatalf("BuildBubblewrap: %v", err)
	}
	joined := ShellString(cmd)
	if !strings.Contains(joined, "'"+runtimeLauncher+"' 'bash' '-c'") {
		t.Fatalf("command did not select the immutable runtime launcher:\n%s", joined)
	}
	if strings.Contains(joined, "'"+runtimeLauncher+"' 'bash' '-lc'") {
		t.Fatalf("command selected a login shell after the immutable runtime launcher:\n%s", joined)
	}
	for _, forbidden := range []string{
		"nix' 'develop",
		"--override-input",
		"./overlays/ouroboros-terraform#default",
		"path:" + repoRoot,
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("command retained consumer flake authority %q:\n%s", forbidden, joined)
		}
	}
}

func TestBuildBubblewrapRejectsMissingOrUntrustedRuntimeLauncher(t *testing.T) {
	tmp := t.TempDir()
	devkitRoot := filepath.Join(tmp, "devkit")
	worktree := filepath.Join(tmp, "worktree")
	for _, dir := range []string{devkitRoot, worktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, launcher := range []string{"", "relative/dev-all-runtime-shell"} {
		p, err := nativeplan.Build(nativeplan.BuildOptions{
			Paths:           devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
			Repo:            "ouroboros-ide",
			RuntimeLauncher: launcher,
			WorktreeRoot:    filepath.Dir(worktree),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := BuildBubblewrap(p, []string{"true"}); err == nil ||
			!strings.Contains(err.Error(), "immutable native runtime launcher") {
			t.Fatalf("runtime launcher %q was not rejected: %v", launcher, err)
		}
	}
	for _, bubblewrap := range []string{"", "relative/bwrap"} {
		p, err := nativeplan.Build(nativeplan.BuildOptions{
			Paths:            devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
			Repo:             "ouroboros-ide",
			RuntimeLauncher:  runtimeLauncherFixture(t),
			BubblewrapBinary: bubblewrap,
			WorktreeRoot:     filepath.Dir(worktree),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := BuildBubblewrap(p, []string{"true"}); err == nil ||
			!strings.Contains(err.Error(), "immutable native bubblewrap binary") {
			t.Fatalf("bubblewrap binary %q was not rejected: %v", bubblewrap, err)
		}
	}
}

func TestBuildBubblewrapProxySocketUnsharesNetworkAndStartsBridge(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	repoRoot := filepath.Join(devRoot, "ouroboros-ide")
	for _, dir := range []string{devkitRoot, repoRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	proxySocket := filepath.Join(tmp, "egress.sock")
	if err := os.WriteFile(proxySocket, []byte("socket placeholder"), 0o600); err != nil {
		t.Fatalf("write proxy socket placeholder: %v", err)
	}
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Repo:             "ouroboros-ide",
		Flake:            ".#runtime-test-agent",
		RuntimeLauncher:  runtimeLauncherFixture(t),
		BubblewrapBinary: runtimeLauncherFixture(t),
		ProxySocket:      proxySocket,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	cmd, err := BuildBubblewrap(p, []string{"curl", "-I", "https://api.openai.com"})
	if err != nil {
		t.Fatalf("BuildBubblewrap: %v", err)
	}
	joined := ShellString(cmd)
	for _, want := range []string{
		"'--unshare-net'",
		"'--bind' '" + proxySocket + "' '" + proxySocket + "'",
		"native proxy-bridge --listen 127.0.0.1:18888",
		"'--setenv' 'HTTP_PROXY' 'http://127.0.0.1:18888'",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command missing %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "proxy-bridge") || !strings.Contains(joined, proxySocket) {
		t.Fatalf("command missing proxy socket bridge:\n%s", joined)
	}
	if strings.Contains(joined, "'--share-net'") {
		t.Fatalf("proxy socket launches must not share host network:\n%s", joined)
	}
}

func TestBuildBubblewrapManagementFleetUsesOnlyTypedExecHandle(t *testing.T) {
	withProductGovernanceEnvironmentFixture(t)
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	worktreeRoot := filepath.Join(devRoot, "control-plane-worktrees")
	worktree := filepath.Join(worktreeRoot, "agent2", "shadow-throne-management")
	ti4Worktree := "/home/bayesartre/dev/control-plane-worktrees/agent1/ti4-calculator"
	for _, dir := range []string{devkitRoot, worktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	proxySocket := filepath.Join(tmp, "egress.sock")
	if err := os.WriteFile(proxySocket, []byte("socket placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	execRoot, err := os.MkdirTemp("/tmp", "devkit-exec-handle-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(execRoot) })
	if err := os.Chmod(execRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	execSocket := filepath.Join(execRoot, "control.sock")
	listener, err := net.Listen("unix", execSocket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := os.Chmod(execSocket, 0o600); err != nil {
		t.Fatal(err)
	}
	previousExecSocket := nativeplan.WorkspaceControllerExecSocket
	nativeplan.WorkspaceControllerExecSocket = execSocket
	t.Cleanup(func() { nativeplan.WorkspaceControllerExecSocket = previousExecSocket })
	p, err := nativeplan.Build(nativeplan.BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		HostRoot:         devRoot,
		WorktreeRoot:     worktreeRoot,
		Project:          "dev-workspace",
		Index:            2,
		Repo:             "shadow-throne-management",
		Flake:            ".#dev-workspace",
		RuntimeLauncher:  runtimeLauncherFixture(t),
		BubblewrapBinary: runtimeLauncherFixture(t),
		IsolationProfile: nativeplan.IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(devRoot, "allowlist.txt"),
		ProxySocket:      proxySocket,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// The test host has no immutable inventory projections. Keep only the
	// protected exec handle and ordinary workspace capabilities.
	filtered := p.Binds[:0]
	for _, bind := range p.Binds {
		if isWorkspaceControllerCapabilityTarget(bind.Target) && bind.Target != execSocket {
			continue
		}
		filtered = append(filtered, bind)
	}
	p.Binds = filtered
	fleetCommand := []string{"/run/current-system/sw/bin/fleet-control", "pressure", "--inventory", "/etc/fleet/source/fleet-inventory.json", "--json", "derpinator"}
	cmd, err := BuildBubblewrap(p, fleetCommand)
	if err != nil {
		t.Fatalf("BuildBubblewrap Fleet: %v", err)
	}
	joined := ShellString(cmd)
	if !strings.Contains(joined, "'--unshare-net'") || strings.Contains(joined, "'--share-net'") {
		t.Fatalf("Management Fleet command must remain network-isolated: %s", joined)
	}
	if !strings.Contains(joined, "'--ro-bind' '"+execSocket+"' '"+execSocket+"'") ||
		!strings.Contains(joined, "'--setenv' 'FLEET_EXEC_TRANSPORT_HANDLE' 'required'") {
		t.Fatalf("Management Fleet command lacks the exact typed exec handle: %s", joined)
	}
	if !strings.Contains(
		joined,
		"'--bind' '"+ti4Worktree+"' '/workspaces/dev/ti4-calculator'",
	) {
		t.Fatalf("Management runtime command lacks the exact registered TI4 bind: %s", joined)
	}
	if strings.Contains(joined, "'--bind' '"+devRoot+"' '/workspaces/dev'") {
		t.Fatalf("Management runtime command exposed the broad dev root: %s", joined)
	}
	for _, nonFleet := range [][]string{{"/bin/bash", "-lc", "curl https://example.invalid"}, {"/run/current-system/sw/bin/fleet", "pressure"}} {
		other, err := BuildBubblewrap(p, nonFleet)
		if err != nil {
			t.Fatalf("BuildBubblewrap non-Fleet: %v", err)
		}
		otherText := ShellString(other)
		if !strings.Contains(otherText, "'--unshare-net'") || strings.Contains(otherText, "'--share-net'") {
			t.Fatalf("Management child received host network: %s", otherText)
		}
	}
}

func TestBuildBubblewrapWorkspaceEgressUsesNarrowBinds(t *testing.T) {
	withProductGovernanceEnvironmentFixture(t)
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	hostWorktree := filepath.Join(devRoot, "agent-worktrees", "agent3", "ouroboros-ide")
	commonGitDir := filepath.Join(devRoot, "ouroboros-ide", ".git")
	worktreeGitDir := filepath.Join(commonGitDir, "worktrees", "agent3")
	for _, dir := range []string{devkitRoot, hostWorktree, worktreeGitDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	relativeGitDir, err := filepath.Rel(hostWorktree, worktreeGitDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostWorktree, ".git"), []byte("gitdir: "+relativeGitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeGitDir, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Project:          "dev-all",
		Index:            3,
		Repo:             "ouroboros-ide",
		Flake:            ".#runtime-test-agent",
		RuntimeLauncher:  runtimeLauncherFixture(t),
		BubblewrapBinary: runtimeLauncherFixture(t),
		IsolationProfile: nativeplan.IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(devRoot, "ouroboros-ide", "infra", "docker", "dev", "tinyproxy", "allowlist.txt"),
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	cmd, err := BuildBubblewrap(p, []string{"true"})
	if err != nil {
		t.Fatalf("BuildBubblewrap: %v", err)
	}
	joined := ShellString(cmd)
	for _, want := range []string{
		"'--tmpfs' '/mnt'",
		"'--unshare-net'",
		"'--ro-bind' '/var/run/nscd/socket' '/var/run/nscd/socket'",
		"'--bind' '" + hostWorktree + "' '/workspace'",
		"'--bind' '" + hostWorktree + "' '/workspaces/dev/agent-worktrees/agent3/ouroboros-ide'",
		"'--bind' '" + commonGitDir + "' '/workspaces/dev/ouroboros-ide/.git'",
		"'--bind' '" + filepath.Join(devRoot, "agent-worktrees", "agent3", ".devhome-agent3") + "' '/workspaces/dev/agent-worktrees/agent3/.devhome-agent3'",
		"'--ro-bind' '" + devkitRoot + "' '/workspaces/dev/devkit'",
		"native proxy-bridge --listen 127.0.0.1:18888",
		"'--setenv' 'COURSIER_CACHE' '/workspaces/dev/agent-worktrees/agent3/.devhome-agent3/.cache/coursier'",
		"'--setenv' 'SBT_IVY_HOME' '/workspaces/dev/agent-worktrees/agent3/.devhome-agent3/.cache/ivy2'",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command missing %q:\n%s", want, joined)
		}
	}
	for _, forbidden := range []string{
		"'--share-net'",
		"'--bind' '" + devRoot + "' '/workspaces/dev'",
		"'--bind' '" + devRoot + "' '" + devRoot + "'",
		"'--bind' '" + hostWorktree + "' '" + hostWorktree + "'",
		"'--bind' '" + commonGitDir + "' '" + commonGitDir + "'",
		"'--bind' '" + filepath.Join(devRoot, "agent-worktrees") + "' '/workspaces/dev/agent-worktrees'",
		"'--bind' '" + filepath.Join(devRoot, ".devkit", "native-agents") + "' '/agent-state'",
		"/workspaces/dev/.cache/shared",
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("workspace-egress command contains forbidden %q:\n%s", forbidden, joined)
		}
	}
}

func TestPrepareRequiresExistingWorktree(t *testing.T) {
	tmp := t.TempDir()
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: filepath.Join(tmp, "devkit"), RuntimeAuthorityRoot: filepath.Join(tmp, "devkit")},
		Repo:  "missing",
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if err := Prepare(p); err == nil {
		t.Fatalf("expected missing worktree error")
	}
}

func TestPrepareRejectsPlainDirectoryAsReadyWorktree(t *testing.T) {
	tmp := t.TempDir()
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: filepath.Join(tmp, "devkit"), RuntimeAuthorityRoot: filepath.Join(tmp, "devkit")},
		Repo:  "ouroboros-ide",
		Index: 2,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if err := os.MkdirAll(p.Agent.HostWorktree, 0o755); err != nil {
		t.Fatalf("mkdir plain worktree path: %v", err)
	}
	if err := Prepare(p); err == nil || !strings.Contains(err.Error(), "is not a Git worktree") {
		t.Fatalf("Prepare plain directory error = %v", err)
	}
}

func TestPrepareGitBootstrapUsesPackageOwnedConsumerIdentityAndProxy(t *testing.T) {
	tmp := t.TempDir()
	hostUserHome := filepath.Join(tmp, "host-user")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519"), "private")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519.pub"), "public")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "known_hosts"), "github.com ssh-ed25519 key")
	t.Setenv("HOME", hostUserHome)

	hostHome := filepath.Join(tmp, "agent-home")
	runtimeRoot := filepath.Join(tmp, "runtime-authority")
	devctlPath := filepath.Join(runtimeRoot, "kit", "bin", "devctl")
	writeTestFile(t, devctlPath, "#!/bin/sh\nexit 99\n")
	if err := os.Chmod(devctlPath, 0o755); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(tmp, "native-egress", "product-a.sock")
	p := nativeplan.Plan{
		IsolationProfile:     nativeplan.IsolationProfileWorkspaceEgress,
		RuntimeAuthorityRoot: runtimeRoot,
		Agent: agent.Spec{
			ID:          agent.ID{Project: "dev-all", Index: 1, Repo: "ouroboros-ide"},
			HostHome:    hostHome,
			SandboxHome: "/workspaces/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1",
		},
		Proxy: nativeplan.ProxyConfig{
			HTTPProxy:  "http://127.0.0.1:18888",
			UnixSocket: socketPath,
		},
	}
	sshCommand, err := PrepareGitBootstrap(p)
	if err != nil {
		t.Fatalf("PrepareGitBootstrap: %v", err)
	}
	expectedCommand := testPackageSSHCommand(t, filepath.Join(hostHome, ".ssh", "config"))
	if sshCommand != expectedCommand {
		t.Fatalf("bootstrap SSH command = %q, want %q", sshCommand, expectedCommand)
	}
	cfg := readTestFile(t, filepath.Join(hostHome, ".ssh", "config"))
	for _, want := range []string{
		"Host github.com ssh.github.com",
		"  HostName ssh.github.com",
		"  Port 443",
		"  ProxyCommand '" + devctlPath + "' -p 'dev-all' native proxy-connect --socket '" + socketPath + "' --target %h:%p",
		"  IdentityFile " + filepath.Join(hostHome, ".ssh", "id_ed25519"),
		"  IdentitiesOnly yes",
		"  BatchMode yes",
		"  StrictHostKeyChecking yes",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("bootstrap SSH config missing %q:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, p.Agent.SandboxHome) {
		t.Fatalf("host bootstrap SSH config selected sandbox-only paths:\n%s", cfg)
	}
	for _, forbidden := range []string{"127.0.0.1:18888", "ProxyCommand nc ", "https://"} {
		if strings.Contains(cfg, forbidden) {
			t.Fatalf("host bootstrap SSH config selected forbidden transport %q:\n%s", forbidden, cfg)
		}
	}
}

func TestPrepareGitBootstrapRejectsMissingPackageOwnedProxyHelper(t *testing.T) {
	tmp := t.TempDir()
	hostUserHome := filepath.Join(tmp, "host-user")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519"), "private")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519.pub"), "public")
	t.Setenv("HOME", hostUserHome)

	p := nativeplan.Plan{
		IsolationProfile:     nativeplan.IsolationProfileWorkspaceEgress,
		RuntimeAuthorityRoot: filepath.Join(tmp, "missing-runtime"),
		Agent: agent.Spec{
			ID:       agent.ID{Project: "dev-all", Index: 1, Repo: "ouroboros-ide"},
			HostHome: filepath.Join(tmp, "agent-home"),
		},
		Proxy: nativeplan.ProxyConfig{UnixSocket: filepath.Join(tmp, "proxy.sock")},
	}
	_, err := PrepareGitBootstrap(p)
	if err == nil || !strings.Contains(err.Error(), "package-owned proxy helper") {
		t.Fatalf("PrepareGitBootstrap error = %v, want package-owned proxy helper rejection", err)
	}
}

func TestPrepareGitBootstrapRejectsMissingIdentity(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmp, "empty-host-home"))
	p := nativeplan.Plan{
		Agent: agent.Spec{
			HostHome: filepath.Join(tmp, "agent-home"),
		},
	}
	_, err := PrepareGitBootstrap(p)
	if err == nil || !strings.Contains(err.Error(), "requires a seeded SSH identity") {
		t.Fatalf("PrepareGitBootstrap error = %v, want missing identity rejection", err)
	}
}

func TestSeedSSHSeedsOnlyCallerIdentityAndNeverCallerKnownHosts(t *testing.T) {
	hostUserHome := t.TempDir()
	srcSSH := filepath.Join(hostUserHome, ".ssh")
	if err := os.MkdirAll(srcSSH, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"id_ed25519":     "private",
		"id_ed25519.pub": "public",
		"known_hosts":    "github.com key",
	} {
		if err := os.WriteFile(filepath.Join(srcSSH, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", hostUserHome)

	nativeHome := filepath.Join(t.TempDir(), "native-home")
	if err := SeedSSH(nativeHome, false); err != nil {
		t.Fatalf("SeedSSH: %v", err)
	}
	for name, wantMode := range map[string]os.FileMode{
		"id_ed25519":     0o600,
		"id_ed25519.pub": 0o644,
	} {
		info, err := os.Stat(filepath.Join(nativeHome, ".ssh", name))
		if err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
		if got := info.Mode().Perm(); got != wantMode {
			t.Fatalf("%s mode = %v, want %v", name, got, wantMode)
		}
	}
	if _, err := os.Lstat(filepath.Join(nativeHome, ".ssh", "known_hosts")); !os.IsNotExist(err) {
		t.Fatalf("SeedSSH copied caller known_hosts: %v", err)
	}
}

func TestSeedCodexConfigMaterializesAndRefreshesNixAuthoredSource(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "nix-store", "codex-config.toml")
	first := "# source = nixos-wsl codex config\nmodel_provider = \"openai\"\n"
	writeTestFile(t, source, first)
	t.Setenv("DEVKIT_CODEX_CONFIG_SOURCE", source)
	home := filepath.Join(root, "consumer-home")

	if err := SeedCodexConfig(home); err != nil {
		t.Fatalf("SeedCodexConfig first materialization: %v", err)
	}
	target := filepath.Join(home, ".codex", "config.toml")
	if got := readTestFile(t, target); got != first {
		t.Fatalf("materialized Codex config = %q, want %q", got, first)
	}
	if info, err := os.Lstat(target); err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("materialized Codex config mode = %v error=%v", info, err)
	}

	second := first + "[profiles.openai]\nmodel_provider = \"openai\"\n"
	if err := os.WriteFile(source, []byte(second), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SeedCodexConfig(home); err != nil {
		t.Fatalf("SeedCodexConfig refresh: %v", err)
	}
	if got := readTestFile(t, target); got != second {
		t.Fatalf("refreshed Codex config = %q, want %q", got, second)
	}
}

func TestSeedCodexConfigRejectsRelativeSymlinkAndNonRegularSources(t *testing.T) {
	home := filepath.Join(t.TempDir(), "consumer-home")
	t.Setenv("DEVKIT_CODEX_CONFIG_SOURCE", "relative-config.toml")
	if err := SeedCodexConfig(home); err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("relative config source error = %v", err)
	}

	root := t.TempDir()
	realSource := filepath.Join(root, "real-config.toml")
	writeTestFile(t, realSource, "model_provider = \"openai\"\n")
	symlinkSource := filepath.Join(root, "config-link.toml")
	if err := os.Symlink(realSource, symlinkSource); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVKIT_CODEX_CONFIG_SOURCE", symlinkSource)
	if err := SeedCodexConfig(home); err == nil || !strings.Contains(err.Error(), "regular non-symlink") {
		t.Fatalf("symlink config source error = %v", err)
	}

	t.Setenv("DEVKIT_CODEX_CONFIG_SOURCE", root)
	if err := SeedCodexConfig(home); err == nil || !strings.Contains(err.Error(), "regular non-symlink") {
		t.Fatalf("directory config source error = %v", err)
	}
}

func TestSeedAWSSyncsConfigAndCaches(t *testing.T) {
	srcAWS := filepath.Join(t.TempDir(), ".aws")
	for _, dir := range []string{
		srcAWS,
		filepath.Join(srcAWS, "sso", "cache"),
		filepath.Join(srcAWS, "cli", "cache"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	files := map[string]string{
		"config":                                "[profile ouroboros]\nsso_session = mysesh\n",
		"credentials":                           "",
		filepath.Join("sso", "cache", "a.json"): `{"accessToken":"redacted"}`,
		filepath.Join("cli", "cache", "b.json"): `{"Credentials":{"AccessKeyId":"redacted"}}`,
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(srcAWS, rel), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	t.Setenv("DEVKIT_AWS_HOME", srcAWS)

	nativeHome := filepath.Join(t.TempDir(), "native-home")
	if err := SeedAWS(nativeHome, false); err != nil {
		t.Fatalf("SeedAWS: %v", err)
	}
	for rel, want := range files {
		if got := readTestFile(t, filepath.Join(nativeHome, ".aws", rel)); got != want {
			t.Fatalf("%s = %q, want %q", rel, got, want)
		}
		info, err := os.Stat(filepath.Join(nativeHome, ".aws", rel))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s mode = %v, want 0600", rel, got)
		}
	}
	for _, rel := range []string{".aws", filepath.Join(".aws", "sso"), filepath.Join(".aws", "sso", "cache"), filepath.Join(".aws", "cli"), filepath.Join(".aws", "cli", "cache")} {
		info, err := os.Stat(filepath.Join(nativeHome, rel))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("%s mode = %v, want 0700", rel, got)
		}
	}
}

func TestSeedAWSMaterializesExistingExternalTargetSymlink(t *testing.T) {
	srcAWS := filepath.Join(t.TempDir(), ".aws")
	writeTestFile(t, filepath.Join(srcAWS, "config"), "[profile ouroboros]\nregion = us-east-2\n")
	writeTestFile(t, filepath.Join(srcAWS, "sso", "cache", "session.json"), `{"accessToken":"redacted"}`)
	t.Setenv("DEVKIT_AWS_HOME", srcAWS)

	externalAWS := filepath.Join(t.TempDir(), "windows-aws")
	writeTestFile(t, filepath.Join(externalAWS, "sentinel"), "must-remain")
	nativeHome := filepath.Join(t.TempDir(), "native-home")
	if err := os.MkdirAll(nativeHome, 0o700); err != nil {
		t.Fatalf("mkdir native home: %v", err)
	}
	targetAWS := filepath.Join(nativeHome, ".aws")
	if err := os.Symlink(externalAWS, targetAWS); err != nil {
		t.Fatalf("symlink target AWS home: %v", err)
	}

	if err := SeedAWS(nativeHome, false); err != nil {
		t.Fatalf("SeedAWS: %v", err)
	}
	info, err := os.Lstat(targetAWS)
	if err != nil {
		t.Fatalf("lstat materialized AWS home: %v", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("target AWS home was not materialized: mode=%v", info.Mode())
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("target AWS home mode = %v, want 0700", got)
	}
	if got := readTestFile(t, filepath.Join(targetAWS, "config")); got != "[profile ouroboros]\nregion = us-east-2\n" {
		t.Fatalf("materialized config = %q", got)
	}
	if got := readTestFile(t, filepath.Join(targetAWS, "sso", "cache", "session.json")); got != `{"accessToken":"redacted"}` {
		t.Fatalf("materialized cache = %q", got)
	}
	if got := readTestFile(t, filepath.Join(externalAWS, "sentinel")); got != "must-remain" {
		t.Fatalf("external AWS target was modified: %q", got)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func countFiles(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk %s: %v", root, err)
	}
	return count
}

func runTestCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

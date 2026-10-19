package launch

import (
	"devkit/cli/devctl/internal/devkitpaths"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Exercise actual preparation on the same non-secret Git/home fixture. The
// private preparation primitive uses temporary geometry; public TF1 admission
// is independently exercised by native command controls.
func TestTerraformToolsActualPreparationDefersOnlyProviders(t *testing.T) {
	// Model hostile ambient precedence with public owned data, never a real
	// inherited credential file. The nested fixture must select its own file.
	poison := filepath.Join(t.TempDir(), "ambient-credentials")
	writeTestFile(t, poison, "PUBLIC_POISON_FIXTURE\n")
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	if _, err := syscall.InotifyAddWatch(fd, poison, syscall.IN_OPEN|syscall.IN_ACCESS|syscall.IN_MODIFY|syscall.IN_ATTRIB|syscall.IN_DELETE_SELF|syscall.IN_MOVE_SELF); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", poison)
	t.Run("explicit-owned-file", terraformToolsActualPreparationFixture)
	buffer := make([]byte, 8192)
	count, err := syscall.Read(fd, buffer)
	if count > 0 || (err != nil && err != syscall.EAGAIN) {
		t.Fatalf("synthetic ambient credential file was accessed or watch failed: %d %v", count, err)
	}
	if os.Getenv("AWS_SHARED_CREDENTIALS_FILE") != poison {
		t.Fatal("nested fixture did not restore its enclosing selector")
	}
}

func terraformToolsActualPreparationFixture(t *testing.T) {
	store := withManagementSkillsStoreRoot(t)
	fixture := writeManagementSkillsFixture(t, store, "terraform-tools", strings.Repeat("a", 40), map[string]string{"fleet-health-hypervisor/SKILL.md": "public fixture\n"})
	p := prepareManagementSkillsFixture(t, fixture, filepath.Join(t.TempDir(), "dev"), filepath.Join(t.TempDir(), "home"))
	p.Agent.ID.Project = "ouroboros-terraform"
	p.Agent.ID.Repo = "ouroboros-terraform"
	p.Agent.ID.Index = 1
	oldHydrator := terraformProviderCredentialHydrator
	oldRoot := terraformProviderCredentialRoot
	t.Cleanup(func() { terraformProviderCredentialHydrator = oldHydrator; terraformProviderCredentialRoot = oldRoot })
	marker := filepath.Join(t.TempDir(), "hydrator-called")
	terraformProviderCredentialHydrator = filepath.Join(t.TempDir(), "hydrator")
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, terraformProviderCredentialHydrator, "#!"+shell+"\necho called >> '"+marker+"'\necho 'Terraform provider credential hydration failed: approved DNSimple token source is unavailable' >&2\nexit 1\n")
	if err := os.Chmod(terraformProviderCredentialHydrator, 0700); err != nil {
		t.Fatal(err)
	}
	terraformProviderCredentialRoot = filepath.Join(t.TempDir(), "providers")
	aws := filepath.Join(t.TempDir(), "aws-source")
	target := filepath.Join(p.Agent.HostHome, ".aws")
	paths := []string{filepath.Join(aws, "credentials"), filepath.Join(target, "credentials"), filepath.Join(terraformProviderCredentialRoot, "google-adc.json"), filepath.Join(p.Agent.HostHome, ".config", "ouroboros", "dnsimple-provider.env")}
	for _, path := range paths {
		writeTestFile(t, path, "SYNTHETIC_PRESERVED_PROVIDER_FIXTURE\n")
	}
	t.Setenv("DEVKIT_AWS_HOME", aws)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(aws, "credentials"))
	if err := Prepare(p); err == nil || !strings.Contains(err.Error(), "approved DNSimple token source is unavailable") {
		t.Fatalf("normal preparation did not retain real missing-provider refusal: %v", err)
	}
	before, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != "called\n" {
		t.Fatal("normal preparation did not invoke exactly one owned refusing hydrator")
	}
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	for _, path := range append(append([]string{}, paths...), aws, target, terraformProviderCredentialRoot) {
		if _, err := syscall.InotifyAddWatch(fd, path, syscall.IN_OPEN|syscall.IN_ACCESS|syscall.IN_MODIFY|syscall.IN_ATTRIB|syscall.IN_DELETE_SELF|syscall.IN_MOVE_SELF); err != nil {
			t.Fatal(err)
		}
	}
	if err := prepare(p, false, true); err != nil {
		t.Fatalf("same-fixture tools preparation: %v", err)
	}
	buffer := make([]byte, 8192)
	count, err := syscall.Read(fd, buffer)
	if count > 0 || (err != nil && err != syscall.EAGAIN) {
		t.Fatalf("tools phase opened or changed provider material: %d %v", count, err)
	}
	after, err := os.ReadFile(marker)
	if err != nil || string(after) != string(before) {
		t.Fatal("tools phase invoked provider hydrator")
	}
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil || string(b) != "SYNTHETIC_PRESERVED_PROVIDER_FIXTURE\n" {
			t.Fatalf("preserved provider fixture changed %s: %v", path, err)
		}
	}
	required := p
	required.Binds = append(append([]nativeplan.Bind{}, p.Binds...), nativeplan.Bind{Source: filepath.Join(t.TempDir(), "missing-bind"), Required: true})
	if err := prepare(required, false, true); err == nil || !strings.Contains(err.Error(), "required bind source") {
		t.Fatalf("tools preparation bypassed required bind: %v", err)
	}
	configured := p
	configured.GUITargetConfig = &nativeplan.GUITargetConfigProjection{TargetID: "terraform-ouro-1", Source: filepath.Join(t.TempDir(), "missing-immutable-config"), SourceSHA256: strings.Repeat("0", 64)}
	if err := prepare(configured, false, true); err == nil {
		t.Fatal("tools preparation bypassed immutable Codex configuration")
	}
	p.Agent.HostWorktree = filepath.Join(t.TempDir(), "missing")
	if err := prepare(p, false, true); err == nil {
		t.Fatal("tools phase bypassed worktree custody")
	}
}

// Derive the missing bindings from the actual native TF1 planner, then run the
// actual preparation primitive in a non-secret temporary namespace. Only the
// unrelated external service binds are omitted from this fixture; their normal
// immutable checks and public target/host admission remain separate controls.
func terraformRuntimeSupportFixture(t *testing.T) nativeplan.Plan {
	t.Helper()
	withProductGovernanceEnvironmentFixture(t)
	store := withManagementSkillsStoreRoot(t)
	fixture := writeManagementSkillsFixture(t, store, "terraform-runtime-support", strings.Repeat("a", 40), map[string]string{"fleet-health-hypervisor/SKILL.md": "public fixture\n"})
	devRoot := filepath.Join(t.TempDir(), "dev")
	prepareManagementSkillsFixture(t, fixture, devRoot, filepath.Join(t.TempDir(), "unused-home"))
	workspace := filepath.Join(devRoot, "control-plane-worktrees", "terraform-ouro-1")
	worktree := filepath.Join(workspace, "ouroboros-terraform")
	initTestGitWorktree(t, worktree)
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	runtimeHelper := filepath.Join(runtimeRoot, "kit", "bin", "devctl")
	writeTestFile(t, runtimeHelper, "#!/bin/sh\nexit 99\n")
	if err := os.Chmod(runtimeHelper, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths:    devkitpaths.Paths{Root: filepath.Join(devRoot, "devkit"), RuntimeAuthorityRoot: runtimeRoot},
		HostRoot: devRoot, Project: "ouroboros-terraform", Repo: "ouroboros-terraform", Index: 1,
		WorktreeRoot: filepath.Join(devRoot, "control-plane-worktrees"), WorkspaceRoot: workspace,
		IsolationProfile: nativeplan.IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(runtimeRoot, "allowlist.txt"),
		RuntimeLauncher:  runtimeLauncherFixture(t), BubblewrapBinary: runtimeLauncherFixture(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	binds := []nativeplan.Bind{}
	for _, bind := range p.Binds {
		if strings.HasPrefix(bind.Target, "/workspaces/dev/.devkit/") || bind.Target == nativeplan.WorkspaceProductGovernanceEnvTarget {
			binds = append(binds, bind)
		}
	}
	p.Binds = binds
	// Nix's test sandbox has no host resolver. Supply the planner's actual
	// destination as public fixture data; retain normal resolver preparation.
	writeTestFile(t, p.DNS.ResolvConf, "nameserver 127.0.0.1\n")
	if len(binds) != 4 {
		t.Fatalf("actual TF1 governance bind count = %d", len(binds))
	}
	for name, content := range map[string]string{
		"ouro8-governance-env.sh":        "export DEVKIT_GOVERNANCE_REPO_PATHS='ouroboros-terraform=/workspaces/dev/ouroboros-terraform'\n",
		"ouro8-governance-repo-env.json": "{\"repository\":\"ouroboros-terraform\"}\n",
	} {
		writeTestFile(t, filepath.Join(p.HostRuntimeSupportRoot, name), content)
	}
	writeTestFile(t, filepath.Join(p.HostRuntimeSupportRoot, "governance-control-plane", "central-state"), "must not project mutable state\n")
	return p
}

func TestTerraformActualPlanPreparationMaterializesRequiredSupport(t *testing.T) {
	p := terraformRuntimeSupportFixture(t)
	centralBefore := map[string]string{}
	for _, name := range []string{"ouro8-governance-env.sh", "ouro8-governance-repo-env.json"} {
		centralBefore[name] = readTestFile(t, filepath.Join(p.HostRuntimeSupportRoot, name))
	}
	sibling := filepath.Join(filepath.Dir(p.HostWorkspaceRoot), "agent2", ".devkit", "sibling-marker")
	writeTestFile(t, sibling, "preserved sibling\n")
	before, err := os.Stat(sibling)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepare(p, false, true); err != nil {
		t.Fatalf("actual TF1 plan preparation must materialize required lane binds: %v", err)
	}
	for _, bind := range p.Binds {
		if strings.HasPrefix(bind.Target, "/workspaces/dev/.devkit/") {
			if !bind.Required || bind.Source != filepath.Join(p.HostWorkspaceRoot, ".devkit", filepath.Base(bind.Target)) {
				t.Fatalf("foreign/optional TF1 bind %#v", bind)
			}
			info, err := os.Stat(bind.Source)
			if err != nil {
				t.Fatal(err)
			}
			if info.IsDir() {
				if info.Mode().Perm() != 0700 || bind.Mode != "rw" {
					t.Fatalf("state bind %#v mode %v", bind, info.Mode())
				}
				if entries, err := os.ReadDir(bind.Source); err != nil || len(entries) != 0 {
					t.Fatalf("shared mutable state copied: %v %v", entries, err)
				}
			} else {
				name := filepath.Base(bind.Source)
				if info.Mode().Perm() != 0644 || bind.Mode != "ro" || readTestFile(t, bind.Source) != centralBefore[name] {
					t.Fatalf("public projection mismatch: %#v %v", bind, info.Mode())
				}
				if readTestFile(t, filepath.Join(p.HostRuntimeSupportRoot, name)) != centralBefore[name] {
					t.Fatal("central support changed")
				}
			}
		}
	}
	if info, err := os.Stat(filepath.Join(p.HostWorkspaceRoot, ".devkit")); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("lane support mode: %v %v", info, err)
	}
	after, err := os.Stat(sibling)
	if err != nil || !os.SameFile(before, after) || readTestFile(t, sibling) != "preserved sibling\n" {
		t.Fatal("sibling custody changed")
	}
	command, err := BuildBubblewrap(p, []string{"codex", "app-server"})
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(command.Args, " ")
	for _, bind := range p.Binds {
		flag := "--ro-bind"
		if bind.Mode == "rw" {
			flag = "--bind"
		}
		if !strings.Contains(argv, flag+" "+bind.Source+" "+bind.Target) {
			t.Fatalf("launcher omitted actual required bind %#v: %s", bind, argv)
		}
	}
	if strings.Contains(argv, p.HostRuntimeSupportRoot+" /workspaces/dev/.devkit") {
		t.Fatal("launcher broadened shared support bind")
	}
	missing := p
	missing.Binds = append(append([]nativeplan.Bind{}, p.Binds...), nativeplan.Bind{Source: filepath.Join(p.HostWorkspaceRoot, "missing-mandatory"), Target: "/required-test", Mode: "ro", Required: true})
	if err := prepare(missing, false, true); err == nil || !strings.Contains(err.Error(), "required bind source") {
		t.Fatalf("mandatory bind relaxed: %v", err)
	}
}

func TestTerraformSupportMaterializationRetainsSourceAndDestinationGuards(t *testing.T) {
	for _, kind := range []string{"missing-source", "source-symlink", "destination-symlink", "file-symlink", "state-symlink", "state-file"} {
		t.Run(kind, func(t *testing.T) {
			p := terraformRuntimeSupportFixture(t)
			target := filepath.Join(p.HostWorkspaceRoot, ".devkit")
			source := filepath.Join(p.HostRuntimeSupportRoot, "ouro8-governance-env.sh")
			switch kind {
			case "missing-source":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
			case "source-symlink":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(p.HostRuntimeSupportRoot, "ouro8-governance-repo-env.json"), source); err != nil {
					t.Fatal(err)
				}
			case "destination-symlink":
				if err := os.Symlink(t.TempDir(), target); err != nil {
					t.Fatal(err)
				}
			case "file-symlink":
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(source, filepath.Join(target, "ouro8-governance-env.sh")); err != nil {
					t.Fatal(err)
				}
			case "state-symlink":
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), filepath.Join(target, "governance-control-plane")); err != nil {
					t.Fatal(err)
				}
			case "state-file":
				writeTestFile(t, filepath.Join(target, "governance-control-plane"), "not a directory\n")
			}
			if err := prepare(p, false, true); err == nil {
				t.Fatalf("%s accepted", kind)
			}
		})
	}
}

func TestTerraformSupportMaterializationRejectsForeignProjectionBeforeMutation(t *testing.T) {
	for _, kind := range []string{"project", "repo", "index", "lane", "home", "state", "sandbox", "isolation", "source-root", "optional-bind", "foreign-bind", "duplicate-bind"} {
		t.Run(kind, func(t *testing.T) {
			p := terraformRuntimeSupportFixture(t)
			target := filepath.Join(p.HostWorkspaceRoot, ".devkit")
			switch kind {
			case "project":
				p.Agent.ID.Project = "foreign"
			case "repo":
				p.Agent.ID.Repo = "foreign"
			case "index":
				p.Agent.ID.Index = 2
			case "lane":
				p.HostWorkspaceRoot = filepath.Join(filepath.Dir(p.HostWorkspaceRoot), "terraform-ouro-2")
			case "home":
				p.Agent.HostHome = t.TempDir()
			case "state":
				p.Agent.StateRoot = t.TempDir()
			case "sandbox":
				p.Agent.SandboxWorktree = "/foreign"
			case "isolation":
				p.IsolationProfile = ""
			case "source-root":
				p.HostRuntimeSupportRoot = t.TempDir()
			case "optional-bind":
				p.Binds[0].Required = false
			case "foreign-bind":
				p.Binds[0].Source = filepath.Join(t.TempDir(), "foreign")
			case "duplicate-bind":
				p.Binds = append(p.Binds, p.Binds[0])
			}
			err := ensureIsolatedProductRuntimeSupport(p)
			if kind != "project" && kind != "repo" && kind != "index" && err == nil {
				t.Fatalf("foreign %s accepted", kind)
			}
			for _, root := range []string{target, filepath.Join(p.HostWorkspaceRoot, ".devkit")} {
				if _, err := os.Lstat(root); !os.IsNotExist(err) {
					t.Fatalf("%s materialized before rejection: %s %v", kind, root, err)
				}
			}
		})
	}
}

package nativecmd

import (
	"bytes"
	"crypto/sha256"
	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/devkitpaths"
	"devkit/cli/devctl/internal/runtime/agent"
	runtimebroker "devkit/cli/devctl/internal/runtime/broker"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func terraformToolsPlan() nativeplan.Plan {
	const workspace = "/home/bayesartre/dev/control-plane-worktrees/terraform-ouro-1"
	const state = "/home/bayesartre/dev/.devkit/native-agents/ouroboros-terraform-agent1"
	return nativeplan.Plan{Agent: agent.Spec{ID: agent.ID{Project: "ouroboros-terraform", Repo: "ouroboros-terraform", Index: 1}, HostWorktree: workspace + "/ouroboros-terraform", SandboxWorktree: "/workspaces/dev/ouroboros-terraform", HostHome: state + "/home", SandboxHome: "/agent-state/ouroboros-terraform-agent1/home", StateRoot: state}, HostWorkspaceRoot: workspace, SandboxWorkspaceRoot: "/workspaces/dev", IsolationProfile: "workspace-egress", GUITargetConfig: &nativeplan.GUITargetConfigProjection{TargetID: "terraform-ouro-1", Kind: "devkit-agent", ExpectedExecutionHost: "shadow-throne"}}
}
func terraformToolsCommand() []string {
	return []string{"/run/current-system/sw/bin/ouro-terra-runtime", "--", "/nix/store/selected-codex/bin/codex", "app-server", "-c", `projects."/workspaces/dev/ouroboros-terraform".trust_level="trusted"`, "-c", `projects."/home/bayesartre/dev/control-plane-worktrees/terraform-ouro-1/ouroboros-terraform".trust_level="trusted"`, "--listen", "unix:///agent-state/ouroboros-terraform-agent1/home/.codex/a1-app.sock", "--analytics-default-enabled"}
}
func TestTerraformToolsSelectedCommandAndGeometry(t *testing.T) {
	p := terraformToolsPlan()
	command := terraformToolsCommand()
	if err := launch.ValidateTerraformToolsPlan(p, "shadow-throne"); err != nil {
		t.Fatal(err)
	}
	if err := validateTerraformToolsAppServerCommand(p, command, command[2]); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"target", "host", "index", "project", "workspace", "home", "isolation", "projection", "arbitrary", "override", "codex"} {
		t.Run(kind, func(t *testing.T) {
			p := terraformToolsPlan()
			c := terraformToolsCommand()
			host := "shadow-throne"
			switch kind {
			case "target":
				p.GUITargetConfig.TargetID = "terraform-ouro-2"
			case "host":
				host = "shadow"
			case "index":
				p.Agent.ID.Index = 2
			case "project":
				p.Agent.ID.Project = "dev-all"
			case "workspace":
				p.HostWorkspaceRoot += "/.."
			case "home":
				p.Agent.HostHome = "/tmp/home"
			case "isolation":
				p.IsolationProfile = "none"
			case "projection":
				p.GUITargetConfig = nil
			case "arbitrary":
				c = []string{"/bin/sh", "-c", "terraform plan"}
			case "override":
				c = append(c, "--config", "foreign")
			case "codex":
				c[2] = "/tmp/codex"
			}
			if launch.ValidateTerraformToolsPlan(p, host) == nil && validateTerraformToolsAppServerCommand(p, c, command[2]) == nil {
				t.Fatal("accepted foreign tools startup")
			}
		})
	}
}
func TestTerraformToolsExecPhasesAreClosed(t *testing.T) {
	c := terraformToolsCommand()
	for _, phase := range []string{"", terraformToolsPreparationPhase} {
		if err := validateTerraformToolsExecSelection(phase, false, false, "ouroboros-terraform", "terraform-ouro-1", c); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"unknown", "software", "managed", "project", "target", "shell"} {
		phase := terraformToolsPreparationPhase
		software, managed := false, false
		project, target := "ouroboros-terraform", "terraform-ouro-1"
		command := c
		switch kind {
		case "unknown":
			phase = "skip-auth"
		case "software":
			software = true
		case "managed":
			managed = true
		case "project":
			project = "dev-all"
		case "target":
			target = "terraform-ouro-2"
		case "shell":
			command = []string{"bash", "-c", "true"}
		}
		if err := validateTerraformToolsExecSelection(phase, software, managed, project, target, command); err == nil {
			t.Fatalf("accepted %s", kind)
		}
	}
	if terraformToolsProviderReadiness(terraformToolsPreparationPhase) != "unassessed" || terraformToolsProviderHydration(terraformToolsPreparationPhase) != "deferred" || strings.Contains(terraformToolsReadinessStatus(terraformToolsPreparationPhase, true, "ready"), "provider") {
		t.Fatal("invalid provider assertion")
	}
}

func TestTerraformToolsImmutableChecksAdmission(t *testing.T) {
	ctx, cfg, _ := terraformSelection(t)
	root := "/nix/store/selected-devctl"
	ctx.Paths.Root = root
	ctx.Paths.RuntimeAuthorityRoot = root
	ctx.Paths.OverlayPaths = []string{root + "/overlays"}
	cfg.SourceDir = root + "/overlays/ouroboros-terraform"
	cfg.Defaults.BaseBranch = "main"
	cfg.Defaults.BranchPrefix = "agent"
	if err := validateTerraformToolsSource(terraformToolsPreparationPhase, ctx, cfg); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"root", "overlays", "extra-overlay", "source", "origin", "branch"} {
		cc := *ctx
		cc.Paths = ctx.Paths
		other := cfg
		switch kind {
		case "root":
			cc.Paths.Root = "/tmp/root"
		case "overlays":
			cc.Paths.OverlayPaths = []string{"/tmp/arbitrary-checks"}
		case "extra-overlay":
			cc.Paths.OverlayPaths = append([]string{}, ctx.Paths.OverlayPaths...)
			cc.Paths.OverlayPaths = append(cc.Paths.OverlayPaths, "/tmp/checks")
		case "source":
			other.SourceDir = "/tmp/source-fixed-looking-overlay"
		case "origin":
			other.Defaults.Origin = "foreign"
		case "branch":
			other.Defaults.BranchPrefix = "foreign"
		}
		if err := validateTerraformToolsSource(terraformToolsPreparationPhase, &cc, other); err == nil {
			t.Fatalf("accepted %s", kind)
		}
		if err := validateTerraformToolsSource("", &cc, other); err != nil {
			t.Fatalf("changed default source behavior: %v", err)
		}
	}
}

func TestTerraformToolsRejectsRuntimeAndLifecycleOverrides(t *testing.T) {
	for _, args := range [][]string{{"exec", "1", "--flake", "foreign", "--", "anything"}, {"prepare", "--proxy", "foreign"}, {"readiness", "--repo-check", "arbitrary"}, {"readiness", "--full"}, {"exec", "1", "--broker-socket", "/tmp/broker", "--", "anything"}, {"prepare", "--dry-run"}, {"shell", "--preparation-phase", terraformToolsPreparationPhase}} {
		if err := validateTerraformToolsArguments(args); err == nil {
			t.Fatalf("accepted override %v", args)
		}
	}
}

func TestTerraformToolsTopExecParserPropagation(t *testing.T) {
	ctx, cfg, _ := terraformSelection(t)
	root := "/nix/store/selected-devctl"
	ctx.Paths.Root = root
	ctx.Paths.RuntimeAuthorityRoot = root
	ctx.Paths.OverlayPaths = []string{root + "/overlays"}
	cfg.SourceDir = root + "/overlays/ouroboros-terraform"
	cfg.Defaults.BaseBranch = "main"
	cfg.Defaults.BranchPrefix = "agent"
	ctx.Args = []string{"1", "--repo", "ouroboros-terraform", "--worktree-root", "/home/bayesartre/dev/control-plane-worktrees", "--agent-state-root", "/home/bayesartre/dev/.devkit/native-agents", "--workspace-root", "/home/bayesartre/dev/control-plane-worktrees/terraform-ouro-1", "--gui-target-id", "terraform-ouro-1", "--isolation-profile", "workspace-egress", "--preparation-phase", terraformToolsPreparationPhase, "--"}
	ctx.Args = append(ctx.Args, terraformToolsCommand()...)
	parsed, err := parseTopExecArgs(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTerraformToolsSource(parsed.preparationPhase, ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if err := validateTerraformToolsExecSelection(parsed.preparationPhase, parsed.softwareOnly, parsed.managedAppServer, ctx.Project, parsed.guiTargetID, parsed.command); err != nil {
		t.Fatal(err)
	}
	if err := validateTerraformToolsAppServerCommand(terraformToolsPlan(), parsed.command, parsed.command[2]); err != nil {
		t.Fatal(err)
	}
}

func TestTerraformToolsStartupVerificationReportConsequences(t *testing.T) {
	// Execute the real report composition with non-secret source-check commands.
	// Namespace/broker startup and preparation are separate real fixture controls.
	endpoints := func(p nativeplan.Plan, _ runtimebroker.Config, _ bool, run func(nativeplan.Plan) error) error {
		return run(p)
	}
	prepared := 0
	prepare := func(nativeplan.Plan) error { prepared++; return nil }
	runChecks := func(_ nativeplan.Plan, checks []sandboxReadinessCheck) ([]sandboxReadinessResult, error) {
		results := []sandboxReadinessResult{}
		for _, check := range checks {
			ok := true
			detail := ""
			if check.Name == "source-fixed-tool" {
				output, err := exec.Command("sh", "-c", check.Command).CombinedOutput()
				ok = err == nil
				detail = string(output)
			}
			results = append(results, sandboxReadinessResult{Phase: check.Phase, Name: check.Name, OK: ok, Detail: detail})
		}
		return results, nil
	}
	for _, command := range []string{"exit 7", "test 1 -eq 1"} {
		report := runReadinessReportPreparedWith(terraformToolsPlan(), runtimebroker.Config{}, []runtimeCheck{{Name: "source-fixed-tool", Command: command}}, nil, prepare, endpoints, runChecks)
		evidence := nativeReadinessEvidenceFor(terraformToolsPreparationPhase, config.ReadinessModeRuntimeOnly, report)
		if evidence.RuntimeReady != (command != "exit 7") || evidence.RepoReady || evidence.CapacityAvailable || evidence.ProviderReadiness != "unassessed" || evidence.ProviderHydration != "deferred" {
			t.Fatalf("tools verification overstated readiness %#v", evidence)
		}
		if evidence.RuntimeReady && evidence.Status != "tools-ready" {
			t.Fatalf("tools success labelled full-ready %#v", evidence)
		}
	}
	if prepared != 2 {
		t.Fatalf("phase preparation callback lost or duplicated: %d", prepared)
	}
	// Default preparation still refuses this non-existent real host/worktree.
	report := runReadinessReportPreparedWith(terraformToolsPlan(), runtimebroker.Config{}, nil, nil, launch.Prepare, endpoints, runChecks)
	if report.RuntimeReady() {
		t.Fatal("default preparation stopped enforcing custody")
	}
}

// These controls use the real overlay parser, readiness batch executor/script,
// result parser and report/evidence composition. Only fixed production paths
// are mapped to an isolated nonsecret fixture. No actual consumer is modified.
type terraformReadinessFixture struct {
	root, worktree, home, baseBin, toolBin, store, wrapper, manifest string
	source, config, support, calls                                   string
	records                                                          []map[string]any
	wholeBatch                                                       bool
}

func terraformReadinessWrite(t *testing.T, path, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	// The normal Nix test builder is unprivileged. Adverse fixtures rewrite
	// their own read-only manifest/source, then restore the requested mode.
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			t.Fatalf("refuse fixture rewrite of nonregular path: %s", path)
		}
		if err := os.Chmod(path, 0600); err != nil {
			t.Fatal(err)
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func terraformReadinessNewFixture(t *testing.T) *terraformReadinessFixture {
	t.Helper()
	root, err := os.MkdirTemp("", "tf-ready-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	f := &terraformReadinessFixture{root: root, worktree: filepath.Join(root, "workspace", "ouroboros-terraform"), home: filepath.Join(root, "home"), baseBin: filepath.Join(root, "base-bin"), store: filepath.Join(root, "nix", "store"), calls: filepath.Join(root, "provider-calls")}
	f.baseBin = filepath.Join(f.store, "generic-tools", "bin")
	f.toolBin = filepath.Join(f.store, "selected-tools", "bin")
	f.wrapper = filepath.Join(f.store, "selected-runtime", "bin", "ouro-terra-runtime")
	f.manifest = filepath.Join(root, "immutable-profile", "codex-config-projections.json")
	f.source = filepath.Join(f.store, "terraform-codex-config.toml")
	f.config = filepath.Join(f.home, ".codex", "config.toml")
	f.support = filepath.Join(root, "workspace", ".devkit")
	for _, dir := range []string{f.worktree, f.home, f.baseBin, f.toolBin, f.support, filepath.Join(f.home, "rollouts")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Normal shell/JSON/hash machinery comes from the native check toolchain.
	// Terraform binaries are deliberately absent from the generic PATH.
	for _, name := range []string{"bash", "sh", "python3", "jq", "grep", "timeout", "mktemp", "base64", "rm", "readlink", "sha256sum", "cut", "tr", "env"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("native check fixture requires %s: %v", name, err)
		}
		if err := os.Symlink(path, filepath.Join(f.baseBin, name)); err != nil {
			t.Fatal(err)
		}
	}
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	shebang := "#!" + shellPath + "\n"
	for _, name := range []string{"git", "ssh", "curl", "docker", "codex"} {
		body := shebang + "exit 0\n"
		switch name {
		case "curl":
			body = shebang + "printf 'OK\\n'\n"
		case "docker":
			body = shebang + "[ \"$1\" = --version ] || exit 97\nprintf 'Docker fixture\\n'\n"
		case "codex":
			body = shebang + "[ \"$1\" = --version ] || exit 97\nprintf 'codex 0.160.0\\n'\n"
		}
		terraformReadinessWrite(t, filepath.Join(f.baseBin, name), body, 0755)
	}
	// Closed fake tool contracts refuse any provider/mutation/auth arguments.
	terra := shebang + "if [ \"$1\" = version ] && [ \"$2\" = -json ] && [ \"$#\" = 2 ]; then printf '%s\\n' '{\"terraform_version\":\"1.15.8\",\"platform\":\"linux_amd64\"}'; else printf call >" + nativeShellQuote(f.calls) + "; exit 98; fi\n"
	terraformReadinessWrite(t, filepath.Join(f.toolBin, "terraform"), terra, 0755)
	terraformReadinessWrite(t, filepath.Join(f.toolBin, "packer"), shebang+"[ \"$1\" = version ] && [ \"$#\" = 1 ] || { printf call >"+nativeShellQuote(f.calls)+"; exit 98; }\nprintf 'Packer v1.11.2\\n'\n", 0755)
	terraformReadinessWrite(t, filepath.Join(f.toolBin, "aws"), shebang+"[ \"$1\" = --version ] && [ \"$#\" = 1 ] || { printf call >"+nativeShellQuote(f.calls)+"; exit 98; }\nprintf 'aws-cli/2.27.2 fixture\\n'\n", 0755)
	// A whole-batch wrapper would incorrectly expose this vendor Codex version.
	terraformReadinessWrite(t, filepath.Join(f.toolBin, "codex"), shebang+"printf 'codex 0.139.0\\n'\n", 0755)
	b, err := os.ReadFile(filepath.Join(f.toolBin, "terraform"))
	if err != nil {
		t.Fatal(err)
	}
	wrapper := shebang + "export PATH=\"" + f.toolBin + ":" + f.baseBin + "\"\nexport OURO_TERRAFORM_BIN=" + nativeShellQuote(filepath.Join(f.toolBin, "terraform")) + "\nexport OURO_TERRAFORM_VERSION=1.15.8\nexport OURO_TERRAFORM_PLATFORM=linux_amd64\nexport OURO_TERRAFORM_SHA256=" + nativeShellQuote(fmt.Sprintf("%x", sha256.Sum256(b))) + "\n[ \"${1:-}\" != -- ] || shift\nexec \"$@\"\n"
	terraformReadinessWrite(t, f.wrapper, wrapper, 0755)
	config := "# fleet_config_profile = \"terraform\"\n# source = nixos-wsl codex config\n[profiles.openai]\nmodel_provider = \"openai\"\n"
	terraformReadinessWrite(t, f.source, config, 0444)
	terraformReadinessWrite(t, f.config, config, 0600)
	terraformReadinessWrite(t, filepath.Join(f.home, ".codex", "auth.json"), "{\"OPENAI_API_KEY\":\"nonsecret-test-fixture\"}\n", 0600)
	terraformReadinessWrite(t, filepath.Join(f.support, "ouro8-governance-env.sh"), "export DEVKIT_GOVERNANCE_REPO_PATHS='ouroboros-terraform="+f.worktree+"'\n", 0644)
	terraformReadinessWrite(t, filepath.Join(f.support, "ouro8-governance-repo-env.json"), "{}\n", 0644)
	f.records = []map[string]any{{"targetId": "terraform-ouro-1", "configProfile": "terraform", "kind": "devkit-agent", "expectedExecutionHost": "shadow-throne", "project": "ouroboros-terraform", "repo": "ouroboros-terraform", "agentIndex": 1, "workspaceRoot": "/home/bayesartre/dev/control-plane-worktrees/terraform-ouro-1", "hostWorktree": "/home/bayesartre/dev/control-plane-worktrees/terraform-ouro-1/ouroboros-terraform", "hostHome": "/home/bayesartre/dev/.devkit/native-agents/ouroboros-terraform-agent1/home", "source": f.source, "sourceSha256": fmt.Sprintf("%x", sha256.Sum256([]byte(config)))}}
	f.writeManifest(t)
	listener, err := net.Listen("unix", filepath.Join(f.root, "broker.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return f
}

func (f *terraformReadinessFixture) writeManifest(t *testing.T) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"schemaVersion": "devkit/gui-codex-config-projections/v1", "projections": f.records})
	if err != nil {
		t.Fatal(err)
	}
	terraformReadinessWrite(t, f.manifest, string(b), 0444)
}

func terraformReadinessActualOverlayChecks(t *testing.T) []runtimeCheck {
	t.Helper()
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx := &cmdregistry.Context{Project: "ouroboros-terraform", Paths: devkitpaths.Paths{OverlayPaths: []string{filepath.Join(root, "overlays")}}}
	checks, err := runtimeChecksFor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return checks
}

func (f *terraformReadinessFixture) mapFixedPaths(command string) string {
	// Exact fixture substitutions only; production exposes no override interface.
	return strings.NewReplacer("/run/current-system/sw/bin/ouro-terra-runtime", f.wrapper, "/run/current-system/etc/fleet/source/codex-config-projections.json", f.manifest, "/nix/store/", f.store+"/", "/workspaces/dev", filepath.Dir(f.worktree), "/usr/bin/env", filepath.Join(f.baseBin, "env"), "/bin/sh", filepath.Join(f.baseBin, "sh")).Replace(command)
}

func (f *terraformReadinessFixture) report(t *testing.T, checks []runtimeCheck) nativeReadinessEvidence {
	t.Helper()
	p := terraformToolsPlan()
	p.Agent.HostHome = f.home
	p.BrokerEndpoint = filepath.Join(f.root, "broker.sock")
	endpoints := func(p nativeplan.Plan, _ runtimebroker.Config, _ bool, run func(nativeplan.Plan) error) error {
		return run(p)
	}
	executor := func(_ nativeplan.Plan, batch []sandboxReadinessCheck) ([]sandboxReadinessResult, error) {
		for i := range batch {
			batch[i].Command = f.mapFixedPaths(batch[i].Command)
		}
		argv := sandboxReadinessCommand(batch)
		cmd := exec.Command(argv[0], argv[1:]...)
		if f.wholeBatch {
			cmd = exec.Command(f.wrapper, append([]string{"--"}, argv...)...)
		}
		cmd.Dir = f.worktree
		cmd.Env = []string{"PATH=" + f.baseBin, "HOME=" + f.home, "CODEX_HOME=" + filepath.Join(f.home, ".codex"), "DEVKIT_NATIVE_AGENT=1", "CODEX_ROLLOUT_DIR=" + filepath.Join(f.home, "rollouts"), "DOCKER_HOST=unix://" + p.BrokerEndpoint, "DEVKIT_READINESS_CHECK_TIMEOUT_SECONDS=10"}
		b, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("actual fixture batch: %w: %s", err, b)
		}
		return parseSandboxReadinessResults(string(b))
	}
	report := runReadinessReportPreparedWith(p, runtimebroker.Config{}, checks, nil, func(nativeplan.Plan) error { return nil }, endpoints, executor)
	if _, err := os.Stat(f.calls); err == nil {
		t.Fatal("pure readiness invoked a provider/mutation/auth argument")
	}
	return nativeReadinessEvidenceFor(terraformToolsPreparationPhase, config.ReadinessModeRuntimeOnly, report)
}

func TestTerraformSourceSelectedReadinessCompleteBatch(t *testing.T) {
	f := terraformReadinessNewFixture(t)
	checks := terraformReadinessActualOverlayChecks(t)
	e := f.report(t, checks)
	if !e.RuntimeReady || e.Status != "tools-ready" || e.RepoReady || e.CapacityAvailable || e.ProviderReadiness != "unassessed" || e.ProviderHydration != "deferred" {
		t.Fatalf("actual parsed overlay/batch must earn only tools readiness: %#v", e)
	}
}

func TestTerraformSourceSelectedReadinessRefusesWrongConsequences(t *testing.T) {
	for _, kind := range []string{"missing-terraform", "terraform-version", "terraform-platform", "terraform-path", "terraform-hash", "packer-version", "aws-version", "whole-batch-codex", "missing-record", "duplicate-record", "foreign-profile", "foreign-geometry", "noncanonical-source", "source-hash", "changed-config", "product-profile", "source-symlink", "config-symlink", "config-mode", "missing-support", "foreign-support", "missing-auth"} {
		t.Run(kind, func(t *testing.T) {
			f := terraformReadinessNewFixture(t)
			checks := terraformReadinessActualOverlayChecks(t)
			switch kind {
			case "missing-terraform":
				if err := os.Remove(filepath.Join(f.toolBin, "terraform")); err != nil {
					t.Fatal(err)
				}
			case "terraform-version", "terraform-platform", "terraform-path", "terraform-hash":
				b, err := os.ReadFile(f.wrapper)
				if err != nil {
					t.Fatal(err)
				}
				s := string(b)
				switch kind {
				case "terraform-version":
					s = strings.Replace(s, "OURO_TERRAFORM_VERSION=1.15.8", "OURO_TERRAFORM_VERSION=9.9.9", 1)
				case "terraform-platform":
					s = strings.Replace(s, "OURO_TERRAFORM_PLATFORM=linux_amd64", "OURO_TERRAFORM_PLATFORM=foreign", 1)
				case "terraform-path":
					s = strings.Replace(s, "OURO_TERRAFORM_BIN=", "OURO_TERRAFORM_BIN=foreign", 1)
				case "terraform-hash":
					s = strings.Replace(s, "OURO_TERRAFORM_SHA256=", "OURO_TERRAFORM_SHA256=foreign", 1)
				}
				terraformReadinessWrite(t, f.wrapper, s, 0755)
			case "packer-version", "aws-version":
				name, old, new := "packer", "1.11.2", "9.9.9"
				if kind == "aws-version" {
					name, old = "aws", "2.27.2"
				}
				b, err := os.ReadFile(filepath.Join(f.toolBin, name))
				if err != nil {
					t.Fatal(err)
				}
				terraformReadinessWrite(t, filepath.Join(f.toolBin, name), strings.Replace(string(b), old, new, 1), 0755)
			case "whole-batch-codex":
				f.wholeBatch = true
			case "missing-record":
				f.records = nil
				f.writeManifest(t)
			case "duplicate-record":
				f.records = append(f.records, f.records[0])
				f.writeManifest(t)
			case "foreign-profile":
				f.records[0]["configProfile"] = "product-governance"
				f.writeManifest(t)
			case "foreign-geometry":
				f.records[0]["hostWorktree"] = "/foreign"
				f.writeManifest(t)
			case "noncanonical-source":
				f.records[0]["source"] = filepath.Join(f.root, "foreign.toml")
				f.writeManifest(t)
			case "source-hash":
				f.records[0]["sourceSha256"] = strings.Repeat("0", 64)
				f.writeManifest(t)
			case "changed-config":
				terraformReadinessWrite(t, f.config, "changed", 0600)
			case "product-profile":
				b, err := os.ReadFile(f.source)
				if err != nil {
					t.Fatal(err)
				}
				b = append(b, []byte("[mcp_servers.governance]\n")...)
				terraformReadinessWrite(t, f.source, string(b), 0444)
				terraformReadinessWrite(t, f.config, string(b), 0600)
				f.records[0]["sourceSha256"] = fmt.Sprintf("%x", sha256.Sum256(b))
				f.writeManifest(t)
			case "source-symlink", "config-symlink":
				path := f.source
				if kind == "config-symlink" {
					path = f.config
				}
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(path); err != nil {
					t.Fatal(err)
				}
				other := filepath.Join(f.root, "redirected-file")
				terraformReadinessWrite(t, other, string(b), 0600)
				if err = os.Symlink(other, path); err != nil {
					t.Fatal(err)
				}
			case "config-mode":
				if err := os.Chmod(f.config, 0644); err != nil {
					t.Fatal(err)
				}
			case "missing-support":
				if err := os.Remove(filepath.Join(f.support, "ouro8-governance-repo-env.json")); err != nil {
					t.Fatal(err)
				}
			case "foreign-support":
				terraformReadinessWrite(t, filepath.Join(f.support, "ouro8-governance-env.sh"), "foreign", 0644)
			case "missing-auth":
				if err := os.Remove(filepath.Join(f.home, ".codex", "auth.json")); err != nil {
					t.Fatal(err)
				}
			}
			e := f.report(t, checks)
			if e.RuntimeReady || e.Status == "tools-ready" || e.RepoReady || e.CapacityAvailable || len(e.FailedChecks) == 0 {
				t.Fatalf("%s falsely earned readiness: %#v", kind, e)
			}
			wantFailed := "terraform-config-profile"
			switch kind {
			case "missing-terraform", "terraform-version", "terraform-platform", "terraform-path", "terraform-hash", "packer-version", "aws-version":
				wantFailed = "terraform-packer-aws"
			case "whole-batch-codex":
				wantFailed = "codex-version"
			case "missing-auth":
				wantFailed = "codex-auth-present"
			}
			found := false
			for _, failed := range e.FailedChecks {
				if failed.Name == wantFailed {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s failed unrelated checks; required failure %s: %#v", kind, wantFailed, e.FailedChecks)
			}
		})
	}
}

func TestTerraformReadinessFixtureUsesExactUnchangedGenericAndAuthChecks(t *testing.T) {
	// This also binds fixture config comparison to exact bytes, not marker presence.
	f := terraformReadinessNewFixture(t)
	a, err := os.ReadFile(f.source)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(f.config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("fixture projected source differs")
	}
	checks := terraformReadinessActualOverlayChecks(t)
	for _, c := range checks {
		if c.Name == "codex-version" && strings.Contains(c.Command, "ouro-terra-runtime") {
			t.Fatal("GUI vendor check wrapped in Terraform")
		}
	}
}

func TestTerraformOwnershipRefusalReportsPublicMetadataAndStaysFailed(t *testing.T) {
	// Fault injection exercises diagnostic emission through the real parsed batch
	// and report. It does not claim a foreign/mapped UID or namespace acceptance.
	const ownerPredicate = "if info.st_uid != os.geteuid() and not (immutable_source and info.st_uid == 0):"
	for _, fileClass := range []string{"immutable-source", "projected-config"} {
		t.Run(fileClass, func(t *testing.T) {
			f := terraformReadinessNewFixture(t)
			const privateMarker = "diagnostic-must-not-read-config-or-auth-content-93d25e"
			b, err := os.ReadFile(f.source)
			if err != nil {
				t.Fatal(err)
			}
			b = append(b, []byte("# "+privateMarker+"\n")...)
			terraformReadinessWrite(t, f.source, string(b), 0444)
			terraformReadinessWrite(t, f.config, string(b), 0600)
			terraformReadinessWrite(t, filepath.Join(f.home, ".codex", "auth.json"), `{"OPENAI_API_KEY":"`+privateMarker+`"}`, 0600)
			f.records[0]["sourceSha256"] = fmt.Sprintf("%x", sha256.Sum256(b))
			f.writeManifest(t)
			checks := terraformReadinessActualOverlayChecks(t)
			injected := false
			for i := range checks {
				if checks[i].Name != "terraform-config-profile" {
					continue
				}
				if strings.Count(checks[i].Command, ownerPredicate) != 1 {
					t.Fatal("production ownership predicate changed")
				}
				condition := "if immutable_source:"
				if fileClass == "projected-config" {
					condition = "if not immutable_source:"
				}
				checks[i].Command = strings.Replace(checks[i].Command, ownerPredicate, condition, 1)
				injected = true
			}
			if !injected {
				t.Fatal("actual profile check absent")
			}
			e := f.report(t, checks)
			if e.RuntimeReady || e.Status == "tools-ready" || e.RepoReady || e.CapacityAvailable {
				t.Fatalf("owner refusal earned readiness: %#v", e)
			}
			if len(e.FailedChecks) != 1 || e.FailedChecks[0].Name != "terraform-config-profile" {
				t.Fatalf("unrelated failure: %#v", e.FailedChecks)
			}
			detail := e.FailedChecks[0].Detail
			if !strings.Contains(detail, "exit status 1: Terraform profile readiness: foreign source or projected file owner;") {
				t.Fatalf("nonzero owner refusal lost: %s", detail)
			}
			_, raw, ok := strings.Cut(detail, "ownership_readback=")
			if !ok {
				t.Fatal("public ownership evidence absent")
			}
			var evidence struct {
				Schema    string `json:"schema"`
				FileClass string `json:"failedFileClass"`
				Opened    struct {
					Path string `json:"path"`
					UID  int    `json:"uid"`
				} `json:"failedOpenedFile"`
				Checker struct {
					EUID   int    `json:"euid"`
					UIDMap string `json:"uidMap"`
					GIDMap string `json:"gidMap"`
				} `json:"checker"`
				Home         string `json:"home"`
				CodexHome    string `json:"codexHome"`
				ContentsRead bool   `json:"fileContentsReadForDiagnostic"`
			}
			if err := json.Unmarshal([]byte(raw), &evidence); err != nil {
				t.Fatalf("invalid diagnostic JSON: %v: %s", err, raw)
			}
			wantPath := f.source
			if fileClass == "projected-config" {
				wantPath = f.config
			}
			if evidence.Schema != "devkit/tf-profile-ownership-refusal/v1" || evidence.FileClass != fileClass || evidence.Opened.Path != wantPath || evidence.Opened.UID != os.Geteuid() || evidence.Checker.EUID != os.Geteuid() || evidence.Home != f.home || evidence.CodexHome != filepath.Dir(f.config) || evidence.ContentsRead {
				t.Fatalf("wrong actual diagnostic fields: %s", raw)
			}
			if len(evidence.Checker.UIDMap) == 0 || len(evidence.Checker.GIDMap) == 0 || len(evidence.Checker.UIDMap) > 8192 || len(evidence.Checker.GIDMap) > 8192 {
				t.Fatal("namespace maps absent or unbounded")
			}
			if strings.Contains(raw, privateMarker) || strings.Contains(raw, "OPENAI_API_KEY") || strings.Contains(raw, "[profiles.openai]") {
				t.Fatal("diagnostic disclosed source/config/auth content")
			}
		})
	}
}

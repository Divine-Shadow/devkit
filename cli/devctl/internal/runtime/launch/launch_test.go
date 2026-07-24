package launch

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devkit/cli/devctl/internal/devkitpaths"
	"devkit/cli/devctl/internal/governanceentrypoint"
	"devkit/cli/devctl/internal/runtime/agent"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"devkit/cli/devctl/internal/sshauthority"
)

const testRuntimeSourceRev = "1111111111111111111111111111111111111111"
const testArtifactColumnVersion = "0.0.0-runtime-constructor-test"
const testArtifactColumnJarSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestMain(m *testing.M) {
	ambientCodexConfig, hadAmbientCodexConfig := os.LookupEnv("DEVKIT_CODEX_CONFIG_SOURCE")
	if err := os.Unsetenv("DEVKIT_CODEX_CONFIG_SOURCE"); err != nil {
		panic(err)
	}
	testExecutable, err := os.Executable()
	if err != nil {
		panic(err)
	}
	gitExecutable, err := exec.LookPath("git")
	if err != nil {
		panic(err)
	}
	gitExecutable, err = filepath.Abs(gitExecutable)
	if err != nil {
		panic(err)
	}
	packageGitExecutable = gitExecutable
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
	codexSystemConfigPath = filepath.Join(os.TempDir(), "devkit-launch-test-missing-codex-config.toml")
	resolveOuroGovernanceRuntimeIdentityForPlan = func(string) (ouroGovernanceRuntimeIdentity, error) {
		return ouroGovernanceRuntimeIdentity{
			RuntimeBundlePath: "/nix/store/ffffffffffffffffffffffffffffffff-dev-all-runtime-bundle",
		}, nil
	}
	code := m.Run()
	if hadAmbientCodexConfig {
		_ = os.Setenv("DEVKIT_CODEX_CONFIG_SOURCE", ambientCodexConfig)
	}
	os.Exit(code)
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

func TestDevAllRuntimeExportsSourceFreePublicBundleConstructor(t *testing.T) {
	root := devkitRootFromPackage(t)
	runtimeNix := readTestFile(t, filepath.Join(root, "overlays", "dev-all", "runtime.nix"))
	for _, want := range []string{
		"packages.diagnosticRuntimeBundle",
		"source ${packages.diagnosticRuntimeBundle}/share/dev-all-runtime-bundle/identity.env",
		"Production Fleet composition",
	} {
		if !strings.Contains(runtimeNix, want) {
			t.Fatalf("dev-all runtime missing %q:\n%s", want, runtimeNix)
		}
	}

	flakeNix := readTestFile(t, filepath.Join(root, "flake.nix"))
	for _, want := range []string{
		`mkDevAllRuntimeBundle = import ./nix/mk-dev-all-runtime-bundle.nix;`,
		`mkDiagnosticRuntimeFixture =`,
		`mkDiagnosticRuntimeBundle =`,
		`runtimeBundle = mkDiagnosticRuntimeBundle pkgs;`,
		`dev-all-runtime-bundle = runtimeBundle;`,
		`dev-all-runtime-bundle-bridge-smoke = mkDevAllRuntimeBundleBridgeSmoke pkgs;`,
		`dev-all-runtime-bundle-public-constructor =`,
		`mkDevAllRuntimeBundleConstructorContract pkgs;`,
	} {
		if !strings.Contains(flakeNix, want) {
			t.Fatalf("flake missing %q:\n%s", want, flakeNix)
		}
	}
	bundleNix := readTestFile(t, filepath.Join(root, "nix", "dev-all-runtime-bundle.nix"))
	for _, want := range []string{
		`schema = "fleet-runtime-authority/v1";`,
		`exactSourceIds = [`,
		`sourceShapeIsExact =`,
		`authorityShapeIsExact =`,
		`authorityTemplate =`,
		`identityEnvTemplate =`,
		`identity.env`,
		`identity.json`,
		`identity-fingerprint`,
		`identity-nul`,
		`plugin-smoke`,
		`bundle_root='@bundleRoot@'`,
		`substitute '${launcherTemplate}' "$out/bin/dev-all-runtime-bundle"`,
		`ln -s '${runtime.artifactColumnPlugin.repositoryPath}' "$out/runtime/artifact-column-plugin-repository"`,
	} {
		if !strings.Contains(bundleNix, want) {
			t.Fatalf("runtime bundle missing %q:\n%s", want, bundleNix)
		}
	}
	publicConstructorNix := readTestFile(t, filepath.Join(root, "nix", "mk-dev-all-runtime-bundle.nix"))
	for _, want := range []string{
		`authoritative WSL flake selects`,
		`derives every input value`,
		`import ./dev-all-runtime-bundle.nix args`,
	} {
		if !strings.Contains(publicConstructorNix, want) {
			t.Fatalf("public runtime constructor missing %q:\n%s", want, publicConstructorNix)
		}
	}
	launchGo := readTestFile(t, filepath.Join(root, "cli", "devctl", "internal", "runtime", "launch", "launch.go"))
	resolverStart := strings.Index(launchGo, "func selectOuroGovernanceSystemRuntimeLauncher(")
	resolverEnd := strings.Index(launchGo, "\nfunc metadataEnvValue(")
	if resolverStart < 0 || resolverEnd <= resolverStart {
		t.Fatal("cannot isolate production governance runtime resolver")
	}
	resolverGo := launchGo[resolverStart:resolverEnd]
	for _, forbidden := range []string{
		`6826ff0ad172d35ce2eaeb62473ae26facb765a0`,
		`8e23ded5579e896c95b5a751f4d4a18da70049a9`,
		`builtins.getFlake`,
		`builtins.fetchGit`,
		`builtins.fetchTree`,
		`RuntimeSourceFlake`,
		`mkPinnedGovernanceJar`,
		`mkPinnedSubmitToCiJar`,
		`mkPinnedArtifactColumnPlugin`,
		`mkPinnedSbtControlPlaneRuntimeJar`,
		`mkDefaultDevAllRuntimeBundle`,
		`wsl-nix-dev-all-runtime-authority/v1`,
		`devkit-dev-all-runtime-identity/v1`,
	} {
		for label, content := range map[string]string{
			"flake":              flakeNix,
			"public constructor": publicConstructorNix,
			"bundle":             bundleNix,
			"runtime resolver":   resolverGo,
		} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s contains forbidden runtime authority %q", label, forbidden)
			}
		}
	}
	for _, forbidden := range []string{
		`git+file:///workspaces/dev/ouroboros-ide`,
		`/workspaces/dev/ouroboros-ide`,
	} {
		for label, content := range map[string]string{
			"public constructor": publicConstructorNix,
			"bundle":             bundleNix,
			"runtime resolver":   resolverGo,
		} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s contains forbidden runtime source locator %q", label, forbidden)
			}
		}
	}
	for _, forbidden := range []string{
		`exec.LookPath("nix")`,
		`/run/current-system/sw/bin/nix`,
		`#dev-all-runtime-bundle`,
		`"--print-out-paths"`,
		`DEVKIT_GOVERNANCE_RUNTIME_LAUNCHER`,
		`validateOuroGovernanceActivationRuntimeLauncher`,
	} {
		if strings.Contains(resolverGo, forbidden) {
			t.Fatalf("runtime resolver contains forbidden Nix build fallback %q", forbidden)
		}
	}
	if strings.Contains(bundleNix, "cp ") || strings.Contains(bundleNix, "cp -") {
		t.Fatalf("runtime bundle must retain Nix references instead of copying artifacts:\n%s", bundleNix)
	}
	if strings.Contains(bundleNix, `dirname -- "$0"`) {
		t.Fatalf("runtime bundle must not derive immutable package root from an aggregate-profile argv[0]:\n%s", bundleNix)
	}
	profileSmokeNix := readTestFile(t, filepath.Join(root, "nix", "dev-all-runtime-bundle-profile-smoke.nix"))
	for _, want := range []string{
		`pathsToLink = [ "/bin" ];`,
		`aggregate_profile_has_identity_env=false`,
		`BASH_ENV="$hook" ENV="$hook"`,
		`identity-fingerprint`,
		`identity-nul`,
		`governance-forward`,
		`direct_profile_equivalence=passed`,
	} {
		if !strings.Contains(profileSmokeNix, want) {
			t.Fatalf("runtime bundle profile smoke missing %q:\n%s", want, profileSmokeNix)
		}
	}
	for _, content := range []string{runtimeNix, bundleNix} {
		if strings.Contains(content, "ARTIFACT_COLUMN_PLUGIN_REPOSITORY=") {
			t.Fatalf("retired Artifact Column repository alias survived:\n%s", content)
		}
	}
	lifecycleNix := readTestFile(t, filepath.Join(root, "nix", "product-adapter-lifecycle-check.nix"))
	for _, want := range []string{
		`runtimeBundle =`,
		`mkDevAllRuntimeBundle ({ inherit pkgs; } // runtimeBundleFixture.constructorArgs);`,
		`.devkitProductAdapter.runtimeLauncherPath == $launcher`,
		`(.devkitProductAdapter | has("runtimeRoot") | not)`,
		`.devkitProductAdapter.governanceEnvPath == $governance`,
	} {
		if !strings.Contains(lifecycleNix, want) {
			t.Fatalf("Product adapter lifecycle does not consume public runtime bundle %q:\n%s", want, lifecycleNix)
		}
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

func TestRuntimeSourceRevisionRequiresExactLowercaseCommit(t *testing.T) {
	if !isExactRuntimeSourceRevision("1111111111111111111111111111111111111111") {
		t.Fatal("exact lowercase revision rejected")
	}
	for _, invalid := range []string{
		"",
		"1111111",
		"111111111111111111111111111111111111111g",
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	} {
		if isExactRuntimeSourceRevision(invalid) {
			t.Fatalf("invalid runtime source revision accepted: %q", invalid)
		}
	}
}

func TestParseOuroGovernanceRuntimeIdentityOutputIgnoresNixChatter(t *testing.T) {
	bundlePath := "/nix/store/ffffffffffffffffffffffffffffffff-dev-all-runtime-bundle"
	jarPath := "/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-subagent-governance-dev/share/subagent-governance/subagent-governance.jar"
	submitJarPath := "/nix/store/cccccccccccccccccccccccccccccccc-submit-to-ci-dev/share/submit-to-ci/submit-to-ci.jar"
	artifactRepoPath := "/nix/store/dddddddddddddddddddddddddddddddd-artifact-column-plugin-repository-" + testArtifactColumnVersion
	artifactMetadataPath := artifactRepoPath + "/share/artifact-column-plugin/metadata.env"
	artifactIvyPath := "ivy2/local/com.crib.bills.ouroboros/artifact-column-plugin_sbt2_3/" + testArtifactColumnVersion
	sbtRuntimeJarPath := "/nix/store/eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee-sbt-control-plane-runtime-dev/share/sbt-control-plane-runtime/sbt-control-plane-runtime.jar"
	javaHome := "/nix/store/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-openjdk/lib/openjdk"
	fields := []string{
		ouroGovernanceRuntimeIdentitySchema,
		bundlePath,
		testRuntimeSourceRev,
		testRuntimeSourceRev,
		testRuntimeSourceRev,
		testRuntimeSourceRev,
		jarPath, jarPath, jarPath, "deadbeef", "deadbeef",
		submitJarPath, submitJarPath + ".sha256", submitJarPath, "facefeed",
		"force", "6g", "force",
		artifactRepoPath, artifactMetadataPath,
		testArtifactColumnVersion, testRuntimeSourceRev, testRuntimeSourceRev[:7],
		artifactIvyPath, testArtifactColumnJarSHA256, "1", "0",
		sbtRuntimeJarPath, "54907ebe40a4cc7598dba7774a2d793fc7ca83d9c11811cc98fe96d189413872", "1", "0",
		javaHome,
	}
	out := "building '/nix/store/example.drv'...\n" + ouroGovernanceRuntimeIdentityMarker + "\x00" + strings.Join(fields, "\x00") + "\x00"

	identity, err := parseOuroGovernanceRuntimeIdentityOutput([]byte(out), "/workspaces/dev/devkit#dev-all")
	if err != nil {
		t.Fatalf("parse runtime identity: %v", err)
	}
	if identity.SchemaVersion != ouroGovernanceRuntimeIdentitySchema || identity.RuntimeBundlePath != bundlePath ||
		identity.SubmitToCiSourceRev != testRuntimeSourceRev ||
		identity.ArtifactColumnRuntimeSourceRev != testRuntimeSourceRev {
		t.Fatalf("bundle identity parsed incorrectly: %#v", identity)
	}
	if identity.LatestJarPath != jarPath || identity.ControlPlaneJar != jarPath || identity.ExpectedJarPath != jarPath {
		t.Fatalf("jar identity parsed incorrectly: %#v", identity)
	}
	if identity.ExpectedJarSHA256 != "deadbeef" || identity.SubagentExpectedJarSHA256 != "deadbeef" {
		t.Fatalf("sha identity parsed incorrectly: %#v", identity)
	}
	if identity.SubmitToCiJarPath != submitJarPath || identity.SubmitToCiHashPath != submitJarPath+".sha256" || identity.SubmitToCiExpectedJarPath != submitJarPath {
		t.Fatalf("submit-to-ci identity parsed incorrectly: %#v", identity)
	}
	if identity.SubmitToCiExpectedSHA256 != "facefeed" {
		t.Fatalf("submit-to-ci sha parsed incorrectly: %#v", identity)
	}
	if identity.SubmitSbt2ClientMode != "force" || identity.SubmitSbt2JavaXmx != "6g" || identity.LintInvarianceSbt2Mode != "force" {
		t.Fatalf("submit runtime authority parsed incorrectly: %#v", identity)
	}
	if identity.ArtifactColumnRepositoryPath != artifactRepoPath ||
		identity.ArtifactColumnMetadataEnv != artifactMetadataPath ||
		identity.ArtifactColumnVersion != testArtifactColumnVersion ||
		identity.ArtifactColumnSourceRev != testRuntimeSourceRev ||
		identity.ArtifactColumnSourceShortRev != testRuntimeSourceRev[:7] ||
		identity.ArtifactColumnIvyPath != artifactIvyPath ||
		identity.ArtifactColumnJarSHA256 != testArtifactColumnJarSHA256 ||
		identity.ArtifactColumnPinnedArtifact != "1" ||
		identity.ArtifactColumnFlakeArtifact != "0" {
		t.Fatalf("artifact-column plugin repository identity parsed incorrectly: %#v", identity)
	}
	if identity.SbtControlPlaneRuntimeJarPath != sbtRuntimeJarPath ||
		identity.SbtControlPlaneRuntimeJarSHA256 != "54907ebe40a4cc7598dba7774a2d793fc7ca83d9c11811cc98fe96d189413872" ||
		identity.SbtControlPlanePinnedArtifact != "1" ||
		identity.SbtControlPlaneFlakeArtifact != "0" {
		t.Fatalf("SBT control-plane runtime identity parsed incorrectly: %#v", identity)
	}
	if identity.JavaHome != javaHome {
		t.Fatalf("java home parsed incorrectly: %#v", identity)
	}
}

func TestParseOuroGovernanceRuntimeIdentityOutputRejectsClosedOrIncompleteIdentity(t *testing.T) {
	for name, output := range map[string][]byte{
		"closed":     nil,
		"no marker":  []byte("ordinary launcher output\n"),
		"incomplete": []byte(ouroGovernanceRuntimeIdentityMarker + "\x00" + strings.Repeat("field\x00", 31)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseOuroGovernanceRuntimeIdentityOutput(output, "/relocated/devkit#dev-all-runtime-bundle"); err == nil {
				t.Fatalf("expected %s identity to fail closed", name)
			}
		})
	}
}

func TestResolveOuroGovernanceRuntimeIdentityRequiresInstalledAuthorityWithoutEffects(t *testing.T) {
	t.Setenv("DEVKIT_GOVERNANCE_AUTHORITATIVE_ENV", "")
	sentinel := filepath.Join(t.TempDir(), "ambient-nix-ran")
	fakeNix := filepath.Join(t.TempDir(), "nix")
	if err := os.WriteFile(fakeNix, []byte("#!/bin/sh\n: > "+sentinel+"\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(fakeNix))
	_, err := resolveOuroGovernanceRuntimeIdentity(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "installed authoritative system launcher is required") {
		t.Fatalf("missing installed authority error = %v", err)
	}
	if pathExists(sentinel) {
		t.Fatal("runtime identity resolution executed ambient Nix")
	}
}

func TestSelectOuroGovernanceSystemRuntimeLauncherRequiresAuthoritativeEnv(t *testing.T) {
	original := ouroGovernanceSystemRuntimeLauncherPath
	t.Cleanup(func() { ouroGovernanceSystemRuntimeLauncherPath = original })
	launcher := filepath.Join(t.TempDir(), "dev-all-runtime-bundle")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ouroGovernanceSystemRuntimeLauncherPath = launcher

	t.Setenv("DEVKIT_GOVERNANCE_AUTHORITATIVE_ENV", "")
	if got, selected, err := selectOuroGovernanceSystemRuntimeLauncher(); err != nil || selected || got != "" {
		t.Fatalf("untrusted selection = path %q selected %t err %v", got, selected, err)
	}

	t.Setenv("DEVKIT_GOVERNANCE_AUTHORITATIVE_ENV", "1")
	if got, selected, err := selectOuroGovernanceSystemRuntimeLauncher(); err != nil || !selected || got != launcher {
		t.Fatalf("authoritative selection = path %q selected %t err %v", got, selected, err)
	}
}

func TestSelectOuroGovernanceSystemRuntimeLauncherIgnoresHostileCallerOverride(t *testing.T) {
	original := ouroGovernanceSystemRuntimeLauncherPath
	t.Cleanup(func() { ouroGovernanceSystemRuntimeLauncherPath = original })
	installedLauncher := filepath.Join(t.TempDir(), "dev-all-runtime-bundle")
	if err := os.WriteFile(installedLauncher, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ouroGovernanceSystemRuntimeLauncherPath = installedLauncher
	t.Setenv("DEVKIT_GOVERNANCE_AUTHORITATIVE_ENV", "1")
	t.Setenv(
		"DEVKIT_GOVERNANCE_RUNTIME_LAUNCHER",
		"/nix/store/00000000000000000000000000000000-hostile-runtime-bundle/bin/dev-all-runtime-bundle",
	)
	got, selected, err := selectOuroGovernanceSystemRuntimeLauncher()
	if err != nil || !selected || got != installedLauncher {
		t.Fatalf("hostile caller override redirected selection: path %q selected %t err %v", got, selected, err)
	}
}

func TestImmutableRuntimeBundleRejectsMutableIdentityOverride(t *testing.T) {
	bundlePath := strings.TrimSpace(os.Getenv("DEVKIT_TEST_RUNTIME_BUNDLE"))
	if bundlePath == "" {
		t.Skip("set DEVKIT_TEST_RUNTIME_BUNDLE to exercise the built immutable bundle")
	}
	launcher := filepath.Join(bundlePath, "bin", "dev-all-runtime-bundle")
	identityOutput, err := exec.Command(launcher, "identity-nul").CombinedOutput()
	if err != nil {
		t.Fatalf("bundle identity-nul: %v: %s", err, identityOutput)
	}
	identity, err := parseOuroGovernanceRuntimeIdentityOutput(identityOutput, bundlePath)
	if err != nil {
		t.Fatalf("parse bundle identity: %v", err)
	}
	physicalJar := filepath.Join(
		identity.ArtifactColumnRepositoryPath,
		identity.ArtifactColumnIvyPath,
		"jars",
		"artifact-column-plugin_sbt2_3.jar",
	)
	jarData, err := os.ReadFile(physicalJar)
	if err != nil {
		t.Fatalf("read physical Artifact Column Ivy jar: %v", err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(jarData)); got != identity.ArtifactColumnJarSHA256 {
		t.Fatalf("physical Artifact Column Ivy jar sha256 = %s, want %s", got, identity.ArtifactColumnJarSHA256)
	}
	metadata := readTestFile(t, identity.ArtifactColumnMetadataEnv)
	for key, want := range map[string]string{
		"ARTIFACT_COLUMN_PLUGIN_SOURCE_REV": identity.ArtifactColumnSourceRev,
		"ARTIFACT_COLUMN_PLUGIN_VERSION":    identity.ArtifactColumnVersion,
		"ARTIFACT_COLUMN_PLUGIN_IVY_PATH":   identity.ArtifactColumnIvyPath,
		"ARTIFACT_COLUMN_PLUGIN_JAR_SHA256": identity.ArtifactColumnJarSHA256,
	} {
		if got := metadataEnvValue(metadata, key); got != want {
			t.Fatalf("physical Artifact Column metadata %s = %q, want %q", key, got, want)
		}
	}
	tmp := t.TempDir()
	bashEnvSentinel := filepath.Join(tmp, "bash-env-sourced")
	bashEnv := filepath.Join(tmp, "hostile-bash-env.sh")
	if err := os.WriteFile(bashEnv, []byte(": > \"$DEVKIT_TEST_BASH_ENV_SENTINEL\"\nexport ARTIFACT_COLUMN_PLUGIN_SOURCE_REV=attacker\n"), 0o600); err != nil {
		t.Fatalf("write hostile BASH_ENV: %v", err)
	}

	cmd := exec.Command(launcher, "exec", "bash", "-c", strings.Join([]string{
		`test "$ARTIFACT_COLUMN_PLUGIN_SOURCE_REV" = "$EXPECTED_ARTIFACT_REV"`,
		`test "$ARTIFACT_COLUMN_PLUGIN_VERSION" = "$EXPECTED_ARTIFACT_VERSION"`,
		`test "$ARTIFACT_COLUMN_PLUGIN_JAR_SHA256" = "$EXPECTED_ARTIFACT_SHA"`,
		`test "$DEVKIT_RUNTIME_BUNDLE_PATH" = "$EXPECTED_BUNDLE"`,
		`test ! -e "$DEVKIT_TEST_BASH_ENV_SENTINEL"`,
	}, "; "))
	cmd.Env = append(os.Environ(),
		"BASH_ENV="+bashEnv,
		"ENV="+bashEnv,
		"DEVKIT_TEST_BASH_ENV_SENTINEL="+bashEnvSentinel,
		"EXPECTED_ARTIFACT_REV="+identity.ArtifactColumnSourceRev,
		"EXPECTED_ARTIFACT_VERSION="+identity.ArtifactColumnVersion,
		"EXPECTED_ARTIFACT_SHA="+identity.ArtifactColumnJarSHA256,
		"EXPECTED_BUNDLE="+bundlePath,
		"ARTIFACT_COLUMN_PLUGIN_SOURCE_REV=attacker",
		"ARTIFACT_COLUMN_PLUGIN_VERSION=attacker",
		"ARTIFACT_COLUMN_PLUGIN_JAR_SHA256=attacker",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("immutable bundle exec rejected valid authority or accepted hostile startup hooks: %v: %s", err, out)
	}
}

func TestBuildOuroGovernanceEnvBindsRoutingToImmutableEntrypoint(t *testing.T) {
	entrypointSHA256 := governanceentrypoint.SHA256ForRuntimeBundle("/nix/store/ffffffffffffffffffffffffffffffff-dev-all-runtime-bundle")
	got := buildOuroGovernanceEnv(
		"/home/bayesartre/dev",
		"/workspaces/dev/.devkit/ouro8-governance-repo-env.json",
		"abc123",
		entrypointSHA256,
	)
	for _, want := range []string{
		"Shared governance MCP/control-plane routing",
		"Runtime artifact identity is applied afterward by the immutable bundle launcher.",
		"export DEVKIT_GOVERNANCE_REPO_CONFIG_PATH='/workspaces/dev/.devkit/ouro8-governance-repo-env.json'",
		"export SUBAGENT_GOVERNANCE_REPO_CONFIG_PATH='/workspaces/dev/.devkit/ouro8-governance-repo-env.json'",
		"export DEVKIT_GOVERNANCE_REPO_CONFIG_SHA256='abc123'",
		"export DEVKIT_GOVERNANCE_EXPECTED_MCP_ENTRYPOINT_SHA256='" + entrypointSHA256 + "'",
		"export SUBAGENT_GOVERNANCE_KNOWN_WORKSPACE_IDS=",
		"export SUBAGENT_GOVERNANCE_WORKSPACE_ROOTS=",
		"export SUBAGENT_GOVERNANCE_HOST_DEV_ROOT_ALIAS='/home/bayesartre/dev'",
		"export SUBAGENT_GOVERNANCE_SCHEMA_ROOT=",
		"export SUBAGENT_GOVERNANCE_CONTROL_PLANE_URL=",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated governance routing env missing %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{
		"DEVKIT_RUNTIME_BUNDLE",
		"DEVKIT_RUNTIME_IDENTITY",
		"DEVKIT_GOVERNANCE_SOURCE_REV",
		"DEVKIT_SUBMIT_TO_CI_SOURCE_REV",
		"DEVKIT_ARTIFACT_COLUMN_SOURCE_REV",
		"DEVKIT_SBT_CONTROL_PLANE_SOURCE_REV",
		"SUBAGENT_GOVERNANCE_LATEST_JAR_PATH",
		"SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR",
		"DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH",
		"DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256",
		"DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_PATH",
		"DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_SHA256",
		"DEVKIT_GOVERNANCE_AUTHORITATIVE_ENV",
		"SUBAGENT_GOVERNANCE_EXPECTED_JAR_SHA256",
		"SUBAGENT_GOVERNANCE_PINNED_ARTIFACT",
		"SUBAGENT_GOVERNANCE_FLAKE_ARTIFACT",
		"SUBMIT_TO_CI_JAR",
		"SUBMIT_TO_CI_HASH_PATH",
		"SUBMIT_TO_CI_BUILD_POLICY",
		"SUBMIT_TO_CI_EXTERNAL_JAR",
		"SUBMIT_TO_CI_FLAKE_ARTIFACT",
		"SUBMIT_TO_CI_PINNED_ARTIFACT",
		"ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH",
		"ARTIFACT_COLUMN_PLUGIN_METADATA_ENV",
		"ARTIFACT_COLUMN_PLUGIN_VERSION",
		"ARTIFACT_COLUMN_PLUGIN_SOURCE_REV",
		"ARTIFACT_COLUMN_PLUGIN_SOURCE_SHORT_REV",
		"ARTIFACT_COLUMN_PLUGIN_IVY_PATH",
		"ARTIFACT_COLUMN_PLUGIN_JAR_SHA256",
		"ARTIFACT_COLUMN_PLUGIN_PINNED_ARTIFACT",
		"ARTIFACT_COLUMN_PLUGIN_FLAKE_ARTIFACT",
		"SBT_CONTROL_PLANE_RUNTIME_JAR",
		"SBT_CONTROL_PLANE_RUNTIME_JAR_SHA256",
		"SBT_CONTROL_PLANE_PINNED_ARTIFACT",
		"SBT_CONTROL_PLANE_FLAKE_ARTIFACT",
		"SBT2_CLIENT_MODE",
		"SBT2_JAVA_XMX",
		"OURO_LINT_INVARIANCE_SCRIPTED_SBT2_CLIENT_MODE",
		"JAVA_HOME",
		"identity-fingerprint",
		"print-dev-env",
		"DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("mutable routing env retained runtime authority %q:\n%s", forbidden, got)
		}
	}
	runtimeNamespaceEnv := buildOuroGovernanceEnv(
		"/workspaces/dev",
		"/workspaces/dev/.devkit/ouro8-governance-repo-env.json",
		"abc123",
		entrypointSHA256,
	)
	if strings.Contains(runtimeNamespaceEnv, "SUBAGENT_GOVERNANCE_HOST_DEV_ROOT_ALIAS") {
		t.Fatalf("canonical runtime namespace must not emit an overlapping host alias:\n%s", runtimeNamespaceEnv)
	}
}

func TestOuroGovernanceHostDevRootAliasRejectsUnsafeOrOverlappingRoots(t *testing.T) {
	tests := []struct {
		root string
		want string
	}{
		{root: "/home/bayesartre/dev", want: "/home/bayesartre/dev"},
		{root: "/workspaces/dev"},
		{root: "/workspaces/dev/agent-worktrees"},
		{root: "/workspaces"},
		{root: "/tmp/governance-alias"},
		{root: "relative/dev"},
	}
	for _, tt := range tests {
		t.Run(tt.root, func(t *testing.T) {
			if got := ouroGovernanceHostDevRootAlias(tt.root); got != tt.want {
				t.Fatalf("ouroGovernanceHostDevRootAlias(%q) = %q, want %q", tt.root, got, tt.want)
			}
		})
	}
}

func TestEnsureCodexGovernanceConfigRejectsMissingImmutableBundle(t *testing.T) {
	p := nativeplan.Plan{Agent: agent.Spec{ID: agent.ID{Repo: "ouroboros-ide"}}}
	if err := ensureCodexGovernanceConfig(p, ""); err == nil || !strings.Contains(err.Error(), "immutable runtime bundle path is required") {
		t.Fatalf("missing bundle error = %v", err)
	}
}

func TestEnsureCodexGovernanceConfigRejectsMissingHostHome(t *testing.T) {
	p := nativeplan.Plan{Agent: agent.Spec{ID: agent.ID{Repo: "ouroboros-ide"}}}
	err := ensureCodexGovernanceConfig(p, "/nix/store/ffffffffffffffffffffffffffffffff-dev-all-runtime-bundle")
	if err == nil || !strings.Contains(err.Error(), "agent host home is required") {
		t.Fatalf("missing host home error = %v", err)
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

func assertSourceGeneratedGovernanceConfig(t *testing.T, got string, preserved string) {
	t.Helper()
	if !strings.Contains(got, preserved) {
		t.Fatalf("config did not preserve existing content %q:\n%s", preserved, got)
	}
	for _, want := range []string{
		codexNixManagedConfigMarker,
		`model_provider = "openai"`,
		"[profiles.openai]",
		codexGovernanceManagedBegin,
		"# source = devkit native launch generator",
		"governance_mcp_entrypoint_sha256",
		"[mcp_servers.governance]",
		`command = "/run/current-system/sw/bin/bash"`,
		`startup_timeout_sec = 240`,
		`tool_timeout_sec = 10800`,
		`default_tools_approval_mode = "approve"`,
		`"get_run_status"`,
		`"cancel_run"`,
		`DEVKIT_GOVERNANCE_REPO_CONFIG_PATH = "/workspaces/dev/.devkit/ouro8-governance-repo-env.json"`,
		`SUBAGENT_GOVERNANCE_REPO_CONFIG_PATH = "/workspaces/dev/.devkit/ouro8-governance-repo-env.json"`,
		"DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256",
		"DEVKIT_GOVERNANCE_EXPECTED_MCP_ENTRYPOINT_SHA256",
		"/nix/store/ffffffffffffffffffffffffffffffff-dev-all-runtime-bundle/bin/dev-all-runtime-bundle",
		"[projects.",
		codexGovernanceManagedEnd,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("source-generated governance config missing %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{
		"env_vars = [",
		`"SUBAGENT_GOVERNANCE_CONTROL_PLANE_AUTOWARM"`,
		"SUBAGENT_GOVERNANCE_CONTROL_PLANE_BIND=0.0.0.0",
		`command = "bash"`,
		`args = ["-lc", "mkdir -p ${HOME:-/tmp}/.codex/log; set -x; exec bash scripts/devops/governance-mcp-stdio-forward"]`,
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("source-generated governance config contains stale mutable value %q:\n%s", forbidden, got)
		}
	}
}

func testAuthoritativeCodexConfig(topLevel string, tail ...string) string {
	parts := []string{
		codexNixManagedConfigMarker,
		`model = "gpt-5.5"`,
		`model_provider = "openai"`,
		`model_reasoning_effort = "xhigh"`,
		`approval_policy = "never"`,
		`sandbox_mode = "danger-full-access"`,
	}
	if strings.TrimSpace(topLevel) != "" {
		parts = append(parts, strings.TrimRight(topLevel, "\r\n"))
	}
	parts = append(parts,
		"",
		"[profiles.openai]",
		`model = "gpt-5.5"`,
		`model_provider = "openai"`,
		`model_reasoning_effort = "xhigh"`,
		`approval_policy = "never"`,
		`sandbox_mode = "danger-full-access"`,
	)
	for _, section := range tail {
		if strings.TrimSpace(section) == "" {
			continue
		}
		parts = append(parts, "", strings.TrimRight(section, "\r\n"))
	}
	return strings.Join(parts, "\n") + "\n"
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

func TestBuildBubblewrapWorkspaceEgressUsesNarrowBinds(t *testing.T) {
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

func TestPrepareConfiguresGitSSHForSeededNativeIdentity(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Repo:  "ouroboros-ide",
		Index: 2,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p.Agent.HostWorktree), 0o755); err != nil {
		t.Fatalf("mkdir worktree parent: %v", err)
	}
	runTestCommand(t, "", "git", "init", p.Agent.HostWorktree)

	hostUserHome := filepath.Join(tmp, "host-user")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519"), "private")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519.pub"), "public")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "known_hosts"), "github.com ssh-ed25519 key")
	t.Setenv("HOME", hostUserHome)
	writeTestFile(t, filepath.Join(devRoot, filepath.FromSlash(codexDevkitConfigSourceRelPath)), testAuthoritativeCodexConfig(""))

	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".ssh", "config"), strings.Join([]string{
		"Host example.invalid",
		"  User keep",
		"Host github.com",
		"  HostName ssh.github.com",
		"  Port 443",
		"  User git",
		"  IdentityFile /old/home/.ssh/id_ed25519",
		"",
	}, "\n"))

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	sshCommand := testPackageSSHCommand(t, filepath.Join(p.Agent.SandboxHome, ".ssh", "config"))
	cfg := readTestFile(t, filepath.Join(p.Agent.HostHome, ".ssh", "config"))
	for _, want := range []string{
		gitSSHManagedBegin,
		"Host github.com ssh.github.com",
		"  IdentityFile " + filepath.Join(p.Agent.SandboxHome, ".ssh", "id_ed25519"),
		"  IdentitiesOnly yes",
		"  BatchMode yes",
		"  StrictHostKeyChecking yes",
		"  UserKnownHostsFile " + filepath.Join(p.Agent.SandboxHome, ".ssh", "known_hosts"),
		gitSSHManagedEnd,
		"Host example.invalid",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("ssh config missing %q:\n%s", want, cfg)
		}
	}
	if got := runTestCommand(t, "", "git", "config", "--file", filepath.Join(p.Agent.HostHome, ".gitconfig"), "--get", "core.sshCommand"); got != sshCommand {
		t.Fatalf("global core.sshCommand = %q, want %q", got, sshCommand)
	}
	if got := runTestCommand(t, "", "git", "-C", p.Agent.HostWorktree, "config", "--get", "extensions.worktreeConfig"); got != "true" {
		t.Fatalf("extensions.worktreeConfig = %q, want true", got)
	}
	if got := runTestCommand(t, "", "git", "-C", p.Agent.HostWorktree, "config", "--worktree", "--get", "core.bare"); got != "false" {
		t.Fatalf("worktree core.bare = %q, want false", got)
	}
	if got := runTestCommand(t, "", "git", "-C", p.Agent.HostWorktree, "config", "--worktree", "--get", "core.sshCommand"); got != sshCommand {
		t.Fatalf("worktree core.sshCommand = %q, want %q", got, sshCommand)
	}

	if err := Prepare(p); err != nil {
		t.Fatalf("second Prepare: %v", err)
	}
	cfg = readTestFile(t, filepath.Join(p.Agent.HostHome, ".ssh", "config"))
	if count := strings.Count(cfg, gitSSHManagedBegin); count != 1 {
		t.Fatalf("managed block count = %d, want 1:\n%s", count, cfg)
	}
	if !strings.Contains(cfg, "Host github.com ssh.github.com") || strings.Contains(cfg, "/old/home") {
		t.Fatalf("legacy github host block was preserved:\n%s", cfg)
	}
	if count := strings.Count(cfg, "Host github.com ssh.github.com"); count != 1 {
		t.Fatalf("github host block count = %d, want 1:\n%s", count, cfg)
	}
	authority, err := resolvePackageSSHAuthority()
	if err != nil {
		t.Fatal(err)
	}
	wantKnownHosts, err := os.ReadFile(authority.KnownHostsFile())
	if err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".ssh", "known_hosts")); got != string(wantKnownHosts) {
		t.Fatalf("consumer known-hosts = %q, want exact package material %q", got, wantKnownHosts)
	}
}

func TestPrepareConfiguresWorkspaceEgressGitSSHThroughManagedProxy(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	allowlistPath := filepath.Join(devkitRoot, "kit", "proxy", "allowlist.txt")
	writeTestFile(t, allowlistPath, "github.com\nssh.github.com\n")
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths:            devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Repo:             "ouroboros-ide",
		Index:            2,
		IsolationProfile: nativeplan.IsolationProfileWorkspaceEgress,
		EgressAllowlist:  allowlistPath,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	initTestGitWorktree(t, p.Agent.HostWorktree)
	writeTestFile(t, p.Proxy.UnixSocket, "socket placeholder")
	hostUserHome := filepath.Join(tmp, "host-user")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519"), "private")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519.pub"), "public")
	t.Setenv("HOME", hostUserHome)
	writeTestFile(t, filepath.Join(devRoot, filepath.FromSlash(codexDevkitConfigSourceRelPath)), testAuthoritativeCodexConfig(""))

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	cfg := readTestFile(t, filepath.Join(p.Agent.HostHome, ".ssh", "config"))
	for _, want := range []string{
		"Host github.com ssh.github.com",
		"  HostName ssh.github.com",
		"  Port 443",
		"  ProxyCommand nc -X connect -x 127.0.0.1:18888 %h %p",
		"  IdentityFile " + filepath.Join(p.Agent.SandboxHome, ".ssh", "id_ed25519"),
		"  StrictHostKeyChecking yes",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("workspace-egress SSH config missing %q:\n%s", want, cfg)
		}
	}
}

func TestPrepareGitBootstrapRefusesUncomposedProductAuthority(t *testing.T) {
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
		ProductCount:         1,
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
	_, err := PrepareGitBootstrap(p)
	if err == nil || !strings.Contains(err.Error(), "manifest-selected absolute executable") {
		t.Fatalf("PrepareGitBootstrap error = %v, want missing composed helper refusal", err)
	}
}

func TestPrepareGitBootstrapDoesNotFallBackWhenUncomposedProductHelperIsMissing(t *testing.T) {
	tmp := t.TempDir()
	hostUserHome := filepath.Join(tmp, "host-user")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519"), "private")
	writeTestFile(t, filepath.Join(hostUserHome, ".ssh", "id_ed25519.pub"), "public")
	t.Setenv("HOME", hostUserHome)

	p := nativeplan.Plan{
		IsolationProfile:     nativeplan.IsolationProfileWorkspaceEgress,
		ProductCount:         1,
		RuntimeAuthorityRoot: filepath.Join(tmp, "missing-runtime"),
		Agent: agent.Spec{
			ID:       agent.ID{Project: "dev-all", Index: 1, Repo: "ouroboros-ide"},
			HostHome: filepath.Join(tmp, "agent-home"),
		},
		Proxy: nativeplan.ProxyConfig{UnixSocket: filepath.Join(tmp, "proxy.sock")},
	}
	_, err := PrepareGitBootstrap(p)
	if err == nil || !strings.Contains(err.Error(), "manifest-selected absolute executable") {
		t.Fatalf("PrepareGitBootstrap error = %v, want missing composed helper refusal", err)
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

func TestPrepareImportsMissingLegacyCodexStateWithoutClobber(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Repo:  "ouroboros-ide",
		Index: 2,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	initTestGitWorktree(t, p.Agent.HostWorktree)

	legacyCodex := filepath.Join(p.Agent.StateRoot, "home", ".codex")
	legacySession := filepath.Join(legacyCodex, "sessions", "2026", "05", "15", "rollout.jsonl")
	writeTestFile(t, legacySession, "legacy session")
	legacySessionMtime := time.Date(2026, 5, 15, 12, 34, 56, 0, time.UTC)
	if err := os.Chtimes(legacySession, legacySessionMtime, legacySessionMtime); err != nil {
		t.Fatalf("set legacy session mtime: %v", err)
	}
	writeTestFile(t, filepath.Join(legacyCodex, "rollouts", "in-flight.jsonl"), "legacy rollout")
	writeTestFile(t, filepath.Join(legacyCodex, "shell_snapshots", "snapshot.sh"), "legacy shell")
	writeTestFile(t, filepath.Join(legacyCodex, "log", "codex-tui.log"), "legacy log")
	writeTestFile(t, filepath.Join(legacyCodex, "state_5.sqlite"), "legacy state")
	writeTestFile(t, filepath.Join(legacyCodex, "logs_2.sqlite"), "legacy logs")
	writeTestFile(t, filepath.Join(legacyCodex, "auth.json"), "legacy auth")
	writeTestFile(t, filepath.Join(legacyCodex, "config.toml"), "legacy config")

	dstCodex := filepath.Join(p.Agent.HostHome, ".codex")
	writeTestFile(t, filepath.Join(dstCodex, "auth.json"), "current auth")
	writeTestFile(t, filepath.Join(dstCodex, "config.toml"), `model = "stale-home"`+"\n")
	writeTestFile(t, filepath.Join(devRoot, filepath.FromSlash(codexDevkitConfigSourceRelPath)), testAuthoritativeCodexConfig(`personality = "current"`))
	beforeSessions := countFiles(t, filepath.Join(dstCodex, "sessions"))

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	importedSession := filepath.Join(dstCodex, "sessions", "2026", "05", "15", "rollout.jsonl")
	if got := readTestFile(t, importedSession); got != "legacy session" {
		t.Fatalf("session content = %q", got)
	}
	if st, err := os.Stat(importedSession); err != nil {
		t.Fatalf("stat imported session: %v", err)
	} else if !st.ModTime().Equal(legacySessionMtime) {
		t.Fatalf("imported session mtime = %s, want %s", st.ModTime(), legacySessionMtime)
	}
	for rel, want := range map[string]string{
		filepath.Join("rollouts", "in-flight.jsonl"):    "legacy rollout",
		filepath.Join("shell_snapshots", "snapshot.sh"): "legacy shell",
		filepath.Join("log", "codex-tui.log"):           "legacy log",
		"state_5.sqlite":                                "legacy state",
		"logs_2.sqlite":                                 "legacy logs",
		"auth.json":                                     "current auth",
	} {
		if got := readTestFile(t, filepath.Join(dstCodex, rel)); got != want {
			t.Fatalf("%s content = %q, want %q", rel, got, want)
		}
	}
	assertSourceGeneratedGovernanceConfig(t, readTestFile(t, filepath.Join(dstCodex, "config.toml")), `personality = "current"`)
	if strings.Contains(readTestFile(t, filepath.Join(dstCodex, "config.toml")), `model = "stale-home"`) {
		t.Fatalf("stale mutable config survived Nix-source restore")
	}
	afterSessions := countFiles(t, filepath.Join(dstCodex, "sessions"))
	if afterSessions < beforeSessions+1 {
		t.Fatalf("session count did not increase as expected: before=%d after=%d", beforeSessions, afterSessions)
	}

	if err := Prepare(p); err != nil {
		t.Fatalf("second Prepare: %v", err)
	}
	if got := countFiles(t, filepath.Join(dstCodex, "sessions")); got != afterSessions {
		t.Fatalf("second Prepare changed session count: before=%d after=%d", afterSessions, got)
	}
	if got := readTestFile(t, filepath.Join(dstCodex, "auth.json")); got != "current auth" {
		t.Fatalf("auth was clobbered: %q", got)
	}
	assertSourceGeneratedGovernanceConfig(t, readTestFile(t, filepath.Join(dstCodex, "config.toml")), `personality = "current"`)
}

func TestPrepareRepairsRetiredCodexShellHookWithoutTouchingSessions(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Repo:  "ouroboros-ide",
		Index: 2,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	initTestGitWorktree(t, p.Agent.HostWorktree)
	writeTestFile(t, filepath.Join(devRoot, filepath.FromSlash(codexDevkitConfigSourceRelPath)), testAuthoritativeCodexConfig(""))
	sessionPath := filepath.Join(p.Agent.HostHome, ".codex", "sessions", "past.jsonl")
	writeTestFile(t, sessionPath, "past")
	zshrc := filepath.Join(p.Agent.HostHome, ".zshrc")
	retiredPath := "/usr/local/bin/" + "codex"
	writeTestFile(t, zshrc, `codex() {
  HOME="$HOME" CODEX_HOME="$HOME/.codex" `+retiredPath+` "$@"
}
`)

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	got := readTestFile(t, zshrc)
	if strings.Contains(got, retiredPath) {
		t.Fatalf("retired codex path was not repaired:\n%s", got)
	}
	if !strings.Contains(got, "command codex") {
		t.Fatalf("repaired wrapper missing command codex:\n%s", got)
	}
	for _, forbidden := range []string{`"${extra[@]}"`, "mcp_servers.", "    -c ", "    -m gpt-5.5", "    -a never", "    -s danger-full-access"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("repaired wrapper must not override Codex config with CLI fragment %q:\n%s", forbidden, got)
		}
	}
	if !strings.Contains(got, "devkit_codex_tui_log_guard()") {
		t.Fatalf("repaired wrapper missing TUI log guard:\n%s", got)
	}
	if !strings.Contains(got, "DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256") {
		t.Fatalf("repaired wrapper missing governance entrypoint fingerprint:\n%s", got)
	}
	if !strings.Contains(got, "devkit_codex_require_config()") || !strings.Contains(got, "Codex config must use model_provider = \\\"openai\\\"") {
		t.Fatalf("repaired wrapper missing loud openai config guard:\n%s", got)
	}
	if strings.Contains(got, "mcp_servers.governance.args=") {
		t.Fatalf("repaired wrapper must not pass array-valued governance args through CLI -c:\n%s", got)
	}
	gotConfig := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml"))
	assertSourceGeneratedGovernanceConfig(t, gotConfig, "")
	if !strings.Contains(gotConfig, "args = [\"-lc\",") || !strings.Contains(gotConfig, "using governance env: ${governance_env}") {
		t.Fatalf("generated config missing array-valued governance entrypoint:\n%s", gotConfig)
	}
	if session := readTestFile(t, sessionPath); session != "past" {
		t.Fatalf("session was changed: %q", session)
	}
}

func TestPrepareAddsTUILogGuardToGeneratedCodexShellHook(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Repo:  "ouroboros-ide",
		Index: 2,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	initTestGitWorktree(t, p.Agent.HostWorktree)
	writeTestFile(t, filepath.Join(devRoot, filepath.FromSlash(codexDevkitConfigSourceRelPath)), testAuthoritativeCodexConfig(""))
	zshrc := filepath.Join(p.Agent.HostHome, ".zshrc")
	writeTestFile(t, zshrc, `unalias codex 2>/dev/null || true
codex() {
  local -a extra
  extra=(
    -a never
  )
  HOME="$HOME" CODEX_HOME="$HOME/.codex" CODEX_ROLLOUT_DIR="$HOME/.codex/rollouts" command codex "${extra[@]}" "$@"
}
`)

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	got := readTestFile(t, zshrc)
	if !strings.Contains(got, "devkit_codex_tui_log_guard()") {
		t.Fatalf("generated wrapper missing TUI log guard function:\n%s", got)
	}
	for _, forbidden := range []string{`"${extra[@]}"`, "mcp_servers.", "    -c ", "    -m gpt-5.5", "    -a never", "    -s danger-full-access"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("generated wrapper must not override Codex config with CLI fragment %q:\n%s", forbidden, got)
		}
	}
	if !strings.Contains(got, "DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256") {
		t.Fatalf("generated wrapper missing governance entrypoint fingerprint:\n%s", got)
	}
	if !strings.Contains(got, "  devkit_codex_tui_log_guard\n  devkit_codex_require_config || return\n  HOME=\"$HOME\" CODEX_HOME=") {
		t.Fatalf("generated wrapper does not call TUI log guard before codex:\n%s", got)
	}
	if !strings.Contains(got, "Codex config missing [profiles.openai]") {
		t.Fatalf("generated wrapper missing loud openai profile failure:\n%s", got)
	}
	if strings.Contains(got, "mcp_servers.governance.args=") {
		t.Fatalf("generated wrapper must not pass array-valued governance args through CLI -c:\n%s", got)
	}
	gotConfig := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml"))
	assertSourceGeneratedGovernanceConfig(t, gotConfig, "")
	if !strings.Contains(gotConfig, "args = [\"-lc\",") ||
		!strings.Contains(gotConfig, "${PWD%%/agent-worktrees/*}/.devkit/ouro8-governance-env.sh") ||
		!strings.Contains(gotConfig, "required governance env missing") {
		t.Fatalf("generated config missing source-derived governance entrypoint:\n%s", gotConfig)
	}
}

func TestPrepareInstallsDevAllGovernedSearchPolicyRules(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	policy := "prefix_rule(\n    pattern = [\"rg\"],\n    decision = \"forbidden\"\n)\n"
	writeTestFile(t, filepath.Join(devkitRoot, "overlays", "dev-all", "codex-governed-search-policy.rules"), policy)

	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Repo:  "ouroboros-ide",
		Index: 2,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	initTestGitWorktree(t, p.Agent.HostWorktree)
	existingConfig := testAuthoritativeCodexConfig(`personality = "existing"`, strings.Join([]string{
		`[projects."/home/bayesartre/dev/ouroboros-ide"]`,
		`trust_level = "trusted"`,
		``,
		`[projects."/workspaces/dev/agent-worktrees/agent2/ouroboros-ide"]`,
		`trust_level = "trusted"`,
		``,
		codexGovernanceManagedBegin,
		`[mcp_servers.governance]`,
		`command = "bash"`,
		codexGovernanceManagedEnd,
		``,
	}, "\n"))
	writeTestFile(t, filepath.Join(devRoot, filepath.FromSlash(codexDevkitConfigSourceRelPath)), existingConfig)
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml"), `model = "stale-home"`+"\n")
	writeTestFile(t, filepath.Join(p.Agent.HostWorktree, ".codex", "config.toml"), strings.Join([]string{
		`approval_policy = "never"`,
		`[mcp_servers.governance]`,
		`command = "bash"`,
		`args = ["-lc", "mkdir -p ${HOME:-/tmp}/.codex/log; set -x; exec bash scripts/devops/governance-mcp-stdio-forward"]`,
		`startup_timeout_sec = 60`,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "sessions", "past.jsonl"), "past session")
	for _, altForwarder := range []string{
		filepath.Join(devRoot, "agent-worktrees", "agent2", "ouroboros-ide-statement-classifier-submit", "scripts", "devops", "governance-mcp-stdio-forward"),
		filepath.Join(devRoot, "agent-worktrees", "terraform-ouro-1-redaction-safe", "ouroboros-ide", "scripts", "devops", "governance-mcp-stdio-forward"),
	} {
		writeTestFile(t, altForwarder, "#!/usr/bin/env bash\n")
	}

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	target := filepath.Join(p.Agent.HostHome, ".codex", "rules", "governed-search-policy.rules")
	if got := readTestFile(t, target); got != policy {
		t.Fatalf("policy rules = %q, want %q", got, policy)
	}
	gotConfig := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml"))
	assertSourceGeneratedGovernanceConfig(t, gotConfig, `personality = "existing"`)
	if strings.Contains(gotConfig, `model = "stale-home"`) {
		t.Fatalf("stale mutable config survived Nix-source restore:\n%s", gotConfig)
	}
	if strings.Count(gotConfig, `[projects."/home/bayesartre/dev/ouroboros-ide"]`) > 1 {
		t.Fatalf("config retained duplicate stale project trust table:\n%s", gotConfig)
	}
	for _, forbidden := range []string{
		"env_vars = [",
		"mcp_servers.governance.env.",
		`"SUBAGENT_GOVERNANCE_CONTROL_PLANE_AUTOWARM"`,
		"SUBAGENT_GOVERNANCE_CONTROL_PLANE_BIND=0.0.0.0",
	} {
		if strings.Contains(gotConfig, forbidden) {
			t.Fatalf("config contains forbidden %q:\n%s", forbidden, gotConfig)
		}
	}
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "sessions", "past.jsonl")); got != "past session" {
		t.Fatalf("session was clobbered: %q", got)
	}
	wantWorktreeConfig := strings.Join([]string{
		`approval_policy = "never"`,
		"",
	}, "\n")
	if gotWorktreeConfig := readTestFile(t, filepath.Join(p.Agent.HostWorktree, ".codex", "config.toml")); gotWorktreeConfig != wantWorktreeConfig {
		t.Fatalf("worktree config should remove mutable governance table, got:\n%s\nwant:\n%s", gotWorktreeConfig, wantWorktreeConfig)
	}
	envPath := filepath.Join(devRoot, ".devkit", "ouro8-governance-env.sh")
	gotEnv := readTestFile(t, envPath)
	wantEntrypointSHA256 := governanceentrypoint.SHA256ForRuntimeBundle("/nix/store/ffffffffffffffffffffffffffffffff-dev-all-runtime-bundle")
	for _, want := range []string{
		"Shared governance MCP/control-plane routing",
		"Runtime artifact identity is applied afterward by the immutable bundle launcher.",
		"export DEVKIT_GOVERNANCE_REPO_CONFIG_PATH='/workspaces/dev/.devkit/ouro8-governance-repo-env.json'",
		"export SUBAGENT_GOVERNANCE_REPO_CONFIG_PATH='/workspaces/dev/.devkit/ouro8-governance-repo-env.json'",
		"export DEVKIT_GOVERNANCE_REPO_CONFIG_SHA256=",
		"export DEVKIT_GOVERNANCE_EXPECTED_MCP_ENTRYPOINT_SHA256='" + wantEntrypointSHA256 + "'",
		"export SUBAGENT_GOVERNANCE_KNOWN_WORKSPACE_IDS=",
		"dev-workspace=/workspaces/dev",
		"ouroboros-ide=/workspaces/dev/ouroboros-ide",
		"ouroboros-terraform=/workspaces/dev/ouroboros-terraform",
		"fleet-runtime-workspace=" + filepath.Join(devRoot, ".devkit", "governance-control-plane", "runtime-workspace"),
		"agent1=/workspaces/dev/agent-worktrees/agent1/ouroboros-ide",
		"agent9-ouroboros-terraform=/workspaces/dev/agent-worktrees/agent9/ouroboros-terraform",
		"email-policy-mcp-app=/workspaces/dev/agent-worktrees/email-policy-mcp-app/ouroboros-ide",
		"shadow1-workbook-patch-mcp-localcontext=/workspaces/dev/agent-worktrees/shadow1-workbook-patch-mcp-localcontext/ouroboros-ide",
		"export SUBAGENT_GOVERNANCE_SCHEMA_ROOT=/workspaces/dev/ouroboros-ide/tools/subagent-governance/schemas",
		"export SUBAGENT_GOVERNANCE_WARM_HOOK_CMD='scripts/devops/governance-control-plane warm'",
		"export SUBAGENT_GOVERNANCE_CONTROL_PLANE_STATE_DIR=/workspaces/dev/.devkit/governance-control-plane",
		"export SUBAGENT_GOVERNANCE_EXECUTION_GRAPH_DECISION_LOG_PATH=/workspaces/dev/.devkit/governance-control-plane/execution-graph-decisions.jsonl",
	} {
		if !strings.Contains(gotEnv, want) {
			t.Fatalf("governance routing env missing %q:\n%s", want, gotEnv)
		}
	}
	for _, forbidden := range []string{
		"DEVKIT_RUNTIME_IDENTITY",
		"DEVKIT_RUNTIME_BUNDLE",
		"DEVKIT_GOVERNANCE_SOURCE_REV",
		"DEVKIT_SUBMIT_TO_CI_SOURCE_REV",
		"DEVKIT_ARTIFACT_COLUMN_SOURCE_REV",
		"DEVKIT_SBT_CONTROL_PLANE_SOURCE_REV",
		"DEVKIT_GOVERNANCE_EXPECTED_JAR_",
		"DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_",
		"DEVKIT_GOVERNANCE_AUTHORITATIVE_ENV",
		"DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256",
		"SUBAGENT_GOVERNANCE_LATEST_JAR_PATH",
		"SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR",
		"SUBAGENT_GOVERNANCE_EXPECTED_JAR_SHA256",
		"SUBAGENT_GOVERNANCE_PINNED_ARTIFACT",
		"SUBAGENT_GOVERNANCE_FLAKE_ARTIFACT",
		"SUBMIT_TO_CI_",
		"ARTIFACT_COLUMN_PLUGIN_",
		"SBT_CONTROL_PLANE_",
		"SBT2_CLIENT_MODE",
		"SBT2_JAVA_XMX",
		"OURO_LINT_INVARIANCE_SCRIPTED_SBT2_CLIENT_MODE",
		"JAVA_HOME=",
		"identity-fingerprint",
		"print-dev-env",
		"/workspaces/dev/devkit#dev-all",
	} {
		if strings.Contains(gotEnv, forbidden) {
			t.Fatalf("mutable governance routing env retained runtime authority %q:\n%s", forbidden, gotEnv)
		}
	}
	for _, forbidden := range []string{
		"agent2-ouroboros-ide-statement-classifier-submit",
		"terraform-ouro-1-redaction-safe",
	} {
		if strings.Contains(gotEnv, forbidden) {
			t.Fatalf("governance env leaked ad-hoc worktree %q:\n%s", forbidden, gotEnv)
		}
	}
	if strings.Contains(gotEnv, "SUBAGENT_GOVERNANCE_WORKSPACE_ID=") {
		t.Fatalf("shared governance env must not pin a per-agent workspace id:\n%s", gotEnv)
	}
	if strings.Contains(gotEnv, "\n  local ") {
		t.Fatalf("shared governance env must avoid local declarations for remote eval compatibility:\n%s", gotEnv)
	}
	for _, forbidden := range []string{
		"export SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR=" + filepath.Join(devRoot, "ouroboros-ide", "tools", "subagent-governance", "subagent-governance.jar"),
		"export SUBAGENT_GOVERNANCE_LATEST_JAR_PATH=" + filepath.Join(devRoot, "ouroboros-ide", "tools", "subagent-governance", "subagent-governance.jar"),
	} {
		if strings.Contains(gotEnv, forbidden) {
			t.Fatalf("shared governance env retained mutable jar path %q:\n%s", forbidden, gotEnv)
		}
	}
	gotRepoConfig := readTestFile(t, filepath.Join(devRoot, ".devkit", "ouro8-governance-repo-env.json"))
	for _, want := range []string{
		`"workspaceRoot": "/workspaces/dev/ouroboros-ide"`,
		`"skillCatalogPath": "/workspaces/dev/ouroboros-ide/tools/subagent-governance/catalog/skills.json"`,
		`"policyCatalogPath": "/workspaces/dev/ouroboros-ide/tools/subagent-governance/catalog/policies.json"`,
		`"promptBundleRoot": "/workspaces/dev/ouroboros-ide/tools/subagent-governance/skills"`,
		`"knownWorkspaceIds": [`,
		`"dev-workspace"`,
		`"ouroboros-ide"`,
		`"ouroboros-terraform"`,
		`"fleet-runtime-workspace"`,
		`"agent4"`,
		`"agent4-ouroboros-terraform"`,
		`"email-policy-mcp-app"`,
		`"dev-workspace": "/workspaces/dev"`,
		`"ouroboros-terraform": "/workspaces/dev/ouroboros-terraform"`,
		`"fleet-runtime-workspace": "` + filepath.ToSlash(filepath.Join(devRoot, ".devkit", "governance-control-plane", "runtime-workspace")) + `"`,
		`"agent1": "/workspaces/dev/agent-worktrees/agent1/ouroboros-ide"`,
		`"agent4": "/workspaces/dev/agent-worktrees/agent4/ouroboros-ide"`,
		`"agent4-ouroboros-terraform": "/workspaces/dev/agent-worktrees/agent4/ouroboros-terraform"`,
		`"email-policy-mcp-app": "/workspaces/dev/agent-worktrees/email-policy-mcp-app/ouroboros-ide"`,
		`"shadow1-workbook-patch-mcp-localcontext": "/workspaces/dev/agent-worktrees/shadow1-workbook-patch-mcp-localcontext/ouroboros-ide"`,
		`"schemaRoot": "/workspaces/dev/ouroboros-ide/tools/subagent-governance/schemas"`,
		`"controlPlaneUrl": "http://127.0.0.1:7778"`,
		`"controlPlaneStateDir": "/workspaces/dev/.devkit/governance-control-plane"`,
		`"executionGraphDecisionLogPath": "/workspaces/dev/.devkit/governance-control-plane/execution-graph-decisions.jsonl"`,
	} {
		if !strings.Contains(gotRepoConfig, want) {
			t.Fatalf("governance repo config missing %q:\n%s", want, gotRepoConfig)
		}
	}
	for _, forbidden := range []string{
		`"agent2-ouroboros-ide-statement-classifier-submit"`,
		`"terraform-ouro-1-redaction-safe"`,
	} {
		if strings.Contains(gotRepoConfig, forbidden) {
			t.Fatalf("governance repo config leaked ad-hoc worktree %q:\n%s", forbidden, gotRepoConfig)
		}
	}
	if strings.Contains(gotRepoConfig, `"latestJarPath"`) || strings.Contains(gotRepoConfig, "tools/subagent-governance/subagent-governance.jar") {
		t.Fatalf("governance repo config must not carry mutable jar authority:\n%s", gotRepoConfig)
	}
	if wantHash := fmt.Sprintf("%x", sha256.Sum256([]byte(gotRepoConfig))); !strings.Contains(gotEnv, "export DEVKIT_GOVERNANCE_REPO_CONFIG_SHA256='"+wantHash+"'") {
		t.Fatalf("governance env missing repo config hash %s:\n%s", wantHash, gotEnv)
	}
	if st, err := os.Stat(envPath); err != nil {
		t.Fatalf("stat governance env: %v", err)
	} else if got := st.Mode().Perm(); got != 0o600 {
		t.Fatalf("governance env mode = %o, want 600", got)
	}
	staleEnv := strings.Replace(gotEnv, wantEntrypointSHA256, strings.Repeat("0", 64), 1)
	writeTestFile(t, envPath, staleEnv)
	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare should rewrite stale governance entrypoint identity: %v", err)
	}
	if repairedEnv := readTestFile(t, envPath); repairedEnv != gotEnv {
		t.Fatalf("stale governance entrypoint identity was not rewritten:\n%s", repairedEnv)
	}
	repoConfigPath := filepath.Join(devRoot, ".devkit", "ouro8-governance-repo-env.json")
	if err := os.Chmod(envPath, 0o644); err != nil {
		t.Fatalf("loosen governance env mode: %v", err)
	}
	if err := os.Chmod(repoConfigPath, 0o644); err != nil {
		t.Fatalf("loosen governance repo config mode: %v", err)
	}
	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare should repair generated governance file modes: %v", err)
	}
	if st, err := os.Stat(envPath); err != nil {
		t.Fatalf("stat repaired governance env: %v", err)
	} else if got := st.Mode().Perm(); got != 0o600 {
		t.Fatalf("repaired governance env mode = %o, want 600", got)
	}
	if st, err := os.Stat(repoConfigPath); err != nil {
		t.Fatalf("stat repaired governance repo config: %v", err)
	} else if got := st.Mode().Perm(); got != 0o600 {
		t.Fatalf("repaired governance repo config mode = %o, want 600", got)
	}
}

func TestOuroGovernanceGeneratedRoutingUsesOneCanonicalRuntimeWorkspaceBinding(t *testing.T) {
	tmp := t.TempDir()
	hostDevRoot := filepath.Join(tmp, "dev")
	externalState := filepath.Join(tmp, "external-state")
	canonicalRuntimeRoot := filepath.Join(externalState, "governance-control-plane", "runtime-workspace")
	if err := os.MkdirAll(canonicalRuntimeRoot, 0o755); err != nil {
		t.Fatalf("mkdir canonical runtime root: %v", err)
	}
	if err := os.MkdirAll(hostDevRoot, 0o755); err != nil {
		t.Fatalf("mkdir host dev root: %v", err)
	}
	if err := os.Symlink(externalState, filepath.Join(hostDevRoot, ".devkit")); err != nil {
		t.Fatalf("symlink host state root: %v", err)
	}

	catalog := buildOuroGovernanceCatalogForRoot(hostDevRoot)
	if got := strings.Count(strings.Join(catalog.ids, ","), "fleet-runtime-workspace"); got != 1 {
		t.Fatalf("runtime workspace id count = %d, want 1: %v", got, catalog.ids)
	}
	if got := catalog.rootMap["fleet-runtime-workspace"]; got != canonicalRuntimeRoot {
		t.Fatalf("runtime workspace root = %q, want canonical %q", got, canonicalRuntimeRoot)
	}
	if _, exists := catalog.rootMap["fleet-runtime-workspace-host"]; exists {
		t.Fatalf("generated catalog retained forbidden runtime workspace alias: %v", catalog.rootMap)
	}

	repoConfig, err := buildOuroGovernanceRepoConfig(hostDevRoot)
	if err != nil {
		t.Fatalf("build governance repo config: %v", err)
	}
	repoConfigSHA := fmt.Sprintf("%x", sha256.Sum256(repoConfig))
	env := buildOuroGovernanceEnv(hostDevRoot, "/workspaces/dev/.devkit/ouro8-governance-repo-env.json", repoConfigSHA, strings.Repeat("a", 64))
	expectedBinding := "fleet-runtime-workspace=" + canonicalRuntimeRoot
	if strings.Count(env, expectedBinding) != 1 {
		t.Fatalf("routing env must contain exactly one canonical runtime binding %q:\n%s", expectedBinding, env)
	}
	if strings.Count(string(repoConfig), `"fleet-runtime-workspace"`) != 2 {
		t.Fatalf("repo config must contain the runtime id once in ids and once in roots:\n%s", repoConfig)
	}
	if !strings.Contains(string(repoConfig), `"fleet-runtime-workspace": "`+filepath.ToSlash(canonicalRuntimeRoot)+`"`) {
		t.Fatalf("repo config missing canonical runtime binding:\n%s", repoConfig)
	}
	for _, generated := range []string{env, string(repoConfig)} {
		if strings.Contains(generated, "fleet-runtime-workspace-host") {
			t.Fatalf("generated routing retained forbidden runtime workspace alias:\n%s", generated)
		}
	}
}

func TestPrepareMigratesOuroGovernanceStateOutsideProductCheckout(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	writeTestFile(t, filepath.Join(devkitRoot, "overlays", "dev-all", "codex-governed-search-policy.rules"), "prefix_rule()\n")
	writeTestFile(t, filepath.Join(devRoot, filepath.FromSlash(codexDevkitConfigSourceRelPath)), testAuthoritativeCodexConfig(""))

	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Repo:  "ouroboros-ide",
		Index: 2,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	initTestGitWorktree(t, p.Agent.HostWorktree)

	legacyRoot := filepath.Join(devRoot, "ouroboros-ide", "logs", "subagent-governance")
	writeTestFile(t, filepath.Join(legacyRoot, "control-plane", "historical-continuation-operator-token"), "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	writeTestFile(t, filepath.Join(legacyRoot, "control-plane", "workspace-topology", "child-workspaces.jsonl"), "topology\n")
	writeTestFile(t, filepath.Join(legacyRoot, "control-plane", "operator-attention", "state.json"), "{}\n")
	writeTestFile(t, filepath.Join(legacyRoot, "control-plane", "workspace-submit-artifacts", "run", "subagent-report-cards", "report.yaml"), "report: pass\n")
	writeTestFile(t, filepath.Join(legacyRoot, "execution-graph-decisions.jsonl"), "decision\n")
	writeTestFile(t, filepath.Join(legacyRoot, "history", "20260714-pre-typed-binding-epoch", "README.md"), "history\n")
	writeTestFile(t, filepath.Join(legacyRoot, "probes", "probe-1", "probe-descriptor.json"), "{}\n")

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	externalRoot := filepath.Join(devRoot, ".devkit", "governance-control-plane")
	for rel, want := range map[string]string{
		"historical-continuation-operator-token":                                                   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		filepath.Join("workspace-topology", "child-workspaces.jsonl"):                              "topology\n",
		filepath.Join("operator-attention", "state.json"):                                          "{}\n",
		filepath.Join("workspace-submit-artifacts", "run", "subagent-report-cards", "report.yaml"): "report: pass\n",
		"execution-graph-decisions.jsonl":                                                          "decision\n",
		filepath.Join("history", "20260714-pre-typed-binding-epoch", "README.md"):                  "history\n",
		filepath.Join("probes", "probe-1", "probe-descriptor.json"):                                "{}\n",
	} {
		if got := readTestFile(t, filepath.Join(externalRoot, rel)); got != want {
			t.Fatalf("migrated governance state %s = %q, want %q", rel, got, want)
		}
	}
	for _, legacyPath := range []string{
		filepath.Join(legacyRoot, "control-plane", "historical-continuation-operator-token"),
		filepath.Join(legacyRoot, "control-plane", "workspace-topology", "child-workspaces.jsonl"),
		filepath.Join(legacyRoot, "execution-graph-decisions.jsonl"),
		filepath.Join(legacyRoot, "history", "20260714-pre-typed-binding-epoch", "README.md"),
	} {
		if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
			t.Fatalf("legacy governance state path still exists after source-owned move: %s err=%v", legacyPath, err)
		}
	}
	gotEnv := readTestFile(t, filepath.Join(devRoot, ".devkit", "ouro8-governance-env.sh"))
	gotRepoConfig := readTestFile(t, filepath.Join(devRoot, ".devkit", "ouro8-governance-repo-env.json"))
	for _, generated := range []string{gotEnv, gotRepoConfig} {
		if strings.Contains(generated, "/workspaces/dev/ouroboros-ide/logs/subagent-governance/control-plane") {
			t.Fatalf("generated governance config retained Product-checkout state dir:\n%s", generated)
		}
		if !strings.Contains(generated, "/workspaces/dev/.devkit/governance-control-plane") {
			t.Fatalf("generated governance config missing external state dir:\n%s", generated)
		}
	}
}

func TestOuroGovernanceExternalStateMigrationFailsClosedOnConflict(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	legacyRoot := filepath.Join(devRoot, "ouroboros-ide", "logs", "subagent-governance", "control-plane")
	externalRoot := filepath.Join(devRoot, ".devkit", "governance-control-plane")
	writeTestFile(t, filepath.Join(legacyRoot, "server.pid"), "old")
	writeTestFile(t, filepath.Join(externalRoot, "server.pid"), "new")

	err := migrateOuroGovernanceExternalState(devRoot)
	if err == nil || !strings.Contains(err.Error(), "migration conflict") {
		t.Fatalf("migrate conflict error = %v, want migration conflict", err)
	}
	if got := readTestFile(t, filepath.Join(legacyRoot, "server.pid")); got != "old" {
		t.Fatalf("legacy conflicting state was mutated: %q", got)
	}
	if got := readTestFile(t, filepath.Join(externalRoot, "server.pid")); got != "new" {
		t.Fatalf("external conflicting state was mutated: %q", got)
	}
}

func TestOuroGovernanceMigrationRejectsEstablishedExternalTokenWithLegacyResidue(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	legacyRoot := filepath.Join(devRoot, "ouroboros-ide", "logs", "subagent-governance", "control-plane")
	externalRoot := filepath.Join(devRoot, ".devkit", "governance-control-plane")
	legacyToken := strings.Repeat("a", 64)
	establishedToken := strings.Repeat("b", 64)
	writeTestFile(t, filepath.Join(legacyRoot, "historical-continuation-operator-token"), legacyToken)
	writeTestFile(t, filepath.Join(externalRoot, "historical-continuation-operator-token"), establishedToken)

	err := migrateOuroGovernanceExternalState(devRoot)
	if err == nil || !strings.Contains(err.Error(), "migration conflict") {
		t.Fatalf("migrate token conflict error = %v, want migration conflict", err)
	}
	if got := readTestFile(t, filepath.Join(legacyRoot, "historical-continuation-operator-token")); got != legacyToken {
		t.Fatalf("legacy token was mutated: %q", got)
	}
	if got := readTestFile(t, filepath.Join(externalRoot, "historical-continuation-operator-token")); got != establishedToken {
		t.Fatalf("external token was mutated: %q", got)
	}
}

func TestPrepareOuroTerraformCleansHomeGovernanceConfig(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	repoRoot := filepath.Join(devRoot, "ouroboros-terraform")
	for _, dir := range []string{devkitRoot, repoRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	p, err := nativeplan.Build(nativeplan.BuildOptions{
		Paths:   devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Project: "ouroboros-terraform",
		Repo:    "ouroboros-terraform",
		Flake:   "./overlays/ouroboros-terraform#default",
		FlakeInputOverrides: map[string]string{
			"ouroboros-terraform": "path:" + repoRoot,
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	initTestGitWorktree(t, p.Agent.HostWorktree)
	writeTestFile(t, filepath.Join(devRoot, filepath.FromSlash(codexDevkitConfigSourceRelPath)), testAuthoritativeCodexConfig(`personality = "terraform"`, strings.Join([]string{
		codexGovernanceManagedBegin,
		`[mcp_servers.governance]`,
		`command = "bash"`,
		codexGovernanceManagedEnd,
		``,
	}, "\n")))
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml"), `model = "stale-terraform"`+"\n")
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "sessions", "past.jsonl"), "past session")

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	gotConfig := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml"))
	assertSourceGeneratedGovernanceConfig(t, gotConfig, `personality = "terraform"`)
	if strings.Contains(gotConfig, `model = "stale-terraform"`) {
		t.Fatalf("stale mutable config survived Nix-source restore")
	}
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "sessions", "past.jsonl")); got != "past session" {
		t.Fatalf("session was clobbered: %q", got)
	}
	if _, err := os.Stat(filepath.Join(p.Agent.HostWorktree, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("Prepare must not create repo-local Terraform Codex config, err=%v", err)
	}
	gotEnv := readTestFile(t, filepath.Join(devRoot, ".devkit", "ouro8-governance-env.sh"))
	for _, want := range []string{
		"ouroboros-terraform=/workspaces/dev/ouroboros-terraform",
		"agent1-ouroboros-terraform=/workspaces/dev/agent-worktrees/agent1/ouroboros-terraform",
	} {
		if !strings.Contains(gotEnv, want) {
			t.Fatalf("governance env missing %q:\n%s", want, gotEnv)
		}
	}
	gotRepoConfig := readTestFile(t, filepath.Join(devRoot, ".devkit", "ouro8-governance-repo-env.json"))
	for _, want := range []string{
		`"ouroboros-terraform": "/workspaces/dev/ouroboros-terraform"`,
		`"agent1-ouroboros-terraform": "/workspaces/dev/agent-worktrees/agent1/ouroboros-terraform"`,
		`"executionGraphDecisionLogPath": "/workspaces/dev/.devkit/governance-control-plane/execution-graph-decisions.jsonl"`,
	} {
		if !strings.Contains(gotRepoConfig, want) {
			t.Fatalf("governance repo config missing %q:\n%s", want, gotRepoConfig)
		}
	}
}

func TestPrepareOuroTerraformRequiresAuthoritativeOpenAICodexConfig(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	repoRoot := filepath.Join(devRoot, "ouroboros-terraform")
	for _, dir := range []string{devkitRoot, repoRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	p, err := nativeplan.Build(nativeplan.BuildOptions{
		Paths:   devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Project: "ouroboros-terraform",
		Repo:    "ouroboros-terraform",
		Flake:   "./overlays/ouroboros-terraform#default",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	initTestGitWorktree(t, p.Agent.HostWorktree)
	badSource := filepath.Join(tmp, "base-only-config.toml")
	writeTestFile(t, badSource, `model = "base-only"`+"\n")
	t.Setenv("DEVKIT_CODEX_CONFIG_SOURCE", badSource)
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml"), testAuthoritativeCodexConfig(`personality = "stale-home"`))

	err = Prepare(p)
	if err == nil {
		t.Fatalf("expected missing openai Codex config to fail")
	}
	for _, want := range []string{
		"Nix-authored Codex config",
		"refusing to synthesize a base-only config.toml",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q: %v", want, err)
		}
	}
}

func TestPrepareOuroTerraformRestoresCodexConfigFromNixSource(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	repoRoot := filepath.Join(devRoot, "ouroboros-terraform")
	for _, dir := range []string{devkitRoot, repoRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	sourceConfig := testAuthoritativeCodexConfig(`personality = "nix-source"`)
	systemConfigPath := filepath.Join(tmp, "etc", "codex", "config.toml")
	writeTestFile(t, systemConfigPath, sourceConfig)
	setCodexSystemConfigForTest(t, systemConfigPath)
	cachePath := filepath.Join(devRoot, filepath.FromSlash(codexDevkitConfigSourceRelPath))
	writeTestFile(t, cachePath, testAuthoritativeCodexConfig(`personality = "stale-cache"`))

	p, err := nativeplan.Build(nativeplan.BuildOptions{
		Paths:   devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Project: "ouroboros-terraform",
		Repo:    "ouroboros-terraform",
		Flake:   "./overlays/ouroboros-terraform#default",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	initTestGitWorktree(t, p.Agent.HostWorktree)
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml"), `model = "base-only"`+"\n")

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml"))
	assertSourceGeneratedGovernanceConfig(t, got, `personality = "nix-source"`)
	if strings.Contains(got, `model = "base-only"`) {
		t.Fatalf("base-only config survived Nix-source restore:\n%s", got)
	}
	if gotCache := readTestFile(t, cachePath); gotCache != sourceConfig {
		t.Fatalf("cache was not refreshed from system config:\n%s", gotCache)
	}
}

func TestPrepareDevWorkspaceCleansHomeGovernanceConfigAndLinksSkills(t *testing.T) {
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	policy := "prefix_rule(\n    pattern = [\"rg\"],\n    decision = \"forbidden\"\n)\n"
	writeTestFile(t, filepath.Join(devkitRoot, "overlays", "dev-all", "codex-governed-search-policy.rules"), policy)
	writeTestFile(t, filepath.Join(devRoot, ".codex", "config.toml"), "top_level = true\n")
	writeTestFile(t, filepath.Join(devRoot, ".codex", "skills", "devkit-management", "SKILL.md"), "# devkit management\n")
	writeTestFile(t, filepath.Join(devRoot, ".codex", "skills", "autonomy-contract", "SKILL.md"), "# autonomy contract\n")
	writeTestFile(t, filepath.Join(devRoot, "ouroboros-ide", ".codex", "skills", "governed-search", "SKILL.md"), "# governed search\n")
	systemConfigPath := filepath.Join(tmp, "etc", "codex", "config.toml")
	writeTestFile(t, systemConfigPath, testAuthoritativeCodexConfig(`personality = "dev-workspace"`, strings.Join([]string{
		codexGovernanceManagedBegin,
		`[mcp_servers.governance]`,
		`command = "bash"`,
		codexGovernanceManagedEnd,
		``,
	}, "\n")))
	setCodexSystemConfigForTest(t, systemConfigPath)
	writeTestFile(t, filepath.Join(devRoot, filepath.FromSlash(codexDevkitConfigSourceRelPath)), testAuthoritativeCodexConfig(`personality = "stale-dev-workspace-cache"`))

	p, err := nativeplan.Build(nativeplan.BuildOptions{
		Paths:   devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Project: "dev-workspace",
		Flake:   "./overlays/dev-workspace#default",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	initTestGitWorktree(t, p.Agent.HostWorktree)
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml"), `model = "stale-dev-workspace"`+"\n")

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if got := readTestFile(t, filepath.Join(devRoot, ".codex", "config.toml")); got != "top_level = true\n" {
		t.Fatalf("top-level config was modified:\n%s", got)
	}
	gotAgentConfig := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "config.toml"))
	assertSourceGeneratedGovernanceConfig(t, gotAgentConfig, `personality = "dev-workspace"`)
	if strings.Contains(gotAgentConfig, `model = "stale-dev-workspace"`) {
		t.Fatalf("stale mutable config survived Nix-source restore")
	}
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "rules", "governed-search-policy.rules")); got != policy {
		t.Fatalf("policy rules = %q, want %q", got, policy)
	}
	for skill, wantTarget := range map[string]string{
		"devkit-management": filepath.Join(devRoot, ".codex", "skills", "devkit-management"),
		"autonomy-contract": filepath.Join(devRoot, ".codex", "skills", "autonomy-contract"),
		"governed-search":   filepath.Join(devRoot, "ouroboros-ide", ".codex", "skills", "governed-search"),
	} {
		link := filepath.Join(p.Agent.HostHome, ".codex", "skills", skill)
		gotTarget, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("read skill link %s: %v", link, err)
		}
		if gotTarget != wantTarget {
			t.Fatalf("skill link %s = %q, want %q", skill, gotTarget, wantTarget)
		}
	}
	gotEnv := readTestFile(t, filepath.Join(devRoot, ".devkit", "ouro8-governance-env.sh"))
	for _, want := range []string{
		"dev-workspace=/workspaces/dev",
		"ouroboros-ide=/workspaces/dev/ouroboros-ide",
		"ouroboros-terraform=/workspaces/dev/ouroboros-terraform",
	} {
		if !strings.Contains(gotEnv, want) {
			t.Fatalf("governance env missing %q:\n%s", want, gotEnv)
		}
	}
	gotRepoConfig := readTestFile(t, filepath.Join(devRoot, ".devkit", "ouro8-governance-repo-env.json"))
	for _, want := range []string{
		`"dev-workspace"`,
		`"dev-workspace": "/workspaces/dev"`,
		`"ouroboros-ide": "/workspaces/dev/ouroboros-ide"`,
		`"ouroboros-terraform": "/workspaces/dev/ouroboros-terraform"`,
		`"executionGraphDecisionLogPath": "/workspaces/dev/.devkit/governance-control-plane/execution-graph-decisions.jsonl"`,
	} {
		if !strings.Contains(gotRepoConfig, want) {
			t.Fatalf("governance repo config missing %q:\n%s", want, gotRepoConfig)
		}
	}
	if strings.Contains(gotRepoConfig, `"latestJarPath"`) || strings.Contains(gotRepoConfig, "tools/subagent-governance/subagent-governance.jar") {
		t.Fatalf("governance repo config must not carry mutable jar authority:\n%s", gotRepoConfig)
	}
}

func TestPrepareCreatesSharedScalaCachesAndCapsOnlyCodexTUILog(t *testing.T) {
	t.Setenv("DEVKIT_CODEX_TUI_LOG_MAX_BYTES", "8")
	tmp := t.TempDir()
	devRoot := filepath.Join(tmp, "dev")
	devkitRoot := filepath.Join(devRoot, "devkit")
	p, err := nativeplan.BuildDevAll(nativeplan.BuildOptions{
		Paths: devkitpaths.Paths{Root: devkitRoot, RuntimeAuthorityRoot: devkitRoot},
		Repo:  "ouroboros-ide",
		Index: 2,
	})
	if err != nil {
		t.Fatalf("BuildDevAll: %v", err)
	}
	initTestGitWorktree(t, p.Agent.HostWorktree)
	writeTestFile(t, filepath.Join(devRoot, filepath.FromSlash(codexDevkitConfigSourceRelPath)), testAuthoritativeCodexConfig(""))
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "log", "codex-tui.log"), "0123456789abcdef")
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "sessions", "keep.jsonl"), "session")
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "state.sqlite"), "sqlite")
	writeTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "rollouts", "keep.jsonl"), "rollout")

	if err := Prepare(p); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	for _, dir := range []string{
		filepath.Join(devRoot, ".cache", "shared", "coursier"),
		filepath.Join(devRoot, ".cache", "shared", "ivy2"),
		filepath.Join(p.Agent.HostHome, ".sbt"),
		filepath.Join(p.Agent.HostHome, ".sbt", "boot"),
	} {
		if st, err := os.Stat(dir); err != nil {
			t.Fatalf("shared/per-agent cache dir missing %s: %v", dir, err)
		} else if !st.IsDir() {
			t.Fatalf("cache path is not a dir: %s", dir)
		}
	}
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "log", "codex-tui.log")); got != "89abcdef" {
		t.Fatalf("capped codex-tui.log = %q", got)
	}
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "sessions", "keep.jsonl")); got != "session" {
		t.Fatalf("session was touched: %q", got)
	}
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "state.sqlite")); got != "sqlite" {
		t.Fatalf("sqlite state was touched: %q", got)
	}
	if got := readTestFile(t, filepath.Join(p.Agent.HostHome, ".codex", "rollouts", "keep.jsonl")); got != "rollout" {
		t.Fatalf("rollout was touched: %q", got)
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

func TestDevAllOverlayRequiresGovernanceProvenance(t *testing.T) {
	overlayConfig := readTestFile(t, filepath.Join("..", "..", "..", "..", "..", "overlays", "dev-all", "devkit.yaml"))
	for _, want := range []string{
		"name: governance-provenance",
		"env_file=/workspaces/dev/.devkit/ouro8-governance-env.sh",
		"/nix/store/*/share/subagent-governance/subagent-governance.jar",
		"DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH",
		"DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256",
		"governance jar hash drift",
		"server.jar.path",
		"server.jar.sha256",
		"governance singleton jar path drift",
		"governance singleton jar hash drift",
		"/agent-state",
		"governance-mcp-stdio-forward/provenance.json",
		"governance bridge jar path drift",
		"governance bridge catalog hash drift",
		"governance bridge entrypoint fingerprint drift",
		"/workspaces/dev/.devkit/governance-control-plane",
		"governance state dir must be external to Product checkout",
		"ouroboros-ide=/workspaces/dev/ouroboros-ide",
	} {
		if !strings.Contains(overlayConfig, want) {
			t.Fatalf("dev-all overlay missing governance provenance guard %q:\n%s", want, overlayConfig)
		}
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

func setCodexSystemConfigForTest(t *testing.T, path string) {
	t.Helper()
	previous := codexSystemConfigPath
	codexSystemConfigPath = path
	t.Cleanup(func() {
		codexSystemConfigPath = previous
	})
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

func TestLocalDirectFleetTargetAllowsOnlyCanonicalControllerAlias(t *testing.T) {
	if target, ok, err := localDirectFleetTarget([]string{"/home/bayesartre/dev/bin/fleet", "app-rpc", "thread-list", "--target", "target-1"}); err != nil || !ok || target != "target-1" {
		t.Fatalf("canonical controller alias was not accepted: target=%q ok=%v err=%v", target, ok, err)
	}
	if _, _, err := localDirectFleetTarget([]string{"/tmp/fleet", "app-rpc", "thread-list", "--target", "target-1"}); err != nil {
		t.Fatalf("target parsing should defer executable identity validation: %v", err)
	}
	if _, _, err := localDirectFleetTarget([]string{"/home/bayesartre/dev/bin/fleet", "app-rpc", "thread-list", "--target", "target-1", "--socket", "/tmp/foreign.sock"}); err == nil {
		t.Fatal("caller socket override was accepted")
	}
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

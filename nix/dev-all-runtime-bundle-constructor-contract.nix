{
  mkDevAllRuntimeBundle,
  pkgs,
}:

let
  fixture = import ./dev-all-runtime-bundle-fixture.nix {
    inherit pkgs;
    productSourceRev = "1111111111111111111111111111111111111111";
  };
  constructorArgs = {
    inherit pkgs;
  } // fixture.constructorArgs;
  alternateCodexConfig = pkgs.writeText "alternate-codex-authorization.toml" ''
    approval_policy = "on-request"
    sandbox_mode = "workspace-write"
  '';
  bundle = mkDevAllRuntimeBundle constructorArgs;
  closure = pkgs.closureInfo {
    rootPaths = [ bundle ];
  };
in
pkgs.runCommand "dev-all-runtime-bundle-public-constructor-contract" {
  nativeBuildInputs = [
    pkgs.coreutils
    pkgs.findutils
    pkgs.gnugrep
    pkgs.jq
  ];
} ''
  set -euo pipefail

  launcher='${bundle}/bin/dev-all-runtime-bundle'
  identity='${bundle}/share/dev-all-runtime-bundle/identity.json'
  identity_env='${bundle}/share/dev-all-runtime-bundle/identity.env'
  product_projection='${fixture.productRuntimeProjection.envPath}'
  revision='${fixture.productSourceRev}'
  verifier='${bundle.codexAuthorizationVerifierPath}'
  config_path='${constructorArgs.codexAuthorization.configPath}'
  config_sha256='${constructorArgs.codexAuthorization.configSha256}'
  system_path='${constructorArgs.codexAuthorization.systemPath}'
  alternate_config='${alternateCodexConfig}'
  wrong_sha256='ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff'

  expect_rejection() {
    label="$1"
    shift
    if "$verifier" "$@"; then
      echo "Codex authorization verifier accepted $label sabotage" >&2
      exit 1
    fi
  }

  test -r '${bundle.codexAuthorizationByteProofPath}/verified'
  "$verifier" "$config_path" "$config_sha256" "$config_path" "$config_sha256" "$system_path"
  expect_rejection coordinated-digest \
    "$config_path" "$wrong_sha256" "$config_path" "$wrong_sha256" "$system_path"
  expect_rejection coordinated-path \
    "$alternate_config" "$config_sha256" "$alternate_config" "$config_sha256" "$system_path"
  expect_rejection coordinated-path-and-digest \
    "$alternate_config" "$wrong_sha256" "$alternate_config" "$wrong_sha256" "$system_path"
  expect_rejection one-sided-path \
    "$alternate_config" "$config_sha256" "$config_path" "$config_sha256" "$system_path"
  expect_rejection one-sided-digest \
    "$config_path" "$wrong_sha256" "$config_path" "$config_sha256" "$system_path"
  expect_rejection noncanonical-system-path \
    "$config_path" "$config_sha256" "$config_path" "$config_sha256" /etc/codex/other.toml

  env -i PATH=/nonexistent "$launcher" validate
  grep -Fx ". $product_projection" "$identity_env" >/dev/null
  test "$(grep -c '^export DEVKIT_GOVERNANCE_SOURCE_REV=' "$product_projection")" = 1
  test "$(grep -c '^export DEVKIT_GOVERNANCE_SOURCE_REV=' "$identity_env")" = 0
  env -i PATH=/nonexistent "$launcher" identity-nul > "$TMPDIR/identity.nul"
  test "$(tr -cd '\000' < "$TMPDIR/identity.nul" | wc -c)" = 33
  env -i PATH=/nonexistent "$launcher" fleet route-check exact > "$TMPDIR/fleet"
  grep -Fx 'fleet-fixture:route-check exact' "$TMPDIR/fleet" >/dev/null
  env -i PATH=/nonexistent "$launcher" plugin-smoke > "$TMPDIR/plugin-smoke"
  grep -Fx 'sourceRev=${fixture.productSourceRev}' "$TMPDIR/plugin-smoke" >/dev/null
  jq -e --arg revision "$revision" --arg product_projection "$product_projection" '
    .schemaVersion == "fleet-runtime-authority/v1" and
    .sources["ouroboros-ide"].rev == $revision and
    (.sources | keys) == [
      "dev-workspace", "devkit", "fleet-control", "microvm",
      "nixos-wsl", "nixpkgs", "ouroboros-ide", "wsl"
    ] and
    (.sources | all(.[]; (keys == ["rev"]))) and
    (.runtimeIdentity.governance.jarPath | startswith("/nix/store/")) and
    (.runtimeIdentity.submitToCi.jarPath | startswith("/nix/store/")) and
    (.runtimeIdentity.artifactColumnPlugin.repositoryPath | startswith("/nix/store/")) and
    (.runtimeIdentity.sbtControlPlane.jarPath | startswith("/nix/store/")) and
    .artifactDigests.governance == .runtimeIdentity.governance.jarSha256 and
    .artifactDigests.submitToCi == .runtimeIdentity.submitToCi.jarSha256 and
    .artifactDigests.artifactColumnPlugin == .runtimeIdentity.artifactColumnPlugin.jarSha256 and
    .artifactDigests.sbtControlPlane == .runtimeIdentity.sbtControlPlane.jarSha256 and
    .codexAuthorization.configPath == .devkitProductAdapter.codexConfigPath and
    .codexAuthorization.configSha256 == .devkitProductAdapter.artifactDigests.codex_config and
    .devkitProductAdapter.governanceEnvPath == $product_projection and
    .codexAuthorization.systemPath == "/etc/codex/config.toml"
  ' "$identity" >/dev/null

  forbidden_hash="builtins.hash""File"
  forbidden_read="builtins.read""File"
  forbidden_try="builtins.try""Eval"
  forbidden_deep="builtins.deep""Seq"
  ! grep -F "$forbidden_hash" '${./dev-all-runtime-bundle.nix}'
  ! grep -F "$forbidden_read" '${./dev-all-runtime-bundle.nix}'
  ! grep -F -e "$forbidden_try" -e "$forbidden_deep" \
    '${./dev-all-runtime-bundle-constructor-contract.nix}'

  scan_forbidden() {
    label="$1"
    shift
    for forbidden in "$@"; do
      matches="$TMPDIR/closure-forbidden-matches"
      : > "$matches"
      while IFS= read -r path; do
        find "$path" -type f -size -32M \
          -exec grep -aFl -- "$forbidden" {} + >> "$matches" || true
      done < '${closure}/store-paths'
      if test -s "$matches"; then
        cat "$matches" >&2
        echo "$label contains forbidden authority: $forbidden" >&2
        exit 1
      fi
    done
  }

  scan_forbidden 'public constructor closure' \
    '6826ff0ad172d35ce2eaeb62473ae26facb765a0' \
    '8e23ded5579e896c95b5a751f4d4a18da70049a9' \
    'git+file:///workspaces/dev/ouroboros-ide' \
    'builtins.getFlake' \
    'builtins.fetchGit' \
    'builtins.fetchTree'

  for source in \
    '${./mk-dev-all-runtime-bundle.nix}' \
    '${./dev-all-runtime-bundle.nix}' \
    '${./product-runtime-projection.nix}'
  do
    for forbidden in \
      '6826ff0ad172d35ce2eaeb62473ae26facb765a0' \
      '8e23ded5579e896c95b5a751f4d4a18da70049a9' \
      'git+file:///workspaces/dev/ouroboros-ide' \
      '/workspaces/dev/ouroboros-ide' \
      'builtins.getFlake' \
      'builtins.fetchGit' \
      'builtins.fetchTree' \
      '/var/lib/product-runtime/authority-selector.json' \
      'product-adapter-connect-proxy.py' \
      'python' \
      'colmena' \
      'PATH:-' \
      'mkDefaultDevAllRuntimeBundle'
    do
      ! grep -aF -- "$forbidden" "$source"
    done
  done
  flake_source='${../flake.nix}'
  for forbidden in \
    '6826ff0ad172d35ce2eaeb62473ae26facb765a0' \
    '8e23ded5579e896c95b5a751f4d4a18da70049a9' \
    'builtins.getFlake' \
    'builtins.fetchGit' \
    'builtins.fetchTree' \
    'RuntimeSourceFlake' \
    'mkPinnedGovernanceJar' \
    'mkPinnedSubmitToCiJar' \
    'mkPinnedArtifactColumnPlugin' \
    'mkPinnedSbtControlPlaneRuntimeJar' \
    'mkDefaultDevAllRuntimeBundle'
  do
    ! grep -aF -- "$forbidden" "$flake_source"
  done
  runtime_resolver="$TMPDIR/runtime-resolver.go"
  sed -n \
    '/^func selectOuroGovernanceSystemRuntimeLauncher(/,/^func metadataEnvValue(/p' \
    '${../cli/devctl/internal/runtime/launch/launch.go}' \
    > "$runtime_resolver"
  test -s "$runtime_resolver"
  for source in \
    "$runtime_resolver" \
    '${../overlays/dev-all/runtime.nix}'
  do
    for forbidden in \
      '6826ff0ad172d35ce2eaeb62473ae26facb765a0' \
      '8e23ded5579e896c95b5a751f4d4a18da70049a9' \
      'git+file:///workspaces/dev/ouroboros-ide' \
      '/workspaces/dev/ouroboros-ide' \
      'builtins.getFlake' \
      'RuntimeSourceFlake'
    do
      ! grep -aF -- "$forbidden" "$source"
    done
  done
  for forbidden in \
    'exec.LookPath("nix")' \
    '/run/current-system/sw/bin/nix' \
    '#dev-all-runtime-bundle' \
    '"--print-out-paths"' \
    'DEVKIT_GOVERNANCE_RUNTIME_LAUNCHER' \
    'validateOuroGovernanceActivationRuntimeLauncher'
  do
    ! grep -aF -- "$forbidden" "$runtime_resolver"
  done

  mkdir -p "$out"
  cp "$identity" "$out/identity.json"
  cp '${closure}/store-paths' "$out/store-paths"
  printf '%s\n' \
    'blocking public constructor contract passed' \
    'all artifacts and source revisions were caller supplied by the authoritative derivation' \
    'one fleet-runtime-authority/v1 manifest carries the exact Product revision' \
    'no historical Product or Artifact Column source authority entered the closure' \
    'no source evaluator, selector writer, deployment route, local checkout, Python adapter, ambient PATH, or fallback entered the public implementation' \
    > "$out/contract"
''

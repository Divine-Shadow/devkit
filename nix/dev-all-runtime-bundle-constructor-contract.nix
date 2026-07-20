{
  mkDevAllRuntimeBundle,
  pkgs,
}:

let
  fixture = import ./dev-all-runtime-bundle-fixture.nix {
    inherit pkgs;
    productSourceRev = "1111111111111111111111111111111111111111";
  };
  bundle = mkDevAllRuntimeBundle ({
    inherit pkgs;
  } // fixture);
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
  revision='${fixture.productSourceRev}'

  env -i PATH=/nonexistent "$launcher" validate
  env -i PATH=/nonexistent "$launcher" plugin-smoke > "$TMPDIR/plugin-smoke"
  grep -Fx 'sourceRev=${fixture.productSourceRev}' "$TMPDIR/plugin-smoke" >/dev/null
  jq -e --arg revision "$revision" '
    .governance.sourceRev == $revision and
    .submitToCi.sourceRev == $revision and
    .artifactColumnPlugin.sourceRev == $revision and
    .sbtControlPlane.sourceRev == $revision
  ' "$identity" >/dev/null

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
    '${./dev-all-runtime-bundle.nix}'
  do
    for forbidden in \
      '6826ff0ad172d35ce2eaeb62473ae26facb765a0' \
      '8e23ded5579e896c95b5a751f4d4a18da70049a9' \
      'git+file:///workspaces/dev/ouroboros-ide' \
      '/workspaces/dev/ouroboros-ide' \
      'builtins.getFlake' \
      'builtins.fetchGit' \
      'builtins.fetchTree' \
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
    'all artifacts and revision metadata were caller supplied' \
    'one authoritative Product revision reached every runtime identity' \
    'no historical Product or Artifact Column source authority entered the closure' \
    'no source evaluator, local checkout, ambient PATH, or fallback entered the public implementation' \
    > "$out/contract"
''

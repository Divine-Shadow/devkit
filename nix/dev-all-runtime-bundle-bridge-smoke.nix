{
  bundle,
  pkgs,
}:

pkgs.runCommand "dev-all-runtime-bundle-bridge-smoke" {
  nativeBuildInputs = [
    pkgs.coreutils
    pkgs.jq
  ];
} ''
  set -euo pipefail

  forwarder="$TMPDIR/governance-mcp-stdio-forward"
  provenance="$TMPDIR/provenance.json"
  cat > "$forwarder" <<'EOF'
  #!${pkgs.bash}/bin/bash
  set -euo pipefail
  test -r "$DEVKIT_GOVERNANCE_ENV"
  # shellcheck disable=SC1090
  source "$DEVKIT_GOVERNANCE_ENV"
  test "$DEVKIT_GOVERNANCE_AUTHORITATIVE_ENV" = 1
  actual_sha="$(sha256sum "$SUBAGENT_GOVERNANCE_LATEST_JAR_PATH" | awk '{print $1}')"
  test "$actual_sha" = "$DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256"
  jq -n \
    --arg identityEnv "$DEVKIT_GOVERNANCE_ENV" \
    --arg bundle "$DEVKIT_RUNTIME_BUNDLE_PATH" \
    --arg sourceRev "$DEVKIT_GOVERNANCE_SOURCE_REV" \
    --arg jar "$SUBAGENT_GOVERNANCE_LATEST_JAR_PATH" \
    '{
      jarMatchesExpected: true,
      authoritativeEnv: "1",
      identityEnv: $identityEnv,
      bundlePath: $bundle,
      sourceRev: $sourceRev,
      governanceJar: $jar
    }' > "$SUBAGENT_GOVERNANCE_MCP_BRIDGE_PROVENANCE_PATH"
  EOF
  chmod 0555 "$forwarder"

  export SUBAGENT_GOVERNANCE_MCP_BRIDGE_PROVENANCE_PATH="$provenance"
  env -i \
    SUBAGENT_GOVERNANCE_MCP_BRIDGE_PROVENANCE_PATH="$provenance" \
    '${bundle}/bin/dev-all-runtime-bundle' governance-forward "$forwarder"

  identity_env='${bundle}/share/dev-all-runtime-bundle/identity.env'
  jq -e \
    --arg identityEnv "$identity_env" \
    --arg bundle '${bundle}' \
    '.jarMatchesExpected == true and .authoritativeEnv == "1" and
     .identityEnv == $identityEnv and .bundlePath == $bundle and
     (.sourceRev | test("^[0-9a-f]{40}$")) and
     (.governanceJar | startswith("/nix/store/"))' \
    "$provenance" >/dev/null

  mkdir -p "$out"
  cp "$provenance" "$out/provenance.json"
  printf '%s\n' \
    'immutable caller-supplied bundle identity survived the governance forwarder reload boundary' \
    'diagnostic fixture only; production Product artifacts are Fleet-supplied' \
    > "$out/README"
''

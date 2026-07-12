{
  bundle,
  governanceSource,
  pkgs,
}:

pkgs.runCommand "dev-all-runtime-bundle-bridge-smoke" {
  nativeBuildInputs = with pkgs; [
    bash
    coreutils
    gawk
    gnugrep
    gnused
    jq
    python3
  ];
} ''
  set -euo pipefail

  repo="$TMPDIR/ouroboros-ide"
  mkdir -p "$repo/scripts/devops" "$repo/tools/subagent-governance/schemas" "$TMPDIR/codex/log"
  cp '${governanceSource}/scripts/devops/governance-mcp-stdio-forward' "$repo/scripts/devops/"
  cp '${governanceSource}/scripts/devops/governance-repo-env' "$repo/scripts/devops/"
  sed -i '1c #!${pkgs.bash}/bin/bash' "$repo/scripts/devops/governance-mcp-stdio-forward" "$repo/scripts/devops/governance-repo-env"
  chmod +x "$repo/scripts/devops/governance-mcp-stdio-forward" "$repo/scripts/devops/governance-repo-env"

  cat > "$repo/scripts/devops/governance-control-plane" <<'EOF'
  #!${pkgs.bash}/bin/bash
  set -euo pipefail
  test "''${1:-}" = status
  printf '%s\n' 'bridge-smoke control plane ready'
  EOF
  chmod +x "$repo/scripts/devops/governance-control-plane"

  config="$TMPDIR/ouro8-governance-repo-env.json"
  cat > "$config" <<EOF
  {
    "workspaceRoot": "$repo",
    "governanceAdapter": {
      "knownWorkspaceIds": ["bridge-smoke"],
      "workspaceRoots": {"bridge-smoke": "$repo"},
      "schemaRoot": "$repo/tools/subagent-governance/schemas",
      "controlPlaneUrl": "http://127.0.0.1:7778",
      "controlPlaneStateDir": "$TMPDIR/control-plane"
    }
  }
  EOF

  config_sha="$(sha256sum "$config" | awk '{print $1}')"
  export DEVKIT_GOVERNANCE_ENV="$TMPDIR/mutable-routing-only.sh"
  cat > "$DEVKIT_GOVERNANCE_ENV" <<EOF
  export DEVKIT_GOVERNANCE_REPO_CONFIG_PATH='$config'
  export SUBAGENT_GOVERNANCE_REPO_CONFIG_PATH='$config'
  export DEVKIT_GOVERNANCE_REPO_CONFIG_SHA256='$config_sha'
  export SUBAGENT_GOVERNANCE_WORKSPACE_ID='bridge-smoke'
  export SUBAGENT_GOVERNANCE_KNOWN_WORKSPACE_IDS='bridge-smoke'
  export SUBAGENT_GOVERNANCE_WORKSPACE_ROOTS='bridge-smoke=$repo'
  export SUBAGENT_GOVERNANCE_SCHEMA_ROOT='$repo/tools/subagent-governance/schemas'
  export SUBAGENT_GOVERNANCE_CONTROL_PLANE_URL='http://127.0.0.1:7778'
  export SUBAGENT_GOVERNANCE_FORWARD_SERVER_URL='http://127.0.0.1:7778'
  export SUBAGENT_GOVERNANCE_LATEST_JAR_PATH='/tmp/attacker.jar'
  export DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256='attacker'
  EOF
  # shellcheck disable=SC1090
  source "$DEVKIT_GOVERNANCE_ENV"

  export DEVKIT_GOVERNANCE_MCP_ENTRYPOINT_SHA256='bridge-smoke-entrypoint'
  export DEVKIT_GOVERNANCE_EXPECTED_MCP_ENTRYPOINT_SHA256='bridge-smoke-entrypoint'
  export SUBAGENT_GOVERNANCE_MCP_BRIDGE_PROVENANCE_PATH="$TMPDIR/provenance.json"
  export SUBAGENT_GOVERNANCE_MCP_BRIDGE_STATE_DIR="$TMPDIR/bridge-state"
  export SUBAGENT_GOVERNANCE_MCP_BRIDGE_TRACE_PATH="$TMPDIR/bridge-trace.jsonl"
  export CODEX_HOME="$TMPDIR/codex"
  export HOME="$TMPDIR/home"

  '${bundle}/bin/dev-all-runtime-bundle' governance-forward "$repo/scripts/devops/governance-mcp-stdio-forward" </dev/null

  identity_env='${bundle}/share/dev-all-runtime-bundle/identity.env'
  jq -e \
    --arg identityEnv "$identity_env" \
    '.jarMatchesExpected == true and .mcpEntrypointMatchesExpected == true and
     .authoritativeEnv == "1" and .repoConfigPath != ""' \
    "$TMPDIR/provenance.json" >/dev/null
  grep -Fx "export DEVKIT_RUNTIME_BUNDLE_PATH='${bundle}'" "$identity_env" >/dev/null

  mkdir -p "$out"
  cp "$TMPDIR/provenance.json" "$out/provenance.json"
  printf '%s\n' 'immutable bundle identity survived the real pinned governance forwarder reload boundary' > "$out/README"
''

{
  artifactDigests,
  codexAuthorization,
  controllerFleetPath ? null,
  controllerSourceLayer,
  controllerSourceInventory ? null,
  controllerGUIInventory ? null,
  devctlLauncherPath ? null,
  devkitProductAdapter,
  pkgs,
  productRuntimeProjection,
  runtimeIdentity,
  sources,
}:

let
  schema = "fleet-runtime-authority/v1";
  exactSourceIds = [
    "dev-workspace"
    "devkit"
    "fleet-control"
    "microvm"
    "nixos-wsl"
    "nixpkgs"
    "ouroboros-ide"
    "wsl"
  ];
  isRevision = value:
    builtins.isString value && builtins.match "[0-9a-f]{40}" value != null;
  isSha256 = value:
    builtins.isString value && builtins.match "[0-9a-f]{64}" value != null;
  sourceShapeIsExact =
    builtins.attrNames sources == exactSourceIds
    && builtins.all
      (id: builtins.attrNames sources.${id} == [ "rev" ] && isRevision sources.${id}.rev)
      exactSourceIds;
  runtime = runtimeIdentity;
  productRuntimeProjectionShapeIsExact =
    builtins.isAttrs productRuntimeProjection
    && builtins.attrNames productRuntimeProjection == [
      "envPath"
      "productSourceRev"
      "schemaVersion"
    ]
    && productRuntimeProjection.schemaVersion == "devkit/product-runtime-projection/v1"
    && productRuntimeProjection.productSourceRev == sources.ouroboros-ide.rev
    && builtins.substring 0 11 (builtins.toString productRuntimeProjection.envPath) == "/nix/store/";
  requiredRuntimeShape =
    builtins.isAttrs runtime
    && runtime ? governance
    && runtime ? submitToCi
    && runtime ? artifactColumnPlugin
    && runtime ? sbtControlPlane
    && runtime ? javaHome;
  artifactDigestShapeIsExact =
    builtins.isAttrs artifactDigests
    && builtins.attrNames artifactDigests == [
      "artifactColumnPlugin"
      "governance"
      "sbtControlPlane"
      "submitToCi"
    ]
    && builtins.all (name: isSha256 artifactDigests.${name}) (builtins.attrNames artifactDigests)
    && artifactDigests.governance == runtime.governance.jarSha256
    && artifactDigests.submitToCi == runtime.submitToCi.jarSha256
    && artifactDigests.artifactColumnPlugin == runtime.artifactColumnPlugin.jarSha256
    && artifactDigests.sbtControlPlane == runtime.sbtControlPlane.jarSha256;
  codexAuthorizationShapeIsExact =
    builtins.isAttrs codexAuthorization
    && builtins.attrNames codexAuthorization == [
      "configPath"
      "configSha256"
      "systemPath"
    ]
    && builtins.substring 0 11 (builtins.toString codexAuthorization.configPath) == "/nix/store/"
    && isSha256 codexAuthorization.configSha256
    && codexAuthorization.systemPath == "/etc/codex/config.toml"
    && builtins.isAttrs devkitProductAdapter
    && devkitProductAdapter ? codexConfigPath
    && devkitProductAdapter ? artifactDigests
    && builtins.isAttrs devkitProductAdapter.artifactDigests
    && devkitProductAdapter.artifactDigests ? codex_config
    && builtins.toString codexAuthorization.configPath
      == builtins.toString devkitProductAdapter.codexConfigPath
    && codexAuthorization.configSha256 == devkitProductAdapter.artifactDigests.codex_config;
  artifactShortRevision = builtins.substring 0 7 sources.ouroboros-ide.rev;
  quote = pkgs.lib.escapeShellArg;
  codexAuthorizationVerifier = pkgs.writeShellScriptBin "verify-codex-authorization" ''
    set -eu
    fail() {
      echo "verify-codex-authorization: $*" >&2
      exit 1
    }
    [ "$#" -eq 5 ] || fail "requires CONFIG_PATH CONFIG_SHA256 ADAPTER_CONFIG_PATH ADAPTER_CONFIG_SHA256 SYSTEM_PATH"
    config_path="$1"
    config_sha256="$2"
    adapter_config_path="$3"
    adapter_config_sha256="$4"
    system_path="$5"
    case "$config_path" in
      /nix/store/*) ;;
      *) fail "config path is not an immutable store path" ;;
    esac
    [ "$config_path" = "$adapter_config_path" ] || fail "config path projections differ"
    [ "$config_sha256" = "$adapter_config_sha256" ] || fail "config digest projections differ"
    [ "$system_path" = "/etc/codex/config.toml" ] || fail "system path is not canonical"
    [ "''${#config_sha256}" -eq 64 ] || fail "config digest length is invalid"
    case "$config_sha256" in
      *[!0-9a-f]*) fail "config digest is not lowercase hexadecimal" ;;
    esac
    [ -f "$config_path" ] || fail "config path is not a regular file"
    actual="$(${pkgs.coreutils}/bin/sha256sum "$config_path")"
    actual="''${actual%% *}"
    [ "$actual" = "$config_sha256" ] || fail "config bytes do not match the declared digest"
  '';
  codexAuthorizationByteProof = pkgs.runCommand
    "dev-all-runtime-codex-authorization-byte-proof"
    { }
    ''
      set -euo pipefail
      ${codexAuthorizationVerifier}/bin/verify-codex-authorization \
        ${quote (builtins.toString codexAuthorization.configPath)} \
        ${quote codexAuthorization.configSha256} \
        ${quote (builtins.toString devkitProductAdapter.codexConfigPath)} \
        ${quote devkitProductAdapter.artifactDigests.codex_config} \
        ${quote codexAuthorization.systemPath}
      mkdir -p "$out"
      printf '%s\n' 'Codex authorization projections and immutable bytes verified' > "$out/verified"
    '';
  envLines = [
    "export DEVKIT_RUNTIME_IDENTITY_SCHEMA_VERSION=${quote schema}"
    "export DEVKIT_RUNTIME_BUNDLE_PATH=${quote "@bundleRoot@"}"
    "export DEVKIT_RUNTIME_IDENTITY_JSON_PATH=${quote "@bundleRoot@/share/dev-all-runtime-bundle/identity.json"}"
    ". ${quote (builtins.toString productRuntimeProjection.envPath)}"
  ];
  identityEnvTemplate = pkgs.writeText "fleet-runtime-authority-env-template" (
    builtins.concatStringsSep "\n" envLines + "\n"
  );
  authority = {
    schemaVersion = schema;
    inherit
      artifactDigests
      codexAuthorization
      controllerSourceLayer
      devkitProductAdapter
      runtimeIdentity
      sources
      ;
    bundlePath = "@bundleRoot@";
    launcherPath = "@bundleRoot@/bin/dev-all-runtime-bundle";
    identityEnvPath = "@bundleRoot@/share/dev-all-runtime-bundle/identity.env";
    identityJsonPath = "@bundleRoot@/share/dev-all-runtime-bundle/identity.json";
  };
  authorityShapeIsExact = builtins.attrNames authority == [
    "artifactDigests"
    "bundlePath"
    "codexAuthorization"
    "controllerSourceLayer"
    "devkitProductAdapter"
    "identityEnvPath"
    "identityJsonPath"
    "launcherPath"
    "runtimeIdentity"
    "schemaVersion"
    "sources"
  ];
  authorityTemplate = pkgs.writeText "fleet-runtime-authority-json-template" (
    builtins.toJSON authority
  );
  launcherTemplate = pkgs.writeText "fleet-runtime-authority-launcher-template" ''
    #!${pkgs.dash}/bin/dash
    set -eu
    bundle_root='@bundleRoot@'
    identity_dir="$bundle_root/share/dev-all-runtime-bundle"
    identity_json="$identity_dir/identity.json"
    identity_env="$identity_dir/identity.env"
    identity_sha="$identity_dir/identity.json.sha256"
    fail() { echo "dev-all-runtime-bundle: $*" >&2; exit 1; }
    require_readable() { [ -r "$1" ] || fail "missing immutable runtime input: $1"; }
    require_executable() { [ -x "$1" ] || fail "missing immutable runtime executable: $1"; }
    require_sha256() {
      require_readable "$1"
      actual="$(${pkgs.coreutils}/bin/sha256sum "$1" | ${pkgs.coreutils}/bin/cut -d' ' -f1)"
      [ "$actual" = "$2" ] || fail "immutable runtime digest mismatch: $1"
    }
    validate() {
      [ -r "$identity_json" ] || fail "missing sole runtime authority"
      [ -r "$identity_env" ] || fail "missing runtime env projection"
      [ -r "$identity_sha" ] || fail "missing runtime authority digest projection"
      expected="$(${pkgs.coreutils}/bin/cat "$identity_sha")"
      actual="$(${pkgs.coreutils}/bin/sha256sum "$identity_json" | ${pkgs.coreutils}/bin/cut -d' ' -f1)"
      [ "$actual" = "$expected" ] || fail "runtime authority digest mismatch"
      ${pkgs.jq}/bin/jq -e \
        --arg schema '${schema}' \
        --arg bundle "$bundle_root" \
        --arg launcher "$bundle_root/bin/dev-all-runtime-bundle" \
        --arg env "$identity_env" \
        --arg json "$identity_json" \
        '.schemaVersion == $schema and .bundlePath == $bundle and
         .launcherPath == $launcher and .identityEnvPath == $env and
         .identityJsonPath == $json and
         (.controllerSourceLayer.packagePath | startswith("/nix/store/")) and
         (.controllerSourceLayer.packageSha256 | test("^[0-9a-f]{64}$")) and
         (.controllerSourceLayer.manifestPath | startswith("/nix/store/")) and
         (.controllerSourceLayer.manifestSha256 | test("^[0-9a-f]{64}$")) and
         (.controllerSourceLayer.launcherPath | startswith("/nix/store/")) and
         (.controllerSourceLayer.controllerDevctlPath | startswith("/nix/store/"))' "$identity_json" >/dev/null \
        || fail "runtime authority self-path mismatch"
      set -a
      # shellcheck disable=SC1090
      . "$identity_env"
      set +a
      [ "$DEVKIT_RUNTIME_IDENTITY_SCHEMA_VERSION" = '${schema}' ] \
        || fail "runtime env schema mismatch"
      [ "$DEVKIT_RUNTIME_BUNDLE_PATH" = "$bundle_root" ] \
        || fail "runtime env bundle mismatch"
      [ "$DEVKIT_RUNTIME_IDENTITY_JSON_PATH" = "$identity_json" ] \
        || fail "runtime env identity path mismatch"
      [ '${devkitProductAdapter.governanceEnvPath}' = '${productRuntimeProjection.envPath}' ] \
        || fail "Product adapter does not consume the sole runtime projection"
      require_sha256 '${codexAuthorization.configPath}' '${codexAuthorization.configSha256}'
      require_sha256 "$SUBAGENT_GOVERNANCE_LATEST_JAR_PATH" "$DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256"
      require_sha256 "$SUBMIT_TO_CI_JAR" "$DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_SHA256"
      require_readable "$SUBMIT_TO_CI_HASH_PATH"
      require_readable "$ARTIFACT_COLUMN_PLUGIN_METADATA_ENV"
      require_sha256 \
        "$ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH/$ARTIFACT_COLUMN_PLUGIN_IVY_PATH/jars/artifact-column-plugin_sbt2_3.jar" \
        "$ARTIFACT_COLUMN_PLUGIN_JAR_SHA256"
      require_sha256 "$SBT_CONTROL_PLANE_RUNTIME_JAR" "$SBT_CONTROL_PLANE_RUNTIME_JAR_SHA256"
      require_executable "$JAVA_HOME/bin/java"
      source_layer_package="$(${pkgs.jq}/bin/jq -r '.controllerSourceLayer.packagePath' "$identity_json")"
      source_layer_package_sha="$(${pkgs.jq}/bin/jq -r '.controllerSourceLayer.packageSha256' "$identity_json")"
      source_layer_manifest="$(${pkgs.jq}/bin/jq -r '.controllerSourceLayer.manifestPath' "$identity_json")"
      source_layer_manifest_sha="$(${pkgs.jq}/bin/jq -r '.controllerSourceLayer.manifestSha256' "$identity_json")"
      source_layer_launcher="$(${pkgs.jq}/bin/jq -r '.controllerSourceLayer.launcherPath' "$identity_json")"
      source_layer_devctl="$(${pkgs.jq}/bin/jq -r '.controllerSourceLayer.controllerDevctlPath' "$identity_json")"
      require_readable "$source_layer_package"
      require_executable "$source_layer_launcher"
      require_sha256 "$source_layer_manifest" "$source_layer_manifest_sha"
      require_readable "$source_layer_manifest.sha256"
      [ "$(${pkgs.coreutils}/bin/cat "$source_layer_manifest.sha256")" = "$source_layer_manifest_sha" ] \
        || fail "source-layer adjacent digest mismatch"
      source_layer_launcher_package="''${source_layer_launcher%/bin/*}"
      [ "$source_layer_launcher_package" = "$source_layer_package" ] \
        || fail "source-layer launcher is outside its package"
      ${pkgs.jq}/bin/jq -e \
        --arg package "$source_layer_package" \
        --arg launcher "$source_layer_launcher" \
        '.schemaVersion == "fleet-controller-source-layer/v1" and
         .packagePath == $package and .launcherPath == ($package + "/bin/fleet-source-layer") and
         $launcher == .launcherPath and
         (.controllerSourceInventory.path | startswith("/nix/store/")) and
         (.controllerGUIInventory.path | startswith("/nix/store/")) and
         (.controllerSourceInventory.sha256 | test("^[0-9a-f]{64}$")) and
         (.controllerGUIInventory.sha256 | test("^[0-9a-f]{64}$")) and
         (.controllerDevctlPath | startswith("/nix/store/"))' \
        "$source_layer_manifest" >/dev/null || fail "source-layer manifest contract mismatch"
      require_executable "$source_layer_devctl"
      require_executable '${devkitProductAdapter.executablePath}'
    }
    command="''${1:-}"
    case "$command" in
      validate) validate ;;
      identity-env) validate; ${pkgs.coreutils}/bin/cat "$identity_env" ;;
      identity-json) validate; ${pkgs.coreutils}/bin/cat "$identity_json" ;;
      identity-fingerprint) validate; ${pkgs.coreutils}/bin/cat "$identity_sha" ;;
      identity-nul)
        validate
        printf '__FLEET_RUNTIME_AUTHORITY__\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0' \
          "$DEVKIT_RUNTIME_IDENTITY_SCHEMA_VERSION" "$DEVKIT_RUNTIME_BUNDLE_PATH" \
          "$DEVKIT_GOVERNANCE_SOURCE_REV" "$DEVKIT_SUBMIT_TO_CI_SOURCE_REV" \
          "$DEVKIT_ARTIFACT_COLUMN_SOURCE_REV" "$DEVKIT_SBT_CONTROL_PLANE_SOURCE_REV" \
          "$SUBAGENT_GOVERNANCE_LATEST_JAR_PATH" "$SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR" \
          "$DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH" "$DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256" \
          "$SUBAGENT_GOVERNANCE_EXPECTED_JAR_SHA256" "$SUBMIT_TO_CI_JAR" "$SUBMIT_TO_CI_HASH_PATH" \
          "$DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_PATH" "$DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_SHA256" \
          "$SBT2_CLIENT_MODE" "$SBT2_JAVA_XMX" "$OURO_LINT_INVARIANCE_SCRIPTED_SBT2_CLIENT_MODE" \
          "$ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH" "$ARTIFACT_COLUMN_PLUGIN_METADATA_ENV" \
          "$ARTIFACT_COLUMN_PLUGIN_VERSION" \
          "$ARTIFACT_COLUMN_PLUGIN_SOURCE_REV" "$ARTIFACT_COLUMN_PLUGIN_SOURCE_SHORT_REV" \
          "$ARTIFACT_COLUMN_PLUGIN_IVY_PATH" "$ARTIFACT_COLUMN_PLUGIN_JAR_SHA256" \
          "$ARTIFACT_COLUMN_PLUGIN_PINNED_ARTIFACT" "$ARTIFACT_COLUMN_PLUGIN_FLAKE_ARTIFACT" \
          "$SBT_CONTROL_PLANE_RUNTIME_JAR" "$SBT_CONTROL_PLANE_RUNTIME_JAR_SHA256" \
          "$SBT_CONTROL_PLANE_PINNED_ARTIFACT" "$SBT_CONTROL_PLANE_FLAKE_ARTIFACT" "$JAVA_HOME"
        ;;
      plugin-smoke)
        validate
        smoke='${runtime.artifactColumnPlugin.smokeEvidence}'
        [ -r "$smoke" ] || fail "missing Product plugin smoke evidence"
        ${pkgs.coreutils}/bin/cat "$smoke"
        ;;
      exec)
        validate
        shift
        [ "$#" -gt 0 ] || fail "exec requires a command"
        unset BASH_ENV ENV
        exec "$@"
        ;;
      governance-forward)
        validate
        shift
        [ "$#" -eq 1 ] || fail "governance-forward requires one exact executable"
        [ -x "$1" ] || fail "governance forwarder is not executable"
        export DEVKIT_GOVERNANCE_ENV="$identity_env"
        unset BASH_ENV ENV
        exec '${pkgs.bash}/bin/bash' "$1"
        ;;
      fleet)
        validate
        shift
        [ "$#" -gt 0 ] || fail "fleet requires a Fleet subcommand"
        export FLEET_RUNTIME_AUTHORITY_MARKER='__FLEET_RUNTIME_AUTHORITY__'
        export FLEET_RUNTIME_AUTHORITY_SCHEMA='${schema}'
        export FLEET_RUNTIME_AUTHORITY_LAUNCHER="$bundle_root/bin/dev-all-runtime-bundle"
        export FLEET_RUNTIME_AUTHORITY_MANIFEST="$identity_json"
        export FLEET_RUNTIME_AUTHORITY_SHA256="$(${pkgs.coreutils}/bin/cat "$identity_sha")"
        unset BASH_ENV ENV
        source_layer_launcher="$(${pkgs.jq}/bin/jq -r '.controllerSourceLayer.launcherPath' "$identity_json")"
        exec "$source_layer_launcher" "$@"
        ;;
      *) fail "usage: $0 {validate|identity-env|identity-json|identity-fingerprint|identity-nul|plugin-smoke|exec COMMAND...|governance-forward FORWARDER|fleet FLEET_ARGS...}" ;;
    esac
  '';
  bundleBase = pkgs.runCommand "dev-all-runtime-bundle" {
    nativeBuildInputs = [ pkgs.jq ];
  } ''
    set -euo pipefail
    mkdir -p "$out/bin" "$out/runtime" "$out/share/dev-all-runtime-bundle"
    substitute '${launcherTemplate}' "$out/bin/dev-all-runtime-bundle" \
      --replace-fail '@bundleRoot@' "$out"
    chmod 0555 "$out/bin/dev-all-runtime-bundle"
    substitute '${authorityTemplate}' "$out/share/dev-all-runtime-bundle/identity.json" \
      --replace-fail '@bundleRoot@' "$out"
    substitute '${identityEnvTemplate}' "$out/share/dev-all-runtime-bundle/identity.env" \
      --replace-fail '@bundleRoot@' "$out"
    ${pkgs.coreutils}/bin/sha256sum "$out/share/dev-all-runtime-bundle/identity.json" \
      | ${pkgs.coreutils}/bin/cut -d' ' -f1 \
      > "$out/share/dev-all-runtime-bundle/identity.json.sha256"
    ln -s '${runtime.governance.packagePath}' "$out/runtime/governance"
    ln -s '${runtime.submitToCi.packagePath}' "$out/runtime/submit-to-ci"
    ln -s '${runtime.artifactColumnPlugin.repositoryPath}' "$out/runtime/artifact-column-plugin-repository"
    ln -s '${runtime.sbtControlPlane.packagePath}' "$out/runtime/sbt-control-plane"
    ln -s '${runtime.javaHome}' "$out/runtime/java"
    test -r '${codexAuthorizationByteProof}/verified'
    ln -s '${codexAuthorizationByteProof}' "$out/runtime/codex-authorization-byte-proof"
    "$out/bin/dev-all-runtime-bundle" validate
  '';
  bundle = bundleBase // {
    identityJsonPath = "${bundleBase}/share/dev-all-runtime-bundle/identity.json";
    identityJsonSha256Path = "${bundleBase}/share/dev-all-runtime-bundle/identity.json.sha256";
    identityEnvPath = "${bundleBase}/share/dev-all-runtime-bundle/identity.env";
    launcherPath = "${bundleBase}/bin/dev-all-runtime-bundle";
    codexAuthorizationVerifierPath =
      "${codexAuthorizationVerifier}/bin/verify-codex-authorization";
    codexAuthorizationByteProofPath = codexAuthorizationByteProof;
  };
in
assert sourceShapeIsExact;
assert requiredRuntimeShape;
assert productRuntimeProjectionShapeIsExact;
assert artifactDigestShapeIsExact;
assert codexAuthorizationShapeIsExact;
assert authorityShapeIsExact;
bundle

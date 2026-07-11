{
  artifactColumnPluginRepository,
  artifactColumnPluginSmoke,
  artifactColumnRuntimeVersion,
  governanceJar,
  governanceJarVersion,
  java,
  pkgs,
  sbtControlPlaneRuntimeJar,
  sbtControlPlaneRuntimeVersion,
  submitRuntimeVersion,
  submitToCiJar,
}:

let
  identitySchema = "devkit-dev-all-runtime-identity/v1";
  artifactColumnVersion = "0.1.0-artifact-column-v2-package-derived-ownership-20260711";
  artifactColumnJarSha256 = "948d70381978242d5da4288368622e365b1d746546606c183d3cc321f41c00d2";
  submitJarSha256 = "f3fd06efc9b92ffbda400fa5c5bbe3cc88bc46743a347e22c5f20d16441f531c";
  artifactColumnIvyPath =
    "ivy2/local/com.crib.bills.ouroboros/artifact-column-plugin_sbt2_3/${artifactColumnVersion}";
  launcher = pkgs.writeScript "dev-all-runtime-bundle" ''
    #!${pkgs.dash}/bin/dash
    set -eu
    caller_path="''${PATH:-}"
    export PATH='${pkgs.bash}/bin:${pkgs.coreutils}/bin:${pkgs.gawk}/bin:${pkgs.gnugrep}/bin:${pkgs.jq}/bin'

    bundle_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)"
    identity_dir="$bundle_root/share/dev-all-runtime-bundle"
    identity_env="$identity_dir/identity.env"
    identity_json="$identity_dir/identity.json"
    smoke_evidence="$identity_dir/plugin-smoke/adoption-check.txt"

    fail() {
      echo "dev-all-runtime-bundle: $*" >&2
      exit 1
    }

    load_identity() {
      [ -r "$identity_env" ] || fail "missing immutable identity env: $identity_env"
      [ -r "$identity_json" ] || fail "missing immutable identity JSON: $identity_json"
      set -a
      # shellcheck disable=SC1090
      . "$identity_env"
      set +a
    }

    require_file_hash() {
      path="$1"
      expected="$2"
      [ -f "$path" ] || fail "missing identity artifact: $path"
      actual="$(${pkgs.coreutils}/bin/sha256sum "$path" | ${pkgs.gawk}/bin/awk '{print $1}')"
      [ "$actual" = "$expected" ] || fail "identity artifact sha256 mismatch: $path"
    }

    validate() {
      load_identity
      [ "$DEVKIT_RUNTIME_IDENTITY_SCHEMA_VERSION" = '${identitySchema}' ] || fail "identity schema mismatch"
      [ "$DEVKIT_RUNTIME_BUNDLE_PATH" = "$bundle_root" ] || fail "identity bundle path mismatch"
      [ "$DEVKIT_GOVERNANCE_SOURCE_REV" = '${governanceJarVersion}' ] || fail "governance source revision mismatch"
      [ "$DEVKIT_SUBMIT_TO_CI_SOURCE_REV" = '${submitRuntimeVersion}' ] || fail "submit-to-ci source revision mismatch"
      [ "$DEVKIT_ARTIFACT_COLUMN_SOURCE_REV" = '${artifactColumnRuntimeVersion}' ] || fail "Artifact Column source revision mismatch"
      [ "$DEVKIT_SBT_CONTROL_PLANE_SOURCE_REV" = '${sbtControlPlaneRuntimeVersion}' ] || fail "SBT control-plane source revision mismatch"

      [ "$SUBAGENT_GOVERNANCE_LATEST_JAR_PATH" = "$SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR" ] || fail "governance jar path mismatch"
      [ "$SUBAGENT_GOVERNANCE_LATEST_JAR_PATH" = "$DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH" ] || fail "expected governance jar path mismatch"
      [ "$DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256" = "$SUBAGENT_GOVERNANCE_EXPECTED_JAR_SHA256" ] || fail "governance jar sha256 identity mismatch"
      require_file_hash "$SUBAGENT_GOVERNANCE_LATEST_JAR_PATH" "$DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256"

      [ "$SUBMIT_TO_CI_JAR" = "$DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_PATH" ] || fail "submit-to-ci jar path mismatch"
      [ "$SUBMIT_TO_CI_HASH_PATH" = "$SUBMIT_TO_CI_JAR.sha256" ] || fail "submit-to-ci hash path mismatch"
      [ "$SUBMIT_TO_CI_BUILD_POLICY" = reuse ] || fail "submit-to-ci build policy mismatch"
      [ "$SUBMIT_TO_CI_EXTERNAL_JAR" = 1 ] || fail "submit-to-ci external-jar identity mismatch"
      [ "$SUBMIT_TO_CI_FLAKE_ARTIFACT" = 0 ] || fail "submit-to-ci flake-artifact identity mismatch"
      [ "$SUBMIT_TO_CI_PINNED_ARTIFACT" = 0 ] || fail "submit-to-ci pinned-artifact identity mismatch"
      require_file_hash "$SUBMIT_TO_CI_JAR" "$DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_SHA256"
      [ "$(tr -d '[:space:]' < "$SUBMIT_TO_CI_HASH_PATH")" = "$DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_SHA256" ] || fail "submit-to-ci packaged hash mismatch"

      [ "$ARTIFACT_COLUMN_PLUGIN_METADATA_ENV" = "$ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH/share/artifact-column-plugin/metadata.env" ] || fail "Artifact Column metadata path mismatch"
      [ "$ARTIFACT_COLUMN_PLUGIN_VERSION" = '${artifactColumnVersion}' ] || fail "Artifact Column version mismatch"
      [ "$ARTIFACT_COLUMN_PLUGIN_SOURCE_REV" = '${artifactColumnRuntimeVersion}' ] || fail "Artifact Column metadata source revision mismatch"
      [ "$ARTIFACT_COLUMN_PLUGIN_SOURCE_SHORT_REV" = '4eaf59e' ] || fail "Artifact Column short source revision mismatch"
      [ "$ARTIFACT_COLUMN_PLUGIN_IVY_PATH" = '${artifactColumnIvyPath}' ] || fail "Artifact Column Ivy path mismatch"
      [ "$ARTIFACT_COLUMN_PLUGIN_JAR_SHA256" = '${artifactColumnJarSha256}' ] || fail "Artifact Column jar sha256 identity mismatch"
      [ "$ARTIFACT_COLUMN_PLUGIN_PINNED_ARTIFACT" = 1 ] || fail "Artifact Column pinned-artifact identity mismatch"
      [ "$ARTIFACT_COLUMN_PLUGIN_FLAKE_ARTIFACT" = 0 ] || fail "Artifact Column flake-artifact identity mismatch"
      [ -r "$ARTIFACT_COLUMN_PLUGIN_METADATA_ENV" ] || fail "missing Artifact Column metadata"
      for expected_line in \
        "ARTIFACT_COLUMN_PLUGIN_VERSION=$ARTIFACT_COLUMN_PLUGIN_VERSION" \
        "ARTIFACT_COLUMN_PLUGIN_SOURCE_REV=$ARTIFACT_COLUMN_PLUGIN_SOURCE_REV" \
        "ARTIFACT_COLUMN_PLUGIN_SOURCE_SHORT_REV=$ARTIFACT_COLUMN_PLUGIN_SOURCE_SHORT_REV" \
        "ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH=$ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH" \
        "ARTIFACT_COLUMN_PLUGIN_IVY_PATH=$ARTIFACT_COLUMN_PLUGIN_IVY_PATH" \
        "ARTIFACT_COLUMN_PLUGIN_JAR_SHA256=$ARTIFACT_COLUMN_PLUGIN_JAR_SHA256"
      do
        ${pkgs.gnugrep}/bin/grep -Fx "$expected_line" "$ARTIFACT_COLUMN_PLUGIN_METADATA_ENV" >/dev/null || fail "Artifact Column metadata mismatch: $expected_line"
      done
      plugin_jar="$ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH/$ARTIFACT_COLUMN_PLUGIN_IVY_PATH/jars/artifact-column-plugin_sbt2_3.jar"
      require_file_hash "$plugin_jar" "$ARTIFACT_COLUMN_PLUGIN_JAR_SHA256"
      [ "$(tr -d '[:space:]' < "$ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH/share/artifact-column-plugin/artifact-column-plugin.jar.sha256")" = "$ARTIFACT_COLUMN_PLUGIN_JAR_SHA256" ] || fail "Artifact Column packaged hash mismatch"

      [ "$SBT_CONTROL_PLANE_PINNED_ARTIFACT" = 1 ] || fail "SBT control-plane pinned-artifact identity mismatch"
      [ "$SBT_CONTROL_PLANE_FLAKE_ARTIFACT" = 0 ] || fail "SBT control-plane flake-artifact identity mismatch"
      require_file_hash "$SBT_CONTROL_PLANE_RUNTIME_JAR" "$SBT_CONTROL_PLANE_RUNTIME_JAR_SHA256"
      [ "$(tr -d '[:space:]' < "$SBT_CONTROL_PLANE_RUNTIME_JAR.sha256")" = "$SBT_CONTROL_PLANE_RUNTIME_JAR_SHA256" ] || fail "SBT control-plane packaged hash mismatch"
      [ -x "$JAVA_HOME/bin/java" ] || fail "JAVA_HOME does not contain executable java"

      ${pkgs.jq}/bin/jq -e \
        --arg schema "$DEVKIT_RUNTIME_IDENTITY_SCHEMA_VERSION" \
        --arg bundle "$DEVKIT_RUNTIME_BUNDLE_PATH" \
        --arg submitRev "$DEVKIT_SUBMIT_TO_CI_SOURCE_REV" \
        --arg artifactRev "$DEVKIT_ARTIFACT_COLUMN_SOURCE_REV" \
        --arg artifactVersion "$ARTIFACT_COLUMN_PLUGIN_VERSION" \
        --arg artifactRepo "$ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH" \
        --arg artifactIvy "$ARTIFACT_COLUMN_PLUGIN_IVY_PATH" \
        --arg artifactSha "$ARTIFACT_COLUMN_PLUGIN_JAR_SHA256" \
        --arg submitJar "$SUBMIT_TO_CI_JAR" \
        --arg submitSha "$DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_SHA256" \
        '.schemaVersion == $schema and .bundlePath == $bundle and
         .submitToCi.sourceRev == $submitRev and .submitToCi.jarPath == $submitJar and .submitToCi.jarSha256 == $submitSha and
         .artifactColumnPlugin.sourceRev == $artifactRev and .artifactColumnPlugin.version == $artifactVersion and
         .artifactColumnPlugin.repositoryPath == $artifactRepo and .artifactColumnPlugin.ivyPath == $artifactIvy and
         .artifactColumnPlugin.jarSha256 == $artifactSha' \
        "$identity_json" >/dev/null || fail "identity JSON disagrees with identity env"
    }

    fingerprint() {
      printf '%s\n' \
        "$DEVKIT_RUNTIME_IDENTITY_SCHEMA_VERSION" \
        "$DEVKIT_RUNTIME_BUNDLE_PATH" \
        "$DEVKIT_GOVERNANCE_SOURCE_REV" \
        "$DEVKIT_SUBMIT_TO_CI_SOURCE_REV" \
        "$DEVKIT_ARTIFACT_COLUMN_SOURCE_REV" \
        "$DEVKIT_SBT_CONTROL_PLANE_SOURCE_REV" \
        "$SUBAGENT_GOVERNANCE_LATEST_JAR_PATH" \
        "$SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR" \
        "$DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH" \
        "$DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256" \
        "$SUBAGENT_GOVERNANCE_EXPECTED_JAR_SHA256" \
        "$SUBMIT_TO_CI_JAR" \
        "$SUBMIT_TO_CI_HASH_PATH" \
        "$DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_PATH" \
        "$DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_SHA256" \
        "$SBT2_CLIENT_MODE" \
        "$SBT2_JAVA_XMX" \
        "$OURO_LINT_INVARIANCE_SCRIPTED_SBT2_CLIENT_MODE" \
        "$ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH" \
        "$ARTIFACT_COLUMN_PLUGIN_METADATA_ENV" \
        "$ARTIFACT_COLUMN_PLUGIN_VERSION" \
        "$ARTIFACT_COLUMN_PLUGIN_SOURCE_REV" \
        "$ARTIFACT_COLUMN_PLUGIN_SOURCE_SHORT_REV" \
        "$ARTIFACT_COLUMN_PLUGIN_IVY_PATH" \
        "$ARTIFACT_COLUMN_PLUGIN_JAR_SHA256" \
        "$ARTIFACT_COLUMN_PLUGIN_PINNED_ARTIFACT" \
        "$ARTIFACT_COLUMN_PLUGIN_FLAKE_ARTIFACT" \
        "$SBT_CONTROL_PLANE_RUNTIME_JAR" \
        "$SBT_CONTROL_PLANE_RUNTIME_JAR_SHA256" \
        "$SBT_CONTROL_PLANE_PINNED_ARTIFACT" \
        "$SBT_CONTROL_PLANE_FLAKE_ARTIFACT" \
        "$JAVA_HOME" | ${pkgs.coreutils}/bin/sha256sum | ${pkgs.gawk}/bin/awk '{print $1}'
    }

    command="''${1:-}"
    case "$command" in
      validate)
        validate
        ;;
      identity-env)
        validate
        cat "$identity_env"
        ;;
      identity-json)
        validate
        cat "$identity_json"
        ;;
      identity-fingerprint)
        validate
        fingerprint
        ;;
      identity-nul)
        validate
        printf '__DEVKIT_GOVERNANCE_RUNTIME_IDENTITY__\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0%s\0' \
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
        [ -r "$smoke_evidence" ] || fail "missing packaged live plugin smoke evidence: $smoke_evidence"
        ${pkgs.gnugrep}/bin/grep -Fx 'artifact-column plugin adoption lane passed' "$smoke_evidence" >/dev/null || fail "live plugin smoke did not pass"
        ${pkgs.gnugrep}/bin/grep -Fx "version=$ARTIFACT_COLUMN_PLUGIN_VERSION" "$smoke_evidence" >/dev/null || fail "live plugin smoke version mismatch"
        ${pkgs.gnugrep}/bin/grep -Fx "sourceRev=$ARTIFACT_COLUMN_PLUGIN_SOURCE_REV" "$smoke_evidence" >/dev/null || fail "live plugin smoke source revision mismatch"
        ${pkgs.gnugrep}/bin/grep -Fx "pinnedRepo=$ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH" "$smoke_evidence" >/dev/null || fail "live plugin smoke repository mismatch"
        cat "$smoke_evidence"
        ;;
      exec)
        validate
        shift
        [ "$#" -gt 0 ] || fail "exec requires a command"
        export PATH="$caller_path"
        unset BASH_ENV ENV
        exec "$@"
        ;;
      governance-forward)
        validate
        shift
        [ "$#" -eq 1 ] || fail "governance-forward requires exactly one forwarder path"
        [ -x "$1" ] || fail "governance forwarder is missing or not executable: $1"
        export DEVKIT_GOVERNANCE_ENV="$identity_env"
        export PATH="$caller_path"
        unset BASH_ENV ENV
        exec '${pkgs.bash}/bin/bash' "$1"
        ;;
      *)
        fail "usage: $0 {validate|identity-env|identity-json|identity-fingerprint|identity-nul|plugin-smoke|exec COMMAND...|governance-forward FORWARDER}"
        ;;
    esac
  '';
in
pkgs.runCommand "dev-all-runtime-bundle" { nativeBuildInputs = [ pkgs.jq ]; } ''
  set -euo pipefail

  governance_jar="${governanceJar}/share/subagent-governance/subagent-governance.jar"
  governance_sha="$(cat "$governance_jar.sha256")"
  submit_jar="${submitToCiJar}/share/submit-to-ci/submit-to-ci.jar"
  submit_sha="$(cat "$submit_jar.sha256")"
  artifact_metadata="${artifactColumnPluginRepository}/share/artifact-column-plugin/metadata.env"
  artifact_sha="$(cat "${artifactColumnPluginRepository}/share/artifact-column-plugin/artifact-column-plugin.jar.sha256")"
  sbt_runtime_jar="${sbtControlPlaneRuntimeJar}/share/sbt-control-plane-runtime/sbt-control-plane-runtime.jar"
  sbt_runtime_sha="$(cat "$sbt_runtime_jar.sha256")"

  test "$submit_sha" = '${submitJarSha256}'
  test "$(sha256sum "$submit_jar" | awk '{print $1}')" = '${submitJarSha256}'
  test "$artifact_sha" = '${artifactColumnJarSha256}'
  grep -Fx 'ARTIFACT_COLUMN_PLUGIN_VERSION=${artifactColumnVersion}' "$artifact_metadata" >/dev/null
  grep -Fx 'ARTIFACT_COLUMN_PLUGIN_SOURCE_REV=${artifactColumnRuntimeVersion}' "$artifact_metadata" >/dev/null
  grep -Fx 'ARTIFACT_COLUMN_PLUGIN_SOURCE_SHORT_REV=4eaf59e' "$artifact_metadata" >/dev/null
  grep -Fx 'ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH=${artifactColumnPluginRepository}' "$artifact_metadata" >/dev/null
  grep -Fx 'ARTIFACT_COLUMN_PLUGIN_IVY_PATH=${artifactColumnIvyPath}' "$artifact_metadata" >/dev/null
  grep -Fx 'ARTIFACT_COLUMN_PLUGIN_JAR_SHA256=${artifactColumnJarSha256}' "$artifact_metadata" >/dev/null

  mkdir -p "$out/bin" "$out/runtime" "$out/share/dev-all-runtime-bundle/plugin-smoke"
  ln -s '${launcher}' "$out/bin/dev-all-runtime-bundle"
  ln -s '${governanceJar}' "$out/runtime/governance"
  ln -s '${submitToCiJar}' "$out/runtime/submit-to-ci"
  ln -s '${artifactColumnPluginRepository}' "$out/runtime/artifact-column-plugin-repository"
  ln -s '${artifactColumnPluginSmoke}' "$out/runtime/artifact-column-plugin-smoke"
  ln -s '${sbtControlPlaneRuntimeJar}' "$out/runtime/sbt-control-plane"
  ln -s '${java}' "$out/runtime/java"
  ln -s '${artifactColumnPluginSmoke}/adoption-check.txt' "$out/share/dev-all-runtime-bundle/plugin-smoke/adoption-check.txt"

  for runtime_link in governance submit-to-ci artifact-column-plugin-repository artifact-column-plugin-smoke sbt-control-plane java; do
    test -L "$out/runtime/$runtime_link"
  done
  if find "$out" -type f -name '*.jar' -print -quit | grep -q .; then
    echo "dev-all-runtime-bundle: copied jar found inside bundle output" >&2
    exit 1
  fi

  cat > "$out/share/dev-all-runtime-bundle/identity.env" <<EOF
  export DEVKIT_RUNTIME_IDENTITY_SCHEMA_VERSION='${identitySchema}'
  export DEVKIT_RUNTIME_BUNDLE_PATH='$out'
  export DEVKIT_GOVERNANCE_SOURCE_REV='${governanceJarVersion}'
  export DEVKIT_SUBMIT_TO_CI_SOURCE_REV='${submitRuntimeVersion}'
  export DEVKIT_ARTIFACT_COLUMN_SOURCE_REV='${artifactColumnRuntimeVersion}'
  export DEVKIT_SBT_CONTROL_PLANE_SOURCE_REV='${sbtControlPlaneRuntimeVersion}'
  export SUBAGENT_GOVERNANCE_LATEST_JAR_PATH='$governance_jar'
  export SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR='$governance_jar'
  export DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH='$governance_jar'
  export DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256='$governance_sha'
  export SUBAGENT_GOVERNANCE_EXPECTED_JAR_SHA256='$governance_sha'
  export SUBMIT_TO_CI_JAR='$submit_jar'
  export SUBMIT_TO_CI_HASH_PATH='$submit_jar.sha256'
  export SUBMIT_TO_CI_BUILD_POLICY='reuse'
  export SUBMIT_TO_CI_EXTERNAL_JAR='1'
  export SUBMIT_TO_CI_FLAKE_ARTIFACT='0'
  export SUBMIT_TO_CI_PINNED_ARTIFACT='0'
  export DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_PATH='$submit_jar'
  export DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_SHA256='$submit_sha'
  export SBT2_CLIENT_MODE='force'
  export SBT2_JAVA_XMX='6g'
  export OURO_LINT_INVARIANCE_SCRIPTED_SBT2_CLIENT_MODE='force'
  export ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH='${artifactColumnPluginRepository}'
  export ARTIFACT_COLUMN_PLUGIN_METADATA_ENV='$artifact_metadata'
  export ARTIFACT_COLUMN_PLUGIN_VERSION='${artifactColumnVersion}'
  export ARTIFACT_COLUMN_PLUGIN_SOURCE_REV='${artifactColumnRuntimeVersion}'
  export ARTIFACT_COLUMN_PLUGIN_SOURCE_SHORT_REV='4eaf59e'
  export ARTIFACT_COLUMN_PLUGIN_IVY_PATH='${artifactColumnIvyPath}'
  export ARTIFACT_COLUMN_PLUGIN_JAR_SHA256='$artifact_sha'
  export ARTIFACT_COLUMN_PLUGIN_PINNED_ARTIFACT='1'
  export ARTIFACT_COLUMN_PLUGIN_FLAKE_ARTIFACT='0'
  export SBT_CONTROL_PLANE_RUNTIME_JAR='$sbt_runtime_jar'
  export SBT_CONTROL_PLANE_RUNTIME_JAR_SHA256='$sbt_runtime_sha'
  export SBT_CONTROL_PLANE_PINNED_ARTIFACT='1'
  export SBT_CONTROL_PLANE_FLAKE_ARTIFACT='0'
  export SUBAGENT_GOVERNANCE_PINNED_ARTIFACT='1'
  export SUBAGENT_GOVERNANCE_FLAKE_ARTIFACT='0'
  export DEVKIT_GOVERNANCE_AUTHORITATIVE_ENV='1'
  export JAVA_HOME='${java}'
  EOF

  jq -n \
    --arg schema '${identitySchema}' \
    --arg bundle "$out" \
    --arg governanceRev '${governanceJarVersion}' \
    --arg governancePackage '${governanceJar}' \
    --arg governanceJar "$governance_jar" \
    --arg governanceSha "$governance_sha" \
    --arg submitRev '${submitRuntimeVersion}' \
    --arg submitPackage '${submitToCiJar}' \
    --arg submitJar "$submit_jar" \
    --arg submitSha "$submit_sha" \
    --arg artifactRev '${artifactColumnRuntimeVersion}' \
    --arg artifactVersion '${artifactColumnVersion}' \
    --arg artifactRepository '${artifactColumnPluginRepository}' \
    --arg artifactMetadata "$artifact_metadata" \
    --arg artifactIvy '${artifactColumnIvyPath}' \
    --arg artifactSha "$artifact_sha" \
    --arg artifactSmoke '${artifactColumnPluginSmoke}/adoption-check.txt' \
    --arg sbtRev '${sbtControlPlaneRuntimeVersion}' \
    --arg sbtPackage '${sbtControlPlaneRuntimeJar}' \
    --arg sbtJar "$sbt_runtime_jar" \
    --arg sbtSha "$sbt_runtime_sha" \
    --arg javaHome '${java}' \
    '{
      schemaVersion: $schema,
      bundlePath: $bundle,
      governance: { sourceRev: $governanceRev, packagePath: $governancePackage, jarPath: $governanceJar, jarSha256: $governanceSha },
      submitToCi: { sourceRev: $submitRev, packagePath: $submitPackage, jarPath: $submitJar, jarSha256: $submitSha, buildPolicy: "reuse", externalJar: 1, flakeArtifact: 0, pinnedArtifact: 0 },
      artifactColumnPlugin: { sourceRev: $artifactRev, version: $artifactVersion, repositoryPath: $artifactRepository, metadataEnv: $artifactMetadata, ivyPath: $artifactIvy, jarSha256: $artifactSha, pinnedArtifact: 1, flakeArtifact: 0, smokeEvidence: $artifactSmoke },
      sbtControlPlane: { sourceRev: $sbtRev, packagePath: $sbtPackage, jarPath: $sbtJar, jarSha256: $sbtSha, pinnedArtifact: 1, flakeArtifact: 0 },
      javaHome: $javaHome
    }' > "$out/share/dev-all-runtime-bundle/identity.json"

  "$out/bin/dev-all-runtime-bundle" validate
''

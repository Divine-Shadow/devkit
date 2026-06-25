{ mkShell, packages, pkgs, pkgsPlaywright, toolsets, ... }:

mkShell "dev-all" toolsets.ouroborosAgentTools ''
  export JAVA_HOME=${pkgs.jdk21}
  export GOROOT=${packages.pinnedGo}
  case " ''${SBT_OPTS:-} " in
    *" -Djava.awt.headless="*) ;;
    *) export SBT_OPTS="-Djava.awt.headless=true''${SBT_OPTS:+ $SBT_OPTS}" ;;
  esac
  export NODE_PATH=${pkgsPlaywright.playwright-test}/lib/node_modules:${packages.pinnedNpmTools}/lib/devkit-npm-tools/node_modules''${NODE_PATH:+:$NODE_PATH}
  export PATH=${pkgs.jdk21}/bin:$PATH
  export SUBAGENT_GOVERNANCE_LATEST_JAR_PATH=${packages.pinnedGovernanceJar}/share/subagent-governance/subagent-governance.jar
  export SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR=$SUBAGENT_GOVERNANCE_LATEST_JAR_PATH
  export DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH=$SUBAGENT_GOVERNANCE_LATEST_JAR_PATH
  export DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256=$(cat ${packages.pinnedGovernanceJar}/share/subagent-governance/subagent-governance.jar.sha256)
  export SUBAGENT_GOVERNANCE_EXPECTED_JAR_SHA256=$DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256
  export SUBMIT_TO_CI_JAR=${packages.pinnedSubmitToCiJar}/share/submit-to-ci/submit-to-ci.jar
  export SUBMIT_TO_CI_HASH_PATH=${packages.pinnedSubmitToCiJar}/share/submit-to-ci/submit-to-ci.jar.sha256
  export SUBMIT_TO_CI_BUILD_POLICY=reuse
  export SUBMIT_TO_CI_EXTERNAL_JAR=1
  export SUBMIT_TO_CI_FLAKE_ARTIFACT=0
  export SUBMIT_TO_CI_PINNED_ARTIFACT=0
  export DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_PATH=$SUBMIT_TO_CI_JAR
  export DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_SHA256=$(cat "$SUBMIT_TO_CI_HASH_PATH")
  export ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH=${packages.pinnedArtifactColumnPluginRepository}
  export ARTIFACT_COLUMN_PLUGIN_REPOSITORY=$ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH
  export ARTIFACT_COLUMN_PLUGIN_METADATA_ENV=$ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH/share/artifact-column-plugin/metadata.env
  export ARTIFACT_COLUMN_PLUGIN_VERSION=$(awk -F= '/^ARTIFACT_COLUMN_PLUGIN_VERSION=/{print $2; exit}' "$ARTIFACT_COLUMN_PLUGIN_METADATA_ENV")
  export ARTIFACT_COLUMN_PLUGIN_SOURCE_REV=$(awk -F= '/^ARTIFACT_COLUMN_PLUGIN_SOURCE_REV=/{print $2; exit}' "$ARTIFACT_COLUMN_PLUGIN_METADATA_ENV")
  export ARTIFACT_COLUMN_PLUGIN_SOURCE_SHORT_REV=$(awk -F= '/^ARTIFACT_COLUMN_PLUGIN_SOURCE_SHORT_REV=/{print $2; exit}' "$ARTIFACT_COLUMN_PLUGIN_METADATA_ENV")
  export ARTIFACT_COLUMN_PLUGIN_IVY_PATH=$(awk -F= '/^ARTIFACT_COLUMN_PLUGIN_IVY_PATH=/{print $2; exit}' "$ARTIFACT_COLUMN_PLUGIN_METADATA_ENV")
  export ARTIFACT_COLUMN_PLUGIN_JAR_SHA256=$(cat ${packages.pinnedArtifactColumnPluginRepository}/share/artifact-column-plugin/artifact-column-plugin.jar.sha256)
  export ARTIFACT_COLUMN_PLUGIN_PINNED_ARTIFACT=1
  export ARTIFACT_COLUMN_PLUGIN_FLAKE_ARTIFACT=0
  export SBT_CONTROL_PLANE_RUNTIME_JAR=${packages.pinnedSbtControlPlaneRuntimeJar}/share/sbt-control-plane-runtime/sbt-control-plane-runtime.jar
  export SBT_CONTROL_PLANE_RUNTIME_JAR_SHA256=$(cat ${packages.pinnedSbtControlPlaneRuntimeJar}/share/sbt-control-plane-runtime/sbt-control-plane-runtime.jar.sha256)
  export SBT_CONTROL_PLANE_PINNED_ARTIFACT=1
  export SBT_CONTROL_PLANE_FLAKE_ARTIFACT=0
  export SBT2_CLIENT_MODE=force
  export SBT2_JAVA_XMX=6g
  export OURO_LINT_INVARIANCE_SCRIPTED_SBT2_CLIENT_MODE=force
''

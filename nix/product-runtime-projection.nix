{
  pkgs,
  productSourceRev,
  runtimeIdentity,
}:

let
  schemaVersion = "devkit/product-runtime-projection/v1";
  runtime = runtimeIdentity;
  isRevision = value:
    builtins.isString value && builtins.match "[0-9a-f]{40}" value != null;
  isSha256 = value:
    builtins.isString value && builtins.match "[0-9a-f]{64}" value != null;
  requiredRuntimeShape =
    builtins.isAttrs runtime
    && runtime ? governance
    && runtime ? submitToCi
    && runtime ? artifactColumnPlugin
    && runtime ? sbtControlPlane
    && runtime ? javaHome
    && isSha256 runtime.governance.jarSha256
    && isSha256 runtime.submitToCi.jarSha256
    && isSha256 runtime.artifactColumnPlugin.jarSha256
    && isSha256 runtime.sbtControlPlane.jarSha256;
  quote = pkgs.lib.escapeShellArg;
  artifactShortRevision = builtins.substring 0 7 productSourceRev;
  envLines = [
    "export DEVKIT_GOVERNANCE_SOURCE_REV=${quote productSourceRev}"
    "export DEVKIT_SUBMIT_TO_CI_SOURCE_REV=${quote productSourceRev}"
    "export DEVKIT_ARTIFACT_COLUMN_SOURCE_REV=${quote productSourceRev}"
    "export DEVKIT_SBT_CONTROL_PLANE_SOURCE_REV=${quote productSourceRev}"
    "export SUBAGENT_GOVERNANCE_LATEST_JAR_PATH=${quote runtime.governance.jarPath}"
    "export SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR=${quote runtime.governance.jarPath}"
    "export DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH=${quote runtime.governance.jarPath}"
    "export DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256=${quote runtime.governance.jarSha256}"
    "export SUBAGENT_GOVERNANCE_EXPECTED_JAR_SHA256=${quote runtime.governance.jarSha256}"
    "export SUBMIT_TO_CI_JAR=${quote runtime.submitToCi.jarPath}"
    "export SUBMIT_TO_CI_HASH_PATH=${quote (runtime.submitToCi.jarPath + ".sha256")}"
    "export SUBMIT_TO_CI_BUILD_POLICY='reuse'"
    "export SUBMIT_TO_CI_EXTERNAL_JAR='1'"
    "export SUBMIT_TO_CI_FLAKE_ARTIFACT='0'"
    "export SUBMIT_TO_CI_PINNED_ARTIFACT='0'"
    "export DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_PATH=${quote runtime.submitToCi.jarPath}"
    "export DEVKIT_GOVERNANCE_EXPECTED_SUBMIT_TO_CI_JAR_SHA256=${quote runtime.submitToCi.jarSha256}"
    "export SBT2_CLIENT_MODE='force'"
    "export SBT2_JAVA_XMX='6g'"
    "export OURO_LINT_INVARIANCE_SCRIPTED_SBT2_CLIENT_MODE='off'"
    "export ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH=${quote runtime.artifactColumnPlugin.repositoryPath}"
    "export ARTIFACT_COLUMN_PLUGIN_METADATA_ENV=${quote runtime.artifactColumnPlugin.metadataEnv}"
    "export ARTIFACT_COLUMN_PLUGIN_VERSION=${quote runtime.artifactColumnPlugin.version}"
    "export ARTIFACT_COLUMN_PLUGIN_SOURCE_REV=${quote productSourceRev}"
    "export ARTIFACT_COLUMN_PLUGIN_SOURCE_SHORT_REV=${quote artifactShortRevision}"
    "export ARTIFACT_COLUMN_PLUGIN_IVY_PATH=${quote runtime.artifactColumnPlugin.ivyPath}"
    "export ARTIFACT_COLUMN_PLUGIN_JAR_SHA256=${quote runtime.artifactColumnPlugin.jarSha256}"
    "export ARTIFACT_COLUMN_PLUGIN_PINNED_ARTIFACT='1'"
    "export ARTIFACT_COLUMN_PLUGIN_FLAKE_ARTIFACT='0'"
    "export SBT_CONTROL_PLANE_RUNTIME_JAR=${quote runtime.sbtControlPlane.jarPath}"
    "export SBT_CONTROL_PLANE_RUNTIME_JAR_SHA256=${quote runtime.sbtControlPlane.jarSha256}"
    "export SBT_CONTROL_PLANE_PINNED_ARTIFACT='1'"
    "export SBT_CONTROL_PLANE_FLAKE_ARTIFACT='0'"
    "export SUBAGENT_GOVERNANCE_PINNED_ARTIFACT='1'"
    "export SUBAGENT_GOVERNANCE_FLAKE_ARTIFACT='0'"
    "export DEVKIT_GOVERNANCE_AUTHORITATIVE_ENV='1'"
    "export JAVA_HOME=${quote runtime.javaHome}"
  ];
  envPath = pkgs.writeText "product-runtime-projection.env" (
    builtins.concatStringsSep "\n" envLines + "\n"
  );
in
assert isRevision productSourceRev;
assert requiredRuntimeShape;
{
  inherit envPath productSourceRev schemaVersion;
}

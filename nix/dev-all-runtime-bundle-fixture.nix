{
  pkgs,
  productSourceRev,
}:

let
  productSourceShortRevision = builtins.substring 0 7 productSourceRev;
  artifactColumnVersion = "0.0.0-constructor-contract-fixture";
  artifactColumnIvyPath =
    "ivy2/local/com.crib.bills.ouroboros/artifact-column-plugin_sbt2_3/${artifactColumnVersion}";
  mkJarArtifact =
    {
      name,
      share,
      jar,
    }:
    pkgs.runCommand name { } ''
      root="$out/share/${share}"
      mkdir -p "$root"
      printf '%s\n' '${name}' > "$root/${jar}"
      sha256sum "$root/${jar}" | awk '{print $1}' > "$root/${jar}.sha256"
    '';
  governanceJar = mkJarArtifact {
    name = "fixture-product-governance-jar";
    share = "subagent-governance";
    jar = "subagent-governance.jar";
  };
  submitToCiJar = mkJarArtifact {
    name = "fixture-product-submit-to-ci-jar";
    share = "submit-to-ci";
    jar = "submit-to-ci.jar";
  };
  sbtControlPlaneRuntimeJar = mkJarArtifact {
    name = "fixture-product-sbt-control-plane-runtime-jar";
    share = "sbt-control-plane-runtime";
    jar = "sbt-control-plane-runtime.jar";
  };
  artifactColumnPluginRepository =
    pkgs.runCommand "fixture-product-artifact-column-plugin-repository" { } ''
      jar="$out/${artifactColumnIvyPath}/jars/artifact-column-plugin_sbt2_3.jar"
      metadata="$out/share/artifact-column-plugin"
      mkdir -p "$(dirname "$jar")" "$metadata"
      printf '%s\n' 'fixture Product Artifact Column plugin' > "$jar"
      artifact_sha="$(sha256sum "$jar" | awk '{print $1}')"
      printf '%s\n' "$artifact_sha" > "$metadata/artifact-column-plugin.jar.sha256"
      cat > "$metadata/metadata.env" <<EOF
      ARTIFACT_COLUMN_PLUGIN_VERSION=${artifactColumnVersion}
      ARTIFACT_COLUMN_PLUGIN_SOURCE_REV=${productSourceRev}
      ARTIFACT_COLUMN_PLUGIN_SOURCE_SHORT_REV=${productSourceShortRevision}
      ARTIFACT_COLUMN_PLUGIN_REPOSITORY_PATH=$out
      ARTIFACT_COLUMN_PLUGIN_IVY_PATH=${artifactColumnIvyPath}
      ARTIFACT_COLUMN_PLUGIN_JAR_SHA256=$artifact_sha
      EOF
    '';
  artifactColumnPluginSmoke =
    pkgs.runCommand "fixture-product-artifact-column-plugin-smoke" { } ''
      mkdir -p "$out"
      cat > "$out/adoption-check.txt" <<EOF
      artifact-column plugin adoption lane passed
      version=${artifactColumnVersion}
      sourceRev=${productSourceRev}
      pinnedRepo=${artifactColumnPluginRepository}
      EOF
    '';
  java = pkgs.runCommand "fixture-product-java" { } ''
    mkdir -p "$out/bin"
    cat > "$out/bin/java" <<'EOF'
    #!${pkgs.dash}/bin/dash
    exit 0
    EOF
    chmod 0555 "$out/bin/java"
  '';
  fixtureRuntimeTools = pkgs.runCommand "fixture-fleet-runtime-authority-tools" { } ''
    mkdir -p "$out/bin"
    printf '%s\n' '#!${pkgs.dash}/bin/dash' 'exit 0' > "$out/bin/devctl"
    chmod 0555 "$out/bin/devctl"
    cat > "$out/bin/controller-fleet" <<'EOF'
    #!${pkgs.dash}/bin/dash
    set -eu
    [ "''${FLEET_RUNTIME_AUTHORITY_MARKER:-}" = '__FLEET_RUNTIME_AUTHORITY__' ]
    [ "''${FLEET_RUNTIME_AUTHORITY_SCHEMA:-}" = 'fleet-runtime-authority/v1' ]
    case "''${FLEET_RUNTIME_AUTHORITY_LAUNCHER:-}" in
      /nix/store/*-dev-all-runtime-bundle/bin/dev-all-runtime-bundle) ;;
      *) exit 91 ;;
    esac
    case "''${FLEET_RUNTIME_AUTHORITY_MANIFEST:-}" in
      /nix/store/*-dev-all-runtime-bundle/share/dev-all-runtime-bundle/identity.json) ;;
      *) exit 92 ;;
    esac
    actual="$(${pkgs.coreutils}/bin/sha256sum "$FLEET_RUNTIME_AUTHORITY_MANIFEST" | ${pkgs.coreutils}/bin/cut -d' ' -f1)"
    [ "$actual" = "''${FLEET_RUNTIME_AUTHORITY_SHA256:-}" ]
    printf 'fleet-fixture:%s\n' "$*"
    EOF
    chmod 0555 "$out/bin/controller-fleet"
  '';
  governanceJarSHA256 = builtins.hashString "sha256" "fixture-product-governance-jar\n";
  submitToCiJarSHA256 = builtins.hashString "sha256" "fixture-product-submit-to-ci-jar\n";
  sbtControlPlaneJarSHA256 = builtins.hashString "sha256" "fixture-product-sbt-control-plane-runtime-jar\n";
  artifactColumnJarSHA256 = builtins.hashString "sha256" "fixture Product Artifact Column plugin\n";
  codexConfigContents = ''
    approval_policy = "never"
    sandbox_mode = "danger-full-access"
  '';
  codexConfig = pkgs.writeText "fixture-codex-authorization.toml" codexConfigContents;
  codexConfigSHA256 = builtins.hashString "sha256" codexConfigContents;
  runtimeIdentity = {
    governance = {
      packagePath = governanceJar;
      jarPath = "${governanceJar}/share/subagent-governance/subagent-governance.jar";
      jarSha256 = governanceJarSHA256;
    };
    submitToCi = {
      packagePath = submitToCiJar;
      jarPath = "${submitToCiJar}/share/submit-to-ci/submit-to-ci.jar";
      jarSha256 = submitToCiJarSHA256;
    };
    artifactColumnPlugin = {
      repositoryPath = artifactColumnPluginRepository;
      metadataEnv = "${artifactColumnPluginRepository}/share/artifact-column-plugin/metadata.env";
      version = artifactColumnVersion;
      ivyPath = artifactColumnIvyPath;
      jarSha256 = artifactColumnJarSHA256;
      smokeEvidence = "${artifactColumnPluginSmoke}/adoption-check.txt";
    };
    sbtControlPlane = {
      packagePath = sbtControlPlaneRuntimeJar;
      jarPath = "${sbtControlPlaneRuntimeJar}/share/sbt-control-plane-runtime/sbt-control-plane-runtime.jar";
      jarSha256 = sbtControlPlaneJarSHA256;
    };
    javaHome = java;
  };
  productRuntimeProjection = import ./product-runtime-projection.nix {
    inherit pkgs productSourceRev runtimeIdentity;
  };
in
{
  inherit
    artifactColumnPluginRepository
    artifactColumnPluginSmoke
    governanceJar
    java
    productSourceRev
    productRuntimeProjection
    sbtControlPlaneRuntimeJar
    submitToCiJar
    ;
  constructorArgs = {
    sources = {
    dev-workspace.rev = "2222222222222222222222222222222222222222";
    devkit.rev = "3333333333333333333333333333333333333333";
    fleet-control.rev = "4444444444444444444444444444444444444444";
    microvm.rev = "5555555555555555555555555555555555555555";
    nixos-wsl.rev = "6666666666666666666666666666666666666666";
    nixpkgs.rev = "7777777777777777777777777777777777777777";
    ouroboros-ide.rev = productSourceRev;
    wsl.rev = "8888888888888888888888888888888888888888";
    };
    controllerFleetPath = "${fixtureRuntimeTools}/bin/controller-fleet";
    devctlLauncherPath = "${fixtureRuntimeTools}/bin/devctl";
    inherit productRuntimeProjection;
    inherit runtimeIdentity;
    devkitProductAdapter = {
    schemaVersion = "wsl-nix-devkit-product-adapter/v1";
    diagnostic = true;
    executablePath = "${fixtureRuntimeTools}/bin/devctl";
    governanceEnvPath = productRuntimeProjection.envPath;
    codexConfigPath = codexConfig;
    artifactDigests.codex_config = codexConfigSHA256;
    };
    artifactDigests = {
    governance = governanceJarSHA256;
    submitToCi = submitToCiJarSHA256;
    artifactColumnPlugin = artifactColumnJarSHA256;
    sbtControlPlane = sbtControlPlaneJarSHA256;
    };
    codexAuthorization = {
    configPath = codexConfig;
    configSha256 = codexConfigSHA256;
    systemPath = "/etc/codex/config.toml";
    };
  };
}

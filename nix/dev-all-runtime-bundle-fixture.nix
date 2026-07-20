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
in
{
  inherit
    artifactColumnPluginRepository
    artifactColumnPluginSmoke
    governanceJar
    java
    productSourceRev
    sbtControlPlaneRuntimeJar
    submitToCiJar
    ;
}

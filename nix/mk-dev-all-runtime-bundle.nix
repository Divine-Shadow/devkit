{
  artifactColumnPluginRepository,
  artifactColumnPluginSmoke,
  governanceJar,
  java,
  pkgs,
  productSourceRev,
  sbtControlPlaneRuntimeJar,
  submitToCiJar,
}:

# Public composition boundary. The caller selects one authoritative Product
# input, derives every artifact from it, and supplies its exact revision once.
# This constructor performs no source evaluation or artifact selection.
import ./dev-all-runtime-bundle.nix {
  inherit
    artifactColumnPluginRepository
    artifactColumnPluginSmoke
    governanceJar
    java
    pkgs
    sbtControlPlaneRuntimeJar
    submitToCiJar
    ;
  sourceRevisions = {
    governance = productSourceRev;
    submitToCi = productSourceRev;
    artifactColumn = productSourceRev;
    sbtControlPlane = productSourceRev;
  };
}

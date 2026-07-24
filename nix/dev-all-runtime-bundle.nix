{
  devctl,
  pkgs,
  runtimeTools,
}:

# This bundle is deliberately Product-agnostic.  The authoritative WSL/Nix
# composition supplies artifacts derived from one accepted Product revision;
# Devkit supplies only the reusable development tools and compiled lifecycle
# controller.
pkgs.buildEnv {
  name = "dev-all-runtime-bundle";
  paths = [
    devctl
    runtimeTools
  ];
  pathsToLink = [
    "/bin"
    "/kit"
  ];
}

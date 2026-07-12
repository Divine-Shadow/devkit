{
  description = "Devkit adapter for the ouroboros-terraform repo runtime";

  inputs.ouroboros-terraform.url = "path:./repo-owned-runtime-input-required";

  outputs =
    { ouroboros-terraform, ... }:
    {
      devShells = builtins.mapAttrs (_system: shells: {
        default = shells.default;
        ouroboros-terraform = shells.default;
      }) ouroboros-terraform.devShells;

      packages = ouroboros-terraform.packages;
      checks = ouroboros-terraform.checks;
      apps = ouroboros-terraform.apps;
    };
}

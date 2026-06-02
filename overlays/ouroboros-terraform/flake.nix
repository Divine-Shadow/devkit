{
  description = "Devkit adapter for the ouroboros-terraform repo runtime";

  inputs.ouroboros-terraform.url = "git+file:///workspaces/dev/ouroboros-terraform";

  outputs =
    { ouroboros-terraform, ... }:
    {
      devShells = builtins.mapAttrs (_system: shells: {
        default = shells.default;
        ouroboros-terraform = shells.default;
      }) ouroboros-terraform.devShells;

      apps = ouroboros-terraform.apps;
    };
}

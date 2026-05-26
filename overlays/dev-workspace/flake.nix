{
  description = "Devkit dev-workspace overlay runtime shell";

  inputs.devkit.url = "path:../..";

  outputs =
    { devkit, ... }:
    {
      devShells = builtins.mapAttrs (_system: shells: {
        default = shells.dev-workspace;
        dev-workspace = shells.dev-workspace;
      }) devkit.devShells;
    };
}

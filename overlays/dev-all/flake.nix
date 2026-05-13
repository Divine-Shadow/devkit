{
  description = "Devkit dev-all overlay runtime shell";

  inputs.devkit.url = "path:../..";

  outputs =
    { devkit, ... }:
    {
      devShells = builtins.mapAttrs (_system: shells: {
        default = shells.dev-all;
        dev-all = shells.dev-all;
      }) devkit.devShells;
    };
}

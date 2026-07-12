{
  description = "Devkit ouroboros-static-front-end overlay runtime shell";

  inputs.devkit.url = "path:../..";

  outputs =
    { devkit, ... }:
    {
      devShells = builtins.mapAttrs (_system: shells: {
        default = shells.ouroboros-static-front-end;
        ouroboros-static-front-end = shells.ouroboros-static-front-end;
      }) devkit.devShells;
    };
}

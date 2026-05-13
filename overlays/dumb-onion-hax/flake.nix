{
  description = "Devkit dumb-onion-hax overlay runtime shell";

  inputs.devkit.url = "path:../..";

  outputs =
    { devkit, ... }:
    {
      devShells = builtins.mapAttrs (_system: shells: {
        default = shells.dumb-onion-hax;
        dumb-onion-hax = shells.dumb-onion-hax;
      }) devkit.devShells;
    };
}

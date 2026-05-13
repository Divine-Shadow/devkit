{
  description = "Devkit ouroboros-terraform overlay runtime shell";

  inputs.devkit.url = "path:../..";

  outputs =
    { devkit, ... }:
    {
      devShells = builtins.mapAttrs (_system: shells: {
        default = shells.ouroboros-terraform;
        ouroboros-terraform = shells.ouroboros-terraform;
      }) devkit.devShells;
    };
}

{
  description = "Devkit pokeemerald overlay runtime shell";

  inputs.devkit.url = "path:../..";

  outputs =
    { devkit, ... }:
    {
      devShells = builtins.mapAttrs (_system: shells: {
        default = shells.pokeemerald;
        pokeemerald = shells.pokeemerald;
      }) devkit.devShells;
    };
}

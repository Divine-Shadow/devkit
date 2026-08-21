{
  description = "Devkit pokeemerald Shared Power overlay runtime shell";

  inputs.devkit.url = "path:../..";

  outputs =
    { devkit, ... }:
    {
      devShells = builtins.mapAttrs (_system: shells: {
        default = shells.pokeemerald-expansion-shared-power;
        pokeemerald-expansion-shared-power = shells.pokeemerald-expansion-shared-power;
      }) devkit.devShells;
    };
}

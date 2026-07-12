{
  description = "Devkit ouro-integration overlay runtime shell";

  inputs.devkit.url = "path:../..";

  outputs =
    { devkit, ... }:
    {
      devShells = builtins.mapAttrs (_system: shells: {
        default = shells.ouro-integration;
        ouro-integration = shells.ouro-integration;
      }) devkit.devShells;
    };
}

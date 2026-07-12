{
  description = "Devkit codex overlay runtime shell";

  inputs.devkit.url = "path:../..";

  outputs =
    { devkit, ... }:
    {
      devShells = builtins.mapAttrs (_system: shells: {
        default = shells.codex;
        codex = shells.codex;
        ouroboros-dev-agent = shells.ouroboros-dev-agent;
      }) devkit.devShells;
    };
}

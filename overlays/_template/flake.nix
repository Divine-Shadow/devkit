{
  description = "Devkit template overlay runtime shell";

  inputs.devkit.url = "path:../..";

  outputs =
    { devkit, ... }:
    {
      devShells = builtins.mapAttrs (_system: shells: {
        default = shells.template-agent;
        _template = shells._template;
        template-agent = shells.template-agent;
      }) devkit.devShells;
    };
}

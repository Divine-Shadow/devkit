{ mkShell, packages, toolsets, ... }:

mkShell "template-agent" (toolsets.commonAgentTools ++ [
  packages.pinnedCodex
]) ""

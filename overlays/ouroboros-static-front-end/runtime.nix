{ mkShell, packages, pkgs, pkgsPlaywright, toolsets, ... }:

mkShell "ouroboros-static-front-end" (toolsets.commonAgentTools ++ (with pkgs; [
  nodejs_20
  purescript
  packages.pinnedCodex
  packages.pinnedNpmTools
]) ++ [
  pkgsPlaywright.deno
  pkgsPlaywright.playwright-test
]) ''
  export NODE_PATH=${pkgsPlaywright.playwright-test}/lib/node_modules:${packages.pinnedNpmTools}/lib/devkit-npm-tools/node_modules''${NODE_PATH:+:$NODE_PATH}
''

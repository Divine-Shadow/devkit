{ mkShell, packages, pkgs, pkgsPlaywright, toolsets, ... }:

mkShell "pokeemerald" (toolsets.ouroborosAgentTools ++ (with pkgs; [
  gcc-arm-embedded
])) ''
  export JAVA_HOME=${pkgs.jdk21}
  export GOROOT=${packages.pinnedGo}
  export NODE_PATH=${pkgsPlaywright.playwright-test}/lib/node_modules:${packages.pinnedNpmTools}/lib/devkit-npm-tools/node_modules''${NODE_PATH:+:$NODE_PATH}
  export PATH=${pkgs.jdk21}/bin:$PATH
''

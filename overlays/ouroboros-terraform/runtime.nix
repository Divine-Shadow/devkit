{ mkShell, packages, pkgs, pkgsPlaywright, toolsets, ... }:

mkShell "ouroboros-terraform" (toolsets.ouroborosAgentTools ++ [
  packages.pinnedPacker
  packages.pinnedTerraform
]) ''
  export JAVA_HOME=${pkgs.jdk21}
  export GOROOT=${packages.pinnedGo}
  export NODE_PATH=${pkgsPlaywright.playwright-test}/lib/node_modules:${packages.pinnedNpmTools}/lib/devkit-npm-tools/node_modules''${NODE_PATH:+:$NODE_PATH}
  export AWS_CONFIG_FILE="$HOME/.aws/config"
  export AWS_SHARED_CREDENTIALS_FILE="$HOME/.aws/credentials"
  export AWS_SDK_LOAD_CONFIG=1
  export AWS_PROFILE="''${AWS_PROFILE:-ouroboros}"
  export PATH=${pkgs.jdk21}/bin:$PATH
''

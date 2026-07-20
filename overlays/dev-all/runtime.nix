{ mkShell, packages, pkgs, pkgsPlaywright, toolsets, ... }:

mkShell "dev-all" toolsets.ouroborosAgentTools ''
  export JAVA_HOME=${pkgs.jdk21}
  export GOROOT=${packages.pinnedGo}
  case " ''${SBT_OPTS:-} " in
    *" -Djava.awt.headless="*) ;;
    *) export SBT_OPTS="-Djava.awt.headless=true''${SBT_OPTS:+ $SBT_OPTS}" ;;
  esac
  export NODE_PATH=${pkgsPlaywright.playwright-test}/lib/node_modules:${packages.pinnedNpmTools}/lib/devkit-npm-tools/node_modules''${NODE_PATH:+:$NODE_PATH}
  export PATH=${pkgs.jdk21}/bin:$PATH
  # This shell is a local diagnostic consumer. Production Fleet composition
  # supplies its own Product-derived bundle through Devkit's public constructor.
  set -a
  source ${packages.diagnosticRuntimeBundle}/share/dev-all-runtime-bundle/identity.env
  set +a
''

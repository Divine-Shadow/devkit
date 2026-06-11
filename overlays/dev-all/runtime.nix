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
  export SUBAGENT_GOVERNANCE_LATEST_JAR_PATH=${packages.pinnedGovernanceJar}/share/subagent-governance/subagent-governance.jar
  export SUBAGENT_GOVERNANCE_CONTROL_PLANE_JAR=$SUBAGENT_GOVERNANCE_LATEST_JAR_PATH
  export DEVKIT_GOVERNANCE_EXPECTED_JAR_PATH=$SUBAGENT_GOVERNANCE_LATEST_JAR_PATH
  export DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256=$(cat ${packages.pinnedGovernanceJar}/share/subagent-governance/subagent-governance.jar.sha256)
  export SUBAGENT_GOVERNANCE_EXPECTED_JAR_SHA256=$DEVKIT_GOVERNANCE_EXPECTED_JAR_SHA256
''

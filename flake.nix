{
  description = "Devkit Nix-native agent runtime shells and migration checks";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.05";
    # Keep Playwright browser revisions compatible with the current
    # ouroboros-ide/frontend lockfile.
    nixpkgs-playwright.url = "github:NixOS/nixpkgs/f86a612cb49b3ca434c9b87f2049797656a0138d";
  };

  outputs =
    { self, nixpkgs, nixpkgs-playwright, ... }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forEachSystem =
        f:
        nixpkgs.lib.genAttrs systems (
          system:
          f {
            inherit system;
            pkgs = import nixpkgs {
              inherit system;
              config.allowUnfree = true;
            };
            pkgsPlaywright = import nixpkgs-playwright {
              inherit system;
              config.allowUnfree = true;
            };
          }
        );
      systemDetails = {
        x86_64-linux = {
          dockerArch = "x86_64";
          hashicorpArch = "amd64";
          goArch = "amd64";
          codexAsset = "codex-package-x86_64-unknown-linux-musl";
          codexHash = "sha256-awPS2JkQh0+lvie2F2Iddjj5BuiR/Yy0CvPSh2qKNv0=";
          dockerHash = "sha256-T3mLPuHgFA6rW/MLDtxOhPTNtTJVpCncO7rpUkhF1kA=";
          goHash = "sha256-unnUUmECV1GWJzQWI5zKQYplHgScKwmfMVnbhee63n0=";
          terraformHash = "sha256-GG4BRfXl8uuXy9eFvHjyG65O8VEZNJ9q1PpTW4OxDfg=";
          packerHash = "sha256-ztE+/CV9AlWTLRS4ro84hjJlEzc5oAfEMMrhBq/PxFo=";
        };
        aarch64-linux = {
          dockerArch = "aarch64";
          hashicorpArch = "arm64";
          goArch = "arm64";
          codexAsset = "codex-package-aarch64-unknown-linux-musl";
          codexHash = "sha256-1YvgTm7oBIM8JbWGhp8fpn8n8L3D85EFoqm6zvFnrkI=";
          dockerHash = "sha256-5rU3Jac3Y6s/mIxz+HcurtQpdUwaV521/xHyGZD9GBc=";
          goHash = "sha256-qOF3w1TS5KG2ECCso1YuJ+o+j4JH7KMXDj+h4ML553E=";
          terraformHash = "sha256-+FhoeYg0VYI59hSINIhACPJyJUj4QDTJsPYpNLLXPrs=";
          packerHash = "sha256-3SltdD3UWTMEMHWDz/UpC7qbho/CsLYFtkVm+BQcpyg=";
        };
      };
      codexVersion = "0.144.0";
      codexReleaseTag = "rust-v${codexVersion}";
      productRuntimeVersion = "6826ff0ad172d35ce2eaeb62473ae26facb765a0";
      governanceJarVersion = productRuntimeVersion;
      submitRuntimeVersion = productRuntimeVersion;
      artifactColumnRuntimeVersion = "8e23ded5579e896c95b5a751f4d4a18da70049a9";
      sbtControlPlaneRuntimeVersion = productRuntimeVersion;
      governanceJarSourceFlake = builtins.getFlake "git+file:///workspaces/dev/ouroboros-ide?rev=${governanceJarVersion}";
      submitRuntimeSourceFlake = builtins.getFlake "git+file:///workspaces/dev/ouroboros-ide?rev=${submitRuntimeVersion}";
      artifactColumnRuntimeSourceFlake = builtins.getFlake "git+file:///workspaces/dev/ouroboros-ide?rev=${artifactColumnRuntimeVersion}";
      sbtControlPlaneRuntimeSourceFlake = builtins.getFlake "git+file:///workspaces/dev/ouroboros-ide?rev=${sbtControlPlaneRuntimeVersion}";
      mkPinnedGovernanceJar = pkgs: governanceJarSourceFlake.packages.${pkgs.system}.governance-jar;
      mkPinnedSubmitToCiJar = pkgs: submitRuntimeSourceFlake.packages.${pkgs.system}.submit-to-ci-jar;
      mkPinnedArtifactColumnPluginRepository = pkgs: artifactColumnRuntimeSourceFlake.packages.${pkgs.system}.artifact-column-plugin-repository;
      mkPinnedArtifactColumnPluginSmoke = pkgs: artifactColumnRuntimeSourceFlake.packages.${pkgs.system}.artifact-column-plugin-adoption-check;
      mkPinnedSbtControlPlaneRuntimeJar = pkgs: sbtControlPlaneRuntimeSourceFlake.packages.${pkgs.system}.sbt-control-plane-runtime-current-source;
      mkDevAllRuntimeBundle =
        pkgs:
        let
          submitToCiJar = mkPinnedSubmitToCiJar pkgs;
          x86SubmitBaselineIsExact =
            if pkgs.system == "x86_64-linux" then
              assert toString submitToCiJar == "/nix/store/iymxmh43af91w1rh1i58xrs9a3cvd3kz-submit-to-ci-dev";
              assert submitToCiJar.drvPath == "/nix/store/bh31sf8fy6najxayfq74h8p2sy74178g-submit-to-ci-dev.drv";
              true
            else
              true;
        in
        assert x86SubmitBaselineIsExact;
        import ./nix/dev-all-runtime-bundle.nix {
          inherit
            artifactColumnRuntimeVersion
            governanceJarVersion
            pkgs
            sbtControlPlaneRuntimeVersion
            submitRuntimeVersion
            ;
          artifactColumnPluginRepository = mkPinnedArtifactColumnPluginRepository pkgs;
          artifactColumnPluginSmoke = mkPinnedArtifactColumnPluginSmoke pkgs;
          governanceJar = mkPinnedGovernanceJar pkgs;
          java = pkgs.jdk21;
          sbtControlPlaneRuntimeJar = mkPinnedSbtControlPlaneRuntimeJar pkgs;
          inherit submitToCiJar;
        };
      mkDevAllRuntimeBundleBridgeSmoke =
        pkgs:
        import ./nix/dev-all-runtime-bundle-bridge-smoke.nix {
          bundle = mkDevAllRuntimeBundle pkgs;
          governanceSource = governanceJarSourceFlake.outPath;
          inherit pkgs;
        };
      mkDevAllRuntimeBundleProfileSmoke =
        pkgs:
        import ./nix/dev-all-runtime-bundle-profile-smoke.nix {
          bundle = mkDevAllRuntimeBundle pkgs;
          inherit pkgs;
        };
      mkDevctl =
        {
          pkgs,
          sshExecutable,
          tags ? [ ],
        }:
        pkgs.buildGoModule {
          pname = "devkit-devctl";
          version = "dev";
          src = ./.;
          modRoot = "cli/devctl";
          vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
          subPackages = [ "." ];
          inherit tags;
          env.CGO_ENABLED = "0";
          ldflags = [
            "-s"
            "-w"
            "-X=devkit/cli/devctl/internal/sshauthority.packageExecutable=${sshExecutable}"
          ];
          postInstall = ''
            mkdir -p "$out/kit/bin"
            mv "$out/bin/devctl" "$out/kit/bin/devctl"
            rmdir "$out/bin"
            cp "$src/flake.nix" "$src/flake.lock" "$out/"
            cp -R "$src/nix" "$src/overlays" "$out/"
          '';
        };
      mkProductionDevctl =
        pkgs:
        mkDevctl {
          inherit pkgs;
          sshExecutable = "${pkgs.openssh}/bin/ssh";
        };
      mkProductFreshConsumerSSHAuthorityCheck =
        pkgs:
        let
          fixtureSSH = pkgs.writeShellScript "devkit-product-fixture-ssh" ''
            set -eu
            : "''${DEVKIT_TEST_PRODUCT_REMOTE:?}"
            : "''${DEVKIT_TEST_PRODUCT_SSH_LOG:?}"
            : "''${DEVKIT_TEST_PRODUCT_PROXY_USED:?}"

            printf '%s\n' "$*" >> "$DEVKIT_TEST_PRODUCT_SSH_LOG"
            config=
            previous=
            configuration_query=0
            for argument in "$@"; do
              if [ "$previous" = "-F" ]; then
                config="$argument"
              fi
              if [ "$argument" = "-G" ]; then
                configuration_query=1
              fi
              previous="$argument"
            done
            test -n "$config"
            test -r "$config"

            identity="$(${pkgs.gnused}/bin/sed -n 's/^  IdentityFile //p' "$config" | ${pkgs.coreutils}/bin/head -n 1)"
            test -n "$identity"
            test -r "$identity"
            proxy_command="$(${pkgs.gnused}/bin/sed -n 's/^  ProxyCommand //p' "$config" | ${pkgs.coreutils}/bin/head -n 1)"
            test -n "$proxy_command"

            if [ "$configuration_query" = 1 ]; then
              exit 0
            fi
            proxy_command="''${proxy_command//%h/ssh.github.com}"
            proxy_command="''${proxy_command//%p/443}"
            ${pkgs.bash}/bin/bash -c "$proxy_command" </dev/null >/dev/null
            printf '%s\n' "$config" > "$DEVKIT_TEST_PRODUCT_PROXY_USED"
            exec ${pkgs.git}/bin/git-upload-pack "$DEVKIT_TEST_PRODUCT_REMOTE"
          '';
          fixtureBwrap = pkgs.writeShellScript "devkit-fresh-consumer-bwrap" ''
            set -eu
            all_arguments="$*"
            governance_env=
            governance_catalog=
            governance_state=
            host_home=
            host_worktree=
            sandbox_home=
            codex_home=
            rollout_dir=
            mount_policy=
            while [ "$#" -gt 0 ]; do
              case "$1" in
                --ro-bind|--bind)
                  source_path="$2"
                  target_path="$3"
                  case "$target_path" in
                    /workspaces/dev/.devkit/ouro8-governance-env.sh) governance_env="$source_path" ;;
                    /workspaces/dev/.devkit/ouro8-governance-repo-env.json) governance_catalog="$source_path" ;;
                    /workspaces/dev/.devkit/governance-control-plane) governance_state="$source_path" ;;
                    /workspaces/dev/agent-worktrees/agent*/ouroboros-ide)
                      host_worktree="$source_path"
                      ;;
                    /workspaces/dev/agent-worktrees/agent*/ouroboros-ide/.devhome-agent*|\
                    /workspaces/dev/agent-worktrees/agent*/.devhome-agent*)
                      host_home="$source_path"
                      ;;
                  esac
                  shift 3
                  ;;
                --setenv)
                  case "$2" in
                    HOME) sandbox_home="$3" ;;
                    CODEX_HOME) codex_home="$3" ;;
                    CODEX_ROLLOUT_DIR) rollout_dir="$3" ;;
                    DEVKIT_NATIVE_MOUNT_POLICY_IDENTITY) mount_policy="$3" ;;
                  esac
                  shift 3
                  ;;
                *)
                  shift
                  ;;
              esac
            done

            case "$all_arguments" in
              *__DEVKIT_READINESS_CHECK__*)
                emit() {
                  name="$1"
                  result="$2"
                  detail="$3"
                  encoded="$(printf '%s' "$detail" | ${pkgs.coreutils}/bin/base64 -w0)"
                  printf '__DEVKIT_READINESS_CHECK__\truntime\t%s\t%s\t%s\n' "$name" "$result" "$encoded"
                }
                emit sandbox-command 0 ""
                emit broker-socket 0 ""
                emit required-tools 0 ""
                emit docker-client 0 ""
                emit purescript-spago-netlify 0 ""
                emit playwright-browser 0 ""
                emit codex-version 0 ""
                if [ "$mount_policy" = devkit/workspace-egress/v3 ] &&
                   [ -n "$host_worktree" ] &&
                   ${pkgs.git}/bin/git -C "$host_worktree" diff --quiet &&
                   ${pkgs.git}/bin/git -C "$host_worktree" diff --cached --quiet &&
                   [ "$(${pkgs.git}/bin/git -C "$host_worktree" rev-parse HEAD)" = "$(${pkgs.git}/bin/git -C "$host_worktree" rev-parse refs/remotes/origin/main)" ]; then
                  emit package-runtime-tools 0 ""
                else
                  emit package-runtime-tools 1 "package runtime identity or clean current Product worktree is missing"
                fi
                if [ -r "$governance_env" ] && [ -r "$governance_catalog" ] && [ -d "$governance_state" ]; then
                  emit governance-provenance 0 ""
                else
                  emit governance-provenance 1 "prepared governance runtime support is not projected into the sandbox"
                fi
                if [ -r "$host_home/.codex/config.toml" ]; then
                  emit codex-provider-config 0 ""
                  emit app-server-preparation 0 ""
                else
                  emit codex-provider-config 1 "Nix-authored Codex config is missing"
                  emit app-server-preparation 1 "source-defined app-server preparation is missing"
                fi
                exit 0
                ;;
            esac

            : "''${DEVKIT_TEST_APP_SERVER_BOUNDARY_LOG:?}"
            config_present=0
            if [ -r "$host_home/.codex/config.toml" ]; then
              config_present=1
            fi
            {
              printf 'HOME=%s\n' "$sandbox_home"
              printf 'CODEX_HOME=%s\n' "$codex_home"
              printf 'CODEX_ROLLOUT_DIR=%s\n' "$rollout_dir"
              printf 'CONFIG_PRESENT=%s\n' "$config_present"
            } > "$DEVKIT_TEST_APP_SERVER_BOUNDARY_LOG"
            printf '__DEVKIT_APP_SERVER_BOUNDARY__=PASS\n'
          '';
          fixtureAllowlist = pkgs.writeText "devkit-fresh-consumer-egress-allowlist" ''
            ssh.github.com
          '';
          fixtureCodexConfig = pkgs.writeText "devkit-fresh-consumer-codex-config.toml" ''
            # source = nixos-wsl codex config
            model = "gpt-5.5"
            model_provider = "openai"

            [profiles.openai]
            model = "gpt-5.5"
            model_provider = "openai"
          '';
          fixtureDevctlBase = mkDevctl {
            inherit pkgs;
            sshExecutable = fixtureSSH;
            tags = [ "devkitintegration" ];
          };
          fixtureDevctl = fixtureDevctlBase.overrideAttrs (old: {
            postInstall = old.postInstall + ''
              substituteInPlace "$out/overlays/dev-all/devkit.yaml" \
                --replace-fail "../../../ouroboros-ide/infra/docker/dev/tinyproxy/allowlist.txt" "${fixtureAllowlist}"
            '';
          });
        in
        pkgs.buildGoModule {
          pname = "devkit-product-fresh-consumer-ssh-authority-check";
          version = "dev";
          src = ./cli/devctl;
          modRoot = ".";
          vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
          subPackages = [ "." ];
          env.CGO_ENABLED = "0";
          DEVKIT_TEST_INSTALLED_RUNTIME_DEVCTL = "${fixtureDevctl}/kit/bin/devctl";
          DEVKIT_TEST_INSTALLED_RUNTIME_OVERLAYS = "${fixtureDevctl}/overlays";
          DEVKIT_TEST_INSTALLED_RUNTIME_BROKER = "${self.packages.${pkgs.system}.postgres-broker}/bin/postgres-broker";
          DEVKIT_TEST_INSTALLED_RUNTIME_SHELL = "${pkgs.bash}/bin/bash";
          DEVKIT_TEST_INSTALLED_RUNTIME_BWRAP = fixtureBwrap;
          DEVKIT_TEST_INSTALLED_SSH_EXECUTABLE = fixtureSSH;
          DEVKIT_TEST_INSTALLED_CODEX_CONFIG_SOURCE = fixtureCodexConfig;
          DEVKIT_TEST_SINGLE_FRESH_CONSUMER = "1";
          nativeCheckInputs = [
            pkgs.git
          ];
          doCheck = true;
          checkPhase = ''
            runHook preCheck
            go test ./integration -run '^TestInstalledRuntimeEmptyRootReconstructsThreeSlotsWithRealReadiness$' -count=1 -v
            runHook postCheck
          '';
          installPhase = ''
            mkdir -p "$out"
            printf '%s\n' ${fixtureDevctl} > "$out/fixture-devctl"
            printf '%s\n' ${fixtureSSH} > "$out/fixture-ssh"
          '';
        };
      mkNativeBootstrapStdioCleanupCheck =
        pkgs:
        pkgs.buildGoModule {
          pname = "devkit-native-bootstrap-stdio-cleanup-check";
          version = "dev";
          src = ./cli/devctl;
          modRoot = ".";
          vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
          subPackages = [ "." ];
          env.CGO_ENABLED = "0";
          DEVKIT_TEST_NSS_WRAPPER = "${pkgs.nss_wrapper}/lib/libnss_wrapper.so";
          ldflags = [
            "-s"
            "-w"
          ];
          nativeCheckInputs = [
            pkgs.git
            pkgs.openssh
          ];
          doCheck = true;
          checkPhase = ''
            runHook preCheck
            go test ./internal/runtime/egressproxy -run 'Test(ConnectUsesExactUnixSocketAndPreservesImmediateTunnelBytes|ConnectNeverTouchesHostileFixedLoopbackBridge|ConnectFailsClosedOnProxyRejection|ConnectFailsClosedWhenExactUnixSocketIsMissing|ConnectCancellationClosesTunnelWithoutWaitingForOpenInput|DialConnectTargetPreservesBannerBufferedWithUpstreamResponse|ServeRefusesExistingSocketAuthority|ServeDrainsFullPackAfterClientHalfClose|RelayFullDuplexPropagatesPeerWriteFailure|ServeAndConnectCarryCompleteGitSmartProtocolFetch)' -count=1
            go test ./internal/runtime/launch -run 'TestPrepareGitBootstrap(UsesPackageOwnedConsumerIdentityAndProxy|RejectsMissingPackageOwnedProxyHelper|RejectsMissingIdentity)' -count=1
            go test ./internal/commands/nativecmd -run 'TestWithManagedEgressProxy(EstablishesSocketBeforeBootstrapAndCleansUp|CleansExactSocketWhenCallbackFails)|TestEnsureManagedEgressProxyRefusesArbitraryExistingListener|TestRunCommandPreservingExitProjectsStdoutByteExactly|TestLifecyclePlanOptionsConsumesImmutableRuntimeExecutables' -count=1
            go test ./internal/runtime/broker -run 'TestResolveBinaryRequiresImmutableAbsoluteExecutable' -count=1
            go test ./internal/runtime/plan -run 'Test(WorkspaceEgressIsolatedRelativeMetadataUsesNoHostAliases|BuildDevAllWorkspaceEgressProjectsPreparedRuntimeSupportExactly)' -count=1
            go test ./internal/runtime/launch -run 'TestBuildBubblewrap(UsesImmutableRuntimeLauncherWithoutConsumerFlakeEvaluation|RejectsMissingOrUntrustedRuntimeLauncher)' -count=1
            go test ./internal/execx -run 'TestRunManaged(AllowsActiveCommandBeyondIdleWindow|IdleTimeoutTerminatesDescendantGroup|ContextDeadlineTerminatesDescendantGroup|PreservesCommandExitClassification)' -count=1
            go test ./internal/worktrees -run 'TestSetupNative(SSHOriginUsesExplicitBootstrapCommand|SSHOriginRejectsMissingBootstrapCommand|ProductBootstrapRejectsHTTPSFallback|ProductBootstrapRejectsAmbientCheckoutOriginAuthority|ProductBootstrapDoesNotReuseWorktreeAfterFetchFailure|IsolatedOwnedRootsUseRelativeCanonicalMetadata|RejectsStaleCommonRepositoryWithoutOwnershipMarker|FailedFetchCleansPartialOwnedRepository|RejectsRepositoryPathTraversalBeforeBootstrap|RejectsAndPreservesPartialWorktreeBeforeBootstrap)|TestRewriteNativeGitdirRejectsForeignCommondirTraversal|TestNativeReset(DisposesOpaqueInPrefixPayloadWithoutForeignCustody|RejectsOwnershipEscapesBeforeDisposal|RevalidatesCompleteBoundaryBeforeDisposal)' -count=1
            go test ./integration -run 'Test(DevAllResetReconstructsThreeSlotsThroughPackageSSHAuthority|Native(TopLevel(ExecProjectsStdoutAndCleansProxyOnEveryExit|PrepareAndExecUseIsolatedRelativeMetadata)|PrepareCarriesDelayedPackThroughActualOpenSSHProxyCommand))' -count=1
            runHook postCheck
          '';
        };
      mkDevAllRuntimeTools =
        {
          pkgs,
          pkgsPlaywright,
        }:
        let
          shell = self.devShells.${pkgs.system}.dev-all;
          runtimeInputs =
            [
              pkgs.bashInteractive
              pkgs.curl
              pkgs.nodejs_22
            ]
            ++ (shell.nativeBuildInputs or [ ])
            ++ (shell.buildInputs or [ ]);
          packageNamed =
            name:
            let
              matches = builtins.filter
                (package: (package.pname or "") == name)
                runtimeInputs;
            in
            if matches == [ ]
            then throw "dev-all runtime shell is missing ${name}"
            else builtins.head matches;
          pinnedGo = packageNamed "go";
          pinnedNpmTools = packageNamed "devkit-npm-tools";
        in
        pkgs.writeShellApplication {
          name = "dev-all-runtime-tools";
          inherit runtimeInputs;
          text = ''
            export DEVKIT_NIX_SHELL=dev-all
            export GOROOT='${pinnedGo}'
            export SSL_CERT_FILE='${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt'
            export GIT_SSL_CAINFO="$SSL_CERT_FILE"
            export TESTCONTAINERS_RYUK_DISABLED=true
            export DOCKER_HOST="''${DOCKER_HOST:-unix:///run/devkit/test-container-broker.sock}"
            export DOCKER_API_VERSION="''${DOCKER_API_VERSION:-1.52}"
            case " ''${JAVA_TOOL_OPTIONS:-} " in
              *" -Ddocker.api.version="*) ;;
              *) export JAVA_TOOL_OPTIONS="''${JAVA_TOOL_OPTIONS:+$JAVA_TOOL_OPTIONS }-Dapi.version=$DOCKER_API_VERSION -Ddocker.api.version=$DOCKER_API_VERSION" ;;
            esac
            export PLAYWRIGHT_BROWSERS_PATH='${pkgsPlaywright.playwright-driver.browsers}'
            export PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD="''${PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD:-1}"
            export NODE_PATH="${pkgsPlaywright.playwright-test}/lib/node_modules:${pinnedNpmTools}/lib/devkit-npm-tools/node_modules''${NODE_PATH:+:$NODE_PATH}"
            if [ -n "''${HTTP_PROXY:-}" ] && [ -z "''${HTTPS_PROXY:-}" ]; then
              export HTTPS_PROXY="$HTTP_PROXY"
            fi
            export NO_PROXY="''${NO_PROXY:-localhost,127.0.0.1}"
            test "$#" -gt 0 || {
              echo "dev-all-runtime-tools: command is required" >&2
              exit 64
            }
            exec "$@"
          '';
        };
      mkDevAllRuntimeShell =
        {
          bundle,
          pkgs,
          runtimeTools,
        }:
        pkgs.writeShellApplication {
          name = "dev-all-runtime-shell";
          text = ''
            test "$#" -gt 0 || {
              echo "dev-all-runtime-shell: command is required" >&2
              exit 64
            }
            exec '${bundle}/bin/dev-all-runtime-bundle' exec \
              '${runtimeTools}/bin/dev-all-runtime-tools' "$@"
          '';
        };
      mkManagementInspectionApp =
        pkgs:
        let
          devctl = mkProductionDevctl pkgs;
        in
        pkgs.writeShellApplication {
          name = "management-inspection";
          runtimeInputs = with pkgs; [
            bashInteractive
            bubblewrap
            coreutils
            findutils
            git
            gnugrep
            gnused
            gnutar
            jq
            less
            nix
            ripgrep
          ];
          text = ''
            exec ${devctl}/kit/bin/devctl management-inspect "$@"
          '';
        };
    in
    {
      devShells = forEachSystem (
        { system, pkgs, pkgsPlaywright, ... }:
        let
          details = systemDetails.${system};
          pinnedCodex = pkgs.stdenvNoCC.mkDerivation {
            pname = "codex";
            version = codexReleaseTag;
            src = pkgs.fetchurl {
              url = "https://github.com/openai/codex/releases/download/${codexReleaseTag}/${details.codexAsset}.tar.gz";
              hash = details.codexHash;
            };
            dontUnpack = true;
            installPhase = ''
              runHook preInstall
              mkdir -p "$out/bin"
              tar --no-same-owner -xzf "$src" -C "$out"
              test -x "$out/bin/codex"
              test -x "$out/bin/codex-code-mode-host"
              test -x "$out/codex-path/rg"
              test -x "$out/codex-resources/bwrap"
              test -x "$out/codex-resources/zsh/bin/zsh"
              runHook postInstall
            '';
          };

          pinnedDockerCli = pkgs.stdenvNoCC.mkDerivation {
            pname = "docker-cli";
            version = "27.5.1";
            src = pkgs.fetchurl {
              url = "https://download.docker.com/linux/static/stable/${details.dockerArch}/docker-27.5.1.tgz";
              hash = details.dockerHash;
            };
            dontUnpack = true;
            installPhase = ''
              runHook preInstall
              mkdir -p "$out/bin"
              tar -xzf "$src" -C "$TMPDIR"
              install -m 0755 "$TMPDIR/docker/docker" "$out/bin/docker"
              runHook postInstall
            '';
          };

          pinnedGo = pkgs.stdenvNoCC.mkDerivation {
            pname = "go";
            version = "1.22.4";
            src = pkgs.fetchurl {
              url = "https://go.dev/dl/go1.22.4.linux-${details.goArch}.tar.gz";
              hash = details.goHash;
            };
            dontUnpack = true;
            installPhase = ''
              runHook preInstall
              mkdir -p "$out"
              tar -xzf "$src" -C "$out" --strip-components=1
              runHook postInstall
            '';
          };

          mkHashicorpTool =
            name: version: hash:
            pkgs.stdenvNoCC.mkDerivation {
              pname = name;
              inherit version;
              src = pkgs.fetchurl {
                url = "https://releases.hashicorp.com/${name}/${version}/${name}_${version}_linux_${details.hashicorpArch}.zip";
                inherit hash;
              };
              dontUnpack = true;
              nativeBuildInputs = [ pkgs.unzip ];
              installPhase = ''
                runHook preInstall
                mkdir -p "$out/bin"
                unzip -q "$src" -d "$TMPDIR"
                install -m 0755 "$TMPDIR/${name}" "$out/bin/${name}"
                runHook postInstall
              '';
            };

          pinnedTerraform = mkHashicorpTool "terraform" "1.9.8" details.terraformHash;
          pinnedPacker = mkHashicorpTool "packer" "1.11.2" details.packerHash;

          pinnedNpmTools = pkgs.buildNpmPackage {
            pname = "devkit-npm-tools";
            version = "1.0.0";
            src = ./nix/npm-tools;
            npmDepsHash = "sha256-GD3F9zFoliysds53NG1E/8OzsknS3V+g7duGQYj3iCA=";
            dontNpmBuild = true;
            nativeBuildInputs = with pkgs; [
              makeWrapper
              pkg-config
              python3
            ];
            buildInputs = with pkgs; [
              sqlite
              vips
            ];
            installPhase = ''
              runHook preInstall

              tools_root="$out/lib/devkit-npm-tools"
              mkdir -p "$tools_root" "$out/bin"
              cp -r package.json package-lock.json node_modules "$tools_root/"

              makeWrapper ${pkgs.nodejs_22}/bin/node "$out/bin/spago" \
                --add-flags "$tools_root/node_modules/spago/bin/bundle.js" \
                --set NODE_PATH "$tools_root/node_modules"
              makeWrapper ${pkgs.nodejs_22}/bin/node "$out/bin/vite" \
                --add-flags "$tools_root/node_modules/vite/bin/vite.js" \
                --set NODE_PATH "$tools_root/node_modules"
              makeWrapper ${pkgs.nodejs_22}/bin/node "$out/bin/netlify" \
                --add-flags "$tools_root/node_modules/netlify-cli/bin/run.js" \
                --set NODE_PATH "$tools_root/node_modules"
              makeWrapper ${pkgs.nodejs_22}/bin/node "$out/bin/ntl" \
                --add-flags "$tools_root/node_modules/netlify-cli/bin/run.js" \
                --set NODE_PATH "$tools_root/node_modules"

              runHook postInstall
            '';
          };

          pinnedGovernanceJar = mkPinnedGovernanceJar pkgs;
          pinnedSubmitToCiJar = mkPinnedSubmitToCiJar pkgs;
          pinnedArtifactColumnPluginRepository = mkPinnedArtifactColumnPluginRepository pkgs;
          pinnedSbtControlPlaneRuntimeJar = mkPinnedSbtControlPlaneRuntimeJar pkgs;

          mgbaRuntimeLibs = with pkgs; [
            libedit
            libpng
            libzip
            lua5_2
            minizip
            sqlite
            zlib
          ];

          pinnedMgbaHeadless = pkgs.stdenv.mkDerivation {
            pname = "mgba-headless";
            version = "b19b557a78930ede7ee7f5dcbc880f9ff2533ffe";
            src = pkgs.fetchFromGitHub {
              owner = "mgba-emu";
              repo = "mgba";
              rev = "b19b557a78930ede7ee7f5dcbc880f9ff2533ffe";
              hash = "sha256-wUS4wLYPk/E8Ro/C7ZBhxUDOwVOV5JmKLuyDdvfdnTA=";
            };
            nativeBuildInputs = with pkgs; [
              cmake
              patchelf
              pkg-config
            ];
            buildInputs = mgbaRuntimeLibs;
            configurePhase = ''
              runHook preConfigure
              cmake -S . -B build \
                -DCMAKE_BUILD_TYPE=Release \
                -DBUILD_HEADLESS=ON \
                -DBUILD_QT=OFF \
                -DBUILD_SDL=OFF \
                -DBUILD_TEST=OFF \
                -DBUILD_SUITE=OFF \
                -DBUILD_PERF=OFF \
                -DBUILD_ROM_TEST=OFF \
                -DBUILD_CINEMA=OFF \
                -DBUILD_LIBRETRO=OFF \
                -DBUILD_SHARED=ON \
                -DENABLE_SCRIPTING=ON \
                -DUSE_LUA=ON \
                -DUSE_DISCORD_RPC=OFF \
                -DUSE_FFMPEG=OFF
              runHook postConfigure
            '';
            buildPhase = ''
              runHook preBuild
              cmake --build build --target mgba-headless -j"$NIX_BUILD_CORES"
              runHook postBuild
            '';
            installPhase = ''
              runHook preInstall
              install -Dm755 build/mgba-headless "$out/bin/mgba-headless"
              mkdir -p "$out/lib"
              find build -type f \( -name '*.so' -o -name '*.so.*' \) -exec cp -a {} "$out/lib/" \;
              ln -sfn libmgba.so.0.11.0 "$out/lib/libmgba.so.0.11"
              ln -sfn libmgba.so.0.11.0 "$out/lib/libmgba.so"
              patchelf --set-rpath "$out/lib:${pkgs.lib.makeLibraryPath mgbaRuntimeLibs}" "$out/bin/mgba-headless"
              runHook postInstall
            '';
          };

          commonAgentTools = with pkgs; [
            bashInteractive
            bubblewrap
            cacert
            coreutils
            curl
            file
            findutils
            gawk
            git
            gnugrep
            gnused
            jq
            less
            netcat-openbsd
            openssh
            procps
            python3
            ripgrep
            tmux
            uv
            vim
            which
            zsh
            pinnedDockerCli
          ];

          ouroborosAgentTools = commonAgentTools ++ (with pkgs; [
            awscli2
            cmake
            gcc
            gnumake
            jdk21
            libedit
            libpng
            libzip
            lua5_2
            minizip
            nodejs_22
            pkg-config
            purescript
            sbt
            sqlite
            tini
            unzip
            zip
            zlib
            pinnedCodex
            pinnedGo
            pinnedMgbaHeadless
            pinnedNpmTools
          ]) ++ [
            pkgsPlaywright.deno
            pkgsPlaywright.playwright-test
          ];

          mkShell =
            name: packages: extraHook:
            pkgs.mkShell {
              inherit packages;
              shellHook = ''
                export DEVKIT_NIX_SHELL=${name}
                export PATH=${pkgs.lib.makeBinPath packages}:$PATH
                export SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt
                export GIT_SSL_CAINFO=$SSL_CERT_FILE
                export TESTCONTAINERS_RYUK_DISABLED=true
                export DOCKER_HOST=''${DOCKER_HOST:-unix:///run/devkit/test-container-broker.sock}
                export DOCKER_API_VERSION=''${DOCKER_API_VERSION:-1.52}
                case " ''${JAVA_TOOL_OPTIONS:-} " in
                  *" -Ddocker.api.version="*) ;;
                  *) export JAVA_TOOL_OPTIONS="''${JAVA_TOOL_OPTIONS:+$JAVA_TOOL_OPTIONS }-Dapi.version=$DOCKER_API_VERSION -Ddocker.api.version=$DOCKER_API_VERSION" ;;
                esac
                export PLAYWRIGHT_BROWSERS_PATH=${pkgsPlaywright.playwright-driver.browsers}
                export PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=''${PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD:-1}
                if [ -n "''${HTTP_PROXY:-}" ] && [ -z "''${HTTPS_PROXY:-}" ]; then
                  export HTTPS_PROXY="$HTTP_PROXY"
                fi
                export NO_PROXY=''${NO_PROXY:-localhost,127.0.0.1}
              '' + extraHook;
            };

          runtimeArgs = {
            inherit mkShell pkgs pkgsPlaywright;
            packages = {
              inherit
                pinnedCodex
                pinnedArtifactColumnPluginRepository
                pinnedDockerCli
                pinnedGo
                pinnedGovernanceJar
                pinnedMgbaHeadless
                pinnedNpmTools
                pinnedPacker
                pinnedSbtControlPlaneRuntimeJar
                pinnedSubmitToCiJar
                pinnedTerraform
                ;
            };
            toolsets = {
              inherit commonAgentTools ouroborosAgentTools;
            };
          };

          templateAgent = import ./overlays/_template/runtime.nix runtimeArgs;
          codex = import ./overlays/codex/runtime.nix runtimeArgs;
          devAll = import ./overlays/dev-all/runtime.nix runtimeArgs;
          devWorkspace = import ./overlays/dev-workspace/runtime.nix runtimeArgs;
          dumbOnionHax = import ./overlays/dumb-onion-hax/runtime.nix runtimeArgs;
          ouroIntegration = import ./overlays/ouro-integration/runtime.nix runtimeArgs;
          ouroborosStaticFrontEnd = import ./overlays/ouroboros-static-front-end/runtime.nix runtimeArgs;
          pokeemerald = import ./overlays/pokeemerald/runtime.nix runtimeArgs;
        in
        {
          default = devAll;

          _template = templateAgent;
          template-agent = templateAgent;

          ouroboros-dev-agent = codex;

          dev-all = devAll;
          dev-workspace = devWorkspace;
          codex = codex;
          dumb-onion-hax = dumbOnionHax;
          ouro-integration = ouroIntegration;
          ouroboros-static-front-end = ouroborosStaticFrontEnd;
          pokeemerald = pokeemerald;

          runtime-test-agent = mkShell "runtime-test-agent" (with pkgs; [
            bashInteractive
            cacert
            curl
            git
            openssh
          ]) "";

          tinyproxy = mkShell "tinyproxy" (with pkgs; [
            bashInteractive
            cacert
            curl
            netcat-openbsd
            python3
            tinyproxy
            uv
          ]) "";
        }
      );

      packages = forEachSystem (
        { pkgs, pkgsPlaywright, ... }:
        let
          runtimeBundle = mkDevAllRuntimeBundle pkgs;
          runtimeTools = mkDevAllRuntimeTools {
            inherit pkgs pkgsPlaywright;
          };
        in
        {
          devctl = mkProductionDevctl pkgs;
          dev-all-runtime-bundle = runtimeBundle;
          dev-all-runtime-tools = runtimeTools;
          dev-all-runtime-shell = mkDevAllRuntimeShell {
            bundle = runtimeBundle;
            inherit pkgs runtimeTools;
          };
          management-inspection = mkManagementInspectionApp pkgs;
          pinned-artifact-column-plugin-repository = mkPinnedArtifactColumnPluginRepository pkgs;
          pinned-governance-jar = mkPinnedGovernanceJar pkgs;
          pinned-sbt-control-plane-runtime-jar = mkPinnedSbtControlPlaneRuntimeJar pkgs;
          pinned-submit-to-ci-jar = mkPinnedSubmitToCiJar pkgs;

          postgres-broker = pkgs.buildGoModule {
            pname = "devkit-postgres-broker";
            version = "dev";
            src = ./brokers/postgres-broker;
            vendorHash = "sha256-0HDZ3llIgLMxRLNei93XrcYliBzjajU6ZPllo3/IZVY=";
            env.CGO_ENABLED = "0";
            ldflags = [ "-s" "-w" ];
          };

          default = self.packages.${pkgs.system}.postgres-broker;
        }
      );

      apps = forEachSystem (
        { pkgs, ... }:
        {
          management-inspection = {
            type = "app";
            program = "${mkManagementInspectionApp pkgs}/bin/management-inspection";
            meta.description = "Explicitly refreshed, revision-identified, read-only Management source inspection profile";
          };
        }
      );

      checks = forEachSystem (
        { pkgs, pkgsPlaywright, ... }:
        let
          runtimeBundle = mkDevAllRuntimeBundle pkgs;
          runtimeTools = mkDevAllRuntimeTools {
            inherit pkgs pkgsPlaywright;
          };
        in
        {
          dev-all-runtime-bundle = mkDevAllRuntimeBundle pkgs;
          dev-all-runtime-tools = runtimeTools;
          dev-all-runtime-shell = mkDevAllRuntimeShell {
            bundle = runtimeBundle;
            inherit pkgs runtimeTools;
          };
          dev-all-runtime-bundle-bridge-smoke = mkDevAllRuntimeBundleBridgeSmoke pkgs;
          dev-all-runtime-bundle-profile-smoke = mkDevAllRuntimeBundleProfileSmoke pkgs;
          management-inspection-cli = mkProductionDevctl pkgs;
          native-bootstrap-stdio-cleanup = mkNativeBootstrapStdioCleanupCheck pkgs;
          product-fresh-consumer-ssh-authority = mkProductFreshConsumerSSHAuthorityCheck pkgs;

          devctl-openssh-executable-authority =
            let
              devctl = mkProductionDevctl pkgs;
              closure = pkgs.closureInfo {
                rootPaths = [ devctl ];
              };
            in
            pkgs.runCommand "devkit-devctl-openssh-executable-authority" {
              nativeBuildInputs = [ pkgs.gnugrep ];
            } ''
              grep -aqF '${pkgs.openssh}/bin/ssh' ${devctl}/kit/bin/devctl
              grep -qFx '${pkgs.openssh}' ${closure}/store-paths
              mkdir -p "$out"
              printf '%s\n' '${pkgs.openssh}/bin/ssh' > "$out/ssh-executable"
              cp ${closure}/store-paths "$out/store-paths"
            '';

          devctl-overlay-runtime-authority-layout =
            let
              devctl = mkProductionDevctl pkgs;
            in
            pkgs.runCommand "devkit-devctl-overlay-runtime-authority-layout" {
              nativeBuildInputs = [ pkgs.gnugrep ];
            } ''
              test -x ${devctl}/kit/bin/devctl
              test -f ${devctl}/flake.nix
              test -f ${devctl}/flake.lock
              test -f ${devctl}/nix/dev-all-runtime-bundle.nix
              test -f ${devctl}/overlays/dev-all/flake.nix
              test -f ${devctl}/overlays/dev-all/runtime.nix
              grep -F 'inputs.devkit.url = "path:../..";' ${devctl}/overlays/dev-all/flake.nix
              mkdir -p "$out"
              printf '%s\n' ${devctl} > "$out/devctl-runtime-authority-path"
            '';

          runtime-shell-inventory = pkgs.runCommand "devkit-runtime-shell-inventory" {
            nativeBuildInputs = [ runtimeTools ];
          } ''
            env -i PATH=/nonexistent \
              ${runtimeTools}/bin/dev-all-runtime-tools \
              ${pkgs.bash}/bin/bash -c '
                set -eu
                for tool in \
                  bash git ssh curl docker go java sbt node npm purs spago \
                  vite netlify playwright deno aws make gcc mgba-headless \
                  timeout base64
                do
                  command -v "$tool" >/dev/null || {
                    echo "immutable runtime is missing declared tool: $tool" >&2
                    exit 127
                  }
                done
                node_path="$(command -v node)"
                case "$node_path" in
                  /nix/store/*-nodejs-22.*/bin/node) ;;
                  *)
                    echo "runtime tools selected non-pinned Node: $node_path" >&2
                    exit 1
                    ;;
                esac
                node --version >/dev/null
                test -x "$(command -v playwright)"
                test -n "$NODE_PATH"
              '
            mkdir -p "$out"
            printf '%s\n' \
              'immutable dev-all runtime selects pinned Node and Playwright' \
              > "$out/README"
          '';

          overlay-runtime-metadata = pkgs.runCommand "devkit-overlay-runtime-metadata" {
            nativeBuildInputs = [ pkgs.gnugrep pkgs.python3 ];
          } ''
            mkdir -p "$out"
            python3 ${./nix/validate-overlay-runtimes.py} ${./overlays} > "$out/overlay-runtimes.json"

            empty="$TMPDIR/empty-delegation/example"
            mkdir -p "$empty"
            cat > "$empty/devkit.yaml" <<'EOF'
            runtime:
              flake: ./overlays/example#default
              flake_input_overrides:
              codex_version: 0.144.0
              core_check: true
            EOF
            cat > "$empty/flake.nix" <<'EOF'
            { outputs = _: { }; }
            EOF
            if python3 ${./nix/validate-overlay-runtimes.py} "$TMPDIR/empty-delegation" > /dev/null 2> "$TMPDIR/empty.err"; then
              echo "empty flake_input_overrides unexpectedly bypassed runtime.nix requirement" >&2
              exit 1
            fi
            grep -Fx 'example: missing per-overlay runtime.nix' "$TMPDIR/empty.err" >/dev/null
            cp "$TMPDIR/empty.err" "$out/empty-delegation-sabotage.err"
          '';

          retired-runtime-static = pkgs.runCommand "devkit-retired-runtime-static" {
            nativeBuildInputs = [
              pkgs.bash
              pkgs.ripgrep
            ];
          } ''
            mkdir -p "$out"
            bash ${./kit/scripts/retired-runtime-guard} ${./.} > "$out/guard.log"
          '';

          overlay-nix-runtime-static = pkgs.runCommand "devkit-overlay-nix-runtime-static" {
            nativeBuildInputs = [
              pkgs.bash
              pkgs.findutils
              pkgs.gnugrep
              pkgs.ripgrep
            ];
          } ''
            mkdir -p "$out"
            bash ${./kit/scripts/nix-overlay-runtime-guard} ${./.} > "$out/guard.log"
          '';
        }
      );
    };
}

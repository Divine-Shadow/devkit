{
  description = "Devkit Nix-native agent runtime shells and migration checks";

  inputs = {
    # Match the authoritative WSL/Nix source so the disposable lifecycle gate
    # and deployed system use one Nixpkgs implementation and test driver.
    nixpkgs.url = "github:NixOS/nixpkgs/6201e203d09599479a3b3450ed24fa81537ebc4e";
    # Keep Playwright browser revisions compatible with the current
    # ouroboros-ide/frontend lockfile.
    nixpkgs-playwright.url = "github:NixOS/nixpkgs/f86a612cb49b3ca434c9b87f2049797656a0138d";
  };

  outputs =
    {
      self,
      nixpkgs,
      nixpkgs-playwright,
      ...
    }:
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
      productConsumerModule = import ./nix/product-consumer.nix;
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
      productMountPolicy = {
        schemaVersion = "devkit/workspace-egress-policy/v1";
        identity = "devkit/workspace-egress/v3";
      };
      mkProductMountPolicyContract =
        pkgs: pkgs.writeText "devkit-product-mount-policy.json" (builtins.toJSON productMountPolicy);
      productSSHSessionContract = {
        schemaVersion = "devkit/product-ssh-session/v1";
        prepareToken = "devkit-product-prepare/v1";
        approvedCodexConfigArgv = [
          "-c"
          "features.code_mode_host=true"
        ];
        forceCommandArgs = [
          "force-command"
          "--count"
          "$count"
          "--index"
          "$index"
        ];
        supervisorServiceArgs = [
          "serve"
          "--count"
          "$count"
          "--index"
          "$index"
        ];
        socketMode = "0600";
	    accountShellArgv = [ "-c" "{forceCommand}" ];
	    interactive = false;
	    tty = false;
      };
      mkProductSSHSessionContract =
        pkgs: pkgs.writeText "devkit-product-ssh-session.json" (builtins.toJSON productSSHSessionContract);
	  productStoppedVolumeSeedContract = {
	    schemaVersion = "devkit/product-stopped-volume-seed/v1";
	    sourceIdentityEnvironment = "DEVKIT_SOURCE_TRANSPORT_IDENTITY";
	    argv = [
	      "seed-git"
	      "--count"
	      "{count}"
	      "--index"
	      "{index}"
	      "--root-projection"
	      "{relativeProjection}"
	    ];
	    relativeProjection = {
	      schemaVersion = "devkit/product-relative-root-projection/v1";
	      kind = "single-safe-path-component-under-private-cwd";
	      absoluteHostPathsAccepted = false;
	      rewritesGuestHandles = false;
	    };
	    codexAuthSeed = {
	      schemaVersion = "devkit/product-codex-auth-seed/v1";
	      failureSchemaVersion = "devkit/product-codex-auth-seed-failure/v1";
	      input = "stdin";
	      argumentsAccepted = false;
	      target = "manifest-consumer.codexAuthPath";
	      mode = "0600";
	      createOnly = true;
	      anonymousGeneration = "linux-o-tmpfile";
	      failureOutcomes = [
	        "attempted"
	        "effect"
	        "ambiguous"
	      ];
	      failedEffectRequiresFreshConsumer = true;
	      slots = {
	        "1" = "product-codex-auth-seed-1";
	        "2" = "product-codex-auth-seed-2";
	      };
	    };
	    privateKeyMaterialInOutput = false;
	    codexAuthMaterialInOutput = false;
	  };
	  mkProductStoppedVolumeSeedContract =
	    pkgs: pkgs.writeText "devkit-product-stopped-volume-seed.json" (
	      builtins.toJSON productStoppedVolumeSeedContract
	    );
      codexVersion = "0.144.0";
      codexReleaseTag = "rust-v${codexVersion}";
      mkPinnedCodex =
        pkgs:
        let
          details = systemDetails.${pkgs.system};
        in
        pkgs.stdenvNoCC.mkDerivation {
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
      githubSSHKnownHosts = ./nix/github-ssh-known-hosts;
      mkGitHubSSHKnownHosts =
        pkgs: pkgs.writeText "devkit-github-ssh-known-hosts" (builtins.readFile githubSSHKnownHosts);
      mkDevAllRuntimeBundle = import ./nix/mk-dev-all-runtime-bundle.nix;
      mkFleetSourceLayer = import ./nix/fleet-source-layer.nix;
      mkProductRuntimeProjection = import ./nix/product-runtime-projection.nix;
      mkDiagnosticRuntimeFixture =
        pkgs:
        import ./nix/dev-all-runtime-bundle-fixture.nix {
          inherit pkgs;
          productSourceRev = "1111111111111111111111111111111111111111";
        };
      # Devkit's own package/check surfaces are source-free diagnostics. Fleet
      # consumers compose production artifacts through lib.mkDevAllRuntimeBundle.
      mkDiagnosticRuntimeBundle =
        pkgs:
        mkDevAllRuntimeBundle (
          {
            inherit pkgs;
          }
          // (mkDiagnosticRuntimeFixture pkgs).constructorArgs
        );
      mkDevAllRuntimeBundleBridgeSmoke =
        pkgs:
        import ./nix/dev-all-runtime-bundle-bridge-smoke.nix {
          bundle = mkDiagnosticRuntimeBundle pkgs;
          inherit pkgs;
        };
      mkDevAllRuntimeBundleProfileSmoke =
        pkgs:
        import ./nix/dev-all-runtime-bundle-profile-smoke.nix {
          bundle = mkDiagnosticRuntimeBundle pkgs;
          inherit pkgs;
        };
      mkDevAllRuntimeBundleConstructorContract =
        pkgs:
        import ./nix/dev-all-runtime-bundle-constructor-contract.nix {
          inherit
            mkDevAllRuntimeBundle
            pkgs
            ;
        };
      mkPackageEnv = pkgs: "${pkgs.coreutils}/bin/coreutils";
      mkDevctl =
        {
          pkgs,
          sshExecutable,
          knownHostsFile,
          tags ? [ ],
        }:
        let
          envExecutable = mkPackageEnv pkgs;
        in
        pkgs.buildGoModule {
          pname = "devkit-devctl";
          version = "dev";
          src = ./.;
          modRoot = "cli/devctl";
          vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
          subPackages = [ "." ];
          inherit tags;
          env.CGO_ENABLED = "0";
          preCheck = ''
            go test ${pkgs.lib.optionalString (tags != [ ]) "-tags=${builtins.concatStringsSep "," tags}"} ./internal/productadapter
          '';
          ldflags = [
            "-s"
            "-w"
            "-X=devkit/cli/devctl/internal/sshauthority.packageExecutable=${sshExecutable}"
            "-X=devkit/cli/devctl/internal/sshauthority.packageKnownHosts=${knownHostsFile}"
            "-X=devkit/cli/devctl/internal/worktrees.packageGitExecutable=${pkgs.git}/bin/git"
            "-X=devkit/cli/devctl/internal/worktrees.packageEnvExecutable=${envExecutable}"
            "-X=devkit/cli/devctl/internal/runtime/launch.packageGitExecutable=${pkgs.git}/bin/git"
            "-X=devkit/cli/devctl/internal/runtime/launch.packageSourceAcquisitionBubblewrap=${pkgs.bubblewrap}/bin/bwrap"
          ];
          postInstall = ''
            mkdir -p "$out/kit/bin"
            mv "$out/bin/"* "$out/kit/bin/"
            rmdir "$out/bin"
            cp "$src/flake.nix" "$src/flake.lock" "$out/"
            cp -R "$src/nix" "$src/overlays" "$out/"
          '';
          passthru.productAdapterResources = {
            env = envExecutable;
            git = "${pkgs.git}/bin/git";
            inherit knownHostsFile sshExecutable;
          };
        };
      mkProductionDevctl =
        pkgs:
        mkDevctl {
          inherit pkgs;
          sshExecutable = "${pkgs.openssh}/bin/ssh";
          knownHostsFile = mkGitHubSSHKnownHosts pkgs;
        };
      mkSourceTransportPackage =
        {
          pkgs,
          sshExecutable ? "${pkgs.openssh}/bin/ssh",
          shellExecutable ? "${pkgs.bash}/bin/bash",
          knownHostsFile ? mkGitHubSSHKnownHosts pkgs,
          directNetwork ? false,
        }:
        let
          outputPlaceholder = placeholder "out";
          sourceAllowlist = pkgs.writeText "devkit-source-transport-allowlist" ''
            ssh.github.com
          '';
          upstreamProxyURL = if directNetwork then "" else "http://127.0.0.1:18888";
          networkContractFields = {
            schemaVersion = "devkit/source-transport-network/v2";
            mode = if directNetwork then "package-owned-direct-connect" else "managed-loopback-connect";
            allowlistPath = sourceAllowlist;
            connectTarget = "ssh.github.com:443";
            managedConnectProxy = upstreamProxyURL;
            directFallback = false;
          };
          networkContract = pkgs.writeText "devkit-source-transport-network.json" (
            builtins.toJSON networkContractFields
          );
          sourceTransport = pkgs.buildGoModule {
            pname = "devkit-source-transport";
            version = "dev";
            src = ./cli/devctl;
            modRoot = ".";
            vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
            subPackages = [
              "cmd/source-transport"
              "cmd/source-transport-git-ssh"
            ];
            env.CGO_ENABLED = "0";
            ldflags = [
              "-s"
              "-w"
              "-X=devkit/cli/devctl/internal/sourcetransport.packageOpenSSHExecutable=${outputPlaceholder}/libexec/devkit-source-transport/ssh"
              "-X=devkit/cli/devctl/internal/sourcetransport.packageShellExecutable=${outputPlaceholder}/libexec/devkit-source-transport/bash"
              "-X=devkit/cli/devctl/internal/sourcetransport.packageSSHConfig=${outputPlaceholder}/share/devkit-source-transport/ssh-config"
              "-X=devkit/cli/devctl/internal/sourcetransport.packageTransport=${outputPlaceholder}/bin/devkit-source-transport"
              "-X=main.packageUpstreamProxyURL=${upstreamProxyURL}"
            ];
            nativeCheckInputs = [ pkgs.git ];
            doCheck = true;
            checkPhase = ''
              runHook preCheck
              go test ./cmd/source-transport ./internal/runtime/egressproxy \
                -run 'Test(Run|ConnectTarget|ConnectUsesExactUnixSocketAndPreservesImmediateTunnelBytes|ConnectNeverTouchesHostileFixedLoopbackBridge|ConnectFailsClosedOnProxyRejection|ConnectFailsClosedWhenExactUnixSocketIsMissing|ConnectCancellationClosesTunnelWithoutWaitingForOpenInput|ServeRefusesExistingSocketAuthority|RemoveOwnedSocketNeverRemovesReplacement|ServeDrainsFullPackAfterClientHalfClose|ServeAndConnectCarryCompleteGitSmartProtocolFetch)' \
                -count=1
              go test ./internal/sourcetransport -run '^Test(ValidateGitSSHArgs|OpenSSHEnvironmentBindsOnlyPackageShellAndGitProtocol)$' -count=1
              runHook postCheck
            '';
            postInstall = ''
              mv "$out/bin/source-transport" \
                "$out/bin/devkit-source-transport"
              mv "$out/bin/source-transport-git-ssh" \
                "$out/bin/devkit-source-git-ssh"
              mkdir -p \
                "$out/libexec/devkit-source-transport" \
                "$out/share/devkit-source-transport"
              ln -s '${sshExecutable}' \
                "$out/libexec/devkit-source-transport/ssh"
              ln -s '${shellExecutable}' \
                "$out/libexec/devkit-source-transport/bash"
              cp '${knownHostsFile}' \
                "$out/share/devkit-source-transport/github-ssh-known-hosts"
              substitute '${./nix/source-transport-ssh-config}' \
                "$out/share/devkit-source-transport/ssh-config" \
                --replace-fail '@knownHostsPath@' \
                  "$out/share/devkit-source-transport/github-ssh-known-hosts"
            '';
            passthru.sourceTransport = {
              schemaVersion = "devkit/source-transport/v4";
              executablePath = "${sourceTransport}/bin/devkit-source-transport";
              openSSHExecutablePath = "${sourceTransport}/libexec/devkit-source-transport/ssh";
              knownHostsPath = "${sourceTransport}/share/devkit-source-transport/github-ssh-known-hosts";
              gitSSH = {
                schemaVersion = "devkit/source-transport-git-ssh/v2";
                executablePath = "${sourceTransport}/bin/devkit-source-git-ssh";
                configPath = "${sourceTransport}/share/devkit-source-transport/ssh-config";
                proxyShellExecutablePath = "${sourceTransport}/libexec/devkit-source-transport/bash";
                identityEnvironment = "DEVKIT_SOURCE_TRANSPORT_IDENTITY";
                socketEnvironment = "DEVKIT_SOURCE_TRANSPORT_SOCKET";
              };
              network = networkContractFields // {
                contractPath = networkContract;
              };
            };
          };
        in
        sourceTransport;
      mkSourceTransportInterfaceCheck =
        pkgs:
        let
          sourceTransport = mkSourceTransportPackage { inherit pkgs; };
          interface = sourceTransport.sourceTransport;
        in
        assert interface.schemaVersion == "devkit/source-transport/v4";
        assert interface.gitSSH.schemaVersion == "devkit/source-transport-git-ssh/v2";
        assert interface.network.schemaVersion == "devkit/source-transport-network/v2";
        assert interface.network.directFallback == false;
        pkgs.runCommand "devkit-source-transport-interface"
          {
            nativeBuildInputs = [
              pkgs.coreutils
              pkgs.gnugrep
            ];
          }
          ''
            set -eu
            test -x '${interface.executablePath}'
            test -x '${interface.openSSHExecutablePath}'
            test -f '${interface.knownHostsPath}'
            test -x '${interface.gitSSH.executablePath}'
            test -f '${interface.gitSSH.configPath}'
            test -x '${interface.gitSSH.proxyShellExecutablePath}'
            test -f '${interface.network.contractPath}'
            test '${interface.network.mode}' = 'managed-loopback-connect'
            test '${interface.network.connectTarget}' = 'ssh.github.com:443'
            test '${interface.network.managedConnectProxy}' = \
              'http://127.0.0.1:18888'
            test "$(grep -Ev '^[[:space:]]*(#|$)' '${interface.network.allowlistPath}')" = \
              'ssh.github.com'
            test "$(readlink '${interface.openSSHExecutablePath}')" = \
              '${pkgs.openssh}/bin/ssh'
            test "$(readlink '${interface.gitSSH.proxyShellExecutablePath}')" = \
              '${pkgs.bash}/bin/bash'
            grep -qF '[ssh.github.com]:443 ' \
              '${interface.knownHostsPath}'

            if env -i PATH=/hostile \
              '${interface.executablePath}' \
              > "$TMPDIR/refusal.out" 2>&1
            then
              echo "source transport accepted an absent command" >&2
              exit 1
            fi
            grep -qF 'usage: devkit-source-transport serve' \
              "$TMPDIR/refusal.out"

            if grep -aE \
              'wsl-nix-dev-all-runtime-authority|controller-runtime-authority|product-adapter|sourceRevision' \
              '${interface.executablePath}'
            then
              echo "source transport contains a forbidden runtime/source authority" >&2
              exit 1
            fi

            mkdir -p "$out"
            {
              printf '%s\n' \
                'schemaVersion=${interface.schemaVersion}' \
                'executablePath=${interface.executablePath}' \
                'openSSHExecutablePath=${interface.openSSHExecutablePath}' \
                'knownHostsPath=${interface.knownHostsPath}' \
                'gitSSHExecutablePath=${interface.gitSSH.executablePath}' \
                'gitSSHConfigPath=${interface.gitSSH.configPath}' \
                'gitSSHProxyShellExecutablePath=${interface.gitSSH.proxyShellExecutablePath}' \
                'gitSSHIdentityEnvironment=${interface.gitSSH.identityEnvironment}' \
                'gitSSHSocketEnvironment=${interface.gitSSH.socketEnvironment}' \
                'networkContractPath=${interface.network.contractPath}' \
                'networkMode=${interface.network.mode}' \
                'networkAllowlistPath=${interface.network.allowlistPath}' \
                'networkManagedConnectProxy=${interface.network.managedConnectProxy}' \
                'networkDirectFallback=false'
              printf '%s\n' \
                'diagnostic=hostile PATH refusal and transport-only binary passed'
            } > "$out/evidence.txt"
          '';
      mkSourceTransportGitSSHCheck =
        pkgs:
        import ./nix/source-transport-git-ssh-check.nix {
          inherit mkSourceTransportPackage pkgs;
        };
      mkProductSourceAcquirer =
        args:
        import ./nix/product-source-acquirer.nix (
          args
          // {
            inherit mkSourceTransportPackage;
          }
        );
      mkProductSourceAcquirerConstructionCheck =
        pkgs:
        let
          package = mkProductSourceAcquirer {
            inherit pkgs;
            productOrigin = "git@github.com:Divine-Shadow/ouroboros-ide.git";
            productRevision = "0123456789abcdef0123456789abcdef01234567";
            lifecycleRoot = "/var/lib/devkit-product-lifecycle";
            identityPath = "/run/credentials/devkit-product-git-identity";
          };
          interface = package.productSourceAcquisition;
          closure = pkgs.closureInfo { rootPaths = [ package ]; };
        in
        pkgs.runCommand "devkit-product-source-acquirer-construction-prerequisite"
          {
            nativeBuildInputs = [
              pkgs.jq
              pkgs.gnugrep
            ];
          }
          ''
            set -eu
            test -x '${interface.executablePath}'
            test -r '${interface.manifestPath}'
            test -r '${interface.manifestSha256Path}'
            test "$(sha256sum '${interface.manifestPath}' | cut -d' ' -f1)" = \
              "$(cat '${interface.manifestSha256Path}')"
            jq -e \
              --arg package '${interface.packagePath}' \
              --arg executable '${interface.executablePath}' \
              --arg root '${interface.lifecycleRoot}' \
              --arg checkout '${interface.checkoutPath}' \
              --arg receipt '${interface.receiptPath}' \
              '
                .schemaVersion == "devkit/product-source-acquisition-manifest/v1" and
                .packagePath == $package and
                .executablePath == $executable and
                .product.lifecycleRoot == $root and
                .product.checkoutPath == $checkout and
                .product.receiptPath == $receipt and
                .transport.schemaVersion == "devkit/source-transport/v4" and
                .transport.networkMode == "package-owned-direct-connect" and
                .transport.managedConnectProxy == "" and
                (.runtime.gitExecutablePath | startswith("/nix/store/")) and
                (.runtime.openSSHExecutablePath | startswith("/nix/store/"))
              ' '${interface.manifestPath}' >/dev/null
            grep -qFx '${pkgs.git}' '${closure}/store-paths'
            grep -qFx '${pkgs.openssh}' '${closure}/store-paths'
            grep -qFx '${package}' '${closure}/store-paths'
            if grep -E 'ouroboros-ide|product-runtime|governance-control-plane' '${closure}/store-paths'; then
              echo 'source acquirer closure unexpectedly contains a downstream Product authority' >&2
              exit 1
            fi
            mkdir -p "$out"
            cp '${interface.manifestPath}' "$out/manifest.json"
            cp '${closure}/store-paths' "$out/store-paths"
          '';
      mkProductAdapterPackage =
        {
          pkgs,
          bubblewrap,
          broker,
          egressAllowlist,
          codexConfig,
          governanceEnv,
          governanceRepoConfig,
          governanceRules,
          shellHook,
          codexExecutable,
          mcpRequirement,
          sshExecutable ? "${pkgs.openssh}/bin/ssh",
	      sshKeygenExecutable ? "${pkgs.openssh}/bin/ssh-keygen",
          knownHostsFile ? mkGitHubSSHKnownHosts pkgs,
          gitSSHExecutable ?
            (mkSourceTransportPackage {
              inherit
                knownHostsFile
                pkgs
                sshExecutable
                ;
            }).sourceTransport.gitSSH.executablePath,
          rootlessWrapperCheck ? false,
        }:
        let
          envExecutable = mkPackageEnv pkgs;
          outputPlaceholder = placeholder "out";
          runtimeLauncher = "${outputPlaceholder}/bin/product-runtime-exec";
          namespaceWrapperDir = "/run/wrappers/bin";
          namespaceWrappers = {
            adapter = {
              name = "devkit-product-adapter";
              target = "product-adapter";
            };
            proxy = {
              name = "devkit-product-proxy";
              target = "product-proxy";
            };
            supervisor = {
              name = "devkit-product-adapter-supervisor";
              target = "product-adapter-supervisor";
            };
            sshSession = {
              name = "devkit-product-ssh-session";
              target = "product-ssh-session";
            };
            sshSetup = {
              name = "devkit-product-ssh-setup";
              target = "product-ssh-setup";
              controllerOnly = true;
            };
            codexAuthSeed1 = {
              name = "devkit-product-codex-auth-seed-1";
              target = "product-codex-auth-seed-1";
              controllerOnly = true;
            };
            codexAuthSeed2 = {
              name = "devkit-product-codex-auth-seed-2";
              target = "product-codex-auth-seed-2";
              controllerOnly = true;
            };
          };
          wrapperPath = wrapper: "${namespaceWrapperDir}/${wrapper.name}";
          mountPolicyContract = mkProductMountPolicyContract pkgs;
	      sshSessionContract = mkProductSSHSessionContract pkgs;
	      stoppedVolumeSeedContract = mkProductStoppedVolumeSeedContract pkgs;
        in
        pkgs.buildGoModule {
          pname = "devkit-product-adapter";
          version = "dev";
          src = ./cli/devctl;
          modRoot = ".";
          vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
          subPackages = [
            "cmd/product-adapter"
            "cmd/product-adapter-supervisor"
            "cmd/product-authority-selector-install"
            "cmd/product-proxy"
            "cmd/product-readiness"
            "cmd/product-runtime-exec"
            "cmd/product-ssh-session"
	        "cmd/product-ssh-setup"
            "cmd/product-codex-auth-seed-1"
            "cmd/product-codex-auth-seed-2"
          ];
          env.CGO_ENABLED = "0";
          env.DEVKIT_TEST_PINNED_CODEX = codexExecutable;
          tags = nixpkgs.lib.optional rootlessWrapperCheck "devkitrootlesswrappercheck";
          doCheck = !rootlessWrapperCheck;
          ldflags = [
            "-s"
            "-w"
            "-X=devkit/cli/devctl/internal/worktrees.packageGitExecutable=${pkgs.git}/bin/git"
            "-X=devkit/cli/devctl/internal/worktrees.packageEnvExecutable=${envExecutable}"
            "-X=devkit/cli/devctl/internal/productadapter.packageMode=composed"
            "-X=devkit/cli/devctl/internal/productadapter.packageAdapterExecutable=${outputPlaceholder}/bin/product-adapter"
            "-X=devkit/cli/devctl/internal/productadapter.packageGitExecutable=${pkgs.git}/bin/git"
            "-X=devkit/cli/devctl/internal/productadapter.packageGitSSHExecutable=${gitSSHExecutable}"
            "-X=devkit/cli/devctl/internal/productadapter.packageEnvExecutable=${envExecutable}"
	            "-X=devkit/cli/devctl/internal/productadapter.packageSSHExecutable=${sshExecutable}"
	            "-X=devkit/cli/devctl/internal/productadapter.packageSSHKeygenExecutable=${sshKeygenExecutable}"
            "-X=devkit/cli/devctl/internal/productadapter.packageKnownHosts=${knownHostsFile}"
            "-X=devkit/cli/devctl/internal/productadapter.packageRuntimeLauncher=${runtimeLauncher}"
            "-X=devkit/cli/devctl/internal/productadapter.packageBubblewrapExecutable=${bubblewrap}"
            "-X=devkit/cli/devctl/internal/productadapter.packageBrokerExecutable=${broker}"
            "-X=devkit/cli/devctl/internal/productadapter.packageEgressAllowlist=${egressAllowlist}"
            "-X=devkit/cli/devctl/internal/productadapter.packageCodexConfig=${codexConfig}"
            "-X=devkit/cli/devctl/internal/productadapter.packageGovernanceEnv=${governanceEnv}"
            "-X=devkit/cli/devctl/internal/productadapter.packageGovernanceRepoConfig=${governanceRepoConfig}"
            "-X=devkit/cli/devctl/internal/productadapter.packageGovernanceRules=${governanceRules}"
            "-X=devkit/cli/devctl/internal/productadapter.packageShellHook=${shellHook}"
            "-X=devkit/cli/devctl/internal/productadapter.packageCodexExecutable=${codexExecutable}"
            "-X=devkit/cli/devctl/internal/productadapter.packageReadinessExecutable=${outputPlaceholder}/bin/product-readiness"
	            "-X=devkit/cli/devctl/internal/productadapter.packageMCPRequirement=${mcpRequirement}"
            "-X=devkit/cli/devctl/internal/productadapter.packageMountPolicyContract=${mountPolicyContract}"
	            "-X=devkit/cli/devctl/internal/productadapter.packageProductProxyExecutable=${outputPlaceholder}/bin/product-proxy"
	            "-X=devkit/cli/devctl/internal/productadapter.packageProductSupervisor=${outputPlaceholder}/bin/product-adapter-supervisor"
            "-X=devkit/cli/devctl/internal/productadapter.packageProductSSHSession=${outputPlaceholder}/bin/product-ssh-session"
	            "-X=devkit/cli/devctl/internal/productadapter.packageProductSSHSetup=${outputPlaceholder}/bin/product-ssh-setup"
            "-X=devkit/cli/devctl/internal/productadapter.packageProductCodexAuthSeed1=${outputPlaceholder}/bin/product-codex-auth-seed-1"
            "-X=devkit/cli/devctl/internal/productadapter.packageProductCodexAuthSeed2=${outputPlaceholder}/bin/product-codex-auth-seed-2"
            "-X=devkit/cli/devctl/internal/productadapter.packageAdapterLaunchExecutable=${wrapperPath namespaceWrappers.adapter}"
            "-X=devkit/cli/devctl/internal/productadapter.packageProxyLaunchExecutable=${wrapperPath namespaceWrappers.proxy}"
            "-X=devkit/cli/devctl/internal/productadapter.packageSupervisorLaunchExecutable=${wrapperPath namespaceWrappers.supervisor}"
            "-X=devkit/cli/devctl/internal/productadapter.packageSSHSessionLaunchExecutable=${wrapperPath namespaceWrappers.sshSession}"
            "-X=devkit/cli/devctl/internal/productadapter.packageSSHSetupLaunchExecutable=${wrapperPath namespaceWrappers.sshSetup}"
            "-X=devkit/cli/devctl/internal/productadapter.packageCodexAuthSeed1Launch=${wrapperPath namespaceWrappers.codexAuthSeed1}"
            "-X=devkit/cli/devctl/internal/productadapter.packageCodexAuthSeed2Launch=${wrapperPath namespaceWrappers.codexAuthSeed2}"
	            "-X=devkit/cli/devctl/internal/productadapter.packageProductSSHSetupContract=${stoppedVolumeSeedContract}"
	            "-X=devkit/cli/devctl/internal/productadapter.packageProductSessionContract=${sshSessionContract}"
          ];
          passthru.productAdapterResources = {
            env = envExecutable;
            git = "${pkgs.git}/bin/git";
            gitSSH = gitSSHExecutable;
            authoritySelectorInstaller = "${outputPlaceholder}/bin/product-authority-selector-install";
            inherit
              broker
              bubblewrap
              codexConfig
              codexExecutable
              egressAllowlist
              mcpRequirement
              governanceEnv
              governanceRepoConfig
              governanceRules
              knownHostsFile
              runtimeLauncher
              shellHook
              sshExecutable
	          sshKeygenExecutable
              ;
            mountPolicy = productMountPolicy // {
              contractPath = mountPolicyContract;
            };
            namespaceWrappers =
              builtins.mapAttrs
                (
                  _: wrapper:
                  wrapper
                  // {
                    path = wrapperPath wrapper;
                    setuid = true;
                  }
                )
                namespaceWrappers;
	        sshSession = productSSHSessionContract // {
              executablePath = "${outputPlaceholder}/bin/product-ssh-session";
              supervisorExecutablePath = "${outputPlaceholder}/bin/product-adapter-supervisor";
              contractPath = sshSessionContract;
            };
	        stoppedVolumeSeed = productStoppedVolumeSeedContract // {
	          executablePath = "${outputPlaceholder}/bin/product-ssh-setup";
	          contractPath = stoppedVolumeSeedContract;
	        };
          };
        };
      mkProductMCPFixture =
        pkgs:
        pkgs.buildGoModule {
          pname = "devkit-product-mcp-fixture";
          version = "dev";
          src = ./cli/devctl;
          modRoot = ".";
          vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
          subPackages = [ "cmd/product-mcp-fixture" ];
          env.CGO_ENABLED = "0";
          doCheck = true;
          checkPhase = ''
            runHook preCheck
            go test ./cmd/product-mcp-fixture -count=1
            runHook postCheck
          '';
        };
      mkProductConnectFixture =
        pkgs:
        pkgs.buildGoModule {
          pname = "devkit-product-connect-fixture";
          version = "dev";
          src = ./cli/devctl;
          modRoot = ".";
          vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
          subPackages = [ "cmd/product-connect-fixture" ];
          env.CGO_ENABLED = "0";
          doCheck = true;
          checkPhase = ''
            runHook preCheck
            go test ./cmd/product-connect-fixture -count=1
            runHook postCheck
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
            go test ./internal/runtime/launch -run 'TestPrepareGitBootstrap(RefusesUncomposedProductAuthority|DoesNotFallBackWhenUncomposedProductHelperIsMissing|RejectsMissingIdentity)' -count=1
            go test ./internal/commands/nativecmd -run 'TestWithManagedEgressProxy(EstablishesSocketBeforeBootstrapAndCleansUp|CleansExactSocketWhenCallbackFails)|TestEnsureManagedEgressProxyRefusesArbitraryExistingListener|TestRunCommandPreservingExitProjectsStdoutByteExactly|TestLifecyclePlanOptionsConsumesImmutableRuntimeExecutables|TestReadinessBatchDeadlineReturnsTypedResultsAndTerminatesDescendants|TestRawProductSourceRevisionAuthorityIsUnavailable' -count=1
            go test ./internal/runtime/broker -run 'TestResolveBinaryRequiresImmutableAbsoluteExecutable' -count=1
            go test ./internal/runtime/plan -run 'Test(WorkspaceEgressIsolatedRelativeMetadataUsesNoHostAliases|BuildDevAllWorkspaceEgressProjectsPreparedRuntimeSupportExactly)' -count=1
            go test ./internal/runtime/launch -run 'TestBuildBubblewrap(UsesImmutableRuntimeLauncherWithoutConsumerFlakeEvaluation|RejectsMissingOrUntrustedRuntimeLauncher)' -count=1
            go test ./internal/execx -run 'TestRunManaged(AllowsActiveCommandBeyondIdleWindow|IdleTimeoutTerminatesDescendantGroup|ContextDeadlineTerminatesDescendantGroup|PreservesCommandExitClassification)' -count=1
            go test ./internal/worktrees -run 'TestSetupNative(SSHOriginUsesExplicitBootstrapCommand|SSHOriginRejectsMissingBootstrapCommand|ProductBootstrapRejectsHTTPSFallback|ProductBootstrapRejectsAmbientCheckoutOriginAuthority|ProductBootstrapDoesNotReuseWorktreeAfterFetchFailure|IsolatedOwnedRootsUseRelativeCanonicalMetadata|RejectsStaleCommonRepositoryWithoutOwnershipMarker|FailedFetchCleansPartialOwnedRepository|RejectsRepositoryPathTraversalBeforeBootstrap|RejectsAndPreservesPartialWorktreeBeforeBootstrap)|TestRewriteNativeGitdirRejectsForeignCommondirTraversal' -count=1
            go test -tags devkitintegration ./integration -run '^TestDevAllFreshOpenAndResetRefuseLegacyProductConstruction$' -count=1
            runHook postCheck
          '';
        };
      mkNativeAbsentIndexConstructionCheck =
        pkgs:
        let
          packageEnv = mkPackageEnv pkgs;
          fixtureText = name: text: pkgs.writeText "devkit-product-${name}" text;
          fixtureExecutable = name: text: pkgs.writeShellScript "devkit-product-${name}" text;
          productionAdapter = mkProductAdapterPackage {
            inherit pkgs;
            bubblewrap = "${pkgs.bubblewrap}/bin/bwrap";
            broker = fixtureExecutable "broker" "exit 0";
            egressAllowlist = fixtureText "egress-allowlist" "ssh.github.com\n";
            codexConfig = fixtureText "codex-config" "";
            governanceEnv = fixtureText "governance-env" "";
            governanceRepoConfig = fixtureText "governance-repo-config" "{}\n";
            governanceRules = fixtureText "governance-rules" "";
            shellHook = fixtureText "shell-hook" "";
            codexExecutable = "${mkPinnedCodex pkgs}/bin/codex";
            mcpRequirement = fixtureText "mcp-requirement" (
              builtins.toJSON {
                schemaVersion = "devkit/product-mcp-requirement/v1";
                servers = [
                  {
                    name = "governance";
                    tools = [
                      "get_run_status"
                      "run"
                    ];
                  }
                ];
              }
            );
          };
        in
        pkgs.buildGoModule {
          pname = "devkit-native-absent-index-construction-check";
          version = "dev";
          src = ./cli/devctl;
          modRoot = ".";
          vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
          subPackages = [ "." ];
          env.CGO_ENABLED = "0";
          DEVKIT_TEST_NSS_WRAPPER = "${pkgs.nss_wrapper}/lib/libnss_wrapper.so";
          DEVKIT_TEST_REAL_SLOT_GIT = "${pkgs.git}/bin/git";
          DEVKIT_TEST_REAL_SLOT_ENV = packageEnv;
          DEVKIT_TEST_REAL_SLOT_SSH = "${pkgs.openssh}/bin/ssh";
          DEVKIT_TEST_REAL_SLOT_SSHD = "${pkgs.openssh}/bin/sshd";
          DEVKIT_TEST_REAL_SLOT_SSH_KEYGEN = "${pkgs.openssh}/bin/ssh-keygen";
          DEVKIT_TEST_REAL_SLOT_UPLOAD_PACK = "${pkgs.git}/bin/git-upload-pack";
          DEVKIT_TEST_REAL_SLOT_SHELL = "${pkgs.bash}/bin/bash";
          DEVKIT_TEST_IMMUTABLE_RUNTIME_AUTHORITY = pkgs.writeText "devkit-dispatch-runtime-authority.json" ''
            {
              "schemaVersion": "fleet-runtime-authority/v1",
              "sources": {
                "ouroboros-ide": {
                  "rev": "87f5631f87f0be3a731e3d41aa98f9ac6d7d90d3"
                }
              }
            }
          '';
          nativeCheckInputs = [
            pkgs.coreutils
            pkgs.git
            pkgs.openssh
          ];
          doCheck = true;
          checkPhase = ''
            runHook preCheck
            go test ./internal/productadapter ./internal/worktrees \
              -run '^Test(ParseAcceptsOnlyDedicatedProductGrammar|ParseRejectsAliasesDuplicatesAndAuthorityOptions|InvocationSelectsCanonicalProductAliases|LegacySetupNativeRejectsCanonicalProductIdentityBeforeEffects|SetupNativeSlotPublishesOneCompleteGenerationAndCleansFailedStaging)$' \
              -count=1 -v
            go test ./internal/productruntime ./internal/productseed ./internal/productsession \
              -count=1 -v
            if ! test_output="$(
              go test -tags devkitintegration ./integration \
                -run '^TestDevAllProductMutationDispatchIsDeniedBeforeEffects$' \
                -count=1 -v 2>&1
            )"; then
              printf '%s\n' "$test_output"
              exit 1
            fi
            printf '%s\n' "$test_output"
            grep -q -- '--- PASS: TestDevAllProductMutationDispatchIsDeniedBeforeEffects' \
              <<<"$test_output"
	        grep -aqF '/var/lib/product-runtime/authority-selector.json' \
              ${productionAdapter}/bin/product-adapter
            if grep -aqF 'DEVKIT_TEST_PRODUCT_AUTHORITY_LOCATOR' \
              ${productionAdapter}/bin/product-adapter; then
              echo "production Product adapter contains the integration locator seam" >&2
              exit 1
            fi
            for forbidden in \
              'git+file:///workspaces/dev/ouroboros-ide' \
              'DEVKIT_GOVERNANCE_' \
              '#dev-all-runtime-bundle'
            do
              if grep -aF "$forbidden" ${productionAdapter}/bin/product-adapter \
                ${productionAdapter}/bin/product-proxy; then
                echo "production Product adapter closure contains forbidden authority: $forbidden" >&2
                exit 1
              fi
            done
            runHook postCheck
          '';
          installPhase = ''
            mkdir -p "$out"
            printf '%s\n' \
              'diagnostic-only prerequisite; not lifecycle promotion evidence' \
              'slot-private common repository' \
              'absent selected agent and state boundary' \
              'raw Product dispatch refusal and composition constructor only' \
              'does not claim installed Product lifecycle execution' \
              > "$out/contract"
          '';
        };
      mkProductConsumerBoundaryDiagnostic =
        pkgs:
        import ./nix/product-adapter-lifecycle-check.nix {
          inherit
            mkDevAllRuntimeBundle
            mkPinnedCodex
            mkProductAdapterPackage
            mkProductConnectFixture
            mkProductMCPFixture
            mkProductMountPolicyContract
            mkProductSSHSessionContract
	        mkProductStoppedVolumeSeedContract
            pkgs
            ;
          mkNixosSystem = nixpkgs.lib.nixosSystem;
          inherit productConsumerModule;
        };
      mkDevAllRuntimeTools =
        {
          pkgs,
          pkgsPlaywright,
        }:
        let
          shell = self.devShells.${pkgs.system}.dev-all;
          runtimeInputs = [
            pkgs.bashInteractive
            pkgs.curl
            pkgs.nodejs_22
            pkgs.openssh
          ]
          ++ (shell.nativeBuildInputs or [ ])
          ++ (shell.buildInputs or [ ]);
          packageNamed =
            name:
            let
              matches = builtins.filter (package: (package.pname or "") == name) runtimeInputs;
            in
            if matches == [ ] then throw "dev-all runtime shell is missing ${name}" else builtins.head matches;
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
      lib = {
        inherit
          mkDevAllRuntimeBundle
          mkFleetSourceLayer
          mkPinnedCodex
          mkProductAdapterPackage
          mkProductRuntimeProjection
          mkProductSourceAcquirer
          mkSourceTransportPackage
          ;
      };

      nixosModules = {
        product-consumer = productConsumerModule;
        default = self.nixosModules.product-consumer;
      };

      devShells = forEachSystem (
        {
          system,
          pkgs,
          pkgsPlaywright,
          ...
        }:
        let
          details = systemDetails.${system};
          pinnedCodex = mkPinnedCodex pkgs;

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
            nodejs = pkgs.nodejs_22;
            npmDepsHash = "sha256-0MQeDR6NllOZUcRKMM1lbzNyXwZLZDBuBSH2NmLtmyU=";
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

          diagnosticRuntimeBundle = mkDiagnosticRuntimeBundle pkgs;

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

          ouroborosAgentTools =
            commonAgentTools
            ++ (with pkgs; [
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
            ])
            ++ [
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
              ''
              + extraHook;
            };

          runtimeArgs = {
            inherit mkShell pkgs pkgsPlaywright;
            packages = {
              inherit
                diagnosticRuntimeBundle
                pinnedCodex
                pinnedDockerCli
                pinnedGo
                pinnedMgbaHeadless
                pinnedNpmTools
                pinnedPacker
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
        {
          devctl = mkProductionDevctl pkgs;
          github-ssh-known-hosts = mkGitHubSSHKnownHosts pkgs;
          source-transport = mkSourceTransportPackage { inherit pkgs; };
          management-inspection = mkManagementInspectionApp pkgs;
          postgres-broker = pkgs.buildGoModule {
            pname = "devkit-postgres-broker";
            version = "dev";
            src = ./brokers/postgres-broker;
            vendorHash = "sha256-0HDZ3llIgLMxRLNei93XrcYliBzjajU6ZPllo3/IZVY=";
            env.CGO_ENABLED = "0";
            ldflags = [
              "-s"
              "-w"
            ];
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
          runtimeTools = mkDevAllRuntimeTools {
            inherit pkgs pkgsPlaywright;
          };
          productBoundary = mkProductConsumerBoundaryDiagnostic pkgs;
        in
        {
          source-transport = mkSourceTransportPackage { inherit pkgs; };
          source-transport-interface = mkSourceTransportInterfaceCheck pkgs;
          source-transport-git-ssh-lifecycle = mkSourceTransportGitSSHCheck pkgs;
          product-source-acquirer-construction-prerequisite =
            mkProductSourceAcquirerConstructionCheck pkgs;
          dev-all-runtime-bundle-public-constructor = mkDevAllRuntimeBundleConstructorContract pkgs;
          management-inspection-cli = mkProductionDevctl pkgs;
          native-bootstrap-stdio-cleanup = mkNativeBootstrapStdioCleanupCheck pkgs;
          native-absent-index-construction = mkNativeAbsentIndexConstructionCheck pkgs;
          product-consumer-module-contract = productBoundary.moduleContract;
          product-supervisor-identity-lifecycle = productBoundary.supervisorIdentityHermetic;
          product-codex-auth-seed-entrypoint-hermetic =
            productBoundary.codexAuthSeedEntrypointHermetic;

          devctl-openssh-executable-authority =
            let
              devctl = mkProductionDevctl pkgs;
              knownHostsAuthority = mkGitHubSSHKnownHosts pkgs;
              closure = pkgs.closureInfo {
                rootPaths = [ devctl ];
              };
            in
            pkgs.runCommand "devkit-devctl-openssh-executable-authority"
              {
                src = ./.;
                nativeBuildInputs = [
                  pkgs.gnugrep
                  pkgs.ripgrep
                ];
              }
              ''
                grep -aqF '${pkgs.openssh}/bin/ssh' ${devctl}/kit/bin/devctl
                grep -aqF '${knownHostsAuthority}' ${devctl}/kit/bin/devctl
                grep -qFx '${pkgs.openssh}' ${closure}/store-paths
                grep -qFx '${knownHostsAuthority}' ${closure}/store-paths
                grep -qF '[ssh.github.com]:443 ' '${knownHostsAuthority}'
                ! rg -n 'StrictHostKeyChecking[[:space:]]+accept-new' "$src/cli/devctl"
                ! rg -n 'copyNativeSSHFile\([^,]*known|BuildWriteSteps' "$src/cli/devctl"
                mkdir -p "$out"
                printf '%s\n' '${pkgs.openssh}/bin/ssh' > "$out/ssh-executable"
                cp '${knownHostsAuthority}' "$out/github-ssh-known-hosts"
                cp ${closure}/store-paths "$out/store-paths"
              '';

          devctl-overlay-runtime-authority-layout =
            let
              devctl = mkProductionDevctl pkgs;
            in
            pkgs.runCommand "devkit-devctl-overlay-runtime-authority-layout"
              {
                nativeBuildInputs = [ pkgs.gnugrep ];
              }
              ''
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

          runtime-shell-inventory =
            pkgs.runCommand "devkit-runtime-shell-inventory"
              {
                nativeBuildInputs = [ runtimeTools ];
              }
              ''
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

          overlay-runtime-metadata =
            pkgs.runCommand "devkit-overlay-runtime-metadata"
              {
                nativeBuildInputs = [
                  pkgs.gnugrep
                  pkgs.python3
                ];
              }
              ''
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

          retired-runtime-static =
            pkgs.runCommand "devkit-retired-runtime-static"
              {
                nativeBuildInputs = [
                  pkgs.bash
                  pkgs.ripgrep
                ];
              }
              ''
                mkdir -p "$out"
                bash ${./kit/scripts/retired-runtime-guard} ${./.} > "$out/guard.log"
              '';

          overlay-nix-runtime-static =
            pkgs.runCommand "devkit-overlay-nix-runtime-static"
              {
                nativeBuildInputs = [
                  pkgs.bash
                  pkgs.findutils
                  pkgs.gnugrep
                  pkgs.ripgrep
                ];
              }
              ''
                mkdir -p "$out"
                bash ${./kit/scripts/nix-overlay-runtime-guard} ${./.} > "$out/guard.log"
              '';
        }
      );
    };
}

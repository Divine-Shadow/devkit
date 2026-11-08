{
  description = "Devkit Nix-native agent runtime shells and migration checks";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.05";
    codex-source = {
      url = "github:openai/codex/83086188ae0614bd882818ff1aa23f3194962012";
      flake = false;
    };
    codex-native-tool-witness-source = {
      url = "github:openai/codex/a956835d020762cb2b570053af06f643a11c0ecc";
      flake = false;
    };
    codex-native-tool-witness-rust-overlay = {
      url = "github:oxalica/rust-overlay/e60029353d0c48d216bc4b065168ccd4079c166f";
      flake = false;
    };
    # Keep Playwright browser revisions compatible with the current
    # ouroboros-ide/frontend lockfile.
    nixpkgs-playwright.url = "github:NixOS/nixpkgs/f86a612cb49b3ca434c9b87f2049797656a0138d";
  };

  outputs =
    { self, nixpkgs, nixpkgs-playwright, codex-source, codex-native-tool-witness-source, codex-native-tool-witness-rust-overlay, ... }:
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
          codexHash = "sha256-WCZGxr8FYesN5UD3/IW5EhsAkagmvG6Wgwt/FyISIYI=";
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
          codexHash = "sha256-nY7PLRnT4YSHeYOQLWyF3kiziI3LcsOCqKDJdYRZJ/s=";
          dockerHash = "sha256-5rU3Jac3Y6s/mIxz+HcurtQpdUwaV521/xHyGZD9GBc=";
          goHash = "sha256-qOF3w1TS5KG2ECCso1YuJ+o+j4JH7KMXDj+h4ML553E=";
          terraformHash = "sha256-+FhoeYg0VYI59hSINIhACPJyJUj4QDTJsPYpNLLXPrs=";
          packerHash = "sha256-3SltdD3UWTMEMHWDz/UpC7qbho/CsLYFtkVm+BQcpyg=";
        };
      };
      codexVersion = "0.163.0";
      codexReleaseTag = "rust-v${codexVersion}";
      mkOfficialCodex =
        pkgs:
        pkgs.stdenvNoCC.mkDerivation {
          pname = "codex";
          version = codexReleaseTag;
          src = pkgs.fetchurl {
            url = "https://github.com/openai/codex/releases/download/${codexReleaseTag}/${systemDetails.${pkgs.system}.codexAsset}.tar.gz";
            hash = systemDetails.${pkgs.system}.codexHash;
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
            runHook postInstall
          '';
        };
      githubSSHKnownHosts = ./nix/github-ssh-known-hosts;
      managedGitSSHConfig = ./nix/managed-git-ssh-config;
      mkDevAllRuntimeBundle =
        { pkgs, pkgsPlaywright }:
        import ./nix/dev-all-runtime-bundle.nix {
          inherit pkgs;
          devctl = mkProductionDevctl pkgs;
          runtimeTools = mkDevAllRuntimeTools { inherit pkgs pkgsPlaywright; };
        };
      mkDevctl =
        {
          pkgs,
          sshExecutable,
          knownHostsFile,
          sqliteExecutable,
          currentSourceRegistrationExecutable ? null,
          currentSourceConsumer ? null,
          registeredCurrentWorktreeReadiness ? false,
          tags ? [ ],
        }:
        assert !registeredCurrentWorktreeReadiness || currentSourceRegistrationExecutable != null;
        assert currentSourceConsumer == null || (
          pkgs.lib.isDerivation currentSourceConsumer.devctl
          && currentSourceConsumer.devctl.pname == "devkit-devctl"
          && builtins.isString currentSourceConsumer.targetId
          && builtins.match "[A-Za-z0-9][A-Za-z0-9._-]*" currentSourceConsumer.targetId != null
        );
        pkgs.buildGoModule {
          pname = "devkit-devctl";
          version = "dev";
          src = ./.;
          modRoot = "cli/devctl";
          vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
          subPackages = [ "." ];
          inherit tags;
          env.CGO_ENABLED = "0";
          passthru.withCurrentSourceRegistration = executable: mkDevctl {
            inherit pkgs sshExecutable knownHostsFile sqliteExecutable tags
              currentSourceConsumer registeredCurrentWorktreeReadiness;
            currentSourceRegistrationExecutable = executable;
          };
          passthru.withRegisteredCurrentWorktreeReadiness = executable: mkDevctl {
            inherit pkgs sshExecutable knownHostsFile sqliteExecutable tags
              currentSourceConsumer;
            currentSourceRegistrationExecutable = executable;
            registeredCurrentWorktreeReadiness = true;
          };
          passthru.withCurrentSourceConsumer = consumer: mkDevctl {
            inherit pkgs sshExecutable knownHostsFile sqliteExecutable tags
              currentSourceRegistrationExecutable registeredCurrentWorktreeReadiness;
            currentSourceConsumer = consumer;
          };
          ldflags = [
            "-s"
            "-w"
            "-X=devkit/cli/devctl/internal/sshauthority.packageExecutable=${sshExecutable}"
            "-X=devkit/cli/devctl/internal/sshauthority.packageKnownHosts=${knownHostsFile}"
            "-X=devkit/cli/devctl/internal/sshauthority.packageManagedGitConfig=${managedGitSSHConfig}"
            "-X=devkit/cli/devctl/internal/worktrees.packageEnvExecutable=${pkgs.coreutils}/bin/env"
            "-X=devkit/cli/devctl/internal/gitauthority.packageExecutable=${pkgs.git}/bin/git"
            "-X=devkit/cli/devctl/internal/sqliteauthority.packageExecutable=${sqliteExecutable}"
            "-X=devkit/cli/devctl/internal/commands/nativecmd.nativeHomeMoveExecutable=${pkgs.coreutils}/bin/mv"
            "-X=devkit/cli/devctl/internal/commands/nativecmd.packageRuntimeLauncher=${self.packages.${pkgs.system}.dev-all-runtime-shell}/bin/dev-all-runtime-shell"
            "-X=devkit/cli/devctl/internal/commands/nativecmd.packageBrokerBinary=${self.packages.${pkgs.system}.postgres-broker}/bin/postgres-broker"
            "-X=devkit/cli/devctl/internal/commands/nativecmd.packageBubblewrapBinary=${pkgs.bubblewrap}/bin/bwrap"
          ]
          ++ pkgs.lib.optionals (currentSourceRegistrationExecutable != null) [
            "-X=devkit/cli/devctl/internal/commands/nativecmd.currentSourceRegistrationExecutable=${currentSourceRegistrationExecutable}"
          ]
          ++ pkgs.lib.optionals registeredCurrentWorktreeReadiness [
            "-X=devkit/cli/devctl/internal/commands/nativecmd.registeredCurrentWorktreeReadiness=registered"
          ]
          ++ pkgs.lib.optionals (currentSourceConsumer != null) [
            "-X=devkit/cli/devctl/internal/commands/nativecmd.currentSourceConsumerRuntimeRoot=${currentSourceConsumer.devctl}"
            "-X=devkit/cli/devctl/internal/commands/nativecmd.currentSourceConsumerTargetID=${currentSourceConsumer.targetId}"
          ]



;
          postInstall = ''
            mkdir -p "$out/kit/bin"
            mv "$out/bin/devctl" "$out/kit/bin/devctl"
            rmdir "$out/bin"
            mkdir -p "$out/kit/proxy"
            cp "$src/kit/proxy/allowlist.txt" "$out/kit/proxy/allowlist.txt"
            cp "$src/flake.nix" "$src/flake.lock" "$out/"
            cp -R "$src/nix" "$src/overlays" "$out/"
            mkdir -p "$out/brokers"
            cp -R "$src/brokers/postgres-broker" "$out/brokers/"
          '';
        };
      mkProductionDevctl =
        pkgs:
        mkDevctl {
          inherit pkgs;
          sshExecutable = "${pkgs.openssh}/bin/ssh";
          knownHostsFile = githubSSHKnownHosts;
          sqliteExecutable = "${pkgs.sqlite}/bin/sqlite3";
        };
      mkDevctlGoTests =
        pkgs:
        pkgs.buildGoModule {
          pname = "devkit-devctl-go-tests";
          version = "dev";
          src = ./.;
          modRoot = "cli/devctl";
          vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
          subPackages = [ "." ];
          env.CGO_ENABLED = "0";
          nativeCheckInputs = [
            pkgs.git
            pkgs.openssh
            pkgs.sqlite
            pkgs.util-linux
            pkgs.python3
            pkgs.jq
          ];
          doCheck = true;
          checkPhase = ''
            runHook preCheck
            # This custom check phase replaces buildGoModule's bounded test
            # runner. Keep package/test concurrency within the Nix allocation.
            export GOMAXPROCS="$NIX_BUILD_CORES"
            devkit_check_go_flags="''${GOFLAGS:+$GOFLAGS }-p=$NIX_BUILD_CORES"
            export GOFLAGS="$devkit_check_go_flags"
            printf 'Go check resource settings: NIX_BUILD_CORES=%s GOMAXPROCS=%s GOFLAGS=%s\n' \
              "$NIX_BUILD_CORES" "$GOMAXPROCS" "$GOFLAGS"
            export DEVKIT_BROKER_PACKAGE_TEST_BINARY=${self.packages.${pkgs.system}.postgres-broker}/bin/postgres-broker
            # The integration package and repository-layout contract tests are
            # exercised from the source checkout, where their real host and
            # repository prerequisites exist. Keep this derivation hermetic and
            # cover the complete portable unit-test surface without inventing
            # host fixtures that would prove a different execution path.
            packages="$(go list ./... | grep -v '/integration$' | grep -v '/internal/config$')"
            go test -count=1 $packages
            # Emit concrete owned SSH stage/cleanup evidence with the same
            # complete package source; this adds no live/native consumer probe.
            go test -count=1 -v -run '^TestStandardManagedGit' ./internal/runtime/launch
            # Retain actual v4 preservation/refusal/race/reader case outcomes
            # from the same full source and native SQLite test closure.
            go test -count=1 -v -run '^TestV4' ./internal/codexhistory
            go test -run '^$' ./internal/config
            runHook postCheck
          '';
          installPhase = ''
            mkdir -p "$out"
            printf '%s\n' \
              'Devkit portable Go unit tests passed.' \
              'Excluded from test execution: integration and internal/config packages.' \
              'The internal/config package was compile-checked only.' > "$out/README"
          '';
        };
      mkDevAllRuntimeTools =
        {
          pkgs,
          pkgsPlaywright,
        }:
        let
          # The installed launcher is shared by every admitted native overlay,
          # so its closure must include overlay-specific tools as well as the
          # default dev-all toolset. Keep this list explicit and gate each
          # admitted runtime at the executable boundary below.
          admittedRuntimeShells = [
            self.devShells.${pkgs.system}.dev-all
            self.devShells.${pkgs.system}.pokeemerald
          ];
          runtimeInputs =
            [
              pkgs.bashInteractive
              pkgs.bubblewrap
              pkgs.curl
              pkgs.nodejs_22
            ]
            ++ builtins.concatLists (map
              (shell:
                (shell.nativeBuildInputs or [ ])
                ++ (shell.buildInputs or [ ]))
              admittedRuntimeShells);
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
            exec '${runtimeTools}/bin/dev-all-runtime-tools' "$@"
          '';
        };
    in
    {
      devShells = forEachSystem (
        { system, pkgs, pkgsPlaywright, ... }:
        let
          details = systemDetails.${system};
          pinnedCodex = self.packages.${system}.codex;

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
          dumbOnionHax = import ./overlays/dumb-onion-hax/runtime.nix runtimeArgs;
          ouroIntegration = import ./overlays/ouro-integration/runtime.nix runtimeArgs;
          ouroborosStaticFrontEnd = import ./overlays/ouroboros-static-front-end/runtime.nix runtimeArgs;
          pokeemerald = import ./overlays/pokeemerald/runtime.nix runtimeArgs;
          pokeemeraldSharedPower = import ./overlays/pokeemerald-expansion-shared-power/runtime.nix runtimeArgs;
        in
        {
          default = devAll;

          _template = templateAgent;
          template-agent = templateAgent;

          ouroboros-dev-agent = codex;

          dev-all = devAll;
          codex = codex;
          dumb-onion-hax = dumbOnionHax;
          ouro-integration = ouroIntegration;
          ouroboros-static-front-end = ouroborosStaticFrontEnd;
          pokeemerald = pokeemerald;
          pokeemerald-expansion-shared-power = pokeemeraldSharedPower;

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
          runtimeBundle = mkDevAllRuntimeBundle { inherit pkgs pkgsPlaywright; };
          runtimeTools = mkDevAllRuntimeTools {
            inherit pkgs pkgsPlaywright;
          };
        in
        {
          codex = import ./nix/codex {
            inherit pkgs;
            source = codex-source;
            rustOverlaySource = codex-native-tool-witness-rust-overlay;
            officialCodex = mkOfficialCodex pkgs;
          };
          devctl = mkProductionDevctl pkgs;
          # Reuse the production fixed-output dependency recipe. Bind its
          # vendor payload to the exact module declarations of this source.
          devctl-ci-dependencies = pkgs.runCommand "devkit-devctl-ci-dependencies" { } ''
            mkdir -p "$out"
            cp ${./cli/devctl/go.mod} "$out/go.mod"
            cp ${./cli/devctl/go.sum} "$out/go.sum"
            ln -s ${(mkProductionDevctl pkgs).goModules} "$out/vendor"
          '';
          codex-gui-history-custody = (mkProductionDevctl pkgs).overrideAttrs (_: {
            pname = "devkit-codex-gui-history-custody";
            subPackages = [ "cmd/codex-gui-history-custody" ];
            postInstall = "";
          });
          station-broker-observer = (mkProductionDevctl pkgs).overrideAttrs (_: {
            pname = "devkit-station-broker-observer";
            subPackages = [ "cmd/station-broker-observe" ];
            postInstall = "";
          });
          dev-all-runtime-bundle = runtimeBundle;
          # Source-checkout reset fixtures consume immutable config bytes via
          # a test-owned GUI projection manifest, never the host /etc manifest.
          native-reset-test-config = pkgs.writeText "devkit-native-reset-test-config.toml" ''
            # Immutable Devkit reset test fixture; never launches Codex.
            model_provider = "reset-fixture"
          '';
          # Parser/transport integration tests use the same OpenSSH dependency
          # as Devctl, rather than an unrelated runner-installed version/layout.
          devctl-test-openssh = pkgs.openssh;
          # Full source tests consume the same locked tool suppliers already
          # declared by portable tests.
          devctl-test-tools = pkgs.buildEnv {
            name = "devkit-devctl-test-tools";
            paths = with pkgs; [ openssh sqlite gnutar jq util-linux coreutils ];
            pathsToLink = [ "/bin" ];
          };
          dev-all-runtime-tools = runtimeTools;
          dev-all-runtime-shell = mkDevAllRuntimeShell {
            inherit pkgs runtimeTools;
          };

          postgres-broker = pkgs.buildGoModule {
            pname = "devkit-postgres-broker";
            version = "dev";
            src = ./brokers/postgres-broker;
            vendorHash = "sha256-0HDZ3llIgLMxRLNei93XrcYliBzjajU6ZPllo3/IZVY=";
            env.CGO_ENABLED = "0";
            ldflags = [ "-s" "-w" ];
          };

          default = self.packages.${pkgs.system}.dev-all-runtime-bundle;
        } // nixpkgs.lib.optionalAttrs (pkgs.system == "x86_64-linux") {
          sbt-rc8-diagnostic-actions = (import ./nix/sbt-rc8-diagnostics { inherit pkgs; }).package;
          sbt-rc8-diagnostic-deps-discovery = (import ./nix/sbt-rc8-diagnostics { inherit pkgs; }).deps-discovery;
          codex-native-tool-witness = import ./nix/codex-native-tool-witness {
            inherit pkgs;
            source = codex-native-tool-witness-source;
            rustOverlaySource = codex-native-tool-witness-rust-overlay;
            officialCodex = mkOfficialCodex pkgs;
          };
        }
      );

      checks = forEachSystem (
        { pkgs, pkgsPlaywright, ... }:
        let
          runtimeBundle = mkDevAllRuntimeBundle { inherit pkgs pkgsPlaywright; };
          runtimeTools = mkDevAllRuntimeTools {
            inherit pkgs pkgsPlaywright;
          };
        in
        {
          dev-all-runtime-bundle = runtimeBundle;
          dev-all-runtime-tools = runtimeTools;
          dev-all-runtime-shell = mkDevAllRuntimeShell {
            inherit pkgs runtimeTools;
          };
          devctl-go-tests = mkDevctlGoTests pkgs;
          fixed-slot-history-provider =
            let provider = self.packages.${pkgs.system}.codex-gui-history-custody;
            in pkgs.runCommand "devkit-fixed-slot-history-provider-authority" {
              nativeBuildInputs = [ pkgs.gnugrep ];
            } ''
              if env -i ${provider}/bin/codex-gui-history-custody 1 host > result 2> refusal; then
                echo 'unbound history provider unexpectedly admitted a source' >&2
                exit 1
              fi
              grep -Fx 'cold-history custody has no package-owned fixed-slot bindings' refusal
              grep -aqF '${pkgs.sqlite}/bin/sqlite3' ${provider}/bin/codex-gui-history-custody
              mkdir -p "$out"
              cp refusal "$out/refusal"
            '';

          native-broker-shared-lifetime = import ./nix/checks/native-broker-lifetime.nix {
            inherit pkgs;
            devctl = mkProductionDevctl pkgs;
            broker = self.packages.${pkgs.system}.postgres-broker;
            stationObserver = self.packages.${pkgs.system}.station-broker-observer;
          };
          native-cli-fixed-owner-consequences = import ./nix/checks/native-broker-lifetime.nix {
            inherit pkgs;
            devctl = mkProductionDevctl pkgs;
            broker = self.packages.${pkgs.system}.postgres-broker;
            stationObserver = self.packages.${pkgs.system}.station-broker-observer;
            cliConsequences = true;
            nativeResetConfig = self.packages.${pkgs.system}.native-reset-test-config;
          };
          devctl-openssh-executable-authority =
            let
              devctl = mkProductionDevctl pkgs;
              closure = pkgs.closureInfo {
                rootPaths = [ devctl ];
              };
            in
            pkgs.runCommand "devkit-devctl-openssh-executable-authority" {
              src = ./.;
              nativeBuildInputs = [
                pkgs.gnugrep
                pkgs.ripgrep
              ];
            } ''
              grep -aqF '${pkgs.openssh}/bin/ssh' ${devctl}/kit/bin/devctl
              grep -aqF '${pkgs.coreutils}/bin/env' ${devctl}/kit/bin/devctl
              grep -aqF '${pkgs.git}/bin/git' ${devctl}/kit/bin/devctl
              grep -aqF '${githubSSHKnownHosts}' ${devctl}/kit/bin/devctl
              grep -qFx '${pkgs.openssh}' ${closure}/store-paths
              grep -qFx '${pkgs.coreutils}' ${closure}/store-paths
              grep -qFx '${pkgs.git}' ${closure}/store-paths
              grep -qFx '${githubSSHKnownHosts}' ${closure}/store-paths
              grep -qF '[ssh.github.com]:443 ' '${githubSSHKnownHosts}'
              ! rg -n 'StrictHostKeyChecking[[:space:]]+accept-new' "$src/cli/devctl"
              ! rg -n 'copyNativeSSHFile\([^,]*known|BuildWriteSteps' "$src/cli/devctl"
              mkdir -p "$out"
              printf '%s\n' '${pkgs.openssh}/bin/ssh' > "$out/ssh-executable"
              cp '${githubSSHKnownHosts}' "$out/github-ssh-known-hosts"
              cp ${closure}/store-paths "$out/store-paths"
            '';

          devctl-sqlite-executable-authority =
            let
              devctl = mkProductionDevctl pkgs;
              closure = pkgs.closureInfo {
                rootPaths = [ devctl ];
              };
            in
            pkgs.runCommand "devkit-devctl-sqlite-executable-authority" {
              nativeBuildInputs = [ pkgs.gnugrep ];
            } ''
              grep -aqF '${pkgs.sqlite}/bin/sqlite3' ${devctl}/kit/bin/devctl
              grep -qFx '${pkgs.sqlite}' ${closure}/store-paths
              mkdir -p "$out"
              printf '%s\n' '${pkgs.sqlite}/bin/sqlite3' > "$out/sqlite-executable"
              cp ${closure}/store-paths "$out/store-paths"
            '';

          devctl-overlay-runtime-authority-layout =
            let devctl = mkProductionDevctl pkgs;
            in pkgs.runCommand "devkit-standalone-native-layout" {
              nativeBuildInputs = [ pkgs.jq ];
            } ''
              mkdir -p "$out"
              env -i HOME="$TMPDIR/home" \
                ${devctl}/kit/bin/devctl -p dev-all native plan \
                  --host-root /tmp/standalone-devkit \
                  --worktree-root /tmp/standalone-devkit/worktrees \
                  --state-root /tmp/standalone-devkit/state \
                  --no-containers --repo ouroboros-ide --index 1 --format json > "$out/plan.json"
              jq -e '
                .agent.host_worktree == "/tmp/standalone-devkit/worktrees/agent1/ouroboros-ide" and
                .windows_mounts_visible == false and
                .broker_endpoint == "" and
                (.postgres_docker_socket // "") == "" and
                all(.binds[]; (.source | contains("/etc/fleet") | not) and
                               (.source | contains("ouro8-governance") | not) and
                               (.source | contains("shadow-throne-management") | not) and
                               (.source | startswith("/mnt") | not) and
                               (.target | startswith("/mnt") | not)) and
                all(.env | keys[]; (startswith("FLEET_") | not))' "$out/plan.json"
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
                  arm-none-eabi-gcc arm-none-eabi-as timeout base64
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

          sbt-rc8-diagnostic-dependency-normalization =
            (import ./nix/sbt-rc8-diagnostics { inherit pkgs; }).dependency-normalization-check;

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

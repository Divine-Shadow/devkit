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
      githubSSHKnownHosts = ./nix/github-ssh-known-hosts;
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
            "-X=devkit/cli/devctl/internal/sshauthority.packageKnownHosts=${knownHostsFile}"
            "-X=devkit/cli/devctl/internal/worktrees.packageEnvExecutable=${pkgs.coreutils}/bin/env"
            "-X=devkit/cli/devctl/internal/gitauthority.packageExecutable=${pkgs.git}/bin/git"
          ];
          postInstall = ''
            mkdir -p "$out/kit/bin"
            mv "$out/bin/devctl" "$out/kit/bin/devctl"
            rmdir "$out/bin"
            mkdir -p "$out/kit/proxy"
            cp "$src/kit/proxy/allowlist.txt" "$out/kit/proxy/allowlist.txt"
            cp "$src/flake.nix" "$src/flake.lock" "$out/"
            cp -R "$src/nix" "$src/overlays" "$out/"
          '';
        };
      mkProductionDevctl =
        pkgs:
        mkDevctl {
          inherit pkgs;
          sshExecutable = "${pkgs.openssh}/bin/ssh";
          knownHostsFile = githubSSHKnownHosts;
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
          ];
          doCheck = true;
          checkPhase = ''
            runHook preCheck
            # The integration package and repository-layout contract tests are
            # exercised from the source checkout, where their real host and
            # repository prerequisites exist. Keep this derivation hermetic and
            # cover the complete portable unit-test surface without inventing
            # host fixtures that would prove a different execution path.
            packages="$(go list ./... | grep -v '/integration$' | grep -v '/internal/config$')"
            go test -count=1 $packages
            go test -run '^$' ./internal/config
            runHook postCheck
          '';
          installPhase = ''
            mkdir -p "$out"
            printf '%s\n' 'full Devkit Go suite passed' > "$out/README"
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
          runtimeBundle = mkDevAllRuntimeBundle { inherit pkgs pkgsPlaywright; };
          runtimeTools = mkDevAllRuntimeTools {
            inherit pkgs pkgsPlaywright;
          };
        in
        {
          devctl = mkProductionDevctl pkgs;
          dev-all-runtime-bundle = runtimeBundle;
          dev-all-runtime-tools = runtimeTools;
          dev-all-runtime-shell = mkDevAllRuntimeShell {
            inherit pkgs runtimeTools;
          };
          management-inspection = mkManagementInspectionApp pkgs;

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
          management-inspection-cli = mkProductionDevctl pkgs;
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

          devctl-overlay-runtime-authority-layout =
            let
              devctl = mkProductionDevctl pkgs;
              broker = self.packages.${pkgs.system}.postgres-broker;
              runtimeShell = mkDevAllRuntimeShell {
                inherit pkgs runtimeTools;
              };
            in
            pkgs.runCommand "devkit-devctl-overlay-runtime-authority-layout" {
              nativeBuildInputs = [ pkgs.bubblewrap pkgs.coreutils pkgs.gnugrep pkgs.jq ];
            } ''
              test -x ${devctl}/kit/bin/devctl
              test -f ${devctl}/flake.nix
              test -f ${devctl}/flake.lock
              test -f ${devctl}/nix/dev-all-runtime-bundle.nix
              test -f ${devctl}/overlays/dev-all/flake.nix
              test -f ${devctl}/overlays/dev-all/runtime.nix
              test -f ${devctl}/overlays/dev-all/devkit.yaml
              test -f ${devctl}/overlays/dev-workspace/runtime.nix
              test -f ${devctl}/overlays/dev-workspace/devkit.yaml
              test -f ${devctl}/overlays/ouroboros-terraform/flake.nix
              test -f ${devctl}/overlays/ouroboros-terraform/devkit.yaml
              test -f ${devctl}/overlays/pokeemerald/flake.nix
              test -f ${devctl}/overlays/pokeemerald/runtime.nix
              test -f ${devctl}/overlays/pokeemerald/devkit.yaml
              test -r ${devctl}/kit/proxy/allowlist.txt
              grep -Fx 'ssh.github.com' ${devctl}/kit/proxy/allowlist.txt
              grep -F 'inputs.devkit.url = "path:../..";' ${devctl}/overlays/dev-all/flake.nix
              for overlay in dev-all dev-workspace ouroboros-terraform pokeemerald; do
                grep -Fx '  host_root: /home/bayesartre/dev' ${devctl}/overlays/$overlay/devkit.yaml
                grep -Fx '  worktree_root: /home/bayesartre/dev/agent-worktrees' ${devctl}/overlays/$overlay/devkit.yaml
                grep -Fx '  state_root: /home/bayesartre/dev/.devkit/native-agents' ${devctl}/overlays/$overlay/devkit.yaml
                grep -Fx '  worktree_container_root: /workspaces/dev/agent-worktrees' ${devctl}/overlays/$overlay/devkit.yaml
                grep -Fx '  state_container_root: /agent-state' ${devctl}/overlays/$overlay/devkit.yaml
                grep -Fx '  required_isolation_profile: workspace-egress' ${devctl}/overlays/$overlay/devkit.yaml
              done

              plan="$TMPDIR/installed-plan.json"
              env -i \
                HOME="$TMPDIR/home" \
                DEVKIT_RUNTIME_BROKER_BINARY=${broker}/bin/postgres-broker \
                DEVKIT_RUNTIME_SHELL_LAUNCHER=${runtimeShell}/bin/dev-all-runtime-shell \
                DEVKIT_RUNTIME_BWRAP_BINARY=${pkgs.bubblewrap}/bin/bwrap \
                ${devctl}/kit/bin/devctl -p dev-all native plan \
                  --repo ouroboros-ide --index 1 --format json > "$plan"
              ${pkgs.jq}/bin/jq -e \
                --arg devctl '${devctl}' \
                --arg broker '${broker}/bin/postgres-broker' \
                '.host_worktree_root == "/home/bayesartre/dev/agent-worktrees" and
                 .host_state_root == "/home/bayesartre/dev/.devkit/native-agents" and
                 .sandbox_worktree_root == "/workspaces/dev/agent-worktrees" and
                 .sandbox_state_root == "/agent-state" and
                 .broker_endpoint == "/home/bayesartre/dev/.devkit/native-broker/broker.sock" and
                 .proxy.unix_socket == "/home/bayesartre/dev/.devkit/native-egress/dev-all-agent1-workspace-egress.sock" and
                 .proxy.allowlist_path == ($devctl + "/kit/proxy/allowlist.txt") and
                 .agent.host_worktree == "/home/bayesartre/dev/agent-worktrees/agent1/ouroboros-ide" and
                 .agent.sandbox_worktree == "/workspaces/dev/agent-worktrees/agent1/ouroboros-ide" and
                 .env.DEVKIT_RUNTIME_BROKER_BINARY == $broker and
                 any(.binds[]; .source == "/run/current-system/etc/fleet/product-governance.env" and
                               .target == "/etc/fleet/product-governance.env" and
                               .mode == "ro" and .required == true) and
                 any(.binds[]; .source == "/home/bayesartre/dev/.devkit/ouro8-governance-env.sh") and
                 any(.binds[]; .source == "/home/bayesartre/dev/.devkit/ouro8-governance-repo-env.json") and
                 any(.binds[]; .source == "/home/bayesartre/dev/.devkit/governance-control-plane" and .mode == "rw") and
                 all(.binds[]; (.mode != "rw") or (.source | startswith("/nix/store") | not)) and
                 (tostring | contains("/nix/store/.devkit/") | not)' \
                "$plan"
              grep -F '    - name: product-governance-envelope' \
                ${devctl}/overlays/dev-all/devkit.yaml
              grep -F "        governance_env=/etc/fleet/product-governance.env" \
                ${devctl}/overlays/dev-all/devkit.yaml
              grep -F "        docker version --format '{{.Server.APIVersion}}' >/dev/null" \
                ${devctl}/overlays/dev-all/devkit.yaml
              allowlist="$(${pkgs.jq}/bin/jq -r '.proxy.allowlist_path' "$plan")"
              test "$allowlist" = '${devctl}/kit/proxy/allowlist.txt'
              test -r "$allowlist"
              grep -Fx 'ssh.github.com' "$allowlist"

              terraform_plan="$TMPDIR/installed-terraform-plan.json"
              env -i \
                HOME="$TMPDIR/home" \
                DEVKIT_RUNTIME_BROKER_BINARY=${broker}/bin/postgres-broker \
                DEVKIT_RUNTIME_SHELL_LAUNCHER=${runtimeShell}/bin/dev-all-runtime-shell \
                DEVKIT_RUNTIME_BWRAP_BINARY=${pkgs.bubblewrap}/bin/bwrap \
                ${devctl}/kit/bin/devctl -p ouroboros-terraform native plan \
                  --repo ouroboros-terraform --index 1 --format json > "$terraform_plan"
              ${pkgs.jq}/bin/jq -e \
                '.agent.host_worktree == "/home/bayesartre/dev/agent-worktrees/agent1/ouroboros-terraform" and
                 .agent.sandbox_worktree == "/workspaces/dev/agent-worktrees/agent1/ouroboros-terraform" and
                 .flake_input_overrides["ouroboros-terraform"] == "path:/workspaces/dev/agent-worktrees/agent1/ouroboros-terraform"' \
                "$terraform_plan"
              grep -Fx \
                '  origin: ssh://git@ssh.github.com:443/Divine-Shadow/ouroboros-terraform.git' \
                ${devctl}/overlays/ouroboros-terraform/devkit.yaml
              grep -Fx \
                "      command: codex --version | grep -q '0.144.0'" \
                ${devctl}/overlays/ouroboros-terraform/devkit.yaml

              pokeemerald_plan="$TMPDIR/installed-pokeemerald-plan.json"
              env -i \
                HOME="$TMPDIR/home" \
                DEVKIT_RUNTIME_BROKER_BINARY=${broker}/bin/postgres-broker \
                DEVKIT_RUNTIME_SHELL_LAUNCHER=${runtimeShell}/bin/dev-all-runtime-shell \
                DEVKIT_RUNTIME_BWRAP_BINARY=${pkgs.bubblewrap}/bin/bwrap \
                ${devctl}/kit/bin/devctl -p pokeemerald native plan \
                  --repo pokeemerald --index 1 --format json > "$pokeemerald_plan"
              ${pkgs.jq}/bin/jq -e \
                --arg devctl '${devctl}' \
                '.host_worktree_root == "/home/bayesartre/dev/agent-worktrees" and
                 .host_state_root == "/home/bayesartre/dev/.devkit/native-agents" and
                 .sandbox_worktree_root == "/workspaces/dev/agent-worktrees" and
                 .sandbox_state_root == "/agent-state" and
                 .mount_policy_identity == "devkit/workspace-egress/v3" and
                 .windows_mounts_visible == false and
                 .proxy.unix_socket == "/home/bayesartre/dev/.devkit/native-egress/pokeemerald-agent1-workspace-egress.sock" and
                 .proxy.allowlist_path == ($devctl + "/kit/proxy/allowlist.txt") and
                 .agent.host_worktree == "/home/bayesartre/dev/agent-worktrees/agent1/pokeemerald" and
                 .agent.sandbox_worktree == "/workspaces/dev/agent-worktrees/agent1/pokeemerald" and
                 .agent.host_home == "/home/bayesartre/dev/.devkit/native-agents/pokeemerald-agent1/home" and
                 .agent.sandbox_home == "/agent-state/pokeemerald-agent1/home" and
                 all(.binds[]; (.mode != "rw") or (.source | startswith("/nix/store") | not)) and
                 (tostring | contains("/nix/store/.devkit/") | not)' \
                "$pokeemerald_plan"
              grep -Fx \
                '  origin: ssh://git@ssh.github.com:443/Divine-Shadow/pokeemerald.git' \
                ${devctl}/overlays/pokeemerald/devkit.yaml

              # Exercise the real installed dev-workspace package path from an
              # empty environment.  The protected controller files are exact
              # read-only mount capabilities; this nested namespace supplies
              # only those paths so the production planner can validate its
              # complete package-owned geometry without ambient host state.
              controller_source="$TMPDIR/fleet-inventory.json"
              controller_gui="$TMPDIR/fleet-codex-gui-inventory.json"
              controller_exec_dir="$TMPDIR/fleet-controller-exec"
              controller_exec="$controller_exec_dir/control.sock"
              printf '%s\n' '{}' > "$controller_source"
              printf '%s\n' '{}' > "$controller_gui"
              mkdir -m 0700 "$controller_exec_dir"
              ${pkgs.socat}/bin/socat \
                UNIX-LISTEN:"$controller_exec",fork,mode=0600 \
                EXEC:${pkgs.coreutils}/bin/true &
              controller_exec_pid=$!
              trap 'kill "$controller_exec_pid" 2>/dev/null || true' EXIT
              for _ in $(${pkgs.coreutils}/bin/seq 1 50); do
                test -S "$controller_exec" && break
                sleep 0.1
              done
              test -S "$controller_exec"
              workspace_plan="$TMPDIR/installed-dev-workspace-plan.json"
              ${pkgs.bubblewrap}/bin/bwrap \
                --die-with-parent \
                --unshare-all \
                --ro-bind /nix/store /nix/store \
                --proc /proc \
                --dev /dev \
                --tmpfs /tmp \
                --dir /etc \
                --dir /etc/fleet \
                --dir /etc/fleet/source \
                --ro-bind "$controller_source" /etc/fleet/source/fleet-inventory.json \
                --ro-bind "$controller_gui" /etc/fleet/source/fleet-codex-gui-inventory.json \
                --dir /run \
                --ro-bind "$controller_exec_dir" /run/fleet-controller-exec \
                -- \
                ${pkgs.coreutils}/bin/env -i \
                  HOME=/tmp/home \
                  DEVKIT_RUNTIME_BROKER_BINARY=${broker}/bin/postgres-broker \
                  DEVKIT_RUNTIME_SHELL_LAUNCHER=${runtimeShell}/bin/dev-all-runtime-shell \
                  DEVKIT_RUNTIME_BWRAP_BINARY=${pkgs.bubblewrap}/bin/bwrap \
                  ${devctl}/kit/bin/devctl -p dev-workspace native plan \
                    --repo shadow-throne-management --index 2 --format json > "$workspace_plan"
              ${pkgs.jq}/bin/jq -e \
                --arg devctl '${devctl}' \
                '.host_worktree_root == "/home/bayesartre/dev/agent-worktrees" and
                 .host_state_root == "/home/bayesartre/dev/.devkit/native-agents" and
                 .sandbox_worktree_root == "/workspaces/dev/agent-worktrees" and
                 .sandbox_state_root == "/agent-state" and
                 .broker_endpoint == "/home/bayesartre/dev/.devkit/native-broker/broker.sock" and
                 .proxy.unix_socket == "/home/bayesartre/dev/.devkit/native-egress/dev-workspace-agent2-workspace-egress.sock" and
                 .proxy.allowlist_path == ($devctl + "/kit/proxy/allowlist.txt") and
                 .agent.host_worktree == "/home/bayesartre/dev/agent-worktrees/agent2/shadow-throne-management" and
                 .agent.sandbox_worktree == "/workspaces/dev/agent-worktrees/agent2/shadow-throne-management" and
                 .agent.host_home == "/home/bayesartre/dev/.devkit/native-agents/dev-workspace-agent2/home" and
                 .agent.sandbox_home == "/agent-state/dev-workspace-agent2/home" and
                 .env.FLEET_EXEC_TRANSPORT_HANDLE == "required" and
                 any(.binds[]; .source == "/run/fleet-controller-exec/control.sock" and
                               .target == "/run/fleet-controller-exec/control.sock" and
                               .mode == "ro" and .required == true) and
                 all(.binds[]; (.source | contains("codex_tailnet_stations_ed25519") | not) and
                               (.target | contains("codex_tailnet_stations_ed25519") | not)) and
                 all(.binds[]; (.mode != "rw") or (.source | startswith("/nix/store") | not)) and
                 (tostring | contains("/nix/store/.devkit/") | not)' \
                "$workspace_plan"
              kill "$controller_exec_pid"
              wait "$controller_exec_pid" 2>/dev/null || true
              trap - EXIT
              mkdir -p "$out"
              cp "$plan" "$out/installed-plan.json"
              cp "$pokeemerald_plan" "$out/installed-pokeemerald-plan.json"
              cp "$workspace_plan" "$out/installed-dev-workspace-plan.json"
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

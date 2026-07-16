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
      governanceJarVersion = "38ec4f97e2f699d2e84110d01c877971d1e8bd97";
      submitRuntimeVersion = "d15715adeadc8881b08ac7a05f19fec15fd29986";
      artifactColumnRuntimeVersion = "8e23ded5579e896c95b5a751f4d4a18da70049a9";
      sbtControlPlaneRuntimeVersion = "be9a16cd8abdf9d479bbf0b7379ebdf0651d156e";
      governanceJarSourceFlake = builtins.getFlake "git+file:///workspaces/dev/ouroboros-ide?rev=${governanceJarVersion}";
      submitRuntimeSourceFlake = builtins.getFlake "git+file:///workspaces/dev/ouroboros-ide?rev=${submitRuntimeVersion}";
      artifactColumnRuntimeSourceFlake = builtins.getFlake "git+file:///workspaces/dev/ouroboros-ide?rev=${artifactColumnRuntimeVersion}";
      sbtControlPlaneRuntimeSourceFlake = builtins.getFlake "git+file:///workspaces/dev/ouroboros-ide?rev=${sbtControlPlaneRuntimeVersion}";
      mkPinnedGovernanceJar = pkgs: governanceJarSourceFlake.packages.${pkgs.system}.governance-jar;
      mkPinnedSubmitToCiJar = pkgs: submitRuntimeSourceFlake.packages.${pkgs.system}.submit-to-ci-jar;
      mkPinnedArtifactColumnPluginRepository = pkgs: artifactColumnRuntimeSourceFlake.packages.${pkgs.system}.artifact-column-plugin-repository;
      mkPinnedArtifactColumnPluginSmoke = pkgs: artifactColumnRuntimeSourceFlake.packages.${pkgs.system}.artifact-column-plugin-adoption-check;
      mkPinnedSbtControlPlaneRuntimeJar = pkgs: sbtControlPlaneRuntimeSourceFlake.packages.${pkgs.system}.sbt-control-plane-runtime-jar;
      mkDevAllRuntimeBundle =
        pkgs:
        let
          submitToCiJar = mkPinnedSubmitToCiJar pkgs;
          x86SubmitBaselineIsExact =
            if pkgs.system == "x86_64-linux" then
              assert toString submitToCiJar == "/nix/store/4xxf15fa8ajm60np3d9vnmiinmb53zd2-submit-to-ci-dev";
              assert submitToCiJar.drvPath == "/nix/store/k9jfshsf7pl3zk87szjdzw3jqzxivz05-submit-to-ci-dev.drv";
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
        pkgs:
        pkgs.buildGoModule {
          pname = "devkit-devctl";
          version = "dev";
          src = ./.;
          modRoot = "cli/devctl";
          vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
          subPackages = [ "." ];
          env.CGO_ENABLED = "0";
          ldflags = [
            "-s"
            "-w"
          ];
          postInstall = ''
            mkdir -p "$out/kit/bin"
            mv "$out/bin/devctl" "$out/kit/bin/devctl"
            rmdir "$out/bin"
            cp "$src/flake.nix" "$src/flake.lock" "$out/"
            cp -R "$src/nix" "$src/overlays" "$out/"
          '';
        };
      mkManagementInspectionApp =
        pkgs:
        let
          devctl = mkDevctl pkgs;
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
        { pkgs, ... }:
        {
          devctl = mkDevctl pkgs;
          dev-all-runtime-bundle = mkDevAllRuntimeBundle pkgs;
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
        { pkgs, ... }:
        {
          dev-all-runtime-bundle = mkDevAllRuntimeBundle pkgs;
          dev-all-runtime-bundle-bridge-smoke = mkDevAllRuntimeBundleBridgeSmoke pkgs;
          dev-all-runtime-bundle-profile-smoke = mkDevAllRuntimeBundleProfileSmoke pkgs;
          management-inspection-cli = mkDevctl pkgs;

          devctl-overlay-runtime-authority-layout =
            let
              devctl = mkDevctl pkgs;
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

          runtime-shell-inventory = pkgs.runCommand "devkit-runtime-shell-inventory" { } ''
            mkdir -p "$out"
            cat > "$out/README" <<'EOF'
            Devkit Nix runtime shell inventory evaluates.

            Required follow-up evidence for each shell:
            - nix develop .#<shell> --command <tool smoke>
            - tool parity against the source Dockerfile
            - brokered OCI access check where applicable
            EOF
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

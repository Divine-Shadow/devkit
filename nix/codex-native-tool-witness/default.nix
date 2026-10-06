{ pkgs, source, rustOverlaySource, officialCodex }:
let
  rustPkgs = import pkgs.path {
    system = pkgs.system;
    overlays = [ (import rustOverlaySource) ];
  };
  rust = rustPkgs.rust-bin.stable."1.95.0".minimal;
  rustPlatform = pkgs.makeRustPlatform { cargo = rust; rustc = rust; };
  native = rustPlatform.buildRustPackage {
    pname = "codex-native-tool-witness-core";
    version = "0.160.0";
    src = source + "/codex-rs";
    patches = [ ./native-tool-witness.patch ];
    patchFlags = [ "-p2" ];
    postPatch = "cp ${./Cargo.lock} Cargo.lock";
    cargoLock = {
      lockFile = ./Cargo.lock;
      outputHashes = {
        "h3-0.0.8" = "sha256-fgE0AMj5d4iattTC/yQwnACV8uEu+KR7wD29xfEm8M0=";
        "appcontainer_common-0.8.0" = "sha256-XUkT2R+RYk9WIqgKnmIAagNW4xOTyp4bWHmQL1iznHw=";
        "crossterm-0.29.0" = "sha256-0OFnAzKZOd5lNkvwdXPu5zbfDWBRQG80OruXxqrFklQ=";
        "nucleo-0.5.0" = "sha256-Hm4SxtTSBrcWpXrtSqeO0TACbUxq3gizg1zD/6Yw/sI=";
        "nucleo-matcher-0.3.1" = "sha256-Hm4SxtTSBrcWpXrtSqeO0TACbUxq3gizg1zD/6Yw/sI=";
        "runfiles-0.1.0" = "sha256-uJpVLcQh8wWZA3GPv9D8Nt43EOirajfDJ7eq/FB+tek=";
        "tokio-tungstenite-0.28.0" = "sha256-V1xmnrfRWOcZZogelZEA4vvyMj2awCfHVA5/glQ6KAI=";
        "tungstenite-0.27.0" = "sha256-VVHhk7l9J/sEmG3q/UuV/sQ3f+fGsmq5vumSy8vbMvw=";
      };
    };
    cargoBuildFlags = [ "-p" "codex-cli" "--bin" "codex" ];
    # Match upstream's package contract; attributed source and native caller
    # checks are collected separately before publication or selection.
    doCheck = false;
    nativeBuildInputs = [ pkgs.cmake pkgs.llvmPackages.clang pkgs.llvmPackages.libclang.lib pkgs.pkg-config ];
    buildInputs = [ pkgs.openssl pkgs.libcap ];
    env = {
      LIBCLANG_PATH = "${pkgs.llvmPackages.libclang.lib}/lib";
      CC = "${pkgs.llvmPackages.clang}/bin/clang";
      CXX = "${pkgs.llvmPackages.clang}/bin/clang++";
      PKG_CONFIG_PATH = pkgs.lib.makeSearchPathOutput "dev" "lib/pkgconfig" [ pkgs.openssl pkgs.libcap ];
    };
  };
in pkgs.runCommand "codex-0.160.0-native-tool-witness" {
  pname = "codex";
  version = "0.160.0-native-tool-witness";
  passthru = { inherit native; witnessSubject = "01a107d7-4be2-71e3-91c3-299ad135b69d"; };
} ''
  mkdir -p "$out"
  cp -a ${officialCodex}/. "$out/"
  chmod u+w "$out" "$out/bin" "$out/bin/codex"
  rm "$out/bin/codex"
  install -m 0755 ${native}/bin/codex "$out/bin/codex"
  # The authoritative WSL package exposes the bundled ripgrep in bin as well.
  ln -s ../codex-path/rg "$out/bin/rg"
  test -x "$out/bin/rg"
  mkdir -p "$out/share/codex-native-tool-witness"
  cat > "$out/share/codex-native-tool-witness/source.json" <<'EOF'
  {"upstreamRevision":"a956835d020762cb2b570053af06f643a11c0ecc","upstreamVersion":"0.160.0","rustVersion":"1.95.0","patchSHA256":"bbc1f58be409b883adb9bcfef453a130d85ebb9c9cecd7d2b086c6d239ea7cbe","subject":"01a107d7-4be2-71e3-91c3-299ad135b69d","phase":"prepared_outbound","executableTarget":"x86_64-unknown-linux-gnu","resourcesFromOfficialRelease":"rust-v0.160.0"}
  EOF
  sha256sum "$out/bin/codex" > "$out/share/codex-native-tool-witness/binary.sha256"
''

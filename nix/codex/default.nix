{ pkgs, source, rustOverlaySource, officialCodex }:
let
  version = "0.163.0";
  rustPkgs = import pkgs.path {
    system = pkgs.system;
    overlays = [ (import rustOverlaySource) ];
  };
  rust = rustPkgs.rust-bin.stable."1.95.0".minimal;
  rustPlatform = pkgs.makeRustPlatform { cargo = rust; rustc = rust; };
  cargoLock = {
    # Release Cargo.toml is 0.163.0 while upstream's lock still labels its 160
    # workspace members 0.0.0. Only those labels are normalized in this lock.
    lockFile = ./Cargo.lock;
    outputHashes = {
      "h3-0.0.8" = "sha256-fgE0AMj5d4iattTC/yQwnACV8uEu+KR7wD29xfEm8M0=";
      "appcontainer_common-0.8.0" = "sha256-XUkT2R+RYk9WIqgKnmIAagNW4xOTyp4bWHmQL1iznHw=";
      "crossterm-0.29.0" = "sha256-7ZzFZ6c6uU27ZxCGOPK82Aia4CzTnJwksAfbpTMPikY=";
      "nucleo-0.5.0" = "sha256-Hm4SxtTSBrcWpXrtSqeO0TACbUxq3gizg1zD/6Yw/sI=";
      "nucleo-matcher-0.3.1" = "sha256-Hm4SxtTSBrcWpXrtSqeO0TACbUxq3gizg1zD/6Yw/sI=";
      "rmcp-3.5.1" = "sha256-RbGuEuNyBUF2WeqKwkBAwCYnjb7DZJP8ZI8sIuR4D88=";
      "runfiles-0.1.0" = "sha256-uJpVLcQh8wWZA3GPv9D8Nt43EOirajfDJ7eq/FB+tek=";
      "tokio-tungstenite-0.28.0" = "sha256-V1xmnrfRWOcZZogelZEA4vvyMj2awCfHVA5/glQ6KAI=";
      "tungstenite-0.27.0" = "sha256-VVHhk7l9J/sEmG3q/UuV/sQ3f+fGsmq5vumSy8vbMvw=";
    };
  };
  upstream = pkgs.callPackage (source + "/codex-rs") {
    inherit rustPlatform version;
  };
  native = upstream.overrideAttrs (old: {
    pname = "codex-web-search-json-core";
    patches = (old.patches or [ ]) ++ [ ./web-search-json.patch ];
    patchFlags = [ "-p2" ];
    postPatch = (old.postPatch or "") + ''
      cp ${./Cargo.lock} Cargo.lock
    '';
    # overrideAttrs runs after buildRustPackage has computed cargoDeps; replace
    # that derivation explicitly so the normalized lock and exact git hashes win.
    cargoDeps = rustPlatform.importCargoLock cargoLock;
    cargoBuildFlags = [ "-p" "codex-cli" "--bin" "codex" ];
    env = (old.env or { }) // {
      LIBCLANG_PATH = "${pkgs.llvmPackages.libclang.lib}/lib";
      CC = "${pkgs.llvmPackages.clang}/bin/clang";
      CXX = "${pkgs.llvmPackages.clang}/bin/clang++";
    };
  });
  provenance = {
    upstreamRevision = "83086188ae0614bd882818ff1aa23f3194962012";
    upstreamVersion = version;
    acceptedSourceCommit = "b3e1e01cd3304f4247fa79dc33a2032436131d43";
    patchSHA256 = builtins.hashFile "sha256" ./web-search-json.patch;
    normalizedLockSHA256 = builtins.hashFile "sha256" ./Cargo.lock;
    rustVersion = "1.95.0";
    executableTarget = pkgs.stdenv.hostPlatform.rust.rustcTarget;
    resourcesFromOfficialRelease = "rust-v0.163.0";
    officialBundle = toString officialCodex;
  };
in pkgs.runCommand "codex-${version}-web-search-json" {
  pname = "codex";
  inherit version;
  passthru = { inherit native provenance officialCodex; };
  meta.mainProgram = "codex";
} ''
  mkdir -p "$out"
  cp -a ${officialCodex}/. "$out/"
  chmod u+w "$out" "$out/bin"
  rm "$out/bin/codex"
  install -m 0755 ${native}/bin/codex "$out/bin/codex"
  ln -s ../codex-path/rg "$out/bin/rg"
  test -x "$out/bin/codex-code-mode-host"
  test -x "$out/bin/rg"
  test -x "$out/codex-resources/bwrap"
  test -d "$out/codex-resources/voice"
  "$out/bin/codex" --version | grep -Fx 'codex-cli 0.163.0'
  mkdir -p "$out/share/codex-source"
  cp ${pkgs.writeText "codex-source.json" (builtins.toJSON provenance)} "$out/share/codex-source/source.json"
  sha256sum "$out/bin/codex" > "$out/share/codex-source/binary.sha256"
''

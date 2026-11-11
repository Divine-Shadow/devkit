{ pkgs, source, rustOverlaySource, officialCodex }:
let
  version = "0.167.1";
  rustPkgs = import pkgs.path {
    system = pkgs.system;
    overlays = [ (import rustOverlaySource) ];
  };
  rust = rustPkgs.rust-bin.stable."1.95.0".minimal;
  rustPlatform = pkgs.makeRustPlatform { cargo = rust; rustc = rust; };
  cargoLock = {
    # Release Cargo.toml is 0.167.1 while upstream's lock still labels its 171
    # workspace members 0.0.0. Only those labels are normalized in this lock.
    lockFile = ./Cargo.lock;
    outputHashes = {
      "v8-152.2.0" = "sha256-y7tZjz7gEBgxhllKWMTEn0VlCDF/NQrdORYeZBOLEU4=";
      "mxc-sdk-1.0.0" = "sha256-AI4l2yCbB/4UWweLFhEefEPqSt/ZB1V+Mk/svdAT8No=";
      "h3-0.0.8" = "sha256-fgE0AMj5d4iattTC/yQwnACV8uEu+KR7wD29xfEm8M0=";
      "crossterm-0.29.0" = "sha256-7ZzFZ6c6uU27ZxCGOPK82Aia4CzTnJwksAfbpTMPikY=";
      "nucleo-0.5.0" = "sha256-Hm4SxtTSBrcWpXrtSqeO0TACbUxq3gizg1zD/6Yw/sI=";
      "nucleo-matcher-0.3.1" = "sha256-Hm4SxtTSBrcWpXrtSqeO0TACbUxq3gizg1zD/6Yw/sI=";
      "rmcp-3.5.1" = "sha256-yczrrD6SnXhblos5oU6fbyDc7vkRah9DsF41hcaKzi4=";
      "runfiles-0.1.0" = "sha256-uJpVLcQh8wWZA3GPv9D8Nt43EOirajfDJ7eq/FB+tek=";
      "tokio-tungstenite-0.28.0" = "sha256-V1xmnrfRWOcZZogelZEA4vvyMj2awCfHVA5/glQ6KAI=";
      "tungstenite-0.27.0" = "sha256-VVHhk7l9J/sEmG3q/UuV/sQ3f+fGsmq5vumSy8vbMvw=";
    };
  };
  # Keep the dependency's own upstream lock provenance available to its
  # unchanged binding-version guard. The shared Codex lock has unrelated
  # Windows versions, so the guard's parent-directory fallback is ambiguous.
  mxcSource = pkgs.fetchgit {
    url = "https://github.com/microsoft/mxc";
    rev = "b21120168d70befe12356411423266bfef168446";
    hash = "sha256-AI4l2yCbB/4UWweLFhEefEPqSt/ZB1V+Mk/svdAT8No=";
    fetchSubmodules = true;
  };
  cargoVendor = rustPlatform.importCargoLock cargoLock;
  cargoDeps = pkgs.runCommand "cargo-vendor-dir" {
    nativeBuildInputs = [ pkgs.python3 ];
  } ''
    python3 - ${./Cargo.lock} ${mxcSource}/src/Cargo.lock ${cargoVendor}/mxc-sdk-1.0.0/Cargo.toml <<'PY'
import sys, tomllib
from pathlib import Path
root, upstream, manifest = [tomllib.loads(Path(path).read_text()) for path in sys.argv[1:]]
mxc = [package for package in root["package"] if package["name"] == "mxc-sdk"]
assert len(mxc) == 1, "mxc dependency selection is ambiguous"
selected = [dep.split()[1] for dep in mxc[0]["dependencies"] if dep.startswith("windows ")]
assert len(selected) == 1, "mxc Windows dependency is not version-qualified"
windows = [package["version"] for package in upstream["package"] if package["name"] == "windows"]
assert windows == selected, "upstream mxc lock disagrees with the actual Codex dependency resolution"
requirements = {table["dependencies"]["windows"]["version"] for table in manifest["target"].values()
                if "windows" in table.get("dependencies", {})}
assert requirements == {".".join(selected[0].split(".")[:2])}, "mxc Windows manifest requirement disagrees"
PY
    mkdir -p "$out"
    cp -a ${cargoVendor}/. "$out/"
    chmod u+w "$out"
    rm "$out/mxc-sdk-1.0.0"
    cp -aL ${cargoVendor}/mxc-sdk-1.0.0 "$out/mxc-sdk-1.0.0"
    chmod u+w "$out/mxc-sdk-1.0.0"
    cp ${mxcSource}/src/Cargo.lock "$out/mxc-sdk-1.0.0/Cargo.lock"
  '';
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
    inherit cargoDeps;
    cargoBuildFlags = [ "-p" "codex-cli" "--bin" "codex" ];
    env = (old.env or { }) // {
      LIBCLANG_PATH = "${pkgs.llvmPackages.libclang.lib}/lib";
      CC = "${pkgs.llvmPackages.clang}/bin/clang";
      CXX = "${pkgs.llvmPackages.clang}/bin/clang++";
    };
  });
  provenance = {
    upstreamRevision = "0a94398fa60c4fac0e7659e4213a0a36e613dd46";
    upstreamVersion = version;
    acceptedSourceCommit = "b3e1e01cd3304f4247fa79dc33a2032436131d43";
    patchSHA256 = builtins.hashFile "sha256" ./web-search-json.patch;
    normalizedLockSHA256 = builtins.hashFile "sha256" ./Cargo.lock;
    rustVersion = "1.95.0";
    executableTarget = pkgs.stdenv.hostPlatform.rust.rustcTarget;
    resourcesFromOfficialRelease = "rust-v0.167.1";
    officialBundle = toString officialCodex;
    mxcBindingLockSource = "${mxcSource}/src/Cargo.lock";
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
  "$out/bin/codex" --version | grep -Fx 'codex-cli 0.167.1'
  mkdir -p "$out/share/codex-source"
  cp ${pkgs.writeText "codex-source.json" (builtins.toJSON provenance)} "$out/share/codex-source/source.json"
  sha256sum "$out/bin/codex" > "$out/share/codex-source/binary.sha256"
''

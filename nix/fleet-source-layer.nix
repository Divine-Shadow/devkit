{
  pkgs,
  controllerFleetPath,
  controllerDevctlPath,
  sourceTransport,
  controllerSourceInventory,
  controllerGUIInventory,
  runtimeBashExecutable ? "${pkgs.bash}/bin/bash",
  runtimePath ? pkgs.lib.makeBinPath [ pkgs.bash pkgs.coreutils pkgs.curl pkgs.findutils pkgs.gawk pkgs.gnugrep pkgs.gnused pkgs.iproute2 pkgs.procps ],
}:

let
  sourceSchema = "fleet-controller-source-layer/v1";
  sourceTransportFields = {
    sourceTransportPath = sourceTransport.executablePath;
    sourceTransportGitSSHPath = sourceTransport.gitSSH.executablePath;
    sourceTransportOpenSSHPath = sourceTransport.openSSHExecutablePath;
    sourceTransportConfigPath = sourceTransport.gitSSH.configPath;
    sourceTransportKnownHostsPath = sourceTransport.knownHostsPath;
  };
  manifestTemplate = pkgs.writeText "fleet-controller-source-layer-manifest.json" (builtins.toJSON {
    schemaVersion = sourceSchema;
    packagePath = "@out@";
    launcherPath = "@out@/bin/fleet-source-layer";
    inherit controllerFleetPath controllerDevctlPath controllerSourceInventory controllerGUIInventory;
    gitExecutablePath = "${pkgs.git}/bin/git";
    gitSSHExecutablePath = sourceTransport.gitSSH.executablePath;
    openSSHExecutablePath = sourceTransport.openSSHExecutablePath;
    inherit sourceTransportFields;
    runtime = {
      bashExecutablePath = runtimeBashExecutable;
      path = runtimePath;
    };
  });
  launcher = pkgs.writeShellScriptBin "fleet-source-layer" ''
    set -eu
    bundle_root="''${0%/bin/fleet-source-layer}"
    manifest="$bundle_root/share/fleet-controller-source-layer/manifest.json"
    fail() { echo "fleet-source-layer: $*" >&2; exit 1; }
    [ -r "$manifest" ] || fail "missing immutable source-layer manifest"
    digest_file="$manifest.sha256"
    [ -r "$digest_file" ] || fail "missing source-layer manifest digest"
    declared_digest="$(${pkgs.coreutils}/bin/cat "$digest_file")"
    actual_digest="$(${pkgs.coreutils}/bin/sha256sum "$manifest" | ${pkgs.coreutils}/bin/cut -d' ' -f1)"
    [ "$declared_digest" = "$actual_digest" ] || fail "source-layer manifest digest mismatch"
    ${pkgs.jq}/bin/jq -e \
      --arg schema '${sourceSchema}' \
      --arg fleet '${controllerFleetPath}' \
      --arg self "$bundle_root" \
      '.schemaVersion == $schema and .packagePath == $self and .launcherPath == ($self + "/bin/fleet-source-layer") and .controllerFleetPath == $fleet and
       (.controllerSourceInventory.path | startswith("/nix/store/")) and
       (.controllerGUIInventory.path | startswith("/nix/store/")) and
       (.runtime.bashExecutablePath | startswith("/nix/store/")) and
       (.runtime.path | length > 0) and
       (.sourceTransportFields | type == "object")' \
      "$manifest" >/dev/null || fail "source-layer manifest shape mismatch"
    for projection in controllerSourceInventory controllerGUIInventory; do
      path="$(${pkgs.jq}/bin/jq -r --arg key "$projection" '.[$key].path' "$manifest")"
      sha="$(${pkgs.jq}/bin/jq -r --arg key "$projection" '.[$key].sha256' "$manifest")"
      [ -f "$path" ] || fail "source-layer inventory is not a regular file"
      actual="$(${pkgs.coreutils}/bin/sha256sum "$path" | ${pkgs.coreutils}/bin/cut -d' ' -f1)"
      [ "$actual" = "$sha" ] || fail "source-layer inventory digest mismatch"
    done
    export FLEET_SOURCE_LAYER_MANIFEST="$manifest"
    export FLEET_SOURCE_LAYER_SHA256="$(${pkgs.coreutils}/bin/sha256sum "$manifest" | ${pkgs.coreutils}/bin/cut -d' ' -f1)"
    exec '${controllerFleetPath}' "$@"
  '';
in
pkgs.runCommand "fleet-source-layer" { } ''
  bundle_root="$out"
  mkdir -p "$out/bin" "$out/share/fleet-controller-source-layer"
  cp '${launcher}/bin/fleet-source-layer' "$out/bin/fleet-source-layer"
  substitute '${manifestTemplate}' "$out/share/fleet-controller-source-layer/manifest.json" --replace-fail '@out@' "$out"
  sha256sum "$out/share/fleet-controller-source-layer/manifest.json" | cut -d' ' -f1 > "$out/share/fleet-controller-source-layer/manifest.json.sha256"
  chmod 0555 "$out/bin/fleet-source-layer"
  chmod 0444 "$out/share/fleet-controller-source-layer/manifest.json"
  chmod 0444 "$out/share/fleet-controller-source-layer/manifest.json.sha256"
''

{
  pkgs,
  mkSourceTransportPackage,
  productOrigin,
  productRevision,
  lifecycleRoot,
  identityPath,
}:

let
  lib = pkgs.lib;
  sourceTransportPackage = mkSourceTransportPackage {
    inherit pkgs;
    directNetwork = true;
  };
  sourceTransport = sourceTransportPackage.sourceTransport;
  checkoutPath = "${lifecycleRoot}/product";
  receiptPath = "${lifecycleRoot}/source-acquisition-receipt.json";
  taintMarkerPath = "${lifecycleRoot}/.tainted";
  transportSocketPath = "${lifecycleRoot}/source-transport.sock";
  manifestRelativePath = "share/devkit-product-source-acquisition/manifest.json";
  manifestTemplate = pkgs.writeText "devkit-product-source-acquisition-manifest-template.json" (
    builtins.toJSON {
      schemaVersion = "devkit/product-source-acquisition-manifest/v1";
      packagePath = "@out@";
      executablePath = "@out@/bin/devkit-product-source-acquire";
      product = {
        origin = productOrigin;
        revision = productRevision;
        inherit
          checkoutPath
          identityPath
          lifecycleRoot
          receiptPath
          taintMarkerPath
          ;
      };
      runtime = {
        gitExecutablePath = "${pkgs.git}/bin/git";
        openSSHExecutablePath = sourceTransport.openSSHExecutablePath;
        path = lib.makeBinPath [
          pkgs.bash
          pkgs.coreutils
          pkgs.git
          pkgs.openssh
        ];
      };
      transport = {
        inherit (sourceTransport) schemaVersion;
        networkMode = sourceTransport.network.mode;
        executablePath = sourceTransport.executablePath;
        gitSSHExecutablePath = sourceTransport.gitSSH.executablePath;
        sshConfigPath = sourceTransport.gitSSH.configPath;
        knownHostsPath = sourceTransport.knownHostsPath;
        allowlistPath = sourceTransport.network.allowlistPath;
        networkContractPath = sourceTransport.network.contractPath;
        managedConnectProxy = sourceTransport.network.managedConnectProxy;
        socketPath = transportSocketPath;
      };
    }
  );
  outputPlaceholder = placeholder "out";
  package = pkgs.buildGoModule {
    pname = "devkit-product-source-acquirer";
    version = "dev";
    src = ../cli/devctl;
    modRoot = ".";
    vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
    subPackages = [ "cmd/product-source-acquire" ];
    env.CGO_ENABLED = "0";
    ldflags = [
      "-s"
      "-w"
      "-X=devkit/cli/devctl/internal/productsourceacquire.packageManifestPath=${outputPlaceholder}/${manifestRelativePath}"
    ];
    doCheck = true;
    checkPhase = ''
      runHook preCheck
      go test ./cmd/product-source-acquire ./internal/productsourceacquire -count=1
      runHook postCheck
    '';
    postInstall = ''
      mv "$out/bin/product-source-acquire" "$out/bin/devkit-product-source-acquire"
      mkdir -p "$out/share/devkit-product-source-acquisition"
      substitute '${manifestTemplate}' "$out/${manifestRelativePath}" \
        --replace-fail '@out@' "$out"
      sha256sum "$out/${manifestRelativePath}" | cut -d' ' -f1 \
        > "$out/${manifestRelativePath}.sha256"
      chmod 0555 "$out/bin/devkit-product-source-acquire"
      chmod 0444 "$out/${manifestRelativePath}" "$out/${manifestRelativePath}.sha256"
    '';
    passthru.productSourceAcquisition = {
      schemaVersion = "devkit/product-source-acquisition-manifest/v1";
      packagePath = package;
      executablePath = "${package}/bin/devkit-product-source-acquire";
      manifestPath = "${package}/${manifestRelativePath}";
      manifestSha256Path = "${package}/${manifestRelativePath}.sha256";
      inherit
        checkoutPath
        identityPath
        lifecycleRoot
        productOrigin
        productRevision
        receiptPath
        taintMarkerPath
        transportSocketPath
        ;
    };
  };
in
assert builtins.match "[0-9a-f]{40}" productRevision != null;
assert
  (lib.hasPrefix "git@github.com:" productOrigin || lib.hasPrefix "ssh://git@ssh.github.com:443/" productOrigin)
  && lib.hasSuffix ".git" productOrigin;
assert lib.hasPrefix "/" lifecycleRoot && lifecycleRoot != "/";
assert lib.hasPrefix "/" identityPath;
assert sourceTransport.schemaVersion == "devkit/source-transport/v4";
assert sourceTransport.gitSSH.schemaVersion == "devkit/source-transport-git-ssh/v2";
assert sourceTransport.network.schemaVersion == "devkit/source-transport-network/v2";
assert sourceTransport.network.mode == "package-owned-direct-connect";
assert sourceTransport.network.managedConnectProxy == "";
assert sourceTransport.network.directFallback == false;
package

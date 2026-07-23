{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.devkitProductConsumer;
  adapter = cfg.adapterPackage;
  resources = adapter.productAdapterResources;
  wrappers = resources.namespaceWrappers;
  geometry = import ./product-consumer-geometry.nix;
  candidateParent = geometry.candidateParent;
  supervisorRoot = geometry.supervisorRoot;
  consumerDefinitions = map (
    consumer:
    consumer
    // {
      authorizedKeys =
        if consumer.index == 1 then
          cfg.consumer1AuthorizedKeys
        else
          cfg.consumer2AuthorizedKeys;
    }
  ) geometry.consumers;
  forcedCommandConfig = lib.concatMapStringsSep "\n" (
    consumer:
    ''
      Match User ${consumer.name}
        AuthorizedKeysFile /etc/devkit/product-consumer-authorized-keys/${consumer.name}
        ForceCommand ${adapter}/bin/product-ssh-session force-command --count 2 --index ${toString consumer.index}
        PermitTTY no
        X11Forwarding no
        AllowTcpForwarding no
    ''
  ) consumerDefinitions;
  installAuthoritySelector = pkgs.writeShellScript "devkit-product-consumer-install-authority-selector" ''
    set -eu
    digest="$(${pkgs.coreutils}/bin/cut -d' ' -f1 ${cfg.authorityManifestSha256File})"
    test "''${#digest}" -eq 64 || {
      echo "Product authority manifest digest file is not one exact lowercase SHA-256" >&2
      exit 65
    }
    case "$digest" in
      *[!0-9a-f]*)
        echo "Product authority manifest digest file is not one exact lowercase SHA-256" >&2
        exit 65
        ;;
    esac
    ${pkgs.jq}/bin/jq -e \
      --arg candidate_parent ${candidateParent} \
      --arg supervisor_root ${supervisorRoot} \
      --arg authorized_1 ${cfg.consumer1AuthorizedKeys} \
      --arg authorized_2 ${cfg.consumer2AuthorizedKeys} \
      '
        [
          {
            index: 1,
            uid: 2001,
            projection: "a",
            authorized: $authorized_1
          },
          {
            index: 2,
            uid: 2002,
            projection: "b",
            authorized: $authorized_2
          }
        ] as $expected |
        . as $authority |
        $authority.schemaVersion == "fleet-runtime-authority/v1" and
        $authority.devkitProductAdapter.count == 2 and
        all(range(0; 2);
          . as $slot |
          $authority.devkitProductAdapter.consumers[$slot] as $consumer |
          $expected[$slot] as $identity |
          $consumer.index == $identity.index and
          $consumer.uid == $identity.uid and
          $consumer.gid == $identity.uid and
          $consumer.candidateRoot == ($candidate_parent + "/" + $identity.projection + "/slot") and
          $consumer.homePath == ($consumer.candidateRoot + "/home") and
          $consumer.stateRoot == ($consumer.candidateRoot + "/state") and
          $consumer.appServerSocketPath == ($consumer.stateRoot + "/app-server.sock") and
          $consumer.supervisorSocketPath ==
            ($supervisor_root + "/control-" + ($identity.index | tostring) + "/product-supervisor.sock") and
          $consumer.authorizedKeysPath == $identity.authorized
        )
      ' ${cfg.authorityManifest} >/dev/null
    exec ${adapter}/bin/product-authority-selector-install \
      --manifest ${cfg.authorityManifest} \
      --manifest-sha256 "$digest"
  '';
  wrapperConfiguration = lib.mapAttrs' (
    _: wrapper:
    lib.nameValuePair wrapper.name {
      inherit (wrapper) setuid;
      source = "${adapter}/bin/${wrapper.target}";
      owner = "root";
      group = if wrapper.controllerOnly or false then cfg.controllerGroup else "root";
      permissions =
        if wrapper.controllerOnly or false then
          "u+rx,g+rx,o-rwx"
        else
          "u+rx,g+x,o+x";
    }
  ) wrappers;
  consumerGroups = builtins.listToAttrs (
    map (consumer: {
      name = consumer.name;
      value.gid = consumer.uid;
    }) consumerDefinitions
  );
  consumerUsers = builtins.listToAttrs (
    map (consumer: {
      name = consumer.name;
      value = {
        isSystemUser = true;
        uid = consumer.uid;
        group = consumer.name;
        autoSubUidGidRange = false;
        subUidRanges = [ ];
        subGidRanges = [ ];
        home = "${candidateParent}/${consumer.projection}/slot/home";
        shell = wrappers.sshSession.path;
        hashedPassword = "";
      };
    }) consumerDefinitions
  );
  authorizedKeyFiles = builtins.listToAttrs (
    map (consumer: {
      name = "devkit/product-consumer-authorized-keys/${consumer.name}";
      value = {
        source = consumer.authorizedKeys;
        mode = "0444";
        user = "root";
        group = "root";
      };
    }) consumerDefinitions
  );
  supervisorServices = builtins.listToAttrs (
    map (consumer: {
      name = "devkit-product-consumer-${toString consumer.index}";
      value = {
        description = "Devkit Product consumer ${toString consumer.index} supervisor";
        wantedBy = [ "multi-user.target" ];
        requires = [ "devkit-product-authority-selector.service" ];
        after = [ "devkit-product-authority-selector.service" ];
        serviceConfig = {
          Type = "simple";
          User = consumer.name;
          Group = consumer.name;
          UMask = "0077";
          ExecStart = "${wrappers.supervisor.path} serve --count 2 --index ${toString consumer.index}";
          Restart = "no";
          # Signal only the supervisor first. Its compiled shutdown path stops
          # the app-server/adapter and verifies proxy cleanup. systemd may kill
          # the remaining cgroup only if that bounded graceful stop times out.
          KillMode = "mixed";
          KillSignal = "SIGTERM";
          TimeoutStopSec = "10s";
        };
      };
    }) consumerDefinitions
  );
in
{
  options.services.devkitProductConsumer = {
    enable = lib.mkEnableOption "the source-derived two-slot Product consumer boundary";

    adapterPackage = lib.mkOption {
      type = lib.types.package;
      description = "The immutable compiled Devkit Product adapter package selected by the authoritative derivation.";
    };

    authorityManifest = lib.mkOption {
      type = lib.types.path;
      description = "The exact immutable fleet-runtime-authority/v1 manifest consumed by the Product adapter.";
    };

    authorityManifestSha256File = lib.mkOption {
      type = lib.types.path;
      description = "An immutable file containing the exact SHA-256 of authorityManifest.";
    };

    consumer1AuthorizedKeys = lib.mkOption {
      type = lib.types.path;
      description = "The immutable authorized_keys source for Product consumer 1.";
    };

    consumer2AuthorizedKeys = lib.mkOption {
      type = lib.types.path;
      description = "The immutable authorized_keys source for Product consumer 2.";
    };

    controllerGroup = lib.mkOption {
      type = lib.types.str;
      default = "controller";
      description = "The pre-existing controller group allowed to invoke controller-only entry wrappers.";
    };

  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = builtins.hasAttr "productAdapterResources" adapter;
        message = "services.devkitProductConsumer.adapterPackage must expose productAdapterResources";
      }
      {
        assertion = builtins.match "/nix/store/[^/]+/.+" (toString cfg.authorityManifest) != null;
        message = "services.devkitProductConsumer.authorityManifest must be one immutable Nix store file";
      }
      {
        assertion = builtins.match "/nix/store/[^/]+/.+" (toString cfg.authorityManifestSha256File) != null;
        message = "services.devkitProductConsumer.authorityManifestSha256File must be one immutable Nix store file";
      }
    ];

    users.groups = consumerGroups;
    users.users = consumerUsers;

    environment.shells = [ wrappers.sshSession.path ];
    environment.systemPackages = [
      adapter
      pkgs.bash
      pkgs.coreutils
    ];
    environment.etc = authorizedKeyFiles // {
      "devkit/product-consumer-force-command.conf" = {
        text = forcedCommandConfig;
        mode = "0444";
        user = "root";
        group = "root";
      };
    };

    services.openssh.extraConfig = forcedCommandConfig;
    security.wrappers = wrapperConfiguration;

    systemd.tmpfiles.rules =
      [
        "d /var/lib/product-runtime 0755 root root -"
        "d ${candidateParent} 0711 root root -"
        "d ${supervisorRoot} 0711 root root -"
      ]
      ++ lib.concatMap (consumer: [
        "d ${candidateParent}/${consumer.projection} 0700 ${consumer.name} ${consumer.name} -"
        "d ${supervisorRoot}/control-${toString consumer.index} 0700 ${consumer.name} ${consumer.name} -"
      ]) consumerDefinitions;

    systemd.services = supervisorServices // {
      devkit-product-authority-selector = {
        description = "Install the exact immutable Product runtime authority selector";
        wantedBy = [ "multi-user.target" ];
        before = [
          "sshd.service"
          "devkit-product-consumer-1.service"
          "devkit-product-consumer-2.service"
        ];
        serviceConfig = {
          Type = "oneshot";
          ExecStart = installAuthoritySelector;
          RemainAfterExit = true;
        };
      };
    };

  };
}

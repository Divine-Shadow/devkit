{ pkgs, mkSourceTransportPackage }:
let
  fixture =
    pkgs.runCommand "devkit-source-transport-git-ssh-fixture"
      {
        nativeBuildInputs = [
          pkgs.git
          pkgs.openssh
        ];
      }
      ''
        set -euo pipefail
        umask 077
        mkdir -p "$out"
        ssh-keygen -q -t ed25519 -N "" -f "$out/host-key"
        host_fields="$(awk '{print $1 " " $2}' "$out/host-key.pub")"
        printf '[ssh.github.com]:443 %s\n' "$host_fields" > "$out/known-hosts"
        seed="$TMPDIR/seed"
        git init --initial-branch=main "$seed"
        git -C "$seed" config user.name fixture
        git -C "$seed" config user.email fixture@example.invalid
        dd if=/dev/zero of="$seed/payload.bin" bs=1024 count=2048 status=none
        git -C "$seed" add payload.bin
        GIT_AUTHOR_DATE='2000-01-01T00:00:00Z' \
          GIT_COMMITTER_DATE='2000-01-01T00:00:00Z' \
          git -C "$seed" commit -m 'source transport fixture'
        git init --bare --initial-branch=main "$out/repository.git"
        git -C "$seed" push "$out/repository.git" main:main
        git -C "$seed" rev-parse HEAD > "$out/revision"
      '';
  transport = mkSourceTransportPackage {
    inherit pkgs;
    knownHostsFile = "${fixture}/known-hosts";
  };
  interface = transport.sourceTransport;
  sshdConfig = pkgs.writeText "devkit-source-transport-test-sshd-config" ''
    ListenAddress 127.0.0.1
    Port 443
    PidFile /run/devkit-source-transport/sshd.pid
    HostKey /run/devkit-source-transport/host-key
    AuthorizedKeysFile /var/lib/git/.ssh/authorized_keys
    StrictModes no
    PasswordAuthentication no
    KbdInteractiveAuthentication no
    PubkeyAuthentication yes
    UsePAM no
  '';
  hostileLoginShell = pkgs.writeShellScript "devkit-hostile-login-shell" ''
    '${pkgs.coreutils}/bin/touch' \
      /run/devkit-source-transport/hostile-login-shell-invoked
    exit 93
  '';
  lifecycle = pkgs.writeShellScript "devkit-source-transport-git-ssh-lifecycle" ''
    set -Eeuo pipefail
    umask 077
    root=/run/devkit-source-transport
    chroot_root="$root/chroot"
    socket_path="$chroot_root/run/devkit-source-transport/consumer.sock"
    sshd_pid=
    inner_pid=
    store_mounted=0
    dev_null_mounted=0
    remove_mutable_key_material() {
      rm -f -- \
        "$root/host-key" \
        "$root/client-key" \
        "$root/client-key.pub" \
        "$root/authorized-keys" \
        "$chroot_root/run/devkit-source-transport/client-key" \
        /var/lib/git/.ssh/authorized_keys
    }
    best_effort_cleanup() {
      status=$?
      trap - EXIT
      if test "$status" -eq 0; then
        exit 0
      fi
      for pid in "$inner_pid" "$sshd_pid"; do
        test -z "$pid" && continue
        kill "$pid" >/dev/null 2>&1 || true
        wait "$pid" >/dev/null 2>&1 || true
      done
      if test "$dev_null_mounted" -eq 1; then
        '${pkgs.util-linux}/bin/umount' "$chroot_root/dev/null" \
          >/dev/null 2>&1 || true
      fi
      if test "$store_mounted" -eq 1; then
        '${pkgs.util-linux}/bin/umount' "$chroot_root/nix/store" \
          >/dev/null 2>&1 || true
      fi
      remove_mutable_key_material >/dev/null 2>&1 || true
      rm -f -- "$socket_path" >/dev/null 2>&1 || true
      exit "$status"
    }
    strict_stop() {
      label=$1
      pid=$2
      expected_statuses=$3
      test -n "$pid"
      kill -0 "$pid"
      kill -TERM "$pid"
      wait_status=0
      if wait "$pid"; then
        wait_status=0
      else
        wait_status=$?
      fi
      case ",$expected_statuses," in
        *",$wait_status,"*) ;;
        *)
          echo "$label exited with unexpected teardown status $wait_status" >&2
          return 1
          ;;
      esac
      if kill -0 "$pid" 2>/dev/null; then
        echo "$label remained alive after teardown wait" >&2
        return 1
      fi
      printf '%s_wait_status=%s\n' "$label" "$wait_status" \
        >> "$root/teardown.receipt"
    }
    strict_unmount() {
      label=$1
      path=$2
      '${pkgs.util-linux}/bin/mountpoint' -q "$path"
      '${pkgs.util-linux}/bin/umount' "$path"
      if '${pkgs.util-linux}/bin/mountpoint' -q "$path"; then
        echo "$label bind mount remained after teardown" >&2
        return 1
      fi
      printf '%s_unmounted=true\n' "$label" >> "$root/teardown.receipt"
    }
    assert_no_key_material_in_evidence() {
      client_public_material="$(awk '{print $2}' "$root/client-key.pub")"
      host_public_material="$(awk '{print $2}' '${fixture}/host-key.pub')"
      test -n "$client_public_material"
      test -n "$host_public_material"
      for evidence in \
        "$root/sshd.log" \
        "$root/inner.log" \
        "$root/teardown.receipt"
      do
        test -f "$evidence"
        if grep -aF -- 'BEGIN OPENSSH PRIVATE KEY' "$evidence" \
          || grep -aF -- "$client_public_material" "$evidence" \
          || grep -aF -- "$host_public_material" "$evidence"
        then
          echo "key material escaped into lifecycle evidence" >&2
          return 1
        fi
      done
    }
    trap best_effort_cleanup EXIT
    rm -rf "$root"
    mkdir -m0700 -p \
      "$root/home" \
      "$chroot_root/dev" \
      "$chroot_root/etc" \
      "$chroot_root/home" \
      "$chroot_root/nix/store" \
      "$chroot_root/run/devkit-source-transport" \
      "$chroot_root/tmp" \
      "$chroot_root/work" \
      /run/sshd
    chmod 01777 "$chroot_root/tmp"
    printf '%s\n' \
      'root:x:0:0:root:/home:${hostileLoginShell}' \
      > "$chroot_root/etc/passwd"
    printf '%s\n' 'root:x:0:' > "$chroot_root/etc/group"
    printf '%s\n' \
      'passwd: files' \
      'group: files' \
      'hosts: files' \
      > "$chroot_root/etc/nsswitch.conf"
    printf '%s\n' \
      '127.0.0.1 localhost ssh.github.com' \
      > "$chroot_root/etc/hosts"
    touch "$chroot_root/dev/null"
    '${pkgs.util-linux}/bin/mount' --bind /nix/store "$chroot_root/nix/store"
    store_mounted=1
    '${pkgs.util-linux}/bin/mount' --bind /dev/null "$chroot_root/dev/null"
    dev_null_mounted=1
    test ! -e "$chroot_root/bin/sh"
    test "$(cut -d: -f7 "$chroot_root/etc/passwd")" = '${hostileLoginShell}'
    cp '${fixture}/host-key' "$root/host-key"
    chmod 0600 "$root/host-key"
    '${pkgs.openssh}/bin/ssh-keygen' -q -t ed25519 -N "" -f "$root/client-key"
    cp "$root/client-key" \
      "$chroot_root/run/devkit-source-transport/client-key"
    chmod 0600 "$chroot_root/run/devkit-source-transport/client-key"
    printf 'restrict,command="%s" %s\n' \
      '${pkgs.git}/bin/git-upload-pack ${fixture}/repository.git' \
      "$(cat "$root/client-key.pub")" > "$root/authorized-keys"
    chmod 0600 "$root/authorized-keys"
    install -d -o git -g git -m0700 /var/lib/git/.ssh
    install -o git -g git -m0600 "$root/authorized-keys" /var/lib/git/.ssh/authorized_keys

    '${pkgs.openssh}/bin/sshd' -D -e -f '${sshdConfig}' >"$root/sshd.log" 2>&1 &
    sshd_pid=$!
    for attempt in $(seq 1 100); do
      if '${pkgs.netcat-openbsd}/bin/nc' -z 127.0.0.1 443; then
        break
      fi
      sleep 0.05
    done
    if ! '${pkgs.netcat-openbsd}/bin/nc' -z 127.0.0.1 443; then
      head -c 16384 "$root/sshd.log" >&2 || true
      exit 1
    fi
    '${interface.executablePath}' serve \
      --socket "$socket_path" \
      --allowlist '${interface.network.allowlistPath}' \
      >"$root/inner.log" 2>&1 &
    inner_pid=$!
    for attempt in $(seq 1 100); do
      test -S "$socket_path" && break
      sleep 0.05
    done
    test -S "$socket_path"

    for index in 1 2; do
      destination="$chroot_root/work/clone-$index"
      env -i \
        HOME=/home \
        PATH=/hostile \
        SHELL='${hostileLoginShell}' \
        GIT_CONFIG_GLOBAL=/dev/null \
        GIT_CONFIG_NOSYSTEM=1 \
        GIT_SSH='${interface.gitSSH.executablePath}' \
        GIT_SSH_VARIANT=ssh \
        DEVKIT_SOURCE_TRANSPORT_IDENTITY=/run/devkit-source-transport/client-key \
        DEVKIT_SOURCE_TRANSPORT_SOCKET=/run/devkit-source-transport/consumer.sock \
        '${pkgs.coreutils}/bin/chroot' "$chroot_root" \
          '${pkgs.git}/bin/git' clone \
          'ssh://git@github.com:443/repository.git' "/work/clone-$index"
      test "$('${pkgs.git}/bin/git' -C "$destination" rev-parse HEAD)" = \
        "$(cat '${fixture}/revision')"
      test -s "$destination/payload.bin"
      rm -rf "$destination"
    done

    test "$(stat -c %a "$socket_path")" = 600
    test ! -e "$root/hostile-login-shell-invoked"
    test ! -e "$chroot_root/run/devkit-source-transport/hostile-login-shell-invoked"
    test ! -e "$root/generated-git-ssh-wrapper"

    : > "$root/teardown.receipt"
    strict_stop inner_proxy "$inner_pid" '0,2,143'
    inner_pid=
    strict_stop sshd "$sshd_pid" '0,143'
    sshd_pid=
    test ! -e "$socket_path"
    strict_unmount dev_null "$chroot_root/dev/null"
    dev_null_mounted=0
    strict_unmount nix_store "$chroot_root/nix/store"
    store_mounted=0
    assert_no_key_material_in_evidence
    remove_mutable_key_material
    for residue in \
      "$root/host-key" \
      "$root/client-key" \
      "$root/client-key.pub" \
      "$root/authorized-keys" \
      "$chroot_root/run/devkit-source-transport/client-key" \
      /var/lib/git/.ssh/authorized_keys
    do
      test ! -e "$residue"
    done
    test ! -e "$socket_path"
    test ! -e "$root/hostile-login-shell-invoked"
    test ! -e "$chroot_root/run/devkit-source-transport/hostile-login-shell-invoked"
    grep -qF 'inner_proxy_wait_status=' "$root/teardown.receipt"
    grep -qF 'sshd_wait_status=' "$root/teardown.receipt"
    grep -qF 'dev_null_unmounted=true' "$root/teardown.receipt"
    grep -qF 'nix_store_unmounted=true' "$root/teardown.receipt"
    rm -rf "$root"
    test ! -e "$root"
    printf '%s\n' \
      'source-transport-v3/git-ssh-v2 hostile two-clone lifecycle accepted after strict teardown'
  '';
in
pkgs.testers.runNixOSTest {
  name = "devkit-source-transport-git-ssh-lifecycle";
  nodes.machine =
    { ... }:
    {
      networking.hosts."127.0.0.1" = [ "ssh.github.com" ];
      services.openssh.enable = true;
      users.groups.git = { };
      users.users.git = {
        isSystemUser = true;
        group = "git";
        home = "/var/lib/git";
        createHome = true;
        shell = "${pkgs.bashInteractive}/bin/bash";
        hashedPassword = "";
      };
      environment.systemPackages = [
        pkgs.coreutils
        pkgs.git
        pkgs.openssh
        transport
      ];
    };
  testScript = ''
    start_all()
    machine.wait_for_unit("multi-user.target")
    machine.succeed("${lifecycle}")
    machine.succeed("test ! -e /run/devkit-source-transport")
    machine.succeed("test ! -e /var/lib/git/.ssh/authorized_keys")
  '';
}

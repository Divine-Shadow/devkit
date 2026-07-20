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
  lifecycle = pkgs.writeShellScript "devkit-source-transport-git-ssh-lifecycle" ''
    set -Eeuo pipefail
    umask 077
    root=/run/devkit-source-transport
    rm -rf "$root"
    mkdir -m0700 -p "$root/home" /run/sshd
    cp '${fixture}/host-key' "$root/host-key"
    chmod 0600 "$root/host-key"
    '${pkgs.openssh}/bin/ssh-keygen' -q -t ed25519 -N "" -f "$root/client-key"
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
      --socket "$root/consumer.sock" \
      --allowlist '${interface.network.allowlistPath}' \
      >"$root/inner.log" 2>&1 &
    inner_pid=$!
    cleanup() {
      status=$?
      if test "$status" -ne 0; then
        head -c 16384 "$root/sshd.log" >&2 || true
        head -c 16384 "$root/inner.log" >&2 || true
      fi
      kill "$inner_pid" "$sshd_pid" >/dev/null 2>&1 || true
      wait "$inner_pid" "$sshd_pid" >/dev/null 2>&1 || true
      exit "$status"
    }
    trap cleanup EXIT
    for attempt in $(seq 1 100); do
      test -S "$root/consumer.sock" && break
      sleep 0.05
    done
    test -S "$root/consumer.sock"

    for index in 1 2; do
      destination="$root/clone-$index"
      env -i \
        HOME="$root/home" \
        PATH=/hostile \
        GIT_CONFIG_GLOBAL=/dev/null \
        GIT_CONFIG_NOSYSTEM=1 \
        GIT_SSH='${interface.gitSSH.executablePath}' \
        GIT_SSH_VARIANT=ssh \
        DEVKIT_SOURCE_TRANSPORT_IDENTITY="$root/client-key" \
        DEVKIT_SOURCE_TRANSPORT_SOCKET="$root/consumer.sock" \
        '${pkgs.git}/bin/git' clone \
          'ssh://git@github.com:443/repository.git' "$destination"
      test "$('${pkgs.git}/bin/git' -C "$destination" rev-parse HEAD)" = \
        "$(cat '${fixture}/revision')"
      test -s "$destination/payload.bin"
      rm -rf "$destination"
    done

    test "$(stat -c %a "$root/consumer.sock")" = 600
    test ! -e "$root/generated-git-ssh-wrapper"
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
  '';
}

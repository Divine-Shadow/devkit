{
  pkgs,
  mkPinnedCodex,
  mkProductAdapterPackage,
}:
let
  fixtureRoot = "/run/product-adapter-lifecycle";
  candidateParent = "/var/lib/product-adapter-candidates";
  pinnedCodex = mkPinnedCodex pkgs;
  runtimeRoot = pkgs.runCommand "devkit-product-lifecycle-runtime-root" { } ''
    mkdir -p "$out"
  '';
  runtimeLauncher = pkgs.writeShellScript "devkit-product-runtime-launcher" ''
    exec "$@"
  '';
  brokerExecutable = pkgs.writeShellScript "devkit-product-fixture-broker" ''
    exit 0
  '';
  firstExecutable = pkgs.writeShellScript "devkit-product-first-executable" ''
    test "$#" -eq 1
    printf %s "$1"
  '';
  allowlist = pkgs.writeText "devkit-product-egress-allowlist" ''
    ssh.github.com
  '';
  codexConfig = pkgs.writeText "devkit-product-codex-config" ''
    model = "gpt-5.5"
    model_provider = "openai"
  '';
  governanceEnv = pkgs.writeText "devkit-product-governance-env" ''
    GOVERNANCE_MODE=source-derived
  '';
  governanceRepoConfig = pkgs.writeText "devkit-product-governance-repo-config" ''
    {}
  '';
  governanceRules = pkgs.writeText "devkit-product-governance-rules" ''
    prefix_rule(pattern=["git"], decision="allow")
  '';
  shellHook = pkgs.writeText "devkit-product-shell-hook" ''
    export DEVKIT_PRODUCT_RUNTIME=source-derived
  '';
  resolvConf = pkgs.writeText "devkit-product-resolv-conf" ''
    nameserver 192.0.2.53
  '';

  # The fixture host key and Product repository are deterministic test
  # authority. No private key is tracked in source. The host key is copied to
  # tmpfs with mode 0600 before sshd starts; the client identity is generated
  # afresh in that same tmpfs.
  fixtureSource = pkgs.runCommand "devkit-product-lifecycle-ssh-source" {
    nativeBuildInputs = [
      pkgs.git
      pkgs.openssh
    ];
  } ''
    set -euo pipefail
    umask 077
    mkdir -p "$out"
    ssh-keygen -q -t ed25519 -N "" -f "$TMPDIR/host-key"
    cp "$TMPDIR/host-key" "$out/host-key"
    cp "$TMPDIR/host-key.pub" "$out/host-key.pub"
    host_fields="$(awk '{print $1 " " $2}' "$TMPDIR/host-key.pub")"
    printf '[ssh.github.com]:443 %s\n' "$host_fields" > "$out/known_hosts"

    seed="$TMPDIR/seed"
    git init --initial-branch=main "$seed"
    git -C "$seed" config user.name "Product lifecycle fixture"
    git -C "$seed" config user.email "fixture@example.invalid"
    printf 'product lifecycle fixture\n' > "$seed/README.md"
    git -C "$seed" add README.md
    GIT_AUTHOR_DATE='2000-01-01T00:00:00Z' \
      GIT_COMMITTER_DATE='2000-01-01T00:00:00Z' \
      git -C "$seed" commit -m fixture
    git init --bare --initial-branch=main "$out/ouroboros-ide.git"
    git -C "$seed" push "$out/ouroboros-ide.git" main:main
    git -C "$seed" rev-parse HEAD > "$out/revision"
  '';

  fixtureManifestRelative = "share/devkit-product-adapter-fixture/authority.json";
  fixtureAuthorityLocator = "${placeholder "out"}/${fixtureManifestRelative}";
  fixturePostFixup = ''
    set -eo pipefail
    manifest="$out/${fixtureManifestRelative}"
    mkdir -p "$(dirname "$manifest")"
    digest() {
      ${pkgs.coreutils}/bin/sha256sum "$1" | ${pkgs.coreutils}/bin/cut -d' ' -f1
    }
    adapter="$out/bin/product-adapter"
    proxy="$out/bin/product-proxy"
    readiness="$out/bin/product-readiness"
    revision="$(${pkgs.coreutils}/bin/cat ${fixtureSource}/revision)"
    origin="ssh://root@ssh.github.com:443${fixtureSource}/ouroboros-ide.git"
    candidate_a="${candidateParent}/a/slot"
    candidate_b="${candidateParent}/b/slot"
    ${pkgs.jq}/bin/jq -n \
      --arg revision "$revision" \
      --arg adapter "$adapter" \
      --arg proxy "$proxy" \
      --arg git ${pkgs.git}/bin/git \
      --arg env ${pkgs.coreutils}/bin/coreutils \
      --arg ssh ${pkgs.openssh}/bin/ssh \
      --arg known_hosts ${fixtureSource}/known_hosts \
      --arg runtime_launcher ${runtimeLauncher} \
      --arg bubblewrap ${pkgs.bubblewrap}/bin/bwrap \
      --arg broker ${brokerExecutable} \
      --arg runtime_root ${runtimeRoot} \
      --arg allowlist ${allowlist} \
      --arg codex_config ${codexConfig} \
      --arg governance_env ${governanceEnv} \
      --arg governance_repo ${governanceRepoConfig} \
      --arg governance_rules ${governanceRules} \
      --arg shell_hook ${shellHook} \
      --arg codex ${pinnedCodex}/bin/codex \
      --arg readiness "$readiness" \
      --arg first_executable ${firstExecutable} \
      --arg credential_root ${fixtureRoot} \
      --arg origin "$origin" \
      --arg resolv ${resolvConf} \
      --arg candidate_a "$candidate_a" \
      --arg candidate_b "$candidate_b" \
      --arg d_adapter "$(digest "$adapter")" \
      --arg d_proxy "$(digest "$proxy")" \
      --arg d_git "$(digest ${pkgs.git}/bin/git)" \
      --arg d_env "$(digest ${pkgs.coreutils}/bin/coreutils)" \
      --arg d_ssh "$(digest ${pkgs.openssh}/bin/ssh)" \
      --arg d_known "$(digest ${fixtureSource}/known_hosts)" \
      --arg d_launcher "$(digest ${runtimeLauncher})" \
      --arg d_bwrap "$(digest ${pkgs.bubblewrap}/bin/bwrap)" \
      --arg d_broker "$(digest ${brokerExecutable})" \
      --arg d_allowlist "$(digest ${allowlist})" \
      --arg d_codex_config "$(digest ${codexConfig})" \
      --arg d_governance_env "$(digest ${governanceEnv})" \
      --arg d_governance_repo "$(digest ${governanceRepoConfig})" \
      --arg d_governance_rules "$(digest ${governanceRules})" \
      --arg d_shell_hook "$(digest ${shellHook})" \
      --arg d_codex "$(digest ${pinnedCodex}/bin/codex)" \
      --arg d_readiness "$(digest "$readiness")" \
      --arg d_first "$(digest ${firstExecutable})" \
      --arg d_resolv "$(digest ${resolvConf})" \
      '
      def consumer($index; $uid; $candidate):
        ($candidate + "/agent" + ($index|tostring)) as $agent |
        ($agent + "/ouroboros-ide") as $worktree |
        ($agent + "/.devhome-agent" + ($index|tostring)) as $home |
        ($candidate + "/state") as $state |
        ($credential_root + "/consumer" + ($index|tostring)) as $credentials |
        {
          index: $index,
          uid: $uid,
          candidateRoot: $candidate,
          agentRoot: $agent,
          worktreePath: $worktree,
          commonDirPath: ($agent + "/.devkit/git/ouroboros-ide.git"),
          homePath: $home,
          stateRoot: $state,
          receiptPath: ($state + "/product-construction-receipt.json"),
          proxySocketPath: ($state + "/product-egress.sock"),
          brokerSocketPath: ($state + "/postgres.sock"),
          sandboxWorktreePath: "/workspaces/dev",
          sandboxHomePath: "/home/product",
          sandboxStateRoot: "/agent-state/product",
          sandboxProxySocketPath: "/agent-state/product/product-egress.sock",
          sandboxBrokerSocketPath: "/agent-state/product/postgres.sock",
          governanceEnvTarget: ($state + "/governance.env"),
          governanceRepoConfigTarget: ($state + "/governance-repo.json"),
          governanceStateRoot: ($state + "/governance"),
          sshIdentityPath: ($credentials + "/client-key"),
          sshPublicKeyPath: ($credentials + "/client-key-public"),
          codexAuthPath: ($credentials + "/codex-auth.json"),
          binds: [
            {source:"/nix/store",target:"/nix/store",mode:"ro",required:true},
            {source:$worktree,target:"/workspaces/dev",mode:"rw",required:true},
            {source:$home,target:"/home/product",mode:"rw",required:true},
            {source:$state,target:"/agent-state/product",mode:"rw",required:true}
          ]
        };
      {
        schemaVersion:"wsl-nix-dev-all-runtime-authority/v1",
        sources:{product:{rev:$revision}},
        devkitProductAdapter:{
          schemaVersion:"wsl-nix-devkit-product-adapter/v1",
          executablePath:$adapter,
          proxyHelperPath:$proxy,
          gitPath:$git,
          envPath:$env,
          sshPath:$ssh,
          knownHostsPath:$known_hosts,
          runtimeLauncherPath:$runtime_launcher,
          bubblewrapPath:$bubblewrap,
          brokerPath:$broker,
          runtimeRoot:$runtime_root,
          egressAllowlistPath:$allowlist,
          codexConfigPath:$codex_config,
          governanceEnvPath:$governance_env,
          governanceRepoConfigPath:$governance_repo,
          governanceRulesPath:$governance_rules,
          shellHookPath:$shell_hook,
          codexExecutablePath:$codex,
          readinessExecutablePath:$readiness,
          firstExecutablePath:$first_executable,
          productOrigin:$origin,
          count:2,
          baseBranch:"main",
          branchPrefix:"agent",
          upstreamProxyUrl:"http://127.0.0.1:18080",
          resolvConfPath:$resolv,
          nscdSocketPath:"",
          mountPolicyIdentity:"devkit/workspace-egress/v3",
          runtimeEnvironment:{},
          consumers:[consumer(1;2001;$candidate_a),consumer(2;2002;$candidate_b)],
          artifactDigests:{
            adapter:$d_adapter,proxy_helper:$d_proxy,git:$d_git,env:$d_env,
            ssh:$d_ssh,known_hosts:$d_known,runtime_launcher:$d_launcher,
            bubblewrap:$d_bwrap,broker:$d_broker,egress_allowlist:$d_allowlist,
            codex_config:$d_codex_config,governance_env:$d_governance_env,
            governance_repo:$d_governance_repo,governance_rules:$d_governance_rules,
            shell_hook:$d_shell_hook,codex_executable:$d_codex,
            readiness_executable:$d_readiness,first_executable:$d_first,
            resolv_conf:$d_resolv
          }
        }
      }' > "$manifest"
  '';

  fixtureAdapter = mkProductAdapterPackage {
    inherit
      codexConfig
      fixtureAuthorityLocator
      fixturePostFixup
      governanceEnv
      governanceRepoConfig
      governanceRules
      pkgs
      runtimeLauncher
      runtimeRoot
      shellHook
      ;
    egressAllowlist = allowlist;
    bubblewrap = "${pkgs.bubblewrap}/bin/bwrap";
    broker = brokerExecutable;
    codexExecutable = "${pinnedCodex}/bin/codex";
    inherit firstExecutable;
    knownHostsFile = "${fixtureSource}/known_hosts";
    tags = [ "devkitintegration" ];
  };

  productionAdapter = mkProductAdapterPackage {
    inherit
      codexConfig
      governanceEnv
      governanceRepoConfig
      governanceRules
      pkgs
      runtimeLauncher
      runtimeRoot
      shellHook
      ;
    egressAllowlist = allowlist;
    bubblewrap = "${pkgs.bubblewrap}/bin/bwrap";
    broker = brokerExecutable;
    codexExecutable = "${pinnedCodex}/bin/codex";
    inherit firstExecutable;
  };
  productionContract = pkgs.runCommand "devkit-product-adapter-production-contract" {
    nativeBuildInputs = [
      pkgs.binutils
      pkgs.gnugrep
    ];
  } ''
    set -euo pipefail
    adapter=${productionAdapter}/bin/product-adapter
    proxy=${productionAdapter}/bin/product-proxy
    grep -aqF '/etc/fleet/dev-all-runtime-bundle/authority.json' "$adapter"
    grep -aqF -- '--coreutils-prog=env' "$adapter"
    ! grep -aqF '${fixtureManifestRelative}' "$adapter"
    ! grep -aqF 'testAuthorityLocator' "$adapter"
    for forbidden in \
      'git+file:///workspaces/dev/ouroboros-ide' \
      'DEVKIT_GOVERNANCE_' \
      '#dev-all-runtime-bundle'
    do
      ! grep -aF "$forbidden" "$adapter" "$proxy"
    done
    for executable in \
      ${fixtureAdapter}/bin/product-adapter \
      ${fixtureAdapter}/bin/product-proxy \
      ${fixtureAdapter}/bin/product-readiness \
      ${pkgs.git}/bin/git \
      ${pkgs.coreutils}/bin/coreutils \
      ${pkgs.openssh}/bin/ssh \
      ${runtimeLauncher} \
      ${pkgs.bubblewrap}/bin/bwrap \
      ${brokerExecutable} \
      ${pinnedCodex}/bin/codex \
      ${firstExecutable}
    do
      test -f "$executable"
      test -x "$executable"
      test ! -L "$executable"
      test "$(readlink -f "$executable")" = "$executable"
    done
    mkdir -p "$out"
    printf '%s\n' \
      'production adapter contains only the canonical /etc authority locator' \
      'production adapter contains no integration locator or Product revision authority' \
      'WSL owns environment.etc and current-generation same-file proof' \
      > "$out/contract"
  '';

  proxyPeer = pkgs.writeShellScript "devkit-product-connect-peer" ''
    exec ${pkgs.python3}/bin/python3 ${./product-adapter-connect-proxy.py} "$@"
  '';
  lifecycle = pkgs.writeShellScript "devkit-product-installed-lifecycle" ''
    set -Eeuo pipefail
    umask 077
    rm -rf ${fixtureRoot} ${candidateParent}
    mkdir -p ${fixtureRoot} ${candidateParent}/a ${candidateParent}/b
    chmod 0711 ${fixtureRoot} ${candidateParent}
    chmod 0700 ${candidateParent}/a ${candidateParent}/b
    chown product1:product1 ${candidateParent}/a
    chown product2:product2 ${candidateParent}/b
    report_failure() {
      result=$?
      for file in \
        ${fixtureRoot}/prepare-*.stderr \
        ${fixtureRoot}/exec-*.stderr \
        ${fixtureRoot}/sshd.log \
        ${fixtureRoot}/proxy.log
      do
        test -f "$file" || continue
        printf '%s\n' "----- $file"
        ${pkgs.coreutils}/bin/head -c 16384 "$file"
        printf '\n'
      done
      exit "$result"
    }
    trap report_failure ERR

    : > ${fixtureRoot}/hostile-used
    chmod 0666 ${fixtureRoot}/hostile-used
    printf '%s\n' 'printf x >> ${fixtureRoot}/hostile-used' > ${fixtureRoot}/hostile-bash-env
    chmod 0644 ${fixtureRoot}/hostile-bash-env
    printf '#!${pkgs.bash}/bin/bash\nprintf x >> %s\nexit 97\n' \
      ${fixtureRoot}/hostile-used > ${fixtureRoot}/hostile-ssh
    chmod 0755 ${fixtureRoot}/hostile-ssh

    cp ${fixtureSource}/host-key ${fixtureRoot}/host-key
    chmod 0600 ${fixtureRoot}/host-key
    : > ${fixtureRoot}/authorized_keys
    for index in 1 2; do
      credentials=${fixtureRoot}/consumer$index
      mkdir "$credentials"
      ${pkgs.openssh}/bin/ssh-keygen -q -t ed25519 -N "" -f "$credentials/client-key"
      cp "$credentials/client-key.pub" "$credentials/client-key-public"
      printf '{}\n' > "$credentials/codex-auth.json"
      chmod 0600 \
        "$credentials/client-key" \
        "$credentials/client-key-public" \
        "$credentials/codex-auth.json"
      chown -R product$index:product$index "$credentials"
    done

    cat > ${fixtureRoot}/upload-pack <<EOF
#!${pkgs.bash}/bin/bash
set -eu
test "\''${SSH_ORIGINAL_COMMAND:-}" = "git-upload-pack '${fixtureSource}/ouroboros-ide.git'" || exit 91
exec ${pkgs.git}/bin/git-upload-pack '${fixtureSource}/ouroboros-ide.git'
EOF
    chmod 0700 ${fixtureRoot}/upload-pack
    for index in 1 2; do
      printf 'restrict,command="%s" %s\n' \
        ${fixtureRoot}/upload-pack \
        "$(${pkgs.coreutils}/bin/cat ${fixtureRoot}/consumer$index/client-key-public)" \
        >> ${fixtureRoot}/authorized_keys
    done
    cat > ${fixtureRoot}/sshd_config <<EOF
ListenAddress 127.0.0.1
Port 2222
PidFile ${fixtureRoot}/sshd.pid
HostKey ${fixtureRoot}/host-key
AuthorizedKeysFile ${fixtureRoot}/authorized_keys
StrictModes no
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
UsePAM no
EOF
    ${pkgs.openssh}/bin/sshd -D -e -f ${fixtureRoot}/sshd_config \
      >${fixtureRoot}/sshd.log 2>&1 &
    sshd_pid=$!
    ${proxyPeer} 18080 2222 >${fixtureRoot}/proxy.log 2>&1 &
    outer_proxy_pid=$!
    cleanup() {
      kill "$outer_proxy_pid" "$sshd_pid" 2>/dev/null || true
      wait "$outer_proxy_pid" 2>/dev/null || true
      wait "$sshd_pid" 2>/dev/null || true
    }
    trap cleanup EXIT
    for port in 2222 18080; do
      for attempt in $(${pkgs.coreutils}/bin/seq 1 100); do
        ${pkgs.netcat-openbsd}/bin/nc -z 127.0.0.1 "$port" && break
        ${pkgs.coreutils}/bin/sleep 0.05
      done
      ${pkgs.netcat-openbsd}/bin/nc -z 127.0.0.1 "$port"
    done

    adapter=${fixtureAdapter}/bin/product-adapter
    revision="$(${pkgs.coreutils}/bin/cat ${fixtureSource}/revision)"
    test -f ${fixtureAdapter}/${fixtureManifestRelative}
    if ${fixtureAdapter}/bin/product-proxy supervise \
      >${fixtureRoot}/direct-supervise.stdout \
      2>${fixtureRoot}/direct-supervise.stderr; then
      echo "Product proxy accepted direct supervise use" >&2
      exit 1
    fi
    ${pkgs.gnugrep}/bin/grep -q 'one-shot adapter capability' \
      ${fixtureRoot}/direct-supervise.stderr
    for index in 1 2; do
      stdout=${fixtureRoot}/prepare-$index.json
      stderr=${fixtureRoot}/prepare-$index.stderr
      ${pkgs.util-linux}/bin/runuser -u product$index -- \
      ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i \
        PATH=/nonexistent \
        BASH_ENV=${fixtureRoot}/hostile-bash-env \
        HOME=${fixtureRoot}/hostile-home \
        XDG_CONFIG_HOME=${fixtureRoot}/hostile-xdg \
        GIT_CONFIG_COUNT=1 \
        GIT_CONFIG_KEY_0=core.sshCommand \
        GIT_CONFIG_VALUE_0=${fixtureRoot}/hostile-ssh \
        GIT_DIR=${fixtureRoot}/hostile-git-dir \
        GIT_WORK_TREE=${fixtureRoot}/hostile-worktree \
        GIT_COMMON_DIR=${fixtureRoot}/hostile-common \
        GIT_OBJECT_DIRECTORY=${fixtureRoot}/hostile-objects \
        GIT_ALTERNATE_OBJECT_DIRECTORIES=${fixtureRoot}/hostile-alternates \
        GIT_INDEX_FILE=${fixtureRoot}/hostile-index \
        GIT_SSH=${fixtureRoot}/hostile-ssh \
        GIT_SSH_COMMAND=${fixtureRoot}/hostile-ssh \
        "$adapter" prepare --count 2 --index "$index" >"$stdout" 2>"$stderr"
      ${pkgs.jq}/bin/jq -e '
        .schema_version == "devkit/product-construction-receipt/v1" and
        .status == "ready" and
        .proxy_cleanup == "absent" and
        .readiness_runtime == true and
        .readiness_repository == true and
        (.credential_handle_digests | length) == 3 and
        (.credential_handle_digests[] | length) == 64 and
        any(.readiness_evidence[];
          .name == "codex-app-server-thread-and-standalone-executable" and
          .status == "ready" and
          .result.schema_version == "devkit/product-codex-app-server-thread-readiness/v1" and
          .result.app_server_alive == true and
          .result.initialize_ready == true and
          .result.mcp_status_read == true and
          .result.ephemeral_thread_created == true and
          .result.ephemeral_thread_read_back == true and
          .result.approval_policy == "never" and
          .result.sandbox_policy == "dangerFullAccess" and
          .result.standalone_executable_exit == 0
        )
      ' "$stdout" >/dev/null
      ${pkgs.util-linux}/bin/runuser -u product$index -- \
      ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i \
        PATH=/nonexistent \
        BASH_ENV=${fixtureRoot}/hostile-bash-env \
        HOME=${fixtureRoot}/hostile-home \
        GIT_CONFIG_COUNT=1 \
        GIT_CONFIG_KEY_0=core.sshCommand \
        GIT_CONFIG_VALUE_0=${fixtureRoot}/hostile-ssh \
        "$adapter" exec --count 2 --index "$index" -- ${pkgs.coreutils}/bin/true \
        >${fixtureRoot}/exec-$index.stdout 2>${fixtureRoot}/exec-$index.stderr
      candidate=${candidateParent}/$([ "$index" = 1 ] && printf a || printf b)/slot
      test ! -e "$candidate/state/product-egress.sock"
      test "$(${pkgs.coreutils}/bin/stat -c %u "$candidate/agent$index")" = "$((2000 + index))"
      test "$(${pkgs.coreutils}/bin/stat -c %u "$candidate/state/product-construction-receipt.json")" = "$((2000 + index))"
      observed_revision="$(
        ${pkgs.util-linux}/bin/runuser -u product$index -- \
        ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i \
          GIT_CONFIG_GLOBAL=/dev/null \
          GIT_CONFIG_NOSYSTEM=1 \
          ${pkgs.git}/bin/git --no-optional-locks \
          -C "$candidate/agent$index/ouroboros-ide" \
          -c core.fsmonitor=false \
          -c core.hooksPath=/dev/null \
          rev-parse HEAD
      )"
      test "$observed_revision" = "$revision"
      gitdir="$(sed -n 's/^gitdir: //p' "$candidate/agent$index/ouroboros-ide/.git")"
      test -n "$gitdir"
      test "''${gitdir#/}" = "$gitdir"
      ${pkgs.util-linux}/bin/runuser -u product$index -- \
        ${pkgs.coreutils}/bin/coreutils --coreutils-prog=test \
        -w "$candidate/agent$index/ouroboros-ide/.git"
      cp "$candidate/state/product-construction-receipt.json" ${fixtureRoot}/receipt-$index.json
      rm -rf "$candidate"
      test ! -e "$candidate"
    done
    test "$(${pkgs.jq}/bin/jq -r .artifacts.ssh_identity ${fixtureRoot}/receipt-1.json)" != \
      "$(${pkgs.jq}/bin/jq -r .artifacts.ssh_identity ${fixtureRoot}/receipt-2.json)"
    test "$(${pkgs.jq}/bin/jq -r .artifacts.codex_auth ${fixtureRoot}/receipt-1.json)" != \
      "$(${pkgs.jq}/bin/jq -r .artifacts.codex_auth ${fixtureRoot}/receipt-2.json)"
    test "$(${pkgs.jq}/bin/jq -r .credential_handle_digests.ssh_identity ${fixtureRoot}/receipt-1.json)" != \
      "$(${pkgs.jq}/bin/jq -r .credential_handle_digests.ssh_identity ${fixtureRoot}/receipt-2.json)"
    ! ${pkgs.gnugrep}/bin/grep -q 'OPENSSH PRIVATE KEY' \
      ${fixtureRoot}/receipt-1.json ${fixtureRoot}/receipt-2.json
    test ! -s ${fixtureRoot}/hostile-used
    test -z "$(${pkgs.procps}/bin/pgrep -f '${fixtureAdapter}/bin/product-(proxy|readiness)' || true)"
    test -z "$(${pkgs.procps}/bin/pgrep -f '${pinnedCodex}/bin/codex app-server' || true)"
    cleanup
    trap - EXIT
    trap - ERR
    rm -rf ${candidateParent} ${fixtureRoot}
    test ! -e ${candidateParent}
    test ! -e ${fixtureRoot}
  '';
in
pkgs.testers.runNixOSTest {
  name = "product-fresh-consumer-ssh-authority";
  nodes.machine =
    { ... }:
    {
      virtualisation.memorySize = 6144;
      virtualisation.cores = 4;
      users.groups.sshd = { };
      users.users.sshd = {
        isSystemUser = true;
        group = "sshd";
      };
      users.groups.product1.gid = 2001;
      users.groups.product2.gid = 2002;
      users.users.product1 = {
        isSystemUser = true;
        uid = 2001;
        group = "product1";
      };
      users.users.product2 = {
        isSystemUser = true;
        uid = 2002;
        group = "product2";
      };
      environment.systemPackages = [
        fixtureAdapter
        pkgs.bash
        pkgs.coreutils
      ];
    };
  testScript = ''
    machine.start()
    machine.wait_for_unit("multi-user.target")
    machine.succeed("test -f ${productionContract}/contract")
    machine.succeed("${lifecycle}")
    machine.shutdown()
  '';
}

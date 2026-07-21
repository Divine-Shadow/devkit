{
  pkgs,
  mkDevAllRuntimeBundle,
  mkPinnedCodex,
  mkProductAdapterPackage,
  mkProductConnectFixture,
  mkProductMCPFixture,
  mkProductMountPolicyContract,
	  mkProductSSHSessionContract,
	  mkProductStoppedVolumeSeedContract,
}:
let
  fixtureRoot = "/run/product-adapter-lifecycle";
  candidateParent = "/var/lib/product-adapter-candidates";
  pinnedCodex = mkPinnedCodex pkgs;
  mcpFixture = mkProductMCPFixture pkgs;
	  connectFixture = mkProductConnectFixture pkgs;
	  managedClient = pkgs.buildGoModule {
	    pname = "devkit-product-managed-client";
	    version = "dev";
	    src = ../cli/devctl;
	    modRoot = ".";
	    vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
	    subPackages = [ "cmd/product-managed-client" ];
	    env.CGO_ENABLED = "0";
	  };
	  mcpRequirement = pkgs.writeText "devkit-product-mcp-requirement.json" (
	    builtins.toJSON {
	      schemaVersion = "devkit/product-mcp-requirement/v1";
	      servers = [
	        {
	          name = "governance";
	          tools = [ "get_run_status" "run" ];
	        }
	      ];
	    }
	  );
	  mountPolicyContract = mkProductMountPolicyContract pkgs;
	  sshSessionContract = mkProductSSHSessionContract pkgs;
	  stoppedVolumeSeedContract = mkProductStoppedVolumeSeedContract pkgs;
  lifecycleProductRevision = "7c49b072973e6ea3ced9515352c79cfbd915754e";
  runtimeBundleFixture =
    import ./dev-all-runtime-bundle-fixture.nix {
      inherit pkgs;
      productSourceRev = lifecycleProductRevision;
    };
  runtimeBundle =
    mkDevAllRuntimeBundle ({ inherit pkgs; } // runtimeBundleFixture.constructorArgs);
  brokerExecutable = pkgs.writeShellScript "devkit-product-fixture-broker" ''
    exit 0
  '';
  allowlist = pkgs.writeText "devkit-product-egress-allowlist" ''
    ssh.github.com
  '';
  codexConfig = pkgs.writeText "devkit-product-codex-config" ''
    model = "gpt-5.5"
    model_provider = "openai"

    # Connector discovery belongs to the real Desktop promotion consumer.
    # This diagnostic has no Desktop connector catalog, so keep the fixture's
    # immutable MCP requirement limited to the packaged governance server it
    # can initialize and verify end to end.
    [features]
    apps = false

	[mcp_servers.governance]
	command = "${mcpFixture}/bin/product-mcp-fixture"
	startup_timeout_sec = 30
	tool_timeout_sec = 30
  '';
  governanceEnv =
    "${runtimeBundle}/share/dev-all-runtime-bundle/identity.env";
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
    test "$(git -C "$seed" rev-parse HEAD)" = '${lifecycleProductRevision}'
    printf '%s\n' '${lifecycleProductRevision}' > "$out/revision"
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
	runtime_exec="$out/bin/product-runtime-exec"
	    supervisor="$out/bin/product-adapter-supervisor"
	    ssh_session="$out/bin/product-ssh-session"
	    ssh_setup="$out/bin/product-ssh-setup"
	    ssh_session_contract='${sshSessionContract}'
	    ssh_setup_contract='${stoppedVolumeSeedContract}'
    revision="$(${pkgs.coreutils}/bin/cat ${fixtureSource}/revision)"
	  mount_policy_identity="$(${pkgs.jq}/bin/jq -er .identity ${mountPolicyContract})"
    origin="ssh://root@ssh.github.com:443${fixtureSource}/ouroboros-ide.git"
    candidate_a="${candidateParent}/a/slot"
    candidate_b="${candidateParent}/b/slot"
	    ${pkgs.jq}/bin/jq -n \
	      --slurpfile base ${runtimeBundle}/share/dev-all-runtime-bundle/identity.json \
      --arg revision "$revision" \
      --arg adapter "$adapter" \
      --arg proxy "$proxy" \
      --arg git ${pkgs.git}/bin/git \
      --arg env ${pkgs.coreutils}/bin/coreutils \
      --arg ssh ${pkgs.openssh}/bin/ssh \
	  --arg ssh_keygen ${pkgs.openssh}/bin/ssh-keygen \
      --arg known_hosts ${fixtureSource}/known_hosts \
      --arg runtime_launcher "$runtime_exec" \
      --arg bubblewrap ${pkgs.bubblewrap}/bin/bwrap \
      --arg broker ${brokerExecutable} \
      --arg allowlist ${allowlist} \
      --arg codex_config ${codexConfig} \
      --arg governance_env ${governanceEnv} \
      --arg governance_repo ${governanceRepoConfig} \
      --arg governance_rules ${governanceRules} \
      --arg shell_hook ${shellHook} \
      --arg codex ${pinnedCodex}/bin/codex \
      --arg readiness "$readiness" \
	  --arg mcp_requirement ${mcpRequirement} \
      --arg mount_policy_contract ${mountPolicyContract} \
	  --arg supervisor "$supervisor" \
	  --arg ssh_session "$ssh_session" \
	  --arg ssh_session_contract "$ssh_session_contract" \
	  --arg ssh_setup "$ssh_setup" \
	  --arg ssh_setup_contract "$ssh_setup_contract" \
	  --arg mount_policy_identity "$mount_policy_identity" \
	  --arg supervisor_root ${fixtureRoot} \
      --arg origin "$origin" \
      --arg resolv ${resolvConf} \
      --arg candidate_a "$candidate_a" \
      --arg candidate_b "$candidate_b" \
      --arg d_adapter "$(digest "$adapter")" \
      --arg d_proxy "$(digest "$proxy")" \
      --arg d_git "$(digest ${pkgs.git}/bin/git)" \
      --arg d_env "$(digest ${pkgs.coreutils}/bin/coreutils)" \
      --arg d_ssh "$(digest ${pkgs.openssh}/bin/ssh)" \
	  --arg d_ssh_keygen "$(digest ${pkgs.openssh}/bin/ssh-keygen)" \
      --arg d_known "$(digest ${fixtureSource}/known_hosts)" \
      --arg d_launcher "$(digest "$runtime_exec")" \
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
	  --arg d_mcp_requirement "$(digest ${mcpRequirement})" \
	  --arg d_mount_policy_contract "$(digest ${mountPolicyContract})" \
	  --arg d_supervisor "$(digest "$supervisor")" \
	  --arg d_ssh_session "$(digest "$ssh_session")" \
	  --arg d_ssh_session_contract "$(digest "$ssh_session_contract")" \
	  --arg d_ssh_setup "$(digest "$ssh_setup")" \
	  --arg d_ssh_setup_contract "$(digest "$ssh_setup_contract")" \
      --arg d_resolv "$(digest ${resolvConf})" \
      '
	  def consumer($index; $uid; $candidate):
	        ($candidate + "/agent" + ($index|tostring)) as $agent |
        ($agent + "/ouroboros-ide") as $worktree |
	        ($candidate + "/home") as $home |
        ($candidate + "/state") as $state |
        {
          index: $index,
          uid: $uid,
	      gid: $uid,
          candidateRoot: $candidate,
          agentRoot: $agent,
          worktreePath: $worktree,
          commonDirPath: ($agent + "/.devkit/git/ouroboros-ide.git"),
          homePath: $home,
          stateRoot: $state,
          receiptPath: ($state + "/product-construction-receipt.json"),
          proxySocketPath: ($state + "/product-egress.sock"),
          brokerSocketPath: ($state + "/postgres.sock"),
		  supervisorSocketPath: ($supervisor_root + "/control-" + ($index|tostring) + "/product-supervisor.sock"),
		  appServerSocketPath: ($state + "/app-server.sock"),
          sandboxWorktreePath: "/workspaces/dev",
          sandboxHomePath: "/home/product",
          sandboxStateRoot: "/agent-state/product",
          sandboxProxySocketPath: "/agent-state/product/product-egress.sock",
          sandboxBrokerSocketPath: "/agent-state/product/postgres.sock",
		  sandboxAppServerSocketPath: "/agent-state/product/app-server.sock",
          governanceEnvTarget: ($state + "/governance.env"),
          governanceRepoConfigTarget: ($state + "/governance-repo.json"),
          governanceStateRoot: ($state + "/governance"),
	      sshIdentityPath: ($home + "/.ssh/id_ed25519"),
	      sshPublicKeyPath: ($home + "/.ssh/id_ed25519.pub"),
	      codexAuthPath: ($home + "/.codex/auth.json"),
	      authorizedKeysPath: ($home + "/.ssh/authorized_keys"),
          binds: [
            {source:"/nix/store",target:"/nix/store",mode:"ro",required:true},
            {source:$worktree,target:"/workspaces/dev",mode:"rw",required:true},
            {source:$home,target:"/home/product",mode:"rw",required:true},
            {source:$state,target:"/agent-state/product",mode:"rw",required:true}
          ]
        };
	      $base[0] |
	      .devkitProductAdapter = {
          schemaVersion:"wsl-nix-devkit-product-adapter/v1",
          executablePath:$adapter,
          proxyHelperPath:$proxy,
          gitPath:$git,
          envPath:$env,
          sshPath:$ssh,
	      sshKeygenPath:$ssh_keygen,
          knownHostsPath:$known_hosts,
          runtimeLauncherPath:$runtime_launcher,
          bubblewrapPath:$bubblewrap,
          brokerPath:$broker,
          egressAllowlistPath:$allowlist,
          codexConfigPath:$codex_config,
          governanceEnvPath:$governance_env,
          governanceRepoConfigPath:$governance_repo,
          governanceRulesPath:$governance_rules,
          shellHookPath:$shell_hook,
          codexExecutablePath:$codex,
          readinessExecutablePath:$readiness,
			  mcpRequirementPath:$mcp_requirement,
			  mountPolicyContractPath:$mount_policy_contract,
			  supervisorExecutablePath:$supervisor,
				  sshSessionExecutablePath:$ssh_session,
				  sshSessionContractPath:$ssh_session_contract,
		  sshSetupExecutablePath:$ssh_setup,
		  sshSetupContractPath:$ssh_setup_contract,
          productOrigin:$origin,
	      controllerCredentialOwnerUid:1000,
          count:2,
          baseBranch:"main",
          branchPrefix:"agent",
          upstreamProxyUrl:"http://127.0.0.1:18080",
          resolvConfPath:$resolv,
          nscdSocketPath:"",
		  mountPolicyIdentity:$mount_policy_identity,
          runtimeEnvironment:{},
          consumers:[consumer(1;2001;$candidate_a),consumer(2;2002;$candidate_b)],
          artifactDigests:{
            adapter:$d_adapter,proxy_helper:$d_proxy,git:$d_git,env:$d_env,
	        ssh:$d_ssh,ssh_keygen:$d_ssh_keygen,known_hosts:$d_known,runtime_launcher:$d_launcher,
            bubblewrap:$d_bwrap,broker:$d_broker,egress_allowlist:$d_allowlist,
            codex_config:$d_codex_config,governance_env:$d_governance_env,
            governance_repo:$d_governance_repo,governance_rules:$d_governance_rules,
            shell_hook:$d_shell_hook,codex_executable:$d_codex,
			readiness_executable:$d_readiness,
				mcp_requirement:$d_mcp_requirement,
				mount_policy_contract:$d_mount_policy_contract,
					supervisor:$d_supervisor,ssh_session:$d_ssh_session,
					ssh_session_contract:$d_ssh_session_contract,
			ssh_setup:$d_ssh_setup,ssh_setup_contract:$d_ssh_setup_contract,
            resolv_conf:$d_resolv
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
      shellHook
	  mcpRequirement
      ;
    egressAllowlist = allowlist;
    bubblewrap = "${pkgs.bubblewrap}/bin/bwrap";
    broker = brokerExecutable;
    codexExecutable = "${pinnedCodex}/bin/codex";
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
      shellHook
	  mcpRequirement
      ;
    egressAllowlist = allowlist;
    bubblewrap = "${pkgs.bubblewrap}/bin/bwrap";
    broker = brokerExecutable;
    codexExecutable = "${pinnedCodex}/bin/codex";
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
	grep -aqF '/var/lib/product-runtime/authority-selector.json' "$adapter"
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
	  ${fixtureAdapter}/bin/product-adapter-supervisor \
	  ${fixtureAdapter}/bin/product-ssh-session \
	  ${fixtureAdapter}/bin/product-ssh-setup \
	  ${fixtureAdapter}/bin/product-runtime-exec \
      ${pkgs.git}/bin/git \
      ${pkgs.coreutils}/bin/coreutils \
      ${pkgs.openssh}/bin/ssh \
      ${pkgs.bubblewrap}/bin/bwrap \
	  ${brokerExecutable} \
	  ${pinnedCodex}/bin/codex
    do
      test -f "$executable"
      test -x "$executable"
      test ! -L "$executable"
      test "$(readlink -f "$executable")" = "$executable"
    done
    mkdir -p "$out"
    printf '%s\n' \
	  'production adapter consumes only the WSL-owned canonical atomic runtime selector' \
      'production adapter contains no integration locator or Product revision authority' \
      'WSL owns environment.etc and current-generation same-file proof' \
      > "$out/contract"
  '';

  # Diagnostic localization only. Promotion/lifecycle is owned solely by the
  # governed Product Scala app; Fleet supplies narrow primitives and this
  # fixture cannot satisfy Product promotion. It exercises only the complete
  # compiled Devkit consumer boundary.
  lifecycle = pkgs.writeShellScript "devkit-product-consumer-boundary-diagnostic" ''
    set -Eeuo pipefail
    umask 077
    exec 8>&1 9>&2
    rm -rf ${fixtureRoot} ${candidateParent}
    mkdir -p ${fixtureRoot} ${candidateParent}/a ${candidateParent}/b
    chmod 0711 ${fixtureRoot} ${candidateParent}
	chown 2001:2001 ${candidateParent}/a
	chown 2002:2002 ${candidateParent}/b
	chmod 0700 ${candidateParent}/a ${candidateParent}/b
    supervisor_pid=""
    lookalike_pid=""
    sshd_pid=""
    outer_proxy_pid=""
    phase="initialize"
    cleanup() {
      test -z "$lookalike_pid" || kill "$lookalike_pid" 2>/dev/null || true
      test -z "$supervisor_pid" || kill "$supervisor_pid" 2>/dev/null || true
      test -z "$outer_proxy_pid" || kill "$outer_proxy_pid" 2>/dev/null || true
      test -z "$sshd_pid" || kill "$sshd_pid" 2>/dev/null || true
      test -z "$lookalike_pid" || wait "$lookalike_pid" 2>/dev/null || true
      test -z "$supervisor_pid" || wait "$supervisor_pid" 2>/dev/null || true
      test -z "$outer_proxy_pid" || wait "$outer_proxy_pid" 2>/dev/null || true
      test -z "$sshd_pid" || wait "$sshd_pid" 2>/dev/null || true
    }
    report_failure() {
      result="$1"
      line="$2"
      command="$3"
      trap - ERR EXIT
      printf '%s\n' \
        "diagnostic lifecycle failed: phase=$phase line=$line command=$command exit=$result" \
        >&9
      cleanup
	      for file in ${fixtureRoot}/*.log ${fixtureRoot}/*.stderr ${fixtureRoot}/status-*.json ${candidateParent}/*/slot/state/product-construction-receipt.json; do
        test -f "$file" || continue
        printf '%s\n' "----- $file" >&9
        ${pkgs.coreutils}/bin/head -c 32768 "$file" >&9
        printf '\n' >&9
      done
      exit "$result"
    }
    trap cleanup EXIT
    trap 'report_failure "$?" "$LINENO" "$BASH_COMMAND"' ERR

    phase="prepare diagnostic ssh transport"
    cp ${fixtureSource}/host-key ${fixtureRoot}/host-key
    chmod 0600 ${fixtureRoot}/host-key
    host_fields="$(${pkgs.gawk}/bin/awk '{print $1 " " $2}' ${fixtureSource}/host-key.pub)"
    printf '[127.0.0.1]:2222 %s\n' "$host_fields" > ${fixtureRoot}/client-known-hosts
    chown controller:controller ${fixtureRoot}/client-known-hosts
    chmod 0600 ${fixtureRoot}/client-known-hosts
    mkdir ${fixtureRoot}/controller-home
    chown controller:controller ${fixtureRoot}/controller-home

    : > ${fixtureRoot}/root-authorized-keys
    for index in 1 2; do
      phase="consumer-$index offline seed"
      source_dir=${fixtureRoot}/source-$index
      mkdir "$source_dir"
      ${pkgs.openssh}/bin/ssh-keygen -q -t ed25519 -N "" -f "$source_dir/id_ed25519"
      chmod 0600 "$source_dir/id_ed25519" "$source_dir/id_ed25519.pub"
      chown -R controller:controller "$source_dir"
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
        "$(${pkgs.coreutils}/bin/cat ${fixtureRoot}/source-$index/id_ed25519.pub)" \
        >> ${fixtureRoot}/root-authorized-keys
    done
    chmod 0600 ${fixtureRoot}/root-authorized-keys

    cat > ${fixtureRoot}/sshd_config <<EOF
ListenAddress 127.0.0.1
Port 2222
PidFile ${fixtureRoot}/sshd.pid
HostKey ${fixtureRoot}/host-key
AuthorizedKeysFile none
StrictModes no
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
PermitRootLogin prohibit-password
AllowUsers root product1 product2
LogLevel VERBOSE
Match User root
  AuthorizedKeysFile ${fixtureRoot}/root-authorized-keys
Match User product1
  AuthorizedKeysFile ${candidateParent}/a/slot/home/.ssh/authorized_keys
  ForceCommand ${fixtureAdapter}/bin/product-ssh-session force-command --count 2 --index 1
Match User product2
  AuthorizedKeysFile ${candidateParent}/b/slot/home/.ssh/authorized_keys
  ForceCommand ${fixtureAdapter}/bin/product-ssh-session force-command --count 2 --index 2
EOF
    ${pkgs.openssh}/bin/sshd -D -e -f ${fixtureRoot}/sshd_config \
      >${fixtureRoot}/sshd.log 2>&1 &
    sshd_pid=$!
    ${connectFixture}/bin/product-connect-fixture serve \
      --listen 127.0.0.1:18080 --upstream 127.0.0.1:2222 \
      >${fixtureRoot}/proxy.log 2>&1 &
    outer_proxy_pid=$!
    for port in 2222 18080; do
      for attempt in $(${pkgs.coreutils}/bin/seq 1 100); do
        ${pkgs.netcat-openbsd}/bin/nc -z 127.0.0.1 "$port" && break
        ${pkgs.coreutils}/bin/sleep 0.05
      done
      ${pkgs.netcat-openbsd}/bin/nc -z 127.0.0.1 "$port"
    done

    revision="$(${pkgs.coreutils}/bin/cat ${fixtureSource}/revision)"
    test -f ${fixtureAdapter}/${fixtureManifestRelative}
    ${pkgs.jq}/bin/jq -e \
      --arg launcher '${fixtureAdapter}/bin/product-runtime-exec' \
      --arg governance '${runtimeBundle}/share/dev-all-runtime-bundle/identity.env' \
      '.devkitProductAdapter.runtimeLauncherPath == $launcher and
       .devkitProductAdapter.controllerCredentialOwnerUid == 1000 and
       .devkitProductAdapter.governanceEnvPath == $governance and
       (.devkitProductAdapter | has("runtimeRoot") | not)' \
      ${fixtureAdapter}/${fixtureManifestRelative} >/dev/null
    if ${fixtureAdapter}/bin/product-proxy supervise \
      >${fixtureRoot}/direct-supervise.stdout \
      2>${fixtureRoot}/direct-supervise.stderr; then
      echo "Product proxy accepted direct supervise use" >&2
      exit 1
    fi
    ${pkgs.gnugrep}/bin/grep -q 'one-shot adapter capability' \
      ${fixtureRoot}/direct-supervise.stderr

    ssh_consumer() {
      index="$1"
      shift
      ${pkgs.util-linux}/bin/runuser -u controller -- \
        ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i \
        HOME=${fixtureRoot}/controller-home \
        ${pkgs.openssh}/bin/ssh -T -F /dev/null \
        -o BatchMode=yes -o IdentitiesOnly=yes \
        -o IdentityFile=${fixtureRoot}/source-$index/id_ed25519 \
        -o UserKnownHostsFile=${fixtureRoot}/client-known-hosts \
        -o GlobalKnownHostsFile=/dev/null -o StrictHostKeyChecking=yes \
        -p 2222 product$index@127.0.0.1 "$@"
    }

    for index in 1 2; do
      uid="$((2000 + index))"
      projection=$([ "$index" = 1 ] && printf a || printf b)
      candidate=${candidateParent}/$projection/slot
      mkdir "$candidate"
      chmod 0700 "$candidate"
      (
        cd ${candidateParent}/$projection
        ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i \
          DEVKIT_SOURCE_TRANSPORT_IDENTITY=${fixtureRoot}/source-$index/id_ed25519 \
          ${fixtureAdapter}/bin/product-ssh-setup seed-git \
          --count 2 --index "$index" --root-projection slot
      ) >${fixtureRoot}/seed-$index.json 2>${fixtureRoot}/seed-$index.stderr
      ${pkgs.jq}/bin/jq -e \
        --argjson index "$index" \
        '.schema_version == "devkit/product-stopped-volume-seed/v1" and
         .status == "seeded" and .consumer_index == $index and
         .relative_projection == "slot"' \
        ${fixtureRoot}/seed-$index.json >/dev/null
      ! ${pkgs.gnugrep}/bin/grep -q 'OPENSSH PRIVATE KEY' ${fixtureRoot}/seed-$index.json

      # Non-promoting Fleet-auth diagnostic fixture. The promotion gate uses
      # Fleet's real `auth plan` + confirmed apply into the stopped volume.
      mkdir -p "$candidate/home/.codex"
      printf '{}\n' > "$candidate/home/.codex/auth.json"
      chown -R "$uid:$uid" "$candidate/home/.codex"
      chmod 0700 "$candidate/home/.codex"
      chmod 0600 "$candidate/home/.codex/auth.json"
      mkdir ${fixtureRoot}/control-$index
      chown "$uid:$uid" ${fixtureRoot}/control-$index
      chmod 0700 ${fixtureRoot}/control-$index

      ${pkgs.util-linux}/bin/setpriv \
        --reuid="$uid" --regid="$uid" --clear-groups \
        --inh-caps=-all --ambient-caps=-all -- \
        ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i \
        ${fixtureAdapter}/bin/product-adapter-supervisor \
        serve --count 2 --index "$index" \
        >${fixtureRoot}/supervisor-$index.log 2>&1 &
      supervisor_pid=$!
      for attempt in $(${pkgs.coreutils}/bin/seq 1 100); do
        test -S ${fixtureRoot}/control-$index/product-supervisor.sock && break
        ${pkgs.coreutils}/bin/sleep 0.05
      done
      test -S ${fixtureRoot}/control-$index/product-supervisor.sock

      phase="consumer-$index initial managed prepare"
      ssh_consumer "$index" devkit-product-prepare/v1 \
        >${fixtureRoot}/prepare-$index.json 2>${fixtureRoot}/prepare-$index.stderr
      ${pkgs.jq}/bin/jq -e \
        --argjson index "$index" \
        '.schema_version == "devkit/product-supervisor-response/v1" and
         .command.kind == "prepare" and .status.consumer_index == $index and
         .status.mount_policy_identity == "devkit/workspace-egress/v3" and
         .status.app_server_running == false' \
        ${fixtureRoot}/prepare-$index.json >/dev/null

      if test "$index" = 1; then
        phase="consumer-$index unmanaged lookalike rejection"
        mkdir ${fixtureRoot}/lookalike
        chown "$uid:$uid" ${fixtureRoot}/lookalike
        chmod 0700 ${fixtureRoot}/lookalike
        (
          cd "$candidate/agent$index/ouroboros-ide"
          exec ${pkgs.util-linux}/bin/setpriv \
            --reuid="$uid" --regid="$uid" --clear-groups \
            --inh-caps=-all --ambient-caps=-all -- \
            ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i \
            HOME="$candidate/home" CODEX_HOME="$candidate/home/.codex" \
            ${pinnedCodex}/bin/codex app-server \
            --listen unix://${fixtureRoot}/lookalike/unmanaged.sock
        ) >${fixtureRoot}/lookalike.log 2>&1 &
        lookalike_pid=$!
        for attempt in $(${pkgs.coreutils}/bin/seq 1 100); do
          test -S ${fixtureRoot}/lookalike/unmanaged.sock && break
          ${pkgs.coreutils}/bin/sleep 0.05
        done
        test -S ${fixtureRoot}/lookalike/unmanaged.sock
        if ssh_consumer "$index" codex -c features.code_mode_host=true \
          app-server --listen unix:// \
          >${fixtureRoot}/lookalike-accepted.stdout \
          2>${fixtureRoot}/lookalike-refusal.stderr; then
          echo "managed Product session accepted a pinned app-server lookalike" >&2
          exit 1
        fi
        ${pkgs.gnugrep}/bin/grep -Eq 'preexisting pinned app-server|lookalike' \
          ${fixtureRoot}/lookalike-refusal.stderr
        kill "$lookalike_pid"
        wait "$lookalike_pid" 2>/dev/null || true
        lookalike_pid=""
        rm -f ${fixtureRoot}/lookalike/unmanaged.sock
      fi

      phase="consumer-$index managed leading-config launch"
      ssh_consumer "$index" codex -c features.code_mode_host=true \
        app-server --listen unix:// \
        >${fixtureRoot}/listen-$index.stdout 2>${fixtureRoot}/listen-$index.stderr
      ssh_consumer "$index" devkit-product-prepare/v1 \
        >${fixtureRoot}/status-$index.json 2>${fixtureRoot}/status-$index.stderr
      ${pkgs.jq}/bin/jq -e \
        --argjson index "$index" --argjson uid "$uid" \
        '.status.consumer_index == $index and
         .status.app_server_running == true and
         .status.app_server_process_count == 1 and
         .status.start_count == 1 and
         .status.app_server_socket_device > 0 and
         .status.app_server_socket_inode > 0 and
         .status.app_server_socket_owner == $uid and
         .status.mount_namespace_distinct == true and
         .status.network_namespace_distinct == true and
         .status.windows_mounts_absent == true' \
        ${fixtureRoot}/status-$index.json >/dev/null
      test "$(${pkgs.coreutils}/bin/stat -c %d "$candidate/state/app-server.sock")" = \
        "$(${pkgs.jq}/bin/jq -r .status.app_server_socket_device ${fixtureRoot}/status-$index.json)"
      test "$(${pkgs.coreutils}/bin/stat -c %i "$candidate/state/app-server.sock")" = \
        "$(${pkgs.jq}/bin/jq -r .status.app_server_socket_inode ${fixtureRoot}/status-$index.json)"
      test "$(${pkgs.coreutils}/bin/stat -c %u "$candidate/state/app-server.sock")" = "$uid"

      phase="consumer-$index real app-server thread and MCP probe"
      ${pkgs.util-linux}/bin/runuser -u controller -- \
        ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i \
        HOME=${fixtureRoot}/controller-home \
        ${managedClient}/bin/product-managed-client probe \
        --ssh ${pkgs.openssh}/bin/ssh \
        --identity ${fixtureRoot}/source-$index/id_ed25519 \
        --known-hosts ${fixtureRoot}/client-known-hosts \
        --port 2222 --user product$index --cwd /workspaces/dev \
        >${fixtureRoot}/managed-$index.json 2>${fixtureRoot}/managed-$index.stderr
      ${pkgs.jq}/bin/jq -e '
        .schema_version == "devkit/product-managed-app-server-client/v1" and
        .initialize_ready == true and
        .ephemeral_thread_created == true and
        .ephemeral_thread_read == true and
        .mcp_status_read == true and .governance_tools == 2
      ' ${fixtureRoot}/managed-$index.json >/dev/null

      phase="consumer-$index construction receipt and git proof"
      receipt="$candidate/state/product-construction-receipt.json"
	      ${pkgs.jq}/bin/jq -e '
	        .schema_version == "devkit/product-construction-receipt/v1" and
	        .status == "executing" and .proxy_cleanup == "listener-active" and
        .readiness_runtime == true and .readiness_repository == true and
        (.credential_handle_digests | length) == 3 and
        any(.readiness_evidence[];
          .name == "codex-app-server-stdio-thread-mcp-readiness" and
          .status == "ready" and
          .result.schema_version == "devkit/product-codex-app-server-stdio-thread-mcp-readiness/v1" and
          .result.initialize_ready == true and .result.mcp_status_read == true and
          .result.ephemeral_thread_created == true and
          .result.ephemeral_thread_read_back == true and
          .result.approval_policy == "never" and
          .result.sandbox_policy == "dangerFullAccess"
        )
	      ' "$receipt" >/dev/null
	      test -S "$candidate/state/product-egress.sock"
      test "$(${pkgs.coreutils}/bin/stat -c %u "$candidate/agent$index")" = "$uid"
      observed_revision="$(
        ${pkgs.util-linux}/bin/runuser -u product$index -- \
          ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i \
          GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
          ${pkgs.git}/bin/git --no-optional-locks \
          -C "$candidate/agent$index/ouroboros-ide" \
          -c core.fsmonitor=false -c core.hooksPath=/dev/null rev-parse HEAD
      )"
      test "$observed_revision" = "$revision"
      gitdir="$(${pkgs.gnused}/bin/sed -n 's/^gitdir: //p' "$candidate/agent$index/ouroboros-ide/.git")"
      test -n "$gitdir"
      test "''${gitdir#/}" = "$gitdir"
      diagnostic_ref="refs/devkit-consumer-boundary/consumer-$index"
      product_git() {
        ${pkgs.util-linux}/bin/runuser -u product$index -- \
          ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i \
          GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
          ${pkgs.git}/bin/git --no-optional-locks \
          -C "$candidate/agent$index/ouroboros-ide" \
          -c core.fsmonitor=false -c core.hooksPath=/dev/null "$@"
      }
      product_git update-ref "$diagnostic_ref" HEAD
      test "$(product_git rev-parse "$diagnostic_ref")" = "$revision"
      product_git update-ref -d "$diagnostic_ref"
      if product_git show-ref --verify --quiet "$diagnostic_ref"; then
        echo "temporary Product diagnostic ref survived cleanup" >&2
        exit 1
      fi
      cp "$receipt" ${fixtureRoot}/receipt-$index.json

      phase="consumer-$index teardown"
      kill "$supervisor_pid"
      wait "$supervisor_pid"
	      supervisor_pid=""
	      test ! -e "$candidate/state/app-server.sock"
	      test ! -e "$candidate/state/product-egress.sock"
      test ! -e ${fixtureRoot}/control-$index/product-supervisor.sock
      test -z "$(${pkgs.procps}/bin/pgrep -u "$uid" -f '${pinnedCodex}/bin/codex.*app-server' || true)"
      rm -rf "$candidate"
      test ! -e "$candidate"
    done

    phase="cross-consumer independence and final teardown"
    test "$(${pkgs.jq}/bin/jq -r .credential_handle_digests.ssh_identity ${fixtureRoot}/receipt-1.json)" != \
      "$(${pkgs.jq}/bin/jq -r .credential_handle_digests.ssh_identity ${fixtureRoot}/receipt-2.json)"
    ! ${pkgs.gnugrep}/bin/grep -q 'OPENSSH PRIVATE KEY' \
      ${fixtureRoot}/receipt-1.json ${fixtureRoot}/receipt-2.json \
      ${fixtureRoot}/seed-1.json ${fixtureRoot}/seed-2.json
    cleanup
    sshd_pid=""
    outer_proxy_pid=""
    trap - EXIT
    trap - ERR
    rm -rf ${candidateParent} ${fixtureRoot}
    test ! -e ${candidateParent}
    test ! -e ${fixtureRoot}
  '';
in
pkgs.testers.runNixOSTest {
  name = "product-consumer-boundary-diagnostic";
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
	  users.groups.controller.gid = 1000;
	  users.users.controller = {
	    isNormalUser = true;
	    uid = 1000;
	    group = "controller";
	    home = "${fixtureRoot}/controller-home";
	  };
      users.groups.product1.gid = 2001;
      users.groups.product2.gid = 2002;
      users.users.product1 = {
        isSystemUser = true;
        uid = 2001;
        group = "product1";
		home = "${candidateParent}/a/slot/home";
		shell = "${fixtureAdapter}/bin/product-ssh-session";
		hashedPassword = "";
      };
      users.users.product2 = {
        isSystemUser = true;
        uid = 2002;
        group = "product2";
		home = "${candidateParent}/b/slot/home";
		shell = "${fixtureAdapter}/bin/product-ssh-session";
		hashedPassword = "";
      };
	  environment.shells = [ "${fixtureAdapter}/bin/product-ssh-session" ];
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

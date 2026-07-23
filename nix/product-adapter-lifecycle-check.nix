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
  controllerHome = "/var/lib/product-lifecycle-controller";
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
	          name = "devkit_fixture";
	          tools = [ "lifecycle_probe" ];
	        }
	      ];
	    }
	  );
	  mountPolicyContract = mkProductMountPolicyContract pkgs;
	  sshSessionContract = mkProductSSHSessionContract pkgs;
	  stoppedVolumeSeedContract = mkProductStoppedVolumeSeedContract pkgs;
  lifecycleProductRevision = "7c49b072973e6ea3ced9515352c79cfbd915754e";
  diagnosticRuntimeFixture =
    import ./dev-all-runtime-bundle-fixture.nix {
      inherit pkgs;
      productSourceRev = lifecycleProductRevision;
    };
  diagnosticRuntimeBundle =
    mkDevAllRuntimeBundle ({ inherit pkgs; } // diagnosticRuntimeFixture.constructorArgs);
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
    # immutable MCP requirement limited to a packaged framing fixture. It
    # proves transport and typed tool calls, never governed admission.
    [features]
    apps = false

	[mcp_servers.devkit_fixture]
	command = "${mcpFixture}/bin/product-mcp-fixture"
	startup_timeout_sec = 30
	tool_timeout_sec = 30
  '';
  governanceEnv =
    diagnosticRuntimeFixture.productRuntimeProjection.envPath;
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

  # These fixed keys are public, test-only data. They grant access only to this
  # diagnostic's ephemeral sshd and make the derivation reproducible. Runtime
  # copies receive mode 0600 before OpenSSH consumes them.
  fixtureHostKey = pkgs.writeText "devkit-product-lifecycle-host-key-test-only" ''
    -----BEGIN OPENSSH PRIVATE KEY-----
    b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
    QyNTUxOQAAACDF6SwxKUBBjXOynyxKilU3Y7QpK1g9YlK8vndR+Z4YagAAALAq7esCKu3r
    AgAAAAtzc2gtZWQyNTUxOQAAACDF6SwxKUBBjXOynyxKilU3Y7QpK1g9YlK8vndR+Z4Yag
    AAAEDnPgc2QV1We8i1lg+mqSPUw2QFZGmoAGTf5CJ7mVIpSsXpLDEpQEGNc7KfLEqKVTdj
    tCkrWD1iUry+d1H5nhhqAAAAJ2RldmtpdC1wcm9kdWN0LWxpZmVjeWNsZS1ob3N0LXRlc3
    Qtb25seQECAwQFBg==
    -----END OPENSSH PRIVATE KEY-----
  '';
  fixtureGUI1Key = pkgs.writeText "devkit-product-lifecycle-gui-1-key-test-only" ''
    -----BEGIN OPENSSH PRIVATE KEY-----
    b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
    QyNTUxOQAAACDWmeem46DUnQIZ4ki0YzJqH1bj7ezOIZsKYvvR9ZZtwwAAALC0Mj4GtDI+
    BgAAAAtzc2gtZWQyNTUxOQAAACDWmeem46DUnQIZ4ki0YzJqH1bj7ezOIZsKYvvR9ZZtww
    AAAECNVk1uKm8l+XttOU5JiCVhsFBw1bpasbOZlwTQlLNzudaZ56bjoNSdAhniSLRjMmof
    VuPt7M4hmwpi+9H1lm3DAAAAJ2RldmtpdC1wcm9kdWN0LWxpZmVjeWNsZS1ndWkxLXRlc3
    Qtb25seQECAwQFBg==
    -----END OPENSSH PRIVATE KEY-----
  '';
  fixtureGUI2Key = pkgs.writeText "devkit-product-lifecycle-gui-2-key-test-only" ''
    -----BEGIN OPENSSH PRIVATE KEY-----
    b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
    QyNTUxOQAAACDbZhzQqtuBuq1JggrlztH1nG0/pHTFGnExmvk+eXWfwQAAALB7w1Noe8NT
    aAAAAAtzc2gtZWQyNTUxOQAAACDbZhzQqtuBuq1JggrlztH1nG0/pHTFGnExmvk+eXWfwQ
    AAAEBG6pwiQTDPAVMXgUnJK+wXMTJ7bz+Ii95nz+gnSfaZP9tmHNCq24G6rUmCCuXO0fWc
    bT+kdMUacTGa+T55dZ/BAAAAJ2RldmtpdC1wcm9kdWN0LWxpZmVjeWNsZS1ndWkyLXRlc3
    Qtb25seQECAwQFBg==
    -----END OPENSSH PRIVATE KEY-----
  '';

  # The repository and all SSH authority below are deterministic test inputs.
  fixtureSource = pkgs.runCommand "devkit-product-lifecycle-ssh-source" {
    nativeBuildInputs = [
      pkgs.git
      pkgs.openssh
    ];
  } ''
    set -euo pipefail
    umask 077
    mkdir -p "$out"
    cp ${fixtureHostKey} "$TMPDIR/host-key"
    chmod 0600 "$TMPDIR/host-key"
    ssh-keygen -y -f "$TMPDIR/host-key" \
      | awk '{print $1 " " $2 " devkit-product-lifecycle-host-test-only"}' \
      > "$TMPDIR/host-key.pub"
    cp "$TMPDIR/host-key" "$out/host-key"
    cp "$TMPDIR/host-key.pub" "$out/host-key.pub"
    cp ${fixtureGUI1Key} "$TMPDIR/gui-1"
    cp ${fixtureGUI2Key} "$TMPDIR/gui-2"
    chmod 0600 "$TMPDIR/gui-1" "$TMPDIR/gui-2"
    for index in 1 2; do
      ssh-keygen -y -f "$TMPDIR/gui-$index" \
        > "$TMPDIR/gui-$index.pub"
      cp "$TMPDIR/gui-$index" "$out/gui-$index"
      awk '{print "restrict " $1 " " $2}' \
        "$TMPDIR/gui-$index.pub" > "$out/gui-$index-authorized_keys"
    done
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
  diagnosticAdapterManifest = pkgs.runCommand "devkit-product-adapter-lifecycle-authority" {
    nativeBuildInputs = [
      pkgs.coreutils
      pkgs.jq
    ];
  } ''
    set -eo pipefail
    manifest="$out/identity.json"
    mkdir -p "$out"
    digest() {
      ${pkgs.coreutils}/bin/sha256sum "$1" | ${pkgs.coreutils}/bin/cut -d' ' -f1
    }
    adapter="${productionAdapter}/bin/product-adapter"
    proxy="${productionAdapter}/bin/product-proxy"
    readiness="${productionAdapter}/bin/product-readiness"
	runtime_exec="${productionAdapter}/bin/product-runtime-exec"
	    supervisor="${productionAdapter}/bin/product-adapter-supervisor"
	    ssh_session="${productionAdapter}/bin/product-ssh-session"
	    ssh_setup="${productionAdapter}/bin/product-ssh-setup"
	    ssh_session_contract='${sshSessionContract}'
	    ssh_setup_contract='${stoppedVolumeSeedContract}'
    revision="$(${pkgs.coreutils}/bin/cat ${fixtureSource}/revision)"
	  mount_policy_identity="$(${pkgs.jq}/bin/jq -er .identity ${mountPolicyContract})"
    origin="ssh://git@ssh.github.com:443${fixtureSource}/ouroboros-ide.git"
    candidate_a="${candidateParent}/a/slot"
    candidate_b="${candidateParent}/b/slot"
	    ${pkgs.jq}/bin/jq -n \
	      --slurpfile base ${diagnosticRuntimeBundle}/share/dev-all-runtime-bundle/identity.json \
      --arg revision "$revision" \
      --arg adapter "$adapter" \
      --arg proxy "$proxy" \
      --arg git ${pkgs.git}/bin/git \
      --arg git_ssh ${productionAdapter.productAdapterResources.gitSSH} \
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
      --arg authorized_a ${fixtureSource}/gui-1-authorized_keys \
      --arg authorized_b ${fixtureSource}/gui-2-authorized_keys \
      --arg d_adapter "$(digest "$adapter")" \
      --arg d_proxy "$(digest "$proxy")" \
      --arg d_git "$(digest ${pkgs.git}/bin/git)" \
      --arg d_git_ssh "$(digest ${productionAdapter.productAdapterResources.gitSSH})" \
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
	  def consumer($index; $uid; $candidate; $authorized_keys):
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
	      authorizedKeysPath: $authorized_keys,
          binds: [
            {source:"/nix/store",target:"/nix/store",mode:"ro",required:true},
            {source:$worktree,target:"/workspaces/dev",mode:"rw",required:true},
            {source:$home,target:"/home/product",mode:"rw",required:true},
            {source:$state,target:"/agent-state/product",mode:"rw",required:true}
          ]
        };
	      $base[0] |
	      .codexAuthorization = {
	        configPath:$codex_config,
	        configSha256:$d_codex_config,
	        systemPath:"/etc/codex/config.toml"
	      } |
	      .devkitProductAdapter = {
          schemaVersion:"wsl-nix-devkit-product-adapter/v1",
          executablePath:$adapter,
          proxyHelperPath:$proxy,
          gitPath:$git,
          gitSSHPath:$git_ssh,
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
          consumers:[
            consumer(1;2001;$candidate_a;$authorized_a),
            consumer(2;2002;$candidate_b;$authorized_b)
          ],
          artifactDigests:{
            adapter:$d_adapter,proxy_helper:$d_proxy,git:$d_git,git_ssh:$d_git_ssh,env:$d_env,
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
    knownHostsFile = "${fixtureSource}/known_hosts";
  };
  readinessHermetic = pkgs.runCommand "devkit-product-readiness-hermetic" {
    nativeBuildInputs = [
      pkgs.coreutils
      pkgs.jq
    ];
  } ''
    set -euo pipefail
    root="$TMPDIR/product-readiness"
    mkdir -p "$root/home/.codex" "$root/tmp" "$root/workspace"
    cp ${codexConfig} "$root/home/.codex/config.toml"
    chmod 0600 "$root/home/.codex/config.toml"
    requirement_digest="$(${pkgs.coreutils}/bin/sha256sum ${mcpRequirement} | ${pkgs.coreutils}/bin/cut -d' ' -f1)"
    (
      cd "$root/workspace"
      ${pkgs.coreutils}/bin/env -i \
        PATH=/hostile \
        HOME="$root/home" \
        CODEX_HOME="$root/home/.codex" \
        TMPDIR="$root/tmp" \
        ${productionAdapter}/bin/product-readiness probe \
          --codex ${pinnedCodex}/bin/codex \
          --mcp-requirement ${mcpRequirement} \
          --mcp-requirement-digest "$requirement_digest" \
          --consumer-index 1
    ) > "$root/result.json"
    ${pkgs.jq}/bin/jq -e '
      .schema_version == "devkit/product-codex-app-server-stdio-thread-mcp-readiness/v1" and
      .app_server_alive == true and
      .initialize_ready == true and
      .ephemeral_thread_created == true and
      .ephemeral_thread_read_back == true and
      .mcp_status_read == true and
      .mcp_server_count == 1 and
      .mcp_tool_count == 1 and
      .approval_policy == "never" and
      .sandbox_policy == "dangerFullAccess" and
      (has("governance_admitted") | not) and
      (has("governance_run_id") | not) and
      (has("governance_tree_id") | not)
    ' "$root/result.json" >/dev/null
    mkdir -p "$out"
    cp "$root/result.json" "$out/readiness.json"
  '';
  namespaceWrappers = productionAdapter.productAdapterResources.namespaceWrappers;
  adapterPackageContract = pkgs.runCommand "devkit-product-adapter-production-contract" {
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
    ! grep -aqF 'testAuthorityLocator' "$adapter"
    for forbidden in \
      'git+file:///workspaces/dev/ouroboros-ide' \
      'DEVKIT_GOVERNANCE_' \
      '#dev-all-runtime-bundle'
    do
      ! grep -aF "$forbidden" "$adapter" "$proxy"
    done
    for invented_admission in \
      'product-governance-admission-fixture' \
      'run-devkit-product-consumer' \
      'governance_admitted' \
      'governance_run_id' \
      'governance_tree_id'
    do
      ! grep -aF "$invented_admission" \
        ${productionAdapter}/bin/product-adapter \
        ${productionAdapter}/bin/product-readiness \
        ${productionAdapter}/bin/product-adapter-supervisor \
        ${productionAdapter}/bin/product-runtime-exec
    done
    for executable in \
      ${productionAdapter}/bin/product-adapter \
      ${productionAdapter}/bin/product-proxy \
      ${productionAdapter}/bin/product-readiness \
	  ${productionAdapter}/bin/product-adapter-supervisor \
	  ${productionAdapter}/bin/product-ssh-session \
	  ${productionAdapter}/bin/product-ssh-setup \
      ${productionAdapter}/bin/product-runtime-exec \
      ${productionAdapter.productAdapterResources.gitSSH} \
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
    export PATH=${pkgs.lib.makeBinPath [
      pkgs.bash
      pkgs.coreutils
      pkgs.findutils
      pkgs.gawk
      pkgs.git
      pkgs.gnugrep
      pkgs.gnused
      pkgs.iproute2
      pkgs.jq
      pkgs.netcat-openbsd
      pkgs.openssh
      pkgs.procps
      pkgs.util-linux
    ]}
    exec 8>&1 9>&2
    test ! -e ${fixtureRoot}
    test ! -e ${candidateParent}
    mkdir -p ${fixtureRoot} ${candidateParent}/a ${candidateParent}/b
    chmod 0711 ${fixtureRoot} ${candidateParent}
	chown 2001:2001 ${candidateParent}/a
	chown 2002:2002 ${candidateParent}/b
	chmod 0700 ${candidateParent}/a ${candidateParent}/b
    test ! -e /var/lib/product-runtime/authority-selector.json
    mkdir -p /var/lib/product-runtime
    chown root:root /var/lib/product-runtime
    chmod 0755 /var/lib/product-runtime
    manifest_sha="$(${pkgs.coreutils}/bin/sha256sum ${diagnosticAdapterManifest}/identity.json | ${pkgs.coreutils}/bin/cut -d' ' -f1)"
    ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i PATH=/hostile \
      ${productionAdapter}/bin/product-authority-selector-install \
      --manifest ${diagnosticAdapterManifest}/identity.json \
      --manifest-sha256 "$manifest_sha"
    test "$(${pkgs.coreutils}/bin/stat -c %u /var/lib/product-runtime/authority-selector.json)" = 0
    test "$(${pkgs.coreutils}/bin/stat -c %a /var/lib/product-runtime/authority-selector.json)" = 444
    supervisor_pid=""
    held_client_pid=""
    lookalike_pid=""
    sshd_pid=""
    outer_proxy_pid=""
    phase="initialize"
    process_live() {
      pid="$1"
      kill -0 "$pid" 2>/dev/null || return 1
      state="$(${pkgs.gawk}/bin/awk '{print $3}' "/proc/$pid/stat" 2>/dev/null)" \
        || return 1
      test "$state" != Z
    }
    cleanup() {
      test -z "$lookalike_pid" || kill "$lookalike_pid" 2>/dev/null || true
      test -z "$supervisor_pid" || kill "$supervisor_pid" 2>/dev/null || true
      test -z "$held_client_pid" || kill "$held_client_pid" 2>/dev/null || true
      test -z "$outer_proxy_pid" || kill "$outer_proxy_pid" 2>/dev/null || true
      test -z "$sshd_pid" || kill "$sshd_pid" 2>/dev/null || true
      test -z "$lookalike_pid" || wait "$lookalike_pid" 2>/dev/null || true
      test -z "$supervisor_pid" || wait "$supervisor_pid" 2>/dev/null || true
      test -z "$held_client_pid" || wait "$held_client_pid" 2>/dev/null || true
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
    test -d ${controllerHome}
    test "$(${pkgs.coreutils}/bin/stat -c %U:%G ${controllerHome})" = controller:controller

    : > ${fixtureRoot}/root-authorized-keys
    for index in 1 2; do
      phase="consumer-$index offline seed"
      source_dir=${fixtureRoot}/source-$index
      gui_dir=${fixtureRoot}/gui-$index
      mkdir "$source_dir" "$gui_dir"
      ${pkgs.openssh}/bin/ssh-keygen -q -t ed25519 -N "" -f "$source_dir/id_ed25519"
      cp ${fixtureSource}/gui-$index "$gui_dir/id_ed25519"
      chmod 0600 "$source_dir/id_ed25519" "$source_dir/id_ed25519.pub"
      chmod 0600 "$gui_dir/id_ed25519"
      chown -R controller:controller "$source_dir" "$gui_dir"
    done
    cat > ${fixtureRoot}/upload-pack <<EOF
#!${pkgs.bash}/bin/bash
set -eu
test "\''${SSH_ORIGINAL_COMMAND:-}" = "git-upload-pack '${fixtureSource}/ouroboros-ide.git'" || exit 91
exec ${pkgs.git}/bin/git-upload-pack '${fixtureSource}/ouroboros-ide.git'
EOF
    chmod 0555 ${fixtureRoot}/upload-pack
    for index in 1 2; do
      printf 'restrict,command="%s" %s\n' \
        ${fixtureRoot}/upload-pack \
        "$(${pkgs.coreutils}/bin/cat ${fixtureRoot}/source-$index/id_ed25519.pub)" \
        >> ${fixtureRoot}/root-authorized-keys
    done
    chmod 0600 ${fixtureRoot}/root-authorized-keys
    ${pkgs.coreutils}/bin/install -d -o root -g root -m 0755 \
      ${fixtureRoot}/authorized-keys
    ${pkgs.coreutils}/bin/install -o root -g root -m 0644 \
      ${fixtureRoot}/root-authorized-keys \
      ${fixtureRoot}/authorized-keys/git
    for index in 1 2; do
      ${pkgs.coreutils}/bin/install -o root -g root -m 0644 \
        ${fixtureSource}/gui-$index-authorized_keys \
        ${fixtureRoot}/authorized-keys/product$index
    done

    cat > ${fixtureRoot}/sshd_config <<EOF
ListenAddress 127.0.0.1
Port 2222
PidFile ${fixtureRoot}/sshd.pid
HostKey ${fixtureRoot}/host-key
AuthorizedKeysFile ${fixtureRoot}/authorized-keys/%u
StrictModes yes
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
PermitRootLogin no
AllowUsers git product1 product2
LogLevel VERBOSE
Match User product1
  ForceCommand ${productionAdapter}/bin/product-ssh-session force-command --count 2 --index 1
Match User product2
  ForceCommand ${productionAdapter}/bin/product-ssh-session force-command --count 2 --index 2
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
    test -f ${diagnosticAdapterManifest}/identity.json
    ${pkgs.jq}/bin/jq -e \
      --arg launcher '${productionAdapter}/bin/product-runtime-exec' \
      --arg governance '${governanceEnv}' \
      '.devkitProductAdapter.runtimeLauncherPath == $launcher and
       .devkitProductAdapter.controllerCredentialOwnerUid == 1000 and
       .devkitProductAdapter.governanceEnvPath == $governance and
       (.devkitProductAdapter | has("runtimeRoot") | not)' \
      ${diagnosticAdapterManifest}/identity.json >/dev/null
    ssh_consumer() {
      index="$1"
      shift
      ${pkgs.util-linux}/bin/runuser -u controller -- \
        ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i \
        HOME=${controllerHome} \
        ${pkgs.openssh}/bin/ssh -T -F /dev/null \
        -o BatchMode=yes -o IdentitiesOnly=yes \
        -o IdentityFile=${fixtureRoot}/gui-$index/id_ed25519 \
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
      if (
        cd ${candidateParent}/$projection
        ${pkgs.util-linux}/bin/runuser -u product$index -- \
          ${pkgs.util-linux}/bin/unshare --user --map-root-user --mount -- \
          ${productionAdapter}/bin/product-ssh-setup seed-git \
          --count 2 --index "$index" --root-projection slot
      ) >${fixtureRoot}/namespaced-ssh-setup-$index.log 2>&1; then
        echo "caller-created child root bypassed Product SSH setup admission" >&2
        exit 1
      fi
      test ! -e "$candidate/.devkit-product-offline-seed.json"
      if (
        cd ${candidateParent}/$projection
        ${pkgs.util-linux}/bin/runuser -u product$index -- \
          ${pkgs.util-linux}/bin/unshare --user --map-root-user --mount -- \
          ${namespaceWrappers.sshSetup.path} seed-git \
          --count 2 --index "$index" --root-projection slot
      ) >${fixtureRoot}/unauthorized-ssh-setup-$index.log 2>&1; then
        echo "Product consumer executed the controller-only SSH setup wrapper" >&2
        exit 1
      fi
      test ! -e "$candidate/.devkit-product-offline-seed.json"
      (
        cd ${candidateParent}/$projection
        ${pkgs.util-linux}/bin/runuser -u controller -- \
          ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i \
            DEVKIT_SOURCE_TRANSPORT_IDENTITY=${fixtureRoot}/source-$index/id_ed25519 \
            ${namespaceWrappers.sshSetup.path} seed-git \
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

      phase="consumer-$index namespace-attested launch"
      if ${pkgs.util-linux}/bin/setpriv \
        --reuid="$uid" --regid="$uid" --clear-groups \
        --inh-caps=-all --ambient-caps=-all -- \
        ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i \
        ${productionAdapter}/bin/product-adapter-supervisor \
        serve --count 2 --index "$index" \
        >${fixtureRoot}/direct-supervisor-$index.log 2>&1; then
        echo "direct Product supervisor bypassed the package-owned entry wrapper" >&2
        exit 1
      fi
      test ! -e ${fixtureRoot}/control-$index/product-supervisor.sock

      if ${pkgs.util-linux}/bin/runuser -u product$index -- \
        ${pkgs.util-linux}/bin/unshare --user --map-current-user --mount -- \
        ${namespaceWrappers.supervisor.path} \
        serve --count 2 --index "$index" \
        >${fixtureRoot}/namespaced-supervisor-$index.log 2>&1; then
        echo "caller-created namespace acquired Product supervisor authority" >&2
        exit 1
      fi
      test ! -e ${fixtureRoot}/control-$index/product-supervisor.sock

      ${pkgs.util-linux}/bin/setpriv \
        --reuid="$uid" --regid="$uid" --clear-groups \
        --inh-caps=-all --ambient-caps=-all -- \
        ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i \
        ${namespaceWrappers.supervisor.path} \
        serve --count 2 --index "$index" \
        >${fixtureRoot}/supervisor-$index.log 2>&1 &
      supervisor_pid=$!
      for attempt in $(${pkgs.coreutils}/bin/seq 1 100); do
        test -S ${fixtureRoot}/control-$index/product-supervisor.sock && break
        ${pkgs.coreutils}/bin/sleep 0.05
      done
      test -S ${fixtureRoot}/control-$index/product-supervisor.sock
      for status in /proc/$supervisor_pid/task/*/status; do
        ${pkgs.gawk}/bin/awk -v expected="$uid" '
          $1 == "Uid:" &&
            ($2 != expected || $3 != expected || $4 != expected || $5 != expected) {
            exit 1
          }
          $1 == "Gid:" &&
            ($2 != expected || $3 != expected || $4 != expected || $5 != expected) {
            exit 1
          }
          $1 == "Groups:" && NF != 1 {
            exit 1
          }
          /^(CapInh|CapPrm|CapEff|CapAmb):/ && $2 != "0000000000000000" {
            exit 1
          }
        ' "$status"
      done
      if ${pkgs.util-linux}/bin/runuser -u product$index -- \
        ${namespaceWrappers.proxy.path} supervise \
        >${fixtureRoot}/privileged-proxy-supervise-$index.log 2>&1; then
        echo "privileged proxy wrapper admitted the bwrap-only supervise mode" >&2
        exit 1
      fi

      phase="consumer-$index immutable GUI admission"
      admission="${fixtureSource}/gui-$index-authorized_keys"
      test "$(${pkgs.jq}/bin/jq -r \
        ".devkitProductAdapter.consumers[$((index - 1))].authorizedKeysPath" \
        ${diagnosticAdapterManifest}/identity.json)" = "$admission"
      if ${pkgs.util-linux}/bin/runuser -u product$index -- \
        ${pkgs.bash}/bin/bash -c 'printf replacement > "$1"' _ "$admission"; then
        echo "Product consumer overwrote immutable GUI admission" >&2
        exit 1
      fi
      if ${pkgs.util-linux}/bin/runuser -u product$index -- \
        ${pkgs.coreutils}/bin/rm -f "$admission"; then
        echo "Product consumer unlinked immutable GUI admission" >&2
        exit 1
      fi
      wrong_index=$([ "$index" = 1 ] && printf 2 || printf 1)
      if ${pkgs.util-linux}/bin/runuser -u controller -- \
        ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i \
        HOME=${controllerHome} \
        ${pkgs.openssh}/bin/ssh -T -F /dev/null \
        -o BatchMode=yes -o IdentitiesOnly=yes \
        -o IdentityFile=${fixtureRoot}/gui-$wrong_index/id_ed25519 \
        -o UserKnownHostsFile=${fixtureRoot}/client-known-hosts \
        -o GlobalKnownHostsFile=/dev/null -o StrictHostKeyChecking=yes \
        -p 2222 product$index@127.0.0.1 devkit-product-prepare/v1 \
        >${fixtureRoot}/wrong-gui-$index.stdout \
        2>${fixtureRoot}/wrong-gui-$index.stderr; then
        echo "Product SSH admission accepted another consumer's GUI key" >&2
        exit 1
      fi

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
        ${pkgs.jq}/bin/jq -e '
          .schema_version == "devkit/product-supervisor-response/v1" and
          .failure.schema_version == "devkit/product-supervisor-failure/v1" and
          .failure.phase == "app-server-start" and
          .failure.code == "app-server-start-failed"
        ' ${fixtureRoot}/lookalike-accepted.stdout >/dev/null
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
		 .status.app_server_kernel_socket_inode > 0 and
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
        HOME=${controllerHome} \
        ${managedClient}/bin/product-managed-client probe \
        --ssh ${pkgs.openssh}/bin/ssh \
        --identity ${fixtureRoot}/gui-$index/id_ed25519 \
        --known-hosts ${fixtureRoot}/client-known-hosts \
        --port 2222 --user product$index --cwd /workspaces/dev \
        --consumer-index "$index" \
        >${fixtureRoot}/managed-$index.json 2>${fixtureRoot}/managed-$index.stderr
      ${pkgs.jq}/bin/jq -e --argjson index "$index" '
        .schema_version == "devkit/product-managed-app-server-client/v1" and
        .initialize_ready == true and
        .ephemeral_thread_created == true and
        .ephemeral_thread_read == true and
        .mcp_status_read == true and .mcp_fixture_tools == 1 and
        .mcp_fixture_call_verified == true and
        .mcp_fixture_consumer_index == $index and
        (has("governance_admitted") | not) and
        (has("governance_run_id") | not) and
        (has("governance_tree_id") | not)
      ' ${fixtureRoot}/managed-$index.json >/dev/null

      phase="consumer-$index live managed session for teardown"
      ${pkgs.util-linux}/bin/runuser -u controller -- \
        ${pkgs.coreutils}/bin/coreutils --coreutils-prog=env -i \
        HOME=${controllerHome} \
        ${managedClient}/bin/product-managed-client hold \
        --ssh ${pkgs.openssh}/bin/ssh \
        --identity ${fixtureRoot}/gui-$index/id_ed25519 \
        --known-hosts ${fixtureRoot}/client-known-hosts \
        --port 2222 --user product$index --cwd /workspaces/dev \
        --consumer-index "$index" \
        >${fixtureRoot}/held-$index.json 2>${fixtureRoot}/held-$index.stderr &
      held_client_pid=$!
      for attempt in $(${pkgs.coreutils}/bin/seq 1 100); do
        test -s ${fixtureRoot}/held-$index.json && break
        kill -0 "$held_client_pid"
        ${pkgs.coreutils}/bin/sleep 0.05
      done
      ${pkgs.jq}/bin/jq -e '
        .schema_version == "devkit/product-managed-app-server-hold/v1" and
        .initialize_ready == true and .status == "holding"
      ' ${fixtureRoot}/held-$index.json >/dev/null
      kill -0 "$held_client_pid"

      phase="consumer-$index construction receipt and git proof"
      receipt="$candidate/state/product-construction-receipt.json"
	      ${pkgs.jq}/bin/jq -e '
	        .schema_version == "devkit/product-construction-receipt/v1" and
	        .status == "executing" and .proxy_cleanup == "listener-active" and
        .readiness_runtime == true and .readiness_repository == true and
        (.credential_handle_digests | length) == 4 and
        (.credential_handle_digests
          | has("ssh_identity") and has("ssh_public_key")
            and has("authorized_keys") and has("codex_auth")) and
        any(.readiness_evidence[];
          .name == "codex-app-server-stdio-thread-mcp-readiness" and
          .status == "ready" and
          .result.schema_version == "devkit/product-codex-app-server-stdio-thread-mcp-readiness/v1" and
          .result.initialize_ready == true and .result.mcp_status_read == true and
          .result.ephemeral_thread_created == true and
          .result.ephemeral_thread_read_back == true and
          .result.approval_policy == "never" and
          .result.sandbox_policy == "dangerFullAccess" and
          (.result | has("governance_admitted") | not) and
          (.result | has("governance_run_id") | not) and
          (.result | has("governance_tree_id") | not)
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
      if ! process_live "$held_client_pid"; then
        echo "held Product app-server session ended before supervisor teardown" >&2
        exit 1
      fi
      kill "$supervisor_pid"
      for attempt in $(${pkgs.coreutils}/bin/seq 1 200); do
        if ! process_live "$supervisor_pid" \
          && ! process_live "$held_client_pid"
        then
          break
        fi
        ${pkgs.coreutils}/bin/sleep 0.05
      done
      if process_live "$supervisor_pid"; then
        echo "Product supervisor did not stop within the teardown bound" >&2
        exit 1
      fi
      if process_live "$held_client_pid"; then
        echo "held Product app-server session survived supervisor teardown" >&2
        exit 1
      fi
      wait "$supervisor_pid"
      supervisor_pid=""
      wait "$held_client_pid"
      held_client_pid=""
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
  vmTest = pkgs.testers.runNixOSTest {
    name = "product-consumer-boundary-diagnostic";
    nodes.machine =
    { ... }:
    {
      # The diagnostic uses guest loopback only. Avoid host TAP/VDE authority
      # and keep QEMU's user network unable to reach outside the guest.
      virtualisation.vlans = [ ];
      virtualisation.restrictNetwork = true;
      virtualisation.memorySize = 4096;
      virtualisation.cores = 2;
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
	    home = controllerHome;
        createHome = true;
        autoSubUidGidRange = false;
        subUidRanges = [ ];
        subGidRanges = [ ];
	  };
      users.groups.product1.gid = 2001;
      users.groups.product2.gid = 2002;
      users.groups.git.gid = 2000;
      users.users.git = {
        isSystemUser = true;
        uid = 2000;
        group = "git";
        shell = "${pkgs.bashInteractive}/bin/bash";
        hashedPassword = "";
      };
      users.users.product1 = {
        isSystemUser = true;
        uid = 2001;
        group = "product1";
        autoSubUidGidRange = false;
        subUidRanges = [ ];
        subGidRanges = [ ];
		home = "${candidateParent}/a/slot/home";
		shell = namespaceWrappers.sshSession.path;
		hashedPassword = "";
      };
      users.users.product2 = {
        isSystemUser = true;
        uid = 2002;
        group = "product2";
        autoSubUidGidRange = false;
        subUidRanges = [ ];
        subGidRanges = [ ];
		home = "${candidateParent}/b/slot/home";
		shell = namespaceWrappers.sshSession.path;
		hashedPassword = "";
      };
	  environment.shells = [ namespaceWrappers.sshSession.path ];
      security.wrappers =
        pkgs.lib.mapAttrs'
          (
            _: wrapper:
            pkgs.lib.nameValuePair wrapper.name {
              inherit (wrapper) setuid;
              source = "${productionAdapter}/bin/${wrapper.target}";
              owner = "root";
              group = if wrapper.controllerOnly or false then "controller" else "root";
              permissions =
                if wrapper.controllerOnly or false then
                  "u+rx,g+rx,o-rwx"
                else
                  "u+rx,g+x,o+x";
            }
          )
          namespaceWrappers;
      environment.systemPackages = [
        productionAdapter
        pkgs.bash
        pkgs.coreutils
      ];
    };
  testScript = ''
    machine.wait_for_unit("multi-user.target")
    machine.succeed("! grep -E '^(controller|product1|product2):' /etc/subuid /etc/subgid")
    machine.succeed("test -f ${adapterPackageContract}/contract")
    machine.succeed("${lifecycle}")
  '';
  };
in
{
  inherit readinessHermetic vmTest;
}

{ pkgs, devctl, broker, stationObserver, cliConsequences ? false, nativeResetConfig ? null }:
let
  ownerRoot = "/home/bayesartre/dev/.devkit/native-broker";
  sharedSocket = "${ownerRoot}/broker.sock";
  upstream = "unix:///run/fixture-docker/docker.sock";
  fixtureSystemctl = pkgs.writeShellScript "fixture-fixed-systemctl" ''
    set -euo pipefail
    # Test-only uncertain delivery: the real fixed stop completes, then the
    # client observes failure. No production unit/controller override exists.
    if [ "$#" = 4 ] && [ "$3" = stop ] && [ "$4" = devkit-native-broker.service ] &&
      [ -e ${ownerRoot}/fixture-fail-stop-after-job ]; then
      ${pkgs.systemd}/bin/systemctl "$@"
      ${pkgs.coreutils}/bin/rm ${ownerRoot}/fixture-fail-stop-after-job
      exit 23
    fi
    exec ${pkgs.systemd}/bin/systemctl "$@"
  '';
  ownerPolicy = {
    schemaVersion = "devkit-native-broker-owner/v1";
    service = "devkit-native-broker.service";
    systemctl = "${fixtureSystemctl}";
    hostRoot = if cliConsequences then "/home/bayesartre/fixture-native-cli/declaration" else "/home/bayesartre/dev";
    stateRoot = ownerRoot;
    socket = sharedSocket;
    binary = "${broker}/bin/postgres-broker";
    inherit upstream;
    allowedImages = [ "postgres:latest" "testcontainers/ryuk:0.7.0" ];
    socketAliases = if cliConsequences then [] else [ "/workspaces/dev/dev/.devkit/native-broker/broker.sock" ];
    allowPulls = true;
    logLevel = "info";
  };
  observationManifest = pkgs.writeText "fixture-native-broker-observation.json" (builtins.toJSON {
    schemaVersion = "devkit-native-broker-observation/v1";
    inherit (ownerPolicy) service binary;
  });
  selectedObserver = stationObserver.overrideAttrs (old: {
    ldflags = (old.ldflags or []) ++ [
      "-X=devkit/cli/devctl/internal/runtime/broker.packageObservationManifest=${observationManifest}"
    ];
  });
  ownerManifest = pkgs.writeText "fixture-native-broker-owner.json" (builtins.toJSON (ownerPolicy // {
    systemdRun = "${pkgs.systemd}/bin/systemd-run";
    observationExecutable = "${selectedObserver}/bin/station-broker-observe";
  }));
  selectedDevctl = devctl.overrideAttrs (old: {
    ldflags = (old.ldflags or []) ++ [
      "-X=devkit/cli/devctl/internal/runtime/broker.packageOwnerManifest=${ownerManifest}"
    ];
  });
  ownerProbeSource = pkgs.writeText "fixture-owner-acquire.go" ''
    package main
    import (
      "context"
      "fmt"
      "os"
      "time"
      "devkit/cli/devctl/internal/runtime/broker"
    )
    func main() {
      requested := broker.Config{
        DevkitRoot: "/home/bayesartre/dev", StateRoot: "${ownerRoot}",
        Socket: "${sharedSocket}", Upstream: "${upstream}",
        AllowedImages: []string{"postgres:latest", "testcontainers/ryuk:0.7.0"},
        SocketBindAliases: []string{"/workspaces/dev/dev/.devkit/native-broker/broker.sock"},
        AllowPulls: true, LogLevel: "info",
        // Fixture budget: six serialized real observations can each take the
        // production 5s bound plus 1s reap delay under software emulation.
        // Keep production defaults, observer/readiness and unit limits intact.
        StartTimeout: 40*time.Second,
      }
      if len(os.Args) == 2 && os.Args[1] == "mismatch" {
        requested.AllowedImages = append(requested.AllowedImages, "samcli/build-python3.13")
      }
      // Bound the lock queue, the unit's 15s job and retired-state query.
      ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
      defer cancel()
      binary, err := broker.EnsureEndpointReady(ctx, requested, false)
      if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
      fmt.Println(binary)
    }
  '';
  # Test-only executable of the production ordinary acquisition helper, with
  # the same immutable owner manifest as the real packaged Devctl unit.
  ownerProbe = selectedDevctl.overrideAttrs (old: {
    pname = "devkit-native-broker-acquisition-fixture";
    subPackages = [ "cmd/fixture-owner-acquire" ];
    doCheck = false;
    postPatch = (old.postPatch or "") + ''
      mkdir -p cli/devctl/cmd/fixture-owner-acquire
      cp ${ownerProbeSource} cli/devctl/cmd/fixture-owner-acquire/main.go
    '';
    postInstall = "";
  });
  restrictedObservation = pkgs.writeText "fixture-restricted-owner.py" ''
    import os, subprocess, sys
    pid = int(sys.argv[1])
    try:
      os.readlink("/proc/%d/exe" % pid)
    except PermissionError:
      pass
    else:
      raise AssertionError("fixture did not reproduce restricted executable observation")
    completed = subprocess.run(["${ownerProbe}/bin/fixture-owner-acquire"], capture_output=True, text=True, timeout=80)
    assert completed.returncode == 0, (completed.returncode, completed.stdout, completed.stderr)
    assert completed.stdout.strip() == "${broker}/bin/postgres-broker", completed.stdout
  '';
  concurrent = pkgs.writeText "fixture-concurrent-owner.py" ''
    import concurrent.futures, subprocess, sys
    with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
      outcomes = list(pool.map(lambda _: subprocess.run(sys.argv[1:], capture_output=True, text=True, timeout=80), range(6)))
    assert len(outcomes) == 6
    assert all(result.returncode == 0 for result in outcomes), [(result.returncode, result.stdout, result.stderr) for result in outcomes]
  '';
  stopRace = pkgs.writeText "fixture-stop-acquire.py" ''
    import json, os, subprocess, sys, time
    stop = subprocess.Popen(sys.argv[2:], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    deadline = time.monotonic() + 5
    while not os.path.exists("${ownerRoot}/fixture-stop-entered"):
      assert stop.poll() is None, stop.communicate()
      assert time.monotonic() < deadline, "stop service job was not entered"
      time.sleep(0.005)
    with open("${ownerRoot}/broker.stopped.json") as f:
      assert json.load(f)["inProgress"]
    acquire = subprocess.run([sys.argv[1]], capture_output=True, timeout=2)
    assert acquire.returncode != 0 and b"inhibited" in acquire.stderr, (acquire.returncode, acquire.stderr)
    out, err = stop.communicate(timeout=12)
    with open("${ownerRoot}/fixture-stop-identity.json") as f:
      identity = json.load(f)
    assert stop.returncode == 0, (out, err, identity)
    assert not os.path.exists("${sharedSocket}")
  '';
  docker = pkgs.writeText "fixture-docker.py" ''
    import http.server, json, os, socketserver, threading
    class Server(socketserver.ThreadingUnixStreamServer): daemon_threads = True
    class Echo(socketserver.StreamRequestHandler):
      def handle(self):
        self.wfile.write(b"fixture-postgres:" + self.rfile.readline())
    echo = socketserver.ThreadingTCPServer(("127.0.0.1", 55432), Echo)
    threading.Thread(target=echo.serve_forever, daemon=True).start()
    class Handler(http.server.BaseHTTPRequestHandler):
      def reply(self, value, status=200):
        body = value if isinstance(value, bytes) else json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
      def do_GET(self):
        if self.path == "/_ping": self.reply(b"OK")
        elif self.path == "/containers/fixture-pg/json":
          self.reply({"State":{"Running":True},"NetworkSettings":{"Ports":{"5432/tcp":[{"HostPort":"55432"}]}}})
        else: self.reply(b"missing", 404)
      def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        if self.path.split("?")[0].endswith("/containers/create"):
          assert json.loads(body)["Image"] == "postgres:latest"
          self.reply({"Id":"fixture-pg","Warnings":[]}, 201)
        else: self.reply(b"", 204)
      def log_message(self, *args): pass
    server = Server("/run/fixture-docker/docker.sock", Handler)
    os.chmod("/run/fixture-docker/docker.sock", 0o666)
    server.serve_forever()
  '';
  tunnel = pkgs.writeText "fixture-postgres-tunnel.py" ''
    import http.client, json, socket, sys
    class UnixHTTP(http.client.HTTPConnection):
      def connect(self):
        self.sock = socket.socket(socket.AF_UNIX)
        self.sock.settimeout(2)
        self.sock.connect(sys.argv[1])
    client = UnixHTTP("docker", timeout=2)
    create = {"Image":"postgres:latest","HostConfig":{"PortBindings":{"5432/tcp":[{"HostIp":"","HostPort":""}]}}}
    rejected = json.loads(json.dumps(create))
    rejected["HostConfig"]["PortBindings"]["5432/tcp"][0]["HostIp"] = "127.0.0.1"
    client.request("POST", "/containers/create?name=fixture-rejected", json.dumps(rejected), {"Content-Type":"application/json"})
    response = client.getresponse()
    rejection = response.read()
    assert response.status == 403, (response.status, rejection)
    client.request("POST", "/containers/create?name=fixture-pg", json.dumps(create), {"Content-Type":"application/json"})
    response = client.getresponse()
    assert response.status == 201, (response.status, response.read())
    container = json.loads(response.read())["Id"]
    client.request("POST", "/_devkit/postgres/lease/" + container)
    response = client.getresponse()
    assert response.status == 200, (response.status, response.read())
    lease = json.loads(response.read())["lease"]
    client.close()
    stream = socket.socket(socket.AF_UNIX)
    stream.settimeout(2)
    stream.connect(sys.argv[1])
    stream.sendall(("CONNECT /_devkit/postgres/connect/" + container + " HTTP/1.1\r\nHost: docker\r\nX-Devkit-Postgres-Lease: " + lease + "\r\n\r\n").encode())
    reader = stream.makefile("rb")
    status = reader.readline()
    assert status.startswith(b"HTTP/1.1 200 "), status
    while reader.readline() != b"\r\n": pass
    stream.sendall(b"authorized-loopback-tunnel\n")
    assert reader.readline() == b"fixture-postgres:authorized-loopback-tunnel\n"
    stream.close()
  '';
  stationConfig = pkgs.writeTextDir "overlays/dev-all/devkit.yaml" (builtins.toJSON {
    native.host_root = ownerPolicy.hostRoot;
    broker = {
      socket = sharedSocket;
      inherit upstream;
      allowed_images = ownerPolicy.allowedImages;
      allow_pulls = ownerPolicy.allowPulls;
      log_level = ownerPolicy.logLevel;
    };
  });
  request = action: (pkgs.lib.optionalString cliConsequences "${pkgs.coreutils}/bin/env DEVKIT_ROOT=${stationConfig} ")
    + "${selectedDevctl}/kit/bin/devctl -p dev-all broker ${action} --upstream ${upstream}";
  consequenceNames = [
    "TestNativeTopLevelExecAndAttachPreserveSandboxExitCode"
    "TestNativeTopLevelExecProjectsStdoutAndCleansProxyOnEveryExit"
    "TestNativeTopLevelPrepareAndExecUseIsolatedRelativeMetadata"
    "TestNativeTopLevelSelectedLaneGitReciprocity"
    "TestDevAllResetReconstructsThreeSlotsThroughPackageSSHAuthority"
    "TestSelectedStandardSlotResetRebuildsGitProjectionAfterBootstrap"
    "TestNativePrepareCarriesDelayedPackThroughActualOpenSSHProxyCommand"
  ];
  consequenceTests = pkgs.buildGoModule {
    pname = "devkit-native-cli-consequence-tests";
    version = "dev";
    src = devctl.src;
    modRoot = "cli/devctl";
    vendorHash = devctl.vendorHash;
    env.CGO_ENABLED = "0";
    doCheck = false;
    buildPhase = ''
      runHook preBuild
      go test -c -o native-cli-consequence-tests ./integration
      runHook postBuild
    '';
    installPhase = ''
      mkdir -p $out/bin
      cp native-cli-consequence-tests $out/bin/
    '';
  };
  stopIdentity = pkgs.writeText "fixture-stop-identity.py" ''
    import json, os, pathlib
    state = json.loads(pathlib.Path("${ownerRoot}/broker.json").read_text())
    result = {"recorded": {k:state.get(k) for k in ["pid","startTicks","binary","ownerService"]}}
    for key, operation in {
      "start": lambda: pathlib.Path("/proc/" + str(state["pid"]) + "/stat").read_text().split(")", 1)[1].split()[19],
      "exe": lambda: os.readlink("/proc/" + str(state["pid"]) + "/exe"),
      "cgroup": lambda: pathlib.Path("/proc/" + str(state["pid"]) + "/cgroup").read_text(),
      "selfCgroup": lambda: pathlib.Path("/proc/self/cgroup").read_text(),
      "wantedExe": lambda: os.path.realpath(state["binary"]),
    }.items():
      try: result[key] = operation()
      except OSError as e: result[key] = {"errno":e.errno,"message":e.strerror}
    print(json.dumps(result, sort_keys=True))
  '';
  fixtureStop = pkgs.writeShellScript "fixture-delayed-owner-stop" ''
    ${pkgs.coreutils}/bin/touch ${ownerRoot}/fixture-stop-entered
    ${pkgs.coreutils}/bin/sleep 0.25
    ${pkgs.python3}/bin/python ${stopIdentity} > ${ownerRoot}/fixture-stop-identity.json
    # Systemd performs fixed-cgroup retirement after this test-only delay.
    exit 0
  '';
  user = command: "runuser -u bayesartre -- env XDG_RUNTIME_DIR=/run/user/1000 DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus ${command}";
in pkgs.testers.runNixOSTest ({ config, lib, ... }: {
  name = if cliConsequences then "native-cli-fixed-owner-consequences" else "native-broker-shared-lifetime";
  # Use the public test-package option with the same pinned noninteractive
  # driver. Its generated QEMU command already supports accel=kvm:tcg; this
  # contract proves real Linux/systemd custody, independent of acceleration.
  test = lib.mkForce (assert !config.sshBackdoor.enable; pkgs.runCommand "vm-test-run-${config.name}" {
    requiredSystemFeatures = [ "nixos-test" ]
      ++ lib.optionals pkgs.stdenv.hostPlatform.isDarwin [ "apple-virt" ];
    passthru = config.passthru;
    meta = config.meta;
  } ''
    mkdir -p $out
    export LOGFILE=/dev/null
    ${config.driver}/bin/nixos-test-driver -o $out
  '');
  nodes.machine = { ... }: {
    virtualisation.memorySize = 1024;
    virtualisation.cores = 2;
    users.users.bayesartre = {
      isNormalUser = true;
      uid = 1000;
      createHome = true;
    };
    environment.systemPackages = [
      selectedDevctl broker pkgs.curl pkgs.python3 pkgs.util-linux pkgs.jq
    ] ++ pkgs.lib.optionals cliConsequences [ consequenceTests pkgs.go pkgs.git pkgs.openssh pkgs.sqlite pkgs.bubblewrap pkgs.tmux pkgs.gnumake ];
    systemd.tmpfiles.rules = [
      "d /home/bayesartre/dev 0700 bayesartre users -"
      "d /home/bayesartre/dev/.devkit 0700 bayesartre users -"
      "d ${ownerRoot} 0700 bayesartre users -"
    ];
    systemd.services.fixture-docker = {
      wantedBy = [ "multi-user.target" ];
      serviceConfig = {
        RuntimeDirectory = "fixture-docker";
        ExecStart = "${pkgs.python3}/bin/python ${docker}";
      };
    };
    systemd.user.services.devkit-native-broker = {
      restartIfChanged = false;
      stopIfChanged = false;
      serviceConfig = {
        Type = "forking";
        PIDFile = "${ownerRoot}/broker.pid";
        UMask = "0077";
        Environment = [
          "HOME=/home/bayesartre"
          "DEVKIT_RUNTIME_BROKER_BINARY=${broker}/bin/postgres-broker"
        ];
        ExecStart = request "start";
        ExecStop = "${fixtureStop}";
        Restart = "no";
        KillMode = "control-group";
        TimeoutStartSec = "15s";
        TimeoutStopSec = "15s";
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = "read-only";
        ReadWritePaths = [ ownerRoot ];
        # Existing leased Postgres streams dial only IPv4 loopback.
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" ];
      };
    };
    systemd.user.services.fixture-target-a.serviceConfig = {
      ExecStart = "${broker}/bin/postgres-broker agent-proxy /home/bayesartre/dev/agent-a.sock ${sharedSocket} project-a";
      KillMode = "control-group";
    };
    systemd.user.services.fixture-target-b.serviceConfig = {
      ExecStart = "${broker}/bin/postgres-broker agent-proxy /home/bayesartre/dev/agent-b.sock ${sharedSocket} project-b";
      KillMode = "control-group";
    };
    systemd.services.fixture-old-namespace.serviceConfig = {
      ExecStart = "${pkgs.coreutils}/bin/sleep infinity";
      BindReadOnlyPaths = [ "${sharedSocket}:/run/fixture-bound.sock" ];
      KillMode = "control-group";
    };
    systemd.services.fixture-fresh-namespace.serviceConfig = {
      ExecStart = "${pkgs.coreutils}/bin/sleep infinity";
      BindReadOnlyPaths = [ "${sharedSocket}:/run/fixture-bound.sock" ];
      KillMode = "control-group";
    };
  };
  testScript = if cliConsequences then assert nativeResetConfig != null; ''
    import json, os, pathlib, shlex
    start_all()
    machine.wait_for_unit("multi-user.target")
    machine.succeed("loginctl enable-linger bayesartre; systemctl start user@1000.service")
    machine.wait_until_succeeds("test -S /run/user/1000/bus")
    machine.succeed("${user (request "start")}")
    machine.succeed("${user "systemctl --user start fixture-target-b.service"}")
    machine.wait_until_succeeds("curl --max-time 1 --unix-socket /home/bayesartre/dev/agent-b.sock http://docker/_ping | grep '^OK$'")
    retained = machine.succeed("sha256sum ${ownerRoot}/broker.json ${ownerRoot}/broker.pid; stat -c '%d:%i' ${sharedSocket}")
    machine.succeed("mkdir -p /home/bayesartre/consequence-source; cp -R ${devctl.src}/. /home/bayesartre/consequence-source/; chmod -R u+w /home/bayesartre/consequence-source; chown -R bayesartre:users /home/bayesartre/consequence-source")
    machine.succeed("${user "ln -s ${devctl.goModules} /home/bayesartre/consequence-source/cli/devctl/vendor"}")
    names = json.loads('${builtins.toJSON consequenceNames}')
    logs = []
    for name in names:
      print("native CLI consequence start: " + name, flush=True)
      # The restricted Git proof binds /nix/store, so its supplier must resolve
      # tools to their actual immutable paths rather than the system profile.
      command = "cd /home/bayesartre/consequence-source/cli/devctl/integration; PATH=${pkgs.lib.makeBinPath [ pkgs.go pkgs.git pkgs.openssh pkgs.sqlite pkgs.bubblewrap pkgs.coreutils pkgs.gnumake ]}:$PATH GOMAXPROCS=2 GOFLAGS='-mod=vendor -p=1' GOPROXY=off GOSUMDB=off TMPDIR=/tmp DEVKIT_TEST_NATIVE_STATION_OWNER_MANIFEST=${ownerManifest} DEVKIT_TEST_NATIVE_RESET_CODEX_CONFIG_SOURCE=${nativeResetConfig} ${consequenceTests}/bin/native-cli-consequence-tests -test.v -test.count=1 -test.run '^" + name + "$'"
      # Preserve the child's original output even if its exit is nonzero.
      code, output = machine.execute("${user "${pkgs.bash}/bin/bash"} -c " + shlex.quote(command))
      pathlib.Path(os.environ["out"], name + ".log").write_text(output)
      print(output, end="", flush=True)
      print("native CLI consequence terminal: " + name + " exit=" + str(code), flush=True)
      assert code == 0, (name, code, output)
      assert "--- PASS: " + name + " (" in output and "--- SKIP:" not in output, output
      logs.append(output)
      assert machine.succeed("sha256sum ${ownerRoot}/broker.json ${ownerRoot}/broker.pid; stat -c '%d:%i' ${sharedSocket}") == retained
      machine.succeed("curl --max-time 1 --unix-socket /home/bayesartre/dev/agent-b.sock http://docker/_ping | grep '^OK$'")
    pathlib.Path(os.environ["out"], "native-cli-consequences.log").write_text("\n".join(logs))
    pathlib.Path(os.environ["out"], "native-cli-consequences.json").write_text(json.dumps({"tests": names, "result": "PASS", "ownerManifest": "${ownerManifest}", "sharedOwnerPreserved": True, "siblingReadyAfterEach": True}, sort_keys=True))
  '' else ''
    import json
    start_all()
    machine.wait_for_unit("multi-user.target")
    machine.succeed("loginctl enable-linger bayesartre; systemctl start user@1000.service")
    machine.wait_until_succeeds("test -S /run/user/1000/bus")
    machine.succeed("echo retained-goal > /home/bayesartre/dev/goal; echo dirty-work > /home/bayesartre/dev/dirty; chown bayesartre:users /home/bayesartre/dev/goal /home/bayesartre/dev/dirty")
    snapshot = machine.succeed("sha256sum /home/bayesartre/dev/goal /home/bayesartre/dev/dirty")
    machine.fail("${user "${ownerProbe}/bin/fixture-owner-acquire mismatch"}")
    machine.succeed("test ! -e ${sharedSocket}; test ! -e ${ownerRoot}/broker.pid")
    machine.succeed("${user "${pkgs.python3}/bin/python ${concurrent} ${ownerProbe}/bin/fixture-owner-acquire"}")
    original = json.loads(machine.succeed("cat ${ownerRoot}/broker.json"))
    machine.succeed("grep 'devkit-native-broker.service' /proc/" + str(original["pid"]) + "/cgroup")
    machine.succeed("${user "${pkgs.python3}/bin/python ${concurrent} ${ownerProbe}/bin/fixture-owner-acquire"}")
    assert json.loads(machine.succeed("cat ${ownerRoot}/broker.json")) == original

    # Private VM process-loss fixtures retain real state/start/socket identity
    # while systemd retires PIDFile. Exercise ordinary acquisition, the unit's
    # own activating ExecStart, and typed stop without touching a live station.
    for recovery in ["acquire", "unit-start", "typed-stop"]:
      lost = json.loads(machine.succeed("cat ${ownerRoot}/broker.json"))
      retained = machine.succeed("sha256sum ${ownerRoot}/broker.json; stat -c '%d:%i' ${sharedSocket}")
      machine.succeed("${user "systemctl --user kill --kill-whom=main --signal=KILL devkit-native-broker.service"}")
      machine.wait_until_succeeds("${user "systemctl --user show -p MainPID --value devkit-native-broker.service"} | grep '^0$'")
      machine.wait_until_succeeds("test ! -e ${ownerRoot}/broker.pid; test ! -e /proc/" + str(lost["pid"]))
      assert machine.succeed("sha256sum ${ownerRoot}/broker.json; stat -c '%d:%i' ${sharedSocket}") == retained
      machine.succeed("test ! -e ${ownerRoot}/broker.stopped.json")
      machine.fail("curl --max-time 1 --unix-socket ${sharedSocket} http://docker/_ping")
      if recovery == "acquire":
        machine.succeed("${user "${ownerProbe}/bin/fixture-owner-acquire"}")
      elif recovery == "unit-start":
        machine.succeed("${user "systemctl --user start devkit-native-broker.service"}")
      else:
        machine.succeed("${user (request "stop")}")
        machine.succeed("test ! -e ${sharedSocket}; test ! -e ${ownerRoot}/broker.json; test ! -e ${ownerRoot}/broker.pid")
        assert not json.loads(machine.succeed("cat ${ownerRoot}/broker.stopped.json"))["inProgress"]
        machine.fail("${user "${ownerProbe}/bin/fixture-owner-acquire"}")
        machine.succeed("${user (request "start")}")
      recovered = json.loads(machine.succeed("cat ${ownerRoot}/broker.json"))
      assert (recovered["pid"], recovered["startTicks"]) != (lost["pid"], lost["startTicks"])
      machine.succeed("curl --max-time 1 --unix-socket ${sharedSocket} http://docker/_ping | grep '^OK$'")
    original = recovered

    # Two independent restricted consumers must observe the retained host owner
    # through the actual packaged helper while direct Readlink is unavailable.
    for _ in range(2):
      machine.succeed("${user "${pkgs.bubblewrap}/bin/bwrap --unshare-user --uid 1000 --gid 100 --ro-bind / / --dev /dev --proc /proc ${pkgs.python3}/bin/python ${restrictedObservation}"} " + str(original["pid"]))
      assert json.loads(machine.succeed("cat ${ownerRoot}/broker.json")) == original
    machine.fail("${user "${selectedObserver}/bin/station-broker-observe"} " + str(original["pid"]) + " " + str(original["startTicks"] + 1))
    machine.fail("${user "${selectedObserver}/bin/station-broker-observe 1 1"}")

    machine.succeed("${user "systemctl --user start fixture-target-a.service fixture-target-b.service"}")
    for endpoint in ["agent-a.sock", "agent-b.sock"]:
      machine.wait_until_succeeds("curl --max-time 1 --unix-socket /home/bayesartre/dev/" + endpoint + " http://docker/_ping | grep '^OK$'")
    machine.succeed("${pkgs.python3}/bin/python ${tunnel} /home/bayesartre/dev/agent-a.sock")
    machine.succeed("${user "systemctl --user stop fixture-target-a.service"}")
    machine.succeed("curl --max-time 1 --unix-socket /home/bayesartre/dev/agent-b.sock http://docker/_ping | grep '^OK$'")
    assert json.loads(machine.succeed("cat ${ownerRoot}/broker.json")) == original
    machine.succeed("systemctl start fixture-old-namespace.service")
    old_pid = machine.succeed("systemctl show -p MainPID --value fixture-old-namespace.service").strip()
    old_identity = machine.succeed("stat -c '%d:%i' /proc/" + old_pid + "/root/run/fixture-bound.sock").strip()
    machine.succeed("${user "${pkgs.python3}/bin/python ${stopRace} ${ownerProbe}/bin/fixture-owner-acquire ${request "stop"}"}")
    machine.fail("${user (request "status")} | grep 'running: true'")
    machine.succeed("test -f ${ownerRoot}/broker.stopped.json")
    machine.fail("${user "${ownerProbe}/bin/fixture-owner-acquire"}")
    machine.fail("${user "systemctl --user start devkit-native-broker.service"}")
    machine.succeed("test ! -e ${sharedSocket}")
    machine.succeed("${user "systemctl --user reset-failed devkit-native-broker.service"}")
    machine.succeed("${user (request "start")}")
    replacement = json.loads(machine.succeed("cat ${ownerRoot}/broker.json"))
    assert replacement["pid"] != original["pid"]
    new_identity = machine.succeed("stat -c '%d:%i' ${sharedSocket}").strip()
    assert old_identity != new_identity
    assert machine.succeed("stat -c '%d:%i' /proc/" + old_pid + "/root/run/fixture-bound.sock").strip() == old_identity
    machine.fail("curl --max-time 1 --unix-socket /proc/" + old_pid + "/root/run/fixture-bound.sock http://docker/_ping")
    machine.succeed("systemctl start fixture-fresh-namespace.service")
    fresh_pid = machine.succeed("systemctl show -p MainPID --value fixture-fresh-namespace.service").strip()
    assert machine.succeed("stat -c '%d:%i' /proc/" + fresh_pid + "/root/run/fixture-bound.sock").strip() == new_identity
    machine.succeed("curl --max-time 1 --unix-socket /proc/" + fresh_pid + "/root/run/fixture-bound.sock http://docker/_ping | grep '^OK$'")
    machine.succeed("${user "systemctl --user kill --signal=KILL fixture-target-b.service"}")
    machine.succeed("curl --max-time 1 --unix-socket ${sharedSocket} http://docker/_ping | grep '^OK$'")
    machine.succeed("${user "${pkgs.coreutils}/bin/touch ${ownerRoot}/fixture-fail-stop-after-job"}")
    machine.fail("${user (request "stop")}")
    pending = json.loads(machine.succeed("cat ${ownerRoot}/broker.stopped.json"))
    assert pending["inProgress"] and pending["recordedPID"] == replacement["pid"]
    machine.succeed("test ! -e ${ownerRoot}/broker.pid")
    machine.fail("${user "${ownerProbe}/bin/fixture-owner-acquire"}")
    machine.succeed("${user (request "stop")}")
    assert not json.loads(machine.succeed("cat ${ownerRoot}/broker.stopped.json"))["inProgress"]
    machine.fail("${user "${ownerProbe}/bin/fixture-owner-acquire"}")
    machine.succeed("${user (request "start")}")
    machine.succeed("curl --max-time 1 --unix-socket ${sharedSocket} http://docker/_ping | grep '^OK$'")
    assert machine.succeed("sha256sum /home/bayesartre/dev/goal /home/bayesartre/dev/dirty") == snapshot
  '';
})

{ pkgs, devctl, broker }:
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
  ownerManifest = pkgs.writeText "fixture-native-broker-owner.json" (builtins.toJSON {
    schemaVersion = "devkit-native-broker-owner/v1";
    service = "devkit-native-broker.service";
    systemctl = "${fixtureSystemctl}";
    hostRoot = "/home/bayesartre/dev";
    stateRoot = ownerRoot;
    socket = sharedSocket;
    binary = "${broker}/bin/postgres-broker";
    inherit upstream;
    allowedImages = [ "postgres:latest" "testcontainers/ryuk:0.7.0" ];
    socketAliases = [ "/workspaces/dev/dev/.devkit/native-broker/broker.sock" ];
    allowPulls = true;
    logLevel = "info";
  });
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
      }
      if len(os.Args) == 2 && os.Args[1] == "mismatch" {
        requested.AllowedImages = append(requested.AllowedImages, "samcli/build-python3.13")
      }
      ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
  concurrent = pkgs.writeText "fixture-concurrent-owner.py" ''
    import concurrent.futures, subprocess, sys
    with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
      outcomes = list(pool.map(lambda _: subprocess.run(sys.argv[1:], check=True, capture_output=True, timeout=12), range(6)))
    assert len(outcomes) == 6
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
  request = action: "${selectedDevctl}/kit/bin/devctl -p dev-all broker ${action} --upstream ${upstream}";
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
in pkgs.testers.runNixOSTest {
  name = "native-broker-shared-lifetime";
  nodes.machine = { ... }: {
    virtualisation.memorySize = 1024;
    users.users.bayesartre = {
      isNormalUser = true;
      uid = 1000;
      createHome = true;
    };
    environment.systemPackages = [
      selectedDevctl broker pkgs.curl pkgs.python3 pkgs.util-linux pkgs.jq
    ];
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
  testScript = ''
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
}

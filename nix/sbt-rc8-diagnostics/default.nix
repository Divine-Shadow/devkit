{ pkgs }:
let
  lib = pkgs.lib;
  identity = builtins.fromJSON (builtins.readFile ./source.json);
  hashes = builtins.fromJSON (builtins.readFile ./dependency-hashes.json);
  dependencyHash = hashes.${pkgs.system} or null;
  source = builtins.fetchTree {
    type = "git";
    url = "https://github.com/sbt/sbt.git";
    rev = identity.upstreamRevision;
    narHash = identity.upstreamNarHash;
  };
  common = {
    pname = "sbt-rc8-owned-test-diagnostics";
    version = "2.0.0-RC8-diagnostics";
    src = source.outPath;
    patches = [ ./owned-test-event-diagnostics.patch ./reproducible-version-resource.patch ];
    nativeBuildInputs = [
      pkgs.jdk21_headless pkgs.sbt pkgs.git pkgs.python3 pkgs.findutils
    ];
    # These limits are part of this producer, independent of parent scheduling.
    enableParallelBuilding = false;
    preBuild = ''
      export HOME="$TMPDIR/home"
      export COURSIER_CACHE="$TMPDIR/caches/coursier"
      export SBT_GLOBAL_BASE="$TMPDIR/caches/global"
      export SBT_BOOT_DIR="$TMPDIR/caches/boot"
      export SBT_IVY_HOME="$TMPDIR/caches/ivy"
      export XDG_CACHE_HOME="$TMPDIR/caches/xdg"
      export JAVA_HOME=${pkgs.jdk21_headless}
      export CI=true TZ=UTC LC_ALL=C.UTF-8 SOURCE_DATE_EPOCH=1767594037
      unset BUILD_VERSION
      mkdir -p "$HOME" "$COURSIER_CACHE" "$SBT_GLOBAL_BASE" "$SBT_BOOT_DIR" "$SBT_IVY_HOME" "$XDG_CACHE_HOME"
      test "$(cat project/build.properties)" = 'sbt.version=1.11.6'
      cp ${./repositories} "$TMPDIR/repositories"
      sbt_source() {
        "$JAVA_HOME/bin/java" -Xms256m -Xmx2G -Xss4m -XX:ActiveProcessorCount=2 \
          -Dsbt.version=1.11.6 -Dsbt.task.cpus=2 -Dsbt.supershell=false -Dsbt.server.autostart=false \
          -Dsbt.global.base="$SBT_GLOBAL_BASE" -Dsbt.boot.directory="$SBT_BOOT_DIR" \
          -Dsbt.ivy.home="$SBT_IVY_HOME" -Dsbt.coursier.home="$COURSIER_CACHE" \
          -Dsbt.override.build.repos=true -Dsbt.repository.config="$TMPDIR/repositories" \
          -Duser.home="$HOME" -Dfile.encoding=UTF-8 -Duser.timezone=UTC \
          -jar ${pkgs.sbt}/share/sbt/bin/sbt-launch.jar \
          'set ThisBuild / version := "2.0.0-RC8"' \
          'set ThisBuild / packageTimestamp := Some(1767594037000L)' "$@"
      }
    '';
    meta = {
      description = "Source-built RC8 actions diagnostics; unselected generic upstream toolchain";
      license = lib.licenses.asl20;
      platforms = [ "x86_64-linux" ];
    };
  };
  mkDependencies = hash: pkgs.stdenv.mkDerivation (common // {
    pname = "sbt-rc8-diagnostic-source-dependencies";
    outputHashAlgo = "sha256";
    outputHashMode = "recursive";
    outputHash = hash;
    buildPhase = ''
      runHook preBuild
      # Only this fixed-output derivation has network access. In actual80336
      # discovery, dependencyClasspath compiled transitive modules and17 actions
      # sources. Formatters also mutate this temporary source. Neither target/
      # products nor those formatted source bytes are copied into the cache.
      sbt_source 'update' 'Test / update' \
        'actionsProj / Test / dependencyClasspath' \
        'actionsProj / Compile / scalafmt' 'actionsProj / Test / scalafmt' \
        'actionsProj / Compile / javafmt' 'actionsProj / Test / javafmt'
      runHook postBuild
    '';
    installPhase = ''
      mkdir -p "$out"
      cp -a "$TMPDIR/caches/." "$out/"
      python3 ${./normalize-dependencies.py} "$out"
    '';
    dontFixup = true;
  });
  dependencies =
    if dependencyHash == null then
      throw "SBT diagnostic dependency hash is unqualified. Allocate deps-discovery, preserve the actual mismatch receipt, review and commit its hash before building the offline package."
    else mkDependencies dependencyHash;
  package = pkgs.stdenv.mkDerivation (common // {
    pname = "sbt-rc8-diagnostic-actions-maven-repository";
    buildPhase = ''
      runHook preBuild
      set -o pipefail
      python3 ${./test-normalize-dependencies.py} ${./normalize-dependencies.py}
      python3 -B ${./test-bind-sources.py} ${./bind-sources.py}
      cp -a ${dependencies}/. "$TMPDIR/caches/"
      chmod -R u+w "$TMPDIR/caches"
      export COURSIER_MODE=offline
      mkdir -p "$TMPDIR/evidence"
      test ! -e main-actions/target
      python3 ${./bind-sources.py} capture "$PWD" "$TMPDIR/evidence/source-patched"
      # Ordinary non-fixed-output Nix sandbox: missing dependencies fail closed.
      # Explicit deterministic normal upstream transform, then bind the actual
      # sources to be compiled. No test/fork/scheduling override.
      sbt_source 'set ThisBuild / offline := true' \
        'actionsProj / Compile / scalafmt' 'actionsProj / Test / scalafmt' \
        'actionsProj / Compile / javafmt' 'actionsProj / Test / javafmt'
      python3 ${./bind-sources.py} capture "$PWD" "$TMPDIR/evidence/source-effective"
      sbt_source 'set ThisBuild / offline := true' \
        'actionsProj / Compile / scalafmtCheck' 'actionsProj / Test / scalafmtCheck' \
        'actionsProj / Compile / javafmtCheck' 'actionsProj / Test / javafmtCheck' \
        'actionsProj / Test / testOnly sbt.ForkTestsDiagnosticsTest sbt.internal.WorkerExchangeTest' \
        'actionsProj / publishM2' 2>&1 | tee "$TMPDIR/source-build-and-tests.log"
      python3 ${./bind-sources.py} check "$PWD" "$TMPDIR/evidence/source-effective"
      python3 ${./bind-sources.py} check-generated "$PWD" \
        "$TMPDIR/evidence/generated-version-resources.json" --epoch 1767594037
      runHook postBuild
    '';
    installPhase = ''
      mkdir -p "$out/repository/org/scala-sbt" "$out/evidence"
      cp -a "$HOME/.m2/repository/org/scala-sbt/actions_3" "$out/repository/org/scala-sbt/"
      test -s "$out/repository/org/scala-sbt/actions_3/2.0.0-RC8/actions_3-2.0.0-RC8.jar"
      cp ${./source.json} "$out/evidence/source.json"
      cp ${./dependency-hashes.json} "$out/evidence/dependency-hashes.json"
      cp -a "$TMPDIR/evidence/." "$out/evidence/"
      # Complete raw logs remain in the owning Nix -L runner/courier. Their
      # wall-clock/timing/path bytes are deliberately outside functional output.
      cd "$out"
      find repository -type f -print0 | sort -z | xargs -0 sha256sum > evidence/artifacts.sha256
    '';
    dontFixup = true;
    passthru = { inherit dependencies; };
  });
in assert builtins.hashFile "sha256" ./owned-test-event-diagnostics.patch == identity.patchSHA256;
   assert builtins.hashFile "sha256" ./reproducible-version-resource.patch == identity.reproducibleVersionPatchSHA256; {
  inherit package dependencies;
  # Deliberately non-accepting first materialization. A hash mismatch is a
  # discovery receipt, never a qualified build or automatically trusted hash.
  deps-discovery = mkDependencies lib.fakeHash;
  dependency-normalization-check = pkgs.runCommand "sbt-rc8-dependency-normalization-test" {
    nativeBuildInputs = [ pkgs.python3 ];
  } ''
    python3 ${./test-normalize-dependencies.py} ${./normalize-dependencies.py}
    python3 -B ${./test-bind-sources.py} ${./bind-sources.py}
    mkdir -p "$out"
    printf '%s\n' 'dependency cache normalization tests passed' > "$out/README"
  '';
}

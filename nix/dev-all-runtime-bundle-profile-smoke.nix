{
  bundle,
  pkgs,
}:

let
  aggregateProfile = pkgs.buildEnv {
    name = "dev-all-runtime-bundle-aggregate-profile";
    paths = [ bundle ];
    pathsToLink = [ "/bin" ];
  };
in
pkgs.runCommand "dev-all-runtime-bundle-profile-smoke" {
  nativeBuildInputs = with pkgs; [
    bash
    coreutils
    jq
  ];
} ''
  set -euo pipefail

  direct='${bundle}/bin/dev-all-runtime-bundle'
  profile='${aggregateProfile}/bin/dev-all-runtime-bundle'
  identity_env='${bundle}/share/dev-all-runtime-bundle/identity.env'
  hook="$TMPDIR/hostile-startup-hook"
  sentinel="$TMPDIR/startup-hook-executed"

  test -L "$profile"
  test ! -e '${aggregateProfile}/share/dev-all-runtime-bundle/identity.env'
  cat > "$hook" <<EOF
  printf '%s\n' executed > '$sentinel'
  exit 97
  EOF
  chmod 0644 "$hook"

  run_profile() {
    label="$1"
    shift
    rm -f "$sentinel"
    env BASH_ENV="$hook" ENV="$hook" "$profile" "$@" > "$TMPDIR/profile-$label"
    test ! -e "$sentinel"
  }

  run_profile validate validate
  run_profile identity-env identity-env
  run_profile identity-json identity-json
  run_profile identity-fingerprint identity-fingerprint
  run_profile identity-nul identity-nul
  run_profile plugin-smoke plugin-smoke

  "$direct" identity-env > "$TMPDIR/direct-identity-env"
  "$direct" identity-json > "$TMPDIR/direct-identity-json"
  "$direct" identity-fingerprint > "$TMPDIR/direct-identity-fingerprint"
  "$direct" identity-nul > "$TMPDIR/direct-identity-nul"
  "$direct" plugin-smoke > "$TMPDIR/direct-plugin-smoke"
  cmp "$TMPDIR/direct-identity-env" "$TMPDIR/profile-identity-env"
  cmp "$TMPDIR/direct-identity-json" "$TMPDIR/profile-identity-json"
  cmp "$TMPDIR/direct-identity-fingerprint" "$TMPDIR/profile-identity-fingerprint"
  cmp "$TMPDIR/direct-identity-nul" "$TMPDIR/profile-identity-nul"
  cmp "$TMPDIR/direct-plugin-smoke" "$TMPDIR/profile-plugin-smoke"
  jq -e --arg bundle '${bundle}' '.bundlePath == $bundle' "$TMPDIR/profile-identity-json" >/dev/null

  rm -f "$sentinel"
  env BASH_ENV="$hook" ENV="$hook" "$profile" exec '${pkgs.dash}/bin/dash' -c \
    'printf "%s\n" profile-exec-ok' > "$TMPDIR/profile-exec"
  test ! -e "$sentinel"
  grep -Fx profile-exec-ok "$TMPDIR/profile-exec" >/dev/null
  env BASH_ENV="$hook" ENV="$hook" "$direct" exec '${pkgs.dash}/bin/dash' -c \
    'printf "%s\n" profile-exec-ok' > "$TMPDIR/direct-exec"
  test ! -e "$sentinel"
  cmp "$TMPDIR/direct-exec" "$TMPDIR/profile-exec"

  forwarder="$TMPDIR/forwarder"
  cat > "$forwarder" <<'EOF'
  #!${pkgs.bash}/bin/bash
  set -euo pipefail
  test "''${DEVKIT_GOVERNANCE_ENV:-}" = '${bundle}/share/dev-all-runtime-bundle/identity.env'
  test -z "''${BASH_ENV:-}"
  test -z "''${ENV:-}"
  printf '%s\n' profile-governance-forward-ok
  EOF
  chmod 0555 "$forwarder"
  rm -f "$sentinel"
  env BASH_ENV="$hook" ENV="$hook" "$profile" governance-forward "$forwarder" \
    > "$TMPDIR/profile-governance-forward"
  test ! -e "$sentinel"
  grep -Fx profile-governance-forward-ok "$TMPDIR/profile-governance-forward" >/dev/null
  env BASH_ENV="$hook" ENV="$hook" "$direct" governance-forward "$forwarder" \
    > "$TMPDIR/direct-governance-forward"
  test ! -e "$sentinel"
  cmp "$TMPDIR/direct-governance-forward" "$TMPDIR/profile-governance-forward"

  mkdir -p "$out"
  cp "$TMPDIR/profile-identity-fingerprint" "$out/identity-fingerprint"
  cat > "$out/evidence.txt" <<EOF
  bundle=${bundle}
  aggregate_profile=${aggregateProfile}
  aggregate_profile_has_identity_env=false
  operations=validate,identity-env,identity-json,identity-fingerprint,identity-nul,plugin-smoke,exec,governance-forward
  startup_hook_sabotage=BASH_ENV_and_ENV_not_executed
  direct_profile_equivalence=passed
  EOF
''

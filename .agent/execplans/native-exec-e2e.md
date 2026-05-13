# Native Exec End-to-End Runtime

This ExecPlan tracks the slice that makes native Nix execution a reliable
wrapper-to-sandbox runtime path. The user-visible result is that
`kit/scripts/devkit` can run real commands inside bubblewrap-backed Nix shells
without Nix flake registry or certificate retry noise, and `dev-all` proves the
Spago, Netlify, and Playwright toolchain through that same path.

## Progress

- [x] (2026-05-13T15:35:00Z) Reproduced the canary path through
  `kit/scripts/devkit -p _template exec`; it reached bubblewrap and
  `nix develop ./overlays/_template#default`, but emitted registry/certificate
  retry warnings before succeeding.
- [x] (2026-05-13T15:42:00Z) Used subagents to inspect the launcher and smoke
  layout. One confirmed the warning root cause in missing `/etc/static` and
  CA binds; the other recommended extending `kit/scripts/native-runtime-smoke`.
- [x] (2026-05-13T15:49:00Z) Added optional native bubblewrap binds for
  `/etc/static`, `/etc/ssl`, and `/etc/pki`, with launcher test coverage.
- [x] (2026-05-13T15:54:00Z) Extended `kit/scripts/native-runtime-smoke` with
  `_template` overlay-local native exec and `dev-all` native exec toolchain
  checks.
- [x] (2026-05-13T15:56:00Z) Fixed host Docker setup in the smoke so it works
  when invoked from the repo Nix shell.
- [x] (2026-05-13T16:00:00Z) Fixed native launcher temp-dir handling for broker
  sockets under `/tmp` and inherited outer `TMPDIR`.
- [x] (2026-05-13T16:04:00Z) Hardened the tmux bell integration test exposed by
  the required full Go suite.
- [x] (2026-05-13T16:05:00Z) Ran the verification gate and recorded evidence.
- [ ] Commit the completed slice.

## Surprises & Discoveries

- Observation: NixOS `/etc/nix/registry.json` and certificate files are symlinks
  through `/etc/static` into the store. Binding `/etc/nix` alone makes the path
  appear present but broken inside bubblewrap, so Nix attempts to fetch the
  online flake registry and reports SSL certificate errors.
  Evidence: `_template` native exec printed missing registry and SSL issuer
  warnings; a subagent verified `/etc/static` and `/etc/ssl` were absent from
  the dry-run launcher command.

- Observation: Running `make native-runtime-smoke` inside the repo Nix shell
  inherits `DOCKER_HOST=unix:///run/devkit/test-container-broker.sock` from the
  shell hook, which is correct for agents but wrong for host smoke setup before
  the broker starts.
  Evidence: The first smoke attempt failed in `require_host_docker` by trying
  to contact `/run/devkit/test-container-broker.sock`; the script now uses an
  explicit host Docker socket for host setup and cleanup.

- Observation: The first `_template` smoke used stdin-fed `bash -s`, which made
  the failure look like a command transport problem. Switching to `bash -lc`
  matched the manually proven command shape, but the temp-file failure
  persisted until the launcher environment was fixed.
  Evidence: Both command forms failed with `creating temporary file
  '/tmp/nix-shell...': No such file or directory` under the outer Nix shell
  before `TMPDIR=/tmp` was added to native plans.

- Observation: The temp-file failure was caused by launcher argument ordering
  when a broker socket lives under `/tmp`: bubblewrap mounted `--tmpfs /tmp`
  and then the recursive directory creation emitted `--dir /tmp`.
  Evidence: The smoke dry-run showed both arguments; the launcher now marks
  `/tmp` as already provided by tmpfs and only creates child directories such
  as `/tmp/devkit-native-smoke.*`.

- Observation: A native agent launched from an outer `nix develop` can inherit
  `TMPDIR=/tmp/nix-shell.*`, but bubblewrap gives the agent a fresh `/tmp`
  where that host path does not exist.
  Evidence: `make native-runtime-smoke` continued to fail with
  `creating temporary file '/tmp/nix-shell...': No such file or directory`
  after the duplicate `/tmp` directory argument was removed; native plans now
  set `TMPDIR=/tmp` explicitly.

- Observation: Full Go tests exposed brittle tmux integration test setup around
  the temporary wrapper and attached client.
  Evidence: `TestTMUXBellIntegration` consistently failed to find the wrapper
  socket session. The test now replaces `PATH` instead of appending a duplicate
  environment variable, starts an explicit shell in the tmux session, and sends
  the bell without a fragile `script attach` client.

## Decision Log

- Decision: Preserve host Nix behavior by read-only binding host Nix/CA support
  paths instead of disabling registries.
  Rationale: Disabling registries avoids the warning but breaks indirect refs
  such as `flake:nixpkgs`; the native launcher should behave like host Nix where
  possible.
  Date/Author: 2026-05-13 / Codex

- Decision: Put the wrapper-to-sandbox smoke in `kit/scripts/native-runtime-smoke`.
  Rationale: `overlay-runtime-smoke` proves `nix develop` directly; the missing
  evidence is canonical wrapper -> devctl -> native exec -> bubblewrap -> Nix
  shell.
  Date/Author: 2026-05-13 / Codex

## Validation and Acceptance

Acceptance requires:

- `kit/scripts/devkit -p _template exec 1 --repo ouroboros-ide -- ...` reports
  `DEVKIT_NIX_SHELL=template-agent` and emits no flake registry or certificate
  retry warnings.
- `kit/scripts/devkit -p dev-all exec 1 --repo ouroboros-ide -- ...` proves
  Spago, Netlify, and Playwright through native exec.
- `make native-runtime-smoke` includes both wrapper-to-sandbox checks.
- `go test -count=1 ./...` passes under `cli/devctl`.
- `nix flake check`, `make overlay-runtime-smoke`, and
  `kit/scripts/devkit image-matrix --all --check` pass.
- Representative dry-runs still show root refs for `dev-all` and the
  overlay-local canary ref for `_template`.
- `find overlays -maxdepth 2 -name flake.lock -print` produces no output.

## Outcomes & Retrospective

The implementation made native bubblewrap launchers expose host Nix registry
and CA symlink support paths, set `TMPDIR=/tmp` inside native agents, and avoid
recreating `/tmp` after mounting it as a tmpfs. `kit/scripts/native-runtime-smoke`
now proves `_template` overlay-local native exec through the canonical wrapper
without registry/certificate retry warnings and proves `dev-all` Spago,
Netlify, and Playwright through native exec. The smoke also uses the real host
Docker socket for host setup/cleanup even when invoked from a Nix shell whose
shell hook sets `DOCKER_HOST` for agents.

Gate evidence:

    kit/scripts/devkit -p _template exec 1 --repo ouroboros-ide -- sh -lc 'test "${DEVKIT_NATIVE_AGENT:-}" = 1 && test "${DEVKIT_NIX_SHELL:-}" = template-agent && test ! -e /var/run/docker.sock && printf "overlay=%s shell=%s repo=%s\n" "$DEVKIT_NATIVE_AGENT" "$DEVKIT_NIX_SHELL" "$(basename "$PWD")" && codex --version && uv --version && python3 --version'
    Result: exited 0; output included `overlay=1 shell=template-agent repo=ouroboros-ide`, `codex-cli 0.130.0`, `uv 0.7.22`, and `Python 3.12.12`; grep for registry/certificate retry strings produced no matches.

    kit/scripts/devkit -p dev-all exec 1 --repo ouroboros-ide --flake .#dev-all -- bash -lc 'purs --version && spago --version && netlify --version && deno --version && playwright --version && node ...'
    Result: exited 0; output included `0.15.15`, `0.93.45`, `netlify-cli/26.0.1`, `Version 1.58.2`, and `native-exec-playwright-ok`; grep for registry/certificate retry strings produced no matches.

    nix --extra-experimental-features 'nix-command flakes' develop --command make native-runtime-smoke
    Result: exited 0 with `native-runtime-smoke: ok`; the new smoke output included `_template` `shell=template-agent`, `dev-all` `netlify-cli/26.0.1`, and `native-exec-playwright-ok`.

    nix --extra-experimental-features 'nix-command flakes' develop --command sh -c 'cd cli/devctl && go test -count=1 ./...'
    Result: exited 0; all Go packages passed.

    nix --extra-experimental-features 'nix-command flakes' flake check
    Result: exited 0; root checks and overlay runtime metadata check passed.

    nix --extra-experimental-features 'nix-command flakes' develop --command make overlay-runtime-smoke
    Result: exited 0 with `overlay-runtime-smoke: ok`.

    kit/scripts/devkit image-matrix --all --check
    Result: exited 0 with `image-matrix: OK`; `_template` reported `./overlays/_template#default` and production overlays kept root refs.

    kit/scripts/devkit --dry-run -p dev-all native plan --repo ouroboros-ide
    kit/scripts/devkit --dry-run -p _template native plan --repo ouroboros-ide
    kit/scripts/devkit --dry-run -p dev-all ensure-ready --repo ouroboros-ide --runtime-only
    kit/scripts/devkit --dry-run -p _template ensure-ready --repo ouroboros-ide --runtime-only
    Result: all exited 0; `dev-all` printed `flake: .#dev-all`, `_template` printed `flake: ./overlays/_template#default`, and native plans included `TMPDIR=/tmp`.

    find overlays -maxdepth 2 -name flake.lock -print
    git diff --check
    Result: both exited 0; `find` produced no output.

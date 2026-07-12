# Nix Native Retired Runtime Hardening

This ExecPlan is a living document. It is maintained according to `.agent/PLANS.md`.

## Purpose

Nix-native runtime should be the only live operational path. Retired Compose,
container-exec, codexw, fallback-wrapper, and legacy runtime assumptions should
be absent from live source and active docs, except for explicit rejection tests
and historical material quarantined under `documentation/archive/compose-retirement/`.
The supported entrypoint remains `kit/scripts/devkit`, which execs
`kit/bin/devctl`.

## Progress

- [x] (2026-05-15T05:21:59Z) Audited current guard coverage, wrapper behavior,
  live references, and active docs.
- [x] (2026-05-15T05:21:59Z) Ran two independent subagent audit passes over
  live source/scripts and docs/tests/guards.
- [x] (2026-05-15T05:59:32Z) Tightened guard coverage for active
  documentation drift.
- [x] (2026-05-15T05:59:32Z) Quarantined or rewrote active docs that still
  described retired runtime operation.
- [x] (2026-05-15T05:59:32Z) Ran required verification gates and prepared the
  scoped hardening commit.

## Surprises & Discoveries

- Live code already hard-rejects `--compose-project` and the retired `compose`
  namespace before any retired runtime files are needed.
- The current `kit/scripts/devkit` wrapper is already a thin binary exec shim
  with no fallback path.
- The existing retired runtime guard blocks live Compose files, compose command
  packages, docker compose spellings, codexw, and old absolute codex paths, but
  it does not check active docs for runtime-specific Compose/container-exec
  guidance.
- `docs/layout-apply-window-reuse.md` is an active Compose-era note that still
  instructs container and `docker exec` behavior. It has no live references and
  belongs in the compose-retirement archive.
- Active docs also contain two posture conflicts: a Nix contract line naming
  retired Compose namespace removal, and an old migration note saying a safe
  fallback remains.

## Decision Log

- Decision: Add a docs-only retirement check to `retired-runtime-guard`.
  Rationale: Live source checks should stay precise to avoid false positives,
  but active docs should not drift back into executable retired-runtime
  guidance.
- Decision: Move the layout-window-reuse note to the compose-retirement archive
  instead of deleting it.
  Rationale: The note is historical migration context, not supported runtime
  documentation.

## Acceptance

Acceptance requires:

- `kit/scripts/retired-runtime-guard` blocks active docs that mention
  runtime-specific Compose, docker-compose, codexw, fallback-wrapper, safe
  fallback, or direct container-exec guidance outside
  `documentation/archive/compose-retirement/`.
- Active docs no longer describe retired runtime operation as executable
  guidance.
- `kit/scripts/devkit` remains the canonical no-fallback wrapper to
  `kit/bin/devctl`.
- Existing Nix-first overlay, brokered Docker, degraded readiness, and Codex
  home preservation behavior remains verified.
- Required native and guard verification passes.

## Outcomes & Retrospective

The retired runtime guard now has a docs-only scan that blocks active
documentation from reintroducing runtime-specific Compose, docker-compose,
codexw, fallback-wrapper, safe-fallback, or direct container-exec guidance
outside `documentation/archive/**`. The stale layout-window-reuse note was
moved into `documentation/archive/compose-retirement/kit-docs/` with a
historical banner. Active Nix contract and migration docs were reworded to avoid
presenting retired runtime removal or fallback behavior as current guidance.

Verification passed:

- `nix --extra-experimental-features 'nix-command flakes' develop /home/bayesartre/dev/devkit/overlays/dev-all#default --output-lock-file /dev/null --command make ci-cheap`
- `nix --extra-experimental-features 'nix-command flakes' develop /home/bayesartre/dev/devkit/overlays/dev-all#default --output-lock-file /dev/null --command make native-e2e-lifecycle`
- `nix --extra-experimental-features 'nix-command flakes' develop /home/bayesartre/dev/devkit/overlays/dev-all#default --output-lock-file /dev/null --command make native-overlay-e2e-matrix`
- `nix --extra-experimental-features 'nix-command flakes' develop /home/bayesartre/dev/devkit/overlays/dev-all#default --output-lock-file /dev/null --command make native-readiness-degraded-guard`
- `nix --extra-experimental-features 'nix-command flakes' develop /home/bayesartre/dev/devkit/overlays/dev-all#default --output-lock-file /dev/null --command make native-codex-home-preservation-guard`
- `kit/scripts/retired-runtime-guard`
- `kit/scripts/nix-overlay-runtime-guard`
- `find overlays -name flake.lock -print` returned no files.
- `git diff --check`
- `bash -n kit/scripts/retired-runtime-guard`

Remaining local edits in `cli/devctl/internal/seed/seed.go`,
`cli/devctl/internal/seed/seed_test.go`, `kit/dns/dnsmasq.conf`, and
`kit/proxy/allowlist.txt` are intentionally outside this scoped change and were
not staged for the retirement-hardening commit.

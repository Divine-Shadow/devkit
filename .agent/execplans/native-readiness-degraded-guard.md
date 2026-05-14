# Native Readiness Degraded Guard

This ExecPlan is a living document. It is maintained according to `.agent/PLANS.md`.

## Purpose

Native readiness already exposes structured operator status. This plan adds a
canonical guard that proves degraded and blocked states stay stable through the
supported `kit/scripts/devkit` entrypoint. The guard should exercise real CLI
output for worktree, broker, tooling, repo readiness, and partial-capacity
failure modes without reintroducing retired runtime paths.

## Progress

- [x] (2026-05-14T22:22:00Z) Audited current readiness/capacity/lifecycle
  tests, guard scripts, Make targets, and ExecPlans.
- [x] (2026-05-14T22:29:18Z) Added
  `kit/scripts/native-readiness-degraded-guard`.
- [x] (2026-05-14T22:29:18Z) Added
  `make native-readiness-degraded-guard`.
- [x] (2026-05-14T22:29:18Z) Ran focused Go tests and the new guard.
- [x] (2026-05-14T23:00:16Z) Ran the required native verification
  gates.
- [x] (2026-05-14T23:00:16Z) Confirmed no overlay-local lock files were
  generated.
- [x] (2026-05-14T23:00:16Z) Prepared the verified guard for a scoped
  commit.

## Surprises & Discoveries

- Existing unit tests cover the data model for missing worktree, missing tool,
  repo-check failure, partial capacity, and stale/stopped broker state, but no
  script proves those shapes through the canonical CLI.
- `status --ready` can prove partial capacity deterministically after preparing
  only agent 1: agent 1 contributes usable capacity while agent 2 remains
  blocked on missing worktree.
- `up --skip-ready` can emit Git worktree progress before its JSON payload.
  Guard JSON assertions now parse the JSON object out of captured command output
  instead of assuming the file starts at `{`.

## Decision Log

- Decision: Add the degraded readiness guard as a named Make target rather than
  wiring it into `ci-cheap`.
  Rationale: The guard starts a broker, creates temporary Git worktrees, and runs
  real sandbox readiness checks. That is operator-grade evidence, but not cheap
  static CI.
- Decision: Use temporary fixture overlays via `DEVKIT_OVERLAYS_DIR`.
  Rationale: The fixture overlays keep runtime checks deterministic while still
  using the real root `dev-all` flake and canonical `kit/scripts/devkit`
  entrypoint.

## Acceptance

Acceptance requires:

- The guard asserts JSON and text output for missing worktree, broker stopped,
  broker stale, missing runtime tool, repo-check failure, and partial capacity.
- The guard asserts `status`, `action`, `usable_capacity`, `blocked_agents`,
  `degraded_agents`, component states, `failed_checks`, and broker
  stale/stopped fields where those fields apply.
- Focused Go tests pass for readiness, capacity, and native lifecycle output.
- `make native-readiness-degraded-guard` passes.
- `make ci-cheap` passes.
- `make native-e2e-lifecycle` passes.
- `make native-overlay-e2e-matrix` passes.
- `kit/scripts/retired-runtime-guard` passes.
- `kit/scripts/nix-overlay-runtime-guard` passes.
- `find overlays -name flake.lock -print` returns no files.

## Outcomes & Retrospective

The guard is implemented as `kit/scripts/native-readiness-degraded-guard` and
is exposed by `make native-readiness-degraded-guard`. It uses temporary fixture
overlays through `DEVKIT_OVERLAYS_DIR`, but still runs the supported
`kit/scripts/devkit` entrypoint and the real `dev-all` flake. It covers missing
worktree, broker stopped, broker stale, partial capacity, missing runtime tool,
repo-check degraded readiness, and final broker shutdown.

Verification passed:

- `nix --extra-experimental-features 'nix-command flakes' develop overlays/dev-all#default --output-lock-file /dev/null --command make native-readiness-degraded-guard`
- `nix --extra-experimental-features 'nix-command flakes' develop overlays/dev-all#default --output-lock-file /dev/null --command go test -count=1 ./internal/runtime/readiness ./internal/runtime/capacity ./internal/commands/nativecmd`
- `nix --extra-experimental-features 'nix-command flakes' develop overlays/dev-all#default --output-lock-file /dev/null --command make ci-cheap`
- `nix --extra-experimental-features 'nix-command flakes' develop overlays/dev-all#default --output-lock-file /dev/null --command make native-e2e-lifecycle`
- `nix --extra-experimental-features 'nix-command flakes' develop overlays/dev-all#default --output-lock-file /dev/null --command make native-overlay-e2e-matrix`
- `kit/scripts/retired-runtime-guard`
- `kit/scripts/nix-overlay-runtime-guard`
- `find overlays -name flake.lock -print` returned no files.
- `git diff --check`
- `bash -n kit/scripts/native-readiness-degraded-guard`

The guard remains a named operator gate instead of part of `ci-cheap` because it
starts a broker, creates temporary worktrees, and runs real sandbox readiness
checks.

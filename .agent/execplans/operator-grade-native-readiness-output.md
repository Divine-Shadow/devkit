# Operator-Grade Native Readiness Output

This ExecPlan is a living document. It is maintained according to `.agent/PLANS.md`.

## Purpose

Native lifecycle commands should tell an operator whether the runtime is usable,
which agents contribute usable capacity, which agents are degraded or blocked,
and what action to take next. The supported path remains `kit/scripts/devkit`,
which execs `kit/bin/devctl`; this plan does not reintroduce Compose,
Docker Compose, `codexw`, or wrapper fallbacks.

The work touches:

- `cli/devctl/internal/runtime/readiness`
- `cli/devctl/internal/runtime/capacity`
- `cli/devctl/internal/commands/nativecmd`
- focused Go tests for readiness, capacity, broker status, and lifecycle output

## Progress

- [x] (2026-05-14T00:00:00Z) Read the current readiness, capacity, broker
  status, lifecycle output, and tests.
- [x] (2026-05-14T00:00:00Z) Added structured readiness check component/action
  metadata and derived failed-check helpers.
- [x] (2026-05-14T00:00:00Z) Added per-agent capacity status, usable capacity,
  blocked/degraded agent lists, and failed checks.
- [x] (2026-05-14T00:00:00Z) Added lifecycle-level status/action/failure fields
  and text output for capacity and broker state.
- [x] (2026-05-14T22:16:11Z) Ran focused Go tests for degraded-state and
  lifecycle output behavior.
- [x] (2026-05-14T22:16:11Z) Ran the required native verification gate and
  guard scripts.
- [x] (2026-05-14T22:16:11Z) Confirmed no overlay-local lock files are
  generated.
- [ ] Commit the verified implementation.

## Surprises & Discoveries

- `runtimebroker.Status` already carries useful operator fields: running state,
  socket existence, stale state, paths, and message. Lifecycle output only needed
  to surface those fields consistently in text and top-level JSON status.
- Missing worktree, missing tools, broker failures, sandbox command failures,
  and repo readiness failures can be represented deterministically without
  launching a full sandbox in unit tests.

## Decision Log

- Decision: Runtime checks now carry a stable `component` and failure `action`.
  Rationale: Operators need to distinguish worktree, broker, sandbox, tooling,
  and repo failures without parsing free-form check names.
- Decision: Capacity output includes `status`, `usable_capacity`,
  `blocked_agents`, `degraded_agents`, per-agent component states, and
  `failed_checks`.
  Rationale: Scaling output should answer whether capacity is usable before
  requiring inspection of every raw check.
- Decision: Lifecycle status attaches capacity summaries when readiness runs and
  attaches broker-only status for lightweight `status`, `down`, and `logs`.
  Rationale: `status --ready` should show full capacity, while lightweight
  commands should still expose broker state and clear next action.

## Acceptance

Acceptance requires:

- Focused Go tests pass for readiness metadata, capacity summaries, lifecycle
  JSON/text output, and broker stale/stopped state.
- `make ci-cheap` passes.
- `make native-e2e-lifecycle` passes.
- `make native-overlay-e2e-matrix` passes.
- `kit/scripts/retired-runtime-guard` passes.
- `kit/scripts/nix-overlay-runtime-guard` passes.
- Degraded states are exercised for missing worktree, stale/down broker, missing
  runtime tool, repo-check failure, and partial capacity.
- No overlay-local `flake.lock` files are generated.

## Outcomes & Retrospective

Verification completed on 2026-05-14:

- `nix --extra-experimental-features 'nix-command flakes' develop /home/bayesartre/dev/devkit/overlays/dev-all#default --output-lock-file /dev/null --command go test -count=1 ./internal/runtime/readiness ./internal/runtime/capacity ./internal/commands/nativecmd`
  passed.
- `nix --extra-experimental-features 'nix-command flakes' develop /home/bayesartre/dev/devkit/overlays/dev-all#default --output-lock-file /dev/null --command make ci-cheap`
  passed, including Go tests, `nix flake check`, runtime matrix dry-runs,
  retired runtime guard, and overlay Nix runtime guard.
- `nix --extra-experimental-features 'nix-command flakes' develop /home/bayesartre/dev/devkit/overlays/dev-all#default --output-lock-file /dev/null --command make native-e2e-lifecycle`
  passed for `dev-all` and `ouroboros-static-front-end`, including
  `up/status --ready/exec/attach/scale/down`.
- `nix --extra-experimental-features 'nix-command flakes' develop /home/bayesartre/dev/devkit/overlays/dev-all#default --output-lock-file /dev/null --command make native-overlay-e2e-matrix`
  passed. Full e2e rows passed for `dev-all`,
  `ouroboros-static-front-end`, `ouroboros-terraform`, and `pokeemerald`;
  classified runtime rows passed for `codex`, `dumb-onion-hax`, and
  `ouro-integration`.
- `kit/scripts/retired-runtime-guard` passed.
- `kit/scripts/nix-overlay-runtime-guard` passed.
- `find overlays -name flake.lock -print` returned no files.
- `git diff --check` passed.

The focused tests exercise missing worktree, stale/down broker, missing runtime
tool, repo-check failure, and partial capacity cases. The e2e commands showed
the new JSON fields in real lifecycle output: top-level `status`, broker state,
capacity `status`, `usable_capacity`, per-agent worktree/broker/sandbox/tooling
/repo states, and `down` reporting `status: stopped`.

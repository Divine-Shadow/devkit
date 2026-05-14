# Native End-to-End Lifecycle Evidence

This ExecPlan is a living document. It is maintained according to `.agent/PLANS.md`.

## Purpose / Big Picture

The native Nix runtime should be operator-proven, not just structurally present.
The user-visible result is that `kit/scripts/devkit` can build the CLI and run
real native lifecycle cycles without Compose: `dev-all` and one smaller
non-`dev-all` overlay can launch, report `status --ready`, execute a command,
open an attach shell, scale, and shut down through overlay-local flakes.

## Progress

- [x] (2026-05-14T00:00:00Z) Accepted the end-to-end lifecycle autonomy
  contract as the active goal.
- [x] (2026-05-14T00:05:00Z) Audited prior ExecPlans and found existing gates
  cover cheap CI, broker container smoke, native runtime smoke, readiness audit,
  and overlay `up/status/exec/down`, but not real attach or explicit
  `status --ready` cycles for a smaller overlay.
- [x] (2026-05-14T00:15:00Z) Added `kit/scripts/native-e2e-lifecycle` and
  `make native-e2e-lifecycle` for real `up/status --ready/exec/attach/scale/down`
  cycles on `dev-all` and `ouroboros-static-front-end`.
- [x] (2026-05-14T00:20:00Z) Ran `make native-e2e-lifecycle`; it passed for
  `dev-all` and `ouroboros-static-front-end`.
- [x] (2026-05-14T00:22:00Z) Fixed E2E cleanup to remove temporary worktrees
  before pruning and deleting branches, then reran the target successfully.
- [x] (2026-05-14T00:28:00Z) Ran the required verification gate and recorded
  observed evidence.
- [x] (2026-05-14T00:32:00Z) Committed the completed lifecycle evidence slice
  as `39fc207`.

## Surprises & Discoveries

- Existing `native-runtime-smoke` covered `dev-all` scale and attach dry-run, but
  attach was not executed as a real shell.
- Existing `native-overlay-matrix` covered every overlay with native
  `up/status/exec/down`, but it intentionally used `--skip-prepare` and did not
  run `status --ready`, attach, or scale.
- `documentation/tech_debt/layout-apply-resilience.md` still described the old
  container-oriented layout model. The document was updated to native agent
  language so supported docs no longer describe Compose-era operation as active.

## Decision Log

- Decision: Add a focused manual/local E2E target instead of widening cheap CI.
  Rationale: The E2E cycle requires sibling repo checkouts, Git worktrees,
  Nix shells, broker sockets, and real sandbox launches. It is the right
  operator gate but too stateful for the cheap CI path.
  Date/Author: 2026-05-14 / Codex

- Decision: Use `ouroboros-static-front-end` as the default smaller overlay.
  Rationale: It is a real non-`dev-all` overlay with an overlay-local flake,
  default repo metadata, and runtime-only readiness checks that are meaningful
  without running the full repository build.
  Date/Author: 2026-05-14 / Codex

## Validation and Acceptance

Acceptance requires:

- `make ci-cheap` passes.
- `make native-runtime-smoke` passes.
- `make native-readiness-audit` passes.
- `make postgres-broker-container-smoke` passes.
- `make native-e2e-lifecycle` passes and proves real `up`, `status --ready`,
  `exec`, stdin-driven `attach`, `scale`, and `down` for `dev-all` and
  `ouroboros-static-front-end`.
- Supported docs, scripts, and CLI help do not describe Docker Compose as an
  active runtime path. Retired namespace errors and archived migration notes may
  remain.
- `find overlays -maxdepth 2 -name flake.lock -print` produces no output.
- `git diff --check` passes.

## Idempotence and Recovery

The E2E script uses temporary broker sockets, state roots, worktree roots, and
agent homes under `/tmp`. It uses unique branch prefixes for native worktrees,
removes the temporary directories, prunes stale Git worktree metadata, and
deletes the unique test branches during cleanup.

## Artifacts and Notes

Verification evidence will be recorded here as commands are run.

Verification run on 2026-05-14:

```bash
nix --extra-experimental-features 'nix-command flakes' develop --command \
  make native-e2e-lifecycle
```

Result: passed. The script built `kit/bin/devctl`, then ran real native
`up`, `status --ready`, `exec`, stdin-driven `attach`, `scale 2`, and `down`
cycles for `dev-all` and `ouroboros-static-front-end`. Both overlays reported
`"runtime": "native"`, `"readiness_mode": "runtime-only"`,
`"capacity_available": 1` after `up` and `status --ready`, and
`"capacity_available": 2` after `scale`. Exec output included `codex-cli
0.130.0`, `exec-ok shell=dev-all`, and
`exec-ok shell=ouroboros-static-front-end`; attach output included
`attach-ok shell=dev-all` and `attach-ok shell=ouroboros-static-front-end`.
Both `down` calls reported `"running": false`.

Cleanup checks after the E2E run:

```bash
git -C ../ouroboros-ide branch --list 'devkit-e2e-*'
git -C ../ouroboros-static-front-end branch --list 'devkit-e2e-*'
find /tmp -maxdepth 1 -type d -name 'devkit-native-e2e.*' -print
find overlays -maxdepth 2 -name flake.lock -print
```

Result: all produced no output. No temporary branches, E2E temp dirs, or
overlay lock files remained.

```bash
nix --extra-experimental-features 'nix-command flakes' develop --command \
  make ci-cheap
```

Result: passed after the final wording cleanup. It rebuilt `kit/bin/devctl`,
ran `go test -count=1 ./...`, ran `nix flake check`, validated overlay runtime
metadata, enforced the no-overlay-lock policy, ran
`kit/scripts/devkit --dry-run -p dev-all image-matrix --all --check`, and ran
`kit/scripts/compose-retirement-guard`.

```bash
nix --extra-experimental-features 'nix-command flakes' develop --command \
  make postgres-broker-container-smoke
```

Result: passed. Output included `broker_ping=OK`, `redis_create_http=403`,
`postgres_pull_http=200`, `postgres_create_http=201`,
`postgres_start_http=204`, `postgres_running=true`,
`postgres_delete_http=204`, and `postgres-broker-container-smoke: ok`.

```bash
nix --extra-experimental-features 'nix-command flakes' develop --command \
  make native-runtime-smoke
```

Result: passed. Output included native plan/bind evidence, native
`up/status/logs/scale/down`, `_template` overlay-local native exec,
`codex-cli 0.130.0`, Spago `0.93.45`, Netlify `26.0.1`, Playwright
`1.58.2`, `native-exec-playwright-ok`, egress allow/block checks, broker
allow/deny checks, `frontend-playwright-ok`, and `native-runtime-smoke: ok`.

```bash
nix --extra-experimental-features 'nix-command flakes' develop --command \
  make native-readiness-audit
```

Result: passed. Output included two-agent native launch/status, egress policy,
governance warm on both agents, exact Codex `0.130.0` command on both agents,
backend jar assembly, backend jar launch, clean native down, and
`native-readiness-audit: ok`.

Supported-surface audit:

```bash
kit/scripts/devkit --help
rg -n 'docker[[:space:]]+compose|docker-compose|compose[.]override[.]yml|kit/compose[^[:space:]]*[.]yml|codexw|/usr/local/bin/codex|COMPOSE_PROJECT|ComposeProject|compose project|container ID|Container resolution|container seeding|duplicate window/container' . \
  --glob '!documentation/archive/**' \
  --glob '!kit/scripts/compose-retirement-guard' \
  --glob '!kit/bin/**' \
  --glob '!result/**' \
  --glob '!.agent/**'
kit/scripts/compose-retirement-guard
git diff --check
```

Result: wrapper help listed native lifecycle commands and no active Compose
command. The text audit produced no matches after stale test and tech-debt
wording was removed. The retirement guard reported
`compose-retirement-guard: ok`, and `git diff --check` passed.

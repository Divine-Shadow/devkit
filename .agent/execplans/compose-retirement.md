# Compose Runtime Retirement

This ExecPlan is a living document. It is maintained according to `.agent/PLANS.md`.

## Purpose / Big Picture

Devkit is moving from a mixed Docker Compose plus native runtime model to a single supported runtime model: Nix flakes plus the native devkit sandbox. After this change, operators use `kit/scripts/devkit` and the compiled `kit/bin/devctl` for launch, status, exec, readiness, and audit without any supported Compose files, Compose commands, Compose wrapper fallbacks, or Compose docs. Brokered Docker access may remain where the native runtime deliberately exposes it through policy; Compose orchestration does not remain.

## Progress

- [x] (2026-05-13T18:35:00Z) Accepted full Compose retirement as the active goal; legacy support may be removed because all agents have transitioned.
- [x] (2026-05-13T18:38:00Z) Read the local ExecPlan convention and started a full repository Compose surface inventory.
- [x] (2026-05-13T18:39:00Z) Started two read-only explorer agents for CLI/runtime and non-Go cleanup inventory.
- [x] (2026-05-13T22:02:00Z) Removed the registered Compose command package, stopped CLI dispatch from loading Compose files, made retired runner helpers fail loudly, and rewrote/deleted legacy tests.
- [x] (2026-05-13T22:08:00Z) Deleted root kit Compose YAML, overlay Compose overrides, old runtime fixture assets, Compose-only shell tests, and the legacy frontend Dockerfile wrapper surface.
- [x] (2026-05-13T22:14:00Z) Rewrote supported README/docs/examples for native-only operation and archived historical migration documents under `documentation/archive/compose-retirement/`.
- [x] (2026-05-13T22:17:00Z) Added `kit/scripts/compose-retirement-guard`, `make compose-retirement-guard`, and a `nix flake check` check for retired runtime strings.
- [x] (2026-05-13T22:29:00Z) Ran the required verification gate and manual native lifecycle check; evidence is recorded below.
- [x] (2026-05-14T17:09:57Z) Hardened the retirement guard after overlay-wide
  native E2E coverage: renamed the public target to `retired-runtime-guard`,
  added explicit path checks for retired runtime package/file names, removed
  the stale empty `composecmd` directory from the workspace, and expanded
  dry-run coverage so the retired namespace is rejected for both flake-backed
  and non-flake projects.
- [x] (2026-05-14T17:48:52Z) Re-ran the required retirement-hardening gates:
  cheap CI, native overlay matrix, focused native E2E lifecycle, broad overlay
  E2E classification, lockfile audit, retired runtime guard, and targeted
  string/path audits.

## Surprises & Discoveries

- Observation: Compose references are still spread across the top-level Makefile, README, overlay metadata, overlay Compose overrides, CLI main dispatch, Compose-specific command packages, tests, and historical docs.
  Evidence: `rg -n "compose|Compose|docker compose|compose\\.override|kit/compose|codexw|/usr/local/bin/codex"` returned references in all of those areas before retirement edits.

- Observation: Nix flake checks only see files staged into the Git-backed flake source.
  Evidence: The first `nix flake check` could not find the new `kit/scripts/compose-retirement-guard` until `git add -A` staged the new script.

- Observation: The static guard must run from the source root so archive exclusions match in both the live worktree and the Nix store source path.
  Evidence: The first Nix check found archived historical documents until the guard changed from `rg "$ROOT"` to `cd "$ROOT"; rg .`.

- Observation: The remaining supported references are intentional guardrails,
  not runtime instructions.
  Evidence: The post-hardening audit for `compose`, `docker compose`,
  `docker-compose`, and `COMPOSE` outside archives reports only the retired
  command/flag rejection paths, tests that assert those paths cannot launch,
  historical archive links, and the `retired-runtime-guard` pattern list.

## Decision Log

- Decision: Remove Compose as a supported command namespace instead of preserving a hidden compatibility path.
  Rationale: The user confirmed all agents have transitioned and explicitly authorized dropping legacy support.
  Date/Author: 2026-05-13 / Codex

- Decision: Keep native brokered Docker policy separate from Compose retirement.
  Rationale: Native runtime smoke and broker tests prove intentional Docker access through policy controls; the goal removes Compose orchestration, not Docker capability itself.
  Date/Author: 2026-05-13 / Codex

- Decision: Use `retired-runtime-guard` as the supported static gate name.
  Rationale: The guard still scans for forbidden Compose-era artifacts, but
  operator-facing Make and flake surfaces should describe the invariant rather
  than reintroducing the retired runtime as a named supported target.
  Date/Author: 2026-05-14 / Codex

## Outcomes & Retrospective

The implementation removes Compose as a supported runtime path. The registered Compose command package and Docker Compose runner execution path are gone from dispatch; lifecycle commands are native registry commands, non-flake runtime commands refuse with `runtime.flake` errors, and the explicit `compose` namespace fails with a retirement error. Supported docs and examples now describe native flakes, while historical migration material lives under `documentation/archive/compose-retirement/`.

Root kit Compose YAML files, overlay Compose override files, old runtime integration fixtures, Compose-only shell tests, and legacy Dockerfile Codex wrapper surfaces were deleted. `image-matrix` and overlay runtime validation now require `runtime.flake` and reject retired image metadata and overlay runtime files. The static guard is wired into both `make retired-runtime-guard` and `nix flake check`.

Residual risk: the string `compose` remains in the explicit retired namespace
and retired flag rejection paths, plus focused tests that prove those names do
not invoke any runtime. The executable Compose command package, Compose file
resolver, root Compose YAML files, and overlay Compose override files are absent
from tracked supported code.

## Context and Orientation

The repository root is `/home/bayesartre/dev/devkit`. The supported executable path is `kit/scripts/devkit`, which execs the compiled CLI at `kit/bin/devctl`. Native runtime code currently lives under `cli/devctl/internal/commands/nativecmd`, `cli/devctl/internal/runtime`, `cli/devctl/internal/sandbox`, and related packages. Overlay runtime metadata lives in `overlays/<overlay>/devkit.yaml`, and flake-backed overlays have `runtime.flake` plus overlay-local `flake.nix` files.

Before this plan, the CLI still contained Docker Compose support through `cli/devctl/main.go`, `internal/commands/composecmd`, `internal/compose`, `internal/runner` Compose helpers, and Compose branches in several command packages. Non-Go surfaces still included `kit/compose*.yml`, `overlays/*/compose.override.yml`, Compose smoke scripts, Compose docs, and Dockerfile-installed `/usr/local/bin/codex` wrappers.

## Plan of Work

First, remove Compose from supported CLI command dispatch. Top-level lifecycle commands should resolve to native/flakes only. The explicit `compose` namespace should be unknown or retired with a clear error, not registered as a command that can run Docker Compose. Struct fields and helper packages whose only purpose is Compose file resolution or Compose project naming should be deleted or renamed where native code only needs repository path information.

Second, simplify command packages that still carry Compose fallbacks. Native implementations for launch, status, exec, attach, readiness, broker, network checks, hooks, tmux, and image matrix should remain. Compose-only helpers, runners, tests, and command docs should be removed or rewritten around native behavior.

Third, remove supported non-Go Compose artifacts. Overlay `compose.override.yml` files, root `kit/compose*.yml`, Compose-only smoke tests, Make targets, and supported docs should be deleted or rewritten. Historical docs may be moved under an explicit archive only if they contain non-Compose runtime knowledge worth preserving.

Fourth, add a static retirement guard that fails on retired strings in supported surfaces: `docker compose`, `compose.override.yml`, `kit/compose*.yml`, `codexw`, and `/usr/local/bin/codex`. The guard may exclude explicit historical archive paths and its own pattern definitions.

Finally, run the verification gate: `go test -count=1 ./...` under `cli/devctl`, `make -C cli/devctl build`, `nix flake check`, `make native-runtime-smoke`, `make native-readiness-audit`, the static guard, and manual `kit/scripts/devkit -p dev-all up/status/exec/down` through native flakes only.

## Validation and Acceptance

Acceptance requires all supported runtime command paths to be native/flakes only, no supported docs or scripts to instruct Compose usage, no supported overlay runtime artifacts named `compose.override.yml` or root `kit/compose*.yml`, and passing verification commands with evidence recorded here.

The final completion audit must map every requirement in the user objective to concrete file state and command output. Passing tests alone is not sufficient unless the static guard and manual runtime checks cover the removal requirements.

## Completion Audit

- Requirement: `kit/scripts/devkit` remains the canonical entrypoint and has no
  fallback.
  Evidence: inspecting `kit/scripts/devkit` shows it resolves `kit/bin/devctl`,
  exits 127 with build instructions when the binary is missing, and then
  `exec`s only that binary.

- Requirement: no Compose lifecycle path or hidden fallback remains.
  Evidence: `cli/devctl/main.go` rejects the retired namespace before launch;
  `native_defaults_dryrun_test.go` covers flake-backed and non-flake retired
  namespace rejection; `kit/scripts/devkit --dry-run -p dev-all compose up`
  exits 2 with `retired runtime namespace`; `retired-runtime-guard` exits 0.

- Requirement: delete or quarantine remaining Compose command/runtime code.
  Evidence: `find cli/devctl/internal -maxdepth 3 \( -path '*/composecmd' -o
  -path '*/compose' \) -print` produced no output. `find kit -maxdepth 2
  -name 'compose*.yml' -print` and `find overlays -maxdepth 3 -name
  'compose.override.yml' -print` also produced no output.

- Requirement: supported docs, examples, and tests do not present Compose as a
  supported runtime.
  Evidence: supported README/check lists now use `make retired-runtime-guard`.
  The targeted `rg` audit only reports historical archive links, explicit
  rejection paths, negative tests/smokes, the forbidden-file validator, and the
  guard's own pattern list.

- Requirement: top-level `up/down/status/exec/attach/scale` dispatch is native
  lifecycle only.
  Evidence: integration tests cover flake-backed non-`dev-all` lifecycle and
  helper dispatch through native dry-runs; `kit/scripts/devkit --dry-run -p
  codex up --repo ouroboros-ide --count 1` reports `runtime: native`;
  `kit/scripts/devkit --dry-run -p codex exec 1 --repo ouroboros-ide -- echo
  ok` renders a native `bwrap`/`nix develop ./overlays/codex#default` command.

- Requirement: every supported overlay declares a flake/runtime path and the
  matrix remains operator proof.
  Evidence: `nix/validate-overlay-runtimes.py overlays` passed inside
  `make ci-cheap`; a direct overlay metadata audit shows `_template`, `codex`,
  `dev-all`, `dumb-onion-hax`, `ouro-integration`,
  `ouroboros-static-front-end`, `ouroboros-terraform`, and `pokeemerald` all
  declare overlay-local `runtime.flake` refs.

- Requirement: required verification commands pass.
  Evidence: the post-hardening verification evidence below records passing
  `make ci-cheap`, `make native-overlay-matrix`,
  `make native-overlay-e2e-matrix`, `make native-e2e-lifecycle`,
  `find overlays -maxdepth 2 -name flake.lock -print`, and targeted `rg` audits.

## Idempotence and Recovery

All edits should be source-controlled and recoverable through normal Git history. If historical Compose files are retained for context, they must live under an explicit archive path excluded from supported runtime discovery and documentation. Verification commands should not create overlay `flake.lock` files or mutate agent homes beyond normal native runtime state.

## Artifacts and Notes

Verification evidence:

    nix --extra-experimental-features 'nix-command flakes' develop --command bash -lc 'cd cli/devctl && go test -count=1 ./...'
    Result: exited 0; all Go packages passed.

    nix --extra-experimental-features 'nix-command flakes' develop --command make -C cli/devctl build
    Result: exited 0; `kit/bin/devctl` was rebuilt.

    nix --extra-experimental-features 'nix-command flakes' flake check
    Result: exited 0; checks included `retired-runtime-static` and `overlay-runtime-metadata`.

    nix --extra-experimental-features 'nix-command flakes' develop --command make retired-runtime-guard
    kit/scripts/retired-runtime-guard
    Result: both exited 0 with `retired-runtime-guard: ok`.

    find kit -maxdepth 2 -name 'compose*.yml' -print
    find overlays testing -name 'compose.override.yml' -print
    Result: both produced no output.

    rg -n 'internal/commands/composecmd|docker[[:space:]]+compose|docker-compose|compose\.override\.yml|kit/compose[^[:space:]]*\.yml|codexw|/usr/local/bin/codex' . --glob '!documentation/archive/**' --glob '!kit/scripts/compose-retirement-guard' --glob '!kit/bin/**' --glob '!result/**' --glob '!.agent/**'
    Result: exited 0 with no matches.

    nix --extra-experimental-features 'nix-command flakes' develop --command make native-runtime-smoke
    Result: exited 0 with `native-runtime-smoke: ok`; output proved retired namespace rejection, native up/status/logs/scale/down, `_template` overlay-local native exec, dev-all Spago/Netlify/Playwright, egress proxy, and broker policy.

    nix --extra-experimental-features 'nix-command flakes' develop --command make native-readiness-audit
    Result: exited 0 with `native-readiness-audit: ok`; output proved two-agent native launch/status, egress allowlist enforcement, governance warm, exact Codex command on both agents, backend jar assembly, backend launch, and native down.

    kit/scripts/devkit -p dev-all up/status/exec/down with isolated broker socket/state root
    Result: exited 0; `up` and `status` returned `"runtime": "native"`, `exec` printed `native-manual-ok`, and `down` stopped the isolated broker.

    git diff --check
    Result: exited 0.

Post-hardening verification evidence:

    nix --extra-experimental-features 'nix-command flakes' develop --command make ci-cheap
    Result: exited 0. This rebuilt `kit/bin/devctl`, ran Go tests, ran
    `nix flake check` including `retired-runtime-static`, validated overlay
    runtime metadata, checked overlay lock policy, ran the image matrix dry-run,
    and ran `retired-runtime-guard`.

    nix --extra-experimental-features 'nix-command flakes' develop --command make native-overlay-matrix
    Result: exited 0 with `native-overlay-matrix: ok` after native
    up/status/exec/down probes for `_template`, `codex`, `dev-all`,
    `dumb-onion-hax`, `ouro-integration`, `ouroboros-static-front-end`,
    `ouroboros-terraform`, and `pokeemerald`.

    nix --extra-experimental-features 'nix-command flakes' develop --command make native-e2e-lifecycle
    Result: exited 0 with `native-e2e-lifecycle: ok`.

    nix --extra-experimental-features 'nix-command flakes' develop --command make native-overlay-e2e-matrix
    Result: exited 0 with `native-overlay-e2e-matrix: ok`.
    Evidence report: `/tmp/devkit-native-overlay-e2e.4wpDuk/report.tsv`.

    Final overlay E2E classifications:
    - `e2e-pass`: `dev-all`, `ouroboros-static-front-end`,
      `ouroboros-terraform`, `pokeemerald`
    - `runtime-pass`: `codex`, `dumb-onion-hax`, `ouro-integration`
    - `not-locally-runnable`: none

    find overlays -maxdepth 2 -name flake.lock -print
    Result: exited 0 with no output.

    kit/scripts/retired-runtime-guard
    Result: exited 0 with `retired-runtime-guard: ok`.

    rg -n "docker[[:space:]]+compose|docker-compose|COMPOSE" cli kit nix README.md Makefile flake.nix overlays \
      -g '!kit/bin/**' -g '!kit/.devhome/**' -g '!kit/scripts/retired-runtime-guard'
    Result: exited 1 with no matches.

    find kit -maxdepth 2 -name 'compose*.yml' -print
    find overlays -maxdepth 3 -name 'compose.override.yml' -print
    find cli/devctl/internal -maxdepth 3 \( -path '*/composecmd' -o -path '*/compose' \) -print
    Result: all produced no output.

    rg -n -i "compose|docker compose|COMPOSE|docker-compose" cli kit nix README.md Makefile flake.nix overlays \
      -g '!kit/bin/**' -g '!kit/.devhome/**'
    Result: exited 0 only for historical archive links, explicit retired
    command/flag rejection, tests/smokes that assert rejection, the runtime
    validation check for forbidden overlay files, and the
    `retired-runtime-guard` pattern list.

    git diff --check
    bash -n kit/scripts/retired-runtime-guard kit/scripts/native-runtime-smoke kit/scripts/native-overlay-e2e-matrix kit/scripts/native-e2e-lifecycle kit/scripts/native-overlay-matrix
    Result: both exited 0.

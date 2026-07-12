# Native Runtime Readiness Hardening

This ExecPlan is a living document. It is maintained according to `.agent/PLANS.md`.

## Purpose / Big Picture

Devkit has retired Docker Compose as a supported runtime. This follow-up makes the post-retirement state structurally native-only and operator-ready: remaining dead legacy architecture is removed or renamed, each flake-backed overlay can be evaluated as its own native runtime unit, and the capabilities formerly hidden by Compose are proven through the canonical `kit/scripts/devkit` path.

## Progress

- [x] (2026-05-13T22:54:00Z) Accepted the native runtime hardening autonomy contract as the active goal.
- [x] (2026-05-13T22:56:00Z) Re-read existing ExecPlans for Compose retirement, overlay-local flakes, and native exec evidence.
- [x] (2026-05-13T22:57:00Z) Started read-only explorer agents for legacy architecture cleanup and overlay/runtime readiness coverage.
- [x] (2026-05-14T00:30:00Z) Inventoried remaining legacy-shaped code, metadata, docs, scripts, and test gaps.
- [x] (2026-05-14T00:43:00Z) Removed dead container-backed command builders, host/container mutation paths, retired runner helpers, stale command context plumbing, and legacy layout fields.
- [x] (2026-05-14T00:57:00Z) Added `kit/scripts/native-overlay-matrix` and `make native-overlay-matrix` to prove every overlay as an independent flake/runtime unit.
- [x] (2026-05-14T01:01:00Z) Hardened `kit/scripts/native-runtime-smoke` so direct overlay `nix develop` checks use `--output-lock-file /dev/null`.
- [x] (2026-05-14T01:05:00Z) Ran the required verification gate and recorded concrete evidence below.
- [ ] Commit the completed hardening work.

## Surprises & Discoveries

- `native-runtime-smoke` initially created `overlays/dev-all/flake.lock` and `overlays/ouroboros-static-front-end/flake.lock` during direct `nix develop` toolchain probes. The script now passes `--output-lock-file /dev/null` for those probes, the generated locks were removed, and a rerun left no overlay locks.
- `hosts` was still a container mutation command for non-native overlays. It is now native host-only, requires `runtime.flake`, and refuses agent/container targets instead of shelling out to container commands.
- The static retirement guard was too narrow for dead-code regressions. It now also catches structural symbols such as `ComposeProject`, `BuildCommand`, `ResolveContainerName`, `ExistingComposeNetwork`, Docker label probes, and generated ingress fragments.

## Decision Log

- Decision: Treat this as a structural and operator-readiness follow-up, not a second Compose retirement.
  Rationale: The previous commit removed supported Compose paths and artifacts. This plan should close remaining dead-code and coverage gaps while preserving the working native runtime.
  Date/Author: 2026-05-13 / Codex

- Decision: Keep explicit retired-namespace errors for `compose` and `--compose-project`, but remove executable fallback code behind them.
  Rationale: Operators get a clear refusal while the implementation no longer retains the old runtime architecture.
  Date/Author: 2026-05-14 / Codex

- Decision: Keep ingress Caddyfile generation knowledge but remove generated container orchestration fragments.
  Rationale: The route/cert rendering is non-Compose operational knowledge; the generated runtime artifact must be a native config file, not a retired runtime fragment.
  Date/Author: 2026-05-14 / Codex

## Outcomes & Retrospective

The post-Compose runtime is now structurally native-only in supported code paths. Legacy command builders and Docker/container lookup helpers were removed, `internal/compose` was renamed to `internal/devkitpaths`, layout schema fields that only described the old runtime were deleted, `hosts` became native host-only, and the static guard was expanded to catch dead architecture returning.

All eight overlays have overlay-local `runtime.flake` refs and passed the native lifecycle matrix through `kit/scripts/devkit`: `_template`, `codex`, `dev-all`, `dumb-onion-hax`, `ouro-integration`, `ouroboros-static-front-end`, `ouroboros-terraform`, and `pokeemerald`.

## Context and Orientation

The repository root is `/home/bayesartre/dev/devkit`. The canonical operator entrypoint is `kit/scripts/devkit`, which execs the compiled CLI at `kit/bin/devctl`. Native runtime implementation is centered around `cli/devctl/internal/commands/nativecmd`, native launch/sandbox code, overlay `devkit.yaml` metadata, root and overlay-local flakes, and the smoke/audit scripts under `kit/scripts`.

The previous retirement commit intentionally left a small amount of dead or legacy-shaped code, including older monolithic dispatch helpers in `cli/devctl/main.go` and a neutral path helper package still named `internal/compose`. This plan decides what can now be removed or renamed safely and adds evidence for operator-facing readiness.

## Plan of Work

First, inventory remaining legacy-shaped surfaces. Look for unreachable command branches, packages or symbols whose names still imply Compose as architecture, docs or scripts that still describe retired paths, and tests that only prove retired behavior indirectly.

Second, inspect overlay runtime structure. For each flake-backed overlay, record whether it has `flake.nix`, what `runtime.flake` points at, whether overlay-local Nix evaluation succeeds without creating locks, and whether `kit/scripts/devkit` can plan, launch, report status, exec a small command, and tear down where the overlay is expected to be runnable.

Third, implement narrowly scoped cleanup and readiness improvements. Remove dead dispatch branches if they are unreachable and covered by native registry paths. Rename neutral helpers only if the rename improves structure without causing churn. Extend smoke/audit scripts or add a new readiness matrix script if that gives operators concrete native evidence.

Fourth, run the verification gate. At minimum this includes `go test -count=1 ./...` under `cli/devctl`, `make -C cli/devctl build`, `nix flake check`, `make native-runtime-smoke`, `make native-readiness-audit`, `make compose-retirement-guard`, and the per-overlay readiness matrix evidence added by this plan.

## Validation and Acceptance

Acceptance requires:

- No supported command path restores Compose, Docker Compose files, `codexw`, or `/usr/local/bin/codex`.
- Remaining legacy-shaped code is either removed, renamed, or documented in this plan with a concrete reason it is still active.
- Every flake-backed overlay is represented in an operator-readable readiness matrix with flake metadata, native plan evidence, and native lifecycle evidence where applicable.
- Spago, Netlify, Playwright, brokered Docker, egress enforcement, Codex auth/state, multi-agent launch, and failure diagnostics are exercised through `kit/scripts/devkit` or a script that calls it.
- The required verification commands pass and their observed outputs are recorded here.
- The final completion audit maps each requirement in the active autonomy contract to real file state or command evidence.

## Idempotence and Recovery

All changes are source-controlled and recoverable through Git. Runtime smoke commands should use isolated broker sockets, state roots, and temporary homes where practical. Overlay-local Nix checks must use `--output-lock-file /dev/null` and must not leave `overlays/*/flake.lock` files behind unless the design explicitly changes.

## Artifacts and Notes

Verification run on 2026-05-14:

```bash
nix --extra-experimental-features 'nix-command flakes' develop --command \
  bash -lc 'cd cli/devctl && go test -count=1 ./...'
```

Result: passed.

```bash
nix --extra-experimental-features 'nix-command flakes' develop --command \
  make -C cli/devctl build
```

Result: passed; rebuilt `kit/bin/devctl`.

```bash
nix --extra-experimental-features 'nix-command flakes' flake check
```

Result: passed; evaluated all dev shells/packages and ran runtime shell inventory, overlay runtime metadata, and compose retirement static checks.

```bash
nix --extra-experimental-features 'nix-command flakes' develop --command \
  nix/validate-overlay-runtimes.py overlays
kit/scripts/devkit -p dev-all image-matrix --all --check
```

Result: passed; all eight overlays reported overlay-local flake refs, per-overlay `flake.nix` and `runtime.nix`, Codex `0.130.0`, and runtime checks.

```bash
nix --extra-experimental-features 'nix-command flakes' develop --command \
  make native-overlay-matrix
```

Result: passed; each overlay completed native `up`, `status`, `exec`, and `down`, verified `DEVKIT_NATIVE_AGENT=1`, expected `DEVKIT_NIX_SHELL`, broker `DOCKER_HOST`, no direct `/var/run/docker.sock`, and `codex-cli 0.130.0`.

```bash
nix --extra-experimental-features 'nix-command flakes' develop --command \
  make native-runtime-smoke
```

Result: passed; covered wrapper missing-binary behavior, retired namespace refusal, native plan/binds, native lifecycle, dry-run attach, overlay-local exec, Spago `0.93.45`, Netlify `26.0.1`, Playwright `1.58.2`, Deno, broker allow/deny policy, egress allow/block policy, runtime-only readiness, repo-failure capacity, frontend Playwright, static frontend toolchain, and clean `down`.

```bash
nix --extra-experimental-features 'nix-command flakes' develop --command \
  make native-readiness-audit
```

Result: passed; launched two native `dev-all` agents, verified egress enforcement, warmed governance on both agents, ran exact Codex `0.130.0` command on both agents, assembled the backend jar, launched it in the native sandbox, and shut down cleanly.

```bash
nix --extra-experimental-features 'nix-command flakes' develop --command \
  make overlay-runtime-smoke
nix --extra-experimental-features 'nix-command flakes' develop --command \
  make compose-retirement-guard
bash -n kit/scripts/native-overlay-matrix kit/scripts/native-runtime-smoke \
  kit/scripts/overlay-runtime-smoke kit/scripts/native-readiness-audit \
  kit/scripts/compose-retirement-guard
git diff --check
find overlays -maxdepth 2 -name flake.lock -print
```

Result: all passed; the final lockfile probe printed no paths.

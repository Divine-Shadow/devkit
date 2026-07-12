# Quiet Overlay Runtime Guard

This ExecPlan tracks the follow-up that turns quiet overlay-local Nix evaluation
from a convention into a maintained guard.

## Purpose

Supported runtime code, scripts, docs, and checks should reject regressions that
reintroduce noisy lock suppression, omit `--output-lock-file /dev/null` on
overlay-local `nix develop`, generate `overlays/*/flake.lock`, or restore retired
runtime paths.

## Progress

- [x] Inspected existing `retired-runtime-guard`, `ci-cheap`, and flake checks.
- [x] Added a dedicated `kit/scripts/nix-overlay-runtime-guard`.
- [x] Wired the guard into `make ci-cheap`, a standalone Make target, and
  `nix flake check`.
- [x] Run verification commands and stage the completed guard.

## Decision Log

- Keep retired runtime detection in `kit/scripts/retired-runtime-guard` and add
  a separate Nix overlay guard. `ci-cheap` and `nix flake check` run both, which
  keeps the concerns distinct while enforcing the combined invariant.
- The Nix guard scans supported surfaces only. Historical archives, agent homes,
  build outputs, and the guard script itself are excluded to avoid matching old
  evidence or local runtime state.

## Acceptance

- `nix --extra-experimental-features 'nix-command flakes' develop ./overlays/dev-all#default --output-lock-file /dev/null --command make ci-cheap`
  passed, including the new `overlay-nix-runtime-static` flake check and
  `== Overlay Nix runtime guard ==`.
- `kit/scripts/retired-runtime-guard` passed.
- `kit/scripts/nix-overlay-runtime-guard` passed.
- Negative guard probes prove the new guard fails on generated overlay locks,
  `--no-write-lock-file`, and overlay-local `nix develop` without
  `--output-lock-file /dev/null`; all three probes failed as expected.
- `find overlays -maxdepth 2 -name flake.lock -print` prints no paths.
- Representative `nix develop ./overlays/<overlay>#default --output-lock-file
  /dev/null --command true` for `./overlays/codex#default` emitted no
  lock-update warning.

## Outcomes & Retrospective

Quiet overlay-local evaluation is now enforced by a dedicated static guard in
the same cheap verification path as retired runtime drift. The guard is also
available as `make nix-overlay-runtime-guard` for focused local checks.

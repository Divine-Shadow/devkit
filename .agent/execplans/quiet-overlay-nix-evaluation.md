# Quiet Overlay Nix Evaluation

This ExecPlan tracks the cleanup that keeps overlay-local flakes lockless while
removing lock-update warning noise from native runtime commands.

## Purpose

Operators should be able to run native `up`, `status`, `exec`, readiness, and
overlay smoke/matrix flows against `./overlays/<name>#default` without seeing
lock-update warnings or producing `overlays/*/flake.lock`. The root
`flake.lock` remains the single committed pin source.

## Progress

- [x] Reproduced the warning with the old suppress-write lock flag on
  `./overlays/codex#default`.
- [x] Verified `--output-lock-file /dev/null` evaluates
  `./overlays/codex#default` and `./overlays/_template#default` without the
  lock-update warning and without creating overlay locks.
- [x] Patch runtime launch, dry-run rendering, runtime matrix, smoke scripts,
  docs, and tests.
- [x] Run required verification commands and lock-file audits.
- [x] Stage the completed cleanup for commit.

## Surprises & Discoveries

- `--no-update-lock-file` is not viable for lockless overlay flakes because Nix
  treats the missing top-level overlay lock graph as a forbidden update.
- `--reference-lock-file ./flake.lock` does not cleanly solve this because the
  overlay top-level lock graph differs from the root lock graph.
- An index-visible empty `overlays/dev-all/flake.lock` was present during the
  audit; removing it from the staged/worktree state is part of enforcing the no
  overlay-local locks policy.

## Decision Log

- Use `--output-lock-file /dev/null` for automation that evaluates
  overlay-local flakes. This lets Nix compute the overlay-local lock graph
  without persisting it or warning that persistence was suppressed.
- Keep overlay flakes in the delegated `inputs.devkit.url = "path:../.."`
  shape so every overlay remains independently flake-backed while still using
  the root flake as the source of shells and pinned inputs.

## Acceptance

- `nix --extra-experimental-features 'nix-command flakes' develop ./overlays/dev-all#default --output-lock-file /dev/null --command make ci-cheap`
  passed.
- `nix --extra-experimental-features 'nix-command flakes' develop ./overlays/dev-all#default --output-lock-file /dev/null --command make native-overlay-matrix`
  passed.
- `nix --extra-experimental-features 'nix-command flakes' develop ./overlays/dev-all#default --output-lock-file /dev/null --command make native-e2e-lifecycle`
  passed.
- `nix --extra-experimental-features 'nix-command flakes' develop ./overlays/dev-all#default --output-lock-file /dev/null --command make native-overlay-e2e-matrix`
  passed with report `/tmp/devkit-native-overlay-e2e.845Pql/report.tsv`.
- `kit/scripts/retired-runtime-guard` passed.
- `find overlays -maxdepth 2 -name flake.lock -print` printed no paths.
- Focused `dev-all up --runtime-only` audit captured
  `/tmp/devkit-lock-warning-audit.IOFQKn/dev-all-up.out`; it included
  `purescript-spago-netlify` and `playwright-browser`, and `rg` found no
  lock-update warning or suppress-write lock flag in the output.
- `rg` found no active suppress-write lock workaround in runtime code, scripts,
  operator docs, or ExecPlans.

## Outcomes & Retrospective

The runtime now lets Nix compute an overlay-local lock graph without writing one
into `overlays/` and without emitting the previous warning. The repository state
contains no overlay-local lock files, matching the lockless overlay policy.
No remaining implementation risks are known.

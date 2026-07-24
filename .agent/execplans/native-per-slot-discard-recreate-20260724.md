# Per-slot native discard and recreate

This ExecPlan is a living document. Keep `Progress`, `Surprises & Discoveries`,
`Decision Log`, and `Outcomes & Retrospective` current while implementing it.

## Purpose

An unhealthy native `dev-all` Product slot must be replaceable without wiping
or modifying healthy sibling slots. The package-owned CLI currently has an
unregistered broad reset implementation that disposes every slot and their
shared Git repository. Add one generic, fail-closed command:

    devkit -p dev-all native reset --repo ouroboros-ide --index N

The command derives all filesystem and runtime geometry from the source-owned
`dev-all` overlay. The caller may select only the declared repository and one
declared slot index; it cannot supply roots, transports, branches, runtime
composition, or deletion paths.

## Progress

- [x] (2026-07-24) Created a clean worktree at exact Devkit
  `4fc9b08c2b17010d73a188a015bdf62e644af180`; read `AGENTS.md` and
  `.agent/PLANS.md`.
- [x] (2026-07-24) Inspected the current native lifecycle, worktree geometry,
  source-declared SSH bootstrap, preparation, readiness, and broad reset.
- [x] (2026-07-24) Implemented exact per-slot disposal, selected-slot process shutdown, and
  selected-slot reconstruction.
- [x] (2026-07-24) Added command and production-path integration coverage, including sibling
  preservation and failed-reconstruction absence.
- [x] (2026-07-24) Passed focused and full Go tests, Go vet, and the staged-source Nix flake check.
- [x] (2026-07-24) Closed this implementation plan for publication.

## Context and orientation

`cli/devctl/internal/commands/nativecmd/native.go` registers and implements the
native CLI. `lifecyclePlanOptions` converts the source overlay into immutable
runtime geometry. `prepareNativeGitBootstrapAndWorktrees` is the ordinary
Git/SSH/proxy bootstrap path, while `launch.Prepare` creates the real home and
runtime prerequisites. `cli/devctl/internal/worktrees/worktrees.go` owns the
package common Git repository and linked-worktree lifecycle.

`ResetOwnedPrefix` and `PlanNativeReset` currently target every slot, the shared
common repository, and the shared manifest. That contract is too broad for
replacing one failed consumer and is not registered as `native reset`.

## Invariants

- Only `-p dev-all`, its source-declared default repository, and an index within
  the source-declared agent count are accepted.
- Worktree, home, state, common Git repository, origin, branch, transport,
  proxy, and runtime paths come only from the active package overlay and plan.
- Every deletion target passes absolute-path, containment, symlink/junction,
  protected-root, and mount-point validation before any process or filesystem
  effect.
- Only processes carrying the selected slot's exact runtime identity may be
  terminated. A process touching selected paths without that identity blocks
  disposal as an unowned active effect. Sibling processes are never signaled.
- The selected linked worktree, selected home, selected state, and selected
  bootstrap residue are disposable. The shared common Git repository,
  manifest geometry, and every sibling are preserved.
- Reconstruction uses the ordinary package-owned Git/OpenSSH/proxy fetch,
  linked-worktree setup, home/runtime preparation, manifest write, and source
  readiness checks. It creates no Product source or runtime authority.
- Any reconstruction failure invokes the same selected-slot disposal and
  leaves that slot absent. There is no salvage mode.

## Implementation plan

1. Replace the broad worktree reset plan with an index-aware selected-slot
   plan, retaining its full preflight and apply-time revalidation. Add a
   selected-slot setup primitive that fetches the declared origin through the
   ordinary package SSH path and creates only one linked worktree.
2. Register `native reset`. Give it a narrow parser accepting only `--repo`,
   `--index`, and output format. Derive all other values from the `dev-all`
   overlay, validate the shared common repository/manifest geometry, preflight
   the disposal boundary, stop only selected owned processes, apply disposal,
   reconstruct and prepare one plan, update the derived manifest, and run
   readiness for that one index.
3. Add direct safety tests for index isolation, protected/symlink/mount
   rejection, and apply-time revalidation. Extend the real CLI integration
   fixture so a dirty/stale selected slot becomes clean/current/ready while
   sibling worktree, home, and process markers remain unchanged; make fetch or
   readiness fail and prove the selected slot is absent while the sibling is
   intact.
4. Run formatting, focused Go tests, the full Devctl Go suite exposed by the
   repository, and `nix flake check --show-trace`. Record exact outcomes below.

## Decision log

- 2026-07-24: Keep the package-owned common Git repository because it is shared
  source-acquisition infrastructure, not selected-slot state. Reset removes
  only the selected worktree and its exact metadata reference; reconstruction
  refreshes the common remote through the existing package SSH path.
- 2026-07-24: Do not reuse `lifecycleUp`, because it loops over every configured
  slot and would prepare or readiness-check healthy siblings. The new handler
  composes the same lower-level production functions for exactly one index.

## Verification

Success requires all of the following from source-level executable tests:

- the registered CLI rejects root/path/branch/transport overrides before any
  deletion;
- a dirty selected checkout is replaced at exact `origin/main`, its home and
  state are recreated, and selected readiness is green;
- sibling checkout HEAD/status/metadata, home sentinel, state sentinel, and a
  sibling runtime process remain byte-identical/alive;
- a selected runtime process is stopped, while any unowned process touching
  the selected path prevents deletion;
- failed fetch, preparation, or readiness leaves the selected worktree, home,
  and state absent, with the sibling intact;
- protected-root, symlink/junction, mount, traversal, and out-of-range inputs
  fail before disposal; and
- focused Go tests and the Devkit Nix flake checks pass.

## Outcomes & retrospective

`native reset` now replaces exactly one declared `dev-all` slot through the
ordinary package Git/SSH, linked-worktree, runtime preparation, and readiness
path. It preserves the common repository and sibling slots, rejects caller
geometry and unowned processes, and removes the selected slot again after any
failed reconstruction. The overlay's existing `runtime-only` default is used
for reconstruction readiness so a Product source defect does not circularly
prevent creation of the governed Product agent that must repair it. Selected
linked-worktree admin/ref state is discarded with the slot; the shared
manifest is verified and preserved byte-for-byte; and a broker created by a
failed reconstruction is stopped. Process signaling is guarded by both slot
identity and Linux process start time.

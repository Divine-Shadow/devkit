# GUI Target Configuration Projection

This ExecPlan is a living implementation record for the target-bound Codex
configuration and stale native-manifest transition used by the Linux hub.

## Objective

Make Devkit the sole writer of a GUI member's `CODEX_HOME/config.toml` while
keeping profile selection in the immutable WSL-Nix inventory projection. An
exact `--gui-target-id` must select one manifest record, validate its lane
geometry and immutable source, and atomically install the regular owner-matched
`0600` file before the target command starts. Permit only a proven suffix-only
shrink of a stale native slot manifest so a source-declared capacity reduction
can reconstruct the selected slot without touching retained or active lanes.

## Constraints

- The fixed authority is
  `/etc/fleet/source/codex-config-projections.json`, schema
  `devkit/gui-codex-config-projections/v1`. Caller environment variables and
  mutable fallback files are not authority.
- Ordinary non-GUI Devkit exec plans do not project a config.
- Immutable source reads and mutable destination writes must be no-follow,
  owner-checked, same-handle/same-inode operations. A symlinked parent,
  noncanonical path, foreign owner, wrong hash, wrong geometry, or pathname
  replacement fails before other target-home mutation.
- A stale native manifest may transition only from canonical `1..M` to
  canonical `1..N`, `N < M`, with the retained prefix exactly equal and every
  surplus lane proven twice to have no processes, sockets, worktree, home,
  state, locks, Git metadata, branch custody, or unpublished commits.
- Unknown JSON fields, trailing JSON, symlinked/nonregular manifests, growth,
  non-suffix changes, and concurrent residue fail closed.
- No active business lane is stopped or rewritten by this source writer.

## Progress

- [x] Propagate required `--gui-target-id` through native plan/render/exec.
- [x] Implement strict manifest selection and exact geometry/profile/hash
  validation.
- [x] Install through directory handles with `O_NOFOLLOW`, atomic rename,
  fsync, post-rename inode/bytes/hash/mode/UID verification.
- [x] Remove the global `DEVKIT_CODEX_CONFIG_SOURCE` overlay authority.
- [x] Implement locked suffix-only stale-manifest shrink with double absence
  proof and structured diagnostics.
- [x] Add adversarial symlink, foreign-owner, pathname-race, unknown-field,
  trailing-JSON, growth, residue, and retained-lane tests.
- [x] Rebase the complete committed candidate onto `origin/master` at
  `7fa979e9828a5286fd68e16a052143fcae35ab62`, retaining its Product-lane
  runtime-support, Git-metadata, isolated-root, and Codex-home fixes.
- [x] Run the complete post-rebase `go test` suite, focused `go vet`, formatting,
  and `git diff --check` at bounded scheduler priority.
- [ ] Pin and deploy the published Devkit source through WSL-Nix, then prove a
  real target's effective config origin and isolation.

## Implementation

The projection contract lives in
`cli/devctl/internal/runtime/plan/gui_config_projection.go`. Native commands
carry the canonical ID through `plan.go`, `render.go`, and `native.go`.
`launch.Prepare` reselects the exact record and installs the config before any
other `HostHome` mutation. The destination implementation in `launch.go`
traverses the home and `.codex` directories through no-follow directory file
descriptors, validates ownership, writes a private temporary file, fsyncs,
renames through the directory descriptor, and reopens the final file to prove
identity and content.

`native_slot_reset.go` owns the stale-manifest transition under the existing
reset lock. It strictly decodes the current manifest from a regular no-follow
handle, proves the only difference is a surplus suffix, runs the full absence
proof twice, and atomically installs the expected manifest before continuing
the selected-slot reset.

## Verification

Run from `cli/devctl` with bounded resources:

    TMPDIR=/tmp GOMAXPROCS=2 nice -n 19 ionice -c 3 go test -p 2 ./...
    nice -n 19 ionice -c 3 go vet ./internal/runtime/plan ./internal/runtime/launch ./internal/commands/nativecmd

Then run `gofmt -d` over every changed Go file, `git diff --check`, and verify
the worktree is committed and clean. Deployment acceptance is not source-test
success: a real GUI target must start with the declared target ID, exact
profile bytes/hash/owner/mode, and no visibility or mutation of another lane.

## Surprises & Discoveries

- The original projection implementation checked only the final immutable
  source component and followed parent symlinks; destination parents were also
  followed and ownership was not proven. Both were publication blockers.
- The existing native manifest reader used permissive `json.Unmarshal` and
  followed a symlink/nonregular file. A shrink could therefore erase a future
  field or foreign authority even when lane absence checks were correct.
- The shrink algorithm's lane, Git, and process proofs were otherwise strong;
  the repair preserves that logic and hardens only its input authority.
- Four upstream commits added Product-lane isolation in the same planner.
  The rebase had one mechanical `Plan` struct conflict; the resolved type keeps
  both upstream `HostRuntimeSupportRoot` and the new `GUITargetConfig`, while
  the generalized upstream isolated-workspace validator remains authoritative.
- The first compare-and-swap correctly rejected after `origin/master` advanced
  from `8fdb3ce54143ed0b291ba212b12acefa10be3fe8` to
  `7fa979e9828a5286fd68e16a052143fcae35ab62`. The candidate then rebased cleanly
  over the added isolated Product Codex-home projection and reran every gate.

## Decision Log

- 2026-08-29: Pass only a canonical target identity from Fleet. Devkit
  reselects the immutable record locally; callers never pass a config path.
- 2026-08-29: Keep config projection launch-bound rather than activation-bound,
  so a stopped/absent lane is not silently mutated and the writer is exactly
  the process preparing that lane.
- 2026-08-29: Admit one suffix-only manifest shrink as a source transition,
  not a general reconciliation algorithm. All other drift remains a typed
  failure.

## Outcomes & Retrospective

The rebased implementation commit is
`e3d4e0d354394941cdf089898e31841345e4194e`. It provides a single target-bound
writer and a bounded, monotone stale-manifest transition while preserving the
upstream Product-lane projections. Post-rebase verification passed:

    TMPDIR=/tmp GOMAXPROCS=2 nice -n 19 ionice -c3 go test -p 2 ./...
    TMPDIR=/tmp GOMAXPROCS=2 nice -n 19 ionice -c3 go vet ./internal/runtime/plan ./internal/runtime/launch ./internal/commands/nativecmd
    gofmt -l <all changed Go files>
    git diff --check origin/master..HEAD

The final source publication boundary is a fetch plus compare-and-swap update
of `origin/master` containing this outcome record and the implementation
commit. The source work still receives no Linux-hub delivery credit until the
published package is deployed and a real member passes task
create/list/read/restart and effective-configuration verification.

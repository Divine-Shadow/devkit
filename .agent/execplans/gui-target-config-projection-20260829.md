# GUI Target Configuration Projection

This ExecPlan is a living implementation record for the target-bound Codex
configuration and stale native-manifest transition used by the Linux hub.

## Objective

Make Devkit the sole writer of a GUI member's `CODEX_HOME/config.toml` while
keeping profile selection in the immutable WSL-Nix inventory projection. An
exact `--gui-target-id` must select one manifest record, validate its lane
geometry and immutable source, and atomically install the regular owner-matched
`0600` file before the target command starts. A selected-slot reset whose
controller route carries no target ID must select exactly one immutable record
by the same full source-derived geometry and materialize it before readiness.
Permit only a proven suffix-only
shrink of a stale native slot manifest so a source-declared capacity reduction
can reconstruct the selected slot without touching retained or active lanes.

## Constraints

- The fixed authority is
  `/etc/fleet/source/codex-config-projections.json`, schema
  `devkit/gui-codex-config-projections/v1`. Caller environment variables and
  mutable fallback files are not authority.
- Ordinary non-GUI Devkit exec plans do not project a config.
- Every destructive `dev-all` reconstruction plan projects one config before
  readiness: exact-ID selection when supplied, otherwise unique full-geometry
  selection. Missing or duplicate geometry fails before deletion; ambient
  config remains non-authoritative.
- Immutable source reads and mutable destination writes must be no-follow,
  owner-checked, same-handle/same-inode operations. A symlinked parent,
  noncanonical path, foreign owner, wrong hash, wrong geometry, or pathname
  replacement fails before other target-home mutation.
- A stale native manifest may transition only from canonical `1..M` to
  canonical `1..N`, `N < M`, with the retained prefix exactly equal. Every
  present surplus lane must be idle, clean, on its exact source-derived branch,
  registered only in the package-owned common Git repository, and have no
  commits outside `origin/<base>`. Its cold GUI history is captured before any
  custody path is moved.
- Surplus retirement is a journal-before-effects transaction. The exact
  source-to-quarantine mapping is durably recorded before the first rename;
  pre-CAS failure rolls every lane back, post-CAS failure retains typed cleanup
  state, and retry may resume only mappings re-derived from the current opaque
  reset plan. Journal JSON never grants new filesystem authority.
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
- [x] Publish the implementation and validation record by exact
  compare-and-swap from
  `7fa979e9828a5286fd68e16a052143fcae35ab62` to
  `beca5b996a62b34b610de792ac204b4eabc963ca`, then fetch and verify the remote
  ref equals the candidate.
- [x] Deploy the descendant workspace-root profile contract at published
  Devkit `14e83243e145d69e01269589cce5011f909b6ae4`; typed reconstruction then
  exposed one obsolete, clean slot suffix that the original absence-only
  transition could classify but not retire.
- [x] Implement custody-aware surplus retirement by reusing the exact
  selected-slot reset planner, preserving cold history outside the retired
  lane, and making manifest replacement a strict locked compare-and-swap.
- [x] Reject the first green implementation after independent review found
  renames preceding the durable journal and an over-broad recovery-source
  allowance. The corrected transaction prepares and persists exact mappings
  before effects and revalidates every recovery source, target, mount, and Git
  metadata identity before mutation.
- [x] Pass focused crash/adversarial tests, full `go test ./... -count=1`,
  `nix flake check --no-build`, the packaged `devctl-go-tests` check, and the
  runtime bundle/tools/shell checks with at most two Nix jobs and two cores.
- [x] Publish the custody-aware implementation by exact compare-and-swap from
  `14e83243e145d69e01269589cce5011f909b6ae4` to
  `8103bd8bb6bc4b60fb97e254997d7b06129471a0`, then fetch and prove the remote
  `master` ref equals the candidate.
- [x] Diagnose controller-local Product reconstruction failure
  `op-603b0b1f0ccfdd0cf7d07e680cacd428`: its selected reset omitted
  `--gui-target-id`, built an ordinary plan, skipped config projection, and
  failed `codex-provider-config` before readiness.
- [x] Require targetless selected-slot reset to use the existing unique
  full-geometry immutable projection while preserving exact-ID selection.
- [x] Pass focused reset/geometry/materialization tests, full
  `go test -p 2 ./... -count=1`, focused `go vet`, formatting/diff checks,
  `nix flake check --no-build`, and the packaged Devctl/runtime Nix checks.
- [ ] Publish by exact compare-and-swap, pin/deploy through WSL-Nix, and prove
  a fresh local3 reconstruction reaches exact config/auth/native-governance
  readiness.

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
handle and proves the only geometry difference is a canonical surplus suffix.
For a present surplus lane it proves idle process/socket state, exact Git
registration and branch, a clean index/worktree, and zero commits outside the
source-derived remote base. It captures cold history into global state-root
custody, prepares exact reset mappings through `worktrees.NativeResetPlan`,
persists the typed shrink journal, stages all lanes, compare-and-swaps the
manifest, and only then commits quarantine cleanup. The manifest package uses
one no-follow flock for every writer and fsyncs the parent after installation.

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
- A real Drtalos reconstruction demonstrated that an absence-only shrink is
  not a capacity-decrease operation: slot 3 was idle, clean, fully contained in
  the remote base, and still correctly rejected because its worktree, home,
  state, and linked-worktree metadata remained present.
- Normal-path tests initially passed a two-phase retirement that was not
  reboot-safe. A crash after `Stage` but before journal installation could
  strand random quarantines with the old manifest, and recovery accepted any
  missing source under one validated boundary. Independent review caught both
  before publication; crash-before-first-effect, crash-after-first-rename,
  arbitrary-source, and cross-boundary recovery tests now cover them.
- The controller-local and remote Fleet Product reconstruction routes had
  different reset arguments: remote supplied the canonical GUI target ID,
  while local supplied only exact lane geometry. Selected reset treated the
  latter as an ordinary plan even though the destructive command exclusively
  reconstructs a GUI Product lane. The immutable full-geometry selector
  already used by whole-prefix reset was present; its selected-reset
  composition flag was missing.

## Decision Log

- 2026-08-29: Pass only a canonical target identity from Fleet. Devkit
  reselects the immutable record locally; callers never pass a config path.
- 2026-08-29: Keep config projection launch-bound rather than activation-bound,
  so a stopped/absent lane is not silently mutated and the writer is exactly
  the process preparing that lane.
- 2026-08-29: Admit one suffix-only manifest shrink as a source transition,
  not a general reconciliation algorithm. All other drift remains a typed
  failure.
- 2026-08-29: Treat a clean, idle, base-contained surplus suffix as disposable
  execution custody only after cold history capture. Preserve branch refs and
  refuse dirty, ahead, detached, foreign-metadata, locked, active, or opaque
  lanes.
- 2026-08-29: Journal exact plan-derived mappings before the first destructive
  rename. A prepared transaction rolls back while the prior manifest remains;
  a committed/replacement-manifest transaction can only finish cleanup.
- 2026-09-01: Preserve exact target-ID selection when present. When selected
  reset has no ID, require exactly one projection by the same source-derived
  project/repo/index/workspace/worktree/home geometry before any destructive
  effect. Do not add an ambient path, config copy, or readiness bypass.

## Outcomes & Retrospective

The rebased implementation commit is
`e3d4e0d354394941cdf089898e31841345e4194e`. It provides a single target-bound
writer and a bounded, monotone stale-manifest transition while preserving the
upstream Product-lane projections. Post-rebase verification passed:

    TMPDIR=/tmp GOMAXPROCS=2 nice -n 19 ionice -c3 go test -p 2 ./...
    TMPDIR=/tmp GOMAXPROCS=2 nice -n 19 ionice -c3 go vet ./internal/runtime/plan ./internal/runtime/launch ./internal/commands/nativecmd
    gofmt -l <all changed Go files>
    git diff --check origin/master..HEAD

The 2026-09-01 selected-reset repair passed the focused causal chain and full
repository/package gates from the dedicated clean source writer:

    go test ./internal/commands/nativecmd -run 'Test(ParseNativeSlotResetArgsRequiresExactTypedLaneInterface|NativeSlotResetLifecycleAlwaysSelectsImmutableGUIConfig)$' -count=1
    go test ./internal/runtime/plan -run TestBuildRequiresUniqueGUITargetConfigProjectionByFullGeometry -count=1
    go test ./internal/runtime/launch -run 'TestSeedCodexConfig(DataAtomicallyMaterializesAndRefreshes0600Target|IgnoresHostileLegacyAmbientSourceForOrdinaryPlan)$' -count=1
    go test -p 2 ./... -count=1
    go vet ./internal/runtime/plan ./internal/runtime/launch ./internal/commands/nativecmd
    nix --option max-jobs 2 --option cores 2 flake check --no-build
    nix --option max-jobs 2 --option cores 2 build --no-link .#checks.x86_64-linux.devctl-go-tests .#checks.x86_64-linux.dev-all-runtime-bundle .#checks.x86_64-linux.dev-all-runtime-tools .#checks.x86_64-linux.dev-all-runtime-shell

The original source publication boundary completed with expected remote ref
`7fa979e9828a5286fd68e16a052143fcae35ab62`, candidate
`beca5b996a62b34b610de792ac204b4eabc963ca`, and an exact post-push remote
readback match. The custody-aware retirement implementation is published at
`8103bd8bb6bc4b60fb97e254997d7b06129471a0`; its publication boundary used
expected remote ref `14e83243e145d69e01269589cce5011f909b6ae4` and exact post-push readback.
Neither source generation receives Linux-hub delivery credit until the package
is deployed and a real member passes task create/list/read/restart and
effective-configuration verification.

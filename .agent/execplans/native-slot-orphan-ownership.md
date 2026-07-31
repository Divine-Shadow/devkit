# Native Slot Orphan Ownership

This ExecPlan is a living document. Keep `Progress`, `Surprises & Discoveries`,
`Decision Log`, and `Outcomes & Retrospective` current as the work advances.

## Purpose

Make `devctl -p dev-all native reset --repo ouroboros-ide --index N`
reconstruct a Product slot after a governed SBT supervisor loses its native
launcher ancestor, without authorizing arbitrary processes that happen to
touch the slot. The reset must remain package-owned, source-declared,
identity-bound, and fail closed.

This repairs the canonical reconstruction owner used by the disposable
Product-agent environment. It does not change Product SBT shutdown authority,
choose Product artifacts, or add a caller-supplied force/PID option.

## Progress

- [x] (2026-07-31) Reproduced reset refusal on Product slots 1, 2, and 3 and
  captured exact read-only process identities.
- [x] (2026-07-31) Located the refusal in
  `cli/devctl/internal/commands/nativecmd/native_slot_reset.go` at Devkit
  revision `e4795d1572ab2502f0343d62e65e6da3087fb2c0`.
- [x] (2026-07-31) Confirmed all three supervisors are parentless exact-slot
  SBT launchers with stable boot/start identity, matching UID/GID and HOME,
  exact slot CWD, and slot-scoped file descriptors; none retains
  `DEVKIT_NATIVE_AGENT`.
- [x] (2026-07-31) Added a source-declared exact orphan-launcher rule to the `dev-all`
  overlay and parse it through the typed config.
- [x] (2026-07-31) Extended reset planning so only a parentless process satisfying the whole
  rule is selected; descendants inherit only that selected root's custody.
- [x] (2026-07-31) Added stable-boot and semantic full-set revalidation before
  effects, Linux pidfd signaling, topology-safe leaf-first ordering, and
  per-PID revalidation before TERM and escalation.
- [x] (2026-07-31) Added positive, hostile, descendant, symlink, topology,
  boot-drift, partial-effect, and pidfd tests. Focused unit, vet, and race
  checks pass with `GOMAXPROCS=2`.
- [x] (2026-07-31) Completed the second independent fail-closed review with no
  actionable P1/P2 findings and passed all 11 canonical x86_64-linux flake
  checks using `--max-jobs 2 --cores 2`.
- [x] (2026-07-31) Built immutable Devctl
  `/nix/store/7wpwvxywqvi68j9h8kzx2hcw22hr83cy-devkit-devctl-dev` with binary
  SHA-256 `1ced47a4e6a2e4fe4d500175c550183112ca4509d17d4768de45cc3b36580d36`.
- [ ] Publish the Devkit repair and pin it in WSL/Nix.
- [ ] Converge through the sole Fleet/Colmena lane, reconstruct failed Product
  consumers, and record the typed reset receipts.

## Surprises & Discoveries

- The orphaned SBT supervisors are not merely missing
  `DEVKIT_NATIVE_AGENT`; their governed implement-stage `CODEX_HOME` is under
  `/tmp/fleet-native-product-governance/agentN/control-plane/workspace-submit-artifacts`
  rather than the native home. Therefore weakening the existing native marker
  check would be both insufficient and unsafe.
- The three supervisors span user-session and controller-operation cgroups.
  Cgroup name alone is not a stable ownership oracle.
- Agent 2 also has `tail --pid=<supervisor> -f /dev/null`; selecting the exact
  supervisor first and then taking descendant closure handles this without a
  second command-specific rule.
- A detached Codex worktree can be Git-clean but is not a native governance
  workspace. It cannot be used to bypass reconstruction.
- `/proc/<pid>/exe` for the Nix OpenJDK launcher resolves to
  `.../lib/openjdk/bin/java`, while `argv[0]` is the immutable
  `.../bin/java` symlink. Exact custody therefore compares the resolved
  source-declared Nix symlink target rather than requiring the two path strings
  to be identical.
- Direct immutable-binary dry-runs see currently relaunched slot-1 and slot-2
  Codex app-servers and the slot-3 SSH wrapper before they reach reset's normal
  enclosing lifecycle shutdown. They remain correctly unowned. An earlier
  slot-2 dry-run reached the exact SBT supervisor and exposed the OpenJDK
  symlink distinction above; the new matcher has dedicated resolution tests.
- Direct ambient `go test ./...` includes repository-layout and native-launch
  integration tests whose package-linked executable and external profile
  prerequisites are absent from an arbitrary source shell. Those known
  environment-bound failures are not changed by this repair; the canonical
  Nix check owns the hermetic full portable test surface.
- An independent fail-closed review found four pre-publication issues in the
  first implementation: sequential partial TERM effects, raw-PID signaling,
  numeric rather than topology ordering, and receipts visible only on
  success. The implementation now prevalidates complete signal sets, binds
  signals to pidfds, orders leaves before parents, and wraps later failures
  with typed attempted-signal receipts.
- A dirty Git flake omits completely untracked source files. The first bounded
  canonical check therefore compiled without the two new pidfd files and
  failed on their undefined symbols. Marking those files as intended Git
  additions made the exact same check include them; all 11 checks then passed.
  This was source selection, not a test or implementation failure.
- The flake declares both x86_64-linux and aarch64-linux. Native execution and
  all checks passed on x86_64-linux; a bounded `GOOS=linux GOARCH=arm64`
  test-binary compile also passed, proving the pidfd wrapper and command
  package compile for the other declared architecture.

## Decision Log

- Decision: Repair Devkit native reset, not Product SBT shutdown.
  Rationale: SBT correctly refuses an identity-mismatched live process. The
  failing operation is slot reconstruction, whose canonical source owner is
  Devkit.
  Date/Author: 2026-07-31 / Codex.
- Decision: Use an overlay-declared exact launcher signature rather than a
  force flag or ambient PID list.
  Rationale: the Devkit package may stop a process only when its own source
  declares the command it expects for this repository. Callers must not be
  able to broaden deletion or signal scope.
  Date/Author: 2026-07-31 / Codex.
- Decision: Require parent PID 1, exact slot owner UID/GID, exact slot HOME and
  CWD, an immutable Nix executable of the declared basename, exact arguments
  and slot-relative launcher, a declared governed `CODEX_HOME` root, at least
  one slot-scoped file descriptor, and no contradictory native-agent marker.
  Rationale: no single ambient field proves custody; the conjunction is narrow
  enough to distinguish the observed orphaned governed SBT servers from an
  unrelated user process.
  Date/Author: 2026-07-31 / Codex.
- Decision: Re-read the full declared identity immediately before signaling.
  Rationale: PID plus start ticks rejects PID reuse but not an `exec` under the
  same PID. The source-declared process must still be the same semantic
  process at the effect boundary.
  Date/Author: 2026-07-31 / Codex.
- Decision: Acquire a Linux pidfd for every planned process before the first
  semantic revalidation and use only `pidfd_send_signal` for TERM/KILL.
  Rationale: start ticks and boot identity detect reuse during revalidation,
  but a pidfd also binds the kernel signal target across the remaining
  revalidate-to-signal interval.
  Date/Author: 2026-07-31 / Codex.
- Decision: Prevalidate the complete selected set before either signal phase,
  then revalidate each member immediately before signaling it in leaf-first
  topology order.
  Rationale: a later drift must not cause an earlier partial effect, and PID
  magnitude does not encode ancestry. After TERM, an adopted descendant may
  be reparented by the package's own effect, so escalation retains custody via
  its pidfd plus stable boot/start, owner, slot-contact, and non-contradictory
  marker evidence.
  Date/Author: 2026-07-31 / Codex.
- Decision: Include attempted TERM/KILL outcomes in process receipts and wrap
  any later reconstruction error with those receipts.
  Rationale: a failure after process disposal is still effectful evidence and
  must not collapse into an untyped bootstrap or readiness error.
  Date/Author: 2026-07-31 / Codex.

## Context and Orientation

`cli/devctl/internal/commands/nativecmd/native_slot_reset.go` discovers every
process touching the selected worktree, home, or state roots. It currently
selects only processes whose environment contains the exact
`DEVKIT_NATIVE_AGENT`, `HOME`, and `CODEX_HOME` triple, plus their descendants.
Any other touching process stops reset before filesystem effects.

`cli/devctl/internal/config/overlay.go` owns the typed YAML surface.
`overlays/dev-all/devkit.yaml` is the authoritative Product-oriented overlay.
The repair will add one exact launcher declaration there; other overlays retain
the current behavior because an absent rule authorizes nothing.

`cli/devctl/internal/commands/nativecmd/native_test.go` owns the process
selection tests. `cli/devctl/internal/config/overlay_test.go` owns config
projection coverage.

## Plan of Work

First, add a typed `reset_orphan_processes` declaration under `native`. The
rule contains a name, immutable executable basename, exact arguments preceding
the launcher, a safe relative launcher path, and the absolute governed Codex
home root. Reject empty names, path-bearing executable basenames, unsafe
relative paths, non-absolute Codex roots, duplicate names, and incomplete
rules before process discovery.

Second, enrich `/proc` observation with UID/GID, command arguments, CWD,
selected environment fields, and whether an FD points into the exact slot.
An orphan root is selectable only when every source-declared condition holds.
Mark descendants by the adopted root that granted custody; do not acquire
ancestors or siblings.

Third, retain stable boot/start identity for every selected PID, acquire a
pidfd for each target, prevalidate the complete set, and revalidate each target
immediately before TERM and escalation. For adopted descendants, require exact
owner, source ancestry, and slot contact before TERM. If the package's TERM
reparents a still-live descendant, escalation keeps only the already-bound
pidfd whose boot/start, owner, slot contact, and marker still agree. Order all
signals leaf-first from captured topology.

Fourth, add a positive exact-match test and hostile cases for missing rule,
wrong UID/GID, non-parentless root, foreign executable, argument drift,
foreign launcher, wrong HOME/CWD/CODEX root, contradictory native marker, and
no slot FD. Add descendant and reversed-PID topology cases; mutate planned
fixtures to prove same-PID and boot drift cause zero signals; verify
post-revalidation PID replacement cannot retarget pidfd signaling; and verify
effect receipts from initial and cleanup plans survive later reconstruction
failure.

Finally, run formatting, focused tests, all Go tests, CLI build, and the
source-derived flake checks. Publish only after clean diff review. Update the
WSL/Nix Devkit pin, rebuild with `--max-jobs 2 --cores 2`, deploy through the
Fleet-selected Colmena path with one evaluation node and one deployment
worker, then rerun typed reconstruction.

## Concrete Steps

From `/workspaces/dev/devkit-native-orphan-repair`:

    gofmt -w cli/devctl/internal/config/overlay.go \
      cli/devctl/internal/config/overlay_test.go \
      cli/devctl/internal/commands/nativecmd/native_slot_reset.go \
      cli/devctl/internal/commands/nativecmd/native_test.go \
      cli/devctl/internal/commands/nativecmd/native_pidfd_linux.go \
      cli/devctl/internal/commands/nativecmd/native_pidfd_unsupported.go
    cd cli/devctl && GOMAXPROCS=2 go test -count=1 ./internal/config ./internal/commands/nativecmd
    cd cli/devctl && GOMAXPROCS=2 go vet ./internal/config ./internal/commands/nativecmd
    cd cli/devctl && GOMAXPROCS=2 go test -race -count=1 ./internal/config ./internal/commands/nativecmd
    nix flake check --no-update-lock-file --max-jobs 2 --cores 2

Use the repository's normal Git publication path. Do not manually signal a
live Product process while iterating.

## Validation and Acceptance

Acceptance requires all of the following:

1. With no source rule, every unowned touching process still blocks reset.
2. The declared Product SBT supervisor is selected only when every identity
   factor agrees, and its exact descendants are selected without acquiring
   ancestors or siblings.
3. Every hostile one-factor mutation has zero signal and filesystem effects.
4. Same-PID command, boot, or identity drift between planning and signaling is
   rejected before any signal; every signal uses an already-bound pidfd.
5. Focused and complete Go tests pass, the compiled CLI builds, and the
   source-derived Nix checks pass with bounded parallelism.
6. The published Devkit revision is pinned and Fleet/Colmena-converged.
7. A typed `native reset` receipt reconstructs each previously blocked slot;
   subsequent status proves the old process identities and dirty candidate
   state absent.
8. If process disposal succeeds but reset, bootstrap, cleanup, or readiness
   later fails, the returned error still aggregates each attempted signal and
   outcome from every effectful plan.

## Idempotence and Recovery

Planning remains read-only and may be repeated. Reset keeps its existing
package-owned lock and re-plans after lock acquisition. If any process field
changes, stop before signaling or filesystem effects. If a test or build
fails, repair only this Devkit branch. If live convergence fails, preserve its
receipt, repair the canonical source owner, discard the failed consumer, and
retry reconstruction; never substitute manual PID signaling.

## Outcomes & Retrospective

The source repair, hostile unit coverage, vet/race checks, independent review,
and canonical Nix checks are complete. Publication, WSL/Nix pinning, live typed
reconstruction, and old-process absence evidence remain pending.

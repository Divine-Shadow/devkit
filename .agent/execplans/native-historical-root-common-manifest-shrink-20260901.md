# Historical root-common native manifest shrink

This ExecPlan is a living document maintained according to `.agent/PLANS.md`.

## Purpose

A source-count reduction must be able to retire an obsolete native lane that
was created before package-owned Git common-directory isolation. In that old
layout the lane's `.git` file selects linked-worktree metadata under the
ordinary source checkout `<host-root>/<repo>/.git/worktrees/<entry>`. The
operator-visible result is that an exact idle, custody-free surplus lane can be
retired through the existing manifest-shrink transaction without fetching,
pruning, resetting, or otherwise changing the source checkout or its shared
Git repository.

## Progress

- [x] (2026-09-01) Reproduced the deployed refusal with a real linked-worktree
  fixture whose `commondir` resolves to the historical root checkout.
- [x] (2026-09-01) Added exact historical-root classification, source origin
  and branch checks, fresh current-remote containment, strict index
  custody checks, the three-file setup-layer declaration, and the separate
  five-directory disposable generated-residue declaration.
- [x] (2026-09-01) Replaced per-check ambient fetches with one operation-bound,
  package-transport proof using managed egress, sanitized Git environment,
  bounded descendant cleanup, disjoint source-derived scratch, and a read-only
  historical object alternate.
- [x] (2026-09-01) Kept filesystem disposal inside the existing typed durable
  manifest-shrink transaction and left historical metadata and refs untouched.
- [x] (2026-09-01) Added acceptance, ambient-ignore/config/object-redirection,
  index flag/stage, remote advance/force-rewrite, one-fetch multi-suffix,
  stale-protected-ref/fresh-setup-tree, late-suffix refusal, root snapshot,
  scratch-boundary, prepared-crash, and post-CAS recovery tests.
- [x] (2026-09-01) Reran and recorded complete Go, vet, Nix, CI,
  semantic-diff, and repository-hygiene gates for the repaired candidate.

## Context

`cli/devctl/internal/commands/nativecmd/native_slot_reset.go` classifies a
surplus lane's common repository and proves process, socket, filesystem, and
Git quiescence. `native_manifest_shrink_transaction.go` owns the typed journal,
compare-and-swap, rollback, and post-CAS cleanup. `worktrees.PlanNativeSlotReset`
stages only source-derived worktree, home, state, and lane-owned common paths.
The historical source-root common is intentionally not a reset candidate.

The deployed classifier admitted only
`<worktree-root>/.devkit/git/agentN/<repo>.git` and the transitional shared
`<worktree-root>/.devkit/git/<repo>.git`. A real surplus linked to
`<host-root>/<repo>/.git/worktrees/<entry>` therefore failed before the durable
transaction with “neither exact lane nor legacy registration.”

## Invariants

- Historical-root authority is derived only from the canonical manifest
  geometry: `filepath.Dir(host_worktree_root)/repo/.git`. No ambient common
  repository or caller path is accepted.
- Root checkout, common repository, surplus worktree, and matching metadata
  are real canonical non-symlink directories. The `.git`, reverse `gitdir`, and
  `commondir` links must select one exact registration, with no competing lane
  or legacy registration.
- The root repository is non-bare and has the exact source-declared identity.
  The later live-migration amendment admits only strict same-repository GitHub
  SSH SCP/default/port-443 spellings for historical/v1 custody. The surplus
  worktree is on exact `agentN`, or the fully derived historical
  `codex/agentN/main`; its commit is contained in the
  freshly fetched current remote base. The protected checkout's potentially
  stale `refs/remotes/origin/<base>` is not ancestry or setup-tree authority
  for historical-root retirement. Lane and legacy layouts retain their local
  source-base check.
- Current remote history is fetched once per shrink operation into an
  ephemeral bare proof object database. The fetch reuses the package-owned
  Git/SSH authority, managed egress proxy, existing idle-timeout/process-group
  cleanup, and an empty sanitized environment. A proof-local alternates file
  reads historical objects without writing the shared root. The scratch
  directory is created beneath a source-derived parent only after canonical
  geometry checks, must be disjoint from every protected/shared boundary, and
  is identity-checked before recursive cleanup. If its identity cannot be
  captured after creation or changes before cleanup, the operation fails and
  deliberately retains that bounded scratch path rather than recursively
  deleting an unowned replacement; transport cleanup still runs.
- Non-ignored untracked files, gitlinks/submodules, unsafe declarations,
  duplicate declarations, and any unexpected tracked modification refuse
  shrink. The only permitted tracked setup projection paths are
  `.codex/config.toml`, `scripts/devops/governance-control-plane`, and
  `scripts/devops/governance-mcp-stdio-forward`. The index must contain only
  stage-zero non-gitlink entries with no assume-unchanged, skip-worktree,
  sparse-checkout, or unmerged state. Ignored residue may leave only beneath
  the separately declared real directory roots `.bsp`, `logs`,
  `project/project/target`, `project/target`, and `target`; ambient global/info
  ignore rules do not confer
  custody authority or permit any arbitrary-path deletion.
- All suffix slots preflight before history capture or quarantine. Cold Codex
  history is captured before filesystem staging, then all custody is rechecked.
- The existing typed shrink journal remains the only durable mutation. It
  stages the exact obsolete worktree, home, and state, installs the manifest by
  compare-and-swap, and recovers prepared or committed crash prefixes. It does
  not stage or prune historical root Git state.

## Surprises & Discoveries

- The ordinary source checkout is not manifest agent1. It is the repository
  beside `host_worktree_root`; every historical native lane, including agent1,
  can be a linked worktree registered beneath that source checkout.
- The live source root did not contain the newest remote-main object. Requiring
  `cat-file` in the root common would preserve root immutability but reject the
  exact valid lane. An isolated bare fetch proves current ancestry without
  mutating the root common.
- A protected root checkout may also retain a stale `origin/<base>` while an
  `agentN` branch is already fast-forwarded to current remote main, including a
  newly tracked declared setup projection. Historical retirement must use the
  one fetched proof commit as the sole authority for both ancestry and
  setup-tree membership; consulting the stale protected ref falsely rejects
  disposable custody.
- The historical lane had three expected tracked setup projections, not only
  `.codex/config.toml`; the two exact governance wrappers are source-declared
  alongside it. A fourth tracked path still refuses.
- The live Meowlnir2 surplus had no tracked or non-ignored untracked dirt, but
  retained source-ignored `.bsp`, `logs`, `project/target`, and `target`
  generated residue. Treating those exact source-known build roots as business
  custody would contradict the disposable execution contract. Treating every
  `!!` entry as disposable would instead let `.git/info/exclude`, a global
  excludes file, or other ambient policy hide arbitrary custody. The final
  classifier therefore permits ignored residue only beneath those five exact
  declared real directories and refuses every other ignored or untracked path.
- An isolated repository is not isolated if ambient `TMPDIR`,
  `GIT_OBJECT_DIRECTORY`, alternate-object variables, or Git config can redirect
  its writes. The proof now starts from `env -i`, carries only fixed/source
  authority, and creates its identity-checked scratch boundary explicitly.

## Decision Log

- 2026-09-01: Reject root-common fetch/prune and reject whole-prefix reset as
  migration mechanisms. Select exact historical registration recognition plus
  a read-only shared-root contract and isolated current-remote proof.
- 2026-09-01: Leave the retired lane's historical metadata and branch ref stale
  but byte-identical. Removing either would mutate the protected root common;
  a later proven-idle whole-prefix reset owns that cleanup.
- 2026-09-01: Permit tracked dirt only through exact source config, never a
  directory, glob, status heuristic, or operator override.
- 2026-09-01: Reject ambient ignore provenance as disposal authority. Add a
  separate exact source declaration for the known generated directory
  roots and reject all other `!!` entries.
- 2026-09-01: Bind current-remote proof to the full shrink operation. Reuse the
  existing package-owned SSH and managed fetch helpers, fetch only once, and
  require sanitized config/index and disjoint scratch geometry.
- 2026-09-01: For historical-root only, make the fetched proof commit the sole
  ancestry and setup-tree authority. Do not consult or advance the protected
  checkout's stale remote-tracking ref; keep the pre-existing local-ref check
  unchanged for lane and legacy layouts.

## Verification

Focused evidence passed after the proof/custody repair:

    cd cli/devctl
    go test -count=1 ./internal/worktrees ./internal/commands/nativecmd

The stale-ref regression fast-forwards `agent3` to a commit already in current
remote main while leaving protected `origin/main` stale, declares and dirties a
setup path tracked only by the fresh tree, and proves successful retirement,
one fetch, and byte-identical root/common state.

Complete evidence passed on the final repaired source state:

    GOCACHE=/tmp/devkit-root-common-full-gocache GOMODCACHE=/tmp/devkit-root-common-full-gomodcache go test -count=1 ./...
    go vet ./...
    nix --extra-experimental-features 'nix-command flakes' flake check --show-trace
    git diff --check
    git diff --check
    env -u DEVKIT_OVERLAYS_DIR make ci-cheap

The controller shell may inherit `DEVKIT_OVERLAYS_DIR` naming multiple packaged
overlay copies; the exact source-checkout CI gate removes only that ambient
variable. The first standalone flake run exposed a test-fixture defect: the
hostile `env -i` fetch wrapper invoked bare `sleep`, so the Nix sandbox correctly
had no `PATH` and error output kept resetting the idle timer. The fixture now
embeds the absolute test-closure `sleep` executable. Its focused timeout test,
the repeated standalone flake check, and the final `ci-cheap` run all passed.
The final CI run passed the full Go suite, all 12 flake checks, immutable devctl
runtime authority, overlay metadata and lock policy, runtime matrix, retired
runtime guard, and Nix-overlay runtime guard. No tests were skipped. No live
station, publication, deployment, or canary retry belongs to this
implementation lane.

## Outcomes & Retrospective

Implementation and full source verification are complete. The repair recognizes
only the one historical topology needed for migration, proves ancestry and
setup-tree custody from one fresh operation-current-remote commit without
changing shared root state, preserves
cold history ordering and transactional recovery, and keeps every refusal
before the durable shrink boundary. The one intentional residue is the retired
lane's historical metadata and branch ref, which remain byte-identical in the
protected root common until a later proven-idle whole-prefix reset.

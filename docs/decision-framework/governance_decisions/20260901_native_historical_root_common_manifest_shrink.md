# Historical root-common native manifest shrink

Framework: [Tradeoff Decision Framework](../tradeoff_decision_framework.md)

## problem

An obsolete native suffix can still be registered as a linked worktree beneath
the ordinary source checkout's `.git/worktrees` directory. The package shrink
classifier knows only isolated lane common repositories and the transitional
package-owned shared bare repository, so it refuses this historical but exact
topology. A repair must retire disposable compute without converting the
operator/source checkout into mutable transaction state.

## options

1. Require an operator to clean or rewrite the station-local Git topology, then
   retry reconstruction.
2. Fetch, prune, or remove the surplus registration/ref directly in the shared
   source-root common repository before shrinking.
3. Recognize only the exact manifest-derived historical root registration,
   prove custody read-only (using an isolated temporary object database for
   current remote history), and retire only worktree/home/state through the
   existing typed manifest-shrink transaction.

## selection_rationale

Option 3 preserves the highest-value constraints: exact authority, no source
checkout mutation, no unpublished work loss, and crash-recoverable disposal.
Option 1 creates an ungoverned manual fallback and is not reproducible. Option
2 risks the root checkout's refs, index, active transactions, and other linked
worktrees. The selected design keeps stale retired metadata/ref bytes until a
later proven-idle whole-prefix reset rather than broadening this transaction.

The only tolerated tracked differences are three exact setup-layer paths
declared by the `dev-all` source overlay. Ignored-path provenance is not custody
authority: only ignored residue beneath the five separately declared directory
roots `.bsp`, `logs`, `project/project/target`, `project/target`, and `target`
may leave with the leased
slot. Every other ignored path, every non-ignored untracked path, and every
submodule, unexpected tracked, non-stage-zero, assume-unchanged, skip-worktree,
sparse, unsafe selected registration, symlinked, wrong-origin,
wrong-branch, ahead, or competing active custody domain fails closed. When the
worktree is already absent, stale historical reverse-registration metadata is
protected but inert because this transaction never stages or removes it; any
surviving exact canonical or permitted historical migration branch remains
active custody and is still checked.

## safety_checks

- Derive `<host-root>/<repo>/.git` only as
  `filepath.Dir(manifest.host_worktree_root)/manifest.repo/.git`.
- Require canonical real host root/worktree/selected-metadata paths, exact
  forward/reverse registration and `commondir`, one active custody domain, a
  non-bare root, and exact source identity. Origin equality remains byte-exact
  except for the known `dev-all`/`ouroboros-ide` migration, where a strict raw
  GitHub SSH parser may equate only the same owner/repository expressed as SCP,
  default-port SSH, or `ssh.github.com:443`; config is read with includes
  disabled so URL rewrites cannot confer authority. A pointer may use only its
  exact
  manifest-derived sandbox counterpart: the consumer worktree must preserve
  the host `agentN/<repo>` suffix, the consumer common must be derived from the
  declared sandbox worktree root, and forward metadata must preserve the exact
  `worktrees/<entry>` suffix. This is a lexical bijection; the bwrap-only
  consumer namespace need not exist in the host shrink process. Absolute
  pointer text is compared byte-for-byte without trimming or cleaning (apart
  from one conventional final LF), and a relative host pointer is valid only
  when it exactly equals the derived `filepath.Rel` spelling. Arbitrary aliases
  remain outside authority, and all protected reads and mutations use canonical
  real host paths.
- For a present worktree, use its bounded exact `.git` pointer as the sole
  active-registration selector and inspect only that selected metadata. Do not
  enumerate unrelated entries in the shared historical registration
  directory. Prove both the selected branch and every other existing exact
  migration branch against the same fresh remote. For an absent worktree,
  resolve exact `agentN` plus the one fully
  derived historical `codex/agentN/main` spelling across the three
  manifest-derived lane, legacy, and historical common repositories;
  when more than one branch domain survives, require every discovered commit to
  be independently contained in one freshly fetched current base and refuse
  disagreement with any known lane/legacy registration. Equal or divergent
  contained branch copies are migration residue; prefer the historical domain
  when present, then the non-disposable legacy domain, and prefer canonical
  `agentN` if both historical spellings survive. Every discovered ref is
  checked against the same fresh remote and both exact ref lock paths are
  inspected. A sole historical branch
  receives the same current-remote proof; sole lane/legacy domains retain their
  existing local-base check. A
  detached or wrong-branch stale historical registration is
  inert but byte-preserved with its objects because no shared-root path is a
  transaction candidate.
- Require exact `agentN`, or only the fully derived historical
  `codex/agentN/main` alias in the known migration domain, and containment in a
  freshly fetched current remote base. For historical-root only, that fetched
  commit is the sole ancestry and
  setup-tree authority; never consult or advance the protected checkout's
  potentially stale `refs/remotes/origin/<base>`. Lane and legacy layouts keep
  their existing local remote-tracking check. One operation-bound proof uses
  the package-owned Git/SSH configuration and managed egress proxy, an empty
  sanitized Git environment, a bounded managed fetch with descendant cleanup,
  and a proof-local alternates file that reads the historical object store. It
  fetches the base once for the complete suffix and never writes the shared
  root. Its source-derived scratch boundary must be canonical and disjoint from
  every protected/shared root. Host-side Git custody commands use a second
  fresh minimal view for each custody pass inside that same owned scratch:
  bounded copies of `HEAD`, `index`, optional `config.worktree`, and at most one
  exact SHA-1/SHA-256-named split-index companion, with only scratch `commondir`
  and `gitdir` rewritten to host spellings. The protected metadata is never
  changed. The compatibility set rejects extra split-index companions,
  non-empty worktree-specific refs, symlink/non-regular required files, and
  every other unsupported metadata shape rather than copying an unbounded
  metadata tree.
- Avoid historical registration-directory enumeration entirely. Cap the
  selected per-worktree metadata enumeration, pointer, config, and index reads,
  plus every captured Git stdout stream. Absent-worktree domain selection uses
  at most four exact `show-ref --verify --quiet` calls across three common
  repositories. Fixed Git deadlines and output overflow terminate the complete
  process group. Managed I/O drain and wait cleanup are grace-bounded; an
  escaped inherited descriptor becomes a typed infrastructure failure rather
  than a hang or an ordinary Git predicate exit. Inspect only exact
  active common, selected metadata, and branch lock surfaces; never recursively
  walk the shared object store.
- Before status, parse bounded common and worktree config without following
  includes and refuse any repo-local `include`/`includeIf` or clean/process
  filter. Preserve common `info/exclude` through the validated commondir, copy
  `config.worktree`, and separately refuse sparse config or index flags so the
  scratch view cannot silently weaken custody semantics or execute a filter.
- Reject all Git locks, non-ignored untracked files, gitlinks/submodules, and
  tracked dirt outside the exact source declaration. Prove a stage-zero index
  with no assume-unchanged, skip-worktree, sparse-checkout, or unmerged state
  under sanitized config. Permit ignored generated residue only below the five
  exact source-declared real directory roots; ambient global/info ignore rules
  cannot broaden that set.
- Preserve all-suffix preflight, cold-history capture-before-stage, recheck,
  typed journal, manifest CAS, rollback, and post-CAS recovery.
- Stage no root-common path. Leave historical metadata and branch refs intact.

## rollback_plan

Revert the source commit before deployment. The implementation performs no
automatic migration until a shrink encounters an exact historical worktree or
surviving exact historical branch, and the transaction never changes the
shared root checkout.
Already retired worktree/home/state remain intentionally disposable; their
stale historical registration/ref remains for a later whole-prefix reset.

## decision_scope

This decision covers only native manifest shrink of exact obsolete linked
worktrees registered beneath the source root checkout, including exact
bijective host/sandbox spellings already declared by the manifest; the three
source-owned setup-layer files; five disposable generated-residue directory
roots; strict known-migration origin and branch identities; operation-bound
remote-containment proof; transaction recovery tests;
and operator documentation. It does not authorize live deployment,
station-local repair, general root-common or alias acceptance, Product source
changes, manual Git cleanup, or whole-prefix reset.

## amendment_2026_09_03

A live source-derived reconstruction found 48 legitimate historical
registrations in one shared root, exceeding the implementation's arbitrary
limit of 32. Raising that count would only defer the same failure and keep
unrelated shared state in a lane-local custody decision. The implementation now
uses target-shaped selection: the present worktree's exact forward link, or a
bounded three-common exact-branch resolution when the worktree is absent.
Unselected and stale historical metadata, including its lock or detached-HEAD
state, remains protected for later whole-prefix custody but cannot authorize or
block deletion of a different physical lane because manifest shrink never
mutates shared-root metadata or refs. Known lane/legacy reverse registrations
remain authoritative because they claim the selected physical worktree; the
lane common is additionally a reset candidate. Disagreement with branch-domain
selection refuses before mutation.

## amendment_2026_09_03_live_identity

The next centrally deployed canary passed target selection and exposed three
later, deterministic historical facts: the v1/root config uses the old exact
SCP GitHub origin while current source declares the port-443 SSH endpoint; the
surplus branch is exactly `codex/agent3/main`; and SBT left ignored output under
`project/project/target`. A read-only audit proved the remaining process,
socket, topology, index, dirt, lock, and no-ahead predicates. The correction is
therefore limited to a strict same-repository GitHub SSH identity parser used
only by historical/v1 shrink validation, one branch alias derived completely
from the exact Product manifest tuple, and one additional exact residue root.
It does not change v2 lane origin creation/reuse, normalize arbitrary URLs, or
accept arbitrary Codex refs. Both allowed exact refs are protected even when
one is not selected by a present worktree or neither ref currently exists:
every existing commit is fresh-remote-contained and both lock paths must be
absent.

Independent review also found that a command leader can exit after a child
escapes the process group with `setsid` while retaining inherited output pipes.
Managed execution now owns those pipes, bounds drain and wait cleanup by the
declared grace, and exposes cleanup failure as a typed error that shrink cannot
mistake for a meaningful Git exit. Every normal, cancellation, deadline, idle,
and overflow path also terminates and proves disappearance of the original
process group before returning. This is a convergence-safety correction, not
additional deletion authority.

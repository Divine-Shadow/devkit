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
authority: only ignored residue beneath the four separately declared directory
roots `.bsp`, `logs`, `project/target`, and `target` may leave with the leased
slot. Every other ignored path, every non-ignored untracked path, and every
submodule, unexpected tracked, non-stage-zero, assume-unchanged, skip-worktree,
sparse, duplicate, unsafe, symlinked, wrong-origin, wrong-branch, ahead, or
competing-registration state fails closed.

## safety_checks

- Derive `<host-root>/<repo>/.git` only as
  `filepath.Dir(manifest.host_worktree_root)/manifest.repo/.git`.
- Require canonical real root/worktree/metadata paths, exact forward/reverse
  registration and `commondir`, one registration domain, a non-bare root, and
  exact declared origin.
- Require exact `agentN` and containment in a freshly fetched current remote
  base. For historical-root only, that fetched commit is the sole ancestry and
  setup-tree authority; never consult or advance the protected checkout's
  potentially stale `refs/remotes/origin/<base>`. Lane and legacy layouts keep
  their existing local remote-tracking check. One operation-bound proof uses
  the package-owned Git/SSH configuration and managed egress proxy, an empty
  sanitized Git environment, a bounded managed fetch with descendant cleanup,
  and a proof-local alternates file that reads the historical object store. It
  fetches the base once for the complete suffix and never writes the shared
  root. Its source-derived scratch boundary must be canonical and disjoint from
  every protected/shared root.
- Reject all Git locks, non-ignored untracked files, gitlinks/submodules, and
  tracked dirt outside the exact source declaration. Prove a stage-zero index
  with no assume-unchanged, skip-worktree, sparse-checkout, or unmerged state
  under sanitized config. Permit ignored generated residue only below the four
  exact source-declared real directory roots; ambient global/info ignore rules
  cannot broaden that set.
- Preserve all-suffix preflight, cold-history capture-before-stage, recheck,
  typed journal, manifest CAS, rollback, and post-CAS recovery.
- Stage no root-common path. Leave historical metadata and branch refs intact.

## rollback_plan

Revert the source commit before deployment. The implementation performs no
automatic migration until a shrink encounters one exact historical
registration, and the transaction never changes the shared root checkout.
Already retired worktree/home/state remain intentionally disposable; their
stale historical registration/ref remains for a later whole-prefix reset.

## decision_scope

This decision covers only native manifest shrink of exact obsolete linked
worktrees registered beneath the source root checkout, the three source-owned
setup-layer files, four disposable generated-residue directory roots,
operation-bound remote-containment proof, transaction recovery tests, and
operator documentation. It does not authorize live deployment, station-local
repair, general root-common acceptance, Product source changes, manual Git
cleanup, or whole-prefix reset.

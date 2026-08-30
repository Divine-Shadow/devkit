# Native lane Git common-directory isolation

Framework: [Tradeoff Decision Framework](../tradeoff_decision_framework.md)

## problem

Native `dev-all` worktrees currently share one package-owned bare Git common
repository. Separate worktree directories therefore still share refs, lock
files, worktree registrations, and mutation custody. A selected-slot
reconstruction must migrate that lane without writing to a still-active legacy
sibling.

## options

1. Keep one common repository and serialize Git operations across every lane.
2. Copy the common repository in place while rewriting every active worktree.
3. Give each lane a source-derived common repository and migrate lanes only
   through their selected-slot reconstruction boundary.

## selection_rationale

Option 3 makes the lane identity, ref domain, and lock domain the same explicit
contract. Fresh lanes use `.devkit/git/agentN/<repo>.git`; the ownership marker
binds the repository, declared origin, and `agentN` identity. An active legacy
lane remains readable at `.devkit/git/<repo>.git` until its own reset, while a
selected reconstruction creates only the selected lane's new repository.

This preserves correctness and explicit custody first, makes independence
directly testable, avoids coordinated mutation of active siblings, retains
portable relative linked-worktree metadata, and permits incremental rollout.

## safety_checks

- Preflight admits only the exact lane common directory or the exact legacy
  package-owned directory during migration.
- A v2 marker binds repository, origin, and lane identity before reuse.
- Selected-slot reset owns exactly its worktree, home/state, and lane common
  repository; the active sibling retains byte-for-byte custody of the legacy
  shared common directory.
- A source-count reduction retires surplus indices through the manifest-shrink
  transaction while the prior manifest still supplies their capacity. An exact
  v1 marker plus matching forward/reverse registration admits a legacy surplus;
  its shared metadata and ref remain stale but untouched until the final
  proven-idle whole-prefix reset.
- Integration tests snapshot a live legacy sibling's refs, metadata, and files
  while agent1 is reconstructed into its new lane domain and while a legacy
  suffix is incrementally retired after source-count shrink.
- Fresh multi-lane setup proves distinct common directories, refs, locks, and
  portable projections.

## rollback_plan

Revert this commit before deploying it. Existing legacy worktrees remain
unchanged because migration occurs only at a selected-slot reset boundary; no
in-place fleet-wide metadata rewrite is performed.

## decision_scope

This decision covers native `dev-all` Git metadata creation, preflight,
portable-pointer repair, selected-slot reset, whole-prefix reset, manifest
shrink checks, and the corresponding tests and operator documentation.

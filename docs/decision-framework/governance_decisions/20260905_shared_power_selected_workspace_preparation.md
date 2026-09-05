# Shared Power selected workspace preparation

Framework: [Tradeoff Decision Framework](../tradeoff_decision_framework.md)

## problem

Fleet declares isolated Shared Power GUI lanes under
project-worktrees/pokeemerald-agentN, but native preparation materialized
agent-worktrees/agentN and runtime planning admitted only Management and
Product lane roots. A missing declared checkout could therefore trigger
successful preparation in a different directory and still fail GUI startup.
The existing native worktree lifecycle and runtime preparation remain the
owning effects; historical worktrees and Codex homes retain their custody.

## options

1. Rewrite Desktop inventory to use the historical prefix.
2. Add manual clones, symlinks or broader filesystem binds at launch time.
3. Adapt existing native prepare, plan and exec to the declared selected
   Shared Power geometry while retaining existing linked-Git ownership
   checks and immutable GUI configuration selection.

## selection_rationale

Select option 3. Exact source-derived geometry preserves correctness and the
existing isolated-lane contract. Real Git preparation and plan tests provide
verifiability. Restricting the adapter to Shared Power indexes 1 and 2,
preserving historical homes, and leaving siblings and shared manifests
untouched protects operational safety. One explicit selected invocation and
the same plan for preparation and execution keep source and runtime evidence
aligned. Delivery speed follows these constraints.

## safety_checks

- Native prepare selects one lane with an explicit index, workspace root and
  GUI target; a simultaneous count is rejected.
- Both preparation and execution use the same exact worktree, preserved
  pokeemerald-agentN/home, and /workspaces/dev/<repo> projection.
- The selected root owns its checkout and .devkit/git/agentN/<repo>.git.
  Linked metadata remains portable and inside the selected root.
- Require the existing immutable GUI projection and source-declared SSH origin
  before materialization. Reject wrong indexes, roots, symlinked parents,
  foreign Git metadata, disabled isolation, and ambiguous selection.
- Generic prefix lifecycle, historical paths, homes and all-slot manifest
  writes are unchanged. The selected receipt never overwrites that manifest.
- Regression tests materialize missing checkouts from a local Git fixture for
  both indexes, exercise repeat preparation, project relative Git metadata,
  and prove sibling, old-checkout and old-home sentinels survive.
- Run the full Go suite and repository cheap gates before publication. Fleet
  integration and the centrally deployed launcher provide the live canary.

## rollback_plan

Revert this compatibility commit and its paired Fleet invocation change if
selected preparation changes an unrelated path or runtime isolation fails.
No historical worktree/home migration is performed. Newly created selected
lanes remain outside the historical prefix, so rollback does not require
rewriting user history.

## decision_scope

This is maintenance of existing Shared Power GUI preparation and isolated
execution. It introduces no provider, external dependency, destructive reset,
arbitrary workspace geometry, mount-policy exception, or new effect owner.
The root controller retains the source/deployment writer lease and live
verification; this Devkit change is the bounded source repair.

# Target-scoped TI4 bind for dev-workspace-2

## Decision

Add one source-declared workspace-egress bind contract for
`dev-workspace-2` when its repository is `shadow-throne-management`:

- host source:
  `/home/bayesartre/dev/control-plane-worktrees/agent1/ti4-calculator`
- sandbox target: `/workspaces/dev/ti4-calculator`
- access: required read/write

The contract is a private typed registry entry in the Devkit planner. Its
literal source is selected through a closed source identifier. No
caller-selectable bind flag, host-root substitution, environment variable,
broad Dev root, or parent-directory mount is introduced.

## Tradeoff made

Expose the one persistent TI4 worktree needed by the existing Management
consumer, while retaining the workspace-egress filesystem boundary for every
other consumer and path.

## Options considered

1. Mount `/home/bayesartre/dev` or `/workspaces/dev` broadly. Rejected because
   it exposes unrelated repositories, state, and credentials.
2. Add a generic CLI or environment-driven extra-bind option. Rejected because
   callers could widen their own development envelope.
3. Reassign the work to a separately launched GUI consumer. Rejected because
   it would split task custody and would not repair the existing consumer
   envelope.
4. Add an exact, planner-owned, identity-scoped bind contract. Selected.

## Value-order justification

The ordering is containment and source authority first, continuity of the
existing task second, and generality last. A single registered source and
target gives the current consumer the required repository without granting a
reusable filesystem-expansion primitive.

## Evidence

- The workspace-egress planner already owns bind generation and identifies
  consumers by project, index, and repository.
- The canonical TI4 worktree is owned at
  `control-plane-worktrees/agent1/ti4-calculator`.
- Existing TI4 consumers already use `/workspaces/dev/ti4-calculator` as the
  stable sandbox alias.
- Focused tests reject broad roots, traversal, unregistered source/target or
  consumer identities, duplicate contracts, and target collisions. Separate
  coverage proves other consumers retain their existing plans.

## Rollback plan

Remove the single registry entry and its resolver/tests, rebuild and repin the
Devkit runtime, converge the owning NixOS source, and restart only
`dev-workspace-2`. The TI4 repository itself requires no rollback because this
decision does not modify it.

# Product exec portable Git metadata migration

Framework: [Tradeoff Decision Framework](../tradeoff_decision_framework.md)

## problem

The Product lane-root guard correctly rejects cross-lane Git metadata, but an
older source-created lane can retain an absolute consumer-visible `.git`
pointer such as `/workspaces/dev/agent-worktrees/.devkit/git/...`. On the host
that path resolves to the exact package-owned repository beneath the declared
worktree root. The guard fails before isolated exec can start, even though the
existing Devkit bootstrap path already normalizes the same owned topology to
portable relative metadata.

## options

1. Widen the mount guard to admit the absolute consumer alias indefinitely.
2. Destructively reconstruct the selected lane before every affected start.
3. Before Product isolated exec planning, migrate only an absolute pointer
   whose worktree, common repository, worktree Git directory, and reverse
   pointer all pass the existing package-ownership checks.

## selection_rationale

Select option 3. It reuses the existing ownership validator and converges old
state to the portable source contract instead of weakening isolation or
discarding GUI history. Correctness and explicit contracts come first: only
the exact `dev-all`/`ouroboros-ide` selected lane is eligible. The migration is
idempotent and becomes a no-op once metadata is relative, which keeps the live
filesystem aligned with the source-defined bootstrap invariant.

## safety_checks

- Do not mutate plan, readiness, Management, or non-Product invocations.
- Require the exact selected workspace root before considering a write.
- Require regular `.git` metadata and the existing package-owned common-dir,
  worktree-dir, and reverse-pointer validation before rewriting.
- Keep foreign, symlink-escaped, malformed, and cross-lane metadata fail-closed.
- Test the historical consumer-alias shape and prove the migrated checkout is
  clean and usable.

## rollback_plan

Revert this commit if an eligible portable checkout changes unexpectedly or
the focused ownership tests fail. The rewrite changes path spelling only; the
same validator proves all rewritten paths resolve to the identical Git
metadata and worktree, so no source content or history rollback is required.

## decision_scope

This decision covers one compatibility migration at the existing native
Product exec boundary. It does not authorize broader mounts, arbitrary Git
metadata repair, lane reconstruction, task/history mutation, or changes to
Management and controller consumers.

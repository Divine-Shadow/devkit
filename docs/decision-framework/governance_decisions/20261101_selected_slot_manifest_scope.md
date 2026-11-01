# Selected-slot reset does not retire unrelated suffix slots

## problem

The selected-slot2 constructor failed trying to preserve surplus slot4 history
under the read-only global dev root. It unconditionally reconciled a shared
manifest count reduction before reaching the selected cold-history boundary.
An unrelated suffix retirement is not required to reconstruct the selected lane.

## options

1. Widen the global writable projection or move/omit surplus history. This
   expands custody and keeps unrelated cleanup coupled to a selected operation.
2. Require full suffix retirement before every selected reset. This turns a
   valid compatible manifest into a global lifecycle prerequisite.
3. Admit a canonical compatible manifest read-only, preserve the suffix and
   refuse an unresolved broad transaction. Select this existing scope repair.

## selection_rationale

The manifest is discovery geometry, not a selected request to dispose every
surplus instance. Existing strict prefix validation proves selected geometry;
the normal reset tail already preserves an existing manifest. No new command,
authority, storage path, transport or effect class is needed.

## safety_checks

Retain strict regular/no-symlink JSON parsing, exact canonical geometry and
current prefix identity. Unknown/pending shrink journals refuse and are never
recovered by a selected operation. Recheck at destructive idle barriers. Selected
process custody, mandatory selected GUI cold snapshot and final readiness remain
unchanged. Tests must leave unrelated history, homes, processes and manifest
bytes intact while preserving genuine conflict and selected-history refusals.

## rollback_plan

Revert the source correction and requalify through the canonical pipeline.
Do not mutate installed manifests, broaden mounts, bypass snapshot failure or
reuse a failed lifecycle attempt as acceptance.

## decision_scope

Source1 maintenance of the existing Devkit selected reset and its tests/docs.
Runtime owns actual operations, retry/reconciliation and receiving. This does
not accept the original operation as effect-free or dispose a surplus lane.

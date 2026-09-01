# Malformed GUI rollout raw cold quarantine

Framework: [Tradeoff Decision Framework](../tradeoff_decision_framework.md)

Decision type: governance/tooling failure at the disposable-execution custody
boundary.

Value order: correctness and durable business custody, security and
recoverability, operability and fleet capacity, then implementation cost.

## problem

The source-derived Codex GUI history snapshot rejects an entire selected-slot
or whole-prefix reset when a SQLite-referenced rollout contains one malformed
JSONL record. The copied staging generation is then removed. A byte-level defect
in an otherwise identifiable historical rollout can therefore hold disposable
compute indefinitely even when its human-authored objective remains intact in
the captured Codex SQLite state.

This is a shared source defect, not permission to edit a station's rollout or
to weaken history custody. A repair must retain the malformed evidence and the
objective needed to reconstruct work while ensuring no consumer mistakes the
rollout for resumable history.

## options

1. Keep rejecting every reset until each source rollout is manually repaired.
2. Delete, truncate, sanitize, or silently skip malformed records during
   capture.
3. Complete the normal atomic snapshot with a typed raw cold-quarantine entry
   only when the exact rollout bytes are retained, SQLite passes integrity
   checks, a valid `session_meta` matches the SQLite thread identity, and the
   thread's durable objective is hash-bound to its captured SQLite row.

## selection_rationale

Option 3 preserves both sides of the boundary. The original bytes remain in the
normal private, source-derived payload with their file digest and bundle digest;
the manifest classifies the referenced rollout as `raw-cold-quarantine`, records
the first malformed line and count, and explicitly marks it ineligible for
resume. Objective content remains in the byte-exact SQLite family rather than
being copied into logs or manifest text, while metadata binds its table, column,
thread identity, byte count, and digest.

Option 1 converts a historical serialization defect into indefinite fleet
capacity loss and invites station-local workarounds. Option 2 destroys evidence
or creates an unreviewed synthetic history. Neither satisfies durable custody.

This is a normal source repair to the canonical reset boundary. It is not a
runtime bypass, station repair, restore path, or authority to salvage an
unaccepted product candidate.

## safety_checks

- The full source file set is copied byte-for-byte and re-hashed before and
  after classification; the generation becomes visible only after payload and
  manifest sync plus atomic rename.
- All captured `state_*.sqlite` and `goals_*.sqlite` databases still pass
  read-only `PRAGMA quick_check` before any reset may continue.
- Wrong, missing, empty, or escaping rollout paths; missing or conflicting
  session identity; corrupt SQLite; and missing durable objective custody still
  fail closed and leave no completed generation.
- Only malformed JSONL records in an otherwise identity-proven referenced
  rollout enter quarantine. Valid rollouts retain the existing resumable count.
- Quarantine metadata contains no objective plaintext. It binds the captured
  `threads.title` value by path, row identity, byte count, and SHA-256.
- Host output reports the quarantine count without emitting conversation
  content. There is no automatic restore or import path.
- Tests cover byte-exact NUL-bearing files in both `sessions/` and
  `archived_sessions/`, identity and objective fail-closed cases, hash binding,
  privacy, atomic cleanup, and the existing corruption and path constraints.

## rollback_plan

Revert this source commit before deployment. Existing v1 cold generations and
station homes remain untouched because the change acts only when a future
source-derived reset captures a new v2 generation. A completed v2 quarantine
generation is immutable evidence and must not be deleted by rollback.

## decision_scope

This decision covers Devkit's Codex GUI history snapshot schema, validation,
manifest, operator receipt, tests, and reset-boundary policy. It is generic to
all native `dev-all` lanes but does not authorize deployment, repinning,
station-local mutation, history import, product-candidate salvage, or changes to
healthy lanes.

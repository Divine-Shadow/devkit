# Native broker recovery after WSL restart

Framework: `docs/decision-framework/tradeoff_decision_framework.md`

## problem

Selected-slot reconstruction is required to start or reuse the managed native
broker before it checks Product readiness. After a WSL restart, the persisted
broker PID can be reused by an unrelated process. The broker inspection path
treated `kill(pid, 0)` alone as proof that the broker was running, skipped
startup, and then failed Product readiness against a dead Unix socket.

## options

1. Add a controller operation that starts the broker separately before every
   reconstruction.
2. Ignore the failed readiness check and let the app-server start without the
   broker.
3. Repair the existing Devkit broker inspection/start contract so a running
   broker requires both source-recorded process identity and a live socket,
   while startup replaces only an exact managed process whose socket is dead.

## selection_rationale

Option 3 preserves the existing single lifecycle path and makes its readiness
claim truthful. It ranks correctness first by refusing PID-only identity,
verifiability second through focused process/socket tests, and operational
safety third by never signaling an unrelated PID. It avoids a new controller
effect and keeps reconstruction generic across every station.

## safety_checks

- A live unrelated process that reuses `broker.pid` is classified as stale and
  is not signaled.
- Process custody requires the exact recorded executable, PID, and
  `BROKER_LISTEN` socket in the live process environment; a same-binary process
  serving a different socket is not signaled.
- An exact source-recorded broker process without a listening socket is
  classified as stale, stopped, and replaced before readiness.
- A replacement is accepted only after its Unix socket accepts connections.
- Existing immutable-binary validation and stale-socket refusal remain in
  force.
- Focused broker tests and the complete Devkit repository gate must pass.

## rollback_plan

Revert this change if a healthy managed broker is replaced, an unrelated
process is signaled, or selected-slot reconstruction no longer starts a broker
from an absent/stale state. Retain fail-closed readiness rather than adding a
controller-side broker-start fallback.

## decision_scope

Devkit native runtime broker inspection and restart only. No Product,
Management operation, persistence, credential, provider, or public lifecycle
surface changes.

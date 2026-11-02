# PostgreSQL lease predicate diagnostics

## Purpose and current state
The ordinary Auth fixture started PostgreSQL but its endpoint helper returned only
`postgres lease rejected: 409 Conflict`, so zero provider assertions ran. The
broker maps every failure of `postgresHostPort` to this response and the helper
discards its response body. Supply a bounded, non-secret failed-predicate code
through that existing error path. This is diagnostic source repair, not proof that
PostgreSQL/Auth works or a choice of the still-unknown underlying correction.

The original Auth11d record remains owned by Runtime/Environment. Runtime's normal
managed read at 2026-11-02T03:51:36Z returned zero rows for that exact container,
through the admitted native broker. Historical63a UNKNOWN remains separate; old94
absence is already earned. Do not create/retry a business fixture here.

## Progress
- [x] Read the actual zero-assertion409, owning11d absence receipt, immutable broker
  source, current managed API version, and ordinary context bindings.
- [x] Opened an isolated clean source worktree at fetched current master cf2cb1a.
- [x] Owning Nix broker checkPhase reproduced the lost-predicate failure: seven
  inspection cases and one recognized-detail case failed with bare409 (RED).
- [x] Preserve all refusals while returning only finite validated codes.
- [x] Canonical changed broker package build/checkPhase passed0 at
  2026-11-02T04:04:45Z (GREEN). Complete repository qualification remains owed.
- [ ] Obtain independent review and normal enforcing publication.
- [ ] Existing Runtime/Environment receive the qualified artifact and use one
  discriminating ordinary endpoint acquisition, then repair its real cause and
  execute actual provider/migration/rollback assertions. These remain owed.

## Files and verification
Change brokers/postgres-broker/endpoint.go and the focused
endpoint_diagnostic_test.go using existing main_test.go HTTP/proxy helpers, plus
this plan. Use the admitted dev-all-runtime-tools for Go tests and the declared
postgres-broker Nix package. Exercise the real HTTP broker, agent Unix proxy and
sandboxLeaseWithin error path, including status/decode/running/port predicates,
cross-sandbox/stale refusal, no grant on failure, malformed/oversized/injected
response suppression, and existing timeout/resource tests. Full make ci-cheap and
native integrated gates/publication are distinct requirements, not inferred from
focused passes. Keep output evidence outside the source tree.

## Surprises & discoveries
A direct Go test first stopped before tests at a forbidden dependency download.
The repository-declared Nix package supplied its existing immutable Go dependency
recipe; RED/GREEN results above are actual checkPhase results, not that fetch.

The earlier host-side disposition read lost the native DOCKER_HOST context and
used a general fallback; that failure was not resource absence. Runtime's later
correct managed read supplies current absence without another read. Source1's
own normal version read is Docker29.3/API1.54/min1.40 with client1.52; no API-version
or dual-stack binding cause is selected without actual predicate evidence.

Canonical decision: [bounded PostgreSQL diagnostics](../../docs/decision-framework/governance_decisions/20261102_postgres_lease_predicate_diagnostics.md).

## Decision log
### problem
A real409 hides which upstream inspection/running/port check failed, preventing a
causal correction while the security rejection itself is correct.

### options
Return arbitrary upstream errors; relax the port/lease checks; add another service;
or expose a small allowlisted diagnostic over the existing409/helper error path.

### selection_rationale
Use the existing path with finite codes. This directly fixes the observed loss of
necessary evidence without guessing the underlying cause or introducing a new
endpoint, observer, identity, credential, business author or execution route.

### safety_checks
Keep exact container/run/credential custody, opaque leases, running-state and
exactly-one5432-binding checks, host-port validation, deadlines, deletion/stale
stream revocation, loopback-only tunneling and stdout-ready protocol unchanged.
Do not return arbitrary upstream bodies, errors, paths, credentials or lease
values. Bound reads and validate diagnostics again in the helper. Preserve the
user GUI hold and independent C69/Writer/receiving work.

### rollback_plan
Retain the accepted installed closure. Reject an unqualified source candidate;
never patch a live broker or substitute an unqualified binary for acceptance.
Any later activation uses the canonical owner/path and a true effect receipt.

### decision_scope
Source1 existing Devkit maintenance duty for the observed Auth endpoint supplying
failure. Source implementation and qualification only; Runtime/Environment keep
business/resource execution and disposition. No other task is held on this work.

## Outcomes & retrospective
Unfinished. The exact underlying409 predicate, canonical source publication,
installed receiving and actual PostgreSQL assertions remain unearned.

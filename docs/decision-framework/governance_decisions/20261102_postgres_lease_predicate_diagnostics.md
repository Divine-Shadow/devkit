# Bounded PostgreSQL lease predicate diagnostics

## problem
The real Auth acquisition started PostgreSQL and then failed before any provider
assertion with only `postgres lease rejected: 409 Conflict`. The broker discarded
the failed inspection/running/port predicate and the helper discarded the409 body.
Current absence of that fixture is separately earned; it does not identify its
historical predicate.

## options
Keep the generic failure; return arbitrary upstream error text; relax the lease or
port guard; or carry finite validated codes through the existing failure response.

## selection_rationale
Choose finite codes. Correctness comes first: the grant, custody and port laws are
unchanged. The real broker/proxy/helper regression mechanically reproduces the
lost evidence and checks the correction. Bounded parsing and suppression of
upstream/private text preserve operational safety. The result reports precisely
the predicate that failed instead of implying an unsupported functional cause.

## safety_checks
Only authenticated owned-container lease requests reach inspection. A409 remains
a refusal with no returned lease. Preserve run/credential matching, exact ID,
running state, exactly one5432 binding, numeric port checks, loopback tunneling,
timeouts, deletion revocation and stdout-ready protocol. The server selects an
allowlisted code with only bounded HTTP-status or binding-count integers. The
helper reads at most257 bytes, accepts at most256, validates the code again and
drops unknown, malformed, injected and oversized details. Arbitrary cause text,
response bodies, credentials and leases never become diagnostics.

The owning Nix broker derivation's checkPhase failed with the generic baseline
on seven predicate cases and one accepted-detail case, then passed on the changed
source at 2026-11-02T04:04:45Z. Existing module guard/lifetime tests were included.
Complete repository CI, independent source review, normal canonical publication,
installed receiving and actual PostgreSQL assertions are separate and pending.
See the living [ExecPlan](../../../.agent/execplans/postgres-lease-predicate-diagnostics-20261102.md).

## rollback_plan
Reject failed/unreviewed source before publication. Keep the installed accepted
closure until the owning canonical deployment/consumer operation succeeds.
Never use the development binary as an accepted runtime or patch a live broker.
A new reported predicate drives the next actual source correction; it does not
authorize relaxing the refusal or replaying an unchanged Auth suite.

## decision_scope
Source1's existing Devkit maintenance role supplies this necessary diagnostic
step. Runtime/Environment retain fixture/resource/business ownership and receive
the eventually qualified package through the declared route. No new endpoint,
service, author, credential, identity grant or alternate deployment path. Existing
C69, ordinary receiving and GUI hold retain their own actual boundaries.

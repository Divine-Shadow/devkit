# Construct one native PostgreSQL binding before lease admission

## problem
A fresh owning managed exercise retained running resource1e535276 through endpoint
acquisition. One requested5432 binding with empty HostIp/HostPort became IPv4 and
IPv6 bindings on32769. The strict endpoint refused409. Exact release/absence0 and
unchanged19 Auth inputs/no assertions are supplied observations; this does not
establish the historical11d/63a predicate.

## options
Relax the exactly-one guard; change daemon/network configuration; require a new
Product-specific create path; pin an unspecified managed request to wildcardIPv4;
or construct a single loopbackIPv4 binding within the existing broker path after
all current create authorization.

## selection_rationale
Choose internal loopbackIPv4 construction. It matches the broker's existing local
IPv4 tunnel destination, keeps one binding, and narrows the already authorized
unspecified mapping. It needs no broader Docker allowlist, caller-selected address,
new service or Product fixture variant. Docker's primary documentation explains
unspecified dual-family mapping and explicit loopback publishing:
https://docs.docker.com/engine/network/port-publishing/ .
Preserve explicit caller addresses; this correction supplies the observed default.
The owning consumer must still prove actual endpoint usability after receiving.

## safety_checks
Only a single existing5432 binding with unspecified HostIp and both launcher proxy
capability headers is transformed, after image/port/bind/capability/network/security
checks. Preserve all other JSON values and current host-client semantics. Direct
caller loopback, multiple requested bindings and all other invalid creates retain
their refusals. Endpoint running/exact-ID/exactly-one/port validation, opaque run
capability, delete revocation, tunnel destination and lease lifetime stay intact.
Meaningful normal broker/proxy/create/start/lease regression must fail before the
change and pass afterward with the whole module; fullCI, independent review,
normal publication, immutable supply and fresh consumer effects remain separate.

## rollback_plan
Reject failing/unreviewed source before normal publication. Keep the accepted
installed closure until the owning canonical adoption succeeds. Do not copy a
binary into a live broker or retry an unchanged Auth suite. If fresh receiving
finds another predicate, preserve that actual bounded result and repair its owner.

## decision_scope
Source1 supplies this narrow existing Go broker correction in Devkit. Root owns
composition; Runtime owns declared adoption/effects; Environment owns retained
fixture and ordinary consumer assertions. No new approval, enrollment, credential,
alternate route or lease bypass is introduced. See the living
[ExecPlan](../../../.agent/execplans/native-postgres-single-binding-20261102.md).

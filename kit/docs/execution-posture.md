# Devkit execution posture

Authorized agents have broad control inside their accepted disposable sandbox,
including local privilege supplied by that environment. Preserve containment of
worktree-lineage parents, sibling and host noninterference, the existing network
allowlist and guarded access. Preserve protected conversation history and goals,
accepted source/results, secrets, customer data and active external transactions.
Applicable correctness, review, CI and publication obligations remain.

Management controller configuration v9 describes existing routes and mounts.
Entry no longer reads or mounts a broker-issued identity, checks socket generation
against that identity, or warms an issuer. The host-operation socket is optional
for ordinary sandbox work. When present, its exact path, owner, protected mode and
read-only mount remain checked. Host effects still use the existing guarded adapter
and target restrictions. Trace/operation IDs serve correlation and idempotency,
not permission issuance. Instruction authentication is a separate institution.

Older identity-admission plans, cold-issuer repair guidance and tests are superseded
at this boundary. Preserve the working Local4/Peano path and Local3/MCP. Candidate
qualification does not require changing them or converging the whole fleet.

Run the ordinary package-owned entry (`devctl -p dev-workspace exec ...`) with the
candidate's composed configuration and runtime. Unit/integration tests must show
useful entry without an identity/issuer and preservation of affected protections.
CLI receiving does not qualify Desktop/app-server behavior by itself.

[Operator runbook](native-operator-runbook.md) · [Agent guidance](../../AGENTS.md)

# Devkit execution posture

Authorized agents have broad control inside their accepted disposable sandbox.
Preserve worktree, sibling and host containment, deny-by-default egress, guarded
credential/provider access, protected conversation history and goals, accepted
source/results, secrets, customer data and active external transactions.
Applicable correctness, independent review, CI and publication obligations remain.

Ordinary native execution consumes the locked Devkit package and explicit
workspace geometry. It does not depend on Management or Fleet repositories,
controller profiles, identity issuers, callback services or their state.
Product supplies its own accepted artifact and CI interfaces. Devkit does not
choose Product revisions or create a competing Product runtime identity.

The existing bubblewrap entry retains isolated writable worktrees/homes,
read-only runtime tools, excluded Windows mounts, proxy-only network access
and guarded optional container access. Build-only workloads may explicitly
select --no-containers. Container workloads retain the existing broker image
policy and lifecycle safeguards. Operation IDs track results and idempotency;
they are not new permission grants. Instruction authentication stays separate.

[Standalone workflow](standalone-native.md) -
[Operator runbook](native-operator-runbook.md) - [Agent guidance](../../AGENTS.md)

## Source-selected controller consumer projection

The existing Management controller profile has a fixed native projection for a
source-declared local Management GUI lane. Selecting its marker alone supplies
no capability: the native plan also requires the exact fixed immutable v9
configuration, strict GUI record, lane-local Management/WSL source identities
and existing owned operation socket. It projects only that read-only manifest,
that socket and the lane-local WSL source. It projects no credential directory,
manager bus or caller-selected mount, and revalidates the captured projection
before preparation and bubblewrap construction. Ordinary Product/native plans
keep their existing behavior and do not depend on this projection.

The v9 document is source configuration, not an online identity grant. The
unchanged Fleet broker owns full document and effect validation. These native
checks neither materialize a protected SSH credential nor admit a live role or
remote provider. Replacement-controller goal custody remains distinct from an
ordinary declared additional GUI consumer.

The owning plan/launch tests use explicitly hypothetical projection premises,
real temporary Git source identities and a local Unix socket. They check source
selection, exact plan arguments, drift/refusal ordering and retained behavior;
they do not start a GUI, namespace, operation broker or remote connection.

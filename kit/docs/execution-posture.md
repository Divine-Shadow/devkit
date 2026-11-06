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

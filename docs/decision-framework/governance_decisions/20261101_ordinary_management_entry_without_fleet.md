# Ordinary Management execution without implicit Fleet admission

Framework: [Management Tradeoff Decision Framework](https://github.com/Divine-Shadow/dev/blob/3560389c9eccbcdf45eda5a6fedbb655b0866fca/docs/decision-framework/tradeoff_decision_framework.md).

## problem

The operator's original 1 November purge commission assigns the environment to
Devkit, remote published Devkit launch to Fleet, and coordination to Management.
Even without the explicitly requested legacy controller profile, an ordinary
Management plan currently requires three Fleet inventory/configuration files
and automatically imports a legacy exec socket if one happens to exist. That
socket also becomes required entry state. TI4 inherits the implicit handle too.
These dependencies supply no necessary guarantee for ordinary contained work.

## options

1. Keep the implicit dependencies and repair them before every ordinary use.
2. Replace them with successful placeholder files or a renamed broker.
3. Stop implicit projection; keep actual host-operation checks with their owner.

## selection_rationale

Select option3 in the existing workspace-egress plan. A normal Management or TI4
entry receives its worktree, durable home, required tools and scoped network
environment. It does not acquire Fleet inventories or a privileged socket just
because of a repository name or socket presence. Explicitly selected legacy host
profiles retain their current guards while their individual responsibilities are
disposed of. They are not selected by the changed ordinary WSL GUI composition.

Correctness preserves containment, protected credentials and host access control.
Verifiability uses real ordinary plans and bubblewrap commands with Fleet state
absent and with an incidental legacy listener present. Operational safety removes
an ambient capability rather than widening access. Epistemic integrity keeps this
source result distinct from publication, GUI receiving and whole-purge acceptance.

## safety_checks

Run affected plan/launch regressions and the full normal Devkit source CI. Check
that the sandbox still excludes Windows mounts, siblings and raw credentials,
uses the declared egress policy, and supplies its ordinary workspace/home/tools.
The legacy explicit-profile guard tests remain. The WSL owner removes the profile
and compulsory broker/ledger selection from ordinary GUI services and qualifies
the resulting composition. CI-controlled publication remains with its existing
institution; this source operation does not deploy or replace any live consumer.

## rollback_plan

If an actual supported ordinary operation loses a necessary capability, name the
specific supplier and integrate its scoped interface. Do not restore a universal
admission graph to repair one operation. Revert this bounded source delta through
normal gates if required, preserving durable data and uncertain external effects.

## decision_scope

Source1 owns this Devkit source increment and WSL composition. Main remains the
sole Product author. Runtime and Environment retain actual effect/receiving work.
Persistence/reset, the narrow remote launcher, downstream Product adapters and
effective Management guidance still require their own evidenced dispositions.
This increment is not a whole-purge completion claim.

# Retained consumer source-preparation transport

## problem

Runtime's executed Local2 observation found that the accepted live launcher uses
an immutable Devctl helper different from the helper exposed by the mounted
current-system profile. Resolving that profile is therefore not a binding to
this consumer. The full transport recipe must remain exact. Socket equality
and uniqueness of the observed helper mismatch remain unestablished.

## options

Keep current-system selection and require reconstruction; accept an immutable
helper parsed from the caller's transport; or compose the source preparer with
the actual selected consumer Devctl derivation and target. The first confuses
system selection with an existing execution. The second has no owning selection
and would weaken the transport guard.

## selection_rationale

Use the existing package-composition seam to bind one selected consumer Devctl
derivation and target at build time. An unbound preparer serves only its own
executable-derived runtime. No runtime path, environment override, allowlist,
trust edit or launcher mutation chooses the helper. Resolve the consumer through
the existing immutable GUI projection, require the bound target to match, and
reconstruct the complete unchanged transport using that helper. This preserves
correctness, permits direct consequence tests, isolates failure, and records
source selection truthfully before optimizing delivery speed.

The authoritative composition calls `base.withCurrentSourceConsumer` with an
attribute set containing the actual selected `devctl` derivation and `targetId`.
The input must be a Devctl derivation, not a string helper path. Both this factory
and `withCurrentSourceRegistration` retain the other binding. Apply factories
before existing station/consumer `overrideAttrs` so those flags are retained.
No inspected current/frozen selector is claimed to reproduce executed75nf;
Runtime still owns that source/construction join and actual binding.

## safety_checks

Qualify the real retained-helper-versus-current-system case through the owning
source-selected derivation. Test wrong helper, target, socket root, command
suffix and missing/noncanonical helper refusal. Run independent review and
complete normal Devkit CI before publication. Source tests do not prove actual
socket equality, source preparation, lifecycle disposition or receiving success.

## rollback_plan

Publish a reviewed source revert through normal CI and composition if the
selected consumer binding cannot be proven or admits a replacement transport.
Preserve the live consumer and its state; Runtime owns reconstruction and
deployment through its existing operation and effect-disposition rules.

## decision_scope

Continue Source1's existing Devkit source-preparation supplier responsibility.
Do not change the active WSL publication subject, Source29 Product lookup,
Source17 SDK leaves, Root9 Runtime.scala, credentials, trust or live lifecycle.
This is normal source repair, not an alternate deployment or admission route.
See the [Tradeoff Decision Framework](../tradeoff_decision_framework.md).

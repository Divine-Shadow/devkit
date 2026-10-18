# Upstream RC8 diagnostic supplier

This optional generic toolchain output builds upstream SBT `actions_3` from
exact source with bounded test-event diagnostics. It has no Product source input
or runtime selection. See the [decision](../../docs/decision-framework/governance_decisions/20261018_sbt_rc8_diagnostic_source_supplier.md).

`source.json` identifies upstream RC8, its NAR, the preserved diagnostic source
candidate and the committed four-file patch hash. Nix asserts the patch hash.
Locked Nixpkgs supplies JDK21 and the generic launcher. That launcher loads the
source-declared SBT 1.11.6 bootstrap; it is not the resulting RC8 provider.

## Qualification sequence

1. Under the owning controller's allocated heavy slot, build
   `.#sbt-rc8-diagnostic-deps-discovery`. This uses a deliberate fake output hash
   and must terminate with the real recursive hash before any acceptance claim.
   Preserve the full actual process, output and mismatch receipt. An earlier
   repository/network/build failure does not supply a hash.
2. Review the actual closure and commit its hash into `dependency-hashes.json`.
   A null hash deliberately prevents the source package from evaluating.
3. Build `.#sbt-rc8-diagnostic-actions`. Its ordinary non-fixed-output sandbox
   copies the qualified dependency closure to new writable temporary caches,
   executes touched-module format checks and actual upstream tests, and uses
   normal `actionsProj / publishM2` into a fresh HOME. A failed format or compile
   requires a source correction; no skip or generated acceptance follows.
4. Preserve the resulting immutable Maven repository, actual log, JAR digest,
   derivation/closure identity, and full repository gate/review/publication.
   Only the existing Product writer may select this upstream supplier and perform
   normal-provider synthetic113 failure/allpass/order/decode/foreign/empty
   qualification. These source tests alone cannot establish that qualification.

The first dependency materialization may discover missing resolver/compiler or
formatter dependencies. Correct its declared source closure; never seed a live
consumer cache or switch transports after a denial. Only repo1.maven.org is
configured, matching the existing declared generic repository. The earlier
repo.maven.apache.org CONNECT403 is retained and not retried.

## Resource envelope

The producer requests one derivation at a time, two active JVM processors,
two SBT task CPUs, and a 2 GiB heap. Reserve 6 GiB total RAM including JVM native
memory and possible worker/compiler processes, and at least 24 GiB free builder
scratch plus 12 GiB store capacity. Initial download/build time is unknown;
budget 30 minutes for dependency discovery, separately from source qualification.
These are allocation estimates, not measured build usage or host reservations.
Do not infer daemon builder scratch capacity from the controller's `/tmp`.

No build has yet run. No package is published or selected. The diagnostics
preserve existing RC8 aggregation/completion behavior, including any reproduced
failure; this is an evidence supplier, not a claimed causality fix.

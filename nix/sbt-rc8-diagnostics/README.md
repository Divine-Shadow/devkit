# Upstream RC8 diagnostic supplier

This optional generic toolchain output builds upstream SBT `actions_3` from
exact source with bounded test-event diagnostics. It has no Product source input
or runtime selection. See the [initial decision](../../docs/decision-framework/governance_decisions/20261018_sbt_rc8_diagnostic_source_supplier.md)
and [actual discovery follow-up](../../docs/decision-framework/governance_decisions/20261018_sbt_rc8_discovery_followup_source_binding.md).

`source.json` identifies upstream RC8, its NAR, the preserved diagnostic source
candidate, committed four-code-leaf patch hash and separate reproducible version
generator patch hash. Nix asserts both patch hashes.
Locked Nixpkgs supplies JDK21 and the generic launcher. That launcher loads the
source-declared SBT 1.11.6 bootstrap; it is not the resulting RC8 provider.

## Qualification sequence

1. The initial allocated80336 command built
   `.#sbt-rc8-diagnostic-deps-discovery`. This uses a deliberate fake output hash
   and terminated naturally with the actual recursive hash recorded in
   `dependency-hashes.json`. Full output and terminal receipt are retained in the
   public actual-discovery courier. This is hash discovery, not qualification.
2. Actual inspection found1961 retained files/zero symlinks: only the declared
   Coursier, boot, global, Ivy and XDG caches. The retained compiler bridge JARs
   have fixed1980 ZIP entry timestamps. No speculative artifact rewriting is
   performed. The normalizer preflights the entire quiescent build-local cache
   before deleting markers, and tests preservation across root/ancestor links.
3. Build `.#sbt-rc8-diagnostic-actions`. Its ordinary non-fixed-output sandbox
   copies the exact hash-bound dependency closure to new writable temporary
   caches. It records patched sources, explicitly runs normal upstream formatters,
   records the resulting effective sources, retains format checks and actual
   upstream tests, and uses normal `actionsProj / publishM2` into a fresh HOME.
   The effective source manifest must still match after tests/publication.
   A failed format, compile or test requires a source correction; no skip follows.
4. Preserve the resulting immutable Maven repository, actual logs, JAR digest,
   derivation/closure identity, and full repository gate/review/publication.
   Only the existing Product writer may select this upstream supplier and perform
   normal-provider synthetic113 failure/allpass/order/decode/foreign/empty
   qualification. These source tests alone cannot establish that qualification.

The first FOD's Test/dependencyClasspath task actually compiled transitive upstream
modules and17 main-actions Scala files, then its formatters changed temporary
sources. Those target/products and formatted source files were not exported as
dependencies. The final package always unpacks fresh source, with no module target
products, and binds its own deterministic formatting transform before compilation.
No test/fork/parallelExecution override is used. The latch ordering unit test uses
the same dispatch body with local listeners; it neither proves TCP delivery nor
pollutes the existing worker tests' global listeners.

SOURCE_DATE_EPOCH is honored at the owning version-resource generator; the normal
SBT package timestamp is also fixed. Stable evidence includes exact patched and
effective public sources, hashes, generator inputs and resource digests. Complete
raw Nix -L stdout/stderr contains timings/paths and stays in the owning Source
runner/public courier, outside the functional artifact output. One observed
dependency hash and static review do not prove bit reproducibility of a rebuilt
dependency closure or final package; retain that limit in the qualification report.

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
use the supported1200-second build timeout and300-second silence bound for the
next source qualification command. Initial discovery took about97seconds;
qualification wall-time and peak resources are not yet measured.
These are allocation estimates, not measured build usage or host reservations.
Do not infer daemon builder scratch capacity from the controller's `/tmp`.

Only the initial discovery has run. No package is published or selected. The diagnostics
preserve existing RC8 aggregation/completion behavior, including any reproduced
failure; this is an evidence supplier, not a claimed causality fix.

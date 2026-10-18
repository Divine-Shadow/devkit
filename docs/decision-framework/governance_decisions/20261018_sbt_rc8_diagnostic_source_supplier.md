# SBT RC8 diagnostic source supplier

Historical initial design. The actual80336 discovery contradicted its download-only
description: Test/dependencyClasspath compiled upstream application modules and
formatters changed temporary files. The [follow-up decision](20261018_sbt_rc8_discovery_followup_source_binding.md)
corrects the current producer, source binding, timestamp generation and cache
preflight. The original7fdb candidate and actual discovery evidence stay preserved.

## problem

The accepted Product provider uses upstream SBT 2.0.0-RC8. Its normal immutable
dependency supplier downloads released artifacts and does not compile SBT source.
The bounded upstream diagnostic candidate must be compiled and tested before the
existing Product writer can select it. An instrumented source file or an edited
live cache is not an immutable supplier. This is SourceRepairAuthority source
construction; heavy execution still requires the parent controller allocation.

## options

1. Add the source producer to Product: collides with its current writer and
   obscures the independent upstream toolchain boundary.
2. Add it to WSL composition: couples a generic compiler build to its downstream
   composition and risks an input cycle.
3. Add a generic upstream toolchain producer to Devkit, independently of Product.

## selection_rationale

Choose option 3. Devkit already owns generic SBT/JDK tooling. This producer takes
only exact public upstream SBT source, a committed patch, and locked Nixpkgs tools;
it never acquires Product source or chooses a Product revision. Correctness comes
from preserving RC8 semantics and coordinates; verifiability from isolated fixed
output dependencies followed by an offline source build, source unit tests and
format checks; safety from build-local homes and immutable outputs. The manifest
separates source qualification from normal-provider consumer qualification. This
is slower than replacing a cached JAR but preserves auditable producer identity.
See the Management [tradeoff framework](https://github.com/Divine-Shadow/shadow-throne-management/blob/main/docs/decision-framework/tradeoff_decision_framework.md).

## safety_checks

- Exact upstream 7e59872d310a6a1d2383038a5ee0f3027df8d585 (RC8) is NAR-pinned.
- The patch contains only the four upstream code/test leaves from source candidate
  7fdb55c6e017cd57cb35169a0713ee23934163d9. Its original six-leaf bundle is retained.
- Bootstrap uses locked Nixpkgs' declared SBT launcher and source-pinned SBT 1.11.6,
  JDK21, isolated HOME and caches, bounded CPU and heap. No live cache is read.
- A prior direct repo.maven.apache.org bootstrap fetch failed CONNECT403. No retry
  or alternate transport follows. Declared repository endpoints are repo1.maven.org
  and repo.scala-sbt.org; retain actual refusals during normal dependency closure.
- The first dependency derivation deliberately uses a discovery hash. It cannot
  qualify an artifact. Record its actual hash and commit that value before the
  offline package/check can evaluate. Never manufacture a dependency hash.
- Do not set BUILD_VERSION or sbt.build.version: upstream also applies that value
  to IO and Zinc dependencies. A normal SBT ThisBuild/version setting selects RC8
  output coordinates while preserving upstream dependency versions.
- Only actions_3 is published into a build-local Maven repository. Unchanged RC8
  dependencies stay external. Nothing is installed, selected, adopted or deployed.
- The final package must pass the actual upstream diagnostic and WorkerExchange
  tests plus touched-file formatting checks. This does not establish normal
  provider synthetic113 failure/allpass/order/decode/foreign/empty qualification.
- Full Devkit gates and independent review remain owed before publication. Hash
  discovery, successful evaluation and a source commit are not acceptance.

## rollback_plan

Until qualification/publication, discard only this isolated candidate; accepted
WSL ce36/794492 and all Product software pins remain unchanged. A failed upstream
build requires a bounded source correction and new hash where dependencies change.
A published supplier is optional and unselected until the existing Product writer
accepts it. Remove its input selection there through that writer if it fails.

## decision_scope

Only nix/sbt-rc8-diagnostics/**, the optional packages/checks in flake.nix, and this
note. No Product/WSL source, runtime wrapper, fork mode, test selection workaround,
credentials, security, new listener, live cache/JAR replacement or provider mutation.
No heavy work before allocation. This is an unfinished source candidate until the
actual declared build, tests, review and publication receipts exist.

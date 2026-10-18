# RC8 supplier follow-up after actual dependency discovery

## problem

The allocated80336 discovery ran naturally23:49:46–23:51:24UTC on2026-10-18,
PID804617/session98373, exit1 with actual recursive dependency hash
`sha256-HZK8g8SSDoSmXDlGDrzwg4oUc4bmVWiimFsm8NmNEKg=`. It compiled transitive
upstream modules and17 main-actions sources through Test/dependencyClasspath,
then formatted temporary sources. No tests ran. The original note claiming no
upstream application compile was incorrect. The temporary formatted files are
not the committed patch. Independent review also found a global-listener fixture
collision, a normalizer symlink preflight gap, timestamped generated resources,
and volatile raw build logs in the proposed functional output.

## options

1. Treat the discovery as qualified: rejected; no tests or final artifact exists.
2. Remove formatting/tests, strip arbitrary cache/JAR bytes, serialize consumer
   tests or edit live caches: rejected; changes the question or destroys evidence.
3. Keep normal source tooling; explicitly bind its formatting transformation and
   generated metadata, isolate the unit fixture, and preserve full raw logs in
   the public courier independently of the stable functional artifact.

## selection_rationale

Choose3. Correctness preserves normal RC8 protocol/aggregation/completion and
valid-empty behavior. Verifiability records exact patched and formatted sources,
checks those bytes unchanged after tests/publication, and retains format checks.
Safety keeps all mutation in the source/build roots, preflights cache symlinks
before any deletion, and introduces no consumer scheduling or runtime override.
Epistemic integrity names the FOD's actual compilation and separates candidate
hash identity from reproducibility, test success and normal-provider acceptance.
The [Management tradeoff framework](https://github.com/Divine-Shadow/shadow-throne-management/blob/main/docs/decision-framework/tradeoff_decision_framework.md)
orders those properties before throughput.

## safety_checks

- Record the actual observed hash without inventing a reproducibility result.
  The retained cache has1961files/zero symlinks. Both retained compiler-bridge
  JARs have133 entries at1980-01-01; no demonstrated timestamp defect justifies
  rewriting those artifacts. Preserve artifact bytes.
- Keep the first FOD's task sequence and dependency versions. Its isolated
  target/classes and timestamped generated resources are not copied to output;
  only the declared five cache roots are. This is now explicitly documented.
- Final source build runs normal upstream scalafmt/javafmt BEFORE compilation,
  then checks formatting. Record original patch inputs and exact transformed
  four-code-leaf hashes/content. Compile/test/publish cannot silently reformat
  them afterward. The original unformatted patch is not relabeled formatted.
- Honor SOURCE_DATE_EPOCH at the owning Utils version-resource generator and
  set the normal SBT package timestamp; do not modify generated JARs afterward.
- Full actual stdout/stderr stays in the Source runner and public courier. It
  includes wall-clock timings and cannot be placed in a reproducible output.
- Extract only the existing dispatch body into a package-private helper. The
  synchronized production entry passes its original mutable listener collection
  directly. Latch tests use a local collection and retain explicit ordering
  negatives, without global state, lock or scheduling changes.
- Normalizer tests must prove outside-cache sentinel preservation for symlinked
  root/ancestor/global entries, byte preservation, and preflight-before-mutation.
- Independent static reviews precede the next bounded normal build/test command.
  Actual compiled test/API/formatting and offline dependency sufficiency remain
  unproven until that allocated command. Source tests do not establish the later
  Product normal-provider synthetic113 contract.

## rollback_plan

Preserve80336, the original7fdb bundle and all actual discovery logs unchanged.
Revert only this follow-up source candidate if the actual next gate rejects it.
Never repair a live consumer into acceptance. Published WSL ce36/794492 and all
accepted Product software pins remain unchanged.

## decision_scope

Source-owned Devkit producer, cache normalizer/tests/docs and the bounded upstream
source patch. One source writer, existing independent reviewers only. No heavy
execution from this source phase; return one exact bounded command for Root
allocation. No Product edit, public upstream push, cache/JAR install, new observer,
credentials/security change, runtime selection, adoption or deployment.

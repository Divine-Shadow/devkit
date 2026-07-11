# Immutable dev-all runtime bundle for Artifact Column V2

This ExecPlan is self-contained and is maintained as implementation and proof proceed. All commands run from the fresh devkit worktree `/workspaces/dev/devkit-worktrees/dev-all-runtime-bundle-artifact-column-v2` unless noted otherwise.

## Purpose and acceptance

Publish a reviewed devkit topic branch based exactly on `3769f658b74b47c2f87f4a9eee3ed55764d80da5` that provides `packages.x86_64-linux.dev-all-runtime-bundle`. The bundle must independently pin the Artifact Column plugin repository to Ouro commit `4eaf59e32d6ebd49c842c8038e7cfc4f825870d7`, plugin version `0.1.0-artifact-column-v2-package-derived-ownership-20260711`, and plugin jar SHA-256 `948d70381978242d5da4288368622e365b1d746546606c183d3cc321f41c00d2`. The existing submit-to-ci authority remains `d15715adeadc8881b08ac7a05f19fec15fd29986`, with identical package output, jar, hash, and runtime policy.

The canonical operator entrypoint remains `kit/scripts/devkit`. No live runtime, runtime home, station configuration, Ouro source, fleet-control source, wsl-nix source, or protected devkit checkout is modified.

## Authority and boundaries

- BayeSartre approved devkit source, tests, Nix proof, exact-diff review, and publication of a new `codex/` topic branch only.
- Worktree: `/workspaces/dev/devkit-worktrees/dev-all-runtime-bundle-artifact-column-v2`.
- Topic branch: `codex/dev-all-runtime-bundle-artifact-column-v2-20260711`.
- Exact base: `3769f658b74b47c2f87f4a9eee3ed55764d80da5`.
- Protected checkout `/workspaces/dev/devkit` and branch `codex/upstream-devkit-nixos-fixes` are read-only and must not be switched, cleaned, rebased, pushed, or modified.
- The implementation adds no fallback, cache authority, compatibility alias, copied jar, station config, or deployment step.
- Read-only agents may inspect and review. A GUI-visible reviewer must review the exact final diff before publication.

## Baseline evidence

- Exact-base worktree HEAD: `3769f658b74b47c2f87f4a9eee3ed55764d80da5`.
- Existing submit source pin: `submitRuntimeVersion = "d15715adeadc8881b08ac7a05f19fec15fd29986"`.
- Exact-base submit package output: `/nix/store/4xxf15fa8ajm60np3d9vnmiinmb53zd2-submit-to-ci-dev`.
- Exact-base submit jar: `/nix/store/4xxf15fa8ajm60np3d9vnmiinmb53zd2-submit-to-ci-dev/share/submit-to-ci/submit-to-ci.jar`.
- Exact-base submit jar SHA-256 and packaged hash-file contents: `f3fd06efc9b92ffbda400fa5c5bbe3cc88bc46743a347e22c5f20d16441f531c`.
- Exact-base submit derivation: `/nix/store/k9jfshsf7pl3zk87szjdzw3jqzxivz05-submit-to-ci-dev.drv`; NAR hash: `sha256-jTSzueqQSSXlSnS3FiBXZQAw1i8yclutuCNAehpYH6Y=`; NAR size: `67291640`.
- Accepted Artifact Column repository output: `/nix/store/ylxxn2lrsg0dn17r1b7h60lppc85vl9q-artifact-column-plugin-repository-0.1.0-artifact-column-v2-package-derived-ownership-20260711`.
- Accepted Artifact Column repository derivation: `/nix/store/h100aw0vaqn9rqi3vbl2q32xxyn8msja-artifact-column-plugin-repository-0.1.0-artifact-column-v2-package-derived-ownership-20260711.drv`; NAR hash: `sha256-WR3ZWtAUJlUy94U/yr46HmuL6wO18LJS/JTbFMzWgQc=`; NAR size: `492822552`.
- The repository metadata and independently hashed physical Ivy jar exactly match source `4eaf59e32d6ebd49c842c8038e7cfc4f825870d7`, short source `4eaf59e`, requested version, canonical Ivy path, and SHA-256 `948d70381978242d5da4288368622e365b1d746546606c183d3cc321f41c00d2`.
- Before-change derivation metadata and after-change equality are recorded during final verification.

## Design

1. Add a distinct `artifactColumnRuntimeVersion` and `artifactColumnRuntimeSourceFlake`. Only `artifact-column-plugin-repository` and its live adoption smoke derive from this source. `submitRuntimeVersion` continues to own only submit-to-ci. Governance, SBT control plane, Codex, nixpkgs, tool versions, and every other pin remain byte-for-byte unchanged.
2. Add a Nix-owned `dev-all-runtime-bundle` derivation. It retains immutable references to the governance jar package, submit-to-ci package, Artifact Column plugin repository, Artifact Column adoption-check output, SBT control-plane runtime package, and Java runtime without copying any jar.
3. The bundle exposes stable `identity.env` and `identity.json` contracts plus one launcher. The launcher validates the bundle and supports explicit identity readback, fingerprint readback, command execution under that identity, and the packaged live plugin smoke. It never accepts an alternate identity path from mutable workspace state.
4. Refactor devctl governance-env preparation to build the bundle from the devkit source root associated with the canonical binary, execute the bundle launcher for NUL-delimited identity, and validate all paths, metadata, and hashes before writing workspace routing/config.
5. Generated workspace governance env is routing-only: repo-config location/hash, workspace topology, schema, control-plane URLs, state, and decision-log paths. The generated Codex MCP command independently embeds the exact resolved Nix-store bundle launcher and fingerprints that exact path. It sources routing first, restores its entrypoint fingerprint, and then delegates through the immutable launcher, which validates and reapplies the complete runtime identity. The routing file contains no bundle pointer, artifact identity, fingerprint, validator, compatibility alias, or `print-dev-env` refresh.

## Verification matrix

- Pin separation: source inspection and tests prove `mkPinnedArtifactColumnPluginRepository` and the adoption smoke use only `artifactColumnRuntimeSourceFlake`, while `mkPinnedSubmitToCiJar` uses only `submitRuntimeSourceFlake`.
- Exact plugin identity: build the target repository; inspect `metadata.env`, Ivy path, jar, and packaged hash; compute jar SHA-256 independently.
- Bundle contract: evaluate and build `.#dev-all-runtime-bundle`; inspect `identity.env`, `identity.json`, artifact links, launcher commands, and closure references.
- Live plugin smoke: build the upstream adoption check through the bundle and run the bundle launcher smoke, verifying accepted source revision, version, repository, and adoption evidence.
- Submit invariance: build `.#pinned-submit-to-ci-jar` after the change and compare output path, derivation, jar path, jar SHA-256, hash file, and launch flags to the exact-base baseline.
- Failure behavior: focused tests cover missing devkit flake, malformed launcher identity, exact physical jar/hash validation, hostile `DEVKIT_ROOT`, hostile inherited identity, and hostile `BASH_ENV`/`ENV`. The immutable launcher uses Dash so shell startup hooks cannot run before validation, then clears both hooks before executing a child command.
- Location independence: invoke the canonical entrypoint and bundle resolution from a devkit path other than `/workspaces/dev/devkit`; assert no generated identity contract contains the protected path or `#dev-all` refresh.
- Go: focused launch/entrypoint/runtime tests, then `go test ./...` from `cli/devctl`.
- Nix: `nix flake show`, `nix flake check`, target repository build, submit build, and bundle build.
- Hygiene and review: `git diff --check`, independent read-only exact-diff review, GUI-visible exact-diff review, clean statuses for the workspace control repo, protected devkit checkout, and task worktree.

## Progress

- [x] Created the fresh exact-base worktree and topic branch.
- [x] Recorded the before-change submit package path and jar SHA-256.
- [x] Record target plugin output metadata and hashes.
- [x] Implement independent source authority and runtime bundle.
- [x] Implement explicit launcher consumption and fail-loud generated env.
- [x] Add focused and conditional built-bundle adversarial tests.
- [x] Complete Go and Nix verification. Focused and full Go tests pass; the real bundle and live adoption smoke pass; flake evaluation/no-build pass. The two full-build check failures reproduce exact-base overlay-policy debt and are outside this diff.
- [x] Complete independent and GUI review, resolve findings, and rerun affected proof. Final independent and split GUI implementation/verification readbacks report no findings.
- [ ] Commit and push only the reviewed topic branch.

## Surprises & discoveries

- The exact-base implementation couples only one source constructor incorrectly: `mkPinnedArtifactColumnPluginRepository` derives from `submitRuntimeSourceFlake`. The runtime shell consumes that constructor rather than referencing `submitRuntimeVersion` directly.
- The accepted target repository was already realizable from the requested Ouro revision and its physical jar hash independently matched the requested SHA-256.
- The existing generated governance env has two identity authorities: prepared static values and a hard-coded `print-dev-env /workspaces/dev/devkit#dev-all` refresh. Merely changing the Nix pin would leave mutable refresh authority in place, so the refresh must be removed and replaced with bundle fingerprint verification.
- Repo-local ExecPlan authority is `.agent/PLANS.md`; the plan was moved to `.agent/execplans/` immediately after discovery.
- Initial independent review found that caller-controlled `DEVKIT_ROOT` still selected bundle authority. Bundle resolution now uses a separate executable-derived `RuntimeAuthorityRoot`; hostile-root path detection, plan rendering, launch selection, and canonical-wrapper tests pass.
- Initial GUI review found that the mutable `.devkit` shell file remained self-certifying, that `ARTIFACT_COLUMN_PLUGIN_REPOSITORY` duplicated the canonical `_PATH` name, and that a missing devkit flake returned empty identity. The shell file is now routing-only, the generated MCP command embeds the immutable launcher outside it, the alias is removed from producer/consumer schemas, and a missing flake is a loud error.
- A shell-script launcher using Bash could consume hostile `BASH_ENV` before its own first instruction. The launcher now has a Dash shebang, validates under a hermetic path, unsets `BASH_ENV` and `ENV`, and only then executes the requested child.
- Final independent review then exercised the selected Ouro forwarder rather than stopping at the launcher boundary. It found that the forwarder deliberately clears jar identity and re-sources `DEVKIT_GOVERNANCE_ENV`, while the launcher had left that variable pointing to routing-only `.devkit`; it also found that the launcher retained its validation-only PATH, hiding the forwarder’s `python3`. The launcher now has a dedicated `governance-forward` operation: after validation it points `DEVKIT_GOVERNANCE_ENV` to the bundle’s immutable `identity.env`, restores the caller runtime PATH, clears shell startup hooks, and invokes the selected forwarder with the bundle’s exact Bash. A Nix check runs the real pinned forwarder/repo-env reload boundary with a controlled downstream status stub and requires provenance showing exact jar path/hash, authoritative identity, matching entrypoint fingerprint, and Python bridge completion.
- Split GUI review found a remaining legacy test/plan fallback from an absent `RuntimeAuthorityRoot` to caller-controlled `Paths.Root`, plus ignored executable canonicalization errors. The plan no longer supplies any fallback, governed preparation rejects an empty authority root, and executable detection now requires a real canonicalizable executable or returns an error. Unit and canonical-wrapper tests cover both separation and failure.
- Follow-up review traced fail-loud behavior through the production caller and config writer: main now handles both executable lookup and canonicalization errors immediately, and governed configuration rejects a missing host home before cleaning or writing any config. The routing regression blacklist now includes both actual and expected MCP fingerprints plus every bundle authority flag.
- GUI verification review also requested stronger executable evidence for submit invariance, physical Ivy identity, routing-only exclusions, missing-flake failure, and no-copy behavior. The x86 flake now asserts the exact accepted submit package and derivation paths; the bundle build hashes the submit jar against the accepted SHA and validates its packaged hash/runtime policy; the real-bundle Go test independently hashes the physical Ivy jar and metadata; routing tests reject the complete artifact identity vocabulary; missing-flake failure has a direct test; and the Nix build requires runtime artifact symlinks while rejecting any regular copied jar in the bundle output.

## Decision log

- Runtime artifacts are referenced through Nix-store paths and bundle symlinks; jars are never copied into devkit or into a second package payload.
- The mutable `.devkit` governance env is routing-only. Runtime identity is selected by an exact Nix-store launcher path embedded and fingerprinted in generated Codex config, then validated and applied inside that launcher after routing is loaded.
- A broken or absent bundle is terminal and explicit. There is no refresh from the mutable devkit checkout and no legacy identity fallback.

### 2026-07-11 executable-derived bundle authority

`problem`: Independent review found that `DEVKIT_ROOT` still selected the flake from which devctl built the bundle. That variable is a legitimate overlay/config root override, but it cannot be runtime artifact authority because a caller could point it at a foreign flake before bundle validation begins.

`options`: (1) Keep using `DEVKIT_ROOT` and rely on the selected launcher to validate itself; this is rejected because it makes authority self-attested. (2) Derive a distinct runtime-authority root from the canonical `kit/bin/devctl` executable while retaining `DEVKIT_ROOT` for ordinary config/overlay behavior; this preserves relocation and separates authorities. (3) Embed a realized bundle store path in devctl; this creates a build-time cycle and would make source-worktree relocation awkward.

`selection_rationale`: Select option 2. Correctness and explicit contracts come first: the executable-selected source owns bundle identity, while the caller-selected root keeps its existing non-identity purpose. It is directly testable, does not relax any invariant, and has no rollout impact.

`safety_checks`: Add distinct path/plan fields, an end-to-end path-detection/plan test with hostile `DEVKIT_ROOT`, a launch test proving bundle selection uses the authority field, full Go tests, rebuilt bundle proof, and repeat both reviewers after the diff changes. Do not change the canonical shell wrapper or make `DEVKIT_ROOT` silently ineffective for its existing surfaces.

`rollback_plan`: Revert the authority-root field and resolver call together if canonical `kit/bin/devctl` path detection fails in a supported source layout; owner is the devkit topic-branch author. No runtime rollout occurs in this task.

`decision_scope`: Devkit path detection, native plan metadata, governance bundle selection, and directly coupled tests only.

`required_artifacts`: This ExecPlan, exact-diff review, hostile-root tests, and final Nix/Go evidence. The deployed tradeoff skills referenced a missing `docs/decision-framework` tree; this plan is the repo-authorized decision-note path under `.agent/PLANS.md` rather than creating an unrelated policy hierarchy.

## Outcomes & retrospective

The implementation now separates three authorities cleanly: caller-selected `DEVKIT_ROOT` remains ordinary config/overlay input; the canonical devctl executable selects the source flake that realizes the bundle; and the exact realized Nix-store launcher selected in generated Codex config applies artifact identity after mutable workspace routing. The Artifact Column consumer has its own requested Ouro source and version, submit-to-ci retains its original source/output/hash, and no jar is copied or cached as a second authority.

The main review lesson was that an immutable payload is insufficient if a mutable file can still select or attest it. Moving both selection and fingerprinting out of `.devkit`, then removing the duplicate repository name, made the authority boundary inspectable. A second lesson was that a Bash launcher cannot sanitize `BASH_ENV` after startup; using a non-Bash immutable launcher closes that earlier hook.

## Final evidence

- Bundle output: `/nix/store/6mgmgwbh66ysna0akmfrjp87pnmkm1jk-dev-all-runtime-bundle`; derivation: `/nix/store/691grgp4qfyy0ywwr7yns5whfh485bwc-dev-all-runtime-bundle.drv`; NAR hash: `sha256-HSGK07yYrN6J86IRl14j5aw4w/0MFDzwYiH+0imzg1U=`; NAR size: `9968`.
- Artifact Column output/derivation/hash remain the accepted values in baseline evidence. Bundle `validate`, `identity-env`, `identity-json`, `identity-fingerprint`, and `plugin-smoke` pass; the live smoke reports the requested source, version, repository, deterministic V3 projections, manifest parity, producer/classpath/assembly checks, submit/governance closure checks, and stale-sabotage controls.
- Submit output/derivation/jar/hash remain `/nix/store/4xxf15fa8ajm60np3d9vnmiinmb53zd2-submit-to-ci-dev`, `/nix/store/k9jfshsf7pl3zk87szjdzw3jqzxivz05-submit-to-ci-dev.drv`, its canonical jar path, and `f3fd06efc9b92ffbda400fa5c5bbe3cc88bc46743a347e22c5f20d16441f531c`. The x86 flake now asserts the exact package and derivation paths during evaluation, while the bundle build independently hashes the jar against the accepted SHA and the launcher validates the packaged hash file and unchanged runtime flags. Manual manifest readback confirms main class `com.crib.bills.ouroboros.tools.submit_to_ci.Main` and implementation version `0.2.1-SNAPSHOT`.
- `go test -count=1 ./...` passes from `cli/devctl`. Conditional real-bundle adversarial tests pass with `DEVKIT_TEST_RUNTIME_BUNDLE` set to the output above and independently hash the physical Ivy jar plus repository metadata. The Nix `dev-all-runtime-bundle-bridge-smoke` output `/nix/store/fdc4sx3qg3j9bbxbh6v8h9mdnx06m2zk-dev-all-runtime-bundle-bridge-smoke` passes the real pinned forwarder/repo-env boundary; its provenance has `authoritativeEnv=1`, `jarMatchesExpected=true`, `mcpEntrypointMatchesExpected=true`, and exact selected/expected governance store jar and SHA-256.
- `nix flake show --all-systems` and `nix flake check --no-build` pass. The bundle, runtime-shell-inventory, and retired-runtime checks build. Full `nix flake check` reaches the new bundle successfully, then fails only the exact-base-identical overlay guards: overlay-local lock/output-lock debt, and missing `dev-workspace runtime.core_check` plus `ouroboros-terraform runtime.nix`.
- Canonical `kit/scripts/devkit` plan with hostile `DEVKIT_ROOT=/tmp/hostile-devkit` reports that ordinary root unchanged while `runtime_authority_root` is the topic worktree and the overlay flake remains `./overlays/dev-all#default`.
- Final independent exact-diff review reported no findings after all authority, forwarder, serialization, submit-invariance, and regression-coverage amendments. Initial GUI review on `spacequeen-1` thread `019f52f1-f80b-7ef3-918b-8e2312abab81` produced the remediated authority findings. Final split GUI review covered every implementation hunk on `davidlich-1` thread `019f5317-adb1-74a3-851a-eb2e60465c81` and every verification/ExecPlan/Nix hunk on `spacequeen-2` thread `019f5317-e384-7af2-a4fe-ab13857a49f5`; both final turns reported `NO FINDINGS`. All disposable GUI review threads were archived after readback. Commit, remote branch, and final clean custody remain the publication steps below.

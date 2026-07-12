# Dev-all runtime bundle profile-root repair

This ExecPlan is a living record for the corrected descendant of reviewed
devkit commit `b2b0df6793970c4682d517d5d325db42a5a46d4b`.

## Objective

Make every `dev-all-runtime-bundle` launcher operation select the immutable
bundle package root when invoked directly or through an aggregate Nix/NixOS
profile symlink. Preserve the accepted Artifact Column and submit identities,
publish the independently reviewed descendant, and hand its exact commit to
fleet-control and wsl-nix for a Spacequeen-only closure canary.

## Custody and constraints

- Worktree:
  `/workspaces/dev/devkit-worktrees/dev-all-runtime-bundle-profile-root-fix-20260711`.
- Branch: `codex/dev-all-runtime-bundle-profile-root-fix-20260711`.
- Exact base: `b2b0df6793970c4682d517d5d325db42a5a46d4b`.
- Protected `/workspaces/dev/devkit` and the accepted-bundle worktree remain
  read-only.
- Do not change the Artifact Column source/version/jar hash, submit source/jar
  hash, governance identity, SBT identity, or routing/forwarder semantics.
- No mutable wrapper, Bash startup-hook boundary, copied runtime artifact, or
  profile-local identity fallback is acceptable.

## Discovery

The first source/lock-pinned Spacequeen Colmena dry-run built system
`/nix/store/byrh3zlyjc1sdwb5hpvm75f7m4vizfq6-nixos-system-spacequeen-nix-26.05pre-git`.
Its `/sw/bin/dev-all-runtime-bundle` symlink entered the accepted Dash launcher
with `$0` under the aggregate `system-path`. The launcher consequently searched
that profile for `share/dev-all-runtime-bundle/identity.env` and failed before
identity validation. A provisional wsl-nix Bash wrapper corrected `$0` but an
independent reviewer proved a readable hostile `BASH_ENV` could exit before the
immutable launcher. That wrapper is rejected and must not be published.

The first repository-wide `nix flake check` then reached an unrelated
exact-base guard failure: two tracked overlay-local `flake.lock` files violated
the existing no-overlay-lock policy, and one Terraform recovery instruction
omitted the required `--output-lock-file /dev/null`. The updated authority
permits bounded lock-blocker repair. Removing only the forbidden locks and
correcting that instruction preserves the guard rather than weakening it and
is required for a fully checkable corrected descendant.

After that repair, the metadata validator exposed two more exact-base contract
drifts: `dev-workspace` lacked its required core check, and the validator had
not learned that an overlay with explicit `flake_input_overrides` delegates its
runtime to the repo-owned flake and therefore intentionally has no parallel
`runtime.nix`. The bounded repair adds `git diff --check` for the workspace and
makes only that explicit delegation shape exempt from the local-runtime file.
Independent review tightened this further: the validator now parses a nested,
non-empty override mapping rather than accepting the key's mere presence, and
the flake check proves an empty delegation cannot bypass `runtime.nix`.

## Design

Generate the public package launcher as a Dash script whose bundle root is
substituted with the outer derivation's exact `$out` during the build. The
launcher no longer derives authority from `$0`, so direct and aggregate-profile
invocation use the same immutable root without a wrapper hop.

Add a real aggregate-profile check with only `/bin` linked, deliberately
omitting the adjacent identity files that fooled `$0`-derived launchers. Run
all public operations (`validate`, identity env/JSON/fingerprint/NUL,
`plugin-smoke`, `exec`, and `governance-forward`) through that symlink. Compare
direct/profile outputs and use readable `BASH_ENV` and `ENV` hooks that write a
sentinel and exit 97 if any pre-authority shell executes them.

## Progress

- [x] Reproduce aggregate-system-path root misresolution from the first dry-run.
- [x] Reject the provisional Bash wrapper after independent sabotage.
- [x] Create a fresh worktree at the accepted devkit commit.
- [x] Implement embedded immutable-root launcher generation.
- [x] Add aggregate-profile, every-operation, and startup-hook sabotage checks.
- [x] Repair exact-base overlay lock-policy blockers without weakening the guard.
- [x] Run focused/full Go tests and the full seven-check devkit flake proof.
- [ ] Obtain independent exact-diff review, then commit and publish.
- [ ] Repin fleet-control and wsl-nix proofs to the corrected descendant.

## Verification

- `go test` for `cli/devctl/internal/runtime/launch`.
- `nix build` of the bundle, bridge smoke, and profile smoke checks.
- `nix flake check` for the full devkit proof.
- Direct/profile identity comparison and physical plugin-smoke evidence.
- Exact-diff review before source publication.

## Outcomes

Pre-publication evidence:

- Corrected bundle:
  `/nix/store/xldxr0qfcihh05z9qydmcmw7rzfb5dgi-dev-all-runtime-bundle`.
- Aggregate-profile proof:
  `/nix/store/v4q2z55hys5k053fp2zb0yp6nrkm96l7-dev-all-runtime-bundle-profile-smoke`.
  It covers all eight public operations, direct/profile equivalence, missing
  profile-adjacent identity data, and readable `BASH_ENV`/`ENV` exit-97 hooks.
- Real pinned governance forwarder proof:
  `/nix/store/xnsjs4kv7cvh1him9qhm5s1ms6zidccn-dev-all-runtime-bundle-bridge-smoke`.
- Corrected bundle fingerprint:
  `19edb1c395e841da4488f76cab38f045cf7cb2a2207d6629be439fa55917a65a`.
- Full `go test -count=1 ./...` passes in `cli/devctl`.
- Full `nix flake check --print-build-logs` passes all seven checks after the
  bounded exact-base lock/metadata repairs.
- Overlay metadata proof
  `/nix/store/qwyn62f4j2kjqkxjln7wr3lvci7jvhf1-devkit-overlay-runtime-metadata`
  includes an empty-delegation sabotage that fails the local-runtime contract.
- Identity JSON and `plugin-smoke` retain exact Artifact Column source
  `4eaf59e32d6ebd49c842c8038e7cfc4f825870d7`, version
  `0.1.0-artifact-column-v2-package-derived-ownership-20260711`, jar SHA-256
  `948d70381978242d5da4288368622e365b1d746546606c183d3cc321f41c00d2`,
  submit source `d15715adeadc8881b08ac7a05f19fec15fd29986`, and submit jar SHA-256
  `f3fd06efc9b92ffbda400fa5c5bbe3cc88bc46743a347e22c5f20d16441f531c`.

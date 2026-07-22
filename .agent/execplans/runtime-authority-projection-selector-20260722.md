# Runtime Authority Projection And Selector

This ExecPlan is a living document.

## Purpose

Provide one pure Devkit constructor whose Product runtime projection is consumed by both the adapter and the final `fleet-runtime-authority/v1` bundle, plus one compiled root-only atomic selector installer. The source layer remains independent of Product and source-acquisition v4 remains unchanged.

## Progress

- [x] Confirmed current Devkit has the final manifest parser and strict selector reader.
- [x] Remove the adapter `governanceEnv` build cycle.
- [x] Add the pure Product runtime projection shared by adapter and final bundle.
- [x] Add compiled selector installation with exact manifest path and digest, root/0600/no-follow/atomic guarantees.
- [x] Add sabotage and full absent-consumer lifecycle checks.
- [x] Pass the full Devkit flake check.
- [ ] Govern-publish trunk.

## Invariants and acceptance

The constructor derives identity once from pinned exact Product source. Neither adapter nor Fleet may copy or reinterpret the schema. The selector installer verifies but never derives identity. It refuses symlinks, non-root execution/ownership, wrong digest/path/schema, and non-atomic destination geometry. Tests must start without selector/consumer state, install, execute the package-owned adapter, verify the final manifest, remove state, and repeat.

## Decision Log

- 2026-07-22: A shared pure projection replaces the `governanceEnv` parameter. Post-build rewrites, copied schema fragments, Python helpers, and ambient Product packages are excluded.

## Surprises & Discoveries

- The strict final manifest reader already exists; the missing pieces are the pre-bundle pure projection and installer, not another parser.
- The post-refactor composed VM lifecycle passed at `/nix/store/y3m05h8gai9wirglwi8rysc1kzryxlf5-vm-test-run-product-consumer-boundary-diagnostic`; the public constructor contract passed at `/nix/store/7zh0xjv216xfwf4dqpsf3w288n770qd7-dev-all-runtime-bundle-public-constructor-contract`.
- After the selector-test fixture was corrected without weakening the production `/nix/store` restriction, the complete 19-check Devkit flake gate passed, including `/nix/store/98gnm12yn8hj99gsn8k0jnhqliavk0vz-vm-test-run-product-consumer-boundary-diagnostic`.

## Outcomes & Retrospective

Incomplete. No Devkit publication has occurred.

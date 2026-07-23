# Manifest-bound Product Codex auth seeding

This is the active Devkit implementation record for closing the pre-VM Product
credential gap. It is subordinate to the single-authority disposable-computation
objective: exact pinned source is derived once, one immutable manifest names the
runtime and fixed consumer geometry, and compiled package entrypoints consume
that manifest without caller-selected substitutes.

## Purpose

The Product lifecycle currently creates a synthetic `auth.json` directly and
then changes its owner and mode. That bypasses the compiled consumer boundary
and cannot prove that real Fleet-provided Codex authorization bytes can be
installed safely before a fresh consumer starts.

Devkit will expose two controller-only compiled entrypoints, one per declared
Product slot. Each accepts no arguments, reads one bounded JSON document from
stdin, loads the same immutable `fleet-runtime-authority/v1` manifest as the
Product services, verifies the complete offline Git seed for its fixed slot,
and atomically creates only the manifest's `codexAuthPath` as that slot's
UID/GID with mode `0600`.

## Progress

- [x] Reconstruct a clean worktree from accepted Devkit `origin/master`
  `30045946140c0dba8aa89c32c6bb9d56e64d2cb5`.
- [x] Confirm the defect: the VM lifecycle directly wrote `{}` and used
  `chown`/`chmod` instead of exercising a compiled credential effect.
- [x] Select fixed compiled slot entrypoints instead of extending the
  caller-indexed SSH setup command.
- [x] Implement anonymous create-only installation and unit sabotage for
  invalid input, symlink targets, wrong directory identity,
  duplicate/concurrent seed, temp discovery, in-place mutation, post-link
  effect classification, exact ownership/mode, and residue cleanup.
- [x] Complete package/manifest/module wiring and production lifecycle sabotage.
- [x] Add and pass the cheap Nix gate that executes both compiled fixed-slot
  entrypoints against immutable production-shaped manifests and binds them to
  the declared module wrappers.
- [ ] Obtain an independent read-only source audit on the unchanged candidate.
- [ ] After audit approval, pass the named Product consumer VM lifecycle and
  full relevant flake checks.
- [x] Commit the accepted local candidate and return its diff/check receipts
  without publishing.

## Milestones

1. Keep the privilege boundary inside the existing immutable Product adapter
   package and NixOS wrapper model. Add no shell/Python authority, ambient PATH,
   host share, provider effect, Fleet implementation, or runtime override.
2. Make slot selection a compiled property of `product-codex-auth-seed-1` and
   `product-codex-auth-seed-2`. Both load the manifest; callers provide only
   stdin bytes and cannot provide a path, UID, GID, mode, manifest, or index.
3. Require the manifest-bound offline Git marker, credential files, and empty
   `.codex` directory before reading the secret. Traverse held directory
   descriptors without following links, write and verify an anonymous
   `O_TMPFILE` generation, publish it create-only, then verify inode, metadata,
   and bytes across parent-directory durability.
4. Return machine-readable `attempted`, `effect`, or `ambiguous`
   classification for install failures. Close anonymous generations without
   residue and require fresh-consumer reconstruction after any failed effect
   or ambiguity.
5. Replace the lifecycle's direct auth fixture with the compiled wrapper and
   prove wrong-slot, unauthorized caller, symlink, duplicate, temp discovery,
   in-place mutation, post-link failure classification, secret-output,
   metadata, cleanup, and ordinary consumer launch/teardown behavior.
6. Use cheap source and Nix checks to localize failures. Only an independent
   source audit may admit the unchanged candidate to the expensive VM gate.

## Acceptance

- The two entrypoints and wrappers are immutable outputs of the existing
  Product adapter package and are named and digested by the same authoritative
  manifest consumed by `services.devkitProductConsumer`.
- No effect request accepts path, owner, group, mode, manifest, slot, or secret
  as an argument or environment variable. The secret enters only on stdin and
  is absent from stdout, stderr, receipts, temporary residue, and other paths.
- Missing or mismatched offline seed state, wrong fixed slot, invalid JSON,
  wrong owner/mode, symlinked ancestry/leaf, duplicate seed, and concurrent
  seed fail closed without replacing an accepted target.
- A successful seed creates exactly `codexAuthPath`, owned by the declared
  consumer UID/GID at `0600`, then the ordinary supervisor/app-server lifecycle
  succeeds and teardown leaves no consumer, process, socket, mount, or seed
  temporary residue.
- Focused Go tests, Nix evaluation/package/module checks, independent source
  audit, and the full named QEMU lifecycle pass on one unchanged source tree.

## Decisions

- Fixed-slot compiled entrypoints are required because the existing SSH setup
  command accepts caller-selected `--index` and `--root-projection`; those are
  valid for Git projection but violate the Codex-auth effect contract.
- An anonymous `O_TMPFILE` plus a no-replace final hard link is the acceptance
  primitive. Direct final-file writes expose partial contents; named
  consumer-owned temporaries expose mutable secret generations; ordinary
  rename can replace an accepted credential; and a manual pre-check has a
  race.
- A post-link failure is never flattened into an ordinary retryable error. The
  typed receipt says whether no target remains, the exact target exists, or
  the result is ambiguous. Failed effects are not repaired in place; their
  disposable consumer is reconstructed.
- The seed receipt contains only schema, status, and fixed consumer index. It
  contains no credential bytes, digest, caller path, or rewritten identity.
- The cross-cutting Management decision must consume this Devkit interface in
  the authoritative WSL/Nix derivation before deployment. Devkit does not
  rewrite that derivation or create a parallel manifest authority.
- The required tradeoff record is
  `docs/decision-framework/governance_decisions/20260723_manifest_bound_product_codex_auth_seed.md`.

## Surprises & Discoveries

- The existing `internal/productseed` package already supplied no-follow,
  descriptor-relative Git seed writes and sabotage. Its direct final-file
  create is correct for an absent offline projection but is not atomic enough
  for the Codex authorization acceptance contract.
- The current generic controller-only wrapper is privileged correctly, but its
  caller-selected index makes it unsuitable as the fixed-slot credential
  boundary.

## Outcomes & Retrospective

The repaired candidate passes the full Go suite, Nix flake evaluation, the
named `product-codex-auth-seed-entrypoint-hermetic` gate, the Product consumer
module contract, supervisor identity lifecycle, readiness hermetic, and absent
consumer construction checks. The QEMU lifecycle remains intentionally blocked
on a fresh independent audit of the final committed tree. No publication,
deployment, station contact, or real credential hydration is in scope for this
plan.

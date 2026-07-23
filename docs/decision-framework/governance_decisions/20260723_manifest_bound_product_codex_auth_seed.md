# Manifest-bound Product Codex auth seed

This note records the Devkit-side implementation decision. The cross-cutting
Management decision remains responsible for consuming the interface in the
single authoritative WSL/Nix derivation; this note creates no deployment or
manifest authority.

## problem

The Product VM diagnostic directly created and changed ownership on a synthetic
`auth.json`. That did not exercise a package-owned credential effect and could
not establish safe pre-VM Codex hydration. Extending the existing SSH seed
command with another caller-selected index/path mode would preserve the same
authority ambiguity.

## options

1. Keep direct fixture writes and treat file metadata checks as proof.
2. Add a generic privileged seed command accepting target, UID/GID, mode,
   manifest, or slot arguments.
3. Add two zero-argument compiled entrypoints inside the existing Product
   adapter package, each fixed to one manifest consumer and accepting secret
   bytes only on stdin.

## selection_rationale

Choose option 3. The authoritative manifest already defines two fixed consumer
identities and their final `codexAuthPath` values. Compiled fixed-slot
entrypoints preserve that identity, eliminate caller policy, and let the
ordinary NixOS wrapper provide the narrow privilege needed to install the
consumer-owned file. This is the smallest design whose postcondition can be
reasoned about without a second authority.

The applicable decision framework was read from the canonical Management
source because the deployed `tradeoff-decision` skill's referenced bundled
framework path was absent. This record follows its required problem, options,
selection rationale, safety checks, rollback plan, and decision scope shape.

## safety_checks

- Each entrypoint has one source-fixed slot and accepts zero arguments.
- The executable, contract, slot geometry, target path, UID/GID, and immutable
  offline seed state are validated against one held authority manifest.
- Input is bounded valid JSON read only from stdin and is never emitted or
  included in the typed receipt.
- Directory traversal is descriptor-relative and no-follow; existing
  ownership/mode mismatches are rejected rather than repaired.
- The offline Git seed creates the empty manifest-owned `.codex` directory
  before authorization bytes are accepted. The authorization generation is an
  anonymous `O_TMPFILE`, so no consumer-owned temporary pathname exposes
  secret bytes before the create-only final link.
- The held generation is byte-verified before publication and the installed
  path is revalidated by inode, metadata, and exact bytes before and after
  parent-directory durability. Duplicate and concurrent seeds cannot replace
  the accepted credential.
- Failures before the link are typed `attempted`; post-link reconciliation
  returns `effect` only when the exact intended file is present and
  `ambiguous` otherwise. Anonymous generations close without residue. Any
  failed effect or ambiguity taints the disposable consumer for reconstruction;
  the command performs no unsafe pathname cleanup or inline repair.
- Tests cover wrong slot, unauthorized caller, symlink ancestry/leaf,
  malformed input, duplicate/concurrent seed, anonymous-generation discovery,
  in-place mutation, post-link failure classification, exact metadata, no
  secret output, cleanup, ordinary launch, and teardown.
- The held-session teardown check starts only after unrelated construction and
  Git assertions. Its immutable client timeout therefore diagnoses a missing
  supervisor termination rather than QEMU speed. A separately linked
  short-timeout sabotage must fail with the exact timeout diagnostic.
- A clean held session succeeds only when the session process exits zero.
  Non-clean SSH/session exits remain typed RED, and stderr collection is
  bounded after process exit.
- The production SSH session relay deliberately suppresses `io.Copy`
  ReaderFrom/WriterTo fast paths. Ordinary Go reads observe supervisor EOF
  reliably; Linux splice was empirically capable of surviving the peer close.
- App-server ownership counts exactly one listening `/proc/net/unix` entry and
  proves its inode is held by the pinned PID. Connected sockets retaining the
  same pathname are effects of that listener and do not invalidate its
  authority.
- The named cheap Nix gate executes both fixed compiled command entrypoints
  against immutable production-shaped manifests; the module contract
  separately proves that deployed controller-only wrappers bind those package
  targets.
- An independent source audit precedes the expensive VM promotion check.

## rollback_plan

Revert the Devkit interface and its authoritative manifest fields together
before deployment. Never restore direct auth creation as promotion evidence or
retain a partially composed manifest. Real credentials are not hydrated by
this change, so rollback requires no credential recovery.

## decision_scope

Only the Devkit Product adapter package, its fixed-slot NixOS wrappers,
manifest parser/package binding, synthetic lifecycle fixture, tests, and
documentation. Management/Fleet/WSL/Product source, publication, deployment,
provider state, stations, and real credentials remain out of scope.

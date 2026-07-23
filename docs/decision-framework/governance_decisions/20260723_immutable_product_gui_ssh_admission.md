# Immutable Product GUI SSH admission

This note records an implementation choice; it is not a promotion gate.

## problem

A Product consumer must accept only the controller key selected by the
authoritative Nix build. Seeding `authorized_keys` into a writable consumer
home would let stopped-volume setup become a second authorization authority.

## options

1. Write GUI admission into each consumer home during credential seeding.
2. Derive one restricted public-key file per consumer in Nix and use that same
   immutable path in the runtime manifest and sshd configuration.

## selection_rationale

Choose option 2. Nix establishes admission once; Devkit validates and consumes
it without rewriting it. Product Git and Codex credentials remain separately
seeded into the stopped volume.

## safety_checks

The manifest accepts only a regular read-only `/nix/store` artifact containing
exactly one `restrict ssh-ed25519` key. The lifecycle proves the Product UID
cannot replace it and another consumer's key is rejected.

## rollback_plan

Revert the source batch if the authoritative Nix build cannot bind the same
artifact to both manifest and sshd. Do not restore runtime authorization
seeding or add an `AuthorizedKeysCommand` fallback.

## decision_scope

Product GUI SSH admission only. It does not select source, hydrate credentials,
or create another runtime identity.

# Readable Product authority selector

This note records an implementation choice; it is not a promotion gate.

## problem

The production adapter, supervisor, proxy, and SSH-session processes run as
their declared unprivileged Product users, but the root installer created the
authority selector as `0600`. The fixture build bypassed that selector, hiding
the fact that production consumers could not open it.

## options

1. Keep `0600` and add a privileged descriptor broker.
2. Keep root as the only writer while making the non-secret selector itself
   read-only to consumers.

## selection_rationale

Choose option 2. The selector contains only an immutable Nix-store path and
SHA-256 digest. Exact root ownership, a protected parent, no-follow opens,
strict parsing, immutable-manifest validation, and digest verification retain
the authority contract without introducing a broker or second capability
seam. This is simpler to verify and preserves the single-authority model.

## safety_checks

- The installer writes a private temporary generation, changes it to exact
  `0444`, fsyncs it, and atomically renames it.
- Readers reject any selector that is not a root-owned regular `0444` file.
- Writable, symlinked, malformed, non-store, or digest-mismatched inputs remain
  rejected.
- The production package, not the integration locator, must exercise the
  absent-consumer lifecycle.

## rollback_plan

Revert the mode contract only if the selector gains secret material. In that
case, introduce a separately reviewed typed capability boundary before
restoring `0600`; do not revive the fixture locator as production evidence.

## decision_scope

Only the Product authority selector's read permission and its production-path
tests. The manifest derivation, root-only installation, and downstream
authority semantics do not change.

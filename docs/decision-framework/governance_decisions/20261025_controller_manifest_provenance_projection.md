# Preserve the installed controller manifest's immutable path

## problem

The ordinary v9 native Runtime resolved the correct c75aa9fc manifest bytes to
the regular bind at /etc/fleet/controller-operation-authority.json. Fleet's
source-derived real-path guard refused before operation acceptance. The host
locator instead resolved to the actual xf5wl92g Nix-store artifact. Changing
manifest bytes, spelling or identity would not repair this projection defect.

## options

1. Preserve the fixed locator as a symlink to its actual installed immutable
   artifact, using the existing renderer's source-owned projection pattern.
2. Weaken Fleet's provenance guard or supply a caller-selected manifest.
3. Copy the same bytes to a newly named artifact or hand-build a live namespace.

## selection_rationale

Choose1. Resolve only the package-owned public/static locators, require their
exact agreement and a real regular file within the immutable store, then bind
that actual artifact read-only at its genuine path and preserve the public
symlink. The explicit file bind also permits hermetic renderer fixtures without
altering production authority. Existing profile decoding, exact bind geometry,
hashes, worktree/socket/custody and ordinary Fleet validation remain intact.
The existing service-owner projection already demonstrates this pattern.

## safety_checks

Test identical-byte flattened, different-artifact and store-escape refusals.
The actual CLI-rendered sandbox fixture must resolve the selected artifact,
retain its hash, deny writes, preserve containment/egress and reuse its home on
a later invocation. Run the normal complete Devkit source CI, independent review
and affected WSL composition qualification. The installed c75 first typed read
remains Root/Runtime's receiving obligation, distinct from fixture proof.

## rollback_plan

Preserve candidate and full results after any refusal. Source performs no
deployment, refresh, reset, live namespace construction or unknown-effect replay.
Root owns supported receiving/adoption and rollback.

## decision_scope

Continue the existing Source commission under Root01a165e0, from accepted
Devkit118f1f68. Preserve published CLI supply aea5e9a8 and held realignment work.
This changes profile construction, not instruction authentication, issuance,
manifest authority, Product code or the full business acceptance conditions.

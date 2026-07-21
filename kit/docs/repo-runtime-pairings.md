# Repo Runtime Pairings

Status: Active operator guidance for package/runtime metadata and ordinary
non-Product overlays. Product lifecycle authority is the installed
`fleet-runtime-authority/v1` manifest, not this matrix.

Devkit treats Codex as a tool inside repo-specific runtimes, not as the runtime
identity of a project. Session names must not be used to decide which runtime
to refresh.

## Canonical Pairings

Run `devkit/kit/scripts/devkit runtime-matrix --all` for the machine-readable
view. Runtime metadata uses one overlay-local `runtime.flake` per overlay, such
as
`./overlays/dev-all#default`. The root flake still exposes compatible shells
for direct Nix use, but devkit runtime metadata points at the overlay-local
flake boundary.

| Repo | Canonical overlay | Service | Runtime | Core build check |
| --- | --- | --- | --- | --- |
| `ouroboros-ide` | `dev-all` (package-composition metadata only) | `dev-agent` | `./overlays/dev-all#default` | `bash scripts/sbt2 "Compile / compile"` (diagnostic only) |
| `dumb-onion-hax` | `dumb-onion-hax` | `dev-agent` | `./overlays/dumb-onion-hax#default` | `sbt compile` |
| `ouroboros-static-front-end` | `ouroboros-static-front-end` | `frontend` | `./overlays/ouroboros-static-front-end#default` | `npm run build` |
| `ouroboros-terraform` | `ouroboros-terraform` | `dev-agent` | `./overlays/ouroboros-terraform#default` | `terraform fmt -check -recursive` |
| `pokeemerald` | `pokeemerald` | `dev-agent` | `./overlays/pokeemerald#default` | `make modern` |

Each listed runtime is expected to carry the current Codex CLI version declared
in the overlay `runtime.codex_version`. On May 21, 2026 that value is `0.133.0`.
Verify local runtimes with:

```bash
devkit/kit/scripts/devkit runtime-matrix --all --check
```

For a focused check while unrelated overlays are under active custody, filter by
repo or overlay:

```bash
devkit/kit/scripts/devkit runtime-matrix --repo ouroboros-terraform --check
devkit/kit/scripts/devkit runtime-matrix --overlay ouroboros-terraform --check
```

Use `--all` when you need to include non-canonical overlays. The `codex`
overlay is not a second runtime pairing for `ouroboros-ide`.

## Refresh Rule

Refresh ordinary non-Product native runtimes by updating the overlay
`runtime.nix` or root flake inputs/packages and rebuilding the CLI. Validate
both the overlay-local Nix shell and native lifecycle matrix:

```bash
make -C devkit/cli/devctl build
devkit/kit/scripts/devkit -p ouroboros-static-front-end ensure-ready --repo ouroboros-static-front-end --count 1 --flake ./overlays/ouroboros-static-front-end#default
nix --extra-experimental-features 'nix-command flakes' develop ./overlays/ouroboros-static-front-end --output-lock-file /dev/null --command true
make -C devkit native-overlay-matrix
```

`runtime.flake` is ordinary overlay pairing metadata. For Product it is only a
package-composition input: the sole authoritative Nix derivation emits the
installed manifest, Product consumers use its exact adapter/launcher, and the
Product-owned twice-fresh app alone decides promotion.

## Build Evidence

The `runtime.core_check` value documents the build gate that proves the repo's
core app still builds in the paired runtime. Prefer the overlay `maintain` hook
when it runs the same command. If a core check changes, update both the overlay
metadata and this page in the same change.

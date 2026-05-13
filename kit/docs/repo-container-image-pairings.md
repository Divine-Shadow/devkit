# Repo Runtime Pairings

Status: Active operator guidance.

Devkit treats Codex as a tool inside repo-specific runtimes, not as the runtime
identity of a project. Compose project names such as `devkit-codex8` or
`devkit-ouro8` are session names only; they must not be used to decide which
runtime to refresh.

## Canonical Pairings

Run `devkit/kit/scripts/devkit image-matrix --all` for the machine-readable
view. The command name is historical; runtime metadata now uses
`runtime.flake`. The values in `runtime.flake` intentionally remain root-flake
refs such as `.#dev-all` in this transition slice. Each flake-backed overlay
also has an overlay-local `flake.nix`; use `nix develop ./overlays/<overlay>`
when you want to enter that overlay directly.

| Repo | Canonical overlay | Service | Runtime | Core build check |
| --- | --- | --- | --- | --- |
| `ouroboros-ide` | `dev-all` | `dev-agent` | `.#dev-all` | `bash scripts/sbt2 "Compile / compile"` |
| `dumb-onion-hax` | `dumb-onion-hax` | `dev-agent` | `.#dumb-onion-hax` | `sbt compile` |
| `ouroboros-static-front-end` | `ouroboros-static-front-end` | `frontend` | `.#ouroboros-static-front-end` | `npm run build` |
| `ouroboros-terraform` | `ouroboros-terraform` | `dev-agent` | `.#ouroboros-terraform` | `terraform fmt -check -recursive` |
| `pokeemerald` | `pokeemerald` | `dev-agent` | `.#pokeemerald` | `make modern` |

Each listed runtime is expected to carry the current Codex CLI version declared
in the overlay `runtime.codex_version`. On May 9, 2026 that value is `0.130.0`.
Verify local runtimes with:

```bash
devkit/kit/scripts/devkit image-matrix --all --check
```

Use `--all` when you need to include legacy/non-canonical overlays. The
`codex` overlay is a compatibility image-build/debug shim for
`ouroboros-ide`; it is not a second runtime pairing for that repo.

## Refresh Rule

Refresh native runtimes by updating the overlay `runtime.nix` or root flake
inputs/packages and rebuilding the CLI. Keep root refs working while validating
the overlay-local flakes:

```bash
make -C devkit/cli/devctl build
devkit/kit/scripts/devkit -p dev-all ensure-ready --repo ouroboros-ide --count 1 --flake .#dev-all
nix --extra-experimental-features 'nix-command flakes' develop ./overlays/dev-all --no-write-lock-file --command true
```

Legacy Compose image tags may still appear in `compose.override.yml` files for
historical non-`dev-all` operation, but they are no longer the authoritative
runtime pairing metadata.

## Build Evidence

The `runtime.core_check` value documents the build gate that proves the repo's
core app still builds in the paired runtime. Prefer the overlay `maintain` hook
when it runs the same command. If a core check changes, update both the overlay
metadata and this page in the same change.

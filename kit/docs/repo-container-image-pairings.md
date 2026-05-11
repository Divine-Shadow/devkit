# Repo Container Image Pairings

Status: Active operator guidance.

Devkit treats Codex as a tool inside repo-specific images, not as the runtime
identity of a project. Compose project names such as `devkit-codex8` or
`devkit-ouro8` are session names only; they must not be used to decide which
repo image to refresh.

## Canonical Pairings

Run `devkit/kit/scripts/devkit image-matrix` for the machine-readable view.

| Repo | Canonical overlay | Service | Image | Core build check |
| --- | --- | --- | --- | --- |
| `ouroboros-ide` | `dev-all` | `dev-agent` | `local/dev-agent:ouroboros-ide` | `scripts/sbt2 "Compile / compile"` |
| `ouroboros-static-front-end` | `ouroboros-static-front-end` | `frontend` | `local/dev-agent:ouroboros-static-front-end` | `npm run build` |
| `ouroboros-terraform` | `ouroboros-terraform` | `dev-agent` | `local/dev-agent:ouroboros-terraform` | `terraform fmt -check -recursive` |
| `pokeemerald` | `pokeemerald` | `dev-agent` | `local/dev-agent:pokeemerald` | `make modern` |

Each listed image is expected to carry the current Codex CLI version declared in
the overlay `runtime.codex_version`. On May 9, 2026 that value is `0.130.0`.
Verify the local images with:

```bash
devkit/kit/scripts/devkit image-matrix --check
```

Use `--all` when you need to include legacy/non-canonical overlays. The
`codex` overlay is a compatibility image-build/debug shim for
`ouroboros-ide`; it is not a second runtime pairing for that repo.

## Refresh Rule

Refresh images by repo image tag:

```bash
docker build -t local/dev-agent:ouroboros-ide \
  -f ouroboros-ide/infra/docker/dev/codex-agent.Dockerfile \
  --build-arg JDK_VERSION=21 ouroboros-ide
```

Then recreate only the containers that use that image. If a stack name contains
`codex8`, confirm the mounted repo and image first; the stack name alone is not
evidence.

## Build Evidence

The `runtime.core_check` value documents the build gate that proves the repo's
core app still builds in the paired image. Prefer the overlay `maintain` hook
when it runs the same command. If a core check changes, update both the overlay
metadata and this page in the same change.

# Native Readiness Contract

Native readiness is the authoritative readiness path for every overlay with
`runtime.flake`. Legacy Compose readiness remains only for overlays without a
native flake and for explicit legacy Compose workflows.

## Contract

- `kit/scripts/devkit` remains the supported entrypoint; it execs
  `kit/bin/devctl`.
- `verify`, `doctor-runtime`, and `ensure-ready` route flake-backed overlays
  through native lifecycle/readiness.
- Native readiness runs inside the Nix sandbox through bubblewrap, not through
  Docker Compose.
- Runtime checks prove the sandbox, brokered Docker endpoint, declared Codex
  version, and overlay toolchain are present.
- Repo checks prove repository-specific readiness. These checks may be slower or
  stateful and are intentionally separate from runtime capacity.
- Explicit `compose ...` remains the legacy escape hatch for overlays that still
  need Compose semantics.

## Built-In Native Checks

Every native readiness run first validates:

- native agent state can be prepared;
- the sandbox can launch and has `DEVKIT_NATIVE_AGENT=1`;
- `DOCKER_HOST` points at the broker socket, not `/var/run/docker.sock`;
- the host Docker socket is absent inside the sandbox;
- the broker socket responds to Docker `_ping`.

Overlay `readiness.runtime_checks` then adds tool-specific checks, and
`runtime.codex_version` is converted into a `codex-version` runtime check.

## Repo Checks

Overlay `readiness.repo_checks` is now the named repo-readiness surface. Native
readiness also preserves existing behavior by adding:

- `warm-hook`, when `hooks.warm` is configured and no explicit repo check uses
  that name;
- `core-check`, when `runtime.core_check` is configured and no explicit repo
  check uses that name.

That keeps existing warm/core behavior while making extra overlay checks
visible and named.

## Overlay Expectations

| Overlay | Runtime readiness | Repo readiness |
| --- | --- | --- |
| `_template` | common agent tools, Docker client, Codex version | placeholder `core-check` for new overlays to replace |
| `codex` | SBT/JVM, Go, Docker client, Spago, Netlify, Deno, Playwright | warm hook plus `scripts/sbt2 "Compile / compile"` |
| `dev-all` | SBT/JVM, Go, Docker client, AWS, Pokeemerald tools, Spago, Netlify, Deno, Playwright browser launch | git remote, frontend warm/build/typecheck/test/browser/server checks, SBT core check |
| `dumb-onion-hax` | SBT/JVM, AWS CLI, Docker client | warm hook plus `sbt compile` |
| `ouro-integration` | Terraform, Packer, AWS CLI, SBT/JVM, Docker client | AWS config plus Terraform plan/backend assembly |
| `ouroboros-static-front-end` | Node/npm, PureScript, Spago, Netlify, Deno, Playwright, Docker client | warm hook plus `npm run build` |
| `ouroboros-terraform` | Terraform, Packer, AWS CLI, Docker client | AWS profile config plus `terraform fmt -check -recursive` |
| `pokeemerald` | ARM toolchain, `mgba-headless` scripting support, Docker client | warm hook plus `make modern` |

## Cost Model

Runtime checks should be fast and safe to run after native lifecycle commands.
Repo checks may compile code, run package manager commands, start local dev
servers, or validate cloud configuration. Use `--skip-repo-checks` when only
runtime capacity is needed, and use `--skip-ready` on lifecycle commands when
recovering capacity before repo validation is practical.

Broker policy create/delete smokes remain in `kit/scripts/native-runtime-smoke`;
default readiness proves broker connectivity but does not create containers.

## Verification

The readiness contract is covered by:

- `go test -count=1 ./...`
- `kit/scripts/devkit --dry-run -p <flake-overlay> verify`
- `kit/scripts/devkit --dry-run -p <flake-overlay> doctor-runtime`
- `kit/scripts/devkit --dry-run -p <flake-overlay> ensure-ready`
- `kit/scripts/devkit image-matrix --all --check`
- `make overlay-runtime-smoke`

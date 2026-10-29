# Native Readiness Contract

Native readiness is the authoritative readiness path for every supported
overlay.

## Contract

- `kit/scripts/devkit` remains the supported entrypoint; it execs
  `kit/bin/devctl`.
- `verify`, `doctor-runtime`, and `ensure-ready` route flake-backed overlays
  through native lifecycle/readiness.
- Native readiness runs inside the Nix sandbox through bubblewrap.
- Runtime checks prove the sandbox, brokered OCI endpoint, declared Codex
  version, and overlay toolchain are present.
- Repo checks prove repository-specific readiness. These checks may be slower or
  stateful and are intentionally separate from runtime capacity.
- Every flake-backed overlay declares `readiness.default_mode`. The current
  overlay contract sets that default to `runtime-only`, so lifecycle capacity
  recovery does not run app builds or package-manager checks unless requested.
- Use `--runtime-only` to force runtime checks and `--repo-readiness` or
  `--full` to force repo checks. A bare trailing `--repo` is accepted as a
  full-readiness alias when it is not followed by a repo name.
- The retired runtime namespace is not a readiness path.

## Built-In Native Checks

Every native readiness run first validates:

- native agent state can be prepared;
- the sandbox can launch and has `DEVKIT_NATIVE_AGENT=1`;
- `DOCKER_HOST` points at the broker socket, not `/var/run/docker.sock`;
- the host daemon socket is absent inside the sandbox;
- the broker socket responds to API `_ping`.

Overlay `readiness.runtime_checks` then adds tool-specific checks, and
`runtime.codex_version` is converted into a `codex-version` runtime check.
Per-agent checks resolve mutable runtime evidence from the current slot's
`HOME`/`CODEX_HOME`; they must not block a freshly reconstructed slot on stale
provenance retained in a sibling leased home.
The immutable devctl package is also the runtime-flake authority: because each
overlay delegates to `path:../..`, that package must include the repository
root `flake.nix`, `flake.lock`, `nix/`, and `overlays/` beside `kit/bin/devctl`.
The `devctl-overlay-runtime-authority` check builds that immutable package,
resolves the packaged `overlays/dev-all` flake offline, evaluates its delegated
dev-shell derivation, and executes the packaged devctl's dev-all runtime-matrix
contract. It also requires the packaged native plan to resolve that immutable
authority into an absolute root-directed flake reference while mutable
worktree and state roots remain separate. The gate compares the packaged
lock's exact `nixpkgs` and `nixpkgs-playwright` identities (including their
hashes) against the offline overlay metadata and evaluates the exact dev-shell
derivation. It then builds the exact packaged runtime shell and Postgres broker
offline, runs a bounded shell canary to derive its store-owned Bubblewrap, and
passes all three package-owned paths explicitly to the runtime matrix and
native-plan checks with ambient selectors unset. Protected runtime execution is
proved separately by a fresh consumer.
The configured rooted form remains `path:.?dir=overlays/dev-all#default`, and
the installed runtime resolves it before Nix evaluation to the direct immutable
subdirectory reference `path:<authority>/overlays/dev-all#default`. This keeps
the overlay's `path:../..` input inside that exact packaged authority while
remaining compatible with Nix versions that do not support `dir` on `path:`.

## Repo Checks

An already accepted Product GUI consumer can run one package-owned named repo
check in its current Git worktree, including a registered nested writer:

```bash
devctl -p dev-all native readiness --current-worktree --repo ouroboros-ide --repo-check-name frontend-warm --format json
```

Run it from the exact writer repository root using the manifest-selected
Devctl package. This form uses the existing v4 consumer and its unchanged
environment. It derives the lane root and home from the fixed GUI projection,
verifies that the current canonical Git root belongs to that lane's worktree
list, and loads only the executable-owned overlay recipe. It accepts no
command, target, root, flake, profile or index overrides. Global `--dry-run`
validates and reports the binding with status `planned` without executing it.

Only the selected named command runs; no boundary preparation, hydration,
listener, broader runtime/repo check, or warm/core hook runs. Its report scope
is `current-worktree-repo-check`, and a nonzero command remains a failure.
This does not establish runtime capacity or Product acceptance. Ordinary
host readiness below retains its full existing preparation and checks.

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
| `_template` | common agent tools, container API client, Codex version | placeholder `core-check` for new overlays to replace |
| `codex` | SBT/JVM, Go, container API client, Spago, Netlify, Deno, Playwright | warm hook plus `scripts/sbt2 "Compile / compile"` |
| `dev-all` | SBT/JVM, Go, container API client, AWS, Pokeemerald tools, Spago, Netlify, Deno, Playwright browser launch | git remote, frontend warm/build/typecheck/test/browser/server checks, SBT core check |
| `dumb-onion-hax` | SBT/JVM, AWS CLI, container API client | warm hook plus `sbt compile` |
| `ouro-integration` | Terraform, Packer, AWS CLI, SBT/JVM, container API client | AWS config plus Terraform plan/backend assembly |
| `ouroboros-static-front-end` | Node/npm, PureScript, Spago, Netlify, Deno, Playwright, container API client | warm hook plus `npm run build` |
| `ouroboros-terraform` | Terraform, Packer, AWS CLI, container API client | AWS profile config plus `terraform fmt -check -recursive` |
| `pokeemerald` | ARM toolchain, `mgba-headless` scripting support, container API client | warm hook plus `make modern` |

## Cost Model

Runtime checks should be fast and safe to run after native lifecycle commands.
Repo checks may compile code, run package manager commands, start local dev
servers, or validate cloud configuration. Lifecycle commands `up`, `restart`,
and `scale` use overlay `readiness.default_mode`; the declared flake-backed
default is `runtime-only`. Use `verify`, `ensure-ready --repo-readiness`, or
`native readiness --repo-readiness` when app-level repo validation should be
part of the gate. `--skip-repo-checks` remains as a compatibility alias for
runtime-only readiness, and `--skip-ready` remains the capacity recovery escape
hatch when readiness should not run at all.

Broker policy create/delete smokes remain in `kit/scripts/native-runtime-smoke`;
default readiness proves broker connectivity but does not create containers.

## Verification

The readiness contract is covered by:

- `go test -count=1 ./...`
- `kit/scripts/devkit --dry-run -p <flake-overlay> verify`
- `kit/scripts/devkit --dry-run -p <flake-overlay> doctor-runtime`
- `kit/scripts/devkit --dry-run -p <flake-overlay> ensure-ready --runtime-only`
- `kit/scripts/devkit --dry-run -p <flake-overlay> ensure-ready --repo-readiness`
- `kit/scripts/devkit runtime-matrix --all --check`
- `make overlay-runtime-smoke`

## Declared Terraform Agent1 startup geometry

Fleet's existing `terraform-ouro-1` startup holds the normal socket lease and invokes `native prepare` with exact project/repository `ouroboros-terraform`, explicit index 1, canonical source-declared SSH origin, standard `main`/`agent1` and `/home/bayesartre/dev/control-plane-worktrees/terraform-ouro-1` workspace. No count, target, origin, branch or arbitrary geometry override is accepted. The package's native Git/SSH bootstrap materializes an absent lane-owned linked checkout. Existing checkout reuse validates lane common ownership, marker/origin, clean worktree and branch before returning without fetching, pruning, resetting or rewriting Git metadata. Foreign, partial, dirty, symlinked and branch-conflicting custody is preserved and refused; failure does not bulk-delete the TF1 workspace.

Preparation and execution project only this workspace to `/workspaces/dev`, with the unchanged `ouroboros-terraform-agent1` persistent home. Generic native prefix geometry, Shared Power and Terraform Agent2 are unchanged. Ordinary runtime/Auth preparation remains institution-owned through the normal entry and is not proven by source or launch-construction fixtures.

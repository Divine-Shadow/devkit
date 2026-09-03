# Native Operator Runbook

Devkit's supported runtime is Nix flakes plus the native sandbox. Use
`kit/scripts/devkit` as the operator entrypoint. It execs `kit/bin/devctl` and
fails loudly if the compiled binary is missing.

## Build

```bash
make -C cli/devctl build
```

The build writes `kit/bin/devctl`. Direct binary invocation is allowed for power
users, but scripts and runbooks should use `kit/scripts/devkit`.

## Launch

Start one or more agents for an overlay:

```bash
kit/scripts/devkit -p dev-all up --repo ouroboros-ide --count 2
kit/scripts/devkit -p dev-all status --repo ouroboros-ide --ready
```

For a single-overlay repo, use that overlay:

```bash
kit/scripts/devkit -p ouroboros-static-front-end up --repo ouroboros-static-front-end --count 1
```

## Exec And Attach

Run a non-interactive command inside an agent sandbox:

```bash
kit/scripts/devkit -p dev-all exec 1 --repo ouroboros-ide -- bash -lc 'pwd && codex --version'
```

Open an interactive shell:

```bash
kit/scripts/devkit -p dev-all attach 1 --repo ouroboros-ide
```

The sandbox should expose:

- `DEVKIT_NATIVE_AGENT=1`
- for `dev-all`, `CODEX_HOME` under the repo-local per-agent `.devhome-agentN`
  directory so tmux and `codex resume` see the same session history
- `DOCKER_HOST=unix://...` only when brokered OCI access is configured
- no direct `/var/run/docker.sock` bind for standard agents

## Scale

Resize the agent set:

```bash
kit/scripts/devkit -p dev-all scale 3 --repo ouroboros-ide
kit/scripts/devkit -p dev-all status --repo ouroboros-ide --ready
```

`scale` updates native manifests and preserves per-agent state under the native
state root.

## Git Lane Custody And Migration

Every prepared native lane owns a separate bare common repository:

```text
<worktree-root>/.devkit/git/agentN/<repo>.git
```

The marker in that directory binds the repository, source-declared origin, and
`agentN` identity. Git refs, linked-worktree registrations, index files, and
transaction locks therefore remain inside one execution lane. Relative `.git`,
`commondir`, and reverse `gitdir` pointers keep the complete lane topology
portable when the worktree root is projected at another sandbox path.

Older installations may still have lanes linked to the legacy shared path
`<worktree-root>/.devkit/git/<repo>.git`. Migrate one idle lane through the
selected-slot reset boundary:

```bash
kit/scripts/devkit -p dev-all native reset --repo ouroboros-ide --index 1
```

That reset creates the selected lane's new common repository. It leaves the
legacy common repository, sibling worktrees, refs, locks, and processes
unchanged, so an active legacy agent2 can coexist with a reconstructed agent1.
Repeat only when each remaining lane reaches its own disposable boundary. A
whole-prefix reset may remove the legacy common repository because that command
first proves the complete source-declared slot set is idle.

When reducing the declared lane count, let the source-derived manifest-shrink
transaction retire the old suffix before running another setup or selected-slot
reset. The shrink transaction deliberately validates indices against the prior
installed manifest, so it can still retire legacy `agentN` after the new source
count makes `N` out of range for an ordinary selected reset. For a legacy
surplus it requires the exact v1 ownership marker, origin, forward `.git` link,
reverse worktree registration, branch, and clean/no-ahead custody. It then
removes only that surplus worktree, home, and state. The shared legacy common
repository—including the retired lane's stale registration/ref and every
retained sibling's metadata and lock domain—remains untouched until all legacy
lanes have migrated or a proven-idle whole-prefix reset owns it.

The still-older historical layout may register a surplus linked worktree under
the ordinary source checkout at `<host-root>/<repo>/.git/worktrees/<entry>`.
Manifest shrink accepts that layout only when the common path is derived
exactly from the manifest's worktree root and repository, all paths and
forward/reverse links are canonical, the source origin and `agentN` branch are
exact, and the branch commit is contained in a fresh current remote base. The
present worktree's bounded, exact `.git` pointer is the sole selector for its
active historical registration; unrelated and stale entries in the shared
registration directory are not enumerated or treated as lane custody. When the
worktree is already absent, the exact `agentN` branch is resolved across the
lane, legacy, and historical common repositories instead. When more than one
branch domain survives, every discovered commit must be independently contained
in the same freshly fetched current base; equal or divergent contained copies
are safe migration residue, and the preserved historical domain is preferred
when present. A sole historical domain receives that same current-remote proof,
while sole lane/legacy domains retain their existing local-base check. A stale
historical registration and its metadata locks are
then protected but inert: this transaction neither reads through nor mutates
that metadata, and it still protects any surviving branch through the same
current-remote containment proof.

The protected source checkout's potentially stale `refs/remotes/origin/<base>` is
not ancestry or setup-tree authority for this historical layout. One
operation-bound proof fetches the base once for the full surplus suffix and
uses that exact fetched commit for both checks through package-owned Git/SSH
configuration and the managed egress proxy. The fetch is idle-bounded with
process-group cleanup and stores only missing remote objects in source-derived
disjoint scratch; a proof-local alternate reads the historical objects.
Ambient Git config, object redirection, HOME, and TMP settings are removed, and
the source checkout's objects, refs, indexes, metadata, and content remain
read-only.

Historical-root shrink refuses any Git lock, non-ignored untracked file,
gitlink/submodule, unexpected tracked difference, non-stage-zero entry,
assume-unchanged/skip-worktree bit, or sparse checkout. `dev-all` declares only
three generated setup-layer tracked exceptions: `.codex/config.toml`,
`scripts/devops/governance-control-plane`, and
`scripts/devops/governance-mcp-stdio-forward`. Separately, only ignored residue
below the real directory roots `.bsp`, `logs`, `project/target`, and `target`
may leave with the staged worktree. Global config and `.git/info/exclude` do not
authorize any other ignored path. These declarations are exact paths, not
globs or operator overrides. Retirement still stages only the surplus
worktree, home, and state through the durable shrink journal. The historical
metadata and branch ref remain byte-identical until a later proven-idle
whole-prefix reset.

## Down

Stop broker state for the overlay and clean lifecycle metadata:

```bash
kit/scripts/devkit -p dev-all down --repo ouroboros-ide --count 3
```

Use the same broker socket and state-root overrides on `down` that were used on
`up` when running isolated smokes.

## Troubleshooting

Broker:

```bash
kit/scripts/devkit -p dev-all broker status --format json
kit/scripts/devkit -p dev-all logs --repo ouroboros-ide --tail 50 --format json
```

If brokered OCI commands fail, confirm that `DOCKER_HOST` points at the
broker socket in `exec`, that the socket exists, and that the requested image is
allowed by overlay policy.

Egress:

```bash
kit/scripts/devkit -p dev-all exec 1 --repo ouroboros-ide -- bash -lc 'env | grep -E "HTTP_PROXY|HTTPS_PROXY|NO_PROXY"'
```

A blocked host should fail through the proxy; an allowed host should connect.
`make native-runtime-smoke` exercises both paths.

Nix:

Overlay runtime refs are intentionally overlay-local, for example
`./overlays/dev-all#default`. Automation must pass `--output-lock-file
/dev/null` when running overlay flakes directly. Do not commit generated
`overlays/*/flake.lock` files.

Decision: keep overlay flakes lockless. The root `flake.lock` remains the
single pin source; committing one lock per overlay would remove the warning but
would duplicate pins and create drift risk. The supported friction reduction is
to use devkit commands or smoke scripts, all of which pass `--output-lock-file
/dev/null`
for direct overlay checks.

Auth:

Codex and SSH state live under each agent home. For `dev-all`, the active
Codex home is repo-local:

- agent 1: `<dev-root>/<repo>/.devhome-agent1/.codex`
- agent N: `<dev-root>/agent-worktrees/agentN/.devhome-agentN/.codex`

The native state root under `.devkit/native-agents/<project>-agentN/` remains
for manifests, resolver state, broker metadata, and legacy imports. Native
prepare imports only missing Codex files from the old
`.devkit/native-agents/<project>-agentN/home/.codex` location; it does not
delete files or overwrite existing auth, config, sessions, logs, rollouts,
shell snapshots, or SQLite state.

To inspect the active state:

```bash
kit/scripts/devkit -p dev-all exec 1 --repo ouroboros-ide -- bash -lc 'printf "HOME=%s\nCODEX_HOME=%s\n" "$HOME" "$CODEX_HOME"; ls -la "$CODEX_HOME"'
```

Use the native reseed commands when auth needs to be refreshed:

```bash
kit/scripts/devkit -p dev-all codex-auth reseed 1 --repo ouroboros-ide
kit/scripts/devkit -p dev-all ssh-setup /path/to/id_ed25519 --index 1
```

## Add A New Overlay Flake

1. Create `overlays/<overlay>/runtime.nix`.
2. Create `overlays/<overlay>/flake.nix` as a thin wrapper around the root
   flake output for that overlay.
3. Add `runtime.flake: ./overlays/<overlay>#default` to
   `overlays/<overlay>/devkit.yaml`.
4. Add `runtime.codex_version` and `runtime.core_check`.
5. Add the overlay to `flake.nix` root outputs and to smoke scripts when it is
   intended to be part of the supported matrix.
6. Run:

```bash
nix --extra-experimental-features 'nix-command flakes' flake check
nix --extra-experimental-features 'nix-command flakes' develop --command nix/validate-overlay-runtimes.py overlays
kit/scripts/devkit --dry-run -p dev-all runtime-matrix --all --check
make overlay-runtime-smoke
make native-overlay-matrix
```

## Local And CI Gates

Cheap CI-facing gate:

```bash
make ci-cheap
```

Full local readiness gate:

```bash
make native-e2e-lifecycle
make native-overlay-e2e-matrix
make native-runtime-smoke
make postgres-broker-container-smoke
```

`make native-e2e-lifecycle` runs real native `up`, `status --ready`, `exec`,
stdin-driven `attach`, `scale`, and `down` cycles for `dev-all` and the
configured smaller overlay, defaulting to `ouroboros-static-front-end`.

`make native-overlay-e2e-matrix` classifies real overlays as `e2e-pass`,
`runtime-pass`, or `not-locally-runnable` with per-overlay evidence under
`/tmp`. It preserves the cheap CI boundary because full E2E coverage expects
sibling repository checkouts and creates temporary native Git worktrees.

`make postgres-broker-container-smoke` starts the Nix-built Postgres broker,
denies a Redis create request, pulls/creates/starts/inspects/deletes a real
Postgres container through the broker socket, and then cleans up.

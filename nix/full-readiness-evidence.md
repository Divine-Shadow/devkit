# Full Native Readiness Evidence

Date: 2026-05-12

Command shape:

```bash
git -C /home/bayesartre/dev/ouroboros-ide worktree add --detach \
  /home/bayesartre/dev/ouroboros-ide-nix-readiness origin/main

kit/scripts/devkit -p dev-all down --repo ouroboros-ide-nix-readiness \
  --count 1 \
  --format json

kit/scripts/devkit -p dev-all up --repo ouroboros-ide-nix-readiness \
  --count 1 \
  --branch-prefix nixready-agent \
  --flake .#dev-all \
  --skip-ready \
  --format json

kit/scripts/devkit -p dev-all exec 1 --repo ouroboros-ide-nix-readiness \
  --flake .#dev-all -- bash -lc \
  'GIT_SSH_COMMAND="ssh -o ProxyCommand=none -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=$HOME/.ssh/known_hosts -i $HOME/.ssh/id_ed25519" git ls-remote --heads origin >/dev/null && purs --version >/dev/null && spago --version >/dev/null && netlify --version >/dev/null && deno --version >/dev/null && playwright --version >/dev/null'

kit/scripts/devkit -p dev-all ensure-ready --repo ouroboros-ide-nix-readiness \
  --count 1 \
  --branch-prefix nixready-agent \
  --flake .#dev-all \
  --format json

kit/scripts/devkit -p dev-all ensure-ready --repo ouroboros-ide --count 1 \
  --flake .#dev-all \
  --skip-repo-checks \
  --format json

kit/scripts/devkit -p dev-all down --repo ouroboros-ide-nix-readiness \
  --count 1 \
  --format json
```

## Result

- Clean linked-source-worktree `up --skip-ready` exit code: `0`.
- Clean linked-source-worktree `exec` smoke exit code: `0`.
- Clean linked-source-worktree full `ensure-ready` exit code: `0`.
- Runtime-only active-repo `ensure-ready --skip-repo-checks` exit code: `0`.
- `down` exit code: `0` for cleanup.
- Native runtime: `runtime_ready: 1/1`.
- Launchable capacity: `capacity_available: 1/1`.
- Repo readiness: `repo_ready: 1/1`.
- Broker cleanup: final `down` reported `running: false`.
- Clean evidence artifacts:
  `/tmp/devkit-clean-lifecycle-up-skip-ready-20260512.json`,
  `/tmp/devkit-clean-lifecycle-exec-tools-20260512.out`, and
  `/tmp/devkit-clean-lifecycle-ensure-ready-20260512.json`.

## Passing Checks

- `prepare-state`
- `sandbox-command`
- `broker-socket`
- `required-tools`
- `purescript-spago-netlify`
- `playwright-browser`
- `git-remote`
- `frontend-warm`
- `frontend-playwright-browser`
- `frontend-netlify-dev-server`
- `frontend-purescript-build`
- `frontend-typecheck`
- `frontend-test`
- `core-check`

The frontend test evidence included `10` Vitest files and `26` tests passing
after lockfile install, the Nix `purs` shim, `better-sqlite3` rebuild, Spago
build, generated PureScript typings, and Vitest.

The core check now passes:

```bash
bash scripts/sbt2 "Compile / compile"
```

Observed SBT result:

- `submit-to-ci`, `backend`, `invariants`, `services`, and the remaining
  aggregate compile graph completed.
- Representative final line:
  `[success] elapsed time: 23 s, cache 100%, 402 disk cache hits`.

## Additional Probe

The same core check also passed in an isolated clean worktree mounted through
the native agent1 path:

```bash
kit/scripts/devkit -p dev-all native exec \
  --repo ouroboros-ide-core-readiness \
  --flake .#dev-all \
  -- bash -lc 'bash scripts/sbt2 "Compile / compile"'
```

Result: exit code `0`, `[success] elapsed time: 245 s (0:04:05.0), cache 77%,
312 disk cache hits, 90 onsite tasks`.

## Operational Caveat

`ensure-ready` starts or reuses the managed broker by default. Use
`--skip-broker` only when intentionally checking current broker state without
starting it first. A stopped-broker probe with `--skip-broker --skip-repo-checks`
returned exit code `2`, reported `broker-socket` as false, and left broker
status at `running: false`.

A socket-only broker override probe also passed:

```bash
kit/scripts/devkit -p dev-all ensure-ready --repo ouroboros-ide --count 1 \
  --flake .#dev-all \
  --broker-socket /tmp/devkit-broker-socket-only.<id>/broker.sock \
  --skip-repo-checks \
  --format json
```

The result reported the temporary socket and derived state root instead of the
default managed broker state, then stopped cleanly with `broker stop`.

Native worktrees created from a source path that is itself a Git worktree are
now covered by the clean readiness evidence above. `native prepare` writes
absolute `.git` gitdir metadata for native agent worktrees, and the sandbox plan
binds the dev root both at `/workspaces/dev` and at its host path so those Git
metadata paths resolve inside bubblewrap.
